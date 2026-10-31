package diag

import (
	"errors"
	"fmt"
	"testing"
)

func TestWrapKeepsTextAndFindsCode(t *testing.T) {
	inner := errors.New("deck.md:3: boom")
	e := Wrap("kind.unknown", inner)
	if e.Error() != inner.Error() || !errors.Is(e, inner) || CodeOf(e) != "kind.unknown" {
		t.Errorf("%v %q", e, CodeOf(e))
	}
	// through other layers, %w or not
	if got := CodeOf(fmt.Errorf("outer: %w", e)); got != "kind.unknown" {
		t.Errorf("through %%w: %q", got)
	}
	// the cause names the problem: an inner code wins over an outer wrap
	if got := CodeOf(Wrap("template.exec", e)); got != "kind.unknown" {
		t.Errorf("inner code: %q", got)
	}
	if Wrap("x", nil) != nil || CodeOf(nil) != "" || CodeOf(inner) != "" {
		t.Error("nil and plain errors have no code")
	}
}

func TestLeavesOfAJoin(t *testing.T) {
	j := errors.Join(Errorf("yaml.syntax", "a"), errors.New("b"), errors.Join(Errorf("file.missing", "c")))
	ls := Leaves(j)
	if len(ls) != 3 || CodeOf(ls[0]) != "yaml.syntax" || CodeOf(ls[1]) != "" || CodeOf(ls[2]) != "file.missing" {
		t.Errorf("%v", ls)
	}
	if Leaves(nil) != nil {
		t.Error("nil")
	}
}

func TestThemeWarningCode(t *testing.T) {
	w := ThemeUnknown("x", "default", []string{"default", "gaia"})
	if WarningCode(w) != "theme.unknown" || WarningCode("theme is fine") != "" || WarningCode("something") != "" {
		t.Errorf("%q", w)
	}
}

func TestEveryCodeIsDocumented(t *testing.T) {
	for _, c := range []string{"kind.unknown", "directive.unknown", "template.function", "template.exec", "template.syntax",
		"yaml.syntax", "hcl.syntax", "hcl.unsupported", "theme.unknown", "file.missing"} {
		if Codes[c] == "" {
			t.Errorf("%q has no English text", c)
		}
	}
}
