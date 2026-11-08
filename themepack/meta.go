package themepack

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
	"golang.org/x/mod/semver"

	"github.com/go-slides/slides/diag"
	"github.com/go-slides/slides/internal/hclx"
)

// Meta is what theme.hcl declares.
//
//	theme {
//	  name        = "acme"
//	  version     = "1.2.0"
//	  description = "ACME corporate slides"
//	  authors     = ["Ada Lovelace <ada@example.org>"]
//	  license     = "MIT"
//	  homepage    = "https://example.org/acme-theme"
//	  extends     = "default"
//	  min_slides  = "0.18.0"
//	}
//	css "theme.css" { sha256 = "..." }
//	font "Inter" {
//	  weight = 400
//	  style  = "normal"
//	  file   = "fonts/Inter-Regular.woff2"
//	  license = "OFL-1.1"
//	}
//	asset "logo" { file = "assets/logo.svg" }
//	kind "title" { class = "lead" }
type Meta struct {
	Name        string
	Version     string
	Description string
	Authors     []string
	License     string
	Homepage    string
	// Extends names the base theme, built in or installed.
	Extends string
	// MinSlides is the oldest go-slides version the package works with.
	MinSlides string
	CSS       []CSSFile
	Fonts     []Font
	Assets    []Asset
	Kinds     []Kind
}

// CSSFile is a style sheet of the package; sheets are joined in order.
type CSSFile struct {
	Path   string
	SHA256 string
}

// Font is one face of a family.
type Font struct {
	Family string
	// Weight is a number ("400") or a range of two ("100 900").
	Weight  string
	Style   string
	File    string
	License string
	SHA256  string
}

// Asset is a logo, a background or any image the CSS refers to.
type Asset struct {
	ID     string
	File   string
	SHA256 string
}

// Kind is a slide kind the package brings: a name, its base and its
// directives, every value a string.
type Kind struct {
	Name    string
	Extends string
	Dirs    [][2]string
}

// Bounds of the metadata file's own lists.
const (
	maxAuthors = 32
	maxBlocks  = 256
	maxString  = 1024
)

// ReservedNames are the built-in themes: a package cannot take their name.
var ReservedNames = []string{"default", "gaia", "uncover"}

var (
	nameRe   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	sha256Re = regexp.MustCompile(`^[0-9a-f]{64}$`)
	// spdxRe is the syntax of an SPDX license expression: ids joined with
	// AND, OR and WITH, parentheses allowed. The list of ids is not checked.
	spdxRe  = regexp.MustCompile(`^\(?[A-Za-z0-9.+:-]+\)?( (AND|OR|WITH) \(?[A-Za-z0-9.+:-]+\)?)*$`)
	weightR = regexp.MustCompile(`^[1-9][0-9]{0,3}( [1-9][0-9]{0,3})?$`)
)

