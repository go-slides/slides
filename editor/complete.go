package editor

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf16"

	"github.com/go-slides/slides"
	"github.com/go-slides/slides/deck"
)

// Item is a completion.
type Item struct {
	Label string `json:"label"`
	// Kind is "directive", "value", "kind", "theme", "function" or
	// "data".
	Kind   string `json:"kind"`
	Detail string `json:"detail,omitempty"`
	// Doc is Markdown.
	Doc string `json:"doc,omitempty"`
	// Insert is the text to insert when it differs from Label.
	Insert string `json:"insert,omitempty"`
}

// Hover is what to show over a position.
type Hover struct {
	// Doc is Markdown.
	Doc string `json:"doc"`
	// The hovered word, on Line from Column to EndColumn (UTF-16).
	Line      int `json:"line"`
	Column    int `json:"column"`
	EndColumn int `json:"endColumn"`
}

// where is the kind of place a position is in.
type where int

const (
	nowhere   where = iota
	frontYAML       // a YAML front matter
	frontHCL        // an HCL front matter (---hcl)
	comment         // an HTML comment: directives
	action          // a template action {{ ... }}
)

// spot is a position and what surrounds it.
type spot struct {
	where  where
	before string // the text before the position, on its line, from where the context starts
	line   string
	col    int // byte offset of the position in line
	lineNo int
}

// locateSpot finds the context of a 0-based line and UTF-16 character.
func locateSpot(src string, line, char int) spot {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	lines := strings.Split(src, "\n")
	if line < 0 || line >= len(lines) {
		return spot{}
	}
	text := lines[line]
	col := byteOffset(text, char)
	s := spot{line: text, col: col, lineNo: line, before: text[:col]}

	// Front matter: from line 1 to its closing line.
	if _, isHCL, ok := frontMatter(src); ok && line > 0 {
		closing := frontMatterEnd(lines)
		if closing < 0 || line < closing {
			s.where = frontYAML
			if isHCL {
				s.where = frontHCL
			}
			return s
		}
	}

	// A template action: an opening {{ before the position on this line
	// with no closing }} after it.
	if i := strings.LastIndex(s.before, "{{"); i >= 0 && !strings.Contains(s.before[i:], "}}") {
		s.where = action
		s.before = s.before[i+2:]
		return s
	}

	// An HTML comment around the position.
	offset := 0
	for _, l := range lines[:line] {
		offset += len(l) + 1
	}
	offset += col
	if open := strings.LastIndex(src[:offset], "<!--"); open >= 0 && !strings.Contains(src[open:offset], "-->") {
		s.where = comment
		if strings.LastIndex(s.before, "<!--") >= 0 {
			s.before = s.before[strings.LastIndex(s.before, "<!--")+4:]
		}
		return s
	}
	return s
}

// frontMatterEnd returns the index of the line that closes the front
// matter, -1 if it is never closed.
func frontMatterEnd(lines []string) int {
	n := len(lines[0]) - len(strings.TrimLeft(lines[0], "-"))
	for i := 1; i < len(lines); i++ {
		t := strings.TrimLeft(lines[i], " ")
		d := len(t) - len(strings.TrimLeft(t, "-"))
		if len(lines[i])-len(t) <= 3 && d >= n && strings.Trim(t[d:], " ") == "" {
			return i
		}
	}
	return -1
}

var (
	// valueRE is "key: val" or "_key: val" (or "key = val" in HCL) up to
	// the position.
	valueRE = regexp.MustCompile(`(?:^|[\s,{])(_?)([A-Za-z]+)\s*[:=]\s*"?([\w:.-]*)$`)
	// keyRE is a key being typed.
	keyStartRE = regexp.MustCompile(`(?:^|\s)(_?)(\w*)$`)
	// identRE is a template identifier being typed, a data field if it
	// starts with a dot.
	identRE = regexp.MustCompile(`(\.?)(\w*)$`)
)

// loaded is the request's configuration and parsed deck, as far as they
// go: kinds, themes and data for completions.
type loaded struct {
	cfg  slides.Config
	deck *deck.Deck
}

