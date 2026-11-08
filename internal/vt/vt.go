// Package vt is a small terminal emulator: it takes the bytes a program wrote
// to a terminal and keeps the screen they leave, as cells with a character, a
// colour and attributes. It reads no input and writes nothing back (no
// answerbacks, no title, no clipboard), so a hostile stream can change the
// screen and nothing else.
//
// The parser is the state machine of ECMA-48 clause 5.4 (CSI P..P I..I F:
// parameter bytes 03/00-03/15, intermediate bytes 02/00-02/15, a final byte
// 04/00-07/14), as xterm's control sequences document it, in the subset a
// screen needs: the C0 controls BS HT LF VT FF CR SO SI; ESC 7 8 c D E M H and
// the designation of the DEC special graphics set; CSI @ A B C D E F G H I J K
// L M P S T X Z ^ ` a b d e f g h l m r s u, with the private modes 6 7 25 47
// 1047 1049 and the insert mode 4; SGR with 16, 256 and direct colours (the
// semicolon and the colon forms). OSC, DCS, APC, PM and SOS strings are read
// to their terminator and dropped. The 8-bit C1 controls are not recognised
// (a code point U+0080..U+009F is dropped), so that UTF-8 text cannot start a
// sequence. Combining marks are dropped: a cell holds one code point.
//
// Every operation is bounded by the screen's size; Work counts the cells the
// stream made it touch, so that a caller can refuse a stream that makes the
// emulator do far more than it was given bytes.
package vt

import "unicode/utf8"

// Attr is a set of text attributes.
type Attr uint8

// The attributes.
const (
	Bold Attr = 1 << iota
	Faint
	Italic
	Underline
	Blink
	Inverse
	Hidden
	Strike
)

// Color is a cell's colour: 0 the default, an index into the 256 colours, or a
// direct colour.
type Color uint32

// Kinds of colour.
const (
	kindIndexed = 1 << 24
	kindRGB     = 2 << 24
)

// Indexed is the colour n of the 256.
func Indexed(n int) Color { return Color(kindIndexed | n&255) }

// RGB is a direct colour.
func RGB(r, g, b uint8) Color { return Color(kindRGB | uint32(r)<<16 | uint32(g)<<8 | uint32(b)) }

// IsDefault reports whether c is the default colour.
func (c Color) IsDefault() bool { return c == 0 }

// Index returns the index and true for an indexed colour.
func (c Color) Index() (int, bool) {
	if c&0xff000000 == kindIndexed {
		return int(c & 255), true
	}
	return 0, false
}

// Direct returns the components and true for a direct colour.
func (c Color) Direct() (r, g, b uint8, ok bool) {
	if c&0xff000000 == kindRGB {
		return uint8(c >> 16), uint8(c >> 8), uint8(c), true
	}
	return 0, 0, 0, false
}

// Cell is one position of the screen. A wide character takes two cells: the
// first (Wide 1) holds it, the second (Wide 2) is its tail and holds nothing.
type Cell struct {
	R      rune
	FG, BG Color
	A      Attr
	Wide   uint8
}

// Limits of the emulator's screen.
const (
	MaxCols = 1000
	MaxRows = 500
)

type state uint8

const (
	stGround state = iota
	stEsc
	stEscInter
	stCSI // parameter bytes
	stCSIInter
	stCSIIgnore
	stOSC    // ended by BEL or ST
	stString // DCS, SOS, PM, APC: ended by ST
	stStrEsc // ESC inside a string
)

const maxParams = 32

type pen struct {
	fg, bg Color
	a      Attr
}

type savedCursor struct {
	x, y     int
	wrap     bool
	p        pen
	charsets [2]byte
	gl       int
	origin   bool
}

// Term is a terminal screen.
type Term struct {
	cols, rows int
	main, alt  [][]Cell
	g          [][]Cell
	useAlt     bool
	x, y       int
	wrap       bool
	p          pen
	savedMain  savedCursor
	savedAlt   savedCursor
	top, bot   int
	tabs       []bool

	autowrap, origin, insert, cursorOn bool
	charsets                           [2]byte // 'B' ASCII, '0' DEC special graphics
	gl                                 int
	last                               rune

	st      state
	priv    byte
	inter   byte
	params  [maxParams]int
	seps    [maxParams]byte
	nparams int
	haveNum bool

	utf     rune
	utfNeed int
	utfLen  int

	dirty []bool
	work  uint64
}

