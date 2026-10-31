// Package diag gives the errors go-slides returns a stable Code, at the place
// they are made: a deck that names an unknown kind, a template that does not
// parse, a YAML front matter that is not well formed. The message stays the
// compiler's or the parser's, in English; the Code is what a UI explains in its
// own language, and it survives a rewording of the message.
//
// A coded error is an ordinary error: Wrap keeps the text of the error it wraps
// (so the positions in it, "deck.md:12:5: …", still read the same) and
// errors.Unwrap reaches it. CodeOf finds the code anywhere in an error's chain,
// or in the branches of an errors.Join.
package diag

import (
	"errors"
	"fmt"
	"strings"
)

// Error is an error with a Code.
type Error struct {
	Code string
	// Err is the error this one names; its text is this error's.
	Err error
}

func (e *Error) Error() string { return e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

// Wrap is err with the code; nil stays nil, and an error that has a code
// already keeps the inner one (the cause names the problem, not the layers).
func Wrap(code string, err error) error {
	if err == nil {
		return nil
	}
	if CodeOf(err) != "" {
		return err
	}
	return &Error{Code: code, Err: err}
}

// Errorf is a new coded error, its text format filled with args (a %w in it
// wraps as fmt.Errorf does).
func Errorf(code, format string, args ...any) error {
	return &Error{Code: code, Err: fmt.Errorf(format, args...)}
}

// CodeOf is the code of err's first coded error, "" when there is none.
func CodeOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

// Leaves are the errors an error is joined of, each with its own code: an
// errors.Join (or a fmt.Errorf with several %w) is split, anything else is one.
func Leaves(err error) []error {
	if err == nil {
		return nil
	}
	if j, ok := err.(interface{ Unwrap() []error }); ok {
		var out []error
		for _, e := range j.Unwrap() {
			out = append(out, Leaves(e)...)
		}
		return out
	}
	return []error{err}
}

// Codes are the codes, with what each means in English: the text a UI
// translates (its catalogue has a message for each code).
var Codes = map[string]string{
	"kind.unknown":      "The slide names a kind that is not defined.",
	"directive.unknown": "The directive is not one go-slides knows.",
	"template.function": "The template calls a function that does not exist.",
	"template.exec":     "The template failed while it was filled in.",
	"template.syntax":   "The template is not well formed ({{ … }} unbalanced or unexpected).",
	"yaml.syntax":       "The YAML is not well formed.",
	"hcl.syntax":        "The HCL is not well formed.",
	"hcl.unsupported":   "The HCL file uses something go-slides does not read.",
	"theme.unknown":     "The deck's theme is not one that exists.",
	"file.missing":      "A file the deck reads cannot be found.",
}

// ThemeUnknownPrefix starts the build warning for a theme no theme set has:
// the one warning that is a string, not an error, so its code is told by this
// prefix (WarningCode), which slides.go writes the message with.
const ThemeUnknownPrefix = "theme "

// ThemeUnknown is that warning.
func ThemeUnknown(name, fallback string, known []string) string {
	return fmt.Sprintf("%s%q is not known; using %q (known: %v)", ThemeUnknownPrefix, name, fallback, known)
}

// WarningCode is the code of a build warning, "" when it has none.
func WarningCode(msg string) string {
	if strings.HasPrefix(msg, ThemeUnknownPrefix) && strings.Contains(msg, " is not known; using ") {
		return "theme.unknown"
	}
	return ""
}
