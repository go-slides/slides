package mathfont

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/go-tex/math"
)

// Font bombs (go-slides/slides#111): fonts under MaxFontBytes with a valid
// signature whose tables declare counts far beyond the bytes they hold, share
// one sub-table among thousands of records, or hold glyphs that each expand
// into millions of points. go-opentype v0.21.1 sized slices from those counts
// before it checked them, so parsing one allocated tens of gigabytes (an
// out-of-memory cannot be recovered); since v0.23.0 it refuses them, and the
// rows marked up say how. Each bomb builds at full size (full=true: the font a
// host would be handed) and scaled (full=false: the same shape, small).

func p16(b []byte, v int) []byte   { return binary.BigEndian.AppendUint16(b, uint16(v)) }
func p32(b []byte, v int64) []byte { return binary.BigEndian.AppendUint32(b, uint32(v)) }

// baseTables is the smallest font go-opentype accepts: one glyph.
func baseTables() map[string][]byte {
	head := make([]byte, 54)
	binary.BigEndian.PutUint16(head[18:], 1000) // unitsPerEm
	maxp := []byte{0, 1, 0, 0, 0, 1}            // version 1.0, one glyph
	hhea := make([]byte, 36)
	binary.BigEndian.PutUint16(hhea[34:], 1) // numberOfHMetrics
	return map[string][]byte{
		"head": head, "maxp": maxp, "hhea": hhea, "MATH": make([]byte, 10), // an empty MATH table

		"hmtx": make([]byte, 4), "loca": make([]byte, 4), "glyf": make([]byte, 4),
	}
}

func sfnt(tables map[string][]byte) []byte { return rebuild([]byte{0, 1, 0, 0}, tables) }

// cmapOf wraps subtables, one record each, platform 3 encoding 10.
func cmapOf(offsets []int64, body []byte) []byte {
	c := p16(p16(nil, 0), len(offsets))
	base := int64(4 + 8*len(offsets))
	for _, o := range offsets {
		c = p32(p16(p16(c, 3), 10), base+o)
	}
	return append(c, body...)
}

// withCmap is a font whose only hostile table is one cmap with one subtable.
func withCmap(sub []byte) []byte {
	t := baseTables()
	t["cmap"] = cmapOf([]int64{0}, sub)
	return sfnt(t)
}

func fmt12(format int, groups int64) []byte {
	return p32(p32(p32(p16(p16(nil, format), 0), 16), 0), groups)
}

type bomb struct {
	name string
	font func(full bool) []byte
	// up is what go-opentype v0.23.0 does with the font on its own, with no
	// guard here: "refused" (math.New fails), "bounded" (it reads the font
	// within a small allocation: a shared sub-table is decoded once, a table
	// go-tex/math ignores is not a risk), "glyph" (it reads the font and
	// refuses the glyph when it is typeset: the formula comes out empty), or ""
	// (it does not bound it: the sum over glyphs is this package's).
	up string
	// budget is the bytes refusing the font may allocate, 8 MiB when zero.
	budget uint64
}

// dropped names the bombs whose hostile table is one go-tex/math never reads:
// it is removed before parsing, not refused.
var dropped = map[string]bool{"gpos-pairpos2-grid": true}

func pick(full bool, huge, small int64) int64 {
	if full {
		return huge
	}
	return small
}

