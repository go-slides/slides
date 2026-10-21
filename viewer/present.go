package viewer

// presentJS shows one slide at a time. Fragments reveal one by one; the URL
// hash names the slide, so a reload or a link lands on it.
//
// It plays the part of marp-cli 4.5.1's bespoke.js for three views, chosen as
// there by the view query parameter: the page itself, the presenter view
// (?view=presenter&sync=KEY, opened by the p key) and the next-step view the
// presenter embeds (?view=next). The windows of a talk follow one another
// through localStorage, in marp-cli's format: the key
// bespoke-marp-sync-KEY holds {index, fragmentIndex, reference}, the
// storage event of the other windows carries a change, and the key is
// removed when the last window goes. No network is involved.
const presentJS = `
(() => {
  const OPT = /*OPTS*/;
  const Q = new URLSearchParams(location.search), VIEW = Q.get('view') || '';
  const nextView = VIEW === 'next', presenter = VIEW === 'presenter', overview = VIEW === 'overview';
  const slides = [...document.querySelectorAll('div.marpit > svg[data-marpit-svg]')];
  if (!slides.length) return;
  document.body.dataset.slidesView = document.documentElement.dataset.slidesView = VIEW;
  let bar = null;
  if (OPT.progress && !VIEW) {
    bar = document.createElement('div');
    bar.className = 'slides-progress';
    document.body.appendChild(bar);
  }
  const frags = s => [...s.querySelectorAll('[data-marpit-fragment]')]
    .sort((a, b) => a.dataset.marpitFragment - b.dataset.marpitFragment);
  let cur = 0, step = 0, osc = null, applying = false, ovOpen = false;
  const after = [];
  // A change of slide goes through here: if the slide being left has a
  // transition for the way it is left, the page plays it around the change
  // (not while the overview is up, nor in it).
  const go = (change, back) => {
    const t = window.__slidesTransit;
    if (ovOpen || overview || !(t && t(slides[cur], back, change))) change();
  };
  const show = (i, f) => {
    cur = Math.max(0, Math.min(slides.length - 1, i));
    const fs = frags(slides[cur]);
    step = f === undefined ? 0 : Math.max(0, Math.min(fs.length, f));
    slides.forEach((s, j) => s.classList.toggle('active', j === cur));
    fs.forEach((e, j) => e.classList.toggle('hidden-fragment', j >= step));
    if (bar) bar.style.width = (slides.length > 1 ? cur / (slides.length - 1) * 100 : 100) + '%';
    if (osc) osc.update(cur);
    const hash = '#' + (cur + 1);
    if (!nextView && !overview && location.hash !== hash) history.replaceState(history.state, '', hash);
    after.forEach(h => h());
  };
  const jump = (i, f) => {
    const to = Math.max(0, Math.min(slides.length - 1, i));
    if (to !== cur) go(() => show(to, f), to < cur); else show(to, f);
  };
  const next = () => {
    const n = frags(slides[cur]).length;
    if (step < n) show(cur, step + 1); else if (cur < slides.length - 1) go(() => show(cur + 1, 0), false);
  };
  const prev = () => {
    if (step > 0) show(cur, step - 1);
    else if (cur > 0) go(() => show(cur - 1, frags(slides[cur - 1]).length), true);
  };
  const fullscreen = () => document.fullscreenElement ? document.exitFullscreen() : document.documentElement.requestFullscreen();

  // The windows of a talk: a key shared through the page's history state and
  // the sync parameter, and the state in localStorage under it.
  let key = null, sk = '';
  const ls = (() => {
    try { localStorage.setItem('bespoke-marp', '1'); localStorage.removeItem('bespoke-marp'); } catch (e) { return null; }
    return {
      get: k => { try { return JSON.parse(localStorage.getItem(k)) || {}; } catch (e) { return {}; } },
      set: (k, v) => { try { localStorage.setItem(k, JSON.stringify(v)); } catch (e) {} },
      del: k => { try { localStorage.removeItem(k); } catch (e) {} },
    };
  })();
  let start = (parseInt(location.hash.slice(1), 10) || 1) - 1, startStep;
  if (ls && !nextView) {
    key = Q.get('sync') || (history.state && history.state.marpBespokeSyncKey) || Math.random().toString(36).slice(2);
    sk = 'bespoke-marp-sync-' + key;
    Q.delete('sync');
    const q = Q.toString();
    history.replaceState(Object.assign({}, history.state, { marpBespokeSyncKey: key }), '', location.pathname + (q ? '?' + q : '') + location.hash);
    const ref = d => {
      const s = ls.get(sk), n = (s.reference || 0) + d;
      if (n <= 0) ls.del(sk); else { s.reference = n; ls.set(sk, s); }
    };
    const was = ls.get(sk);
    if ((presenter || overview) && typeof was.index === 'number') { start = was.index; startStep = was.fragmentIndex; }
    ref(1);
    addEventListener('pagehide', () => ref(-1));
    after.push(() => {
      if (applying) return;
      const s = ls.get(sk);
      if (s.index === cur && s.fragmentIndex === step) return;
      s.index = cur; s.fragmentIndex = step; ls.set(sk, s);
    });
    addEventListener('storage', e => {
      if (e.key !== sk || !e.oldValue || !e.newValue) return;
      let o, n;
      try { o = JSON.parse(e.oldValue); n = JSON.parse(e.newValue); } catch (x) { return; }
      if (o.index === n.index && o.fragmentIndex === n.fragmentIndex) return;
      applying = true;
      try { jump(n.index, n.fragmentIndex); } finally { applying = false; }
    });
  }
  const openPresenter = () => {
    if (!sk) return;
    const q = new URLSearchParams(location.search);
    q.set('view', 'presenter');
    q.set('sync', key);
    const w = Math.max(Math.floor(innerWidth * .85), 640), h = Math.max(Math.floor(innerHeight * .85), 360);
    window.open(location.pathname + '?' + q + location.hash, 'bespoke-marp-presenter-' + key, 'width=' + w + ',height=' + h + ',menubar=no,toolbar=no');
  };

  // The overview view, over the page or the presenter view: this page again
  // in a frame (?view=overview&sync=KEY, closeOnSelect=1 over the page), a
  // grid of every slide. o or Escape opens and closes it, a slide chosen in it
  // is the slide here, through the shared state.
  let ovBox = null;
  const toggleOverview = open => {
    if (!sk || nextView || overview) return;
    if (!ovBox || !ovBox.isConnected) {
      ovBox = document.createElement('div');
      ovBox.className = 'slides-overview';
      ovBox.inert = true;
      const f = document.createElement('iframe'), q = new URLSearchParams(location.search);
      q.set('view', 'overview');
      q.set('sync', key);
      if (presenter) q.delete('closeOnSelect'); else q.set('closeOnSelect', '1');
      f.src = location.pathname + '?' + q;
      f.title = 'Overview';
      ovBox.append(f);
      document.body.append(ovBox);
    }
    const o = open === undefined ? !ovBox.dataset.open : open;
    setTimeout(() => {
      ovBox.dataset.open = o ? '1' : '';
      ovBox.inert = !o;
      ovOpen = o;
      const f = ovBox.querySelector('iframe');
      if (o) { try { f.contentWindow.focus(); } catch (e) { f.focus(); } } else window.focus();
    }, 0);
  };
  const closeOverview = () => window.parent.postMessage('closeOverview', window.origin === 'null' ? '*' : window.origin);
  if (!overview && !nextView) {
    addEventListener('message', e => { if (e.origin === window.origin && e.data === 'closeOverview') toggleOverview(false); });
  }

  if (nextView) {
    // The step after the one the presenter shows, told by message.
    addEventListener('message', e => {
      if (e.origin !== window.origin || typeof e.data !== 'string') return;
      const [k, v] = e.data.split(':');
      if (k !== 'navigate') return;
      let [o, f] = v.split(',').map(x => parseInt(x, 10));
      if (isNaN(o) || isNaN(f) || o < 0 || o >= slides.length) return;
      f += 1;
      if (f > frags(slides[o]).length) { o += 1; f = 0; }
      show(o, f);
    });
  } else if (overview) {
    // Every slide in a grid, shown whole. The arrows choose the slide (the
    // windows follow), Enter or a click chooses it and, over the page, closes
    // the overview; Home and End, o and Escape as elsewhere.
    const framed = window.parent !== window, close = !!Q.get('closeOnSelect');
    const pick = i => { jump(i, 1e9); slides[cur].focus({ preventScroll: true }); };
    const cx = i => { const r = slides[i].getBoundingClientRect(); return Math.round((r.left + r.right) / 2); };
    const row = d => {
      const x = cx(cur);
      for (let i = cur + d; i >= 0 && i < slides.length; i += d) if (Math.abs(cx(i) - x) < 2) return i;
      return cur;
    };
    slides.forEach((s, i) => {
      s.tabIndex = 0;
      s.setAttribute('role', 'button');
      s.setAttribute('aria-label', 'Slide ' + (i + 1));
      const choose = () => { pick(i); if (close && framed) closeOverview(); };
      s.addEventListener('click', choose);
      s.addEventListener('keydown', e => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); choose(); } });
    });
    document.addEventListener('keydown', e => {
      if (e.altKey || e.ctrlKey || e.metaKey) return;
      switch (e.key) {
        case 'ArrowLeft': pick(cur - 1); break;
        case 'ArrowRight': pick(cur + 1); break;
        case 'ArrowUp': pick(row(-1)); break;
        case 'ArrowDown': pick(row(1)); break;
        case 'Home': pick(0); break;
        case 'End': pick(slides.length - 1); break;
        case 'o': case 'Escape': if (framed) closeOverview(); else return; break;
        default: return;
      }
      e.preventDefault();
    });
    after.push(() => slides[cur].scrollIntoView({ behavior: 'smooth', block: 'nearest' }));
    if (framed) {
      const head = document.createElement('header'), x = document.createElement('button');
      head.className = 'slides-ov-head';
      x.type = 'button';
      x.textContent = x.title = 'Close overview';
      x.addEventListener('click', closeOverview);
      head.append(x);
      document.body.prepend(head);
    }
  } else {
    document.addEventListener('keydown', e => {
      if (e.altKey || e.ctrlKey || e.metaKey) return;
      switch (e.key) {
        case 'ArrowRight': case 'ArrowDown': case 'PageDown': case ' ': case 'Enter': next(); break;
        case 'ArrowLeft': case 'ArrowUp': case 'PageUp': case 'Backspace': prev(); break;
        case 'Home': jump(0); break;
        case 'End': jump(slides.length - 1, frags(slides[slides.length - 1]).length); break;
        case 'f': fullscreen(); break;
        case 'p': if (!VIEW && sk) openPresenter(); else return; break;
        case 'o': case 'Escape': if (OPT.presenter && sk) toggleOverview(); else return; break;
        default: return;
      }
      e.preventDefault();
    });
    document.addEventListener('click', e => {
      if (presenter || e.target.closest('a, .slides-osc')) return;
      (e.clientX < innerWidth / 3 ? prev : next)();
    });
    addEventListener('hashchange', () => jump(parseInt(location.hash.slice(1), 10) - 1 || 0));
  }

  if (OPT.osc && !VIEW) {
    // The on-screen controller: previous, the page, next, full screen and the
    // presenter view, shown while the pointer or a key is active and hidden
    // after 2.5 s.
    osc = document.createElement('div');
    osc.className = 'slides-osc';
    osc.setAttribute('role', 'toolbar');
    osc.setAttribute('aria-label', 'Slide controls');
    const button = (text, label, fn) => {
      const b = document.createElement('button');
      b.type = 'button';
      b.textContent = text;
      b.setAttribute('aria-label', label);
      b.addEventListener('click', e => { e.stopPropagation(); fn(); });
      return b;
    };
    const page = document.createElement('span');
    page.className = 'slides-osc-page';
    osc.append(button('‹', 'Previous slide', prev), page, button('›', 'Next slide', next), button('⛶', 'Full screen', fullscreen));
    if (OPT.presenter) {
      const p = button('▣', 'Presenter view', openPresenter), v = button('▦', 'Overview view', toggleOverview);
      if (!sk) for (const b of [p, v]) { b.disabled = true; b.title = b.getAttribute('aria-label') + ' is disabled due to restricted localStorage.'; }
      osc.append(p, v);
    }
    osc.update = i => { page.textContent = (i + 1) + ' / ' + slides.length; };
    document.body.appendChild(osc);
    let timer;
    const wake = () => {
      osc.classList.add('active');
      clearTimeout(timer);
      timer = setTimeout(() => osc.classList.remove('active'), 2500);
    };
    for (const ev of ['mousemove', 'touchstart', 'keydown']) document.addEventListener(ev, wake, { passive: true });
  }

  if (presenter) {
    // The presenter view: this slide, the step after it (the same page in an
    // iframe, told by message), the notes, the page, the time and a timer.
    const t = document.title;
    document.title = '[Presenter view]' + (t ? ' - ' + t : '');
    const el = (tag, cls, text, title) => {
      const e = document.createElement(tag);
      if (cls) e.className = cls;
      if (text) e.textContent = text;
      if (title) { e.title = title; if (tag === 'button') e.tabIndex = -1; }
      return e;
    };
    const box = el('div', 'slides-pv');
    const nextBox = el('div', 'slides-pv-next'), frame = el('iframe');
    frame.src = '?view=next';
    frame.title = 'Next slide';
    nextBox.append(frame);
    nextBox.addEventListener('click', next);
    const drag = el('div', 'slides-pv-drag');
    const notes = el('div', 'slides-pv-notes'), wrap = el('div', 'slides-pv-wrap');
    const smaller = el('button', '', 'Smaller notes font size', 'Smaller notes font size');
    const bigger = el('button', '', 'Bigger notes font size', 'Bigger notes font size');
    notes.append(wrap, el('div', 'slides-pv-buttons'));
    notes.lastChild.append(smaller, bigger);
    const info = el('div', 'slides-pv-info');
    const pp = el('button', '', 'Previous', 'Previous'), pn = el('button', '', 'Next', 'Next');
    const page = el('button', 'slides-pv-page', '', 'Overview view'), clock = el('time', 'slides-pv-time', '', 'Current time'), timerEl = el('time', 'slides-pv-timer', '', 'Timer');
    info.append(pp, page, pn, clock, timerEl);
    const main = document.querySelector('div.marpit');
    const right = el('div', 'slides-pv-right');
    right.append(nextBox, notes);
    box.append(main, drag, right, info);
    document.body.appendChild(box);
    let dragging = false;
    drag.addEventListener('mousedown', () => { dragging = true; drag.classList.add('active'); });
    addEventListener('mouseup', () => { dragging = false; drag.classList.remove('active'); });
    addEventListener('mousemove', e => {
      if (dragging) box.style.setProperty('--slides-split', Math.max(0, Math.min(100, e.clientX / document.documentElement.clientWidth * 100)) + '%');
    });
    const post = () => {
      if (frame.contentWindow) frame.contentWindow.postMessage('navigate:' + cur + ',' + step, window.origin === 'null' ? '*' : window.origin);
    };
    frame.addEventListener('load', post);
    const noteEls = [...document.querySelectorAll('.slides-note')];
    noteEls.forEach(n => { n.addEventListener('keydown', e => e.stopPropagation()); wrap.appendChild(n); });
    let scale = 0;
    const resize = d => { scale = Math.max(-5, scale + d); notes.style.setProperty('--slides-note-scale', (1.2 ** scale).toFixed(4)); };
    bigger.addEventListener('click', () => { bigger.blur(); resize(1); });
    smaller.addEventListener('click', () => { smaller.blur(); resize(-1); });
    document.addEventListener('keydown', e => { if (e.key === '+') resize(1); if (e.key === '-') resize(-1); }, true);
    page.addEventListener('click', () => { page.blur(); toggleOverview(); });
    pp.addEventListener('click', () => { pp.blur(); prev(); });
    pn.addEventListener('click', () => { pn.blur(); next(); });
    let t0 = new Date();
    const tick = () => {
      const n = new Date(), d = n - t0, p2 = x => String(Math.floor(x)).padStart(2, '0');
      clock.textContent = n.toLocaleTimeString();
      timerEl.textContent = p2(d / 36e5 % 24) + ':' + p2(d / 6e4 % 60) + ':' + p2(d / 1e3 % 60);
    };
    timerEl.addEventListener('click', () => { t0 = new Date(); tick(); });
    tick();
    setInterval(tick, 250);
    after.push(() => {
      page.textContent = (cur + 1) + ' / ' + slides.length;
      noteEls.forEach(n => n.classList.toggle('active', +n.dataset.index === cur));
      pp.disabled = cur === 0 && step === 0;
      pn.disabled = cur === slides.length - 1 && step === frags(slides[cur]).length;
      post();
    });
  }

  addEventListener('beforeprint', () => slides.forEach(s => s.classList.add('active')));
  addEventListener('afterprint', () => show(cur, step));
  show(start, startStep);
})();
`

