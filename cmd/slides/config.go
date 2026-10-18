package main

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/go-slides/slides"
	"github.com/go-slides/slides/deck"
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
			return cfg, fmt.Errorf("--set %q: want key=value", kv)
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