var bombs = []bomb{
	// cmap.go:283, 357, 444, 618: a 32-bit count of 12- or 2-byte records.
	{name: "cmap12-groups", up: "refused", font: func(f bool) []byte { return withCmap(fmt12(12, pick(f, 0xFFFFFFFF, 1<<20))) }},
	{name: "cmap13-groups", up: "refused", font: func(f bool) []byte { return withCmap(fmt12(13, pick(f, 0xFFFFFFFF, 1<<20))) }},
	{name: "cmap8-groups", up: "refused", font: func(f bool) []byte {
		s := p32(p32(p16(p16(nil, 8), 0), 0), 0)
		s = append(s, make([]byte, 8192)...)
		return withCmap(p32(s, pick(f, 0xFFFFFFFF, 1<<20)))
	}},
	{name: "cmap10-chars", up: "refused", font: func(f bool) []byte {
		s := p32(p32(p32(p32(p16(p16(nil, 10), 0), 0), 0), 0), pick(f, 0xFFFFFFFF, 1<<22))
		return withCmap(s)
	}},
	// cmap.go:827, 873, 898: format 14 (variation selectors), three nested counts.
	{name: "cmap14-records", up: "refused", font: func(f bool) []byte {
		return withCmap(p32(p32(p16(nil, 14), 0), pick(f, 0xFFFFFFFF, 1<<20)))
	}},
	{name: "cmap14-default-ranges", up: "refused", font: func(f bool) []byte { return withCmap(fmt14Nested(pick(f, 0xFFFFFFFF, 1<<20), true)) }},
	{name: "cmap14-nondefault-mappings", up: "refused", font: func(f bool) []byte { return withCmap(fmt14Nested(pick(f, 0xFFFFFFFF, 1<<20), false)) }},
	// cmap.go:827+: records that share one subtable, each sized to the bytes present.
	{name: "cmap-shared-subtable", up: "bounded", font: func(f bool) []byte {
		records, groups := int(pick(f, 8000, 50)), int(pick(f, 2000, 100000))
		offs := make([]int64, records)
		body := fmt12(12, int64(groups))
		body = append(body, make([]byte, 12*groups)...)
		t := baseTables()
		t["cmap"] = cmapOf(offs, body)
		return sfnt(t)
	}},
	// gpos.go:503: class1Count*class2Count ints, both 16-bit, no bytes read.
	{name: "gpos-pairpos2-grid", up: "bounded", font: func(f bool) []byte {
		n := int(pick(f, 65535, 2048))
		sub := p16(p16(p16(p16(p16(p16(p16(p16(nil, 2), 0), 0), 0), 0), 0), n), n)
		g := []byte{0, 1, 0, 0, 0, 10, 0, 12, 0, 14} // version, script/feature/lookup offsets
		g = append(g, 0, 0, 0, 0, 0, 1, 0, 4)        // empty ScriptList, FeatureList; LookupList count 1 at +4
		g = append(g, 0, 2, 0, 0, 0, 1, 0, 8)        // Lookup: type 2, flag 0, one subtable at +8
		t := baseTables()
		t["GPOS"] = append(g, sub...)
		return sfnt(t)
	}},
	// math.go:421-425: a MathKern of n heights shared by every corner of every record.
	{name: "math-kern-shared", up: "bounded", font: func(f bool) []byte {
		records, n := int(pick(f, 8000, 1000)), int(pick(f, 8000, 2000))
		covAt := 4 + 8*records
		kernAt := covAt + 4
		ki := p16(p16(nil, covAt), records)
		for i := 0; i < records; i++ {
			for c := 0; c < 4; c++ {
				ki = p16(ki, kernAt)
			}
		}
		ki = append(ki, 0, 1, 0, 0) // Coverage format 1, no glyphs
		ki = append(ki, p16(nil, n)...)
		ki = append(ki, make([]byte, 4*n+4*(n+1))...)
		gi := append([]byte{0, 0, 0, 0, 0, 0, 0, 8}, ki...) // MathGlyphInfo: only the kern offset
		m := []byte{0, 1, 0, 0, 0, 0, 0, 10, 0, 0}
		t := baseTables()
		t["MATH"] = append(m, gi...)
		return sfnt(t)
	}},
	// glyf.go:261-270, read when a glyph is typeset: composites of composites.
	{name: "glyf-composite-chain", up: "glyph", font: func(f bool) []byte { return glyfChain(int(pick(f, 1000, 5))) }},
	// go-opentype/opentype#75: charstring subroutines that call each other k
	// times draw k^10 lines (the depth was capped, not the number of calls).
	{name: "cff-fanout", up: "glyph", font: func(f bool) []byte { return cffFanout(int(pick(f, 8, 4))) }},
	// 400 glyphs of 40,000 lines each, sharing their subroutines: 120 MB of path
	// text kept by go-tex/math's cache, from a font of a few hundred bytes.
	// Refusing it runs glyphs until the total is over, which is time and
	// churn, not memory: 2 GB allocated in all, 110 glyphs live at a time.
	{name: "cff-many-glyphs", budget: 4 << 30, font: func(f bool) []byte { return cffManyGlyphs(int(pick(f, 400, 40))) }},
	// glyf.go:122-169 and go-tex/math's unbounded path cache: glyphs each under
	// the per-glyph cap, together over the total. 40 glyphs of about 3.9 MB
	// counted is 156 MB, in a 20 KB font.
	{name: "glyf-many-glyphs", font: func(f bool) []byte { return glyfManyGlyphs(int(pick(f, 40, 2))) }},
	// math.go:444, 502: a glyph construction of n variants shared by every offset.
	{name: "math-variants-shared", up: "bounded", font: func(f bool) []byte {
		count, n := int(pick(f, 30000, 1000)), int(pick(f, 8000, 2000))
		covAt := 10 + 2*count
		v := p16(p16(p16(p16(p16(nil, 0), covAt), 0), count), 0)
		for i := 0; i < count; i++ {
			v = p16(v, covAt+4)
		}
		v = append(v, 0, 1, 0, 0)     // Coverage format 1, no glyphs
		v = append(v, 0, 0)           // no assembly
		v = append(v, p16(nil, n)...) // variantCount
		v = append(v, make([]byte, 4*n)...)
		m := []byte{0, 1, 0, 0, 0, 0, 0, 0, 0, 10}
		t := baseTables()
		t["MATH"] = append(m, v...)
		return sfnt(t)
	}},
}

