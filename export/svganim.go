package export

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/go-gfx/oksvg"
	"github.com/go-gfx/oksvg/svganim"
	"golang.org/x/net/html"

	"github.com/go-slides/slides/doc"
)

// Animated SVG images in the exports.
//
// A browser plays the SMIL and CSS animations of an <img src="x.svg"> by itself.
// The exports draw an SVG as vector shapes (the web engine, over the oksvg
// fork) at its initial state, so an animated drawing has to be carried
// through them explicitly: the animations are modelled (oksvg's svganim), and
// where an exported format can say what an element does (PowerPoint's
// p:timing, OpenDocument's anim:* elements) each animated element is a shape of
// its own with that animation; where it cannot (a path that morphs, a
// stroke that draws itself) the image becomes an animated GIF, which
// PowerPoint and Impress play. The PDF, which cannot animate, shows the initial
// state, and says so.
//
// PrepareSVG is the first step: it reads the images of a deck's slides (also
// saying, as marp-cli does, which local files the options do not allow), and
// hands the slides' markup with each animated SVG replaced by its state at t=0,
// which is also what a PDF or an image shows. The writers (PPTXWriter,
// ODPWriter) recognise the drawings in the layouts by the hash of that
// document and carry them.

// Diagnostic codes of the warnings of this file, at the start of each line.
const (
	// CodeLocalFileBlocked: a local image the deck names is not read because
	// the options do not allow local files (marp-cli's --allow-local-files).
	CodeLocalFileBlocked = "local-file-blocked"
	// CodeLocalFileMissing: a local image the deck names does not exist.
	CodeLocalFileMissing = "local-file-missing"
	// CodeSVGAnimation: how an animated SVG image is carried in an export.
	CodeSVGAnimation = "svg-animation"
)

// maxSVGBytes bounds one SVG image read for its animations.
const maxSVGBytes = 8 << 20

// SVGAnimations are the animated SVG images of a deck.
type SVGAnimations struct {
	bySource map[string]*animSVG
	order    []*animSVG
	// Warnings are what the preparation found: local files not read, in marp-cli's
	// terms, and what the animations had that is not carried.
	Warnings []string
}

// animSVG is one animated SVG image.
type animSVG struct {
	name    string // as the deck wrote it
	svg     []byte // the document
	initial []byte // the document at t=0, animations out
	source  string // hex SHA-256 of initial: what the engine calls the drawing
	anim    *svganim.Animations
	slides  []int // 1-based slide numbers it appears on

	planOnce sync.Once
	plan     *svgPlan

	gifMu sync.Mutex // the animated GIFs made, by size
	gifs  map[gifKey]*svgFrames
}

// Animated reports whether the deck has any animated SVG image.
func (a *SVGAnimations) Animated() bool { return a != nil && len(a.order) > 0 }

func (a *SVGAnimations) lookup(source string) *animSVG {
	if a == nil || source == "" {
		return nil
	}
	return a.bySource[source]
}

// Names are the images, as the deck wrote them.
func (a *SVGAnimations) Names() []string {
	var out []string
	if a == nil {
		return out
	}
	for _, s := range a.order {
		out = append(out, s.name)
	}
	return out
}

