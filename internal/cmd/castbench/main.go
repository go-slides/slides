// Command castbench times this repository's reading and replaying of an
// asciinema recording against agg (asciinema's own GIF generator), which
// parses the recording, runs its terminal emulator (avt) over every event and
// draws a frame. The two do not do the same work: agg draws a bitmap (and
// encodes a GIF) where this makes text, so its time is an upper bound for "the
// screen at time t" in agg's terms, not an equal. The rounds are interleaved,
// one core (GOMAXPROCS=1 here; agg's own threads are not pinned), and the
// median and the minimum are printed.
//
//	castbench -file big.cast -agg /opt/homebrew/bin/agg -rounds 7
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"time"

	"github.com/go-slides/slides/internal/cast"
)

func main() {
	file := flag.String("file", "", "the recording")
	aggPath := flag.String("agg", "", "path of agg (skipped when empty)")
	rounds := flag.Int("rounds", 7, "rounds")
	flag.Parse()
	if *file == "" {
		fmt.Fprintln(os.Stderr, "castbench: -file is required")
		os.Exit(2)
	}
	runtime.GOMAXPROCS(1)
	data, err := os.ReadFile(*file)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	parsed, err := cast.Parse(data, cast.Limits{})
	if err != nil {
		fmt.Fprintln(os.Stderr, "parse:", err)
		os.Exit(1)
	}
	dir, err := os.MkdirTemp("", "castbench")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer os.RemoveAll(dir)

	type step struct {
		name string
		run  func() error
		t    []time.Duration
	}
	steps := []*step{
		{name: "parse", run: func() error { _, err := cast.Parse(data, cast.Limits{}); return err }},
		{name: "screen at the end (emulate, no parse)", run: func() error { _, err := parsed.ScreenAt(-1, cast.Limits{}); return err }},
		{name: "parse + screen at the end + HTML", run: func() error {
			c, err := cast.Parse(data, cast.Limits{})
			if err != nil {
				return err
			}
			s, err := c.ScreenAt(-1, cast.Limits{})
			_ = s.HTML()
			return err
		}},
		{name: "parse + frames for the player + JSON", run: func() error {
			c, err := cast.Parse(data, cast.Limits{})
			if err != nil {
				return err
			}
			r, err := c.Replay(0, cast.Limits{})
			if err == nil {
				_ = r.JSON()
			}
			return err
		}},
	}
	if *aggPath != "" {
		steps = append(steps, &step{name: "agg --select 100% (parse, emulate, rasterise one frame, GIF)", run: func() error {
			return exec.Command(*aggPath, "--quiet", "--select", "100%", *file, filepath.Join(dir, "out.gif")).Run()
		}})
	}
	for r := 0; r < *rounds; r++ {
		for _, s := range steps {
			t0 := time.Now()
			if err := s.run(); err != nil {
				fmt.Fprintf(os.Stderr, "%s: %v\n", s.name, err)
				os.Exit(1)
			}
			s.t = append(s.t, time.Since(t0))
		}
	}
	fmt.Printf("%s: %d bytes, %d events, %.1f s\n", filepath.Base(*file), len(data), len(parsed.Events), parsed.Duration)
	for _, s := range steps {
		slices.Sort(s.t)
		fmt.Printf("%-66s median %9.1f ms   min %9.1f ms\n", s.name, ms(s.t[len(s.t)/2]), ms(s.t[0]))
	}
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }
