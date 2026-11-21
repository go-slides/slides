package mathfont

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/go-opentype/opentype"
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

// TestIdleGlyphsAreRefusedByTheWork: no glyph is over budget and the path total
// stays zero, so only the work total refuses the font. 65,535 glyphs of 40,000
// operators would be 2.6 billion operators (about 80 s); the refusal comes after
// MaxFontWork of them, deterministically, whatever the host's speed. The elapsed
// time is logged, not asserted: the bound is on operators, not on the clock.
func TestIdleGlyphsAreRefusedByTheWork(t *testing.T) {
	// 21,000 glyphs, 840 million operators (the test font's 2-byte INDEX offsets
	// allow 21,845): without the bound, about 25 s of interpretation.
	f := cffIdleGlyphs(21000)
	start := time.Now()
	_, err := bound(f)
	t.Logf("21,000 idle glyphs of 40,000 operators: refused in %v: %v", time.Since(start), err)
	if err == nil || !strings.Contains(err.Error(), "operators allowed") {
		t.Fatalf("21,000 idle glyphs: %v", err)
	}
	// The refusal is exact: glyph 0 costs 1, every idle glyph costs per (its
	// 40,000 operators and the 40,200 subroutine calls that reach them), and the
	// first glyph to pass MaxFontWork is named.
	parsed, perr := opentype.Parse(f)
	if perr != nil {
		t.Fatal(perr)
	}
	_, per, perr := parsed.NewFace(pathPx).GlyphSVGPathCost(1)
	if perr != nil {
		t.Fatal(perr)
	}
	k := (MaxFontWork-1)/per + 1
	if want := fmt.Sprintf("counted at glyph %d)", k); !strings.Contains(err.Error(), want) {
		t.Errorf("each glyph costs %d, the refusal should name glyph %d: %v", per, k, err)
	}
	// A control: the same glyphs, one fewer than the refusal, are read.
	if _, err := bound(cffIdleGlyphs(k - 1)); err != nil {
		t.Errorf("%d idle glyphs (%d operators): %v", k-1, 1+(k-1)*per, err)
	}
}

// TestScanGlyphsLimits: STIX Two Math is read within both limits, and each limit
// alone refuses it when lowered under what STIX measures (166,885 operators,
// 8,202,727 bytes); a glyph that fails for another reason than its budget (a
// call to a subroutine that does not exist) refuses the font too.
func TestScanGlyphsLimits(t *testing.T) {
	font := stix()
	if err := scanGlyphs(font, MaxFontWork, MaxPathBytes); err != nil {
		t.Fatalf("STIX Two Math: %v", err)
	}
	if err := scanGlyphs(font, 166_884, MaxPathBytes); err == nil || !strings.Contains(err.Error(), "operators allowed") {
		t.Errorf("one operator under STIX's total: %v", err)
	}
	if err := scanGlyphs(font, MaxFontWork, 8_202_726); err == nil || !strings.Contains(err.Error(), "of paths allowed") {
		t.Errorf("one byte under STIX's paths: %v", err)
	}
	broken := cffFont([][]byte{{139, 139, 5}}, [][]byte{callg(50)})
	if err := scanGlyphs(broken, MaxFontWork, MaxPathBytes); err == nil || strings.Contains(err.Error(), "outline budget") {
		t.Errorf("a call to a missing subroutine: %v", err)
	}
}

// TestGlyphCostMatchesTheBudgetError: a glyph over go-opentype's per-glyph budget
// is told apart from any other failure by errors.Is.
func TestGlyphCostMatchesTheBudgetError(t *testing.T) {
	f, err := opentype.Parse(cffFanout(8))
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = f.NewFace(pathPx).GlyphSVGPathCost(1)
	if !errors.Is(err, opentype.ErrOutlineBudget) {
		t.Errorf("a glyph of 8^10 lines: %v", err)
	}
}
