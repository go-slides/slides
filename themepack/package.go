package themepack

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"golang.org/x/mod/semver"

	"github.com/go-slides/slides/deck"
	"github.com/go-slides/slides/diag"
)

// Package is a checked theme package.
type Package struct {
	Meta  *Meta
	Files Files
}

// docRe are the files a package may carry that theme.hcl does not declare:
// licence and readme texts at its root.
var docRe = regexp.MustCompile(`^(LICENSE|LICENCE|COPYING|NOTICE|README)[A-Za-z0-9._-]*$`)

// Open reads and checks a package file.
func Open(file string, lim Limits) (*Package, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, archiveErr("%s is not a regular file", file)
	}
	p, err := Read(f, st.Size(), lim)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	return p, nil
}

// Read reads and checks a package: the archive (see ReadFiles), theme.hcl,
// every declared file (present, within its bound, matching its sha256, of the
// right kind: a font that parses, an image whose header agrees, an SVG
// without script, CSS without a reference that leaves the package), and
// nothing undeclared.
func Read(r io.ReaderAt, size int64, lim Limits) (*Package, error) {
	files, err := ReadFiles(r, size, lim)
	if err != nil {
		return nil, err
	}
	return FromFiles(files, lim)
}

// FromFiles checks the files of a package (see Read).
func FromFiles(files Files, lim Limits) (*Package, error) {
	lim = lim.resolved()
	src, ok := files[MetaFile]
	if !ok {
		return nil, diag.Errorf("theme.meta", "theme package: no %s at the root", MetaFile)
	}
	meta, err := ParseMeta(src, lim)
	if err != nil {
		return nil, err
	}
	p := &Package{Meta: meta, Files: files}
	if err := p.check(lim); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *Package) check(lim Limits) error {
	m := p.Meta
	declared := map[string]bool{MetaFile: true}
	use := func(kind, file, sum string) ([]byte, error) {
		if declared[file] {
			return nil, diag.Errorf("theme.meta", "theme package: %s is declared twice", file)
		}
		declared[file] = true
		b, ok := p.Files[file]
		if !ok {
			return nil, diag.Errorf("theme.meta", "theme package: the %s %s is not in the archive", kind, file)
		}
		if sum != "" {
			got := sha256.Sum256(b)
			if hex.EncodeToString(got[:]) != sum {
				return nil, diag.Errorf("theme.checksum", "theme package: %s does not match its sha256 (declared %s, is %x)", file, sum, got)
			}
		}
		return b, nil
	}
	for _, c := range m.CSS {
		b, err := use("css", c.Path, c.SHA256)
		if err != nil {
			return err
		}
		if int64(len(b)) > lim.MaxCSS {
			return limitErr("%s is %d bytes, the limit is %d", c.Path, len(b), lim.MaxCSS)
		}
	}
	for _, f := range m.Fonts {
		b, err := use("font", f.File, f.SHA256)
		if err != nil {
			return err
		}
		if err := checkFont(f.File, b, lim); err != nil {
			return err
		}
	}
	for _, a := range m.Assets {
		b, err := use("asset", a.File, a.SHA256)
		if err != nil {
			return err
		}
		if strings.HasSuffix(strings.ToLower(a.File), ".svg") {
			if int64(len(b)) > lim.MaxCSS {
				return limitErr("%s is %d bytes, the limit is %d", a.File, len(b), lim.MaxCSS)
			}
			if err := checkSVG(a.File, b); err != nil {
				return err
			}
			continue
		}
		if err := checkImage(a.File, b, lim); err != nil {
			return err
		}
	}
	for name := range p.Files {
		if !declared[name] && !docRe.MatchString(name) {
			return diag.Errorf("theme.meta", "theme package: %s is in the archive and not declared in %s", name, MetaFile)
		}
	}
	// The CSS is read last: its references need the declared files.
	if _, err := p.CSS(); err != nil {
		return err
	}
	if _, err := p.Kinds(); err != nil {
		return diag.Wrap("theme.meta", err)
	}
	return nil
}

// CheckVersion refuses a package whose min_slides is newer than current, a
// go-slides version ("v0.18.0" or "0.18.0"). A current that is not a version
// (a development build) accepts everything.
func (p *Package) CheckVersion(current string) error {
	need := p.Meta.MinSlides
	if need == "" {
		return nil
	}
	if !strings.HasPrefix(current, "v") {
		current = "v" + current
	}
	if !semver.IsValid(current) {
		return nil
	}
	if semver.Compare(current, "v"+need) < 0 {
		return diag.Errorf("theme.version", "theme %q needs go-slides %s or newer, this is %s", p.Meta.Name, need, strings.TrimPrefix(current, "v"))
	}
	return nil
}

