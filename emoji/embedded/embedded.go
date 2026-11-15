//go:build !slides_noemoji

package embedded

import (
	"github.com/go-icons/twemoji"

	"github.com/go-slides/slides/emoji"
)

type provider struct{}

func (provider) SVGByStem(stem string) (string, bool) { return twemoji.SVGByStem(stem) }

// Register makes the embedded set the registered provider again, after a host
// or a test put another one in its place. Importing the package does it once.
func Register() { emoji.Register(provider{}) }

func init() { Register() }
