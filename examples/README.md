# Examples

Small decks, each showing one thing well. They are built in CI (HTML and PDF,
by `cmd/slides/examples_test.go`), shown in the
[documentation's gallery](https://go-slides.github.io/docs/latest/gallery/)
and offered in the [playground](https://go-slides.github.io/playground/).

Build one yourself:

```sh
slides build --allow-local-files examples/talk/deck.md     # deck.html
slides build --allow-local-files --pdf examples/talk/deck.md
```

A deck that has a `media/`, `kinds/` or `parts/` folder needs its files beside
it: copy or zip the whole folder. `--allow-local-files` lets a build read the
images under the deck's directory.

| Deck | Shows |
| --- | --- |
| **theme-default** | Marp's default theme: headings, lists, tables, code, `invert`. |
| **theme-gaia** | The gaia theme: `lead` cover and chapter slides, spot directives. |
| **theme-uncover** | The uncover theme: centred, spacious, quiet. |
| **kinds** | Slide kinds, with a kinds file (`kinds/brand.yaml`). |
| **templates** | Go templates: data, a `range` that writes slides, `page`, `total`, `include`. |
| **hcl** | A front matter in HCL2, with expressions, functions and typed data. |
| **math** | Inline and display formulas, typeset to SVG. |
| **images** | Sized images, filters, backgrounds, split and multiple backgrounds (SVGs in `media/`). |
| **code** | Highlighting, line numbers, and callouts that point at notes. |
| **emoji-and-fit** | Emoji shortcodes as embedded Twemoji, and headings that fill the slide. |
| **presenting** | Transitions, speaker notes, the presenter and overview views. |
| **talk** | A complete short talk that combines kinds, templates, math, code, images, emoji and notes. |

## Pictures

The SVGs under `media/` are original work of the go-slides authors, released
under [CC0 1.0](https://creativecommons.org/publicdomain/zero/1.0/); the decks
that use them say so. The decks themselves are under the repository's licence.
