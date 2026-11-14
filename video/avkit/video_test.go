package avkit_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image/jpeg"
	"os"
	"strings"
	"testing"

	. "github.com/go-slides/slides/video"
	_ "github.com/go-slides/slides/video/avkit"
)

func read(t testing.TB, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("../../testdata/video/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func code(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func TestPosterOfH264InMP4AndMatroska(t *testing.T) {
	for _, name := range []string{"bframes.mp4", "bframes.mkv"} {
		info, err := Open(read(t, name), DefaultLimits)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !info.Decodable || info.Codec != "avc1" || info.Width <= 0 {
			t.Fatalf("%s: %+v", name, info)
		}
		for _, at := range []float64{0, 0.3, 1000} { // 1000: past the end, the last picture
			jpg, w, h, err := info.Poster(at, DefaultLimits)
			if err != nil {
				t.Fatalf("%s at %g: %v", name, at, err)
			}
			img, err := jpeg.Decode(bytes.NewReader(jpg))
			if err != nil || img.Bounds().Dx() != w || img.Bounds().Dy() != h {
				t.Fatalf("%s at %g: not the JPEG it says (%v)", name, at, err)
			}
		}
	}
}

func TestPosterTimeIsRefusedWhenNegative(t *testing.T) {
	info, _ := Open(read(t, "bframes.mp4"), DefaultLimits)
	if _, _, _, err := info.Poster(-1, DefaultLimits); code(err) != CodeSyntax {
		t.Errorf("negative time: %v", err)
	}
}

func TestPosterOfOtherCodecsIsAPlaceholder(t *testing.T) {
	info, err := Open(read(t, "hvc1.mp4"), DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	if info.Decodable {
		t.Fatal("hvc1 is not decodable")
	}
	if _, _, _, err := info.Poster(0, DefaultLimits); code(err) != CodeCodec {
		t.Errorf("poster: %v", err)
	}
	jpg, w, h := info.Placeholder(DefaultLimits)
	if _, err := jpeg.Decode(bytes.NewReader(jpg)); err != nil || w <= 0 || h <= 0 {
		t.Errorf("placeholder: %v", err)
	}
	if _, w, h := PlaceholderSize(8000, 4000, DefaultLimits); w != 1280 || h != 640 {
		t.Errorf("placeholder size %dx%d", w, h)
	}
}

func TestRefusals(t *testing.T) {
	mp4 := read(t, "bframes.mp4")
	for name, tc := range map[string]struct {
		data []byte
		lim  func(*Limits)
		want string
	}{
		"not a video":   {[]byte("hello, this is text"), nil, CodeSyntax},
		"empty":         {nil, nil, CodeSyntax},
		"too big":       {mp4, func(l *Limits) { l.MaxBytes = 100 }, CodeLimit},
		"too many":      {mp4, func(l *Limits) { l.MaxBoxes = 3 }, CodeLimit},
		"too wide":      {mp4, func(l *Limits) { l.MaxWidth = 8 }, CodeLimit},
		"truncated mp4": {mp4[:40], nil, CodeSyntax},
	} {
		lim := DefaultLimits
		if tc.lim != nil {
			tc.lim(&lim)
		}
		if _, err := Open(tc.data, lim); code(err) != tc.want {
			t.Errorf("%s: %v, want %s", name, err, tc.want)
		}
	}
}

func box(typ string, body []byte) []byte {
	b := make([]byte, 8, 8+len(body))
	binary.BigEndian.PutUint32(b, uint32(8+len(body)))
	copy(b[4:], typ)
	return append(b, body...)
}

// A file of a million empty boxes, and one nested far deeper than any file is,
// are refused by the pre-scan, before the container reader builds anything.
func TestBoxBombs(t *testing.T) {
	ftyp := box("ftyp", []byte("isom\x00\x00\x02\x00isom"))
	many := append([]byte{}, ftyp...)
	for range 300000 {
		many = append(many, box("free", nil)...)
	}
	if _, err := Open(many, DefaultLimits); code(err) != CodeLimit || !strings.Contains(err.Error(), "boxes") {
		t.Errorf("many boxes: %v", err)
	}
	deep := box("free", nil)
	for range 40 {
		deep = box("moov", deep)
		deep = box("trak", deep)
	}
	if _, err := Open(append(append([]byte{}, ftyp...), deep...), DefaultLimits); code(err) != CodeLimit || !strings.Contains(err.Error(), "nest") {
		t.Errorf("deep boxes: %v", err)
	}
}

func TestEBMLBombs(t *testing.T) {
	head := []byte{0x1A, 0x45, 0xDF, 0xA3, 0x80}
	// Segment (unknown size) of 300000 empty Void elements (0xEC, size 0)
	data := append(append([]byte{}, head...), 0x18, 0x53, 0x80, 0x67, 0x01, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF)
	for range 300000 {
		data = append(data, 0xEC, 0x80)
	}
	if _, err := Open(data, DefaultLimits); code(err) != CodeLimit {
		t.Errorf("many elements: %v", err)
	}
	// nested Clusters (unknown size) 40 deep
	deep := append([]byte{}, head...)
	for range 40 {
		deep = append(deep, 0x1F, 0x43, 0xB6, 0x75, 0x01, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF)
	}
	if _, err := Open(deep, DefaultLimits); code(err) != CodeLimit {
		t.Errorf("deep elements: %v", err)
	}
}

func TestPosterWorkIsBounded(t *testing.T) {
	info, err := Open(read(t, "bframes.mp4"), DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	lim := DefaultLimits
	lim.MaxSamples = 1
	var refused bool
	for _, at := range []float64{0.4, 0.8, 1.2, 2} {
		if _, _, _, err := info.Poster(at, lim); code(err) == CodeLimit {
			refused = true
		}
	}
	if !refused {
		t.Error("a poster far from its keyframe is not refused at MaxSamples=1")
	}
}

func FuzzOpen(f *testing.F) {
	for _, n := range []string{"bframes.mp4", "bframes.mkv", "hvc1.mp4"} {
		f.Add(read(f, n))
	}
	lim := DefaultLimits
	lim.MaxBytes = 1 << 20
	f.Fuzz(func(t *testing.T, data []byte) {
		info, err := Open(data, lim)
		if err != nil {
			if code(err) == "" {
				t.Fatalf("an error with no code: %v", err)
			}
			return
		}
		if _, _, _, err := info.Poster(0.5, lim); err != nil && code(err) == "" {
			t.Fatalf("a poster error with no code: %v", err)
		}
	})
}
