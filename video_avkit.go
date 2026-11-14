//go:build !slides_novideo

package slides

// The video provider go-slides ships (go-avkit's container reader and H.264
// decoder), registered with package video. A host that must not link it, the
// playground's WebAssembly module, builds with the tag slides_novideo and
// registers a video.Provider of its own: without one, a video in a deck is
// refused with video.unavailable and a deck without video renders as before.
import _ "github.com/go-slides/slides/video/avkit"
