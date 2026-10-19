package main

import (
	"bytes"
	"os"
	"strconv"
	"strings"
)

// On Linux the process list and each process's resident set come from
// /proc (proc(5)): the parent and start time from /proc/<pid>/stat, VmRSS
// from /proc/<pid>/status.

func residentBytes(pid int) (uint64, bool) {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/status")
	if err != nil {
		return 0, false
	}
	for _, line := range bytes.Split(b, []byte("\n")) {
		if v, ok := bytes.CutPrefix(line, []byte("VmRSS:")); ok {
			f := strings.Fields(string(v))
			if len(f) < 1 {
				return 0, false
			}
			kb, err := strconv.ParseUint(f[0], 10, 64)
			return kb * 1024, err == nil
		}
	}
	return 0, false
}

type procEntry struct {
	ppid  int
	start int64 // clock ticks since boot
}

func processTable() (map[int]procEntry, error) {
	ents, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	t := map[int]procEntry{}
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		b, err := os.ReadFile("/proc/" + e.Name() + "/stat")
		if err != nil {
			continue
		}
		// The command name is in parentheses and may hold spaces; the
		// fields after the last ')' start with the state (field 3).
		i := bytes.LastIndexByte(b, ')')
		if i < 0 {
			continue
		}
		f := strings.Fields(string(b[i+1:]))
		if len(f) < 20 {
			continue
		}
		ppid, _ := strconv.Atoi(f[1])               // field 4
		start, _ := strconv.ParseInt(f[19], 10, 64) // field 22, starttime
		t[pid] = procEntry{ppid: ppid, start: start}
	}
	return t, nil
}

func loadAverage() (float64, error) {
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, err
	}
	f := strings.Fields(string(b))
	if len(f) == 0 {
		return 0, os.ErrInvalid
	}
	return strconv.ParseFloat(f[0], 64)
}

// maxrssBytes converts ru_maxrss to bytes: getrusage(2) on Linux gives it
// in kilobytes.
func maxrssBytes(v int64) int64 { return v * 1024 }

func cpuModel() string {
	b, _ := os.ReadFile("/proc/cpuinfo")
	for _, line := range strings.Split(string(b), "\n") {
		if k, v, ok := strings.Cut(line, ":"); ok && strings.TrimSpace(k) == "model name" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func osRelease() string {
	b, _ := os.ReadFile("/proc/sys/kernel/osrelease")
	return "Linux " + strings.TrimSpace(string(b))
}

const memoryMethod = "sum of VmRSS (/proc/<pid>/status) over the process and every descendant, sampled every 10 ms"
