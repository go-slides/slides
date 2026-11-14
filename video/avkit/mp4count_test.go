package avkit_test

import (
	"bytes"
	"encoding/binary"
	"runtime"
	"runtime/debug"
	"testing"

	. "github.com/go-slides/slides/video"
)

// patch sets the 32-bit field at off bytes after the box type's first
// occurrence.
func patch(t *testing.T, data []byte, typ string, off int, v uint32) []byte {
	t.Helper()
	out := append([]byte(nil), data...)
	i := bytes.Index(out, []byte(typ))
	if i < 0 {
		t.Skipf("no %s box in the clip", typ)
	}
	binary.BigEndian.PutUint32(out[i+4+off:], v)
	return out
}

// The sample counts of an MP4's tables drive what the container reader
// allocates: a table that says 2^31 samples in a few bytes (a run in stts or
// ctts, a fixed sample size in stsz) must be refused before it is read.
func TestMP4SampleCountsAreBoundedBeforeTheyAreRead(t *testing.T) {
	old := debug.SetMemoryLimit(1 << 30)
	defer debug.SetMemoryLimit(old)
	clip := read(t, "bframes.mp4")
	for _, tc := range []struct {
		name string
		typ  string
		off  int // from the end of the box type: version/flags, then...
		v    uint32
	}{
		{"stts: a run of 2^31 samples", "stts", 8, 1 << 31},
		{"ctts: a run of 2^31 samples", "ctts", 8, 1 << 31},
		{"stsz: 2^31 samples", "stsz", 8, 1 << 31},
		{"stsz: an entry count the box cannot hold", "stsz", 8, 1<<31 - 1},
		{"stts: more entries than the box holds", "stts", 4, 1<<31 - 1},
	} {
		data := patch(t, clip, tc.typ, tc.off, tc.v)
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		_, err := Open(data, DefaultLimits)
		runtime.ReadMemStats(&after)
		got := (after.TotalAlloc - before.TotalAlloc) >> 20
		e, coded := err.(*Error)
		t.Logf("%s: %v (%d MiB allocated)", tc.name, err, got)
		if got > 64 {
			t.Errorf("%s: %d MiB allocated", tc.name, got)
		}
		if err != nil && coded && e.Code != CodeLimit && e.Code != CodeSyntax {
			t.Errorf("%s: code %s", tc.name, e.Code)
		}
	}
}
