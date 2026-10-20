package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/go-slides/slides/internal/serve"
)

// decks makes
//
//	in/a.md  in/sub/b.md  in/sub/deep/c.markdown  in/node_modules/x.md  in/readme.txt
func decks(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range map[string]string{
		"in/a.md":                   "# A\n\n<!-- note a -->\n",
		"in/sub/b.md":               "# B\n",
		"in/sub/deep/c.markdown":    "# C\n\n<!-- note c -->\n",
		"in/node_modules/x.md":      "# X\n",
		"in/readme.txt":             "not a deck",
		"in/UPPER.MD":               "# U\n",
		"in/.hidden/not-skipped.md": "# H\n",
	} {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func TestInputDirMirrorsTheTree(t *testing.T) {
	dir := decks(t)
	in, out := filepath.Join(dir, "in"), filepath.Join(dir, "out")
	if _, stderr, err := run(t, "build", "-I", in, "-o", out); err != nil {
		t.Fatal(err, stderr)
	}
	for _, want := range []string{"a.html", "sub/b.html", "sub/deep/c.html"} {
		if !exists(filepath.Join(out, filepath.FromSlash(want))) {
			t.Errorf("missing %s", want)
		}
	}
	// as marp-cli -I: no node_modules, no dot directory, no UPPER.MD
	for _, bad := range []string{"node_modules/x.html", "readme.html", "readme.txt", "UPPER.html", ".hidden/not-skipped.html"} {
		if exists(filepath.Join(out, filepath.FromSlash(bad))) {
			t.Errorf("%s should not be written", bad)
		}
	}
	if exists(filepath.Join(in, "a.html")) {
		t.Error("with --output, nothing is written next to the decks")
	}
	b, _ := os.ReadFile(filepath.Join(out, "sub", "deep", "c.html"))
	if !strings.Contains(string(b), "<h1 id=\"c\">C</h1>") {
		t.Errorf("c.html:\n%.300s", b)
	}

	// the format comes from the flags: notes mirror as .txt, a PDF as .pdf
	out2 := filepath.Join(dir, "out.d") // a dot in its name says nothing about the format
	if _, stderr, err := run(t, "build", "--notes", "-I", in, "-o", out2); err != nil {
		t.Fatal(err, stderr)
	}
	if b, err := os.ReadFile(filepath.Join(out2, "sub", "deep", "c.txt")); err != nil || string(b) != "note c" {
		t.Errorf("c.txt = %q, %v", b, err)
	}
	if _, stderr, err := run(t, "build", "--pdf", "-I", filepath.Join(in, "sub"), "-o", filepath.Join(dir, "pdfs")); err != nil {
		t.Fatal(err, stderr)
	}
	for _, want := range []string{"b.pdf", "deep/c.pdf"} {
		if b, err := os.ReadFile(filepath.Join(dir, "pdfs", filepath.FromSlash(want))); err != nil || !strings.HasPrefix(string(b), "%PDF") {
			t.Errorf("%s: %v", want, err)
		}
	}
}

func TestInputDirWithoutOutputWritesNextToTheDecks(t *testing.T) {
	dir := decks(t)
	in := filepath.Join(dir, "in")
	if _, stderr, err := run(t, "-I", in); err != nil { // the root command builds
		t.Fatal(err, stderr)
	}
	for _, want := range []string{"a.html", "sub/b.html", "sub/deep/c.html"} {
		if !exists(filepath.Join(in, filepath.FromSlash(want))) {
			t.Errorf("missing %s", want)
		}
	}
}

func TestInputDirErrors(t *testing.T) {
	dir := decks(t)
	in := filepath.Join(dir, "in")
	if _, _, err := run(t, "build", "-I", in, filepath.Join(in, "a.md")); err == nil || !strings.Contains(err.Error(), "cannot pass files together with input directory") {
		t.Errorf("files with -I: %v", err)
	}
	if _, _, err := run(t, "build", "-I", filepath.Join(dir, "missing")); err == nil || !strings.Contains(err.Error(), "is not found") {
		t.Errorf("missing directory: %v", err)
	}
	if _, _, err := run(t, "build", "-I", filepath.Join(in, "a.md")); err == nil || !strings.Contains(err.Error(), "is not directory") {
		t.Errorf("a file as directory: %v", err)
	}
	empty := filepath.Join(dir, "empty")
	os.Mkdir(empty, 0o755)
	if _, stderr, err := run(t, "build", "-I", empty); err != nil || !strings.Contains(stderr, "no Markdown file") {
		t.Errorf("an empty directory: %v %q", err, stderr)
	}
}

func TestSeveralFiles(t *testing.T) {
	dir := decks(t)
	in := filepath.Join(dir, "in")
	a, b := filepath.Join(in, "a.md"), filepath.Join(in, "sub", "b.md")
	if _, stderr, err := run(t, "build", a, b, a); err != nil { // a twice is once
		t.Fatal(err, stderr)
	}
	if !exists(filepath.Join(in, "a.html")) || !exists(filepath.Join(in, "sub", "b.html")) {
		t.Error("both decks are built next to themselves")
	}
	if _, _, err := run(t, "build", a, b, "-o", filepath.Join(dir, "one.html")); err == nil || !strings.Contains(err.Error(), "multiple files") {
		t.Errorf("one output for two decks: %v", err)
	}
	// to standard output, one after the other
	stdout, _, err := run(t, "build", "--notes", a, filepath.Join(in, "sub", "deep", "c.markdown"), "-o", "-")
	if err != nil || stdout != "note anote c" {
		t.Errorf("-o - : %q, %v", stdout, err)
	}
	// a directory is the decks under it; a pattern the files it matches
	dir2 := filepath.Join(dir, "d2")
	os.MkdirAll(dir2, 0o755)
	os.WriteFile(filepath.Join(dir2, "x.md"), []byte("# x"), 0o644)
	os.WriteFile(filepath.Join(dir2, "y.md"), []byte("# y"), 0o644)
	if _, stderr, err := run(t, "build", filepath.Join(dir2, "*.md")); err != nil {
		t.Fatal(err, stderr)
	}
	if !exists(filepath.Join(dir2, "x.html")) || !exists(filepath.Join(dir2, "y.html")) {
		t.Error("a pattern builds what it matches")
	}
	if _, _, err := run(t, "build", filepath.Join(dir, "nothing*.md")); err == nil || !strings.Contains(err.Error(), "not found processable") {
		t.Errorf("a pattern that matches nothing: %v", err)
	}
	if _, stderr, err := run(t, "build", filepath.Join(in, "sub")); err != nil {
		t.Fatal(err, stderr)
	}
	if !exists(filepath.Join(in, "sub", "deep", "c.html")) {
		t.Error("a directory is its decks, recursively")
	}
}

// TestDoubleStarPatterns: ** and braces in a file argument, as marp-cli's
// glob (the sets of files are compared with marp-cli's in internal/glob).
func TestDoubleStarPatterns(t *testing.T) {
	dir := decks(t)
	in := filepath.Join(dir, "in")
	if _, stderr, err := run(t, "build", filepath.Join(in, "**", "*.md")); err != nil {
		t.Fatal(err, stderr)
	}
	for _, want := range []string{"a.html", "sub/b.html"} {
		if !exists(filepath.Join(in, filepath.FromSlash(want))) {
			t.Errorf("missing %s", want)
		}
	}
	for _, bad := range []string{"node_modules/x.html", "sub/deep/c.html", "UPPER.html", ".hidden/not-skipped.html"} {
		if exists(filepath.Join(in, filepath.FromSlash(bad))) {
			t.Errorf("%s should not be built", bad)
		}
	}
	if _, stderr, err := run(t, "build", filepath.Join(in, "sub", "**", "*.{md,markdown}")); err != nil {
		t.Fatal(err, stderr)
	}
	if !exists(filepath.Join(in, "sub", "deep", "c.html")) {
		t.Error("braces and ** find sub/deep/c.markdown")
	}
	if _, _, err := run(t, "build", filepath.Join(in, "nothing", "**", "*.md")); err == nil || !strings.Contains(err.Error(), "not found processable") {
		t.Errorf("no match: %v", err)
	}
}

func TestParallelIsTheSameAsSerial(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in")
	os.MkdirAll(in, 0o755)
	for i := 0; i < 24; i++ {
		os.WriteFile(filepath.Join(in, fmt.Sprintf("d%02d.md", i)), []byte(fmt.Sprintf("# Deck %d\n\n---\n\nSlide %d\n", i, i)), 0o644)
	}
	outs := map[int]string{}
	for _, p := range []int{1, 4, 16} {
		out := filepath.Join(dir, fmt.Sprintf("out%d", p))
		_, stderr, err := run(t, "build", "-I", in, "-o", out, "-P", fmt.Sprint(p))
		if err != nil {
			t.Fatal(err, stderr)
		}
		if n := strings.Count(stderr, "=>"); n != 24 {
			t.Errorf("-P %d: %d messages for 24 decks:\n%s", p, n, stderr)
		}
		outs[p] = out
	}
	for i := 0; i < 24; i++ {
		name := fmt.Sprintf("d%02d.html", i)
		want, err := os.ReadFile(filepath.Join(outs[1], name))
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range []int{4, 16} {
			got, err := os.ReadFile(filepath.Join(outs[p], name))
			if err != nil || string(got) != string(want) {
				t.Errorf("%s differs with -P %d", name, p)
			}
		}
	}
}

func TestOneBadDeckDoesNotStopTheOthers(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "good1.md", "# 1")
	write(t, dir, "good2.md", "# 2")
	write(t, dir, "bad.md", "<!-- _kind: nosuchkind -->\n\n# bad")
	_, stderr, err := run(t, "build", "-I", dir, "-o", filepath.Join(dir, "out"))
	if err == nil || !strings.Contains(err.Error(), "1 of 3 decks failed") || !strings.Contains(err.Error(), "bad.md") {
		t.Fatalf("error %v\n%s", err, stderr)
	}
	if !exists(filepath.Join(dir, "out", "good1.html")) || !exists(filepath.Join(dir, "out", "good2.html")) {
		t.Error("the good decks were not built")
	}
	// a single bad deck reports its own error as before
	if _, _, err := run(t, "build", filepath.Join(dir, "bad.md")); err == nil || strings.Contains(err.Error(), "decks failed") {
		t.Errorf("single deck: %v", err)
	}
}

func TestInputDirDoesNotFollowLinksOutOfItself(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symbolic links")
	}
	dir := t.TempDir()
	in := filepath.Join(dir, "in")
	os.MkdirAll(in, 0o755)
	outside := filepath.Join(dir, "outside")
	os.MkdirAll(outside, 0o755)
	os.WriteFile(filepath.Join(outside, "o.md"), []byte("# o"), 0o644)
	os.Symlink(outside, filepath.Join(in, "linkdir"))
	os.WriteFile(filepath.Join(in, "a.md"), []byte("# a"), 0o644)
	os.Symlink("nowhere.md", filepath.Join(in, "dangling.md"))
	if _, stderr, err := run(t, "build", "-I", in, "-o", filepath.Join(dir, "out")); err != nil {
		t.Fatal(err, stderr)
	}
	if exists(filepath.Join(dir, "out", "linkdir", "o.html")) {
		t.Error("a link to a directory is not followed")
	}
	if !exists(filepath.Join(dir, "out", "a.html")) {
		t.Error("a.html missing")
	}
}

