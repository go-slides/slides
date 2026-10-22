package deck

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
)

// Global directives apply to the whole deck; the last value written wins.
// The first four are Marpit's (directives/directives.js), size and math are
// Marp Core's, and the metadata ones are Marp CLI's.
var globalNames = []string{
	"headingDivider", "style", "theme", "lang",
	"size", "math",
	"title", "description", "author", "keywords", "url", "image", "marp",
}

// Local directives apply to the slide that defines them and every slide after
// it; prefixed with "_" (a spot directive) they apply to that slide only.
// They are Marpit's, in its order, and Marp CLI's transition last.
var localNames = []string{
	"backgroundColor", "backgroundImage", "backgroundPosition", "backgroundRepeat",
	"backgroundSize", "class", "color", "footer", "header", "paginate",
	"transition",
	// ours: line numbers on the code blocks (linenumbers.go)
	"lineNumbers",
}

// KindDirective is this tool's own local directive: it names a kind, a preset
// of local directives (see Kind).
const KindDirective = "kind"

// looseKeys are the keys whose values loose YAML quotes.
var looseKeys = append(append(slices.Clone(globalNames), localNames...), KindDirective)

// GlobalDirectives returns the global directive names, for editors.
func GlobalDirectives() []string { return slices.Clone(globalNames) }

// LocalDirectives returns the local directive names (kind excluded), for
// editors.
func LocalDirectives() []string { return slices.Clone(localNames) }

func isGlobal(name string) bool { return slices.Contains(globalNames, name) }
func isLocal(name string) bool  { return slices.Contains(localNames, name) }

// Dirs is an ordered set of directive values. Marpit writes them out as
// data-* attributes and CSS custom properties in the order they were set.
type Dirs struct {
	keys []string
	vals map[string]string
}

// Get returns a directive's value.
func (d *Dirs) Get(name string) string { return d.vals[name] }

// Has reports whether a directive is set, even to the empty string.
func (d *Dirs) Has(name string) bool { _, ok := d.vals[name]; return ok }

// Keys returns the directive names in the order they were first set.
func (d *Dirs) Keys() []string { return slices.Clone(d.keys) }

// Set sets a directive, keeping its first position.
func (d *Dirs) Set(name, value string) {
	if d.vals == nil {
		d.vals = map[string]string{}
	}
	if _, ok := d.vals[name]; !ok {
		d.keys = append(d.keys, name)
	}
	d.vals[name] = value
}

// Delete removes a directive.
func (d *Dirs) Delete(name string) {
	if _, ok := d.vals[name]; !ok {
		return
	}
	delete(d.vals, name)
	d.keys = slices.DeleteFunc(d.keys, func(k string) bool { return k == name })
}

// Merge sets every directive of o on d, in o's order.
func (d *Dirs) Merge(o Dirs) {
	for _, k := range o.keys {
		d.Set(k, o.vals[k])
	}
}

// Clone copies d.
func (d Dirs) Clone() Dirs {
	c := Dirs{keys: slices.Clone(d.keys), vals: make(map[string]string, len(d.vals))}
	for k, v := range d.vals {
		c.vals[k] = v
	}
	return c
}

// normalizeLocal converts a raw YAML value the way Marpit's local directive
// functions do. ok is false when Marpit would ignore the value.
func normalizeLocal(name string, v any) (string, bool) {
	switch name {
	case "class":
		if list, isList := v.([]any); isList {
			parts := make([]string, 0, len(list))
			for _, p := range list {
				parts = append(parts, fmt.Sprint(p))
			}
			return strings.Join(parts, " "), true
		}
	case "header", "footer":
		s, isString := v.(string)
		return s, isString
	case "paginate":
		s, _ := v.(string)
		switch n := strings.ToLower(s); n {
		case "hold", "skip":
			return n, true
		case "true":
			return "true", true
		}
		return "false", true
	}
	s, isString := v.(string)
	if !isString {
		if v == nil {
			return "", false
		}
		s = fmt.Sprint(v)
	}
	return s, true
}

// ValidURL reports whether s is an absolute URL, as JavaScript's URL
// constructor, which marp-cli tries the url directive with, would take it.
func ValidURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && u.Scheme != "" && !strings.ContainsAny(s, " \t\r\n")
}

// keywordSep joins the items of a keywords list in a directive value, which
// cannot hold a list: a character no keyword has.
const keywordSep = "\x1f"

// KeywordList returns the keywords of a directive value the way marp-cli
// does: a list as it is, a string split at commas and trimmed, the empty
// ones dropped and the duplicates after the first too.
func KeywordList(raw string) []string {
	var items []string
	if strings.Contains(raw, keywordSep) {
		items = strings.Split(raw, keywordSep)
	} else {
		for _, p := range strings.Split(raw, ",") {
			items = append(items, strings.TrimSpace(p))
		}
	}
	var out []string
	for _, it := range items {
		if it != "" && !slices.Contains(out, it) {
			out = append(out, it)
		}
	}
	return out
}

// normalizeGlobal converts a global directive. headingDivider keeps its
// raw form; see parseHeadingDivider.
func normalizeGlobal(name string, v any) (string, bool) {
	if name == "headingDivider" {
		if list, isList := v.([]any); isList {
			parts := make([]string, 0, len(list))
			for _, p := range list {
				parts = append(parts, fmt.Sprint(p))
			}
			return "[" + strings.Join(parts, ",") + "]", true
		}
	}
	if name == "keywords" {
		// marp-cli takes a list as it is, and a string split at commas.
		if list, isList := v.([]any); isList {
			var parts []string
			for _, p := range list {
				if s, ok := p.(string); ok {
					parts = append(parts, s)
				}
			}
			return keywordSep + strings.Join(parts, keywordSep), true
		}
	}
	if name == "math" {
		s, _ := v.(string)
		if s != "katex" && s != "mathjax" {
			return "", false
		}
	}
	s, isString := v.(string)
	if !isString {
		return "", false
	}
	return s, true
}

// parseHeadingDivider returns the heading levels that start a new slide.
func parseHeadingDivider(v string) []int {
	v = strings.TrimSpace(v)
	if v == "" || v == "false" {
		return nil
	}
	if strings.HasPrefix(v, "[") {
		var levels []int
		for _, p := range strings.Split(strings.Trim(v, "[]"), ",") {
			var n int
			if _, err := fmt.Sscan(strings.TrimSpace(p), &n); err == nil && n >= 1 && n <= 6 && !slices.Contains(levels, n) {
				levels = append(levels, n)
			}
		}
		slices.Sort(levels)
		return levels
	}
	var n int
	if _, err := fmt.Sscan(v, &n); err != nil || n < 1 || n > 6 {
		return nil
	}
	levels := make([]int, n)
	for i := range levels {
		levels[i] = i + 1
	}
	return levels
}
