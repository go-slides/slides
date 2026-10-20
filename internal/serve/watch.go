package serve

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// maxWatchedDirs bounds the directories a watch holds open: inotify and its
// kin have a limit of their own, and a tree this size is not a deck's.
const maxWatchedDirs = 4096

// Watch calls onChange with the absolute paths that changed, until ctx is
// done. A target that is a directory is watched with its subdirectories (not
// the dot ones nor node_modules) and everything that changes under it is
// reported; a target that is a file is reported when it changes. The
// directory of a file is the one watched, since editors that replace a file
// by renaming another over it end the watch of the file itself.
//
// A burst of events is reported once, after debounce without a new one, and
// onChange is never called concurrently with itself. Names an editor writes
// on its way (.#x, x~, .x.swp) are not reported.
func Watch(ctx context.Context, targets []string, debounce time.Duration, onChange func(paths []string)) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer w.Close()

	files := map[string]bool{} // files asked for
	dirs := map[string]bool{}  // directories asked for, whole
	watched := 0
	var add func(dir string, recurse bool) error
	add = func(dir string, recurse bool) error {
		if watched >= maxWatchedDirs {
			return fmt.Errorf("watch: more than %d directories; watch a smaller tree", maxWatchedDirs)
		}
		if err := w.Add(dir); err != nil {
			return err
		}
		watched++
		if !recurse {
			return nil
		}
		des, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, de := range des {
			if de.IsDir() && !skipDir(de.Name()) {
				if err := add(filepath.Join(dir, de.Name()), true); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for _, t := range targets {
		abs, err := filepath.Abs(t)
		if err != nil {
			return err
		}
		st, err := os.Stat(abs)
		if err != nil {
			return err
		}
		if st.IsDir() {
			dirs[abs] = true
			if err := add(abs, true); err != nil {
				return err
			}
			continue
		}
		files[abs] = true
		if err := add(filepath.Dir(abs), false); err != nil && !errors.Is(err, fs.ErrExist) {
			return err
		}
	}

	// The paths go to coalesce, which owns the timing: it is the part a test
	// can run on a fake clock (testing/synctest), which the file system
	// events cannot.
	in := make(chan string)
	done := make(chan struct{})
	go func() {
		defer close(done)
		coalesce(in, debounce, onChange)
	}()
	defer func() {
		close(in)
		<-done // onChange is never called after Watch returns
	}()
	send := func(p string) bool {
		select {
		case in <- p:
			return true
		case <-ctx.Done():
			return false
		}
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case err, ok := <-w.Errors:
			if !ok {
				return nil
			}
			return err
		case ev, ok := <-w.Events:
			if !ok {
				return nil
			}
			p := filepath.Clean(ev.Name)
			if !wanted(p, files, dirs) {
				continue
			}
			if ev.Has(fsnotify.Create) {
				// a directory made under a watched one is watched too
				if st, err := os.Lstat(p); err == nil && st.IsDir() && !skipDir(filepath.Base(p)) && underDir(p, dirs) {
					_ = add(p, true)
					// what was created in it before the watch began
					var found []string
					filepath.WalkDir(p, func(q string, d fs.DirEntry, err error) error {
						if err == nil && !d.IsDir() && !hiddenName(d.Name()) {
							found = append(found, q)
						}
						return nil
					})
					for _, q := range found {
						if !send(q) {
							return nil
						}
					}
				}
			}
			if hiddenName(filepath.Base(p)) {
				continue
			}
			if !send(p) {
				return nil
			}
		}
	}
}

// coalesce calls onChange with the paths received on in, sorted and once
// each, when debounce has passed without a new one, until in is closed; what
// is pending then is dropped. It calls onChange from one goroutine, so never
// concurrently with itself.
func coalesce(in <-chan string, debounce time.Duration, onChange func(paths []string)) {
	pending := map[string]struct{}{}
	var timer *time.Timer
	var fire <-chan time.Time
	for {
		select {
		case p, ok := <-in:
			if !ok {
				if timer != nil {
					timer.Stop()
				}
				return
			}
			pending[p] = struct{}{}
			// Since Go 1.23 Reset leaves no stale tick in the channel.
			if timer == nil {
				timer = time.NewTimer(debounce)
			} else {
				timer.Reset(debounce)
			}
			fire = timer.C
		case <-fire:
			fire = nil
			paths := make([]string, 0, len(pending))
			for p := range pending {
				paths = append(paths, p)
			}
			clear(pending)
			slices.Sort(paths)
			onChange(paths)
		}
	}
}

// skipDir is the directories no deck is in.
func skipDir(name string) bool { return name == "node_modules" || strings.HasPrefix(name, ".") }

// hiddenName is the names an editor writes on its way and a dotfile.
func hiddenName(name string) bool {
	return strings.HasPrefix(name, ".") || strings.HasSuffix(name, "~")
}

// underDir reports whether p is inside one of the watched directories.
func underDir(p string, dirs map[string]bool) bool {
	for d := range dirs {
		if inside(d, p) {
			return true
		}
	}
	return false
}

// wanted says whether an event on p is one asked for: a file asked for, or
// anything below a directory asked for outside the directories that are
// skipped.
func wanted(p string, files, dirs map[string]bool) bool {
	if files[p] {
		return true
	}
	for d := range dirs {
		if !inside(d, p) {
			continue
		}
		rel, err := filepath.Rel(d, p)
		if err != nil {
			continue
		}
		skipped := false
		for _, seg := range strings.Split(filepath.ToSlash(filepath.Dir(rel)), "/") {
			if seg != "." && skipDir(seg) {
				skipped = true
			}
		}
		if !skipped {
			return true
		}
	}
	return false
}
