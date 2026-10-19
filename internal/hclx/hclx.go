// Package hclx evaluates the HCL2 native syntax (github.com/hashicorp/hcl/v2,
// hclsyntax) wherever go-slides accepts YAML: a deck's "---hcl" front matter,
// slides.hcl, kinds files and data files.
//
// It holds what those places share: the evaluation context (go-cty's
// standard functions, page and total, env, file, now and timestamp, and the
// data variable), the conversions between cty values and the Go values the
// YAML path produces, and diagnostics turned into errors that keep HCL's
// file:line:col positions.
package hclx

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/function"
	"github.com/zclconf/go-cty/cty/function/stdlib"

	"github.com/go-slides/slides/interp"
)

// MaxFileSize bounds what the file function reads, so that a deck cannot
// make the build read a device or a huge file into memory.
const MaxFileSize = 1 << 20

// Env is what an evaluation can see.
type Env struct {
	// Data is the template data, the data variable of expressions.
	Data map[string]any
	// BaseDir resolves the file function's relative paths.
	BaseDir string
	// Now is the clock behind now and timestamp; nil means time.Now.
	Now func() time.Time
}

// Context returns the evaluation context: the functions, and the data
// variable when Data is not nil.
func (e Env) Context() *hcl.EvalContext {
	ctx := &hcl.EvalContext{Functions: Functions(e)}
	if e.Data != nil {
		ctx.Variables = map[string]cty.Value{"data": FromGo(e.Data)}
	}
	return ctx
}

// Functions returns the function table: a documented subset of go-cty's
// standard library (no function reaches the network or the file system),
// plus this tool's own.
func Functions(e Env) map[string]function.Function {
	now := e.Now
	if now == nil {
		now = time.Now
	}
	str := func(s string) function.Function {
		return function.New(&function.Spec{
			Type: function.StaticReturnType(cty.String),
			Impl: func([]cty.Value, cty.Type) (cty.Value, error) { return cty.StringVal(s), nil },
		})
	}
	clock := func(utc bool) function.Function {
		return function.New(&function.Spec{
			Type: function.StaticReturnType(cty.String),
			Impl: func([]cty.Value, cty.Type) (cty.Value, error) {
				t := now()
				if utc {
					t = t.UTC()
				}
				return cty.StringVal(t.Format(time.RFC3339)), nil
			},
		})
	}
	return map[string]function.Function{
		// Numbers.
		"abs":      stdlib.AbsoluteFunc,
		"ceil":     stdlib.CeilFunc,
		"floor":    stdlib.FloorFunc,
		"log":      stdlib.LogFunc,
		"max":      stdlib.MaxFunc,
		"min":      stdlib.MinFunc,
		"parseint": stdlib.ParseIntFunc,
		"pow":      stdlib.PowFunc,
		"signum":   stdlib.SignumFunc,
		// Strings.
		"chomp":        stdlib.ChompFunc,
		"format":       stdlib.FormatFunc,
		"formatlist":   stdlib.FormatListFunc,
		"join":         stdlib.JoinFunc,
		"lower":        stdlib.LowerFunc,
		"regex":        stdlib.RegexFunc,
		"regexall":     stdlib.RegexAllFunc,
		"regexreplace": stdlib.RegexReplaceFunc,
		"replace":      stdlib.ReplaceFunc,
		"split":        stdlib.SplitFunc,
		"strlen":       stdlib.StrlenFunc,
		"strrev":       stdlib.ReverseFunc,
		"substr":       stdlib.SubstrFunc,
		"title":        stdlib.TitleFunc,
		"trim":         stdlib.TrimFunc,
		"trimprefix":   stdlib.TrimPrefixFunc,
		"trimspace":    stdlib.TrimSpaceFunc,
		"trimsuffix":   stdlib.TrimSuffixFunc,
		"upper":        stdlib.UpperFunc,
		// Collections.
		"chunklist":    stdlib.ChunklistFunc,
		"coalesce":     stdlib.CoalesceFunc,
		"coalescelist": stdlib.CoalesceListFunc,
		"compact":      stdlib.CompactFunc,
		"concat":       stdlib.ConcatFunc,
		"contains":     stdlib.ContainsFunc,
		"distinct":     stdlib.DistinctFunc,
		"element":      stdlib.ElementFunc,
		"flatten":      stdlib.FlattenFunc,
		"keys":         stdlib.KeysFunc,
		"length":       stdlib.LengthFunc,
		"lookup":       stdlib.LookupFunc,
		"merge":        stdlib.MergeFunc,
		"range":        stdlib.RangeFunc,
		"reverse":      stdlib.ReverseListFunc,
		"slice":        stdlib.SliceFunc,
		"sort":         stdlib.SortFunc,
		"values":       stdlib.ValuesFunc,
		"zipmap":       stdlib.ZipmapFunc,
		// Encodings.
		"csvdecode":  stdlib.CSVDecodeFunc,
		"jsondecode": stdlib.JSONDecodeFunc,
		"jsonencode": stdlib.JSONEncodeFunc,
		// Dates.
		"formatdate": stdlib.FormatDateFunc,
		"timeadd":    stdlib.TimeAddFunc,
		// This tool's own.
		"page":      str(interp.PageSentinel),
		"total":     str(interp.TotalSentinel),
		"now":       clock(false),
		"timestamp": clock(true),
		"env": function.New(&function.Spec{
			Params: []function.Parameter{{Name: "name", Type: cty.String}},
			Type:   function.StaticReturnType(cty.String),
			Impl: func(args []cty.Value, _ cty.Type) (cty.Value, error) {
				return cty.StringVal(os.Getenv(args[0].AsString())), nil
			},
		}),
		"file": function.New(&function.Spec{
			Params: []function.Parameter{{Name: "path", Type: cty.String}},
			Type:   function.StaticReturnType(cty.String),
			Impl: func(args []cty.Value, _ cty.Type) (cty.Value, error) {
				s, err := ReadFile(e.BaseDir, args[0].AsString())
				return cty.StringVal(s), err
			},
		}),
	}
}

