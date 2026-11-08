package export

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/go-webengine/engine"

	"github.com/go-slides/slides/internal/benchdeck"
)

// BenchmarkWrite times a whole export (layout, drawing, writing) of the
// 10-slide benchmark decks and of one with a transition on each slide and
// three fragments on each, and reports the file's size: the cost of carrying
// the animations (BENCHMARKS.md). "layout" is the path a deck without
// fragments takes (EachLayout); "steps" lays a slide out once for each of its
// fragment steps (EachSteps), which the editable decks and --pdf-fragments do.
func BenchmarkWrite(b *testing.B) {
	animated := animatedDeck(10)
	for _, d := range []struct {
		name string
		src  []byte
	}{{"default-10", benchdeck.Large(10)}, {"gaia-10", benchdeck.Gaia(10)}, {"math-code-10", benchdeck.MathCode(10)}, {"animated-10", animated}} {
		for _, format := range []string{"pdf", "pptx", "odp"} {
			for _, mode := range []string{"layout", "steps"} {
				b.Run(fmt.Sprintf("%s/%s/%s", d.name, format, mode), func(b *testing.B) {
					bd := benchBuild(b, d.src)
					var size int
					b.ReportAllocs()
					for b.Loop() {
						var buf bytes.Buffer
						var err error
						ctx := context.Background()
						opts := Options{Workers: 1, Scale: 1}
						switch format {
						case "pdf":
							w := NewPDFWith(bd, PDFOptions{OutlinePages: true, OutlineHeadings: true})
							if mode == "layout" {
								err = EachLayout(ctx, bd, opts, func(_ int, l *engine.PageLayout) error { return w.AddLayout(l) })
							} else {
								err = EachSteps(ctx, bd, opts, func(_ int, s []*engine.PageLayout) error { return w.AddSteps(s) })
							}
							if err == nil {
								err = w.Write(&buf)
							}
						case "pptx":
							w := NewPPTX(bd)
							if mode == "layout" {
								err = EachLayout(ctx, bd, opts, func(_ int, l *engine.PageLayout) error { return w.AddLayout(l) })
							} else {
								err = EachSteps(ctx, bd, opts, func(_ int, s []*engine.PageLayout) error { return w.AddSteps(s) })
							}
							if err == nil {
								err = w.Write(&buf)
							}
						case "odp":
							w := NewODP(bd)
							if mode == "layout" {
								err = EachLayout(ctx, bd, opts, func(_ int, l *engine.PageLayout) error { return w.AddLayout(l) })
							} else {
								err = EachSteps(ctx, bd, opts, func(_ int, s []*engine.PageLayout) error { return w.AddSteps(s) })
							}
							if err == nil {
								err = w.Write(&buf)
							}
						}
						if err != nil {
							b.Fatal(err)
						}
						size = buf.Len()
					}
					b.ReportMetric(float64(size)/1024, "KiB")
				})
			}
		}
	}
}

// animatedDeck is n slides, each with a transition and three fragments.
func animatedDeck(n int) []byte {
	var b bytes.Buffer
	b.WriteString("---\ntheme: default\ntransition: fade\n---\n\n")
	names := []string{"fade", "push", "wipe", "cube", "zoom"}
	for i := range n {
		if i > 0 {
			b.WriteString("\n---\n\n")
		}
		fmt.Fprintf(&b, "<!-- transition: %s -->\n\n# Slide %d\n\n* first point of slide %d\n* second point with a few more words\n* third point\n", names[i%len(names)], i+1, i+1)
	}
	return b.Bytes()
}
