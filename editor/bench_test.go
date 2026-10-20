package editor

import (
	"testing"

	"github.com/go-slides/slides/internal/benchdeck"
)

// BenchmarkRender is the editor preview of the 100-slide benchmark deck,
// the input the VS Code extension's latency is measured on.
func BenchmarkRender(b *testing.B) {
	r := req(string(benchdeck.Large(100)), nil)
	b.SetBytes(int64(len(r.Source)))
	for b.Loop() {
		if res := Render(r); res.Slides != 100 {
			b.Fatalf("%d slides, %+v", res.Slides, res.Diagnostics)
		}
	}
}

func BenchmarkComplete(b *testing.B) {
	r := req(string(benchdeck.Large(100)), nil)
	for b.Loop() {
		Complete(r, 1, 0)
	}
}
