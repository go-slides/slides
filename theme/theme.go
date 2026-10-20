// Package theme loads Marp theme CSS and packs it for a deck, as Marpit's
// Theme, ThemeSet and theme pack pipeline do (theme.js, theme_set.js and the
// postcss plugins they run).
package theme

import (
	"embed"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/go-slides/slides/internal/css"
)

// Theme is a theme style sheet and what its comments declare.
type Theme struct {
	Name string
	// CSS is the theme as loaded: nesting resolved and :root replaced.
	CSS string
	// Meta holds the @key value comments, every occurrence in order.
	Meta map[string][]string
	// Imports name the themes this one imports, @import-theme first.
	Imports []string
	// Width and Height are the section size the theme sets, as written.
	Width, Height string
}

// MetaValue returns the last value of a metadata key.
func (t *Theme) MetaValue(key string) string {
	v := t.Meta[key]
	if len(v) == 0 {
		return ""
	}
	return v[len(v)-1]
}

// rootPseudo is Marpit's :marpit-root, which :root becomes.
const rootPseudo = ":marpit-root"

var (
	metaRe        = regexp.MustCompile(`(?m)^[*!\s]*@([\w-]+)\s+(.+)$`)
	rootReplaceRe = regexp.MustCompile(`(^|[\s>+~(])(?:section)?:root\b`)
)

// Parse loads a theme. It fails when the CSS declares no @theme name.
func Parse(src string) (*Theme, error) {
	t, err := parse(src)
	if err != nil {
		return nil, err
	}
	if t.Name == "" {
		return nil, fmt.Errorf("theme CSS requires a @theme meta comment")
	}
	return t, nil
}

func parse(src string) (*Theme, error) {
	sheet := css.Parse(src)
	sheet.Nodes = css.Flatten(sheet.Nodes)
	t := &Theme{Meta: map[string][]string{}}
	css.Walk(sheet.Nodes, func(n, _ css.Node) bool {
		if c, ok := n.(*css.Comment); ok {
			for _, m := range metaRe.FindAllStringSubmatch(c.Text, -1) {
				t.Meta[m[1]] = append(t.Meta[m[1]], strings.TrimSpace(m[2]))
			}
		}
		return true
	})
	t.Name = t.MetaValue("theme")
	// :root means the slide.
	css.Walk(sheet.Nodes, func(n, _ css.Node) bool {
		if r, ok := n.(*css.Rule); ok {
			r.Selector = mapSelectors(r.Selector, func(s string) string {
				return replaceRoot(s)
			})
		}
		return true
	})
	t.Width, t.Height = sectionSize(sheet.Nodes)
	t.Imports = importNames(sheet.Nodes)
	t.CSS = sheet.String()
	return t, nil
}

// sectionSize reads the width and height a theme gives section, preferring
// what it gives :root (Marpit's section_size.js).
func sectionSize(nodes []css.Node) (w, h string) {
	var detected, preferred [2]string
	css.Walk(nodes, func(n, _ css.Node) bool {
		r, ok := n.(*css.Rule)
		if !ok {
			return true
		}
		for _, s := range css.SplitSelectors(r.Selector) {
			target := &detected
			switch s {
			case "section":
			case "section" + rootPseudo:
				target = &preferred
			default:
				continue
			}
			for _, d := range r.Nodes {
				if decl, ok := d.(*css.Decl); ok {
					switch decl.Prop {
					case "width":
						target[0] = decl.Value
					case "height":
						target[1] = decl.Value
					}
				}
			}
		}
		return true
	})
	return or(preferred[0], detected[0]), or(preferred[1], detected[1])
}

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// importNames lists @import-theme names, then the quoted @import names at
// the head of the sheet (Marpit's import/parse.js).
func importNames(nodes []css.Node) []string {
	var imports, importThemes []string
	allow := true
	for _, n := range nodes {
		switch x := n.(type) {
		case *css.Comment:
			continue
		case *css.AtRule:
			if allow {
				if x.Name == "import" {
					if v, ok := quoted(x.Params); ok {
						imports = append(imports, v)
					}
				} else if x.Name != "charset" {
					allow = false
				}
			}
			if x.Name == "import-theme" {
				if v, ok := quoted(x.Params); ok {
					importThemes = append(importThemes, v)
				}
			}
		default:
			allow = false
		}
	}
	return append(importThemes, imports...)
}

