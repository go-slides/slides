// Drives the page and its presenter view in headless Chrome: the p key opens
// the presenter window, the two follow each other through localStorage, the
// next-step frame is told by message, the notes are escaped text.
// usage: PUPPETEER_CORE=path CHROME=path node presenter-check.js file.html [screenshot-dir]
// It prints what it saw as JSON; TestPresenterInChrome checks it.
const puppeteer = require(process.env.PUPPETEER_CORE)
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
;(async () => {
  const browser = await puppeteer.launch({ executablePath: process.env.CHROME, headless: true, args: ['--no-sandbox'] })
  const main = await browser.newPage()
  const errors = []
  main.on('pageerror', (e) => errors.push('main: ' + e.message))
  await main.setViewport({ width: 1100, height: 650 })
  await main.goto('file://' + process.argv[2])
  const active = (p) => p.evaluate(() => [...document.querySelectorAll('div.marpit > svg[data-marpit-svg]')].findIndex((s) => s.classList.contains('active')))
  const r = {}
  r.mainAccessibleText = await main.evaluate(() => document.body.innerText)
  r.mainNoteDisplay = await main.evaluate(() => getComputedStyle(document.querySelector('.slides-note')).display)
  const [pres] = await Promise.all([new Promise((res) => main.once('popup', res)), main.keyboard.press('p')])
  pres.on('pageerror', (e) => errors.push('presenter: ' + e.message))
  await pres.waitForSelector('.slides-pv-info')
  await sleep(600)
  r.presUrl = pres.url().replace(/sync=[a-z0-9]+/, 'sync=KEY').replace(/^file:.*\/(?=[^/]*\?)/, '')
  r.presTitle = await pres.title()
  r.syncParamRemoved = !/sync=/.test(await pres.evaluate(() => location.search))
  r.presPage = await pres.$eval('.slides-pv-page', (e) => e.textContent)
  r.presNote = await pres.$eval('.slides-note.active', (e) => e.innerHTML)
  r.presNoteBold = await pres.$$eval('.slides-pv-wrap b', (e) => e.length)
  const frame = pres.frames().find((f) => f.url().includes('view=next'))
  r.nextFrameActive = await active(frame)
  // the main window moves: the presenter follows, its next frame too
  await main.keyboard.press('ArrowRight')
  await sleep(500)
  r.afterMainRight = { main: await active(main), presenter: await active(pres), page: await pres.$eval('.slides-pv-page', (e) => e.textContent), note: await pres.$eval('.slides-note.active', (e) => e.textContent), next: await active(frame) }
  // the presenter moves: the main window follows (the fragment first)
  await pres.keyboard.press('ArrowRight')
  await sleep(500)
  r.afterPresRight1 = { main: await active(main), presenter: await active(pres), fragmentsShown: await main.evaluate(() => document.querySelectorAll('.hidden-fragment').length), next: await active(frame) }
  await pres.keyboard.press('ArrowRight')
  await pres.keyboard.press('ArrowRight')
  await sleep(500)
  r.afterPresRight3 = { main: await active(main), presenter: await active(pres), nextDisabled: await pres.$eval('.slides-pv-info button:last-of-type', (e) => e.disabled) }
  await pres.keyboard.press('ArrowLeft')
  await sleep(400)
  r.afterPresLeft = { main: await active(main), presenter: await active(pres) }
  // notes font, timer, click on the next frame
  await pres.keyboard.press('+')
  r.noteScale = await pres.$eval('.slides-pv-notes', (e) => e.style.getPropertyValue('--slides-note-scale'))
  r.timer = await pres.$eval('.slides-pv-timer', (e) => e.textContent)
  await pres.click('.slides-pv-next')
  await sleep(400)
  r.afterNextClick = { main: await active(main), presenter: await active(pres) }
  // the state shared through localStorage
  r.stored = await main.evaluate(() => Object.entries(localStorage).filter(([k]) => k.startsWith('bespoke-marp-sync-')).map(([k, v]) => v))
  if (process.argv[3]) {
    await pres.screenshot({ path: process.argv[3] + '/presenter.png' })
    await main.screenshot({ path: process.argv[3] + '/main.png' })
  }
  // closing the presenter leaves one reference
  await pres.close()
  await sleep(300)
  r.storedAfterClose = await main.evaluate(() => Object.entries(localStorage).filter(([k]) => k.startsWith('bespoke-marp-sync-')).map(([k, v]) => v))
  r.errors = errors
  await browser.close()
  console.log(JSON.stringify(r, null, 1))
})().catch((e) => { console.error(e); process.exit(1) })
