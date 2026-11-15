//go:build !slides_nomathfont

package slides

import "github.com/go-slides/slides/mathfont/embedded"

// restoreMathFont puts the build's own provider back after a test replaced it.
func restoreMathFont() { embedded.Register() }
