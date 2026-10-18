<p align="center"><img src="https://raw.githubusercontent.com/go-slides/brand/main/social/go-slides-slides.png" alt="go-slides/slides" width="720"></p>

# go-slides/slides

[![Docs](https://img.shields.io/badge/docs-hugo%20%2B%20hextra-4F46E5)](https://go-slides.github.io/docs/)
[![CI](https://github.com/go-slides/slides/actions/workflows/ci.yml/badge.svg)](https://github.com/go-slides/slides/actions/workflows/ci.yml)
[![pkg.go.dev](https://img.shields.io/badge/pkg.go.dev-slides-007d9c?logo=go&logoColor=white)](https://pkg.go.dev/github.com/go-slides/slides)
![go](https://img.shields.io/badge/Go-1.27.2%2B-00ADD8?logo=go&logoColor=white)
![cgo](https://img.shields.io/badge/CGO-0-555)
![status](https://img.shields.io/badge/status-unreleased-9a6700)
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
go install github.com/go-slides/slides/cmd/slides@main   # no release yet
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

## Roadmap

Marp CLI drives a headless Chrome for its PDF, PPTX and image output; `slides`
will use the go-* libraries instead:

- PNG/JPEG via [go-webengine](https://github.com/go-webengine/engine). The
  rendering path already exists: the `FrameBox` frame and the packer's
  `GeneratedContent` option lay slides out for an engine without
  `<foreignObject>` or `::after`.
- PDF via [go-pdfkit](https://github.com/go-pdfkit/pdfkit), with outlines from
  the headings and speaker notes.
- PPTX via [go-ooxml/opc](https://github.com/go-ooxml/opc), with speaker
  notes.
- Math (`$…$`, `$$…$$`) via [go-tex/math](https://github.com/go-tex/math),
  code highlighting via [go-rouge](https://github.com/go-rouge/rouge) with
  highlight.js class names, emoji, and fitting headings (`<!-- fit -->`).

## Packages

| | |
|---|---|
| [`deck`](deck) | parses a deck: directives, kinds, images, notes; renders slides |
| [`interp`](interp) | the template stage |
| [`theme`](theme) | Marp themes and the Marpit theme pipeline |
| [`viewer`](viewer) | the HTML page |
| [`slides`](slides.go) | wires them together |

## License

BSD-3-Clause. The built-in themes in [`theme/marp`](theme/marp) are Marp
Core's, under its MIT license ([`theme/marp/LICENSE`](theme/marp/LICENSE)).
