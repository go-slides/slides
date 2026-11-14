// Package video reads what a deck needs of a video file: its container, its
// codec and size, and one picture of it (the poster). The file is untrusted:
// its size, the number and nesting of its boxes or elements, the size of its
// frames and the work of decoding the poster are bounded, and a refusal carries
// a Code. It imports nothing of go-slides, and no decoder.
//
// What needs a decoder is behind a Provider: the container's tracks and the
// poster's decoding. Package video/avkit is the one go-slides ships (go-avkit's
// container reader and H.264 decoder); the slides command links it. A host
// that must not link it (the playground's WebAssembly module, which runs the
// provider in a worker of its own) builds slides with the tag slides_novideo
// and registers a Provider of its own with Register. The checks that bound a
// file (its size, its boxes, its frame) stay here, so that they hold for every
// provider and run before one is asked anything.
//
// Containers read: MP4 (ISO/IEC 14496-12) and WebM / Matroska. The poster is
// decoded for H.264 (avc1, avc3) only, because that is the one video decoder
// of go-avkit; any other codec is Info-readable and plays where it is embedded,
// but has no picture (CodeCodec).
package video

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"sync"
)

// The codes of a refusal.
const (
	CodeLimit  = "video.limit"
	CodeSyntax = "video.syntax"
	CodeCodec  = "video.codec"
	// CodeUnavailable: no Provider is registered (a build with slides_novideo
	// and a host that registered none), or the host's could not be reached.
	CodeUnavailable = "video.unavailable"
)

// Error is a refusal with the code that names it.
type Error struct {
	Code string
	Msg  string
}

func (e *Error) Error() string { return e.Msg }

// Fail is an *Error with a formatted message: what a Provider returns.
func Fail(code, format string, a ...any) error { return fail(code, format, a...) }

func fail(code, format string, a ...any) error {
	return &Error{Code: code, Msg: fmt.Sprintf(format, a...)}
}

// Limits bound what a file may cost.
type Limits struct {
	// MaxBytes is the largest file read.
	MaxBytes int
	// MaxBoxes is the most boxes (MP4) or elements (EBML) a file may hold, and
	// MaxDepth how deeply they may nest.
	MaxBoxes, MaxDepth int
	// MaxWidth and MaxHeight bound the frame, in pixels.
	MaxWidth, MaxHeight int
	// MaxSamples is the most samples decoded for one poster (a keyframe far
	// from the time asked for is refused); MaxTrackSamples the most a track may
	// list; MaxSampleBytes the largest sample.
	MaxSamples, MaxTrackSamples, MaxSampleBytes int
	// PosterWidth is the width the poster is scaled down to (never up).
	PosterWidth int
}

// DefaultLimits are what go-slides reads.
var DefaultLimits = Limits{
	MaxBytes: 64 << 20, MaxBoxes: 1 << 18, MaxDepth: 16,
	MaxWidth: 4096, MaxHeight: 4096,
	MaxSamples: 600, MaxTrackSamples: 1 << 18, MaxSampleBytes: 8 << 20,
	PosterWidth: 1280,
}

// Info describes the file.
type Info struct {
	// Format is "mp4" or "webm" (Matroska included).
	Format string
	// MIME is the media type of the file as it is embedded.
	MIME string
	// Codec is the video track's sample entry or codec id (avc1, V_VP9...);
	// Width and Height its size, Duration the file's in seconds.
	Codec         string
	Width, Height int
	Duration      float64
	// Decodable says the poster can be decoded for Codec.
	Decodable bool
}

// Probed is what a Provider says of a file: the video track's description,
// and State, anything the provider wants handed back to its Poster (a parsed
// container, so that the file is read once).
type Probed struct {
	Codec         string
	Width, Height int
	Duration      float64
	Decodable     bool
	State         any
}

