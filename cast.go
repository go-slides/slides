package slides

import (
	"math"
	"net/url"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/go-slides/slides/deck"
	"github.com/go-slides/slides/diag"
	"github.com/go-slides/slides/internal/cast"
	"github.com/go-slides/slides/vfs"
)

// castRenderer writes the images that name an asciinema recording
// (![cast](demo.cast#t=12.5)).
//
// Every output gets the screen at the poster time (#t=, else the end of the
// recording) as text: a <code> block of spans in the colours of the recording,
// in the theme's monospace font, which the PDF, PPTX and ODP exports draw as
// the vector text it is. The HTML page also gets the recording's frames, as
// data in a <script type="application/json">, which the player in package
// viewer replays; that is only when the slides are framed for a browser
// (deck.FrameSVG), so the exports do not carry it.
type castRenderer struct {
	fs     vfs.FS
	base   string
	player bool
	w, h   int
	lim    cast.Limits

	mu       sync.Mutex
	warnings []string
	parsed   map[string]*cast.Cast
}

func newCastRenderer(o deck.Options, player bool, w, h int) *castRenderer {
	return &castRenderer{fs: vfs.Or(o.FS), base: o.BaseDir, player: player, w: w, h: h, lim: cast.DefaultLimits, parsed: map[string]*cast.Cast{}}
}

func (c *castRenderer) fail(code, path, format string, a ...any) error {
	err := diag.Errorf(code, "%s: "+format, append([]any{path}, a...)...)
	w := code + ": " + err.Error()
	c.mu.Lock()
	if !slices.Contains(c.warnings, w) {
		c.warnings = append(c.warnings, w)
	}
	c.mu.Unlock()
	return err
}

// load reads and parses a recording once per build.
func (c *castRenderer) load(rel string) (*cast.Cast, error) {
	if cc, ok := c.parsed[rel]; ok {
		return cc, nil
	}
	if !filepath.IsLocal(rel) {
		return nil, c.fail("cast.path", rel, "a recording must be a file in the deck's folder, not a URL, an absolute path or one that leaves it")
	}
	full := filepath.Join(c.base, rel)
	if fi, err := c.fs.Stat(full); err == nil && fi.Size() > int64(c.lim.MaxBytes) {
		return nil, c.fail("cast.limit", rel, "the file is %d bytes; at most %d are read", fi.Size(), c.lim.MaxBytes)
	}
	data, err := c.fs.ReadFile(full)
	if err != nil {
		return nil, c.fail("file.missing", rel, "the recording cannot be read")
	}
	cc, err := cast.Parse(data, c.lim)
	if err != nil {
		return nil, c.failCast(rel, err)
	}
	c.parsed[rel] = cc
	return cc, nil
}

func (c *castRenderer) failCast(rel string, err error) error {
	code := "cast.syntax"
	if e, ok := err.(*cast.Error); ok {
		code = e.Code
	}
	return c.fail(code, rel, "%s", err.Error())
}

func (c *castRenderer) render(dest, alt string) (string, error) {
	p, frag, _ := strings.Cut(dest, "#")
	p, _, _ = strings.Cut(p, "?")
	if u, err := url.PathUnescape(p); err == nil {
		p = u
	}
	if strings.Contains(p, "://") || strings.HasPrefix(p, "//") || strings.HasPrefix(p, "data:") {
		return "", c.fail("cast.path", p, "a recording must be a file in the deck's folder, not a URL, an absolute path or one that leaves it")
	}
	poster := -1.0
	if frag != "" {
		if q, err := url.ParseQuery(frag); err == nil && q.Has("t") {
			t, ok := parseMoment(q.Get("t"))
			if !ok {
				return "", c.fail("cast.syntax", p, "#t=%s is not a number of seconds (12.5) or minutes and seconds (1:23)", truncateText(q.Get("t")))
			}
			poster = t
		}
	}
	cc, err := c.load(p)
	if err != nil {
		return "", err
	}
	scr, err := cc.ScreenAt(poster, c.lim)
	if err != nil {
		return "", c.failCast(p, err)
	}
	var rec *cast.Recording
	if c.player {
		if rec, err = cc.Replay(0, c.lim); err != nil {
			return "", c.failCast(p, err)
		}
	}
	return castMarkup(cc, scr, rec, poster, alt, c.w, c.h), nil
}

func truncateText(s string) string {
	if len(s) > 24 {
		return s[:24] + "…"
	}
	return s
}

// parseMoment reads 12.5 or 1:23.5 (seconds, or minutes and seconds).
func parseMoment(s string) (float64, bool) {
	m, sec, colon := strings.Cut(s, ":")
	var total float64
	if colon {
		mi, err := strconv.ParseUint(m, 10, 16)
		if err != nil {
			return 0, false
		}
		total = float64(mi) * 60
		s = sec
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
		return 0, false
	}
	return total + v, true
}

// castMarkup is the image's markup. The screen's text is the recording's, and
// only ever as escaped text inside a span; its colours are #rrggbb values the
// emulator resolved. Inline elements only (the image sits in a paragraph).
func castMarkup(c *cast.Cast, s *cast.Screen, rec *cast.Recording, poster float64, alt string, w, h int) string {
	cols, rows := max(s.Cols, c.Cols), max(s.Rows, c.Rows)
	if rec != nil {
		for _, f := range rec.Frames {
			cols, rows = max(cols, f.W), max(rows, f.H)
		}
	}
	// the glyphs are about 0.6 em wide and the lines 1.2 em high; the screen
	// takes at most 90% of the slide's width and 62% of its height
	fs := math.Min(0.9*float64(w)/(0.6*float64(cols)), 0.62*float64(h)/(1.2*float64(rows)))
	fs = math.Max(math.Floor(fs), 6)
	fs = math.Min(fs, 40)
	label := alt
	if label == "" || strings.EqualFold(label, "cast") {
		label = c.Title
	}
	if label == "" {
		label = "Terminal recording"
	}
	fg, bg := c.Theme.FG.Hex(), c.Theme.BG.Hex()
	var b strings.Builder
	b.WriteString(`<span class="slides-cast" role="group" aria-label="` + escapeAttr(label) + `" data-poster="` + strconv.FormatFloat(poster, 'f', -1, 64) + `"`)
	b.WriteString(` style="display:block;background-color:` + bg + `;color:` + fg + `;border-radius:8px;padding:` + strconv.Itoa(int(fs)/2) + `px;overflow:hidden">`)
	b.WriteString(`<code class="slides-cast-screen" style="display:block;white-space:pre;margin:0;padding:0;border:0;border-radius:0;background:none;color:` + fg +
		`;font-size:` + strconv.Itoa(int(fs)) + `px;line-height:1.2;font-variant-ligatures:none">`)
	b.WriteString(s.HTML())
	b.WriteString(`</code>`)
	if rec != nil {
		// </ cannot occur in it (< is escaped), so the data cannot end its element
		b.WriteString(`<script type="application/json" class="slides-cast-data">` + rec.JSON() + `</script>`)
	}
	b.WriteString(`</span>`)
	return b.String()
}

func escapeAttr(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#39;").Replace(s)
}