// New is a blank screen of cols by rows, or nil when either is outside
// 1..MaxCols and 1..MaxRows.
func New(cols, rows int) *Term {
	if cols < 1 || cols > MaxCols || rows < 1 || rows > MaxRows {
		return nil
	}
	t := &Term{cursorOn: true}
	t.alloc(cols, rows)
	t.reset()
	return t
}

func (t *Term) alloc(cols, rows int) {
	t.cols, t.rows = cols, rows
	t.main = t.grid(cols, rows)
	t.alt = t.grid(cols, rows)
	t.g = t.main
	t.useAlt = false
	t.dirty = make([]bool, rows)
	t.tabs = make([]bool, cols)
}

func (t *Term) grid(cols, rows int) [][]Cell {
	g := make([][]Cell, rows)
	cells := make([]Cell, cols*rows)
	for i := range cells {
		cells[i].R = ' '
	}
	for i := range g {
		g[i] = cells[i*cols : (i+1)*cols : (i+1)*cols]
	}
	return g
}

func (t *Term) reset() {
	for _, g := range [][][]Cell{t.main, t.alt} {
		for _, row := range g {
			for i := range row {
				row[i] = Cell{R: ' '}
			}
		}
	}
	t.g, t.useAlt = t.main, false
	t.x, t.y, t.wrap = 0, 0, false
	t.p = pen{}
	t.top, t.bot = 0, t.rows-1
	t.autowrap, t.origin, t.insert, t.cursorOn = true, false, false, true
	t.charsets, t.gl = [2]byte{'B', 'B'}, 0
	t.savedMain, t.savedAlt = savedCursor{charsets: [2]byte{'B', 'B'}}, savedCursor{charsets: [2]byte{'B', 'B'}}
	for i := range t.tabs {
		t.tabs[i] = i%8 == 0
	}
	t.st, t.utfNeed = stGround, 0
	t.markAll()
}

// Cols and Rows are the screen's size.
func (t *Term) Cols() int { return t.cols }

// Rows is the screen's height.
func (t *Term) Rows() int { return t.rows }

// Cursor is the cursor's position (0-based) and whether it is shown.
func (t *Term) Cursor() (x, y int, visible bool) { return t.x, t.y, t.cursorOn }

// Work is the number of cells the stream has made the emulator touch.
func (t *Term) Work() uint64 { return t.work }

// Cell returns the cell at row y, column x; the blank cell outside the screen.
func (t *Term) Cell(y, x int) Cell {
	if y < 0 || y >= t.rows || x < 0 || x >= t.cols {
		return Cell{R: ' '}
	}
	return t.g[y][x]
}

// TakeDirty returns the rows changed since the last call, in order, and
// forgets them.
func (t *Term) TakeDirty() []int {
	var out []int
	for y, d := range t.dirty {
		if d {
			out = append(out, y)
			t.dirty[y] = false
		}
	}
	return out
}

func (t *Term) markAll() {
	for i := range t.dirty {
		t.dirty[i] = true
	}
}

// Resize changes the screen's size, keeping what fits at the top left; the
// margins are reset and the cursor kept on the screen. A size outside the
// limits is refused (false) and changes nothing.
func (t *Term) Resize(cols, rows int) bool {
	if cols < 1 || cols > MaxCols || rows < 1 || rows > MaxRows {
		return false
	}
	if cols == t.cols && rows == t.rows {
		return true
	}
	move := func(old [][]Cell) [][]Cell {
		g := t.grid(cols, rows)
		for y := 0; y < rows && y < len(old); y++ {
			copy(g[y], old[y])
		}
		return g
	}
	t.main, t.alt = move(t.main), move(t.alt)
	if t.useAlt {
		t.g = t.alt
	} else {
		t.g = t.main
	}
	oldTabs := t.tabs
	t.cols, t.rows = cols, rows
	t.tabs = make([]bool, cols)
	for i := range t.tabs {
		if i < len(oldTabs) {
			t.tabs[i] = oldTabs[i]
		} else {
			t.tabs[i] = i%8 == 0
		}
	}
	t.dirty = make([]bool, rows)
	t.top, t.bot = 0, rows-1
	t.x, t.y = min(t.x, cols-1), min(t.y, rows-1)
	t.wrap = false
	t.markAll()
	return true
}

