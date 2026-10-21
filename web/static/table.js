// Sorting, search and paging for tables inside [data-table]. Without this
// script the full table is shown, so nothing depends on it.
//
// Markup inside the wrapper: table[data-sortable], input[data-search],
// select[data-page-size] (0 means all), [data-info] and [data-pager].
// A select[data-submit-on-change] submits its form when it changes.
// Buttons with data-check="all" or "none" tick or clear the checkboxes of their form.
(() => {
  for (const sel of document.querySelectorAll("select[data-submit-on-change]")) {
    sel.addEventListener("change", () => sel.form && sel.form.submit());
  }

  for (const btn of document.querySelectorAll("button[data-check]")) {
    btn.addEventListener("click", () => {
      const on = btn.getAttribute("data-check") === "all";
      for (const box of btn.closest("form").querySelectorAll('input[type="checkbox"]')) box.checked = on;
    });
  }

  const number = (text) => {
    const t = text.trim();
    if (!/^-?\d+([.,]\d+)?%?$/.test(t)) return null;
    return parseFloat(t.replace("%", "").replace(",", "."));
  };

  for (const root of document.querySelectorAll("[data-table]")) {
    const table = root.querySelector("table[data-sortable]");
    if (!table || !table.tBodies[0]) continue;
    const body = table.tBodies[0];
    const rows = Array.from(body.rows).filter((r) => r.cells.length > 1);
    if (rows.length === 0) continue;

    const search = root.querySelector("[data-search]");
    const sizeSel = root.querySelector("[data-page-size]");
    const info = root.querySelector("[data-info]");
    const pager = root.querySelector("[data-pager]");
    const heads = Array.from(table.tHead.rows[0].cells);

    let order = rows.slice();
    let sortCol = 1;
    let sortDir = -1;
    let page = 0;

    const matching = () => {
      const q = search ? search.value.trim().toLowerCase() : "";
      return q ? order.filter((r) => r.textContent.toLowerCase().includes(q)) : order;
    };

    const render = () => {
      const list = matching();
      const size = sizeSel ? parseInt(sizeSel.value, 10) : 0;
      const pages = size > 0 ? Math.max(1, Math.ceil(list.length / size)) : 1;
      page = Math.min(page, pages - 1);
      const from = size > 0 ? page * size : 0;
      const to = size > 0 ? Math.min(list.length, from + size) : list.length;

      for (const r of rows) r.hidden = true;
      const shown = list.slice(from, to);
      for (const r of shown) {
        r.hidden = false;
        body.appendChild(r);
      }

      if (info) {
        info.textContent = list.length === 0
          ? "No matching players"
          : `Showing ${from + 1} to ${to} of ${list.length} players`;
      }
      if (pager) {
        pager.textContent = "";
        const add = (label, target, disabled, current) => {
          const b = document.createElement("button");
          b.type = "button";
          b.className = "btn sm" + (current ? " primary" : "");
          b.textContent = label;
          b.disabled = disabled;
          b.addEventListener("click", () => { page = target; render(); });
          pager.appendChild(b);
        };
        if (pages > 1) {
          add("Previous", page - 1, page === 0, false);
          const first = Math.max(0, Math.min(page - 2, pages - 5));
          for (let i = first; i < Math.min(pages, first + 5); i++) add(String(i + 1), i, false, i === page);
          add("Next", page + 1, page === pages - 1, false);
        }
      }
    };

    const sortBy = (col, dir) => {
      sortCol = col;
      sortDir = dir;
      const numeric = rows.every((r) => number(r.cells[col].textContent) !== null);
      order = rows.slice().sort((a, b) => {
        const x = a.cells[col].textContent.trim();
        const y = b.cells[col].textContent.trim();
        const c = numeric ? number(x) - number(y) : x.localeCompare(y, undefined, { sensitivity: "base" });
        return c * dir;
      });
      heads.forEach((h, i) => h.setAttribute("aria-sort", i === col ? (dir > 0 ? "ascending" : "descending") : "none"));
      page = 0;
      render();
    };

    heads.forEach((h, i) => {
      h.tabIndex = 0;
      const toggle = () => sortBy(i, i === sortCol ? -sortDir : (i === 0 ? 1 : -1));
      h.addEventListener("click", toggle);
      h.addEventListener("keydown", (e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); toggle(); } });
    });
    if (search) search.addEventListener("input", () => { page = 0; render(); });
    if (sizeSel) sizeSel.addEventListener("change", () => { page = 0; render(); });

    sortBy(1, -1);
  }
})();
