package video

import (
	"encoding/binary"
	"errors"
	"testing"
)

// fake is a Provider that records what it is asked and answers a fixed
// description: the seam is tested without any decoder.
type fake struct {
	probes, posters int
	probeErr        error
	posterErr       error
	got             Probed
}

func (f *fake) Probe(data []byte, format string, lim Limits) (Probed, error) {
	f.probes++
	if f.probeErr != nil {
		return Probed{}, f.probeErr
	}
	return Probed{Codec: "avc1", Width: 640, Height: 360, Duration: 2, Decodable: true, State: "s"}, nil
}

func (f *fake) Poster(p Probed, data []byte, t float64, lim Limits) ([]byte, int, int, error) {
	f.posters++
	f.got = p
	if f.posterErr != nil {
		return nil, 0, 0, f.posterErr
	}
	return []byte("jpeg"), 320, 180, nil
}

func ftyp() []byte {
	b := make([]byte, 16)
	binary.BigEndian.PutUint32(b, 16)
	copy(b[4:], "ftypisom")
	return b
}

func use(t *testing.T, p Provider) {
	t.Helper()
	Register(p)
	t.Cleanup(func() { Register(nil) })
}

func code(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func TestNoProviderIsUnavailable(t *testing.T) {
	Register(nil)
	if _, err := Open(ftyp(), DefaultLimits); code(err) != CodeUnavailable {
		t.Fatalf("no provider: %v", err)
	}
}

func TestProviderIsAskedAfterTheChecks(t *testing.T) {
	f := &fake{}
	use(t, f)
	if _, err := Open([]byte("not a video at all"), DefaultLimits); code(err) != CodeSyntax {
		t.Fatalf("not a video: %v", err)
	}
	lim := DefaultLimits
	lim.MaxBytes = 8
	if _, err := Open(ftyp(), lim); code(err) != CodeLimit {
		t.Fatalf("too large: %v", err)
	}
	if f.probes != 0 {
		t.Fatalf("the provider was asked %d times about files the checks refuse", f.probes)
	}
}

func TestProbeAndPosterGoThroughTheProvider(t *testing.T) {
	f := &fake{}
	use(t, f)
	file, err := Open(ftyp(), DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	if file.Format != "mp4" || file.MIME != "video/mp4" || file.Codec != "avc1" || file.Width != 640 || !file.Decodable {
		t.Fatalf("%+v", file.Info)
	}
	jpg, w, h, err := file.Poster(1, DefaultLimits)
	if err != nil || string(jpg) != "jpeg" || w != 320 || h != 180 {
		t.Fatalf("%q %d %d %v", jpg, w, h, err)
	}
	if f.got.State != "s" {
		t.Errorf("the poster was not handed the probe's state: %+v", f.got)
	}
}

func TestProviderErrorsCarryACode(t *testing.T) {
	f := &fake{probeErr: errors.New("plain")}
	use(t, f)
	if _, err := Open(ftyp(), DefaultLimits); code(err) != CodeSyntax {
		t.Fatalf("a plain error is video.syntax: %v", err)
	}
	f.probeErr = Fail(CodeLimit, "too big")
	if _, err := Open(ftyp(), DefaultLimits); code(err) != CodeLimit {
		t.Fatalf("a coded error is kept: %v", err)
	}
	f.probeErr = nil
	file, err := Open(ftyp(), DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	f.posterErr = Fail(CodeLimit, "costly")
	if _, _, _, err := file.Poster(0, DefaultLimits); code(err) != CodeLimit {
		t.Fatalf("poster: %v", err)
	}
}

func TestFrameOverTheLimitIsRefusedWhateverTheProvider(t *testing.T) {
	use(t, &fake{})
	lim := DefaultLimits
	lim.MaxWidth = 100
	if _, err := Open(ftyp(), lim); code(err) != CodeLimit {
		t.Fatalf("frame: %v", err)
	}
}
