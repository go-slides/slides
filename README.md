<p align="center"><img src="https://raw.githubusercontent.com/go-slides/brand/main/social/go-slides-slides.png" alt="go-slides/slides" width="720"></p>

# go-slides/slides

[![Docs](https://img.shields.io/badge/docs-hugo%20%2B%20hextra-4F46E5)](https://go-slides.github.io/docs/)
[![CI](https://github.com/go-slides/slides/actions/workflows/ci.yml/badge.svg)](https://github.com/go-slides/slides/actions/workflows/ci.yml)
[![pkg.go.dev](https://img.shields.io/badge/pkg.go.dev-slides-007d9c?logo=go&logoColor=white)](https://pkg.go.dev/github.com/go-slides/slides)
![go](https://img.shields.io/badge/Go-1.27.2%2B-00ADD8?logo=go&logoColor=white)
![cgo](https://img.shields.io/badge/CGO-0-555)
[![release](https://img.shields.io/github/v/release/go-slides/slides)](https://github.com/go-slides/slides/releases)
[![license](https://img.shields.io/badge/license-BSD--3--Clause-blue)](./LICENSE)

🌐 **[Website](https://go-slides.github.io)** · 📚 **[Documentation](https://go-slides.github.io/docs/)**

Marp-compatible slide decks in pure Go (no cgo, no Node, no browser), with
two additions:

- **Go templates in slides.** `{{ .title }}`, `{{ range }}` loops that
  generate slides, `{{ page }}` / `{{ total }}`, `{{ include "part.md" }}`.
- **Slide kinds.** A slide says what it *is* (`kind: title`) instead of
  repeating how it looks. A kind is a named preset of header, footer, page
  numbering, class and background.

```markdown
---
theme: gaia
paginate: true
footer: "{{ .title }} — {{ page }}/{{ total }}"
title: Quarterly review
kind: content
data:
  topics: [Revenue, Hiring, Roadmap]
---

<!-- _kind: title -->

# {{ .title }}

{{ range .topics }}
---

## {{ . }}
{{ end }}
```

```sh
go install github.com/go-slides/slides/cmd/slides@latest
slides deck.md            # writes deck.html
```

## Marp compatibility

The deck format is [Marp](https://marp.app)'s: [Marpit](https://marpit.marp.app)
directives and [Marp Core](https://github.com/marp-team/marp-core)'s flavour of
CommonMark. Its behaviour was ported from the Marpit and Marp Core sources
rather than from their documentation. Every case below is pinned by a test
against output that marp-cli 4.5.1 (Marp Core 4.4.0) produced; see
[`testdata/oracle`](testdata/oracle).

- Slides split at `---`, or at the headings that `headingDivider` names.
- Global, local and spot (`_name`) directives, from the front matter or from
  HTML comments. Loose YAML is on, as in Marp Core: `backgroundColor: #eee`
  is a colour, not a comment.
- `header`, `footer` (inline Markdown), `paginate` (`true`, `false`, `hold`,
  `skip`), `class`, `color`, and the `background*` directives.
- The image syntax: `w:` / `h:`, CSS filters, `![bg]` backgrounds. Several
  backgrounds and split backgrounds (`bg left:40%`) render as layers, as in
  Marp's inline SVG mode.
- Fragmented lists (`*` and `1)`), speaker notes (the comments that are not
  directives), and `<style>` / `<style scoped>`.
- Marp Core's built-in `default`, `gaia` and `uncover` themes, the `size`
  directive (`16:9`, `4:3`), custom themes (`@theme`, `@import`, `@size`).
- GFM tables, strikethrough, URL linkify, line breaks as `<br>`, heading ids
  slugged as Marp Core does them.
- Raw HTML filtered through Marp Core's allowlist; `--html` allows it all.

`slides expand` writes a deck back out as plain Marp Markdown, with templates
expanded and kinds replaced by the directives they stand for, so marp-cli and
the Marp editor extensions render it the same way. The kinds test renders the
expanded deck with Marp and requires the same HTML as `slides` produces for
the original.

## Templates

The deck body is one Go [`text/template`](https://pkg.go.dev/text/template),
executed before the Markdown is parsed. Its data (the dot) is the front
matter, overlaid by the front matter's `data:` mapping, then by `data` in
`slides.yaml`, `--data` files and `--set key=value` flags. A missing key is an
error that names the file and line, rather than printing `<no value>`.

| | |
|---|---|
| `{{ page }}`, `{{ total }}` | the page number and the last page number, as Marp counts them (`skip` and `hold` slides do not advance it) |
| `{{ include "file.md" }}` | another file, expanded with the same data |
| `now`, `date`, `env` | `{{ now \| date "2006-01-02" }}`, `{{ env "USER" }}` |
| strings | `upper lower title trim replace split join contains hasPrefix hasSuffix repeat quote` |
| values | `default seq add sub mul list dict merge` |

Front-matter values and kind definitions are templates too, so
`footer: "{{ .title }} — {{ page }}"` numbers every slide.

Code is left alone. Text inside fenced code blocks and inline code spans is
never interpreted, so a slide about Go templates can show `{{ .Name }}` as it
is. Front matter `template: code` interpolates code too, and
`template: false` turns templates off. `delims: ["<%", "%>"]` changes the
delimiters.

## Kinds

```yaml
kinds:
  title:              # redefines a built-in kind
    class: lead
    paginate: false
    header: ""
    footer: ""
  quote:
    extends: blank    # starts from another kind
    class: invert
    backgroundColor: "#111"
```

- `kind: name` applies a kind like a local directive: to this slide and to the
  ones after it. `_kind: name` applies it to one slide.
- Switching kinds first clears what the previous kind set, so a class or a
  footer never leaks from one kind into the next.
- Directives written by hand always win over the kind. They carry over to the
  following slides as Marp's local directives do, kind switches included.
- A kind accepts the local directives: `backgroundColor`, `backgroundImage`,
  `backgroundPosition`, `backgroundRepeat`, `backgroundSize`, `class`,
  `color`, `footer`, `header`, `paginate`. It also accepts `extends`.
- Each slide carries `data-kind="<name>"`, so a theme can style a kind
  directly.

Built-in kinds: `title` (class `lead`, no header, footer or page number),
`section` (class `lead`), `content` (the deck's defaults) and `blank`
(nothing around the content). Only the gaia theme defines `lead`; with the
other themes, give `title` and `section` a class your theme knows.

Kinds come from, in increasing priority: the built-ins, `slides.yaml`,
`--kinds` files, and the deck's front matter `kinds:` key. That key takes either
a mapping or the path of a file, or a list of paths, so several decks can share
one file. `slides kinds deck.md` lists what a deck can use.

## Command line

```
slides [build] [flags] deck.md     write the presentation as HTML
slides expand  [flags] deck.md     print plain Marp Markdown
slides notes   [flags] deck.md     print the speaker notes
slides kinds   [flags] deck.md     list the kinds and what they set
slides themes  [--theme-set ...]   list the themes
slides version                     print the version (also --version)
slides completion bash|zsh|fish|powershell
```

Flags may come before or after the deck. `slides <command> --help` lists a
command's flags.

| flag | commands | |
|---|---|---|
| `-o`, `--output file` | build, expand, notes | output file; `-` for standard output (build defaults to `deck.html` next to the deck) |
| `--html` | build | allow all raw HTML |
| `--lang code` | build | the `lang` attribute when the deck sets none |
| `--bare` | build | stack the slides, without the presenting script |
| `--pdf` | build | write a PDF, one page per slide, with an outline (also chosen by `-o file.pdf`) |
| `--images png\|jpeg` | build | write one image per slide: `deck.001.png`, … |
| `--image png\|jpeg` | build | write the first slide as an image (also chosen by `-o file.png` / `.jpg`) |
| `--jpeg-quality n` | build | JPEG quality, 1 to 100 (default 85) |
| `--image-scale n` | build | device pixels per CSS pixel for images and PDF (default 1) |
| `--allow-local-files` | build | let images and PDF read local files, from under the deck's directory only |
| `--theme-set path` | all | a theme CSS file or a directory of them (repeatable) |
| `--kinds file` | all | a YAML file of kinds (repeatable) |
| `--data file`, `--set k=v` | all | template data (repeatable) |
| `--no-template`, `--strict-yaml` | all | plain Marpit behaviour |
| `--config file`, `--no-config` | all | `slides.yaml` is read from the deck's directory by default |

```yaml
# slides.yaml
kinds: kinds.yaml        # or a mapping, or a list of files
themes: [themes/acme.css]
data: {company: ACME}    # or file paths
html: false
lang: en
```

The HTML page is self-contained. Navigate with the arrow keys, Space and
PageUp/PageDown, `f` toggles full screen, a click advances, and the URL hash
names the slide. Fragments reveal one at a time. Printing gives one slide per
page.

## Images and PDF without a browser

Marp CLI drives a headless Chrome for its PDF and image output. `slides` uses
the go-* libraries instead:

- [go-webengine](https://github.com/go-webengine/engine) lays out and paints
  each slide. The slides go through the box frame (`deck.FrameBox`), which
  needs neither `<foreignObject>` nor generated content: the theme's
  `section::after` is lowered to a real element carrying its evaluated content.
- [go-pdfkit](https://github.com/go-pdfkit/pdfkit) writes the PDF: one page
  per slide at the slide's size, and an outline of the slides and their
  headings.
- The fonts the built-in themes ask for (Lato, Roboto Mono) come from
  [go-opentype/fonts](https://github.com/go-opentype/fonts), embedded, so no
  render touches the network.

- `--image-scale 2` renders at two device pixels per CSS pixel (2560×1440 for
  16:9). Text and vectors are drawn at that density, not enlarged.
- Local images are refused unless `--allow-local-files` is given, as with
  marp-cli. Even then, only files under the deck's directory are read: a deck
  from elsewhere cannot reach `~/.ssh`.

Several and split backgrounds (`![bg left:40%]`) render as Marp's layers do.

Lato Regular, Bold and Black and Roboto Mono Regular and Bold are embedded,
so gaia's headings get their weight. No italic face is embedded yet, so
italic text in those families renders upright.

## Math and code

- **Math**: `$…$` inline and `$$…$$` display, with Marp Core's delimiter
  rules (a `$` followed by a space does not open, one followed by a digit does
  not close). Formulas are typeset by
  [go-tex/math](https://github.com/go-tex/math) (STIX Two Math) into inline
  SVG sized in `em`, so they follow the text's size and sit on its baseline,
  in the HTML as in images and PDF. A formula that does not parse stays visible
  as written.
- **Code**: fenced blocks are coloured by
  [go-rouge](https://github.com/go-rouge/rouge) in highlight.js class names,
  which is what Marp's themes style. A language go-rouge does not know is
  written as plain text, as Marp Core does.

## Roadmap

- PPTX via [go-ooxml](https://github.com/go-ooxml/ooxml), with speaker notes.
- Emoji, and fitting headings (`<!-- fit -->`).

## Packages

| | |
|---|---|
| [`deck`](deck) | parses a deck: directives, kinds, images, notes; renders slides |
| [`interp`](interp) | the template stage |
| [`theme`](theme) | Marp themes and the Marpit theme pipeline |
| [`viewer`](viewer) | the HTML page |
| [`export`](export) | images and PDF, through go-webengine and go-pdfkit |
| [`slides`](slides.go) | wires them together |

## License

BSD-3-Clause. The built-in themes in [`theme/marp`](theme/marp) are Marp
Core's, under its MIT license ([`theme/marp/LICENSE`](theme/marp/LICENSE)).