// glyfChain: glyph 0 is one point; glyph i (1..8) is a composite of k copies of
// glyph i-1. Loading glyph 8 (the parser allows 8 levels) expands to k^8
// contours, each component decoded and copied separately (glyf.go:261-270). The
// cmap maps "1" to glyph 8.
func glyfChain(k int) []byte {
	const levels = 8
	// glyph 0: one contour, one on-curve point, no coordinate bytes (flags 0x31)
	glyphs := [][]byte{{0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x31, 0}}
	for i := 1; i <= levels; i++ {
		g := append([]byte{0xFF, 0xFF}, make([]byte, 8)...)
		for c := 0; c < k; c++ {
			fl := 0x0002 // arguments are x/y offsets, bytes
			if c < k-1 {
				fl |= 0x0020 // more components
			}
			g = append(p16(p16(g, fl), i-1), 0, 0)
		}
		if len(g)%2 != 0 {
			g = append(g, 0)
		}
		glyphs = append(glyphs, g)
	}
	return glyfFont(glyphs, levels)
}

// glyfManyGlyphs: n distinct simple glyphs of 59,904 points each (a repeated
// flag byte, no coordinate bytes), every one under MaxGlyphBytes by itself.
func glyfManyGlyphs(n int) []byte {
	glyphs := [][]byte{nil} // glyph 0 is empty
	for i := 0; i < n; i++ {
		g := p16(append(p16(nil, 1), make([]byte, 8)...), 59904-1) // one contour
		g = p16(g, 0)                                              // instrLen
		for r := 0; r < 234; r++ {
			g = append(g, 0x39, 255) // on-curve, same x, same y, repeated 255 more times
		}
		if len(g)%2 != 0 {
			g = append(g, 0)
		}
		glyphs = append(glyphs, g)
	}
	return glyfFont(glyphs, 1)
}

// glyfFont assembles a TrueType-outline font from glyph programs, "1" mapping to
// glyph mapTo.
func glyfFont(glyphs [][]byte, mapTo int) []byte {
	var glyf, loca []byte
	for _, g := range glyphs {
		loca = p32(loca, int64(len(glyf)))
		glyf = append(glyf, g...)
	}
	loca = p32(loca, int64(len(glyf)))
	t := baseTables()
	t["head"][51] = 1 // indexToLocFormat: long
	t["maxp"] = p16([]byte{0, 1, 0, 0}, len(glyphs))
	t["hmtx"] = make([]byte, 4+2*(len(glyphs)-1))
	t["loca"], t["glyf"] = loca, glyf
	cmap := make([]byte, 262) // format 0: 256 glyph ids
	binary.BigEndian.PutUint16(cmap[2:], 262)
	cmap['1'+6] = byte(mapTo)
	t["cmap"] = append([]byte{0, 0, 0, 1, 0, 0, 0, 3, 0, 0, 0, 12}, cmap...) // platform 0 (Unicode)
	return sfnt(t)
}

