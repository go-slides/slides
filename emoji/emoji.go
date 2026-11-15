// Package emoji is where the deck package gets the DATA of its emoji: the SVG
// of each graphic. The graphics are a Provider. Package emoji/embedded is the
// one go-slides ships (the 2 MB set of go-icons/twemoji), and the slides
// command links it. A host that must not link it (a WebAssembly page, which
// fetches the data when it needs it) builds slides with the tag slides_noemoji
// and registers a Provider of its own with Register, now or later.
//
// What stays in the binary either way is everything that RECOGNIZES an emoji,
// so that reading text never calls a provider: the list of the 4,009 stems of
// the set (stems.txt, 69 KB), the markdown-it-emoji :shortcode: table (35 KB),
// the list of the characters whose default presentation is text (1 KB), and
// Cut, which reads the longest emoji at the start of a text the way Marp Core
// does. A Provider is asked for one thing, the SVG of a stem, once for each
// graphic a deck uses.
//
// # Before the data arrives
//
// Rendering is synchronous and never waits. With no Provider registered, or one
// that answers "none" (SVGByStem returns false) because its data is not loaded
// yet, an emoji is left as the text that was written: the Unicode character, or
// the :shortcode: with its colons, exactly as with Config.NoEmoji except that
// the stylesheet rule of the images is still added. Nothing is an error and
// nothing is a warning. When the data has arrived the host registers the
// Provider again (Register, with the same value or a new one) and renders the
// deck again: the pictures appear on that render. A Provider must answer the
// same way for the same stem for as long as it stays registered; Register drops
// what was cached from the one before.
//
// # What a Provider may be
//
// A Provider is untrusted: its answers are bounded here, before the render
// uses them. An SVG is at most MaxSVGBytes and must start with "<svg" or
// "<?xml"; an answer that is not that is taken as "none". A panic in a Provider
// is taken as "none" too (it is contained once for each graphic, not for each
// character of the text). At most MaxCachedImages images, MaxCachedBytes bytes
// in all, are kept between renders; past that an image is made again each time
// it is used. A Provider is called from several goroutines.
package emoji

import (
	_ "embed" // the tables
	"encoding/base64"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf8"
)

// The bounds of what a Provider may answer, and of what is kept.
const (
	MaxSVGBytes     = 256 << 10
	MaxCachedImages = 1 << 13
	MaxCachedBytes  = 16 << 20
)

// A Provider holds the graphics.
type Provider interface {
	// SVGByStem returns the SVG document of the graphic a stem names (one of
	// Stems), or false when it does not have it (yet). A stem is how twemoji.js
	// names a graphic: lower-case hexadecimal code points joined by "-".
	SVGByStem(stem string) (string, bool)
}

type state struct {
	p     Provider
	cache sync.Map // stem -> data URI
	n     atomic.Int64
	bytes atomic.Int64
}

var current atomic.Pointer[state]

// Register makes p the Provider the render uses, from the next call on, and
// drops what was cached from the one before. A nil p leaves no Provider: emoji
// stay text. It may be called at any time, from any goroutine.
func Register(p Provider) {
	if p == nil {
		current.Store(nil)
		return
	}
	current.Store(&state{p: p})
}

// Registered reports whether a Provider is registered. It says nothing of
// whether its data is loaded.
func Registered() bool { return current.Load() != nil }

func validSVG(s string) bool {
	if len(s) == 0 || len(s) > MaxSVGBytes {
		return false
	}
	s = strings.TrimLeft(s, " \t\r\n\xef\xbb\xbf")
	return strings.HasPrefix(s, "<svg") || strings.HasPrefix(s, "<?xml")
}

// svg asks the provider, the one place its code runs: a panic is "none".
func (s *state) svg(stem string) (svg string, ok bool) {
	defer func() {
		if recover() != nil {
			svg, ok = "", false
		}
	}()
	svg, ok = s.p.SVGByStem(stem)
	if !ok || !validSVG(svg) {
		return "", false
	}
	return svg, true
}

// Src is the data: URI of the graphic for seq, or "" when there is none: seq is
// not in the set, no Provider is registered, the Provider has not the graphic
// (or not its data yet), or its answer is out of bounds.
func Src(seq string) string {
	stem := stemOf(seq)
	if stem == "" {
		return ""
	}
	s := current.Load()
	if s == nil {
		return ""
	}
	if v, ok := s.cache.Load(stem); ok {
		return v.(string)
	}
	svg, ok := s.svg(stem)
	if !ok {
		return ""
	}
	uri := "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(svg))
	if s.n.Load() < MaxCachedImages && s.bytes.Load()+int64(len(uri)) <= MaxCachedBytes {
		if _, loaded := s.cache.LoadOrStore(stem, uri); !loaded {
			s.n.Add(1)
			s.bytes.Add(int64(len(uri)))
		}
	}
	return uri
}

