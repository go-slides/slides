package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-slides/slides/diag"
)

const acme = "../../testdata/themes/acme"

// userDirs points the user's configuration directory into the test.
func userDirs(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
}

func packAcme(t *testing.T) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "acme.slidestheme")
	out, _, err := run(t, "theme", "pack", acme, "-o", file)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "acme 1.0.0") {
		t.Fatalf("pack printed %q", out)
	}
	return file
}

func TestThemePackVerifyInstallListRemove(t *testing.T) {
	userDirs(t)
	file := packAcme(t)

	out, _, err := run(t, "theme", "verify", file)
	if err != nil || !strings.Contains(out, "acme 1.0.0 ok") {
		t.Fatalf("verify: %v %q", err, out)
	}
	if out, _, _ := run(t, "theme", "list"); out != "" {
		t.Fatalf("a fresh store lists %q", out)
	}
	out, _, err = run(t, "theme", "install", file)
	if err != nil || !strings.Contains(out, "installed acme 1.0.0") {
		t.Fatalf("install: %v %q", err, out)
	}
	out, _, _ = run(t, "theme", "list")
	if !strings.Contains(out, "acme") || !strings.Contains(out, "MIT") {
		t.Fatalf("list: %q", out)
	}
	if _, _, err := run(t, "theme", "remove", "acme"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := run(t, "theme", "remove", "acme"); err == nil {
		t.Fatal("removing twice succeeded")
	}
	if _, _, err := run(t, "theme", "remove", "../../etc/passwd"); err == nil {
		t.Fatal("a path was taken for a theme name")
	}
}

func TestThemeVerifyRefusesAHostilePackage(t *testing.T) {
	userDirs(t)
	bad := filepath.Join(t.TempDir(), "bad.slidestheme")
	if err := os.WriteFile(bad, []byte("PK\x05\x06 not an archive"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, sub := range []string{"verify", "install"} {
		_, _, err := run(t, "theme", sub, bad)
		if diag.CodeOf(err) != "theme.archive" {
			t.Fatalf("%s: want theme.archive, got %v", sub, err)
		}
	}
	if out, _, _ := run(t, "theme", "list"); out != "" {
		t.Fatalf("a refused package was installed: %q", out)
	}
}

const deckTmpl = "---\nmarp: true\n%s---\n\n# Hello\n"

func writeDeck(t *testing.T, front string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "deck.md")
	if err := os.WriteFile(p, []byte(strings.Replace(deckTmpl, "%s", front, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDeckUsesAPackageFile(t *testing.T) {
	userDirs(t)
	file := packAcme(t)
	d := writeDeck(t, "kind: acme-title\n")
	out, _, err := run(t, "--theme", file, "-o", "-", d)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"data:font/ttf;base64,", "@font-face", `"Roboto Mono"`, "data:image/svg+xml;base64,"} {
		if !strings.Contains(out, want) {
			t.Errorf("the page lacks %q", want)
		}
	}
	// the package's kind is in force: acme-title sets class lead
	if !strings.Contains(out, `class="lead"`) {
		t.Errorf("the package's kind did not apply")
	}
	if strings.Contains(out, "assets/logo.svg") {
		t.Error("a package path reached the page")
	}
}

func TestDeckFindsAnInstalledPackageByName(t *testing.T) {
	userDirs(t)
	d := writeDeck(t, "theme: acme\n")
	_, stderr, err := run(t, "-o", "-", d)
	if err != nil || !strings.Contains(stderr, `theme "acme" is not known`) {
		t.Fatalf("before install: %v, stderr %q", err, stderr)
	}
	if _, _, err := run(t, "theme", "install", packAcme(t)); err != nil {
		t.Fatal(err)
	}
	out, stderr, err := run(t, "-o", "-", d)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stderr, "not known") || !strings.Contains(out, "data:font/ttf;base64,") {
		t.Fatalf("an installed theme was not found: stderr %q", stderr)
	}
	// the kinds come with the theme the front matter names
	out, _, err = run(t, "-o", "-", writeDeck(t, "theme: acme\nkind: acme-title\n"))
	if err != nil || !strings.Contains(out, `class="lead"`) {
		t.Fatalf("an installed package's kind from the front matter: %v", err)
	}
	// --theme name resolves the same way
	out, _, err = run(t, "--theme", "acme", "-o", "-", writeDeck(t, "kind: acme-title\n"))
	if err != nil || !strings.Contains(out, "data:font/ttf;base64,") || !strings.Contains(out, `class="lead"`) {
		t.Fatalf("--theme acme: %v", err)
	}
}

func TestBrokenInstalledPackageIsTold(t *testing.T) {
	userDirs(t)
	dir := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "go-slides", "themes")
	if home, err := os.UserConfigDir(); err == nil {
		dir = filepath.Join(home, "go-slides", "themes")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "acme.slidestheme"), []byte("garbage"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := run(t, "-o", "-", writeDeck(t, "theme: acme\n"))
	if diag.CodeOf(err) != "theme.archive" {
		t.Fatalf("a broken installed package: want theme.archive, got %v", err)
	}
	out, _, _ := run(t, "theme", "list")
	if !strings.Contains(out, "ERROR") {
		t.Fatalf("list hides the broken package: %q", out)
	}
}

func TestThemeSetTakesAPackage(t *testing.T) {
	userDirs(t)
	file := packAcme(t)
	out, _, err := run(t, "--theme-set", file, "themes")
	if err != nil || !strings.Contains(out, "acme") {
		t.Fatalf("themes: %v %q", err, out)
	}
}
