package mathfont

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"

	"github.com/go-opentype/opentype"
)

// What go-opentype bounds, and what it leaves to this package. Since v0.23.0
// (go-opentype/opentype#75) Parse checks every count a font declares against the
// bytes present, decodes a shared sub-table once, and charges every glyph a
// budget (a composite or a charstring emits at most 65,535 points, a charstring
// runs at most 131,072 operators): a font cannot make Parse allocate more than
// 1 MiB plus 64 bytes per input byte, nor make one glyph cost more than a few
// milliseconds. What it cannot bound is the SUM over glyphs: go-tex/math keeps
// the SVG path of every (glyph, pixel size) it typesets, with no limit, so a font
// of thousands of glyphs, each within its own budget, keeps gigabytes alive.
// That is bounded here, once for each kind of outline, by MaxOutlineBytes
// (TrueType) and MaxPathBytes (CFF).

// MaxOutlineBytes bounds the outlines of a TrueType (glyf) font, as glyfCost
// counts them: the bytes go-opentype allocates loading every glyph once, which
// is about 3.8 times the path text go-tex/math keeps for it. It was set from
// TestGlyfBudgets, which measured seven system fonts: the largest total is
// 15,625,865 (Apple Symbols), a margin of 8; Arial Unicode (281,719,255) is over
// MaxFontBytes. go-tex/math caches a path per (glyph, pixel size), so what a
// hostile font keeps alive is the bound times the number of sizes a deck uses.
const MaxOutlineBytes = 128 << 20

// MaxPathBytes bounds the outlines of a CFF font, as the SVG path text of all its
// glyphs at one size, which is what go-tex/math keeps. A CFF charstring cannot be
// costed without running it (subroutines fan out), so the font's glyphs are run,
// once, by go-opentype, and the run stops at the bound. STIX Two Math, 6,760
// glyphs, is 8,202,727 bytes at 80 px (7,429,115 at 10 px, 8,713,974 at 1000 px)
// and runs in 74 ms: the margin is 8
// (7.7 at 1000 px). The upper bound without running them,
// 65,536 points per glyph, would allow gigabytes for STIX's glyph count.
const MaxPathBytes = 64 << 20

// pathPx is the pixel size the CFF glyphs are run at. The text of a path hardly
// depends on it (see MaxPathBytes).
const pathPx = 80

// keep is the set of sfnt tables handed to the parser: the ones go-tex/math
// reads (outlines, metrics, character map, MATH) and the hinting programs.
// Everything else, the layout (GSUB, GPOS, GDEF, kern) and variation tables
// above all, is dropped before parsing: go-tex/math never reads them, so there
// is no reason to give them to a parser. This is defence in depth, not a
// bound: go-opentype checks them since v0.23.0.
var keep = map[string]bool{
	"head": true, "maxp": true, "hhea": true, "hmtx": true, "loca": true,
	"glyf": true, "CFF ": true, "cmap": true, "MATH": true, "OS/2": true,
	"post": true, "fpgm": true, "prep": true, "cvt ": true,
}

var errBound = errors.New("the font declares more than it holds")

// bound returns the font reduced to the tables in keep, after checking, without
// allocating from any count it reads, that the table directory lies within the
// font, and that the outlines go-tex/math will cache stay within MaxOutlineBytes
// (TrueType) or MaxPathBytes (CFF).
func bound(font []byte) ([]byte, error) {
	if len(font) < 12 {
		return nil, fmt.Errorf("%w: short header", errBound)
	}
	n := int(binary.BigEndian.Uint16(font[4:]))
	if 12+n*16 > len(font) {
		return nil, fmt.Errorf("%w: table directory", errBound)
	}
	tables := map[string][]byte{}
	for i := 0; i < n; i++ {
		rec := font[12+i*16:]
		tag := string(rec[:4])
		off := int64(binary.BigEndian.Uint32(rec[8:]))
		length := int64(binary.BigEndian.Uint32(rec[12:]))
		if off+length > int64(len(font)) {
			return nil, fmt.Errorf("%w: table %q is out of range", errBound, tag)
		}
		if _, dup := tables[tag]; !keep[tag] || dup {
			continue
		}
		tables[tag] = font[off : off+length]
	}
	total, err := glyfCost(tables)
	if err != nil {
		return nil, err
	}
	if total > MaxOutlineBytes {
		return nil, fmt.Errorf("%w: typesetting its glyphs would allocate over the %d bytes allowed (%d counted)", errBound, MaxOutlineBytes, total)
	}
	slim := rebuild(font[:4], tables)
	if tables["CFF "] != nil {
		if err := cffPaths(slim); err != nil {
			return nil, err
		}
	}
	return slim, nil
}

// cffPaths runs every glyph of a CFF font, as go-tex/math will when it typesets,
// and refuses the font when a glyph is over go-opentype's budget (real fonts
// have none: 155 were measured upstream) or when the paths together are over
// MaxPathBytes. The run stops at the first refusal: a font of 65,535 glyphs each
// just under its budget would otherwise cost minutes.
func cffPaths(font []byte) error {
	f, err := opentype.Parse(font)
	if err != nil {
		return fmt.Errorf("%w: %v", errBound, err)
	}
	face := f.NewFace(pathPx)
	var total int
	for g := 0; g < f.NumGlyphs(); g++ {
		d, ok := face.GlyphSVGPath(opentype.GlyphIndex(g))
		if !ok {
			return fmt.Errorf("%w: glyph %d is over the outline budget", errBound, g)
		}
		if total += len(d); total > MaxPathBytes {
			return fmt.Errorf("%w: its glyphs would keep over the %d bytes of paths allowed (%d counted at glyph %d)", errBound, MaxPathBytes, total, g)
		}
	}
	return nil
}

