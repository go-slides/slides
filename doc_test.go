package slides

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/go-slides/slides/deck"
	"github.com/go-slides/slides/doc"
)

// Build.Doc carries what the exports read, and survives JSON: a host may
// build in one place and export in another.
func TestBuildDoc(t *testing.T) {
	src := "---\nauthor: Ada\ndescription: A test\nkeywords: one, two\nlang: fr\n---\n\n# First\n\n## Sub\n\n<!-- a note -->\n\n---\n\n# Second\n"
	b, err := Render([]byte(src), Config{}, deck.FrameBox)
	if err != nil {
		t.Fatal(err)
	}
	d := b.Doc()
	if d.CSS != b.Packed.CSS || d.Width != b.Packed.Width || d.Height != b.Packed.Height || d.Width == 0 {
		t.Fatalf("size %v×%v, css %d bytes", d.Width, d.Height, len(d.CSS))
	}
	if d.Title != Title(b.Deck) || d.Title != "First" || d.Author != "Ada" || d.Description != "A test" || d.Lang != "fr" {
		t.Fatalf("metadata %+v", d)
	}
	if !reflect.DeepEqual(d.Keywords, []string{"one", "two"}) || d.KeywordsDirective != "one, two" || d.TitleDirective != "" {
		t.Fatalf("keywords %q %q, title directive %q", d.Keywords, d.KeywordsDirective, d.TitleDirective)
	}
	if len(d.Slides) != 2 || d.Slides[0].HTML != b.Slides[0].HTML {
		t.Fatalf("%d slides", len(d.Slides))
	}
	want := []doc.Heading{{Level: 1, Text: "First"}, {Level: 2, Text: "Sub"}}
	if !reflect.DeepEqual(d.Slides[0].Headings, want) || !reflect.DeepEqual(d.Slides[0].Notes, []string{"a note"}) {
		t.Fatalf("slide 1: %+v %q", d.Slides[0].Headings, d.Slides[0].Notes)
	}
	j, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	var back doc.Doc
	if err := json.Unmarshal(j, &back); err != nil || !reflect.DeepEqual(&back, d) {
		t.Fatalf("JSON round trip: %v", err)
	}
}
