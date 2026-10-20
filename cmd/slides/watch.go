package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/go-slides/slides"
	"github.com/go-slides/slides/deck"
	"github.com/go-slides/slides/internal/serve"
	"github.com/go-slides/slides/project"
)

// debounce is how long a burst of file events is waited out: an editor that
// saves a deck writes it more than once.
const debounce = 100 * time.Millisecond

// browserOpen opens a URL in the default browser; tests replace it.
var browserOpen = openBrowser

// openBrowser opens an http, https or file URL in the system's browser. It
// starts the program that does it and does not wait. Nothing but the URL is
// passed on, and only as an argument, never to a shell.
func openBrowser(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "file") {
		return fmt.Errorf("will not open %q in a browser", raw)
	}
	var c *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		c = exec.Command("open", raw)
	case "windows":
		c = exec.Command("rundll32", "url.dll,FileProtocolHandler", raw)
	default:
		c = exec.Command("xdg-open", raw)
	}
	if err := c.Start(); err != nil {
		return err
	}
	go c.Wait()
	return nil
}

// isLoopback reports whether a listen host is the machine's own.
func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// serverDir is the directory --server serves: --input-dir, or the one
// directory named, as marp-cli.
func (o *options) serverDir(args []string) (string, error) {
	switch {
	case o.inputDir != "" && len(args) > 0:
		return "", errors.New("cannot pass files together with input directory")
	case o.inputDir != "":
		return o.inputDir, nil
	case len(args) > 1:
		return "", errors.New("server mode have to specify just one directory")
	case len(args) == 1:
		return args[0], nil
	}
	return "", errors.New("server mode needs a directory: slides --server dir")
}

// converter answers the server's requests: it converts the deck at a path as
// the query asks, with the options of the command line. At most a few decks
// are converted at once, whatever the number of requests.
type converter struct {
	o   *options
	e   env
	sem chan struct{}
}

func (c *converter) convert(ctx context.Context, path string, kind serve.Kind, ws string) (serve.Result, error) {
	select {
	case c.sem <- struct{}{}:
		defer func() { <-c.sem }()
	case <-ctx.Done():
		return serve.Result{}, ctx.Err()
	}
	o := *c.o // a request must not change the options of the next
	o.output, o.mirrorBase, o.mirrored = "", "", false
	src, err := os.ReadFile(path)
	if err != nil {
		return serve.Result{}, err
	}
	cfg, err := o.config(path)
	if err != nil {
		return serve.Result{}, err
	}
	e := c.e
	switch kind {
	case serve.HTML:
		cfg.WatchWS = ws
		page, b, err := slides.HTML(src, cfg)
		if err != nil {
			return serve.Result{}, err
		}
		e.warn(b.Warnings)
		return serve.Result{Data: []byte(page), ContentType: "text/html; charset=utf-8"}, nil
	case serve.Notes:
		d, err := deck.Parse(src, cfg.Parse)
		if err != nil {
			return serve.Result{}, err
		}
		return serve.Result{Data: []byte(notesText(d)), ContentType: "text/plain; charset=utf-8"}, nil
	}
	format, ct := "", ""
	switch kind {
	case serve.PDF:
		format, ct = "pdf", "application/pdf"
	case serve.PPTX:
		format, ct = "pptx", "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	case serve.PNG:
		format, ct = "image:png", "image/png"
	case serve.JPEG:
		format, ct = "image:jpeg", "image/jpeg"
	default:
		return serve.Result{}, fmt.Errorf("cannot convert to %s", kind)
	}
	b, err := slides.Render(src, cfg, deck.FrameBox)
	if err != nil {
		return serve.Result{}, err
	}
	e.warn(b.Warnings)
	opts, err := o.exportOptions(path, format)
	if err != nil {
		return serve.Result{}, err
	}
	data, err := exportBytes(ctx, &o, b, opts, path, format)
	if err != nil {
		return serve.Result{}, err
	}
	return serve.Result{Data: data, ContentType: ct}, nil
}

