package avkit_test

import (
	"encoding/binary"
	"runtime"
	"runtime/debug"
	"testing"

	. "github.com/go-slides/slides/video"
	_ "github.com/go-slides/slides/video/avkit"
)

// el is an EBML element: its id, an 8-byte size, its body.
func el(id uint32, body ...[]byte) []byte {
	var b []byte
	for _, p := range body {
		b = append(b, p...)
	}
	var idb []byte
	for s := 24; s >= 0; s -= 8 {
		if c := byte(id >> uint(s)); c != 0 || len(idb) > 0 {
			idb = append(idb, c)
		}
	}
	size := make([]byte, 8)
	binary.BigEndian.PutUint64(size, uint64(len(b)))
	size[0] = 0x01
	return append(append(idb, size...), b...)
}

func repeat(b byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return out
}

func u16(n uint64) []byte { return []byte{byte(n >> 8), byte(n)} }

// webm is a one-track VP8 file whose cluster holds the blocks.
func webm(blocks ...[]byte) []byte {
	track := el(0xAE, el(0xD7, []byte{1}), el(0x73C5, []byte{1}), el(0x83, []byte{1}), el(0x86, []byte("V_VP8")),
		el(0xE0, el(0xB0, u16(64)), el(0xBA, u16(48))))
	cluster := el(0xE7, []byte{0})
	for _, b := range blocks {
		cluster = append(cluster, b...)
	}
	return append(el(0x1A45DFA3, el(0x4282, []byte("webm"))),
		el(0x18538067, el(0x1549A966, el(0x2AD7B1, []byte{0x0F, 0x42, 0x40})), el(0x1654AE6B, track), el(0x1F43B675, cluster))...)
}

// simpleBlock is track 1, timecode 0, the flags, and the payload.
func simpleBlock(flags byte, payload ...byte) []byte {
	return el(0xA3, append([]byte{0x81, 0, 0, flags}, payload...))
}

// lace flags of a block: bits 1-2.
const (
	laceXiph  = 0x02
	laceFixed = 0x04
	laceEBML  = 0x06
)

// A block of 7 bytes with fixed lacing and a count byte of 0xFF holds 256
// frames; the reader unlaces every block up front. A few hundred such blocks
// hold more frames than a track may list, from a file of a few kilobytes: it is
// refused with video.limit, and what it costs to refuse it is small.
func TestLacedBlocksAreCountedBeforeTheyAreUnlaced(t *testing.T) {
	old := debug.SetMemoryLimit(512 << 20)
	defer debug.SetMemoryLimit(old)
	var blocks [][]byte
	for i := 0; i < 2000; i++ { // 2000 x 256 = 512,000 frames, over the 262,144 a track may list
		blocks = append(blocks, simpleBlock(laceFixed, 0xFF))
	}
	data := webm(blocks...)
	if len(data) > 64<<10 {
		t.Fatalf("the bomb is %d bytes", len(data))
	}
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	_, err := Open(data, DefaultLimits)
	runtime.ReadMemStats(&after)
	var e *Error
	if err == nil || !asError(err, &e) || e.Code != CodeLimit {
		t.Fatalf("a file of %d bytes holding 512,000 laced frames: %v", len(data), err)
	}
	if got := after.TotalAlloc - before.TotalAlloc; got > 64<<20 {
		t.Fatalf("refusing it allocated %d MiB", got>>20)
	}
}

func asError(err error, out **Error) bool {
	e, ok := err.(*Error)
	*out = e
	return ok
}

