package mathfont

import (
	"encoding/binary"
	"os"
	"strings"
	"testing"

	"github.com/go-opentype/fonts/gomono"
	"github.com/go-opentype/fonts/goregular"
	"github.com/go-opentype/opentype"
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

// TestGlyfBudgets logs what glyfCost counts for any TrueType-outline font named
// in GLYF_FONTS (colon separated), the calibration of MaxOutlineBytes, and what
// the CFF glyphs of STIX (and of any CFF font named there) keep as paths, the
// calibration of MaxPathBytes. A font that would be refused fails the test.
func TestGlyfBudgets(t *testing.T) {
	fonts := map[string][]byte{"STIX Two Math (embedded)": stix()}
	for _, p := range splitList(os.Getenv("GLYF_FONTS")) {
		if b, err := os.ReadFile(p); err == nil {
			fonts[p] = b
		}
	}
	for name, f := range fonts {
		if len(f) > MaxFontBytes {
			continue
		}
		if tb := tablesOf(f); tb["CFF "] == nil {
			total, err := glyfCost(tb)
			t.Logf("%s: glyphs=%d glyf total=%d err=%v", name, binary.BigEndian.Uint16(tb["maxp"][4:]), total, err)
		} else {
			// The paths at each size: the margin below MaxPathBytes is the smallest.
			face, err := opentype.Parse(f)
			if err != nil {
				t.Fatal(err)
			}
			for _, px := range []int{10, 40, pathPx, 200, 1000} {
				var total, worst int
				fc := face.NewFace(px)
				for g := 0; g < face.NumGlyphs(); g++ {
					d, _ := fc.GlyphSVGPath(opentype.GlyphIndex(g))
					total, worst = total+len(d), max(worst, len(d))
				}
				t.Logf("%s: glyphs=%d at %d px: paths total=%d worst=%d (limit %d)", name, face.NumGlyphs(), px, total, worst, MaxPathBytes)
			}
		}
		if _, err := bound(f); err != nil {
			t.Errorf("%s would be refused: %v", name, err)
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

// callg is the charstring "callgsubr sub" (bias 107 for fewer than 1240).
func callg(sub int) []byte { return []byte{byte(sub - 107 + 139), 29} }

// repeat is s, n times.
func repeat(s []byte, n int) (out []byte) {
	for ; n > 0; n-- {
		out = append(out, s...)
	}
	return out
}

// cffFont is an OpenType font with CFF outlines: glyph 0 is empty, glyph i+1 is
// the charstring glyphs[i], and "1" maps to glyph 1. A charstring ends with
// endchar.
func cffFont(gsubrs, glyphs [][]byte) []byte {
	strs := [][]byte{{14}}
	for _, g := range glyphs {
		strs = append(strs, append(append([]byte(nil), g...), 14))
	}
	header, name, str := []byte{1, 0, 4, 2}, cffIndex([][]byte{[]byte("B")}), cffIndex(nil)
	gs, cs := cffIndex(gsubrs), cffIndex(strs)
	dict := func(csOff int) []byte {
		return cffIndex([][]byte{append([]byte{29, byte(csOff >> 24), byte(csOff >> 16), byte(csOff >> 8), byte(csOff)}, 17)})
	}
	csOff := len(header) + len(name) + len(dict(0)) + len(str) + len(gs)
	cff := append(append(append(append(append(header, name...), dict(csOff)...), str...), gs...), cs...)

	t := baseTables()
	delete(t, "loca")
	delete(t, "glyf")
	t["CFF "] = cff
	t["maxp"] = p16([]byte{0, 0, 0x50, 0}, len(strs))
	t["hmtx"] = make([]byte, 4+2*(len(strs)-1))
	cmap := make([]byte, 262)
	binary.BigEndian.PutUint16(cmap[2:], 262)
	cmap['1'+6] = 1
	t["cmap"] = append([]byte{0, 0, 0, 1, 0, 0, 0, 3, 0, 0, 0, 12}, cmap...)
	return rebuild([]byte("OTTO"), t)
}

// cffFanout (go-opentype/opentype#75): global subroutine 0 draws one line;
// subroutine i calls subroutine i-1 k times; the glyph calls subroutine 9 k
// times. Only the call depth was capped (10), so the glyph drew k^10 lines.
func cffFanout(k int) []byte {
	subrs := [][]byte{{149, 149, 5}} // 10 10 rlineto
	for i := 1; i <= 9; i++ {
		subrs = append(subrs, repeat(callg(i-1), k))
	}
	return cffFont(subrs, [][]byte{repeat(callg(9), k)})
}

// cffManyGlyphs: n glyphs that each draw 40,000 lines, a quarter of what a glyph
// may, by calling the same subroutines: every one is within its own budget, and
// together they keep n * 300 KB of path text.
func cffManyGlyphs(n int) []byte {
	subrs := [][]byte{{149, 149, 5}, repeat(callg(0), 200), repeat(callg(1), 200)}
	glyphs := make([][]byte, n)
	for i := range glyphs {
		glyphs[i] = callg(2)
	}
	return cffFont(subrs, glyphs)
}

// TestManyGlyphsAreRefusedByTheTotal: each glyph is under go-opentype's budget,
// so only the total refuses the font.
func TestManyGlyphsAreRefusedByTheTotal(t *testing.T) {
	total, err := glyfCost(tablesOf(glyfManyGlyphs(40)))
	if err != nil || total <= MaxOutlineBytes {
		t.Fatalf("total %d (cap %d), err %v", total, MaxOutlineBytes, err)
	}
	t.Logf("40 glyphs: total %d", total)
	_, err = bound(cffManyGlyphs(400))
	if err == nil || !strings.Contains(err.Error(), "operators allowed") {
		t.Fatalf("400 CFF glyphs of 40,000 lines: %v", err)
	}
	t.Log(err)
}

// TestCFFGuardReadsTheFont: a CFF table go-opentype cannot read, or a glyph over
// its budget, refuses the font; a font within both is passed on whole.
func TestCFFGuardReadsTheFont(t *testing.T) {
	broken := tablesOf(cffFanout(1))
	broken["CFF "] = broken["CFF "][:20]
	if _, err := bound(rebuild([]byte("OTTO"), broken)); err == nil || !strings.Contains(err.Error(), "the font declares") {
		t.Errorf("a truncated CFF table: %v", err)
	}
	if _, err := bound(cffFanout(8)); err == nil || !strings.Contains(err.Error(), "outline budget") {
		t.Errorf("a glyph of 8^10 lines: %v", err)
	}
	if _, err := bound(cffFanout(2)); err != nil {
		t.Errorf("a glyph of 2^10 lines: %v", err)
	}
}

// TestTrueTypeFontsPassTheGuard runs in CI on real TrueType outlines (the Go
// fonts), which STIX's CFF outlines cannot check: the guard accepts them, and
// their costs stay far under the cap.
func TestTrueTypeFontsPassTheGuard(t *testing.T) {
	for name, font := range map[string][]byte{"Go Regular": goregular.TTF, "Go Mono Bold": gomono.BoldTTF} {
		tb := tablesOf(font)
		if tb["glyf"] == nil {
			t.Fatalf("%s: no glyf table", name)
		}
		total, err := glyfCost(tb)
		if err != nil || total == 0 || total > MaxOutlineBytes/4 {
			t.Errorf("%s: total %d (cap %d), err %v", name, total, MaxOutlineBytes, err)
		}
		if _, err := bound(font); err != nil {
			t.Errorf("%s refused: %v", name, err)
		}
		t.Logf("%s: total %d bytes", name, total)
	}
}

// TestBoundRefusesBrokenFonts: a font whose table directory runs past its end,
// or whose composite glyphs refer to themselves, is refused before it is parsed.
func TestBoundRefusesBrokenFonts(t *testing.T) {
	for name, font := range map[string][]byte{
		"a short header":       {0, 1, 0, 0},
		"a table directory":    {0, 1, 0, 0, 0xFF, 0xFF, 0, 0, 0, 0, 0, 0},
		"a cyclic composite":   glyfFont([][]byte{nil, comp(part(0x0002, 1, 0, 0))}, 1),
		"a table out of range": {0, 1, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 'h', 'e', 'a', 'd', 0, 0, 0, 0, 0, 0, 0xFF, 0xFF, 0, 0, 0xFF, 0xFF},
	} {
		if _, err := bound(font); err == nil || !strings.Contains(err.Error(), "the font declares more than it holds") {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// comp is a composite glyph of parts; part one component record: flags, glyph,
// then the argument and scale bytes the flags declare.
func comp(parts ...[]byte) []byte {
	g := []byte{0xFF, 0xFF, 0, 0, 0, 0, 0, 0, 0, 0}
	for _, p := range parts {
		g = append(g, p...)
	}
	return g
}

func part(flags, gid int, extra ...int) []byte {
	b := p16(p16(nil, flags), gid)
	for _, e := range extra {
		b = append(b, byte(e))
	}
	return b
}

// TestGlyfCostReadsComposites: glyfCost follows the component records as the
// parser does, with each of their argument and scale layouts, and stops where
// the parser does (a truncated record, a missing glyph, point-matching
// arguments).
func TestGlyfCostReadsComposites(t *testing.T) {
	point := []byte{0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x31, 0} // one contour, one point
	cost := func(glyphs ...[]byte) int64 {
		total, err := glyfCost(tablesOf(glyfFont(append([][]byte{nil, point}, glyphs...), 1)))
		if err != nil {
			t.Fatal(err)
		}
		return total
	}
	base := cost()
	if base == 0 {
		t.Fatal("a point costs nothing")
	}
	drawn := map[string][]byte{
		"word arguments and a scale":     comp(part(0x0001|0x0002|0x0008, 1, 0, 0, 0, 0, 0, 0)),
		"x and y scales":                 comp(part(0x0002|0x0040, 1, 0, 0, 0, 0, 0, 0)),
		"a two by two":                   comp(part(0x0002|0x0080, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0)),
		"a record after it, cut short":   comp(part(0x0002|0x0020, 1, 0, 0), []byte{0, 2}),
		"a second record, one more draw": comp(part(0x0002|0x0020, 1, 0, 0), part(0x0002, 1, 0, 0)),
	}
	var first int64
	for name, g := range drawn {
		got := cost(g)
		if got <= base {
			t.Errorf("%s: %d, a point alone costs %d", name, got, base)
		}
		if name == "word arguments and a scale" {
			first = got
		}
	}
	if got := cost(drawn["x and y scales"]); got != first {
		t.Errorf("one record of any layout costs the same, got %d and %d", got, first)
	}
	for name, g := range map[string][]byte{
		"point-matching arguments":  comp(part(0x0000, 1, 0, 0)),
		"a glyph that is not there": comp(part(0x0002, 200, 0, 0)),
	} {
		if got := cost(g); got != base {
			t.Errorf("%s: %d, want %d", name, got, base)
		}
	}
}

// TestGlyfCostLeavesATruncatedLocaToTheParser: with fewer loca entries than
// glyphs, which go-opentype refuses, there is nothing to count.
func TestGlyfCostLeavesATruncatedLocaToTheParser(t *testing.T) {
	tb := tablesOf(glyfManyGlyphs(3))
	tb["loca"] = tb["loca"][:4]
	if total, err := glyfCost(tb); total != 0 || err != nil {
		t.Errorf("%d, %v", total, err)
	}
}
