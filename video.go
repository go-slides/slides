package slides

import (
	"encoding/base64"
	"math"
	"net/url"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/go-slides/slides/deck"
	"github.com/go-slides/slides/diag"
	"github.com/go-slides/slides/doc"
	"github.com/go-slides/slides/vfs"
	"github.com/go-slides/slides/video"
)

// videoRenderer writes the images that name a video (![video](clip.mp4)).
//
// The HTML page gets a <video controls playsinline> whose poster is the frame
// at #t= (else the first), as a data: URI, so that it shows before playback
// and costs no request. The PDF, PPTX and ODP exports get that frame as a
// picture inside a link to the file (<a href>): the PDF keeps the link, and the
// PPTX and ODP writers find the picture by it and make it the poster of the
// video they embed (package export). A video that is a URL is linked, and
// shown with a neutral card, since nothing is fetched at build time.
type videoRenderer struct {
	fs     vfs.FS
	base   string
	player bool
	w, h   int
	lim    video.Limits

	mu       sync.Mutex
	warnings []string
	opened   map[string]*videoFile
	media    map[string]doc.Media
}

type videoFile struct {
	info *video.File
	data []byte
}

func newVideoRenderer(o deck.Options, player bool, w, h int) *videoRenderer {
	return &videoRenderer{fs: vfs.Or(o.FS), base: o.BaseDir, player: player, w: w, h: h, lim: video.DefaultLimits,
		opened: map[string]*videoFile{}, media: map[string]doc.Media{}}
}

func (v *videoRenderer) warn(code, name, format string, a ...any) error {
	err := diag.Errorf(code, "%s: "+format, append([]any{name}, a...)...)
	w := code + ": " + err.Error()
	v.mu.Lock()
	if !slices.Contains(v.warnings, w) {
		v.warnings = append(v.warnings, w)
	}
	v.mu.Unlock()
	return err
}

func (v *videoRenderer) failVideo(rel string, err error) error {
	code := video.CodeSyntax
	if e, ok := err.(*video.Error); ok {
		code = e.Code
	}
	return v.warn(code, rel, "%s", err.Error())
}

// load reads and describes a file once per build.
func (v *videoRenderer) load(rel string) (*videoFile, error) {
	if f, ok := v.opened[rel]; ok {
		return f, nil
	}
	if !filepath.IsLocal(rel) {
		return nil, v.warn("video.path", rel, "a video must be a file in the deck's folder or an http(s) URL, not an absolute path or one that leaves the folder")
	}
	full := filepath.Join(v.base, rel)
	if fi, err := v.fs.Stat(full); err == nil && fi.Size() > int64(v.lim.MaxBytes) {
		return nil, v.warn("video.limit", rel, "the file is %d bytes; at most %d are read", fi.Size(), v.lim.MaxBytes)
	}
	data, err := v.fs.ReadFile(full)
	if err != nil {
		return nil, v.warn("file.missing", rel, "the video cannot be read")
	}
	info, err := video.Open(data, v.lim)
	if err != nil {
		return nil, v.failVideo(rel, err)
	}
	f := &videoFile{info: info, data: data}
	v.opened[rel] = f
	v.mu.Lock()
	v.media[rel] = doc.Media{MIME: info.MIME, Data: data}
	v.mu.Unlock()
	return f, nil
}

// IsVideo reports whether an image destination names a video this renders.
func isVideoDest(dest string) bool { return deck.IsVideo(dest) }

func (v *videoRenderer) render(dest, alt string) (string, error) {
	p, frag, _ := strings.Cut(dest, "#")
	p, _, _ = strings.Cut(p, "?")
	if u, err := url.PathUnescape(p); err == nil {
		p = u
	}
	remote := strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://")
	if !remote && (strings.Contains(p, "://") || strings.HasPrefix(p, "//") || strings.HasPrefix(p, "data:")) {
		return "", v.warn("video.path", p, "a video must be a file in the deck's folder or an http(s) URL")
	}
	poster := 0.0
	if frag != "" {
		if q, err := url.ParseQuery(frag); err == nil && q.Has("t") {
			// a media fragment may name a range (t=10,20): the start is the poster
			start, _, _ := strings.Cut(q.Get("t"), ",")
			t, ok := parseMoment(start)
			if !ok {
				return "", v.warn("video.syntax", p, "#t=%s is not a number of seconds (12.5) or minutes and seconds (1:23)", truncateText(q.Get("t")))
			}
			poster = t
		}
	}
	var autoplay, loop, muted bool
	var words []string
	for _, w := range strings.Fields(alt) {
		switch strings.ToLower(w) {
		case "autoplay":
			autoplay = true
		case "loop":
			loop = true
		case "muted":
			muted = true
		default:
			words = append(words, w)
		}
	}
	if autoplay && !muted {
		v.warn("video.syntax", p, "autoplay is ignored without muted: a video does not start by itself with sound")
		autoplay = false
	}
	label := strings.Join(words, " ")
	if label == "" || strings.EqualFold(label, "video") {
		label = "Video " + path.Base(p)
	}

	var jpg []byte
	var iw, ih, vw, vh int
	if remote {
		vw, vh = 1280, 720
		jpg, iw, ih = video.PlaceholderSize(vw, vh, v.lim)
	} else {
		f, err := v.load(p)
		if err != nil {
			return "", err
		}
		vw, vh = f.info.Width, f.info.Height
		var perr error
		jpg, iw, ih, perr = f.info.Poster(poster, v.lim)
		if perr != nil {
			// the video is sound: it is its picture that cannot be made
			v.failVideo(p, perr)
			jpg, iw, ih = f.info.Placeholder(v.lim)
		}
	}
	_ = iw
	_ = ih
	// the video takes at most 90% of the slide's width and 62% of its height
	s := math.Min(1, math.Min(0.9*float64(v.w)/float64(vw), 0.62*float64(v.h)/float64(vh)))
	dw, dh := max(int(float64(vw)*s), 1), max(int(float64(vh)*s), 1)
	src := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(jpg)
	href := escapeAttr(p)
	var b strings.Builder
	if v.player {
		b.WriteString(`<video class="slides-video" controls playsinline preload="metadata" data-slides-video="` + href + `" src="` + href + `" poster="` + src + `"`)
		b.WriteString(` width="` + strconv.Itoa(dw) + `" height="` + strconv.Itoa(dh) + `" aria-label="` + escapeAttr(label) + `" style="display:block;max-width:100%;height:auto;background:#000"`)
		if muted {
			b.WriteString(` muted`)
		}
		if autoplay {
			b.WriteString(` autoplay`)
		}
		if loop {
			b.WriteString(` loop`)
		}
		b.WriteString(`><a href="` + href + `">` + escapeAttr(label) + `</a></video>`)
		return b.String(), nil
	}
	link := href
	if !remote {
		// the exports find the picture by this address (doc.VideoLinkPrefix)
		link = escapeAttr(doc.VideoLinkPrefix + (&url.URL{Path: p}).EscapedPath())
	}
	b.WriteString(`<a class="slides-video" href="` + link + `"><img src="` + src + `" width="` + strconv.Itoa(dw) + `" height="` + strconv.Itoa(dh) +
		`" alt="` + escapeAttr(label) + `" style="display:block"></a>`)
	return b.String(), nil
}
