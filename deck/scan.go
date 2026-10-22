package deck

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/yuin/goldmark/ast"
)

// nodeInfo is what scanning learned about a node, for the renderer.
type nodeInfo struct {
	image    *imageMeta
	fragment int    // a list item's fragment number, from 1
	id       string // a heading's id
	hidden   bool   // renders nothing
	fit      bool   // a heading marked <!-- fit -->
	// callouts are a code block's callout markers, when its notes follow it
	// (see callouts.go).
	callouts *codeCallouts
	// callout is a callout note's number, on an item of the list that
	// follows such a block.
	callout int
}

// scan reads each slide's images, fragmented lists, headings and styles.
func (d *Deck) scan() {
	d.info = map[ast.Node]*nodeInfo{}
	info := func(n ast.Node) *nodeInfo {
		i := d.info[n]
		if i == nil {
			i = &nodeInfo{}
			d.info[n] = i
		}
		return i
	}
	// Heading ids are unique across the deck, and never collide with the
	// slides' own anchors (Marp Core's marp_slug).
	slugs := map[string]int{}
	for _, s := range d.Slides {
		slugs[strconv.Itoa(s.Index+1)] = 0
	}
	for _, s := range d.Slides {
		for _, top := range s.nodes {
			_ = ast.Walk(top, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
				if !entering {
					return ast.WalkContinue, nil
				}
				switch x := n.(type) {
				case *ast.Image:
					im := parseImageAlt(rawLabel(x, d.source))
					info(n).image = im
					if im.background {
						info(n).hidden = true
						if u := string(x.Destination); strings.TrimSpace(u) != "" {
							s.Backgrounds = append(s.Backgrounds, Background{
								URL: u, Alt: im.alt(), Size: im.backgroundSizeValue(), Filter: im.filter(),
							})
						}
						if im.backgroundDirection != "" {
							s.BackgroundDirection = im.backgroundDirection
						}
						if im.backgroundSplit != "" {
							s.BackgroundSplit = im.backgroundSplit
							s.BackgroundSplitSize = im.backgroundSplitSize
						}
					}
				case *ast.FencedCodeBlock:
					d.scanCallouts(x, info)
				case *ast.ListItem:
					if l, ok := x.Parent().(*ast.List); ok && (l.Marker == '*' || l.Marker == ')') {
						s.Fragments++
						info(n).fragment = s.Fragments
					}
				case *ast.HTMLBlock:
					if css, scoped, ok := isStyleBlock(x, d.source); ok {
						info(n).hidden = true
						switch {
						case scoped:
							s.ScopedStyles = append(s.ScopedStyles, css)
						case css != "":
							d.Styles = append(d.Styles, css)
						}
					} else if _, _, ok := commentText(x, d.source); ok {
						info(n).hidden = true
					}
				case *ast.RawHTML:
					if _, _, ok := commentText(x, d.source); ok {
						info(n).hidden = true
						if c := commentContent(x, d.source); c == "fit" {
							for p := x.Parent(); p != nil; p = p.Parent() {
								if p.Kind() == ast.KindHeading {
									info(p).fit = true
								}
							}
						}
					}
				}
				return ast.WalkContinue, nil
			})
		}
		// Headings, once images and comments are known to be hidden.
		for _, top := range s.nodes {
			_ = ast.Walk(top, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
				h, ok := n.(*ast.Heading)
				if !entering || !ok {
					return ast.WalkContinue, nil
				}
				slugText, plain := d.headingText(h)
				base := slugify(slugText)
				k, seen := slugs[base]
				if seen {
					k++
				}
				slugs[base] = k
				id := base
				if k > 0 {
					id = fmt.Sprintf("%s-%d", base, k)
				}
				info(n).id = id
				var raw strings.Builder
				for i := 0; i < h.Lines().Len(); i++ {
					seg := h.Lines().At(i)
					raw.Write(seg.Value(d.source))
				}
				s.Headings = append(s.Headings, Heading{Level: h.Level, Text: plain, ID: id, Raw: strings.TrimSpace(raw.String())})
				return ast.WalkSkipChildren, nil
			})
		}
	}
}

