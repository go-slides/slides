package vt

import (
	"runtime/debug"
	"strings"
	"testing"
	"unicode/utf8"
)

func screen(t *Term) string {
	var b strings.Builder
	for y := 0; y < t.Rows(); y++ {
		for _, r := range t.Runs(y) {
			b.WriteString(r.Text)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func TestText(t *testing.T) {
	cases := []struct {
		name, in, want string
		cols, rows     int
	}{
		{"plain", "hello", "hello\n\n", 10, 2},
		{"crlf", "ab\r\ncd", "ab\ncd\n", 10, 2},
		{"lf alone keeps the column", "ab\ncd", "ab\n  cd\n", 10, 2},
		{"wrap", "abcdef", "abcd\nef\n", 4, 2},
		{"scroll", "a\r\nb\r\nc", "b\nc\n", 4, 2},
		{"cup", "\x1b[2;3Hx", "\n  x\n", 5, 2},
		{"cup default", "ab\x1b[Hx", "xb\n\n", 5, 2},
		{"cuf cub", "abc\x1b[2D\x1b[Cx", "abx\n\n", 5, 2},
		{"el 0", "abcde\x1b[2D\x1b[K", "ab\n\n", 5, 2},
		{"el 1", "abcde\x1b[2D\x1b[1K", "   de\n\n", 5, 2},
		{"el 2", "abcde\x1b[2K", "\n\n", 5, 2},
		{"ed 2", "ab\r\ncd\x1b[2J", "\n\n", 5, 2},
		{"ed 0", "ab\r\ncd\x1b[1;2H\x1b[J", "a\n\n", 5, 2},
		{"ich", "abc\x1b[1G\x1b[2@", "  abc\n\n", 6, 2},
		{"dch", "abcd\x1b[1G\x1b[2P", "cd\n\n", 6, 2},
		{"ech", "abcd\x1b[1G\x1b[2X", "  cd\n\n", 6, 2},
		{"il", "a\r\nb\x1b[1;1H\x1b[L", "\na\n", 3, 2},
		{"dl", "a\r\nb\x1b[1;1H\x1b[M", "b\n\n", 3, 2},
		{"su sd", "a\r\nb\x1b[S", "b\n\n", 3, 2},
		{"sd", "a\r\nb\x1b[T", "\na\n", 3, 2},
		{"margins", "1\r\n2\r\n3\x1b[2;3r\x1b[3;1H\n", "1\n3\n\n", 3, 3},
		{"ri", "a\r\nb\x1b[1;1H\x1bM", "\na\n", 3, 2},
		{"nel", "ab\x1bEc", "ab\nc\n", 4, 2},
		{"tab", "a\tb", "a       b\n\n", 12, 2},
		{"hts tbc", "\x1b[3g\x1b[1;4H\x1bH\x1b[1G\ta", "   a\n\n", 12, 2},
		{"rep", "a\x1b[3b", "aaaa\n\n", 8, 2},
		{"utf8", "é€𝄞", "é€𝄞\n\n", 8, 2},
		{"wide", "日本x", "日本x\n\n", 8, 2},
		{"bad utf8", "a\xffb\xe2\x82", "a�b\n\n", 8, 2},
		{"split utf8", "\xe2", "\n\n", 8, 2},
		{"dec graphics", "\x1b(0lqk\x1b(B", "┌─┐\n\n", 8, 2},
		{"shift out", "\x1b)0\x0eq\x0fq", "─q\n\n", 8, 2},
		{"osc bel dropped", "a\x1b]0;title\x07b", "ab\n\n", 8, 2},
		{"osc st dropped", "a\x1b]8;;http://x\x1b\\b", "ab\n\n", 8, 2},
		{"dcs dropped", "a\x1bPq#0;2;0;0;0\x1b\\b", "ab\n\n", 8, 2},
		{"apc dropped", "a\x1b_Gf=24;AAAA\x1b\\b", "ab\n\n", 8, 2},
		{"c1 dropped", "a\u009bHb", "aHb\n\n", 8, 2},
		{"private csi ignored", "a\x1b[?1000hb\x1b[>cX", "abX\n\n", 8, 2},
		{"unknown csi", "a\x1b[5;6;7zb", "ab\n\n", 8, 2},
		{"can aborts", "a\x1b[3\x18b", "ab\n\n", 8, 2},
		{"save restore", "ab\x1b7\x1b[1;1Hx\x1b8c", "xbc\n\n", 8, 2},
		{"alt screen", "main\x1b[?1049h\x1b[Halt\x1b[?1049l", "main\n\n", 8, 2},
		{"insert mode", "abc\x1b[1G\x1b[4hx", "xabc\n\n", 8, 2},
		{"no autowrap", "\x1b[?7labcdef", "abcf\n\n", 4, 2},
		{"origin", "\x1b[2;3r\x1b[?6h\x1b[1;1Hx", "\nx\n\n", 4, 3},
		{"ris", "abc\x1bc", "\n\n", 4, 2},
	}
	for _, c := range cases {
		tm := New(c.cols, c.rows)
		tm.WriteString(c.in)
		if got := screen(tm); got != c.want {
			t.Errorf("%s: %q: got %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

func TestSplitWrites(t *testing.T) {
	a, b := New(8, 2), New(8, 2)
	in := "x\x1b[1;31m€\x1b[0mé\r\n\x1b[38;2;1;2;3mz"
	a.WriteString(in)
	for i := 0; i < len(in); i++ {
		b.WriteString(in[i : i+1])
	}
	if screen(a) != screen(b) || a.Cell(0, 1) != b.Cell(0, 1) {
		t.Errorf("a byte at a time differs: %q and %q", screen(a), screen(b))
	}
}

func TestSGR(t *testing.T) {
	cases := []struct {
		in   string
		fg   Color
		bg   Color
		attr Attr
	}{
		{"\x1b[1;3;4;7mx", 0, 0, Bold | Italic | Underline | Inverse},
		{"\x1b[31;42mx", Indexed(1), Indexed(2), 0},
		{"\x1b[91;102mx", Indexed(9), Indexed(10), 0},
		{"\x1b[38;5;200;48;5;17mx", Indexed(200), Indexed(17), 0},
		{"\x1b[38;2;1;2;3;48;2;4;5;6mx", RGB(1, 2, 3), RGB(4, 5, 6), 0},
		{"\x1b[38:2::10:20:30mx", RGB(10, 20, 30), 0, 0},
		{"\x1b[38:2:10:20:30mx", RGB(10, 20, 30), 0, 0},
		{"\x1b[38:5:99mx", Indexed(99), 0, 0},
		{"\x1b[1;22mx", 0, 0, 0},
		{"\x1b[4:3mx", 0, 0, Underline},
		{"\x1b[4:0mx", 0, 0, 0},
		{"\x1b[31;1;39mx", 0, 0, Bold},
		{"\x1b[9;29;5;25mx", 0, 0, 0},
		{"\x1b[38;5;300mx", 0, 0, 0},
		{"\x1b[38;2;1;2mx", 0, 0, 0},
		{"\x1b[1;38;5mx", 0, 0, Bold},
		{"\x1b[mx", 0, 0, 0},
	}
	for _, c := range cases {
		tm := New(4, 1)
		tm.WriteString(c.in)
		got := tm.Cell(0, 0)
		if got.R != 'x' || got.FG != c.fg || got.BG != c.bg || got.A != c.attr {
			t.Errorf("%q: got %+v, want fg %x bg %x attr %b", c.in, got, c.fg, c.bg, c.attr)
		}
	}
}

func TestBackgroundColorErase(t *testing.T) {
	tm := New(4, 1)
	tm.WriteString("\x1b[44m\x1b[2K")
	if c := tm.Cell(0, 3); c.BG != Indexed(4) {
		t.Errorf("an erased cell keeps the pen's background: %+v", c)
	}
	if len(tm.Runs(0)) != 1 {
		t.Errorf("a coloured blank row is one run: %+v", tm.Runs(0))
	}
}

func TestResize(t *testing.T) {
	tm := New(4, 2)
	tm.WriteString("abcd\r\nef")
	if !tm.Resize(6, 3) || tm.Cols() != 6 || tm.Rows() != 3 || screen(tm) != "abcd\nef\n\n" {
		t.Fatalf("grow: %q", screen(tm))
	}
	if !tm.Resize(2, 1) || screen(tm) != "ab\n" {
		t.Fatalf("shrink: %q", screen(tm))
	}
	if tm.Resize(0, 1) || tm.Resize(MaxCols+1, 1) || tm.Resize(1, MaxRows+1) {
		t.Fatal("a size outside the limits must be refused")
	}
	if New(0, 1) != nil || New(1, 0) != nil || New(MaxCols+1, 1) != nil {
		t.Fatal("New must refuse a size outside the limits")
	}
}

func TestDirty(t *testing.T) {
	tm := New(4, 3)
	tm.TakeDirty()
	tm.WriteString("\x1b[2;1Hx")
	if d := tm.TakeDirty(); len(d) != 1 || d[0] != 1 {
		t.Fatalf("dirty rows %v", d)
	}
	if d := tm.TakeDirty(); len(d) != 0 {
		t.Fatalf("dirty rows after the take: %v", d)
	}
}

// A stream that asks for the most work a few bytes can ask for is refused by
// its caller through Work; the emulator itself stays inside its screen.
func TestBombStaysBounded(t *testing.T) {
	debug.SetMemoryLimit(256 << 20)
	tm := New(MaxCols, MaxRows)
	before := tm.Work()
	for i := 0; i < 100; i++ {
		tm.WriteString("\x1b[65535S\x1b[65535T\x1b[65535L\x1b[65535M\x1b[65535@\x1b[65535P\x1b[65535X\x1b[65535b\x1b[2J\x1b[9999;9999H\x1b[65535;65535r")
	}
	if tm.Work() <= before {
		t.Fatal("Work must count the cells touched")
	}
	if tm.Cols() != MaxCols || tm.Rows() != MaxRows {
		t.Fatal("the screen must not change size")
	}
	// parameters beyond the table and beyond 65535 are clamped, not stored
	tm.WriteString("\x1b[" + strings.Repeat("9;", 1000) + "99999999999999999999m\x1b[?" + strings.Repeat("1;", 100) + "h")
}

func FuzzWrite(f *testing.F) {
	for _, s := range []string{
		"hello\r\nworld", "\x1b[1;31mred\x1b[0m", "\x1b[38;2;1;2;3m\x1b[48;5;9mx", "\x1b[38:2::1:2:3mx",
		"\x1b[2J\x1b[H\x1b[3;5r\x1b[10S\x1b[10T", "\x1b]0;t\x07\x1b]8;;u\x1b\\", "\x1bP1$r\x1b\\", "\xe2\x82\xac\xff\xf0\x9f",
		"\x1b[?1049h\x1b[10@\x1b[10P\x1b[10L\x1b[10M\x1b[?1049l", "\x1b(0qqq\x0e\x0f\x1b#8\x1bc", "日本語\x1b[3b", "\x1b[999999999999;9999999999H",
	} {
		f.Add([]byte(s), uint8(10), uint8(5))
	}
	f.Fuzz(func(t *testing.T, data []byte, cols, rows uint8) {
		tm := New(int(cols)%40+1, int(rows)%20+1)
		tm.Write(data)
		if x, y, _ := tm.Cursor(); x < 0 || x >= tm.Cols() || y < 0 || y >= tm.Rows() {
			t.Fatalf("the cursor left the screen: %d,%d", x, y)
		}
		for y := 0; y < tm.Rows(); y++ {
			for _, r := range tm.Runs(y) {
				if !utf8.ValidString(r.Text) {
					t.Fatalf("a run is not valid UTF-8: %q", r.Text)
				}
			}
		}
		tm.Resize(int(rows)%30+1, int(cols)%30+1)
		tm.Write(data)
	})
}
