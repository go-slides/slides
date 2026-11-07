package deck

import (
	"strings"
	"testing"

	"github.com/go-slides/slides/diag"
)

func TestCheckNesting(t *testing.T) {
	for _, tc := range []struct {
		name, src string
		line      int
		bad       bool
	}{
		{"plain", "# a\n\ntext\n", 0, false},
		{"empty", "", 0, false},
		{"quotes at the bound", strings.Repeat(">", maxQuoteDepth) + " x\n", 0, false},
		{"quotes past the bound", "a\n" + strings.Repeat("> ", maxQuoteDepth+1) + "x\n", 2, true},
		{"quotes without spaces", strings.Repeat(">", maxQuoteDepth+1), 1, true},
		{"quote marks in text", "a > b > c " + strings.Repeat(">", 500) + "\n", 0, false},
		{"indent at the bound", strings.Repeat(" ", maxIndent) + "- a\n", 0, false},
		{"indent past the bound", "\n\n" + strings.Repeat(" ", maxIndent+1) + "- a\n", 3, true},
		{"tabs", strings.Repeat("\t", maxIndent/4+1) + "- a\n", 1, true},
	} {
		line, err := checkNesting(tc.src)
		if (err != nil) != tc.bad || line != tc.line {
			t.Errorf("%s: line %d, %v", tc.name, line, err)
		}
		if err != nil && diag.CodeOf(err) != "deck.limit" {
			t.Errorf("%s: code %q", tc.name, diag.CodeOf(err))
		}
	}
}

func TestKindsExtendsCycleAndDepth(t *testing.T) {
	k := Kinds{"a": &Kind{Extends: "b"}, "b": &Kind{Extends: "a"}, "s": &Kind{Extends: "s"}}
	for _, name := range []string{"a", "s"} {
		if _, err := k.Resolve(name); diag.CodeOf(err) != "kind.cycle" {
			t.Errorf("%s: %v", name, err)
		}
	}
	long := Kinds{}
	for i := 0; i < 2*maxExtendsDepth; i++ {
		long[kindName(i)] = &Kind{Extends: kindName(i + 1)}
	}
	long[kindName(2*maxExtendsDepth)] = &Kind{}
	if _, err := long.Resolve(kindName(0)); diag.CodeOf(err) != "kind.cycle" {
		t.Errorf("a chain too long: %v", err)
	}
	if _, err := long.Resolve(kindName(maxExtendsDepth + 5)); err != nil {
		t.Errorf("a chain within the bound: %v", err)
	}
}

func kindName(i int) string { return "k" + strings.Repeat("x", i%7) + string(rune('A'+i%26)) + itoa(i) }

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for ; i > 0; i /= 10 {
		b = append([]byte{byte('0' + i%10)}, b...)
	}
	return string(b)
}
