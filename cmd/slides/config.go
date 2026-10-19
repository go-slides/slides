package main

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"go.yaml.in/yaml/v3"

	"github.com/go-slides/slides"
	"github.com/go-slides/slides/deck"
	"github.com/go-slides/slides/internal/hclx"
	"github.com/go-slides/slides/theme"
)

// options are the command-line flags.
type options struct {
	output     string
	configFile string
	noConfig   bool
	themeSets  []string
	kindFiles  []string
	dataFiles  []string
	sets       []string
	html       bool
	lang       string
	bare       bool
	noTemplate bool
	strictYAML bool

	pdf         bool
	pptx        bool
	images      string
	image       string
	jpegQuality int

	imageScale      float64
	allowLocalFiles bool
}

// fileConfig is slides.hcl or slides.yaml.
type fileConfig struct {
	Kinds  any      `yaml:"kinds"`
	Themes []string `yaml:"themes"`
	Data   any      `yaml:"data"`
	HTML   *bool    `yaml:"html"`
	Lang   string   `yaml:"lang"`

	// hcl is slides.hcl's source and path, whose kind blocks are read
	// once the template data is known.
	hcl     []byte
	hclPath string
}

// configNames are the configuration files looked up next to the deck, in
// order.
var configNames = []string{"slides.hcl", "slides.yaml", "slides.yml"}

// config assembles the build configuration: built-ins, then the
// configuration file, then the flags. The template data comes first, so
// that HCL kinds files can use it.
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
	data := cfg.Parse.Data
	if fc != nil && fc.Data != nil {
		d, err := dataFrom(fc.Data, fcDir, data)
		if err != nil {
			return cfg, err
		}
		maps.Copy(data, d)
	}
	for _, f := range o.dataFiles {
		d, err := dataFrom(f, ".", data)
		if err != nil {
			return cfg, err
		}
		maps.Copy(data, d)
	}
	for _, kv := range o.sets {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			return cfg, fmt.Errorf("--set %q: want key=value", kv)
		}
		data[k] = v
	}
	env := deck.HCLEnv{Data: data, BaseDir: fcDir}
	if fc == nil {
		env.BaseDir = baseDir
	}

	themeFiles := []string{}
	if fc != nil {
		if fc.Kinds != nil {
			k, err := kindsFrom(fc.Kinds, fcDir, env)
			if err != nil {
				return cfg, err
			}
			cfg.Parse.Kinds = cfg.Parse.Kinds.Merge(k)
		}
		if fc.hcl != nil {
			k, err := deck.ParseKindsHCL(fc.hcl, fc.hclPath, env)
			if err != nil {
				return cfg, err
			}
			cfg.Parse.Kinds = cfg.Parse.Kinds.Merge(k)
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
		k, err := deck.LoadKindsFileEnv(f, env)
		if err != nil {
			return cfg, err
		}
		cfg.Parse.Kinds = cfg.Parse.Kinds.Merge(k)
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
		for _, name := range configNames {
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
	if deck.IsHCL(path) {
		fc, err := decodeHCLConfig(b, path)
		return fc, filepath.Dir(path), err
	}
	var fc fileConfig
	if err := yaml.Unmarshal(b, &fc); err != nil {
		return nil, "", fmt.Errorf("%s: %v", path, err)
	}
	return &fc, filepath.Dir(path), nil
}

// decodeHCLConfig reads slides.hcl: the attributes themes, html, lang, data
// and kinds (file paths), and kind blocks. Attributes see the functions but
// no data: the data attribute is where data comes from.
func decodeHCLConfig(src []byte, path string) (*fileConfig, error) {
	body, err := hclx.Parse(src, path, hcl.InitialPos)
	if err != nil {
		return nil, err
	}
	ctx := deck.HCLEnv{BaseDir: filepath.Dir(path)}.Context()
	fc := &fileConfig{hcl: src, hclPath: path}
	for _, a := range hclx.Attributes(body) {
		v, err := hclx.Eval(a, ctx)
		if err != nil {
			return nil, err
		}
		g := hclx.ToGo(v)
		bad := func(want string) error {
			return hclx.Errorf(a.Expr.Range(), "Invalid "+a.Name, "%s wants %s, got %s", a.Name, want, v.Type().FriendlyName())
		}
		switch a.Name {
		case "themes":
			list, ok := g.([]any)
			if !ok && g != nil {
				return nil, bad("a list of paths")
			}
			for _, t := range list {
				s, ok := t.(string)
				if !ok {
					return nil, bad("a list of paths")
				}
				fc.Themes = append(fc.Themes, s)
			}
		case "html":
			b, ok := g.(bool)
			if !ok {
				return nil, bad("a bool")
			}
			fc.HTML = &b
		case "lang":
			s, ok := g.(string)
			if !ok {
				return nil, bad("a string")
			}
			fc.Lang = s
		case "data":
			fc.Data = g
		case "kinds":
			fc.Kinds = g
		default:
			return nil, hclx.Errorf(a.NameRange, "Unsupported attribute", "%q is not a configuration key (want themes, html, lang, data or kinds)", a.Name)
		}
	}
	// Kind blocks are read later, with the data; a wrong block is an
	// error now.
	for _, b := range body.Blocks {
		if b.Type != "kind" {
			return nil, hclx.Errorf(b.TypeRange, "Unsupported block type", "only kind blocks are expected here, not %q", b.Type)
		}
	}
	return fc, nil
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
func kindsFrom(v any, dir string, env deck.HCLEnv) (deck.Kinds, error) {
	switch x := v.(type) {
	case string:
		return deck.LoadKindsFileEnv(resolve(dir, x), env)
	case []any:
		k := deck.Kinds{}
		for _, p := range x {
			s, ok := p.(string)
			if !ok {
				return nil, fmt.Errorf("kinds: want file paths, got %T", p)
			}
			f, err := deck.LoadKindsFileEnv(resolve(dir, s), env)
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

// dataFrom reads template data given inline or as file paths: YAML or JSON,
// or HCL for a .hcl file, whose expressions see the data read so far.
func dataFrom(v any, dir string, sofar map[string]any) (map[string]any, error) {
	switch x := v.(type) {
	case map[string]any:
		return x, nil
	case string:
		return deck.LoadDataFile(resolve(dir, x), deck.HCLEnv{Data: maps.Clone(sofar)})
	case []any:
		m := map[string]any{}
		for _, p := range x {
			d, err := dataFrom(p, dir, sofar)
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
