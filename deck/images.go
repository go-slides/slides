package deck

import (
	"fmt"
	"regexp"
	"strings"
)

// imageOption is one whitespace-separated word of an image's alternative
// text (Marpit's image/parse.js).
type imageOption struct {
	content, leading string
	consumed         bool
}

// imageMeta is what Marpit's image syntax reads from the alternative text.
type imageMeta struct {
	options       []imageOption
	size          string // a percentage, for backgrounds
	width, height string
	filters       [][2]string

	background          bool
	backgroundSize      string
	backgroundSplit     string
	backgroundSplitSize string
	backgroundDirection string
}

var imageMatchers = []struct {
	re    *regexp.Regexp
	apply func(m []string, im *imageMeta)
}{
	{regexp.MustCompile(`^(\d*\.)?\d+%$`), func(m []string, im *imageMeta) { im.size = m[0] }},
	{regexp.MustCompile(`^w(?:idth)?:((?:\d*\.)?\d+(?:%|ch|cm|em|ex|in|mm|pc|pt|px)?|auto)$`), func(m []string, im *imageMeta) { im.width = normalizeLength(m[1]) }},
	{regexp.MustCompile(`^h(?:eight)?:((?:\d*\.)?\d+(?:%|ch|cm|em|ex|in|mm|pc|pt|px)?|auto)$`), func(m []string, im *imageMeta) { im.height = normalizeLength(m[1]) }},
	{regexp.MustCompile(`^blur(?::(.+))?$`), filter("blur", "10px")},
	{regexp.MustCompile(`^brightness(?::(.+))?$`), filter("brightness", "1.5")},
	{regexp.MustCompile(`^contrast(?::(.+))?$`), filter("contrast", "2")},
	{regexp.MustCompile(`^drop-shadow(?::(.+?),(.+?)(?:,(.+?))?(?:,(.+?))?)?$`), func(m []string, im *imageMeta) {
		var args []string
		for _, a := range m[1:] {
			if a == "" {
				continue
			}
			if c := colorFunc.FindStringSubmatch(a); c != nil {
				args = append(args, c[1]+"("+escapeFilter(c[2])+")")
			} else {
				args = append(args, escapeFilter(a))
			}
		}
		v := strings.Join(args, " ")
		if v == "" {
			v = "0 5px 10px rgba(0,0,0,.4)"
		}
		im.filters = append(im.filters, [2]string{"drop-shadow", v})
	}},
	{regexp.MustCompile(`^grayscale(?::(.+))?$`), filter("grayscale", "1")},
	{regexp.MustCompile(`^hue-rotate(?::(.+))?$`), filter("hue-rotate", "180deg")},
	{regexp.MustCompile(`^invert(?::(.+))?$`), filter("invert", "1")},
	{regexp.MustCompile(`^opacity(?::(.+))?$`), filter("opacity", ".5")},
	{regexp.MustCompile(`^saturate(?::(.+))?$`), filter("saturate", "2")},
	{regexp.MustCompile(`^sepia(?::(.+))?$`), filter("sepia", "1")},
}

var (
	colorFunc     = regexp.MustCompile(`^(rgba?|hsla?|hwb|(?:ok)?(?:lab|lch)|color)\((.*)\)$`)
	numberOnly    = regexp.MustCompile(`^(\d*\.)?\d+$`)
	filterEscapes = regexp.MustCompile(`[\\;:()]`)
	splitMatcher  = regexp.MustCompile(`^(left|right)(?::((?:\d*\.)?\d+%))?$`)
	wordSplitter  = regexp.MustCompile(`\s+`)
)

func filter(name, def string) func([]string, *imageMeta) {
	return func(m []string, im *imageMeta) {
		v := def
		if len(m) > 1 && m[1] != "" {
			v = escapeFilter(m[1])
		}
		im.filters = append(im.filters, [2]string{name, v})
	}
}

