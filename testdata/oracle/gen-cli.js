// Regenerates cli.json: what marp-cli itself writes into the head of the page
// for a deck and a command line, which cmd/slides' TestCLIOracle compares
// with. Marp Core cannot say, since the metadata and --theme are marp-cli's.
//
//   node testdata/oracle/gen-cli.js </dev/null
//
// needs the `marp` binary of marp-cli 4.5.1 on the PATH.
const fs = require('fs')
const os = require('os')
const path = require('path')
const { execFileSync } = require('child_process')

const cases = [
  { name: 'title-from-headings', md: '## Sub\n\n# Top\n\n---\n\n# Other\n', args: [] },
  { name: 'title-first-of-lowest-level', md: '### c\n\n## B\n\n---\n\n## A\n', args: [] },
  { name: 'title-markup-kept', md: '# **Bold** and `code` title\n', args: [] },
  { name: 'title-none', md: 'just text\n', args: [] },
  { name: 'title-front-matter', md: '---\ntitle: From front matter\n---\n\n# Heading\n', args: [] },
  { name: 'title-comment', md: '# Heading\n\n<!-- title: From comment -->\n', args: [] },
  { name: 'title-cli-over-front-matter', md: '---\ntitle: FM\n---\n\n# Heading\n', args: ['--title', 'CLI'] },
  { name: 'title-cli-over-comment', md: '# Heading\n\n<!-- title: Late -->\n', args: ['--title', 'CLI'] },
  { name: 'title-number', md: '---\ntitle: 123\n---\n\n# Heading\n', args: [] },
  { name: 'meta-all-cli', md: '# H\n', args: ['--title', 'T', '--description', 'D', '--author', 'A', '--keywords', 'x, y,x,,z', '--url', 'https://example.org/deck/', '--og-image', 'https://example.org/i.png'] },
  { name: 'meta-front-matter', md: '---\ndescription: FMD\nauthor: FMA\nkeywords: p, q\nurl: https://example.org/fm\nimage: https://example.org/fm.png\n---\n\n# H\n', args: [] },
  { name: 'meta-front-matter-over-by-cli', md: '---\ndescription: FMD\nauthor: FMA\nkeywords: p, q\nurl: https://example.org/fm\nimage: https://example.org/fm.png\n---\n\n# H\n', args: ['--author', 'CLI', '--keywords', 'k'] },
  { name: 'keywords-list', md: '---\nkeywords: [a, b, a, " c "]\n---\n\n# H\n', args: [] },
  { name: 'keywords-dedupe', md: '---\nkeywords: a, b, a,, c\n---\n\n# H\n', args: [] },
  { name: 'url-invalid', md: '---\nurl: not a url\n---\n\n# H\n', args: [] },
  { name: 'url-invalid-cli', md: '# H\n', args: ['--url', 'nope'] },
  { name: 'meta-escaped', md: '# H\n', args: ['--description', 'a & b <c>'] },
]

const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'slides-cli-'))
const field = (head, re) => {
  const m = head.match(re)
  return m ? m[1] : null
}
const out = []
for (const c of cases) {
  const md = path.join(dir, c.name + '.md')
  const html = path.join(dir, c.name + '.html')
  fs.writeFileSync(md, c.md)
  let log = ''
  try {
    log = execFileSync('marp', [md, '-o', html, ...c.args], { stdio: ['ignore', 'pipe', 'pipe'] }).toString()
  } catch (e) {
    log = String(e.stderr)
  }
  const head = fs.readFileSync(html, 'utf8').split('</head>')[0]
  const meta = (attr, key) => field(head, new RegExp(`<meta ${attr}="${key}" content="([^"]*)"`))
  out.push({
    name: c.name,
    md: c.md,
    args: c.args,
    title: field(head, /<title>([^<]*)<\/title>/),
    description: meta('name', 'description'),
    author: meta('name', 'author'),
    keywords: meta('name', 'keywords'),
    ogURL: meta('property', 'og:url'),
    ogImage: meta('property', 'og:image'),
    canonical: field(head, /<link rel="canonical" href="([^"]*)"/),
  })
}
fs.writeFileSync(path.join(__dirname, 'cli.json'), JSON.stringify(out, null, 1) + '\n')

