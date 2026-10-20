// Package lru is a small bounded least-recently-used cache keyed by content
// hashes, safe for concurrent use. go-slides keeps what an editor's repeated
// renders of one deck would otherwise recompute on every keystroke (parsed
// themes, packed style sheets, highlighted code) and must never grow without
// bound when the deck changes on every keystroke.
package lru

import (
	"container/list"
	"crypto/sha256"
	"encoding/binary"
	"sync"
)

// Key is a content hash.
type Key [sha256.Size]byte

// KeyOf hashes parts, each length-prefixed so that ("ab","c") and
// ("a","bc") differ.
func KeyOf(parts ...string) Key {
	h := sha256.New()
	var n [8]byte
	for _, p := range parts {
		binary.LittleEndian.PutUint64(n[:], uint64(len(p)))
		h.Write(n[:])
		h.Write([]byte(p))
	}
	var k Key
	h.Sum(k[:0])
	return k
}

// Cache maps keys to values, holding at most max of them.
type Cache[V any] struct {
	mu    sync.Mutex
	max   int
	order *list.List // front is most recent; elements hold *entry[V]
	items map[Key]*list.Element
}

type entry[V any] struct {
	key Key
	val V
}

// New returns a cache holding at most max values (at least one).
func New[V any](n int) *Cache[V] {
	return &Cache[V]{max: max(n, 1), order: list.New(), items: map[Key]*list.Element{}}
}

// Get returns the value for k and marks it recently used.
func (c *Cache[V]) Get(k Key) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.items[k]; ok {
		c.order.MoveToFront(e)
		return e.Value.(*entry[V]).val, true
	}
	var zero V
	return zero, false
}

// Put stores v for k, evicting the least recently used values past the bound.
func (c *Cache[V]) Put(k Key, v V) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.items[k]; ok {
		e.Value.(*entry[V]).val = v
		c.order.MoveToFront(e)
		return
	}
	c.items[k] = c.order.PushFront(&entry[V]{key: k, val: v})
	for c.order.Len() > c.max {
		last := c.order.Back()
		c.order.Remove(last)
		delete(c.items, last.Value.(*entry[V]).key)
	}
}

// Len returns how many values the cache holds.
func (c *Cache[V]) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.order.Len()
}
