package interp

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"text/template"
	"text/template/parse"
	"time"

	"github.com/go-slides/slides/diag"
)

// A deck is untrusted input: a shared playground link, a repository one opens.
// Everything a template can multiply is therefore bounded, and an expansion that
// would pass a bound is refused with a coded error (diag) before it takes the
// process down: a deck that includes itself used to overflow the stack, and a
// range over seq or a doubling replace could ask for all of the memory.
const (
	// MaxIncludeDepth is how many includes may be open at once (a includes b
	// includes c: depth 2).
	MaxIncludeDepth = 16
	// MaxIncludes is how many include calls one expansion may make in all.
	MaxIncludes = 10_000
	// MaxIncludeBytes is the total size of the files one expansion reads with
	// include, a file counting once per call.
	MaxIncludeBytes = 8 << 20
	// MaxOutput is the size of what one template execution may write, and of
	// what repeat, replace and printf may return.
	MaxOutput = 16 << 20
	// MaxSeq is the length of one seq, and MaxIterations the sum of the lengths
	// of all the seqs of one expansion (nested ranges multiply: the inner seq is
	// called once per outer step, and each call is paid for).
	MaxSeq        = 1 << 20
	MaxIterations = 1 << 22
	// MaxWidth is the largest width or precision printf accepts.
	MaxWidth = 1 << 16
	// MaxSteps is how many loop iterations and template calls one expansion may
	// run. text/template has no hook in its loops (a range over a list the deck
	// holds calls no function), so the parsed template is given one: tickLoops.
	// MaxDuration bounds the expansion's time as a whole, for a step that is
	// itself slow.
	MaxSteps    = 1 << 20
	MaxDuration = 10 * time.Second
)

// the bounds above, as variables so that a test can lower them
var (
	maxSteps    = MaxSteps
	maxDuration = MaxDuration
)

// limits is the state one expansion shares with the includes it opens: the
// stack of files being expanded and what has been spent.
type limits struct {
	stack []string
	root  int // 1 when the root file is the bottom of the stack
	files int
	bytes int
	iter  int
	steps int
	start time.Time
}

// tickName is the function tickLoops puts in every loop body and every
// template: it counts the step, and refuses the one past the budget.
const tickName = "goSlidesTick"

func (l *limits) tick() (string, error) {
	if l.steps++; l.steps > maxSteps {
		return "", diag.Errorf("template.limit", "the template ran more than %d steps (loop iterations and template calls)", maxSteps)
	}
	if l.steps&255 == 0 && time.Since(l.start) > maxDuration {
		return "", diag.Errorf("template.limit", "the template ran for more than %v", maxDuration)
	}
	return "", nil
}

// tickLoops puts a call to the tick function at the start of every range body
// and of every template (the root of the parsed text and of each define), so that
// a nested range over data, which writes nothing and calls nothing, is counted
// and timed as it runs, and so is a template that calls itself. The call is
// parsed from the template's own syntax, so it is a node like any other, and it
// writes nothing. text/template offers no other hook: its exec takes no context,
// and a range over a slice or a map is a loop inside the package (exec.go,
// walkRange).
func tickLoops(t *template.Template) error {
	probe, err := template.New("tick").Funcs(template.FuncMap{tickName: func() string { return "" }}).Parse("{{ " + tickName + " }}")
	if err != nil {
		return err
	}
	tick := probe.Tree.Root.Nodes[0]
	list := func(l *parse.ListNode) {
		l.Nodes = append([]parse.Node{tick}, l.Nodes...)
	}
	var visit func(n parse.Node)
	visit = func(n parse.Node) {
		switch n := n.(type) {
		case *parse.ListNode:
			if n != nil {
				for _, c := range n.Nodes {
					visit(c)
				}
			}
		case *parse.IfNode:
			visit(n.List)
			visit(n.ElseList)
		case *parse.WithNode:
			visit(n.List)
			visit(n.ElseList)
		case *parse.RangeNode:
			visit(n.List)
			visit(n.ElseList)
			list(n.List)
		}
	}
	for _, tt := range t.Templates() {
		if tt.Tree == nil || tt.Tree.Root == nil {
			continue
		}
		visit(tt.Tree.Root)
		list(tt.Tree.Root)
	}
	return nil
}

// withLimits returns opts holding a limits, a new one (the root file on its
// stack) when it has none. Options are values: the caller's own never gets it, so
// a second expansion of the same options starts from zero.
func withLimits(opts Options) Options {
	if opts.lim != nil {
		return opts
	}
	opts.lim = &limits{start: time.Now()}
	if opts.Name != "" {
		opts.lim.stack = append(opts.lim.stack, rootKey(opts))
		opts.lim.root = 1
	}
	return opts
}

