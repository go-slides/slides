package main

import (
	"bytes"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"

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
	if o.pdf {
		chosen = append(chosen, "pdf")
	}
	if o.pptx {
		chosen = append(chosen, "pptx")
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
		return "", fmt.Errorf("--pdf, --pptx, --images and --image are exclusive")
	}
	switch strings.ToLower(filepath.Ext(o.output)) {
	case ".pdf":
		return "pdf", nil
	case ".pptx":
		return "pptx", nil
	case ".png":
		return "image:png", nil
	case ".jpg", ".jpeg":
		return "image:jpeg", nil
	}
	return "html", nil
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
	if format == "pptx" && !cmd.Flags().Changed("image-scale") {
		// A deck is shown full screen: marp-cli renders PPTX at 2 by default.
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
	images, err := export.Rasterize(cmd.Context(), b, opts)
	if err != nil {
		return err
	}
	base := strings.TrimSuffix(path, filepath.Ext(path))
	kind, imgFormat, _ := strings.Cut(format, ":")
	switch kind {
	case "pdf":
		var buf bytes.Buffer
		if err := export.PDF(&buf, b, images); err != nil {
			return err
		}
		return o.emit(cmd, path, or(o.output, base+".pdf"), buf.Bytes(), len(images))
	case "pptx":
		var buf bytes.Buffer
		if err := export.PPTX(&buf, b, images); err != nil {
			return err
		}
		return o.emit(cmd, path, or(o.output, base+".pptx"), buf.Bytes(), len(images))
	case "image":
		var buf bytes.Buffer
		if err := encode(&buf, images[0], imgFormat, opts); err != nil {
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
	for i, img := range images {
		var buf bytes.Buffer
		if err := encode(&buf, img, imgFormat, opts); err != nil {
			return err
		}
		name := fmt.Sprintf("%s.%03d.%s", prefix, i+1, ext(imgFormat))
		if err := os.WriteFile(name, buf.Bytes(), 0o644); err != nil {
			return err
		}
	}
	fmt.Fprintf(stderr, "slides: %s => %s.%03d.%s ... (%d slides)\n", path, prefix, 1, ext(imgFormat), len(images))
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
