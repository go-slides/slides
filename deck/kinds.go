package deck

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// A Kind is a named preset of local directives: the header, the footer,
// whether the slide is numbered, its class, its background. A slide states
// what it is ("kind: title") instead of repeating how it looks.
//
// "kind: name" applies the preset like a local directive, to the slide and
// the ones after it; "_kind: name" applies it to one slide, like a spot
// directive. Switching kinds first clears what the previous kind set, so a
// class or a footer never leaks from one kind into the next. Directives
// written by hand always win over the kind, and, being the author's own,
// carry over to the following slides as in Marp, kind switches included.
type Kind struct {
	Name string
	// Extends names a kind whose directives this one starts from.
	Extends string
	// Dirs are the kind's own directives, already normalized.
	Dirs Dirs
}

// Kinds is a set of kinds by name.
type Kinds map[string]*Kind

// kindKeys are the keys a kind definition accepts.
var kindKeys = append(slices.Clone(localNames), "extends")

// BuiltinKinds returns the kinds every deck starts with. A deck, a
// configuration file or a kinds file can redefine any of them.
func BuiltinKinds() Kinds {
	k := Kinds{}
	def := func(name string, kv ...string) {
		kind := &Kind{Name: name}
		for i := 0; i+1 < len(kv); i += 2 {
			kind.Dirs.Set(kv[i], kv[i+1])
		}
		k[name] = kind
	}
	// A title slide: no header, no footer, counted but not numbered.
	def("title", "class", "lead", "paginate", "false", "header", "", "footer", "")
	// A section divider: numbered, styled like a title.
	def("section", "class", "lead")
	// An ordinary slide: the deck's own defaults.
	def("content")
	// A slide for a full-bleed image or a quote: nothing around it.
	def("blank", "paginate", "false", "header", "", "footer", "")
	return k
}

// ParseKinds reads kind definitions from a decoded YAML mapping of name to
// directives.
func ParseKinds(v any) (Kinds, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("kinds: want a mapping of kind name to directives, got %T", v)
	}
	k := Kinds{}
	for _, name := range orderedKeys(m) {
		if name == orderKey {
			continue
		}
		def, ok := m[name].(map[string]any)
		if m[name] == nil {
			def, ok = map[string]any{}, true
		}
		if !ok {
			return nil, fmt.Errorf("kind %q: want a mapping of directives, got %T", name, m[name])
		}
		kind := &Kind{Name: name}
		for _, key := range orderedKeys(def) {
			if key == orderKey {
				continue
			}
			val := def[key]
			if key == "extends" {
				s, isString := val.(string)
				if !isString {
					return nil, fmt.Errorf("kind %q: extends wants a kind name", name)
				}
				kind.Extends = s
				continue
			}
			if !isLocal(key) {
				return nil, fmt.Errorf("kind %q: %q is not a local directive (want one of %s)", name, key, strings.Join(kindKeys, ", "))
			}
			if s, ok := normalizeLocal(key, stringify(val)); ok {
				kind.Dirs.Set(key, s)
			}
		}
		k[name] = kind
	}
	return k, nil
}

// stringify turns typed YAML (a front matter decoded with the core schema)
// into the failsafe form directives expect.
func stringify(v any) any {
	switch x := v.(type) {
	case nil, string:
		return x
	case []any:
		s := make([]any, len(x))
		for i, e := range x {
			s[i] = stringify(e)
		}
		return s
	}
	return fmt.Sprint(v)
}

// Merge returns k overridden by o.
func (k Kinds) Merge(o Kinds) Kinds {
	m := maps.Clone(k)
	if m == nil {
		m = Kinds{}
	}
	maps.Copy(m, o)
	return m
}

// Resolve returns a kind's directives with its extends chain applied.
func (k Kinds) Resolve(name string) (Dirs, error) {
	return k.resolve(name, nil)
}

func (k Kinds) resolve(name string, seen []string) (Dirs, error) {
	if slices.Contains(seen, name) {
		return Dirs{}, fmt.Errorf("kind %q extends itself (%s)", name, strings.Join(append(seen, name), " → "))
	}
	kind, ok := k[name]
	if !ok {
		return Dirs{}, fmt.Errorf("unknown kind %q (known: %s)", name, strings.Join(k.Names(), ", "))
	}
	var d Dirs
	if kind.Extends != "" {
		base, err := k.resolve(kind.Extends, append(seen, name))
		if err != nil {
			return Dirs{}, err
		}
		d = base
	}
	d.Merge(kind.Dirs)
	return d, nil
}

// Names returns the kind names, sorted.
func (k Kinds) Names() []string {
	return slices.Sorted(maps.Keys(k))
}
