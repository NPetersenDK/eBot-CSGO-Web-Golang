// Closes the user menu (a <details> element) on an outside click or Escape.
document.addEventListener("click", (e) => {
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
