package deck

import "github.com/go-slides/slides/diag"

// A deck is untrusted input, and Markdown nests blocks without a bound of its
// own: a line of 100 000 ">" markers makes a hundred thousand nested quotes, and
// the parser and the renderer take time that grows with the square of it (five
// seconds for 200 KB; the compressed link of a playground deck is a hundred
// bytes). These bounds are far above anything a slide has.
const (
	// maxQuoteDepth is how many ">" block quote markers one line may start with.
	maxQuoteDepth = 100
	// maxIndent is the widest indentation, in columns, a line may start with
	// (nested lists nest by indentation).
	maxIndent = 1024
)

// checkNesting returns the 1-based line that nests blocks past the bounds, with
// the refusal; line 0 and nil when there is none.
func checkNesting(src string) (int, error) {
	line := 1
	for i := 0; i < len(src); line++ {
		end := i
		for end < len(src) && src[end] != '\n' {
			end++
		}
		col, quotes := 0, 0
		for j := i; j < end; j++ {
			switch src[j] {
			case ' ':
				col++
			case '\t':
				col += 4 - col%4
			case '>':
				quotes++
			default:
				j = end // the first other character ends the markers
			}
			if col > maxIndent {
				return line, diag.Errorf("deck.limit", "line indented by more than %d columns", maxIndent)
			}
			if quotes > maxQuoteDepth {
				return line, diag.Errorf("deck.limit", "more than %d nested block quotes", maxQuoteDepth)
			}
		}
		i = end + 1
	}
	return 0, nil
}
