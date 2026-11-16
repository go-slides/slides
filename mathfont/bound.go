package mathfont

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
)

// MaxParseBytes bounds what reading a font may allocate, as estimated from the
// counts the font declares (see bound). The 8 MiB file limit does not bound it:
// go-opentype v0.21.1 sizes slices from counts in the file before it checks them
// against the bytes present, and one table can be referenced many times, so a
// few hundred bytes can ask for gigabytes (go-slides/slides#111). 64 MiB is far
// above any real math font (STIX Two Math needs well under 8 MiB).
const MaxParseBytes = 64 << 20

// keep is the set of sfnt tables handed to the parser: the ones go-tex/math
// reads (outlines, metrics, character map, MATH) and the hinting programs.
// Everything else, the layout (GSUB, GPOS, GDEF, kern) and variation tables
// above all, is dropped before parsing: go-tex/math never reads them, and each
// sizes allocations from counts in the file.
var keep = map[string]bool{
	"head": true, "maxp": true, "hhea": true, "hmtx": true, "loca": true,
	"glyf": true, "CFF ": true, "cmap": true, "MATH": true, "OS/2": true,
	"post": true, "fpgm": true, "prep": true, "cvt ": true,
}

var errBound = errors.New("the font declares more than it holds")

// bound checks, without allocating from any count it reads, that parsing the
// font stays within MaxParseBytes, and returns the font reduced to the tables
// in keep. It refuses a variable-CFF (CFF2) font, which carries counts
// go-opentype sizes the same way.
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
		if tag == "CFF2" {
			return nil, errors.New("a variable CFF (CFF2) font is not supported")
		}
		if _, dup := tables[tag]; !keep[tag] || dup {
			continue
		}
		tables[tag] = font[off : off+length]
	}
	var cost int64
	if b, ok := tables["cmap"]; ok {
		c, err := cmapCost(b)
		if err != nil {
			return nil, err
		}
		cost += c
	}
	if b, ok := tables["MATH"]; ok {
		c, err := mathCost(b)
		if err != nil {
			return nil, err
		}
		cost += c
	}
	if cost > MaxParseBytes {
		return nil, fmt.Errorf("%w: parsing it would allocate over the %d bytes allowed (%d counted before stopping)", errBound, MaxParseBytes, cost)
	}
	return rebuild(font[:4], tables), nil
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

func (r *u) u32(off int) int64 {
	if off < 0 || off+4 > len(r.b) {
		r.bad = true
		return 0
	}
	return int64(binary.BigEndian.Uint32(r.b[off:]))
}

// has reports whether n records of size bytes lie at off, without multiplying
// an unbounded count into an int.
func (r *u) has(off int, n int64, size int) bool {
	return off >= 0 && off <= len(r.b) && n >= 0 && n <= int64(len(r.b)-off)/int64(size)
}

func truncated(what string) error {
	return fmt.Errorf("%w: %s", errBound, what)
}

// cmapCost estimates what go-opentype allocates reading every subtable of the
// cmap table (it reads them all, and the records may share one offset).
func cmapCost(b []byte) (int64, error) {
	r := &u{b: b}
	n := r.u16(2)
	if r.bad || !r.has(4, int64(n), 8) {
		return 0, truncated("cmap records")
	}
	var cost int64
	for i := 0; i < n; i++ {
		off := r.u32(4 + i*8 + 4)
		if r.bad || off+2 > int64(len(b)) {
			return 0, truncated("cmap subtable offset")
		}
		sub := &u{b: b[off:]}
		switch format := sub.u16(0); format {
		case 8:
			groups := sub.u32(12 + 8192)
			if sub.bad || !sub.has(12+8192+4, groups, 12) {
				return 0, truncated("cmap format 8 groups")
			}
			cost += 8192 + groups*16
		case 10:
			chars := sub.u32(16)
			if sub.bad || !sub.has(20, chars, 2) {
				return 0, truncated("cmap format 10 glyphs")
			}
			cost += chars * 2
		case 12, 13:
			groups := sub.u32(12)
			if sub.bad || !sub.has(16, groups, 12) {
				return 0, truncated("cmap groups")
			}
			cost += groups * 16
		case 14:
			recs := sub.u32(6)
			if sub.bad || !sub.has(10, recs, 11) {
				return 0, truncated("cmap format 14 records")
			}
			cost += recs * 64
			for j := int64(0); j < recs; j++ {
				at := 10 + int(j)*11
				for k, size := range [2]int{4, 5} {
					o := sub.u32(at + 3 + k*4)
					if o == 0 {
						continue
					}
					if o+4 > int64(len(sub.b)) {
						return 0, truncated("cmap format 14 table offset")
					}
					cnt := sub.u32(int(o))
					if !sub.has(int(o)+4, cnt, size) {
						return 0, truncated("cmap format 14 mappings")
					}
					cost += cnt * 16
				}
			}
		default:
			// Formats 0, 2, 4 and 6 size their slices by 16-bit counts and by the
			// bytes that remain: the remainder of the table over-counts, soundly.
			cost += int64(len(sub.b))
		}
		if cost > MaxParseBytes {
			return cost, nil
		}
	}
	return cost, nil
}

