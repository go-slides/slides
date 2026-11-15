//go:build slides_noemoji

package slides

import "github.com/go-slides/slides/emoji"

// restoreEmoji puts the build's own provider back (none) after a test replaced
// it.
func restoreEmoji() { emoji.Register(nil) }
