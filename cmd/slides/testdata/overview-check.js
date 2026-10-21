// Drives the overview view in headless Chrome, over the page and over the
// presenter view: o and Escape, the grid, the arrows, Enter and a click, the
// windows following one another, the accessibility attributes.
// usage: PUPPETEER_CORE=path CHROME=path node overview-check.js file.html [screenshot-dir]
const puppeteer = require(process.env.PUPPETEER_CORE)
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
;(async () => {
  const browser = await puppeteer.launch({ executablePath: process.env.CHROME, headless: true, args: ['--no-sandbox'] })
  const main = await browser.newPage()
  const errors = []
  main.on('pageerror', (e) => errors.push('main: ' + e.message))
  await main.setViewport({ width: 700, height: 600 })
  await main.goto('file://' + process.argv[2])
  const active = (p) => p.evaluate(() => [...document.querySelectorAll('div.marpit > svg[data-marpit-svg]')].findIndex((s) => s.classList.contains('active')))
  const frameOf = (p) => p.frames().find((f) => f.url().includes('view=overview'))
  const open = (p) => p.evaluate(() => { const b = document.querySelector('.slides-overview'); return !!b && b.dataset.open === '1' && !b.inert })
  const r = {}
  r.closedAtStart = await main.evaluate(() => !document.querySelector('.slides-overview'))
  await main.keyboard.press('o')
  await sleep(700)
  r.openAfterO = await open(main)
  const f = frameOf(main)
  r.url = f.url().replace(/sync=[a-z0-9]+/, 'sync=KEY').replace(/^file:.*\/(?=[^/]*\?)/, '')
  r.title = await f.title()
  r.iframeTitle = await main.$eval('.slides-overview iframe', (e) => e.title)
  r.grid = await f.evaluate(() => [...document.querySelectorAll('div.marpit > svg[data-marpit-svg]')].map((s) => { const b = s.getBoundingClientRect(); return [Math.round(b.left), Math.round(b.top), getComputedStyle(s).display, s.getAttribute('role'), s.getAttribute('aria-label'), s.tabIndex] }))
  r.closeButton = await f.evaluate(() => { const b = document.querySelector('.slides-ov-head button'); return b && b.title })
  r.currentInOverview = await active(f)
  if (process.argv[3]) await main.screenshot({ path: process.argv[3] + '/overview.png' })
  // arrows choose the slide, and the page follows
  await f.evaluate(() => window.focus())
  await main.keyboard.press('ArrowRight')
  await sleep(400)
  r.afterRight = { overview: await active(f), main: await active(main), stillOpen: await open(main) }
  await main.keyboard.press('ArrowDown') // a row down: with two columns, 1 -> 3, which is past the end: stays
  await sleep(300)
  r.afterDown = { overview: await active(f), main: await active(main) }
  await main.keyboard.press('Home')
  await sleep(300)
  r.afterHome = { overview: await active(f), main: await active(main) }
  await main.keyboard.press('ArrowDown') // 0 -> 2
  await sleep(300)
  r.afterDownFromHome = { overview: await active(f), main: await active(main) }
  // Enter chooses the focused slide and closes the overview
  await main.keyboard.press('ArrowUp')
  await main.keyboard.press('Enter')
  await sleep(500)
  r.afterEnter = { main: await active(main), open: await open(main), inert: await main.$eval('.slides-overview', (e) => e.inert) }
  // Escape opens and closes it, as o does
  await main.keyboard.press('Escape')
  await sleep(500)
  r.escapeOpens = await open(main)
  await main.keyboard.press('Escape')
  await sleep(500)
  r.escapeCloses = !(await open(main))
  // a click on a slide chooses it
  await main.keyboard.press('o')
  await sleep(500)
  const box = await f.evaluate(() => { const b = document.querySelectorAll('div.marpit > svg[data-marpit-svg]')[2].getBoundingClientRect(); return [b.left + b.width / 2, b.top + b.height / 2] })
  await main.mouse.click(box[0], box[1])
  await sleep(500)
  r.afterClick = { main: await active(main), open: await open(main) }
  // the button of the controller
  await main.mouse.move(300, 300)
  await sleep(300)
  await main.$eval('.slides-osc button[aria-label="Overview view"]', (b) => b.click())
  await sleep(500)
  r.afterButton = await open(main)
  await main.keyboard.press('Escape')
  await sleep(400)
  // over the presenter view: its page counter opens it, and choosing a slide leaves it open
  const [pres] = await Promise.all([new Promise((res) => main.once('popup', res)), main.keyboard.press('p')])
  pres.on('pageerror', (e) => errors.push('presenter: ' + e.message))
  await pres.waitForSelector('.slides-pv-info')
  await sleep(600)
  await pres.click('.slides-pv-page')
  await sleep(700)
  r.presenterOpen = await open(pres)
  const pf = frameOf(pres)
  r.presenterFrameUrlCloseOnSelect = pf.url().includes('closeOnSelect')
  await pf.evaluate(() => document.querySelectorAll('div.marpit > svg[data-marpit-svg]')[1].dispatchEvent(new MouseEvent('click', { bubbles: true })))
  await sleep(500)
  r.presenterAfterPick = { open: await open(pres), presenter: await active(pres), main: await active(main) }
  if (process.argv[3]) await pres.screenshot({ path: process.argv[3] + '/presenter-overview.png' })
  await pf.evaluate(() => document.querySelector('.slides-ov-head button').click())
  await sleep(400)
  r.presenterClosed = !(await open(pres))
  r.errors = errors
  await browser.close()
  console.log(JSON.stringify(r, null, 1))
})().catch((e) => { console.error(e); process.exit(1) })
