//go:build slides_nomathfont

package export

import (
	"github.com/go-tex/math"

	"github.com/go-slides/slides/mathfont"
)

// Built with slides_nomathfont no font is linked by the slides package; the
// tests that typeset a formula get STIX Two Math through a provider that only
// the test binary links.
type testFontProvider struct{}

func (testFontProvider) Font() ([]byte, error) { return math.DefaultFont(), nil }

func init() { mathfont.Register(testFontProvider{}) }