// A Provider reads the container and decodes the poster. data is a file that
// passed the checks of Open: its size, its boxes or elements and their nesting
// are bounded, and it is an MP4 or a WebM by its first bytes. A refusal is an
// *Error (Fail); any other error is taken as video.syntax. A Provider is called
// from several goroutines.
type Provider interface {
	// Probe describes the file's video track. A file with none, or whose
	// container cannot be read, is video.syntax; a frame over the limits is
	// video.limit (Open checks the frame again).
	Probe(data []byte, format string, lim Limits) (Probed, error)
	// Poster is the picture shown at t seconds (the one before it, if none is
	// exactly at t; the last one when t is past the end), scaled down to
	// lim.PosterWidth, encoded as JPEG, with its size. p is what Probe returned
	// for this data.
	Poster(p Probed, data []byte, t float64, lim Limits) (jpg []byte, w, h int, err error)
}

var (
	regMu    sync.RWMutex
	registry Provider
)

// Register sets the Provider that Open and File.Poster use (nil: none). The
// avkit package registers itself when imported.
func Register(p Provider) {
	regMu.Lock()
	registry = p
	regMu.Unlock()
}

func provider() Provider {
	regMu.RLock()
	defer regMu.RUnlock()
	return registry
}

// File is a video file checked against the limits, and described.
type File struct {
	Info
	data   []byte
	probed Probed
	prov   Provider
}

// Open checks a file against the limits and reads its description with the
// registered Provider. Nothing is decoded.
func Open(data []byte, lim Limits) (*File, error) {
	return OpenWith(provider(), data, lim)
}

// OpenWith is Open with a Provider of the caller's (nil: none), whatever is
// registered: a worker that serves another host's requests reads with its own,
// never with the one that asks it.
func OpenWith(p Provider, data []byte, lim Limits) (*File, error) {
	if len(data) > lim.MaxBytes {
		return nil, fail(CodeLimit, "the file is %d bytes; at most %d are read", len(data), lim.MaxBytes)
	}
	var info Info
	switch {
	case len(data) >= 12 && string(data[4:8]) == "ftyp":
		info.Format, info.MIME = "mp4", "video/mp4"
		if err := scanBoxes(data, lim); err != nil {
			return nil, err
		}
	case len(data) >= 4 && bytes.Equal(data[:4], []byte{0x1A, 0x45, 0xDF, 0xA3}):
		info.Format, info.MIME = "webm", "video/webm"
		if err := scanEBML(data, lim); err != nil {
			return nil, err
		}
	default:
		return nil, fail(CodeSyntax, "not an MP4 or WebM file")
	}
	if p == nil {
		return nil, fail(CodeUnavailable, "this build reads no video: no container reader is registered")
	}
	pr, err := p.Probe(data, info.Format, lim)
	if err != nil {
		return nil, asError(err)
	}
	info.Codec, info.Width, info.Height, info.Duration, info.Decodable = pr.Codec, pr.Width, pr.Height, pr.Duration, pr.Decodable
	if info.Width <= 0 || info.Height <= 0 || info.Width > lim.MaxWidth || info.Height > lim.MaxHeight {
		return nil, fail(CodeLimit, "the frame is %dx%d; at most %dx%d are read", info.Width, info.Height, lim.MaxWidth, lim.MaxHeight)
	}
	return &File{Info: info, data: data, probed: pr, prov: p}, nil
}

// asError makes a provider's error one that carries a code.
func asError(err error) error {
	if e, ok := err.(*Error); ok {
		return e
	}
	return fail(CodeSyntax, "%v", err)
}

// Poster decodes the picture shown at t seconds, scaled down to the limit's
// PosterWidth, and encodes it as JPEG.
func (i *File) Poster(t float64, lim Limits) (jpg []byte, w, h int, err error) {
	if !i.Decodable {
		return nil, 0, 0, fail(CodeCodec, "the poster of a %s video cannot be decoded (H.264 only): a neutral picture stands in", i.Codec)
	}
	jpg, w, h, err = i.prov.Poster(i.probed, i.data, t, lim)
	if err != nil {
		return nil, 0, 0, asError(err)
	}
	return jpg, w, h, nil
}

