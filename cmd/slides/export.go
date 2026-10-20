package main

import (
	"bytes"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-webengine/engine"
	"github.com/spf13/cobra"

	"github.com/go-slides/slides"
	"github.com/go-slides/slides/deck"
	"github.com/go-slides/slides/export"
)

// format is what build writes: "html", "pdf", "images:png", "images:jpeg",
// "image:png" or "image:jpeg". The flags say it; without them, the output
// file's extension does, as in marp-cli.
func (o *options) format() (string, error) {
	var chosen []string
	if o.pdf || o.pdfRaster {
		chosen = append(chosen, "pdf")
	}
	if o.pptx || o.pptxRaster {
		chosen = append(chosen, "pptx")
	}
	if o.odp || o.odpRaster {
		chosen = append(chosen, "odp")
	}
	if o.notes {
		chosen = append(chosen, "notes")
	}
	for name, v := range map[string]string{"images": o.images, "image": o.image} {
		if v == "" {
			continue
		}
		f, err := imageFormat(v)
		if err != nil {
			return "", fmt.Errorf("--%s: %w", name, err)
		}
		chosen = append(chosen, name+":"+f)
	}
	switch len(chosen) {
	case 0:
	case 1:
		return chosen[0], nil
	default:
		return "", fmt.Errorf("--pdf, --pptx, --odp, --notes, --images and --image are exclusive")
	}
	switch strings.ToLower(filepath.Ext(o.output)) {
	case ".html", ".htm":
		return "html", nil
	case ".txt":
		return "notes", nil
	case ".pdf":
		return "pdf", nil
	case ".pptx":
		return "pptx", nil
	case ".odp":
		return "odp", nil
	case ".png":
		return "image:png", nil
	case ".jpg", ".jpeg":
		return "image:jpeg", nil
	}
	if o.pdfOptionsAsked {
		// As marp-cli: --pdf-notes or --pdf-outlines asks for a PDF when
		// nothing else says what to write.
		return "pdf", nil
	}
	return "html", nil
}

// pdfOptions is what the PDF carries. The outline is on unless --pdf-outlines
// is turned off, which marp-cli's default is not: it writes none unless asked.
// go-slides has always written one, and an outline costs a few hundred bytes
// and makes a deck navigable, so the default stays and --pdf-outlines=false
// (or marp-cli's --no-pdf-outlines) turns it off. --pdf-outlines.pages and
// .headings choose the entries, as in marp-cli.
func (o *options) pdfOptions() export.PDFOptions {
	on := o.pdfOutlines && !o.noPDFOutlines
	return export.PDFOptions{
		Notes:           o.pdfNotes,
		OutlinePages:    on && o.outlinePages && !o.noOutlinePages,
		OutlineHeadings: on && o.outlineHeadings && !o.noOutlineHeadings,
	}
}

func imageFormat(v string) (string, error) {
	switch strings.ToLower(v) {
	case "png":
		return "png", nil
	case "jpeg", "jpg":
		return "jpeg", nil
	}
	return "", fmt.Errorf("%q is not png or jpeg", v)
}

