package export

import (
	"archive/zip"
	"bytes"
	"fmt"
	"image"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/go-odf/odf"
	"github.com/go-ooxml/ooxml"
	"github.com/go-webengine/engine"

	"github.com/go-slides/slides/doc"
)

// A video of a deck is a poster picture inside a link to the file
// (<a href="clip.mp4"><img src="data:..."></a>, see slides.videoRenderer). The
// PDF keeps the link. The office writers find the picture the link surrounds
// and make it the poster of the video they embed:
//
//   - PPTX: the media part ppt/media/slidesvideoN.mp4 (.webm), related to the
//     slide twice, as PowerPoint does it: by a video relationship, which
//     a:videoFile (ECMA-376 Part 1, the schema's CT_VideoFile, in
//     p:nvPr: CT_ApplicationNonVisualDrawingProps) points at, and by a media
//     relationship, which the p14:media extension of Microsoft's [MS-PPTX]
//     names. The picture is the p:blipFill's, so it is the poster. ECMA-376's
//     schema was opened (dml-main.xsd, pml.xsd), and [MS-PPTX] v20240820 (2.1.1
//     Media Part: source relationship
//     http://schemas.microsoft.com/office/2007/relationships/media; 2.2.4: ext
//     uri {DAA4B4D4-6D71-4841-9C94-3DE7FCFB9230} in p:nvPr/p:extLst; 2.3.3.14
//     CT_Media: r:embed; 3.2: the example this writes). The video relationship
//     is the transitional ECMA-376 one (Part 1 gives the strict one, in the
//     purl.oclc.org namespace).
//   - ODP: a draw:plugin (ODF 1.3 Part 3, 10.4.8) in the picture's draw:frame,
//     before the draw:image that is the poster, with the file under Video/ and
//     in the manifest.

// videoAt is a video found on a slide: the n-th picture of the slide (counting
// in document order, a group's members included) is its poster.
type videoAt struct {
	pic int
	key string
}

// mediaKey is the key of doc.Doc.Media a link's resolved href names: the href
// is resolved against the page's URL, so a path written clip.mp4 ends in
// /clip.mp4.
func mediaKey(media map[string]doc.Media, href string) (string, bool) {
	if len(media) == 0 {
		return "", false
	}
	h := href
	if u, err := url.Parse(href); err == nil {
		h = u.Path
	}
	best := ""
	for k := range media {
		if strings.HasSuffix(h, "/"+k) || h == k {
			if len(k) > len(best) {
				best = k
			}
		}
	}
	return best, best != ""
}

// shapeBox is the box of a shape in the page's pixels, and whether it is a
// picture (pics: how many pictures it holds, itself or its group's members).
type shapeBox struct {
	x, y, w, h float64
	pics       int
	isPic      bool
}

// findVideos places each link that names a media file on the picture it
// surrounds (the picture whose centre lies in the link's rectangle), once.
// boxes are the slide's top-level shapes in order.
func findVideos(links []engine.Link, media map[string]doc.Media, boxes []shapeBox) []videoAt {
	var out []videoAt
	used := map[int]bool{}
	for _, ln := range links {
		key, ok := mediaKey(media, ln.Href)
		if !ok {
			continue
		}
		before := 0
		for j, b := range boxes {
			if b.isPic && !used[j] && inside(ln.Rect, b.x+b.w/2, b.y+b.h/2) {
				used[j] = true
				out = append(out, videoAt{pic: before, key: key})
				break
			}
			before += b.pics
		}
	}
	return out
}

func inside(r image.Rectangle, x, y float64) bool {
	return x >= float64(r.Min.X) && x <= float64(r.Max.X) && y >= float64(r.Min.Y) && y <= float64(r.Max.Y)
}

// mediaPart is a media file put in a package: its path there, its type.
type mediaPart struct {
	name string
	mime string
	data []byte
}

// videoPackage collects the media of a deck once each, by key.
type videoPackage struct {
	dir   string
	parts map[string]mediaPart
	order []string
}

func (v *videoPackage) add(key string, m doc.Media) mediaPart {
	if p, ok := v.parts[key]; ok {
		return p
	}
	if v.parts == nil {
		v.parts = map[string]mediaPart{}
	}
	ext := ".mp4"
	if m.MIME == "video/webm" {
		ext = ".webm"
	}
	p := mediaPart{name: v.dir + "/slidesvideo" + strconv.Itoa(len(v.parts)+1) + ext, mime: m.MIME, data: m.Data}
	v.parts[key] = p
	v.order = append(v.order, key)
	return p
}

// replaceNth replaces the n-th (0-based) occurrence of old in s.
func replaceNth(s, old, new string, n int) (string, bool) {
	i := 0
	for k := 0; ; k++ {
		j := strings.Index(s[i:], old)
		if j < 0 {
			return s, false
		}
		i += j
		if k == n {
			break
		}
		i += len(old)
	}
	return s[:i] + new + s[i+len(old):], true
}

