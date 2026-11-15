package slides

import (
	"bytes"
	"testing"

	"github.com/go-tex/math"
)

// TestNoMathFontWasmSize: the tag slides_nomathfont takes STIX Two Math (838,508
// bytes, 643,844 gzipped, measured on 2026-11-15) out of the module, with no
// change to go-tex/math: nothing refers to math.DefaultFont, so the linker drops
// the font. The test looks for the font's own bytes in the module, and measures
// what that saves.
func TestNoMathFontWasmSize(t *testing.T) {
	font := math.DefaultFont()
	// three pieces of the font, from its start, middle and end
	pieces := [][]byte{font[1000:1064], font[len(font)/2 : len(font)/2+64], font[len(font)-1064 : len(font)-1000]}

	with := wasmBuild(t, "slides_novideo")
	for i, p := range pieces {
		if !bytes.Contains(with, p) {
			t.Fatalf("piece %d of the font is not in the default module: the check cannot see the font", i)
		}
	}
	without := wasmBuild(t, "slides_novideo", "slides_nomathfont")
	for i, p := range pieces {
		if bytes.Contains(without, p) {
			t.Errorf("piece %d of the font is in the slides_nomathfont module", i)
		}
	}
	gw, gwo := gzSize(t, with), gzSize(t, without)
	t.Logf("gzip: %d bytes with the font, %d without: %d saved (raw %d -> %d)", gw, gwo, gw-gwo, len(with), len(without))
	if saved := gw - gwo; saved < 600_000 {
		t.Errorf("slides_nomathfont saves %d bytes gzipped, want about 643,844", saved)
	}
}