// Every lacing is counted, as the frames it holds and not as one element.
func TestEveryLacingIsCounted(t *testing.T) {
	// 256 frames in each lacing: the count byte, then what the lacing spends on
	// 255 sizes (Xiph: a byte each; EBML: the first size and 254 differences, a
	// byte each; fixed: none, the frames share the rest)
	frames := make([]byte, 256) // a byte a frame, for the lacings that spell out sizes
	payloads := map[string][]byte{
		"xiph":  append(append([]byte{0xFF}, repeat(1, 255)...), frames...),
		"ebml":  append(append([]byte{0xFF, 0x81}, repeat(0xBF, 254)...), frames...),
		"fixed": {0xFF},
	}
	flagsOf := map[string]byte{"xiph": laceXiph, "ebml": laceEBML, "fixed": laceFixed}
	for name, p := range payloads {
		var blocks [][]byte
		for i := 0; i < 2000; i++ {
			blocks = append(blocks, simpleBlock(flagsOf[name], p...))
		}
		if _, err := Open(webm(blocks...), DefaultLimits); err == nil || err.(*Error).Code != CodeLimit {
			t.Errorf("%s lacing: %v", name, err)
		}
	}
	// and a block inside a BlockGroup
	var groups [][]byte
	for i := 0; i < 2000; i++ {
		groups = append(groups, el(0xA0, el(0xA1, []byte{0x81, 0, 0, laceFixed, 0xFF})))
	}
	if _, err := Open(webm(groups...), DefaultLimits); err == nil || err.(*Error).Code != CodeLimit {
		t.Errorf("laced Block in a BlockGroup: %v", err)
	}
}

// A few laced frames are a video: the bound is on what a track may list.
func TestAFewLacedFramesAreRead(t *testing.T) {
	_, err := Open(webm(simpleBlock(laceFixed, 3, 0, 0, 0, 0), simpleBlock(0, 1, 2, 3)), DefaultLimits)
	if err != nil {
		if e, ok := err.(*Error); ok && e.Code == CodeLimit {
			t.Fatalf("refused as over the limit: %v", err)
		}
	}
}

// FuzzLacedBlocks fuzzes the scanner's lace counting: whatever the bytes, a
// file is read or refused with a code.
func FuzzLacedBlocks(f *testing.F) {
	f.Add(webm(simpleBlock(laceFixed, 0xFF)))
	f.Add(webm(simpleBlock(laceXiph, 2, 1, 1, 0, 0, 0)))
	f.Add(webm(simpleBlock(laceEBML, 1, 0x81, 0, 0)))
	f.Add(webm(el(0xA0, el(0xA1, []byte{0x81, 0, 0, laceFixed, 0xFF}))))
	f.Add(webm(simpleBlock(0, 1, 2, 3)))
	lim := DefaultLimits
	lim.MaxBytes = 1 << 20
	lim.MaxTrackSamples = 1000
	f.Fuzz(func(t *testing.T, data []byte) {
		if _, err := Open(data, lim); err != nil {
			if e, ok := err.(*Error); !ok || e.Code == "" {
				t.Fatalf("an error with no code: %v", err)
			}
		}
	})
}

// A laced block nested in any master of the element tables is counted: the
// masters a hand-made list missed (BlockAdditionMapping 0x41E4, Slices 0x8E,
// BlockMore 0xA6, TimeSlice 0xE8, ...) each hold the bomb, 2000 laced blocks of
// 256 frames in a few kilobytes, and each is refused with video.limit.
func TestLacedBlocksInEveryMasterAreCounted(t *testing.T) {
	old := debug.SetMemoryLimit(512 << 20)
	defer debug.SetMemoryLimit(old)
	missed := []uint32{0x41E4, 0x8E, 0xA6, 0xE8, 0xE1, 0xE2, 0xE3, 0xE4, 0xE9, 0xDB, 0x80, 0x8F, 0x5854,
		0x55B0, 0x55D0, 0x7670, 0x6240, 0x6D80, 0x5034, 0x5035, 0x47E7, 0x63C0, 0x67C8, 0x6911, 0x6944, 0x6924, 0x6624}
	for _, id := range missed {
		var blocks [][]byte
		for i := 0; i < 2000; i++ {
			blocks = append(blocks, el(id, simpleBlock(laceFixed, 0xFF)))
		}
		_, err := Open(webm(blocks...), DefaultLimits)
		if e, ok := err.(*Error); !ok || e.Code != CodeLimit {
			t.Errorf("laced blocks in master %#x: %v", id, err)
		}
	}
}
