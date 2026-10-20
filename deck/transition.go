package deck

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
)

// Transitions are Marp CLI's: the transition directive, "name" or "name
// duration" (<!-- transition: fade 1s -->), a local directive like the others,
// with _transition for one slide. The page plays it with the View Transition
// API when the viewer goes from the slide that has it to the next (and, when
// going back, from the slide after it to it). The section carries the
// directive as JSON, data-transition for its own and data-transition-back for
// the slide before's, which is what Marp CLI's marp_cli_transition writes.

// transitionSpec is the JSON of a transition: name, duration and whether the
// built-in keyframes of that name stand in when the theme has none of its own.
type transitionSpec struct {
	Name            string `json:"name"`
	Duration        string `json:"duration,omitempty"`
	BuiltinFallback *bool  `json:"builtinFallback,omitempty"`
}

// parseTransition reads a transition directive: its first two words, as Marp
// CLI splits it. An empty one is no transition.
func parseTransition(v string) (transitionSpec, bool) {
	f := strings.Fields(v)
	if len(f) == 0 {
		return transitionSpec{}, false
	}
	t := transitionSpec{Name: f[0]}
	if len(f) > 1 {
		t.Duration = f[1]
	}
	if builtinTransitions[t.Name] != "" {
		yes := true
		t.BuiltinFallback = &yes
	}
	return t, true
}

// transitionKeyframes matches the name of a keyframes rule in a style.
var transitionKeyframes = regexp.MustCompile(`@keyframes\s+([^\s{]+)`)

// transitionNamed matches the names of the keyframes a transition is made of:
// marp-[outgoing-|incoming-]transition-[backward-]NAME.
var transitionNamed = regexp.MustCompile(`^marp-(?:(?:outgoing|incoming)-)?transition-(?:backward-)?(.+)$`)

// slideTransition is the transition in effect on a slide. When the slide's own
// scoped style defines keyframes of the transition's name, the transition is
// the slide's own and its name takes the slide's scope key, as marp-cli's
// does (foo becomes foo-KEY, where Marpit renames the scoped keyframes the
// same way: see theme.Scope), with no built-in to fall back on.
func slideTransition(s *Slide) (transitionSpec, bool) {
	t, ok := parseTransition(s.Dirs.Get("transition"))
	if !ok {
		return t, false
	}
	for _, css := range s.ScopedStyles {
		for _, k := range transitionKeyframes.FindAllStringSubmatch(css, -1) {
			if m := transitionNamed.FindStringSubmatch(k[1]); m != nil && m[1] == t.Name {
				no := false
				t.Name += "-" + strings.TrimPrefix(scopeAttr(s), "data-marpit-scope-")
				t.BuiltinFallback = &no
				return t, true
			}
		}
	}
	return t, true
}

// transitionAttr is the attribute value of the transition in effect.
func transitionAttr(s *Slide) (string, bool) {
	t, ok := slideTransition(s)
	if !ok {
		return "", false
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false) // JSON.stringify does not escape < > &
	if err := enc.Encode(t); err != nil {
		return "", false
	}
	return strings.TrimRight(b.String(), "\n"), true
}

// TransitionCSS returns the keyframes of the built-in transitions the deck
// uses, in the order of their first use, to be written into the page; and ""
// if it uses none.
func (d *Deck) TransitionCSS() string {
	var b strings.Builder
	seen := map[string]bool{}
	for _, s := range d.Slides {
		// the built-in is collected under the name the directive wrote, even
		// when scoped keyframes then take the transition over (as marp-cli)
		t, ok := parseTransition(s.Dirs.Get("transition"))
		if !ok || t.BuiltinFallback == nil || seen[t.Name] {
			continue
		}
		seen[t.Name] = true
		b.WriteString(builtinTransitions[t.Name])
	}
	return b.String()
}

// HasTransitions reports whether any slide has a transition.
func (d *Deck) HasTransitions() bool {
	for _, s := range d.Slides {
		if _, ok := parseTransition(s.Dirs.Get("transition")); ok {
			return true
		}
	}
	return false
}
