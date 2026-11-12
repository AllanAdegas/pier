// Applies the theme chosen on this browser before the first paint.
try {
  var saved = localStorage.getItem("pier-theme");
  if (saved === "light" || saved === "dark") document.documentElement.dataset.theme = saved;
} catch (_) { /* storage blocked: follow the system */ }