// Write feeds the terminal bytes the program wrote. A sequence or a UTF-8
// character may be split between two calls.
func (t *Term) Write(p []byte) {
	for _, b := range p {
		if t.st == stGround && t.utfNeed == 0 && b >= 0x20 && b < 0x7f {
			t.print(rune(b))
			continue
		}
		t.feed(b)
	}
}

// WriteString is Write for a string.
func (t *Term) WriteString(s string) {
	for i := 0; i < len(s); i++ {
		b := s[i]
		if t.st == stGround && t.utfNeed == 0 && b >= 0x20 && b < 0x7f {
			t.print(rune(b))
			continue
		}
		t.feed(b)
	}
}

func (t *Term) feed(b byte) {
	if t.utfNeed > 0 {
		if b&0xC0 == 0x80 {
			t.utf = t.utf<<6 | rune(b&0x3f)
			t.utfNeed--
			if t.utfNeed == 0 {
				r := t.utf
				lo := [...]rune{0, 0, 0x80, 0x800, 0x10000}[t.utfLen]
				if r < lo || !utf8.ValidRune(r) {
					r = utf8.RuneError
				}
				t.printUnicode(r)
			}
			return
		}
		// a sequence cut short: one replacement character, then b afresh
		t.utfNeed = 0
		t.printUnicode(utf8.RuneError)
	}
	switch t.st {
	case stOSC, stString:
		switch {
		case b == 0x1b:
			t.st = stStrEsc
		case b == 0x07 && t.st == stOSC:
			t.st = stGround
		case b == 0x18 || b == 0x1a:
			t.st = stGround
		}
		return
	case stStrEsc:
		if b == '\\' {
			t.st = stGround
			return
		}
		t.st = stEsc
		t.escape(b)
		return
	}
	switch {
	case b == 0x1b:
		t.st = stEsc
		t.inter = 0
	case b == 0x18 || b == 0x1a:
		t.st = stGround
	case b < 0x20:
		t.control(b)
	case b == 0x7f:
	case b >= 0x80 && t.st != stGround:
		// not part of a sequence: abandon it and read b as text
		t.st = stGround
		t.feed(b)
	default:
		switch t.st {
		case stGround:
			t.ground(b)
		case stEsc:
			t.escape(b)
		case stEscInter:
			if b >= 0x20 && b <= 0x2f {
				return
			}
			t.escInter(b)
		case stCSI:
			t.csiParam(b)
		case stCSIInter:
			if b >= 0x20 && b <= 0x2f {
				t.inter = b
			} else if b >= 0x40 && b <= 0x7e {
				t.csi(b)
				t.st = stGround
			} else {
				t.st = stCSIIgnore
			}
		case stCSIIgnore:
			if b >= 0x40 && b <= 0x7e {
				t.st = stGround
			}
		}
	}
}

func (t *Term) ground(b byte) {
	switch {
	case b < 0x80:
		t.print(rune(b))
	case b >= 0xC2 && b <= 0xDF:
		t.utf, t.utfNeed, t.utfLen = rune(b&0x1f), 1, 2
	case b >= 0xE0 && b <= 0xEF:
		t.utf, t.utfNeed, t.utfLen = rune(b&0x0f), 2, 3
	case b >= 0xF0 && b <= 0xF4:
		t.utf, t.utfNeed, t.utfLen = rune(b&0x07), 3, 4
	default:
		t.printUnicode(utf8.RuneError)
	}
}

func (t *Term) control(b byte) {
	switch b {
	case 0x08:
		if t.x > 0 {
			t.x--
		}
		t.wrap = false
	case 0x09:
		t.tab(1)
	case 0x0a, 0x0b, 0x0c:
		t.index()
	case 0x0d:
		t.x, t.wrap = 0, false
	case 0x0e:
		t.gl = 1
	case 0x0f:
		t.gl = 0
	}
}

