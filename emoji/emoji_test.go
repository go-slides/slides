package emoji

import (
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

const smallSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1 1"><rect width="1" height="1"/></svg>`

// reset leaves no Provider registered after the test.
func reset(t *testing.T) {
	t.Helper()
	Register(nil)
	t.Cleanup(func() { Register(nil) })
}

type fn func(stem string) (string, bool)

func (f fn) SVGByStem(stem string) (string, bool) { return f(stem) }

func always(svg string) Provider { return fn(func(string) (string, bool) { return svg, true }) }

// lazy is a provider whose data arrives later: it answers "none" until load.
type lazy struct {
	loaded atomic.Bool
	calls  atomic.Int32
}

func (l *lazy) SVGByStem(stem string) (string, bool) {
	l.calls.Add(1)
	if !l.loaded.Load() {
		return "", false
	}
	return smallSVG, true
}

// TestNoProvider: with none registered nothing has a picture, and the tables
// that stay in the binary still answer, Cut included.
func TestNoProvider(t *testing.T) {
	reset(t)
	if Registered() {
		t.Fatal("Registered with none")
	}
	if Src("😄") != "" {
		t.Error("a picture with no provider")
	}
	if Cut("😄 text") != len("😄") {
		t.Error("Cut reads the tables, not the provider")
	}
	if seq, ok := Shortcode("smile"); !ok || seq != "😄" {
		t.Errorf("Shortcode(smile) = %q, %v", seq, ok)
	}
	if got := Replace("a :smile: b", func(_, seq string) string { return seq }); got != "a 😄 b" {
		t.Errorf("Replace = %q", got)
	}
	if !TextDefault('©') || TextDefault('😄') {
		t.Error("TextDefault")
	}
	if n := len(Stems()); n < 4000 {
		t.Errorf("%d stems", n)
	}
}

// TestCutNeverCallsTheProvider: recognition is the package's tables.
func TestCutNeverCallsTheProvider(t *testing.T) {
	reset(t)
	l := &lazy{}
	Register(l)
	for _, s := range []string{"😄 x", "🇫🇷", "1️⃣", "é", "plain"} {
		Cut(s)
	}
	if l.calls.Load() != 0 {
		t.Errorf("Cut asked the provider %d times", l.calls.Load())
	}
}

// TestBeforeDataArrives is the contract of the package comment: a provider with
// no data yet answers "none", which is text; once its data has arrived and the
// provider is registered again, the same call gives the picture.
func TestBeforeDataArrives(t *testing.T) {
	reset(t)
	l := &lazy{}
	Register(l)
	if !Registered() {
		t.Fatal("not Registered")
	}
	if Src("😄") != "" {
		t.Fatal("a picture before the data")
	}
	l.loaded.Store(true)
	Register(l) // the host says: the data is here
	src := Src("😄")
	if !strings.HasPrefix(src, "data:image/svg+xml;base64,") {
		t.Fatalf("Src after load = %q", src)
	}
	if b, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(src, "data:image/svg+xml;base64,")); err != nil || string(b) != smallSVG {
		t.Errorf("the image is not the provider's SVG: %v", err)
	}
	if Src("not an emoji") != "" || Src("") != "" {
		t.Error("a graphic the set lacks")
	}
	Register(nil)
	if Src("😄") != "" {
		t.Error("a picture after Register(nil)")
	}
}

// TestRegisterDropsTheCache: a new provider's answer for a stem is not the old one's.
func TestRegisterDropsTheCache(t *testing.T) {
	reset(t)
	Register(always(smallSVG))
	a := Src("😄")
	Register(always(`<svg id="two"/>`))
	b := Src("😄")
	if a == b || b == "" {
		t.Errorf("the cache outlived the provider: %q %q", a, b)
	}
}

func TestFE0FIsIgnored(t *testing.T) {
	reset(t)
	Register(always(smallSVG))
	if a, b := Src("😄"), Src("😄️"); a == "" || a != b {
		t.Errorf("Src with and without U+FE0F differ: %d %d", len(a), len(b))
	}
}

func TestCut(t *testing.T) {
	reset(t)
	for _, c := range []struct{ in, want string }{
		{"😄x", "😄"},
		{"😄️x", "😄"},
		{"🇫🇷x", "🇫🇷"},
		{"🇫x", ""},
		{"👨‍👩‍👧x", "👨‍👩‍👧"},
		{"1️⃣x", "1️⃣"},
		{"1x", ""},
		{"❤x", "❤"},
		{"✔x", ""},
		{"✔️x", "✔️"},
		{"©x", ""},
		{"😄︎x", ""},
		{"éa", ""},
		{"", ""},
	} {
		if got := Cut(c.in); got != len(c.want) || c.in[:got] != c.want {
			t.Errorf("Cut(%q) = %q, want %q", c.in, c.in[:got], c.want)
		}
	}
}

