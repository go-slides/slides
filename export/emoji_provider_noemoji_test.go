//go:build slides_noemoji

package export

import (
	"github.com/go-icons/twemoji"

	"github.com/go-slides/slides/emoji"
)

// Built with slides_noemoji no emoji data is linked; the tests that draw emoji
// get go-icons/twemoji through a provider that only the test binary links.
type testEmojiProvider struct{}

func (testEmojiProvider) SVGByStem(stem string) (string, bool) { return twemoji.SVGByStem(stem) }

func init() { emoji.Register(testEmojiProvider{}) }
