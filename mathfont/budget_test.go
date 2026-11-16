package mathfont

import (
	"encoding/binary"
	"os"
	"testing"

	"github.com/go-opentype/fonts/gomono"
	"github.com/go-opentype/fonts/goregular"
	"github.com/go-tex/math"
)

func tablesOf(font []byte) map[string][]byte {
	out := map[string][]byte{}
	n := int(binary.BigEndian.Uint16(font[4:]))
	for i := 0; i < n; i++ {
		rec := font[12+i*16:]
		off, l := binary.BigEndian.Uint32(rec[8:]), binary.BigEndian.Uint32(rec[12:])
		out[string(rec[:4])] = font[off : off+l]
	}
	return out
}

// TestGlyfBudgets logs what glyfCost counts for STIX and for any TrueType-outline
// font named in GLYF_FONTS (colon separated), the calibration of MaxGlyphBytes
// and MaxOutlineBytes.
func TestGlyfBudgets(t *testing.T) {
	fonts := map[string][]byte{"STIX Two Math (embedded)": stix()}
	for _, p := range splitList(os.Getenv("GLYF_FONTS")) {
		if b, err := os.ReadFile(p); err == nil {
			fonts[p] = b
		}
	}
	for name, f := range fonts {
		tb := tablesOf(f)
		_, hasGlyf := tb["glyf"]
		worst, total, err := glyfCost(tb)
		t.Logf("%s: glyf=%v glyphs=%d worst=%d total=%d err=%v", name, hasGlyf, binary.BigEndian.Uint16(tb["maxp"][4:]), worst, total, err)
		if len(f) <= MaxFontBytes && (err != nil || worst > MaxGlyphBytes || total > MaxOutlineBytes) {
			t.Errorf("%s would be refused", name)
		}
	}
}

func splitList(s string) (out []string) {
	for s != "" {
		i := 0
		for i < len(s) && s[i] != ':' {
			i++
		}
		if i > 0 {
			out = append(out, s[:i])
		}
		if i == len(s) {
			break
		}
		s = s[i+1:]
	}
	return
}

// cffIndex encodes items as a CFF INDEX with 2-byte offsets.
func cffIndex(items [][]byte) []byte {
	b := p16(nil, len(items))
	if len(items) == 0 {
		return b
	}
	b = append(b, 2)
	off := 1
	b = p16(b, off)
	for _, it := range items {
		off += len(it)
		b = p16(b, off)
	}
	for _, it := range items {
		b = append(b, it...)
	}
	return b
}

// cffFanout: global subroutine 0 draws one line; subroutine i calls subroutine
// i-1 k times; the glyph calls subroutine 9 k times. The interpreter caps only
// the call depth (10, cff.go:78, :657), so the glyph draws k^10 lines.
func cffFanout(k int) []byte {
	call := func(sub int) []byte { return []byte{byte(sub - 107 + 139), 29} } // callgsubr; bias 107
	subrs := [][]byte{{149, 149, 5}}                                          // 10 10 rlineto
	for i := 1; i <= 9; i++ {
		var s []byte
		for c := 0; c < k; c++ {
			s = append(s, call(i-1)...)
		}
		subrs = append(subrs, s)
	}
	var glyph []byte
	for c := 0; c < k; c++ {
		glyph = append(glyph, call(9)...)
	}
	glyph = append(glyph, 14) // endchar
	header, name, str := []byte{1, 0, 4, 2}, cffIndex([][]byte{[]byte("B")}), cffIndex(nil)
	gsubrs, cs := cffIndex(subrs), cffIndex([][]byte{{14}, glyph})
	dict := func(csOff int) []byte {
		return cffIndex([][]byte{append([]byte{29, byte(csOff >> 24), byte(csOff >> 16), byte(csOff >> 8), byte(csOff)}, 17)})
	}
	csOff := len(header) + len(name) + len(dict(0)) + len(str) + len(gsubrs)
	cff := append(append(append(append(append(header, name...), dict(csOff)...), str...), gsubrs...), cs...)

	t := baseTables()
	delete(t, "loca")
	delete(t, "glyf")
	t["CFF "] = cff
	t["maxp"] = []byte{0, 0, 0x50, 0, 0, 2}
	t["hmtx"] = make([]byte, 6)
	cmap := make([]byte, 262)
	binary.BigEndian.PutUint16(cmap[2:], 262)
	cmap['1'+6] = 1
	t["cmap"] = append([]byte{0, 0, 0, 1, 0, 0, 0, 3, 0, 0, 0, 12}, cmap...)
	return rebuild([]byte("OTTO"), t)
}

// TestUpstreamCFFFanout is the finding for CFF outlines, which the guard does not
// cover: measured at a scale that is safe to run (k=4, 4^10 lines). It logs and
// never fails.
func TestUpstreamCFFFanout(t *testing.T) {
	font := cffFanout(4)
	var svgLen int
	var err error
	n := allocated(func() {
		defer func() { _ = recover() }()
		r, e := math.New(font)
		if err = e; e == nil {
			s, _ := r.RenderSVG("1", 40)
			svgLen = len(s)
		}
	})
	t.Logf("cff fan-out k=4: %d-byte font (err %v), %d bytes allocated, svg %d bytes", len(font), err, n, svgLen)
}

// TestManyGlyphsAreRefusedByTheTotal: each glyph is under the per-glyph cap, so
// only the total refuses the font.
func TestManyGlyphsAreRefusedByTheTotal(t *testing.T) {
	tb := tablesOf(glyfManyGlyphs(40))
	worst, total, err := glyfCost(tb)
	if err != nil || worst > MaxGlyphBytes || total <= MaxOutlineBytes {
		t.Fatalf("worst %d (cap %d), total %d (cap %d), err %v", worst, MaxGlyphBytes, total, MaxOutlineBytes, err)
	}
	t.Logf("40 glyphs: worst %d, total %d", worst, total)
}

// TestTrueTypeFontsPassTheGuard runs in CI on real TrueType outlines (the Go
// fonts), which STIX's CFF outlines cannot check: the guard accepts them, and
// their glyph costs stay far under the caps.
func TestTrueTypeFontsPassTheGuard(t *testing.T) {
	for name, font := range map[string][]byte{"Go Regular": goregular.TTF, "Go Mono Bold": gomono.BoldTTF} {
		tb := tablesOf(font)
		if tb["glyf"] == nil {
			t.Fatalf("%s: no glyf table", name)
		}
		worst, total, err := glyfCost(tb)
		if err != nil || worst == 0 || worst > MaxGlyphBytes/4 || total > MaxOutlineBytes/4 {
			t.Errorf("%s: worst %d (cap %d), total %d (cap %d), err %v", name, worst, MaxGlyphBytes, total, MaxOutlineBytes, err)
		}
		if _, err := bound(font); err != nil {
			t.Errorf("%s refused: %v", name, err)
		}
		t.Logf("%s: worst glyph %d bytes, total %d bytes", name, worst, total)
	}
}
