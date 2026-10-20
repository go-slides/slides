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
fs.rmSync(dir, { recursive: true })
