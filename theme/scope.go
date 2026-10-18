package theme

import (
	"regexp"
	"strings"

	"github.com/go-slides/slides/internal/css"
)

// Scope confines a <style scoped> element's CSS to one slide, selected by
// attr (a data attribute the slide carries), as Marpit's style/assign.js
// does: a selector starting with section narrows the slide itself, any
// other becomes a descendant of it, and keyframes are renamed so they
// cannot collide with another slide's.
func Scope(src, attr string) string {
	sheet := css.Parse(src)
	sheet.Nodes = css.Flatten(sheet.Nodes)
	key := strings.TrimPrefix(attr, "data-marpit-scope-")
	scope := "section[" + attr + "]"
	var keyframes []string
	var inject func(nodes []css.Node)
	inject = func(nodes []css.Node) {
		for _, n := range nodes {
			switch x := n.(type) {
			case *css.AtRule:
				switch {
				case strings.HasSuffix(x.Name, "keyframes") && x.Params != "":
					keyframes = append(keyframes, x.Params)
					x.Params += "-" + key
				case x.Name == "media" || x.Name == "supports":
					inject(x.Nodes)
				}
			case *css.Rule:
				x.Selector = mapSelectors(x.Selector, func(sel string) string {
					if sectionStart.MatchString(sel) {
						return scope + sel[len("section"):]
					}
					return scope + " " + sel
				})
			}
		}
	}
	inject(sheet.Nodes)
	if len(keyframes) > 0 {
		quoted := make([]string, len(keyframes))
		for i, k := range keyframes {
			quoted[i] = regexp.QuoteMeta(k)
		}
		// \b(name)\b not followed by "(": Go has no lookahead, so
		// check the next byte by hand.
		re := regexp.MustCompile(`\b(` + strings.Join(quoted, "|") + `)\b`)
		css.Walk(sheet.Nodes, func(n, _ css.Node) bool {
			d, ok := n.(*css.Decl)
			if !ok || d.Prop != "animation" && d.Prop != "animation-name" {
				return true
			}
			loc := re.FindStringIndex(d.Value)
			if loc != nil && (loc[1] >= len(d.Value) || d.Value[loc[1]] != '(') {
				d.Value = d.Value[:loc[1]] + "-" + key + d.Value[loc[1]:]
			}
			return true
		})
	}
	return sheet.String()
}