// rewriteZipExtra is rewriteZip, and appends the files in extra.
func rewriteZipExtra(data []byte, want func(string) bool, edit func(string, []byte) ([]byte, error), extra map[string][]byte, order []string) ([]byte, error) {
	out, err := rewriteZip(data, want, edit)
	if err != nil || len(extra) == 0 {
		return out, err
	}
	// rewriteZip closes its archive: append by reading and writing again.
	zr, err := zip.NewReader(bytes.NewReader(out), int64(len(out)))
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range zr.File {
		if err := zw.Copy(f); err != nil {
			return nil, err
		}
	}
	for _, name := range order {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate})
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(extra[name]); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// pptxVideos is the videos of each slide (0-based).
type pptxVideos map[int][]videoAt

const (
	relVideo = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/video"
	relMedia = "http://schemas.microsoft.com/office/2007/relationships/media"
	extMedia = "{DAA4B4D4-6D71-4841-9C94-3DE7FCFB9230}"
)

// addPPTXVideos makes each poster picture the video it stands for.
func addPPTXVideos(deck []byte, vids pptxVideos, media map[string]doc.Media) ([]byte, error) {
	pkg := videoPackage{dir: "ppt/media"}
	slideNum := func(name, prefix, suffix string) (int, bool) {
		rest, ok := strings.CutPrefix(name, prefix)
		if !ok {
			return 0, false
		}
		num, ok := strings.CutSuffix(rest, suffix)
		n, err := strconv.Atoi(num)
		return n - 1, ok && err == nil
	}
	exts := map[string]string{}
	for _, vs := range vids {
		for _, v := range vs {
			p := pkg.add(v.key, media[v.key])
			exts[path.Ext(p.name)[1:]] = p.mime
		}
	}
	want := func(name string) bool {
		if name == "[Content_Types].xml" {
			return true
		}
		if n, ok := slideNum(name, "ppt/slides/slide", ".xml"); ok {
			_, has := vids[n]
			return has
		}
		if n, ok := slideNum(name, "ppt/slides/_rels/slide", ".xml.rels"); ok {
			_, has := vids[n]
			return has
		}
		return false
	}
	// the relationship ids a slide's videos take, by (slide, video index)
	rid := func(slide, i int, kind string) string {
		return "rIdVideo" + kind + strconv.Itoa(slide+1) + "x" + strconv.Itoa(i)
	}
	edit := func(name string, body []byte) ([]byte, error) {
		s := string(body)
		if name == "[Content_Types].xml" {
			var add strings.Builder
			for ext, mime := range exts {
				if !strings.Contains(s, `Extension="`+ext+`"`) {
					add.WriteString(`<Default Extension="` + ext + `" ContentType="` + mime + `"/>`)
				}
			}
			head, tail, ok := strings.Cut(s, "<Override")
			if !ok {
				head, tail = strings.TrimSuffix(s, "</Types>"), "</Types>"
			} else {
				tail = "<Override" + tail
			}
			return []byte(head + add.String() + tail), nil
		}
		if n, ok := slideNum(name, "ppt/slides/_rels/slide", ".xml.rels"); ok {
			var add strings.Builder
			for i, v := range vids[n] {
				target := "../media/" + path.Base(pkg.parts[v.key].name)
				add.WriteString(`<Relationship Id="` + rid(n, i, "V") + `" Type="` + relVideo + `" Target="` + target + `"/>`)
				add.WriteString(`<Relationship Id="` + rid(n, i, "M") + `" Type="` + relMedia + `" Target="` + target + `"/>`)
			}
			head, found := strings.CutSuffix(strings.TrimSpace(s), "</Relationships>")
			if !found {
				return nil, fmt.Errorf("export: %s does not end as go-ooxml's relationships do", name)
			}
			return []byte(head + add.String() + "</Relationships>"), nil
		}
		n, _ := slideNum(name, "ppt/slides/slide", ".xml")
		const nvPr = `</p:cNvPicPr><p:nvPr/></p:nvPicPr>`
		// the pictures are taken from the last: the later ones keep their index
		vs := vids[n]
		for i := len(vs) - 1; i >= 0; i-- {
			v := vs[i]
			ext := `<p:ext uri="` + extMedia + `"><p14:media xmlns:p14="http://schemas.microsoft.com/office/powerpoint/2010/main" r:embed="` + rid(n, i, "M") + `"/></p:ext>`
			repl := `</p:cNvPicPr><p:nvPr><a:videoFile r:link="` + rid(n, i, "V") + `"/><p:extLst>` + ext + `</p:extLst></p:nvPr></p:nvPicPr>`
			var ok bool
			if s, ok = replaceNth(s, nvPr, repl, v.pic); !ok {
				return nil, fmt.Errorf("export: %s has no picture %d for its video", name, v.pic)
			}
		}
		return []byte(s), nil
	}
	extra := map[string][]byte{}
	for _, k := range pkg.order {
		extra[pkg.parts[k].name] = pkg.parts[k].data
	}
	var order []string
	for _, k := range pkg.order {
		order = append(order, pkg.parts[k].name)
	}
	return rewriteZipExtra(deck, want, edit, extra, order)
}