func (t *Term) tab(n int) {
	for ; n > 0; n-- {
		x := t.x + 1
		for x < t.cols-1 && !t.tabs[x] {
			x++
		}
		t.x = min(x, t.cols-1)
	}
	t.wrap = false
}

func (t *Term) backTab(n int) {
	for ; n > 0; n-- {
		x := t.x - 1
		for x > 0 && !t.tabs[x] {
			x--
		}
		t.x = max(x, 0)
	}
	t.wrap = false
}

func (t *Term) escape(b byte) {
	switch {
	case b >= 0x20 && b <= 0x2f:
		t.inter = b
		t.st = stEscInter
		return
	case b == '[':
		t.st = stCSI
		t.priv, t.inter, t.nparams, t.haveNum = 0, 0, 0, false
		return
	case b == ']':
		t.st = stOSC
		return
	case b == 'P' || b == 'X' || b == '^' || b == '_':
		t.st = stString
		return
	case b == 0x1b:
		return
	}
	t.st = stGround
	switch b {
	case '7':
		t.saveCursor()
	case '8':
		t.restoreCursor()
	case 'c':
		t.reset()
	case 'D':
		t.index()
	case 'E':
		t.x, t.wrap = 0, false
		t.index()
	case 'M':
		t.reverseIndex()
	case 'H':
		t.tabs[t.x] = true
	}
}

func (t *Term) escInter(b byte) {
	t.st = stGround
	if b < 0x30 || b > 0x7e {
		return
	}
	switch t.inter {
	case '(', ')':
		set := byte('B')
		if b == '0' {
			set = '0'
		}
		t.charsets[t.inter-'('] = set
	}
}

func (t *Term) csiParam(b byte) {
	switch {
	case b >= '0' && b <= '9':
		t.num()
		i := t.nparams - 1
		if t.params[i] < 65535 {
			t.params[i] = min(t.params[i]*10+int(b-'0'), 65535)
		}
	case b == ';' || b == ':':
		t.num()
		if t.nparams < maxParams {
			t.params[t.nparams], t.seps[t.nparams] = 0, b
			t.nparams++
		}
		t.haveNum = true
	case b >= 0x3c && b <= 0x3f:
		if t.nparams == 0 && !t.haveNum && t.priv == 0 {
			t.priv = b
		} else {
			t.st = stCSIIgnore
		}
	case b >= 0x20 && b <= 0x2f:
		t.inter = b
		t.st = stCSIInter
	case b >= 0x40 && b <= 0x7e:
		t.csi(b)
		t.st = stGround
	}
}

// num makes sure the current parameter exists.
func (t *Term) num() {
	if t.nparams == 0 {
		t.params[0], t.seps[0] = 0, 0
		t.nparams = 1
	}
	t.haveNum = true
}

// p is parameter i, or def when it is missing or 0.
func (t *Term) p1(i, def int) int {
	if i < t.nparams && t.params[i] > 0 {
		return t.params[i]
	}
	return def
}

// raw is parameter i, 0 when missing.
func (t *Term) raw(i int) int {
	if i < t.nparams {
		return t.params[i]
	}
	return 0
}

