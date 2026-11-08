package viewer

import "strings"

// UsesCast reports whether a slide carries a recording's frames (see
// slides.castRenderer), so that its page needs the player.
func UsesCast(slideHTML string) bool {
	return strings.Contains(slideHTML, `class="slides-cast-data"`)
}

// CastJS is the player of the terminal recordings. It reads the frames each
// recording carries (a <script type="application/json" class="slides-cast-data">
// next to its screen), so that it fetches nothing, evaluates nothing it was
// not given as data and needs no third-party code. Each row of the screen is
// built from text nodes and CSS properties set through the CSSOM, never from
// markup; the controls are buttons and one range input. It starts paused when
// the user asks for reduced motion, and otherwise plays once when the slide is
// first shown (a deck page, or a preview frame). Clicks and keys on the
// controls and the screen do not reach the slides' own handlers.
const CastJS = `(function () {
  var reduce = window.matchMedia && matchMedia('(prefers-reduced-motion: reduce)').matches;
  var SPEEDS = [1, 1.5, 2, 4, 0.5];
  function fmt(s) { s = Math.max(0, Math.round(s)); return Math.floor(s / 60) + ':' + ('0' + (s % 60)).slice(-2); }
  function stop(e) { e.stopPropagation(); }
  function setup(box) {
    var dn = box.querySelector('script.slides-cast-data'), scr = box.querySelector('.slides-cast-screen');
    if (!dn || !scr || box.__cast) return;
    var d;
    try { d = JSON.parse(dn.textContent); } catch (e) { return; }
    box.__cast = true;
    var F = d.f, S = d.s, D = d.d || 0, W = d.w, H = d.h;
    var rows = [], els = [], idx = 0, time = 0, playing = false, speed = 0, raf = 0, t0 = 0, base = 0, started = false;
    var poster = parseFloat(box.getAttribute('data-poster'));
    if (!(poster >= 0)) poster = D;
    time = Math.min(poster, D);

    var ctl = document.createElement('div'), play = document.createElement('button'), bar = document.createElement('input'),
        clock = document.createElement('span'), spd = document.createElement('button');
    ctl.className = 'slides-cast-ctl';
    ctl.setAttribute('role', 'group');
    ctl.setAttribute('aria-label', 'Recording controls');
    play.type = spd.type = 'button';
    bar.type = 'range'; bar.min = 0; bar.max = 1000; bar.value = D ? Math.round(time / D * 1000) : 0;
    bar.setAttribute('aria-label', 'Position');
    clock.className = 'slides-cast-clock';
    ctl.append(play, bar, clock, spd);
    box.appendChild(ctl);
    ['click', 'keydown', 'keyup', 'pointerdown', 'mousedown', 'touchstart'].forEach(function (ev) { ctl.addEventListener(ev, stop); });
    scr.addEventListener('click', function (e) { e.stopPropagation(); toggle(); });

    function ui() {
      play.textContent = playing ? '⏸' : '▶';
      play.setAttribute('aria-label', playing ? 'Pause' : 'Play');
      spd.textContent = SPEEDS[speed] + '×';
      spd.setAttribute('aria-label', 'Speed ' + SPEEDS[speed] + ' times');
      clock.textContent = fmt(time) + ' / ' + fmt(D);
      bar.value = D ? Math.round(time / D * 1000) : 0;
    }
    function blank() { rows = []; for (var y = 0; y < H; y++) rows.push([]); }
    function apply(f, dirty) {
      for (var i = 1; i < f.length; i += 2) {
        if (f[i] === -1) {
          W = f[i + 1][0]; H = f[i + 1][1];
          while (rows.length < H) rows.push([]);
          rows.length = H;
          for (var y = 0; y < H; y++) dirty[y] = true;
        } else { rows[f[i]] = f[i + 1]; dirty[f[i]] = true; }
      }
    }
    function draw(dirty) {
      while (els.length < H) {
        var r = document.createElement('span');
        r.style.display = 'block'; r.style.height = '1.2em'; r.style.whiteSpace = 'pre';
        scr.appendChild(r); els.push(r);
      }
      while (els.length > H) scr.removeChild(els.pop());
      for (var y in dirty) {
        var el = els[y]; if (!el) continue;
        el.textContent = '';
        var runs = rows[y] || [];
        for (var j = 0; j < runs.length; j++) {
          var st = S[runs[j][1]], sp = document.createElement('span');
          sp.textContent = runs[j][0];
          sp.style.color = st[0];
          if (st[1]) sp.style.backgroundColor = st[1];
          if (st[2] & 1) sp.style.fontWeight = 'bold';
          if (st[2] & 2) sp.style.fontStyle = 'italic';
          var dec = (st[2] & 4 ? 'underline ' : '') + (st[2] & 8 ? 'line-through' : '');
          if (dec) sp.style.textDecoration = dec;
          el.appendChild(sp);
        }
      }
    }
    function seek(t) {
      if (!started) { started = true; scr.textContent = ''; els = []; }
      W = d.w; H = d.h; blank(); idx = 0;
      var dirty = {};
      for (var y = 0; y < H; y++) dirty[y] = true;
      while (idx < F.length && F[idx][0] <= t) apply(F[idx++], dirty);
      draw(dirty);
      time = t; base = t; t0 = performance.now();
      ui();
    }
    function tick(now) {
      if (!playing) return;
      time = Math.min(base + (now - t0) / 1000 * SPEEDS[speed], D);
      var dirty = {}, any = false;
      while (idx < F.length && F[idx][0] <= time) { apply(F[idx++], dirty); any = true; }
      if (any) draw(dirty);
      if (time >= D) { playing = false; }
      ui();
      if (playing) raf = requestAnimationFrame(tick);
    }
    function run() {
      if (playing) return;
      if (!started || time >= D) seek(0);
      playing = true; base = time; t0 = performance.now();
      ui();
      raf = requestAnimationFrame(tick);
    }
    function pause() { playing = false; cancelAnimationFrame(raf); ui(); }
    function toggle() { if (playing) pause(); else run(); }
    play.addEventListener('click', toggle);
    spd.addEventListener('click', function () {
      speed = (speed + 1) % SPEEDS.length;
      if (playing) { base = time; t0 = performance.now(); }
      ui();
    });
    bar.addEventListener('input', function () {
      var t = D * bar.value / 1000, was = playing;
      if (was) { playing = false; cancelAnimationFrame(raf); }
      seek(t);
      if (was) { playing = true; raf = requestAnimationFrame(tick); }
    });
    ui();
    // the first time the recording is on screen it plays, unless the user
    // prefers less motion: then it waits, showing its poster, for a click
    if (!reduce && 'IntersectionObserver' in window) {
      var seen = false;
      new IntersectionObserver(function (es) {
        es.forEach(function (e) {
          if (e.isIntersecting && !seen) { seen = true; run(); }
          else if (!e.isIntersecting && playing) pause();
        });
      }, { threshold: 0.6 }).observe(box);
    }
  }
  var boxes = document.querySelectorAll('.slides-cast');
  for (var i = 0; i < boxes.length; i++) setup(boxes[i]);
})();`

// CastCSS styles the player's controls. The recording's own colours and sizes
// are on its elements, so that the exports have them too.
const CastCSS = `.slides-cast-ctl{display:flex;align-items:center;gap:.6em;margin-top:.5em;padding:.2em .4em;border-radius:6px;background:rgba(128,128,128,.25);font:22px/1.2 system-ui,-apple-system,sans-serif;color:inherit}
.slides-cast-ctl button{appearance:none;background:none;border:0;color:inherit;font:inherit;min-width:2em;height:1.8em;cursor:pointer;border-radius:4px}
.slides-cast-ctl button:hover,.slides-cast-ctl button:focus-visible{background:rgba(255,255,255,.22)}
.slides-cast-ctl input{flex:1;min-width:0;accent-color:currentColor}
.slides-cast-clock{font-variant-numeric:tabular-nums;white-space:nowrap}
.slides-cast-error{color:#b00020;font-style:italic}
@media print{.slides-cast-ctl{display:none}}`
