package project

import (
	"testing"

	"github.com/go-slides/slides/diag"
	"github.com/go-slides/slides/vfs"
)

// Data files cannot include each other: a file is read once and what it holds
// is data, so a file that names itself, or another, is just a key. Only the
// configuration's own nesting of lists of paths recurses, and it is bounded.
func TestDataFilesDoNotIncludeEachOther(t *testing.T) {
	fsys := vfs.NewMap(map[string][]byte{
		"/w/a.yaml": []byte("data: [b.yaml]\nname: a\n"),
		"/w/b.yaml": []byte("data: [a.yaml]\nname: b\n"),
	})
	m, err := dataFrom(fsys, []any{"a.yaml", "b.yaml"}, "/w", nil)
	if err != nil {
		t.Fatal(err)
	}
	if m["name"] != "b" {
		t.Errorf("%v", m)
	}
}

func TestDataListNestingIsBounded(t *testing.T) {
	var v any = "a.yaml"
	for i := 0; i < 100; i++ {
		v = []any{v}
	}
	fsys := vfs.NewMap(map[string][]byte{"/w/a.yaml": []byte("x: 1\n")})
	_, err := dataFrom(fsys, v, "/w", nil)
	if diag.CodeOf(err) != "include.limit" {
		t.Fatalf("%v", err)
	}
	// a modest nesting is fine
	v = "a.yaml"
	for i := 0; i < 5; i++ {
		v = []any{v}
	}
	if m, err := dataFrom(fsys, v, "/w", nil); err != nil || m["x"] != 1 {
		t.Fatalf("%v, %v", m, err)
	}
}