// extraTargets are the files and directories other than the decks whose
// change changes a build: the theme sets, the kinds and data files, the
// configuration file.
func (o *options) extraTargets(baseDir string) []string {
	var out []string
	for _, group := range [][]string{o.themeSets, o.kindFiles, o.dataFiles} {
		for _, p := range group {
			if _, err := os.Stat(p); err == nil {
				out = append(out, p)
			}
		}
	}
	if p := project.ConfigPath(o.project(), baseDir); p != "" {
		if _, err := os.Stat(p); err == nil {
			out = append(out, p)
		}
	}
	return out
}

// runServer is --server: it serves the directory, converting its decks on
// request, until the context ends. With --preview it opens the browser on it.
func runServer(cmd *cobra.Command, o *options, args []string) error {
	dir, err := o.serverDir(args)
	if err != nil {
		return err
	}
	e := env{ctx: cmd.Context(), stdout: cmd.OutOrStdout(), stderr: cmd.ErrOrStderr()}
	hub := serve.NewHub()
	host := o.host
	if host == "" {
		host = "127.0.0.1"
	}
	// A wildcard address is reached under any name the machine has, so the
	// Host header cannot be checked against one.
	ip := net.ParseIP(host)
	exposed := ip != nil && ip.IsUnspecified()
	if !isLoopback(host) {
		fmt.Fprintf(e.stderr, "slides: warning: listening on %s: the directory is served to the network, to anyone who can reach this port\n", host)
	}
	conv := &converter{o: o, e: e, sem: make(chan struct{}, max(1, o.workers(2)))}
	srv, err := serve.New(serve.Options{
		Dir:            dir,
		Host:           host,
		Convert:        conv.convert,
		DirectoryIndex: []string{"index.md", "PITCHME.md"},
		Hub:            hub,
		Exposed:        exposed,
		Log:            func(format string, args ...any) { fmt.Fprintf(e.stderr, "slides: "+format+"\n", args...) },
	})
	if err != nil {
		return err
	}
	addr, err := srv.Start()
	if err != nil {
		return err
	}
	defer srv.Stop()
	shown := addr
	if h, p, err := net.SplitHostPort(addr); err == nil {
		if ip := net.ParseIP(h); ip != nil && ip.IsUnspecified() {
			shown = net.JoinHostPort("localhost", p)
		}
	}
	root := srv.Root()
	fmt.Fprintf(e.stderr, "slides: [Server mode] Start server listened at http://%s/ ...\n", shown)

	targets := append([]string{root}, o.extraTargets(root)...)
	go func() {
		err := serve.Watch(e.ctx, targets, debounce, func(paths []string) {
			all := false
			for _, p := range paths {
				if serve.IsMarkdown(p) {
					hub.Notify(p)
				} else {
					all = true
				}
			}
			if all {
				hub.NotifyAll()
			}
		})
		if err != nil {
			fmt.Fprintln(e.stderr, "slides: warning: not watching for changes:", err)
		}
	}()
	if o.preview {
		if err := browserOpen("http://" + shown + "/"); err != nil {
			fmt.Fprintln(e.stderr, "slides: warning: cannot open the browser:", err)
		}
	}
	<-e.ctx.Done()
	return nil
}

