package viewer

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBespokeOptions(t *testing.T) {
	slides := []string{"<svg data-marpit-svg></svg>"}
	page := func(b Bespoke, css string) string {
		return Document(Options{Slides: slides, Bespoke: b, TransitionCSS: css})
	}
	// marp-cli's defaults: the controller, no progress bar, transitions
	d := page(DefaultBespoke(), "@keyframes marp-incoming-transition-__builtin__fade{}")
	for _, want := range []string{`{"osc":true,"progress":false}`, "startViewTransition", "::view-transition-group(*)", "@keyframes marp-incoming-transition-__builtin__fade{}"} {
		if !strings.Contains(d, want) {
			t.Errorf("default page lacks %q", want)
		}
	}
	// no transition option: no script for it and no styles
	o := page(Bespoke{OSC: true, Progress: true}, "")
	if !strings.Contains(o, `{"osc":true,"progress":true}`) || strings.Contains(o, "startViewTransition") || strings.Contains(o, "view-transition") {
		t.Errorf("osc and progress without transitions:\n%.400s", o)
	}
	// the bare template has none of it
	bare := Document(Options{Slides: slides, Bare: true, Bespoke: DefaultBespoke()})
	if strings.Contains(bare, "startViewTransition") || strings.Contains(bare, "slides-osc") || strings.Contains(bare, "<script>") {
		t.Error("the bare template has no script")
	}
	// the zero value is a page with neither
	z := Document(Options{Slides: slides})
	if !strings.Contains(z, `{"osc":false,"progress":false}`) {
		t.Errorf("zero Bespoke: %.200s", z)
	}
}

// TestTransitionLogic runs the pure parts of the transition script under node
// (when it is there) with keyframes it would find in a page: marp-cli's
// contract for which animation plays on which pseudo-element, in which
// direction and how long.
func TestTransitionLogic(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "t.js")
	if err := os.WriteFile(script, []byte(transitionJS), 0o644); err != nil {
		t.Fatal(err)
	}
	test := filepath.Join(dir, "test.js")
	if err := os.WriteFile(test, []byte(`
const lib = require(process.argv[2]);
const out = {};
const names = (list) => new Map(list.map((n) => (Array.isArray(n) ? n : [n, ''])));
// a theme that defines a shared animation for "spin" and, for "fly", separate
// ones for the way forward, none for the way back, and a default duration
const spin = lib.find(names(['marp-transition-spin', 'marp-transition-backward-spin']), 'spin');
const fly = lib.find(names([['marp-outgoing-transition-fly', '2s'], 'marp-incoming-transition-fly']), 'fly');
const builtin = lib.find(names(['marp-incoming-transition-__builtin__cover', 'marp-incoming-transition-backward-__builtin__cover']), '__builtin__cover');
out.none = [lib.none(lib.find(names([]), 'zzz')), lib.none(spin), lib.none(builtin)];
out.spinFwd = lib.rules(spin, false, undefined);
out.spinBack = lib.rules(spin, true, '300ms');
out.flyFwd = lib.rules(fly, false, lib.duration(fly, false));
out.flyBack = lib.rules(fly, true, lib.duration(fly, true));
out.cover = lib.rules(builtin, false, '1s');
out.parse = [lib.parse('{"name":"fade","duration":"1s","builtinFallback":true}'), lib.parse('{"name":1}') || null, lib.parse('nope') || null, lib.parse('{"name":"x","duration":3}') || null];
console.log(JSON.stringify(out));
`), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := exec.Command(node, test, script).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, b)
	}
	var got struct {
		None                                      []bool
		SpinFwd, SpinBack, FlyFwd, FlyBack, Cover []string
		Parse                                     []map[string]any
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("%v\n%s", err, b)
	}
	eq := func(name string, got, want []string) {
		t.Helper()
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("%s:\n got %q\nwant %q", name, got, want)
		}
	}
	if len(got.None) != 3 || !got.None[0] || got.None[1] || got.None[2] {
		t.Errorf("none = %v", got.None)
	}
	// a shared animation: the same keyframes on both pseudo-elements, the
	// arriving one run in reverse; backwards it has its own -backward one
	eq("spin forward", got.SpinFwd, []string{
		":root{--marp-transition-direction:1;}",
		"::view-transition-old(root){--marp-bespoke-transition-animation-name:marp-transition-spin;}",
		"::view-transition-new(root){--marp-bespoke-transition-animation-name:marp-transition-spin;--marp-bespoke-transition-animation-direction:reverse;}",
	})
	eq("spin back", got.SpinBack, []string{
		":root{--marp-transition-direction:-1;}",
		"::view-transition-group(*){--marp-bespoke-transition-animation-duration:300ms;}",
		"::view-transition-old(root){--marp-bespoke-transition-animation-name:marp-transition-backward-spin;}",
		"::view-transition-new(root){--marp-bespoke-transition-animation-name:marp-transition-backward-spin;--marp-bespoke-transition-animation-direction:reverse;}",
	})
	// separate animations, the duration of the first that has one; going back
	// with none of its own they are those of the way forward
	eq("fly forward", got.FlyFwd, []string{
		":root{--marp-transition-direction:1;}",
		"::view-transition-group(*){--marp-bespoke-transition-animation-duration:2s;}",
		"::view-transition-old(root){--marp-bespoke-transition-animation-name:marp-outgoing-transition-fly;}",
		"::view-transition-new(root){--marp-bespoke-transition-animation-name:marp-incoming-transition-fly;}",
	})
	if len(got.FlyBack) != 4 || !strings.Contains(got.FlyBack[0], "direction:-1") || got.FlyBack[2] != got.FlyFwd[2] || got.FlyBack[3] != got.FlyFwd[3] {
		t.Errorf("fly back: %q", got.FlyBack)
	}
	// the built-in cover: only an incoming animation, and nothing for the leaving slide
	eq("cover", got.Cover, []string{
		":root{--marp-transition-direction:1;}",
		"::view-transition-group(*){--marp-bespoke-transition-animation-duration:1s;}",
		"::view-transition-old(root){--marp-bespoke-transition-animation-name:__bespoke_marp_transition_no_animation__;}",
		"::view-transition-new(root){--marp-bespoke-transition-animation-name:marp-incoming-transition-__builtin__cover;}",
	})
	if len(got.Parse) != 4 || got.Parse[0]["name"] != "fade" || got.Parse[0]["builtinFallback"] != true || got.Parse[1] != nil || got.Parse[2] != nil || got.Parse[3] != nil {
		t.Errorf("parse = %v", got.Parse)
	}
}
