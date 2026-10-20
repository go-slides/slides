package slides

import (
	"strings"

	"github.com/go-rouge/rouge"

	"github.com/go-slides/slides/internal/lru"
)

const (
	// highlightCacheSize bounds the highlighted blocks kept: an editor
	// re-renders the whole deck on every keystroke, and most of its code
	// blocks have not changed.
	highlightCacheSize = 256
	// maxCachedCode is the largest block cached; a bigger one is highlighted
	// every time rather than held. With the count, it bounds the cache to a
	// few megabytes of markup.
	maxCachedCode = 8 << 10
)

var highlightCache = lru.New[string](highlightCacheSize)

// Highlight colours a fenced code block with go-rouge, in highlight.js class
// names (hljs-keyword, hljs-title function_), which is what Marp's themes
// style: Marp Core highlights with highlight.js. ok is false when no lexer
// knows the language, and the block is then written as plain text, as Marp
// Core does for a language highlight.js does not know.
func Highlight(code, lang string) (html string, ok bool) {
	if lang == "" {
		return "", false
	}
	lang = strings.ToLower(lang)
	lexer := rouge.FindLexer(lang)
	if lexer == nil {
		return "", false
	}
	if len(code) > maxCachedCode {
		return rouge.HLJSFormatter{}.Format(lexer.Lex(code)), true
	}
	// The lexer is found by name, so the name and the code decide the result.
	k := lru.KeyOf(lang, code)
	if h, hit := highlightCache.Get(k); hit {
		return h, true
	}
	html = rouge.HLJSFormatter{}.Format(lexer.Lex(code))
	highlightCache.Put(k, html)
	return html, true
}
