// Copy buttons, theme toggle and the documentation menu on small screens.
// Every visible label comes from the page (data-* attributes written from
// src/strings.ts), so this file has no text of its own in either language.
(function () {
  function copyText(button) {
    var code = button.parentElement.querySelector("code");
    if (!code || !navigator.clipboard || button.classList.contains("copied")) return;
    var label = button.textContent;
    navigator.clipboard.writeText(code.innerText.replace(/\n$/, "")).then(function () {
      button.textContent = button.dataset.copiedLabel || label;
      button.classList.add("copied");
      setTimeout(function () {
        button.textContent = label;
        button.classList.remove("copied");
      }, 1600);
    });
  }

  document.querySelectorAll(".copy").forEach(function (button) {
    button.addEventListener("click", function () {
      copyText(button);
    });
  });

  var root = document.documentElement;
  var themeButton = document.querySelector(".theme-toggle");
  if (themeButton) {
    themeButton.addEventListener("click", function () {
      var current = root.dataset.theme ||
        (matchMedia("(prefers-color-scheme: light)").matches ? "light" : "dark");
      var next = current === "light" ? "dark" : "light";
      root.dataset.theme = next;
      try {
        localStorage.setItem("pier-theme", next);
      } catch (_) { /* storage blocked */ }
    });
  }

  var nav = document.getElementById("docs-nav");
  var menuButton = document.querySelector(".menu-toggle");
  if (nav) document.body.classList.add("has-docs");
  if (nav && menuButton) {
    menuButton.addEventListener("click", function () {
      var open = nav.classList.toggle("open");
      menuButton.setAttribute("aria-expanded", String(open));
      var label = open ? menuButton.dataset.closeLabel : menuButton.dataset.openLabel;
      if (label) menuButton.setAttribute("aria-label", label);
    });
  }
})();
