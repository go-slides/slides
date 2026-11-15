package slides

import (
	"compress/gzip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The size tests build internal/wasmprobe for GOOS=js GOARCH=wasm, with and
// without a data tag, and compare the gzip sizes of the modules: that is what a
// browser downloads. They take a minute each, so they run only when asked:
//
//	SLIDES_WASM_SIZE=1 go test -run WasmSize .
//
// (the CI job "wasm size" does). Measured with `wasmsize --deps` on the same
// build, the code of a package is what its functions weigh and the data is what
// it embeds; the gzip size of the whole module is the number that matters.

type gzCounter int

func (c *gzCounter) Write(p []byte) (int, error) { *c += gzCounter(len(p)); return len(p), nil }

// wasmBuild builds the probe with the given tags and returns the module.
func wasmBuild(t *testing.T, tags ...string) []byte {
	t.Helper()
	if os.Getenv("SLIDES_WASM_SIZE") != "1" {
		t.Skip("set SLIDES_WASM_SIZE=1 to build the WebAssembly probe")
	}
	out := filepath.Join(t.TempDir(), "probe.wasm")
	args := []string{"build", "-o", out}
	if len(tags) > 0 {
		args = append(args, "-tags", strings.Join(tags, ","))
	}
	args = append(args, "./internal/wasmprobe")
	cmd := exec.Command("go", args...)
	cmd.Env = append(cmd.Environ(), "GOOS=js", "GOARCH=wasm")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, b)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// gzSize is the size of b gzipped at level 9.
func gzSize(t *testing.T, b []byte) int {
	t.Helper()
	var c gzCounter
	zw, err := gzip.NewWriterLevel(&c, gzip.BestCompression)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := zw.Write(b); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return int(c)
}

// wasmGzSize builds the probe with the given tags and returns the size of the
// module, raw and gzipped (level 9).
func wasmGzSize(t *testing.T, tags ...string) (raw, gz int) {
	t.Helper()
	b := wasmBuild(t, tags...)
	return len(b), gzSize(t, b)
}

// goList is `go list -deps` of the root package, for GOOS=js GOARCH=wasm when
// js is set, with the tags.
func goList(t *testing.T, js bool, tags ...string) string {
	t.Helper()
	args := []string{"list", "-deps"}
	if len(tags) > 0 {
		args = append(args, "-tags", strings.Join(tags, ","))
	}
	cmd := exec.Command("go", append(args, ".")...)
	if js {
		cmd.Env = append(cmd.Environ(), "GOOS=js", "GOARCH=wasm")
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, out)
	}
	return string(out)
}
