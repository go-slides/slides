//go:build unix

// Command slidesbench measures go-slides against marp-cli on the same decks,
// as subprocesses, the way a user runs them: wall time, CPU time, peak memory
// of the whole process tree (marp drives a Chrome that is its child) and the
// size of what each writes. See BENCHMARKS.md for the method and the results.
//
//	go run ./internal/cmd/slidesbench -rounds 5 -json results.json
//	go run ./internal/cmd/slidesbench -calibrate
//
// It refuses to measure on a loaded machine (-maxload, the 1-minute load
// average): numbers taken under load are meaningless.
package main

import (
	"bytes"
	"cmp"
	"context"
	"debug/buildinfo"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/go-slides/slides/internal/benchdeck"
)

func main() {
	var (
		rounds    = flag.Int("rounds", 5, "measured rounds (one warm-up round is run first and discarded)")
		modes     = flag.String("modes", "html,pdf,png,pptx", "comma-separated modes: html, pdf, png (every slide), pptx")
		decks     = flag.String("decks", "", "comma-separated deck names to keep (default: all)")
		oracle    = flag.String("oracle", "testdata/oracle", "directory of the oracle decks")
		slidesBin = flag.String("slides", "", "go-slides binary (default: built from ./cmd/slides)")
		baseBin   = flag.String("baseline", "", "an older go-slides binary to measure alongside, as go-slides@base (before/after an optimisation)")
		marpBin   = flag.String("marp", "marp", "marp-cli executable")
		work      = flag.String("work", "", "working directory for decks and outputs (default: a new temporary directory, removed at the end)")
		jsonOut   = flag.String("json", "", "write every run and the summary as JSON to this file")
		maxLoad   = flag.Float64("maxload", 8, "refuse to measure while the 1-minute load average is above this")
		wait      = flag.Duration("wait", 30*time.Minute, "how long to wait for the load to fall before giving up")
		calibrate = flag.Bool("calibrate", false, "measure processes of known size and CPU time, to check what the harness reads")

		childAlloc = flag.Int("child-alloc", 0, "(calibration child) MiB to allocate and touch")
		childBurn  = flag.Duration("child-burn", 0, "(calibration child) CPU time to burn")
		childHold  = flag.Duration("child-hold", 0, "(calibration child) time to hold the memory")
		childGrand = flag.Bool("child-grand", false, "(calibration child) start a grandchild of 256 MiB first")
	)
	flag.Parse()
	if *childAlloc > 0 || *childBurn > 0 || *childGrand {
		child(*childAlloc, *childBurn, *childHold, *childGrand)
		return
	}
	if err := waitForQuiet(*maxLoad, *wait); err != nil {
		fatal(err)
	}
	if *calibrate {
		runCalibration()
		return
	}
	if err := bench(*rounds, splitList(*modes), splitList(*decks), *oracle, *slidesBin, *baseBin, *marpBin, *work, *jsonOut, *maxLoad, *wait); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "slidesbench:", err)
	os.Exit(1)
}

