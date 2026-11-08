package themepack

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"image"
	_ "image/gif"  // registers the decoder DecodeConfig reads
	_ "image/jpeg" // registers the decoder DecodeConfig reads
	_ "image/png"  // registers the decoder DecodeConfig reads
	"io"
	"path"
	"strings"

	"github.com/go-opentype/opentype"

	"github.com/go-slides/slides/diag"
)

func cssErr(file, format string, args ...any) error {
	return diag.Errorf("theme.css", "theme package: %s: "+format, append([]any{file}, args...)...)
}

func svgErr(file, format string, args ...any) error {
	return diag.Errorf("theme.svg", "theme package: %s: "+format, append([]any{file}, args...)...)
}

// dataMIME are the media types a data: URL may carry. SVG is not one: an SVG
// is checked only as a file of the package (see checkSVG).
var dataMIME = map[string]bool{
	"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true,
	"font/woff2": true, "font/woff": true, "font/ttf": true, "font/otf": true,
	"application/font-woff": true, "application/font-woff2": true,
}

// mimeOf is the media type of a package file by extension.
var mimeOf = map[string]string{
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif",
	".webp": "image/webp", ".svg": "image/svg+xml",
	".woff2": "font/woff2", ".woff": "font/woff", ".ttf": "font/ttf", ".otf": "font/otf",
}

// refFunc resolves one url() reference of a style sheet to the text that goes
// in its place, or refuses it.
type refFunc func(ref string) (string, error)

// forbiddenFuncs are CSS functions that load a resource or run code and
// whose argument this scanner does not read: refused outright.
var forbiddenFuncs = map[string]bool{
	"image-set": true, "-webkit-image-set": true, "src": true, "image": true,
	"cross-fade": true, "-webkit-cross-fade": true, "element": true, "-moz-element": true,
	"expression": true, "paint": true,
}

func isIdentByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c >= 0x80
}

// scanCSS reads a style sheet token by token enough to find what loads
// something: it refuses @import and @import-theme, every function that loads
// a resource except url(), backslash escapes outside strings (which could
// spell any of them), and a sheet that could close the <style> element it is
// written into. Each url() goes through ref, and its result takes its place.
// file names the sheet in errors.
func scanCSS(file, src string, ref refFunc) (string, error) {
	low := strings.ToLower(src)
	if strings.Contains(low, "</") || strings.Contains(low, "<!--") {
		return "", cssErr(file, "contains </ or <!--, which could end the <style> element")
	}
	var out strings.Builder
	out.Grow(len(src))
	for i := 0; i < len(src); {
		c := src[i]
		switch {
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			end := strings.Index(src[i+2:], "*/")
			if end < 0 {
				out.WriteString(src[i:])
				return out.String(), nil
			}
			out.WriteString(src[i : i+2+end+2])
			i += 2 + end + 2
		case c == '"' || c == '\'':
			j := i + 1
			for j < len(src) && src[j] != c && src[j] != '\n' {
				if src[j] == '\\' {
					j++
				}
				j++
			}
			j = min(j+1, len(src))
			out.WriteString(src[i:j])
			i = j
		case c == '\\':
			return "", cssErr(file, "a backslash escape outside a string (it could spell @import or url)")
		case c == '@':
			j := i + 1
			for j < len(src) && isIdentByte(src[j]) {
				j++
			}
			switch strings.ToLower(src[i+1 : j]) {
			case "import":
				return "", cssErr(file, "@import loads a file; a package puts its styles in css blocks")
			case "import-theme":
				return "", cssErr(file, "@import-theme: name the base theme with extends in theme.hcl")
			}
			out.WriteString(src[i:j])
			i = j
		case isIdentByte(c):
			j := i
			for j < len(src) && isIdentByte(src[j]) {
				j++
			}
			id := strings.ToLower(src[i:j])
			if j < len(src) && src[j] == '(' {
				if forbiddenFuncs[id] {
					return "", cssErr(file, "%s() loads a resource the package check cannot follow; use url()", id)
				}
				if id == "url" {
					arg, end, err := urlArg(file, src, j+1)
					if err != nil {
						return "", err
					}
					repl, err := ref(arg)
					if err != nil {
						return "", err
					}
					out.WriteString(`url("` + repl + `")`)
					i = end
					continue
				}
			}
			out.WriteString(src[i:j])
			i = j
		default:
			out.WriteByte(c)
			i++
		}
	}
	return out.String(), nil
}

