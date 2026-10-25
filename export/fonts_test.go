package export

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/go-opentype/fonts/lato"
	"github.com/go-opentype/fonts/robotomono"
	"github.com/go-webengine/engine"
)

// testFont is the theme font files as a host serves them: go-opentype/fonts'.
func testFont(name string) []byte {
	return map[string][]byte{
		"lato/lato.ttf": lato.TTF, "lato/lato-bold.ttf": lato.Bold, "lato/lato-black.ttf": lato.Black,
		"lato/lato-italic.ttf": lato.Italic, "lato/lato-bolditalic.ttf": lato.BoldItalic, "lato/lato-blackitalic.ttf": lato.BlackItalic,
		"robotomono/robotomono.ttf": robotomono.TTF, "robotomono/robotomono-bold.ttf": robotomono.Bold,
		"robotomono/robotomono-italic.ttf": robotomono.Italic, "robotomono/robotomono-bolditalic.ttf": robotomono.BoldItalic,
	}[name]
}

// The theme fonts are fetched when a slide draws them, and only those: a
// deck in the default theme fetches none, one in gaia its Lato and the
// Roboto Mono of its code.
func TestThemeFontsOnDemand(t *testing.T) {
	var mu sync.Mutex
	var asked []string
	prev := fontSource
	defer SetFontSource(prev)
	// fresh faces: other tests may have drawn Lato already, once per process
	defer func(f func() ([]engine.LoadedFontFace, error)) { bundledFaces = f }(bundledFaces)
	bundledFaces = sync.OnceValues(newBundledFaces)
	SetFontSource(func(name string) ([]byte, error) {
		mu.Lock()
		asked = append(asked, name)
		mu.Unlock()
		return testFont(name), nil
	})
	if _, err := Rasterize(context.Background(), build(t, "# Default\n\nText, `code` and *emphasis*.\n"), Options{}); err != nil {
		t.Fatal(err)
	}
	if asked != nil {
		t.Fatalf("the default theme fetched %v", asked)
	}
	gaia := "---\ntheme: gaia\n---\n\n# Gaia\n\nText and `code`.\n"
	if _, err := Rasterize(context.Background(), build(t, gaia), Options{}); err != nil {
		t.Fatal(err)
	}
	slices.Sort(asked)
	if len(asked) == 0 || slices.ContainsFunc(asked, func(n string) bool { return !strings.HasPrefix(n, "lato/") && !strings.HasPrefix(n, "robotomono/") }) {
		t.Fatalf("gaia fetched %v", asked)
	}
	if !slices.Contains(asked, "lato/lato.ttf") || len(slices.Compact(slices.Clone(asked))) != len(asked) {
		t.Fatalf("gaia fetched %v: Lato regular once each", asked)
	}
}

func TestFontNames(t *testing.T) {
	names := FontNames()
	if len(names) != 10 {
		t.Fatalf("%d names", len(names))
	}
	for _, n := range names {
		if testFont(n) == nil {
			t.Errorf("no file %s", n)
		}
	}
	prev := fontSource
	defer SetFontSource(prev)
	SetFontSource(nil)
	if b, err := themeFont("lato/lato.ttf"); string(b) != string(embeddedFont("lato/lato.ttf")) || (b == nil) != (err != nil) {
		t.Fatalf("no source: %d bytes, %v", len(b), err)
	}
	if _, err := themeFont("nope.ttf"); err == nil || !strings.Contains(err.Error(), "SetFontSource") {
		t.Fatalf("an unknown font: %v", err)
	}
}