// The transition directive: the <section> tags of the slides, with the
// data-transition attributes marp-cli writes, and the built-in keyframes it
// puts in the page.
const transitionCases = [
  { name: 'transition-first', md: '# A\n\n<!-- transition: fade 1s -->\n\n---\n\n# B\n\n---\n\n# C\n' },
  { name: 'transition-spot', md: '# A\n\n---\n\n<!-- _transition: cover -->\n\n# B\n\n---\n\n# C\n' },
  { name: 'transition-with-others', md: '---\npaginate: true\n---\n\n<!-- class: lead -->\n<!-- transition: slide 300ms -->\n\n# A\n\n---\n\n<!-- _backgroundColor: red -->\n\n# B\n' },
  { name: 'transition-unknown-name', md: '<!-- transition: my-own -->\n\n# A\n\n---\n\n# B\n' },
  { name: 'transition-none', md: '<!-- transition: none -->\n\n# A\n\n---\n\n# B\n\n<!-- transition: clockwise -->\n\n---\n\n# C\n' },
  { name: 'transition-three-builtins', md: '<!-- transition: swipe -->\n\n# A\n\n---\n\n<!-- transition: fade -->\n\n# B\n\n---\n\n<!-- transition: swipe -->\n\n# C\n' },
  { name: 'transition-extra-words', md: '<!-- transition: fade 2s ignored words -->\n\n# A\n\n---\n\n# B\n' },
  { name: 'transition-no-note', md: '# A\n\n<!-- transition: fade -->\n\n<!-- a real note -->\n' },
]
const transitions = transitionCases.map((c) => {
  const md = path.join(dir, c.name + '.md')
  const html = path.join(dir, c.name + '.html')
  fs.writeFileSync(md, c.md)
  execFileSync('marp', [md, '-o', html, '--lang', 'C'], { stdio: ['ignore', 'pipe', 'pipe'] })
  const page = fs.readFileSync(html, 'utf8')
  const notes = path.join(dir, c.name + '.txt')
  execFileSync('marp', [md, '--notes', '-o', notes], { stdio: ['ignore', 'pipe', 'pipe'] })
  // the keyframe names of the built-ins the page carries, in order
  const builtins = [...new Set([...page.matchAll(/@keyframes marp-(?:incoming-|outgoing-)?transition-(?:backward-)?__builtin__([a-z-]+)/g)].map((m) => m[1]))]
  return {
    name: c.name,
    md: c.md,
    sections: [...page.matchAll(/<section[^>]*>/g)].map((m) => m[0]),
    builtins,
    notes: fs.readFileSync(notes, 'utf8'),
  }
})
fs.writeFileSync(path.join(__dirname, 'transitions.json'), JSON.stringify(transitions, null, 1) + '\n')

