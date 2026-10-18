package slides

import (
	"strings"
	"testing"
)

func TestHighlight(t *testing.T) {
	out, ok := Highlight("// c\nfunc main() { return \"<x>\" }\n", "Go")
	if !ok {
		t.Fatal("go is known, whatever its case")
	}
	for _, want := range []string{`<span class="hljs-comment">// c</span>`, `<span class="hljs-keyword">func</span>`, `&lt;x&gt;`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s in %s", want, out)
		}
	}
	for _, lang := range []string{"", "nosuchlang"} {
		if _, ok := Highlight("x", lang); ok {
			t.Errorf("%q must fall back to plain text", lang)
		}
	}
	page, _, err := HTML([]byte("```go\nfunc f() {}\n```\n\n```nosuchlang\n<b>\n```\n"), Config{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(page, `<pre><code class="language-go"><span class="hljs-keyword">func</span>`) ||
		!strings.Contains(page, `<pre><code class="language-nosuchlang">&lt;b&gt;`) {
		t.Error("code blocks: highlighted when the language is known, escaped text otherwise")
	}
}