// load parses the deck without its line typing, which is the one being
// typed and rarely valid yet (kind: with no name).
func load(r Request, typing int) loaded {
	m := r.fs()
	cfg, err := r.config(m)
	p := loaded{cfg: cfg}
	if err != nil {
		return p
	}
	lines := strings.Split(r.Source, "\n")
	if typing > 0 && typing < len(lines) {
		lines[typing] = ""
	}
	src := []byte(strings.Join(lines, "\n"))
	d, err := deck.Parse(src, cfg.Parse)
	if err != nil {
		// A deck being typed often has a template that does not run
		// yet; its front matter, kinds and data still hold.
		po := cfg.Parse
		po.NoTemplate = true
		d, err = deck.Parse(src, po)
	}
	if err == nil {
		p.deck = d
	}
	return p
}

// kinds are the kinds the deck can use.
func (p loaded) kinds() deck.Kinds {
	if p.deck != nil {
		return p.deck.Kinds
	}
	return deck.BuiltinKinds().Merge(p.cfg.Parse.Kinds)
}

// Complete returns the completions at a 0-based line and UTF-16 character.
func Complete(r Request, line, char int) []Item {
	s := locateSpot(r.Source, line, char)
	switch s.where {
	case frontYAML, frontHCL, comment:
		if m := valueRE.FindStringSubmatch(s.before); m != nil {
			return values(load(r, line), m[2])
		}
		if m := keyStartRE.FindStringSubmatch(s.before); m != nil {
			// Keys start a line in a front matter.
			if s.where != comment && strings.TrimSpace(s.before) != m[1]+m[2] {
				return nil
			}
			return keys(r.Lang, s.where, m[1] == "_")
		}
	case action:
		m := identRE.FindStringSubmatch(s.before)
		if m != nil && m[1] == "." {
			return dataFields(load(r, line))
		}
		var out []Item
		for _, f := range funcDocs {
			out = append(out, Item{Label: f.Name, Kind: "function", Detail: f.Signature, Doc: doc(r.Lang, "func."+f.Name, f.Doc)})
		}
		return out
	}
	return nil
}

// keys are the directive keys a context takes; spot keeps the local ones.
func keys(lang string, w where, spot bool) []Item {
	sep := ": "
	if w == frontHCL {
		sep = " = "
	}
	var out []Item
	for _, d := range Directives() {
		if spot && d.Scope != "local" {
			continue
		}
		if w == comment && d.Scope == "deck" {
			continue // the deck keys live in the front matter
		}
		detail := d.Scope + " directive"
		if d.Scope == "deck" {
			detail = "go-slides front-matter key"
		}
		out = append(out, Item{Label: d.Name, Kind: "directive", Detail: detail, Doc: doc(lang, "dir."+d.Name, d.Doc), Insert: d.Name + sep})
	}
	return out
}

// values are the values a directive key takes.
func values(p loaded, key string) []Item {
	switch key {
	case deck.KindDirective:
		k := p.kinds()
		var out []Item
		for _, n := range k.Names() {
			out = append(out, Item{Label: n, Kind: "kind", Detail: kindDetail(k, n), Doc: kindDoc(k, n)})
		}
		return out
	case "theme":
		if p.cfg.Themes == nil {
			return nil
		}
		var out []Item
		for _, n := range p.cfg.Themes.Names() {
			w, h := p.cfg.Themes.Size(p.cfg.Themes.Get(n))
			out = append(out, Item{Label: n, Kind: "theme", Detail: "theme, " + w + "×" + h})
		}
		return out
	case "size":
		if p.cfg.Themes == nil {
			return nil
		}
		name := ""
		if p.deck != nil {
			name = p.deck.Global.Theme()
		}
		t := p.cfg.Themes.Resolve(name)
		var out []Item
		for _, v := range p.cfg.Themes.Meta(t, "size") {
			f := strings.Fields(v)
			if len(f) == 3 && !slices.ContainsFunc(out, func(i Item) bool { return i.Label == f[0] }) {
				out = append(out, Item{Label: f[0], Kind: "value", Detail: f[1] + " × " + f[2]})
			}
		}
		return out
	case "headingDivider":
		var out []Item
		for i := 1; i <= 6; i++ {
			out = append(out, Item{Label: fmt.Sprint(i), Kind: "value", Detail: "split at headings of level " + fmt.Sprint(i) + " and above"})
		}
		return out
	}
	if d, ok := directive(key); ok {
		var out []Item
		for _, v := range d.Values {
			out = append(out, Item{Label: v, Kind: "value", Detail: key})
		}
		return out
	}
	return nil
}

