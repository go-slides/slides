package deck

import (
	"bytes"
	"fmt"
	"maps"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"go.yaml.in/yaml/v3"

	"github.com/go-slides/slides/diag"
	"github.com/go-slides/slides/interp"
	"github.com/go-slides/slides/vfs"
)

// Options configures Parse.
type Options struct {
	// Data is template data; it overrides the front matter's keys and its
	// data mapping.
	Data map[string]any
	// Kinds add to (and override) the built-in kinds; the deck's own
	// kinds front-matter key overrides these in turn.
	Kinds Kinds
	// NoTemplate turns template interpolation off.
	NoTemplate bool
	// BaseDir resolves relative paths: included files and kinds files.
	BaseDir string
	// StrictYAML turns off loose YAML parsing of directives (Marp Core
	// turns it on; plain Marpit leaves it off).
	StrictYAML bool
	// Now is the template clock; nil means time.Now.
	Now func() time.Time
	// Name names the deck in error messages.
	Name string
	// NoEmoji leaves :shortcodes: and Unicode emoji as the text they were
	// written as (Marp Core's emoji: false); by default they become Twemoji
	// images.
	NoEmoji bool
	// FS is where included files, kinds files and the HCL file function
	// read; nil means the disk.
	FS vfs.FS
	// LineMap traces the lines templates generate back to the source line
	// that produced them (Slide.Line, Deck.SourceLine), for an editor's
	// scroll sync. It costs a pass over the template's text.
	LineMap bool
}

// Front-matter keys this tool reads besides directives.
const (
	keyKinds    = "kinds"
	keyData     = "data"
	keyTemplate = "template"
	keyDelims   = "delims"
)

var normalizer = strings.NewReplacer("\r\n", "\n", "\r", "\n", "\x00", "�")

// Parse parses a deck.
func Parse(src []byte, opts Options) (*Deck, error) {
	s := string(src)
	// The replacer copies the whole source even when it changes nothing,
	// which is the usual case (no CR, no NUL).
	if strings.ContainsAny(s, "\r\x00") {
		s = normalizer.Replace(s)
	}
	fmText, body, fmLines, hasFM := splitFrontMatter(s)

	// An HCL front matter becomes the YAML front matter it stands for;
	// its values are final, so templates leave them alone.
	var hclKinds Kinds
	fromHCL := hasFM && isHCLFrontMatter(s)
	if fromHCL {
		var err error
		if fmText, hclKinds, err = hclFront(fmText, opts); err != nil {
			return nil, err
		}
	}

	loose := !opts.StrictYAML
	var fmDirs map[string]any
	front := map[string]any{}
	if hasFM {
		fmDirs = parseDirectiveYAML(fmText, loose, looseKeys)
		// A front matter that is not valid YAML is ignored, as Marpit does.
		_ = yaml.Unmarshal([]byte(fmText), &front)
		if front == nil {
			front = map[string]any{}
		}
	}

	d := &Deck{Front: front, opts: opts, frontText: fmText, hasFront: hasFM}

	// Template data: the front matter, its data mapping, then the caller's.
	data := maps.Clone(front)
	if m, ok := front[keyData].(map[string]any); ok {
		maps.Copy(data, m)
	}
	maps.Copy(data, opts.Data)

	kinds := BuiltinKinds().Merge(opts.Kinds)
	if v, ok := fmDirs[keyKinds]; ok && v != nil {
		env := HCLEnv{Data: data, BaseDir: opts.BaseDir, Now: opts.Now, FS: opts.FS}
		k, err := loadKinds(v, opts.BaseDir, env)
		if err != nil {
			return nil, d.errorf(1, "%v", err)
		}
		kinds = kinds.Merge(k)
	}
	kinds = kinds.Merge(hclKinds)

	// Template interpolation.
	if !opts.NoTemplate && templateEnabled(fmDirs) {
		io := interp.Options{
			Data:    data,
			Delims:  delims(fmDirs),
			Code:    fmDirs[keyTemplate] == "code",
			BaseDir: opts.BaseDir,
			Name:    opts.Name,
			Now:     opts.Now,
			FS:      opts.FS,
		}
		valueOpts := io
		valueOpts.Code = true
		var err error
		if !fromHCL {
			if fmDirs, err = expandValues(fmDirs, valueOpts); err != nil {
				return nil, d.errorf(1, "front matter: %v", err)
			}
		}
		for name, k := range kinds {
			if k.literal {
				continue
			}
			// Kinds can be shared between decks: expand a copy.
			c := *k
			c.Dirs = k.Dirs.Clone()
			for _, key := range c.Dirs.Keys() {
				v, err := interp.Expand(c.Dirs.Get(key), valueOpts)
				if err != nil {
					return nil, fmt.Errorf("kind %q, %s: %w", name, key, err)
				}
				c.Dirs.Set(key, v)
			}
			kinds[name] = &c
		}
		if !fromHCL {
			if d.frontText, err = interp.Expand(fmText, valueOpts); err != nil {
				return nil, d.errorf(1, "front matter: %v", err)
			}
		}
		// Blank lines in place of the front matter make template errors
		// report the deck file's own line numbers.
		pad := strings.Repeat("\n", fmLines)
		if opts.LineMap {
			var lines []int
			if body, lines, err = interp.ExpandLines(pad+body, io); err != nil {
				return nil, err
			}
			if len(lines) >= fmLines {
				d.lineMap = lines[fmLines:]
			}
		} else if body, err = interp.Expand(pad+body, io); err != nil {
			return nil, err
		}
		body = body[len(pad):]
	}
	d.Kinds = kinds
	d.source = []byte(body)
	d.fmLines = fmLines

	pc := parser.NewContext()
	doc := markdown().Parser().Parse(text.NewReader(d.source), parser.WithContext(pc))
	d.doc = doc
	d.refs = referencesKey(pc.References())
	comments := collectComments(doc, d.source, loose)

	if err := d.applyGlobals(fmDirs, comments); err != nil {
		return nil, err
	}
	d.split(doc)
	if err := d.applyLocals(fmDirs, comments, fmLines); err != nil {
		return nil, err
	}
	d.paginate()
	d.scan()
	return d, nil
}

