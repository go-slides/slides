package cast

import (
	"html"
	"strconv"
	"strings"

	"github.com/go-slides/slides/internal/vt"
)

// Style bits of a Style.
const (
	StyleBold = 1 << iota
	StyleItalic
	StyleUnderline
	StyleStrike
)

// Style is what a run looks like, with its colours resolved to #rrggbb:
// inverse, faint and hidden are already applied, and BG is empty where the
// background is the theme's own.
type Style struct {
	FG, BG string
	Bits   uint8
}

// Run is text in one style (an index into Styles).
type Run struct {
	Text  string
	Style int
}

// Row is a row of runs.
type Row struct {
	Y    int
	Runs []Run
}

// Frame is what changes at time T: the rows that did, and the new size when
// the terminal was resized (W is 0 otherwise).
type Frame struct {
	T    float64
	W, H int
	Rows []Row
}

// Recording is a cast replayed as the frames a player draws.
type Recording struct {
	Cols, Rows int
	Duration   float64
	Theme      Theme
	Styles     []Style
	Frames     []Frame
}

// Screen is the terminal at one moment.
type Screen struct {
	Cols, Rows int
	Theme      Theme
	Styles     []Style
	// Lines has Rows entries; a blank line has no runs.
	Lines [][]Run
}

// styler resolves the emulator's colours and attributes to styles.
type styler struct {
	th   Theme
	ids  map[Style]int
	list []Style
}

func newStyler(th Theme) *styler { return &styler{th: th, ids: map[Style]int{}} }

func (s *styler) color(c vt.Color, def RGB) RGB {
	if i, ok := c.Index(); ok {
		switch {
		case i < 16:
			return s.th.Palette[i]
		case i < 232:
			i -= 16
			lv := [6]uint8{0, 95, 135, 175, 215, 255}
			return RGB{lv[i/36], lv[i/6%6], lv[i%6]}
		default:
			g := uint8(8 + 10*(i-232))
			return RGB{g, g, g}
		}
	}
	if r, g, b, ok := c.Direct(); ok {
		return RGB{r, g, b}
	}
	return def
}

func mix(a, b RGB) RGB {
	return RGB{uint8((int(a.R) + int(b.R)) / 2), uint8((int(a.G) + int(b.G)) / 2), uint8((int(a.B) + int(b.B)) / 2)}
}

func (s *styler) id(r vt.Run) int {
	fg, bg := s.color(r.FG, s.th.FG), s.color(r.BG, s.th.BG)
	if r.A&vt.Inverse != 0 {
		fg, bg = bg, fg
	}
	if r.A&vt.Faint != 0 {
		fg = mix(fg, bg)
	}
	if r.A&vt.Hidden != 0 {
		fg = bg
	}
	st := Style{FG: fg.Hex()}
	if bg != s.th.BG {
		st.BG = bg.Hex()
	}
	if r.A&vt.Bold != 0 {
		st.Bits |= StyleBold
	}
	if r.A&vt.Italic != 0 {
		st.Bits |= StyleItalic
	}
	if r.A&vt.Underline != 0 {
		st.Bits |= StyleUnderline
	}
	if r.A&vt.Strike != 0 {
		st.Bits |= StyleStrike
	}
	if i, ok := s.ids[st]; ok {
		return i
	}
	s.ids[st] = len(s.list)
	s.list = append(s.list, st)
	return len(s.list) - 1
}

func (s *styler) row(t *vt.Term, y int) []Run {
	var out []Run
	for _, r := range t.Runs(y) {
		id := s.id(r)
		if n := len(out); n > 0 && out[n-1].Style == id {
			out[n-1].Text += r.Text
		} else {
			out = append(out, Run{r.Text, id})
		}
	}
	return out
}

// play feeds the events with T <= until to a terminal, calling after with each
// event's time once it is applied (the terminal is then at that time).
func (c *Cast) play(lim Limits, until float64, after func(t float64, e Event, resized bool, term *vt.Term)) (*vt.Term, error) {
	term := vt.New(c.Cols, c.Rows)
	if term == nil {
		return nil, limit("the terminal is %dx%d", c.Cols, c.Rows)
	}
	for _, e := range c.Events {
		if e.T > until {
			break
		}
		resized := false
		switch e.Code {
		case 'o':
			term.WriteString(e.Data)
		case 'r':
			cols, rows, err := parseSize(e.Data, lim)
			if err != nil {
				return nil, err
			}
			resized = term.Resize(cols, rows)
		default:
			continue
		}
		if term.Work() > lim.MaxWork {
			return nil, limit("the recording makes the terminal do more than %d cell updates", lim.MaxWork)
		}
		if after != nil {
			after(e.T, e, resized, term)
		}
	}
	return term, nil
}

// ScreenAt is the terminal after the events up to time t seconds; a negative t
// is the end of the recording.
func (c *Cast) ScreenAt(t float64, lim Limits) (*Screen, error) {
	lim = lim.norm()
	if t < 0 {
		t = c.Duration
	}
	term, err := c.play(lim, t, nil)
	if err != nil {
		return nil, err
	}
	st := newStyler(c.Theme)
	s := &Screen{Cols: term.Cols(), Rows: term.Rows(), Theme: c.Theme}
	s.Lines = make([][]Run, s.Rows)
	for y := range s.Lines {
		s.Lines[y] = st.row(term, y)
	}
	s.Styles = st.list
	return s, nil
}