// marp-cli's configuration file: where it is found from, how the command line
// and the file combine. Each case is a tree of files, a working directory
// inside it, and the arguments; what is compared is the head of the page.
const configCases = [
  { name: 'json', files: { '.marprc.json': '{"title":"T","author":"A","keywords":["x","y"],"lang":"fr","theme":"gaia"}' } },
  { name: 'yaml', files: { '.marprc.yaml': 'title: T\nauthor: A\nkeywords: x, y\nlang: fr\ntheme: gaia\n' } },
  { name: 'marprc', files: { '.marprc': 'title: T-from-marprc\n' } },
  { name: 'marp-config-json', files: { 'marp.config.json': '{"title":"T-from-marp.config.json"}' } },
  { name: 'marp-config-yaml', files: { 'marp.config.yaml': 'title: T-from-marp.config.yaml\n' } },
  { name: 'package-json', files: { 'package.json': '{"name":"x","marp":{"title":"T-from-package"}}' } },
  { name: 'package-json-without-marp', files: { 'package.json': '{"name":"x"}', '.marprc': 'title: T-after-package\n' } },
  { name: 'parent-dir', files: { '.marprc.json': '{"title":"T-from-parent"}' }, cwd: 'sub/deep' },
  { name: 'nearest-wins', files: { '.marprc.json': '{"title":"far"}', 'sub/.marprc.json': '{"title":"near"}' }, cwd: 'sub' },
  { name: 'place-order', files: { '.marprc.json': '{"title":"marprc.json"}', '.marprc': 'title: marprc\n', 'marp.config.json': '{"title":"marp.config.json"}' } },
  { name: 'cli-wins', files: { '.marprc.json': '{"title":"config","author":"config author","description":"config description"}' }, args: ['--title', 'from the command line'] },
  { name: 'explicit-file', files: { 'my.json': '{"title":"explicit"}', '.marprc.json': '{"title":"searched"}' }, args: ['--config-file', 'my.json'] },
  { name: 'no-config-file', files: { '.marprc.json': '{"title":"ignored"}' }, args: ['--no-config-file'] },
  { name: 'no-config', files: { '.marprc.json': '{"title":"ignored"}' }, args: ['--no-config'] },
  { name: 'url-and-image', files: { '.marprc.json': '{"url":"https://example.org/c","ogImage":"https://example.org/i.png","description":"D"}' } },
  { name: 'unknown-key', files: { '.marprc.json': '{"title":"T","nonsense":1}' } },
]
const configOut = configCases.map((c) => {
  const root = fs.mkdtempSync(path.join(dir, 'cfg-'))
  for (const [f, body] of Object.entries(c.files)) {
    fs.mkdirSync(path.dirname(path.join(root, f)), { recursive: true })
    fs.writeFileSync(path.join(root, f), body)
  }
  const cwd = path.join(root, c.cwd || '')
  fs.mkdirSync(cwd, { recursive: true })
  fs.writeFileSync(path.join(cwd, 'deck.md'), '# H\n\n---\n\n# I\n')
  let log = ''
  try {
    log = execFileSync('marp', ['deck.md', '-o', 'out.html', ...(c.args || [])], { cwd, stdio: ['ignore', 'pipe', 'pipe'] }).toString()
  } catch (e) {
    log = String(e.stderr)
  }
  const page = fs.readFileSync(path.join(cwd, 'out.html'), 'utf8')
  const head = page.split('</head>')[0]
  const meta = (attr, key) => field(head, new RegExp(`<meta ${attr}="${key}" content="([^"]*)"`))
  return {
    name: c.name,
    files: c.files,
    cwd: c.cwd || '',
    args: c.args || [],
    title: field(head, /<title>([^<]*)<\/title>/),
    author: meta('name', 'author'),
    description: meta('name', 'description'),
    keywords: meta('name', 'keywords'),
    canonical: field(head, /<link rel="canonical" href="([^"]*)"/),
    ogImage: meta('property', 'og:image'),
    lang: field(page, /<html lang="([^"]*)"/),
    theme: field(page, /data-theme="([^"]*)"/),
  }
})
fs.writeFileSync(path.join(__dirname, 'config.json'), JSON.stringify(configOut, null, 1) + '\n')

// --notes: the text file marp-cli writes.
const noteCases = [
  { name: 'notes-some', md: '# One\n\n<!-- note one -->\n\n<!-- more -->\n\n---\n\n## Two\n\nno note\n\n---\n\n# Three\n\n<!-- third -->\n' },
  { name: 'notes-none', md: '# One\n\n---\n\n# Two\n' },
  { name: 'notes-directives-only', md: '<!-- paginate: true -->\n\n# One\n\n---\n\n<!-- _class: x -->\n\n# Two\n\n<!-- last -->\n' },
]
const notes = noteCases.map((c) => {
  const md = path.join(dir, c.name + '.md')
  const txt = path.join(dir, c.name + '.txt')
  fs.writeFileSync(md, c.md)
  execFileSync('marp', [md, '--notes', '-o', txt], { stdio: ['ignore', 'pipe', 'pipe'] })
  return { name: c.name, md: c.md, text: fs.readFileSync(txt, 'utf8') }
})
fs.writeFileSync(path.join(__dirname, 'notes.json'), JSON.stringify(notes, null, 1) + '\n')

// --pdf-notes and --pdf-outlines: the PDFs marp-cli writes through Chrome
// and pdf-lib; TestPDFNotesOracle reads their outline and annotations back.
const pdfMD = '---\nauthor: Ada\n---\n\n# One\n\n<!-- note one -->\n\n<!-- more -->\n\n---\n\n## Two\n\nno note\n\n---\n\n# Three\n\n### sub\n\n<!-- third -->\n'
fs.writeFileSync(path.join(__dirname, 'pdf.src.md'), pdfMD)
for (const [name, args] of [
  ['both', ['--pdf-outlines', '--pdf-notes']],
  ['pages', ['--pdf-outlines', '--no-pdf-outlines.headings']],
  ['headings', ['--pdf-outlines', '--no-pdf-outlines.pages']],
  ['none', []],
]) {
  execFileSync('marp', [path.join(__dirname, 'pdf.src.md'), '--pdf', ...args, '-o', path.join(__dirname, `pdf-${name}.pdf`)], { stdio: ['ignore', 'pipe', 'pipe'] })
}
fs.rmSync(dir, { recursive: true })
