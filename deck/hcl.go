package deck

import (
	"fmt"
	"maps"
	"path/filepath"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"go.yaml.in/yaml/v3"

	"github.com/go-slides/slides/diag"
	"github.com/go-slides/slides/internal/hclx"
	"github.com/go-slides/slides/vfs"
)

// HCLEnv is what HCL expressions see: the data variable, the directory the
// file function resolves from, and the clock behind now and timestamp.
type HCLEnv = hclx.Env

// hclOpener is the first line of a deck whose front matter is HCL. Marp's
// front-matter rule (markdown-it-front-matter) opens on any first line
// starting with three dashes, so Marp still takes the block as front matter
// — and, the text not being a YAML mapping, ignores it.
const hclOpener = "---hcl"

// isHCLFrontMatter reports whether a normalized deck opens an HCL front
// matter.
func isHCLFrontMatter(src string) bool {
	first, _, _ := strings.Cut(src, "\n")
	return strings.TrimRight(first, " \t") == hclOpener
}

// IsHCL reports whether a file name has the .hcl extension, which selects
// HCL for slides.hcl, kinds files and data files.
func IsHCL(path string) bool { return strings.EqualFold(filepath.Ext(path), ".hcl") }

// hclFront evaluates an HCL front matter. It returns the equivalent YAML
// front matter, which the rest of the parse and the expand command use as
// if it had been written, and the kinds its kind blocks define.
//
// The data attribute is evaluated first, with the caller's data; every other
// attribute then sees data as that mapping overridden by the caller's, the
// way templates do. Attributes cannot reference each other.
func hclFront(text string, opts Options) (string, Kinds, error) {
	name := opts.Name
	if name == "" {
		name = "deck"
	}
	start := hcl.Pos{Line: 2, Column: 1, Byte: len(hclOpener) + 1}
	body, err := hclx.Parse([]byte(text), name, start)
	if err != nil {
		return "", nil, diag.Wrap("hcl.syntax", err)
	}
	env := HCLEnv{BaseDir: opts.BaseDir, Now: opts.Now, Data: maps.Clone(opts.Data), FS: opts.FS}
	if env.Data == nil {
		env.Data = map[string]any{}
	}
	var frontData map[string]any
	if a, ok := body.Attributes[keyData]; ok {
		v, err := hclx.Eval(a, env.Context())
		if err != nil {
			return "", nil, err
		}
		m, isMap := hclx.ToGo(v).(map[string]any)
		if !isMap {
			return "", nil, hclx.Errorf(a.Expr.Range(), "Invalid data", "data wants an object, got %s", v.Type().FriendlyName())
		}
		frontData = m
	}
	data := maps.Clone(frontData)
	if data == nil {
		data = map[string]any{}
	}
	maps.Copy(data, opts.Data)
	env.Data = data
	ctx := env.Context()

	var b strings.Builder
	for _, a := range hclx.Attributes(body) {
		if a.Name == keyData {
			b.WriteString(keyData + ": " + hclx.YAML(hclx.FromGo(frontData)) + "\n")
			continue
		}
		v, err := hclx.Eval(a, ctx)
		if err != nil {
			return "", nil, err
		}
		if v.IsNull() {
			continue // null leaves the directive unset
		}
		b.WriteString(a.Name + ": " + hclx.YAML(v) + "\n")
	}
	kinds, err := hclKinds(body, env)
	if err != nil {
		return "", nil, err
	}
	return b.String(), kinds, nil
}

// hclKinds reads a body's kind blocks; its attributes are the caller's.
func hclKinds(body *hclsyntax.Body, env HCLEnv) (Kinds, error) {
	ctx := env.Context()
	k := Kinds{}
	for _, blk := range body.Blocks {
		if blk.Type != "kind" {
			return nil, diag.Wrap("hcl.unsupported", hclx.Errorf(blk.TypeRange, "Unsupported block type", "only kind blocks are expected here, not %q", blk.Type))
		}
		if len(blk.Labels) != 1 {
			return nil, hclx.Errorf(blk.DefRange(), "Invalid kind block", `a kind block takes one label, its name: kind "title" { ... }`)
		}
		name := blk.Labels[0]
		if len(blk.Body.Blocks) > 0 {
			inner := blk.Body.Blocks[0]
			return nil, hclx.Errorf(inner.TypeRange, "Unexpected block", "kind %q holds directives only, not %q blocks", name, inner.Type)
		}
		kind := &Kind{Name: name, literal: true}
		for _, a := range hclx.Attributes(blk.Body) {
			v, err := hclx.Eval(a, ctx)
			if err != nil {
				return nil, err
			}
			if a.Name == "extends" {
				s, ok := hclx.ToGo(v).(string)
				if !ok {
					return nil, hclx.Errorf(a.Expr.Range(), "Invalid extends", "kind %q: extends wants a kind name", name)
				}
				kind.Extends = s
				continue
			}
			if !isLocal(a.Name) {
				return nil, diag.Wrap("directive.unknown", hclx.Errorf(a.NameRange, "Unsupported directive", "kind %q: %q is not a local directive (want one of %s)", name, a.Name, strings.Join(kindKeys, ", ")))
			}
			if v.IsNull() {
				continue
			}
			if s, ok := normalizeLocal(a.Name, hclx.Failsafe(v)); ok {
				kind.Dirs.Set(a.Name, s)
			}
		}
		k[name] = kind
	}
	return k, nil
}

// ParseKindsHCL reads kinds from HCL source: kind blocks, each labelled with
// the kind's name and holding its directives and extends. Top-level
// attributes are ignored, so that slides.hcl can serve as a kinds file.
// name is for errors.
func ParseKindsHCL(src []byte, name string, env HCLEnv) (Kinds, error) {
	body, err := hclx.Parse(src, name, hcl.InitialPos)
	if err != nil {
		return nil, diag.Wrap("hcl.syntax", err)
	}
	return hclKinds(body, env)
}

// LoadKindsFileEnv reads kinds from a file: HCL when its extension is .hcl
// (evaluated in env, the file function resolving from the file's
// directory), YAML otherwise.
func LoadKindsFileEnv(path string, env HCLEnv) (Kinds, error) {
	b, err := vfs.Or(env.FS).ReadFile(path)
	if err != nil {
		return nil, err
	}
	if IsHCL(path) {
		env.BaseDir = filepath.Dir(path)
		return ParseKindsHCL(b, path, env)
	}
	return ParseKindsYAML(b, path)
}

// LoadDataFile reads template data from a file: HCL when its extension is
// .hcl (typed values; see the README), YAML or JSON otherwise.
func LoadDataFile(path string, env HCLEnv) (map[string]any, error) {
	b, err := vfs.Or(env.FS).ReadFile(path)
	if err != nil {
		return nil, err
	}
	if IsHCL(path) {
		env.BaseDir = filepath.Dir(path)
		return hclx.Data(b, path, env)
	}
	m := map[string]any{}
	if err := yaml.Unmarshal(b, &m); err != nil {
		return nil, diag.Wrap("yaml.syntax", fmt.Errorf("%s: %v", path, err))
	}
	return m, nil
}