// Placeholder is the picture that stands in for a poster that cannot be
// decoded: a dark card with a play triangle, at the size of the video (scaled
// to the poster width).
func (i *File) Placeholder(lim Limits) (jpg []byte, w, h int) {
	return PlaceholderSize(i.Width, i.Height, lim)
}

// PlaceholderSize is Placeholder for a video of a known size (a remote one is
// taken as 16:9).
func PlaceholderSize(w, h int, lim Limits) (jpg []byte, ow, oh int) {
	if w > lim.PosterWidth {
		h, w = h*lim.PosterWidth/w, lim.PosterWidth
	}
	w, h = max(w, 16), max(h, 16)
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	bg, fg := color.RGBA{0x20, 0x24, 0x2b, 0xff}, color.RGBA{0xf0, 0xf0, 0xf0, 0xff}
	r := min(w, h) / 5
	cx, cy := w/2, h/2
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := bg
			// a triangle pointing right: x from cx-r/2 to cx+r, |y-cy| < (cx+r-x)/2
			x0, x1 := cx-r/2, cx+r
			if x >= x0 && x <= x1 && abs(y-cy)*(x1-x0)*10 <= r*9*(x1-x) {
				c = fg
			}
			img.SetRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85})
	return buf.Bytes(), w, h
}

func abs(a int) int {
	if a < 0 {
		return -a
	}
	return a
}

// mp4Containers are the boxes whose children are boxes (ISO/IEC 14496-12:
// moov, trak, mdia, minf, stbl, dinf, edts, udta, mvex, moof, traf, mfra, sinf,
// schi). meta, a full box, holds boxes after four bytes.
var mp4Containers = map[string]bool{
	"moov": true, "trak": true, "mdia": true, "minf": true, "stbl": true, "dinf": true, "edts": true,
	"udta": true, "mvex": true, "moof": true, "traf": true, "mfra": true, "sinf": true, "schi": true, "meta": true,
}

// scanBoxes walks the boxes without decoding any: it counts them and measures
// their nesting, so that a file made of millions of empty boxes is refused
// before the container reader builds a tree of them.
func scanBoxes(data []byte, lim Limits) error {
	count := 0
	var walk func(b []byte, depth int) error
	walk = func(b []byte, depth int) error {
		if depth > lim.MaxDepth {
			return fail(CodeLimit, "boxes nest deeper than %d", lim.MaxDepth)
		}
		for len(b) >= 8 {
			if count++; count > lim.MaxBoxes {
				return fail(CodeLimit, "the file holds more than %d boxes", lim.MaxBoxes)
			}
			size, hdr := uint64(binary.BigEndian.Uint32(b)), 8
			typ := string(b[4:8])
			if size == 1 {
				if len(b) < 16 {
					return nil
				}
				size, hdr = binary.BigEndian.Uint64(b[8:]), 16
			} else if size == 0 {
				size = uint64(len(b))
			}
			if size < uint64(hdr) || size > uint64(len(b)) {
				return nil // truncated or malformed: the container reader judges
			}
			if mp4Containers[typ] {
				body := b[hdr:size]
				if typ == "meta" {
					if len(body) < 4 {
						body = nil
					} else {
						body = body[4:]
					}
				}
				if err := walk(body, depth+1); err != nil {
					return err
				}
			}
			b = b[size:]
		}
		return nil
	}
	return walk(data, 1)
}

