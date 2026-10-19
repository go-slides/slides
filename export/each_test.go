package export

import (
	"bytes"
	"context"
	"errors"
	"image"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

// TestOrderedBound checks that results come out in order and that no more
// than workers of them exist at once, even when emit is the slow side.
func TestOrderedBound(t *testing.T) {
	for _, workers := range []int{1, 3, 8} {
		var live, peak atomic.Int32
		var got []int
		err := ordered(context.Background(), 40, workers, func(_ context.Context, i int) (int, error) {
			n := live.Add(1)
			for {
				p := peak.Load()
				if n <= p || peak.CompareAndSwap(p, n) {
					break
				}
			}
			time.Sleep(time.Duration(40-i) * 50 * time.Microsecond) // later items finish first
			return i, nil
		}, func(i, v int) error {
			if i != v {
				t.Errorf("emit(%d, %d)", i, v)
			}
			got = append(got, v)
			time.Sleep(100 * time.Microsecond)
			live.Add(-1) // emitted: the result is no longer held
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 40 {
			t.Fatalf("workers %d: %d results", workers, len(got))
		}
		if p := peak.Load(); p > int32(workers) {
			t.Errorf("workers %d: %d results alive at once", workers, p)
		}
	}
}

// TestOrderedErrors checks that a failing item or emit stops the run, is
// returned, and leaves no goroutine behind.
func TestOrderedErrors(t *testing.T) {
	before := runtime.NumGoroutine()
	boom := errors.New("boom")
	err := ordered(context.Background(), 100, 4, func(ctx context.Context, i int) (int, error) {
		if i == 7 {
			return 0, boom
		}
		return i, nil
	}, func(int, int) error { return nil })
	if !errors.Is(err, boom) {
		t.Errorf("work error: %v", err)
	}
	err = ordered(context.Background(), 100, 4, func(_ context.Context, i int) (int, error) { return i, nil },
		func(i, _ int) error {
			if i == 3 {
				return boom
			}
			return nil
		})
	if !errors.Is(err, boom) {
		t.Errorf("emit error: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := ordered(ctx, 100, 4, func(_ context.Context, i int) (int, error) { return i, nil }, func(int, int) error { return nil }); err == nil {
		t.Error("a cancelled context must stop the run")
	}
	time.Sleep(10 * time.Millisecond)
	if after := runtime.NumGoroutine(); after > before {
		t.Errorf("%d goroutines before, %d after", before, after)
	}
}

// TestEachMatchesSerial renders the same deck with one worker and with
// several: the images must be identical, which also exercises one Engine
// rendering concurrently (run with -race).
func TestEachMatchesSerial(t *testing.T) {
	b := build(t, "---\ntheme: gaia\n---\n# One\n\n---\n\n## Two\n\n- a\n- b\n\n---\n\n![bg]("+bluePNG(t)+")\n\nthree\n\n---\n\n`four`\n")
	serial, err := Rasterize(context.Background(), b, Options{Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	parallel, err := Rasterize(context.Background(), b, Options{Workers: 4})
	if err != nil {
		t.Fatal(err)
	}
	for i := range serial {
		if !bytes.Equal(serial[i].Pix, parallel[i].Pix) {
			t.Errorf("slide %d differs between 1 and 4 workers", i+1)
		}
	}
}

func TestEachEncoded(t *testing.T) {
	b := build(t, "# One\n\n---\n\n## Two\n")
	imgs, err := Rasterize(context.Background(), b, Options{NoFonts: true})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	if err := EachEncoded(context.Background(), b, Options{NoFonts: true, Workers: 2}, "png", func(i int, data []byte) error {
		var want bytes.Buffer
		if err := PNG(&want, imgs[i]); err != nil {
			return err
		}
		if !bytes.Equal(data, want.Bytes()) {
			t.Errorf("slide %d: the PNG differs from encoding Each's image", i+1)
		}
		n++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("%d slides", n)
	}
	if err := EachEncoded(context.Background(), b, Options{}, "gif", func(int, []byte) error { return nil }); err == nil {
		t.Error("gif is not a format")
	}
}

// TestBundledFacesApply: gaia's text is set in the bundled Lato and Roboto
// Mono — the slide differs from the one rendered without them — and the
// default theme, which names neither, is not affected by them.
func TestBundledFacesApply(t *testing.T) {
	for _, c := range []struct {
		src    string
		differ bool
	}{
		{"---\ntheme: gaia\n---\n# Heading\n\ntext `code`\n", true},
		{"# Heading\n\ntext\n", false},
	} {
		b := build(t, c.src)
		with, err := Rasterize(context.Background(), b, Options{})
		if err != nil {
			t.Fatal(err)
		}
		without, err := Rasterize(context.Background(), b, Options{NoFonts: true})
		if err != nil {
			t.Fatal(err)
		}
		if got := !bytes.Equal(with[0].Pix, without[0].Pix); got != c.differ {
			t.Errorf("%q: the bundled fonts change the slide = %v, want %v", c.src, got, c.differ)
		}
	}
}

func TestImageOnlyFirst(t *testing.T) {
	b := build(t, "# A\n\n---\n\n# B\n")
	first := *b
	first.Slides = b.Slides[:1]
	n := 0
	if err := Each(context.Background(), &first, Options{NoFonts: true}, func(int, *image.RGBA) error { n++; return nil }); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("%d images", n)
	}
}
