package deck

import (
	"html"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestCutCallouts(t *testing.T) {
	for _, c := range []struct {
		in, rest string
		nums     []int
	}{
		{"x := 1 // <1>", "x := 1", []int{1}},
		{"x = 1   # <2>", "x = 1", []int{2}},
		{"(def x 1) ;; <3>", "(def x 1)", []int{3}},
		{"select 1 -- <4>", "select 1", []int{4}},
		{"x = 1 % <5>", "x = 1", []int{5}},
		{"Dim x ' <6>", "Dim x", []int{6}},
		{"<title>T</title> <!--7-->", "<title>T</title>", []int{7}},
		{"<p>x</p> <!-- <8> -->", "<p>x</p>", []int{8}},
		{"a { color: red; } /* <9> */", "a { color: red; }", []int{9}},
		{"get '/hi' do # <2> <3>", "get '/hi' do", []int{2, 3}},
		{"x = 1 # the count <1>", "x = 1 # the count", []int{1}},
		{"# <1>", "", []int{1}},
		{"x = 1 // <12>  ", "x = 1", []int{12}},
		{"\tx = 1 // <1>", "\tx = 1", []int{1}},
	} {
		rest, nums, ok := cutCallouts(c.in)
		if !ok || rest != c.rest || !slices.Equal(nums, c.nums) {
			t.Errorf("cutCallouts(%q) = %q %v %v, want %q %v", c.in, rest, nums, ok, c.rest, c.nums)
		}
	}
	for _, in := range []string{
		"std::vector<int> v; <1>", // no comment
		"x = a<1>",                // not after white space
		"x = 1 // <0>",            // no zero
		"x = 1 // <01>",           // no leading zero
		"x = 1 // <100>",          // too large
		"x = 1 // <a>",
		"if a <1> b",       // not at the end
		"print(\"# <1>\")", // inside a string, not at the end
		"x <!--a-->",
		"x = 1 //<1>", // a comment glued to the marker is not ours
		"/*/",
		"<!-->",
		"",
	} {
		if rest, nums, ok := cutCallouts(in); ok {
			t.Errorf("cutCallouts(%q) = %q %v, want no callout", in, rest, nums)
		}
	}
}

func TestParseCallouts(t *testing.T) {
	c, ok := parseCallouts("a // <1>\nb\nc # <2> <1>\n")
	if !ok || c.code != "a\nb\nc\n" || c.max != 2 || len(c.lines) != 2 ||
		c.lines[0].line != 0 || c.lines[1].line != 2 || !slices.Equal(c.lines[1].nums, []int{2, 1}) {
		t.Fatalf("got %+v %v", c, ok)
	}
	if _, ok := parseCallouts("a\nb\n"); ok {
		t.Fatal("no markers, no callouts")
	}
}

// FuzzParseCallouts: whatever the code, the parse keeps every line (the same
// count, unmarked lines untouched, a marked line cut to a prefix of itself)
// and only reports numbers 1 to maxCallout.
func FuzzParseCallouts(f *testing.F) {
	for _, s := range []string{"x // <1>\ny\n", "a # <2> <3>\n<!--1-->", "/* <1> */\n", "x = 1 # c <4>", "<1>", " // <1> <!--2-->", "/*/"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, code string) {
		c, ok := parseCallouts(code)
		if !ok {
			return
		}
		in := strings.Split(code, "\n")
		out := strings.Split(c.code, "\n")
		if len(in) != len(out) {
			t.Fatalf("%q: %d lines became %d", code, len(in), len(out))
		}
		marked := map[int]bool{}
		for _, l := range c.lines {
			marked[l.line] = true
			if len(l.nums) == 0 {
				t.Fatalf("%q: line %d marked with no number", code, l.line)
			}
			for _, n := range l.nums {
				if n < 1 || n > maxCallout || n > c.max {
					t.Fatalf("%q: number %d", code, n)
				}
			}
		}
		for i := range in {
			if !marked[i] && in[i] != out[i] {
				t.Fatalf("%q: unmarked line %d changed: %q -> %q", code, i, in[i], out[i])
			}
			if marked[i] && !strings.HasPrefix(in[i], out[i]) {
				t.Fatalf("%q: marked line %d is not cut to a prefix: %q -> %q", code, i, in[i], out[i])
			}
		}
	})
}

const calloutDeck = "# Callouts\n\n```go\nfunc main() { // <1>\n\tx := load() // <2>\n\tfmt.Println(x)\n}\n```\n\n1. The entry point.\n2. Reads the input.\n"

var tagRe = regexp.MustCompile(`<[^>]*>`)

// codeText is what a reader copies from a rendered block: its text, tags
// and generated content left out.
func codeText(markup string) string {
	start := strings.Index(markup, "<pre")
	end := strings.Index(markup, "</pre>")
	return html.UnescapeString(tagRe.ReplaceAllString(markup[start:end], ""))
}

func renderOne(t *testing.T, src string, opts RenderOptions) string {
	t.Helper()
	d, err := Parse([]byte(src), Options{})
	if err != nil {
		t.Fatal(err)
	}
	return d.Render(opts)[0].HTML
}

