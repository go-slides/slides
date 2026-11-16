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
// signature whose tables declare counts far beyond the bytes they hold, or
// share one sub-table among thousands of records. go-opentype v0.21.1 sizes
// slices from those counts before it checks them, so parsing one allocates up
// to tens of gigabytes; an out-of-memory cannot be recovered. Each bomb builds
// at full size (full=true: the font a host would be handed) and scaled
// (full=false: the same shape, small enough to run unguarded and measure).

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
	{"cmap12-groups", func(f bool) []byte { return withCmap(fmt12(12, pick(f, 0xFFFFFFFF, 1<<20))) }},
	{"cmap13-groups", func(f bool) []byte { return withCmap(fmt12(13, pick(f, 0xFFFFFFFF, 1<<20))) }},
	{"cmap8-groups", func(f bool) []byte {
		s := p32(p32(p16(p16(nil, 8), 0), 0), 0)
		s = append(s, make([]byte, 8192)...)
		return withCmap(p32(s, pick(f, 0xFFFFFFFF, 1<<20)))
	}},
	{"cmap10-chars", func(f bool) []byte {
		s := p32(p32(p32(p32(p16(p16(nil, 10), 0), 0), 0), 0), pick(f, 0xFFFFFFFF, 1<<22))
		return withCmap(s)
	}},
	// cmap.go:827, 873, 898: format 14 (variation selectors), three nested counts.
	{"cmap14-records", func(f bool) []byte {
		return withCmap(p32(p32(p16(nil, 14), 0), pick(f, 0xFFFFFFFF, 1<<20)))
	}},
	{"cmap14-default-ranges", func(f bool) []byte { return withCmap(fmt14Nested(pick(f, 0xFFFFFFFF, 1<<20), true)) }},
	{"cmap14-nondefault-mappings", func(f bool) []byte { return withCmap(fmt14Nested(pick(f, 0xFFFFFFFF, 1<<20), false)) }},
	// cmap.go:827+: records that share one subtable, each sized to the bytes present.
	{"cmap-shared-subtable", func(f bool) []byte {
		records, groups := int(pick(f, 8000, 50)), int(pick(f, 2000, 100000))
		offs := make([]int64, records)
		body := fmt12(12, int64(groups))
		body = append(body, make([]byte, 12*groups)...)
		t := baseTables()
		t["cmap"] = cmapOf(offs, body)
		return sfnt(t)
	}},
	// gpos.go:503: class1Count*class2Count ints, both 16-bit, no bytes read.
	{"gpos-pairpos2-grid", func(f bool) []byte {
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
	{"math-kern-shared", func(f bool) []byte {
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
	{"glyf-composite-chain", func(f bool) []byte { return glyfChain(int(pick(f, 1000, 5))) }},
	// math.go:444, 502: a glyph construction of n variants shared by every offset.
	{"math-variants-shared", func(f bool) []byte {
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
	var glyf, loca []byte
	for _, g := range glyphs {
		loca = p32(loca, int64(len(glyf)))
		glyf = append(glyf, g...)
	}
	loca = p32(loca, int64(len(glyf)))
	t := baseTables()
	t["head"][51] = 1 // indexToLocFormat: long
	t["maxp"] = []byte{0, 1, 0, 0, 0, byte(len(glyphs))}
	t["hmtx"] = make([]byte, 4+2*(len(glyphs)-1))
	t["loca"], t["glyf"] = loca, glyf
	cmap := make([]byte, 262) // format 0: 256 glyph ids
	binary.BigEndian.PutUint16(cmap[2:], 262)
	cmap['1'+6] = levels
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
// refusal, within a small allocation. Run under a small limit, never against
// the host's memory: GOMEMLIMIT=256MiB (and ulimit -v where it is enforced).
func TestBombsAreRefused(t *testing.T) {
	const budget = 8 << 20
	for _, b := range bombs {
		t.Run(b.name, func(t *testing.T) {
			reset(t)
			font := b.font(true)
			if len(font) > MaxFontBytes {
				t.Fatalf("the bomb is %d bytes, over MaxFontBytes", len(font))
			}
			Register(fn(func() ([]byte, error) { return font, nil }))
			var err error
			n := allocated(func() { _, err = Renderer() })
			if dropped[b.name] {
				// What is left once the table is removed is a valid font.
				if err != nil {
					t.Fatalf("a font whose only hostile table is dropped: %v", err)
				}
			} else if err == nil || !Unavailable(err) || !strings.Contains(err.Error(), "refused") {
				t.Fatalf("not refused: %v", err)
			}
			if n > budget {
				t.Errorf("refusing allocated %d bytes, over %d", n, budget)
			}
			t.Logf("%d-byte font refused after %d bytes: %v", len(font), n, err)
		})
	}
}

// TestUpstreamAllocatesFromDeclaredCounts is the finding, measured: each bomb
// at a scale that is safe to run, handed straight to go-tex/math with no guard.
// It logs how much the parser allocated for how many bytes of font; it never
// fails, and says so when upstream starts to bound the case.
func TestUpstreamAllocatesFromDeclaredCounts(t *testing.T) {
	for _, b := range bombs {
		t.Run(b.name, func(t *testing.T) {
			font := b.font(false)
			n := allocated(func() {
				defer func() { _ = recover() }()
				if r, err := math.New(font); err == nil {
					_, _ = r.RenderSVG("1", 40) // glyph outlines are decoded when typeset
				}
			})
			if n < 1<<20 {
				t.Logf("upstream now bounds %s (%d bytes allocated): the guard case can go", b.name, n)
				return
			}
			t.Logf("%s: %d-byte font -> %d bytes allocated (x%d)", b.name, len(font), n, n/uint64(len(font)))
		})
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
