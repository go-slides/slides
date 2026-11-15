//go:build slides_noemoji

package deck

import (
	"github.com/go-icons/twemoji"

	"github.com/go-slides/slides/emoji"
)

// Built with slides_noemoji the package links no emoji data, and the tests of
// this package that read the images need some: they get go-icons/twemoji
// through a provider of their own, which only the test binary links. (The
// contract without a provider is tested in the slides package.)
type testProvider struct{}

func (testProvider) SVGByStem(stem string) (string, bool) { return twemoji.SVGByStem(stem) }

func init() { emoji.Register(testProvider{}) }