// ---- the tables that stay in the binary ----

//go:embed stems.txt
var stemData string

//go:embed shortcodes.txt
var shortcodeData string

//go:embed textdefault.txt
var textDefaultData string

var (
	setOnce sync.Once
	stems   []string
	byNorm  map[string]string // sequence without U+FE0F -> stem
	firsts  map[rune]struct{}
	// firstBits is firsts as a bitmap of the code points below firstLimit: one
	// shift and a mask on the path of every accented letter, where a map costs a hash.
	firstBits [firstLimit / 64]uint64

	shortOnce sync.Once
	shortList []string // "name\tsequence" lines, sorted by name

	textOnce sync.Once
	textSet  map[rune]struct{}
)

func loadSet() {
	setOnce.Do(func() {
		stems = strings.Split(strings.TrimSuffix(stemData, "\n"), "\n")
		byNorm = make(map[string]string, len(stems))
		firsts = map[rune]struct{}{}
		var buf [128]byte
		for _, stem := range stems {
			var key []byte = buf[:0]
			first := true
			for _, h := range strings.Split(stem, "-") {
				v, err := strconv.ParseUint(h, 16, 32)
				if err != nil || !utf8.ValidRune(rune(v)) {
					panic("emoji: bad stem in stems.txt: " + stem)
				}
				if first {
					firsts[rune(v)] = struct{}{}
					if v < firstLimit {
						firstBits[v/64] |= 1 << (v % 64)
					}
					first = false
				}
				if rune(v) != vs16 {
					key = utf8.AppendRune(key, rune(v))
				}
			}
			if _, dup := byNorm[string(key)]; !dup {
				byNorm[string(key)] = stem
			}
		}
	})
}

// Stems lists the stem of every graphic the set has, sorted: what a Provider
// may be asked for.
func Stems() []string {
	loadSet()
	return append([]string(nil), stems...)
}

// stemOf is the stem of the graphic for seq, ignoring U+FE0F, or "".
func stemOf(seq string) string {
	loadSet()
	var buf [128]byte
	key := buf[:0]
	for _, r := range seq {
		if r != vs16 {
			key = utf8.AppendRune(key, r)
		}
	}
	return byNorm[string(key)]
}

func shorts() []string {
	shortOnce.Do(func() {
		shortList = strings.Split(strings.TrimSuffix(shortcodeData, "\n"), "\n")
	})
	return shortList
}

// Shortcode returns the sequence of the :name: shortcode (name without the
// colons: "smile", "+1", "100"), and whether it is defined. The table is
// markdown-it-emoji's, whatever the Provider.
func Shortcode(name string) (string, bool) {
	l := shorts()
	lo, hi := 0, len(l)
	for lo < hi {
		m := int(uint(lo+hi) >> 1)
		n, _, _ := strings.Cut(l[m], "\t")
		if n < name {
			lo = m + 1
		} else {
			hi = m
		}
	}
	if lo < len(l) {
		if n, seq, _ := strings.Cut(l[lo], "\t"); n == name {
			return seq, true
		}
	}
	return "", false
}

// Shortcodes lists every shortcode name, sorted.
func Shortcodes() []string {
	l := shorts()
	out := make([]string, len(l))
	for i, line := range l {
		out[i], _, _ = strings.Cut(line, "\t")
	}
	return out
}

// Replace is the :shortcode: pass of markdown-it-emoji over text: each :name:
// that names a shortcode becomes f(name, sequence). Matches do not overlap;
// ":a:smile:" yields the smile, since ":a:" names nothing. Text without a colon
// is returned as is.
func Replace(s string, f func(name, seq string) string) string {
	if !strings.Contains(s, ":") {
		return s
	}
	var b strings.Builder
	last := 0
	for i := 0; i < len(s); {
		if s[i] != ':' {
			i++
			continue
		}
		j := strings.IndexByte(s[i+1:], ':')
		if j < 0 {
			break
		}
		j += i + 1
		if seq, ok := Shortcode(s[i+1 : j]); ok {
			b.WriteString(s[last:i])
			b.WriteString(f(s[i+1:j], seq))
			last, i = j+1, j+1
			continue
		}
		i++
	}
	if last == 0 {
		return s
	}
	b.WriteString(s[last:])
	return b.String()
}

