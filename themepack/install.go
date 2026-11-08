package themepack

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/go-slides/slides/diag"
)

// Dir is where installed packages live: go-slides/themes under the user's
// configuration directory (os.UserConfigDir).
func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "go-slides", "themes"), nil
}

// Store is a directory of installed packages, each <name>.slidestheme.
type Store struct {
	Dir    string
	Limits Limits
}

// UserStore is the store of the current user.
func UserStore() (*Store, error) {
	d, err := Dir()
	if err != nil {
		return nil, err
	}
	return &Store{Dir: d}, nil
}

func (s *Store) path(name string) (string, error) {
	// the name becomes a file name: only the theme-name alphabet reaches the disk
	if !ValidThemeName(name) {
		return "", fmt.Errorf("%q is not a theme name", name)
	}
	return filepath.Join(s.Dir, name+Extension), nil
}

// Install checks the package at file and stores it under its own name,
// replacing an installed one of that name. The copy is written whole and
// renamed into place.
func (s *Store) Install(file string) (*Meta, error) {
	p, err := Open(file, s.Limits)
	if err != nil {
		return nil, err
	}
	b, err := WriteFiles(p.Files)
	if err != nil {
		return nil, err
	}
	dst, err := s.path(p.Meta.Name)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp(s.Dir, ".install-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return nil, err
	}
	return p.Meta, os.Rename(tmp.Name(), dst)
}

// Open reads the installed package called name; ok is false when there is
// none.
func (s *Store) Open(name string) (p *Package, ok bool, err error) {
	file, err := s.path(name)
	if err != nil {
		return nil, false, nil
	}
	if _, err := os.Stat(file); err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	p, err = Open(file, s.Limits)
	if err != nil {
		return nil, true, err
	}
	if p.Meta.Name != name {
		return nil, true, diag.Errorf("theme.meta", "%s declares the name %q", file, p.Meta.Name)
	}
	return p, true, nil
}

// Names lists the installed packages' names, sorted, from the file names.
func (s *Store) Names() ([]string, error) {
	es, err := os.ReadDir(s.Dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range es {
		n, ok := strings.CutSuffix(e.Name(), Extension)
		if ok && !e.IsDir() && ValidThemeName(n) {
			names = append(names, n)
		}
	}
	slices.Sort(names)
	return names, nil
}

// List opens each installed package; one that fails the checks is returned
// with its error beside it, not hidden.
func (s *Store) List() ([]Entry, error) {
	names, err := s.Names()
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, n := range names {
		p, _, err := s.Open(n)
		e := Entry{Name: n, Err: err}
		if p != nil {
			e.Meta = p.Meta
		}
		out = append(out, e)
	}
	return out, nil
}

// Entry is one installed package, or why it cannot be read.
type Entry struct {
	Name string
	Meta *Meta
	Err  error
}

// Remove deletes the installed package called name; it reports whether there
// was one.
func (s *Store) Remove(name string) (bool, error) {
	file, err := s.path(name)
	if err != nil {
		return false, err
	}
	if err := os.Remove(file); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// maxExtendsDepth bounds a chain of packages that extend one another.
const maxExtendsDepth = 8

// Resolver returns the function theme.Set.Installed wants: the CSS of the
// installed package called name. A package that extends another installed
// one has it added first, through add (the set's Ensure). current is the
// go-slides version min_slides is compared to.
func (s *Store) Resolver(current string, ensure func(name string) error) func(string) (string, bool, error) {
	depth := 0
	return func(name string) (string, bool, error) {
		p, ok, err := s.Open(name)
		if err != nil || !ok {
			return "", ok, err
		}
		if err := p.CheckVersion(current); err != nil {
			return "", true, err
		}
		if p.Meta.Extends != "" && ensure != nil {
			depth++
			defer func() { depth-- }()
			if depth > maxExtendsDepth {
				return "", true, diag.Errorf("theme.meta", "theme %q: the chain of extends is longer than %d", name, maxExtendsDepth)
			}
			if err := ensure(p.Meta.Extends); err != nil {
				return "", true, err
			}
		}
		css, err := p.CSS()
		return css, true, err
	}
}
