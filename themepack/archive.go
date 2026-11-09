// Package themepack reads, checks and writes theme packages: one tar archive
// compressed with xz, extension .gsta.xz (Go-Slides Theme Archive), holding
// theme.hcl at its root and the CSS, fonts, images and kinds it declares.
//
// A package is untrusted input (a playground share link can carry one), so
// nothing in it is believed before it is checked:
//
//   - the xz stream is read with go-compressions/xz, whose window is allocated
//     as the data arrives and never beyond Limits.MaxDictionary, whatever the
//     block header declares (xz file format 3.1.1: the LZMA2 dictionary size
//     can be up to 4 GiB), and a single stream, as a package is one;
//   - the tar reader accepts ustar and pax headers (IEEE Std 1003.1, pax
//     interchange format) and refuses what is not a regular file or a
//     directory (links, symbolic links, devices, FIFOs, GNU extensions), a
//     name that is absolute, has a drive letter, a backslash, or a ".."
//     element, a duplicate name (compared without case, so that one name cannot
//     shadow another on a case-folding disk), and a pax header over
//     Limits.MaxPax bytes or one that declares a sparse file;
//   - sizes are bounded by what is read, not by what a header declares, and so
//     are the entry count, the total, and the ratio of what is inflated to the
//     archive's size (a solid archive has no per-entry ratio);
//   - theme.hcl is parsed with HCL's native syntax (hclsyntax/spec.md), with
//     literals only: no function, no variable, no file, no environment;
//   - CSS, SVG, fonts and images are checked (see check.go).
//
// Every refusal is a diag error with a code: theme.archive, theme.limit,
// theme.meta, theme.css, theme.svg, theme.font, theme.checksum, theme.version.
package themepack

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-compressions/xz"
	ulixz "github.com/ulikunitz/xz"

	"github.com/go-slides/slides/diag"
)

// Extension is the file extension of a theme package: the tar archive's
// suffix, then xz's. Use IsPackage to test a name; path.Ext sees only ".xz".
const Extension = ".gsta.xz"

// IsPackage reports whether name ends in Extension, whatever its case.
func IsPackage(name string) bool { return strings.HasSuffix(strings.ToLower(name), Extension) }

// MetaFile is the metadata file at the root of a package.
const MetaFile = "theme.hcl"