// PrepareSVG reads the SVG images of the deck's slides and returns the deck
// with each animated one replaced by its initial state, and the animations.
// The deck given is not changed. opts decides which local files may be read
// (FileRoot, as the rendering does), and fetches http(s) images through
// opts.Fetcher when it is set.
func PrepareSVG(ctx context.Context, b *doc.Doc, opts Options) (*doc.Doc, *SVGAnimations, error) {
	a := &SVGAnimations{bySource: map[string]*animSVG{}}
	out := *b
	out.Slides = slices.Clone(b.Slides)
	type fileRef struct {
		path   string
		slides []int
	}
	blocked := map[string]*fileRef{}
	var blockedOrder []string
	missing := map[string]*fileRef{}
	var missingOrder []string
	// by src: the animated SVG it is, nil when it is not one, and how reading it went
	type cached struct {
		v      *animSVG
		status imgStatus
	}
	cache := map[string]cached{}
	note := func(src string, status imgStatus, slide int) {
		switch status {
		case imgBlocked:
			r := blocked[src]
			if r == nil {
				r = &fileRef{path: src}
				blocked[src] = r
				blockedOrder = append(blockedOrder, src)
			}
			r.slides = appendUnique(r.slides, slide)
		case imgMissing:
			r := missing[src]
			if r == nil {
				r = &fileRef{path: src}
				missing[src] = r
				missingOrder = append(missingOrder, src)
			}
			r.slides = appendUnique(r.slides, slide)
		}
	}
	for i, s := range b.Slides {
		if !strings.Contains(s.HTML, "<img") {
			continue
		}
		rewritten, err := rewriteImages(s.HTML, func(src string) (string, bool) {
			if c, ok := cache[src]; ok {
				note(src, c.status, i+1)
				if c.v == nil {
					return "", false
				}
				c.v.addSlide(i + 1)
				return c.v.dataURL(), true
			}
			data, status := loadImage(ctx, opts, src)
			note(src, status, i+1)
			if status != imgOK || !looksSVG(src, data) {
				cache[src] = cached{nil, status}
				return "", false
			}
			an, err := svganim.Parse(data, svganim.Limits{})
			if err != nil || !an.Animated() {
				if err == nil && len(an.Warnings) > 0 {
					a.Warnings = append(a.Warnings, fmt.Sprintf("%s: %s (slide %d): the animations are not carried: %s", CodeSVGAnimation, src, i+1, strings.Join(an.Warnings, "; ")))
				}
				cache[src] = cached{nil, status}
				return "", false
			}
			initial, err := an.Apply(data, 0, bboxOf(an, data))
			if err != nil {
				cache[src] = cached{nil, status}
				return "", false
			}
			sum := sha256.Sum256(initial)
			src2 := hex.EncodeToString(sum[:])
			v := a.bySource[src2]
			if v == nil {
				v = &animSVG{name: src, svg: data, initial: initial, source: src2, anim: an}
				a.bySource[src2] = v
				a.order = append(a.order, v)
			}
			cache[src] = cached{v, status}
			v.addSlide(i + 1)
			return v.dataURL(), true
		})
		if err != nil {
			return nil, nil, fmt.Errorf("slide %d: %w", i+1, err)
		}
		out.Slides[i].HTML = rewritten
	}
	for _, src := range blockedOrder {
		r := blocked[src]
		a.Warnings = append(a.Warnings, fmt.Sprintf(
			"%s: %q (slide%s %s) is a local file, blocked by security reason: it is left out of the export. Use the --allow-local-files option if you understand the security risk (only the deck's own directory is read), or use an online asset",
			CodeLocalFileBlocked, src, plural(len(r.slides)), joinInts(r.slides)))
	}
	for _, src := range missingOrder {
		r := missing[src]
		a.Warnings = append(a.Warnings, fmt.Sprintf(
			"%s: %q (slide%s %s) does not exist: it is left out of the export. Make sure the file path is correct",
			CodeLocalFileMissing, src, plural(len(r.slides)), joinInts(r.slides)))
	}
	return &out, a, nil
}

func (s *animSVG) addSlide(n int) { s.slides = appendUnique(s.slides, n) }

func (s *animSVG) dataURL() string {
	return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString(s.initial)
}

