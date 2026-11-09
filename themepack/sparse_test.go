package themepack

import (
	"bytes"
	"fmt"
	"testing"
)

// block is one 512-byte ustar header written by hand: tar.Writer refuses to
// write an 'x' header itself and some of its keys, a hostile archive does not.
func block(name string, typ byte, size int) []byte {
	b := make([]byte, 512)
	copy(b[0:], name)
	copy(b[100:], "0000644\x00")
	copy(b[108:], "0000000\x00")
	copy(b[116:], "0000000\x00")
	copy(b[124:], fmt.Sprintf("%011o\x00", size))
	copy(b[136:], "00000000000\x00")
	copy(b[148:], "        ")
	b[156] = typ
	copy(b[257:], "ustar\x0000")
	sum := 0
	for _, c := range b {
		sum += int(c)
	}
	copy(b[148:], fmt.Sprintf("%06o\x00 ", sum))
	return b
}

func pad(b []byte) []byte {
	if r := len(b) % 512; r != 0 {
		b = append(b, make([]byte, 512-r)...)
	}
	return b
}

// paxRaw is a pax extended header with the records, then an ordinary file.
func paxRaw(records map[string]string, name, data string) []byte {
	var rec bytes.Buffer
	for k, v := range records {
		body := fmt.Sprintf(" %s=%s\n", k, v)
		n := len(body) + 1
		for len(fmt.Sprint(n))+len(body) != n {
			n = len(fmt.Sprint(n)) + len(body)
		}
		fmt.Fprintf(&rec, "%d%s", n, body)
	}
	var out []byte
	out = append(out, block("PaxHeaders/x", 'x', rec.Len())...)
	out = append(out, pad(rec.Bytes())...)
	out = append(out, block(name, '0', len(data))...)
	out = append(out, pad([]byte(data))...)
	return append(out, make([]byte, 1024)...)
}

func TestHandWrittenPaxHeaders(t *testing.T) {
	cases := []struct {
		name string
		rec  map[string]string
		code string // "" is any refusal
	}{
		{"sparse 1.0", map[string]string{"GNU.sparse.major": "1", "GNU.sparse.minor": "0", "GNU.sparse.name": "a", "GNU.sparse.realsize": "99999999999"}, "theme.archive"},
		{"sparse 0.1", map[string]string{"GNU.sparse.size": "99999999999", "GNU.sparse.numblocks": "1", "GNU.sparse.map": "0,1"}, "theme.archive"},
		{"size record", map[string]string{"size": "99999999999"}, ""},
		{"path escape", map[string]string{"path": "../../evil"}, "theme.archive"},
		{"pax too big", map[string]string{"comment": string(bytes.Repeat([]byte("x"), 20<<10))}, "theme.limit"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := readArchive(xzOf(t, paxRaw(c.rec, "a", "1"), 1<<20), Limits{})
			if c.code == "" {
				if err == nil {
					t.Fatal("accepted")
				}
				return
			}
			wantCode(t, err, c.code)
		})
	}
	// the helper itself writes what it says
	if _, err := readArchive(xzOf(t, paxRaw(map[string]string{"comment": "hi"}, "a", "1"), 1<<20), Limits{}); err != nil {
		t.Fatalf("a plain pax header refused: %v", err)
	}
}
