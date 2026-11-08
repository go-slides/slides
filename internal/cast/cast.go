// Package cast reads asciinema recordings (asciicast v2 and v3) and replays
// them on a terminal emulator (package vt): the screen at a given time, or the
// frames a player shows.
//
// A recording is untrusted input, so every dimension of it is bounded (Limits)
// and a file past a bound is refused with an *Error whose Code is "cast.limit";
// a file that is not an asciicast, "cast.syntax"; one of a version this does
// not read, "cast.version".
//
// Formats: asciicast v2 (docs.asciinema.org/manual/asciicast/v2): a JSON header
// line, then [time, code, data] events with absolute times; v3
// (docs.asciinema.org/manual/asciicast/v3): the header holds term{cols, rows,
// theme}, the events' times are intervals from the previous event, and a line
// that starts with # is a comment. Event codes o (output) and r (resize) are
// replayed; i, m, x and any code a later version adds are skipped, as both
// specifications say a reader should.
package cast

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/go-slides/slides/internal/vt"
)

// Codes of an *Error.
const (
	CodeSyntax  = "cast.syntax"
	CodeLimit   = "cast.limit"
	CodeVersion = "cast.version"
)

// Error is a refused recording; Code is one of the codes above.
type Error struct {
	Code string
	Msg  string
}

func (e *Error) Error() string { return e.Msg }

func syntax(format string, a ...any) error {
	return &Error{CodeSyntax, "asciicast: " + fmt.Sprintf(format, a...)}
}

func limit(format string, a ...any) error {
	return &Error{CodeLimit, "asciicast: " + fmt.Sprintf(format, a...)}
}

// Limits bound a recording and its replay.
type Limits struct {
	// MaxBytes is the size of the file.
	MaxBytes int
	// MaxLine is the length of one line (the header or an event).
	MaxLine int
	// MaxEvents is the number of o and r events.
	MaxEvents int
	// MaxDuration is the length in seconds, after the idle time limit.
	MaxDuration float64
	// MaxCols and MaxRows bound the terminal, in the header and in resizes.
	MaxCols, MaxRows int
	// MaxWork bounds the cells the emulator touches (vt.Term.Work).
	MaxWork uint64
	// MaxFrameBytes bounds the text of the frames a player gets.
	MaxFrameBytes int
}

// DefaultLimits are generous for a demo and small enough to refuse a bomb in
// well under a second and a few tens of megabytes.
var DefaultLimits = Limits{
	MaxBytes:      16 << 20,
	MaxLine:       1 << 20,
	MaxEvents:     500_000,
	MaxDuration:   3 * 3600,
	MaxCols:       400,
	MaxRows:       200,
	MaxWork:       1_000_000_000,
	MaxFrameBytes: 6 << 20,
}

func (l Limits) norm() Limits {
	d := DefaultLimits
	if l.MaxBytes <= 0 {
		l.MaxBytes = d.MaxBytes
	}
	if l.MaxLine <= 0 {
		l.MaxLine = d.MaxLine
	}
	if l.MaxEvents <= 0 {
		l.MaxEvents = d.MaxEvents
	}
	if l.MaxDuration <= 0 {
		l.MaxDuration = d.MaxDuration
	}
	if l.MaxCols <= 0 || l.MaxCols > vt.MaxCols {
		l.MaxCols = min(d.MaxCols, vt.MaxCols)
	}
	if l.MaxRows <= 0 || l.MaxRows > vt.MaxRows {
		l.MaxRows = min(d.MaxRows, vt.MaxRows)
	}
	if l.MaxWork == 0 {
		l.MaxWork = d.MaxWork
	}
	if l.MaxFrameBytes <= 0 {
		l.MaxFrameBytes = d.MaxFrameBytes
	}
	return l
}

// RGB is a colour of a theme.
type RGB struct{ R, G, B uint8 }

// Hex is "#rrggbb".
func (c RGB) Hex() string {
	const h = "0123456789abcdef"
	return string([]byte{'#', h[c.R>>4], h[c.R&15], h[c.G>>4], h[c.G&15], h[c.B>>4], h[c.B&15]})
}

// Theme is the recording's colours.
type Theme struct {
	FG, BG  RGB
	Palette [16]RGB
}

