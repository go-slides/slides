//go:build !slides_noemoji

package embedded_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/go-icons/twemoji"

	"github.com/go-slides/slides/emoji"
	_ "github.com/go-slides/slides/emoji/embedded"
)

// The embedded provider and the parsing that stays in package emoji must read
// text exactly as go-icons/twemoji does: that is what makes the default build's
// output the bytes it was before the providers.

func tails() []string {
	return []string{"", " x", "️", "︎", "‍", "é", "1", "😄", "⃣", "️️"}
}

func TestEveryStemCutsLikeTwemoji(t *testing.T) {
	if !emoji.Registered() {
		t.Fatal("the embedded provider did not register")
	}
	n := 0
	for _, stem := range twemoji.Stems() {
		var b strings.Builder
		for _, h := range strings.Split(stem, "-") {
			var v int
			for _, c := range h {
				v = v*16 + strings.IndexRune("0123456789abcdef", c)
			}
			b.WriteRune(rune(v))
		}
		seq := b.String()
		variants := []string{seq, strings.ReplaceAll(seq, "‍", "‍️"), seq + "️", "️" + seq}
		for _, v := range variants {
			for _, tail := range tails() {
				s := v + tail
				if got, want := emoji.Cut(s), twemoji.Cut(s); got != want {
					t.Fatalf("Cut(%q) = %d, twemoji %d", s, got, want)
				}
				n++
			}
		}
		if got, want := emoji.Src(seq), twemoji.Lookup(seq); (got == "") != (want == "") {
			t.Fatalf("Src(%q) = %d bytes, twemoji stem %q", seq, len(got), want)
		}
	}
	t.Logf("%d texts agree", n)
}

func TestShortcodesLikeTwemoji(t *testing.T) {
	names := twemoji.Shortcodes()
	got := emoji.Shortcodes()
	if len(got) != len(names) {
		t.Fatalf("%d shortcodes, twemoji %d", len(got), len(names))
	}
	for i, name := range names {
		if got[i] != name {
			t.Fatalf("shortcode %d: %q, twemoji %q", i, got[i], name)
		}
		a, aok := emoji.Shortcode(name)
		b, bok := twemoji.Shortcode(name)
		if a != b || aok != bok {
			t.Fatalf("Shortcode(%q) = %q, twemoji %q", name, a, b)
		}
	}
	for _, name := range []string{"", "zzz", "smile ", "SMILE", "+1", "-1", "100"} {
		a, aok := emoji.Shortcode(name)
		b, bok := twemoji.Shortcode(name)
		if a != b || aok != bok {
			t.Errorf("Shortcode(%q) = %q %v, twemoji %q %v", name, a, aok, b, bok)
		}
	}
	for _, s := range []string{"", "no colon", ":a:smile:", ":smile::tada:", "x :+1: y :", ":::", ":heart_eyes:", "a:b:c"} {
		f := func(name, seq string) string { return "[" + name + "=" + seq + "]" }
		if a, b := emoji.Replace(s, f), twemoji.Replace(s, f); a != b {
			t.Errorf("Replace(%q) = %q, twemoji %q", s, a, b)
		}
	}
}

func TestTextDefaultLikeTwemoji(t *testing.T) {
	for r := rune(0); r < 0x1FFFF; r++ {
		if emoji.TextDefault(r) != twemoji.TextDefault(r) {
			t.Fatalf("TextDefault(%U) differs", r)
		}
	}
}

// FuzzCutLikeTwemoji: on any text, the provider-driven Cut is twemoji's.
func FuzzCutLikeTwemoji(f *testing.F) {
	for _, s := range []string{"😄", "👨‍👩‍👧 x", "1️⃣", "🇫🇷🇫", "✔️", "❤︎", "é😄", "\xff", "#⃣", "*️⃣", "\U000E0067", ""} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if got, want := emoji.Cut(s), twemoji.Cut(s); got != want {
			t.Fatalf("Cut(%q) = %d, twemoji %d", s, got, want)
		}
		if n := emoji.Cut(s); n > 0 && utf8.ValidString(s) && !utf8.ValidString(s[:n]) {
			t.Fatalf("Cut(%q) = %d cuts a character", s, n)
		}
	})
}

// BenchmarkCutText: prose with accents, which is what Cut mostly sees.
func BenchmarkCutText(b *testing.B) {
	text := "Présentation à l'école: où êtes-vous allés cet été? Ça va très bien, merci. 😄"
	b.Run("emoji", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			for j := 0; j < len(text); j++ {
				emoji.Cut(text[j:])
			}
		}
	})
	b.Run("twemoji", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			for j := 0; j < len(text); j++ {
				twemoji.Cut(text[j:])
			}
		}
	})
}

// TestStemsTableIsTheSet: emoji/stems.txt is the set the embedded provider
// holds; after raising go-icons/twemoji run `go run ./internal/genstems emoji/stems.txt`.
func TestStemsTableIsTheSet(t *testing.T) {
	a, b := emoji.Stems(), twemoji.Stems()
	if len(a) != len(b) {
		t.Fatalf("%d stems in the table, %d in the set", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("stem %d: %q in the table, %q in the set", i, a[i], b[i])
		}
		if _, ok := twemoji.SVGByStem(a[i]); !ok {
			t.Fatalf("no SVG for %q", a[i])
		}
	}
}
