//go:build !slides_fontsext

package export

import "testing"

// The embedded files are go-opentype/fonts' own.
func TestFontData(t *testing.T) {
	for _, n := range FontNames() {
		if b := FontData(n); b == nil || string(b) != string(testFont(n)) {
			t.Fatalf("%s: %d bytes", n, len(b))
		}
	}
	if FontData("inter/inter.ttf") != nil {
		t.Fatal("a font the exports do not bundle")
	}
}
