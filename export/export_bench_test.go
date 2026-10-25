package export

import (
	"context"
	"fmt"
	"image"
	"io"
	"runtime/metrics"
	"testing"
	"time"

	"github.com/go-slides/slides"
	"github.com/go-slides/slides/deck"
	"github.com/go-slides/slides/doc"
	"github.com/go-slides/slides/internal/benchdeck"
)

// The rasterizing benchmarks use 10 slides of the deck slidesbench compares
// with marp-cli (every slide kind twice), to keep an iteration short; the
// time per slide is the figure to compare. See BENCHMARKS.md.

func benchBuild(b *testing.B, src []byte) *doc.Doc {
	b.Helper()
	bd, err := slides.Render(src, slides.Config{}, deck.FrameBox)
	if err != nil {
		b.Fatal(err)
	}
	return bd.Doc()
}

func BenchmarkRasterize(b *testing.B) {
	for _, d := range []struct {
		name string
		src  []byte
	}{{"default-10", benchdeck.Large(10)}, {"gaia-10", benchdeck.Gaia(10)}, {"math-code-10", benchdeck.MathCode(10)}} {
		for _, workers := range []int{1, 2, 4, 8} {
			b.Run(fmt.Sprintf("%s/workers-%d", d.name, workers), func(b *testing.B) {
				bd := benchBuild(b, d.src)
				peak := heapPeak()
				b.ReportAllocs()
				for b.Loop() {
					if err := Each(context.Background(), bd, Options{Workers: workers}, func(int, *image.RGBA) error { return nil }); err != nil {
						b.Fatal(err)
					}
				}
				b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/float64(len(bd.Slides))/1e6, "ms/slide")
				b.ReportMetric(peak()/(1<<20), "peak-heap-MiB")
			})
		}
	}
}

// heapPeak samples the bytes held by live and not-yet-collected heap
// objects (runtime/metrics, which does not stop the world) every
// millisecond until the returned function is called, and returns the
// largest value seen: what the workers hold at once, garbage included.
func heapPeak() func() float64 {
	const name = "/memory/classes/heap/objects:bytes"
	s := []metrics.Sample{{Name: name}}
	var peak uint64
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		t := time.NewTicker(time.Millisecond)
		defer t.Stop()
		for {
			metrics.Read(s)
			peak = max(peak, s[0].Value.Uint64())
			select {
			case <-done:
				return
			case <-t.C:
			}
		}
	}()
	return func() float64 {
		close(done)
		<-finished
		return float64(peak)
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
