// Package avkit is the video.Provider go-slides ships: go-avkit's container
// reader (MP4 and WebM / Matroska) and its H.264 poster decoder. Importing it
// registers it (video.Register); the slides package imports it unless built
// with the tag slides_novideo, so that a WebAssembly host can keep all of
// go-avkit out of its module and run this provider in a worker of its own.
package avkit

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"

	"github.com/go-avkit/avkit/container"
	"github.com/go-avkit/decode"
	"github.com/go-slides/slides/video"
)

func init() { video.Register(Provider{}) }

// Provider reads with go-avkit.
type Provider struct{}

var _ video.Provider = Provider{}

// Probe implements video.Provider.
func (Provider) Probe(data []byte, format string, lim video.Limits) (video.Probed, error) {
	r, err := container.NewReader(data)
	if err != nil {
		return video.Probed{}, video.Fail(video.CodeSyntax, "the %s container cannot be read: %v", format, err)
	}
	f := r.File()
	var p video.Probed
	var track uint32
	for _, t := range f.Tracks {
		if t.Kind == container.Video {
			p.Codec, p.Width, p.Height = t.Codec, t.Width, t.Height
			track = t.ID
			break
		}
	}
	if track == 0 {
		return video.Probed{}, video.Fail(video.CodeSyntax, "the file has no video track")
	}
	if f.Timescale > 0 {
		p.Duration = float64(f.Duration) / float64(f.Timescale)
	}
	p.Decodable = p.Codec == "avc1" || p.Codec == "avc3"
	p.State = &state{r: r, track: track}
	return p, nil
}

type state struct {
	r     *container.Reader
	track uint32
}

// Poster implements video.Provider.
func (Provider) Poster(p video.Probed, data []byte, t float64, lim video.Limits) ([]byte, int, int, error) {
	st, ok := p.State.(*state)
	if !ok {
		// a Probed that did not come from this provider: read the file again
		again, err := Provider{}.Probe(data, "", lim)
		if err != nil {
			return nil, 0, 0, err
		}
		st = again.State.(*state)
	}
	cfg := decode.PosterConfig{
		Config:          decode.Config{MaxFrameMBs: (lim.MaxWidth/16 + 1) * (lim.MaxHeight/16 + 1), MaxSampleBytes: lim.MaxSampleBytes},
		MaxSamples:      lim.MaxSamples,
		MaxTrackSamples: lim.MaxTrackSamples,
	}
	f, err := decode.PosterFrame(st.r, st.track, t, cfg)
	if err != nil {
		switch {
		case errors.Is(err, decode.ErrPosterLimit), errors.Is(err, decode.ErrOverLimit):
			return nil, 0, 0, video.Fail(video.CodeLimit, "the poster at %gs costs more than the %d samples read: %v", t, lim.MaxSamples, err)
		case errors.Is(err, decode.ErrUnsupported):
			return nil, 0, 0, video.Fail(video.CodeCodec, "the H.264 stream uses a tool that is not decoded (%v): a neutral picture stands in", err)
		case errors.Is(err, decode.ErrTime):
			return nil, 0, 0, video.Fail(video.CodeSyntax, "#t=%g is not a time in the video", t)
		}
		return nil, 0, 0, video.Fail(video.CodeSyntax, "the poster cannot be decoded: %v", err)
	}
	img := toRGBA(f, lim.PosterWidth)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
		return nil, 0, 0, video.Fail(video.CodeSyntax, "the poster cannot be encoded: %v", err)
	}
	return buf.Bytes(), img.Bounds().Dx(), img.Bounds().Dy(), nil
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
