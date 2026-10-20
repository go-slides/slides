package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/pflag"
	"go.yaml.in/yaml/v3"
)

// marp-cli reads its options from a configuration file (cosmiconfig, module
// name "marp"): the first of the files below found in the working directory
// (not in its parents). Only the files that are data
// can be read here — JSON and YAML; the JavaScript and TypeScript ones are a
// program, which slides does not run, and are said so.
//
// The names are cosmiconfig 9's search places for "marp": package.json (the
// "marp" property), .marprc (YAML, of which JSON is a case), .marprc.json,
// .marprc.yaml, .marprc.yml, .marprc.js .ts .mjs .cjs, the same under
// .config/marprc... and marp.config.js .ts .mjs .cjs. marp.config.json and
// .yaml are not among them: marp-cli 4.5.1 does not find them by search (the
// oracle says so), only --config-file loads them, and so it is here.
var marpConfigPlaces = []string{
	"package.json",
	".marprc",
	".marprc.json", ".marprc.yaml", ".marprc.yml", ".marprc.js", ".marprc.ts", ".marprc.mjs", ".marprc.cjs",
	".config/marprc",
	".config/marprc.json", ".config/marprc.yaml", ".config/marprc.yml", ".config/marprc.js", ".config/marprc.ts", ".config/marprc.mjs", ".config/marprc.cjs",
	"marp.config.js", "marp.config.ts", "marp.config.mjs", "marp.config.cjs",
}

// maxMarpConfig bounds a configuration file: it is a handful of options.
const maxMarpConfig = 1 << 20

// marpConfig is a configuration file read.
type marpConfig struct {
	path string
	data map[string]any
}

// findMarpConfig looks for a configuration file in dir, and only there:
// cosmiconfig 9's search, which marp-cli uses, does not walk up to the parent
// directories (the oracle says so). A file that is a program is skipped with a
// warning; a data file that cannot be read is an error.
func findMarpConfig(dir string, warn func(string)) (*marpConfig, error) {
	for _, place := range marpConfigPlaces {
		p := filepath.Join(dir, filepath.FromSlash(place))
		if st, err := os.Stat(p); err != nil || !st.Mode().IsRegular() {
			continue
		}
		if isProgram(place) {
			warn(fmt.Sprintf("%s is a program, which slides does not run: its options are ignored (use a JSON or YAML file, e.g. .marprc.yaml)", p))
			continue
		}
		c, err := loadMarpConfig(p)
		if err != nil {
			return nil, err
		}
		if c != nil {
			return c, nil
		}
	}
	return nil, nil
}

func isProgram(place string) bool {
	switch filepath.Ext(place) {
	case ".js", ".ts", ".mjs", ".cjs":
		return true
	}
	return false
}

// loadMarpConfig reads a configuration file: JSON or YAML, or package.json's
// "marp" property; nil if a package.json has none.
func loadMarpConfig(path string) (*marpConfig, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxMarpConfig+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxMarpConfig {
		return nil, fmt.Errorf("%s: more than %d bytes: not a configuration file", path, maxMarpConfig)
	}
	m, err := parseMarpConfig(b, filepath.Base(path))
	if err != nil {
		return nil, fmt.Errorf("could not parse configuration file %s: %w", path, err)
	}
	if m == nil {
		return nil, nil
	}
	return &marpConfig{path: path, data: m}, nil
}

// parseMarpConfig decodes a configuration file's content. name decides the
// reading of package.json; anything else is YAML, which holds JSON too.
func parseMarpConfig(b []byte, name string) (map[string]any, error) {
	if name == "package.json" {
		var pkg struct {
			Marp map[string]any `json:"marp"`
		}
		if err := json.Unmarshal(b, &pkg); err != nil {
			return nil, err
		}
		return pkg.Marp, nil
	}
	var v any
	if err := yaml.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	switch m := v.(type) {
	case nil:
		return map[string]any{}, nil
	case map[string]any:
		return m, nil
	}
	return nil, errors.New("a configuration file is a mapping of options")
}

// assignment is a flag to set from the configuration file.
type assignment struct{ flag, value string }

// unsupported are the options of marp-cli's configuration that have no
// meaning here: slides has no browser and no Marp engine to configure.
var unsupported = []string{"browser", "browserPath", "browserProtocol", "browserTimeout", "engine", "options", "baseUrl"}

