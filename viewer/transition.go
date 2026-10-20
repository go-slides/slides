package viewer

// The View Transition API plays the transitions of a deck (the transition
// directive, see deck.TransitionCSS) around the change of slide, as marp-cli's
// bespoke template does, and on the same contract: a transition is a name; its
// CSS keyframes are called marp-transition-NAME (one animation for both the
// slide that leaves and the one that arrives), marp-outgoing-transition-NAME
// and marp-incoming-transition-NAME, each with a -backward- variant for the way
// back; the keyframes of the built-in transitions are called
// marp-...-transition-__builtin__NAME and stand in when the deck's styles have
// none of the name. The page sets a few custom properties on the
// ::view-transition pseudo-elements and the styles below turn them into
// animations.
//
// transitionCSS and the logic of transitionJS follow marp-cli 4.5.1's
// bespoke.js and template (MIT License, Copyright (c) 2017 Marp team), read
// from its sources and rewritten: the keyframes are found by reading the
// style sheets instead of by playing them.

// transitionCSS is what the view transition pseudo-elements animate with: the
// name, duration and direction come from custom properties the script sets,
// and without a name a reduced fade, which is also what is used when the
// viewer asks for reduced motion. The controller and the progress bar do not
// move with the slide.
const transitionCSS = `
@keyframes __bespoke_marp_transition_reduced_outgoing__{0%{opacity:1}to{opacity:0}}
@keyframes __bespoke_marp_transition_reduced_incoming__{0%{mix-blend-mode:plus-lighter;opacity:0}to{mix-blend-mode:plus-lighter;opacity:1}}
@media screen{
::view-transition-group(*){animation-duration:var(--marp-bespoke-transition-animation-duration,.5s);animation-timing-function:ease}
::view-transition-new(*),::view-transition-old(*){animation-delay:0s;animation-direction:var(--marp-bespoke-transition-animation-direction,normal);animation-duration:var(--marp-bespoke-transition-animation-duration,.5s);animation-fill-mode:both;animation-name:var(--marp-bespoke-transition-animation-name,var(--marp-bespoke-transition-animation-name-fallback,__bespoke_marp_transition_no_animation__));mix-blend-mode:normal}
::view-transition-old(*){--marp-bespoke-transition-animation-name-fallback:__bespoke_marp_transition_reduced_outgoing__;animation-timing-function:ease}
::view-transition-new(*){--marp-bespoke-transition-animation-name-fallback:__bespoke_marp_transition_reduced_incoming__;animation-timing-function:ease}
::view-transition-new(root),::view-transition-old(root){animation-timing-function:linear}
.slides-osc{view-transition-name:__bespoke_marp_transition_osc__}
.slides-progress{view-transition-name:__bespoke_marp_transition_progress__}
::view-transition-new(__bespoke_marp_transition_osc__),::view-transition-old(__bespoke_marp_transition_osc__),::view-transition-new(__bespoke_marp_transition_progress__),::view-transition-old(__bespoke_marp_transition_progress__){animation-duration:0s!important;animation-name:__bespoke_marp_transition_osc__!important}
::view-transition-new(__bespoke_marp_transition_osc__),::view-transition-new(__bespoke_marp_transition_progress__){opacity:0!important}
}
`

