// Command slides builds Marp slide decks — with Go template interpolation and
// slide kinds — without a browser.
//
//	slides deck.md                 write deck.html
//	slides build -o out.html deck.md
//	slides expand deck.md          print plain Marp Markdown
//	slides notes deck.md           print the speaker notes
//	slides kinds deck.md           list the kinds a deck can use
//	slides themes                  list the themes
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/go-slides/slides"
	"github.com/go-slides/slides/deck"
	"github.com/go-slides/slides/theme"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		var u usageError
		if errors.As(err, &u) {
			fmt.Fprintln(os.Stderr, "slides:", err)
			os.Exit(2)
		}
		fmt.Fprintln(os.Stderr, "slides:", err)
		os.Exit(1)
	}
}

type usageError struct{ error }

const usage = `usage: slides [command] [flags] deck.md

commands:
  build    write the presentation as HTML (the default)
  expand   print the deck as plain Marp Markdown: templates expanded,
           kinds replaced by the directives they stand for
  notes    print the speaker notes
  kinds    list the kinds the deck can use and what they set
  themes   list the themes
  version  print the version

Run "slides <command> -h" for the flags of a command.
`

func run(args []string, stdout, stderr io.Writer) error {
	cmd := "build"
	if len(args) > 0 {
		switch args[0] {
		case "build", "expand", "notes", "kinds", "themes", "version":
			cmd, args = args[0], args[1:]
		case "help", "-h", "-help", "--help":
			fmt.Fprint(stdout, usage)
			return nil
		}
	}
	if cmd == "version" {
		fmt.Fprintln(stdout, version())
		return nil
	}

	fs := flag.NewFlagSet("slides "+cmd, flag.ContinueOnError)
	fs.SetOutput(stderr)
	var o options
	o.register(fs, cmd)
	positional, err := parseInterspersed(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return usageError{err}
	}

	if cmd == "themes" {
		set, err := o.themeSet(".")
		if err != nil {
			return err
		}
		for _, n := range set.Names() {
			t := set.Get(n)
			w, h := set.Size(t)
			fmt.Fprintf(stdout, "%-12s %s×%s  %s\n", n, w, h, strings.Join(set.Meta(t, "size"), ", "))
		}
		return nil
	}

	if len(positional) != 1 {
		return usageError{fmt.Errorf("%s wants one deck (got %d arguments)\n\n%s", cmd, len(positional), usage)}
	}
	path := positional[0]
	var src []byte
	if path == "-" {
		src, err = io.ReadAll(os.Stdin)
		path = "stdin.md"
	} else {
		src, err = os.ReadFile(path)
	}
	if err != nil {
		return err
	}
	cfg, err := o.config(path)
	if err != nil {
		return err
	}

	switch cmd {
	case "expand":
		d, err := deck.Parse(src, cfg.Parse)
		if err != nil {
			return err
		}
		md, err := d.Markdown()
		if err != nil {
			return err
		}
		return o.write(stdout, md)
	case "notes":
		d, err := deck.Parse(src, cfg.Parse)
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
		return o.write(stdout, b.String())
	case "kinds":
		d, err := deck.Parse(src, cfg.Parse)
		if err != nil {
			return err
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
			fmt.Fprintf(stdout, "%-10s %s\n", n, strings.Join(parts, " "))
		}
		return nil
	}

	page, b, err := slides.HTML(src, cfg)
	if err != nil {
		return err
	}
	for _, w := range b.Warnings {
		fmt.Fprintln(stderr, "slides: warning:", w)
	}
	if o.output == "" && path != "stdin.md" {
		o.output = strings.TrimSuffix(path, filepath.Ext(path)) + ".html"
	}
	if err := o.write(stdout, page); err != nil {
		return err
	}
	if o.output != "" && o.output != "-" {
		fmt.Fprintf(stderr, "slides: %s => %s (%d slides)\n", path, o.output, len(b.Slides))
	}
	return nil
}

