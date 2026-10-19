//go:build unix

package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"syscall"
	"time"
)

// runCalibration measures processes whose memory and CPU time are known, to
// show what each figure covers: wait4's ru_maxrss is the largest single
// process (its own and the waited-for descendants'), not their sum, while
// the sampled tree sum adds up processes alive at the same time; wait4's
// CPU time includes descendants the process waited for.
func runCalibration() {
	self, err := os.Executable()
	if err != nil {
		fatal(err)
	}
	cases := []struct {
		name, expect string
		args         []string
	}{
		{"one process, 256 MiB, 500 ms CPU", "maxrss ≈ tree ≈ 256 MiB + runtime; CPU ≈ 500 ms",
			[]string{"-child-alloc", "256", "-child-burn", "500ms", "-child-hold", "300ms"}},
		{"one process, 64 MiB, no CPU", "maxrss ≈ tree ≈ 64 MiB + runtime; CPU ≈ 0",
			[]string{"-child-alloc", "64", "-child-hold", "300ms"}},
		{"64 MiB parent waiting on a 256 MiB child that burns 500 ms", "maxrss ≈ 256 MiB (the largest one); tree ≈ 64 + 256 MiB (both alive); CPU ≈ 500 ms (the child's included)",
			[]string{"-child-alloc", "64", "-child-grand"}},
	}
	for _, c := range cases {
		ps, wall, s, err := runSampled(self, c.args, os.TempDir(), io.Discard)
		if err != nil {
			fatal(err)
		}
		var cpu float64
		var maxrss int64
		if ru, ok := rusage(ps); ok {
			cpu = float64(ru.Utime.Nano()+ru.Stime.Nano()) / 1e6
			maxrss = maxrssBytes(int64(ru.Maxrss))
		}
		fmt.Printf("%s\n  expect: %s\n  read:   wall %.0f ms, CPU %.0f ms, ru_maxrss %.1f MiB, tree peak %.1f MiB over %d processes\n",
			c.name, c.expect, float64(wall.Microseconds())/1000, cpu, mib(maxrss), mib(s.peak), s.procs)
	}
}

// child is the calibration process: it optionally starts a 256 MiB
// grandchild that burns 500 ms of CPU and waits for it, while holding its
// own allocation.
func child(allocMiB int, burn, hold time.Duration, grand bool) {
	buf := make([]byte, allocMiB<<20)
	for i := 0; i < len(buf); i += 4096 {
		buf[i] = 1 // touch every page, so that it is resident
	}
	if grand {
		self, err := os.Executable()
		if err != nil {
			fatal(err)
		}
		// A plain wait, not runSampled: the sampler's own garbage would
		// grow this process past the 64 MiB it is meant to hold.
		cmd := exec.Command(self, "-child-alloc", "256", "-child-burn", "500ms", "-child-hold", "300ms")
		if err := cmd.Run(); err != nil {
			fatal(err)
		}
	}
	// Burn CPU time, not wall time: on a busy machine the two differ.
	x := 0
	for cpuTime() < burn {
		for i := range 1 << 16 {
			x += i * i
		}
	}
	time.Sleep(hold)
	runtime.KeepAlive(buf)
	io.WriteString(io.Discard, strconv.Itoa(x))
}

// cpuTime is this process's own user + system time.
func cpuTime() time.Duration {
	var ru syscall.Rusage
	if syscall.Getrusage(syscall.RUSAGE_SELF, &ru) != nil {
		return 1 << 62
	}
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}
