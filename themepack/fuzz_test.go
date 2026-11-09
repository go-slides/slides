package themepack

import (
	"archive/tar"
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/go-slides/slides/diag"
)

var importRe = regexp.MustCompile(`@import($|[^a-z0-9_\-\x{80}-\x{10ffff}])`)

var updateCorpus = flag.Bool("update-corpus", false, "write the fuzz seed corpus under testdata/fuzz")

// corpus is the seed of the fuzzers: hostile and honest inputs, committed
// under testdata/fuzz (regenerate with: go test ./themepack -run TestWriteCorpus -update-corpus).
func archiveSeeds(t testing.TB) map[string][]byte {
	return map[string][]byte{
		"empty":       archiveOf(t),
		"slip":        archiveOf(t, entry{name: "../evil.css", data: "x"}),
		"absolute":    archiveOf(t, entry{name: "/etc/passwd", data: "x"}),
		"symlink":     archiveOf(t, entry{name: "l", typ: tar.TypeSymlink, link: "/etc/passwd"}),
		"hardlink":    archiveOf(t, entry{name: "a", data: "1"}, entry{name: "l", typ: tar.TypeLink, link: "a"}),
		"fifo":        archiveOf(t, entry{name: "p", typ: tar.TypeFifo}),
		"duplicate":   archiveOf(t, entry{name: "a", data: "1"}, entry{name: "A", data: "2"}),
		"gnu":         archiveOf(t, entry{name: strings.Repeat("d", 120), data: "1", fmt: tar.FormatGNU}),
		"pax-sparse":  xzOf(t, paxRaw(map[string]string{"GNU.sparse.major": "1", "GNU.sparse.minor": "0", "GNU.sparse.name": "a", "GNU.sparse.realsize": "99999999999"}, "a", "1"), 1<<20),
		"pax-path":    xzOf(t, paxRaw(map[string]string{"path": "../../evil"}, "a", "1"), 1<<20),
		"pax-big":     archiveOf(t, entry{name: "a", data: "1", fmt: tar.FormatPAX, pax: map[string]string{"comment": strings.Repeat("x", 20<<10)}}),
		"zeros":       archiveOf(t, entry{name: "z", data: strings.Repeat("\x00", 1<<18)}),
		"trailing":    xzOf(t, append(tarOf(t, entry{name: "a", data: "1"}), "junk"...), 1<<20),
		"not-a-tar":   xzOf(t, bytes.Repeat([]byte("garbage!"), 200), 1<<20),
		"big-dict":    xzOf(t, tarOf(t, entry{name: "a", data: "1"}), 64<<20),
		"minimal-hcl": archiveOf(t, entry{name: MetaFile, data: "theme {\n name = \"x\"\n version = \"1.0.0\"\n license = \"MIT\"\n}\ncss \"a.css\" {}\n"}, entry{name: "a.css", data: "a{}"}),
	}
}

var metaSeeds = map[string]string{
	"ok":       "theme {\n name = \"x\"\n version = \"1.0.0\"\n license = \"MIT\"\n}\ncss \"a.css\" {}\n",
	"function": "theme {\n name = file(\"/etc/passwd\")\n}\n",
	"deep":     "theme {\n name = " + strings.Repeat("[", 60) + "\n",
	"template": "theme {\n name = \"${env(\\\"HOME\\\")}\"\n}\n",
	"heredoc":  "theme {\n description = <<EOT\nx\nEOT\n}\n",
	"kind":     "theme {\n name = \"x\"\n version = \"1.0.0\"\n license = \"MIT\"\n}\ncss \"a.css\" {}\nkind \"k\" {\n header = \"{{ x }}\"\n}\n",
	"unicode":  "theme {\n name = \"\xff\"\n}\n",
}

var cssSeeds = map[string]string{
	"ok":       "a{color:red;background:url(img/a.png)}",
	"import":   `@import "x";`,
	"escape":   `u\72l(x)`,
	"style":    `</style>`,
	"data":     `a{b:url(data:image/png;base64,AAAA)}`,
	"unclosed": `a{b:url("x`,
	"comment":  `/* url(http://x) `,
}

