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

// lineNumberRe matches one line's number.
var lineNumberRe = regexp.MustCompile(`<span class="ln" aria-hidden="true">[^<]*</span>`)

// What a deck without the option renders is what it rendered before (the
// oracle tests pin it to Marp Core's): the numbers add one element a line
// and nothing else, so taking them out gives the plain block back.
func TestLineNumbersAddOnlyTheGutter(t *testing.T) {
	plain := render(t, "```go\nx := 1\ny := 2\n```\n")
	numbered := render(t, "```go {lineNumbers}\nx := 1\ny := 2\n```\n")
	if plain == numbered {
		t.Fatal("no numbers")
	}
	got := lineNumberRe.ReplaceAllString(numbered, "")
	got = strings.Replace(got, `<pre class="line-numbers">`, "<pre>", 1)
	if got != plain {
		t.Errorf("\n got %q\nwant %q", got, plain)
	}
}

func TestLineNumbersGolden(t *testing.T) {
	got := render(t, "```go {lineNumbers=8}\na\nb\n\nc\n```\n")
	want := "<pre class=\"line-numbers\"><code class=\"language-go\">" +
		"<span class=\"ln\" aria-hidden=\"true\"> 8</span>a\n" +
		"<span class=\"ln\" aria-hidden=\"true\"> 9</span>b\n" +
		"<span class=\"ln\" aria-hidden=\"true\">10</span>\n" +
		"<span class=\"ln\" aria-hidden=\"true\">11</span>c\n</code></pre>"
	if !strings.Contains(got, want) {
		t.Errorf("golden:\n%q\ndoes not contain\n%q", got, want)
	}
}

// Each line starts with its number, right-aligned by spaces to the widest
// one's digits (the code's font is monospace), never by a width written in
// the page (a fixed width clips in a font wider than the one it was computed
// for).
func TestLineNumbersGutterWidth(t *testing.T) {
	lines := func(n int) string { return strings.Repeat("x\n", n) }
	for _, c := range []struct {
		opt     string
		n       int
		numbers string
	}{
		{"{lineNumbers=8}", 5, " 8| 9|10|11|12"},
		{"{lineNumbers=98}", 3, " 98| 99|100"}, // crosses into 3 digits
		{"{lineNumbers=98}", 2, "98|99"},
		{"{lineNumbers=1}", 9, "1|2|3|4|5|6|7|8|9"},
		{"{lineNumbers=1}", 10, " 1| 2| 3| 4| 5| 6| 7| 8| 9|10"},
		{"{lineNumbers}", 3, "1|2|3"},
	} {
		got := render(t, "```go "+c.opt+"\n"+lines(c.n)+"```\n")
		var want strings.Builder
		for _, num := range strings.Split(c.numbers, "|") {
			want.WriteString(`<span class="ln" aria-hidden="true">` + num + "</span>x\n")
		}
		if !strings.Contains(got, want.String()) {
			t.Errorf("%s on %d lines: want %q in %q", c.opt, c.n, want.String(), got)
		}
		if strings.Contains(got, "width:") {
			t.Errorf("%s on %d lines: the page fixes a width: %q", c.opt, c.n, got)
		}
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
	if !has(1, `</span>two`) || has(1, `</span>three`) {
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