// DefaultTheme is what a recording without a theme is shown in.
var DefaultTheme = Theme{
	FG: RGB{0xd4, 0xd4, 0xd4}, BG: RGB{0x1e, 0x1e, 0x1e},
	Palette: [16]RGB{
		{0, 0, 0}, {0xcd, 0, 0}, {0, 0xcd, 0}, {0xcd, 0xcd, 0}, {0x3b, 0x78, 0xff}, {0xcd, 0, 0xcd}, {0, 0xcd, 0xcd}, {0xe5, 0xe5, 0xe5},
		{0x7f, 0x7f, 0x7f}, {0xff, 0x55, 0x55}, {0x55, 0xff, 0x55}, {0xff, 0xff, 0x55}, {0x6c, 0x9e, 0xff}, {0xff, 0x55, 0xff}, {0x55, 0xff, 0xff}, {0xff, 0xff, 0xff},
	},
}

// Event is an output (o) or a resize (r).
type Event struct {
	// T is the time in seconds from the start, after the idle time limit.
	T    float64
	Code byte
	// Data is the output, or "COLSxROWS".
	Data string
}

// Cast is a parsed recording.
type Cast struct {
	Version    int
	Cols, Rows int
	Title      string
	Theme      Theme
	Events     []Event
	// Duration is the time of the last event.
	Duration float64
}

type header struct {
	Version       int      `json:"version"`
	Width         int      `json:"width"`
	Height        int      `json:"height"`
	Title         string   `json:"title"`
	IdleTimeLimit float64  `json:"idle_time_limit"`
	Theme         *jtheme  `json:"theme"`
	Term          *jterm   `json:"term"`
}

type jterm struct {
	Cols  int     `json:"cols"`
	Rows  int     `json:"rows"`
	Theme *jtheme `json:"theme"`
}

type jtheme struct {
	FG      string `json:"fg"`
	BG      string `json:"bg"`
	Palette string `json:"palette"`
}

// Parse reads a recording.
func Parse(data []byte, lim Limits) (*Cast, error) {
	lim = lim.norm()
	if len(data) > lim.MaxBytes {
		return nil, limit("the file is %d bytes; at most %d are read", len(data), lim.MaxBytes)
	}
	pos := 0
	next := func() ([]byte, bool, error) {
		for pos < len(data) {
			end := bytes.IndexByte(data[pos:], '\n')
			var line []byte
			if end < 0 {
				line, pos = data[pos:], len(data)
			} else {
				line, pos = data[pos:pos+end], pos+end+1
			}
			if len(line) > lim.MaxLine {
				return nil, false, limit("a line is %d bytes long; at most %d are read", len(line), lim.MaxLine)
			}
			line = bytes.TrimSpace(line)
			if len(line) > 0 {
				return line, true, nil
			}
		}
		return nil, false, nil
	}
	first, ok, err := next()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, syntax("the file is empty")
	}
	var h header
	if first[0] != '{' {
		return nil, syntax("the first line is not a JSON object")
	}
	if err := json.Unmarshal(first, &h); err != nil {
		return nil, syntax("the header is not valid: %v", err)
	}
	c := &Cast{Version: h.Version, Theme: DefaultTheme, Title: h.Title}
	var th *jtheme
	switch h.Version {
	case 2:
		c.Cols, c.Rows, th = h.Width, h.Height, h.Theme
	case 3:
		if h.Term == nil {
			return nil, syntax("the header has no term")
		}
		c.Cols, c.Rows, th = h.Term.Cols, h.Term.Rows, h.Term.Theme
	case 1:
		return nil, &Error{CodeVersion, "asciicast: version 1 is not read; record again with asciinema 2 or later"}
	default:
		return nil, &Error{CodeVersion, fmt.Sprintf("asciicast: version %d is not read (2 and 3 are)", h.Version)}
	}
	if c.Cols < 1 || c.Rows < 1 {
		return nil, syntax("the terminal size %dx%d is not valid", c.Cols, c.Rows)
	}
	if c.Cols > lim.MaxCols || c.Rows > lim.MaxRows {
		return nil, limit("the terminal is %dx%d; at most %dx%d is read", c.Cols, c.Rows, lim.MaxCols, lim.MaxRows)
	}
	if math.IsNaN(h.IdleTimeLimit) || h.IdleTimeLimit < 0 {
		return nil, syntax("idle_time_limit is not valid")
	}
	if th != nil {
		if c.Theme, err = th.parse(); err != nil {
			return nil, err
		}
	}
	if len(c.Title) > 1024 {
		c.Title = c.Title[:1024]
	}
	c.Title = strings.ToValidUTF8(c.Title, "�")

	var prev, eff float64 // the previous event's time as written, and as played
	for {
		line, ok, err := next()
		if err != nil {
			return nil, err
		}
		if !ok {
			break
		}
		if line[0] == '#' && h.Version == 3 {
			continue
		}
		t, code, d, keep, err := parseEvent(line, lim.MaxLine)
		if err != nil {
			return nil, err
		}
		if !keep {
			continue
		}
		var gap float64
		if h.Version == 3 {
			if t < 0 {
				return nil, syntax("an interval is negative")
			}
			gap = t
		} else {
			gap = max(t-prev, 0)
			prev = max(prev, t)
		}
		if h.IdleTimeLimit > 0 {
			gap = min(gap, h.IdleTimeLimit)
		}
		eff += gap
		if eff > lim.MaxDuration {
			return nil, limit("the recording lasts more than %.0f s", lim.MaxDuration)
		}
		if len(c.Events) >= lim.MaxEvents {
			return nil, limit("the recording has more than %d events", lim.MaxEvents)
		}
		if code == 'r' {
			if _, _, err := parseSize(d, lim); err != nil {
				return nil, err
			}
		}
		c.Events = append(c.Events, Event{T: eff, Code: code, Data: d})
	}
	c.Duration = eff
	return c, nil
}

