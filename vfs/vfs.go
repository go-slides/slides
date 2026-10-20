// Package vfs is the file system a build reads from: the files a deck
// includes, its kinds and data files, slides.yaml or slides.hcl, and theme
// CSS.
//
// The command reads the disk (OS). An editor reads its own buffers and the
// files it chose to hand over (Map), which keeps a deck that names
// /etc/passwd from reading it, and lets the same build run where there is no
// disk at all, in WebAssembly.
//
// Names are the paths the build forms with path/filepath, relative to the
// working directory or absolute; they are not io/fs's slash-separated,
// unrooted names, since a deck says "../shared/kinds.yaml".
package vfs

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// FS reads files.
type FS interface {
	// ReadFile returns a file's contents.
	ReadFile(name string) ([]byte, error)
	// Stat describes a file or a directory.
	Stat(name string) (fs.FileInfo, error)
	// ReadDir lists a directory, sorted by name.
	ReadDir(name string) ([]fs.DirEntry, error)
}

// Or returns f, or OS when f is nil: a nil FS in an options struct means the
// disk.
func Or(f FS) FS {
	if f == nil {
		return OS{}
	}
	return f
}

// OS is the disk.
type OS struct{}

// ReadFile calls os.ReadFile.
func (OS) ReadFile(name string) ([]byte, error) { return os.ReadFile(name) }

// Stat calls os.Stat.
func (OS) Stat(name string) (fs.FileInfo, error) { return os.Stat(name) }

// ReadDir calls os.ReadDir.
func (OS) ReadDir(name string) ([]fs.DirEntry, error) { return os.ReadDir(name) }

// Map is a file system held in memory: file names to contents. A name that
// is not there reads as fs.ErrNotExist and is remembered, so that a caller
// that has the files elsewhere (an editor's workspace) can hand them over
// and build again.
//
// Directories are implied by the files under them, and a directory can be
// declared empty with Dir. Map is safe for concurrent use.
type Map struct {
	mu      sync.Mutex
	files   map[string][]byte
	dirs    map[string]bool
	listed  map[string]bool
	missing map[string]bool
}

// NewMap returns a Map holding files, keyed by cleaned name.
func NewMap(files map[string][]byte) *Map {
	m := &Map{files: map[string][]byte{}, dirs: map[string]bool{}, listed: map[string]bool{}, missing: map[string]bool{}}
	for k, v := range files {
		m.files[filepath.Clean(k)] = v
	}
	return m
}

// Dir declares a directory, so that an empty one lists as empty instead of
// missing.
func (m *Map) Dir(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dirs[filepath.Clean(name)] = true
}

// Listed declares a file the caller has seen in a directory listing but not
// read: it lists and stats as a file (of unknown size, 0), and reading it
// records it as missing. A directory listing can then be handed over
// without the contents of every file in it.
func (m *Map) Listed(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listed[filepath.Clean(name)] = true
}

// Missing returns the names that were asked for and not found, sorted.
func (m *Map) Missing() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.missing))
	for k := range m.missing {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func (m *Map) miss(name string) error {
	m.missing[name] = true
	return &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}

// isDir reports whether name is a declared directory or holds a file. The
// lock is held.
func (m *Map) isDir(name string) bool {
	if m.dirs[name] {
		return true
	}
	prefix := name + string(filepath.Separator)
	for k := range m.files {
		if strings.HasPrefix(k, prefix) {
			return true
		}
	}
	for k := range m.listed {
		if strings.HasPrefix(k, prefix) {
			return true
		}
	}
	return false
}

// ReadFile returns a copy of a file's contents.
func (m *Map) ReadFile(name string) ([]byte, error) {
	name = filepath.Clean(name)
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.files[name]
	if !ok {
		if m.isDir(name) {
			return nil, &fs.PathError{Op: "read", Path: name, Err: errIsDir}
		}
		return nil, m.miss(name)
	}
	return slices.Clone(b), nil
}

var errIsDir = errors.New("is a directory")

// Stat describes a file or a directory.
func (m *Map) Stat(name string) (fs.FileInfo, error) {
	name = filepath.Clean(name)
	m.mu.Lock()
	defer m.mu.Unlock()
	if b, ok := m.files[name]; ok {
		return info{name: filepath.Base(name), size: int64(len(b))}, nil
	}
	if m.listed[name] {
		return info{name: filepath.Base(name)}, nil
	}
	if m.isDir(name) {
		return info{name: filepath.Base(name), dir: true}, nil
	}
	return nil, m.miss(name)
}

// ReadDir lists the files and directories right under name.
func (m *Map) ReadDir(name string) ([]fs.DirEntry, error) {
	name = filepath.Clean(name)
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.isDir(name) {
		return nil, m.miss(name)
	}
	prefix := name + string(filepath.Separator)
	seen := map[string]info{}
	for k, b := range m.files {
		rest, ok := strings.CutPrefix(k, prefix)
		if !ok {
			continue
		}
		if first, _, sub := strings.Cut(rest, string(filepath.Separator)); sub {
			seen[first] = info{name: first, dir: true}
		} else {
			seen[rest] = info{name: rest, size: int64(len(b))}
		}
	}
	for k := range m.listed {
		rest, ok := strings.CutPrefix(k, prefix)
		if !ok {
			continue
		}
		if first, _, sub := strings.Cut(rest, string(filepath.Separator)); sub {
			seen[first] = info{name: first, dir: true}
		} else if _, held := seen[rest]; !held {
			seen[rest] = info{name: rest}
		}
	}
	for d := range m.dirs {
		if rest, ok := strings.CutPrefix(d, prefix); ok && rest != "" {
			first, _, _ := strings.Cut(rest, string(filepath.Separator))
			seen[first] = info{name: first, dir: true}
		}
	}
	out := make([]fs.DirEntry, 0, len(seen))
	for _, i := range seen {
		out = append(out, fs.FileInfoToDirEntry(i))
	}
	slices.SortFunc(out, func(a, b fs.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	return out, nil
}

// info is a Map entry's fs.FileInfo.
type info struct {
	name string
	size int64
	dir  bool
}

func (i info) Name() string { return i.name }
func (i info) Size() int64  { return i.size }
func (i info) Mode() fs.FileMode {
	if i.dir {
		return fs.ModeDir | 0o555
	}
	return 0o444
}
func (i info) ModTime() time.Time { return time.Time{} }
func (i info) IsDir() bool        { return i.dir }
func (i info) Sys() any           { return nil }