var svgSeeds = map[string]string{
	"ok":      `<svg xmlns="http://www.w3.org/2000/svg"><rect fill="url(#a)"/></svg>`,
	"script":  `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`,
	"doctype": `<!DOCTYPE svg [<!ENTITY a "b">]><svg/>`,
	"style":   `<svg xmlns="http://www.w3.org/2000/svg"><style>@import url(x)</style></svg>`,
	"href":    `<svg xmlns="http://www.w3.org/2000/svg"><a href="javascript:1"/></svg>`,
}

// encode is the format of a Go fuzz corpus file.
func encode(b []byte) string { return fmt.Sprintf("go test fuzz v1\n[]byte(%q)\n", b) }

// TestWriteCorpus writes the seeds as corpus files under testdata/fuzz when
// -update-corpus is given, and otherwise checks the committed files are
// there and are the current seeds.
func TestWriteCorpus(t *testing.T) {
	type set struct {
		fuzzer string
		seeds  map[string][]byte
	}
	var sets []set
	sets = append(sets, set{"FuzzReadFiles", archiveSeeds(t)})
	conv := func(m map[string]string) map[string][]byte {
		out := map[string][]byte{}
		for k, v := range m {
			out[k] = []byte(v)
		}
		return out
	}
	sets = append(sets, set{"FuzzParseMeta", conv(metaSeeds)}, set{"FuzzScanCSS", conv(cssSeeds)}, set{"FuzzCheckSVG", conv(svgSeeds)})
	for _, s := range sets {
		dir := filepath.Join("testdata", "fuzz", s.fuzzer)
		for name, b := range s.seeds {
			file := filepath.Join(dir, "seed-"+name)
			if *updateCorpus {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(file, []byte(encode(b)), 0o644); err != nil {
					t.Fatal(err)
				}
				continue
			}
			if _, err := os.Stat(file); err != nil {
				t.Errorf("corpus file %s is missing (go test -run TestWriteCorpus -update-corpus)", file)
			}
		}
	}
}

// Every refusal carries a code, and nothing panics or runs away.
func requireCoded(t *testing.T, err error) {
	t.Helper()
	if err != nil && diag.CodeOf(err) == "" {
		t.Fatalf("an uncoded refusal: %v", err)
	}
}

func FuzzReadFiles(f *testing.F) {
	for _, b := range archiveSeeds(f) {
		f.Add(b)
	}
	lim := Limits{MaxTotal: 4 << 20, MaxFile: 2 << 20, MaxArchive: 1 << 20}
	f.Fuzz(func(t *testing.T, b []byte) {
		files, err := ReadFiles(bytes.NewReader(b), int64(len(b)), lim)
		requireCoded(t, err)
		if err != nil {
			return
		}
		// what is accepted holds only safe names
		for n := range files {
			if err := ValidName(n, 0); err != nil {
				t.Fatalf("accepted the name %q: %v", n, err)
			}
		}
		_, err = FromFiles(files, lim)
		requireCoded(t, err)
	})
}

func FuzzParseMeta(f *testing.F) {
	for _, s := range metaSeeds {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		m, err := ParseMeta(b, Limits{})
		requireCoded(t, err)
		if err == nil && (m.Name == "" || m.Version == "" || len(m.CSS) == 0) {
			t.Fatalf("accepted an incomplete meta: %+v", m)
		}
	})
}

func FuzzScanCSS(f *testing.F) {
	for _, s := range cssSeeds {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		refs := 0
		out, err := scanCSS("a.css", string(b), func(r string) (string, error) {
			refs++
			if strings.HasPrefix(r, "#") {
				return r, nil
			}
			return "", cssErr("a.css", "no")
		})
		requireCoded(t, err)
		if err == nil {
			low := strings.ToLower(out)
			// an @import at-keyword outside any comment or string must not pass
			quoted := strings.ContainsAny(string(b), `"'`) || strings.Contains(string(b), "/*")
			if strings.Contains(low, "</") || importRe.MatchString(low) && !quoted {
				t.Fatalf("accepted %q", b)
			}
		}
	})
}

func FuzzCheckSVG(f *testing.F) {
	for _, s := range svgSeeds {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		err := checkSVG("a.svg", b)
		requireCoded(t, err)
		if err == nil {
			low := strings.ToLower(string(b))
			if strings.Contains(low, "<script") && !strings.Contains(low, "<!--") && !strings.Contains(low, "<![cdata[") {
				t.Fatalf("accepted a script: %q", b)
			}
		}
	})
}
