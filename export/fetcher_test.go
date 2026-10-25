package export

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

// Options.Fetcher fetches the slides' https: images: a blue picture from it
// is drawn, with no network.
func TestFetcher(t *testing.T) {
	body, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(bluePNG(t), "data:image/png;base64,"))
	if err != nil {
		t.Fatal(err)
	}
	var asked []string
	opts := Options{Fetcher: func(_ context.Context, url string) ([]byte, string, error) {
		asked = append(asked, url)
		if url == "https://img.test/blue.png" {
			return body, "image/png", nil
		}
		return nil, "", errors.New("404")
	}}
	imgs, err := Rasterize(context.Background(), build(t, "![w:200 h:200](https://img.test/blue.png)\n"), opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(asked) == 0 || asked[0] != "https://img.test/blue.png" {
		t.Fatalf("fetched %v", asked)
	}
	blue := 0
	for y := 0; y < imgs[0].Bounds().Dy(); y += 4 {
		for x := 0; x < imgs[0].Bounds().Dx(); x += 4 {
			if c := imgs[0].RGBAAt(x, y); near(c, 0, 0, 255) {
				blue++
			}
		}
	}
	if blue < 100 {
		t.Fatalf("%d blue samples: the fetched picture was not drawn", blue)
	}
}
