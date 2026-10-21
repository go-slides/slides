package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const presenterDeck = "# Slide A\n\n<!-- note <b>one</b> & \"q\" -->\n\n---\n\n# Slide B\n\n* x\n* y\n\n<!-- second\n\npara -->\n\n---\n\n# Slide C\n"

// TestPresenterPage checks the page the build writes: the notes of the slides
// that have them, escaped, in elements the style sheet hides; the presenter
// view's script and styles; nothing of it with --template bare.
func TestPresenterPage(t *testing.T) {
	dir := t.TempDir()
	md := write(t, dir, "d.md", presenterDeck)
	out := filepath.Join(dir, "o.html")
	if _, stderr, err := run(t, "build", md, "-o", out); err != nil {
		t.Fatal(err, stderr)
	}
	b, _ := os.ReadFile(out)
	page := string(b)
	for _, want := range []string{
		`<div class="slides-note" data-index="0" tabindex="0"><p>note &lt;b&gt;one&lt;/b&gt; &amp; "q"</p></div>`,
		`<div class="slides-note" data-index="1" tabindex="0"><p>second

para</p></div>`,
		".slides-note { display: none; }", "bespoke-marp-sync-", "view=presenter", `"presenter":true`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if strings.Contains(page, `data-index="2"`) {
		t.Error("a slide without notes has no note element")
	}
	if strings.Contains(page, "<b>one</b>") {
		t.Error("a note is text, not markup")
	}
	// the bare template has none of it
	bare := filepath.Join(dir, "bare.html")
	if _, stderr, err := run(t, "build", md, "-o", bare, "--bare"); err != nil {
		t.Fatal(err, stderr)
	}
	b, _ = os.ReadFile(bare)
	if strings.Contains(string(b), "slides-note") || strings.Contains(string(b), "bespoke-marp-sync") {
		t.Error("--bare has no presenter view")
	}
	// the size of the view: reported, with a ceiling
	var plain bytesCount
	plain.page = len(page)
	// 15500 with the overview view; +1000 for the sync through the parent
	// window (viewer.SyncParent), about 650 bytes.
	if n := len(presenterAdded(page)); n > 16500 {
		t.Errorf("the presenter and overview views add %d bytes to every page: keep the viewer small (16500)", n)
	}
	t.Logf("a deck with notes: %d bytes of page, of which the presenter view is %d bytes of script and style", plain.page, len(presenterAdded(page)))
}

type bytesCount struct{ page int }

// presenterAdded is the part of a page that is the presenter view's: its
// styles and the lines of script about it, by markers; a measure, not a
// parser.
func presenterAdded(page string) string {
	_, css, _ := strings.Cut(page, ".slides-note { display: none; }")
	css, _, _ = strings.Cut(css, "</style>")
	_, js, _ := strings.Cut(page, "let key = null")
	js, _, _ = strings.Cut(js, "  addEventListener('beforeprint'")
	return css + js
}

// TestPresenterInChrome drives the page and its presenter window in headless
// Chrome (puppeteer-core and Chrome found as marp-cli's install has them, or
// by PUPPETEER_CORE and CHROME): the p key opens the presenter at the URL
// marp-cli's has, the two windows follow each other both ways through
// localStorage, the next step is shown in the frame, the notes are text.
func TestPresenterInChrome(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	pup := os.Getenv("PUPPETEER_CORE")
	if pup == "" {
		pup = "/opt/homebrew/Cellar/marp-cli/4.5.1/libexec/lib/node_modules/@marp-team/marp-cli/node_modules/puppeteer-core"
	}
	chrome := os.Getenv("CHROME")
	if chrome == "" {
		chrome = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
	}
	for _, p := range []string{pup, chrome} {
		if _, err := os.Stat(p); err != nil {
			t.Skip(p, "is not there")
		}
	}
	dir := t.TempDir()
	md := write(t, dir, "d.md", presenterDeck)
	page := filepath.Join(dir, "o.html")
	if _, stderr, err := run(t, "build", md, "-o", page); err != nil {
		t.Fatal(err, stderr)
	}
	cmd := exec.Command(node, filepath.Join("testdata", "presenter-check.js"), page)
	cmd.Env = append(os.Environ(), "PUPPETEER_CORE="+pup, "CHROME="+chrome)
	cmd.Stdin = nil
	b, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, b)
	}
	var r struct {
		MainAccessibleText string
		MainNoteDisplay    string
		PresUrl            string
		PresTitle          string
		SyncParamRemoved   bool
		PresPage           string
		PresNote           string
		PresNoteBold       int
		NextFrameActive    int
		AfterMainRight     struct {
			Main, Presenter, Next int
			Page, Note            string
		}
		AfterPresRight1 struct{ Main, Presenter, FragmentsShown, Next int }
		AfterPresRight3 struct {
			Main, Presenter int
			NextDisabled    bool
		}
		AfterPresLeft    struct{ Main, Presenter int }
		NoteScale        string
		Timer            string
		AfterNextClick   struct{ Main, Presenter int }
		Stored           []string
		StoredAfterClose []string
		Errors           []string
	}
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatalf("%v\n%s", err, b)
	}
	if len(r.Errors) > 0 {
		t.Errorf("page errors: %v", r.Errors)
	}
	if strings.Contains(r.MainAccessibleText, "note") || strings.Contains(r.MainAccessibleText, "second") || r.MainNoteDisplay != "none" {
		t.Errorf("the notes are in the audience's text: %q %q", r.MainAccessibleText, r.MainNoteDisplay)
	}
	if r.PresUrl != "o.html?view=presenter#1" || !r.SyncParamRemoved || r.PresTitle != "[Presenter view] - Slide A" {
		t.Errorf("presenter window: %q %v %q", r.PresUrl, r.SyncParamRemoved, r.PresTitle)
	}
	if r.PresPage != "1 / 3" || r.PresNote != `<p>note &lt;b&gt;one&lt;/b&gt; &amp; "q"</p>` || r.PresNoteBold != 0 || r.NextFrameActive != 1 {
		t.Errorf("presenter at the start: page %q note %q bold %d next %d", r.PresPage, r.PresNote, r.PresNoteBold, r.NextFrameActive)
	}
	if a := r.AfterMainRight; a.Main != 1 || a.Presenter != 1 || a.Page != "2 / 3" || a.Note != "second\n\npara" || a.Next != 1 {
		t.Errorf("the main window moved: %+v", a)
	}
	if a := r.AfterPresRight1; a.Main != 1 || a.Presenter != 1 || a.FragmentsShown != 1 {
		t.Errorf("the presenter revealed a fragment: %+v", a)
	}
	if a := r.AfterPresRight3; a.Main != 2 || a.Presenter != 2 || !a.NextDisabled {
		t.Errorf("the presenter reached the end: %+v", a)
	}
	if a := r.AfterPresLeft; a.Main != 1 || a.Presenter != 1 {
		t.Errorf("the presenter went back: %+v", a)
	}
	if r.NoteScale != "1.2000" || !strings.HasPrefix(r.Timer, "00:00:") {
		t.Errorf("notes scale %q timer %q", r.NoteScale, r.Timer)
	}
	if a := r.AfterNextClick; a.Main != 2 || a.Presenter != 2 {
		t.Errorf("a click on the next step: %+v", a)
	}
	if len(r.Stored) != 1 || !strings.Contains(r.Stored[0], `"reference":2`) || len(r.StoredAfterClose) != 1 || !strings.Contains(r.StoredAfterClose[0], `"reference":1`) {
		t.Errorf("the shared state: %q then %q", r.Stored, r.StoredAfterClose)
	}
}
