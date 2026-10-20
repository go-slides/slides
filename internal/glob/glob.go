// Package glob finds the files a pattern names, as marp-cli's file arguments
// do (they go through fast-glob): "*", "?", "[a-z]" and "{md,txt}" within a
// path segment, "**" for any number of directories, no match for a name that
// starts with a dot unless the pattern's segment does, and no looking in
// node_modules.
package glob

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// maxAlternatives bounds what braces expand to: {a,b}{c,d}... is a product.
const maxAlternatives = 256

// ErrTooMany is the error for a pattern whose braces expand to more than
// maxAlternatives patterns.
var ErrTooMany = errors.New("glob: too many alternatives in braces")

// Magic reports whether a pattern has anything to expand.
func Magic(pattern string) bool { return strings.ContainsAny(pattern, "*?[{") }

// Expand opens the braces of a pattern: "a/{b,c}.md" is "a/b.md" and
// "a/c.md"; braces nest. A brace that is not closed, or has no comma, is
// literal text, as in a shell.
func Expand(pattern string) ([]string, error) {
	out, err := expand(pattern, 0)
	if err != nil {
		return nil, err
	}
	if len(out) > maxAlternatives {
		return nil, ErrTooMany
	}
	return out, nil
}

func expand(p string, depth int) ([]string, error) {
	if depth > 8 {
		return []string{p}, nil
	}
	open := strings.IndexByte(p, '{')
	if open < 0 {
		return []string{p}, nil
	}
	// the matching close brace, and the commas at its own level
	level, close := 0, -1
	var commas []int
scan:
	for i := open; i < len(p); i++ {
		switch p[i] {
		case '\\':
			i++
		case '{':
			level++
		case '}':
			level--
			if level == 0 {
				close = i
				break scan
			}
		case ',':
			if level == 1 {
				commas = append(commas, i)
			}
		}
	}
	if close < 0 || len(commas) == 0 {
		// literal: expand what follows the brace
		rest, err := expand(p[open+1:], depth+1)
		if err != nil {
			return nil, err
		}
		for i := range rest {
			rest[i] = p[:open+1] + rest[i]
		}
		return rest, nil
	}
	var parts []string
	prev := open + 1
	for _, c := range commas {
		parts = append(parts, p[prev:c])
		prev = c + 1
	}
	parts = append(parts, p[prev:close])
	var out []string
	for _, part := range parts {
		alts, err := expand(p[:open]+part+p[close+1:], depth+1)
		if err != nil {
			return nil, err
		}
		out = append(out, alts...)
		if len(out) > maxAlternatives {
			return nil, ErrTooMany
		}
	}
	return out, nil
}

// hidden is a name no wildcard reaches: a dot file or node_modules.
func hidden(name string) bool { return strings.HasPrefix(name, ".") || name == "node_modules" }

// Match reports whether name, a relative path with slashes, matches the
// pattern.
func Match(pattern, name string) bool {
	alts, err := Expand(pattern)
	if err != nil {
		return false
	}
	ns := strings.Split(name, "/")
	for _, a := range alts {
		if matchSegments(strings.Split(a, "/"), ns) {
			return true
		}
	}
	return false
}

// matchSegments matches pattern segments against name segments by dynamic
// programming: ok[i][j] says that ps[i:] matches ns[j:].
func matchSegments(ps, ns []string) bool {
	ok := make([][]bool, len(ps)+1)
	for i := range ok {
		ok[i] = make([]bool, len(ns)+1)
	}
	ok[len(ps)][len(ns)] = true
	for i := len(ps) - 1; i >= 0; i-- {
		for j := len(ns); j >= 0; j-- {
			if ps[i] == "**" {
				// no directory, or one more name segment that is not hidden
				ok[i][j] = ok[i+1][j] || j < len(ns) && !hidden(ns[j]) && ok[i][j+1]
				continue
			}
			if j == len(ns) {
				continue
			}
			ok[i][j] = ok[i+1][j+1] && segment(ps[i], ns[j])
		}
	}
	return ok[0][0]
}

// segment matches one name against one pattern segment: path.Match, with
// the dot rule.
func segment(p, name string) bool {
	if hidden(name) && !strings.HasPrefix(p, ".") && p != name {
		return false
	}
	m, err := path.Match(p, name)
	return err == nil && m
}

// Files returns the files, relative to the working directory as the pattern
// is, that match the pattern, sorted. A pattern with nothing to expand names
// one file. Directories are not matched, only the files in them.
func Files(pattern string) ([]string, error) {
	alts, err := Expand(pattern)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, a := range alts {
		segs := strings.Split(a, "/")
		// the leading segments with nothing to expand are where to look
		n := 0
		for n < len(segs) && !Magic(segs[n]) && !strings.Contains(segs[n], `\`) {
			n++
		}
		root := strings.Join(segs[:n], "/")
		if n == len(segs) {
			if st, err := os.Stat(a); err == nil && st.Mode().IsRegular() {
				out = append(out, a)
			}
			continue
		}
		start := root
		if start == "" {
			start = "."
		}
		if a[0] == '/' && root == "" {
			start = "/"
		}
		rest := strings.Join(segs[n:], "/")
		walkErr := filepath.WalkDir(filepath.FromSlash(start), func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				if p == filepath.FromSlash(start) {
					return nil // nothing there
				}
				return nil
			}
			if d.IsDir() {
				if p != filepath.FromSlash(start) && d.Name() == "node_modules" {
					return filepath.SkipDir
				}
				return nil
			}
			if d.Type()&fs.ModeSymlink != 0 {
				if st, err := os.Stat(p); err != nil || !st.Mode().IsRegular() {
					return nil
				}
			} else if !d.Type().IsRegular() {
				return nil
			}
			rel, err := filepath.Rel(filepath.FromSlash(start), p)
			if err != nil {
				return nil
			}
			if matchSegments(strings.Split(rest, "/"), strings.Split(filepath.ToSlash(rel), "/")) {
				out = append(out, p)
			}
			return nil
		})
		if walkErr != nil {
			return nil, walkErr
		}
	}
	slices.Sort(out)
	return slices.Compact(out), nil
}
