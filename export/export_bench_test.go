package export

import (
	"context"
	"fmt"
	"image"
	"io"
	"testing"

	"github.com/go-slides/slides"
	"github.com/go-slides/slides/deck"
	"github.com/go-slides/slides/internal/benchdeck"
)

// The rasterizing benchmarks use 10 slides of the deck slidesbench compares
// with marp-cli (every slide kind twice), to keep an iteration short; the
// time per slide is the figure to compare. See BENCHMARKS.md.

func benchBuild(b *testing.B, src []byte) *slides.Build {
	b.Helper()
	bd, err := slides.Render(src, slides.Config{}, deck.FrameBox)
	if err != nil {
		b.Fatal(err)
	}
	return bd
}

func BenchmarkRasterize(b *testing.B) {
	for _, d := range []struct {
		name string
		src  []byte
	}{{"default-10", benchdeck.Large(10)}, {"gaia-10", benchdeck.Gaia(10)}, {"math-code-10", benchdeck.MathCode(10)}} {
		for _, workers := range []int{1, 2, 4, 8} {
			b.Run(fmt.Sprintf("%s/workers-%d", d.name, workers), func(b *testing.B) {
				bd := benchBuild(b, d.src)
				b.ReportAllocs()
				for b.Loop() {
					if err := Each(context.Background(), bd, Options{Workers: workers}, func(int, *image.RGBA) error { return nil }); err != nil {
						b.Fatal(err)
					}
				}
				b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/float64(len(bd.Slides))/1e6, "ms/slide")
			})
		}
	}
}

func BenchmarkEncode(b *testing.B) {
	bd := benchBuild(b, benchdeck.Large(5))
	imgs, err := Rasterize(context.Background(), bd, Options{})
	if err != nil {
		b.Fatal(err)
	}
	b.Run("png", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			for _, img := range imgs {
				if err := PNG(io.Discard, img); err != nil {
					b.Fatal(err)
				}
			}
		}
	})
	b.Run("pdf", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if err := PDF(io.Discard, bd, imgs); err != nil {
				b.Fatal(err)
			}
		}
	})
}
