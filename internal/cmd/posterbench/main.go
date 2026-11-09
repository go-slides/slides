// Command posterbench times a poster frame: internal/video (Open and Poster,
// GOMAXPROCS 1) against `ffmpeg -ss t -i file -frames:v 1` (one thread), in
// interleaved rounds, and prints the medians and the allocations of ours.
//
//	posterbench file.mp4 seconds [rounds]
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"time"

	"github.com/go-slides/slides/internal/video"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: posterbench file seconds [rounds]")
		os.Exit(2)
	}
	runtime.GOMAXPROCS(1)
	file := os.Args[1]
	at, err := strconv.ParseFloat(os.Args[2], 64)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	rounds := 9
	if len(os.Args) > 3 {
		rounds, _ = strconv.Atoi(os.Args[3])
	}
	data, err := os.ReadFile(file)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	out := filepath.Join(os.TempDir(), "posterbench.jpg")
	var ours, ref []float64
	var allocs uint64
	for range rounds {
		var m0, m1 runtime.MemStats
		runtime.ReadMemStats(&m0)
		t0 := time.Now()
		info, err := video.Open(data, video.DefaultLimits)
		if err == nil {
			_, _, _, err = info.Poster(at, video.DefaultLimits)
		}
		d := time.Since(t0)
		runtime.ReadMemStats(&m1)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		ours, allocs = append(ours, d.Seconds()*1000), m1.Mallocs-m0.Mallocs

		t0 = time.Now()
		err = exec.Command("ffmpeg", "-v", "error", "-y", "-threads", "1", "-ss", strconv.FormatFloat(at, 'f', -1, 64), "-i", file, "-frames:v", "1", "-update", "1", out).Run()
		if err != nil {
			fmt.Fprintln(os.Stderr, "ffmpeg:", err)
			os.Exit(1)
		}
		ref = append(ref, time.Since(t0).Seconds()*1000)
	}
	med := func(v []float64) float64 { slices.Sort(v); return v[len(v)/2] }
	fmt.Printf("%s at %gs, %d rounds: ours %.1f ms (%d allocs), ffmpeg %.1f ms (process start included)\n", filepath.Base(file), at, rounds, med(ours), allocs, med(ref))
}
