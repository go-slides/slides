// Command pdfcheck compares go-slides' vector PDF with its raster render,
// slide by slide: it writes the benchmark decks' PDFs, has poppler's pdftoppm
// rasterise them at the slides' own pixel size, and measures how far each
// page is from the image export.Rasterize paints for the same slide. It also
// reads the text back with pdftotext. It is a development check, not a test:
// it needs poppler, and the numbers are for a person to read.
//
//	go run ./internal/cmd/pdfcheck -out /some/dir/outside/the/repo
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-webengine/engine"

	"github.com/go-slides/slides"
	"github.com/go-slides/slides/deck"
	"github.com/go-slides/slides/export"
	"github.com/go-slides/slides/internal/benchdeck"
)

func main() {
	out := flag.String("out", "", "directory for the PDFs, page renders and diffs (outside any repository)")
	tol := flag.Int("tol", 48, "a pixel differs when a channel is off by more than this")
	flag.Parse()
	if *out == "" {
		fmt.Fprintln(os.Stderr, "pdfcheck: -out is required")
		os.Exit(2)
	}
	if err := insideRepo(*out); err != nil {
		fmt.Fprintln(os.Stderr, "pdfcheck:", err)
		os.Exit(2)
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "pdfcheck:", err)
		os.Exit(1)
	}
	decks := []struct {
		name string
		src  []byte
	}{
		{"generated-100", benchdeck.Large(100)},
		{"gaia-20", benchdeck.Gaia(20)},
		{"math-code-20", benchdeck.MathCode(20)},
	}
	decks = append(decks, struct {
		name string
		src  []byte
	}{"effects", []byte(effects)})
	oracle, _ := filepath.Glob(filepath.Join("testdata", "oracle", "*.md"))
	for _, p := range oracle {
		src, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		decks = append(decks, struct {
			name string
			src  []byte
		}{"oracle-" + strings.TrimSuffix(filepath.Base(p), ".md"), src})
	}
	failed := false
	for _, d := range decks {
		if err := check(*out, d.name, d.src, *tol); err != nil {
			fmt.Fprintf(os.Stderr, "pdfcheck: %s: %v\n", d.name, err)
			failed = true
		}
	}
	if failed {
		os.Exit(1)
	}
}

// effects exercises what the vector painter draws differently or hands to
// the raster fallback: uncover's paginate corner (a hard-stop gradient to
// transparent), a radial gradient, rounded corners, a blurred and a sharp
// shadow, opacity, a fading gradient.
const effects = `---
theme: uncover
paginate: true
---

<style scoped>
h1 { background: radial-gradient(circle, #fc0, #f06); border-radius: 24px; padding: 8px 32px; box-shadow: 0 8px 16px rgba(0,0,0,.4); }
blockquote { background: linear-gradient(90deg, rgba(0,0,255,1), rgba(0,0,255,0)); box-shadow: 6px 6px 0 #0a0; }
p { opacity: .6; }
</style>

# Effects

A paragraph at 60 % opacity.

> A quote on a fading gradient.

---

<style scoped>
section { background: radial-gradient(ellipse at 30% 40%, #fff 0%, #9cf 50%, #036 100%); }
li { border: 3px solid #c00; border-radius: 12px; margin: 6px; }
</style>

## Radial background

- one
- two
`

// insideRepo refuses an output directory inside a git work tree: the page
// renders must never be one `git add -A` away from a commit.
func insideRepo(dir string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	for d := abs; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			return fmt.Errorf("%s is inside the work tree %s; write outside it", abs, d)
		}
		if filepath.Dir(d) == d {
			return nil
		}
	}
}

