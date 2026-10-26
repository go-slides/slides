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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/go-slides/slides"
	"github.com/go-slides/slides/deck"
	"github.com/go-slides/slides/editor"
)

// osExit allows tests to override os.Exit.
var osExit = os.Exit

func main() {
	// Ctrl-C ends the watch and the server cleanly: their context is done.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := RootCmd().ExecuteContext(ctx); err != nil {
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
		Args:          cobra.ArbitraryArgs,
		RunE:          func(cmd *cobra.Command, args []string) error { return runBuild(cmd, o, args) },
	}
	root.SetVersionTemplate("slides {{.Version}}\n")
	o.persistentFlags(root)
	o.buildFlags(root)

	buildCmd := &cobra.Command{
		Use:   "build [deck.md...] | -I dir",
		Short: "Write the presentation as HTML (the default command)",
		Long: `build converts each Markdown deck to HTML, or to what the flags and the
output's extension say. Name decks, or a directory (its Markdown files, found
recursively), or give --input-dir to mirror a tree: the decks found under it
are written under --output at the same relative paths. - is standard input.`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return runBuild(cmd, o, args) },
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

	var kindsJSON bool
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
			if kindsJSON {
				list, err := editor.KindList(d.Kinds)
				if err != nil {
					return err
				}
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(list)
			}
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
	kindsCmd.Flags().BoolVar(&kindsJSON, "json", false, "write the kinds as JSON: name, extends and the resolved directives in order")

	themesCmd := &cobra.Command{
		Use:   "themes",
		Short: "List the themes, built-in and from --theme-set",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			set, err := o.themeSet()
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
	f.BoolVar(&o.noEmoji, "no-emoji", false, "leave :shortcodes: and Unicode emoji as text instead of drawing them as Twemoji images")
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
	f.BoolVar(&o.pdf, "pdf", false, "write a PDF, one page per slide, text and shapes as vectors (also chosen by -o file.pdf)")
	f.BoolVar(&o.pdfRaster, "pdf-raster", false, "write a PDF whose pages are each one image of the slide, instead of vectors")
	f.BoolVar(&o.pptx, "pptx", false, "write an editable PowerPoint deck — text boxes, shapes, pictures, fonts embedded — with the speaker notes (also chosen by -o file.pptx)")
	f.BoolVar(&o.pptxRaster, "pptx-raster", false, "write a PowerPoint deck whose slides are each one picture, as marp-cli does")
	f.BoolVar(&o.odp, "odp", false, "write an editable OpenDocument presentation, with the speaker notes (also chosen by -o file.odp)")
	f.BoolVar(&o.odpRaster, "odp-raster", false, "write an OpenDocument presentation whose slides are each one picture")
	f.StringVar(&o.images, "images", "", "write one image per slide: `png` or jpeg (deck.001.png, ...)")
	f.StringVar(&o.image, "image", "", "write the first slide as an image: `png` or jpeg (also chosen by -o file.png)")
	f.IntVar(&o.jpegQuality, "jpeg-quality", 85, "JPEG quality, 1 to 100")
	f.Float64Var(&o.imageScale, "image-scale", 1, "device pixels per CSS pixel for images and PDF (2 = 2560×1440 for 16:9)")
	f.BoolVar(&o.allowLocalFiles, "allow-local-files", false, "let images and PDF load local files from under the deck's directory")
	f.BoolVar(&o.notes, "notes", false, "write the speaker notes as a text file, slides separated by ---, as marp-cli does (also chosen by -o file.txt)")
	f.BoolVar(&o.pdfNotes, "pdf-notes", false, "add the speaker notes to the PDF as note annotations, as marp-cli does")
	f.BoolVar(&o.pdfOutlines, "pdf-outlines", true, "write the PDF's outline, of pages and headings (on by default, unlike marp-cli; --pdf-outlines=false turns it off)")
	f.BoolVar(&o.outlinePages, "pdf-outlines.pages", true, "outline: a bookmark \"Page N\" for each slide")
	f.BoolVar(&o.outlineHeadings, "pdf-outlines.headings", true, "outline: the slides' headings, under their page")
	f.BoolVar(&o.noPDFOutlines, "no-pdf-outlines", false, "same as --pdf-outlines=false, marp-cli's spelling")
	f.BoolVar(&o.noOutlinePages, "no-pdf-outlines.pages", false, "same as --pdf-outlines.pages=false")
	f.BoolVar(&o.noOutlineHeadings, "no-pdf-outlines.headings", false, "same as --pdf-outlines.headings=false")
	f.BoolVar(&o.pdfTags, "pdf-tags", true, "write a tagged PDF, which screen readers read: headings, paragraphs, lists, tables, links, images and formulas with their text, in reading order (PDF/UA-1); --pdf-tags=false turns it off")
	f.BoolVar(&o.noPDFTags, "no-pdf-tags", false, "same as --pdf-tags=false")
	for _, n := range []string{"no-pdf-outlines", "no-pdf-outlines.pages", "no-pdf-outlines.headings", "no-pdf-tags"} {
		_ = f.MarkHidden(n)
	}
	f.BoolVarP(&o.watch, "watch", "w", false, "build again when a deck, a theme, a kinds or data file or the configuration changes, and tell the open pages to reload")
	f.BoolVarP(&o.server, "server", "s", false, "serve a directory, converting its decks on request: deck.md as HTML, deck.md?pdf, ?pptx, ?png, ?jpg, ?notes; reloads the pages when a deck changes. Listens on 127.0.0.1, port 8080 or $PORT")
	f.BoolVarP(&o.preview, "preview", "p", false, "open the result in the default browser: the HTML pages with --watch (implied), the server's index with --server")
	f.StringVar(&o.host, "host", "127.0.0.1", "address the server listens on; anything but the loopback serves the directory to the network")
	f.StringArrayVar(&o.allowedHosts, "allowed-host", nil, "a public host name the server answers to behind a reverse proxy (slides.example.org); repeatable. The server keeps listening on the loopback, and its Host and Origin checks against DNS rebinding accept this name too")
	f.StringVar(&o.template, "template", "", "template of the HTML page: `bespoke` (the default: presenting script, controller, transitions) or bare (the slides stacked, as --bare)")
	f.BoolVar(&o.bespokeOSC, "bespoke.osc", true, "bespoke template: the on-screen controller (--bespoke.osc=false hides it)")
	f.BoolVar(&o.bespokeProgress, "bespoke.progress", false, "bespoke template: a progress bar")
	f.BoolVar(&o.bespokeTransition, "bespoke.transition", true, "bespoke template: play the transition directive with the View Transition API (--bespoke.transition=false turns it off)")
	f.BoolVar(&o.noBespokeOSC, "no-bespoke.osc", false, "same as --bespoke.osc=false, marp-cli's spelling")
	f.BoolVar(&o.noBespokeProgress, "no-bespoke.progress", false, "same as --bespoke.progress=false")
	f.BoolVar(&o.noBespokeTransition, "no-bespoke.transition", false, "same as --bespoke.transition=false")
	f.StringVarP(&o.marpConfigFile, "config-file", "c", "", "marp-cli's configuration file, JSON or YAML (default: .marprc, .marprc.yaml, marp.config.json ... searched from here upward); `--config` is slides' own")
	f.BoolVar(&o.noMarpConfig, "no-config-file", false, "do not read marp-cli's configuration file (--no-config does neither)")
	f.BoolVar(&o.noParallel, "no-parallel", false, "same as --parallel 1, marp-cli's spelling")
	f.BoolVar(&o.pptxEditable, "pptx-editable", false, "marp-cli's spelling of --pptx: the PowerPoint written here is always editable")
	for _, n := range []string{"browser", "browser-path", "browser-protocol", "browser-timeout"} {
		f.String(n, "", "accepted and ignored: slides has no browser")
		_ = f.MarkHidden(n)
	}
	for _, n := range []string{"no-bespoke.osc", "no-bespoke.progress", "no-bespoke.transition", "no-parallel"} {
		_ = f.MarkHidden(n)
	}
	f.StringVarP(&o.inputDir, "input-dir", "I", "", "convert the Markdown files under this directory, written under --output at the same relative paths (next to the decks without --output)")
	f.IntVarP(&o.parallel, "parallel", "P", 5, "decks built at once, at most the number of cores (1 builds them one after the other)")
	f.StringVar(&o.themeOverride, "theme", "", "override the deck's theme: a theme name, or a CSS file (use --theme-set to add themes)")
	f.StringVar(&o.meta.Title, "title", "", "title of the page, over the deck's title directive")
	f.StringVar(&o.meta.Description, "description", "", "description of the page, over the deck's description directive")
	f.StringVar(&o.meta.Author, "author", "", "author of the page, over the deck's author directive")
	f.StringVar(&o.meta.Keywords, "keywords", "", "comma-separated keywords of the page, over the deck's keywords directive")
	f.StringVar(&o.meta.URL, "url", "", "canonical URL of the page, over the deck's url directive")
	f.StringVar(&o.meta.Image, "og-image", "", "Open Graph image URL, over the deck's image directive")
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

// runBuild is the build command: it lists the decks and builds them, up to
// --parallel at once.
func runBuild(cmd *cobra.Command, o *options, args []string) error {
	fl := cmd.Flags()
	stderr := cmd.ErrOrStderr()
	warn := func(s string) { fmt.Fprintln(stderr, "slides: warning:", s) }
	// marp-cli's configuration file gives the flags the command line did not
	cwd, _ := os.Getwd()
	if err := o.applyMarpConfig(fl, cwd, warn); err != nil {
		return err
	}
	for _, n := range []string{"browser", "browser-path", "browser-protocol", "browser-timeout"} {
		if fl.Changed(n) {
			warn("slides has no browser: --" + n + " is ignored")
		}
	}
	switch o.template {
	case "", "bespoke", "bare":
	default:
		return fmt.Errorf("--template %q: want bespoke or bare", o.template)
	}
	if o.noParallel {
		o.parallel = 1
	}
	if o.pptxEditable {
		o.pptx = true
	}
	o.imageScaleSet = fl.Changed("image-scale")
	// marp-cli 4.5.1 takes --pdf-outlines.pages or .headings alone for
	// nothing (the parent option is what its type choice reads), and so do
	// we.
	o.pdfOptionsAsked = o.pdfNotes || (fl.Changed("pdf-outlines") && o.pdfOutlines)
	if o.server {
		return runServer(cmd, o, args)
	}
	jobs, err := o.inputs(args)
	if err != nil {
		return err
	}
	if len(jobs) == 0 {
		if len(args) > 0 {
			return errors.New("not found processable Markdown file(s)")
		}
		if o.inputDir != "" {
			fmt.Fprintf(cmd.ErrOrStderr(), "slides: no Markdown file under %s\n", o.inputDir)
			return nil
		}
		return cmd.Help()
	}
	if o.inputDir == "" && o.output != "" && o.output != "-" && len(jobs) > 1 {
		return errors.New("output path cannot specify with processing multiple files")
	}
	if o.watch || o.preview {
		return runWatch(cmd, o, jobs)
	}
	e := env{ctx: cmd.Context(), stdout: cmd.OutOrStdout(), stderr: cmd.ErrOrStderr()}
	if len(jobs) > 1 {
		fmt.Fprintf(e.stderr, "slides: converting %d decks (up to %d at once)\n", len(jobs), o.workers(len(jobs)))
	}
	return runJobs(e, o, jobs, buildOne)
}

// buildOne builds the deck of j to HTML, or what the options say.
func buildOne(e env, o *options, j job) error {
	src, path, err := readDeck(j.path)
	if err != nil {
		return err
	}
	cfg, err := o.config(path)
	if err != nil {
		return err
	}
	f, err := o.format()
	if err != nil {
		return err
	}
	switch f {
	case "notes":
		return writeNotes(e, o, src, path, cfg)
	case "html":
	default:
		return exportDeck(e, o, src, path, cfg, f)
	}
	if o.hub != nil && path != "stdin.md" {
		// a page built in the watch mode is a file: it opens the socket of the
		// watcher, named by the deck it shows
		// 127.0.0.1 and not localhost, which a browser resolves to ::1 first,
		// where another program may listen on the same port (marp-cli's
		// notifier does)
		cfg.WatchWS = fmt.Sprintf("ws://127.0.0.1:%d/%s", o.hubPort, o.hub.Register(path))
	}
	page, b, err := slides.HTML(src, cfg)
	if err != nil {
		return err
	}
	e.warn(b.Warnings)
	if o.output == "" && path != "stdin.md" {
		o.output = o.base(path) + ".html"
	}
	if err := o.write(e.stdout, page); err != nil {
		return err
	}
	if o.output != "" && o.output != "-" {
		fmt.Fprintf(e.stderr, "slides: %s => %s (%d slides)\n", path, o.output, len(b.Slides))
	}
	return nil
}

func (o *options) write(stdout io.Writer, s string) error {
	if o.output == "" || o.output == "-" {
		_, err := io.WriteString(stdout, s)
		return err
	}
	if err := os.MkdirAll(filepath.Dir(o.output), 0o755); err != nil {
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
