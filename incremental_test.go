package slides

import (
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-slides/slides/deck"
	"github.com/go-slides/slides/internal/benchdeck"
)

// snippets are what the random edits insert: text, line breaks, slide
// breaks, and every kind of state that crosses slides or depends on them —
// local, spot and global directives, pagination, heading dividers, link
// reference definitions, backgrounds, transitions, headings (deck-wide
// ids), scoped styles, code, math, emoji.
var snippets = []string{
	"x", " ", "\n", "\n\n", "\n---\n\n", "---\n",
	"<!-- _class: lead -->\n", "<!-- class: invert -->\n", "<!-- paginate: true -->\n",
	"<!-- paginate: hold -->\n", "<!-- paginate: skip -->\n", "<!-- _paginate: false -->\n",
	"<!-- headingDivider: 2 -->\n", "<!-- theme: gaia -->\n", "<!-- header: **H** -->\n",
	"<!-- _footer: f -->\n", "<!-- transition: fade -->\n", "<!-- _backgroundColor: red -->\n",
	"# Title\n", "## Sub\n", "![bg](a.png)\n", "![bg left](b.png)\n", "![w:100](c.png)\n",
	"see [the ref][r]\n", "\n[r]: http://example.com \"T\"\n", "[r]: http://other.example\n",
	"<style scoped>p{color:red}</style>\n", "<style>h1{color:blue}</style>\n",
	"```go\nfunc f() {}\n```\n", "* a\n* b\n", "1) one\n", "`code`", "$x^2$", ":smile:",
	"<!-- note -->\n", "<!-- fit -->", "| a | b |\n|---|---|\n| 1 | 2 |\n", "**", "_",
}

// edit applies one random edit to src.
func edit(r *rand.Rand, src []byte) []byte {
	pos := 0
	if len(src) > 0 {
		pos = r.IntN(len(src) + 1)
		if r.IntN(2) == 0 { // often at a line start
			for pos > 0 && src[pos-1] != '\n' {
				pos--
			}
		}
	}
	out := append([]byte{}, src[:pos]...)
	switch r.IntN(4) {
	case 0: // delete a range
		end := min(len(src), pos+r.IntN(40))
		return append(out, src[end:]...)
	default:
		out = append(out, snippets[r.IntN(len(snippets))]...)
		return append(out, src[pos:]...)
	}
}

// checkIncremental renders src after each of n random edits, with one
// render cache kept across them and without any, and requires the same
// slides byte for byte.
func checkIncremental(t *testing.T, src []byte, seed uint64, n int) int {
	t.Helper()
	r := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	cache := &deck.RenderCache{}
	cfg := Config{Parse: deck.Options{LineMap: true}, SourceLines: true}
	cached := cfg
	cached.RenderCache = cache
	hits := 0
	for step := range n {
		if step > 0 {
			src = edit(r, src)
		}
		full, errFull := Render(src, cfg, deck.FrameSVG)
		inc, errInc := Render(src, cached, deck.FrameSVG)
		if (errFull == nil) != (errInc == nil) {
			t.Fatalf("seed %d step %d: errors differ: %v / %v", seed, step, errFull, errInc)
		}
		if errFull != nil {
			continue
		}
		if len(full.Slides) != len(inc.Slides) {
			t.Fatalf("seed %d step %d: %d slides, incremental %d", seed, step, len(full.Slides), len(inc.Slides))
		}
		for i := range full.Slides {
			if full.Slides[i].HTML != inc.Slides[i].HTML {
				t.Fatalf("seed %d step %d: slide %d differs\nfull: %s\nincremental: %s\nsource:\n%s",
					seed, step, i+1, full.Slides[i].HTML, inc.Slides[i].HTML, src)
			}
		}
		hits += cache.Hits
	}
	return hits
}

