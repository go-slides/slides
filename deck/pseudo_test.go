package deck

import (
	"strings"
	"testing"
)

func TestEvalContent(t *testing.T) {
	attrs := map[string]string{"data-marpit-pagination": "3", "data-marpit-pagination-total": "9"}
	cases := map[string]string{
		`attr(data-marpit-pagination)`:                                          "3",
		`attr(data-marpit-pagination) " / " attr(data-marpit-pagination-total)`: "3 / 9",
		`"Page " attr( data-marpit-pagination ) '.'`:                            "Page 3.",
		`attr(data-missing)`:                                                    "",
		// The space after a hex escape belongs to the escape.
		`"\2014 " attr(data-marpit-pagination)`:  "—3",
		`"\2014  " attr(data-marpit-pagination)`: "— 3",
		`"a\"b"`:                                 `a"b`,
		`none`:                                   "",
		`attr(data-marpit-pagination string, "x")`: "3",
		`attr(unclosed`: "",
	}
	for in, want := range cases {
		if got := evalContent(in, attrs); got != want {
			t.Errorf("%s: got %q want %q", in, got, want)
		}
	}
}

func TestDataURIBackgroundKeepsItsSemicolon(t *testing.T) {
	d := parseDeck(t, "![bg](data:image/png;base64,AAAA)\n\nx\n", Options{})
	html := d.Render(RenderOptions{Frame: FramePlain})[0].HTML
	if !strings.Contains(html, `background-image:url(&quot;data:image/png;base64,AAAA&quot;);`) {
		t.Errorf("the background was dropped: %s", html)
	}
	for v, want := range map[string]bool{`a;b`: true, `{`: true, `url("a;b")`: false, `url(a;b)`: false, `'x}'`: false} {
		if got := breaksOut(v); got != want {
			t.Errorf("breaksOut(%q) = %v", v, got)
		}
	}
}

func TestPseudoAfterUsesTheThemeExpression(t *testing.T) {
	d := parseDeck(t, "---\npaginate: true\n---\na\n\n---\n\nb\n", Options{})
	rs := d.Render(RenderOptions{Frame: FrameBox, GeneratedContent: true, AfterContent: `attr(data-marpit-pagination) "/" attr(data-marpit-pagination-total)`})
	if want := `<span data-slides-pseudo="after">2/2</span>`; !strings.Contains(rs[1].HTML, want) {
		t.Errorf("missing %s in %s", want, rs[1].HTML)
	}
	rs = d.Render(RenderOptions{Frame: FrameBox, GeneratedContent: true})
	if want := `<span data-slides-pseudo="after">1</span>`; !strings.Contains(rs[0].HTML, want) {
		t.Errorf("without an expression the page number shows: %s", rs[0].HTML)
	}
}
