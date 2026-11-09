package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, root, name, body string) string {
	t.Helper()
	p := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

const landingToml = `[params]
  version = "v0.18.2"

[[params.links]]
  label  = "v0.18.2"
  blurb  = "Released as v0.18.2: go install."

[[params.docs]]
  label  = "v0.18"
  blurb  = "Describing v0.18.2."

[[params.cards]]
  what = "Transitions. Since v0.18.0; checked in LibreOffice only."
  when = "v0.18.0"

[[params.cards]]
  what = "Recordings, same version as now v0.18.2 in prose."
  when = "v0.18.2"
`

// The landing's history is left alone: a card's "Since v0.18.0" and its when,
// even one equal to the version being left, are not the current version.
func TestLandingBumpChangesOnlyTheCurrentVersionFields(t *testing.T) {
	root := t.TempDir()
	write(t, root, "hugo.toml", landingToml)
	for _, l := range langs[1:] {
		write(t, root, "config/_default/params."+l+".toml", "  what = \"x\"\n")
	}
	fr := write(t, root, "config/_default/params.fr.toml", "  blurb = \"Publiée en v0.18.2.\"\n  what = \"Depuis la v0.18.0 ; v0.18.2.\"\n  when = \"v0.18.2\"\n")
	write(t, root, "README.md", "go-slides/slides is released as v0.18.2. See docs/0.18/self-hosting/.\nsince v0.18.2 the thing.\n")
	var out bytes.Buffer
	if err := run(&out, "landing", root, "v0.18.2", "v0.19.0"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "hugo.toml"))
	want := strings.NewReplacer(
		`version = "v0.18.2"`, `version = "v0.19.0"`,
		`label  = "v0.18.2"`, `label  = "v0.19.0"`,
		`blurb  = "Released as v0.18.2: go install."`, `blurb  = "Released as v0.19.0: go install."`,
		`label  = "v0.18"`, `label  = "v0.19"`,
		`blurb  = "Describing v0.18.2."`, `blurb  = "Describing v0.19.0."`,
	).Replace(landingToml)
	if string(got) != want {
		t.Errorf("hugo.toml:\n%s\nwant:\n%s", got, want)
	}
	for _, kept := range []string{"Since v0.18.0", `when = "v0.18.0"`, `when = "v0.18.2"`, "same version as now v0.18.2"} {
		if !strings.Contains(string(got), kept) {
			t.Errorf("history lost: %s", kept)
		}
	}
	g, _ := os.ReadFile(fr)
	if string(g) != "  blurb = \"Publiée en v0.19.0.\"\n  what = \"Depuis la v0.18.0 ; v0.18.2.\"\n  when = \"v0.18.2\"\n" {
		t.Errorf("params.fr.toml: %s", g)
	}
	r, _ := os.ReadFile(filepath.Join(root, "README.md"))
	if string(r) != "go-slides/slides is released as v0.19.0. See docs/0.19/self-hosting/.\nsince v0.18.2 the thing.\n" {
		t.Errorf("README: %s", r)
	}
}

func TestDocsBump(t *testing.T) {
	root := t.TempDir()
	for _, l := range langs {
		c := "content/" + l + "/"
		write(t, root, c+"_index.md", "v0.18.2 /docs/0.18/ x")
		write(t, root, c+"cli.md", "describes v0.18.2 and again v0.18.2")
		write(t, root, c+"getting-started.md", "v0.18.2")
		write(t, root, c+"playground.md", "v0.18.2 v0.18.2")
		write(t, root, c+"self-hosting.md", "go install x@v0.18.2 since v0.18.2")
	}
	write(t, root, "hugo.yaml", `version: "v0.18.2"`)
	write(t, root, "README.md", "v0.18.2 (`params.slides.version`) and are published under `0.18/`")
	if err := run(&bytes.Buffer{}, "docs", root, "v0.18.2", "v0.19.0"); err != nil {
		t.Fatal(err)
	}
	check := func(name, want string) {
		t.Helper()
		b, _ := os.ReadFile(filepath.Join(root, name))
		if string(b) != want {
			t.Errorf("%s = %q, want %q", name, b, want)
		}
	}
	check("content/fr/_index.md", "v0.19.0 /docs/0.19/ x")
	check("content/fr/cli.md", "describes v0.19.0 and again v0.18.2") // first match only
	check("content/fr/playground.md", "v0.19.0 v0.18.2")
	check("content/fr/self-hosting.md", "go install x@v0.19.0 since v0.18.2")
	check("hugo.yaml", `version: "v0.19.0"`)
	check("README.md", "v0.19.0 (`params.slides.version`) and are published under `0.19/`")
}

func TestUsageErrors(t *testing.T) {
	if err := run(&bytes.Buffer{}, "nope", t.TempDir(), "v0.1.0", "v0.2.0"); err == nil {
		t.Error("an unknown kind is accepted")
	}
	if err := run(&bytes.Buffer{}, "landing", t.TempDir(), "v0.1.0", "v0.2.0"); err == nil {
		t.Error("a missing file is accepted")
	}
	if minor("v1") != "1" {
		t.Error("minor of a bare major")
	}
}
