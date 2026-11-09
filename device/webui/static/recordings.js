// Recordings tab: play / download / delete the SD card's clips, and the
// recorder's settings (record at boot, clip length, auto-delete).
(() => {
  "use strict";
  const $ = (id) => document.getElementById(id);
  const api = (...a) => window.webui.api(...a);
  const CLIP_STEPS = [30, 60, 120, 300, 600, 900, 1800, 3600];
  let data = null;
  let pendingDelete = null; // path waiting for the second tap
  let pendingTimer = null;
  const selected = new Set(); // paths with a checked box
  let bulkConfirm = null;

  function mb(n) { return n >= 1024 ? (n / 1024).toFixed(1) + " GB" : n + " MB"; }
  function sizeText(bytes) { const m = bytes / 1048576; return m >= 1024 ? (m / 1024).toFixed(2) + " GB" : m.toFixed(1) + " MB"; }
  function setMsg(text, isError) { const el = $("rec-msg"); el.textContent = text || ""; el.className = "msg" + (isError ? " error" : ""); }

  async function load() {
    try {
      data = await api("/api/recordings");
      if (!Array.isArray(data.files)) data.files = [];
      render();
    } catch (err) { setMsg("Could not refresh the list: " + err.message, true); }
  }

  function stepper(label, text, onMinus, onPlus) {
    const row = document.createElement("div"); row.className = "stepper";
    const l = document.createElement("span"); l.className = "lbl"; l.textContent = label;
    const m = document.createElement("button"); m.textContent = "−"; m.addEventListener("click", onMinus);
    const v = document.createElement("span"); v.className = "val"; v.textContent = text;
    const p = document.createElement("button"); p.textContent = "+"; p.addEventListener("click", onPlus);
    row.append(l, m, v, p);
    return row;
  }
  function toggleBtn(label, on, onClick) {
    const b = document.createElement("button"); b.className = "toggle" + (on ? " on" : ""); b.textContent = label; b.addEventListener("click", onClick); return b;
  }

  async function save(fields) {
    setMsg("Saving…");
    try {
      const r = await api("/api/recordings/settings", fields);
      data.settings = r.settings;
      setMsg(r.error ? r.error : "Saved.", !!r.error);
    } catch (err) { setMsg(err.message, true); }
    render();
  }

  function render() {
    const st = data.settings;
    const panel = $("rec-settings");
    panel.innerHTML = "";
    panel.appendChild(toggleBtn("Record when the camera boots", st.autostart, () => save({ autostart: !st.autostart })));
    const i = CLIP_STEPS.indexOf(st.duration_s);
    panel.appendChild(stepper("Clip length", st.duration_s >= 60 ? (st.duration_s / 60) + " min" : st.duration_s + " s",
      () => save({ duration_s: i > 0 ? CLIP_STEPS[i - 1] : CLIP_STEPS[0] }),
      () => save({ duration_s: i >= 0 && i < CLIP_STEPS.length - 1 ? CLIP_STEPS[i + 1] : (i < 0 ? 60 : st.duration_s) })));
    panel.appendChild(toggleBtn("Delete old clips when the card fills up", st.cleanup_enabled, () => save({ cleanup_enabled: !st.cleanup_enabled })));
    if (st.cleanup_enabled) {
      panel.appendChild(stepper("Keep recordings under", st.limit_gb ? st.limit_gb + " GB" : "no limit",
        () => save({ limit_gb: Math.max(0, st.limit_gb - 1) }), () => save({ limit_gb: st.limit_gb + 1 })));
      panel.appendChild(stepper("Keep free", mb(st.min_free_mb),
        () => save({ min_free_mb: Math.max(0, st.min_free_mb - 256) }), () => save({ min_free_mb: st.min_free_mb + 256 })));
    }

    const info = $("rec-info");
    if (data.error) { info.textContent = data.error; }
    else if (!data.files.length) info.textContent = `No clips. ${mb(data.free_mb)} free of ${mb(data.total_mb)}`;
    else info.textContent = `${data.files.length}${data.truncated ? "+" : ""} clips, ${mb(data.listed_mb)} · ${mb(data.free_mb)} free of ${mb(data.total_mb)}`;

    // forget selections of files that are gone
    const present = new Set((data.files || []).map((f) => f.path));
    [...selected].forEach((p) => { if (!present.has(p)) selected.delete(p); });
    renderBulk();

    const list = $("rec-list");
    list.innerHTML = "";
    let day = "";
    (data.files || []).forEach((f) => {
      const parts = f.path.split("/");
      const d = parts.slice(0, -1).join("-");
      if (d !== day) {
        day = d;
        const h = document.createElement("h3");
        const dayFiles = data.files.filter((x) => x.path.startsWith(parts.slice(0, -1).join("/") + "/"));
        const allOn = dayFiles.every((x) => selected.has(x.path));
        const cb = document.createElement("input"); cb.type = "checkbox"; cb.checked = allOn; cb.title = "Select the whole day";
        cb.addEventListener("change", () => { dayFiles.forEach((x) => { if (x.recording) return; cb.checked ? selected.add(x.path) : selected.delete(x.path); }); render(); });
        h.append(cb, document.createTextNode(` ${d} (${dayFiles.length})`));
        list.appendChild(h);
      }
      const row = document.createElement("div"); row.className = "rec-row";
      const cb = document.createElement("input"); cb.type = "checkbox"; cb.checked = selected.has(f.path); cb.disabled = !!f.recording;
      cb.addEventListener("change", () => { cb.checked ? selected.add(f.path) : selected.delete(f.path); renderBulk(); syncDayBoxes(); });
      const name = document.createElement("span"); name.className = "rec-name"; name.textContent = parts[parts.length - 1].replace(".mp4", "").replace(/-/g, ":") + (f.recording ? "  ● recording" : "");
      const size = document.createElement("span"); size.className = "rec-size"; size.textContent = sizeText(f.size);
      const play = document.createElement("button"); play.textContent = "Play"; play.addEventListener("click", () => playFile(f));
      const dl = document.createElement("a"); dl.textContent = "Save"; dl.href = `/api/recordings/file?p=${encodeURIComponent(f.path)}&dl=1`; dl.className = "btnlink";
      const del = document.createElement("button"); del.className = "danger";
      del.textContent = pendingDelete === f.path ? "Tap again" : "Delete";
      del.disabled = !!f.recording;
      del.addEventListener("click", () => deleteFile(f, del));
      row.append(cb, name, size, play, dl, del);
      row.dataset.path = f.path;
      list.appendChild(row);
    });
  }

  function syncDayBoxes() {
    document.querySelectorAll("#rec-list h3 input").forEach((cb) => {
      const label = cb.parentNode.textContent.trim().split(" ")[0]; // YYYY-MM-DD
      const prefix = label.replace(/-/g, "/") + "/";
      const dayFiles = data.files.filter((x) => x.path.startsWith(prefix));
      cb.checked = dayFiles.length > 0 && dayFiles.every((x) => selected.has(x.path));
    });
  }

  // --- selected files: delete or download as one zip ---------------------
  function renderBulk() {
    const bar = $("rec-bulk");
    const n = selected.size;
    bar.classList.toggle("hidden", n === 0);
    if (n === 0) { clearTimeout(bulkConfirm); bulkConfirm = null; return; }
    let bytes = 0;
    data.files.forEach((f) => { if (selected.has(f.path)) bytes += f.size; });
    $("rec-bulk-info").textContent = `${n} selected · ${sizeText(bytes)}`;
    const del = $("rec-bulk-delete");
    del.textContent = bulkConfirm ? `Tap again to delete ${n}` : `Delete ${n}`;
    del.className = "danger" + (bulkConfirm ? " confirm" : "");
  }
  $("rec-bulk-clear").addEventListener("click", () => { selected.clear(); render(); });
  $("rec-bulk-delete").addEventListener("click", async () => {
    if (!bulkConfirm) {
      bulkConfirm = setTimeout(() => { bulkConfirm = null; renderBulk(); }, 3000);
      renderBulk();
      return;
    }
    clearTimeout(bulkConfirm); bulkConfirm = null;
    setMsg(`Deleting ${selected.size} files…`);
    const paths = [...selected];
    try {
      const r = await api("/api/recordings/delete", { paths });
      setMsg(`Deleted ${r.deleted} files.` + (r.skipped && r.skipped.length ? ` Skipped: ${r.skipped.join("; ")}` : ""));
      const skipped = new Set((r.skipped || []).map((x) => x.split(":")[0]));
      dropLocal(paths.filter((p) => !skipped.has(p)));
    } catch (err) { setMsg(err.message, true); }
    await load();
  });
  $("rec-bulk-zip").addEventListener("click", async () => {
    setMsg("Preparing the download…");
    try {
      const r = await api("/api/recordings/zip", { paths: [...selected] });
      setMsg(`Downloading ${r.files} files (${sizeText(r.bytes)}) as one zip. On an iPhone it lands in the Files app.`);
      const a = document.createElement("a");
      a.href = `/api/recordings/zip/${r.id}`;
      a.download = "";
      document.body.appendChild(a); a.click(); a.remove();
    } catch (err) { setMsg(err.message, true); }
  });

  function playFile(f) {
    const v = $("rec-player");
    v.src = `/api/recordings/file?p=${encodeURIComponent(f.path)}`;
    $("rec-player-box").classList.remove("hidden");
    $("rec-player-title").textContent = f.path;
    v.play().catch(() => {});
    $("rec-player-box").scrollIntoView({ behavior: "smooth", block: "start" });
  }
  $("rec-player-close").addEventListener("click", () => {
    const v = $("rec-player"); v.pause(); v.removeAttribute("src"); v.load();
    $("rec-player-box").classList.add("hidden");
  });

  async function deleteFile(f, btn) {
    if (pendingDelete !== f.path) {
      pendingDelete = f.path;
      btn.textContent = "Tap again";
      clearTimeout(pendingTimer);
      pendingTimer = setTimeout(() => { pendingDelete = null; render(); }, 3000);
      return;
    }
    clearTimeout(pendingTimer);
    pendingDelete = null;
    setMsg("Deleting…");
    try {
      const r = await api("/api/recordings/delete", { path: f.path });
      if (r.deleted === 1) { setMsg("Deleted " + f.path); dropLocal([f.path]); }
      else setMsg("Not deleted: " + ((r.skipped && r.skipped[0]) || "unknown reason"), true);
    } catch (err) { setMsg(err.message, true); }
    await load();
  }

  // take deleted files out of the list at once, before the fresh list arrives
  function dropLocal(paths) {
    const gone = new Set(paths);
    data.files = (data.files || []).filter((f) => !gone.has(f.path));
    paths.forEach((p) => selected.delete(p));
    render();
  }

  window.recTab = {
    open() { load(); },
    close() {
      const v = $("rec-player"); if (v.src) { v.pause(); v.removeAttribute("src"); v.load(); }
      $("rec-player-box").classList.add("hidden");
    },
  };
})();