// quoted returns the content of a leading quoted string.
func quoted(params string) (string, bool) {
	if params == "" || params[0] != '"' && params[0] != '\'' {
		return "", false
	}
	q := params[0]
	var b strings.Builder
	for i := 1; i < len(params); i++ {
		c := params[i]
		if c == '\\' && i+1 < len(params) && params[i+1] == q {
			b.WriteByte(q)
			i++
			continue
		}
		if c == q {
			return b.String(), true
		}
		b.WriteByte(c)
	}
	return b.String(), true
}

func mapSelectors(sel string, fn func(string) string) string {
	parts := css.SplitSelectors(sel)
	for i, p := range parts {
		parts[i] = fn(p)
	}
	return css.JoinSelectors(parts)
}

// Set is a set of themes by name.
type Set struct {
	themes map[string]*Theme
	// Default is the theme used when a deck names none or an unknown one;
	// nil falls back to Marpit's scaffold.
	Default *Theme
}

//go:embed marp/*.css
var builtin embed.FS

// NewSet returns a set holding Marp Core's built-in themes (default, gaia
// and uncover), with default as the default.
func NewSet() *Set {
	s := &Set{themes: map[string]*Theme{}}
	for _, name := range []string{"default", "gaia", "uncover"} {
		b, err := builtin.ReadFile("marp/" + name + ".css")
		if err != nil {
			panic(err)
		}
		t, err := s.Add(string(b))
		if err != nil {
			panic(err)
		}
		if name == "default" {
			s.Default = t
		}
	}
	return s
}

// Add parses a theme and adds it, replacing a theme of the same name.
func (s *Set) Add(src string) (*Theme, error) {
	t, err := Parse(src)
	if err != nil {
		return nil, err
	}
	if s.themes == nil {
		s.themes = map[string]*Theme{}
	}
	s.themes[t.Name] = t
	return t, nil
}

// Get returns a theme by name.
func (s *Set) Get(name string) *Theme { return s.themes[name] }

// Has reports whether a theme is known.
func (s *Set) Has(name string) bool { _, ok := s.themes[name]; return ok }

// Names lists the theme names, sorted.
func (s *Set) Names() []string { return slices.Sorted(maps.Keys(s.themes)) }

// Resolve returns the theme a deck naming name uses.
func (s *Set) Resolve(name string) *Theme {
	if t := s.themes[name]; t != nil {
		return t
	}
	if s.Default != nil {
		return s.Default
	}
	return scaffold
}

// chain is a theme and the themes it imports, depth first.
func (s *Set) chain(t *Theme, seen []string) []*Theme {
	if slices.Contains(seen, t.Name) {
		return nil
	}
	out := []*Theme{t}
	for _, name := range t.Imports {
		if imp := s.themes[name]; imp != nil {
			out = append(out, s.chain(imp, append(seen, t.Name))...)
		}
	}
	return out
}

// Meta returns a theme's metadata through its imports: the first theme in
// the chain that defines the key, or, when every definition is a list, all
// of them with the importing theme's last.
func (s *Set) Meta(t *Theme, key string) []string {
	var found [][]string
	for _, c := range s.chain(t, nil) {
		if v := c.Meta[key]; len(v) > 0 {
			found = append(found, v)
		}
	}
	if len(found) == 0 {
		return nil
	}
	if len(found) == 1 {
		return found[0]
	}
	var merged []string
	for _, v := range found {
		merged = append(slices.Clone(v), merged...)
	}
	return merged
}

// Size returns a theme's slide size, through its imports, falling back to
// the default theme and to Marpit's 1280x720.
func (s *Set) Size(t *Theme) (w, h string) {
	for _, c := range s.chain(t, nil) {
		if w == "" {
			w = c.Width
		}
		if h == "" {
			h = c.Height
		}
	}
	if s.Default != nil {
		w, h = or(w, s.Default.Width), or(h, s.Default.Height)
	}
	return or(w, scaffold.Width), or(h, scaffold.Height)
}

// SizePreset returns the size a @size preset of the theme names, as Marp
// Core's size directive reads it ("16:9 1280px 720px"; "name false"
// removes an imported preset).
func (s *Set) SizePreset(t *Theme, name string) (w, h string, ok bool) {
	presets := map[string][2]string{}
	for _, v := range s.Meta(t, "size") {
		f := strings.Fields(v)
		switch {
		case len(f) == 3:
			presets[f[0]] = [2]string{f[1], f[2]}
		case len(f) == 2 && f[1] == "false":
			delete(presets, f[0])
		}
	}
	p, ok := presets[name]
	return p[0], p[1], ok
}