// odpVideos is the videos of each slide (0-based).
type odpVideos map[int][]videoAt

// addODPVideos puts a draw:plugin before the draw:image of each poster.
func addODPVideos(deck []byte, vids odpVideos, media map[string]doc.Media) ([]byte, error) {
	pkg := videoPackage{dir: "Video"}
	for _, vs := range vids {
		for _, v := range vs {
			pkg.add(v.key, media[v.key])
		}
	}
	want := func(name string) bool { return name == "content.xml" || name == "META-INF/manifest.xml" }
	edit := func(name string, body []byte) ([]byte, error) {
		s := string(body)
		if name == "META-INF/manifest.xml" {
			var add strings.Builder
			for _, k := range pkg.order {
				p := pkg.parts[k]
				add.WriteString(`<manifest:file-entry manifest:full-path="` + p.name + `" manifest:media-type="` + p.mime + `"/>`)
			}
			head, found := strings.CutSuffix(strings.TrimSpace(s), "</manifest:manifest>")
			if !found {
				return nil, fmt.Errorf("export: the manifest does not end as go-odf's does")
			}
			return []byte(head + add.String() + "</manifest:manifest>"), nil
		}
		// the slides are the draw:page elements, in order
		const page = "<draw:page "
		parts := strings.Split(s, page)
		for n, vs := range vids {
			if n+1 >= len(parts) {
				return nil, fmt.Errorf("export: content.xml has no slide %d for its video", n+1)
			}
			chunk := parts[n+1]
			for i := len(vs) - 1; i >= 0; i-- {
				v := vs[i]
				p := pkg.parts[v.key]
				plugin := `<draw:plugin xlink:href="` + p.name + `" xlink:type="simple" xlink:show="embed" xlink:actuate="onLoad" draw:mime-type="` + p.mime + `"/>`
				var ok bool
				if chunk, ok = replaceNth(chunk, "<draw:image ", plugin+"<draw:image ", v.pic); !ok {
					return nil, fmt.Errorf("export: slide %d has no picture %d for its video", n+1, v.pic)
				}
			}
			parts[n+1] = chunk
		}
		return []byte(strings.Join(parts, page)), nil
	}
	extra := map[string][]byte{}
	var order []string
	for _, k := range pkg.order {
		extra[pkg.parts[k].name] = pkg.parts[k].data
		order = append(order, pkg.parts[k].name)
	}
	return rewriteZipExtra(deck, want, edit, extra, order)
}

func pptxPicCount(s ooxml.Shape) int {
	n := 0
	if s.Image > 0 {
		n = 1
	}
	for _, m := range s.Group {
		n += pptxPicCount(m)
	}
	return n
}

func odpPicCount(s odf.Shape) int {
	n := 0
	if s.Kind == odf.ShapeImage {
		n = 1
	}
	for _, m := range s.Group {
		n += odpPicCount(m)
	}
	return n
}

// pptxBoxes are the shapes' boxes in device pixels (k EMU per pixel).
func pptxBoxes(shapes []ooxml.Shape, k float64) []shapeBox {
	out := make([]shapeBox, len(shapes))
	for i, s := range shapes {
		out[i] = shapeBox{x: float64(s.X) / k, y: float64(s.Y) / k, w: float64(s.W) / k, h: float64(s.H) / k,
			pics: pptxPicCount(s), isPic: s.Image > 0 && len(s.Group) == 0}
	}
	return out
}

// odpBoxes are the shapes' boxes in device pixels (px CSS pixels per pixel).
func odpBoxes(shapes []odf.Shape, px float64) []shapeBox {
	out := make([]shapeBox, len(shapes))
	for i, s := range shapes {
		out[i] = shapeBox{x: s.X / px, y: s.Y / px, w: s.W / px, h: s.H / px,
			pics: odpPicCount(s), isPic: s.Kind == odf.ShapeImage && len(s.Group) == 0}
	}
	return out
}

// videoTarget is the address a link is written with in the PDF: that of a
// video file of the deck is its path, relative as the deck wrote it.
func videoTarget(href string) string {
	if rest, ok := strings.CutPrefix(href, doc.VideoLinkPrefix); ok {
		return rest
	}
	return href
}