// presenterCSS lays the presenter view out and keeps the notes out of the
// page: they are in it for the presenter view to take, and hidden until it
// does, so that they are not in the audience's accessible text (marp-cli's
// bespoke template hides them the same way).
const presenterCSS = `
.slides-note { display: none; }
.slides-overview { position: fixed; inset: 0; background: rgba(0,0,0,.65); opacity: 0; pointer-events: none; transition: opacity .1s; z-index: 20; }
.slides-overview[data-open="1"] { opacity: 1; pointer-events: auto; }
.slides-overview iframe { position: absolute; inset: 0; width: 100%; height: 100%; border: 0; background: #222; }
html[data-slides-view=overview], body[data-slides-view=overview] { height: auto; min-height: 100%; overflow: auto; background: #222; }
body[data-slides-view=overview] > div.marpit { display: grid; grid-template-columns: repeat(auto-fill, minmax(260px, 1fr)); gap: 16px; padding: 56px 16px 16px; }
html[data-slides-view=overview] div.marpit > svg[data-marpit-svg] { display: block; position: static; width: 100%; height: auto; background: #fff; cursor: pointer; scroll-margin-top: 56px; }
html[data-slides-view=overview] div.marpit > svg[data-marpit-svg]:focus-visible, html[data-slides-view=overview] div.marpit > svg[data-marpit-svg]:hover { outline: 3px solid #8ab4f8; outline-offset: 3px; }
html[data-slides-view=overview] div.marpit > svg[data-marpit-svg].active { outline: 4px solid #1a73e8; outline-offset: 3px; }
html[data-slides-view=overview] [data-marpit-fragment].hidden-fragment { opacity: 1; }
.slides-ov-head { position: fixed; top: 0; left: 0; right: 0; height: 40px; z-index: 5; display: flex; justify-content: flex-end; align-items: center; padding: 0 12px; background: rgba(0,0,0,.8); }
.slides-ov-head button { border: 0; border-radius: 4px; background: #444; color: #eee; cursor: pointer; padding: 4px 12px; font: 14px system-ui, sans-serif; }
@media print { .slides-overview { display: none; } }
body[data-slides-view=next] { background: #000; }
body[data-slides-view=presenter] { background: #222; color: #eee; overflow: hidden; font: 15px/1.4 system-ui, -apple-system, sans-serif; }
.slides-pv { --slides-split: 60%; display: grid; grid-template-columns: var(--slides-split) 6px 1fr; grid-template-rows: 1fr 2.4em; height: 100vh; }
.slides-pv > div.marpit { grid-column: 1; grid-row: 1; position: relative; min-width: 0; min-height: 0; background: #000; }
.slides-pv div.marpit > svg[data-marpit-svg] { position: absolute; inset: 0; width: 100%; height: 100%; }
.slides-pv-drag { grid-column: 2; grid-row: 1; cursor: col-resize; background: #444; }
.slides-pv-drag.active, .slides-pv-drag:hover { background: #888; }
.slides-pv-right { grid-column: 3; grid-row: 1; display: flex; flex-direction: column; min-width: 0; min-height: 0; }
.slides-pv-next { flex: none; width: 100%; aspect-ratio: 16 / 9; background: #000; cursor: pointer; position: relative; }
.slides-pv-next iframe { position: absolute; inset: 0; width: 100%; height: 100%; border: 0; pointer-events: none; }
.slides-pv-notes { flex: 1; min-height: 0; display: flex; flex-direction: column; }
.slides-pv-wrap { flex: 1; overflow: auto; padding: 8px 12px; font-size: calc(1em * var(--slides-note-scale, 1)); }
.slides-pv .slides-note.active { display: block; }
.slides-pv-buttons { display: flex; justify-content: flex-end; gap: 4px; padding: 2px 6px; }
.slides-pv-buttons button { font-size: 0; width: 28px; height: 22px; border: 0; border-radius: 4px; background: #444; color: #eee; cursor: pointer; }
.slides-pv-buttons button::before { font-size: 14px; content: "A-"; }
.slides-pv-buttons button + button::before { content: "A+"; }
.slides-pv-info { grid-column: 1 / -1; grid-row: 2; display: flex; align-items: center; gap: 12px; padding: 0 10px; background: #111; font-variant-numeric: tabular-nums; }
.slides-pv-info button { border: 0; border-radius: 4px; background: #444; color: #eee; cursor: pointer; padding: 3px 10px; }
.slides-pv-info button:disabled { opacity: .4; cursor: default; }
.slides-pv-time { margin-left: auto; }
.slides-pv-timer { cursor: pointer; font-weight: bold; }
`
