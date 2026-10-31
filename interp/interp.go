// Package interp evaluates Go text/template interpolations in a slide deck's
// Markdown before the Markdown itself is parsed.
//
// The whole body is executed as one template, so a template can generate
// slides: a {{range}} that emits "---" separators is a deck generator.
// The page number and the page total are only known once the deck has been
// split and paginated, so the page and total functions emit sentinels and
// Resolve replaces them afterwards. Every template is therefore evaluated
// exactly once, and a value coming from data is never re-parsed as one.
//
// Code is left alone by default: fenced code blocks and inline code spans are
// set aside before execution and restored verbatim afterwards, so a slide
// about Go templates can show {{ .Name }} without escaping it.
package interp

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/go-slides/slides/diag"
	"github.com/go-slides/slides/vfs"
)

// Sentinels stand for values known only after pagination. They live in the
// Unicode private-use area, which CommonMark passes through as text.
const (
	sentinelOpen  = ""
	sentinelClose = ""
)

// PageSentinel and TotalSentinel are what the page and total template
// functions produce.
var (
	PageSentinel  = sentinelOpen + "page" + sentinelClose
	TotalSentinel = sentinelOpen + "total" + sentinelClose
)

// Options configures one evaluation.
type Options struct {
	// Data is the template's dot.
	Data map[string]any
	// Delims overrides the action delimiters; both empty means "{{" "}}".
	Delims [2]string
	// Code, when true, interpolates inside code blocks and spans too.
	Code bool
	// BaseDir resolves the include function's relative paths.
	BaseDir string
	// Name names the template in error messages.
	Name string
	// Now is the clock behind the now function; nil means time.Now.
	Now func() time.Time
	// FS is where the include function reads; nil means the disk.
	FS vfs.FS
}

// Expand executes src as a template.
func Expand(src string, opts Options) (string, error) {
	var masked []string
	body := src
	if !opts.Code {
		body, masked = maskCode(src, opts.Delims)
	}
	out, err := execute(body, opts)
	if err != nil {
		return "", err
	}
	return unmaskCode(out, masked), nil
}

// Funcs is the function map available to templates.
func Funcs(opts Options) template.FuncMap {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return template.FuncMap{
		"page":  func() string { return PageSentinel },
		"total": func() string { return TotalSentinel },
		"now":   now,
		"date":  func(layout string, t time.Time) string { return t.Format(layout) },
		"env":   os.Getenv,
		"upper": strings.ToUpper,
		"lower": strings.ToLower,
		"title": func(s string) string {
			words := strings.Fields(s)
			for i, w := range words {
				r := []rune(w)
				words[i] = strings.ToUpper(string(r[0])) + string(r[1:])
			}
			return strings.Join(words, " ")
		},
		"trim":      strings.TrimSpace,
		"replace":   func(old, repl, s string) string { return strings.ReplaceAll(s, old, repl) },
		"split":     func(sep, s string) []string { return strings.Split(s, sep) },
		"join":      joinAny,
		"contains":  func(sub, s string) bool { return strings.Contains(s, sub) },
		"hasPrefix": func(prefix, s string) bool { return strings.HasPrefix(s, prefix) },
		"hasSuffix": func(suffix, s string) bool { return strings.HasSuffix(s, suffix) },
		"repeat":    func(n int, s string) string { return strings.Repeat(s, n) },
		"default": func(def, v any) any {
			if empty(v) {
				return def
			}
			return v
		},
		"seq":  seq,
		"add":  func(a, b int) int { return a + b },
		"sub":  func(a, b int) int { return a - b },
		"mul":  func(a, b int) int { return a * b },
		"list": func(v ...any) []any { return v },
		"dict": dict,
		"merge": func(a, b map[string]any) map[string]any {
			m := maps.Clone(a)
			if m == nil {
				m = map[string]any{}
			}
			maps.Copy(m, b)
			return m
		},
		"quote": strconv.Quote,
		"include": func(path string) (string, error) {
			// An included file is a fragment of the deck, so it is
			// expanded with the same data and options.
			if !filepath.IsAbs(path) {
				path = filepath.Join(opts.BaseDir, path)
			}
			b, err := vfs.Or(opts.FS).ReadFile(path)
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					err = diag.Wrap("file.missing", err)
				}
				return "", err
			}
			sub := opts
			sub.Name = path
			sub.BaseDir = filepath.Dir(path)
			return Expand(string(b), sub)
		},
	}
}

// Resolve replaces the page and total sentinels.
func Resolve(s string, page, total int) string {
	if !HasSentinel(s) {
		return s
	}
	return strings.NewReplacer(
		PageSentinel, strconv.Itoa(page),
		TotalSentinel, strconv.Itoa(total),
	).Replace(s)
}

// HasSentinel reports whether s still holds an unresolved sentinel.
func HasSentinel(s string) bool { return strings.Contains(s, sentinelOpen) }

func empty(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return x == ""
	case bool:
		return !x
	case int:
		return x == 0
	case float64:
		return x == 0
	case []any:
		return len(x) == 0
	case map[string]any:
		return len(x) == 0
	}
	return false
}

// joinAny joins any list, since YAML data decodes lists as []any.
func joinAny(sep string, v any) (string, error) {
	switch x := v.(type) {
	case []string:
		return strings.Join(x, sep), nil
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			parts[i] = fmt.Sprint(e)
		}
		return strings.Join(parts, sep), nil
	}
	return "", fmt.Errorf("join: %T is not a list", v)
}

func seq(n int) []int {
	s := make([]int, max(n, 0))
	for i := range s {
		s[i] = i + 1
	}
	return s
}

func dict(kv ...any) (map[string]any, error) {
	if len(kv)%2 != 0 {
		return nil, fmt.Errorf("dict: odd number of arguments")
	}
	m := make(map[string]any, len(kv)/2)
	for i := 0; i < len(kv); i += 2 {
		k, ok := kv[i].(string)
		if !ok {
			return nil, fmt.Errorf("dict: key %v is not a string", kv[i])
		}
		m[k] = kv[i+1]
	}
	return m, nil
}