// ebmlMasters are the elements whose children are elements: every element of
// type master in the EBML and Matroska element tables. A hand-made list missed
// some (BlockAdditionMapping, Slices, BlockMore...), and a laced block nested in
// one escaped the frame count; the set is checked against ebml-go's own table
// (masters_test.go), which is where it comes from.
var ebmlMasters = map[uint32]bool{
	// EBML header and Segment level
	0x1A45DFA3: true, 0x18538067: true, 0x114D9B74: true, 0x4DBB: true, 0x1549A966: true, 0x6924: true,
	// Cluster
	0x1F43B675: true, 0x5854: true, 0xA0: true, 0x75A1: true, 0xA6: true, 0x8E: true,
	// Tracks
	0x1654AE6B: true, 0xAE: true, 0x41E4: true, 0xE0: true, 0x55B0: true, 0x55D0: true, 0x7670: true,
	0xE1: true, 0xE2: true, 0xE3: true, 0xE4: true, 0xE8: true, 0xE9: true,
	0x6624: true, 0x6D80: true, 0x6240: true, 0x5034: true, 0x5035: true, 0x47E7: true,
	// Cues, Attachments, Chapters, Tags
	0x1C53BB6B: true, 0xBB: true, 0xB7: true, 0xDB: true, 0x1941A469: true, 0x61A7: true,
	0x1043A770: true, 0x45B9: true, 0xB6: true, 0x80: true, 0x8F: true, 0x6944: true, 0x6911: true,
	0x1254C367: true, 0x7373: true, 0x63C0: true, 0x67C8: true,
}

func ebmlVint(b []byte, keepMarker bool) (v uint64, n int) {
	if len(b) == 0 || b[0] == 0 {
		return 0, 0
	}
	n = 1
	for m := byte(0x80); b[0]&m == 0; m >>= 1 {
		n++
	}
	if n > len(b) || n > 8 {
		return 0, 0
	}
	v = uint64(b[0])
	if !keepMarker {
		v &= 0xFF >> uint(n)
	}
	for _, c := range b[1:n] {
		v = v<<8 | uint64(c)
	}
	return v, n
}

// The elements that hold frames: SimpleBlock and Block (in a BlockGroup).
const (
	idSimpleBlock = 0xA3
	idBlock       = 0xA1
)

// blockFrames is how many frames a block holds (Matroska, Block Structure): its
// track number (a vint), a 16-bit timecode, the flags, whose bits 1 and 2 name
// the lacing (0 none; Xiph, fixed-size and EBML otherwise), and for a laced
// block one byte, the number of frames less one. A block too short to say holds
// one, which the container reader refuses or reads as it will.
func blockFrames(b []byte) int {
	_, n := ebmlVint(b, false)
	if n == 0 || len(b) < n+4 {
		return 1
	}
	if b[n+2]>>1&3 == 0 {
		return 1
	}
	return int(b[n+3]) + 1
}

// scanEBML counts and nests the elements as scanBoxes does the boxes.
func scanEBML(data []byte, lim Limits) error {
	count, frames := 0, 0
	var walk func(b []byte, depth int) error
	walk = func(b []byte, depth int) error {
		if depth > lim.MaxDepth {
			return fail(CodeLimit, "elements nest deeper than %d", lim.MaxDepth)
		}
		for len(b) > 0 {
			if count++; count > lim.MaxBoxes {
				return fail(CodeLimit, "the file holds more than %d elements", lim.MaxBoxes)
			}
			id, n := ebmlVint(b, true)
			if n == 0 {
				return nil
			}
			size, m := ebmlVint(b[n:], false)
			if m == 0 {
				return nil
			}
			body := b[n+m:]
			unknown := size == (uint64(1)<<(7*uint(m)))-1
			if unknown || size > uint64(len(body)) {
				size = uint64(len(body)) // unknown or cut short: to the end
			}
			if ebmlMasters[uint32(id)] {
				if err := walk(body[:size], depth+1); err != nil {
					return err
				}
			} else if id == idSimpleBlock || id == idBlock {
				// a block holds as many frames as its lacing says, and the container
				// reader unlaces every block up front: they are counted here, before
				// any is, as the samples a track may list
				frames += blockFrames(body[:size])
				if frames > lim.MaxTrackSamples {
					return fail(CodeLimit, "the file holds more than %d frames", lim.MaxTrackSamples)
				}
			}
			b = body[size:]
		}
		return nil
	}
	return walk(data, 1)
}