// CSS is the theme as one Marp theme sheet: the package's sheets in order,
// with each url() to a font or an asset of the package replaced by a data:
// URL (so that the page is self-contained and reads nothing at run time), an
// @font-face for each font block, and, last so that it wins over any comment
// in the sheets, the metadata comment that names the theme. The base theme is
// named by an @import-theme.
func (p *Package) CSS() (string, error) {
	m := p.Meta
	allowed := map[string]bool{}
	for _, f := range m.Fonts {
		allowed[f.File] = true
	}
	for _, a := range m.Assets {
		allowed[a.File] = true
	}
	var out strings.Builder
	if m.Extends != "" {
		fmt.Fprintf(&out, "@import-theme %q;\n", m.Extends)
	}
	for _, f := range m.Fonts {
		fmt.Fprintf(&out, "@font-face{font-family:%q;font-weight:%s;font-style:%s;font-display:swap;src:url(%q)}\n",
			f.Family, f.Weight, f.Style, dataURI(f.File, p.Files[f.File]))
	}
	for _, c := range m.CSS {
		ref := func(r string) (string, error) {
			if strings.HasPrefix(r, "#") {
				return r, nil
			}
			if strings.HasPrefix(strings.ToLower(r), "data:") {
				return r, checkData(c.Path, r)
			}
			target, err := resolveRef(c.Path, r)
			if err != nil {
				return "", err
			}
			if !allowed[target] {
				return "", cssErr(c.Path, "url(%s) names %s, which is not a declared font or asset", r, target)
			}
			return dataURI(target, p.Files[target]), nil
		}
		s, err := scanCSS(c.Path, string(p.Files[c.Path]), ref)
		if err != nil {
			return "", err
		}
		out.WriteString(s)
		out.WriteString("\n")
	}
	out.WriteString("/*\n")
	field := func(k, v string) {
		if v != "" {
			fmt.Fprintf(&out, "@%s %s\n", k, v)
		}
	}
	field("theme", m.Name)
	field("version", m.Version)
	field("description", m.Description)
	for _, a := range m.Authors {
		field("author", a)
	}
	field("license", m.License)
	field("homepage", m.Homepage)
	out.WriteString("*/\n")
	return out.String(), nil
}

// Kinds are the slide kinds the package brings.
func (p *Package) Kinds() (deck.Kinds, error) {
	if len(p.Meta.Kinds) == 0 {
		return deck.Kinds{}, nil
	}
	all := map[string]any{}
	for _, k := range p.Meta.Kinds {
		d := map[string]any{}
		if k.Extends != "" {
			d["extends"] = k.Extends
		}
		for _, kv := range k.Dirs {
			d[kv[0]] = kv[1]
		}
		all[k.Name] = d
	}
	return deck.ParseKinds(all)
}

// Pack reads the files a theme directory declares, and returns the package
// bytes. dir holds theme.hcl; only the files it declares (and licence texts
// at its root) are packed; a link or a file that is not regular is refused.
// The result is read back under the same checks Open applies.
func Pack(dir string, lim Limits) (*Meta, []byte, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, nil, err
	}
	defer root.Close()
	read := func(name string) ([]byte, error) {
		// os.Root refuses a name that leaves dir, link or not.
		st, err := root.Lstat(name)
		if err != nil {
			return nil, err
		}
		if !st.Mode().IsRegular() {
			return nil, archiveErr("%s is not a regular file (links are not packed)", name)
		}
		f, err := root.Open(name)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		lim := lim.resolved()
		b, err := io.ReadAll(io.LimitReader(f, lim.MaxFile+1))
		if err != nil {
			return nil, err
		}
		if int64(len(b)) > lim.MaxFile {
			return nil, limitErr("%s is larger than %d bytes", name, lim.MaxFile)
		}
		return b, nil
	}
	src, err := read(MetaFile)
	if err != nil {
		return nil, nil, diag.Errorf("theme.meta", "theme package: reading %s: %v", MetaFile, err)
	}
	meta, err := ParseMeta(src, lim)
	if err != nil {
		return nil, nil, err
	}
	files := Files{MetaFile: src}
	names := []string{}
	for _, c := range meta.CSS {
		names = append(names, c.Path)
	}
	for _, f := range meta.Fonts {
		names = append(names, f.File)
	}
	for _, a := range meta.Assets {
		names = append(names, a.File)
	}
	for _, n := range names {
		if files[n], err = read(n); err != nil {
			return nil, nil, diag.Wrap("theme.meta", fmt.Errorf("theme package: %s: %w", n, err))
		}
	}
	ents, err := fsReadDir(dir)
	if err != nil {
		return nil, nil, err
	}
	for _, e := range ents {
		if docRe.MatchString(e) {
			if files[e], err = read(e); err != nil {
				return nil, nil, err
			}
		}
	}
	b, err := WriteFiles(files)
	if err != nil {
		return nil, nil, err
	}
	// Check what was written, as a reader will.
	if _, err := Read(bytes.NewReader(b), int64(len(b)), lim); err != nil {
		return nil, nil, err
	}
	return meta, b, nil
}

func fsReadDir(dir string) ([]string, error) {
	es, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range es {
		if !e.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out, nil
}
