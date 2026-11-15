package slides

import (
	"compress/gzip"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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

// wasmGzSize builds the probe with the given tags and returns the size of the
// module, raw and gzipped (level 9).
func wasmGzSize(t *testing.T, tags ...string) (raw, gz int) {
	t.Helper()
	if os.Getenv("SLIDES_WASM_SIZE") != "1" {
		t.Skip("set SLIDES_WASM_SIZE=1 to build the WebAssembly probe")
	}
	out := filepath.Join(t.TempDir(), "probe.wasm")
	args := []string{"build", "-o", out}
	if len(tags) > 0 {
		list := ""
		for i, tag := range tags {
			if i > 0 {
				list += ","
			}
			list += tag
		}
		args = append(args, "-tags", list)
	}
	args = append(args, "./internal/wasmprobe")
	cmd := exec.Command("go", args...)
	cmd.Env = append(cmd.Environ(), "GOOS=js", "GOARCH=wasm")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, b)
	}
	f, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var c gzCounter
	zw, err := gzip.NewWriterLevel(&c, gzip.BestCompression)
	if err != nil {
		t.Fatal(err)
	}
	n, err := io.Copy(zw, f)
	if err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return int(n), int(c)
}
