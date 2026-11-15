//go:build slides_nomathfont

package slides

import "github.com/go-slides/slides/mathfont"

// restoreMathFont puts the build's own provider back (none) after a test
// replaced it.
func restoreMathFont() { mathfont.Register(nil) }