func splitList(s string) []string {
	var out []string
	for _, f := range strings.Split(s, ",") {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}

// waitForQuiet waits until the 1-minute load average is at most max.
func waitForQuiet(max float64, wait time.Duration) error {
	deadline := time.Now().Add(wait)
	for {
		l, err := loadAverage()
		if err != nil {
			return fmt.Errorf("cannot read the load average: %w", err)
		}
		if l <= max {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the 1-minute load average is %.1f, above %.1f, after waiting %v: not measuring on a loaded machine", l, max, wait)
		}
		fmt.Fprintf(os.Stderr, "slidesbench: load average %.1f > %.1f, waiting\n", l, max)
		time.Sleep(30 * time.Second)
	}
}

// Run is one measured execution.
type Run struct {
	Deck  string `json:"deck"`
	Mode  string `json:"mode"`
	Tool  string `json:"tool"`
	Round int    `json:"round"` // 0 is the warm-up
	// WallMS is the time from start to exit.
	WallMS float64 `json:"wall_ms"`
	// CPUMS is ru_utime + ru_stime from wait4: the process and the
	// descendants it waited for (see -calibrate).
	CPUMS float64 `json:"cpu_ms"`
	// MaxRSS is ru_maxrss from wait4, in bytes: the largest single process
	// among the process and the descendants it waited for, not their sum.
	MaxRSS int64 `json:"maxrss_bytes"`
	// TreePeak is the largest sum of resident sets over the process and
	// all its descendants, sampled every 10 ms.
	TreePeak int64 `json:"tree_peak_bytes"`
	// TreeProcs is the most processes seen at once in the tree.
	TreeProcs int `json:"tree_procs"`
	// OutBytes is the total size of the files written, in Files files.
	OutBytes int64 `json:"out_bytes"`
	Files    int   `json:"files"`
	// Leaked counts descendants still alive after the process exited,
	// which the harness then killed by PID.
	Leaked int     `json:"leaked,omitempty"`
	Load   float64 `json:"load"`
	Err    string  `json:"error,omitempty"`
}

// Peak is the memory figure reported: the sampled tree sum, or ru_maxrss
// when the process lived too briefly for a sample to see its peak.
func (r Run) Peak() int64 { return max(r.TreePeak, r.MaxRSS) }

type deckFile struct{ name, path string }

func bench(rounds int, modes, keep []string, oracle, slidesBin, baseBin, marpBin, work, jsonOut string, maxLoad float64, wait time.Duration) error {
	if rounds < 1 {
		return errors.New("-rounds must be at least 1")
	}
	for _, m := range modes {
		if m != "html" && m != "pdf" && m != "png" && m != "pptx" {
			return fmt.Errorf("unknown mode %q", m)
		}
	}
	if work == "" {
		dir, err := os.MkdirTemp("", "slidesbench-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(dir)
		work = dir
	}
	work, err := filepath.Abs(work)
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "slidesbench: working in", work)
	if slidesBin == "" {
		slidesBin = filepath.Join(work, "slides")
		build := exec.Command("go", "build", "-o", slidesBin, "./cmd/slides")
		build.Stdin, build.Stdout, build.Stderr = nil, os.Stderr, os.Stderr
		if err := build.Run(); err != nil {
			return fmt.Errorf("building go-slides: %w", err)
		}
	}
	if slidesBin, err = filepath.Abs(slidesBin); err != nil {
		return err
	}
	if p, err := exec.LookPath(marpBin); err == nil {
		marpBin = p
	} else {
		return fmt.Errorf("marp: %w", err)
	}
	ds, err := corpus(oracle, work, keep)
	if err != nil {
		return err
	}
	env := environment(slidesBin, marpBin)
	tools := []tool{{"go-slides", slidesBin, slidesArgs}, {"marp", marpBin, marpArgs}}
	if baseBin != "" {
		if baseBin, err = filepath.Abs(baseBin); err != nil {
			return err
		}
		tools = append(tools, tool{"go-slides@base", baseBin, slidesArgs})
		if bi, err := buildinfo.ReadFile(baseBin); err == nil {
			env.BaselineVersion = bi.Main.Version
		}
	}

	var runs []Run
	for round := 0; round <= rounds; round++ {
		if err := waitForQuiet(maxLoad, wait); err != nil {
			return err
		}
		for _, d := range ds {
			for _, m := range modes {
				// Interleave, and reverse the order every other round, so
				// that a drift in the machine's state falls on every tool.
				order := slices.Clone(tools)
				if round%2 == 1 {
					slices.Reverse(order)
				}
				for _, t := range order {
					r := measure(t, d, m, work)
					r.Round = round
					runs = append(runs, r)
					status := ""
					if r.Err != "" {
						status = " ERROR " + r.Err
					}
					fmt.Fprintf(os.Stderr, "round %d %-22s %-4s %-9s %9.0f ms %8.1f MiB%s\n", round, d.name, m, t.name, r.WallMS, mib(r.Peak()), status)
				}
			}
		}
	}
	sum := summarize(runs)
	fmt.Print(markdown(env, sum, rounds))
	if jsonOut != "" {
		b, err := json.MarshalIndent(map[string]any{"environment": env, "runs": runs, "summary": sum}, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(jsonOut, b, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// corpus writes the decks into work/decks: the oracle decks (the ones
// marp-cli can read: not the *.src.md sources of go-slides' own kinds) and
// the generated ones.
func corpus(oracle, work string, keep []string) ([]deckFile, error) {
	dir := filepath.Join(work, "decks")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	var ds []deckFile
	add := func(name string, src []byte) error {
		if len(keep) > 0 && !slices.Contains(keep, name) {
			return nil
		}
		p := filepath.Join(dir, strings.ReplaceAll(name, "/", "-")+".md")
		ds = append(ds, deckFile{name, p})
		return os.WriteFile(p, src, 0o644)
	}
	if err := add("generated-100", benchdeck.Large(100)); err != nil {
		return nil, err
	}
	if err := add("gaia-20", benchdeck.Gaia(20)); err != nil {
		return nil, err
	}
	if err := add("math-code-20", benchdeck.MathCode(20)); err != nil {
		return nil, err
	}
	ms, err := filepath.Glob(filepath.Join(oracle, "*.md"))
	if err != nil {
		return nil, err
	}
	for _, m := range ms {
		if strings.HasSuffix(m, ".src.md") {
			continue
		}
		src, err := os.ReadFile(m)
		if err != nil {
			return nil, err
		}
		if err := add("oracle/"+strings.TrimSuffix(filepath.Base(m), ".md"), src); err != nil {
			return nil, err
		}
	}
	if len(ds) == 0 {
		return nil, errors.New("no deck to measure")
	}
	return ds, nil
}

type tool struct {
	name string
	bin  string
	args func(deck, out, mode string) []string
}

func slidesArgs(deck, out, mode string) []string {
	switch mode {
	case "pdf":
		return []string{"build", "--no-config", "--pdf", "-o", out + ".pdf", deck}
	case "png":
		return []string{"build", "--no-config", "--images", "png", "-o", out + ".png", deck}
	case "pptx":
		return []string{"build", "--no-config", "--pptx", "-o", out + ".pptx", deck}
	}
	return []string{"build", "--no-config", "-o", out + ".html", deck}
}

// marpArgs asks marp for what go-slides writes: go-slides' PDF carries an
// outline, so marp is given --pdf-outlines. Both tools render PPTX at their
// default scale, 2.
func marpArgs(deck, out, mode string) []string {
	switch mode {
	case "pdf":
		return []string{"--no-config-file", "--pdf", "--pdf-outlines", "-o", out + ".pdf", deck}
	case "png":
		return []string{"--no-config-file", "--images", "png", "-o", out + ".png", deck}
	case "pptx":
		return []string{"--no-config-file", "--pptx", "-o", out + ".pptx", deck}
	}
	return []string{"--no-config-file", "-o", out + ".html", deck}
}

func measure(t tool, d deckFile, mode, work string) Run {
	r := Run{Deck: d.name, Mode: mode, Tool: t.name}
	r.Load, _ = loadAverage()
	outDir := filepath.Join(work, "out", t.name, strings.ReplaceAll(d.name, "/", "_"), mode)
	if err := os.RemoveAll(outDir); err != nil {
		r.Err = err.Error()
		return r
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		r.Err = err.Error()
		return r
	}
	var stderr bytes.Buffer
	ps, wall, s, err := runSampled(t.bin, t.args(d.path, filepath.Join(outDir, "deck"), mode), outDir, &stderr)
	r.WallMS = float64(wall.Microseconds()) / 1000
	r.TreePeak, r.TreeProcs, r.Leaked = s.peak, s.procs, s.leaked
	if ru, ok := rusage(ps); ok {
		r.CPUMS = float64(ru.Utime.Nano()+ru.Stime.Nano()) / 1e6
		r.MaxRSS = maxrssBytes(int64(ru.Maxrss))
	}
	if err != nil {
		r.Err = strings.TrimSpace(err.Error() + ": " + lastLine(stderr.String()))
		return r
	}
	_ = filepath.WalkDir(outDir, func(_ string, e fs.DirEntry, err error) error {
		if err == nil && e.Type().IsRegular() {
			if fi, err := e.Info(); err == nil {
				r.OutBytes += fi.Size()
				r.Files++
			}
		}
		return nil
	})
	if r.Files == 0 {
		r.Err = "wrote nothing: " + lastLine(stderr.String())
	}
	return r
}

// rusage is what wait4 returned for an exited process (getrusage(2)'s
// struct rusage).
func rusage(ps *os.ProcessState) (*syscall.Rusage, bool) {
	if ps == nil {
		return nil, false
	}
	ru, ok := ps.SysUsage().(*syscall.Rusage)
	return ru, ok
}

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	return s
}

type sample struct {
	peak   int64
	procs  int
	leaked int
}

// runSampled runs a command with standard input from /dev/null (marp-cli
// waits for standard input otherwise), samples its process tree until it
// exits, and kills — by PID, after checking the start time — any
// descendant still alive afterwards.
func runSampled(bin string, args []string, dir string, stderr io.Writer) (*os.ProcessState, time.Duration, sample, error) {
	var s sample
	devnull, err := os.Open(os.DevNull)
	if err != nil {
		return nil, 0, s, err
	}
	defer devnull.Close()
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Stdin, cmd.Stdout, cmd.Stderr = devnull, io.Discard, stderr
	start := time.Now()
	if err := cmd.Start(); err != nil {
		return nil, 0, s, err
	}
	root := cmd.Process.Pid
	seen := map[int]int64{} // descendant PID -> start time
	ctx, stop := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Go(func() {
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		for {
			t, err := processTable()
			if err == nil {
				tree := descendants(t, root)
				var sum int64
				n := 0
				if rss, ok := residentBytes(root); ok {
					sum += int64(rss)
					n++
				}
				for _, pid := range tree {
					if _, ok := seen[pid]; !ok {
						seen[pid] = t[pid].start
					}
					if rss, ok := residentBytes(pid); ok {
						sum += int64(rss)
						n++
					}
				}
				s.peak = max(s.peak, sum)
				s.procs = max(s.procs, n)
			}
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
		}
	})
	err = cmd.Wait()
	wall := time.Since(start)
	stop()
	wg.Wait()
	if t, terr := processTable(); terr == nil {
		for pid, st := range seen {
			if e, ok := t[pid]; ok && e.start == st {
				if syscall.Kill(pid, syscall.SIGKILL) == nil {
					s.leaked++
				}
			}
		}
	}
	return cmd.ProcessState, wall, s, err
}

// descendants lists every process below root.
func descendants(t map[int]procEntry, root int) []int {
	children := map[int][]int{}
	for pid, e := range t {
		children[e.ppid] = append(children[e.ppid], pid)
	}
	var out []int
	queue := []int{root}
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		for _, c := range children[p] {
			if c != p {
				out = append(out, c)
				queue = append(queue, c)
			}
		}
	}
	return out
}

// Stat is the median and range of one measure over the rounds.
type Stat struct{ Median, Min, Max float64 }

func stat(v []float64) Stat {
	if len(v) == 0 {
		return Stat{}
	}
	s := slices.Clone(v)
	slices.Sort(s)
	m := s[len(s)/2]
	if len(s)%2 == 0 {
		m = (s[len(s)/2-1] + s[len(s)/2]) / 2
	}
	return Stat{m, s[0], s[len(s)-1]}
}

// Summary is one deck, mode and tool over the measured rounds.
type Summary struct {
	Deck, Mode, Tool string
	Rounds           int
	WallMS, CPUMS    Stat
	PeakMiB          Stat // the tree's sampled sum (Run.Peak)
	LargestMiB       Stat // the largest single process (ru_maxrss)
	OutBytes         int64
	Files            int
	Errors           []string `json:",omitempty"`
	Leaked           int      `json:",omitempty"`
}

func summarize(runs []Run) []Summary {
	type key struct{ deck, mode, tool string }
	var order []key
	groups := map[key][]Run{}
	for _, r := range runs {
		k := key{r.Deck, r.Mode, r.Tool}
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], r)
	}
	var out []Summary
	for _, k := range order {
		s := Summary{Deck: k.deck, Mode: k.mode, Tool: k.tool}
		var wall, cpu, peak, largest []float64
		for _, r := range groups[k] {
			s.Leaked += r.Leaked
			if r.Round == 0 {
				continue // warm-up
			}
			if r.Err != "" {
				s.Errors = append(s.Errors, r.Err)
				continue
			}
			s.Rounds++
			wall = append(wall, r.WallMS)
			cpu = append(cpu, r.CPUMS)
			peak = append(peak, mib(r.Peak()))
			largest = append(largest, mib(r.MaxRSS))
			s.OutBytes, s.Files = r.OutBytes, r.Files
		}
		s.WallMS, s.CPUMS, s.PeakMiB, s.LargestMiB = stat(wall), stat(cpu), stat(peak), stat(largest)
		out = append(out, s)
	}
	return out
}

func mib(b int64) float64 { return float64(b) / (1 << 20) }

// Environment is what the numbers were measured on.
type Environment struct {
	Date          string
	OS            string
	CPU           string
	NumCPU        int
	GOMAXPROCS    string
	HarnessGo     string
	SlidesGo      string
	SlidesVersion string
	// BaselineVersion is the version of the -baseline binary, if any.
	BaselineVersion string `json:",omitempty"`
	Marp            string
	Chrome          string
	Memory          string
	LoadAtStart     float64
}

func environment(slidesBin, marpBin string) Environment {
	e := Environment{
		Date:       time.Now().Format(time.RFC3339),
		OS:         osRelease(),
		CPU:        cpuModel(),
		NumCPU:     runtime.NumCPU(),
		GOMAXPROCS: cmp.Or(os.Getenv("GOMAXPROCS"), fmt.Sprintf("unset (Go's default: %d)", runtime.NumCPU())),
		HarnessGo:  runtime.Version(),
		Memory:     memoryMethod,
		Chrome:     chromeVersion(),
	}
	e.LoadAtStart, _ = loadAverage()
	if bi, err := buildinfo.ReadFile(slidesBin); err == nil {
		e.SlidesGo = bi.GoVersion
		e.SlidesVersion = bi.Main.Version
		for _, s := range bi.Settings {
			if s.Key == "vcs.revision" {
				e.SlidesVersion += " " + s.Value
			}
		}
	}
	if devnull, err := os.Open(os.DevNull); err == nil {
		cmd := exec.Command(marpBin, "--version")
		cmd.Stdin = devnull // marp-cli waits for standard input otherwise
		b, _ := cmd.Output()
		devnull.Close()
		e.Marp = strings.TrimSpace(string(b))
	}
	return e
}

// chromeVersion reads the version of the Chrome marp-cli finds on macOS
// from its Info.plist, without starting it.
func chromeVersion() string {
	b, err := os.ReadFile("/Applications/Google Chrome.app/Contents/Info.plist")
	if err != nil {
		return ""
	}
	s := string(b)
	_, after, ok := strings.Cut(s, "<key>CFBundleShortVersionString</key>")
	if !ok {
		return ""
	}
	_, after, ok = strings.Cut(after, "<string>")
	if !ok {
		return ""
	}
	v, _, _ := strings.Cut(after, "</string>")
	return "Google Chrome " + v
}

func markdown(e Environment, sum []Summary, rounds int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Measured %s on %s, %s (%d CPUs), load average %.1f at start.\n", e.Date, e.OS, e.CPU, e.NumCPU, e.LoadAtStart)
	fmt.Fprintf(&b, "go-slides %s built with %s, GOMAXPROCS %s; %s; %s.\n", e.SlidesVersion, e.SlidesGo, e.GOMAXPROCS, strings.ReplaceAll(e.Marp, "\n", " "), e.Chrome)
	if e.BaselineVersion != "" {
		fmt.Fprintf(&b, "go-slides@base is %s.\n", e.BaselineVersion)
	}
	fmt.Fprintf(&b, "Median of %d rounds after a discarded warm-up, [min–max]. Memory: %s.\n\n", rounds, e.Memory)
	b.WriteString("| Deck | Mode | Tool | Wall (ms) | CPU (ms) | Tree peak (MiB) | Largest process (MiB) | Output |\n")
	b.WriteString("|---|---|---|---:|---:|---:|---:|---:|\n")
	for _, s := range sum {
		out := size(s.OutBytes)
		if s.Files > 1 {
			out += fmt.Sprintf(" in %d files", s.Files)
		}
		if len(s.Errors) > 0 {
			out += fmt.Sprintf(" (%d errors: %s)", len(s.Errors), s.Errors[0])
		}
		if s.Leaked > 0 {
			out += fmt.Sprintf(" (%d processes left behind, killed)", s.Leaked)
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %.0f [%.0f–%.0f] | %.0f | %.0f [%.0f–%.0f] | %.0f | %s |\n",
			s.Deck, s.Mode, s.Tool, s.WallMS.Median, s.WallMS.Min, s.WallMS.Max, s.CPUMS.Median,
			s.PeakMiB.Median, s.PeakMiB.Min, s.PeakMiB.Max, s.LargestMiB.Median, out)
	}
	return b.String()
}

func size(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}
