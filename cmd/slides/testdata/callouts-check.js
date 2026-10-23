// Hovers and focuses code callouts in headless Chrome: a disc shows its
// note, a note its disc, by the keyboard as by the pointer, with no script of
// ours; the transition follows prefers-reduced-motion.
// usage: PUPPETEER_CORE=path CHROME=path node callouts-check.js file.html
const puppeteer = require(process.env.PUPPETEER_CORE)
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
;(async () => {
  const browser = await puppeteer.launch({ executablePath: process.env.CHROME, headless: true, args: ['--no-sandbox'] })
  const page = await browser.newPage()
  const errors = []
  page.on('pageerror', (e) => errors.push(e.message))
  await page.setViewport({ width: 1280, height: 720 })
  await page.goto('file://' + process.argv[2])
  await sleep(300)
  // which notes are tinted, which discs ringed, on the first slide
  const state = () => page.evaluate(() => {
    const s = document.querySelector('section')
    return {
      notes: [...s.querySelectorAll('ol.co-list>li')].filter((li) => getComputedStyle(li).backgroundColor !== 'rgba(0, 0, 0, 0)').map((li) => li.dataset.co),
      discs: [...s.querySelectorAll('pre .co-disc')].filter((d) => getComputedStyle(d).outlineStyle === 'solid').map((d) => d.dataset.co),
    }
  })
  const r = {}
  r.rest = await state()
  await page.hover('section pre .co-disc[data-co="2"]')
  await sleep(250)
  r.hoverDisc2 = await state()
  await page.hover('section ol.co-list>li[data-co="1"]')
  await sleep(250)
  r.hoverNote1 = await state()
  await page.mouse.move(5, 5)
  await sleep(250)
  r.away = await state()
  // the keyboard: Tab until a disc has the focus
  for (let i = 0; i < 30; i++) {
    await page.keyboard.press('Tab')
    if (await page.evaluate(() => document.activeElement && document.activeElement.classList.contains('co-disc'))) break
  }
  r.focused = await page.evaluate(() => document.activeElement.dataset.co)
  await sleep(250)
  r.focusDisc = await state()
  await page.keyboard.press('Tab')
  await page.keyboard.press('Tab')
  r.focused2 = await page.evaluate(() => (document.activeElement.tagName + ':' + document.activeElement.dataset.co))
  await sleep(250)
  r.focusNote = await state()
  // what a screen reader is given: the disc's description is its note
  r.described = await page.evaluate(() => {
    const d = document.querySelector('section pre .co-disc[data-co="2"]')
    const note = document.getElementById(d.getAttribute('aria-describedby'))
    return [d.getAttribute('aria-hidden'), note && note.textContent.trim()]
  })
  r.transition = await page.$eval('section ol.co-list>li', (li) => getComputedStyle(li).transitionDuration)
  await page.emulateMediaFeatures([{ name: 'prefers-reduced-motion', value: 'reduce' }])
  r.reducedTransition = await page.$eval('section ol.co-list>li', (li) => getComputedStyle(li).transitionDuration)
  r.scripts = await page.evaluate(() => [...document.scripts].filter((s) => /co-disc|co-list/.test(s.textContent)).length)
  r.errors = errors
  await browser.close()
  console.log(JSON.stringify(r, null, 1))
})().catch((e) => { console.error(e); process.exit(1) })