// exportDeck renders the deck with go-webengine and writes images or a PDF.
func exportDeck(cmd *cobra.Command, o *options, src []byte, path string, cfg slides.Config, format string) error {
	b, err := slides.Render(src, cfg, deck.FrameBox)
	if err != nil {
		return err
	}
	stderr := cmd.ErrOrStderr()
	for _, w := range b.Warnings {
		fmt.Fprintln(stderr, "slides: warning:", w)
	}
	dir, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return err
	}
	if o.imageScale <= 0 || o.imageScale > 8 {
		return fmt.Errorf("--image-scale %v: want a value in (0, 8]", o.imageScale)
	}
	if (format == "pptx" || format == "odp") && !cmd.Flags().Changed("image-scale") {
		// A deck is shown full screen: marp-cli renders PPTX at 2 by
		// default, and an OpenDocument deck is shown the same way.
		o.imageScale = 2
	}
	opts := export.Options{
		BaseURL:     "file://" + filepath.ToSlash(dir) + "/",
		JPEGQuality: o.jpegQuality,
		Scale:       o.imageScale,
	}
	if o.allowLocalFiles {
		// Only the deck's own directory, as marp-cli's --allow-local-files
		// is meant to be used: a deck from elsewhere cannot read ~/.ssh.
		opts.FileRoot = dir
	}
	// The slides are rendered a few at a time and each image is written as
	// soon as it is its turn, so that a deck is never held as images.
	ctx := cmd.Context()
	base := strings.TrimSuffix(path, filepath.Ext(path))
	kind, imgFormat, _ := strings.Cut(format, ":")
	switch kind {
	case "pdf":
		pdf := export.NewPDFWith(b, o.pdfOptions())
		var err error
		if o.pdfRaster {
			// The escape hatch: every page one image, as before vectors.
			err = export.Each(ctx, b, opts, func(_ int, img *image.RGBA) error {
				pdf.Add(img)
				return nil
			})
		} else {
			err = export.EachLayout(ctx, b, opts, func(_ int, l *engine.PageLayout) error {
				return pdf.AddLayout(l)
			})
		}
		if err != nil {
			return err
		}
		var buf bytes.Buffer
		if err := pdf.Write(&buf); err != nil {
			return err
		}
		return o.emit(cmd, path, or(o.output, base+".pdf"), buf.Bytes(), len(b.Slides))
	case "pptx":
		pptx := export.NewPPTX(b)
		var err error
		if o.pptxRaster {
			// One picture a slide, PNG-encoded by the worker that rendered it.
			err = export.EachEncoded(ctx, b, opts, "png", func(_ int, png []byte) error {
				pptx.Add(png)
				return nil
			})
		} else {
			err = export.EachLayout(ctx, b, opts, func(_ int, l *engine.PageLayout) error {
				return pptx.AddLayout(l)
			})
		}
		if err != nil {
			return err
		}
		var buf bytes.Buffer
		if err := pptx.Write(&buf); err != nil {
			return err
		}
		return o.emit(cmd, path, or(o.output, base+".pptx"), buf.Bytes(), len(b.Slides))
	case "odp":
		odp := export.NewODP(b)
		var err error
		if o.odpRaster {
			err = export.EachEncoded(ctx, b, opts, "png", func(_ int, png []byte) error {
				odp.Add(png)
				return nil
			})
		} else {
			err = export.EachLayout(ctx, b, opts, func(_ int, l *engine.PageLayout) error {
				return odp.AddLayout(l)
			})
		}
		if err != nil {
			return err
		}
		var buf bytes.Buffer
		if err := odp.Write(&buf); err != nil {
			return err
		}
		return o.emit(cmd, path, or(o.output, base+".odp"), buf.Bytes(), len(b.Slides))
	case "image":
		if len(b.Slides) == 0 {
			return fmt.Errorf("%s: the deck has no slide", path)
		}
		// Only the first slide is written, so only it is rendered.
		first := *b
		first.Slides = b.Slides[:1]
		var buf bytes.Buffer
		if err := export.Each(ctx, &first, opts, func(_ int, img *image.RGBA) error {
			return encode(&buf, img, imgFormat, opts)
		}); err != nil {
			return err
		}
		return o.emit(cmd, path, or(o.output, base+"."+ext(imgFormat)), buf.Bytes(), 1)
	}
	// One file per slide: deck.001.png, deck.002.png, ...
	prefix := base
	if o.output != "" && o.output != "-" {
		prefix = strings.TrimSuffix(o.output, filepath.Ext(o.output))
	}
	if err := os.MkdirAll(filepath.Dir(prefix), 0o755); err != nil {
		return err
	}
	// Each image is encoded by the worker that rendered it.
	if err := export.EachEncoded(ctx, b, opts, imgFormat, func(i int, data []byte) error {
		name := fmt.Sprintf("%s.%03d.%s", prefix, i+1, ext(imgFormat))
		return os.WriteFile(name, data, 0o644)
	}); err != nil {
		return err
	}
	fmt.Fprintf(stderr, "slides: %s => %s.%03d.%s ... (%d slides)\n", path, prefix, 1, ext(imgFormat), len(b.Slides))
	return nil
}

func encode(buf *bytes.Buffer, img image.Image, format string, opts export.Options) error {
	if format == "jpeg" {
		return export.JPEG(buf, img, opts)
	}
	return export.PNG(buf, img)
}

func ext(format string) string {
	if format == "jpeg" {
		return "jpg"
	}
	return format
}

// emit writes one output file, or standard output for "-".
func (o *options) emit(cmd *cobra.Command, src, name string, data []byte, slides int) error {
	if name == "-" {
		_, err := cmd.OutOrStdout().Write(data)
		return err
	}
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(name, data, 0o644); err != nil {
		return err
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "slides: %s => %s (%d slides)\n", src, name, slides)
	return nil
}

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
