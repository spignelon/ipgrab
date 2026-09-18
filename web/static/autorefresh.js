// Shared polling scheduler for the admin UI. Page-specific scripts
// (dashboard.js, events.js, links.js) register a callback via
// window.Netra.onAutoRefresh(fn); this module calls every registered
// callback on a timer, using the interval configured from Settings
// (window.AUTO_REFRESH_SECONDS, embedded by layout.html's "head" partial).
//
// Pauses while the tab is hidden (no point polling a background tab) and
// fires one immediate catch-up tick when the tab regains focus, so a page
// left open overnight doesn't show 8-hour-old data for a few extra seconds.
(function () {
  const listeners = [];
  let timer = null;

  function intervalSeconds() {
    const s = Number(window.AUTO_REFRESH_SECONDS);
    return s > 0 ? s : 30;
  }

  function tick() {
    if (document.hidden) return;
    listeners.forEach((fn) => {
      try {
        fn();
      } catch (e) {
        console.error("autorefresh listener failed", e);
      }
    });
  }

  function start() {
    if (timer) clearInterval(timer);
    timer = setInterval(tick, intervalSeconds() * 1000);
  }

  document.addEventListener("visibilitychange", () => {
    if (!document.hidden) tick();
  });

  window.Netra = window.Netra || {};
  window.Netra.onAutoRefresh = function (fn) {
    listeners.push(fn);
  };

  start();
})();
