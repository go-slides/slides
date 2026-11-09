package themepack

import (
	"archive/tar"
	"bytes"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"

	ulixz "github.com/ulikunitz/xz"

	"github.com/go-slides/slides/diag"
)

// entry is one entry of a test archive.
type entry struct {
	name string
	data string
	typ  byte   // tar type flag; 0 is a regular file
	link string // link name
	fmt  tar.Format
	pax  map[string]string
}

// tarOf writes entries as a tar stream, in order.
func tarOf(t testing.TB, es ...entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, e := range es {
		typ := e.typ
		if typ == 0 {
			typ = tar.TypeReg
		}
		h := &tar.Header{Typeflag: typ, Name: e.name, Mode: 0o644, Linkname: e.link, Format: e.fmt, PAXRecords: e.pax}
		if typ == tar.TypeReg {
			h.Size = int64(len(e.data))
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if typ == tar.TypeReg {
			if _, err := tw.Write([]byte(e.data)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// xzOf compresses a stream with the given dictionary size.
func xzOf(t testing.TB, b []byte, dict int) []byte {
	t.Helper()
	var out bytes.Buffer
	w, err := ulixz.WriterConfig{DictCap: dict}.NewWriter(&out)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(b); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// archiveOf is entries as a .gsta.xz.
func archiveOf(t testing.TB, es ...entry) []byte { return xzOf(t, tarOf(t, es...), 1<<20) }

func readArchive(b []byte, lim Limits) (Files, error) {
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
	f := func(name string) entry { return entry{name: name, data: big} }
	cases := []struct {
		name string
		z    []byte
		lim  Limits
		code string
	}{
		{"slip", archiveOf(t, f("../evil.css")), Limits{}, "theme.archive"},
		{"slip deep", archiveOf(t, f("a/../../evil")), Limits{}, "theme.archive"},
		{"absolute", archiveOf(t, f("/etc/passwd")), Limits{}, "theme.archive"},
		{"drive letter", archiveOf(t, f("C:/x")), Limits{}, "theme.archive"},
		{"backslash", archiveOf(t, f(`..\evil`)), Limits{}, "theme.archive"},
		{"dot element", archiveOf(t, f("a/./b")), Limits{}, "theme.archive"},
		{"empty element", archiveOf(t, f("a//b")), Limits{}, "theme.archive"},
		{"control character", archiveOf(t, f("a\x01b")), Limits{}, "theme.archive"},
		{"long name", archiveOf(t, entry{name: strings.Repeat("a/", 140) + "b", data: big, fmt: tar.FormatPAX}), Limits{}, "theme.archive"},
		{"duplicate", archiveOf(t, f("a.css"), f("a.css")), Limits{}, "theme.archive"},
		{"duplicate by case", archiveOf(t, f("a.css"), f("A.CSS")), Limits{}, "theme.archive"},
		{"symlink", archiveOf(t, entry{name: "l", typ: tar.TypeSymlink, link: "/etc/passwd"}), Limits{}, "theme.archive"},
		{"hardlink", archiveOf(t, f("a"), entry{name: "l", typ: tar.TypeLink, link: "a"}), Limits{}, "theme.archive"},
		{"char device", archiveOf(t, entry{name: "dev", typ: tar.TypeChar}), Limits{}, "theme.archive"},
		{"block device", archiveOf(t, entry{name: "dev", typ: tar.TypeBlock}), Limits{}, "theme.archive"},
		{"fifo", archiveOf(t, entry{name: "p", typ: tar.TypeFifo}), Limits{}, "theme.archive"},
		{"gnu format", archiveOf(t, entry{name: "a", data: big, fmt: tar.FormatGNU}), Limits{}, "theme.archive"},
		{"gnu long name", archiveOf(t, entry{name: strings.Repeat("d", 120) + "/x", data: big, fmt: tar.FormatGNU}), Limits{}, "theme.archive"},
		{"oversized pax", archiveOf(t, entry{name: "a", data: big, fmt: tar.FormatPAX, pax: map[string]string{"comment": strings.Repeat("x", 20<<10)}}), Limits{}, "theme.limit"},
		{"not xz", []byte("PK\x03\x04 but not really"), Limits{}, "theme.archive"},
		{"xz of not a tar", xzOf(t, bytes.Repeat([]byte("garbage!"), 200), 1<<20), Limits{}, "theme.archive"},
		{"empty file", nil, Limits{}, "theme.archive"},
		{"too many files", archiveOf(t, f("a"), f("b"), f("c")), Limits{MaxFiles: 2}, "theme.limit"},
		{"file too big", archiveOf(t, entry{name: "a", data: strings.Repeat("x", 2000)}), Limits{MaxFile: 1000}, "theme.limit"},
		{"total too big", archiveOf(t, entry{name: "a", data: strings.Repeat("x", 800)}, entry{name: "b", data: strings.Repeat("y", 800)}), Limits{MaxFile: 1000, MaxTotal: 1500}, "theme.limit"},
		{"archive too big", archiveOf(t, f("a")), Limits{MaxArchive: 50}, "theme.limit"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := readArchive(c.z, c.lim)
			wantCode(t, err, c.code)
		})
	}
}

func TestReadFilesAccepts(t *testing.T) {
	files, err := readArchive(archiveOf(t,
		entry{name: "d/", typ: tar.TypeDir},
		entry{name: "d/a.css", data: "a{}", fmt: tar.FormatUSTAR},
		entry{name: "b.txt", data: "ß", fmt: tar.FormatPAX},
	), Limits{})
	if err != nil || string(files["d/a.css"]) != "a{}" || string(files["b.txt"]) != "ß" || len(files) != 2 {
		t.Fatalf("%v %q", err, files)
	}
}

func TestTruncatedAndCorrupt(t *testing.T) {
	z := archiveOf(t, entry{name: "a", data: strings.Repeat("hello ", 500)})
	for _, n := range []int{4, 12, len(z) / 2, len(z) - 1} {
		_, err := readArchive(z[:n], Limits{})
		wantCode(t, err, "theme.archive")
	}
	bad := bytes.Clone(z)
	bad[len(bad)/2] ^= 0xff
	if _, err := readArchive(bad, Limits{}); err == nil {
		t.Fatal("a flipped byte was accepted")
	}
}

func TestDataAfterTheTarEnd(t *testing.T) {
	tarb := append(tarOf(t, entry{name: "a", data: "x"}), []byte("trailing junk")...)
	_, err := readArchive(xzOf(t, tarb, 1<<20), Limits{})
	wantCode(t, err, "theme.archive")
	// zeros are padding, as tar writers pad to a record
	tarb = append(tarOf(t, entry{name: "a", data: "x"}), make([]byte, 10240)...)
	if _, err := readArchive(xzOf(t, tarb, 1<<20), Limits{}); err != nil {
		t.Fatalf("padding refused: %v", err)
	}
}

// The bombs run under a memory limit and assert a refusal, and that the
// refusal spent little: what the reader allocated while refusing is measured.
func TestBombsRefusedUnderMemoryLimit(t *testing.T) {
	zeros := bytes.Repeat([]byte{0}, 100<<20)
	small := tarOf(t, entry{name: "a", data: "x"})
	var three []entry
	for _, n := range []string{"a", "b", "c"} {
		three = append(three, entry{name: n, data: string(zeros[:8<<20])})
	}
	rnd := make([]byte, 3<<20)
	var x uint64 = 88172645463325252
	for i := range rnd {
		x ^= x << 13
		x ^= x >> 7
		x ^= x << 17
		rnd[i] = byte(x)
	}
	cases := []struct {
		name string
		z    []byte
		lim  Limits
		code string
	}{
		// a tar of one small file, then 100 MiB of zeros the tar reader never asked for
		{"zero tail", xzOf(t, append(append([]byte{}, small...), zeros...), 1<<20), Limits{}, "theme.limit"},
		// three entries each within the entry bound, together far over the ratio
		{"ratio", archiveOf(t, three...), Limits{}, "theme.limit"},
		// 3 MiB that needs a 3 MiB window, under a 1 MiB ceiling
		{"dictionary", xzOf(t, tarOf(t, entry{name: "a", data: string(rnd)}), 4<<20), Limits{MaxDictionary: 1 << 20}, "theme.limit"},
	}
	old := debug.SetMemoryLimit(128 << 20)
	defer debug.SetMemoryLimit(old)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			_, err := readArchive(c.z, c.lim)
			runtime.ReadMemStats(&after)
			wantCode(t, err, c.code)
			if grew := after.TotalAlloc - before.TotalAlloc; grew > 64<<20 {
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
	_, err := readArchive(archiveOf(t, es...), Limits{MaxTotal: 50000})
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

func TestIsPackage(t *testing.T) {
	for _, ok := range []string{"a.gsta.xz", "dir/ACME.GSTA.XZ"} {
		if !IsPackage(ok) {
			t.Errorf("%q", ok)
		}
	}
	for _, no := range []string{"a.xz", "a.gsta", "a.slidestheme", "gsta.xz.txt", "a.tar.xz"} {
		if IsPackage(no) {
			t.Errorf("%q", no)
		}
	}
}

func TestWriteFilesRoundTripAndDeterminism(t *testing.T) {
	f := Files{"theme.hcl": []byte("x"), "b.css": []byte("y"), "a.css": []byte("z")}
	a, err := WriteFiles(f)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := WriteFiles(f)
	if !bytes.Equal(a, b) {
		t.Fatal("two writes differ")
	}
	got, err := readArchive(a, Limits{})
	if err != nil || len(got) != 3 || string(got["a.css"]) != "z" {
		t.Fatalf("%v %q", err, got)
	}
	// theme.hcl comes first
	tr := tar.NewReader(bytes.NewReader(mustInflate(t, a)))
	h, _ := tr.Next()
	if h.Name != "theme.hcl" || h.Format != tar.FormatUSTAR && h.Format != tar.FormatPAX {
		t.Fatalf("first entry %s (%v)", h.Name, h.Format)
	}
	if _, err := WriteFiles(Files{"../x": nil}); err == nil {
		t.Fatal("WriteFiles wrote an unsafe name")
	}
}

func mustInflate(t testing.TB, z []byte) []byte {
	t.Helper()
	r, err := ulixz.NewReader(bytes.NewReader(z))
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if _, err := b.ReadFrom(r); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
