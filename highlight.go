package slides

import (
	"strings"

	"github.com/go-rouge/rouge"
)

// Highlight colours a fenced code block with go-rouge, in highlight.js class
// names (hljs-keyword, hljs-title function_), which is what Marp's themes
// style: Marp Core highlights with highlight.js. ok is false when no lexer
// knows the language, and the block is then written as plain text, as Marp
// Core does for a language highlight.js does not know.
func Highlight(code, lang string) (html string, ok bool) {
	if lang == "" {
		return "", false
	}
	lexer := rouge.FindLexer(strings.ToLower(lang))
	if lexer == nil {
		return "", false
	}
	return rouge.HLJSFormatter{}.Format(lexer.Lex(code)), true
}