// parseInterspersed parses flags wherever they appear, before or after the
// deck ("slides deck.md -o out.html"): Go's flag package stops at the first
// argument that is not a flag. "--" still ends the flags.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return positional, nil
		}
		if len(args) > len(rest) && args[len(args)-len(rest)-1] == "--" {
			return append(positional, rest...), nil
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
}

func version() string {
	v := "(devel)"
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" {
		v = bi.Main.Version
	}
	return "slides " + v
}

// options are the command-line flags.
type options struct {
	output     string
	configFile string
	noConfig   bool
	themeSets  multi
	kindFiles  multi
	dataFiles  multi
	sets       multi
	html       bool
	lang       string
	bare       bool
	noTemplate bool
	strictYAML bool
}

type multi []string

func (m *multi) String() string     { return strings.Join(*m, ",") }
func (m *multi) Set(v string) error { *m = append(*m, v); return nil }

func (o *options) register(fs *flag.FlagSet, cmd string) {
	fs.StringVar(&o.output, "o", "", "output file (- for standard output)")
	fs.StringVar(&o.configFile, "config", "", "configuration file (default: slides.yaml next to the deck, if any)")
	fs.BoolVar(&o.noConfig, "no-config", false, "do not read a configuration file")
	fs.Var(&o.themeSets, "theme-set", "theme CSS file or directory (repeatable)")
	if cmd == "themes" {
		return
	}
	fs.Var(&o.kindFiles, "kinds", "YAML file of kinds (repeatable)")
	fs.Var(&o.dataFiles, "data", "YAML or JSON file of template data (repeatable)")
	fs.Var(&o.sets, "set", "template data key=value (repeatable)")
	fs.BoolVar(&o.noTemplate, "no-template", false, "do not expand Go templates")
	fs.BoolVar(&o.strictYAML, "strict-yaml", false, "parse directives as strict YAML, as plain Marpit does")
	if cmd == "build" {
		fs.BoolVar(&o.html, "html", false, "allow all raw HTML (default: Marp Core's allowlist)")
		fs.StringVar(&o.lang, "lang", "", "lang attribute when the deck sets none")
		fs.BoolVar(&o.bare, "bare", false, "stack the slides without the presenting script")
	}
}

func (o *options) write(stdout io.Writer, s string) error {
	if o.output == "" || o.output == "-" {
		_, err := io.WriteString(stdout, s)
		return err
	}
	return os.WriteFile(o.output, []byte(s), 0o644)
}

// fileConfig is slides.yaml.
type fileConfig struct {
	Kinds  any      `yaml:"kinds"`
	Themes []string `yaml:"themes"`
	Data   any      `yaml:"data"`
	HTML   *bool    `yaml:"html"`
	Lang   string   `yaml:"lang"`
}

// config assembles the build configuration: built-ins, then the
// configuration file, then the flags.
func (o *options) config(deckPath string) (slides.Config, error) {
	baseDir := filepath.Dir(deckPath)
	cfg := slides.Config{Parse: deck.Options{
		BaseDir:    baseDir,
		Name:       deckPath,
		NoTemplate: o.noTemplate,
		StrictYAML: o.strictYAML,
		Data:       map[string]any{},
		Kinds:      deck.Kinds{},
	}}
	fc, fcDir, err := o.readConfig(baseDir)
	if err != nil {
		return cfg, err
	}
	themeFiles := []string{}
	if fc != nil {
		if fc.Kinds != nil {
			k, err := kindsFrom(fc.Kinds, fcDir)
			if err != nil {
				return cfg, err
			}
			cfg.Parse.Kinds = cfg.Parse.Kinds.Merge(k)
		}
		if fc.Data != nil {
			d, err := dataFrom(fc.Data, fcDir)
			if err != nil {
				return cfg, err
			}
			maps.Copy(cfg.Parse.Data, d)
		}
		for _, t := range fc.Themes {
			themeFiles = append(themeFiles, resolve(fcDir, t))
		}
		if fc.HTML != nil && *fc.HTML {
			cfg.HTML = deck.HTMLAll
		}
		cfg.Lang = fc.Lang
	}
	for _, f := range o.kindFiles {
		k, err := deck.LoadKindsFile(f)
		if err != nil {
			return cfg, err
		}
		cfg.Parse.Kinds = cfg.Parse.Kinds.Merge(k)
	}
	for _, f := range o.dataFiles {
		d, err := dataFrom(f, ".")
		if err != nil {
			return cfg, err
		}
		maps.Copy(cfg.Parse.Data, d)
	}
	for _, kv := range o.sets {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			return cfg, usageError{fmt.Errorf("-set %q: want key=value", kv)}
		}
		cfg.Parse.Data[k] = v
	}
	if o.html {
		cfg.HTML = deck.HTMLAll
	}
	if o.lang != "" {
		cfg.Lang = o.lang
	}
	cfg.Bare = o.bare
	set, err := o.themeSet(baseDir, themeFiles...)
	if err != nil {
		return cfg, err
	}
	cfg.Themes = set
	return cfg, nil
}

