package slides

import "testing"

// TestNoEmojiWasmSize: the tag slides_noemoji takes the Twemoji data (2,066,979
// bytes gzipped, measured on 2026-11-15) out of the module.
func TestNoEmojiWasmSize(t *testing.T) {
	_, with := wasmGzSize(t, "slides_novideo")
	_, without := wasmGzSize(t, "slides_novideo", "slides_noemoji")
	t.Logf("gzip: %d bytes with the emoji data, %d without: %d saved", with, without, with-without)
	if saved := with - without; saved < 1_900_000 {
		t.Errorf("slides_noemoji saves %d bytes gzipped, want about 2,066,979", saved)
	}
}
