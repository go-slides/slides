package lru

import (
	"strconv"
	"sync"
	"testing"
)

func TestCacheEvictsLeastRecentlyUsed(t *testing.T) {
	c := New[int](2)
	a, b, d := KeyOf("a"), KeyOf("b"), KeyOf("d")
	c.Put(a, 1)
	c.Put(b, 2)
	if v, ok := c.Get(a); !ok || v != 1 { // a is now the most recent
		t.Fatal("a missing")
	}
	c.Put(d, 3) // evicts b
	if _, ok := c.Get(b); ok {
		t.Fatal("b should have been evicted")
	}
	if v, _ := c.Get(a); v != 1 {
		t.Fatal("a evicted instead of b")
	}
	c.Put(a, 9)
	if v, _ := c.Get(a); v != 9 || c.Len() != 2 {
		t.Fatalf("update: %d, len %d", v, c.Len())
	}
	if New[int](0).max != 1 {
		t.Fatal("a cache holds at least one value")
	}
}

func TestKeyOfSeparatesParts(t *testing.T) {
	if KeyOf("ab", "c") == KeyOf("a", "bc") || KeyOf("x") != KeyOf("x") {
		t.Fatal("KeyOf must length-prefix its parts and be deterministic")
	}
}

func TestCacheConcurrent(t *testing.T) {
	c := New[string](8)
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Go(func() {
			for i := range 200 {
				k := KeyOf(strconv.Itoa((g + i) % 20))
				c.Put(k, "v")
				c.Get(k)
			}
		})
	}
	wg.Wait()
	if c.Len() > 8 {
		t.Fatalf("len %d over the bound", c.Len())
	}
}
