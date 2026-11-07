package interp

import (
	"strings"
	"testing"
	"time"

	"github.com/go-slides/slides/diag"
)

// list is a data list of n items.
func list(n int) []any {
	l := make([]any, n)
	for i := range l {
		l[i] = i
	}
	return l
}

// expandData is Expand of src over data, under a time bound.
func expandData(t *testing.T, src string, data map[string]any) (string, error, time.Duration) {
	t.Helper()
	type result struct {
		out string
		err error
	}
	ch := make(chan result, 1)
	start := time.Now()
	go func() {
		out, err := Expand(src, Options{Data: data, Name: "deck.md", BaseDir: "/w"})
		ch <- result{out, err}
	}()
	select {
	case r := <-ch:
		return r.out, r.err, time.Since(start)
	case <-time.After(60 * time.Second):
		t.Fatal("the expansion did not finish in 60 s")
	}
	return "", nil, 0
}

// A nested range over data writes nothing and calls no function: the output limit
// and the budget of seq never see it. Its steps are counted as it runs.
func TestNestedRangeOverDataIsRefused(t *testing.T) {
	// the budget is lowered so that each refusal is quick (at full size one takes
	// seconds under the race detector on a small runner: see the next test)
	prev := maxSteps
	maxSteps = 1 << 15
	defer func() { maxSteps = prev }()
	data := map[string]any{"a": list(10_000), "b": list(300)}
	for name, src := range map[string]string{
		"two lists of 10^4":      `{{ range .a }}{{ range $.a }}{{ end }}{{ end }}`,
		"three lists of 300":     `{{ range .b }}{{ range $.b }}{{ range $.b }}{{ end }}{{ end }}{{ end }}`,
		"with an index":          `{{ range $i, $x := .a }}{{ range $j, $y := $.a }}{{ end }}{{ end }}`,
		"through a template":     `{{ define "in" }}{{ range .a }}{{ end }}{{ end }}{{ range .a }}{{ template "in" $ }}{{ end }}`,
		"through an if":          `{{ range .a }}{{ if true }}{{ range $.a }}{{ end }}{{ end }}{{ end }}`,
		"over a map":             `{{ range .m }}{{ range $.m }}{{ range $.m }}{{ end }}{{ end }}{{ end }}`,
		"a range over an int":    `{{ range 100000 }}{{ range 100000 }}{{ end }}{{ end }}`,
		"with and range":         `{{ with .a }}{{ range . }}{{ range $.a }}{{ end }}{{ end }}{{ end }}`,
		"in a template, no loop": `{{ define "r" }}{{ range .b }}{{ template "r" $ }}{{ end }}{{ end }}{{ template "r" . }}`,
	} {
		t.Run(name, func(t *testing.T) {
			d := map[string]any{"a": data["a"], "b": data["b"], "m": map[string]any{}}
			for i := range 300 {
				d["m"].(map[string]any)[strings.Repeat("k", i+1)] = i
			}
			out, err, took := expandData(t, src, d)
			if err == nil {
				t.Fatalf("not refused: %d bytes", len(out))
			}
			if diag.CodeOf(err) != "template.limit" && !strings.Contains(err.Error(), "maximum template depth") {
				t.Fatalf("code %q: %v", diag.CodeOf(err), err)
			}
			if took > 45*time.Second {
				t.Errorf("refused after %v", took)
			}
		})
	}
}

// The same refusal at the real budget: the step count or, on a slow machine, the
// deadline stops two lists of 10^4. The bound says "does not hang".
func TestNestedRangeKeepsItsFullBudget(t *testing.T) {
	if maxSteps != MaxSteps || maxDuration != MaxDuration {
		t.Fatal("the bounds are not the documented ones")
	}
	_, err, took := expandData(t, `{{ range .a }}{{ range $.a }}{{ end }}{{ end }}`, map[string]any{"a": list(10_000)})
	if diag.CodeOf(err) != "template.limit" {
		t.Fatalf("%v", err)
	}
	if took > 45*time.Second {
		t.Errorf("refused after %v", took)
	}
}

// The deadline bounds a step that is itself slow: with the step budget out of the
// way, a loop that takes too long is refused.
func TestExpansionDeadline(t *testing.T) {
	steps, dur := maxSteps, maxDuration
	maxSteps, maxDuration = 1<<40, 30*time.Millisecond
	defer func() { maxSteps, maxDuration = steps, dur }()
	_, err, took := expandData(t, `{{ range .a }}{{ range $.a }}{{ end }}{{ end }}`, map[string]any{"a": list(5000)})
	if diag.CodeOf(err) != "template.limit" || !strings.Contains(err.Error(), "ran for more than") {
		t.Fatalf("%v", err)
	}
	if took > 45*time.Second {
		t.Errorf("refused after %v", took)
	}
}

func TestLoopsWithinTheBudgetAreNotRefused(t *testing.T) {
	// 300 x 300 = 9 * 10^4 steps: well within the budget, and within the
	// deadline however slow the machine (700 x 700 took seconds under -race)
	out, err, _ := expandData(t, `{{ range .a }}{{ range $.a }}{{ end }}{{ end }}done`, map[string]any{"a": list(300)})
	if err != nil || out != "done" {
		t.Fatalf("%q, %v", out, err)
	}
	// whitespace trimming around the loops still works: the tick writes nothing
	out, err, _ = expandData(t, "a\n{{- range .a }}\n- {{ . }}\n{{- end }}\nz", map[string]any{"a": list(3)})
	if err != nil || out != "a\n- 0\n- 1\n- 2\nz" {
		t.Fatalf("%q, %v", out, err)
	}
	// a hundred slides of twenty items, as a templated deck does
	out, err, _ = expandData(t, "{{ range seq 100 }}\n# Slide {{ . }}\n\n{{ range seq 20 }}- item {{ . }}\n{{ end }}\n---\n{{ end }}", nil)
	if err != nil || strings.Count(out, "# Slide") != 100 {
		t.Fatalf("%d slides, %v", strings.Count(out, "# Slide"), err)
	}
	// a template that recurses within the budget
	out, err, _ = expandData(t, `{{ define "n" }}{{ if gt . 0 }}{{ . }}{{ template "n" (sub . 1) }}{{ end }}{{ end }}{{ template "n" 5 }}`, nil)
	if err != nil || out != "54321" {
		t.Fatalf("%q, %v", out, err)
	}
}

func BenchmarkRangeSteps(b *testing.B) {
	data := map[string]any{"a": list(1000)}
	for range b.N {
		if _, err := Expand(`{{ range .a }}{{ range $.a }}{{ end }}{{ end }}`, Options{Data: data}); err != nil {
			b.Fatal(err)
		}
	}
}