// urlArg reads what follows "url(" up to and including the ")", returning the
// reference and the offset after the parenthesis.
func urlArg(file, src string, i int) (string, int, error) {
	for i < len(src) && (src[i] == ' ' || src[i] == '\t' || src[i] == '\n' || src[i] == '\r') {
		i++
	}
	var arg string
	if i < len(src) && (src[i] == '"' || src[i] == '\'') {
		q := src[i]
		j := i + 1
		for j < len(src) && src[j] != q {
			if src[j] == '\\' || src[j] == '\n' {
				return "", 0, cssErr(file, "a url() string with an escape or a line break")
			}
			j++
		}
		if j >= len(src) {
			return "", 0, cssErr(file, "unterminated url() string")
		}
		arg = src[i+1 : j]
		i = j + 1
	} else {
		j := i
		for j < len(src) && src[j] != ')' {
			if strings.IndexByte(" \t\r\n\"'(\\", src[j]) >= 0 {
				return "", 0, cssErr(file, "a url() with a quote, a space or an escape in an unquoted reference")
			}
			j++
		}
		arg = src[i:j]
		i = j
	}
	for i < len(src) && (src[i] == ' ' || src[i] == '\t' || src[i] == '\n' || src[i] == '\r') {
		i++
	}
	if i >= len(src) || src[i] != ')' {
		return "", 0, cssErr(file, "a url() that does not close")
	}
	return arg, i + 1, nil
}

// checkData accepts a data: URL of an allowed media type.
func checkData(file, ref string) error {
	head, _, ok := strings.Cut(ref[len("data:"):], ",")
	if !ok {
		return cssErr(file, "a data: URL without a comma")
	}
	mime, _, _ := strings.Cut(head, ";")
	if !dataMIME[strings.ToLower(mime)] {
		return cssErr(file, "a data: URL of type %q (allowed: images and fonts, not SVG)", mime)
	}
	return nil
}

// resolveRef resolves a relative reference from the directory of the file that
// holds it, to an entry name; it refuses what leaves the package.
func resolveRef(file, ref string) (string, error) {
	if ref == "" {
		return "", cssErr(file, "an empty url()")
	}
	if strings.HasPrefix(ref, "/") {
		return "", cssErr(file, "url(%s) is absolute, it leaves the package", ref)
	}
	if i := strings.IndexAny(ref, ":/?#\\"); i >= 0 && ref[i] == ':' {
		return "", cssErr(file, "url(%s) has a scheme, it leaves the package", ref)
	}
	if strings.ContainsAny(ref, "?#\\") {
		return "", cssErr(file, "url(%s) has a query, a fragment or a backslash", ref)
	}
	target := path.Join(pathDir(file), ref)
	if target == ".." || strings.HasPrefix(target, "../") {
		return "", cssErr(file, "url(%s) climbs out of the package", ref)
	}
	if err := ValidName(target, 0); err != nil {
		return "", cssErr(file, "url(%s): %v", ref, err)
	}
	return target, nil
}