func TestForJobStaysInsideTheOutput(t *testing.T) {
	o := &options{output: "/out"}
	got, err := o.forJob(job{path: "/in/sub/x.md", dir: "/in"})
	if err != nil || filepath.ToSlash(got.mirrorBase) != "/out/sub/x" || !got.mirrored || got.output != "" {
		t.Errorf("forJob = %+v, %v", got, err)
	}
	for _, j := range []job{{path: "/elsewhere/x.md", dir: "/in"}, {path: "/in/../x.md", dir: "/in"}} {
		if _, err := o.forJob(j); err == nil {
			t.Errorf("forJob(%+v) should refuse", j)
		}
	}
	// no --output: nothing mirrored
	if got, _ := (&options{}).forJob(job{path: "/in/a.md", dir: "/in"}); got.mirrored || got.output != "" {
		t.Errorf("without --output: %+v", got)
	}
}

func FuzzForJob(f *testing.F) {
	f.Add("/in", "/in/a/b.md", "/out")
	f.Add("/in", "/in/../x.md", "/out")
	f.Add("in", "in/x/../../y.md", "out")
	f.Fuzz(func(t *testing.T, dir, path, output string) {
		if output == "" || output == "-" {
			return
		}
		got, err := (&options{output: output}).forJob(job{path: path, dir: dir})
		if err != nil || dir == "" || !got.mirrored {
			return
		}
		rel, rerr := filepath.Rel(output, got.mirrorBase)
		if rerr != nil || !filepath.IsLocal(rel) {
			t.Fatalf("forJob(dir=%q, path=%q, output=%q) = %q: outside the output", dir, path, output, got.mirrorBase)
		}
	})
}

func TestIsMarkdown(t *testing.T) {
	for name, want := range map[string]bool{"a.md": true, "a.MD": false, "a.mdown": true, "a.markdown": true, "a.markdn": true, "a.txt": false, "md": false, "a.md.bak": false} {
		if serve.IsMarkdown(name) != want {
			t.Errorf("isMarkdown(%q) = %v", name, !want)
		}
	}
}
