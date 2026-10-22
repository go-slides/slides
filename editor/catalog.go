package editor

import (
	"slices"

	"github.com/go-slides/slides/deck"
)

// Directive describes a directive for completion and hover.
type Directive struct {
	Name string `json:"name"`
	// Scope is "global" (the whole deck), "local" (this slide and the
	// following ones; "_name" for this slide only) or "deck" (a go-slides
	// front-matter key that is not a Marp directive).
	Scope string `json:"scope"`
	// Doc is Markdown.
	Doc string `json:"doc"`
	// Values are the values the directive takes, when they are a short
	// list.
	Values []string `json:"values,omitempty"`
}

// directiveDocs are written from Marpit's and Marp Core's directive
// documentation (marpit.marp.app/directives, marp-core README) and this
// module's README for kind and the deck keys.
var directiveDocs = map[string]Directive{
	"headingDivider":     {Doc: "Starts a new slide before every heading of the given level or above: `2` splits at `#` and `##`; a list (`[1, 3]`) names levels."},
	"style":              {Doc: "CSS added to the theme for the whole deck."},
	"theme":              {Doc: "The theme the deck uses: a built-in one (`default`, `gaia`, `uncover`) or one from `--theme-set`, `themes:` in slides.yaml, or the editor's settings."},
	"lang":               {Doc: "The `lang` attribute of the deck."},
	"size":               {Doc: "A size preset of the theme, such as `16:9` (1280×720) or `4:3` (960×720)."},
	"math":               {Doc: "The math typesetting library Marp would use: `mathjax` or `katex`. go-slides typesets math itself, the same way for both.", Values: []string{"mathjax", "katex"}},
	"title":              {Doc: "The deck's title, written to the HTML page's `<title>` and metadata."},
	"description":        {Doc: "The deck's description, for the page metadata."},
	"author":             {Doc: "The deck's author, for the page metadata."},
	"keywords":           {Doc: "Keywords for the page metadata, comma-separated or a list."},
	"url":                {Doc: "The deck's canonical URL."},
	"image":              {Doc: "The Open Graph image of the deck."},
	"marp":               {Doc: "Marks the document as a Marp deck. The go-slides preview renders it when Marp for VS Code is not installed (setting `goSlides.preview.marpDocuments`).", Values: []string{"true", "false"}},
	"backgroundColor":    {Doc: "The slide's background color."},
	"backgroundImage":    {Doc: "The slide's background image, as a CSS `background-image` (`url(...)`)."},
	"backgroundPosition": {Doc: "The CSS `background-position` of the slide's background image."},
	"backgroundRepeat":   {Doc: "The CSS `background-repeat` of the slide's background image."},
	"backgroundSize":     {Doc: "The CSS `background-size` of the slide's background image."},
	"class":              {Doc: "HTML class of the slide's `<section>`, for the theme to style (`lead`, `invert`)."},
	"color":              {Doc: "The slide's text color."},
	"footer":             {Doc: "The slide's footer, Markdown on one line."},
	"header":             {Doc: "The slide's header, Markdown on one line."},
	"paginate":           {Doc: "Shows the page number: `true`, `false`, `skip` (hidden and not counted) or `hold` (shown, not advanced).", Values: []string{"true", "false", "skip", "hold"}},
	"lineNumbers":        {Doc: "Numbers the lines of the fenced code blocks of this slide and the following ones: `true` or `false`. A block says it for itself in braces after its language: ` ```go {lineNumbers=10} ` (start at 10) or `{lineNumbers=false}`. Marp ignores the braces.", Values: []string{"true", "false"}},
	"transition":         {Doc: "The transition played when leaving the slide (and, going back, when coming to it): a built-in name — `fade`, `slide`, `cover`, `cube`, `swipe`, `zoom` ... — and an optional duration, `fade 1s`. Played with the browser's View Transition API; `_transition` is for this slide only."},
	deck.KindDirective:   {Doc: "The slide's **kind**, a named preset of local directives (header, footer, paginate, class, background). `kind:` applies to this slide and the following ones, `_kind:` to this slide only. Kinds come from the built-ins (`title`, `section`, `content`, `blank`), the `kinds` front-matter key, slides.yaml or slides.hcl, and `--kinds` files."},
	"slides":             {Scope: "deck", Doc: "Marks the document as a go-slides deck: the go-slides preview renders it.", Values: []string{"true", "false"}},
	"kinds":              {Scope: "deck", Doc: "Kinds this deck defines: a mapping of name to local directives (and `extends`), or the path of a YAML or HCL kinds file, or a list of paths."},
	"data":               {Scope: "deck", Doc: "Template data: a mapping that templates read as `{{ .key }}`, or YAML, JSON or HCL file paths."},
	"template":           {Scope: "deck", Doc: "Go template interpolation: on by default; `false` turns it off; `code` expands inside code blocks too.", Values: []string{"true", "false", "code"}},
	"delims":             {Scope: "deck", Doc: "The template delimiters, a list of two strings, instead of `{{` and `}}`."},
}