var semverRe = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`)

// validSemver is a full semantic version, MAJOR.MINOR.PATCH, no "v": golang.org/x/mod/semver
// also takes "v1" and "v1.2", which a package's version must not be.
func validSemver(s string) bool { return semverRe.MatchString(s) && semver.IsValid("v"+s) }

// ValidThemeName reports whether s can name a theme package.
func ValidThemeName(s string) bool { return nameRe.MatchString(s) }

func metaErr(r *hcl.Range, format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	if r != nil {
		msg = fmt.Sprintf("%s:%d:%d: %s", r.Filename, r.Start.Line, r.Start.Column, msg)
	}
	return diag.Errorf("theme.meta", "theme package: %s", msg)
}

// nesting is the deepest bracket nesting of src, counted over every byte,
// strings and comments included: a conservative bound that needs no parser,
// so that it can run before one. An unbalanced bracket in a comment only
// raises the count.
func nesting(src []byte) int {
	depth, deepest := 0, 0
	for _, c := range src {
		switch c {
		case '{', '[', '(':
			depth++
			deepest = max(deepest, depth)
		case '}', ']', ')':
			depth = max(depth-1, 0)
		}
	}
	return deepest
}

// ParseMeta reads theme.hcl. Its expressions are literals: no variable, no
// function, so that no file or environment variable can be read through it.
func ParseMeta(src []byte, lim Limits) (*Meta, error) {
	lim = lim.resolved()
	if int64(len(src)) > lim.MaxMeta {
		return nil, limitErr("%s is %d bytes, the limit is %d", MetaFile, len(src), lim.MaxMeta)
	}
	if !utf8.Valid(src) {
		return nil, metaErr(nil, "%s is not UTF-8", MetaFile)
	}
	if d := nesting(src); d > lim.MaxDepth {
		return nil, limitErr("%s nests brackets %d deep, the limit is %d", MetaFile, d, lim.MaxDepth)
	}
	body, err := hclx.Parse(src, MetaFile, hcl.InitialPos)
	if err != nil {
		return nil, diag.Wrap("theme.meta", err)
	}
	m, err := decodeMeta(body)
	if err != nil {
		return nil, err
	}
	return m, nil
}

func decodeMeta(body *hclsyntax.Body) (*Meta, error) {
	for _, a := range hclx.Attributes(body) {
		return nil, metaErr(&a.NameRange, "attribute %q outside the theme block", a.Name)
	}
	if len(body.Blocks) > maxBlocks {
		return nil, limitErr("%s has %d blocks, the limit is %d", MetaFile, len(body.Blocks), maxBlocks)
	}
	m := &Meta{}
	seenTheme := false
	seen := map[string]bool{}
	for _, b := range body.Blocks {
		wantLabels := 1
		if b.Type == "theme" {
			wantLabels = 0
		}
		if len(b.Labels) != wantLabels {
			return nil, metaErr(ptr(b.DefRange()), "%s block takes %d label(s)", b.Type, wantLabels)
		}
		key := b.Type
		if wantLabels == 1 {
			key += " " + b.Labels[0]
		}
		if seen[key] {
			return nil, metaErr(ptr(b.DefRange()), "duplicate %s", key)
		}
		seen[key] = true
		var err error
		switch b.Type {
		case "theme":
			seenTheme = true
			err = decodeTheme(m, b)
		case "css":
			err = decodeCSS(m, b)
		case "font":
			err = decodeFont(m, b)
		case "asset":
			err = decodeAsset(m, b)
		case "kind":
			err = decodeKind(m, b)
		default:
			err = metaErr(ptr(b.TypeRange), "unknown block %q (want theme, css, font, asset or kind)", b.Type)
		}
		if err != nil {
			return nil, err
		}
	}
	if !seenTheme {
		return nil, metaErr(nil, "%s has no theme block", MetaFile)
	}
	if len(m.CSS) == 0 {
		return nil, metaErr(nil, "%s declares no css file", MetaFile)
	}
	return m, nil
}

func ptr(r hcl.Range) *hcl.Range { return &r }

// attrs evaluates a block's attributes as literals, each to its Go value.
// Only the names in allowed are accepted (any when allowed is nil).
func attrs(b *hclsyntax.Block, allowed []string) (map[string]cty.Value, map[string]*hclsyntax.Attribute, error) {
	if len(b.Body.Blocks) > 0 {
		inner := b.Body.Blocks[0]
		return nil, nil, metaErr(ptr(inner.TypeRange), "%s holds attributes only, not a %q block", b.Type, inner.Type)
	}
	vals := map[string]cty.Value{}
	srcs := map[string]*hclsyntax.Attribute{}
	for _, a := range hclx.Attributes(b.Body) {
		if allowed != nil && !contains(allowed, a.Name) {
			return nil, nil, metaErr(ptr(a.NameRange), "%s: unknown attribute %q (want %s)", b.Type, a.Name, strings.Join(allowed, ", "))
		}
		v, err := a.Expr.Value(nil)
		if err.HasErrors() {
			return nil, nil, diag.Wrap("theme.meta", fmt.Errorf("%w (theme.hcl takes literal values only)", hclx.Error(err)))
		}
		vals[a.Name] = v
		srcs[a.Name] = a
	}
	return vals, srcs, nil
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// text reads a string attribute: bounded, one line, no control character.
func text(vals map[string]cty.Value, srcs map[string]*hclsyntax.Attribute, name string) (string, bool, error) {
	v, ok := vals[name]
	if !ok {
		return "", false, nil
	}
	if v.IsNull() || v.Type() != cty.String {
		return "", true, metaErr(ptr(srcs[name].Expr.Range()), "%s wants a string", name)
	}
	s := v.AsString()
	if len(s) > maxString {
		return "", true, metaErr(ptr(srcs[name].Expr.Range()), "%s is %d bytes, the limit is %d", name, len(s), maxString)
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return "", true, metaErr(ptr(srcs[name].Expr.Range()), "%s has a control character", name)
		}
	}
	// values are written into a CSS comment: they must not close it
	if strings.Contains(s, "*/") {
		return "", true, metaErr(ptr(srcs[name].Expr.Range()), "%s contains */", name)
	}
	return s, true, nil
}

func decodeTheme(m *Meta, b *hclsyntax.Block) error {
	vals, srcs, err := attrs(b, []string{"name", "version", "description", "authors", "license", "homepage", "extends", "min_slides"})
	if err != nil {
		return err
	}
	field := func(dst *string, name string, required bool) error {
		s, ok, err := text(vals, srcs, name)
		if err != nil {
			return err
		}
		if !ok && required {
			return metaErr(ptr(b.DefRange()), "theme: %s is required", name)
		}
		*dst = s
		return nil
	}
	for _, f := range []struct {
		dst      *string
		name     string
		required bool
	}{
		{&m.Name, "name", true}, {&m.Version, "version", true}, {&m.Description, "description", false},
		{&m.License, "license", true}, {&m.Homepage, "homepage", false}, {&m.Extends, "extends", false},
		{&m.MinSlides, "min_slides", false},
	} {
		if err := field(f.dst, f.name, f.required); err != nil {
			return err
		}
	}
	if !nameRe.MatchString(m.Name) {
		return metaErr(ptr(srcs["name"].Expr.Range()), "name %q is not a theme name (lower-case letters, digits and dashes, at most 64)", m.Name)
	}
	if contains(ReservedNames, m.Name) {
		return metaErr(ptr(srcs["name"].Expr.Range()), "name %q is a built-in theme", m.Name)
	}
	if !validSemver(m.Version) {
		return metaErr(ptr(srcs["version"].Expr.Range()), "version %q is not a semantic version (MAJOR.MINOR.PATCH)", m.Version)
	}
	if !spdxRe.MatchString(m.License) {
		return metaErr(ptr(srcs["license"].Expr.Range()), "license %q is not an SPDX expression", m.License)
	}
	if m.Homepage != "" {
		u, err := url.Parse(m.Homepage)
		if err != nil || u.Host == "" || u.Scheme != "https" && u.Scheme != "http" {
			return metaErr(ptr(srcs["homepage"].Expr.Range()), "homepage %q is not an http(s) URL", m.Homepage)
		}
	}
	if m.Extends != "" && !nameRe.MatchString(m.Extends) {
		return metaErr(ptr(srcs["extends"].Expr.Range()), "extends %q is not a theme name", m.Extends)
	}
	if m.Extends == m.Name {
		return metaErr(ptr(srcs["extends"].Expr.Range()), "theme %q extends itself", m.Name)
	}
	if m.MinSlides != "" && !validSemver(m.MinSlides) {
		return metaErr(ptr(srcs["min_slides"].Expr.Range()), "min_slides %q is not a semantic version", m.MinSlides)
	}
	if v, ok := vals["authors"]; ok {
		if v.IsNull() || !(v.Type().IsTupleType() || v.Type().IsListType()) {
			return metaErr(ptr(srcs["authors"].Expr.Range()), "authors wants a list of strings")
		}
		if v.LengthInt() > maxAuthors {
			return metaErr(ptr(srcs["authors"].Expr.Range()), "authors lists %d names, the limit is %d", v.LengthInt(), maxAuthors)
		}
		for it := v.ElementIterator(); it.Next(); {
			_, e := it.Element()
			if e.IsNull() || e.Type() != cty.String || len(e.AsString()) > 200 || strings.ContainsAny(e.AsString(), "\r\n") {
				return metaErr(ptr(srcs["authors"].Expr.Range()), "authors wants short one-line strings")
			}
			m.Authors = append(m.Authors, e.AsString())
		}
	}
	return nil
}

// optSHA reads the optional sha256 attribute.
func optSHA(vals map[string]cty.Value, srcs map[string]*hclsyntax.Attribute) (string, error) {
	s, ok, err := text(vals, srcs, "sha256")
	if err != nil || !ok {
		return "", err
	}
	if !sha256Re.MatchString(s) {
		return "", metaErr(ptr(srcs["sha256"].Expr.Range()), "sha256 wants 64 lower-case hexadecimal digits")
	}
	return s, nil
}

func decodeCSS(m *Meta, b *hclsyntax.Block) error {
	vals, srcs, err := attrs(b, []string{"sha256"})
	if err != nil {
		return err
	}
	sum, err := optSHA(vals, srcs)
	if err != nil {
		return err
	}
	p := b.Labels[0]
	if err := ValidName(p, 0); err != nil || strings.HasSuffix(p, "/") || !strings.HasSuffix(p, ".css") {
		return metaErr(ptr(b.LabelRanges[0]), "css %q is not a .css file path inside the package", p)
	}
	m.CSS = append(m.CSS, CSSFile{Path: p, SHA256: sum})
	return nil
}

func decodeFont(m *Meta, b *hclsyntax.Block) error {
	vals, srcs, err := attrs(b, []string{"weight", "style", "file", "license", "sha256"})
	if err != nil {
		return err
	}
	f := Font{Family: b.Labels[0], Weight: "400", Style: "normal"}
	// The family is written into a CSS string: keep it plain.
	if f.Family == "" || len(f.Family) > 64 || strings.ContainsAny(f.Family, "\"'\\{};<>/*()@\n\r") {
		return metaErr(ptr(b.LabelRanges[0]), "font family %q is empty, too long or has a character CSS would take for syntax", f.Family)
	}
	if v, ok := vals["weight"]; ok {
		switch {
		case v.IsNull():
			return metaErr(ptr(srcs["weight"].Expr.Range()), "weight wants a number or a range")
		case v.Type() == cty.Number:
			n, _ := v.AsBigFloat().Int64()
			f.Weight = fmt.Sprint(n)
		case v.Type() == cty.String:
			f.Weight = v.AsString()
		}
		if !weightR.MatchString(f.Weight) {
			return metaErr(ptr(srcs["weight"].Expr.Range()), "weight %q is not a number like 400 or a range like \"100 900\"", f.Weight)
		}
	}
	if s, ok, err := text(vals, srcs, "style"); err != nil {
		return err
	} else if ok {
		f.Style = s
	}
	if f.Style != "normal" && f.Style != "italic" && f.Style != "oblique" {
		return metaErr(ptr(b.DefRange()), "font %q: style %q is not normal, italic or oblique", f.Family, f.Style)
	}
	for _, r := range []struct {
		dst  *string
		name string
	}{{&f.File, "file"}, {&f.License, "license"}} {
		s, ok, err := text(vals, srcs, r.name)
		if err != nil {
			return err
		}
		if !ok {
			return metaErr(ptr(b.DefRange()), "font %q: %s is required", f.Family, r.name)
		}
		*r.dst = s
	}
	if err := ValidName(f.File, 0); err != nil || strings.HasSuffix(f.File, "/") {
		return metaErr(ptr(srcs["file"].Expr.Range()), "font file %q is not a path inside the package", f.File)
	}
	if !spdxRe.MatchString(f.License) {
		return metaErr(ptr(srcs["license"].Expr.Range()), "license %q is not an SPDX expression", f.License)
	}
	if f.SHA256, err = optSHA(vals, srcs); err != nil {
		return err
	}
	m.Fonts = append(m.Fonts, f)
	return nil
}

func decodeAsset(m *Meta, b *hclsyntax.Block) error {
	vals, srcs, err := attrs(b, []string{"file", "sha256"})
	if err != nil {
		return err
	}
	a := Asset{ID: b.Labels[0]}
	if !nameRe.MatchString(a.ID) {
		return metaErr(ptr(b.LabelRanges[0]), "asset id %q is not a name (lower-case letters, digits and dashes)", a.ID)
	}
	s, ok, err := text(vals, srcs, "file")
	if err != nil {
		return err
	}
	if !ok {
		return metaErr(ptr(b.DefRange()), "asset %q: file is required", a.ID)
	}
	if err := ValidName(s, 0); err != nil || strings.HasSuffix(s, "/") {
		return metaErr(ptr(srcs["file"].Expr.Range()), "asset file %q is not a path inside the package", s)
	}
	a.File = s
	if a.SHA256, err = optSHA(vals, srcs); err != nil {
		return err
	}
	m.Assets = append(m.Assets, a)
	return nil
}

func decodeKind(m *Meta, b *hclsyntax.Block) error {
	vals, srcs, err := attrs(b, nil)
	if err != nil {
		return err
	}
	if len(vals) > 32 {
		return limitErr("kind %q has %d directives, the limit is 32", b.Labels[0], len(vals))
	}
	k := Kind{Name: b.Labels[0]}
	for _, a := range hclx.Attributes(b.Body) {
		v := vals[a.Name]
		g := hclx.Failsafe(v)
		s, ok := g.(string)
		if g == nil {
			continue
		}
		if !ok {
			return metaErr(ptr(srcs[a.Name].Expr.Range()), "kind %q: %s wants a string, a number or a bool", k.Name, a.Name)
		}
		// A Go template in a package's value would run in the deck's build.
		if strings.Contains(s, "{{") || len(s) > maxString {
			return metaErr(ptr(srcs[a.Name].Expr.Range()), "kind %q: %s holds a template or is too long", k.Name, a.Name)
		}
		if a.Name == "extends" {
			k.Extends = s
			continue
		}
		k.Dirs = append(k.Dirs, [2]string{a.Name, s})
	}
	m.Kinds = append(m.Kinds, k)
	return nil
}
