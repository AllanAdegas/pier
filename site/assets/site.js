// Copy buttons, theme toggle and the documentation menu on small screens.
(function () {
  function copyText(button) {
    var code = button.parentElement.querySelector("code");
    if (!code || !navigator.clipboard) return;
    navigator.clipboard.writeText(code.innerText.replace(/\n$/, "")).then(function () {
      button.textContent = "Copiado";
      button.classList.add("copied");
      setTimeout(function () {
        button.textContent = "Copiar";
        button.classList.remove("copied");
      }, 1600);
    });
  }

  document.querySelectorAll(".copy").forEach(function (button) {
    button.addEventListener("click", function () { copyText(button); });
  });

  var root = document.documentElement;
  var themeButton = document.querySelector(".theme-toggle");
  if (themeButton) {
    themeButton.addEventListener("click", function () {
      var current = root.dataset.theme ||
        (matchMedia("(prefers-color-scheme: light)").matches ? "light" : "dark");
      var next = current === "light" ? "dark" : "light";
      root.dataset.theme = next;
      try { localStorage.setItem("pier-theme", next); } catch (_) { /* storage blocked */ }
    });
  }

  var nav = document.getElementById("docs-nav");
  var menuButton = document.querySelector(".menu-toggle");
  if (nav) document.body.classList.add("has-docs");
  if (nav && menuButton) {
    menuButton.addEventListener("click", function () {
      var open = nav.classList.toggle("open");
      menuButton.setAttribute("aria-expanded", String(open));
      menuButton.setAttribute("aria-label", open ? "Fechar o menu" : "Abrir o menu");
    });
  }
})();
