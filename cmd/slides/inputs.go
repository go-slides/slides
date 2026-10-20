package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"

	"github.com/go-slides/slides/internal/serve"
)

// job is one deck to build.
type job struct {
	// path is the deck's file, "-" for standard input.
	path string
	// dir is the input directory the deck was found under (--input-dir), or
	// "" for a deck named on the command line.
	dir string
}

// base is where a deck's output goes, without its extension: next to the
// deck; or, for a deck found by --input-dir and an --output directory, the
// deck's path under the input directory, mirrored under the output directory
// (marp-cli's File.convert).
func (o *options) base(path string) string {
	if o.mirrorBase != "" {
		return o.mirrorBase
	}
	return strings.TrimSuffix(path, filepath.Ext(path))
}

// forJob returns the options for building j: a copy, which a build may
// change without the others seeing it.
func (o *options) forJob(j job) (*options, error) {
	c := *o
	if j.dir != "" && o.output != "" && o.output != "-" {
		rel, err := filepath.Rel(j.dir, j.path)
		if err != nil || !filepath.IsLocal(rel) {
			// never written outside the output directory
			return nil, fmt.Errorf("%s is not under the input directory %s", j.path, j.dir)
		}
		c.mirrorBase = strings.TrimSuffix(filepath.Join(o.output, rel), filepath.Ext(rel))
		c.mirrored = true
		c.output = ""
	}
	return &c, nil
}

// findDecks returns the Markdown files under dir, sorted, without the ones
// in node_modules or under a dot (marp-cli's finder ignores them too). Links to
// directories are not followed; links to files are, when they lead to a file.
func findDecks(dir string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == dir {
				return err
			}
			return nil // a directory that cannot be read has no decks to find
		}
		// Like marp-cli's glob, dot files and directories are not looked in,
		// and node_modules neither.
		if d.IsDir() {
			if p != dir && (d.Name() == "node_modules" || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") || !serve.IsMarkdown(d.Name()) {
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			if st, err := os.Stat(p); err != nil || !st.Mode().IsRegular() {
				return nil
			}
		} else if !d.Type().IsRegular() {
			return nil
		}
		out = append(out, p)
		return nil
	})
	slices.Sort(out)
	return out, err
}

// inputs lists the decks to build: the files named, a directory named as the
// Markdown files under it, a pattern as the files it matches, "-" as standard
// input; or, with --input-dir, the Markdown files under that directory. As
// in marp-cli, naming files together with an input directory is an error.
func (o *options) inputs(args []string) ([]job, error) {
	if o.inputDir != "" {
		if len(args) > 0 {
			return nil, errors.New("cannot pass files together with input directory")
		}
		root, err := filepath.Abs(o.inputDir)
		if err != nil {
			return nil, err
		}
		st, err := os.Stat(root)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil, fmt.Errorf("input directory %q is not found", root)
			}
			return nil, err
		}
		if !st.IsDir() {
			return nil, fmt.Errorf("%q is not directory", root)
		}
		files, err := findDecks(root)
		if err != nil {
			return nil, err
		}
		jobs := make([]job, len(files))
		for i, f := range files {
			jobs[i] = job{path: f, dir: root}
		}
		return jobs, nil
	}
	var jobs []job
	seen := map[string]bool{}
	add := func(p string) {
		abs, err := filepath.Abs(p)
		if err != nil {
			abs = p
		}
		if !seen[abs] {
			seen[abs] = true
			jobs = append(jobs, job{path: p})
		}
	}
	for _, a := range args {
		if a == "-" {
			if !seen["-"] {
				seen["-"] = true
				jobs = append(jobs, job{path: "-"})
			}
			continue
		}
		st, err := os.Stat(a)
		switch {
		case err == nil && st.IsDir():
			files, err := findDecks(a)
			if err != nil {
				return nil, err
			}
			for _, f := range files {
				add(f)
			}
		case err == nil:
			add(a)
		default:
			// a pattern, as the shell would have expanded it
			if !strings.ContainsAny(a, "*?[") {
				add(a) // the build says what is wrong with it
				continue
			}
			matches, gerr := filepath.Glob(a)
			if gerr != nil {
				return nil, fmt.Errorf("%s: %w", a, gerr)
			}
			slices.Sort(matches)
			for _, m := range matches {
				if st, err := os.Stat(m); err == nil && st.Mode().IsRegular() {
					add(m)
				}
			}
		}
	}
	return jobs, nil
}

// workers is how many decks are built at once: --parallel, which is marp-cli's
// 5 by default, but never more than the machine has cores — a deck's own
// rendering already uses them — and one when the output is standard output,
// where two decks would interleave.
func (o *options) workers(jobs int) int {
	n := o.parallel
	if n <= 0 {
		n = 1
	}
	n = min(n, runtime.GOMAXPROCS(0), jobs)
	if o.output == "-" {
		n = 1
	}
	return max(n, 1)
}

// runJobs builds the decks, up to o.workers at once, as marp-cli's -P. Each
// build's messages are kept and printed as a block when it ends, so that they
// do not interleave; the errors are collected and none stops the others.
func runJobs(e env, o *options, jobs []job, build func(env, *options, job) error) error {
	n := o.workers(len(jobs))
	type result struct {
		j   job
		err error
	}
	var (
		mu      sync.Mutex
		results []result
		next    int
		wg      sync.WaitGroup
	)
	take := func() (job, bool) {
		mu.Lock()
		defer mu.Unlock()
		if next >= len(jobs) || e.ctx.Err() != nil {
			return job{}, false
		}
		j := jobs[next]
		next++
		return j, true
	}
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				j, ok := take()
				if !ok {
					return
				}
				var out io.Writer = e.stdout
				var errBuf bytes.Buffer
				je := env{ctx: e.ctx, stdout: out, stderr: &errBuf}
				if n == 1 {
					je.stderr = e.stderr
				}
				jo, err := o.forJob(j)
				if err == nil {
					err = build(je, jo, j)
				}
				mu.Lock()
				if n > 1 {
					io.Copy(e.stderr, &errBuf)
				}
				results = append(results, result{j, err})
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	var failed []string
	var first error
	for _, r := range results {
		if r.err != nil {
			if first == nil {
				first = r.err
			}
			failed = append(failed, fmt.Sprintf("%s: %v", r.j.path, r.err))
		}
	}
	switch {
	case first == nil:
		return e.ctx.Err()
	case len(results) == 1 && len(jobs) == 1:
		return first
	}
	return fmt.Errorf("%d of %d decks failed:\n  %s", len(failed), len(jobs), strings.Join(failed, "\n  "))
}
