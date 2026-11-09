package themepack

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-slides/slides/theme"
)

const acmeDir = "../testdata/themes/acme"

func TestParseMetaRefuses(t *testing.T) {
	head := "theme {\n name = \"x\"\n version = \"1.0.0\"\n license = \"MIT\"\n}\ncss \"a.css\" {}\n"
	cases := []struct{ name, src, code string }{
		{"function call", strings.Replace(head, `"MIT"`, `file("/etc/passwd")`, 1), "theme.meta"},
		{"env", strings.Replace(head, `"MIT"`, `env("HOME")`, 1), "theme.meta"},
		{"variable", strings.Replace(head, `"MIT"`, `data.x`, 1), "theme.meta"},
		{"syntax", "theme {", "theme.meta"},
		{"no theme block", "css \"a.css\" {}\n", "theme.meta"},
		{"no css", "theme {\n name = \"x\"\n version = \"1.0.0\"\n license = \"MIT\"\n}\n", "theme.meta"},
		{"top-level attribute", head + "x = 1\n", "theme.meta"},
		{"unknown block", head + "script \"x\" {}\n", "theme.meta"},
		{"duplicate css", head + "css \"a.css\" {}\n", "theme.meta"},
		{"bad name", strings.Replace(head, `"x"`, `"../x"`, 1), "theme.meta"},
		{"reserved name", strings.Replace(head, `"x"`, `"default"`, 1), "theme.meta"},
		{"bad version", strings.Replace(head, `1.0.0`, `1.0`, 1), "theme.meta"},
		{"v version", strings.Replace(head, `1.0.0`, `v1.0.0`, 1), "theme.meta"},
		{"bad licence", strings.Replace(head, `"MIT"`, `"not a licence!"`, 1), "theme.meta"},
		{"no licence", strings.Replace(head, " license = \"MIT\"\n", "", 1), "theme.meta"},
		{"unknown attribute", strings.Replace(head, "}\ncss", " evil = 1\n}\ncss", 1), "theme.meta"},
		{"homepage scheme", strings.Replace(head, "}\ncss", " homepage = \"javascript:alert(1)\"\n}\ncss", 1), "theme.meta"},
		{"extends itself", strings.Replace(head, "}\ncss", " extends = \"x\"\n}\ncss", 1), "theme.meta"},
		{"comment end in description", strings.Replace(head, "}\ncss", " description = \"a */ b\"\n}\ncss", 1), "theme.meta"},
		{"newline in description", strings.Replace(head, "}\ncss", " description = \"a\\nb\"\n}\ncss", 1), "theme.meta"},
		{"css path", strings.Replace(head, `"a.css"`, `"../a.css"`, 1), "theme.meta"},
		{"css extension", strings.Replace(head, `"a.css"`, `"a.txt"`, 1), "theme.meta"},
		{"bad sha", strings.Replace(head, `"a.css" {}`, `"a.css" { sha256 = "xyz" }`, 1), "theme.meta"},
		{"font without licence", head + "font \"F\" {\n file = \"f.ttf\"\n}\n", "theme.meta"},
		{"font family with quote", head + "font \"F\\\"; x\" {\n file = \"f.ttf\"\n license = \"MIT\"\n}\n", "theme.meta"},
		{"font style", head + "font \"F\" {\n file = \"f.ttf\"\n license = \"MIT\"\n style = \"wobbly\"\n}\n", "theme.meta"},
		{"asset path", head + "asset \"a\" {\n file = \"/etc/passwd\"\n}\n", "theme.meta"},
		{"kind template", head + "kind \"k\" {\n header = \"{{ .secret }}\"\n}\n", "theme.meta"},
		{"too large", head + "# " + strings.Repeat("x", 70<<10) + "\n", "theme.limit"},
		{"too deep", head + "kind \"k\" {\n x = " + strings.Repeat("[", 100) + strings.Repeat("]", 100) + "\n}\n", "theme.limit"},
		{"not utf8", head + "# \xff\xfe\n", "theme.meta"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseMeta([]byte(c.src), Limits{})
			wantCode(t, err, c.code)
		})
	}
}