// dataFields are the template data's top-level keys.
func dataFields(p loaded) []Item {
	data := map[string]any{}
	if p.deck != nil {
		for k, v := range p.deck.Front {
			data[k] = v
		}
		if m, ok := p.deck.Front["data"].(map[string]any); ok {
			for k, v := range m {
				data[k] = v
			}
		}
	}
	for k, v := range p.cfg.Parse.Data {
		data[k] = v
	}
	var out []Item
	for _, k := range slices.Sorted(func(yield func(string) bool) {
		for k := range data {
			if !yield(k) {
				return
			}
		}
	}) {
		out = append(out, Item{Label: k, Kind: "data", Detail: fmt.Sprintf("%T", data[k])})
	}
	return out
}

func kindDetail(k deck.Kinds, name string) string {
	if kind := k[name]; kind != nil && kind.Extends != "" {
		return "kind, extends " + kind.Extends
	}
	return "kind"
}

// kindDoc lists what a kind sets, extends resolved.
func kindDoc(k deck.Kinds, name string) string {
	dirs, err := k.Resolve(name)
	if err != nil {
		return err.Error()
	}
	keys := dirs.Keys()
	if len(keys) == 0 {
		return "Sets nothing: the deck's own defaults."
	}
	var b strings.Builder
	for _, key := range keys {
		fmt.Fprintf(&b, "- `%s`: `%q`\n", key, dirs.Get(key))
	}
	return b.String()
}

// HoverAt returns the hover at a 0-based line and UTF-16 character, nil if
// there is nothing to say.
func HoverAt(r Request, line, char int) *Hover {
	s := locateSpot(r.Source, line, char)
	if s.where == nowhere {
		return nil
	}
	start, end := wordAt(s.line, s.col)
	if start == end {
		return nil
	}
	word := s.line[start:end]
	h := &Hover{Line: s.lineNo, Column: utf16Len(s.line[:start]), EndColumn: utf16Len(s.line[:end])}
	switch s.where {
	case action:
		if f, ok := funcDoc(word); ok {
			h.Doc = "```\n" + f.Signature + "\n```\n\n" + doc(r.Lang, "func."+f.Name, f.Doc)
			return h
		}
		return nil
	}
	// A value: the kind it names.
	before := s.line[:start]
	if m := valueRE.FindStringSubmatch(before); m != nil && m[3] == "" && m[2] == deck.KindDirective {
		k := load(r, -1).kinds()
		if _, ok := k[word]; ok {
			h.Doc = "**kind " + word + "** (" + kindDetail(k, word) + ")\n\n" + kindDoc(k, word)
			return h
		}
		return nil
	}
	// A key.
	after := strings.TrimLeft(s.line[end:], " ")
	if !strings.HasPrefix(after, ":") && !strings.HasPrefix(after, "=") {
		return nil
	}
	name := strings.TrimPrefix(word, "_")
	d, ok := directive(name)
	if !ok {
		return nil
	}
	scope := doc(r.Lang, "ui.scope."+d.Scope, d.Scope+" directive")
	if word != name {
		scope = doc(r.Lang, "ui.scope.spot", "spot directive (this slide only)")
	} else if d.Scope == "deck" {
		scope = doc(r.Lang, "ui.scope.deck", "go-slides front-matter key")
	}
	h.Doc = "**" + d.Name + "**: " + scope + "\n\n" + doc(r.Lang, "dir."+d.Name, d.Doc)
	return h
}

// wordAt returns the byte bounds of the word ([A-Za-z0-9_-]) around col.
func wordAt(line string, col int) (int, int) {
	isWord := func(c byte) bool {
		return c == '_' || c == '-' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
	}
	start, end := col, col
	for start > 0 && isWord(line[start-1]) {
		start--
	}
	for end < len(line) && isWord(line[end]) {
		end++
	}
	return start, end
}

// byteOffset converts a UTF-16 character offset in line to a byte offset.
func byteOffset(line string, char int) int {
	n := 0
	for i, r := range line {
		if n >= char {
			return i
		}
		n += utf16.RuneLen(r)
	}
	return len(line)
}
