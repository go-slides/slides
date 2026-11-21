package mathfont

import (
	"strings"
	"testing"
	"time"
)

// cffIdleGlyphs: n glyphs that each run 40,000 hstem operators (a subroutine
// chain) and draw nothing: within go-opentype's budget, and adding no path text.
func cffIdleGlyphs(n int) []byte {
	subrs := [][]byte{{139, 139, 1}, repeat(callg(0), 200), repeat(callg(1), 200)}
	glyphs := make([][]byte, n)
	for i := range glyphs {
		glyphs[i] = callg(2)
	}
	return cffFont(subrs, glyphs)
}

// TestIdleGlyphsAreRefusedByTheClock: no glyph is over budget and the path total
// stays zero, so only the time bound refuses the font; with the real bound the
// same font is read, as a control that the refusal is the clock's.
func TestIdleGlyphsAreRefusedByTheClock(t *testing.T) {
	f := cffIdleGlyphs(1000) // about 1.2 s to read
	old := scanBudget
	defer func() { scanBudget = old }()
	scanBudget = 100 * time.Millisecond
	start := time.Now()
	_, err := bound(f)
	if err == nil || !strings.Contains(err.Error(), "takes over") {
		t.Fatalf("1000 idle glyphs, 100 ms allowed: %v", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("the refusal took %v", d)
	}
	scanBudget = 30 * time.Second
	if _, err := bound(f); err != nil {
		t.Errorf("the same font with time to read it: %v", err)
	}
}