// fmt14Nested is a format 14 subtable with one record whose default (or
// non-default) UVS table declares count entries.
func fmt14Nested(count int64, def bool) []byte {
	s := p32(p32(p16(nil, 14), 0), 1)
	var d, nd int64
	if def {
		d = 21
	} else {
		nd = 21
	}
	s = append(s, 0, 0, 0)
	s = p32(p32(s, d), nd)
	return p32(s, count)
}

// allocated reports the bytes f allocated, from the runtime's cumulative count.
func allocated(f func()) uint64 {
	var a, b runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&a)
	f()
	runtime.ReadMemStats(&b)
	return b.TotalAlloc - a.TotalAlloc
}

// TestBombsAreRefused: through Provider, every bomb is an error naming the
// refusal, within a small allocation: by go-opentype's own error for what it
// bounds at parse time ("cannot be read"), by this package's for the rest
// ("refused"). Run under a small limit, never against the host's memory:
// GOMEMLIMIT=256MiB (and ulimit -v where it is enforced).
func TestBombsAreRefused(t *testing.T) {
	for _, b := range bombs {
		t.Run(b.name, func(t *testing.T) {
			reset(t)
			budget := b.budget
			if budget == 0 {
				budget = 8 << 20
			}
			font := b.font(true)
			if len(font) > MaxFontBytes {
				t.Fatalf("the bomb is %d bytes, over MaxFontBytes", len(font))
			}
			Register(fn(func() ([]byte, error) { return font, nil }))
			var err error
			n := allocated(func() { _, err = Renderer() })
			switch {
			case dropped[b.name]:
				// What is left once the table is removed is a valid font.
				if err != nil {
					t.Fatalf("a font whose only hostile table is dropped: %v", err)
				}
			case b.up == "refused":
				if err == nil || !Unavailable(err) || !strings.Contains(err.Error(), "a declared count exceeds the data present") {
					t.Fatalf("not refused by go-opentype: %v", err)
				}
			case b.up == "bounded":
				if err != nil {
					t.Fatalf("go-opentype bounds this font, the guard has no reason to refuse it: %v", err)
				}
			case err == nil || !Unavailable(err) || !strings.Contains(err.Error(), "refused"):
				t.Fatalf("not refused: %v", err)
			}
			if n > budget {
				t.Errorf("refusing allocated %d bytes, over %d", n, budget)
			}
			t.Logf("%d-byte font gave %v after %d bytes", len(font), err, n)
		})
	}
}

// TestUpstreamBoundsWhatItCovers is the finding of go-opentype/opentype#75, as
// fixed in v0.23.0: each bomb handed straight to go-tex/math with no guard, at
// full size, is refused when parsed, or read within a small allocation, or its
// glyph is refused when it is typeset (the formula comes out with no path, and
// an error is not an option: go-tex/math skips a glyph it cannot draw). The
// glyph rows are opentype#75's two reproducers, composites of composites (k^8
// contours) and charstring subroutines (k^10 lines). A bomb marked up "" is the
// sum over glyphs that go-opentype cannot bound: it must be read, and kept.
func TestUpstreamBoundsWhatItCovers(t *testing.T) {
	const budget = 32 << 20
	const tex = "1"
	for _, b := range bombs {
		t.Run(b.name, func(t *testing.T) {
			font := b.font(true)
			var err, rerr error
			var svg string
			n := allocated(func() {
				var r *math.Renderer
				if r, err = math.New(font); err == nil {
					svg, rerr = r.RenderSVG(tex, 40)
				}
			})
			switch b.up {
			case "refused":
				if err == nil || !strings.Contains(err.Error(), "a declared count exceeds the data present") {
					t.Fatalf("go-opentype read it: %v", err)
				}
			case "bounded", "glyph":
				if err != nil || rerr != nil {
					t.Fatalf("go-opentype refused it whole: %v, %v", err, rerr)
				}
				if b.up == "glyph" && strings.Contains(svg, "<path") {
					t.Errorf("the glyph was drawn: %s", svg)
				}
			default:
				if b.up != "" || err != nil || rerr != nil || !strings.Contains(svg, "<path") {
					t.Fatalf("go-opentype does not bound this one, and reads it: %v, %v", err, rerr)
				}
				n = 0 // the paths kept are the finding, not the allocation
			}
			if n > budget {
				t.Errorf("%d bytes allocated, over %d", n, budget)
			}
			t.Logf("%s: %d-byte font -> %d bytes allocated", b.name, len(font), n)
		})
	}
}

