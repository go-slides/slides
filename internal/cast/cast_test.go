package cast

import (
	"encoding/json"
	"errors"
	"fmt"
	"runtime/debug"
	"strings"
	"testing"
)

const v2 = `{"version": 2, "width": 20, "height": 3, "title": "demo", "idle_time_limit": 2}
[0.5, "o", "hello \u001b[1;31mred\u001b[0m\r\n"]
[0.9, "i", "x"]
[1.0, "m", "marker"]
[10.0, "o", "\u001b[7mrev\u001b[0m <b>&"]
[10.5, "x", "0"]
`

const v3 = `{"version": 3, "term": {"cols": 20, "rows": 3, "theme": {"fg": "#ffffff", "bg": "#000000", "palette": "#000000:#111111:#222222:#333333:#444444:#555555:#666666:#777777"}}}
# a comment
[0.5, "o", "hello"]
[0.5, "o", " world"]
[0.1, "r", "30x4"]
[0.1, "x", "0"]
`

func code(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func TestParseV2(t *testing.T) {
	c, err := Parse([]byte(v2), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if c.Version != 2 || c.Cols != 20 || c.Rows != 3 || c.Title != "demo" || len(c.Events) != 2 {
		t.Fatalf("%+v", c)
	}
	// the idle time limit caps the 9.1 s gap at 2 s
	if c.Events[1].T != 2.5 || c.Duration != 2.5 {
		t.Errorf("times %v, duration %v", c.Events[1].T, c.Duration)
	}
	s, err := c.ScreenAt(-1, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	h := s.HTML()
	for _, want := range []string{"hello ", "red", "&lt;b&gt;&amp;", "color:#cd0000;font-weight:bold"} {
		if !strings.Contains(h, want) {
			t.Errorf("html lacks %q: %s", want, h)
		}
	}
	// the inverse run shows the theme's background as its text
	if !strings.Contains(h, "color:#1e1e1e;background-color:#d4d4d4") {
		t.Errorf("inverse not resolved: %s", h)
	}
	if strings.ContainsRune(h, 0x1b) {
		t.Error("an escape reached the markup")
	}
	// at 1 s only the first event has played
	s, _ = c.ScreenAt(1, Limits{})
	if strings.Contains(s.HTML(), "rev") || !strings.Contains(s.HTML(), "hello") {
		t.Errorf("screen at 1 s: %s", s.HTML())
	}
}

func TestParseV3(t *testing.T) {
	c, err := Parse([]byte(v3), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if c.Cols != 20 || len(c.Events) != 3 || c.Events[1].T != 1.0 || c.Duration != 1.1 || c.Theme.BG.Hex() != "#000000" || c.Theme.Palette[9].Hex() != "#111111" {
		t.Fatalf("%+v", c)
	}
	s, _ := c.ScreenAt(-1, Limits{})
	if s.Cols != 30 || s.Rows != 4 || !strings.Contains(s.HTML(), "hello world") {
		t.Errorf("%dx%d %s", s.Cols, s.Rows, s.HTML())
	}
}

func TestRefusals(t *testing.T) {
	big := func(n int) string { return strings.Repeat("a", n) }
	cases := []struct {
		name, in, code string
		lim            Limits
	}{
		{"empty", "", CodeSyntax, Limits{}},
		{"not json", "hello\n", CodeSyntax, Limits{}},
		{"v1", `{"version":1,"width":2,"height":2,"stdout":[]}`, CodeVersion, Limits{}},
		{"v9", `{"version":9}`, CodeVersion, Limits{}},
		{"no size", `{"version":2}`, CodeSyntax, Limits{}},
		{"v3 no term", `{"version":3}`, CodeSyntax, Limits{}},
		{"bad theme", `{"version":2,"width":2,"height":2,"theme":{"fg":"red","bg":"#000000","palette":"#000000"}}`, CodeSyntax, Limits{}},
		{"bad event", "{\"version\":2,\"width\":2,\"height\":2}\n[1,\"o\"]\n", CodeSyntax, Limits{}},
		{"event data not a string", "{\"version\":2,\"width\":2,\"height\":2}\n[1,\"o\",5]\n", CodeSyntax, Limits{}},
		{"time not a number", "{\"version\":2,\"width\":2,\"height\":2}\n[\"1\",\"o\",\"a\"]\n", CodeSyntax, Limits{}},
		{"time overflow", "{\"version\":2,\"width\":2,\"height\":2}\n[1e999,\"o\",\"a\"]\n", CodeSyntax, Limits{}},
		{"raw control in string", "{\"version\":2,\"width\":2,\"height\":2}\n[1,\"o\",\"a\x01\"]\n", CodeSyntax, Limits{}},
		{"bad escape", "{\"version\":2,\"width\":2,\"height\":2}\n[1,\"o\",\"\\q\"]\n", CodeSyntax, Limits{}},
		{"bad resize", "{\"version\":2,\"width\":2,\"height\":2}\n[1,\"r\",\"big\"]\n", CodeSyntax, Limits{}},
		{"negative interval", "{\"version\":3,\"term\":{\"cols\":2,\"rows\":2}}\n[-1,\"o\",\"a\"]\n", CodeSyntax, Limits{}},
		{"file too big", `{"version":2,"width":2,"height":2}` + "\n" + big(100), CodeLimit, Limits{MaxBytes: 50}},
		{"line too long", `{"version":2,"width":2,"height":2}` + "\n[1,\"o\",\"" + big(100) + "\"]\n", CodeLimit, Limits{MaxLine: 50}},
		{"terminal too big", `{"version":2,"width":5000,"height":2}`, CodeLimit, Limits{}},
		{"too many events", `{"version":2,"width":2,"height":2}` + strings.Repeat("\n[1,\"o\",\"a\"]", 10), CodeLimit, Limits{MaxEvents: 5}},
		{"too long", "{\"version\":2,\"width\":2,\"height\":2}\n[1e9,\"o\",\"a\"]\n", CodeLimit, Limits{}},
		{"resize too big", "{\"version\":2,\"width\":2,\"height\":2}\n[1,\"r\",\"9999x2\"]\n", CodeLimit, Limits{}},
	}
	for _, c := range cases {
		_, err := Parse([]byte(c.in), c.lim)
		if code(err) != c.code {
			t.Errorf("%s: got %v (code %q), want %s", c.name, err, code(err), c.code)
		}
	}
}

func TestUnknownCodesAreSkipped(t *testing.T) {
	c, err := Parse([]byte("{\"version\":2,\"width\":4,\"height\":2}\n[1,\"z\",{\"a\":[1]}]\n[2,\"zz\",\"x\"]\n[3,\"o\",\"ok\"]\n"), Limits{})
	if err != nil || len(c.Events) != 1 {
		t.Fatalf("%v %+v", err, c)
	}
	// but a skipped line must still be JSON
	if _, err := Parse([]byte("{\"version\":2,\"width\":4,\"height\":2}\n[1,\"z\",{\n"), Limits{}); code(err) != CodeSyntax {
		t.Errorf("got %v", err)
	}
}

func TestEscapes(t *testing.T) {
	c, err := Parse([]byte("{\"version\":2,\"width\":40,\"height\":2}\n[1,\"o\",\"\\ud83d\\ude00 \\u00e9 \\\"q\\\" \\/ \\ud800 \\udc00x\"]\n"), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Events[0].Data; got != "😀 é \"q\" / \ufffd \ufffdx" {
		t.Errorf("got %q", got)
	}
}

func TestReplayFrames(t *testing.T) {
	c, _ := Parse([]byte(v3), Limits{})
	rec, err := c.Replay(0, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Cols != 20 || len(rec.Frames) == 0 {
		t.Fatalf("%+v", rec)
	}
	last := rec.Frames[len(rec.Frames)-1]
	if last.W != 30 || last.H != 4 {
		t.Errorf("the resize is in the last frame: %+v", last)
	}
	if !json.Valid([]byte(rec.JSON())) {
		t.Errorf("not JSON: %s", rec.JSON())
	}
}

// Text that is markup or a script terminator in a recording stays data in the
// frames and in the static markup.
func TestHostileTextStaysData(t *testing.T) {
	in := "{\"version\":2,\"width\":60,\"height\":2}\n[1,\"o\",\"</script><img src=x onerror=alert(1)>\\u2028\\u001b]0;\\u001b[31m<b>\\u0007 &amp; <!--\"]\n"
	c, err := Parse([]byte(in), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	rec, err := c.Replay(0, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	j := rec.JSON()
	if !json.Valid([]byte(j)) {
		t.Fatalf("not JSON: %s", j)
	}
	for _, bad := range []string{"<", ">", "&", "\u2028"} {
		if strings.Contains(j, bad) {
			t.Errorf("the frames hold a raw %q: %s", bad, j)
		}
	}
	s, _ := c.ScreenAt(-1, Limits{})
	h := s.HTML()
	for _, bad := range []string{"</script", "<img", "<b>", "<!--", "\x1b"} {
		if strings.Contains(h, bad) {
			t.Errorf("the markup holds %q: %s", bad, h)
		}
	}
}

// A recording that makes the emulator do far more than its size, in a small
// file, is refused, and the refusal comes in well under the time a bomb wants.
func TestWorkBomb(t *testing.T) {
	debug.SetMemoryLimit(512 << 20)
	var b strings.Builder
	b.WriteString(`{"version":2,"width":400,"height":200}` + "\n")
	for i := 0; i < 4000; i++ {
		fmt.Fprintf(&b, "[%d,\"o\",\"\\u001b[65535S\\u001b[65535T\\u001b[65535L\\u001b[65535M\\u001b[2J\"]\n", 1+i/1000)
	}
	c, err := Parse([]byte(b.String()), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.ScreenAt(-1, Limits{MaxWork: 100_000_000}); code(err) != CodeLimit {
		t.Errorf("got %v", err)
	}
	if _, err = c.Replay(0, Limits{MaxWork: 100_000_000}); code(err) != CodeLimit {
		t.Errorf("got %v", err)
	}
}

func TestFramesBound(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{"version":2,"width":100,"height":50}` + "\n")
	for i := 0; i < 20000; i++ {
		fmt.Fprintf(&b, "[%g,\"o\",\"\\u001b[%d;1H%d line of text that changes %d\"]\n", float64(i)/10, i%2+1, i, i*7)
	}
	c, err := Parse([]byte(b.String()), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Replay(0, Limits{MaxFrameBytes: 1}); code(err) != CodeLimit {
		t.Errorf("a bound nothing fits is a refusal: %v", err)
	}
	rec, err := c.Replay(0, Limits{MaxFrameBytes: 600_000})
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.Frames) >= 20000 {
		t.Errorf("the interval was not widened: %d frames", len(rec.Frames))
	}
}

func FuzzParse(f *testing.F) {
	f.Add([]byte(v2))
	f.Add([]byte(v3))
	f.Add([]byte("{\"version\":2,\"width\":2,\"height\":2}\n[1,\"o\",\"\\ud83d\"]\n[2,\"r\",\"3x3\"]\n"))
	f.Add([]byte("{\"version\":3,\"term\":{\"cols\":2,\"rows\":2}}\n[1,\"z\",{\"a\":[1]}]\n#c\n"))
	lim := Limits{MaxBytes: 1 << 16, MaxWork: 1_000_000}
	f.Fuzz(func(t *testing.T, data []byte) {
		c, err := Parse(data, lim)
		if err != nil {
			if code(err) == "" {
				t.Fatalf("an error without a code: %v", err)
			}
			return
		}
		s, err := c.ScreenAt(-1, lim)
		if err != nil {
			if code(err) != CodeLimit {
				t.Fatalf("replay: %v", err)
			}
			return
		}
		if h := s.HTML(); strings.ContainsAny(strings.ReplaceAll(strings.ReplaceAll(h, "<span style=\"", ""), "</span>", ""), "\x1b\x00") {
			t.Fatalf("a control character in the markup: %q", h)
		}
		rec, err := c.Replay(0, lim)
		if err == nil && !json.Valid([]byte(rec.JSON())) {
			t.Fatalf("the frames are not JSON")
		}
	})
}