// Directives returns every directive and go-slides front-matter key, in the
// order Marpit lists them, then kind, then the deck keys.
func Directives() []Directive {
	var out []Directive
	add := func(name, scope string) {
		d := directiveDocs[name]
		d.Name = name
		if d.Scope == "" {
			d.Scope = scope
		}
		out = append(out, d)
	}
	for _, n := range deck.GlobalDirectives() {
		add(n, "global")
	}
	for _, n := range deck.LocalDirectives() {
		add(n, "local")
	}
	add(deck.KindDirective, "local")
	for _, n := range []string{"slides", "kinds", "data", "template", "delims"} {
		add(n, "deck")
	}
	return out
}

// directive returns a directive by name, spot prefix allowed.
func directive(name string) (Directive, bool) {
	i := slices.IndexFunc(Directives(), func(d Directive) bool { return d.Name == name })
	if i < 0 {
		return Directive{}, false
	}
	return Directives()[i], true
}

// KindInfo is a kind and the directives it sets, extends resolved.
type KindInfo struct {
	Name    string `json:"name"`
	Extends string `json:"extends,omitempty"`
	// Directives are the resolved directives, in the order they are set.
	Directives []KV `json:"directives"`
}

// KV is a directive and its value.
type KV struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// KindList lists kinds by name.
func KindList(k deck.Kinds) ([]KindInfo, error) {
	out := []KindInfo{}
	for _, n := range k.Names() {
		dirs, err := k.Resolve(n)
		if err != nil {
			return nil, err
		}
		info := KindInfo{Name: n, Extends: k[n].Extends, Directives: []KV{}}
		for _, key := range dirs.Keys() {
			info.Directives = append(info.Directives, KV{key, dirs.Get(key)})
		}
		out = append(out, info)
	}
	return out, nil
}

// Func describes a template function.
type Func struct {
	Name      string `json:"name"`
	Signature string `json:"signature"`
	Doc       string `json:"doc"`
}

// funcDocs document interp.Funcs and text/template's built-ins.
var funcDocs = []Func{
	{"page", "page", "The slide's page number, as Marpit counts it."},
	{"total", "total", "The deck's last page number."},
	{"now", "now", "The current time (a time.Time), for `date`."},
	{"date", "date LAYOUT TIME", "Formats a time with a Go layout: `{{ date \"2006-01-02\" now }}`."},
	{"env", "env NAME", "An environment variable. Empty in the editor preview, which runs without one."},
	{"upper", "upper S", "S in upper case."},
	{"lower", "lower S", "S in lower case."},
	{"title", "title S", "S with each word capitalized."},
	{"trim", "trim S", "S without leading and trailing white space."},
	{"replace", "replace OLD NEW S", "S with every OLD replaced by NEW."},
	{"split", "split SEP S", "S split around SEP, a list."},
	{"join", "join SEP LIST", "The elements of LIST joined with SEP."},
	{"contains", "contains SUB S", "Whether S contains SUB."},
	{"hasPrefix", "hasPrefix PREFIX S", "Whether S starts with PREFIX."},
	{"hasSuffix", "hasSuffix SUFFIX S", "Whether S ends with SUFFIX."},
	{"repeat", "repeat N S", "S repeated N times."},
	{"default", "default DEF V", "V, or DEF when V is empty."},
	{"seq", "seq N", "The list 1, 2, … N, for `range`."},
	{"add", "add A B", "A + B."},
	{"sub", "sub A B", "A − B."},
	{"mul", "mul A B", "A × B."},
	{"list", "list V...", "A list of the arguments."},
	{"dict", "dict KEY VALUE...", "A mapping from key and value pairs."},
	{"merge", "merge A B", "Mapping A overridden by mapping B."},
	{"quote", "quote S", "S as a quoted Go string."},
	{"include", "include PATH", "The file at PATH (relative to the deck), expanded as a template with the same data. In the editor, only files of the workspace are read."},
	// text/template built-ins (pkg.go.dev/text/template#hdr-Functions).
	{"and", "and X Y...", "The first empty argument, or the last one."},
	{"or", "or X Y...", "The first non-empty argument, or the last one."},
	{"not", "not X", "The boolean negation of X."},
	{"len", "len X", "The length of X."},
	{"index", "index X KEY...", "The element of X at KEY (and so on)."},
	{"slice", "slice X I J", "X[I:J]."},
	{"print", "print ARGS...", "fmt.Sprint of the arguments."},
	{"printf", "printf FORMAT ARGS...", "fmt.Sprintf."},
	{"println", "println ARGS...", "fmt.Sprintln of the arguments."},
	{"eq", "eq A B...", "Whether A equals one of B..."},
	{"ne", "ne A B", "A != B."},
	{"lt", "lt A B", "A < B."},
	{"le", "le A B", "A <= B."},
	{"gt", "gt A B", "A > B."},
	{"ge", "ge A B", "A >= B."},
	{"html", "html ARGS...", "The arguments, HTML-escaped."},
	{"js", "js ARGS...", "The arguments, JavaScript-escaped."},
	{"urlquery", "urlquery ARGS...", "The arguments, escaped for a URL query."},
	{"call", "call FUNC ARGS...", "Calls a function value."},
}

// Funcs returns the template functions.
func Funcs() []Func { return slices.Clone(funcDocs) }

func funcDoc(name string) (Func, bool) {
	i := slices.IndexFunc(funcDocs, func(f Func) bool { return f.Name == name })
	if i < 0 {
		return Func{}, false
	}
	return funcDocs[i], true
}
