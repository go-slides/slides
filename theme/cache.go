package theme

import (
	"slices"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/go-slides/slides/internal/lru"
)

// An editor renders the same deck again on every keystroke, and each render
// used to parse the built-in and project themes and pack the style sheet from
// scratch: a fifth of a render in WebAssembly. Parsed themes and packed sheets
// are kept in small least-recently-used caches keyed by content, so a deck
// whose themes and styles did not change reuses them, and one that changes on
// every keystroke (an edited <style>, a new @import) only ever evicts.

const (
	// themeCacheSize bounds the parsed project themes kept across sets (the
	// built-ins are parsed once, apart).
	themeCacheSize = 32
	// packCacheSize bounds the packed style sheets kept: one per deck style
	// in use, a few decks open at once.
	packCacheSize = 16
)

var (
	themeCache = lru.New[*Theme](themeCacheSize)
	packCache  = lru.New[Packed](packCacheSize)
	// themeIDs numbers parsed themes, so that a packed sheet can be keyed by
	// which themes went into it without hashing their CSS on every render.
	// A theme is immutable once parsed, and a number is never reused.
	themeIDs atomic.Uint64
)

// parseCached is Parse through the theme cache. The theme it returns is
// shared and must not be modified, which nothing in this package does.
func parseCached(src string) (*Theme, error) {
	k := lru.KeyOf(src)
	if t, ok := themeCache.Get(k); ok {
		return t, nil
	}
	t, err := Parse(src)
	if err != nil {
		return nil, err
	}
	themeCache.Put(k, t)
	return t, nil
}

// packKey identifies everything Pack reads: the theme, every theme of the
// set (imports reach any of them), the default theme, and the options. ok
// is false when a theme was not made by parse (no number): such a sheet is
// not cached.
func (s *Set) packKey(t *Theme, opts PackOptions) (key lru.Key, ok bool) {
	ok = true
	id := func(t *Theme) string {
		if t == nil {
			return "-"
		}
		if t.id == 0 {
			ok = false
		}
		return strconv.FormatUint(t.id, 10)
	}
	names := make([]string, 0, len(s.themes))
	for name := range s.themes {
		names = append(names, name)
	}
	slices.Sort(names)
	var themes strings.Builder
	for _, name := range names {
		themes.WriteString(strconv.Quote(name))
		themes.WriteString("=")
		themes.WriteString(id(s.themes[name]))
		themes.WriteString(";")
	}
	list := func(v []string) string {
		if v == nil {
			return "nil"
		}
		q := make([]string, len(v))
		for i, x := range v {
			q[i] = strconv.Quote(x)
		}
		return "[" + strings.Join(q, ",") + "]"
	}
	flags := strconv.FormatBool(opts.InlineSVG) + strconv.FormatBool(opts.Printable) +
		strconv.FormatBool(opts.ContainerQuery) + strconv.FormatBool(opts.GeneratedContent)
	key = lru.KeyOf(id(t), id(s.Default), themes.String(),
		list(opts.Before), list(opts.After), opts.Container, list(opts.Slide), opts.Size, flags)
	return key, ok
}