func check(out, name string, src []byte, tol int) error {
	ctx := context.Background()
	built, err := slides.Render(src, slides.Config{}, deck.FrameBox)
	if err != nil {
		return err
	}
	b := built.Doc()
	opts := export.Options{}

	t0 := time.Now()
	imgs, err := export.Rasterize(ctx, b, opts)
	if err != nil {
		return err
	}
	tRaster := time.Since(t0)

	t0 = time.Now()
	var vec bytes.Buffer
	p := export.NewPDF(b)
	if err := export.EachLayout(ctx, b, opts, func(_ int, l *engine.PageLayout) error { return p.AddLayout(l) }); err != nil {
		return err
	}
	if err := p.Write(&vec); err != nil {
		return err
	}
	tVector := time.Since(t0)

	var ras bytes.Buffer
	if err := export.PDF(&ras, b, imgs); err != nil {
		return err
	}
	pdfPath := filepath.Join(out, name+".pdf")
	if err := os.WriteFile(pdfPath, vec.Bytes(), 0o644); err != nil {
		return err
	}
	fmt.Printf("%s: %d slides; vector PDF %.1f KiB in %v, raster PDF %.1f KiB (raster render %v); fallbacks %d (%d px), images %d, glyphs %d, skipped %d\n",
		name, len(b.Slides), float64(vec.Len())/1024, tVector.Round(time.Millisecond), float64(ras.Len())/1024, tRaster.Round(time.Millisecond),
		p.Stats.Rasterized, p.Stats.RasterPixels, p.Stats.Images, p.Stats.Glyphs, p.Stats.Skipped)

	// Poppler renders each page at the slide's pixel size: 96 dpi, the
	// page being 3/4 of a point per CSS pixel.
	prefix := filepath.Join(out, name+"-page")
	cmd := exec.Command("pdftoppm", "-r", "96", "-png", pdfPath, prefix)
	cmd.Stdin = nil
	if msg, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("pdftoppm: %v: %s", err, msg)
	}
	type res struct {
		i    int
		frac float64
		mean float64
	}
	var all []res
	for i, want := range imgs {
		got, err := readPNG(pagePath(prefix, i+1, len(imgs)))
		if err != nil {
			return err
		}
		frac, mean, diff := compare(want, got, tol)
		all = append(all, res{i, frac, mean})
		if frac > 0.01 {
			_ = writePNG(filepath.Join(out, fmt.Sprintf("%s-diff-%03d.png", name, i+1)), diff)
			_ = writePNG(filepath.Join(out, fmt.Sprintf("%s-raster-%03d.png", name, i+1)), want)
		}
	}
	sort.Slice(all, func(a, b int) bool { return all[a].frac > all[b].frac })
	worst := all[:min(5, len(all))]
	var sum float64
	for _, r := range all {
		sum += r.frac
	}
	fmt.Printf("  pixels off by more than %d: mean %.3f %%, worst:", tol, 100*sum/float64(len(all)))
	for _, r := range worst {
		fmt.Printf(" slide %d %.2f %% (mean abs %.2f)", r.i+1, 100*r.frac, r.mean)
	}
	fmt.Println()

	txt := exec.Command("pdftotext", "-layout", pdfPath, "-")
	txt.Stdin = nil
	text, err := txt.Output()
	if err != nil {
		return fmt.Errorf("pdftotext: %w", err)
	}
	words := strings.Fields(string(text))
	fmt.Printf("  pdftotext: %d words; first: %q\n", len(words), strings.Join(words[:min(12, len(words))], " "))
	return nil
}

func pagePath(prefix string, n, total int) string {
	width := len(fmt.Sprint(total))
	return fmt.Sprintf("%s-%0*d.png", prefix, width, n)
}

func readPNG(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return png.Decode(f)
}

func writePNG(path string, img image.Image) error {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// compare returns the fraction of pixels where a channel differs by more
// than tol, the mean absolute channel difference, and a diff image (red
// where they differ, the raster render faded elsewhere).
func compare(want *image.RGBA, got image.Image, tol int) (float64, float64, *image.RGBA) {
	r := want.Bounds()
	if got.Bounds() != r {
		d := image.NewRGBA(r)
		return 1, 255, d
	}
	diff := image.NewRGBA(r)
	n, off := 0, 0
	var total float64
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			a := want.RGBAAt(x, y)
			cr, cg, cb, _ := got.At(x, y).RGBA()
			dr, dg, db := absd(int(a.R), int(cr>>8)), absd(int(a.G), int(cg>>8)), absd(int(a.B), int(cb>>8))
			total += float64(dr+dg+db) / 3
			n++
			if dr > tol || dg > tol || db > tol {
				off++
				diff.SetRGBA(x, y, color.RGBA{255, 0, 0, 255})
			} else {
				g := uint8(200 + (int(a.R)+int(a.G)+int(a.B))/3*55/255)
				diff.SetRGBA(x, y, color.RGBA{g, g, g, 255})
			}
		}
	}
	return float64(off) / float64(n), total / float64(n), diff
}

func absd(a, b int) int {
	if a > b {
		return a - b
	}
	return b - a
}
