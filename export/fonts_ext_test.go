//go:build slides_fontsext

package export

import (
	"errors"
	"os"
	"testing"
)

// Built with slides_fontsext, the exports need a host's source for the
// theme fonts: the tests give them the files a host serves. (go-webengine's
// own bundled fonts stay embedded unless it is built with
// webengine_fontsext.)
func TestMain(m *testing.M) {
	SetFontSource(func(name string) ([]byte, error) {
		if b := testFont(name); b != nil {
			return b, nil
		}
		return nil, errors.New("no " + name)
	})
	os.Exit(m.Run())
}

func TestNothingEmbedded(t *testing.T) {
	for _, n := range FontNames() {
		if embeddedFont(n) != nil {
			t.Fatalf("%s is embedded", n)
		}
	}
}