// Each case edits one slide in a way that changes another slide's output
// without touching that slide's source: the cache must see it.
func TestIncrementalCrossSlideState(t *testing.T) {
	cases := map[string][2]string{
		"link reference defined in another slide": {
			"see [it][r]\n\n---\n\nend\n",
			"see [it][r]\n\n---\n\nend\n\n[r]: http://example.com\n"},
		"heading id made unique by an earlier slide": {
			"# One\n\n---\n\n# Same\n",
			"# Same\n\n---\n\n# Same\n"},
		// The edits below keep every line where it was: a moved line would
		// change the slide's own key (its line) and hide what is tested.
		"transition of the slide before": {
			"<!-- n -->\na\n\n---\n\nb\n",
			"<!-- _transition: fade -->\na\n\n---\n\nb\n"},
		"local directive inherited": {
			"<!-- n -->\na\n\n---\n\nb\n",
			"<!-- class: lead -->\na\n\n---\n\nb\n"},
		"spot directive not inherited": {
			"<!-- class: lead -->\na\n\n---\n\nb\n",
			"<!-- _class: lead -->\na\n\n---\n\nb\n"},
		"page numbers shifted by a skipped slide": {
			"<!-- paginate: true -->\n<!-- n -->\na\n\n---\n\nb\n\n---\n\nc\n",
			"<!-- paginate: true -->\n<!-- _paginate: skip -->\na\n\n---\n\nb\n\n---\n\nc\n"},
		"page numbers held": {
			"<!-- paginate: true -->\na\n\n---\n\n<!-- n -->\nb\n\n---\n\nc\n",
			"<!-- paginate: true -->\na\n\n---\n\n<!-- _paginate: hold -->\nb\n\n---\n\nc\n"},
		"global directive in the last slide": {
			"a\n\n---\n\nb\n",
			"a\n\n---\n\nb\n<!-- theme: gaia -->\n"},
		"heading divider": {
			"# a\n\n## b\n\ntext\n",
			"<!-- headingDivider: 2 -->\n# a\n\n## b\n\ntext\n"},
		"lines moved by an earlier slide": {
			"a\n\n---\n\nb\n",
			"a\n\n\n\n---\n\nb\n"},
		"header inherited": {
			"<!-- n -->\na\n\n---\n\nb\n",
			"<!-- header: H -->\na\n\n---\n\nb\n"},
		"lines traced through a template above": {
			"---\nn: [1]\n---\n\n{{ range .n }}a\n{{ end }}\n\n---\n\nb\n",
			"---\nn: [1, 2]\n---\n\n{{ range .n }}a\n{{ end }}\n\n---\n\nb\n"},
		"template data used further down": {
			"---\nx: 1\n---\n\na {{ .x }}\n\n---\n\nb {{ .x }}\n",
			"---\nx: 2\n---\n\na {{ .x }}\n\n---\n\nb {{ .x }}\n"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := Config{Parse: deck.Options{LineMap: true}, SourceLines: true}
			cached := cfg
			cached.RenderCache = &deck.RenderCache{}
			for _, src := range c {
				full, err := Render([]byte(src), cfg, deck.FrameSVG)
				if err != nil {
					t.Fatal(err)
				}
				inc, err := Render([]byte(src), cached, deck.FrameSVG)
				if err != nil {
					t.Fatal(err)
				}
				for i := range full.Slides {
					if full.Slides[i].HTML != inc.Slides[i].HTML {
						t.Fatalf("slide %d differs\nfull: %s\nincremental: %s", i+1, full.Slides[i].HTML, inc.Slides[i].HTML)
					}
				}
			}
		})
	}
}

func incrementalDecks(t testing.TB) map[string][]byte {
	decks := map[string][]byte{"bench": benchdeck.Large(20)}
	files, err := filepath.Glob(filepath.Join("testdata", "oracle", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		decks[filepath.Base(f)] = b
	}
	return decks
}

func TestIncrementalRenderMatchesFull(t *testing.T) {
	steps := 40
	if testing.Short() {
		steps = 8
	}
	for name, src := range incrementalDecks(t) {
		t.Run(name, func(t *testing.T) {
			hits := 0
			for seed := range uint64(4) {
				hits += checkIncremental(t, src, seed, steps)
			}
			// On the 20-slide deck most edits leave most slides alone: the
			// cache must be doing something, or the test proves nothing.
			if name == "bench" && hits < steps {
				t.Errorf("the cache hit %d slides over %d renders", hits, 4*steps)
			}
		})
	}
}

func FuzzIncrementalRenderMatchesFull(f *testing.F) {
	f.Add(uint64(1), uint8(0))
	f.Add(uint64(7), uint8(3))
	decks := incrementalDecks(f)
	names := make([]string, 0, len(decks))
	for n := range decks {
		names = append(names, n)
	}
	f.Fuzz(func(t *testing.T, seed uint64, which uint8) {
		name := names[int(which)%len(names)]
		checkIncremental(t, decks[name], seed, 12)
	})
}
