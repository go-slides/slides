// Package video reads what a deck needs of a video file: its container, its
// codec and size, and one picture of it (the poster). The file is untrusted:
// its size, the number and nesting of its boxes or elements, the size of its
// frames and the work of decoding the poster are bounded, and a refusal carries
// a Code. It imports nothing of go-slides.
//
// Containers read: MP4 (ISO/IEC 14496-12) and WebM / Matroska. The poster is
// decoded for H.264 (avc1, avc3) only, because that is the one video decoder
// of go-avkit; any other codec is Info-readable and plays where it is embedded,
// but has no picture (ErrNoPoster).
package video

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"

	"github.com/go-avkit/avkit/container"
	"github.com/go-avkit/decode"
)

// The codes of a refusal.
const (
	CodeLimit  = "video.limit"
	CodeSyntax = "video.syntax"
	CodeCodec  = "video.codec"
)

// Error is a refusal with the code that names it.
type Error struct {
	Code string
	Msg  string
}

func (e *Error) Error() string { return e.Msg }

func fail(code, format string, a ...any) error {
	return &Error{Code: code, Msg: fmt.Sprintf(format, a...)}
}

// ErrNoPoster is returned (wrapped in an *Error with CodeCodec) when the video's
// codec is not one the poster can be decoded for.
var ErrNoPoster = errors.New("video: no poster for this codec")

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
	reader    *container.Reader
	track     uint32
}

// Open checks a file against the limits and reads its description. Nothing is
// decoded.
func Open(data []byte, lim Limits) (*Info, error) {
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
	r, err := container.NewReader(data)
	if err != nil {
		return nil, fail(CodeSyntax, "the %s container cannot be read: %v", info.Format, err)
	}
	f := r.File()
	for _, t := range f.Tracks {
		if t.Kind == container.Video {
			info.Codec, info.Width, info.Height = t.Codec, t.Width, t.Height
			info.track = t.ID
			break
		}
	}
	if info.track == 0 {
		return nil, fail(CodeSyntax, "the file has no video track")
	}
	if info.Width <= 0 || info.Height <= 0 || info.Width > lim.MaxWidth || info.Height > lim.MaxHeight {
		return nil, fail(CodeLimit, "the frame is %dx%d; at most %dx%d are read", info.Width, info.Height, lim.MaxWidth, lim.MaxHeight)
	}
	if f.Timescale > 0 {
		info.Duration = float64(f.Duration) / float64(f.Timescale)
	}
	info.Decodable = info.Codec == "avc1" || info.Codec == "avc3"
	info.reader = r
	return &info, nil
}

// Poster decodes the picture shown at t seconds (the one before it, if none is
// exactly at t; the last one when t is past the end), scaled down to the
// limit's PosterWidth, and encodes it as JPEG.
func (i *Info) Poster(t float64, lim Limits) (jpg []byte, w, h int, err error) {
	if !i.Decodable {
		return nil, 0, 0, fail(CodeCodec, "the poster of a %s video cannot be decoded (H.264 only): a neutral picture stands in", i.Codec)
	}
	cfg := decode.PosterConfig{
		Config:          decode.Config{MaxFrameMBs: (lim.MaxWidth/16 + 1) * (lim.MaxHeight/16 + 1), MaxSampleBytes: lim.MaxSampleBytes},
		MaxSamples:      lim.MaxSamples,
		MaxTrackSamples: lim.MaxTrackSamples,
	}
	f, err := decode.PosterFrame(i.reader, i.track, t, cfg)
	if err != nil {
		switch {
		case errors.Is(err, decode.ErrPosterLimit), errors.Is(err, decode.ErrOverLimit):
			return nil, 0, 0, fail(CodeLimit, "the poster at %gs costs more than the %d samples read: %v", t, lim.MaxSamples, err)
		case errors.Is(err, decode.ErrUnsupported):
			return nil, 0, 0, fail(CodeCodec, "the H.264 stream uses a tool that is not decoded (%v): a neutral picture stands in", err)
		case errors.Is(err, decode.ErrTime):
			return nil, 0, 0, fail(CodeSyntax, "#t=%g is not a time in the video", t)
		}
		return nil, 0, 0, fail(CodeSyntax, "the poster cannot be decoded: %v", err)
	}
	img := toRGBA(f, lim.PosterWidth)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
		return nil, 0, 0, fail(CodeSyntax, "the poster cannot be encoded: %v", err)
	}
	return buf.Bytes(), img.Bounds().Dx(), img.Bounds().Dy(), nil
}

