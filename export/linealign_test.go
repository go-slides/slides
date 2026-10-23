package export

import (
	"context"
	"image"
	"math"
	"sort"
	"strings"
	"testing"

	"github.com/go-slides/slides"
	"github.com/go-slides/slides/deck"
	"github.com/go-webengine/engine"
	"github.com/go-webengine/engine/css"
	"github.com/go-webengine/engine/paint"
)

// runRecorder is a canvas that keeps the glyph runs the engine paints, with
// their baselines: what the line-number test measures.
type runRecorder struct{ runs []paint.GlyphRun }

func (*runRecorder) Save()                             {}
func (*runRecorder) Restore()                          {}
func (*runRecorder) Clip(*paint.Path, bool)            {}
func (*runRecorder) Fill(*paint.Path, css.Color, bool) {}
func (*runRecorder) Shade(paint.Shading)               {}
func (*runRecorder) Image(image.Image, paint.Rect)     {}
func (r *runRecorder) Glyphs(run *paint.GlyphRun)      { r.runs = append(r.runs, *run) }
func runText(run paint.GlyphRun) (s string) {
	for _, g := range run.Glyphs {
		s += g.Text
	}
	return s
}

func digits(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// Every line number sits on its code line's baseline, within a pixel: with
// callouts, whose discs are on some lines only, and without; in each built-in
// theme. And the discs do not make their lines taller: the code's baselines
// are the same with them as without.
//
// The style in the deck took the opacity off the separate gutter column that
// go-slides drew up to #46, so that the engine painted it as glyphs: that
// version failed here by up to 13 px.
func TestLineNumbersOnTheCodeBaselines(t *testing.T) {
	const code = "```go {lineNumbers}\nfunc main() {\n\tfmt.Println(\"hello\") // <1>\n\tos.Exit(0) // <2>\n}\n```\n\n1. Prints.\n2. Leaves.\n"
	plain := strings.NewReplacer(" // <1>", "", " // <2>", "").Replace(code)
	for _, theme := range []string{"default", "gaia", "uncover"} {
		lines := map[string][]float64{}
		for _, name := range []string{"callouts", "plain"} {
			body := map[string]string{"callouts": code, "plain": plain}[name]
			src := "---\ntheme: " + theme + "\n---\n\n<style>section pre.line-numbers .ln-gutter{opacity:1}</style>\n\n# Code\n\n" + body
			b, err := slides.Render([]byte(src), slides.Config{}, deck.FrameBox)
			if err != nil {
				t.Fatal(err)
			}
			var rec runRecorder
			if err := EachLayout(context.Background(), b, Options{}, func(_ int, l *engine.PageLayout) error {
				l.PaintVector(&rec)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			lines[name] = checkBaselines(t, theme+"/"+name, rec.runs)
		}
		// Compared from the first line: the notes' list is not as tall
		// with discs as with its plain numbers, and default and uncover
		// centre the slide's content, which moves the whole block.
		p, c := lines["plain"], lines["callouts"]
		for i := 1; i < len(p); i++ {
			if d := math.Abs((p[i] - p[0]) - (c[i] - c[0])); d > 0.5 {
				t.Errorf("%s: a disc moved code line %d by %.1f px", theme, i+1, d)
			}
		}
	}
}

// checkBaselines returns the code's four baselines.
func checkBaselines(t *testing.T, name string, runs []paint.GlyphRun) []float64 {
	t.Helper()
	// The code: selectable runs at or right of the leftmost "func".
	left := math.Inf(1)
	for _, r := range runs {
		if strings.HasPrefix(runText(r), "func") {
			left = math.Min(left, r.X)
		}
	}
	if math.IsInf(left, 1) {
		t.Fatalf("%s: no code painted", name)
	}
	var code, numbers []float64
	seen := map[int]bool{}
	for _, r := range runs {
		switch {
		case r.Unselectable && digits(runText(r)) && r.X < left:
			numbers = append(numbers, r.Y)
		case !r.Unselectable && r.X >= left-0.5 && !digits(runText(r)):
			if k := int(math.Round(r.Y * 4)); !seen[k] && r.Y > 0 {
				seen[k] = true
				code = append(code, r.Y)
			}
		}
	}
	sort.Float64s(numbers)
	sort.Float64s(code)
	// The notes below the block are code-free text right of "func" too:
	// keep the code's four lines, the first four baselines from the top of
	// the block.
	first := 0
	for first < len(code) && code[first] < numbersTop(numbers)-2 {
		first++
	}
	code = code[first:]
	// The notes' discs are digits left of the code too, below the block.
	if len(numbers) > 4 {
		numbers = numbers[:4]
	}
	if len(numbers) != 4 || len(code) < 4 {
		t.Fatalf("%s: %d numbers, %d code lines: %v / %v", name, len(numbers), len(code), numbers, code)
	}
	for i, y := range numbers {
		if d := math.Abs(y - code[i]); d > 1 {
			t.Errorf("%s: line %d's number at y=%.1f, its code at y=%.1f (%.1f px apart)", name, i+1, y, code[i], d)
		}
	}
	return code[:4]
}

func numbersTop(n []float64) float64 {
	if len(n) == 0 {
		return 0
	}
	return n[0]
}