func (t *Term) csi(final byte) {
	if t.inter != 0 {
		return
	}
	if t.priv != 0 {
		if t.priv == '?' && (final == 'h' || final == 'l') {
			for i := 0; i < max(t.nparams, 1); i++ {
				t.setMode(t.raw(i), final == 'h')
			}
		}
		return
	}
	switch final {
	case '@':
		t.insertChars(t.p1(0, 1))
	case 'A':
		t.moveY(-t.p1(0, 1))
	case 'B', 'e':
		t.moveY(t.p1(0, 1))
	case 'C', 'a':
		t.moveX(t.p1(0, 1))
	case 'D':
		t.moveX(-t.p1(0, 1))
	case 'E':
		t.moveY(t.p1(0, 1))
		t.x = 0
	case 'F':
		t.moveY(-t.p1(0, 1))
		t.x = 0
	case 'G', '`':
		t.x, t.wrap = min(t.p1(0, 1), t.cols)-1, false
	case 'H', 'f':
		t.setPos(t.p1(0, 1), t.p1(1, 1))
	case 'I':
		t.tab(t.p1(0, 1))
	case 'J':
		t.eraseDisplay(t.raw(0))
	case 'K':
		t.eraseLine(t.raw(0))
	case 'L':
		t.insertLines(t.p1(0, 1))
	case 'M':
		t.deleteLines(t.p1(0, 1))
	case 'P':
		t.deleteChars(t.p1(0, 1))
	case 'S':
		t.scrollUp(t.top, t.bot, t.p1(0, 1))
	case 'T', '^':
		t.scrollDown(t.top, t.bot, t.p1(0, 1))
	case 'X':
		n := min(t.p1(0, 1), t.cols-t.x)
		t.fill(t.y, t.x, t.x+n)
	case 'Z':
		t.backTab(t.p1(0, 1))
	case 'b':
		if t.last != 0 {
			for n := min(t.p1(0, 1), t.cols); n > 0; n-- {
				t.print(t.last)
			}
		}
	case 'd':
		row := min(t.p1(0, 1), t.rows) - 1
		if t.origin {
			row = min(t.top+row, t.bot)
		}
		t.y, t.wrap = row, false
	case 'g':
		switch t.raw(0) {
		case 0:
			t.tabs[t.x] = false
		case 3:
			clear(t.tabs)
		}
	case 'h', 'l':
		for i := 0; i < max(t.nparams, 1); i++ {
			if t.raw(i) == 4 {
				t.insert = final == 'h'
			}
		}
	case 'm':
		t.sgr()
	case 'r':
		top, bot := t.p1(0, 1)-1, t.p1(1, t.rows)-1
		if bot >= t.rows {
			bot = t.rows - 1
		}
		if top < bot {
			t.top, t.bot = top, bot
			t.setPos(1, 1)
		}
	case 's':
		t.saveCursor()
	case 'u':
		t.restoreCursor()
	}
}

func (t *Term) setMode(n int, on bool) {
	switch n {
	case 6:
		t.origin = on
		t.setPos(1, 1)
	case 7:
		t.autowrap = on
	case 25:
		t.cursorOn = on
	case 47, 1047, 1049:
		if on == t.useAlt {
			return
		}
		if on {
			if n == 1049 {
				t.saveCursor()
			}
			t.useAlt, t.g = true, t.alt
			if n != 47 {
				t.clearGrid(t.alt)
			}
		} else {
			if n == 1047 {
				t.clearGrid(t.alt)
			}
			t.useAlt, t.g = false, t.main
			if n == 1049 {
				t.restoreCursor()
			}
		}
		t.markAll()
	}
}

func (t *Term) clearGrid(g [][]Cell) {
	for _, row := range g {
		for i := range row {
			row[i] = Cell{R: ' '}
		}
	}
	t.work += uint64(t.cols * t.rows)
}

func (t *Term) saved() *savedCursor {
	if t.useAlt {
		return &t.savedAlt
	}
	return &t.savedMain
}

func (t *Term) saveCursor() {
	*t.saved() = savedCursor{t.x, t.y, t.wrap, t.p, t.charsets, t.gl, t.origin}
}

func (t *Term) restoreCursor() {
	s := t.saved()
	t.x, t.y, t.wrap, t.p, t.charsets, t.gl, t.origin = min(s.x, t.cols-1), min(s.y, t.rows-1), s.wrap, s.p, s.charsets, s.gl, s.origin
}

func (t *Term) setPos(row, col int) {
	row, col = min(row, t.rows)-1, min(col, t.cols)-1
	if t.origin {
		row = min(t.top+row, t.bot)
	}
	t.y, t.x, t.wrap = row, col, false
}

func (t *Term) moveX(n int) {
	t.x = min(max(t.x+n, 0), t.cols-1)
	t.wrap = false
}