// escapeFilter escapes the characters that could break out of a CSS filter
// function, as CSS hex escapes.
func escapeFilter(s string) string {
	return filterEscapes.ReplaceAllStringFunc(s, func(c string) string {
		return fmt.Sprintf(`\%x `, c[0])
	})
}

func normalizeLength(v string) string {
	if numberOnly.MatchString(v) {
		return v + "px"
	}
	return v
}

var bgSizeKeywords = map[string]string{"auto": "auto", "contain": "contain", "cover": "cover", "fit": "contain"}

// parseImageAlt reads Marpit's image options from an alternative text.
func parseImageAlt(alt string) *imageMeta {
	im := &imageMeta{}
	leading := ""
	pos := 0
	for _, loc := range append(wordSplitter.FindAllStringIndex(alt, -1), []int{len(alt), len(alt)}) {
		word := alt[pos:loc[0]]
		if word != "" {
			im.options = append(im.options, imageOption{content: word, leading: leading})
			leading = ""
		}
		leading += alt[loc[0]:loc[1]]
		pos = loc[1]
	}
	for i := range im.options {
		opt := &im.options[i]
		for _, m := range imageMatchers {
			if opt.consumed {
				break
			}
			if sub := m.re.FindStringSubmatch(opt.content); sub != nil {
				opt.consumed = true
				m.apply(sub, im)
			}
		}
	}
	// Background keywords (Marpit's background_image/parse.js).
	for _, opt := range im.options {
		if !opt.consumed && opt.content == "bg" {
			im.background = true
		}
	}
	if im.background {
		for i := range im.options {
			opt := &im.options[i]
			if opt.consumed {
				continue
			}
			switch {
			case opt.content == "bg":
				opt.consumed = true
			case bgSizeKeywords[opt.content] != "":
				im.backgroundSize = bgSizeKeywords[opt.content]
				opt.consumed = true
			case opt.content == "vertical" || opt.content == "horizontal":
				im.backgroundDirection = opt.content
				opt.consumed = true
			default:
				if m := splitMatcher.FindStringSubmatch(opt.content); m != nil {
					im.backgroundSplit, im.backgroundSplitSize = m[1], m[2]
					opt.consumed = true
				}
			}
		}
	}
	return im
}

// alt returns the alternative text without the consumed options.
func (im *imageMeta) alt() string {
	var b strings.Builder
	for _, o := range im.options {
		if !o.consumed {
			b.WriteString(o.leading + o.content)
		}
	}
	return strings.TrimLeft(b.String(), " \t\n")
}

// anyConsumed reports whether the alternative text lost any word.
func (im *imageMeta) anyConsumed() bool {
	for _, o := range im.options {
		if o.consumed {
			return true
		}
	}
	return false
}

// filter returns the CSS filter value.
func (im *imageMeta) filter() string {
	parts := make([]string, len(im.filters))
	for i, f := range im.filters {
		parts[i] = f[0] + "(" + f[1] + ")"
	}
	return strings.Join(parts, " ")
}

// inlineStyle returns the style attribute of an inline image.
func (im *imageMeta) inlineStyle() string {
	var b strings.Builder
	if im.width != "" && !strings.HasSuffix(im.width, "%") {
		b.WriteString("width:" + im.width + ";")
	}
	if im.height != "" && !strings.HasSuffix(im.height, "%") {
		b.WriteString("height:" + im.height + ";")
	}
	if f := im.filter(); f != "" {
		b.WriteString("filter:" + f + ";")
	}
	return b.String()
}

// backgroundSizeValue is the CSS background-size of a background image
// (Marpit's background_image/apply.js).
func (im *imageMeta) backgroundSizeValue() string {
	s := im.size
	if s == "" {
		s = im.backgroundSize
	}
	if s != "contain" && s != "cover" && (im.width != "" || im.height != "") {
		w, h := im.width, im.height
		if w == "" {
			w = or(s, "auto")
		}
		if h == "" {
			h = or(s, "auto")
		}
		return w + " " + h
	}
	return s
}

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