func appendUnique(l []int, n int) []int {
	if slices.Contains(l, n) {
		return l
	}
	return append(l, n)
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func joinInts(l []int) string {
	parts := make([]string, len(l))
	for i, n := range l {
		parts[i] = fmt.Sprint(n)
	}
	return strings.Join(parts, ", ")
}

// bboxOf gives the bounding box of an element's geometry in its own user
// space, from the shapes oksvg draws for the document: what a CSS
// transform-box of fill-box refers to.
func bboxOf(an *svganim.Animations, data []byte) svganim.BBoxFunc {
	var shapes []oksvg.Shape
	var loaded bool
	return func(e int) (x, y, w, h float64, ok bool) {
		if !loaded {
			loaded = true
			if icon, err := oksvg.ReadIconStream(bytes.NewReader(rootFixed(data)), oksvg.IgnoreErrorMode); err == nil {
				shapes, _ = icon.Shapes()
			}
		}
		bx, by, bw, bh, ok := svganim.ElementBounds(shapes, e)
		if !ok {
			return 0, 0, 0, 0, false
		}
		// into the element's own space: undo the transforms down to it
		mi, okInv := an.ParentCTM(e).Mul(an.OwnTransform(e)).Inverse()
		if !okInv {
			return bx, by, bw, bh, true
		}
		minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
		for _, c := range [][2]float64{{bx, by}, {bx + bw, by}, {bx, by + bh}, {bx + bw, by + bh}} {
			px, py := mi.Apply(c[0], c[1])
			minX, maxX = math.Min(minX, px), math.Max(maxX, px)
			minY, maxY = math.Min(minY, py), math.Max(maxY, py)
		}
		return minX, minY, maxX - minX, maxY - minY, true
	}
}

// rootFixed is the document as oksvg reads it.
func rootFixed(data []byte) []byte { return fixRootAttrs(data) }

// fixRootAttrs removes width and height from the root svg element, which
// oksvg reads as bare numbers only; a viewBox is made of them when there is
// none.
func fixRootAttrs(svg []byte) []byte {
	i := bytes.Index(svg, []byte("<svg"))
	if i < 0 {
		return svg
	}
	j := bytes.IndexByte(svg[i:], '>')
	if j < 0 {
		return svg
	}
	tag := string(svg[i : i+j])
	get := func(name string) (string, bool) {
		for _, q := range []string{`"`, `'`} {
			if k := strings.Index(tag, " "+name+"="+q); k >= 0 {
				v := tag[k+len(name)+3:]
				if e := strings.Index(v, q); e >= 0 {
					return v[:e], true
				}
			}
		}
		return "", false
	}
	_, hasVB := get("viewBox")
	w, okW := get("width")
	h, okH := get("height")
	out := tag
	if !hasVB && okW && okH {
		wv, hv := svganim.ParseValue(w), svganim.ParseValue(h)
		if len(wv.Nums) > 0 && len(hv.Nums) > 0 {
			out += fmt.Sprintf(` viewBox="0 0 %g %g"`, wv.Nums[0], hv.Nums[0])
		}
	}
	for _, n := range []string{"width", "height"} {
		for _, q := range []string{`"`, `'`} {
			if k := strings.Index(out, " "+n+"="+q); k >= 0 {
				if e := strings.Index(out[k+len(n)+3:], q); e >= 0 {
					out = out[:k] + out[k+len(n)+3+e+1:]
				}
			}
		}
	}
	return append(append(append([]byte{}, svg[:i]...), out...), svg[i+j:]...)
}

// rewriteImages calls f with the src of each <img> of the markup; when f
// answers a replacement, the tag is rewritten with it.
func rewriteImages(markup string, f func(src string) (string, bool)) (string, error) {
	z := html.NewTokenizer(strings.NewReader(markup))
	var b strings.Builder
	changed := false
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			if err := z.Err(); err != io.EOF {
				return "", err
			}
			break
		}
		raw := z.Raw()
		if tt != html.StartTagToken && tt != html.SelfClosingTagToken {
			b.Write(raw)
			continue
		}
		name, hasAttr := z.TagName()
		if string(name) != "img" || !hasAttr {
			b.Write(raw)
			continue
		}
		var attrs [][2]string
		src := -1
		for {
			k, v, more := z.TagAttr()
			if string(k) == "src" && src < 0 {
				src = len(attrs)
			}
			attrs = append(attrs, [2]string{string(k), string(v)})
			if !more {
				break
			}
		}
		if src < 0 {
			b.Write(raw)
			continue
		}
		repl, ok := f(attrs[src][1])
		if !ok {
			b.Write(raw)
			continue
		}
		changed = true
		attrs[src][1] = repl
		b.WriteString("<img")
		for _, a := range attrs {
			b.WriteString(" " + a[0] + `="` + html.EscapeString(a[1]) + `"`)
		}
		if tt == html.SelfClosingTagToken {
			b.WriteString("/")
		}
		b.WriteString(">")
	}
	if !changed {
		return markup, nil
	}
	return b.String(), nil
}

