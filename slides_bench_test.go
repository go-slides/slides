package slides

import (
	"testing"

	"github.com/go-slides/slides/deck"
	"github.com/go-slides/slides/internal/benchdeck"
	"github.com/go-slides/slides/theme"
)

// The benchmarks run on the 100-slide deck slidesbench compares with
// marp-cli; see BENCHMARKS.md.

func BenchmarkParse(b *testing.B) {
	src := benchdeck.Large(100)
	b.SetBytes(int64(len(src)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := deck.Parse(src, deck.Options{}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRender(b *testing.B) {
	src := benchdeck.Large(100)
	for _, f := range []struct {
		name  string
		frame deck.Frame
	}{{"svg", deck.FrameSVG}, {"box", deck.FrameBox}} {
		b.Run(f.name, func(b *testing.B) {
			b.SetBytes(int64(len(src)))
			b.ReportAllocs()
			for b.Loop() {
				if _, err := Render(src, Config{}, f.frame); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkHTML(b *testing.B) {
	for _, d := range []struct {
		name string
		src  []byte
	}{{"default-100", benchdeck.Large(100)}, {"gaia-20", benchdeck.Gaia(20)}, {"math-code-20", benchdeck.MathCode(20)}} {
		b.Run(d.name, func(b *testing.B) {
			b.SetBytes(int64(len(d.src)))
			b.ReportAllocs()
			for b.Loop() {
				if _, _, err := HTML(d.src, Config{}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkPack(b *testing.B) {
	set := theme.NewSet()
	for _, name := range []string{"default", "gaia", "uncover"} {
		t := set.Resolve(name)
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				set.Pack(t, theme.PackOptions{Printable: true, ContainerQuery: true, Slide: []string{"svg", "foreignObject", "section"}, InlineSVG: true})
			}
		})
	}
}
