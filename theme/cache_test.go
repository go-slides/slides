package theme

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

const cachedThemeCSS = `/* @theme cached */
@import "default";
section { color: %s; }`

// A theme cached by its CSS must give way when the CSS changes: an editor
// re-reading an edited theme file sees the edit in the next sheet.
func TestCachedThemeInvalidatedWhenCSSChanges(t *testing.T) {
	sheet := func(color string) string {
		s := NewSet()
		th, err := s.Add(fmt.Sprintf(cachedThemeCSS, color))
		if err != nil {
			t.Fatal(err)
		}
		return s.Pack(th, PackOptions{}).CSS
	}
	first := sheet("rgb(1,2,3)")
	if !strings.Contains(first, "rgb(1,2,3)") {
		t.Fatalf("first sheet lacks the theme's color:\n%s", first)
	}
	again := sheet("rgb(1,2,3)")
	if again != first {
		t.Fatal("the same CSS packed twice differs")
	}
	edited := sheet("rgb(4,5,6)")
	if !strings.Contains(edited, "rgb(4,5,6)") || strings.Contains(edited, "rgb(1,2,3)") {
		t.Fatalf("the edited theme was not picked up:\n%s", edited)
	}
}

// The same CSS parsed twice is one shared theme; different CSS is not.
func TestThemeCacheSharesParsedThemes(t *testing.T) {
	a, err := parseCached(fmt.Sprintf(cachedThemeCSS, "red"))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := parseCached(fmt.Sprintf(cachedThemeCSS, "red"))
	c, _ := parseCached(fmt.Sprintf(cachedThemeCSS, "blue"))
	if a != b || a == c || a.id == 0 || a.id == c.id {
		t.Fatalf("a=%p b=%p c=%p ids %d %d", a, b, c, a.id, c.id)
	}
	if _, err := parseCached("section{}"); err == nil {
		t.Fatal("a theme without @theme must still be refused")
	}
}

// A cached sheet must be the sheet Pack would compute, for every option.
func TestPackCacheMatchesUncached(t *testing.T) {
	s := NewSet()
	for _, opts := range []PackOptions{
		{},
		{Printable: true, ContainerQuery: true, InlineSVG: true, Slide: []string{"svg", "foreignObject", "section"}},
		{Size: "4:3", GeneratedContent: true, Container: "div.x", Before: []string{"p{a:b}"}, After: []string{"h1{c:d}"}},
		{Slide: []string{}},
	} {
		for _, name := range []string{"default", "gaia", "uncover"} {
			th := s.Get(name)
			want := s.pack(th, opts)
			for range 2 {
				if got := s.Pack(th, opts); got != want {
					t.Fatalf("%s %+v: cached sheet differs", name, opts)
				}
			}
		}
	}
}

// A deck whose style changes on every keystroke must not grow the cache.
func TestPackCacheIsBounded(t *testing.T) {
	s := NewSet()
	for i := range 3 * packCacheSize {
		s.Pack(nil, PackOptions{After: []string{fmt.Sprintf("p{width:%dpx}", i)}})
	}
	if n := packCache.Len(); n > packCacheSize {
		t.Fatalf("pack cache holds %d sheets, bound %d", n, packCacheSize)
	}
	for i := range 3 * themeCacheSize {
		if _, err := parseCached(fmt.Sprintf(cachedThemeCSS, fmt.Sprintf("#%06x", i))); err != nil {
			t.Fatal(err)
		}
	}
	if n := themeCache.Len(); n > themeCacheSize {
		t.Fatalf("theme cache holds %d themes, bound %d", n, themeCacheSize)
	}
}

// A theme not made by parse has no number and is never cached: two such
// themes with different CSS must not share a sheet.
func TestPackUnnumberedThemeNotCached(t *testing.T) {
	s := NewSet()
	a := &Theme{Name: "x", CSS: "section{color:rgb(7,7,7)}"}
	b := &Theme{Name: "x", CSS: "section{color:rgb(8,8,8)}"}
	if _, ok := s.packKey(a, PackOptions{}); ok {
		t.Fatal("an unnumbered theme must not be cached")
	}
	if !strings.Contains(s.Pack(a, PackOptions{}).CSS, "rgb(7,7,7)") || !strings.Contains(s.Pack(b, PackOptions{}).CSS, "rgb(8,8,8)") {
		t.Fatal("unnumbered themes shared a sheet")
	}
}

func TestPackCacheConcurrent(t *testing.T) {
	s := NewSet()
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			for j := range 20 {
				opts := PackOptions{After: []string{fmt.Sprintf("p{width:%dpx}", (i+j)%5)}}
				if got, want := s.Pack(nil, opts), s.pack(s.Resolve(""), opts); got != want {
					t.Errorf("concurrent sheet differs")
					return
				}
			}
		})
	}
	wg.Wait()
}
