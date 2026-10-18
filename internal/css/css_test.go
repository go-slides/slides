package css

import (
	"strings"
	"testing"
)

func TestParseRoundTrip(t *testing.T) {
	src := `@charset "UTF-8";
/* c */
@import url("x.css") screen;
a, b:hover { color: red !important; content: "a;b{c}" }
@media (min-width: 1px) { p { margin: 0 } }
:root { --x: { a: b }; --y: 1 }
@font-face { font-family: X; src: url(data:a;b) }
x { y: z
`
	s := Parse(src)
	out := s.String()
	for _, want := range []string{
		`@charset "UTF-8";`, `/* c */`, `@import url("x.css") screen;`,
		"a, b:hover {", "color: red !important;", `content: "a;b{c}";`,
		"@media (min-width: 1px) {", "--x: { a: b };", "src: url(data:a;b);", "y: z;",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if again := Parse(out).String(); again != out {
		t.Errorf("not stable:\n%s\n---\n%s", out, again)
	}
}

func TestSplitSelectors(t *testing.T) {
	got := SplitSelectors(`a, b:is(c, d), [x=","], e`)
	want := []string{"a", "b:is(c, d)", `[x=","]`, "e"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("%q", got)
	}
}

func TestFlatten(t *testing.T) {
	s := Parse(`a, b { color: red; & > c { x: y; } .d & { z: w; } e { f: g; } @media print { h: i; } j: k; }`)
	if !HasNesting(s.Nodes) {
		t.Fatal("nesting not detected")
	}
	s.Nodes = Flatten(s.Nodes)
	out := s.String()
	for _, want := range []string{"a, b {\n  color: red;", "a > c, b > c {", ".d a, .d b {", "a e, b e {", "@media print {\n  a, b {\n    h: i;", "a, b {\n  j: k;"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if HasNesting(s.Nodes) {
		t.Error("still nested")
	}
}

func TestUnterminated(t *testing.T) {
	for _, src := range []string{"/* open", "a { b: c", `a { content: "x`, "@media x { a { b: c }", "}}}"} {
		_ = Parse(src).String() // must not panic or loop
	}
}
