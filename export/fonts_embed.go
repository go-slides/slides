//go:build !slides_fontsext

package export

import (
	"github.com/go-opentype/fonts/lato"
	"github.com/go-opentype/fonts/robotomono"
)

// FontData is the embedded file of one of FontNames, nil for another name:
// what a host that builds a slides_fontsext module serves, byte for byte.
func FontData(name string) []byte { return embeddedFont(name) }

func embeddedFont(name string) []byte {
	switch name {
	case "lato/lato.ttf":
		return lato.TTF
	case "lato/lato-bold.ttf":
		return lato.Bold
	case "lato/lato-black.ttf":
		return lato.Black
	case "lato/lato-italic.ttf":
		return lato.Italic
	case "lato/lato-bolditalic.ttf":
		return lato.BoldItalic
	case "lato/lato-blackitalic.ttf":
		return lato.BlackItalic
	case "robotomono/robotomono.ttf":
		return robotomono.TTF
	case "robotomono/robotomono-bold.ttf":
		return robotomono.Bold
	case "robotomono/robotomono-italic.ttf":
		return robotomono.Italic
	case "robotomono/robotomono-bolditalic.ttf":
		return robotomono.BoldItalic
	}
	return nil
}