// rootKey is the key of the file being expanded, as the path an include of it
// would resolve to: the template's name is the deck's path, and BaseDir the
// directory include resolves from.
func rootKey(opts Options) string {
	if filepath.IsAbs(opts.Name) {
		return filepath.Clean(opts.Name)
	}
	return filepath.Join(opts.BaseDir, filepath.Base(opts.Name))
}

// enter opens an include of path (already resolved) and returns the closing func.
func (l *limits) enter(path string) (func(), error) {
	key := filepath.Clean(path)
	for i, p := range l.stack {
		if p == key {
			chain := append(append([]string{}, l.stack[i:]...), key)
			return nil, diag.Errorf("include.cycle", "include cycle: %s", strings.Join(chain, " → "))
		}
	}
	if open := len(l.stack) - l.root; open >= MaxIncludeDepth {
		return nil, diag.Errorf("include.limit", "include nested more than %d deep: %s", MaxIncludeDepth, key)
	}
	if l.files++; l.files > MaxIncludes {
		return nil, diag.Errorf("include.limit", "more than %d includes in one deck", MaxIncludes)
	}
	l.stack = append(l.stack, key)
	return func() { l.stack = l.stack[:len(l.stack)-1] }, nil
}

// read charges n bytes of an included file.
func (l *limits) read(path string, n int) error {
	if l.bytes += n; l.bytes > MaxIncludeBytes {
		return diag.Errorf("include.limit", "included files total more than %d bytes (%s)", MaxIncludeBytes, path)
	}
	return nil
}

// seq is the template function of that name: 1..n, within the bounds.
func (l *limits) seq(n int) ([]int, error) {
	if n > MaxSeq {
		return nil, diag.Errorf("template.limit", "seq %d is longer than %d", n, MaxSeq)
	}
	if n < 0 {
		n = 0
	}
	if l.iter += n; l.iter > MaxIterations {
		return nil, diag.Errorf("template.limit", "the template steps through more than %d items", MaxIterations)
	}
	s := make([]int, n)
	for i := range s {
		s[i] = i + 1
	}
	return s, nil
}

// bounded is s, or the refusal when it is longer than MaxOutput.
func bounded(what string, s string) (string, error) {
	if len(s) > MaxOutput {
		return "", diag.Errorf("template.limit", "%s would make more than %d bytes", what, MaxOutput)
	}
	return s, nil
}

func repeat(n int, s string) (string, error) {
	if n < 0 {
		return "", fmt.Errorf("repeat: negative count %d", n)
	}
	if n > 0 && len(s) > MaxOutput/n {
		return "", diag.Errorf("template.limit", "repeat would make more than %d bytes", MaxOutput)
	}
	return strings.Repeat(s, n), nil
}

func replace(old, repl, s string) (string, error) {
	if len(repl) > len(old) && strings.Count(s, old) > 0 {
		// the result is len(s) + count * (len(repl) - len(old)) long: refuse it
		// before building it
		grow := len(repl) - len(old)
		if c := strings.Count(s, old); c > (MaxOutput-len(s))/grow {
			return "", diag.Errorf("template.limit", "replace would make more than %d bytes", MaxOutput)
		}
	}
	return strings.ReplaceAll(s, old, repl), nil
}

// printf is fmt.Sprintf, refusing a width or precision that would make the
// result as long as the number says ("%2000000000d"), and `*`, which takes one
// from the arguments.
func printf(format string, args ...any) (string, error) {
	for i := 0; i < len(format); i++ {
		if format[i] != '%' {
			continue
		}
		i++
		for ; i < len(format); i++ {
			c := format[i]
			if c == '*' {
				return "", diag.Errorf("template.limit", "printf: a width taken from an argument is not allowed")
			}
			if c >= '0' && c <= '9' {
				n := 0
				for ; i < len(format) && format[i] >= '0' && format[i] <= '9'; i++ {
					if n = n*10 + int(format[i]-'0'); n > MaxWidth {
						return "", diag.Errorf("template.limit", "printf: a width above %d is not allowed", MaxWidth)
					}
				}
				i--
				continue
			}
			if c == '+' || c == '-' || c == '#' || c == ' ' || c == '.' || c == '[' || c == ']' {
				continue
			}
			break // the verb
		}
	}
	return bounded("printf", fmt.Sprintf(format, args...))
}

// limitWriter is a buffer that refuses to grow past MaxOutput.
type limitWriter struct{ bytes.Buffer }

func (w *limitWriter) Write(p []byte) (int, error) {
	if w.Len()+len(p) > MaxOutput {
		return 0, diag.Errorf("template.limit", "the template wrote more than %d bytes", MaxOutput)
	}
	return w.Buffer.Write(p)
}
