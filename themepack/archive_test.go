package themepack

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"hash/crc32"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/go-slides/slides/diag"
)

// entry is one entry of a test archive.
type entry struct {
	name string
	data string
	mode uint32 // unix mode bits for the external attributes; 0 for none
}

// zipOf writes entries, deflated, in order.
func zipOf(t testing.TB, es ...entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range es {
		h := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		if e.mode != 0 {
			h.SetMode(toFileMode(e.mode))
		}
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(e.data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func readZip(b []byte, lim Limits) (Files, error) {
	return ReadFiles(bytes.NewReader(b), int64(len(b)), lim)
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	if err == nil {
		t.Fatalf("want a %s refusal, got none", code)
	}
	if got := diag.CodeOf(err); got != code {
		t.Fatalf("want code %s, got %q: %v", code, got, err)
	}
}

func TestReadFilesRefuses(t *testing.T) {
	big := strings.Repeat("a", 100)
	cases := []struct {
		name string
		zip  []byte
		lim  Limits
		code string
	}{
		{"zip-slip", zipOf(t, entry{name: "../evil.css", data: big}), Limits{}, "theme.archive"},
		{"zip-slip deep", zipOf(t, entry{name: "a/../../evil", data: big}), Limits{}, "theme.archive"},
		{"absolute", zipOf(t, entry{name: "/etc/passwd", data: big}), Limits{}, "theme.archive"},
		{"drive letter", zipOf(t, entry{name: "C:/x", data: big}), Limits{}, "theme.archive"},
		{"backslash", zipOf(t, entry{name: `..\evil`, data: big}), Limits{}, "theme.archive"},
		{"dot element", zipOf(t, entry{name: "a/./b", data: big}), Limits{}, "theme.archive"},
		{"empty element", zipOf(t, entry{name: "a//b", data: big}), Limits{}, "theme.archive"},
		{"control character", zipOf(t, entry{name: "a\x01b", data: big}), Limits{}, "theme.archive"},
		{"long name", zipOf(t, entry{name: strings.Repeat("a", 300), data: big}), Limits{}, "theme.archive"},
		{"duplicate", zipOf(t, entry{name: "a.css", data: big}, entry{name: "a.css", data: big}), Limits{}, "theme.archive"},
		{"duplicate by case", zipOf(t, entry{name: "a.css", data: big}, entry{name: "A.CSS", data: big}), Limits{}, "theme.archive"},
		{"symlink", zipOf(t, entry{name: "link", data: "/etc/passwd", mode: 0o120777}), Limits{}, "theme.archive"},
		{"device", zipOf(t, entry{name: "dev", data: "", mode: 0o60660}), Limits{}, "theme.archive"},
		{"not a zip", []byte("PK\x03\x04 but not really"), Limits{}, "theme.archive"},
		{"too many files", zipOf(t, entry{name: "a", data: "x"}, entry{name: "b", data: "x"}, entry{name: "c", data: "x"}), Limits{MaxFiles: 2}, "theme.limit"},
		{"file too big", zipOf(t, entry{name: "a", data: strings.Repeat("x", 2000)}), Limits{MaxFile: 1000}, "theme.limit"},
		{"total too big", zipOf(t, entry{name: "a", data: strings.Repeat("x", 800)}, entry{name: "b", data: strings.Repeat("y", 800)}), Limits{MaxFile: 1000, MaxTotal: 1500}, "theme.limit"},
		{"archive too big", zipOf(t, entry{name: "a", data: big}), Limits{MaxArchive: 50}, "theme.limit"},
		{"ratio", zipOf(t, entry{name: "zeros", data: strings.Repeat("\x00", 1<<20)}), Limits{}, "theme.limit"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := readZip(c.zip, c.lim)
			wantCode(t, err, c.code)
		})
	}
}

// rawZip writes one entry whose header claims what the test says, around data
// that is deflated as is: the central directory is a claim.
func rawZip(t testing.TB, name string, method uint16, flags uint16, declared uint64, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	h := &zip.FileHeader{
		Name: name, Method: method, Flags: flags,
		CRC32: crc32.ChecksumIEEE(data), CompressedSize64: uint64(len(data)), UncompressedSize64: declared,
	}
	w, err := zw.CreateRaw(h)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func deflated(t testing.TB, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	fw, err := flate.NewWriter(&buf, flate.BestCompression)
	if err != nil {
		t.Fatal(err)
	}
	fw.Write(data)
	fw.Close()
	return buf.Bytes()
}

func TestReadFilesMethods(t *testing.T) {
	_, err := readZip(rawZip(t, "a", 99, 0, 3, []byte("abc")), Limits{})
	wantCode(t, err, "theme.archive")
	_, err = readZip(rawZip(t, "a", zip.Store, 1, 3, []byte("abc")), Limits{})
	wantCode(t, err, "theme.archive")
	files, err := readZip(rawZip(t, "a", zip.Store, 0, 3, []byte("abc")), Limits{})
	if err != nil || string(files["a"]) != "abc" {
		t.Fatalf("a stored entry: %v %q", err, files["a"])
	}
}

// A bomb under GOMEMLIMIT: a deflate stream of 200 MiB of zeros that a
// directory claims is 10 bytes, and one that declares its honest, huge size.
// Both are refused, and the reader does not grow past its bounds.
func TestBombRefusedUnderMemoryLimit(t *testing.T) {
	old := debug.SetMemoryLimit(96 << 20)
	defer debug.SetMemoryLimit(old)
	stream := deflated(t, make([]byte, 200<<20))
	cases := map[string][]byte{
		"declared small": rawZip(t, "bomb", zip.Deflate, 0, 10, stream),
		"declared big":   rawZip(t, "bomb", zip.Deflate, 0, 200<<20, stream),
	}
	for name, z := range cases {
		t.Run(name, func(t *testing.T) {
			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			_, err := readZip(z, Limits{})
			runtime.ReadMemStats(&after)
			if err == nil {
				t.Fatal("a 200 MiB bomb was read")
			}
			if c := diag.CodeOf(err); c != "theme.limit" && c != "theme.archive" {
				t.Fatalf("want a coded refusal, got %q: %v", c, err)
			}
			if grew := after.TotalAlloc - before.TotalAlloc; grew > 40<<20 {
				t.Fatalf("the refusal allocated %d MiB", grew>>20)
			}
		})
	}
}

// Many small entries that each pass: the total, not the entry, is the bound.
func TestManyEntriesTotalBound(t *testing.T) {
	var es []entry
	for i := 0; i < 20; i++ {
		es = append(es, entry{name: "f" + string(rune('a'+i)), data: strings.Repeat("x", 4000)})
	}
	_, err := readZip(zipOf(t, es...), Limits{MaxTotal: 50000})
	wantCode(t, err, "theme.limit")
}

func TestValidName(t *testing.T) {
	for _, ok := range []string{"theme.hcl", "a/b/c.css", "fonts/Inter-Regular.woff2", "dir/"} {
		if err := ValidName(ok, 0); err != nil {
			t.Errorf("%q refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "/a", "../a", "a/..", "a/../b", "./a", "a//b", `a\b`, "C:a", "a\x00b", "\xff"} {
		if ValidName(bad, 0) == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestWriteFilesIsDeterministic(t *testing.T) {
	f := Files{"theme.hcl": []byte("x"), "b.css": []byte("y"), "a.css": []byte("z")}
	a, err := WriteFiles(f)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := WriteFiles(f)
	if !bytes.Equal(a, b) {
		t.Fatal("two writes differ")
	}
	zr, _ := zip.NewReader(bytes.NewReader(a), int64(len(a)))
	if zr.File[0].Name != "theme.hcl" {
		t.Fatalf("first entry is %s", zr.File[0].Name)
	}
	if _, err := WriteFiles(Files{"../x": nil}); err == nil {
		t.Fatal("WriteFiles wrote an unsafe name")
	}
}
