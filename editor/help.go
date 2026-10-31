package editor

import (
	"strings"
	"sync"
)

// Help in other languages. The help of a directive or a template function is
// English in the catalog; a language's words are given with RegisterHelp, keyed:
//
//	"dir.<name>"      a directive's Doc ("dir.paginate")
//	"func.<name>"     a template function's Doc ("func.upper")
//	"ui.scope.global", "ui.scope.local", "ui.scope.spot", "ui.scope.deck"
//	                  the line under a directive's name on hover
//
// A key a language lacks is read in English, so a catalogue can be partial.
// The Markdown, the code in backticks and the signatures are the catalogue's
// own and stay as they are.

var (
	helpMu sync.RWMutex
	help   = map[string]map[string]string{}
)

// RegisterHelp gives the help of a language ("fr", "pt"): the base of a tag,
// lower-case. A second call for the same language adds to the first.
func RegisterHelp(lang string, docs map[string]string) {
	lang = baseLang(lang)
	helpMu.Lock()
	defer helpMu.Unlock()
	if help[lang] == nil {
		help[lang] = map[string]string{}
	}
	for k, v := range docs {
		help[lang][k] = v
	}
}

// HelpLanguages are the languages that have help registered.
func HelpLanguages() []string {
	helpMu.RLock()
	defer helpMu.RUnlock()
	out := make([]string, 0, len(help))
	for l := range help {
		out = append(out, l)
	}
	return out
}

// baseLang reads "fr-CA", "zh_Hans", "PT-br" as fr, zh, pt.
func baseLang(tag string) string {
	tag = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(tag), "_", "-"))
	base, _, _ := strings.Cut(tag, "-")
	return base
}

// doc is the help for key in lang, english when the language has none.
func doc(lang, key, english string) string {
	if lang == "" {
		return english
	}
	helpMu.RLock()
	s, ok := help[baseLang(lang)][key]
	helpMu.RUnlock()
	if !ok || s == "" {
		return english
	}
	return s
}