// moveY moves the cursor, and stops at the margin it is inside of.
func (t *Term) moveY(n int) {
	lo, hi := 0, t.rows-1
	if t.y >= t.top && t.y <= t.bot {
		lo, hi = t.top, t.bot
	}
	t.y = min(max(t.y+n, lo), hi)
	t.wrap = false
}

// index is a line feed: down one line, scrolling at the bottom margin.
func (t *Term) index() {
	t.wrap = false
	if t.y == t.bot {
		t.scrollUp(t.top, t.bot, 1)
	} else if t.y < t.rows-1 {
		t.y++
	}
}

func (t *Term) reverseIndex() {
	t.wrap = false
	if t.y == t.top {
		t.scrollDown(t.top, t.bot, 1)
	} else if t.y > 0 {
		t.y--
	}
}

func (t *Term) blank() Cell { return Cell{R: ' ', BG: t.p.bg} }

// fill blanks cells [x0, x1) of row y with the pen's background.
func (t *Term) fill(y, x0, x1 int) {
	if x0 >= x1 {
		return
	}
	row := t.g[y]
	b := t.blank()
	for x := x0; x < x1; x++ {
		row[x] = b
	}
	t.dirty[y] = true
	t.work += uint64(x1 - x0)
}

func (t *Term) eraseDisplay(mode int) {
	switch mode {
	case 0:
		t.fill(t.y, t.x, t.cols)
		for y := t.y + 1; y < t.rows; y++ {
			t.fill(y, 0, t.cols)
		}
	case 1:
		for y := 0; y < t.y; y++ {
			t.fill(y, 0, t.cols)
		}
		t.fill(t.y, 0, t.x+1)
	case 2:
		for y := 0; y < t.rows; y++ {
			t.fill(y, 0, t.cols)
		}
	}
}

func (t *Term) eraseLine(mode int) {
	switch mode {
	case 0:
		t.fill(t.y, t.x, t.cols)
	case 1:
		t.fill(t.y, 0, t.x+1)
	case 2:
		t.fill(t.y, 0, t.cols)
	}
}

func (t *Term) scrollUp(top, bot, n int) {
	n = min(n, bot-top+1)
	for y := top; y <= bot-n; y++ {
		copy(t.g[y], t.g[y+n])
		t.dirty[y] = true
	}
	for y := bot - n + 1; y <= bot; y++ {
		t.fill(y, 0, t.cols)
	}
	t.work += uint64((bot - top + 1) * t.cols)
}

func (t *Term) scrollDown(top, bot, n int) {
	n = min(n, bot-top+1)
	for y := bot; y >= top+n; y-- {
		copy(t.g[y], t.g[y-n])
		t.dirty[y] = true
	}
	for y := top; y < top+n; y++ {
		t.fill(y, 0, t.cols)
	}
	t.work += uint64((bot - top + 1) * t.cols)
}

func (t *Term) insertLines(n int) {
	if t.y < t.top || t.y > t.bot {
		return
	}
	t.scrollDown(t.y, t.bot, n)
	t.x, t.wrap = 0, false
}

func (t *Term) deleteLines(n int) {
	if t.y < t.top || t.y > t.bot {
		return
	}
	t.scrollUp(t.y, t.bot, n)
	t.x, t.wrap = 0, false
}

func (t *Term) insertChars(n int) {
	n = min(n, t.cols-t.x)
	row := t.g[t.y]
	copy(row[t.x+n:], row[t.x:t.cols-n])
	t.fill(t.y, t.x, t.x+n)
	t.work += uint64(t.cols - t.x)
	t.wrap = false
}

func (t *Term) deleteChars(n int) {
	n = min(n, t.cols-t.x)
	row := t.g[t.y]
	copy(row[t.x:], row[t.x+n:])
	t.fill(t.y, t.cols-n, t.cols)
	t.work += uint64(t.cols - t.x)
	t.wrap = false
}

// decGraphics is the DEC special graphics set, 0x5f..0x7e.
var decGraphics = [...]rune{
	' ', '◆', '▒', '␉', '␌', '␍', '␊', '°', '±', '␤', '␋', '┘', '┐', '┌', '└', '┼',
	'⎺', '⎻', '─', '⎼', '⎽', '├', '┤', '┴', '┬', '│', '≤', '≥', 'π', '≠', '£', '·',
}