// Limits bound what a package may make the reader spend. A field that is zero
// takes its default.
type Limits struct {
	// MaxArchive is the size of the .gsta.xz file.
	MaxArchive int64
	// MaxFiles is the number of entries, directories included.
	MaxFiles int
	// MaxFile is the uncompressed size of one entry.
	MaxFile int64
	// MaxTotal is the uncompressed size of all entries together.
	MaxTotal int64
	// MaxRatio is the largest ratio of the inflated tar stream to the archive's
	// size, once the stream is MinRatioSize bytes.
	MaxRatio int64
	// MaxDictionary is the xz dictionary a block may ask for.
	MaxDictionary int64
	// MaxPax is the size of the records of one pax header.
	MaxPax int
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

// MinRatioSize is the size under which the ratio is not checked: a short run
// of the same byte compresses far better than 100 to 1 legitimately, and it is
// bounded by MaxFile and MaxTotal anyway.
const MinRatioSize = 64 << 10

// DefaultLimits are the bounds Open and Read apply: above any theme worth
// shipping (fonts included), far below what a hostile archive of a few
// kilobytes can ask for.
var DefaultLimits = Limits{
	MaxArchive:    16 << 20,
	MaxFiles:      256,
	MaxFile:       8 << 20,
	MaxTotal:      24 << 20,
	MaxRatio:      100,
	MaxDictionary: 16 << 20,
	MaxPax:        8 << 10,
	MaxMeta:       64 << 10,
	MaxDepth:      32,
	MaxPath:       255,
	MaxCSS:        1 << 20,
	MaxFont:       4 << 20,
	MaxImage:      4 << 20,
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
	set64(&d.MaxDictionary, l.MaxDictionary)
	set64(&d.MaxMeta, l.MaxMeta)
	set64(&d.MaxCSS, l.MaxCSS)
	set64(&d.MaxFont, l.MaxFont)
	set64(&d.MaxImage, l.MaxImage)
	if l.MaxFiles > 0 {
		d.MaxFiles = l.MaxFiles
	}
	if l.MaxPax > 0 {
		d.MaxPax = l.MaxPax
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

// errCapped is what the capped reader returns when the inflated stream passes
// what the limits allow.
var errCapped = errors.New("inflated stream over its cap")

// capReader passes at most max bytes and then fails with errCapped, so that a
// bomb is refused, not mistaken for the end of the stream.
type capReader struct {
	r    io.Reader
	n    int64
	max  int64
	trip bool
}

func (c *capReader) Read(p []byte) (int, error) {
	if c.n >= c.max {
		// one more byte tells a stream that is exactly max from one that is longer
		var b [1]byte
		k, err := c.r.Read(b[:])
		if k > 0 {
			c.trip = true
			return 0, errCapped
		}
		return 0, err
	}
	if int64(len(p)) > c.max-c.n {
		p = p[:c.max-c.n]
	}
	k, err := c.r.Read(p)
	c.n += int64(k)
	return k, err
}

// streamCap is how much tar stream a package may inflate to: the entries'
// total, 512-byte headers and block padding for each (a pax header doubles
// them), the closing blocks, and the ratio against the archive's size.
func streamCap(lim Limits, archive int64) (max int64, byRatio bool) {
	byEntries := lim.MaxTotal + int64(lim.MaxFiles)*(2*512+2*int64(lim.MaxPax)+1024) + 1<<20
	byRatio64 := max64(MinRatioSize, lim.MaxRatio*max64(archive, 1))
	if byRatio64 < byEntries {
		return byRatio64, true
	}
	return byEntries, false
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// ReadFiles reads a .gsta.xz archive of size bytes under the limits and
// returns its regular files. It checks the archive, not what is in it (see
// Read).
func ReadFiles(r io.ReaderAt, size int64, lim Limits) (Files, error) {
	lim = lim.resolved()
	if size > lim.MaxArchive {
		return nil, limitErr("archive of %d bytes, the limit is %d", size, lim.MaxArchive)
	}
	xr, err := xz.ReaderConfig{MaxDictionary: int(min(lim.MaxDictionary, 1<<30)), SingleStream: true}.
		NewReader(io.NewSectionReader(r, 0, size))
	if err != nil {
		return nil, xzErr(err, lim)
	}
	max, byRatio := streamCap(lim, size)
	cr := &capReader{r: xr, max: max}
	files, err := readTar(cr, lim)
	if err != nil {
		if cr.trip || errors.Is(err, errCapped) {
			if byRatio {
				return nil, limitErr("the archive inflates to more than %d times its size (%d bytes), the limit is %d:1", lim.MaxRatio, size, lim.MaxRatio)
			}
			return nil, limitErr("the archive inflates to more than %d bytes", max)
		}
		if diag.CodeOf(err) != "" {
			return nil, err
		}
		return nil, xzErr(err, lim)
	}
	return files, nil
}

// xzErr names a failure of the xz layer.
func xzErr(err error, lim Limits) error {
	if errors.Is(err, xz.ErrTooLarge) {
		return limitErr("the xz dictionary is over %d bytes", lim.MaxDictionary)
	}
	return archiveErr("not a valid .xz stream: %v", err)
}

func readTar(r io.Reader, lim Limits) (Files, error) {
	tr := tar.NewReader(r)
	files := Files{}
	seen := map[string]bool{}
	var entries int
	var declared, total int64
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			if errors.Is(err, tar.ErrHeader) {
				return nil, archiveErr("not a tar archive: %v", err)
			}
			return nil, err
		}
		if entries++; entries > lim.MaxFiles {
			return nil, limitErr("more than %d entries", lim.MaxFiles)
		}
		if h.Format != tar.FormatUSTAR && h.Format != tar.FormatPAX {
			return nil, archiveErr("%q: only ustar and pax headers are read, not %v", h.Name, h.Format)
		}
		recs := 0
		for k, v := range h.PAXRecords {
			recs += len(k) + len(v) + 3
			if strings.HasPrefix(k, "GNU.sparse") {
				return nil, archiveErr("%q is a sparse file", h.Name)
			}
		}
		if recs > lim.MaxPax {
			return nil, limitErr("%q has pax records of %d bytes, the limit is %d", h.Name, recs, lim.MaxPax)
		}
		if err := ValidName(h.Name, lim.MaxPath); err != nil {
			return nil, archiveErr("%v", err)
		}
		key := strings.ToLower(strings.TrimSuffix(h.Name, "/"))
		if seen[key] {
			return nil, archiveErr("duplicate name %q", h.Name)
		}
		seen[key] = true
		switch h.Typeflag {
		case tar.TypeDir:
			continue
		case tar.TypeReg:
		default:
			return nil, archiveErr("%q is not a regular file or a directory (type %q): links, devices and FIFOs are refused", h.Name, string(h.Typeflag))
		}
		if strings.HasSuffix(h.Name, "/") {
			return nil, archiveErr("%q: a regular file has no trailing slash", h.Name)
		}
		if h.Size < 0 || h.Size > lim.MaxFile {
			return nil, limitErr("%q declares %d bytes, the limit is %d", h.Name, h.Size, lim.MaxFile)
		}
		if declared += h.Size; declared > lim.MaxTotal {
			return nil, limitErr("the entries declare more than %d bytes together", lim.MaxTotal)
		}
		b, err := io.ReadAll(io.LimitReader(tr, lim.MaxFile+1))
		if err != nil {
			return nil, err
		}
		if int64(len(b)) != h.Size {
			return nil, archiveErr("%q declares %d bytes and holds %d", h.Name, h.Size, len(b))
		}
		if total += int64(len(b)); total > lim.MaxTotal {
			return nil, limitErr("the entries inflate to more than %d bytes together", lim.MaxTotal)
		}
		files[h.Name] = b
	}
	// What follows the closing blocks is padding: zeros, to the end of the
	// stream, which also makes the xz check of the last block run.
	var buf [4096]byte
	for {
		n, err := r.Read(buf[:])
		for _, c := range buf[:n] {
			if c != 0 {
				return nil, archiveErr("data after the end of the tar archive")
			}
		}
		if err == io.EOF {
			return files, nil
		}
		if err != nil {
			return nil, err
		}
	}
}

// tarEpoch is the modification time of every entry WriteFiles makes, so that
// packing the same directory twice gives the same bytes.
var tarEpoch = time.Unix(0, 0)

// WriteFiles writes files as a .gsta.xz archive: a pax-format tar (ustar
// headers, pax records only for what ustar cannot hold) with theme.hcl first
// and the rest sorted, a fixed time, owner and mode, compressed with xz (a 4
// MiB dictionary, CRC64), so that the output depends on the content only.
//
// go-compressions/xz reads xz and does not write it yet; the writer is
// ulikunitz/xz's, used here and nowhere on the read path.
func WriteFiles(files Files) ([]byte, error) {
	names := files.Names()
	sort.SliceStable(names, func(i, j int) bool { return names[i] == MetaFile && names[j] != MetaFile })
	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)
	for _, n := range names {
		if err := ValidName(n, 0); err != nil {
			return nil, archiveErr("%v", err)
		}
		h := &tar.Header{
			Typeflag: tar.TypeReg, Name: n, Size: int64(len(files[n])), Mode: 0o644,
			ModTime: tarEpoch, Format: tar.FormatPAX,
		}
		if err := tw.WriteHeader(h); err != nil {
			return nil, err
		}
		if _, err := tw.Write(files[n]); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	var out bytes.Buffer
	xw, err := ulixz.WriterConfig{DictCap: 4 << 20}.NewWriter(&out)
	if err != nil {
		return nil, err
	}
	if _, err := xw.Write(tarBuf.Bytes()); err != nil {
		return nil, err
	}
	if err := xw.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// pathDir is path.Dir for an entry name: "" for a root file.
func pathDir(name string) string {
	d := path.Dir(name)
	if d == "." {
		return ""
	}
	return d
}