// checkSVG refuses an SVG that can run a script, handle an event or load
// something: script, foreignObject, frames and embeds, set/animate on an
// href, event-handler attributes, javascript: values, any href that is not
// a "#" fragment, url() that is not a fragment, a DOCTYPE or entity.
func checkSVG(file string, b []byte) error {
	dec := xml.NewDecoder(bytes.NewReader(b))
	dec.Strict = true
	banned := map[string]bool{
		"script": true, "foreignobject": true, "iframe": true, "object": true, "embed": true,
		"audio": true, "video": true, "canvas": true, "applet": true, "link": true, "meta": true,
		"base": true, "handler": true, "listener": true, "set": true, "html": true, "body": true,
	}
	fragmentOnly := func(ref string) (string, error) {
		if strings.HasPrefix(ref, "#") {
			return ref, nil
		}
		return "", svgErr(file, "url(%s) is not a fragment of this file", ref)
	}
	var stack []string
	var style strings.Builder
	sawRoot := false
	for n := 0; ; n++ {
		if n > 500000 {
			return limitErr("%s has more than 500000 XML tokens", file)
		}
		tok, err := dec.RawToken()
		if err == io.EOF {
			break
		}
		if err != nil {
			return svgErr(file, "not well formed XML: %v", err)
		}
		switch t := tok.(type) {
		case xml.Directive:
			return svgErr(file, "has a DOCTYPE or entity declaration")
		case xml.ProcInst:
			if t.Target != "xml" {
				return svgErr(file, "has a processing instruction <?%s?>", t.Target)
			}
		case xml.StartElement:
			name := strings.ToLower(t.Name.Local)
			if !sawRoot {
				sawRoot = true
				if name != "svg" {
					return svgErr(file, "the root element is <%s>, not <svg>", t.Name.Local)
				}
			}
			if banned[name] {
				return svgErr(file, "has a <%s> element", t.Name.Local)
			}
			stack = append(stack, name)
			for _, a := range t.Attr {
				an := strings.ToLower(a.Name.Local)
				val := strings.ToLower(strings.TrimSpace(a.Value))
				switch {
				case strings.HasPrefix(an, "on"):
					return svgErr(file, "has the event handler %q", a.Name.Local)
				case an == "href":
					if !strings.HasPrefix(a.Value, "#") {
						return svgErr(file, "has an href that leaves the file: %q", a.Value)
					}
				}
				if an == "attributename" && strings.Contains(val, "href") {
					return svgErr(file, "animates an href")
				}
				if strings.Contains(val, "javascript:") || strings.Contains(val, "vbscript:") {
					return svgErr(file, "has a script URL in %q", a.Name.Local)
				}
				if strings.Contains(val, "url(") || strings.Contains(val, "@import") || an == "style" {
					if _, err := scanSVGCSS(file, a.Value, fragmentOnly); err != nil {
						return err
					}
				}
			}
		case xml.EndElement:
			if len(stack) > 0 {
				if stack[len(stack)-1] == "style" {
					if _, err := scanSVGCSS(file, style.String(), fragmentOnly); err != nil {
						return err
					}
					style.Reset()
				}
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			if len(stack) > 0 && stack[len(stack)-1] == "style" {
				style.Write(t)
			}
		}
	}
	if !sawRoot {
		return svgErr(file, "is empty")
	}
	return nil
}

// scanSVGCSS is scanCSS for style the SVG carries, reporting as theme.svg.
func scanSVGCSS(file, src string, ref refFunc) (string, error) {
	out, err := scanCSS(file, src, ref)
	if err != nil && diag.CodeOf(err) == "theme.css" {
		return "", diag.Errorf("theme.svg", "%s", err.Error())
	}
	return out, err
}

// checkImage checks a raster image: the extension names the format, the
// header says the same, the dimensions are sane.
func checkImage(file string, b []byte, lim Limits) error {
	if int64(len(b)) > lim.MaxImage {
		return limitErr("%s is %d bytes, the limit is %d", file, len(b), lim.MaxImage)
	}
	ext := strings.ToLower(path.Ext(file))
	if ext == ".webp" {
		if len(b) < 12 || string(b[:4]) != "RIFF" || string(b[8:12]) != "WEBP" {
			return diag.Errorf("theme.archive", "theme package: %s is not a WebP image", file)
		}
		return nil
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return diag.Errorf("theme.archive", "theme package: %s is not a readable image: %v", file, err)
	}
	want := map[string]string{".png": "png", ".jpg": "jpeg", ".jpeg": "jpeg", ".gif": "gif"}[ext]
	if want != format {
		return diag.Errorf("theme.archive", "theme package: %s is a %s image named like a %s", file, format, want)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > 16384 || cfg.Height > 16384 || cfg.Width*cfg.Height > 64<<20 {
		return limitErr("%s is %dx%d pixels, the limit is 16384 a side and 64 megapixels", file, cfg.Width, cfg.Height)
	}
	return nil
}

// checkFont parses a font under the size bound.
func checkFont(file string, b []byte, lim Limits) error {
	if int64(len(b)) > lim.MaxFont {
		return diag.Errorf("theme.font", "theme package: %s is %d bytes, the font limit is %d", file, len(b), lim.MaxFont)
	}
	switch strings.ToLower(path.Ext(file)) {
	case ".woff2", ".woff", ".ttf", ".otf":
	default:
		return diag.Errorf("theme.font", "theme package: %s: a font is .woff2, .woff, .ttf or .otf", file)
	}
	if _, err := opentype.Parse(b); err != nil {
		return diag.Errorf("theme.font", "theme package: %s is not a font: %v", file, err)
	}
	return nil
}

func dataURI(file string, b []byte) string {
	return "data:" + mimeOf[strings.ToLower(path.Ext(file))] + ";base64," + base64.StdEncoding.EncodeToString(b)
}