func (t *Term) print(r rune) {
	if r >= 0x5f && r <= 0x7e && t.charsets[t.gl] == '0' {
		r = decGraphics[r-0x5f]
	}
	t.printUnicode(r)
}

func (t *Term) printUnicode(r rune) {
	if r >= 0x80 && r < 0xa0 {
		return
	}
	w := Width(r)
	if w == 0 {
		return
	}
	t.last = r
	if t.wrap {
		t.wrap = false
		if t.autowrap {
			t.x = 0
			t.index()
		}
	}
	if t.x+w > t.cols {
		if !t.autowrap {
			t.x = t.cols - w
		} else {
			// a wide character that does not fit goes to the next line
			t.fill(t.y, t.x, t.cols)
			t.x = 0
			t.index()
		}
		if t.x < 0 || t.x+w > t.cols {
			return
		}
	}
	row := t.g[t.y]
	if t.insert {
		copy(row[t.x+w:], row[t.x:t.cols-w])
		t.work += uint64(t.cols - t.x)
	}
	// overwriting half of a wide character blanks the other half
	if row[t.x].Wide == 2 && t.x > 0 {
		row[t.x-1] = Cell{R: ' ', BG: row[t.x-1].BG}
	}
	if row[t.x].Wide == 1 && t.x+1 < t.cols {
		row[t.x+1] = Cell{R: ' ', BG: row[t.x+1].BG}
	}
	c := Cell{R: r, FG: t.p.fg, BG: t.p.bg, A: t.p.a}
	if w == 2 {
		c.Wide = 1
		if t.x+2 < t.cols && row[t.x+1].Wide == 1 {
			row[t.x+2] = Cell{R: ' ', BG: row[t.x+2].BG}
		}
		row[t.x] = c
		row[t.x+1] = Cell{FG: t.p.fg, BG: t.p.bg, A: t.p.a, Wide: 2}
	} else {
		row[t.x] = c
	}
	t.dirty[t.y] = true
	t.work++
	t.x += w
	if t.x >= t.cols {
		t.x = t.cols - 1
		t.wrap = true
	}
}

func (t *Term) sgr() {
	n := max(t.nparams, 1)
	for i := 0; i < n; {
		v := t.raw(i)
		// the end of this parameter's colon group
		j := i + 1
		for j < t.nparams && t.seps[j] == ':' {
			j++
		}
		switch {
		case v == 0:
			t.p = pen{}
		case v == 1:
			t.p.a |= Bold
		case v == 2:
			t.p.a |= Faint
		case v == 3:
			t.p.a |= Italic
		case v == 4:
			if j > i+1 && t.params[i+1] == 0 {
				t.p.a &^= Underline
			} else {
				t.p.a |= Underline
			}
		case v == 5 || v == 6:
			t.p.a |= Blink
		case v == 7:
			t.p.a |= Inverse
		case v == 8:
			t.p.a |= Hidden
		case v == 9:
			t.p.a |= Strike
		case v == 21:
			t.p.a |= Underline
		case v == 22:
			t.p.a &^= Bold | Faint
		case v == 23:
			t.p.a &^= Italic
		case v == 24:
			t.p.a &^= Underline
		case v == 25:
			t.p.a &^= Blink
		case v == 27:
			t.p.a &^= Inverse
		case v == 28:
			t.p.a &^= Hidden
		case v == 29:
			t.p.a &^= Strike
		case v >= 30 && v <= 37:
			t.p.fg = Indexed(v - 30)
		case v == 39:
			t.p.fg = 0
		case v >= 40 && v <= 47:
			t.p.bg = Indexed(v - 40)
		case v == 49:
			t.p.bg = 0
		case v >= 90 && v <= 97:
			t.p.fg = Indexed(v - 90 + 8)
		case v >= 100 && v <= 107:
			t.p.bg = Indexed(v - 100 + 8)
		case v == 38 || v == 48:
			var c Color
			var ok bool
			if j > i+1 { // 38:5:n and 38:2:[cs]:r:g:b
				c, ok = extColor(t.params[i+1 : j])
			} else { // 38;5;n and 38;2;r;g;b
				switch t.raw(i + 1) {
				case 5:
					c, ok = extColor(t.params[i+1 : min(i+3, t.nparams)])
					j = min(i+3, t.nparams)
				case 2:
					c, ok = extColor(t.params[i+1 : min(i+5, t.nparams)])
					j = min(i+5, t.nparams)
				default:
					j = t.nparams
				}
			}
			if ok {
				if v == 38 {
					t.p.fg = c
				} else {
					t.p.bg = c
				}
			}
		}
		i = j
	}
}

