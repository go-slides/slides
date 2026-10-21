package deck

import (
	"reflect"
	"testing"
)

// The render cache keys on every field of RenderOptions, Slide, Global and
// Background (see contextKey and slideKey). A field added to one of them is
// an input the key does not cover yet: this test fails until the key, and
// then the count here, are updated.
func TestRenderCacheKeyCoversEveryField(t *testing.T) {
	for _, c := range []struct {
		v    any
		want int
	}{
		{RenderOptions{}, 11},
		{Slide{}, 17},
		{Global{}, 2},
		{Background{}, 4},
		{Heading{}, 4},
	} {
		if n := reflect.TypeOf(c.v).NumField(); n != c.want {
			t.Errorf("%T has %d fields, the render cache key covers %d: add the new one to contextKey or slideKey", c.v, n, c.want)
		}
	}
}