// TextDefault reports whether r is an emoji whose default presentation is text
// (© ™ ↔ ☺ ✔ ▶ and about 200 more, from Unicode's emoji-sequences.txt).
func TextDefault(r rune) bool {
	textOnce.Do(func() {
		textSet = map[rune]struct{}{}
		for _, line := range strings.Fields(textDefaultData) {
			if v, err := strconv.ParseUint(line, 16, 32); err == nil {
				textSet[rune(v)] = struct{}{}
			}
		}
	})
	_, ok := textSet[r]
	return ok
}

const (
	vs15  = 0xFE0E // text presentation selector
	vs16  = 0xFE0F // emoji presentation selector
	heart = 0x2764 // the text-default character emoji-regex takes bare
	// maxRunes bounds the longest sequence looked at from one position: the
	// longest in Twemoji (a family with skin tones) has 11 code points.
	maxRunes = 16
	// firstLimit: the first code points of the stems are all below it (they end
	// at U+1FAFF); one at or above it is looked up in the map.
	firstLimit = 0x20000
)

// Cut returns the byte length of the emoji at the start of s, or 0 when s does
// not start with one. It reads text the way Marp Core does (the emoji-regex it
// matches with, then Twemoji's parser):
//
//   - the match is the longest sequence the set has a graphic for, with or
//     without U+FE0F inside it;
//   - a character whose default presentation is text (see TextDefault) is an
//     emoji only when U+FE0F follows it, except U+2764, which is one bare;
//   - a U+FE0F after a character that is already an emoji by default is not
//     part of the match; after a text-default one it is;
//   - a match followed by U+FE0E (text presentation selector) is not one;
//   - a lone regional indicator is not a flag, a bare digit, # or * is not a
//     keycap (they are with U+20E3), and nothing in the private use area is an
//     emoji.
//
// It reads the tables of this package and never calls a Provider, so it is the
// same with no Provider, one without data, and the embedded one.
func Cut(s string) int {
	if s == "" {
		return 0
	}
	first, _ := utf8.DecodeRuneInString(s)
	if first < 0x80 {
		// Only keycaps start in ASCII: # * 0-9, then FE0F? 20E3.
		if !(first == '#' || first == '*' || (first >= '0' && first <= '9')) || len(s) < 4 {
			return 0
		}
	}
	loadSet()
	if first < firstLimit {
		if firstBits[first/64]&(1<<(first%64)) == 0 {
			return 0
		}
	} else if _, ok := firsts[first]; !ok {
		return 0
	}
	var (
		rs   [maxRunes + 1]rune
		ends [maxRunes + 1]int // ends[i]: byte offset just after rs[i]
		n    int
	)
	for off := 0; off < len(s) && n < len(rs); {
		r, w := utf8.DecodeRuneInString(s[off:])
		if n > 0 && r < 0xA9 || r == utf8.RuneError {
			break
		}
		off += w
		rs[n], ends[n] = r, off
		n++
	}
	var kb [(maxRunes + 1) * utf8.UTFMax]byte
	for k := min(n, maxRunes); k > 0; k-- {
		key := kb[:0]
		for _, r := range rs[:k] {
			if r != vs16 {
				key = utf8.AppendRune(key, r)
			}
		}
		if _, ok := byNorm[string(key)]; !ok {
			continue
		}
		if (rs[0] >= 0x1F1E6 && rs[0] <= 0x1F1FF || rs[0] >= 0xE000 && rs[0] <= 0xF8FF) && onlyVS16(rs[1:k]) {
			continue // one regional indicator is no flag; private use is no emoji
		}
		if TextDefault(rs[0]) && rs[0] != heart && (k < 2 || rs[1] != vs16) {
			continue
		}
		end := ends[k-1]
		if k >= 2 && rs[k-1] == vs16 && !TextDefault(rs[k-2]) {
			end = ends[k-2]
		}
		if end < len(s) {
			if r, _ := utf8.DecodeRuneInString(s[end:]); r == vs15 {
				continue
			}
		}
		return end
	}
	return 0
}

func onlyVS16(rs []rune) bool {
	for _, r := range rs {
		if r != vs16 {
			return false
		}
	}
	return true
}
