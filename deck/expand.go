package deck

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"

	"github.com/go-slides/slides/interp"
)

// ownFrontKeys are front-matter keys only this tool reads.
var ownFrontKeys = []string{keyKinds, keyData, keyTemplate, keyDelims, KindDirective, "_" + KindDirective}

// Markdown returns the deck as plain Marp Markdown: templates expanded, page
// numbers written out, and kinds replaced by the directives they stand for,
// so that marp-cli or any Marp tool renders the same slides.
//
// Kinds are removed from the front matter and from comments, then the
// result is parsed again the way Marpit would see it; wherever a slide's
// directives differ from what this deck gives it, a spot-directive comment
// at the start of the slide restores them.
func (d *Deck) Markdown() (string, error) {
	front := ""
	if d.hasFront {
		front = stripFrontKeys(d.frontText, ownFrontKeys)
	}
	body := d.stripKindComments()

	// What Marpit makes of the deck without kinds.
	plain := "---\n" + front + "---\n" + body
	if !d.hasFront {
		plain = body
	}
	opts := d.opts
	opts.NoTemplate = true
	opts.Kinds = nil
	marp, err := Parse([]byte(plain), opts)
	if err != nil {
		return "", err
	}

	var out strings.Builder
	if d.hasFront {
		out.WriteString("---\n" + interp.Resolve(front, 1, d.total()) + "---\n")
	}
	// The stripped body has the same slides; each runs from its start to
	// the next one's, its separator included.
	for i, s := range marp.Slides {
		if i >= len(d.Slides) {
			break
		}
		want := d.Slides[i]
		next := len(body)
		if i+1 < len(marp.Slides) {
			next = marp.Slides[i+1].start
		}
		out.WriteString(spotComment(s.Dirs, want))
		out.WriteString(interp.Resolve(body[s.start:next], want.Page, want.Total))
	}
	return out.String(), nil
}

func (d *Deck) total() int {
	if len(d.Slides) == 0 {
		return 1
	}
	return d.Slides[0].Total
}

// spotComment returns the spot directives that turn got into the
// directives want has, page numbers resolved.
func spotComment(got Dirs, want *Slide) string {
	var lines []string
	for _, k := range localNames {
		w := interp.Resolve(want.Dirs.Get(k), want.Page, want.Total)
		g := got.Get(k)
		if k == "paginate" {
			w, g = or(w, "false"), or(g, "false")
		}
		if w == g {
			continue
		}
		v, _ := json.Marshal(w)
		lines = append(lines, "_"+k+": "+string(v))
	}
	if len(lines) == 0 {
		return ""
	}
	sort.Strings(lines)
	return "<!--\n" + strings.Join(lines, "\n") + "\n-->\n\n"
}

var kindLine = regexp.MustCompile(`(?m)^[ \t]*["']?_?kind["']?[ \t]*:.*(?:\n|$)`)

// stripKindComments removes kind directives from the deck's comments, and
// the comments left empty.
func (d *Deck) stripKindComments() string {
	src := string(d.source)
	var out strings.Builder
	pos := 0
	for _, c := range collectComments(d.doc, d.source, !d.opts.StrictYAML) {
		if c.dirs == nil {
			continue
		}
		if _, k := c.dirs[KindDirective]; !k {
			if _, sk := c.dirs["_"+KindDirective]; !sk {
				continue
			}
		}
		raw, start, _ := commentText(c.node, d.source)
		if start < pos {
			continue
		}
		end := start + len(raw)
		m := commentMatcher.FindStringSubmatchIndex(raw)
		if m == nil {
			continue
		}
		inner := kindLine.ReplaceAllString(raw[m[2]:m[3]], "")
		out.WriteString(src[pos:start])
		if strings.TrimSpace(inner) != "" {
			out.WriteString(raw[:m[2]] + inner + raw[m[3]:])
		}
		pos = end
	}
	out.WriteString(src[pos:])
	return out.String()
}

// stripFrontKeys removes top-level keys, and the lines nested under them,
// from YAML front matter text.
func stripFrontKeys(fm string, keys []string) string {
	var out strings.Builder
	skipping := false
	for _, line := range strings.SplitAfter(fm, "\n") {
		if line == "" {
			continue
		}
		top := line[0] != ' ' && line[0] != '\t' && line[0] != '#' && strings.TrimSpace(line) != ""
		if top {
			key, _, _ := strings.Cut(line, ":")
			key = strings.Trim(strings.TrimSpace(key), `"'`)
			skipping = containsString(keys, key)
		}
		if !skipping {
			out.WriteString(line)
		}
	}
	return out.String()
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