// covCost checks a Coverage table at off of b and returns the work it asks for:
// format 2 expands every range into a map, so overlapping ranges cost time
// quadratic in the file.
func covCost(r *u, off int) (int64, error) {
	if off < 0 || off+4 > len(r.b) {
		return 0, truncated("coverage offset")
	}
	cnt := r.u16(off + 2)
	switch f := r.u16(off); f {
	case 1:
		if !r.has(off+4, int64(cnt), 2) {
			return 0, truncated("coverage glyphs")
		}
		return int64(cnt) * 16, nil
	case 2:
		if !r.has(off+4, int64(cnt), 6) {
			return 0, truncated("coverage ranges")
		}
		var span int64
		for i := 0; i < cnt; i++ {
			s, e := r.u16(off+4+i*6), r.u16(off+6+i*6)
			if e >= s {
				span += int64(e-s) + 1
			}
			if span > 1<<20 {
				return 0, truncated("coverage ranges span too many glyphs")
			}
		}
		return span * 16, nil
	}
	return 0, truncated("coverage format")
}

// mathCost estimates what go-opentype allocates reading the MATH table, where
// one MathKern or MathGlyphConstruction may be referenced by thousands of
// records.
func mathCost(b []byte) (int64, error) {
	r := &u{b: b}
	constOff, giOff, varOff := r.u16(4), r.u16(6), r.u16(8)
	if r.bad {
		return 0, truncated("MATH header")
	}
	var cost int64
	if giOff != 0 {
		g := &u{b: b[min(giOff, len(b)):]}
		for i := 0; i < 2; i++ { // italics correction, top accent attachment
			o := g.u16(i * 2)
			if o == 0 {
				continue
			}
			t := &u{b: g.b[min(o, len(g.b)):]}
			cnt := t.u16(2)
			if t.bad || !t.has(4, int64(cnt), 4) {
				return 0, truncated("MATH per-glyph values")
			}
			c, err := covCost(t, t.u16(0))
			if err != nil {
				return 0, err
			}
			cost += int64(cnt)*4 + c
		}
		if o := g.u16(4); o != 0 { // extended shape coverage
			t := &u{b: g.b[min(o, len(g.b)):]}
			c, err := covCost(t, 0)
			if err != nil {
				return 0, err
			}
			cost += c
		}
		if o := g.u16(6); o != 0 {
			c, err := kernInfoCost(&u{b: g.b[min(o, len(g.b)):]})
			if err != nil {
				return 0, err
			}
			cost += c
		}
		if g.bad {
			return 0, truncated("MATH glyph info")
		}
	}
	if varOff != 0 {
		c, err := variantsCost(&u{b: b[min(varOff, len(b)):]})
		if err != nil {
			return 0, err
		}
		cost += c
	}
	_ = constOff
	return cost, nil
}

func kernInfoCost(t *u) (int64, error) {
	cnt := t.u16(2)
	if t.bad || !t.has(4, int64(cnt), 8) {
		return 0, truncated("MATH kern info")
	}
	cost := int64(cnt) * 80
	for i := 0; i < cnt; i++ {
		for c := 0; c < 4; c++ {
			o := t.u16(4 + i*8 + c*2)
			if o == 0 {
				continue
			}
			if o+2 > len(t.b) {
				return 0, truncated("MATH kern offset")
			}
			n := t.u16(o)
			if !t.has(o+2, int64(n), 4) {
				return 0, truncated("MATH kern heights")
			}
			cost += int64(n)*8 + 64
			if cost > MaxParseBytes {
				return cost, nil
			}
		}
	}
	cov, err := covCost(t, t.u16(0))
	return cost + cov, err
}

func variantsCost(t *u) (int64, error) {
	vc, hc := t.u16(6), t.u16(8)
	if t.bad || !t.has(10, int64(vc)+int64(hc), 2) {
		return 0, truncated("MATH variants")
	}
	var cost int64
	for i := 0; i < vc+hc; i++ {
		o := t.u16(10 + i*2)
		if o+4 > len(t.b) {
			return 0, truncated("MATH construction offset")
		}
		cost += 8
		if (i < vc && t.u16(2) == 0) || (i >= vc && t.u16(4) == 0) {
			continue // an empty axis: the construction is not read
		}
		c := &u{b: t.b[o:]}
		asm, n := c.u16(0), c.u16(2)
		if !c.has(4, int64(n), 4) {
			return 0, truncated("MATH variant records")
		}
		cost += int64(n)*8 + 64
		if asm != 0 {
			if asm+6 > len(c.b) {
				return 0, truncated("MATH assembly offset")
			}
			p := c.u16(asm + 4)
			if !c.has(asm+6, int64(p), 10) {
				return 0, truncated("MATH assembly parts")
			}
			cost += int64(p)*16 + 64
		}
		if cost > MaxParseBytes {
			return cost, nil
		}
	}
	return cost, nil
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
