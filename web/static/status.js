// Polls /admin/status and colours the status badge and its menu.
// Markup: [data-status] wraps [data-status-pill], [data-status-text] and, per
// service, [data-state="<name>"].
(() => {
  const root = document.querySelector("[data-status]");
  if (!root) return;

  // null is unknown (muted), true is up (ok), false is down (err).
  const kind = (up) => (up === null ? "muted" : up ? "ok" : "err");

  const set = (name, up) => {
    const state = root.querySelector(`[data-state="${name}"]`);
    if (!state) return;
    state.className = "badge " + kind(up);
    state.textContent = up === null ? "Unknown" : up ? "Online" : "Offline";
  };

  const render = (s) => {
    const pill = root.querySelector("[data-status-pill]");
    const text = root.querySelector("[data-status-text]");
    if (!s) {
      set("ebot", null);
      set("websocket", null);
      text.textContent = "Unknown";
      pill.className = "badge muted";
      return;
    }
    set("ebot", s.ebot);
    set("websocket", s.websocket);
    text.textContent = s.ok ? "All good" : "Unhealthy";
    pill.className = "badge " + kind(s.ok);
  };

  const poll = async () => {
    try {
      const res = await fetch("/admin/status", { credentials: "same-origin", cache: "no-store" });
      if (res.redirected || !(res.headers.get("Content-Type") || "").includes("application/json")) {
        location.href = "/login?next=" + encodeURIComponent(location.pathname + location.search);
        return;
      }
      render(res.ok ? await res.json() : null);
    } catch {
      render(null);
    }
  };

  poll();
  setInterval(() => { if (!document.hidden) poll(); }, 10000);
  document.addEventListener("visibilitychange", () => { if (!document.hidden) poll(); });
})();
