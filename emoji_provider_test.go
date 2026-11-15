//go:build !slides_noemoji

package slides

import (
	"strings"
	"sync/atomic"
	"testing"

	"github.com/go-icons/twemoji"

	"github.com/go-slides/slides/emoji"
	"github.com/go-slides/slides/emoji/embedded"
)

// late is a provider whose data arrives later, as a browser host fetches it.
type late struct{ loaded atomic.Bool }

func (l *late) SVGByStem(stem string) (string, bool) {
	if !l.loaded.Load() {
		return "", false
	}
	return twemoji.SVGByStem(stem)
}

// TestProviderBeforeAndAfterTheData: the contract of package emoji, through a
// whole render. Before the data the page holds the text that was written, with
// no warning; after it, and a new render, it holds the images, the same ones the
// embedded provider gives.
func TestProviderBeforeAndAfterTheData(t *testing.T) {
	t.Cleanup(embedded.Register)
	const src = "# Hi :smile: 😄\n\nA :tada: and `code 🎉`.\n"
	want, _, err := HTML([]byte(src), Config{}) // the embedded provider
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(want, `data-marp-twemoji=""/>`); n != 4 {
		t.Fatalf("%d images with the embedded provider, want 4", n)
	}
	l := &late{}
	emoji.Register(l)
	before, b, err := HTML([]byte(src), Config{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(before, `data-marp-twemoji=""/>`) {
		t.Error("an image before the data arrived")
	}
	for _, text := range []string{":smile:", "😄", ":tada:", "🎉"} {
		if !strings.Contains(before, text) {
			t.Errorf("the page before the data lost %q", text)
		}
	}
	if len(b.Warnings) != 0 {
		t.Errorf("warnings before the data: %q", b.Warnings)
	}
	l.loaded.Store(true)
	emoji.Register(l)
	after, _, err := HTML([]byte(src), Config{})
	if err != nil {
		t.Fatal(err)
	}
	if after != want {
		t.Errorf("after the data: %d bytes, the embedded provider's page is %d", len(after), len(want))
	}
}
