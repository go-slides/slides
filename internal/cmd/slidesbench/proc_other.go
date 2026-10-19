//go:build unix && !darwin && !linux

package main

import "errors"

// Elsewhere the harness cannot see a process tree: the sampled peak stays
// zero and only wait4's ru_maxrss is reported.

var errUnsupported = errors.New("slidesbench: process sampling is implemented for darwin and linux only")

func residentBytes(int) (uint64, bool) { return 0, false }

type procEntry struct {
	ppid  int
	start int64
}

func processTable() (map[int]procEntry, error) { return nil, errUnsupported }

func loadAverage() (float64, error) { return 0, errUnsupported }

func maxrssBytes(v int64) int64 { return v * 1024 } // kilobytes on the BSDs

func cpuModel() string { return "" }

func osRelease() string { return "" }

const memoryMethod = "not sampled on this OS"
