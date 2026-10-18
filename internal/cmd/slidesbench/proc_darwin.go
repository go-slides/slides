package main

import (
	"encoding/binary"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// The process list comes from sysctl kern.proc.all (public); each process's
// memory from proc_info(2), the system call behind libproc's proc_pidinfo,
// called directly so that the harness needs no cgo.
//
// The flavour PROC_PIDTASKINFO (4) and the layout of struct proc_taskinfo are
// those of <sys/proc_info.h> in the macOS SDK. The call number
// PROC_INFO_CALL_PIDINFO (2) is not in a public header: it is XNU's, and
// `slidesbench -calibrate` is what shows that it reads what it should (a
// process that touches 256 MiB is read at 256 MiB, matching wait4's
// ru_maxrss for the same process).
const (
	procInfoCallPIDInfo = 2
	procPIDTaskInfo     = 4
	procTaskInfoSize    = 96 // 6 uint64 + 12 int32
)

// residentBytes is pti_resident_size: the process's resident set, which
// counts a page shared with another process (a framework, Chrome's shared
// memory between its processes) in each of them.
func residentBytes(pid int) (uint64, bool) {
	var buf [procTaskInfoSize]byte
	n, _, errno := syscall.Syscall6(syscall.SYS_PROC_INFO, procInfoCallPIDInfo, uintptr(pid), procPIDTaskInfo, 0,
		uintptr(unsafe.Pointer(&buf[0])), procTaskInfoSize)
	if errno != 0 || int(n) != procTaskInfoSize {
		return 0, false
	}
	return binary.LittleEndian.Uint64(buf[8:16]), true // pti_resident_size, after pti_virtual_size
}

// procEntry is one process: its parent, and its start time, which tells a
// process from a later one that reused its PID.
type procEntry struct {
	ppid  int
	start int64 // microseconds since the epoch
}

func processTable() (map[int]procEntry, error) {
	kps, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return nil, err
	}
	t := make(map[int]procEntry, len(kps))
	for i := range kps {
		p := &kps[i]
		st := p.Proc.P_starttime
		t[int(p.Proc.P_pid)] = procEntry{ppid: int(p.Eproc.Ppid), start: st.Sec*1e6 + int64(st.Usec)}
	}
	return t, nil
}

// loadAverage is the 1-minute load average (sysctl vm.loadavg: struct
// loadavg { fixpt_t ldavg[3]; long fscale; }).
func loadAverage() (float64, error) {
	b, err := unix.SysctlRaw("vm.loadavg")
	if err != nil {
		return 0, err
	}
	if len(b) < 24 {
		return 0, syscall.EINVAL
	}
	ld := binary.LittleEndian.Uint32(b[0:4])
	scale := binary.LittleEndian.Uint64(b[16:24])
	if scale == 0 {
		return 0, syscall.EINVAL
	}
	return float64(ld) / float64(scale), nil
}

// maxrssBytes converts ru_maxrss to bytes: getrusage(2) on macOS gives it
// in bytes.
func maxrssBytes(v int64) int64 { return v }

func cpuModel() string {
	s, _ := unix.Sysctl("machdep.cpu.brand_string")
	return s
}

func osRelease() string {
	s, _ := unix.Sysctl("kern.osproductversion")
	if s == "" {
		s, _ = unix.Sysctl("kern.osrelease")
	}
	return "macOS " + s
}

const memoryMethod = "sum of pti_resident_size (proc_info PROC_PIDTASKINFO) over the process and every descendant, sampled every 10 ms"
