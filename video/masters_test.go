package video

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	ebml "github.com/at-wat/ebml-go"
)

// tableMasters are the master elements of ebml-go's element table (the EBML
// and Matroska element lists it carries), read through its exported API: the
// names come from the table's own source, the ids and types from ElementType.
func tableMasters(t testing.TB) map[uint32]string {
	t.Helper()
	where, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/at-wat/ebml-go").Output()
	if err != nil || len(where) < 2 {
		t.Fatalf("where is ebml-go: %v", err)
	}
	dir := strings.TrimSpace(string(where))
	src, err := os.ReadFile(filepath.Join(dir, "elementtype.go"))
	if err != nil {
		t.Fatal(err)
	}
	names := regexp.MustCompile(`Element\w+:\s+"(\w+)",`).FindAllStringSubmatch(string(src), -1)
	if len(names) < 100 {
		t.Fatalf("read %d element names from ebml-go: the table moved", len(names))
	}
	out := map[uint32]string{}
	for _, m := range names {
		et, err := ebml.ElementTypeFromString(m[1])
		if err != nil {
			t.Fatalf("%s: %v", m[1], err)
		}
		if et.DataType() != ebml.DataTypeMaster {
			continue
		}
		var id uint32
		for _, b := range et.Bytes() {
			id = id<<8 | uint32(b)
		}
		out[id] = m[1]
	}
	return out
}

// The scanner's set of master elements is ebml-go's, not a hand-made list: a
// laced block nested in a master the scanner does not descend into escapes the
// frame count.
func TestMasterSetIsTheElementTables(t *testing.T) {
	want := tableMasters(t)
	got := ebmlMasters
	var missing, extra []uint32
	for id := range want {
		if !got[id] {
			missing = append(missing, id)
		}
	}
	for id := range got {
		if _, ok := want[id]; !ok {
			extra = append(extra, id)
		}
	}
	sort.Slice(missing, func(i, j int) bool { return missing[i] < missing[j] })
	for _, id := range missing {
		t.Errorf("master %#x (%s) is not descended into", id, want[id])
	}
	for _, id := range extra {
		t.Errorf("%#x is descended into and is not a master in ebml-go's table", id)
	}
}
