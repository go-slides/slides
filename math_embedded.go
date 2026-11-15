//go:build !slides_nomathfont

package slides

// The math font go-slides ships (STIX Two Math, go-tex/math's embedded copy,
// 644 KB gzipped), registered with package mathfont. A host that must not link
// it, the playground's WebAssembly module, builds with the tag
// slides_nomathfont and registers a mathfont.Provider of its own, now or once
// the font has arrived: until then a formula stays its TeX source.
import _ "github.com/go-slides/slides/mathfont/embedded"
