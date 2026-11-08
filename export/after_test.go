package export

import (
	"context"
	"image"
	"image/color"
	"testing"

	"github.com/go-slides/slides"
	"github.com/go-slides/slides/deck"
	"github.com/go-slides/slides/theme"
)

// A theme's section.bar::after has the specificity of the scaffold's
// section:not([data-marpit-pagination])::after { display:none } and comes
// later, so its display:block wins on a slide that is paginated and on one
// that is not (issue #77: the bar was missing from the unpaginated one, whose
// ::after element was not written at all).
func TestThemeAfterOverridesScaffoldOnUnpaginatedSlide(t *testing.T) {
	set := theme.NewSet()
	if _, err := set.Add(`/* @theme after-repro */
@import 'default';
section.bar::after{
  content:" ";display:block;position:absolute;
  left:0;bottom:0;right:auto;width:1280px;height:16px;background:#c60;
}`); err != nil {
		t.Fatal(err)
	}
	src := "---\ntheme: after-repro\n---\n\n" +
		"<!-- _paginate: true -->\n<!-- _class: bar -->\n\n# Paginated\n\n---\n\n" +
		"<!-- _paginate: false -->\n<!-- _class: bar -->\n\n# Unpaginated\n"
	b, err := slides.Render([]byte(src), slides.Config{Themes: set}, deck.FrameBox)
	if err != nil {
		t.Fatal(err)
	}
	var imgs []*image.RGBA
	if err := Each(context.Background(), b.Doc(), Options{NoFonts: true}, func(_ int, im *image.RGBA) error {
		imgs = append(imgs, im)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(imgs) != 2 {
		t.Fatalf("%d images", len(imgs))
	}
	for i, im := range imgs {
		bb := im.Bounds()
		c := im.RGBAAt(bb.Min.X+bb.Dx()/2, bb.Max.Y-4)
		if !near(color.RGBA{R: c.R, G: c.G, B: c.B, A: c.A}, 0xcc, 0x66, 0x00) {
			t.Errorf("slide %d: the bar's pixel is %v, want orange #c60", i+1, c)
		}
	}
}
