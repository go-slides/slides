package vfs

import (
	"errors"
	"io/fs"
	"path/filepath"
	"slices"
	"testing"
)

func TestMap(t *testing.T) {
	root := filepath.FromSlash("/w")
	m := NewMap(map[string][]byte{
		filepath.Join(root, "deck.md"):          []byte("# hi"),
		filepath.Join(root, "themes", "a.css"):  []byte("a"),
		filepath.Join(root, "themes", "x", "b"): []byte("b"),
	})
	m.Dir(filepath.Join(root, "empty"))

	b, err := m.ReadFile(filepath.Join(root, "sub", "..", "deck.md"))
	if err != nil || string(b) != "# hi" {
		t.Fatalf("ReadFile = %q, %v", b, err)
	}
	b[0] = 'X'
	if again, _ := m.ReadFile(filepath.Join(root, "deck.md")); string(again) != "# hi" {
		t.Fatalf("ReadFile shares its buffer: %q", again)
	}

	if st, err := m.Stat(filepath.Join(root, "themes")); err != nil || !st.IsDir() {
		t.Fatalf("Stat(themes) = %v, %v", st, err)
	}
	if st, err := m.Stat(filepath.Join(root, "deck.md")); err != nil || st.IsDir() || st.Size() != 4 || !st.Mode().IsRegular() {
		t.Fatalf("Stat(deck.md) = %v, %v", st, err)
	}
	if _, err := m.ReadFile(filepath.Join(root, "themes")); err == nil {
		t.Fatal("reading a directory succeeded")
	}

	entries, err := m.ReadDir(filepath.Join(root, "themes"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if !slices.Equal(names, []string{"a.css", "x"}) || !entries[1].IsDir() {
		t.Fatalf("ReadDir = %v", names)
	}
	if e, err := m.ReadDir(filepath.Join(root, "empty")); err != nil || len(e) != 0 {
		t.Fatalf("ReadDir(empty) = %v, %v", e, err)
	}

	_, err = m.ReadFile(filepath.Join(root, "kinds.yaml"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing file: %v", err)
	}
	_, _ = m.Stat(filepath.Join(root, "slides.hcl"))
	want := []string{filepath.Join(root, "kinds.yaml"), filepath.Join(root, "slides.hcl")}
	if got := m.Missing(); !slices.Equal(got, want) {
		t.Fatalf("Missing = %v, want %v", got, want)
	}
}

func TestOr(t *testing.T) {
	if _, ok := Or(nil).(OS); !ok {
		t.Fatal("Or(nil) is not the disk")
	}
	m := NewMap(nil)
	if Or(m) != FS(m) {
		t.Fatal("Or replaced a file system")
	}
	if _, err := (OS{}).Stat("."); err != nil {
		t.Fatal(err)
	}
	if _, err := (OS{}).ReadDir("."); err != nil {
		t.Fatal(err)
	}
	if _, err := (OS{}).ReadFile("vfs.go"); err != nil {
		t.Fatal(err)
	}
}
