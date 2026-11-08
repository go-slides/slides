package theme

import (
	"errors"
	"sync"
	"testing"
)

func TestEnsureAddsAnInstalledTheme(t *testing.T) {
	calls := 0
	s := NewSet()
	s.Installed = func(name string) (string, bool, error) {
		calls++
		switch name {
		case "acme":
			return "/* @theme acme */\nsection{color:red}", true, nil
		case "broken":
			return "", true, errors.New("broken package")
		case "wrong":
			return "/* @theme other */", true, nil
		}
		return "", false, nil
	}
	if err := s.Ensure("acme"); err != nil || !s.Has("acme") {
		t.Fatalf("acme: %v %v", err, s.Has("acme"))
	}
	if err := s.Ensure("acme"); err != nil || calls != 1 {
		t.Fatalf("a theme in the set is looked up again: %d calls, %v", calls, err)
	}
	if err := s.Ensure("default"); err != nil || calls != 1 {
		t.Fatalf("a built-in was looked up: %d calls", calls)
	}
	if err := s.Ensure("nope"); err != nil || s.Has("nope") {
		t.Fatalf("an unknown name is not an error: %v", err)
	}
	if err := s.Ensure("broken"); err == nil {
		t.Fatal("a broken package was taken for an unknown name")
	}
	if err := s.Ensure("wrong"); err == nil {
		t.Fatal("a package that declares another name was accepted")
	}
	if err := s.Ensure(""); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureWithoutInstalled(t *testing.T) {
	if err := NewSet().Ensure("acme"); err != nil {
		t.Fatal(err)
	}
}

// Ensure while others read: run with -race.
func TestEnsureConcurrently(t *testing.T) {
	s := NewSet()
	s.Installed = func(name string) (string, bool, error) {
		return "/* @theme " + name + " */\nsection{color:red}", true, nil
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, n := range []string{"a", "b", "c"} {
				if err := s.Ensure(n); err != nil {
					t.Error(err)
				}
				_ = s.Has(n)
				_ = s.Names()
				_ = s.Resolve(n)
			}
		}()
	}
	wg.Wait()
}
