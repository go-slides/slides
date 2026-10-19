// Command slides builds Marp slide decks — with Go template interpolation and
// slide kinds — without a browser.
//
//	slides deck.md                     write deck.html
//	slides build -o out.html deck.md
//	slides expand deck.md              print plain Marp Markdown
//	slides notes deck.md               print the speaker notes
//	slides kinds deck.md               list the kinds a deck can use
//	slides themes                      list the themes
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/spf13/cobra"

	"github.com/go-slides/slides"
	"github.com/go-slides/slides/deck"
)

// osExit allows tests to override os.Exit.
var osExit = os.Exit

func main() {
	if err := RootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "slides:", err)
		osExit(1)
	}
}

// RootCmd returns the top-level command. Run with a deck and no command,
// it builds the deck, as marp does.
func RootCmd() *cobra.Command {
	o := &options{}
	root := &cobra.Command{
		Use:   "slides [deck.md]",
		Short: "Build Marp slide decks, with Go templates and slide kinds, without a browser",
		Long: `slides reads Marp Markdown decks — Marpit directives, Marp Core's themes —
plus Go template interpolation and slide kinds, and writes them as a
self-contained HTML presentation or as plain Marp Markdown.

Run with a deck and no command, it builds the deck to HTML.`,
		Version:       version(),
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			return build(cmd, o, args[0])
		},
	}
	root.SetVersionTemplate("slides {{.Version}}\n")
	o.persistentFlags(root)
	o.buildFlags(root)

	buildCmd := &cobra.Command{
		Use:   "build deck.md",
		Short: "Write the presentation as HTML (the default command)",
		Args:  cobra.ExactArgs(1),
		RunE:  func(cmd *cobra.Command, args []string) error { return build(cmd, o, args[0]) },
	}
	o.buildFlags(buildCmd)

	expandCmd := &cobra.Command{
		Use:   "expand deck.md",
		Short: "Print the deck as plain Marp Markdown",
		Long: `expand prints the deck as plain Marp Markdown: templates expanded, page
numbers written out, and kinds replaced by the directives they stand for, so
that marp-cli or any Marp tool renders the same slides.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			d, err := parse(o, args[0])
			if err != nil {
				return err
			}
			md, err := d.Markdown()
			if err != nil {
				return err
			}
			return o.write(cmd.OutOrStdout(), md)
		},
	}
	o.outputFlag(expandCmd)

	notesCmd := &cobra.Command{
		Use:   "notes deck.md",
		Short: "Print the speaker notes, one slide after another",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			d, err := parse(o, args[0])
			if err != nil {
				return err
			}
			var b strings.Builder
			for i, s := range d.Slides {
				if i > 0 {
					b.WriteString("\n---\n\n")
				}
				b.WriteString(strings.Join(s.Notes, "\n\n"))
				b.WriteString("\n")
			}
			return o.write(cmd.OutOrStdout(), b.String())
		},
	}
	o.outputFlag(notesCmd)

	kindsCmd := &cobra.Command{
		Use:   "kinds deck.md",
		Short: "List the kinds a deck can use and the directives they set",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			d, err := parse(o, args[0])
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			for _, n := range d.Kinds.Names() {
				dirs, err := d.Kinds.Resolve(n)
				if err != nil {
					return err
				}
				var parts []string
				for _, k := range dirs.Keys() {
					parts = append(parts, fmt.Sprintf("%s=%q", k, dirs.Get(k)))
				}
				fmt.Fprintf(out, "%-10s %s\n", n, strings.Join(parts, " "))
			}
			return nil
		},
	}

	themesCmd := &cobra.Command{
		Use:   "themes",
		Short: "List the themes, built-in and from --theme-set",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			set, err := o.themeSet(".")
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			for _, n := range set.Names() {
				t := set.Get(n)
				w, h := set.Size(t)
				fmt.Fprintf(out, "%-12s %s×%s  %s\n", n, w, h, strings.Join(set.Meta(t, "size"), ", "))
			}
			return nil
		},
	}

	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Args:  cobra.NoArgs,
		Run:   func(cmd *cobra.Command, _ []string) { fmt.Fprintln(cmd.OutOrStdout(), "slides "+version()) },
	}

	root.AddCommand(buildCmd, expandCmd, notesCmd, kindsCmd, themesCmd, versionCmd)
	return root
}

// persistentFlags are the flags every command shares: where the deck's
// configuration, themes, kinds and data come from.
func (o *options) persistentFlags(cmd *cobra.Command) {
	f := cmd.PersistentFlags()
	f.StringVar(&o.configFile, "config", "", "configuration file, HCL if named *.hcl (default: slides.hcl, else slides.yaml, next to the deck)")
	f.BoolVar(&o.noConfig, "no-config", false, "do not read a configuration file")
	f.StringArrayVar(&o.themeSets, "theme-set", nil, "theme CSS file or directory (repeatable)")
	f.StringArrayVar(&o.kindFiles, "kinds", nil, "YAML or HCL (*.hcl) file of kinds (repeatable)")
	f.StringArrayVar(&o.dataFiles, "data", nil, "YAML, JSON or HCL (*.hcl) file of template data (repeatable)")
	f.StringArrayVar(&o.sets, "set", nil, "template data `key=value` (repeatable)")
	f.BoolVar(&o.noTemplate, "no-template", false, "do not expand Go templates")
	f.BoolVar(&o.strictYAML, "strict-yaml", false, "parse directives as strict YAML, as plain Marpit does")
}

func (o *options) outputFlag(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&o.output, "output", "o", "", "output file (- for standard output)")
}

// buildFlags are the HTML build's own flags; the root command takes them
// too, since it builds by default.
func (o *options) buildFlags(cmd *cobra.Command) {
	o.outputFlag(cmd)
	f := cmd.Flags()
	f.BoolVar(&o.html, "html", false, "allow all raw HTML (default: Marp Core's allowlist)")
	f.StringVar(&o.lang, "lang", "", "lang attribute when the deck sets none")
	f.BoolVar(&o.bare, "bare", false, "stack the slides without the presenting script")
	f.BoolVar(&o.pdf, "pdf", false, "write a PDF, one page per slide (also chosen by -o file.pdf)")
	f.BoolVar(&o.pptx, "pptx", false, "write a PowerPoint deck, a picture per slide with its speaker notes (also chosen by -o file.pptx)")
	f.BoolVar(&o.odp, "odp", false, "write an OpenDocument presentation, a picture per slide with its speaker notes (also chosen by -o file.odp)")
	f.StringVar(&o.images, "images", "", "write one image per slide: `png` or jpeg (deck.001.png, ...)")
	f.StringVar(&o.image, "image", "", "write the first slide as an image: `png` or jpeg (also chosen by -o file.png)")
	f.IntVar(&o.jpegQuality, "jpeg-quality", 85, "JPEG quality, 1 to 100")
	f.Float64Var(&o.imageScale, "image-scale", 1, "device pixels per CSS pixel for images and PDF (2 = 2560×1440 for 16:9)")
	f.BoolVar(&o.allowLocalFiles, "allow-local-files", false, "let images and PDF load local files from under the deck's directory")
}

// readDeck reads a deck, "-" meaning standard input.
func readDeck(path string) ([]byte, string, error) {
	if path == "-" {
		b, err := io.ReadAll(os.Stdin)
		return b, "stdin.md", err
	}
	b, err := os.ReadFile(path)
	return b, path, err
}

func parse(o *options, path string) (*deck.Deck, error) {
	src, path, err := readDeck(path)
	if err != nil {
		return nil, err
	}
	cfg, err := o.config(path)
	if err != nil {
		return nil, err
	}
	return deck.Parse(src, cfg.Parse)
}

func build(cmd *cobra.Command, o *options, path string) error {
	src, path, err := readDeck(path)
	if err != nil {
		return err
	}
	cfg, err := o.config(path)
	if err != nil {
		return err
	}
	if f, err := o.format(); err != nil {
		return err
	} else if f != "html" {
		return exportDeck(cmd, o, src, path, cfg, f)
	}
	page, b, err := slides.HTML(src, cfg)
	if err != nil {
		return err
	}
	stderr := cmd.ErrOrStderr()
	for _, w := range b.Warnings {
		fmt.Fprintln(stderr, "slides: warning:", w)
	}
	if o.output == "" && path != "stdin.md" {
		o.output = strings.TrimSuffix(path, filepath.Ext(path)) + ".html"
	}
	if err := o.write(cmd.OutOrStdout(), page); err != nil {
		return err
	}
	if o.output != "" && o.output != "-" {
		fmt.Fprintf(stderr, "slides: %s => %s (%d slides)\n", path, o.output, len(b.Slides))
	}
	return nil
}

func (o *options) write(stdout io.Writer, s string) error {
	if o.output == "" || o.output == "-" {
		_, err := io.WriteString(stdout, s)
		return err
	}
	return os.WriteFile(o.output, []byte(s), 0o644)
}

func version() string {
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" {
		return bi.Main.Version
	}
	return "(devel)"
}
