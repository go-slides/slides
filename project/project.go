// Package project assembles a deck's build configuration the way the slides
// command does: the built-ins, then slides.hcl or slides.yaml found next to
// the deck, then what the caller asks for (the command's flags).
//
// It reads through a vfs.FS, so that an editor can run the same assembly
// over the files it holds, without a disk.
package project

import (
	"fmt"
	"maps"
	"path/filepath"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"go.yaml.in/yaml/v3"

	"github.com/go-slides/slides"
	"github.com/go-slides/slides/deck"
	"github.com/go-slides/slides/diag"
	"github.com/go-slides/slides/internal/hclx"
	"github.com/go-slides/slides/theme"
	"github.com/go-slides/slides/vfs"
)

// Options are what the caller adds to the configuration file; the command
// fills them from its flags.
type Options struct {
	// ConfigFile names the configuration file; "" looks for ConfigNames
	// next to the deck.
	ConfigFile string
	// NoConfig reads no configuration file.
	NoConfig bool
	// ThemeSets are theme CSS files or directories of them.
	ThemeSets []string
	// KindFiles are YAML or HCL kinds files.
	KindFiles []string
	// DataFiles are YAML, JSON or HCL template data files.
	DataFiles []string
	// Sets are key=value template data.
	Sets []string
	// HTML allows all raw HTML.
	HTML bool
	// Lang is the lang attribute when the deck sets none.
	Lang       string
	Bare       bool
	NoTemplate bool
	StrictYAML bool
	NoEmoji    bool
	// FS is what is read; nil means the disk.
	FS vfs.FS
}

// ConfigNames are the configuration files looked up next to the deck, in
// order.
var ConfigNames = []string{"slides.hcl", "slides.yaml", "slides.yml"}

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

// Load assembles the build configuration of the deck at deckPath. The
// template data comes first, so that HCL kinds files can use it.
func Load(deckPath string, o Options) (slides.Config, error) {
	fsys := vfs.Or(o.FS)
	baseDir := filepath.Dir(deckPath)
	cfg := slides.Config{Parse: deck.Options{
		BaseDir:    baseDir,
		Name:       deckPath,
		NoTemplate: o.NoTemplate,
		StrictYAML: o.StrictYAML,
		NoEmoji:    o.NoEmoji,
		Data:       map[string]any{},
		Kinds:      deck.Kinds{},
		FS:         o.FS,
	}}
	fc, fcDir, err := readConfig(fsys, o, baseDir)
	if err != nil {
		return cfg, err
	}
	data := cfg.Parse.Data
	if fc != nil && fc.Data != nil {
		d, err := dataFrom(o.FS, fc.Data, fcDir, data)
		if err != nil {
			return cfg, err
		}
		maps.Copy(data, d)
	}
	for _, f := range o.DataFiles {
		d, err := dataFrom(o.FS, f, ".", data)
		if err != nil {
			return cfg, err
		}
		maps.Copy(data, d)
	}
	for _, kv := range o.Sets {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			return cfg, fmt.Errorf("--set %q: want key=value", kv)
		}
		data[k] = v
	}
	env := deck.HCLEnv{Data: data, BaseDir: fcDir, FS: o.FS}
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
	for _, f := range o.KindFiles {
		k, err := deck.LoadKindsFileEnv(f, env)
		if err != nil {
			return cfg, err
		}
		cfg.Parse.Kinds = cfg.Parse.Kinds.Merge(k)
	}
	if o.HTML {
		cfg.HTML = deck.HTMLAll
	}
	if o.Lang != "" {
		cfg.Lang = o.Lang
	}
	cfg.Bare = o.Bare
	set, err := ThemeSet(o.FS, append(themeFiles, o.ThemeSets...)...)
	if err != nil {
		return cfg, err
	}
	cfg.Themes = set
	return cfg, nil
}

// ConfigPath returns the configuration file Load would read for a deck in
// baseDir, "" if none.
func ConfigPath(o Options, baseDir string) string {
	if o.NoConfig {
		return ""
	}
	if o.ConfigFile != "" {
		return o.ConfigFile
	}
	fsys := vfs.Or(o.FS)
	for _, name := range ConfigNames {
		p := filepath.Join(baseDir, name)
		if _, err := fsys.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func readConfig(fsys vfs.FS, o Options, baseDir string) (*fileConfig, string, error) {
	path := ConfigPath(o, baseDir)
	if path == "" {
		return nil, "", nil
	}
	b, err := fsys.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	if deck.IsHCL(path) {
		fc, err := decodeHCLConfig(o.FS, b, path)
		return fc, filepath.Dir(path), err
	}
	var fc fileConfig
	if err := yaml.Unmarshal(b, &fc); err != nil {
		return nil, "", diag.Wrap("yaml.syntax", fmt.Errorf("%s: %v", path, err))
	}
	return &fc, filepath.Dir(path), nil
}

// decodeHCLConfig reads slides.hcl: the attributes themes, html, lang, data
// and kinds (file paths), and kind blocks. Attributes see the functions but
// no data: the data attribute is where data comes from.
func decodeHCLConfig(fsys vfs.FS, src []byte, path string) (*fileConfig, error) {
	body, err := hclx.Parse(src, path, hcl.InitialPos)
	err = diag.Wrap("hcl.syntax", err)
	if err != nil {
		return nil, err
	}
	ctx := deck.HCLEnv{BaseDir: filepath.Dir(path), FS: fsys}.Context()
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
			return nil, diag.Wrap("hcl.unsupported", hclx.Errorf(a.NameRange, "Unsupported attribute", "%q is not a configuration key (want themes, html, lang, data or kinds)", a.Name))
		}
	}
	// Kind blocks are read later, with the data; a wrong block is an
	// error now.
	for _, b := range body.Blocks {
		if b.Type != "kind" {
			return nil, diag.Wrap("hcl.unsupported", hclx.Errorf(b.TypeRange, "Unsupported block type", "only kind blocks are expected here, not %q", b.Type))
		}
	}
	return fc, nil
}

// ThemeSet returns the built-in themes plus the theme CSS at paths: files,
// or directories whose *.css files are read.
func ThemeSet(fsys vfs.FS, paths ...string) (*theme.Set, error) {
	f := vfs.Or(fsys)
	set := theme.NewSet()
	for _, p := range paths {
		files := []string{p}
		if st, err := f.Stat(p); err == nil && st.IsDir() {
			files = nil
			entries, err := f.ReadDir(p)
			if err != nil {
				return nil, err
			}
			for _, e := range entries {
				// The command globbed *.css: the same, case-sensitive match.
				if !e.IsDir() && filepath.Ext(e.Name()) == ".css" {
					files = append(files, filepath.Join(p, e.Name()))
				}
			}
		}
		for _, file := range files {
			b, err := f.ReadFile(file)
			if err != nil {
				return nil, err
			}
			if _, err := set.Add(string(b)); err != nil {
				return nil, fmt.Errorf("%s: %w", file, err)
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
func dataFrom(fsys vfs.FS, v any, dir string, sofar map[string]any) (map[string]any, error) {
	switch x := v.(type) {
	case map[string]any:
		return x, nil
	case string:
		return deck.LoadDataFile(resolve(dir, x), deck.HCLEnv{Data: maps.Clone(sofar), FS: fsys})
	case []any:
		m := map[string]any{}
		for _, p := range x {
			d, err := dataFrom(fsys, p, dir, sofar)
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
