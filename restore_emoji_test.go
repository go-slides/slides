//go:build !slides_noemoji

package slides

import "github.com/go-slides/slides/emoji/embedded"

// restoreEmoji puts the build's own provider back after a test replaced it.
func restoreEmoji() { embedded.Register() }
