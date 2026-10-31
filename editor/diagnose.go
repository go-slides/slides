package editor

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// Diagnostic is a problem positioned in a file. Lines are 0-based and
// columns count UTF-16 code units, as editors (LSP, VS Code) do.
type Diagnostic struct {
	// File is the path of the file the problem is in: the deck, or a file
	// it reads (a kinds file, an included fragment, slides.hcl).
	File      string `json:"file"`
	Line      int    `json:"line"`
	Column    int    `json:"column"`
	EndLine   int    `json:"endLine"`
	EndColumn int    `json:"endColumn"`
	// Severity is "error" or "warning".
	Severity string `json:"severity"`
	Message  string `json:"message"`
	// Code names the kind of problem, stable across releases and wording
	// (see Codes): the Message is the compiler's or the parser's, in English;
	// a UI that speaks another language explains the Code. "" when the
	// problem is none of those known.
	Code string `json:"code,omitempty"`
}

// Codes are the diagnostic codes, with what each means in English (the text a
// UI translates).
var Codes = map[string]string{
	"kind.unknown":      "The slide names a kind that is not defined.",
	"directive.unknown": "The directive is not one go-slides knows.",
	"template.function": "The template calls a function that does not exist.",
	"template.exec":     "The template failed while it was filled in.",
	"template.syntax":   "The template is not well formed ({{ … }} unbalanced or unexpected).",
	"yaml.syntax":       "The YAML is not well formed.",
	"hcl.unsupported":   "The HCL file uses something go-slides does not read.",
	"theme.unknown":     "The deck's theme is not one that exists.",
	"file.missing":      "A file the deck reads cannot be found.",
}

// codeRules map a message to its Code, first match wins. The messages are the
// ones go-slides and its parsers write; a message that matches none has no code.
var codeRules = []struct {
	code string
	re   *regexp.Regexp
}{
	{"kind.unknown", regexp.MustCompile(`unknown kind`)},
	{"directive.unknown", regexp.MustCompile(`unknown directive|Unsupported directive`)},
	{"template.function", regexp.MustCompile(`function "[^"]+" not defined`)},
	{"template.exec", regexp.MustCompile(`executing "|error calling`)},
	{"template.syntax", regexp.MustCompile(`unexpected .*in|unclosed action|unexpected "}"|unterminated`)},
	{"yaml.syntax", regexp.MustCompile(`^yaml:|did not find expected|mapping values are not allowed|could not find expected`)},
	{"hcl.unsupported", regexp.MustCompile(`^Unsupported (attribute|argument|block)`)},
	{"theme.unknown", regexp.MustCompile(`^theme .*(not found|unknown|no such)`)},
	{"file.missing", regexp.MustCompile(`no such file|cannot find|not found`)},
}

// CodeOf is the Code of a diagnostic message, "" if none.
func CodeOf(msg string) string {
	for _, r := range codeRules {
		if r.re.MatchString(msg) {
			return r.code
		}
	}
	return ""
}

// Positions in the errors go-slides returns:
//
//	deck.md:12: unknown kind "x" ...               (deck)
//	template: deck.md:12:5: executing ...          (text/template)
//	slides.hcl:3:9: Unsupported attribute: ...     (HCL)
//	kinds.yaml: yaml: line 4: mapping values ...   (YAML)
var (
	posRE  = regexp.MustCompile(`^(?:template: )?(.+?):(\d+)(?::(\d+))?: (.*)$`)
	yamlRE = regexp.MustCompile(`^(.+?): (?:yaml: )?line (\d+): (.*)$`)
	// keyRE is a front-matter value's error: "front matter: footer: ...".
	keyRE = regexp.MustCompile(`^front matter: (\w+): (.*)$`)
)

// frame is one position found in an error message.
type frame struct {
	file      string
	line, col int // 1-based; col 0 when unknown
	rest      string
}

// frames peels the positions off an error message, outermost first:
// "deck.md:1: front matter: template: deck.md:3:5: ..." is two frames.
func frames(msg string) []frame {
	var out []frame
	for range 8 {
		f, ok := parseFrame(msg)
		if !ok {
			break
		}
		out = append(out, f)
		msg = f.rest
		if p, ok := strings.CutPrefix(msg, "front matter: "); ok {
			if _, ok := parseFrame(p); ok {
				msg = p
			}
		}
	}
	return out
}

func parseFrame(msg string) (frame, bool) {
	if m := yamlRE.FindStringSubmatch(msg); m != nil && !strings.Contains(m[1], ": ") {
		line, _ := strconv.Atoi(m[2])
		return frame{file: m[1], line: line, rest: m[3]}, true
	}
	m := posRE.FindStringSubmatch(msg)
	if m == nil || strings.Contains(m[1], ": ") {
		return frame{}, false
	}
	line, _ := strconv.Atoi(m[2])
	col, _ := strconv.Atoi(m[3])
	return frame{file: m[1], line: line, col: col, rest: m[4]}, true
}

