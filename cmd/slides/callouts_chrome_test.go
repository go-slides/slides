package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

// In Chrome, a disc hovered or focused shows its note and a note its disc,
// by the style sheet alone; the disc is described by its note; the tint's
// transition is off under prefers-reduced-motion.
func TestCalloutsHoverInChrome(t *testing.T) {
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
	md := write(t, dir, "d.md", calloutDeck)
	page := filepath.Join(dir, "o.html")
	if _, stderr, err := run(t, "build", md, "-o", page); err != nil {
		t.Fatal(err, stderr)
	}
	cmd := exec.Command(node, filepath.Join("testdata", "callouts-check.js"), page)
	cmd.Env = append(os.Environ(), "PUPPETEER_CORE="+pup, "CHROME="+chrome)
	b, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, b)
	}
	type state struct{ Notes, Discs []string }
	var r struct {
		Rest, HoverDisc2, HoverNote1, Away, FocusDisc, FocusNote state
		Focused, Focused2                                        string
		Described                                                []any
		Transition, ReducedTransition                            string
		Scripts                                                  int
		Errors                                                   []string
	}
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatalf("%v\n%s", err, b)
	}
	if len(r.Errors) > 0 {
		t.Errorf("page errors: %v", r.Errors)
	}
	none := state{Notes: []string{}, Discs: []string{}}
	for name, c := range map[string]struct{ got, want state }{
		"at rest":                {r.Rest, none},
		"hovering disc 2":        {r.HoverDisc2, state{[]string{"2"}, []string{"2"}}},
		"hovering note 1":        {r.HoverNote1, state{[]string{"1"}, []string{"1"}}},
		"pointer away":           {r.Away, none},
		"disc 1 focused":         {r.FocusDisc, state{[]string{"1"}, []string{"1"}}},
		"the next note, focused": {r.FocusNote, state{[]string{"1"}, []string{"1"}}},
	} {
		if !reflect.DeepEqual(c.got, c.want) {
			t.Errorf("%s: notes %v discs %v, want %v %v", name, c.got.Notes, c.got.Discs, c.want.Notes, c.want.Discs)
		}
	}
	if r.Focused != "1" || r.Focused2 != "LI:1" {
		t.Errorf("Tab reached disc %q, then %q: want disc 1, then note 1 (after disc 2)", r.Focused, r.Focused2)
	}
	if len(r.Described) != 2 || r.Described[0] != nil || r.Described[1] != "Reads the input." {
		t.Errorf("disc 2: aria-hidden and description %v", r.Described)
	}
	if r.Transition != "0.15s" || r.ReducedTransition != "0s" {
		t.Errorf("transition %q, %q under reduced motion", r.Transition, r.ReducedTransition)
	}
	if r.Scripts != 0 {
		t.Errorf("%d scripts handle callouts: the style sheet does", r.Scripts)
	}
}