// Replay plays the recording as frames: a frame for each stretch of at least
// minInterval seconds in which the screen changed, and one at the end. When the
// frames' text would pass Limits.MaxFrameBytes the interval is doubled (at most
// six times) before the recording is refused.
func (c *Cast) Replay(minInterval float64, lim Limits) (*Recording, error) {
	lim = lim.norm()
	minInterval = max(minInterval, c.Duration/20000, 1.0/60)
	var lastErr error
	for try := 0; try < 7; try++ {
		rec, err := c.replay(minInterval, lim)
		if err == nil {
			return rec, nil
		}
		if e, ok := err.(*Error); !ok || e.Code != CodeLimit || e.Msg != errFrames.Msg {
			return nil, err
		}
		lastErr = err
		minInterval *= 2
	}
	return nil, lastErr
}

var errFrames = &Error{CodeLimit, "asciicast: the frames are larger than allowed"}

func (c *Cast) replay(minInterval float64, lim Limits) (*Recording, error) {
	st := newStyler(c.Theme)
	rec := &Recording{Cols: c.Cols, Rows: c.Rows, Duration: c.Duration, Theme: c.Theme}
	size := 0
	pending, resized := false, false
	lastEmit := -1.0
	emit := func(t float64, term *vt.Term) {
		f := Frame{T: t}
		if resized {
			f.W, f.H = term.Cols(), term.Rows()
		}
		for _, y := range term.TakeDirty() {
			runs := st.row(term, y)
			f.Rows = append(f.Rows, Row{y, runs})
			size += 16
			for _, r := range runs {
				size += len(r.Text) + 8
			}
		}
		if len(f.Rows) > 0 || f.W != 0 {
			rec.Frames = append(rec.Frames, f)
		}
		pending, resized, lastEmit = false, false, t
	}
	var last float64
	term, err := c.play(lim, c.Duration, func(t float64, e Event, rs bool, term *vt.Term) {
		pending = true
		resized = resized || rs
		last = t
		if lastEmit < 0 || t-lastEmit >= minInterval {
			emit(t, term)
		}
	})
	if err != nil {
		return nil, err
	}
	if pending {
		emit(last, term)
	}
	if size > lim.MaxFrameBytes {
		return nil, errFrames
	}
	rec.Styles = st.list
	return rec, nil
}

// HTML is the screen as the markup of a <pre>'s content: a span for each run
// that is not in the default style, the text escaped. A line is ended by a
// newline.
func (s *Screen) HTML() string {
	var b strings.Builder
	for y, line := range s.Lines {
		if y > 0 {
			b.WriteByte('\n')
		}
		for _, r := range line {
			st := s.Styles[r.Style]
			b.WriteString(`<span style="` + st.css() + `">`)
			b.WriteString(html.EscapeString(r.Text))
			b.WriteString("</span>")
		}
	}
	return b.String()
}

// css is the style as a declaration: the colours are #rrggbb, so nothing in it
// is text of the recording.
func (s Style) css() string {
	d := "color:" + s.FG
	if s.BG != "" {
		d += ";background-color:" + s.BG
	}
	if s.Bits&StyleBold != 0 {
		d += ";font-weight:bold"
	}
	if s.Bits&StyleItalic != 0 {
		d += ";font-style:italic"
	}
	switch {
	case s.Bits&StyleUnderline != 0 && s.Bits&StyleStrike != 0:
		d += ";text-decoration:underline line-through"
	case s.Bits&StyleUnderline != 0:
		d += ";text-decoration:underline"
	case s.Bits&StyleStrike != 0:
		d += ";text-decoration:line-through"
	}
	return d
}

// writeJSONString writes s as a JSON string that is safe inside a <script>: <,
// >, & and the line separators are escaped.
func writeJSONString(b *strings.Builder, s string) {
	const hex = "0123456789abcdef"
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == 0x2028:
			b.WriteString(` `)
		case r == 0x2029:
			b.WriteString(` `)
		case r < 0x20 || r == '<' || r == '>' || r == '&' || r == 0x7f:
			b.WriteString(`\u00`)
			b.WriteByte(hex[r>>4&15])
			b.WriteByte(hex[r&15])
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
}

// JSON is the recording for the player: {"w","h","d","bg","fg","s","f"}. The
// styles "s" are [fg, bg, bits]; each frame in "f" is [t, y, runs, y, runs...]
// with a run [text, style], and a resize is the pair -1, [w, h]. Everything in
// it is data: the player builds text nodes and sets style properties, and no
// part of it is markup.
func (r *Recording) JSON() string {
	var b strings.Builder
	num := func(f float64) { b.WriteString(strconv.FormatFloat(f, 'f', -1, 64)) }
	b.WriteString(`{"w":` + strconv.Itoa(r.Cols) + `,"h":` + strconv.Itoa(r.Rows) + `,"d":`)
	num(round3(r.Duration))
	b.WriteString(`,"fg":"` + r.Theme.FG.Hex() + `","bg":"` + r.Theme.BG.Hex() + `","s":[`)
	for i, s := range r.Styles {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`["` + s.FG + `","` + s.BG + `",` + strconv.Itoa(int(s.Bits)) + `]`)
	}
	b.WriteString(`],"f":[`)
	for i, f := range r.Frames {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('[')
		num(round3(f.T))
		if f.W != 0 {
			b.WriteString(`,-1,[` + strconv.Itoa(f.W) + `,` + strconv.Itoa(f.H) + `]`)
		}
		for _, row := range f.Rows {
			b.WriteString(`,` + strconv.Itoa(row.Y) + `,[`)
			for j, run := range row.Runs {
				if j > 0 {
					b.WriteByte(',')
				}
				b.WriteByte('[')
				writeJSONString(&b, run.Text)
				b.WriteString(`,` + strconv.Itoa(run.Style) + `]`)
			}
			b.WriteByte(']')
		}
		b.WriteByte(']')
	}
	b.WriteString(`]}`)
	return b.String()
}

func round3(f float64) float64 {
	return float64(int64(f*1000+0.5)) / 1000
}
