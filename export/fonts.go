package export

import (
	"fmt"
	"sync"
)

// themeFaces are the faces of bundledFaces, by go-opentype/fonts' package
// and file name.
var themeFaces = []struct {
	family string
	weight int
	italic bool
	file   string
}{
	{"Lato", 400, false, "lato/lato.ttf"},
	{"Lato", 700, false, "lato/lato-bold.ttf"},
	{"Lato", 900, false, "lato/lato-black.ttf"},
	{"Lato", 400, true, "lato/lato-italic.ttf"},
	{"Lato", 700, true, "lato/lato-bolditalic.ttf"},
	{"Lato", 900, true, "lato/lato-blackitalic.ttf"},
	{"Roboto Mono", 400, false, "robotomono/robotomono.ttf"},
	{"Roboto Mono", 700, false, "robotomono/robotomono-bold.ttf"},
	{"Roboto Mono", 400, true, "robotomono/robotomono-italic.ttf"},
	{"Roboto Mono", 700, true, "robotomono/robotomono-bolditalic.ttf"},
}

// FontNames are the files of the fonts the exports bundle for Marp's
// built-in themes (Lato, Roboto Mono), named as go-opentype/fonts names
// them: "lato/lato-black.ttf". A host built with the slides_fontsext tag
// serves these and hands them over through SetFontSource.
func FontNames() []string {
	names := make([]string, len(themeFaces))
	for i, f := range themeFaces {
		names[i] = f.file
	}
	return names
}

var (
	fontMu     sync.Mutex
	fontSource func(name string) ([]byte, error)
)

// SetFontSource sets where the bundled theme fonts' files come from: src is
// asked for one of FontNames the first time a slide draws that face, once
// per process. A build with the slides_fontsext tag embeds none of them and
// must set it before the first export: a host in a browser fetches them
// from its own origin then, so that a deck that never draws Lato never
// downloads it. Without the tag the embedded files answer.
func SetFontSource(src func(name string) ([]byte, error)) {
	fontMu.Lock()
	defer fontMu.Unlock()
	fontSource = src
}

// themeFont is the file of a theme face: the source set, else the embedded
// file.
func themeFont(name string) ([]byte, error) {
	fontMu.Lock()
	src := fontSource
	fontMu.Unlock()
	if src != nil {
		return src(name)
	}
	if b := embeddedFont(name); b != nil {
		return b, nil
	}
	return nil, fmt.Errorf("export: no source for the font %s (built with slides_fontsext: call SetFontSource)", name)
}
