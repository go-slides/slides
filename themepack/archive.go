// Package themepack reads, checks and writes theme packages: one ZIP archive,
// extension .slidestheme, holding theme.hcl at its root and the CSS, fonts,
// images and kinds it declares.
//
// A package is untrusted input (a playground share link can carry one), so
// nothing in it is believed before it is checked:
//
//   - the ZIP reader refuses what APPNOTE 4.4.17 forbids and what zip-slip
//     needs: a backslash, a drive letter, a leading slash, a ".." element; it
//     refuses symbolic links, devices, duplicate names (compared without case,
//     so that one name cannot shadow another on a case-folding disk), methods
//     other than stored and deflate, and encryption;
//   - sizes are bounded by what is read, not by what the central directory
//     declares (APPNOTE 4.3.12: the directory is a claim; the bytes are the
//     fact), and so are the entry count and the compression ratio;
//   - theme.hcl is parsed with HCL's native syntax (hclsyntax/spec.md), with
//     literals only: no function, no variable, no file, no environment;
//   - CSS, SVG, fonts and images are checked (see check.go).
//
// Every refusal is a diag error with a code: theme.archive, theme.limit,
// theme.meta, theme.css, theme.svg, theme.font, theme.checksum, theme.version.
package themepack

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-slides/slides/diag"
)

// Extension is the file extension of a theme package.
const Extension = ".slidestheme"

// MetaFile is the metadata file at the root of a package.
const MetaFile = "theme.hcl"

// Limits bound what a package may make the reader spend. A field that is zero
// takes its default.
type Limits struct {
	// MaxArchive is the size of the .slidestheme file.
	MaxArchive int64
	// MaxFiles is the number of entries, directories included.
	MaxFiles int
	// MaxFile is the uncompressed size of one entry.
	MaxFile int64
	// MaxTotal is the uncompressed size of all entries together.
	MaxTotal int64
	// MaxRatio is the largest uncompressed to compressed ratio of an entry
	// of MinRatioSize bytes or more.
	MaxRatio int64
	// MaxMeta is the size of theme.hcl.
	MaxMeta int64
	// MaxDepth is the deepest bracket nesting of theme.hcl.
	MaxDepth int
	// MaxPath is the length of an entry name.
	MaxPath int
	// MaxCSS is the size of one CSS file; MaxFont of one font; MaxImage of one
	// image.
	MaxCSS, MaxFont, MaxImage int64
}

// MinRatioSize is the size under which an entry's ratio is not checked: a
// short run of the same byte compresses far better than 100 to 1 legitimately,
// and it is bounded by MaxFile and MaxTotal anyway.
const MinRatioSize = 64 << 10

// DefaultLimits are the bounds Open and Read apply: above any theme worth
// shipping (fonts included), far below what a hostile archive of a few
// kilobytes can ask for.
var DefaultLimits = Limits{
	MaxArchive: 16 << 20,
	MaxFiles:   256,
	MaxFile:    8 << 20,
	MaxTotal:   24 << 20,
	MaxRatio:   100,
	MaxMeta:    64 << 10,
	MaxDepth:   32,
	MaxPath:    255,
	MaxCSS:     1 << 20,
	MaxFont:    4 << 20,
	MaxImage:   4 << 20,
}

func (l Limits) resolved() Limits {
	d := DefaultLimits
	set64 := func(dst *int64, v int64) {
		if v > 0 {
			*dst = v
		}
	}
	set64(&d.MaxArchive, l.MaxArchive)
	set64(&d.MaxFile, l.MaxFile)
	set64(&d.MaxTotal, l.MaxTotal)
	set64(&d.MaxRatio, l.MaxRatio)
	set64(&d.MaxMeta, l.MaxMeta)
	set64(&d.MaxCSS, l.MaxCSS)
	set64(&d.MaxFont, l.MaxFont)
	set64(&d.MaxImage, l.MaxImage)
	if l.MaxFiles > 0 {
		d.MaxFiles = l.MaxFiles
	}
	if l.MaxDepth > 0 {
		d.MaxDepth = l.MaxDepth
	}
	if l.MaxPath > 0 {
		d.MaxPath = l.MaxPath
	}
	return d
}

func archiveErr(format string, args ...any) error {
	return diag.Errorf("theme.archive", "theme package: "+format, args...)
}

func limitErr(format string, args ...any) error {
	return diag.Errorf("theme.limit", "theme package: "+format, args...)
}

// ValidName reports whether an entry name is a safe relative path of a
// package: forward slashes only, no leading slash, no drive letter, no empty,
// "." or ".." element, no control character, valid UTF-8. A trailing slash
// (a directory entry) is allowed.
func ValidName(name string, maxPath int) error {
	if maxPath <= 0 {
		maxPath = DefaultLimits.MaxPath
	}
	if name == "" {
		return errors.New("empty name")
	}
	if len(name) > maxPath {
		return fmt.Errorf("name of %d bytes, the limit is %d", len(name), maxPath)
	}
	if !utf8.ValidString(name) {
		return errors.New("name is not UTF-8")
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f || r == '\\' {
			return fmt.Errorf("name %q has a backslash or a control character", name)
		}
	}
	if name[0] == '/' {
		return fmt.Errorf("name %q is absolute", name)
	}
	if len(name) >= 2 && name[1] == ':' {
		return fmt.Errorf("name %q has a drive letter", name)
	}
	for _, el := range strings.Split(strings.TrimSuffix(name, "/"), "/") {
		switch el {
		case "", ".", "..":
			return fmt.Errorf("name %q has an empty, . or .. element", name)
		}
	}
	return nil
}