// The code a reader copies is the code without its markers, highlighted or
// not, with line numbers or not.
func TestCalloutsCopyPlainCode(t *testing.T) {
	want := "func main() {\n\tx := load()\n\tfmt.Println(x)\n}\n"
	for _, src := range []string{calloutDeck, strings.Replace(calloutDeck, "```go", "```go {lineNumbers}", 1)} {
		markup := renderOne(t, src, RenderOptions{})
		got := codeText(markup)
		if strings.Contains(src, "lineNumbers") {
			// the numbers, unselectable
			got = codeText(lineNumberRe.ReplaceAllString(markup, ""))
		}
		if got != want {
			t.Errorf("copied %q, want %q\n%s", got, want, markup)
		}
		if strings.Count(markup, `class="co-disc"`) != 2 || !strings.Contains(markup, `<ol class="co-list">`) ||
			!strings.Contains(markup, `<li data-co="2" id="co1-1-2" tabindex="0">`) {
			t.Errorf("discs or notes missing:\n%s", markup)
		}
	}
}

// Without the list after it, or with too few items, a block keeps its
// markers as written.
func TestCalloutsNeedTheirNotes(t *testing.T) {
	for _, src := range []string{
		"```go\nx // <1>\n```\n",
		"```go\nx // <1>\ny // <2>\n```\n\n1. only one\n",
		"```go\nx // <1>\n```\n\n* not ordered\n",
		"```go\nx // <1>\n```\n\n2. starts at two\n",
		"```go\nx // <1>\n```\n\ntext\n\n1. not right after\n",
	} {
		if markup := renderOne(t, src, RenderOptions{}); !strings.Contains(markup, "// &lt;1&gt;") || strings.Contains(markup, "co-disc") {
			t.Errorf("%q: markers should stay as written:\n%s", src, markup)
		}
	}
}

func TestCalloutsGolden(t *testing.T) {
	got := renderOne(t, calloutDeck, RenderOptions{SourceLines: true, Frame: FramePlain})
	want := `<section id="1">
<h1 id="callouts" class="code-line" data-line="0">Callouts</h1>
<pre class="callouts"><code class="language-go code-line" data-line="2">func main() {<span class="co-disc" data-co="1" tabindex="0" aria-describedby="co1-1-1"></span>
	x := load()<span class="co-disc" data-co="2" tabindex="0" aria-describedby="co1-1-2"></span>
	fmt.Println(x)
}
</code></pre>
<ol class="co-list code-line" data-line="9">
<li data-co="1" id="co1-1-1" tabindex="0" class="code-line" data-line="9">The entry point.</li>
<li data-co="2" id="co1-1-2" tabindex="0" class="code-line" data-line="10">Reads the input.</li>
</ol>
</section>
`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// Each disc names its own note: the ids are unique across blocks and slides,
// every aria-describedby names one of them, and a slide rendered alone (the
// incremental render) writes the same ids as in the whole deck.
func TestCalloutIDs(t *testing.T) {
	block := "```go\na // <1>\nb // <2>\n```\n\n1. A.\n2. B.\n"
	src := "# One\n\n" + block + "\n```sh\nno callouts\n```\n\n" + block + "\n---\n\n# Two\n\n" + block
	d, err := Parse([]byte(src), Options{})
	if err != nil {
		t.Fatal(err)
	}
	slides := d.Render(RenderOptions{Frame: FramePlain})
	idRe := regexp.MustCompile(` id="(co[^"]*)"`)
	refRe := regexp.MustCompile(`aria-describedby="([^"]*)"`)
	ids := map[string]bool{}
	var all []string
	for _, s := range slides {
		for _, m := range idRe.FindAllStringSubmatch(s.HTML, -1) {
			if ids[m[1]] {
				t.Errorf("id %s twice", m[1])
			}
			ids[m[1]] = true
			all = append(all, m[1])
		}
	}
	if strings.Join(all, " ") != "co1-1-1 co1-1-2 co1-2-1 co1-2-2 co2-1-1 co2-1-2" {
		t.Errorf("ids %v", all)
	}
	for _, s := range slides {
		for _, m := range refRe.FindAllStringSubmatch(s.HTML, -1) {
			if !ids[m[1]] {
				t.Errorf("aria-describedby=%q names no note", m[1])
			}
		}
	}
	// The ids come from the slide's number and its own blocks only, so a
	// second render writes the same page.
	again := d.Render(RenderOptions{Frame: FramePlain})
	for i := range slides {
		if again[i].HTML != slides[i].HTML {
			t.Errorf("slide %d renders differently the second time", i+1)
		}
	}
}

func TestCalloutsCSS(t *testing.T) {
	d, err := Parse([]byte(calloutDeck), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if d.UsesCallouts() != 2 {
		t.Fatalf("UsesCallouts = %d", d.UsesCallouts())
	}
	css := CalloutsCSS(2)
	for _, w := range []string{`[data-co="2"]::before{content:"2"}`, "user-select:none",
		// a disc shows its note, a note its disc
		`section pre:has(.co-disc[data-co="2"]:is(:hover,:focus-visible))+ol.co-list>li[data-co="2"]`,
		`section pre:has(+ol.co-list>li[data-co="2"]:is(:hover,:focus-visible)) .co-disc[data-co="2"]`,
		// the only motion, and only for those who did not ask for less
		`@media (prefers-reduced-motion:no-preference){section ol.co-list>li{transition:`,
	} {
		if !strings.Contains(css, w) {
			t.Errorf("CSS lacks %s", w)
		}
	}
	if strings.Contains(css, `content:"3"`) {
		t.Error("CSS for numbers the deck does not use")
	}
	if plain, _ := Parse([]byte("```go\nx\n```\n"), Options{}); plain.UsesCallouts() != 0 {
		t.Error("a deck without callouts uses none")
	}
}