type imgStatus int

const (
	imgOK imgStatus = iota
	imgSkipped
	imgBlocked
	imgMissing
)

func looksSVG(src string, data []byte) bool {
	l := strings.ToLower(src)
	if strings.HasPrefix(l, "data:image/svg+xml") {
		return true
	}
	if i := strings.IndexAny(l, "?#"); i >= 0 {
		l = l[:i]
	}
	if strings.HasSuffix(l, ".svg") || strings.HasSuffix(l, ".svgz") {
		return true
	}
	head := data
	if len(head) > 1024 {
		head = head[:1024]
	}
	return bytes.Contains(head, []byte("<svg"))
}

// loadImage reads the image a deck names, as the rendering would: a data URL
// is decoded; a local file is read only under opts.FileRoot (blocked when
// there is none, or when it is elsewhere); an http(s) image goes through
// opts.Fetcher, or is fetched, bounded. imgSkipped is an image that is not
// to be read for its animations (not fetchable here).
func loadImage(ctx context.Context, opts Options, src string) ([]byte, imgStatus) {
	src = strings.TrimSpace(src)
	switch {
	case src == "":
		return nil, imgSkipped
	case strings.HasPrefix(strings.ToLower(src), "data:"):
		return decodeDataURL(src)
	}
	u, err := url.Parse(src)
	if err != nil {
		return nil, imgSkipped
	}
	if opts.BaseURL != "" {
		if base, err := url.Parse(opts.BaseURL); err == nil {
			u = base.ResolveReference(u)
		}
	}
	switch u.Scheme {
	case "http", "https":
		if opts.Fetcher != nil {
			data, _, err := opts.Fetcher(ctx, u.String())
			if err != nil || len(data) > maxSVGBytes {
				return nil, imgSkipped
			}
			return data, imgOK
		}
		if !looksLikeSVGURL(u) {
			return nil, imgSkipped
		}
		return fetchHTTP(ctx, u.String())
	case "file", "":
		path := u.Path
		if u.Scheme == "" {
			return nil, imgSkipped
		}
		return readLocal(opts.FileRoot, path)
	}
	return nil, imgSkipped
}

func looksLikeSVGURL(u *url.URL) bool {
	return strings.HasSuffix(strings.ToLower(u.Path), ".svg")
}

func decodeDataURL(src string) ([]byte, imgStatus) {
	head, body, ok := strings.Cut(src, ",")
	if !ok {
		return nil, imgSkipped
	}
	if strings.HasSuffix(strings.ToLower(head), ";base64") {
		d, err := base64.StdEncoding.DecodeString(body)
		if err != nil {
			d, err = base64.RawStdEncoding.DecodeString(body)
		}
		if err != nil || len(d) > maxSVGBytes {
			return nil, imgSkipped
		}
		return d, imgOK
	}
	s, err := url.PathUnescape(body)
	if err != nil || len(s) > maxSVGBytes {
		return nil, imgSkipped
	}
	return []byte(s), imgOK
}

func fetchHTTP(ctx context.Context, u string) ([]byte, imgStatus) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, imgSkipped
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, imgSkipped
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, imgSkipped
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxSVGBytes+1))
	if err != nil || len(data) > maxSVGBytes {
		return nil, imgSkipped
	}
	return data, imgOK
}

// readLocal reads a file under root. Without a root nothing is read: the file
// is blocked, as marp-cli blocks it without --allow-local-files. A file that
// is not under the root is blocked as well.
func readLocal(root, path string) ([]byte, imgStatus) {
	path = filepath.FromSlash(path)
	if root == "" {
		// existence says whether to call it blocked or missing
		if _, err := os.Stat(path); err != nil {
			return nil, imgMissing
		}
		return nil, imgBlocked
	}
	absRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, imgBlocked
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, imgMissing
	}
	rel, err := filepath.Rel(absRoot, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, imgBlocked
	}
	f, err := os.Open(real)
	if err != nil {
		return nil, imgMissing
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxSVGBytes+1))
	if err != nil || len(data) > maxSVGBytes {
		return nil, imgSkipped
	}
	return data, imgOK
}
