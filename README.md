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
- Emoji shortcodes and Unicode emoji as Twemoji images (see [Emoji](#emoji)).
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
one file. `slides kinds deck.md` lists what a deck can use; with `--json`
(after v0.5.0), as a list of `{name, extends, directives: [{key, value}]}`, the
directives resolved through `extends`.

## HCL

Everywhere `slides` reads YAML, it also reads
[HCL2](https://github.com/hashicorp/hcl/blob/main/hclsyntax/spec.md)'s native
syntax, for its expressions and functions:

| where | YAML | HCL |
|---|---|---|
| front matter | `---` … `---` | `---hcl` … `---` |
| configuration | `slides.yaml` | `slides.hcl` (looked up first) |
| kinds files | `--kinds kinds.yaml`, `kinds: kinds.yaml` | any file named `*.hcl` |
| data files | `--data data.yaml` / `.json` | any file named `*.hcl` |

```hcl
---hcl
theme    = "gaia"
paginate = true
title    = "Quarterly review"
data = {
  company = "ACME"
  quarter = 3
}
header = format("%s — Q%d", data.company, data.quarter)
footer = "${upper(data.company)} · ${page()}/${total()}"
date   = formatdate("D MMMM YYYY", now())

kind "chapter" {
  extends = "section"
  class   = ["lead", "invert"]
}
---

# {{ .title }}
```

- **Attributes** are the YAML keys under the same names: Marp's directives
  (`theme`, `paginate`, `header`, `footer`, `class`, `backgroundColor`, …),
  the metadata (`title`, `author`, …), `template`, `delims`, `kinds` (file
  paths) and `data`, an object. `kind "<name>" { … }` blocks define kinds,
  in a front matter, in `slides.hcl` and in kinds files. `slides.hcl` takes
  `themes`, `html`, `lang`, `data` and `kinds` attributes besides.
- **Values** reach the directives in the string forms YAML gives them
  (`true` is `"true"`, a list of classes is a list), so a directive behaves
  the same whichever syntax set it. `null` leaves a directive unset. `data`
  keeps its types: in templates an integral number is an `int`, others a
  `float64`, and lists and objects stay lists and maps.
- **Variables**: `data` is the template data — the front matter's own `data`
  overlaid by `slides.hcl`'s, `--data` files and `--set` — so
  `"${data.company}"` works wherever `{{ .company }}` would. Front-matter
  attributes may reference `data.*` but not each other; spot directives
  (`_class`…) are not HCL identifiers, so write them in a comment.
- **Functions**: `page()` and `total()` (the page number and the last page
  number, as `{{ page }}` and `{{ total }}`), `now()` and `timestamp()`
  (RFC 3339, local and UTC), `env(name)`, and `file(path)`, which reads a
  UTF-8 text file of at most 1 MiB relative to the deck or configuration
  file. From go-cty's standard library: `abs ceil floor log max min parseint
  pow signum`, `chomp format formatlist join lower regex regexall
  regexreplace replace split strlen strrev substr title trim trimprefix
  trimspace trimsuffix upper`, `chunklist coalesce coalescelist compact
  concat contains distinct element flatten keys length lookup merge range
  reverse slice sort values zipmap`, `csvdecode jsondecode jsonencode`,
  `formatdate timeadd`. None reaches the network.
- An HCL value is final: Go templates do not run over it a second time. The
  deck body stays a Go template.
- **Errors** carry HCL's diagnostics, positioned in the deck file itself.
  `footer = data.compnay` on the deck's fourth line gives
  `deck.md:4:14: Unsupported attribute: This object does not have an
  attribute named "compnay".`
- **Marp** reads `---hcl` as the start of a front matter too (its rule opens
  on any first line beginning with three dashes) and, the block not being a
  YAML mapping, applies none of it:
  [`testdata/oracle/hcl-frontmatter.md`](testdata/oracle/hcl-frontmatter.md)
  pins this against Marp Core. `slides expand` writes the front matter back
  as YAML, so the expanded deck is plain Marp.
- Directive comments (`<!-- class: invert -->`) stay YAML, as in Marp.

## Command line

```
slides [build] [flags] deck.md...  write the presentation as HTML
slides [build] [flags] -I dir      ... every deck under dir, mirrored under -o
slides expand  [flags] deck.md     print plain Marp Markdown
slides notes   [flags] deck.md     print the speaker notes
slides kinds   [flags] deck.md     list the kinds and what they set
slides themes  [--theme-set ...]   list the themes
slides version                     print the version (also --version)
slides completion bash|zsh|fish|powershell
```

Flags may come before or after the deck. `slides <command> --help` lists a
command's flags. `build` takes several decks, a directory (its decks, found
recursively) or a pattern; `-o` then needs `-I`, or `-`. Unlike marp-cli, standard
input is read only for `-`, not whenever it is a pipe.

| flag | commands | |
|---|---|---|
| `-o`, `--output file` | build, expand, notes | output file; `-` for standard output (build defaults to `deck.html` next to the deck) |
| `-w`, `--watch` | build | build again when a deck, a `--theme-set` file, a kinds or data file or the configuration changes, and tell the open pages to reload (a socket on the loopback; see below) |
| `-s`, `--server dir` | build | serve `dir` (or `-I dir`): its decks converted on request — `deck.md` as HTML, `deck.md?pdf`, `?pptx`, `?png`, `?jpg`, `?notes` — an index of each directory, live reload of the pages. Listens on **127.0.0.1**, port 8080 or `$PORT` |
| `-p`, `--preview` | build | open the result in the default browser: the HTML pages with `--watch` (implied), the server with `--server` |
| `--host addr` | build | the address `--server` listens on (default `127.0.0.1`); anything else serves the directory to the network, with a warning |
| `-I`, `--input-dir dir` | build | convert the Markdown files (`.md`, `.mdown`, `.markdown`, `.markdn`) under `dir`, recursively, as marp-cli does — node_modules, dot directories and `UPPER.MD` left out. With `-o out/` each deck is written at its path under `dir`, mirrored under `out/` (`dir/sub/b.md` gives `out/sub/b.html`); without `-o`, next to the deck. Naming decks as well is an error |
| `-P`, `--parallel n` | build | decks built at once (default 5, at most the number of cores; 1 for one after the other). Messages stay together per deck; one failing deck does not stop the others and the exit status says how many failed |
| `--html` | build | allow all raw HTML |
| `--lang code` | build | the `lang` attribute when the deck sets none |
| `--bare` | build | stack the slides, without the presenting script |
| `--pdf` | build | write a PDF, one page per slide, with an outline — text and shapes as vectors (also chosen by `-o file.pdf`) |
| `--pdf-raster` | build | write the PDF with each page one image of the slide, as before the vector PDF |
| `--notes` | build | write the speaker notes as a text file, slides separated by `---` exactly as marp-cli does (also chosen by `-o file.txt`; the `notes` command prints them in another layout) |
| `--pdf-notes` | build | add each slide's speaker notes to its PDF page as a note annotation, as marp-cli does: a yellow Note icon bottom left, the deck's `author` as its title. Asks for a PDF when nothing else says what to write |
| `--pdf-outlines`, `--pdf-outlines.pages`, `--pdf-outlines.headings` | build | the PDF's bookmarks: "Page N" per slide (bold) and the slide's headings nested by level under it. **On by default here, off in marp-cli**: go-slides has always written an outline, it costs a few hundred bytes and makes a deck navigable. `=false` (or marp-cli's `--no-pdf-outlines`, `--no-pdf-outlines.pages`, `--no-pdf-outlines.headings`) turns it, or a part, off. A heading bookmark jumps to its slide; marp-cli also scrolls to the heading |
| `--pptx` | build | write an editable PowerPoint deck — text boxes, shapes, pictures, fonts embedded — with its speaker notes (also chosen by `-o file.pptx`) |
| `--pptx-raster` | build | write a PowerPoint deck of one picture per slide, at scale 2 by default, as marp-cli does |
| `--odp` | build | write an editable OpenDocument presentation, with its speaker notes (also chosen by `-o file.odp`) |
| `--odp-raster` | build | write an OpenDocument presentation of one picture per slide |
| `--images png\|jpeg` | build | write one image per slide: `deck.001.png`, … |
| `--image png\|jpeg` | build | write the first slide as an image (also chosen by `-o file.png` / `.jpg`) |
| `--jpeg-quality n` | build | JPEG quality, 1 to 100 (default 85) |
| `--image-scale n` | build | device pixels per CSS pixel for images and PDF (default 1) |
| `--allow-local-files` | build | let images and PDF read local files, from under the deck's directory only |
| `--theme name\|file.css` | build | override the deck's theme: a theme name, or a CSS file (whatever `@theme` it declares); a directory is refused, use `--theme-set` |
| `--title`, `--description`, `--author`, `--keywords`, `--url`, `--og-image` | build | the page's metadata, over the deck's `title`, `description`, `author`, `keywords`, `url` and `image` directives, as in marp-cli. A deck with no title takes its first top-level heading's text; `--keywords` is comma-separated; an invalid `--url` is ignored with a warning |
| `--theme-set path` | all | a theme CSS file or a directory of them (repeatable) |
| `--kinds file` | all | a YAML or HCL (`*.hcl`) file of kinds (repeatable) |
| `--data file`, `--set k=v` | all | template data: YAML, JSON or HCL (`*.hcl`) files (repeatable) |
| `--no-template`, `--strict-yaml` | all | plain Marpit behaviour |
| `--config file`, `--no-config` | all | `slides.hcl`, else `slides.yaml`, is read from the deck's directory by default |

```yaml
# slides.yaml
kinds: kinds.yaml        # or a mapping, or a list of files
themes: [themes/acme.css]
data: {company: ACME}    # or file paths
html: false
lang: en
```

```hcl
# slides.hcl, the same in HCL
kinds  = "kinds.hcl"
themes = ["themes/acme.css"]
data   = { company = "ACME", year = 2026 }
html   = false
lang   = "en"
kind "quote" {
  extends = "blank"
  footer  = "${data.company} ${data.year}"
}
```

The HTML page is self-contained. Navigate with the arrow keys, Space and
PageUp/PageDown, `f` toggles full screen, a click advances, and the URL hash
names the slide. Fragments reveal one at a time. Printing gives one slide per
page.

## Watch, serve, preview

`--watch`, `--server` and `--preview` are marp-cli's, with its routes, query
keys and live reload protocol (read from its 4.5.1 sources), and with the
security a local server needs:

- **`--server dir`** serves the directory: files as they are, decks converted
  per request (`?pdf`, `?pptx`, `?png`, `?jpg`, `?notes`; the PPTX comes as an
  attachment; a failing deck answers 503 with its message), `index.md` or
  `PITCHME.md` as a directory's page, else a listing with links to each deck's
  PDF and PPTX. Pages open a WebSocket to `/.__marp-cli-watch-notifier__/<id>`
  and reload when the deck, or anything under the directory, changes. At most a
  few decks are converted at once.
- **Bound to the loopback** (marp-cli listens on every interface). `--host`
  changes it and warns. The **Host** header must be a loopback name (or the
  host it listens on), which stops DNS rebinding; a WebSocket's **Origin** must
  be the server's own; cross-site subresource requests (`<script src>`,
  `fetch`) are refused by their `Sec-Fetch-*` headers. A path is cleaned,
  its symbolic links are resolved and the result must be inside the directory
  (`..` and a link out of it are refused, `?pdf` included); dotfiles
  (`.git`, `.env`) are never served; only GET and HEAD are answered.
- **`--watch`** builds again the deck that changes and, if a theme, kinds,
  data or configuration file changed, all of them; a deck that fails is
  reported and watched still. The pages it writes open a socket on a loopback
  port (37717 or the next free one) whose address holds the SHA-256 of the
  deck's path; the socket accepts the `null` origin of a file page and the
  loopback's, nothing else.
- **`--preview`** opens the default browser (`open`, `xdg-open`,
  `rundll32`) on the server, or on the HTML pages just built (the first only
  when there are more than three decks). There is no window of its own: unlike
  marp-cli's, which drives Chrome, `slides` has none to drive.

## Images and PDF without a browser

Marp CLI drives a headless Chrome for its PDF and image output. `slides` uses
the go-* libraries instead:

- [go-webengine](https://github.com/go-webengine/engine) lays out and paints
  each slide. The slides go through the box frame (`deck.FrameBox`), which
  needs neither `<foreignObject>` nor generated content: the theme's
  `section::after` is lowered to a real element carrying its evaluated content.
- [go-pdfkit](https://github.com/go-pdfkit/pdfkit) writes the PDF: one page
  per slide at the slide's size, and an outline of the slides and their
  headings. The pages are **vectors**: go-webengine draws the laid-out slide
  through its `pdfcanvas` instead of into pixels, so the text is text in the
  fonts it was laid out with (subset-embedded, selectable, searchable),
  backgrounds, borders and gradients are paths and shadings, an image used
  on many slides is embedded once, and links are clickable. An element with
  no vector form here (`opacity`, a `filter`, a blurred shadow, a fading
  gradient) is drawn as a bitmap of that element only, its text kept
  selectable over it; formulas are still bitmaps. `--pdf-raster` writes one
  image per page instead.
- The fonts the built-in themes ask for (Lato, Roboto Mono) come from
  [go-opentype/fonts](https://github.com/go-opentype/fonts), embedded, so no
  render touches the network.

- `--image-scale 2` renders at two device pixels per CSS pixel (2560×1440 for
  16:9). Text and vectors are drawn at that density, not enlarged.
- Local images are refused unless `--allow-local-files` is given, as with
  marp-cli. Even then, only files under the deck's directory are read: a deck
  from elsewhere cannot reach `~/.ssh`.

Several and split backgrounds (`![bg left:40%]`) render as Marp's layers do.

Slides render four at a time and are written as they come, so a long deck is
never held in memory as images. Against marp-cli 4.5.1 on the same machine, a
100-slide deck exports to PNG in 0.97 s against 5.4 s and to PDF in 0.31 s
against 1.8 s, in a ninth of the memory; the PDF is 758 KiB against marp-cli's
837 KiB (10 MiB when its pages were images). See [BENCHMARKS.md](BENCHMARKS.md).

Lato (Regular, Bold, Black) and Roboto Mono (Regular, Bold) are embedded
upright and italic, so gaia's headings, code and emphasis render with their
own faces.

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

## PowerPoint and OpenDocument

`--pptx` writes the deck through
[go-ooxml](https://github.com/go-ooxml/ooxml)'s `WritePresentation`, and
`--odp` through [go-odf](https://github.com/go-odf/odf)'s, with the speaker
notes and the deck's title, author, description and keywords.

The slides are **editable** — marp-cli's PPTX is one picture a slide. Each
slide is drawn from its layout, the one the PNG and PDF exports draw, by
go-webengine's `officecanvas`: one text box per line of text, in the fonts
the slide was laid out with (embedded in the file, so the slide looks the
same on a machine without Lato or Inter), with bold, italic, colours, letter
spacing and links; backgrounds, borders and rounded boxes as shapes;
gradients as gradient fills (as pictures in the ODP, whose gradients have two
colours); pictures stored once however many slides show them. What a slide
cannot express — an element under `opacity`, a `filter`, a blurred shadow, a
formula — is a picture of that element, its text kept over it. Rendered back
by LibreOffice, a slide is within 0.04–2 % of the PNG export's pixels
(`go run ./internal/cmd/officecheck`).

`--pptx-raster` and `--odp-raster` write one picture a slide instead, as
before. Pictures render at scale 2 unless `--image-scale` says otherwise.

## Fitting headings

`# <!-- fit --> Title` scales the heading so its one line fills the slide's
width, up or down, as Marp Core does. The HTML page and the image, PDF and
PPTX outputs all run the same small script: it measures the heading's natural
width and scales its font size by the ratio. Only slides with a fitting heading
run a script when exporting.

## Emoji

Emoji shortcodes (`:smile:`, `:+1:`, `:heart_eyes:`: markdown-it-emoji's 1,903
names, which are the ones Marp Core knows) and Unicode emoji (😄, 👩‍💻, 🇫🇷, 1️⃣)
become [Twemoji](https://github.com/jdecked/twemoji) images, exactly as Marp
Core writes them:

```html
<img class="emoji" draggable="false" alt="😄" src="data:image/svg+xml;base64,…" data-marp-twemoji=""/>
```

Marp Core's `src` is a CDN URL, which a deck shown offline, printed or turned
into a PDF cannot fetch; here it is a `data:` URI of the SVG, taken from the
embedded set of [go-icons/twemoji](https://github.com/go-icons/twemoji) (2 MB
compressed, read lazily: a deck without emoji pays nothing). The rule that
sizes the image (`img[data-marp-twemoji]`, one `em`) is added to the theme as
Marp Core adds it. HTML, images, PDF, PPTX and ODP all draw them. The SVG is
rasterised by go-webengine through
[go-gfx/oksvg](https://github.com/go-gfx/oksvg), the fleet's fork of oksvg with
`clipPath` and correct multi-set arc commands: all 4,009 graphics are drawn, and
the ones that srwiley/oksvg got wrong (🪇, about 200 in all) match Chrome.

The rules are Marp Core's, pinned by `testdata/oracle/emoji.md`:

- Shortcodes are recognized in text, not in code; Unicode emoji are also
  converted inside code spans and blocks, as Marp Core does.
- Text-default characters (`©` `™` `✔` `▶`) stay text unless followed by
  U+FE0F (`✔️`); `❤` is the exception. A U+FE0E after an emoji keeps it text.
- A heading's `id` and outline use the emoji a shortcode stands for.
- `--no-emoji` (`Config.NoEmoji`, Marp Core's `emoji: false`) leaves all of it
  as text.

Where it differs, it converts more: `✌️ ☝️ ✍️ 🏋️` and a few other text-default
characters that Marp Core's emoji-regex leaves as text. See the
[twemoji README](https://github.com/go-icons/twemoji#reading-text-the-way-marp-core-does).

**Attribution.** The graphics are Twemoji, Copyright 2020 Twitter, Inc and other
contributors, licensed [CC-BY 4.0](https://creativecommons.org/licenses/by/4.0/),
continued by the [jdecked/twemoji](https://github.com/jdecked/twemoji) project.
Decks that show them should credit them.

## Packages

| | |
|---|---|
| [`deck`](deck) | parses a deck: directives, kinds, images, notes; renders slides |
| [`interp`](interp) | the template stage |
| [`theme`](theme) | Marp themes and the Marpit theme pipeline |
| [`viewer`](viewer) | the HTML page |
| [`export`](export) | images and PDF, through go-webengine and go-pdfkit |
| [`slides`](slides.go) | wires them together |
| [`project`](project) | the configuration a deck is built with: built-ins, `slides.hcl`/`slides.yaml`, then the flags (after v0.5.0) |
| [`vfs`](vfs) | the files a build reads: the disk, or a map an editor hands over, which reports what it lacked (after v0.5.0) |
| [`editor`](editor) | for editors, as data: the preview, problems positioned on lines, completion and hover (after v0.5.0) |

The [VS Code extension](https://github.com/go-slides/vscode) runs `editor`
compiled to WebAssembly: decks preview in VS Code's Markdown preview, on
vscode.dev too, with problems, completion and export through `slides`.

## License

BSD-3-Clause. The built-in themes in [`theme/marp`](theme/marp) are Marp
Core's, under its MIT license ([`theme/marp/LICENSE`](theme/marp/LICENSE)).
The emoji graphics are Twemoji, CC-BY 4.0 (see [Emoji](#emoji)).

HCL support links [hashicorp/hcl/v2](https://github.com/hashicorp/hcl)
(MPL-2.0, used unmodified as a library) and
[zclconf/go-cty](https://github.com/zclconf/go-cty) (MIT).