// ReadFile reads a UTF-8 text file of at most MaxFileSize bytes, a relative
// path being taken from dir. Only regular files are read.
func ReadFile(dir, path string) (string, error) {
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !st.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file", path)
	}
	b, err := io.ReadAll(io.LimitReader(f, MaxFileSize+1))
	if err != nil {
		return "", err
	}
	if len(b) > MaxFileSize {
		return "", fmt.Errorf("%s is larger than %d bytes", path, MaxFileSize)
	}
	if !utf8.Valid(b) {
		return "", fmt.Errorf("%s is not UTF-8 text", path)
	}
	return string(b), nil
}

// Parse parses HCL native syntax. start is the position of src's first byte
// in its file, so that a front matter reports the deck's own lines.
func Parse(src []byte, filename string, start hcl.Pos) (*hclsyntax.Body, error) {
	if start.Line == 0 {
		start = hcl.InitialPos
	}
	f, diags := hclsyntax.ParseConfig(src, filename, start)
	if diags.HasErrors() {
		return nil, Error(diags)
	}
	return f.Body.(*hclsyntax.Body), nil
}

// Data evaluates a data file: each attribute is a key of the template data,
// with its typed value (see ToGo).
func Data(src []byte, filename string, env Env) (map[string]any, error) {
	body, err := Parse(src, filename, hcl.InitialPos)
	if err != nil {
		return nil, err
	}
	if len(body.Blocks) > 0 {
		b := body.Blocks[0]
		return nil, Errorf(b.TypeRange, "Unexpected block", "a data file holds attributes only, not %q blocks", b.Type)
	}
	ctx := env.Context()
	out := make(map[string]any, len(body.Attributes))
	for _, a := range Attributes(body) {
		v, err := Eval(a, ctx)
		if err != nil {
			return nil, err
		}
		out[a.Name] = ToGo(v)
	}
	return out, nil
}

// Attributes returns a body's attributes in source order: directives apply
// in the order they are written.
func Attributes(b *hclsyntax.Body) []*hclsyntax.Attribute {
	attrs := make([]*hclsyntax.Attribute, 0, len(b.Attributes))
	for _, a := range b.Attributes {
		attrs = append(attrs, a)
	}
	slices.SortFunc(attrs, func(a, b *hclsyntax.Attribute) int {
		return a.SrcRange.Start.Byte - b.SrcRange.Start.Byte
	})
	return attrs
}

// Eval evaluates an attribute's expression.
func Eval(a *hclsyntax.Attribute, ctx *hcl.EvalContext) (cty.Value, error) {
	v, diags := a.Expr.Value(ctx)
	if diags.HasErrors() {
		return cty.NilVal, Error(diags)
	}
	return v, nil
}

// Errorf returns an error positioned on r, formatted like a diagnostic.
func Errorf(r hcl.Range, summary, detail string, args ...any) error {
	return Error(hcl.Diagnostics{{
		Severity: hcl.DiagError,
		Summary:  summary,
		Detail:   fmt.Sprintf(detail, args...),
		Subject:  &r,
	}})
}

// Error turns diagnostics into an error, one "file:line:col: summary: detail"
// line per error diagnostic. It returns nil when there is none.
func Error(diags hcl.Diagnostics) error {
	var errs []error
	for _, d := range diags {
		if d.Severity != hcl.DiagError {
			continue
		}
		msg := d.Summary
		if d.Detail != "" {
			msg += ": " + d.Detail
		}
		if d.Subject != nil {
			r := d.Subject
			msg = fmt.Sprintf("%s:%d:%d: %s", r.Filename, r.Start.Line, r.Start.Column, msg)
		}
		errs = append(errs, errors.New(msg))
	}
	return errors.Join(errs...)
}

