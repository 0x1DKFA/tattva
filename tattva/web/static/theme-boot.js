// Applies the saved theme, mode and pane width before the page draws, so it
// never flashes the wrong colours. Loaded from <head>; there are no inline scripts.
(function () {
  var root = document.documentElement;
  var known = (root.dataset.themes || "").split(" ");
  try {
    var theme = localStorage.getItem("tattva-theme");
    if (theme && known.indexOf(theme) >= 0) root.dataset.theme = theme;
    var mode = localStorage.getItem("tattva-mode");
    if (mode === "dark" || mode === "light") root.dataset.mode = mode;
    var width = localStorage.getItem("tattva-left-width");
    if (/^\d+px$/.test(width || "")) root.style.setProperty("--left-width", width);
  } catch (e) {
    // Storage can be unavailable (private windows, blocked site data): keep the defaults.
  }
})();
