// tattva workspace: theme picker, pane divider and diagrams. No framework.
(function () {
  var root = document.documentElement;
  var save = function (key, value) {
    try { localStorage.setItem(key, value); } catch (e) { /* storage unavailable */ }
  };

  // Theme and mode.
  var theme = document.getElementById("theme");
  var mode = document.getElementById("mode");
  var showMode = function () { mode.textContent = "[" + root.dataset.mode + "]"; };
  theme.value = root.dataset.theme;
  showMode();
  theme.addEventListener("change", function () {
    root.dataset.theme = theme.value;
    save("tattva-theme", theme.value);
    drawDiagrams();
  });
  mode.addEventListener("click", function () {
    root.dataset.mode = root.dataset.mode === "dark" ? "light" : "dark";
    save("tattva-mode", root.dataset.mode);
    showMode();
    drawDiagrams();
  });

  // Divider: drag it, or use the arrow keys, to size the steps pane between
  // 200px and half the window.
  var divider = document.getElementById("divider");
  var steps = document.querySelector(".steps");
  if (divider && steps) {
    var setWidth = function (px) {
      var clamped = Math.round(Math.max(200, Math.min(px, window.innerWidth / 2)));
      root.style.setProperty("--left-width", clamped + "px");
      save("tattva-left-width", clamped + "px");
      divider.setAttribute("aria-valuenow", clamped);
      divider.setAttribute("aria-valuemax", Math.round(window.innerWidth / 2));
    };
    divider.addEventListener("pointerdown", function (down) {
      down.preventDefault();
      divider.setPointerCapture(down.pointerId);
      var left = steps.getBoundingClientRect().left;
      var move = function (e) { setWidth(e.clientX - left); };
      var up = function () {
        divider.removeEventListener("pointermove", move);
        divider.removeEventListener("pointerup", up);
      };
      divider.addEventListener("pointermove", move);
      divider.addEventListener("pointerup", up);
    });
    divider.addEventListener("keydown", function (e) {
      if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
      e.preventDefault();
      setWidth(steps.getBoundingClientRect().width + (e.key === "ArrowRight" ? 20 : -20));
    });
    setWidth(steps.getBoundingClientRect().width);
  }

  // Keep the selected step in view.
  var selected = document.querySelector(".steps .selected");
  if (selected) selected.scrollIntoView({ block: "nearest" });

  // Architecture diagrams, coloured from the current theme's variables.
  function drawDiagrams() {
    var blocks = document.querySelectorAll("pre.mermaid");
    if (!blocks.length || !window.mermaid) return;
    var css = getComputedStyle(root);
    var v = function (name) { return css.getPropertyValue(name).trim(); };
    window.mermaid.initialize({
      startOnLoad: false,
      securityLevel: "strict",
      theme: "base",
      fontFamily: v("--font"),
      themeVariables: {
        background: v("--surface"),
        primaryColor: v("--surface-2"),
        primaryTextColor: v("--text"),
        primaryBorderColor: v("--accent"),
        secondaryColor: v("--surface-2"),
        tertiaryColor: v("--surface"),
        lineColor: v("--subtext"),
        textColor: v("--text")
      }
    });
    blocks.forEach(function (el) {
      el.removeAttribute("data-processed");
      el.textContent = el.dataset.source;
    });
    window.mermaid.run({ nodes: Array.prototype.slice.call(blocks) }).catch(function () {
      // mermaid draws its own error message in place of a broken diagram
    });
  }
  drawDiagrams();
})();