// runWatch is --watch and --preview: it builds the decks, then builds again
// the ones that change, and tells the pages showing them to reload. A deck
// that fails to build is reported and watched still.
func runWatch(cmd *cobra.Command, o *options, jobs []job) error {
	e := env{ctx: cmd.Context(), stdout: cmd.OutOrStdout(), stderr: cmd.ErrOrStderr()}
	for _, j := range jobs {
		if j.path == "-" {
			return errors.New("cannot watch standard input")
		}
	}
	if o.output == "-" {
		return errors.New("cannot watch with the output on standard output")
	}
	hub := serve.NewHub()
	port, stop, err := hub.ListenNotifier(0)
	if err != nil {
		return err
	}
	defer stop()
	o.hub, o.hubPort = hub, port

	build := func(e env, o *options, j job) error {
		err := buildOne(e, o, j)
		if err == nil {
			hub.Notify(j.path)
		}
		return err
	}
	if err := runJobs(e, o, jobs, build); err != nil {
		fmt.Fprintln(e.stderr, "slides:", err)
	}
	if o.preview {
		o.previewDecks(e, jobs)
	}
	fmt.Fprintln(e.stderr, "slides: [Watch mode] Start watching...")

	var targets []string
	byPath := map[string]job{}
	for _, j := range jobs {
		abs, err := filepath.Abs(j.path)
		if err != nil {
			return err
		}
		byPath[abs] = j
		if o.inputDir == "" {
			targets = append(targets, abs)
		}
	}
	baseDir := "."
	if len(jobs) > 0 {
		baseDir = filepath.Dir(jobs[0].path)
	}
	if o.inputDir != "" {
		root, err := filepath.Abs(o.inputDir)
		if err != nil {
			return err
		}
		targets = append(targets, root)
		baseDir = root
	}
	targets = append(targets, o.extraTargets(baseDir)...)

	err = serve.Watch(e.ctx, targets, debounce, func(paths []string) {
		var todo []job
		all := false
		for _, p := range paths {
			if j, ok := byPath[p]; ok {
				if _, err := os.Stat(p); err == nil {
					todo = append(todo, j)
				}
				continue
			}
			if o.inputDir != "" && serve.IsMarkdown(p) {
				root, _ := filepath.Abs(o.inputDir)
				if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() && !strings.Contains(filepath.ToSlash(strings.TrimPrefix(p, root)), "/node_modules/") {
					j := job{path: p, dir: root}
					byPath[p] = j
					todo = append(todo, j)
				}
				continue
			}
			all = true
		}
		if all {
			todo = todo[:0]
			if o.inputDir != "" {
				root, _ := filepath.Abs(o.inputDir)
				files, _ := findDecks(root)
				for _, f := range files {
					todo = append(todo, job{path: f, dir: root})
				}
			} else {
				for _, j := range byPath {
					todo = append(todo, j)
				}
			}
		}
		slices.SortFunc(todo, func(a, b job) int { return strings.Compare(a.path, b.path) })
		todo = slices.Compact(todo)
		if len(todo) == 0 {
			return
		}
		if err := runJobs(e, o, todo, build); err != nil {
			fmt.Fprintln(e.stderr, "slides: failed converting:", err)
		}
	})
	if err != nil {
		return err
	}
	return nil
}

// previewDecks opens the HTML pages just built in the browser: all of them
// up to three, else only the first, so as not to bury the desktop in tabs.
func (o *options) previewDecks(e env, jobs []job) {
	var opened int
	for _, j := range jobs {
		jo, err := o.forJob(j)
		if err != nil {
			continue
		}
		f, err := jo.format()
		if err != nil || f != "html" {
			continue
		}
		out := jo.output
		if out == "" {
			out = jo.base(j.path) + ".html"
		}
		abs, err := filepath.Abs(out)
		if err != nil {
			continue
		}
		if opened >= 1 && len(jobs) > 3 {
			fmt.Fprintf(e.stderr, "slides: preview: not opening %s (more than three decks)\n", abs)
			continue
		}
		u := url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}
		if runtime.GOOS == "windows" {
			u.Path = "/" + u.Path
		}
		if err := browserOpen(u.String()); err != nil {
			fmt.Fprintln(e.stderr, "slides: warning: cannot open the browser:", err)
			return
		}
		opened++
	}
	if opened == 0 {
		fmt.Fprintln(e.stderr, "slides: warning: --preview shows HTML pages; nothing to show")
	}
}
