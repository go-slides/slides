package theme

import (
	"strings"
	"testing"
)

// TestPackBefore: Before's rules sit ahead of the theme's, in the order given,
// and After's behind them (Marpit's pack option of that name).
func TestPackBefore(t *testing.T) {
	s := NewSet()
	p := s.Pack(s.Resolve(""), PackOptions{
		Before: []string{"b1{color:red}", "b2{color:blue}"},
		After:  []string{"a1{color:green}"},
	})
	b1, b2, a1 := strings.Index(p.CSS, "b1"), strings.Index(p.CSS, "b2"), strings.Index(p.CSS, "a1")
	if b1 < 0 || b2 < 0 || a1 < 0 {
		t.Fatalf("missing rule in %.200s", p.CSS)
	}
	if !(b1 < b2 && b2 < a1) {
		t.Errorf("order: b1 %d, b2 %d, a1 %d", b1, b2, a1)
	}
	// With no Before the sheet is the theme's alone.
	if q := s.Pack(s.Resolve(""), PackOptions{}); strings.Contains(q.CSS, "b1") {
		t.Error("Before leaked into another pack")
	}
}
