package deck

import "testing"

// A slide's transition, as the exports read it: the directive's word and its
// duration in seconds, and whether it is custom.
func TestSlideTransition(t *testing.T) {
	d, err := Parse([]byte(`---
transition: fade 500ms
---

# a

---

<!-- _transition: cube 1.5s -->

# b

---

<!-- transition: mine -->

# c

---

<!-- transition: nonesuch 2x -->

# d

---

<!-- transition: " " -->

# e
`), Options{})
	if err != nil {
		t.Fatal(err)
	}
	type tr struct {
		name         string
		sec          float64
		custom, have bool
	}
	want := []tr{{"fade", 0.5, false, true}, {"cube", 1.5, false, true}, {"mine", 0, false, true}, {"nonesuch", 0, false, true}, {}}
	// the slide's own scoped keyframes make its transition custom
	for i, w := range want {
		if i >= len(d.Slides) {
			t.Fatalf("%d slides", len(d.Slides))
		}
		name, sec, custom, ok := d.Slides[i].Transition()
		got := tr{name, sec, custom, ok}
		if i == 2 || i == 3 {
			// a name with no built-in is custom
			w.custom = true
		}
		if got != w {
			t.Errorf("slide %d: %+v, want %+v", i+1, got, w)
		}
	}
}

func TestCSSSeconds(t *testing.T) {
	for in, want := range map[string]float64{"1s": 1, "0.5s": 0.5, "250ms": 0.25, "": 0, "1": 0, "-1s": 0, "nans": 0, "NaNs": 0, "1e9s": 0, "1h": 0, "ms": 0} {
		if got := cssSeconds(in); got != want {
			t.Errorf("cssSeconds(%q) = %v, want %v", in, got, want)
		}
	}
}

// A slide with scoped keyframes of its own is custom, and its name is the
// directive's word, not the scope-suffixed one.
func TestSlideTransitionScopedKeyframes(t *testing.T) {
	d, err := Parse([]byte(`---
---

<style scoped>
@keyframes marp-incoming-transition-mine { from { opacity: 0 } to { opacity: 1 } }
</style>

<!-- transition: mine -->

# a

---

# b
`), Options{})
	if err != nil {
		t.Fatal(err)
	}
	name, _, custom, ok := d.Slides[0].Transition()
	if !ok || !custom || name != "mine" {
		t.Errorf("%q custom=%v ok=%v", name, custom, ok)
	}
}
