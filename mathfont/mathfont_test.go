package mathfont

import (
	"bytes"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/go-tex/math"
)

func reset(t *testing.T) {
	t.Helper()
	Register(nil)
	t.Cleanup(func() { Register(nil) })
}

// stix is the font go-tex/math embeds, used here as the font a host fetched.
func stix() []byte { return math.DefaultFont() }

type fn func() ([]byte, error)

func (f fn) Font() ([]byte, error) { return f() }

// late is a provider whose font arrives later; it counts how often it is asked.
type late struct {
	mu    sync.Mutex
	font  []byte
	calls atomic.Int32
}

func (l *late) Font() ([]byte, error) {
	l.calls.Add(1)
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.font == nil {
		return nil, errors.New("not loaded yet")
	}
	return l.font, nil
}
func (l *late) load(b []byte) { l.mu.Lock(); l.font = b; l.mu.Unlock() }

func typeset(t *testing.T) error {
	t.Helper()
	r, err := Renderer()
	if err != nil {
		return err
	}
	return Safely(func() error {
		svg, _, err := r.RenderSVGMetrics(`x^2+\frac{a}{b}`, 100)
		if err == nil && !strings.HasPrefix(svg, "<svg") {
			t.Errorf("not an svg: %.40s", svg)
		}
		return err
	})
}

func TestNoProvider(t *testing.T) {
	reset(t)
	if Registered() {
		t.Fatal("Registered with none")
	}
	if _, err := Renderer(); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Renderer with no provider: %v", err)
	}
}

// TestBeforeTheFontArrives is the contract of the package comment: an error from
// Font is "unavailable", is not remembered, and the same provider typesets once
// its font is there, with no other call; Register again gives a new one.
func TestBeforeTheFontArrives(t *testing.T) {
	reset(t)
	l := &late{}
	Register(l)
	for i := 0; i < 3; i++ {
		if err := typeset(t); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("before the font: %v", err)
		}
	}
	if l.calls.Load() != 3 {
		t.Errorf("the provider was asked %d times for 3 formulas: an error must not be remembered", l.calls.Load())
	}
	l.load(stix())
	if err := typeset(t); err != nil {
		t.Fatalf("after the font: %v", err)
	}
	n := l.calls.Load()
	for i := 0; i < 5; i++ {
		if err := typeset(t); err != nil {
			t.Fatal(err)
		}
	}
	if l.calls.Load() != n {
		t.Errorf("the font was fetched %d more times: it is parsed once", l.calls.Load()-n)
	}
	Register(l)
	if err := typeset(t); err != nil || l.calls.Load() != n+1 {
		t.Errorf("after Register again: %v, %d calls", err, l.calls.Load()-n)
	}
	Register(nil)
	if err := typeset(t); !errors.Is(err, ErrUnavailable) {
		t.Errorf("after Register(nil): %v", err)
	}
}

func TestHostileFontsAreRefused(t *testing.T) {
	good := stix()
	cases := map[string][]byte{
		"empty":           {},
		"nil":             nil,
		"too short":       []byte("OTTO"),
		"wrong signature": append([]byte("PK\x03\x04"), good[4:]...),
		"html":            []byte("<!doctype html><html>not found</html>"),
		"over the bound":  append(append([]byte{}, good[:4]...), make([]byte, MaxFontBytes)...),
		"signature only":  append(append([]byte{}, good[:4]...), make([]byte, 64)...),
		"truncated":       good[:len(good)/2],
		"header only":     good[:512],
		"zeroed tables":   append(append([]byte{}, good[:300]...), make([]byte, 4096)...),
	}
	for name, font := range cases {
		reset(t)
		calls := 0
		Register(fn(func() ([]byte, error) { calls++; return font, nil }))
		for i := 0; i < 3; i++ {
			err := typeset(t)
			if err == nil && name != "truncated" {
				t.Errorf("%s: a font accepted", name)
			}
		}
		if err := typeset(t); err != nil && calls != 1 {
			t.Errorf("%s: an unusable font was parsed again (%d calls)", name, calls)
		}
	}
}

func TestProviderPanicIsAnError(t *testing.T) {
	reset(t)
	Register(fn(func() ([]byte, error) { panic("hostile") }))
	if err := typeset(t); !errors.Is(err, ErrUnavailable) {
		t.Errorf("panic: %v", err)
	}
}

func TestSafelyTurnsAPanicIntoAnError(t *testing.T) {
	if err := Safely(func() error { panic("boom") }); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("Safely: %v", err)
	}
	want := errors.New("x")
	if err := Safely(func() error { return want }); err != want {
		t.Error("Safely changed an error")
	}
}

func TestConcurrentRegisterAndUse(t *testing.T) {
	reset(t)
	l := &late{}
	l.load(stix())
	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				_ = typeset(t)
				if i%5 == 0 {
					Register(l)
				}
			}
		}()
	}
	wg.Wait()
}

func TestSignature(t *testing.T) {
	for sig, want := range map[string]bool{"OTTO": true, "\x00\x01\x00\x00": true, "true": true, "ttcf": true, "wOFF": false, "wOF2": false, "\x00\x00\x00\x00": false} {
		if got := signature([]byte(sig + "12345678")); got != want {
			t.Errorf("signature(%q) = %v", sig, got)
		}
	}
}

// FuzzFont: whatever bytes a host hands over, building the renderer and
// typesetting with it returns, with an error or an SVG. The seeds are the real
// font cut and flipped at places that matter: the table directory, the MATH table.
func FuzzFont(f *testing.F) {
	good := stix()
	f.Add([]byte("OTTO"))
	f.Add(good[:1024])
	f.Add(good[:20000])
	mid := append([]byte{}, good[:30000]...)
	for i := 12; i < 400; i += 7 {
		mid[i] ^= 0xFF
	}
	f.Add(mid)
	f.Add(bytes.Repeat([]byte{0}, 64))
	f.Fuzz(func(t *testing.T, font []byte) {
		Register(fn(func() ([]byte, error) { return font, nil }))
		defer Register(nil)
		_ = typeset(t)
	})
}
