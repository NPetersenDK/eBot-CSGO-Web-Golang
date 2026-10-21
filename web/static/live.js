/*
 * Live updates and bot commands. Vanilla JS, no dependencies, textContent only.
 *
 * Data attributes (all ids are numeric match ids):
 *   [data-live="matches"] or [data-live-matches]
 *       Open EventSource("/live/matches") for the "match" scope.
 *   [data-live-reload]        On the same element: reload the page (at most once
 *                             per 5 seconds) when a button or finished message arrives.
 *   [data-match-status="<id>"]  Text is replaced on "status" messages.
 *   [data-match-id="<id>"]    Gets class "stale" when a "button" message arrives.
 *   [data-streamer-ready="<id>"]  Gets class "disabled" on "streamerReady".
 *   [data-console="<id>"]     RCON output. Streams /admin/live/<id>/rcon, one line per
 *                             message, last 500 lines kept.
 *   [data-logger="<id>"]      Game log. Streams /admin/live/<id>/logger, same treatment.
 *   [data-rcon-form="<id>"]   Form with input name="arg". Sends action=executeCommand
 *                             to /admin/matches/<id>/command.
 *   [data-alive]              Opens /admin/live/alive. Text is "waiting" until the bot
 *                             answers the alive check, then "online".
 *   [data-command="<action>"][data-match="<id>"]
 *                             Button that posts the action to /admin/matches/<id>/command.
 *                             Optional data-arg is sent as "arg". data-confirm="text"
 *                             asks confirm() first.
 *   [data-command-result]     Receives the result text of the last command.
 *   [data-live-state]         Shows "reconnecting" while an EventSource is in error.
 */
(() => {
  "use strict";

  const MAX_LINES = 500;
  const RELOAD_GAP_MS = 5000;

  const $$ = (sel, root = document) => Array.from(root.querySelectorAll(sel));
  const attr = (id) => CSS.escape(String(id));

  function setLiveState(text) {
    for (const el of $$("[data-live-state]")) el.textContent = text;
  }

  function open(url, handlers) {
    const es = new EventSource(url, { withCredentials: false });
    es.addEventListener("open", () => setLiveState(""));
    es.addEventListener("error", () => setLiveState("reconnecting"));
    for (const [name, fn] of Object.entries(handlers)) {
      es.addEventListener(name, (ev) => fn(ev.data));
    }
    return es;
  }

  function parse(raw) {
    try {
      return JSON.parse(raw);
    } catch {
      return null;
    }
  }

  let lastReload = 0;
  let reloadTimer = 0;
  function reloadSoon() {
    if (reloadTimer) return;
    const wait = Math.max(0, lastReload + RELOAD_GAP_MS - Date.now());
    reloadTimer = setTimeout(() => {
      lastReload = Date.now();
      location.reload();
    }, wait);
  }

  function initMatches() {
    const root = document.querySelector('[data-live="matches"], [data-live-matches]');
    if (!root) return;
    const reload = root.hasAttribute("data-live-reload");

    open("/live/matches", {
      match(raw) {
        const m = parse(raw);
        if (!m || m.id == null) return;
        const id = m.id;
        switch (m.message) {
          case "status":
            if (m.content === "Finished") {
              markStale(id, reload);
            } else if (typeof m.content === "string" && !["is_paused", "is_unpaused", "Starting"].includes(m.content)) {
              for (const el of $$(`[data-match-status="${attr(id)}"]`)) el.textContent = m.content;
            }
            break;
          case "button":
            markStale(id, reload);
            break;
          case "streamerReady":
            for (const el of $$(`[data-streamer-ready="${attr(id)}"]`)) el.classList.add("disabled");
            break;
        }
      },
    });
  }

  function markStale(id, reload) {
    for (const el of $$(`[data-match-id="${attr(id)}"]`)) el.classList.add("stale");
    if (reload) reloadSoon();
  }

  function appendLine(box, text) {
    const nearBottom = box.scrollHeight - box.scrollTop - box.clientHeight < 40;
    const line = document.createElement("div");
    line.textContent = text;
    box.appendChild(line);
    while (box.childElementCount > MAX_LINES) box.firstElementChild.remove();
    if (nearBottom) box.scrollTop = box.scrollHeight;
  }

  function initStream(selector, attrName, pathSuffix, event, format) {
    for (const box of $$(selector)) {
      const id = box.getAttribute(attrName);
      if (!/^\d+$/.test(id)) continue;
      open(`/admin/live/${id}/${pathSuffix}`, {
        [event](raw) {
          const m = parse(raw);
          for (const text of format(m)) appendLine(box, text);
        },
      });
    }
  }

  const rconLines = (m) =>
    m && m.content != null ? [`Answer: ${String(m.content)}`] : [];

  function loggerLines(m) {
    if (Array.isArray(m)) {
      return m.map((x) => (typeof x === "string" ? parse(x) : x)).filter((x) => x && x.content != null).map((x) => String(x.content));
    }
    return m && m.content != null ? [String(m.content)] : [];
  }

  function initAlive() {
    const els = $$("[data-alive]");
    if (!els.length) return;
    for (const el of els) el.textContent = "waiting";
    open("/admin/live/alive", {
      alive(raw) {
        const m = parse(raw);
        if (raw === "__isAlive__" || (m && m.data === "__isAlive__")) {
          for (const el of els) el.textContent = "online";
        }
      },
    });
  }

  async function sendCommand(matchId, action, arg) {
    const body = new URLSearchParams({ action });
    if (arg) body.set("arg", arg);
    const res = await fetch(`/admin/matches/${encodeURIComponent(matchId)}/command`, {
      method: "POST",
      credentials: "same-origin",
      redirect: "manual",
      headers: { "Content-Type": "application/x-www-form-urlencoded" },
      body,
    });
    return res.ok || res.type === "opaqueredirect";
  }

  function showResult(text) {
    for (const el of $$("[data-command-result]")) el.textContent = text;
  }

  async function run(matchId, action, arg) {
    try {
      const ok = await sendCommand(matchId, action, arg);
      showResult(ok ? `Sent: ${action}` : `Failed: ${action}`);
      return ok;
    } catch {
      showResult(`Failed: ${action}`);
      return false;
    }
  }

  function initCommands() {
    document.addEventListener("click", (ev) => {
      const btn = ev.target instanceof Element ? ev.target.closest("[data-command][data-match]") : null;
      if (!btn) return;
      ev.preventDefault();
      const msg = btn.getAttribute("data-confirm");
      if (msg !== null && !confirm(msg || "Are you sure?")) return;
      run(btn.getAttribute("data-match"), btn.getAttribute("data-command"), btn.getAttribute("data-arg") || "");
    });

    for (const form of $$("[data-rcon-form]")) {
      const id = form.getAttribute("data-rcon-form");
      form.addEventListener("submit", async (ev) => {
        ev.preventDefault();
        const input = form.elements.namedItem("arg");
        const cmd = input && input.value.trim();
        if (!cmd) return;
        const ok = await run(id, "executeCommand", cmd);
        if (ok) {
          input.value = "";
          for (const box of $$(`[data-console="${attr(id)}"]`)) appendLine(box, `> ${cmd}`);
        }
      });
    }
  }

  function init() {
    initMatches();
    initStream("[data-console]", "data-console", "rcon", "rcon", rconLines);
    initStream("[data-logger]", "data-logger", "logger", "logger", loggerLines);
    initAlive();
    initCommands();
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
})();
