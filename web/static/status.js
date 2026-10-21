// Polls /admin/status and colours the status pill and its menu.
// Markup: [data-status] wraps [data-status-dot], [data-status-text] and, per
// service, [data-dot="<name>"] and [data-state="<name>"].
(() => {
  const root = document.querySelector("[data-status]");
  if (!root) return;

  const set = (name, up) => {
    const dot = root.querySelector(`[data-dot="${name}"]`);
    const state = root.querySelector(`[data-state="${name}"]`);
    if (dot) dot.className = "dot " + (up === null ? "" : up ? "ok" : "err");
    if (state) state.textContent = up === null ? "Unknown" : up ? "Online" : "Offline";
  };

  const render = (s) => {
    const pill = root.querySelector("summary");
    const dot = root.querySelector("[data-status-dot]");
    const text = root.querySelector("[data-status-text]");
    if (!s) {
      set("ebot", null);
      set("websocket", null);
      dot.className = "dot";
      text.textContent = "Unknown";
      pill.className = "status-pill";
      return;
    }
    set("ebot", s.ebot);
    set("websocket", s.websocket);
    dot.className = "dot " + (s.ok ? "ok" : "err");
    text.textContent = s.ok ? "All good" : "Unhealthy";
    pill.className = "status-pill " + (s.ok ? "ok" : "err");
  };

  const poll = async () => {
    try {
      const res = await fetch("/admin/status", { credentials: "same-origin", cache: "no-store" });
      render(res.ok ? await res.json() : null);
    } catch {
      render(null);
    }
  };

  poll();
  setInterval(() => { if (!document.hidden) poll(); }, 10000);
  document.addEventListener("visibilitychange", () => { if (!document.hidden) poll(); });
})();