func TestParseMetaAccepts(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(acmeDir, MetaFile))
	if err != nil {
		t.Fatal(err)
	}
	m, err := ParseMeta(b, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "acme" || m.Version != "1.0.0" || m.Extends != "default" || m.MinSlides != "0.18.0" ||
		len(m.Fonts) != 1 || m.Fonts[0].Weight != "400" || len(m.Assets) != 2 || len(m.Kinds) != 1 {
		t.Fatalf("unexpected meta: %+v", m)
	}
}

// pkg is a minimal package for the content checks.
func pkg(t testing.TB, css string, extra Files, hcl string) (*Package, error) {
	t.Helper()
	if hcl == "" {
		hcl = "theme {\n name = \"x\"\n version = \"1.0.0\"\n license = \"MIT\"\n}\ncss \"a.css\" {}\n"
	}
	files := Files{MetaFile: []byte(hcl), "a.css": []byte(css)}
	for k, v := range extra {
		files[k] = v
	}
	return FromFiles(files, Limits{})
}

func TestCSSRefusals(t *testing.T) {
	svg := Files{"img/a.svg": []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`)}
	hcl := "theme {\n name = \"x\"\n version = \"1.0.0\"\n license = \"MIT\"\n}\ncss \"a.css\" {}\nasset \"a\" {\n file = \"img/a.svg\"\n}\n"
	bad := map[string]string{
		"import":            `@import "https://evil.example/x.css";`,
		"import upper":      `@IMPORT url(x.css);`,
		"import theme":      `@import-theme "gaia";`,
		"url http":          `a{background:url(https://evil.example/x.png)}`,
		"url http quoted":   `a{background:url("http://evil.example/x.png")}`,
		"url scheme-less":   `a{background:url(//evil.example/x.png)}`,
		"url absolute":      `a{background:url(/etc/passwd)}`,
		"url file":          `a{background:url(file:///etc/passwd)}`,
		"url climbs":        `a{background:url(../x.png)}`,
		"url climbs nested": `a{background:url(img/../../x.png)}`,
		"url undeclared":    `a{background:url(img/other.svg)}`,
		"url uppercase":     `a{background:URL(https://evil.example/x)}`,
		"url data html":     `a{background:url(data:text/html;base64,PHNjcmlwdD4=)}`,
		"url data svg":      `a{background:url(data:image/svg+xml;utf8,<svg/>)}`,
		"url escape":        `a{background:u\72l(https://evil.example/x)}`,
		"import escape":     `@\69mport "x";`,
		"image-set":         `a{background:image-set("https://evil.example/x.png" 1x)}`,
		"webkit image-set":  `a{background:-webkit-image-set("x.png" 1x)}`,
		"src function":      `a{background:src("x.png")}`,
		"expression":        `a{width:expression(alert(1))}`,
		"style break":       `a{} </style><script>alert(1)</script>`,
		"style break upper": `a{} </STYLE><script>alert(1)</script>`,
		"html comment":      `<!-- a{}`,
		"url with space":    `a{background:url(a b)}`,
		"url unclosed":      `a{background:url(a.svg`,
		"query":             `a{background:url(img/a.svg?x=1)}`,
		"fragment":          `a{background:url(img/a.svg#x)}`,
	}
	for name, css := range bad {
		t.Run(name, func(t *testing.T) {
			_, err := pkg(t, css, svg, hcl)
			wantCode(t, err, "theme.css")
		})
	}
	good := map[string]string{
		"plain":                   `a{color:red}`,
		"asset":                   `a{background:url(img/a.svg)}`,
		"asset quoted":            `a{background:url( "img/a.svg" )}`,
		"fragment":                `a{fill:url(#grad)}`,
		"data png":                `a{background:url(data:image/png;base64,AAAA)}`,
		"comment mentions":        `/* @import "x"; url(http://x) */ a{color:red}`,
		"string mentions":         `a::after{content:"@import url(http://x)"}`,
		"string escape":           `a::after{content:"\201C"}`,
		"import-theme in comment": `/* @import-theme "gaia" */`,
	}
	for name, css := range good {
		t.Run("ok "+name, func(t *testing.T) {
			if _, err := pkg(t, css, svg, hcl); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCSSRelativeToItsDirectory(t *testing.T) {
	hcl := "theme {\n name = \"x\"\n version = \"1.0.0\"\n license = \"MIT\"\n}\ncss \"css/a.css\" {}\nasset \"a\" {\n file = \"img/a.svg\"\n}\n"
	files := Files{
		MetaFile:    []byte(hcl),
		"css/a.css": []byte(`a{background:url(../img/a.svg)}`),
		"img/a.svg": []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`),
	}
	p, err := FromFiles(files, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	out, _ := p.CSS()
	if !strings.Contains(out, "data:image/svg+xml;base64,") {
		t.Fatalf("the asset was not inlined: %s", out)
	}
	files["css/a.css"] = []byte(`a{background:url(../../img/a.svg)}`)
	if _, err := FromFiles(files, Limits{}); err == nil {
		t.Fatal("a reference above the root was accepted")
	}
}

func TestSVGRefusals(t *testing.T) {
	hcl := "theme {\n name = \"x\"\n version = \"1.0.0\"\n license = \"MIT\"\n}\ncss \"a.css\" {}\nasset \"a\" {\n file = \"a.svg\"\n}\n"
	const open = `<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink">`
	bad := map[string]string{
		"script":          open + `<script>alert(1)</script></svg>`,
		"script upper":    open + `<SCRIPT>alert(1)</SCRIPT></svg>`,
		"onload":          `<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"/>`,
		"onclick":         open + `<rect onclick="alert(1)"/></svg>`,
		"foreignObject":   open + `<foreignObject><div xmlns="http://www.w3.org/1999/xhtml">x</div></foreignObject></svg>`,
		"iframe":          open + `<iframe src="https://evil.example"/></svg>`,
		"external href":   open + `<image href="https://evil.example/x.png"/></svg>`,
		"xlink href":      open + `<image xlink:href="https://evil.example/x.png"/></svg>`,
		"data href":       open + `<image href="data:image/png;base64,AAAA"/></svg>`,
		"javascript href": open + `<a href="javascript:alert(1)"><rect/></a></svg>`,
		"use external":    open + `<use href="https://evil.example/x.svg#a"/></svg>`,
		"set href":        open + `<a><set attributeName="href" to="javascript:alert(1)"/></a></svg>`,
		"animate href":    open + `<a><animate attributeName="href" values="javascript:alert(1)"/></a></svg>`,
		"doctype":         `<!DOCTYPE svg [<!ENTITY x "y">]>` + open + `</svg>`,
		"style import":    open + `<style>@import url(https://evil.example/x.css);</style></svg>`,
		"style url":       open + `<style>rect{fill:url(https://evil.example/x)}</style></svg>`,
		"style attr url":  open + `<rect style="fill:url(https://evil.example/x)"/></svg>`,
		"fill url":        open + `<rect fill="url(https://evil.example/x)"/></svg>`,
		"stylesheet pi":   `<?xml-stylesheet href="https://evil.example/x.css"?>` + open + `</svg>`,
		"not svg root":    `<html xmlns="http://www.w3.org/1999/xhtml"><body/></html>`,
		"not xml":         `<svg><`,
		"empty":           ``,
	}
	for name, s := range bad {
		t.Run(name, func(t *testing.T) {
			_, err := pkg(t, "a{}", Files{"a.svg": []byte(s)}, hcl)
			wantCode(t, err, "theme.svg")
		})
	}
	good := open + `<defs><linearGradient id="g"/></defs><style>rect{fill:url(#g)}</style><rect fill="url(#g)"/><use href="#g"/></svg>`
	if _, err := pkg(t, "a{}", Files{"a.svg": []byte(good)}, hcl); err != nil {
		t.Fatalf("a clean SVG refused: %v", err)
	}
}

func TestFontsAndImages(t *testing.T) {
	hcl := "theme {\n name = \"x\"\n version = \"1.0.0\"\n license = \"MIT\"\n}\ncss \"a.css\" {}\nfont \"F\" {\n file = \"f.ttf\"\n license = \"MIT\"\n}\n"
	_, err := pkg(t, "a{}", Files{"f.ttf": []byte("not a font at all")}, hcl)
	wantCode(t, err, "theme.font")
	_, err = FromFiles(Files{MetaFile: []byte(hcl), "a.css": []byte("a{}"), "f.ttf": make([]byte, 5<<20)}, Limits{})
	wantCode(t, err, "theme.font")

	ahcl := "theme {\n name = \"x\"\n version = \"1.0.0\"\n license = \"MIT\"\n}\ncss \"a.css\" {}\nasset \"a\" {\n file = \"a.png\"\n}\n"
	_, err = pkg(t, "a{}", Files{"a.png": []byte("GIF89a not a png")}, ahcl)
	wantCode(t, err, "theme.archive")
	_, err = pkg(t, "a{}", Files{"a.png": make([]byte, 5<<20)}, ahcl)
	wantCode(t, err, "theme.limit")
}

func TestDeclaredFiles(t *testing.T) {
	_, err := pkg(t, "a{}", Files{"stray.js": []byte("alert(1)")}, "")
	wantCode(t, err, "theme.meta")
	if _, err := pkg(t, "a{}", Files{"LICENSE": []byte("MIT"), "README.md": []byte("x")}, ""); err != nil {
		t.Fatalf("licence and readme refused: %v", err)
	}
	_, err = FromFiles(Files{MetaFile: []byte("theme {\n name = \"x\"\n version = \"1.0.0\"\n license = \"MIT\"\n}\ncss \"a.css\" {}\n")}, Limits{})
	wantCode(t, err, "theme.meta")
	_, err = FromFiles(Files{"a.css": []byte("a{}")}, Limits{})
	wantCode(t, err, "theme.meta")
}

func TestChecksums(t *testing.T) {
	const sum = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" // sha256("abc")
	hcl := func(s string) string {
		return "theme {\n name = \"x\"\n version = \"1.0.0\"\n license = \"MIT\"\n}\ncss \"a.css\" { sha256 = \"" + s + "\" }\n"
	}
	if _, err := pkg(t, "abc", nil, hcl(sum)); err != nil {
		t.Fatalf("a right checksum refused: %v", err)
	}
	_, err := pkg(t, "abd", nil, hcl(sum))
	wantCode(t, err, "theme.checksum")
}

func TestCheckVersion(t *testing.T) {
	p, err := pkg(t, "a{}", nil, "theme {\n name = \"x\"\n version = \"1.0.0\"\n license = \"MIT\"\n min_slides = \"0.18.0\"\n}\ncss \"a.css\" {}\n")
	if err != nil {
		t.Fatal(err)
	}
	wantCode(t, p.CheckVersion("v0.17.9"), "theme.version")
	for _, ok := range []string{"v0.18.0", "0.18.1", "v1.0.0", "(devel)", ""} {
		if err := p.CheckVersion(ok); err != nil {
			t.Errorf("%q refused: %v", ok, err)
		}
	}
}

func TestPackAcmeRoundTrip(t *testing.T) {
	meta, b, err := Pack(acmeDir, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if meta.Name != "acme" {
		t.Fatal(meta.Name)
	}
	_, b2, _ := Pack(acmeDir, Limits{})
	if !bytes.Equal(b, b2) {
		t.Fatal("packing twice gives different bytes")
	}
	p, err := Read(bytes.NewReader(b), int64(len(b)), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	css, err := p.CSS()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`@import-theme "default";`, `@font-face{font-family:"Roboto Mono"`, "data:font/ttf;base64,", "data:image/svg+xml;base64,", "data:image/png;base64,", "@theme acme"} {
		if !strings.Contains(css, want) {
			t.Errorf("CSS lacks %q", want)
		}
	}
	if strings.Contains(css, "assets/logo.svg") {
		t.Error("a package path is left in the CSS")
	}
	set := theme.NewSet()
	th, err := set.Add(css)
	if err != nil || th.Name != "acme" || th.MetaValue("license") != "MIT" {
		t.Fatalf("the set did not take the theme: %v %+v", err, th)
	}
	kinds, err := p.Kinds()
	if err != nil || kinds["acme-title"] == nil {
		t.Fatalf("kinds: %v %v", err, kinds)
	}
}

func TestPackRefusesLinks(t *testing.T) {
	dir := t.TempDir()
	css, _ := os.ReadFile(filepath.Join(acmeDir, "theme.css"))
	if err := os.WriteFile(filepath.Join(dir, MetaFile), []byte("theme {\n name = \"x\"\n version = \"1.0.0\"\n license = \"MIT\"\n}\ncss \"theme.css\" {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(t.TempDir(), "secret.css")
	if err := os.WriteFile(secret, css, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(dir, "theme.css")); err != nil {
		t.Skip("no symlinks here")
	}
	if _, _, err := Pack(dir, Limits{}); err == nil {
		t.Fatal("a symbolic link was packed")
	}
}
