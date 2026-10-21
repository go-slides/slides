package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestOverviewPage checks what the page carries for the overview view: its
// styles, the controller's button, and nothing of it with --bare.
func TestOverviewPage(t *testing.T) {
	dir := t.TempDir()
	md := write(t, dir, "d.md", presenterDeck)
	out := filepath.Join(dir, "o.html")
	if _, stderr, err := run(t, "build", md, "-o", out); err != nil {
		t.Fatal(err, stderr)
	}
	b, _ := os.ReadFile(out)
	page := string(b)
	for _, want := range []string{".slides-overview", "html[data-slides-view=overview]", "'Overview view'", "closeOverview", "closeOnSelect", ".slides-ov-head"} {
		if !strings.Contains(page, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	bare := filepath.Join(dir, "bare.html")
	if _, stderr, err := run(t, "build", md, "-o", bare, "--bare"); err != nil {
		t.Fatal(err, stderr)
	}
	b, _ = os.ReadFile(bare)
	if strings.Contains(string(b), "slides-overview") || strings.Contains(string(b), "closeOverview") {
		t.Error("--bare has no overview")
	}
	// the size: the whole sync-view code (presenter and overview) is capped
	n := len(presenterAdded(page))
	t.Logf("presenter and overview: %d bytes of script and style in a page of %d", n, len(page))
}

// TestOverviewInChrome drives the overview in headless Chrome (see
// TestPresenterInChrome for what it needs): o, Escape and the button open it,
// the grid shows every slide whole, the arrows, Enter and a click choose a
// slide and the page follows, a choice closes it over the page and not over
// the presenter view, the elements are labelled and the closed overview is
// inert.
func TestOverviewInChrome(t *testing.T) {
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
	cmd := exec.Command(node, filepath.Join("testdata", "overview-check.js"), page)
	cmd.Env = append(os.Environ(), "PUPPETEER_CORE="+pup, "CHROME="+chrome)
	b, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, b)
	}
	var r struct {
		ClosedAtStart, OpenAfterO bool
		URL, Title, IframeTitle   string
		Grid                      [][]any
		CloseButton               string
		CurrentInOverview         int
		AfterRight                struct {
			Overview, Main int
			StillOpen      bool
		}
		AfterDown, AfterHome, AfterDownFromHome struct{ Overview, Main int }
		AfterEnter                              struct {
			Main        int
			Open, Inert bool
		}
		EscapeOpens, EscapeCloses bool
		AfterClick                struct {
			Main int
			Open bool
		}
		AfterButton                    bool
		PresenterOpen                  bool
		PresenterFrameUrlCloseOnSelect bool
		PresenterAfterPick             struct {
			Open            bool
			Presenter, Main int
		}
		PresenterClosed bool
		Errors          []string
	}
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatalf("%v\n%s", err, b)
	}
	if len(r.Errors) > 0 {
		t.Errorf("page errors: %v", r.Errors)
	}
	if !r.ClosedAtStart || !r.OpenAfterO || r.URL != "o.html?view=overview&closeOnSelect=1" || r.IframeTitle != "Overview" || r.CloseButton != "Close overview" || r.CurrentInOverview != 0 {
		t.Errorf("opening: %+v", r)
	}
	if len(r.Grid) != 3 {
		t.Fatalf("grid %v", r.Grid)
	}
	for i, g := range r.Grid {
		if g[2] != "block" || g[3] != "button" || g[4] != "Slide "+string(rune('1'+i)) || g[5] != float64(0) {
			t.Errorf("slide %d in the grid: %v", i+1, g)
		}
	}
	if r.Grid[0][0] != r.Grid[2][0] || r.Grid[0][0] == r.Grid[1][0] {
		t.Errorf("two columns: %v", r.Grid)
	}
	if a := r.AfterRight; a.Overview != 1 || a.Main != 1 || !a.StillOpen {
		t.Errorf("the arrow chose slide 2: %+v", a)
	}
	if a := r.AfterDown; a.Overview != 1 || a.Main != 1 {
		t.Errorf("down from the last row stays: %+v", a)
	}
	if a := r.AfterHome; a.Overview != 0 || a.Main != 0 {
		t.Errorf("Home: %+v", a)
	}
	if a := r.AfterDownFromHome; a.Overview != 2 || a.Main != 2 {
		t.Errorf("down a row: %+v", a)
	}
	if a := r.AfterEnter; a.Main != 0 || a.Open || !a.Inert {
		t.Errorf("Enter chooses and closes: %+v", a)
	}
	if !r.EscapeOpens || !r.EscapeCloses {
		t.Errorf("Escape toggles, as marp-cli's: %v %v", r.EscapeOpens, r.EscapeCloses)
	}
	if r.AfterClick.Main != 2 || r.AfterClick.Open || !r.AfterButton {
		t.Errorf("click %+v, button %v", r.AfterClick, r.AfterButton)
	}
	if !r.PresenterOpen || r.PresenterFrameUrlCloseOnSelect || !r.PresenterAfterPick.Open || r.PresenterAfterPick.Presenter != 1 || r.PresenterAfterPick.Main != 1 || !r.PresenterClosed {
		t.Errorf("over the presenter view: %+v open %v", r.PresenterAfterPick, r.PresenterOpen)
	}
}
