package theme

import (
	"regexp"
	"testing"
)

// The guards in front of the selector regexps must not change a result:
// each is compared with the plain regexp it replaces or short-circuits.
var sectionStartRe = regexp.MustCompile(`^section(?:[^\w-]|$)`)

func checkGuards(t *testing.T, sel string) {
	t.Helper()
	if got, want := startsWithSection(sel), sectionStartRe.MatchString(sel); got != want {
		t.Fatalf("startsWithSection(%q) = %v, regexp says %v", sel, got, want)
	}
	if got, want := replaceRoot(sel), rootReplaceRe.ReplaceAllString(sel, "${1}section"+rootPseudo); got != want {
		t.Fatalf("replaceRoot(%q) = %q, want %q", sel, got, want)
	}
}

func TestSelectorGuards(t *testing.T) {
	for _, sel := range []string{
		"", "section", "section h1", "section-x", "sections", "section_a", "section9", "section:first",
		"sectioné", ":root", "section:root", "a > :root", "x:roots", "(:root)", "h1",
	} {
		checkGuards(t, sel)
	}
}

func FuzzSelectorGuards(f *testing.F) {
	for _, s := range []string{"section", "section>p", ":root a", "a+section:root"} {
		f.Add(s)
	}
	f.Fuzz(checkGuards)
}
