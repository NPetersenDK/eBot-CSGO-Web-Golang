// Closes the user menu (a <details> element) on an outside press or Escape.
document.addEventListener("pointerdown", (e) => {
  document.querySelectorAll("details.dropdown[open]").forEach((d) => {
    if (!d.contains(e.target)) d.removeAttribute("open");
  });
});
document.addEventListener("keydown", (e) => {
  if (e.key !== "Escape") return;
  document.querySelectorAll("details.dropdown[open]").forEach((d) => {
    d.removeAttribute("open");
    d.querySelector("summary")?.focus();
  });
});

// A clickable table row opens its page through the row's own link, so the link stays
// the single source of the address. Clicks on links, buttons and form fields are left alone.
document.addEventListener("click", (e) => {
  if (e.target.closest("a, button, input, select, textarea, label, summary")) return;
  const row = e.target.closest("tr.clickable");
  const link = row && row.querySelector("a[href]");
  if (link) window.location.href = link.href;
});