func (o *options) readConfig(baseDir string) (*fileConfig, string, error) {
	if o.noConfig {
		return nil, "", nil
	}
	path := o.configFile
	if path == "" {
		for _, name := range []string{"slides.yaml", "slides.yml"} {
			p := filepath.Join(baseDir, name)
			if _, err := os.Stat(p); err == nil {
				path = p
				break
			}
		}
		if path == "" {
			return nil, "", nil
		}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	var fc fileConfig
	if err := yaml.Unmarshal(b, &fc); err != nil {
		return nil, "", fmt.Errorf("%s: %v", path, err)
	}
	return &fc, filepath.Dir(path), nil
}

func (o *options) themeSet(baseDir string, extra ...string) (*theme.Set, error) {
	set := theme.NewSet()
	paths := append(extra, o.themeSets...)
	for _, p := range paths {
		files := []string{p}
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			files, _ = filepath.Glob(filepath.Join(p, "*.css"))
		}
		for _, f := range files {
			b, err := os.ReadFile(f)
			if err != nil {
				return nil, err
			}
			if _, err := set.Add(string(b)); err != nil {
				return nil, fmt.Errorf("%s: %v", f, err)
			}
		}
	}
	return set, nil
}

// kindsFrom reads kinds given inline or as file paths.
func kindsFrom(v any, dir string) (deck.Kinds, error) {
	switch x := v.(type) {
	case string:
		return deck.LoadKindsFile(resolve(dir, x))
	case []any:
		k := deck.Kinds{}
		for _, p := range x {
			s, ok := p.(string)
			if !ok {
				return nil, fmt.Errorf("kinds: want file paths, got %T", p)
			}
			f, err := deck.LoadKindsFile(resolve(dir, s))
			if err != nil {
				return nil, err
			}
			k = k.Merge(f)
		}
		return k, nil
	case map[string]any:
		b, err := yaml.Marshal(x)
		if err != nil {
			return nil, err
		}
		return deck.ParseKindsYAML(b, "configuration kinds")
	}
	return nil, fmt.Errorf("kinds: want a mapping or file paths, got %T", v)
}

// dataFrom reads template data given inline or as file paths.
func dataFrom(v any, dir string) (map[string]any, error) {
	switch x := v.(type) {
	case map[string]any:
		return x, nil
	case string:
		b, err := os.ReadFile(resolve(dir, x))
		if err != nil {
			return nil, err
		}
		m := map[string]any{}
		if err := yaml.Unmarshal(b, &m); err != nil {
			return nil, fmt.Errorf("%s: %v", x, err)
		}
		return m, nil
	case []any:
		m := map[string]any{}
		for _, p := range x {
			d, err := dataFrom(p, dir)
			if err != nil {
				return nil, err
			}
			maps.Copy(m, d)
		}
		return m, nil
	}
	return nil, fmt.Errorf("data: want a mapping or file paths, got %T", v)
}

// resolve joins a relative path to dir and leaves an absolute one alone.
func resolve(dir, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(dir, path)
}
