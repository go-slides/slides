package deck

import (
	"regexp"
	"strings"
	"testing"
)

func TestParseFenceOptions(t *testing.T) {
	yes, no := true, false
	for _, c := range []struct {
		info    string
		numbers *bool
		start   int
	}{
		{"go", nil, 0},
		{"go {lineNumbers}", &yes, 0},
		{"go {lineNumbers=10}", &yes, 10},
		{"go {lineNumbers:10}", &yes, 10},
		{"go {lineNumbers=false}", &no, 0},
		{"go {lines:true}", &yes, 0},
		{"go {lineNumbers,startLine=5}", &yes, 5},
		{"go {startLine=5}", nil, 5},
		{`go {.numberLines startFrom="10"}`, &yes, 10},
		{"go {foo=1,keys=[1,2]}", nil, 0},
		{"go {lineNumbers=0}", nil, 0},
		{"go {lineNumbers=x}", nil, 0},
		{"go lineNumbers", nil, 0}, // braces are the syntax
		{"go {lineNumbers", nil, 0},
		{"", nil, 0},
	} {
		got := parseFenceOptions(c.info)
		if (got.numbers == nil) != (c.numbers == nil) || got.numbers != nil && *got.numbers != *c.numbers || got.start != c.start {
			t.Errorf("parseFenceOptions(%q) = %+v, want numbers %v start %d", c.info, got, c.numbers, c.start)
		}
	}
}

func FuzzParseFenceOptions(f *testing.F) {
	f.Add("go {lineNumbers=10,startLine=2}")
	f.Add("{{{{")
	f.Add("x {lines:99999999999999999999}")
	f.Fuzz(func(t *testing.T, info string) {
		o := parseFenceOptions(info)
		if o.start < 0 || o.start > 1_000_000 {
			t.Fatalf("start %d", o.start)
		}
	})
}

func render(t *testing.T, md string) string {
	t.Helper()
	d, err := Parse([]byte(md), Options{})
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, s := range d.Render(RenderOptions{Frame: FramePlain}) {
		b.WriteString(s.HTML)
	}
	return b.String()
}

// What a deck without the option renders is what it rendered before (the
// oracle tests pin it to Marp Core's): the numbers add a gutter and a wrapper
// and nothing else, so taking them out gives the plain block back.
func TestLineNumbersAddOnlyTheGutter(t *testing.T) {
	plain := render(t, "```go\nx := 1\ny := 2\n```\n")
	numbered := render(t, "```go {lineNumbers}\nx := 1\ny := 2\n```\n")
	if plain == numbered {
		t.Fatal("no numbers")
	}
	strip := regexp.MustCompile(`<span class="ln-gutter"[^>]*>[^<]*</span><span class="ln-code">`)
	got := strip.ReplaceAllString(numbered, "")
	got = strings.Replace(got, `<pre class="line-numbers">`, "<pre>", 1)
	got = strings.Replace(got, "</span></code>", "</code>", 1)
	if got != plain {
		t.Errorf("\n got %q\nwant %q", got, plain)
	}
}

func TestLineNumbersGolden(t *testing.T) {
	got := render(t, "```go {lineNumbers=8}\na\nb\n\nc\n```\n")
	want := "<pre class=\"line-numbers\"><code class=\"language-go\"><span class=\"ln-gutter\" aria-hidden=\"true\">8\n9\n10\n11\n</span><span class=\"ln-code\">a\nb\n\nc\n</span></code></pre>"
	if !strings.Contains(got, want) {
		t.Errorf("golden:\n%q\ndoes not contain\n%q", got, want)
	}
	// numbers are not text of the code: the gutter is hidden from readers
	if !strings.Contains(got, `aria-hidden="true"`) {
		t.Error("the gutter is for the eye")
	}
}

func TestLineNumbersDirective(t *testing.T) {
	md := "```sh\none\n```\n\n---\n\n<!-- lineNumbers: true -->\n\n```sh\ntwo\n```\n\n```sh {lineNumbers=false}\nthree\n```\n\n---\n\n<!-- _lineNumbers: false -->\n\n```sh\nfour\n```\n"
	d, err := Parse([]byte(md), Options{})
	if err != nil {
		t.Fatal(err)
	}
	slides := d.Render(RenderOptions{Frame: FramePlain})
	has := func(i int, text string) bool { return strings.Contains(slides[i].HTML, text) }
	if has(0, "line-numbers") {
		t.Error("slide 1 has none")
	}
	if !has(1, `<span class="ln-code">two`) || has(1, `ln-code">three`) {
		t.Errorf("slide 2: the directive on, the block off:\n%s", slides[1].HTML)
	}
	if !d.UsesLineNumbers() {
		t.Error("UsesLineNumbers")
	}
	// the directive is not a speaker note, and not a data attribute
	if len(d.Slides[1].Notes) != 0 || strings.Contains(slides[1].HTML, "data-line-numbers") {
		t.Errorf("notes %v", d.Slides[1].Notes)
	}
	if (&Deck{}).UsesLineNumbers() {
		t.Error("an empty deck uses none")
	}
	d2, _ := Parse([]byte("```go\nx\n```\n"), Options{})
	if d2.UsesLineNumbers() {
		t.Error("a deck without the option does not use it")
	}
}

func TestCodeLines(t *testing.T) {
	for in, want := range map[string]int{"": 0, "a": 1, "a\n": 1, "a\nb\n": 2, "a\n\n": 2, "\n": 1} {
		if codeLines(in) != want {
			t.Errorf("codeLines(%q) = %d, want %d", in, codeLines(in), want)
		}
	}
}
