package hclx

import (
	"math"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/hcl/v2"
	"github.com/zclconf/go-cty/cty"

	"github.com/go-slides/slides/interp"
)

func hclInitial() hcl.Pos { return hcl.Pos{} }

func TestYAML(t *testing.T) {
	for _, tc := range []struct {
		v    cty.Value
		want string
	}{
		{cty.StringVal(`a "b" <c> & ` + interp.PageSentinel), `"a \"b\" <c> & ` + interp.PageSentinel + `"`},
		{cty.StringVal("two\nlines"), `"two\nlines"`},
		{cty.NumberIntVal(3), "3"},
		{cty.NumberFloatVal(1.5), "1.5"},
		{cty.NumberVal(new(big.Float).SetFloat64(1e300)), ""},
		{cty.NumberFloatVal(1e300), "1e+300"},
		{cty.NumberFloatVal(math.Inf(1)), ".inf"},
		{cty.NumberFloatVal(math.Inf(-1)), "-.inf"},
		{cty.True, "true"},
		{cty.NullVal(cty.String), "null"},
		{cty.TupleVal([]cty.Value{cty.StringVal("a"), cty.NumberIntVal(2)}), `["a", 2]`},
		{cty.ObjectVal(map[string]cty.Value{"b": cty.True, "a": cty.EmptyTupleVal}), `{"a": [], "b": true}`},
	} {
		got := YAML(tc.v)
		if tc.want == "" {
			// A float beyond int64 stays a float in YAML.
			if !strings.Contains(got, "e+") {
				t.Errorf("YAML(%#v) = %q", tc.v, got)
			}
			continue
		}
		if got != tc.want {
			t.Errorf("YAML(%#v) = %q, want %q", tc.v, got, tc.want)
		}
	}
}

func TestToGoAndBack(t *testing.T) {
	in := map[string]any{
		"s": "x", "i": 3, "i64": int64(4), "u": uint64(5), "f": 0.5, "b": true, "n": nil,
		"l": []any{"a", 1}, "ls": []string{"x"}, "empty": []any{}, "m": map[string]any{},
		"t":         time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		"\x00order": []string{"s"}, "other": struct{}{},
	}
	got, _ := ToGo(FromGo(in)).(map[string]any)
	if got["s"] != "x" || got["i"] != 3 || got["i64"] != 4 || got["u"] != 5 || got["f"] != 0.5 ||
		got["b"] != true || got["n"] != nil || got["t"] != "2026-01-02T03:04:05Z" || got["other"] != "{}" {
		t.Errorf("round trip: %#v", got)
	}
	if _, ok := got["\x00order"]; ok {
		t.Error("bookkeeping keys are dropped")
	}
	if l, _ := got["ls"].([]any); len(l) != 1 || l[0] != "x" {
		t.Errorf("ls = %#v", got["ls"])
	}
	if ToGo(cty.UnknownVal(cty.String)) != nil {
		t.Error("unknown is nil")
	}
	if s := ToGo(cty.SetVal([]cty.Value{cty.StringVal("a")})); len(s.([]any)) != 1 {
		t.Errorf("set = %#v", s)
	}
	if m := ToGo(cty.MapVal(map[string]cty.Value{"k": cty.NumberIntVal(1)})); m.(map[string]any)["k"] != 1 {
		t.Errorf("map = %#v", m)
	}
}

func TestFailsafe(t *testing.T) {
	v := cty.TupleVal([]cty.Value{cty.True, cty.NumberFloatVal(2.5), cty.NumberIntVal(2), cty.StringVal("s")})
	got, _ := Failsafe(v).([]any)
	if len(got) != 4 || got[0] != "true" || got[1] != "2.5" || got[2] != "2" || got[3] != "s" {
		t.Errorf("failsafe = %#v", got)
	}
	if Failsafe(cty.NumberIntVal(7)) != "7" || Failsafe(cty.NullVal(cty.Bool)) != nil {
		t.Error("scalars")
	}
	m, _ := Failsafe(cty.ObjectVal(map[string]cty.Value{"k": cty.False})).(map[string]any)
	if m["k"] != "false" {
		t.Errorf("object = %#v", m)
	}
}

func TestFunctions(t *testing.T) {
	env := Env{Now: func() time.Time { return time.Date(2026, 10, 19, 11, 0, 0, 0, time.FixedZone("CEST", 2*3600)) }}
	for src, want := range map[string]string{
		`"${now()}"`:                              "2026-10-19T11:00:00+02:00",
		`"${timestamp()}"`:                        "2026-10-19T09:00:00Z",
		`formatdate("YYYY", timestamp())`:         "2026",
		`"${page()}/${total()}"`:                  interp.PageSentinel + "/" + interp.TotalSentinel,
		`join("-", split(",", "a,b"))`:            "a-b",
		`regexreplace("a1b2", "[0-9]", "")`:       "ab",
		`jsonencode({a = [1, true]})`:             `{"a":[1,true]}`,
		`"${length(range(3))} ${max(1, 5, 2)}"`:   "3 5",
		`coalesce(null, "x")`:                     "x",
		`format("%03d %s", 7, title("hi there"))`: "007 Hi There",
		`"${strlen("héllo")} ${strrev("ab")}"`:    "5 ba",
	} {
		body, err := Parse([]byte("v = "+src+"\n"), "t.hcl", hclInitial())
		if err != nil {
			t.Fatal(err)
		}
		v, err := Eval(body.Attributes["v"], env.Context())
		if err != nil {
			t.Errorf("%s: %v", src, err)
			continue
		}
		if got := v.AsString(); got != want {
			t.Errorf("%s = %q, want %q", src, got, want)
		}
	}
	// No function reaches out of the evaluation.
	for _, name := range []string{"http", "fileexists", "templatefile", "setproduct"} {
		if _, ok := Functions(env)[name]; ok {
			t.Errorf("%s is available", name)
		}
	}
}

func TestErrorFormat(t *testing.T) {
	if Error(nil) != nil {
		t.Error("no diagnostics, no error")
	}
	_, err := Parse([]byte("a = \nb = {\n"), "x.hcl", hclInitial())
	if err == nil || !strings.HasPrefix(err.Error(), "x.hcl:1:") {
		t.Errorf("error %v", err)
	}
	if _, err := ReadFile(".", "missing"); err == nil {
		t.Error("a missing file fails")
	}
}