// ToGo converts a value to what YAML's core schema would decode: strings,
// bools, int when a number is integral and fits, float64 otherwise,
// []any and map[string]any. Null is nil.
func ToGo(v cty.Value) any {
	if v.IsNull() || !v.IsKnown() {
		return nil
	}
	v, _ = v.Unmark()
	t := v.Type()
	switch {
	case t == cty.String:
		return v.AsString()
	case t == cty.Bool:
		return v.True()
	case t == cty.Number:
		return number(v.AsBigFloat())
	case t.IsListType() || t.IsTupleType() || t.IsSetType():
		out := make([]any, 0, v.LengthInt())
		for it := v.ElementIterator(); it.Next(); {
			_, e := it.Element()
			out = append(out, ToGo(e))
		}
		return out
	case t.IsMapType() || t.IsObjectType():
		out := make(map[string]any, v.LengthInt())
		for it := v.ElementIterator(); it.Next(); {
			k, e := it.Element()
			out[k.AsString()] = ToGo(e)
		}
		return out
	}
	return nil
}

func number(f *big.Float) any {
	if f.IsInt() {
		if i, acc := f.Int64(); acc == big.Exact && int64(int(i)) == i {
			return int(i)
		}
	}
	x, _ := f.Float64()
	return x
}

// Failsafe converts a value to the form directives decoded with YAML's
// failsafe schema take: every scalar a string ("true", "3"), lists as
// []any. Null is nil.
func Failsafe(v cty.Value) any {
	switch x := ToGo(v).(type) {
	case nil:
		return nil
	case string:
		return x
	case []any:
		for i, e := range x {
			x[i] = failsafeScalar(e)
		}
		return x
	case map[string]any:
		for k, e := range x {
			x[k] = failsafeScalar(e)
		}
		return x
	default:
		return failsafeScalar(x)
	}
}

func failsafeScalar(v any) any {
	switch x := v.(type) {
	case nil, string:
		return x
	case float64:
		return formatFloat(x)
	case []any, map[string]any:
		return x
	}
	return fmt.Sprint(v)
}

func formatFloat(f float64) string { return strconv.FormatFloat(f, 'g', -1, 64) }

// YAML writes a value as a one-line YAML flow node: strings double-quoted
// (JSON escapes are YAML escapes), numbers and bools plain, lists and
// objects as flow sequences and mappings. One line per value keeps the
// result safe from Marp's loose YAML, which rewrites "key: value" lines.
func YAML(v cty.Value) string {
	var b strings.Builder
	writeYAML(&b, ToGo(v))
	return b.String()
}

func writeYAML(b *strings.Builder, v any) {
	switch x := v.(type) {
	case nil:
		b.WriteString("null")
	case string:
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		_ = enc.Encode(x)
		b.Write(bytes.TrimRight(buf.Bytes(), "\n"))
	case bool:
		b.WriteString(strconv.FormatBool(x))
	case int:
		b.WriteString(strconv.Itoa(x))
	case float64:
		switch {
		case math.IsInf(x, 1):
			b.WriteString(".inf")
		case math.IsInf(x, -1):
			b.WriteString("-.inf")
		default:
			s := formatFloat(x)
			if !strings.ContainsAny(s, ".eEn") {
				s += ".0" // keep it a float
			}
			b.WriteString(s)
		}
	case []any:
		b.WriteString("[")
		for i, e := range x {
			if i > 0 {
				b.WriteString(", ")
			}
			writeYAML(b, e)
		}
		b.WriteString("]")
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteString("{")
		for i, k := range keys {
			if i > 0 {
				b.WriteString(", ")
			}
			writeYAML(b, k)
			b.WriteString(": ")
			writeYAML(b, x[k])
		}
		b.WriteString("}")
	}
}

// FromGo converts template data to a cty value.
func FromGo(v any) cty.Value {
	switch x := v.(type) {
	case nil:
		return cty.NullVal(cty.DynamicPseudoType)
	case string:
		return cty.StringVal(x)
	case bool:
		return cty.BoolVal(x)
	case int:
		return cty.NumberIntVal(int64(x))
	case int64:
		return cty.NumberIntVal(x)
	case uint64:
		return cty.NumberUIntVal(x)
	case float64:
		return cty.NumberFloatVal(x)
	case time.Time:
		return cty.StringVal(x.Format(time.RFC3339))
	case []string:
		vals := make([]cty.Value, len(x))
		for i, e := range x {
			vals[i] = cty.StringVal(e)
		}
		return tuple(vals)
	case []any:
		vals := make([]cty.Value, len(x))
		for i, e := range x {
			vals[i] = FromGo(e)
		}
		return tuple(vals)
	case map[string]any:
		attrs := make(map[string]cty.Value, len(x))
		for k, e := range x {
			if strings.HasPrefix(k, "\x00") {
				continue // the YAML decoder's own bookkeeping
			}
			attrs[k] = FromGo(e)
		}
		if len(attrs) == 0 {
			return cty.EmptyObjectVal
		}
		return cty.ObjectVal(attrs)
	}
	return cty.StringVal(fmt.Sprint(v))
}

func tuple(vals []cty.Value) cty.Value {
	if len(vals) == 0 {
		return cty.EmptyTupleVal
	}
	return cty.TupleVal(vals)
}