// extColor reads what follows 38 or 48: 5 n, 2 r g b, or 2 cs r g b (the colour
// space is ignored).
func extColor(s []int) (Color, bool) {
	if len(s) == 0 {
		return 0, false
	}
	switch s[0] {
	case 5:
		if len(s) >= 2 && s[1] <= 255 {
			return Indexed(s[1]), true
		}
	case 2:
		if len(s) >= 5 {
			s = s[2:]
		} else {
			s = s[1:]
		}
		if len(s) >= 3 && s[0] <= 255 && s[1] <= 255 && s[2] <= 255 {
			return RGB(uint8(s[0]), uint8(s[1]), uint8(s[2])), true
		}
	}
	return 0, false
}

// Width is the number of columns r takes: 0 for a control or a combining mark,
// 2 for a wide (East Asian Wide or Fullwidth, or an emoji) character, 1
// otherwise.
func Width(r rune) int {
	switch {
	case r < 0x20 || r == 0x7f:
		return 0
	case r < 0x300:
		return 1
	case r >= 0x300 && r <= 0x36f, r >= 0x200b && r <= 0x200f, r >= 0x20d0 && r <= 0x20ff,
		r >= 0xfe00 && r <= 0xfe0f, r >= 0xfe20 && r <= 0xfe2f, r == 0xfeff, r >= 0x2028 && r <= 0x202e:
		return 0
	case r >= 0x1100 && r <= 0x115f, r >= 0x2e80 && r <= 0x303e, r >= 0x3041 && r <= 0x33ff,
		r >= 0x3400 && r <= 0x4dbf, r >= 0x4e00 && r <= 0xa4cf, r >= 0xa960 && r <= 0xa97f,
		r >= 0xac00 && r <= 0xd7a3, r >= 0xf900 && r <= 0xfaff, r >= 0xfe30 && r <= 0xfe6f,
		r >= 0xff00 && r <= 0xff60, r >= 0xffe0 && r <= 0xffe6, r >= 0x1f300 && r <= 0x1f64f,
		r >= 0x1f900 && r <= 0x1f9ff, r >= 0x20000 && r <= 0x3fffd:
		return 2
	}
	return 1
}

// Run is a stretch of a row in one style.
type Run struct {
	Text   string
	FG, BG Color
	A      Attr
}

// blankCell is a cell that shows nothing on the default background.
func blankCell(c Cell) bool {
	return (c.R == ' ' || c.R == 0 && c.Wide == 0) && c.BG == 0 && c.A&(Inverse|Underline|Strike) == 0
}

// Runs is row y as runs of one style, without the blank cells at its end.
func (t *Term) Runs(y int) []Run {
	if y < 0 || y >= t.rows {
		return nil
	}
	row := t.g[y]
	end := len(row)
	for end > 0 && blankCell(row[end-1]) {
		end--
	}
	var runs []Run
	var cur []byte
	for x := 0; x < end; x++ {
		c := row[x]
		if c.Wide == 2 {
			continue
		}
		if len(runs) == 0 || runs[len(runs)-1].FG != c.FG || runs[len(runs)-1].BG != c.BG || runs[len(runs)-1].A != c.A {
			if len(runs) > 0 {
				runs[len(runs)-1].Text = string(cur)
			}
			cur = cur[:0]
			runs = append(runs, Run{FG: c.FG, BG: c.BG, A: c.A})
		}
		r := c.R
		if r == 0 {
			r = ' '
		}
		cur = utf8.AppendRune(cur, r)
	}
	if len(runs) > 0 {
		runs[len(runs)-1].Text = string(cur)
	}
	return runs
}