// TestGlyphBombsDrawWhenSmall is the control of the two glyph reproducers: the
// same fonts with a fan-out of 2 are drawn, so that "no path" above is the
// refusal and not a font that cannot draw at all.
func TestGlyphBombsDrawWhenSmall(t *testing.T) {
	for name, font := range map[string][]byte{"composite": glyfChain(2), "cff": cffFanout(2)} {
		r, err := math.New(font)
		if err != nil {
			t.Fatal(err)
		}
		if svg, err := r.RenderSVG("1", 40); err != nil || !strings.Contains(svg, "<path") {
			t.Errorf("%s: %v %s", name, err, svg)
		}
	}
}

// TestRealFontIsUnchanged: the guard must change nothing for a real font.
func TestRealFontIsUnchanged(t *testing.T) {
	good := stix()
	slim, err := bound(good)
	if err != nil {
		t.Fatal(err)
	}
	const tex = `\sum_{i=0}^{n}\frac{x^2}{\sqrt{1+\left(\frac{a}{b}\right)}}\int_0^1 f(x)\,dx`
	want, err := render(good, tex)
	if err != nil {
		t.Fatal(err)
	}
	got, err := render(slim, tex)
	if err != nil || got != want {
		t.Errorf("rendering changed after the guard (err %v)", err)
	}
	t.Logf("STIX: %d bytes -> %d after dropping the tables go-tex/math does not read", len(good), len(slim))
}

func render(font []byte, tex string) (string, error) {
	r, err := math.New(font)
	if err != nil {
		return "", err
	}
	return r.RenderDisplaySVG(tex, 80)
}

// The corpus holds the bombs as fuzz seeds: `go test` replays it, so every run
// reads them. UPDATE_CORPUS=1 rewrites it from the builders above.
func TestCorpusHoldsTheBombs(t *testing.T) {
	dir := filepath.Join("testdata", "fuzz", "FuzzFont")
	want := map[string]string{}
	for _, b := range bombs {
		want["bomb-"+b.name] = fmt.Sprintf("go test fuzz v1\n[]byte(%q)\n", b.font(true))
	}
	if os.Getenv("UPDATE_CORPUS") != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, body := range want {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	names := make([]string, 0, len(want))
	for n := range want {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		got, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil || string(got) != want[n] {
			t.Errorf("corpus file %s is missing or stale: run UPDATE_CORPUS=1 go test ./mathfont -run TestCorpusHoldsTheBombs (%v)", n, err)
		}
	}
}

// TestAParserPanicIsAnError: go-opentype no longer panics on the bombs, but a
// font that makes it panic is still an error the host can read, not a crash.
func TestAParserPanicIsAnError(t *testing.T) {
	reset(t)
	old := newRenderer
	t.Cleanup(func() { newRenderer = old })
	newRenderer = func([]byte) (*math.Renderer, error) { panic("index out of range") }
	Register(fn(func() ([]byte, error) { return stix(), nil }))
	if _, err := Renderer(); err == nil || !Unavailable(err) || !strings.Contains(err.Error(), "cannot be read: index out of range") {
		t.Fatalf("got %v", err)
	}
}
