// Command officecheck compares go-slides' editable PPTX and ODP with its
// raster render, slide by slide: it writes the benchmark decks as editable
// decks, has LibreOffice convert each to PDF and poppler's pdftoppm render the
// pages at the slides' own pixel size, and measures how far each is from the
// image export.Rasterize paints for the same slide. It reads the text back
// with pdftotext. A development check, not a test: it needs LibreOffice and
// poppler, and the numbers are for a person to read.
//
//	go run ./internal/cmd/officecheck --out /some/dir/outside/the/repo
package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/go-webengine/engine"

	"github.com/go-slides/slides"
	"github.com/go-slides/slides/deck"
	"github.com/go-slides/slides/export"
	"github.com/go-slides/slides/internal/benchdeck"
)

var osExit = os.Exit

func main() {
	if err := RootCmd().Execute(); err != nil {
		osExit(1)
	}
}

// RootCmd is the command.
func RootCmd() *cobra.Command {
	var out, soffice, only string
	var tol int
	var formats []string
	cmd := &cobra.Command{
		Use:          "officecheck",
		Short:        "Compare the editable PPTX/ODP, rendered by LibreOffice, with the raster render",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if out == "" {
				return fmt.Errorf("--out is required")
			}
			if err := insideRepo(out); err != nil {
				return err
			}
			if err := os.MkdirAll(out, 0o755); err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			for _, d := range decks() {
				if only != "" && !strings.Contains(d.name, only) {
					continue
				}
				for _, f := range formats {
					if err := check(w, out, soffice, d.name, d.src, f, tol); err != nil {
						return fmt.Errorf("%s %s: %w", d.name, f, err)
					}
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&out, "out", "", "directory for the decks, PDFs, page renders and diffs (outside any repository)")
	cmd.Flags().StringVar(&soffice, "soffice", "/Applications/LibreOffice.app/Contents/MacOS/soffice", "LibreOffice's soffice")
	cmd.Flags().IntVar(&tol, "tol", 48, "a pixel differs when a channel is off by more than this")
	cmd.Flags().StringSliceVar(&formats, "formats", []string{"pptx", "odp"}, "formats to check")
	cmd.Flags().StringVar(&only, "decks", "", "only the decks whose name contains this")
	return cmd
}

type namedDeck struct {
	name string
	src  []byte
}

func decks() []namedDeck {
	ds := []namedDeck{
		{"generated-100", benchdeck.Large(100)},
		{"gaia-20", benchdeck.Gaia(20)},
		{"math-code-20", benchdeck.MathCode(20)},
		{"effects", []byte(effects)},
	}
	oracle, _ := filepath.Glob(filepath.Join("testdata", "oracle", "*.md"))
	for _, p := range oracle {
		if src, err := os.ReadFile(p); err == nil {
			ds = append(ds, namedDeck{"oracle-" + strings.TrimSuffix(filepath.Base(p), ".md"), src})
		}
	}
	return ds
}

// effects is the deck pdfcheck uses for what is drawn differently.
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

// insideRepo refuses an output directory inside a git work tree.
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

type adder interface {
	AddLayout(*engine.PageLayout) error
	Write(io.Writer) error
}

func check(w io.Writer, out, soffice, name string, src []byte, format string, tol int) error {
	ctx := context.Background()
	built, err := slides.Render(src, slides.Config{}, deck.FrameBox)
	if err != nil {
		return err
	}
	b := built.Doc()
	opts := export.Options{}
	imgs, err := export.Rasterize(ctx, b, opts)
	if err != nil {
		return err
	}
	var a adder
	switch format {
	case "pptx":
		a = export.NewPPTX(b)
	case "odp":
		a = export.NewODP(b)
	default:
		return fmt.Errorf("unknown format %q", format)
	}
	t0 := time.Now()
	if err := export.EachLayout(ctx, b, opts, func(_ int, l *engine.PageLayout) error { return a.AddLayout(l) }); err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := a.Write(&buf); err != nil {
		return err
	}
	took := time.Since(t0)
	file := filepath.Join(out, name+"."+format)
	if err := os.WriteFile(file, buf.Bytes(), 0o644); err != nil {
		return err
	}
	// LibreOffice renders the deck as a PDF, poppler the PDF's pages.
	conv := exec.Command(soffice, "--headless", "--norestore", "--nologo", "--convert-to", "pdf", "--outdir", out, file)
	conv.Stdin = nil
	if msg, err := conv.CombinedOutput(); err != nil {
		return fmt.Errorf("soffice: %v: %s", err, msg)
	}
	pdfPath := filepath.Join(out, name+".pdf")
	if _, err := os.Stat(pdfPath); err != nil {
		return fmt.Errorf("LibreOffice wrote no PDF: %w", err)
	}
	prefix := filepath.Join(out, name+"-"+format)
	pp := exec.Command("pdftoppm", "-r", "96", "-png", pdfPath, prefix)
	pp.Stdin = nil
	if msg, err := pp.CombinedOutput(); err != nil {
		return fmt.Errorf("pdftoppm: %v: %s", err, msg)
	}
	type res struct {
		i          int
		frac, mean float64
	}
	var all []res
	for i, want := range imgs {
		got, err := readPNG(pagePath(prefix, i+1, len(imgs)))
		if err != nil {
			return err
		}
		frac, mean, diff := compare(want, got, tol)
		all = append(all, res{i, frac, mean})
		if frac > 0.02 {
			_ = writePNG(filepath.Join(out, fmt.Sprintf("%s-%s-diff-%03d.png", name, format, i+1)), diff)
			_ = writePNG(filepath.Join(out, fmt.Sprintf("%s-raster-%03d.png", name, i+1)), want)
		}
	}
	sort.Slice(all, func(x, y int) bool { return all[x].frac > all[y].frac })
	var sum float64
	for _, r := range all {
		sum += r.frac
	}
	fmt.Fprintf(w, "%s %s: %d slides, %.1f KiB in %v; pixels off by more than %d: mean %.2f %%, worst:", name, format, len(b.Slides),
		float64(buf.Len())/1024, took.Round(time.Millisecond), tol, 100*sum/float64(len(all)))
	for _, r := range all[:min(4, len(all))] {
		fmt.Fprintf(w, " slide %d %.2f %%", r.i+1, 100*r.frac)
	}
	txt := exec.Command("pdftotext", "-layout", pdfPath, "-")
	txt.Stdin = nil
	text, err := txt.Output()
	if err != nil {
		return fmt.Errorf("pdftotext: %w", err)
	}
	words := strings.Fields(string(text))
	fmt.Fprintf(w, "; pdftotext %d words: %q\n", len(words), strings.Join(words[:min(10, len(words))], " "))
	return nil
}

func pagePath(prefix string, n, total int) string {
	return fmt.Sprintf("%s-%0*d.png", prefix, len(fmt.Sprint(total)), n)
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
// than tol, the mean absolute channel difference, and a diff image.
func compare(want *image.RGBA, got image.Image, tol int) (float64, float64, *image.RGBA) {
	// LibreOffice's page can come out a pixel wider (1281 for 1280): compare
	// what both have.
	r := want.Bounds().Intersect(got.Bounds())
	if r.Dx() < want.Bounds().Dx()-2 || r.Dy() < want.Bounds().Dy()-2 {
		return 1, 255, image.NewRGBA(want.Bounds())
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