// transitionJS defines window.__slidesTransit(slide, back, change), which the
// presenting script calls to change slides: it returns true when it took
// charge of the change, having arranged to call change() inside a view
// transition, and false for a slide with no transition for that direction or a
// browser without the API. Its pure parts are exported for the tests when
// there is a CommonJS module.
const transitionJS = `(function () {
  var NAME = '--marp-bespoke-transition-animation-name', DUR = '--marp-bespoke-transition-animation-duration',
    ADIR = '--marp-bespoke-transition-animation-direction', CDUR = '--marp-transition-duration', CDIR = '--marp-transition-direction',
    NOANIM = '__bespoke_marp_transition_no_animation__';
  var TYPES = [['both', ''], ['outgoing', 'outgoing-'], ['incoming', 'incoming-']], DIRS = [['forward', ''], ['backward', '-backward']];
  var REDUCED = { forward: { incoming: { name: '__bespoke_marp_transition_reduced_incoming__' }, outgoing: { name: '__bespoke_marp_transition_reduced_outgoing__' } } };
  REDUCED.backward = REDUCED.forward;

  // A transition as the directive wrote it into the section: JSON with a name
  // and maybe a duration.
  function parse(s) {
    try {
      var t = JSON.parse(s);
      if (t && typeof t === 'object' && typeof t.name === 'string' && (t.duration === undefined || typeof t.duration === 'string')) return t;
    } catch (e) {}
  }
  // names maps the names of the keyframes there are to their default duration
  // ('' for none); the result is what there is for a transition name, by
  // direction and type.
  function find(names, name) {
    var r = { forward: {}, backward: {} };
    TYPES.forEach(function (t) {
      DIRS.forEach(function (d) {
        var n = 'marp-' + t[1] + 'transition' + d[1] + '-' + name;
        r[d[0]][t[0]] = names.has(n) ? { name: n, duration: names.get(n) || undefined } : undefined;
      });
    });
    return r;
  }
  function none(r) {
    return ['forward', 'backward'].every(function (d) {
      return ['both', 'incoming', 'outgoing'].every(function (t) { return !r[d][t]; });
    });
  }
  // The animation of the slide that leaves (type outgoing) or arrives
  // (incoming): its own keyframes, else the shared ones (run backwards on
  // arrival), else, going back, those of the way forward.
  function pick(r, type, back) {
    var d = r[back ? 'backward' : 'forward'], e = d[type], o = {};
    if (e) { o[NAME] = e.name; return o; }
    if (d.both) {
      o[NAME] = d.both.name;
      if (type === 'incoming') o[ADIR] = 'reverse';
      return o;
    }
    if (back) return pick(r, type, false);
    o[NAME] = NOANIM;
    return o;
  }
  function duration(r, back) {
    var f = function (d) { var x = r[d]; return ((x.both || x.outgoing || x.incoming || {}).duration); };
    return back ? (f('backward') || f('forward')) : f('forward');
  }
  // The rules that make the view transition play it.
  function rules(r, back, dur) {
    var decl = function (o) { return Object.keys(o).map(function (k) { return k + ':' + o[k] + ';'; }).join(''); };
    var out = [':root{' + CDIR + ':' + (back ? -1 : 1) + ';}'];
    if (dur !== undefined) out.push('::view-transition-group(*){' + DUR + ':' + dur + ';}');
    out.push('::view-transition-old(root){' + decl(pick(r, 'outgoing', back)) + '}');
    out.push('::view-transition-new(root){' + decl(pick(r, 'incoming', back)) + '}');
    return out;
  }
  var lib = { parse: parse, find: find, none: none, pick: pick, duration: duration, rules: rules };
  if (typeof module !== 'undefined' && module.exports) module.exports = lib;
  if (typeof window === 'undefined') return;

  // The keyframes the page has, with the --marp-transition-duration their
  // first frame sets, if any.
  function scan() {
    var m = new Map();
    for (var i = 0; i < document.styleSheets.length; i++) {
      var rs;
      try { rs = document.styleSheets[i].cssRules; } catch (e) { continue; }
      for (var j = 0; j < rs.length; j++) {
        if (rs[j].type === 7) {
          var f = rs[j].cssRules[0];
          m.set(rs[j].name, f && f.style ? f.style.getPropertyValue(CDUR).trim() : '');
        }
      }
    }
    return m;
  }
  var reduced = window.matchMedia('(prefers-reduced-motion: reduce)'), running = null, styleEl = null;
  window.__slidesTransit = function (slide, back, change) {
    if (!document.startViewTransition) return false;
    if (running) {
      // a change while one is playing skips it
      try { running.skipTransition(); } catch (e) {}
      running = null;
      change();
      return true;
    }
    var attr = back ? 'data-transition-back' : 'data-transition';
    var sec = slide.querySelector('section[' + attr + ']');
    var spec = sec && parse(sec.getAttribute(attr));
    if (!spec) return false;
    var names = scan(), r = find(names, spec.name);
    if (none(r)) {
      if (spec.builtinFallback === false) return false;
      r = find(names, '__builtin__' + spec.name);
      if (none(r)) return false;
    }
    if (reduced.matches) r = REDUCED;
    if (styleEl) styleEl.remove();
    styleEl = document.createElement('style');
    document.head.appendChild(styleEl);
    rules(r, back, spec.duration || duration(r, back)).forEach(function (rule) { styleEl.sheet.insertRule(rule); });
    var done = function () { running = null; if (styleEl) { styleEl.remove(); styleEl = null; } };
    try {
      var vt = running = document.startViewTransition(change);
      // a transition the browser aborts rejects these; nobody is waiting
      vt.ready.catch(function () {});
      vt.updateCallbackDone.catch(function () {});
      vt.finished.then(done, done);
    } catch (e) {
      done();
      change();
    }
    return true;
  };
})();`