// Diagnose positions an error from a build of the deck at path, whose text
// is src. Each line of the message (errors.Join) is one diagnostic.
func Diagnose(err error, path, src string) []Diagnostic {
	if err == nil {
		return nil
	}
	var out []Diagnostic
	for msg := range strings.SplitSeq(err.Error(), "\n") {
		if strings.TrimSpace(msg) == "" {
			continue
		}
		out = append(out, diagnose(msg, path, src)...)
	}
	return out
}

func diagnose(msg, path, src string) []Diagnostic {
	lines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
	fs := frames(msg)
	if len(fs) == 0 {
		// No position: point at the line that names what the message
		// is about, else the first line.
		return []Diagnostic{at(path, lines, locate(msg, lines), 0, "error", msg)}
	}
	inner := fs[len(fs)-1]
	outer := fs[0]
	text := inner.rest
	switch {
	case !samePath(inner.file, path):
		// The problem is in another file. Report it there, and on the
		// deck where the deck brings that file in.
		d := []Diagnostic{{
			File: inner.file, Line: max(inner.line-1, 0), Column: 0,
			EndLine: max(inner.line-1, 0), EndColumn: 1 << 16,
			Severity: "error", Message: text, Code: CodeOf(text),
		}}
		line := locate(inner.file, lines)
		if samePath(outer.file, path) && outer.line > 1 {
			line = outer.line - 1
		}
		return append(d, at(path, lines, line, 0, "error", filepath.Base(inner.file)+": "+text))
	case len(fs) > 1 && strings.HasPrefix(outer.rest, "front matter: "):
		// A template error in the YAML front matter: its lines count
		// from the line after the opening dashes, so its line n is the
		// deck's 0-based line n.
		return []Diagnostic{at(path, lines, inner.line, inner.col, "error", text)}
	}
	if m := keyRE.FindStringSubmatch(inner.rest); m != nil {
		// A front-matter value's template: point at its key.
		return []Diagnostic{at(path, lines, keyLine(m[1], lines), 0, "error", inner.rest)}
	}
	return []Diagnostic{at(path, lines, inner.line-1, inner.col, "error", text)}
}

// warning positions a build warning: on the line it is about, if it names
// one, else the first.
func warning(msg, path, src string) Diagnostic {
	lines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
	line := 0
	if strings.HasPrefix(msg, "theme ") {
		line = keyLine("theme", lines)
	} else {
		line = locate(msg, lines)
	}
	return at(path, lines, line, 0, "warning", msg)
}

// at makes a diagnostic on a 0-based line, from a 1-based byte column (0
// for the whole line) to the end of the line.
func at(path string, lines []string, line, col int, severity, msg string) Diagnostic {
	line = min(max(line, 0), max(len(lines)-1, 0))
	text := ""
	if line < len(lines) {
		text = lines[line]
	}
	start := 0
	if col > 1 {
		start = utf16Len(text[:min(col-1, len(text))])
	} else {
		// Skip the indentation.
		start = utf16Len(text[:len(text)-len(strings.TrimLeft(text, " \t"))])
	}
	end := utf16Len(text)
	if end <= start {
		end = start + 1
	}
	return Diagnostic{File: path, Line: line, Column: start, EndLine: line, EndColumn: end, Severity: severity, Message: msg, Code: CodeOf(msg)}
}

// locate returns the 0-based line of the first thing a message quotes or
// names (a "quoted" word, a file's base name) that the source holds; 0 if
// none.
func locate(msg string, lines []string) int {
	var needles []string
	for _, q := range quotedRE.FindAllStringSubmatch(msg, -1) {
		needles = append(needles, q[1])
	}
	for _, w := range strings.Fields(msg) {
		w = strings.Trim(w, `:;,"'()`)
		if strings.ContainsAny(w, `/\`) || strings.Contains(w, ".") && len(w) > 3 {
			needles = append(needles, filepath.Base(w))
		}
	}
	for _, n := range needles {
		if n == "" || n == "." {
			continue
		}
		for i, l := range lines {
			if strings.Contains(l, n) {
				return i
			}
		}
	}
	return 0
}

var quotedRE = regexp.MustCompile(`"([^"]+)"`)

// keyLine returns the 0-based line of a top-level front-matter key, 0 if
// none.
func keyLine(key string, lines []string) int {
	for i, l := range lines {
		if i == 0 {
			continue
		}
		if strings.TrimRight(l, " ") == "---" {
			break
		}
		if k, _, ok := strings.Cut(l, ":"); ok && strings.TrimSpace(k) == key {
			return i
		}
		if k, _, ok := strings.Cut(l, "="); ok && strings.TrimSpace(k) == key {
			return i
		}
	}
	return 0
}

func samePath(a, b string) bool { return filepath.Clean(a) == filepath.Clean(b) }

// utf16Len counts the UTF-16 code units of s.
func utf16Len(s string) int {
	n := 0
	for len(s) > 0 {
		r, size := utf8.DecodeRuneInString(s)
		n += utf16.RuneLen(r)
		s = s[size:]
	}
	return n
}
