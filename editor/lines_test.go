package editor

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/go-slides/slides/deck"
	"github.com/go-slides/slides/project"
)

// frameLines returns the data-line of each slide frame of a preview.
var frameRE = regexp.MustCompile(`<svg data-marpit-svg="" viewBox="[^"]*" class="code-line" data-line="(\d+)">`)

func frameLines(t *testing.T, html string) []int {
	t.Helper()
	var out []int
	for _, m := range frameRE.FindAllStringSubmatch(html, -1) {
		n, _ := strconv.Atoi(m[1])
		out = append(out, n)
	}
	return out
}

// blockLine returns the data-line of the element whose text follows it.
func blockLine(t *testing.T, html, text string) int {
	t.Helper()
	re := regexp.MustCompile(`class="(?:[^"]* )?code-line" data-line="(\d+)"[^>]*>(?:<[^>]+>)*` + regexp.QuoteMeta(text))
	m := re.FindStringSubmatch(html)
	if m == nil {
		t.Fatalf("no line on %q in %s", text, html)
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

func TestSourceLinesThroughTemplates(t *testing.T) {
	src := strings.Join([]string{
		"---", // 0
		"slides: true",
		"kinds: k.yaml",
		"---",
		"",
		"# First", // 5
		"",
		"{{ include \"intro.md\" }}", // 7
		"",
		"Para after include", // 9
		"",
		"---", // 11
		"",
		"<!-- _kind: hero -->", // 13
		"## Kind slide",        // 14
		"",
		"{{ range seq 2 }}", // 16
		"---",
		"",
		"### Generated {{ . }}", // 19
		"{{ end }}",
		"---", // 21
		"",
		"```go", // 23
		"x := 1",
		"```",
		"",
		"- item one", // 27
		"- item two",
	}, "\n")
	files := map[string]string{
		path("intro.md"): "Line A\n\nLine B\n\nLine C",
		path("k.yaml"):   "hero:\n  class: lead\n",
	}
	r := Render(req(src, files))
	if len(r.Diagnostics) != 0 {
		t.Fatalf("%+v", r.Diagnostics)
	}
	if got, want := frameLines(t, r.HTML), []int{4, 12, 18, 18, 22}; !slices.Equal(got, want) {
		t.Errorf("slide lines %v, want %v", got, want)
	}
	for text, want := range map[string]int{
		"First": 5, "Line A": 7, "Line C": 7, "Para after include": 9,
		"Kind slide": 14, "Generated 1": 19, "Generated 2": 19,
		"item one": 27, "item two": 28,
	} {
		if l := blockLine(t, r.HTML, text); l != want {
			t.Errorf("%q on line %d, want %d", text, l, want)
		}
	}
	// A fenced block's line is on its <code>, with its language class.
	if !strings.Contains(r.HTML, `<pre><code class="language-go code-line" data-line="23">`) {
		t.Errorf("code block line missing")
	}
	if !strings.Contains(r.HTML, `<ul class="code-line" data-line="27">`) {
		t.Errorf("list line missing")
	}
}

func TestSourceLinesHeadingDivider(t *testing.T) {
	src := "---\nslides: true\nheadingDivider: 2\n---\n\n# Title\n\ntext\n\n## Part A\n\nmore\n\n## Part B\n"
	r := Render(req(src, nil))
	if got, want := frameLines(t, r.HTML), []int{4, 9, 13}; !slices.Equal(got, want) {
		t.Errorf("slide lines %v, want %v", got, want)
	}
	if l := blockLine(t, r.HTML, "Part B"); l != 13 {
		t.Errorf("Part B on line %d", l)
	}
}

func TestSourceLinesHCLFrontMatterAndNoTemplate(t *testing.T) {
	src := "---hcl\nslides = true\ntemplate = \"false\"\n---\n\n# A {{ x }}\n\n---\n\n# B\n"
	r := Render(req(src, nil))
	if got, want := frameLines(t, r.HTML), []int{4, 8}; !slices.Equal(got, want) {
		t.Errorf("slide lines %v, want %v (%+v)", got, want, r.Diagnostics)
	}
}

// TestNoSourceLinesOutsideTheEditor: the command's output is unchanged.
func TestNoSourceLinesOutsideTheEditor(t *testing.T) {
	cfg, err := project.Load(path("deck.md"), project.Options{FS: req("", nil).fs()})
	if err != nil {
		t.Fatal(err)
	}
	d, err := deck.Parse([]byte("# A\n\n---\n\n# B\n"), cfg.Parse)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range d.Render(deck.RenderOptions{}) {
		if strings.Contains(s.HTML, "code-line") || strings.Contains(s.HTML, "data-line") {
			t.Fatalf("source lines in %s", s.HTML)
		}
	}
	if d.Slides[1].Line != 3 { // the line after "---"
		t.Fatalf("slide 2 starts on line %d", d.Slides[1].Line)
	}
}