// marpConfigFlags turns the options of a configuration file into the flags
// they stand for, and the warnings for what has no meaning here. Paths are
// relative to the file's directory, as marp-cli resolves them.
func marpConfigFlags(m map[string]any, dir string) ([]assignment, []string) {
	var out []assignment
	var warns []string
	set := func(flag, value string) { out = append(out, assignment{flag, value}) }
	path := func(p string) string {
		if filepath.IsAbs(p) {
			return p
		}
		return filepath.Join(dir, p)
	}
	str := func(v any) (string, bool) {
		switch x := v.(type) {
		case string:
			return x, true
		case int, int64, float64, uint64:
			return fmt.Sprint(x), true
		}
		return "", false
	}
	boolOf := func(v any) (string, bool) {
		b, ok := v.(bool)
		return strconv.FormatBool(b), ok
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		v := m[k]
		bad := func(want string) {
			warns = append(warns, fmt.Sprintf("configuration %q: want %s, got %T: ignored", k, want, v))
		}
		switch k {
		case "allowLocalFiles", "notes", "pdf", "pdfNotes", "pptx", "pptxEditable", "preview", "server", "watch":
			// slides' PowerPoint is editable: pptxEditable is what pptx means here
			flag := map[string]string{"allowLocalFiles": "allow-local-files", "notes": "notes", "pdf": "pdf", "pdfNotes": "pdf-notes", "pptx": "pptx", "pptxEditable": "pptx", "preview": "preview", "server": "server", "watch": "watch"}[k]
			if s, ok := boolOf(v); ok {
				if s == "true" {
					set(flag, s)
				}
			} else {
				bad("a boolean")
			}
		case "author", "description", "title", "url", "lang":
			if s, ok := str(v); ok {
				set(k, s)
			} else {
				bad("a string")
			}
		case "ogImage":
			if s, ok := str(v); ok {
				set("og-image", s)
			} else {
				bad("a string")
			}
		case "keywords":
			switch x := v.(type) {
			case string:
				set("keywords", x)
			case []any:
				var parts []string
				for _, e := range x {
					if s, ok := e.(string); ok {
						parts = append(parts, s)
					}
				}
				set("keywords", strings.Join(parts, ","))
			default:
				bad("a string or a list")
			}
		case "html":
			if s, ok := boolOf(v); ok {
				if s == "true" {
					set("html", s)
				}
			} else {
				warns = append(warns, `configuration "html": a list of allowed tags is not supported (use true for all HTML, or the defaults): ignored`)
			}
		case "theme":
			if s, ok := str(v); ok {
				// a theme name stays a name; a CSS file is resolved
				if filepath.Ext(s) == ".css" {
					s = path(s)
				}
				set("theme", s)
			} else {
				bad("a string")
			}
		case "themeSet":
			switch x := v.(type) {
			case string:
				set("theme-set", path(x))
			case []any:
				for _, e := range x {
					if s, ok := e.(string); ok {
						set("theme-set", path(s))
					}
				}
			default:
				bad("a path or a list of paths")
			}
		case "inputDir", "output":
			flag := map[string]string{"inputDir": "input-dir", "output": "output"}[k]
			switch x := v.(type) {
			case string:
				if x == "-" {
					set(flag, x)
				} else {
					set(flag, path(x))
				}
			case bool:
			default:
				bad("a path")
			}
		case "image", "images":
			if s, ok := str(v); ok {
				set(k, s)
			} else if _, isBool := v.(bool); !isBool {
				bad("png or jpeg")
			}
		case "imageScale", "jpegQuality":
			if s, ok := str(v); ok {
				set(map[string]string{"imageScale": "image-scale", "jpegQuality": "jpeg-quality"}[k], s)
			} else {
				bad("a number")
			}
		case "parallel":
			switch x := v.(type) {
			case bool:
				if !x {
					set("parallel", "1")
				}
			default:
				if s, ok := str(v); ok {
					set("parallel", s)
				} else {
					bad("a number")
				}
			}
		case "template":
			if s, ok := str(v); ok {
				set("template", s)
			} else {
				bad("bare or bespoke")
			}
		case "bespoke":
			b, ok := v.(map[string]any)
			if !ok {
				bad("a mapping")
				break
			}
			for _, name := range []string{"osc", "progress", "transition"} {
				if x, present := b[name]; present {
					if s, ok := boolOf(x); ok {
						set("bespoke."+name, s)
					} else {
						warns = append(warns, fmt.Sprintf("configuration bespoke.%s: want a boolean: ignored", name))
					}
				}
			}
		case "pdfOutlines":
			switch x := v.(type) {
			case bool:
				set("pdf-outlines", strconv.FormatBool(x))
			case map[string]any:
				set("pdf-outlines", "true")
				for _, name := range []string{"pages", "headings"} {
					if e, present := x[name]; present {
						if s, ok := boolOf(e); ok {
							set("pdf-outlines."+name, s)
						}
					}
				}
			default:
				bad("a boolean or a mapping")
			}
		default:
			if slices.Contains(unsupported, k) {
				warns = append(warns, fmt.Sprintf("configuration %q has no meaning here (slides has no browser and no Marp engine): ignored", k))
			} else {
				warns = append(warns, fmt.Sprintf("configuration %q is not an option slides knows: ignored", k))
			}
		}
	}
	return out, warns
}

// applyMarpConfig sets the flags a configuration file stands for, for the
// ones the command line did not give — the command line wins, as in marp-cli
// (args ?? conf).
func (o *options) applyMarpConfig(fl *pflag.FlagSet, dir string, warn func(string)) error {
	if o.noMarpConfig || o.noConfig {
		return nil
	}
	var c *marpConfig
	var err error
	if o.marpConfigFile != "" {
		c, err = loadMarpConfig(o.marpConfigFile)
		if err == nil && c == nil {
			err = fmt.Errorf("could not find or parse configuration file [%s]", o.marpConfigFile)
		}
		if errors.Is(err, fs.ErrNotExist) {
			err = fmt.Errorf("could not find or parse configuration file [%s]: %w", o.marpConfigFile, err)
		}
	} else {
		c, err = findMarpConfig(dir, warn)
	}
	if err != nil || c == nil {
		return err
	}
	as, warns := marpConfigFlags(c.data, filepath.Dir(c.path))
	for _, w := range warns {
		warn(c.path + ": " + w)
	}
	for _, a := range as {
		f := fl.Lookup(a.flag)
		if f == nil {
			continue
		}
		if f.Changed && !o.fromConfig[a.flag] {
			continue // the command line gave it: it wins
		}
		if err := fl.Set(a.flag, a.value); err != nil {
			warn(fmt.Sprintf("%s: %s: %v: ignored", c.path, a.flag, err))
			continue
		}
		if o.fromConfig == nil {
			o.fromConfig = map[string]bool{}
		}
		o.fromConfig[a.flag] = true // a repeatable flag takes the file's other values too
	}
	return nil
}
