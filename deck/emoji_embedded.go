//go:build !slides_noemoji

package deck

// The emoji provider go-slides ships (the Twemoji set, about 2 MB), registered
// with package emoji. A host that must not link it, the playground's
// WebAssembly module, builds with the tag slides_noemoji and registers an
// emoji.Provider of its own, now or once its data has arrived: until then the
// emoji stay as text.
import _ "github.com/go-slides/slides/emoji/embedded"
