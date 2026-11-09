package export

import (
	"bytes"
	"context"
	"testing"

	"github.com/go-webengine/engine"
)

// The cost of carrying an animated SVG, against the static export of the same
// deck (the same drawing, drawn at its initial state, nothing animated).
func benchPPTX(b *testing.B, svg string, animated bool) {
	d, opts := animFixture(b, map[string]string{"x.svg": svg}, "# One\n\n![w:600](x.svg)\n", true)
	var size int
	b.ReportAllocs()
	for b.Loop() {
		d2, anims, err := PrepareSVG(context.Background(), d, opts)
		if err != nil {
			b.Fatal(err)
		}
		w := NewPPTX(d2)
		if animated {
			w.SVG = anims
		}
		if err := EachSteps(context.Background(), d2, opts, func(_ int, s []*engine.PageLayout) error { return w.AddSteps(s) }); err != nil {
			b.Fatal(err)
		}
		var buf bytes.Buffer
		if err := w.Write(&buf); err != nil {
			b.Fatal(err)
		}
		size = buf.Len()
	}
	b.ReportMetric(float64(size), "bytes/deck")
}

func BenchmarkPPTXNativeStatic(b *testing.B)   { benchPPTX(b, spinSVG, false) }
func BenchmarkPPTXNativeAnimated(b *testing.B) { benchPPTX(b, spinSVG, true) }
func BenchmarkPPTXFramesStatic(b *testing.B)   { benchPPTX(b, morphSVG, false) }
func BenchmarkPPTXFramesAnimated(b *testing.B) { benchPPTX(b, morphSVG, true) }
