package themepack

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-slides/slides/theme"
)

// Load time of a theme: the loose CSS file against the same theme packed.
//
//	go test ./themepack -run '^$' -bench Load -benchmem
//
// Loose is what --theme-set does with a .css file: read it, parse it.
// PackedCSSOnly is a package of that one sheet (the ZIP, theme.hcl and the
// checks, none of the fonts). Packed is the whole example package: the sheet,
// a 180 KB font that is parsed and embedded, an SVG and a PNG that are
// checked and embedded.
func BenchmarkLoadLoose(b *testing.B) {
	file := filepath.Join(acmeDir, "theme.css")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		src, err := os.ReadFile(file)
		if err != nil {
			b.Fatal(err)
		}
		// the packed sheet names its theme in a comment; so must the loose one
		if _, err := theme.Parse("/* @theme acme */\n" + string(src)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkLoadPackedCSSOnly(b *testing.B) {
	css, err := os.ReadFile(filepath.Join(acmeDir, "theme.css"))
	if err != nil {
		b.Fatal(err)
	}
	css = bytes.ReplaceAll(css, []byte("url(assets/logo.svg) no-repeat right center / contain"), []byte("none"))
	css = bytes.ReplaceAll(css, []byte(`url("assets/bg.png") center / cover`), []byte("#123"))
	zipped, err := WriteFiles(Files{
		MetaFile:    []byte("theme {\n name = \"acme\"\n version = \"1.0.0\"\n license = \"MIT\"\n extends = \"default\"\n}\ncss \"theme.css\" {}\n"),
		"theme.css": css,
	})
	if err != nil {
		b.Fatal(err)
	}
	file := filepath.Join(b.TempDir(), "acme.slidestheme")
	if err := os.WriteFile(file, zipped, 0o644); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		loadPacked(b, file)
	}
}

func BenchmarkLoadPacked(b *testing.B) {
	_, zipped, err := Pack(acmeDir, Limits{})
	if err != nil {
		b.Fatal(err)
	}
	file := filepath.Join(b.TempDir(), "acme.slidestheme")
	if err := os.WriteFile(file, zipped, 0o644); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		loadPacked(b, file)
	}
}

func loadPacked(b *testing.B, file string) {
	p, err := Open(file, Limits{})
	if err != nil {
		b.Fatal(err)
	}
	css, err := p.CSS()
	if err != nil {
		b.Fatal(err)
	}
	if _, err := theme.Parse(css); err != nil {
		b.Fatal(err)
	}
}