func (d *Deck) errorf(line int, format string, args ...any) error {
	name := d.opts.Name
	if name == "" {
		name = "deck"
	}
	err := fmt.Errorf("%s:%d: %s", name, line, fmt.Sprintf(format, args...))
	// the code of the error this one is about (unknown kind, a template...)
	// stays with the message, whatever verb the caller used for it
	for _, a := range args {
		if e, ok := a.(error); ok && diag.CodeOf(e) != "" {
			return diag.Wrap(diag.CodeOf(e), err)
		}
	}
	return err
}

// markdown returns a parser configured like Marp Core's markdown-it:
// CommonMark plus GFM tables, strikethrough and schema-only linkify.
func markdown() goldmark.Markdown {
	return goldmark.New(
		goldmark.WithExtensions(
			extension.Table,
			strikethrough{},
			mathExtension{},
			extension.NewLinkify(
				// linkify-it with fuzzyLink off links only URLs that
				// carry a scheme (and e-mail addresses).
				extension.WithLinkifyWWWRegexp(regexp.MustCompile(`\A\b\B`)),
			),
		),
	)
}

func templateEnabled(fm map[string]any) bool {
	v, _ := fm[keyTemplate].(string)
	return v != "false" && v != "off"
}

func delims(fm map[string]any) [2]string {
	if l, ok := fm[keyDelims].([]any); ok && len(l) == 2 {
		a, _ := l[0].(string)
		b, _ := l[1].(string)
		return [2]string{a, b}
	}
	return [2]string{}
}

// expandValues interpolates every string of a decoded front matter except
// the template data and the kinds, which have their own handling.
func expandValues(m map[string]any, opts interp.Options) (map[string]any, error) {
	var walk func(v any) (any, error)
	walk = func(v any) (any, error) {
		switch x := v.(type) {
		case string:
			return interp.Expand(x, opts)
		case []any:
			out := make([]any, len(x))
			for i, e := range x {
				var err error
				if out[i], err = walk(e); err != nil {
					return nil, err
				}
			}
			return out, nil
		}
		return v, nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		if k == keyData || k == keyKinds || k == keyDelims || k == orderKey {
			out[k] = v
			continue
		}
		e, err := walk(v)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", k, err)
		}
		out[k] = e
	}
	return out, nil
}

// loadKinds reads the kinds front-matter key: a mapping of kinds, or the
// path (or list of paths) of YAML or HCL files holding some.
func loadKinds(v any, baseDir string, env HCLEnv) (Kinds, error) {
	switch x := v.(type) {
	case map[string]any:
		return ParseKinds(x)
	case string:
		return LoadKindsFileEnv(resolvePath(baseDir, x), env)
	case []any:
		k := Kinds{}
		for _, p := range x {
			path, ok := p.(string)
			if !ok {
				return nil, fmt.Errorf("kinds: want a file path, got %T", p)
			}
			f, err := LoadKindsFileEnv(resolvePath(baseDir, path), env)
			if err != nil {
				return nil, err
			}
			k = k.Merge(f)
		}
		return k, nil
	}
	return nil, fmt.Errorf("kinds: want a mapping or file paths, got %T", v)
}

// LoadKindsFile reads kinds from a file. A YAML file holds either a mapping
// of kinds, or a mapping with a kinds key (so a configuration file can serve
// as one); a .hcl file holds kind blocks (see ParseKindsHCL), evaluated
// without template data — LoadKindsFileEnv provides some.
func LoadKindsFile(path string) (Kinds, error) {
	return LoadKindsFileEnv(path, HCLEnv{})
}

// ParseKindsYAML reads kinds from YAML source; name is for errors.
func ParseKindsYAML(src []byte, name string) (Kinds, error) {
	var n yaml.Node
	if err := yaml.Unmarshal(src, &n); err != nil {
		return nil, diag.Wrap("yaml.syntax", fmt.Errorf("%s: %v", name, err))
	}
	m, _ := failsafe(&n).(map[string]any)
	if m == nil {
		return Kinds{}, nil
	}
	if inner, ok := m[keyKinds].(map[string]any); ok {
		m = inner
	}
	k, err := ParseKinds(m)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return k, nil
}

// lineOf returns the 1-based line of a byte offset in source.
func lineOf(source []byte, offset int) int {
	return bytes.Count(source[:min(offset, len(source))], []byte("\n")) + 1
}

// resolvePath joins a relative path to dir and leaves an absolute one alone.
func resolvePath(dir, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(dir, path)
}