// Placeholder is the picture that stands in for a poster that cannot be
// decoded: a dark card with a play triangle, at the size of the video (scaled
// to the poster width).
func (i *Info) Placeholder(lim Limits) (jpg []byte, w, h int) {
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

// toRGBA converts a 4:2:0 or 4:0:0 8-bit picture (BT.601 below 720 lines,
// BT.709 above; limited range) and scales it down by averaging boxes to at
// most width pixels across.
func toRGBA(f decode.Frame, width int) *image.RGBA {
	w, h := f.Width, f.Height
	step := 1
	if width > 0 && w > width {
		step = (w + width - 1) / width
	}
	ow, oh := max(w/step, 1), max(h/step, 1)
	out := image.NewRGBA(image.Rect(0, 0, ow, oh))
	kr, kb := 0.299, 0.114
	if h >= 720 {
		kr, kb = 0.2126, 0.0722
	}
	kg := 1 - kr - kb
	cl := func(v float64) uint8 {
		if v < 0 {
			return 0
		}
		if v > 255 {
			return 255
		}
		return uint8(v + 0.5)
	}
	for y := 0; y < oh; y++ {
		for x := 0; x < ow; x++ {
			var sr, sg, sb, n float64
			for dy := 0; dy < step; dy++ {
				for dx := 0; dx < step; dx++ {
					px, py := x*step+dx, y*step+dy
					if px >= w || py >= h {
						continue
					}
					Y := (float64(f.Y[py*f.YStride+px]) - 16) * 255 / 219
					cb, cr := 0.0, 0.0
					if f.Chroma != decode.Chroma400 && len(f.Cb) > 0 {
						ci := (py/2)*f.CStride + px/2
						if ci < len(f.Cb) && ci < len(f.Cr) {
							cb = (float64(f.Cb[ci]) - 128) * 255 / 224
							cr = (float64(f.Cr[ci]) - 128) * 255 / 224
						}
					}
					r := Y + 2*(1-kr)*cr
					b := Y + 2*(1-kb)*cb
					g := (Y - kr*r - kb*b) / kg
					sr, sg, sb, n = sr+r, sg+g, sb+b, n+1
				}
			}
			if n == 0 {
				continue
			}
			out.SetRGBA(x, y, color.RGBA{cl(sr / n), cl(sg / n), cl(sb / n), 255})
		}
	}
	return out
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

// ebmlMasters are the Matroska elements whose children are elements: Segment,
// SeekHead, Info, Tracks, TrackEntry, Video, Cluster, BlockGroup, Cues,
// CuePoint, CueTrackPositions, Tags, Tag, Chapters, EditionEntry, ChapterAtom,
// Cluster's BlockAdditions, Attachments.
var ebmlMasters = map[uint32]bool{
	0x18538067: true, 0x114D9B74: true, 0x1549A966: true, 0x1654AE6B: true, 0xAE: true, 0xE0: true,
	0x1F43B675: true, 0xA0: true, 0x1C53BB6B: true, 0xBB: true, 0xB7: true, 0x1254C367: true, 0x7373: true,
	0x1043A770: true, 0x45B9: true, 0xB6: true, 0x75A1: true, 0x1941A469: true, 0x61A7: true, 0x4DBB: true,
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

// scanEBML counts and nests the elements as scanBoxes does the boxes.
func scanEBML(data []byte, lim Limits) error {
	count := 0
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
			}
			b = body[size:]
		}
		return nil
	}
	return walk(data, 1)
}
