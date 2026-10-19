// Regenerates the Marp Core reference outputs the Go tests compare with.
//
//   NODE_PATH=$(brew --prefix marp-cli)/libexec/lib/node_modules/@marp-team/marp-cli/node_modules \
//     node testdata/oracle/gen.js </dev/null
//
// Marp Core runs without inline SVG, math, emoji or highlighting, so that
// the output isolates what go-slides' deck package reproduces. The exception
// is emoji.md, which is the emoji deck: it runs with Marp Core's defaults for
// emoji (Twemoji SVG images for shortcodes and Unicode emoji).
const fs = require('fs')
const path = require('path')
const { Marp } = require('@marp-team/marp-core')
const dir = __dirname
const pkg = require('@marp-team/marp-core/package.json')
for (const f of fs.readdirSync(dir).filter((f) => f.endsWith('.md') && !f.endsWith('.src.md'))) {
  const marp = new Marp({
    inlineSVG: false,
    container: false,
    math: false,
    emoji: f === 'emoji.md' ? undefined : { shortcode: false, unicode: false },
    script: false,
  })
  marp.highlighter = () => ''
  const { html, css, comments } = marp.render(fs.readFileSync(path.join(dir, f), 'utf8'))
  const base = path.join(dir, f.replace(/\.md$/, ''))
  fs.writeFileSync(base + '.html', `<!-- marp-core ${pkg.version} -->\n` + html)
  fs.writeFileSync(base + '.notes.json', JSON.stringify(comments, null, 1) + '\n')
  if (f === 'emoji.md') {
    // The rule Marp Core adds to the theme for its emoji images.
    const i = css.indexOf('img[data-marp-twemoji]')
    fs.writeFileSync(base + '.rule.css', css.slice(i, css.indexOf('}', i) + 1) + '\n')
  }
}

// The theme pipeline, through plain Marpit with a custom theme.
{
  const { Marpit } = require('@marp-team/marpit')
  const marpit = new Marpit({ inlineSVG: false })
  marpit.themeSet.default = marpit.themeSet.add(fs.readFileSync(path.join(dir, 'theme-case.css'), 'utf8'))
  const { css } = marpit.render(fs.readFileSync(path.join(dir, 'theme-case.src.md'), 'utf8'))
  fs.writeFileSync(path.join(dir, 'theme-case.packed.css'), css + '\n')
}

// Inline SVG mode, for the advanced backgrounds.
{
  const marp = new Marp({ container: false, math: false, emoji: { shortcode: false, unicode: false }, script: false })
  marp.highlighter = () => ''
  const { html } = marp.render(fs.readFileSync(path.join(dir, 'images.md'), 'utf8'))
  fs.writeFileSync(path.join(dir, 'images.svg.html'), `<!-- marp-core ${pkg.version} -->\n` + html)
}