func parseSize(s string, lim Limits) (cols, rows int, err error) {
	a, b, ok := strings.Cut(s, "x")
	cols, e1 := strconv.Atoi(a)
	rows, e2 := strconv.Atoi(b)
	if !ok || e1 != nil || e2 != nil || cols < 1 || rows < 1 {
		return 0, 0, syntax("a resize event %q is not COLSxROWS", truncate(s))
	}
	if cols > lim.MaxCols || rows > lim.MaxRows {
		return 0, 0, limit("a resize to %dx%d; at most %dx%d is read", cols, rows, lim.MaxCols, lim.MaxRows)
	}
	return cols, rows, nil
}

func truncate(s string) string {
	if len(s) > 32 {
		return s[:32] + "…"
	}
	return s
}

func (t *jtheme) parse() (Theme, error) {
	var th Theme
	var err error
	if th.FG, err = hexColor(t.FG); err != nil {
		return th, err
	}
	if th.BG, err = hexColor(t.BG); err != nil {
		return th, err
	}
	parts := strings.Split(t.Palette, ":")
	if len(parts) != 8 && len(parts) != 16 {
		return th, syntax("the theme's palette has %d colours, not 8 or 16", len(parts))
	}
	for i := range th.Palette {
		// 8 colours stand for the second eight too
		if th.Palette[i], err = hexColor(parts[i%len(parts)]); err != nil {
			return th, err
		}
	}
	return th, nil
}

func hexColor(s string) (RGB, error) {
	if len(s) != 7 || s[0] != '#' {
		return RGB{}, syntax("the theme colour %q is not #rrggbb", truncate(s))
	}
	v, err := strconv.ParseUint(s[1:], 16, 24)
	if err != nil {
		return RGB{}, syntax("the theme colour %q is not #rrggbb", truncate(s))
	}
	return RGB{uint8(v >> 16), uint8(v >> 8), uint8(v)}, nil
}

