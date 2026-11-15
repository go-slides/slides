//go:build !slides_nomathfont

// Package embedded registers, with package mathfont, the provider go-slides
// ships: the STIX Two Math font go-tex/math embeds (838 KB, 644 KB gzipped).
// The slides package imports it for its effect unless it is built with the tag
// slides_nomathfont, which excludes this package's code so that nothing refers
// to the font and the linker leaves it out.
package embedded

import (
	"github.com/go-tex/math"

	"github.com/go-slides/slides/mathfont"
)

type provider struct{}

func (provider) Font() ([]byte, error) { return math.DefaultFont(), nil }

// Register makes the embedded font the registered provider again, after a host
// or a test put another one in its place. Importing the package does it once.
func Register() { mathfont.Register(provider{}) }

func init() { Register() }
