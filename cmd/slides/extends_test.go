package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-slides/slides/diag"
)

// pack makes a package directory name (extending base when it is not empty,
// with a rule whose colour says whose it is) and packs it into dir.
func packTheme(t *testing.T, dir, name, base, colour string) string {
	t.Helper()
	src := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	hcl := fmt.Sprintf("theme {\n name = %q\n version = \"1.0.0\"\n license = \"MIT\"\n", name)
	if base != "" {
		hcl += fmt.Sprintf(" extends = %q\n", base)
	}
	hcl += "}\ncss \"t.css\" {}\n"
	if err := os.WriteFile(filepath.Join(src, "theme.hcl"), []byte(hcl), 0o644); err != nil {
		t.Fatal(err)
	}
	css := fmt.Sprintf("section h6{color:#%s}\n", colour)
	if err := os.WriteFile(filepath.Join(src, "t.css"), []byte(css), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, name+".gsta.xz")
	if _, _, err := run(t, "theme", "pack", src, "-o", out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestExtendsASiblingPackage(t *testing.T) {
	userDirs(t)
	dir := t.TempDir()
	packTheme(t, dir, "base-demo", "", "abcdef")
	child := packTheme(t, dir, "child-demo", "base-demo", "fedcba")
	out, _, err := run(t, "--theme", child, "-o", "-", writeDeck(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "abcdef") || !strings.Contains(out, "fedcba") {
		t.Fatalf("the base and the child are not both in the page (base %v, child %v)",
			strings.Contains(out, "abcdef"), strings.Contains(out, "fedcba"))
	}
}

func TestExtendsOrderBuiltInInstalledSibling(t *testing.T) {
	userDirs(t)
	dir := t.TempDir()
	child := packTheme(t, dir, "child-demo", "base-demo", "fedcba")
	// the sibling base, and an installed one of the same name: installed wins
	packTheme(t, dir, "base-demo", "", "111111")
	other := t.TempDir()
	installed := packTheme(t, other, "base-demo", "", "222222")
	if _, _, err := run(t, "theme", "install", installed); err != nil {
		t.Fatal(err)
	}
	out, _, err := run(t, "--theme", child, "-o", "-", writeDeck(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "222222") || strings.Contains(out, "111111") {
		t.Fatalf("the installed base must win over the sibling (installed %v, sibling %v)",
			strings.Contains(out, "222222"), strings.Contains(out, "111111"))
	}
	// built in beats both: a package may extend gaia
	gaia := packTheme(t, dir, "gaia-kid", "gaia", "333333")
	if _, _, err := run(t, "--theme", gaia, "-o", "-", writeDeck(t, "")); err != nil {
		t.Fatalf("extends a built-in theme: %v", err)
	}
}

func TestExtendsMissingNamesThePlacesSearched(t *testing.T) {
	userDirs(t)
	dir := t.TempDir()
	child := packTheme(t, dir, "child-demo", "base-demo", "fedcba")
	_, _, err := run(t, "--theme", child, "-o", "-", writeDeck(t, ""))
	if diag.CodeOf(err) != "theme.unknown" {
		t.Fatalf("want theme.unknown, got %v", err)
	}
	for _, want := range []string{"not built in", "not installed", filepath.Join(dir, "base-demo.gsta.xz"), "--theme-set"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error lacks %q: %v", want, err)
		}
	}
}

func TestExtendsCycleAndLength(t *testing.T) {
	userDirs(t)
	dir := t.TempDir()
	a := packTheme(t, dir, "aa", "bb", "aaaaaa")
	packTheme(t, dir, "bb", "aa", "bbbbbb")
	_, _, err := run(t, "--theme", a, "-o", "-", writeDeck(t, ""))
	if diag.CodeOf(err) != "theme.meta" || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("a cycle of siblings: %v", err)
	}
	// a chain of 12
	long := t.TempDir()
	var first string
	for i := 0; i < 12; i++ {
		base := ""
		if i < 11 {
			base = fmt.Sprintf("link-%d", i+1)
		}
		p := packTheme(t, long, fmt.Sprintf("link-%d", i), base, "010101")
		if i == 0 {
			first = p
		}
	}
	_, _, err = run(t, "--theme", first, "-o", "-", writeDeck(t, ""))
	if diag.CodeOf(err) != "theme.meta" || !strings.Contains(err.Error(), "longer than") {
		t.Fatalf("a long chain: %v", err)
	}
	// a cycle among installed packages
	other := t.TempDir()
	ia := packTheme(t, other, "ia", "ib", "aaaaaa")
	ib := packTheme(t, other, "ib", "ia", "bbbbbb")
	for _, f := range []string{ia, ib} {
		if _, _, err := run(t, "theme", "install", f); err != nil {
			t.Fatal(err)
		}
	}
	_, _, err = run(t, "-o", "-", writeDeck(t, "theme: ia\n"))
	if diag.CodeOf(err) != "theme.meta" || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("a cycle of installed packages: %v", err)
	}
}