// u reads big-endian integers at off of b, remembering a short read instead of
// panicking.
type u struct {
	b   []byte
	bad bool
}

func (r *u) u16(off int) int {
	if off < 0 || off+2 > len(r.b) {
		r.bad = true
		return 0
	}
	return int(binary.BigEndian.Uint16(r.b[off:]))
}

func truncated(what string) error {
	return fmt.Errorf("%w: %s", errBound, what)
}

// satCap saturates glyph costs: past it the font is refused anyway, and the
// arithmetic cannot overflow an int64.
const satCap = int64(1) << 50

func sat(v int64) int64 {
	if v > satCap || v < 0 {
		return satCap
	}
	return v
}

// glyfCost returns the bytes go-opentype (glyf.go) allocates loading every
// glyph of a TrueType-outline font once: the total MaxOutlineBytes bounds. A
// composite decodes and copies every component record separately, so a glyph
// that references another k times costs k times it, and the cost multiplies
// along the chain; go-opentype refuses a glyph over 65,535 points, but this
// counts it first, which is the safe side. It reads loca and glyf as the parser
// does; a glyph the parser would reject costs what was allocated before the
// rejection. Each glyph is costed once.
func glyfCost(tables map[string][]byte) (total int64, err error) {
	glyf, loca, head, maxp := tables["glyf"], tables["loca"], tables["head"], tables["maxp"]
	if glyf == nil || loca == nil || len(head) < 52 || len(maxp) < 6 {
		return 0, nil
	}
	numGlyphs := int(binary.BigEndian.Uint16(maxp[4:]))
	long := binary.BigEndian.Uint16(head[50:]) != 0
	entry := 2
	if long {
		entry = 4
	}
	if len(loca) < (numGlyphs+1)*entry {
		return 0, nil // the parser refuses the table
	}
	at := func(i int) int64 {
		if long {
			return int64(binary.BigEndian.Uint32(loca[i*4:]))
		}
		return int64(binary.BigEndian.Uint16(loca[i*2:])) * 2
	}
	type cost struct{ size, alloc int64 } // expanded outline bytes; bytes allocated loading
	const point, header = 24, 24          // outlinePoint, contour slice header
	memo := make([]*cost, numGlyphs)
	onPath := make([]bool, numGlyphs)
	var load func(g, depth int) (cost, error)
	load = func(g, depth int) (cost, error) {
		if memo[g] != nil {
			return *memo[g], nil
		}
		if onPath[g] || depth > 64 {
			return cost{}, truncated("cyclic or very deeply nested composite glyphs")
		}
		start, end := at(g), at(g+1)
		if start > end || end > int64(len(glyf)) || end-start < 10 {
			memo[g] = &cost{}
			return cost{}, nil
		}
		onPath[g] = true
		defer func() { onPath[g] = false }()
		b := glyf[start:end]
		n := int(int16(binary.BigEndian.Uint16(b)))
		var c cost
		if n >= 0 {
			points := int64(0)
			if n > 0 && 10+2*n <= len(b) {
				points = int64(binary.BigEndian.Uint16(b[10+2*(n-1):])) + 1
			}
			c.size = points*point + int64(n)*header
			// The points are appended contour by contour (glyf.go:159-165), so
			// the slices grow by doubling: count twice the points.
			c.alloc = 2*c.size + points*17 + int64(n)*2
		} else {
			r := &u{b: b}
			for pos := 10; ; {
				flags, gid := r.u16(pos), r.u16(pos+2)
				pos += 4
				if flags&0x0001 != 0 {
					pos += 4
				} else {
					pos += 2
				}
				switch {
				case flags&0x0008 != 0:
					pos += 2
				case flags&0x0040 != 0:
					pos += 4
				case flags&0x0080 != 0:
					pos += 8
				}
				if r.bad || pos > len(b) || gid >= numGlyphs || flags&0x0002 == 0 {
					break // the parser stops with an error here
				}
				sub, err := load(gid, depth+1)
				if err != nil {
					return cost{}, err
				}
				c.size = sat(c.size + sub.size)
				c.alloc = sat(c.alloc + sub.alloc + 2*sub.size)
				if flags&0x0020 == 0 {
					break
				}
			}
		}
		memo[g] = &c
		return c, nil
	}
	for g := 0; g < numGlyphs; g++ {
		c, err := load(g, 0)
		if err != nil {
			return 0, err
		}
		total = sat(total + c.alloc)
	}
	return total, nil
}

// rebuild assembles an sfnt from the tables, in tag order, 4-byte aligned. The
// parser reads neither the checksums nor the search fields, so they are left
// zero.
func rebuild(version []byte, tables map[string][]byte) []byte {
	tags := make([]string, 0, len(tables))
	size := 12 + 16*len(tables)
	for t, d := range tables {
		tags = append(tags, t)
		size += (len(d) + 3) &^ 3
	}
	sort.Strings(tags)
	out := make([]byte, 12+16*len(tags), size)
	copy(out, version)
	binary.BigEndian.PutUint16(out[4:], uint16(len(tags)))
	for i, t := range tags {
		rec := out[12+16*i:]
		copy(rec, t)
		binary.BigEndian.PutUint32(rec[8:], uint32(len(out)))
		binary.BigEndian.PutUint32(rec[12:], uint32(len(tables[t])))
		out = append(out, tables[t]...)
		for len(out)%4 != 0 {
			out = append(out, 0)
		}
	}
	return out
}