// headingText returns the text Marp Core slugifies (text, code, images'
// raw alternative text and inline HTML, hidden tokens left out) and a plain
// text for outlines.
func (d *Deck) headingText(h *ast.Heading) (slug string, plain string) {
	var sb, pb, run strings.Builder
	// A run of texts (goldmark cuts one at every delimiter character) is
	// read whole, so that a :shortcode: is the emoji it stands for: Marp
	// Core slugs the emoji, not the name.
	flush := func() {
		if run.Len() == 0 {
			return
		}
		t := run.String()
		run.Reset()
		if !d.opts.NoEmoji {
			t = expandShortcodes(t)
		}
		sb.WriteString(t)
		pb.WriteString(t)
	}
	_ = ast.Walk(h, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if _, isText := n.(*ast.Text); !isText {
			flush()
		}
		if i := d.info[n]; i != nil && i.hidden && n.Kind() != ast.KindImage {
			return ast.WalkSkipChildren, nil
		}
		switch x := n.(type) {
		case *ast.Text:
			run.Write(x.Segment.Value(d.source))
			if x.SoftLineBreak() || x.HardLineBreak() {
				flush()
				pb.WriteString(" ")
			}
		case *ast.String:
			sb.Write(x.Value)
			pb.Write(x.Value)
		case *ast.CodeSpan:
			for c := x.FirstChild(); c != nil; c = c.NextSibling() {
				if t, ok := c.(*ast.Text); ok {
					v := string(t.Segment.Value(d.source))
					sb.WriteString(v)
					pb.WriteString(v)
				}
			}
			return ast.WalkSkipChildren, nil
		case *ast.Image:
			sb.WriteString(rawLabel(x, d.source))
			return ast.WalkSkipChildren, nil
		case *ast.RawHTML:
			sb.Write(x.Segments.Value(d.source))
		case *ast.AutoLink:
			v := string(x.Label(d.source))
			sb.WriteString(v)
			pb.WriteString(v)
		}
		return ast.WalkContinue, nil
	})
	flush()
	return sb.String(), strings.TrimSpace(pb.String())
}

// slugPunct is Marp Core's slugifier character class.
var (
	slugSpaces = regexp.MustCompile(`\s+`)
	slugPunct  = regexp.MustCompile("[\\][!/'\"#$%&()*+,./:;<=>?@\\\\^{|}~`。，、；：？！…—·ˉ¨‘’“”々～‖∶＂＇｀｜〃〔〕〈〉《》「」『』．〖〗【】（）［］｛｝]")
	slugEdge   = regexp.MustCompile(`(?:^-+|-+$)`)
)

// slugify is Marp Core's default slugifier: lower case, spaces to dashes,
// punctuation dropped, then the first run of leading or trailing dashes
// trimmed (the JavaScript replace has no g flag), then encodeURI.
func slugify(s string) string {
	// A deck re-parsed on every keystroke slugifies the same headings each
	// time, through four regular expressions: remember the recent ones.
	slugMu.Lock()
	v, ok := slugMemo[s]
	slugMu.Unlock()
	if ok {
		return v
	}
	v = slugifyUncached(s)
	slugMu.Lock()
	if len(slugMemo) >= slugMemoSize {
		clear(slugMemo)
	}
	slugMemo[s] = v
	slugMu.Unlock()
	return v
}

const slugMemoSize = 4096

var (
	slugMu   sync.Mutex
	slugMemo = map[string]string{}
)

func slugifyUncached(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = slugSpaces.ReplaceAllString(s, "-")
	s = slugPunct.ReplaceAllString(s, "")
	if loc := slugEdge.FindStringIndex(s); loc != nil {
		s = s[:loc[0]] + s[loc[1]:]
	}
	return encodeURI(s)
}

// encodeURI is JavaScript's encodeURI.
func encodeURI(s string) string {
	const keep = ";,/?:@&=+$-_.!~*'()#"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte(keep, c) >= 0 {
			b.WriteByte(c)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", c)
	}
	return b.String()
}

// rawLabel returns an image's alternative text as written in the source,
// which is what Marpit parses for options.
func rawLabel(img *ast.Image, source []byte) string {
	start, stop := -1, -1
	_ = ast.Walk(img, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		var segs []int
		switch x := n.(type) {
		case *ast.Text:
			segs = []int{x.Segment.Start, x.Segment.Stop}
		case *ast.RawHTML:
			if x.Segments.Len() > 0 {
				segs = []int{x.Segments.At(0).Start, x.Segments.At(x.Segments.Len() - 1).Stop}
			}
		}
		if segs != nil {
			if start < 0 || segs[0] < start {
				start = segs[0]
			}
			if segs[1] > stop {
				stop = segs[1]
			}
		}
		return ast.WalkContinue, nil
	})
	if start < 0 {
		return ""
	}
	// Emphasis and code delimiters around the first and last text belong
	// to the label too: widen to the brackets.
	for start > 0 && source[start-1] != '[' {
		start--
	}
	for stop < len(source) && source[stop] != ']' {
		stop++
	}
	return string(source[start:stop])
}

func commentContent(n ast.Node, source []byte) string {
	raw, _, ok := commentText(n, source)
	if !ok {
		return ""
	}
	if m := commentMatcher.FindStringSubmatch(raw); m != nil {
		return strings.TrimSpace(m[1])
	}
	return ""
}
