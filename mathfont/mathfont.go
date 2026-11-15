// Package mathfont is where the formulas get their font: the OpenType MATH font
// go-tex/math typesets with, which is STIX Two Math, 838,508 bytes (643,844
// gzipped, measured on 2026-11-15). The bytes are a Provider's. Package
// mathfont/embedded is the one go-slides ships (go-tex/math's own copy), and
// the slides command links it. A host that must not link it (a WebAssembly
// page, which fetches the font when a deck has a formula) builds slides with
// the tag slides_nomathfont and registers a Provider of its own with Register,
// now or later. go-tex/math itself takes the bytes (math.New); nothing there
// has to change.
//
// # Before the font arrives
//
// Rendering is synchronous and never waits. With no Provider registered, or
// one whose Font returns an error because the bytes are not there yet, a
// formula is not typeset: the deck package writes its TeX source as written,
// in <span class="math-error" title="...">, with the error as the title (the
// way a formula that does not parse is written), and the render does not fail.
// When the font has arrived the host registers the Provider again (Register)
// and renders the deck again: the formulas are typeset on that render. A Font
// error is not remembered: the next formula asks again, so that a Provider
// that loads on first use works with no other call.
//
// # What a Provider may be
//
// A Provider is untrusted, and so are the bytes it returns, which a host
// fetches from the network: Font's answer is refused unless it is at most
// MaxFontBytes and starts with an OpenType, TrueType or collection signature;
// a font go-tex/math cannot read is an error, kept (the bytes are not parsed
// again) until the next Register; a panic in a Provider, or in typesetting with
// its font, is an error. The font is parsed once per Register, and the parsed
// renderer is shared by all goroutines, as go-tex/math's is.
package mathfont

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/go-tex/math"
)

// MaxFontBytes is the largest font accepted: ten times STIX Two Math.
const MaxFontBytes = 8 << 20

// ErrUnavailable is what Renderer returns when there is no usable font: no
// Provider is registered, its Font returned an error, or the font it returned
// is unusable (empty, too large, not OpenType, unreadable). errors.Is sees it
// in each case; a new Register may change the answer.
var ErrUnavailable = errors.New("mathfont: the math font is not available")

// A Provider holds the font's bytes.
type Provider interface {
	// Font returns an OpenType MATH font (a .otf or .ttf with a MATH table), or an
	// error when it is not there (yet). It is called from several goroutines, and
	// again after an error, but once its answer has been used, not again until
	// the next Register. The bytes must not change once returned.
	Font() ([]byte, error)
}

type state struct {
	p   Provider
	mu  sync.Mutex
	r   *math.Renderer
	bad error // the font was returned, and is unusable: not asked again
}

var (
	current atomic.Pointer[state]
	gen     atomic.Uint64
)

// Register makes p the Provider formulas are typeset with, from the next
// formula on, and drops the font the one before gave. A nil p leaves none. It
// may be called at any time, from any goroutine.
func Register(p Provider) {
	if p == nil {
		current.Store(nil)
	} else {
		current.Store(&state{p: p})
	}
	gen.Add(1) // after the store: a reader of the new count sees the new state
}

// Generation counts the calls of Register. A cache of renders (deck.RenderCache)
// reads it before it renders and keeps the value with a render that fell back
// for lack of the font (Unavailable), to render it again once the count has
// moved.
func Generation() uint64 { return gen.Load() }

// Unavailable reports whether err says the font is not there: no Provider, one
// that has no font, or one whose font is unusable. A new Register may change it.
func Unavailable(err error) bool { return errors.Is(err, ErrUnavailable) }

// unusable is the error of a font that was returned and cannot be used: its
// message is the cause's, and it is Unavailable.
type unusable struct{ error }

func (unusable) Is(target error) bool { return target == ErrUnavailable }
func (u unusable) Unwrap() error      { return u.error }

// Registered reports whether a Provider is registered, not whether it has the
// font.
func Registered() bool { return current.Load() != nil }

// Renderer is the typesetter built from the registered Provider's font, built
// once per Register. Its error wraps ErrUnavailable when there is no font to
// build it from.
func Renderer() (*math.Renderer, error) {
	s := current.Load()
	if s == nil {
		return nil, fmt.Errorf("%w: no provider is registered", ErrUnavailable)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.r != nil {
		return s.r, nil
	}
	if s.bad != nil {
		return nil, s.bad
	}
	font, err := fontOf(s.p)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	r, err := build(font)
	if err != nil {
		s.bad = unusable{err}
		return nil, s.bad
	}
	s.r = r
	return r, nil
}

func fontOf(p Provider) (font []byte, err error) {
	defer func() {
		if v := recover(); v != nil {
			font, err = nil, fmt.Errorf("provider panicked: %v", v)
		}
	}()
	return p.Font()
}

func build(font []byte) (r *math.Renderer, err error) {
	if len(font) == 0 {
		return nil, errors.New("mathfont: the font is empty")
	}
	if len(font) > MaxFontBytes {
		return nil, fmt.Errorf("mathfont: the font is %d bytes, over the %d allowed", len(font), MaxFontBytes)
	}
	if !signature(font) {
		return nil, errors.New("mathfont: the bytes are not an OpenType font")
	}
	defer func() {
		if v := recover(); v != nil {
			r, err = nil, fmt.Errorf("mathfont: the font cannot be read: %v", v)
		}
	}()
	r, err = math.New(font)
	if err != nil {
		return nil, fmt.Errorf("mathfont: the font cannot be read: %w", err)
	}
	return r, nil
}

// signature: "OTTO" (CFF outlines), 00 01 00 00 or "true" (TrueType outlines),
// "ttcf" (a collection).
func signature(b []byte) bool {
	if len(b) < 12 {
		return false
	}
	switch string(b[:4]) {
	case "OTTO", "\x00\x01\x00\x00", "true", "ttcf":
		return true
	}
	return false
}

// Safely runs f and turns a panic in it, which a font that parses but is
// hostile can cause while typesetting, into an error.
func Safely(f func() error) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("mathfont: typesetting with this font failed: %v", v)
		}
	}()
	return f()
}
