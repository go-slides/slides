// Package serve is the server behind slides build --server and the live
// reload behind --watch: marp-cli's, read from its 4.5.1 sources and tightened
// where a local server is the way into a machine.
//
// The server serves one directory: its files as they are, its Markdown decks
// converted on request (deck.md renders as HTML, deck.md?pdf as a PDF, ...),
// and an index of each directory. It binds to the loopback interface unless it
// is told otherwise, refuses a Host or an Origin it does not know (DNS
// rebinding), and never leaves the directory: a path is cleaned, its symbolic
// links are resolved, and the result must be inside the directory.
package serve

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

// ErrForbidden is the error for a path that leaves the served directory or
// names something the server hides.
var ErrForbidden = errors.New("serve: path not allowed")

// Resolve maps the path of a URL to a file or a directory inside root, which
// must be absolute and free of symbolic links (see Root). The path is
// cleaned, so ".." cannot climb; a name that starts with a dot is not there
// (.git, .env: express's static middleware ignores dotfiles too); and the
// symbolic links of what is left are resolved, which must still be inside root
// — a link inside the directory that points outside it is not followed.
//
// It returns the resolved path and its info; ErrForbidden for a path that is
// not allowed, an error satisfying errors.Is(err, fs.ErrNotExist) for one that
// is not there.
func Resolve(root, urlPath string) (string, fs.FileInfo, error) {
	if strings.ContainsRune(urlPath, 0) {
		return "", nil, ErrForbidden
	}
	clean := path.Clean("/" + urlPath)
	if runtime.GOOS == "windows" && strings.ContainsAny(clean, `\:`) {
		// a backslash is a separator and a colon a drive or a stream there
		return "", nil, ErrForbidden
	}
	for _, seg := range strings.Split(clean, "/") {
		if strings.HasPrefix(seg, ".") {
			// as if it were not there, which is what express answers
			return "", nil, fs.ErrNotExist
		}
	}
	full := filepath.Join(root, filepath.FromSlash(clean))
	real, err := filepath.EvalSymlinks(full)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil, fs.ErrNotExist
		}
		return "", nil, err
	}
	if !inside(root, real) {
		return "", nil, ErrForbidden
	}
	st, err := os.Stat(real)
	if err != nil {
		return "", nil, err
	}
	return real, st, nil
}

// Root returns the directory to serve in the form Resolve wants: absolute,
// with its symbolic links resolved, and a directory.
func Root(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", errors.New("input directory \"" + abs + "\" is not found")
		}
		return "", err
	}
	st, err := os.Stat(real)
	if err != nil {
		return "", err
	}
	if !st.IsDir() {
		return "", errors.New("\"" + abs + "\" is not directory")
	}
	return real, nil
}

// inside reports whether p is root or below it.
func inside(root, p string) bool {
	if p == root {
		return true
	}
	if !strings.HasSuffix(root, string(filepath.Separator)) {
		root += string(filepath.Separator)
	}
	return strings.HasPrefix(p, root)
}

// markdownExts are the extensions of the files marp-cli converts.
var markdownExts = []string{".md", ".mdown", ".markdown", ".markdn"}

// IsMarkdown reports whether a file name has a Markdown extension, as
// marp-cli's finder takes it: in lower case (UPPER.MD is not a deck).
func IsMarkdown(name string) bool {
	return slices.Contains(markdownExts, filepath.Ext(name))
}