// parseEvent reads [time, "code", "data"]. keep is false for an event that is
// not replayed.
func parseEvent(line []byte, maxLine int) (t float64, code byte, data string, keep bool, err error) {
	bad := func(what string) (float64, byte, string, bool, error) {
		return 0, 0, "", false, syntax("an event is not [time, code, data]: %s", what)
	}
	i := 0
	ws := func() {
		for i < len(line) && (line[i] == ' ' || line[i] == '\t' || line[i] == '\r') {
			i++
		}
	}
	comma := func() bool {
		ws()
		if i < len(line) && line[i] == ',' {
			i++
			ws()
			return true
		}
		return false
	}
	ws()
	if i >= len(line) || line[i] != '[' {
		return bad("no [")
	}
	i++
	ws()
	st := i
	for i < len(line) && (line[i] >= '0' && line[i] <= '9' || line[i] == '-' || line[i] == '+' || line[i] == '.' || line[i] == 'e' || line[i] == 'E') {
		i++
	}
	t, perr := strconv.ParseFloat(string(line[st:i]), 64)
	if perr != nil || math.IsInf(t, 0) || math.IsNaN(t) {
		return bad("the time")
	}
	if !comma() {
		return bad("no comma after the time")
	}
	cs, n, ok := jsonString(line[i:])
	if !ok || len(cs) != 1 {
		if ok {
			// a code of several characters is a code of a later version
			code = 0
		} else {
			return bad("the code")
		}
	} else {
		code = cs[0]
	}
	i += n
	if !comma() {
		return bad("no comma after the code")
	}
	switch code {
	case 'o', 'r':
		d, n, ok := jsonString(line[i:])
		if !ok {
			return bad("the data is not a string")
		}
		i += n
		ws()
		if i >= len(line) || line[i] != ']' {
			return bad("no ]")
		}
		i++
		ws()
		if i != len(line) {
			return bad("text after ]")
		}
		return t, code, d, true, nil
	}
	// i, m, x or an unknown code: the line must still be JSON
	if !json.Valid(line) {
		return bad("not JSON")
	}
	return t, code, "", false, nil
}

// jsonString reads the JSON string at the start of b: its value and the bytes
// it took. Invalid UTF-8 becomes U+FFFD; a raw control character is refused.
func jsonString(b []byte) (string, int, bool) {
	if len(b) == 0 || b[0] != '"' {
		return "", 0, false
	}
	i := 1
	esc := false
	for i < len(b) {
		c := b[i]
		if c == '"' {
			break
		}
		if c < 0x20 {
			return "", 0, false
		}
		if c == '\\' {
			esc = true
			i++
		}
		i++
	}
	if i >= len(b) {
		return "", 0, false
	}
	raw := b[1:i]
	if !esc {
		if utf8.Valid(raw) {
			return string(raw), i + 1, true
		}
		return strings.ToValidUTF8(string(raw), "�"), i + 1, true
	}
	var out []byte
	for j := 0; j < len(raw); {
		c := raw[j]
		if c != '\\' {
			out = append(out, c)
			j++
			continue
		}
		j++
		if j >= len(raw) {
			return "", 0, false
		}
		switch raw[j] {
		case '"', '\\', '/':
			out = append(out, raw[j])
		case 'b':
			out = append(out, '\b')
		case 'f':
			out = append(out, '\f')
		case 'n':
			out = append(out, '\n')
		case 'r':
			out = append(out, '\r')
		case 't':
			out = append(out, '\t')
		case 'u':
			r, ok := hex4(raw, j+1)
			if !ok {
				return "", 0, false
			}
			j += 4
			if r >= 0xd800 && r < 0xdc00 { // a surrogate pair
				if j+6 < len(raw)+0 && raw[j+1] == '\\' && raw[j+2] == 'u' {
					if r2, ok := hex4(raw, j+3); ok && r2 >= 0xdc00 && r2 < 0xe000 {
						r = 0x10000 + (r-0xd800)<<10 + (r2 - 0xdc00)
						j += 6
					} else {
						r = utf8.RuneError
					}
				} else {
					r = utf8.RuneError
				}
			} else if r >= 0xdc00 && r < 0xe000 {
				r = utf8.RuneError
			}
			out = utf8.AppendRune(out, r)
		default:
			return "", 0, false
		}
		j++
	}
	if !utf8.Valid(out) {
		return strings.ToValidUTF8(string(out), "�"), i + 1, true
	}
	return string(out), i + 1, true
}

func hex4(b []byte, at int) (rune, bool) {
	if at+4 > len(b) {
		return 0, false
	}
	var r rune
	for _, c := range b[at : at+4] {
		switch {
		case c >= '0' && c <= '9':
			r = r<<4 | rune(c-'0')
		case c >= 'a' && c <= 'f':
			r = r<<4 | rune(c-'a'+10)
		case c >= 'A' && c <= 'F':
			r = r<<4 | rune(c-'A'+10)
		default:
			return 0, false
		}
	}
	return r, true
}
