package viewer

import "strings"

// UsesVideo reports whether a slide carries a <video> of a deck's own (see
// slides.videoRenderer), so that its page needs the script that keeps it
// polite.
func UsesVideo(slideHTML string) bool {
	return strings.Contains(slideHTML, `class="slides-video"`)
}

// VideoJS keeps the videos of a deck polite. Under prefers-reduced-motion no
// video starts by itself or loops: autoplay and loop are taken off and the
// video is paused. Otherwise a video that asks to autoplay (it is muted: the
// markup never writes autoplay without it) plays while at least half of it is
// on screen and pauses when it is not, so that the video of a slide that is not
// shown does not run. Clicks and keys on a video's controls do not reach the
// slides' own handlers. It fetches nothing.
const VideoJS = `(function () {
  var reduce = window.matchMedia && matchMedia('(prefers-reduced-motion: reduce)').matches;
  var io = null;
  if (!reduce && window.IntersectionObserver) {
    io = new IntersectionObserver(function (es) {
      es.forEach(function (e) {
        var v = e.target;
        if (e.isIntersecting) { var p = v.play(); if (p && p.catch) p.catch(function () {}); } else { v.pause(); }
      });
    }, { threshold: 0.5 });
  }
  function setup(v) {
    if (v.__video) return;
    v.__video = true;
    ['click', 'keydown', 'keyup', 'pointerdown', 'mousedown', 'touchstart'].forEach(function (ev) {
      v.addEventListener(ev, function (e) { e.stopPropagation(); });
    });
    if (reduce) {
      v.removeAttribute('autoplay'); v.removeAttribute('loop'); v.autoplay = false; v.loop = false; v.pause();
    } else if (v.hasAttribute('autoplay') && io) {
      v.autoplay = false;
      io.observe(v);
    }
  }
  window.slidesVideoInit = function () {
    var vs = document.querySelectorAll('video.slides-video');
    for (var i = 0; i < vs.length; i++) setup(vs[i]);
  };
  window.slidesVideoInit();
})();`