// Files is the content of a package by entry name.
type Files map[string][]byte

// Names lists the entry names, sorted.
func (f Files) Names() []string {
	names := make([]string, 0, len(f))
	for n := range f {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// ReadFiles reads a ZIP archive of size bytes under the limits and returns its
// regular files. It checks the archive, not what is in it (see Read).
func ReadFiles(r io.ReaderAt, size int64, lim Limits) (Files, error) {
	lim = lim.resolved()
	if size > lim.MaxArchive {
		return nil, limitErr("archive of %d bytes, the limit is %d", size, lim.MaxArchive)
	}
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, archiveErr("not a ZIP archive: %v", err)
	}
	if len(zr.File) > lim.MaxFiles {
		return nil, limitErr("%d entries, the limit is %d", len(zr.File), lim.MaxFiles)
	}
	// First pass: everything the directory says, before one byte is inflated.
	seen := map[string]bool{}
	var declared uint64
	for _, f := range zr.File {
		if err := ValidName(f.Name, lim.MaxPath); err != nil {
			return nil, archiveErr("%v", err)
		}
		key := strings.ToLower(strings.TrimSuffix(f.Name, "/"))
		if seen[key] {
			return nil, archiveErr("duplicate name %q", f.Name)
		}
		seen[key] = true
		mode := f.Mode()
		if !mode.IsRegular() && !mode.IsDir() {
			return nil, archiveErr("%q is a link or a special file", f.Name)
		}
		if strings.HasSuffix(f.Name, "/") != mode.IsDir() {
			return nil, archiveErr("%q: a directory entry must end in a slash, and only one does", f.Name)
		}
		if f.Flags&0x1 != 0 {
			return nil, archiveErr("%q is encrypted", f.Name)
		}
		if f.Method != zip.Store && f.Method != zip.Deflate {
			return nil, archiveErr("%q uses compression method %d; only stored (0) and deflate (8) are read", f.Name, f.Method)
		}
		if mode.IsDir() {
			continue
		}
		if f.UncompressedSize64 > uint64(lim.MaxFile) {
			return nil, limitErr("%q declares %d bytes, the limit is %d", f.Name, f.UncompressedSize64, lim.MaxFile)
		}
		declared += f.UncompressedSize64
		if declared > uint64(lim.MaxTotal) {
			return nil, limitErr("the entries declare more than %d bytes together", lim.MaxTotal)
		}
		if f.UncompressedSize64 >= MinRatioSize {
			c := max(f.CompressedSize64, 1)
			if f.UncompressedSize64/c > uint64(lim.MaxRatio) {
				return nil, limitErr("%q compresses %d:%d, the limit is %d:1", f.Name, f.UncompressedSize64, c, lim.MaxRatio)
			}
		}
	}
	// Second pass: what is really inflated is what counts.
	files := Files{}
	var total int64
	for _, f := range zr.File {
		if f.Mode().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, archiveErr("%q: %v", f.Name, err)
		}
		b, err := io.ReadAll(io.LimitReader(rc, lim.MaxFile+1))
		rc.Close()
		if err != nil {
			return nil, archiveErr("%q: %v", f.Name, err)
		}
		if int64(len(b)) > lim.MaxFile {
			return nil, limitErr("%q inflates to more than %d bytes", f.Name, lim.MaxFile)
		}
		if uint64(len(b)) != f.UncompressedSize64 {
			return nil, archiveErr("%q declares %d bytes and holds %d", f.Name, f.UncompressedSize64, len(b))
		}
		total += int64(len(b))
		if total > lim.MaxTotal {
			return nil, limitErr("the entries inflate to more than %d bytes together", lim.MaxTotal)
		}
		files[f.Name] = b
	}
	return files, nil
}

// zipEpoch is the modification time of every entry Write makes, so that
// packing the same directory twice gives the same bytes.
var zipEpoch = time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)

// WriteFiles writes files as a ZIP archive: theme.hcl first, the rest sorted,
// deflated, with a fixed time, so that the output depends on the content only.
func WriteFiles(files Files) ([]byte, error) {
	names := files.Names()
	sort.SliceStable(names, func(i, j int) bool { return names[i] == MetaFile && names[j] != MetaFile })
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, n := range names {
		if err := ValidName(n, 0); err != nil {
			return nil, archiveErr("%v", err)
		}
		w, err := zw.CreateHeader(&zip.FileHeader{Name: n, Method: zip.Deflate, Modified: zipEpoch})
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(files[n]); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// pathDir is path.Dir for an entry name: "" for a root file.
func pathDir(name string) string {
	d := path.Dir(name)
	if d == "." {
		return ""
	}
	return d
}