// TestCutAllocatesNothing: Cut is on the path of every non-ASCII character.
func TestCutAllocatesNothing(t *testing.T) {
	Cut("😄") // loads the tables
	for _, s := range []string{"é plain", "😄 x", "👨‍👩‍👧 x", "1️⃣", "✔ x"} {
		if n := testing.AllocsPerRun(100, func() { Cut(s) }); n != 0 {
			t.Errorf("Cut(%q) allocates %v", s, n)
		}
	}
}

// ---- a hostile provider ----

type panicky struct{}

func (panicky) SVGByStem(string) (string, bool) { panic("hostile") }

func TestHostileAnswersAreBounded(t *testing.T) {
	reset(t)
	for name, p := range map[string]Provider{
		"huge svg":        always(strings.Repeat("<svg>", MaxSVGBytes)),
		"svg just over":   always("<svg>" + strings.Repeat("x", MaxSVGBytes)),
		"not an svg":      always("<html><script>alert(1)</script></html>"),
		"empty svg":       always(""),
		"svg said absent": fn(func(string) (string, bool) { return smallSVG, false }),
		"panic":           panicky{},
	} {
		Register(p)
		if got := Src("😄"); got != "" {
			t.Errorf("%s: Src = %d bytes, want none", name, len(got))
		}
		if Cut("😄 x") != len("😄") {
			t.Errorf("%s: a hostile provider changed what Cut reads", name)
		}
	}
}

// TestBiggestAcceptedSVG: the bound is inclusive, and an accepted image is the
// provider's bytes, base64.
func TestBiggestAcceptedSVG(t *testing.T) {
	reset(t)
	svg := "<svg>" + strings.Repeat("x", MaxSVGBytes-len("<svg>"))
	Register(always(svg))
	if got := Src("😄"); len(got) != len("data:image/svg+xml;base64,")+base64.StdEncoding.EncodedLen(len(svg)) {
		t.Errorf("Src = %d bytes", len(got))
	}
}

// TestCacheIsBounded: a provider cannot grow what is kept between renders past
// MaxCachedImages and MaxCachedBytes, with small images or big ones.
func TestCacheIsBounded(t *testing.T) {
	stems := Stems()
	stemSeq := func(i int) string { // the sequence of a stem
		var b strings.Builder
		for _, h := range strings.Split(stems[i], "-") {
			var v rune
			fmt.Sscanf(h, "%x", &v)
			b.WriteRune(v)
		}
		return b.String()
	}
	for _, c := range []struct {
		name string
		svg  string
	}{{"small", "<svg>" + strings.Repeat("y", 40)}, {"big", "<svg>" + strings.Repeat("z", MaxSVGBytes-8)}} {
		reset(t)
		Register(always(c.svg))
		st := current.Load()
		for i := range stems {
			if Src(stemSeq(i)) == "" {
				t.Fatalf("%s: no image for %s", c.name, stems[i])
			}
		}
		if n := st.n.Load(); n > MaxCachedImages {
			t.Errorf("%s: %d images kept, bound %d", c.name, n, MaxCachedImages)
		}
		if b := st.bytes.Load(); b > MaxCachedBytes || b == 0 {
			t.Errorf("%s: %d bytes kept, bound %d", c.name, b, MaxCachedBytes)
		}
	}
}

// TestConcurrentRegister: Register while the render runs is not a race.
func TestConcurrentRegister(t *testing.T) {
	reset(t)
	a, b := always(smallSVG), always(`<svg id="b"/>`)
	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				_ = Src("😄")
				_ = Cut("🎉 x")
				if i%50 == 0 {
					Register([]Provider{a, b, nil}[i/50%3])
				}
			}
		}()
	}
	wg.Wait()
}

// ---- fuzz ----

// FuzzHostileProvider: whatever a provider answers, Src is "" or a data URI of
// a bounded SVG, and nothing panics.
func FuzzHostileProvider(f *testing.F) {
	f.Add(smallSVG, true)
	f.Add("", false)
	f.Add("<svg", true)
	f.Add("<?xml version='1.0'?><svg/>", true)
	f.Add("\xef\xbb\xbf  \n<svg/>", true)
	f.Fuzz(func(t *testing.T, svg string, ok bool) {
		Register(fn(func(string) (string, bool) { return svg, ok }))
		defer Register(nil)
		got := Src("😄")
		if got == "" {
			return
		}
		raw, found := strings.CutPrefix(got, "data:image/svg+xml;base64,")
		if !found {
			t.Fatalf("Src = %q", got)
		}
		b, err := base64.StdEncoding.DecodeString(raw)
		if err != nil || len(b) > MaxSVGBytes || string(b) != svg || !validSVG(svg) || !ok {
			t.Fatalf("image out of bounds or altered: %v %d", err, len(b))
		}
	})
}

// FuzzCutBounds: Cut on any text returns a boundary inside s.
func FuzzCutBounds(f *testing.F) {
	f.Add("😄 text")
	f.Add("1️⃣")
	f.Add("\xff\xfe😄")
	f.Fuzz(func(t *testing.T, s string) {
		if n := Cut(s); n < 0 || n > len(s) {
			t.Fatalf("Cut(%q) = %d", s, n)
		}
	})
}
