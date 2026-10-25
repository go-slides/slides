//go:build slides_fontsext

package export

// Built with the slides_fontsext tag, the exports embed none of the theme
// fonts (Lato, Roboto Mono: 4.8 MiB): the host serves FontNames and hands
// them over through SetFontSource, and an export fetches only the faces its
// slides draw.
func embeddedFont(string) []byte { return nil }
