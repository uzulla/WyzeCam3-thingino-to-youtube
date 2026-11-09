// webui app: one page, plain JS. Talks to /api/* (webui's Go server).
(() => {
  "use strict";
  const $ = (id) => document.getElementById(id);
  const STATUS_MS = 2000;
  const CONFIRM_MS = 3000;

  let status = null;
  let pollTimer = null;
  let profiles = null; // {profiles:[...], active, autostart}
  const pending = {}; // button id -> timeout for "tap again"

  // --- api -----------------------------------------------------------------
  async function api(path, body) {
    const opt = body === undefined ? {} : { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) };
    const r = await fetch(path, opt);
    let data = null;
    try { data = await r.json(); } catch (_) { data = null; }
    if (r.status === 401) { showLogin(); throw new Error("sign in first"); }
    if (!r.ok) throw new Error((data && data.error) || `HTTP ${r.status}`);
    return data;
  }

  window.webui = { api };

  // --- screens -------------------------------------------------------------
  function showLogin() {
    stopPolling();
    $("preview").src = "";
    $("app").classList.add("hidden");
    $("tabs").classList.add("hidden");
    showTab("home");
    $("login").classList.remove("hidden");
    setTimeout(() => $("password").focus(), 50);
  }
  function showApp() {
    $("login").classList.add("hidden");
    $("app").classList.remove("hidden");
    $("tabs").classList.remove("hidden");
    startPreview();
    startPolling();
  }

  $("login-form").addEventListener("submit", async (e) => {
    e.preventDefault();
    $("login-error").textContent = "";
    const btn = e.target.querySelector("button");
    btn.disabled = true;
    try {
      await api("/api/login", { password: $("password").value });
      $("password").value = "";
      showApp();
    } catch (err) {
      $("login-error").textContent = err.message;
    } finally {
      btn.disabled = false;
    }
  });

  $("btn-logout").addEventListener("click", async () => {
    try { await api("/api/logout", {}); } catch (_) {}
    showLogin();
  });

  // --- preview -------------------------------------------------------------
  let previewRetry = null;
  function startPreview() {
    const img = $("preview");
    $("preview-off").classList.add("hidden");
    img.src = `/api/preview.mjpeg?fps=4&t=${Date.now()}`;
  }
  $("preview").addEventListener("error", () => {
    $("preview-off").classList.remove("hidden");
    clearTimeout(previewRetry);
    if (!$("app").classList.contains("hidden")) previewRetry = setTimeout(startPreview, 3000);
  });
  // a phone that comes back from the background has a dead stream: restart it
  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState === "visible" && !$("app").classList.contains("hidden")) {
      if (window.osdTab && window.osdTab.isOpen()) window.osdTab.open(); else startPreview();
      refresh();
    }
  });

  // --- status --------------------------------------------------------------
  function startPolling() { stopPolling(); refresh(); pollTimer = setInterval(refresh, STATUS_MS); }
  function stopPolling() { clearInterval(pollTimer); pollTimer = null; }

  async function refresh() {
    try {
      status = await api("/api/status");
      render();
    } catch (err) {
      if (err.message !== "sign in first") setMsg("action-msg", `No answer from the camera: ${err.message}`, true);
    }
  }

  function hms(s) {
    s = Math.max(0, s | 0);
    const h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60), sec = s % 60;
    return (h ? h + ":" + String(m).padStart(2, "0") : m) + ":" + String(sec).padStart(2, "0");
  }
  function uptimeText(s) {
    const d = Math.floor(s / 86400), h = Math.floor((s % 86400) / 3600), m = Math.floor((s % 3600) / 60);
    return d ? `${d}d ${h}h` : h ? `${h}h ${m}m` : `${m}m`;
  }

  function render() {
    const st = status;
    $("hostname").textContent = st.hostname || "camera";
    // badges + buttons
    const live = st.relay.ffmpeg_running;
    $("badge-live").classList.toggle("hidden", !live);
    $("badge-live").querySelector(".t").textContent = hms(st.relay.ffmpeg_elapsed_s);
    const rec = st.recording.active;
    $("badge-rec").classList.toggle("hidden", !rec);
    $("badge-rec").querySelector(".t").textContent = hms(st.recording.elapsed_s);
    $("badge-idle").classList.toggle("hidden", live || rec);
    if (!pending["btn-live"]) {
      const b = $("btn-live");
      if (st.relay.running) { b.textContent = live ? "Stop live" : "Stop (connecting…)"; b.className = "big on"; }
      else { b.textContent = "Go live"; b.className = "big"; }
    }
    if (!pending["btn-rec"]) {
      const b = $("btn-rec");
      b.textContent = rec ? "Stop recording" : "Record";
      b.className = "big" + (rec ? " on" : "");
      b.disabled = !st.recording.sd_mounted && !rec;
    }

    // cards
    $("st-fps").textContent = st.stream.fps;
    $("st-kbps").textContent = st.stream.kbps;
    $("st-tx").textContent = st.stream.tx_kbps;
    $("st-restarts").textContent = st.relay.restarts;
    $("st-profile").textContent = st.relay.profile || "-";
    $("st-autostart").textContent = st.relay.autostart ? "on" : "off";
    $("btn-hflip").classList.toggle("on", !!st.image.hflip);
    $("btn-vflip").classList.toggle("on", !!st.image.vflip);
    $("st-ip").textContent = st.ip || "-";
    const wifi = $("st-wifi");
    if (st.wifi && st.wifi.ssid) {
      const r = st.wifi.rssi_dbm;
      wifi.textContent = r == null ? st.wifi.ssid : `${st.wifi.ssid} · ${r} dBm`;
      wifi.className = r != null && r < -75 ? "warn" : "";
    } else { wifi.textContent = "not connected"; wifi.className = "bad"; }
    const sd = $("st-sd");
    if (st.recording.sd_mounted) {
      const gb = st.recording.free_mb / 1024;
      sd.textContent = `${gb >= 10 ? gb.toFixed(0) : gb.toFixed(1)} GB free of ${(st.recording.total_mb / 1024).toFixed(1)} GB`;
      sd.className = st.recording.free_mb < 1024 ? "warn" : "";
    } else { sd.textContent = "no card"; sd.className = "bad"; }
    $("st-uptime").textContent = uptimeText(st.uptime_s);
    $("st-load").textContent = st.load1 || "-";
    $("st-mem").textContent = st.mem_free_mb;
    $("ap-banner").classList.toggle("hidden", !st.ap_mode);
    if (st.errors && st.errors.length) setMsg("action-msg", st.errors.join(" · "), true);
  }

  let rebootConfirm = null;
  $("btn-reboot").addEventListener("click", async () => {
    const b = $("btn-reboot");
    if (!rebootConfirm) {
      rebootConfirm = setTimeout(() => { rebootConfirm = null; b.textContent = "Reboot camera"; b.className = "small"; }, CONFIRM_MS);
      b.textContent = "Tap again to reboot"; b.className = "small confirm";
      return;
    }
    clearTimeout(rebootConfirm); rebootConfirm = null;
    b.disabled = true; b.textContent = "Rebooting…";
    try { await api("/api/reboot", {}); setMsg("action-msg", "Rebooting. The camera is back in about a minute; reload the page then."); }
    catch (err) { setMsg("action-msg", err.message, true); b.disabled = false; b.textContent = "Reboot camera"; b.className = "small"; }
  });

  function setMsg(id, text, isError) {
    const el = $(id);
    el.textContent = text || "";
    el.className = "msg" + (isError ? " error" : "");
  }

  // --- actions: a stop needs a second tap within 3 s --------------------------
  function twoTap(btnId, label, fn) {
    const b = $(btnId);
    if (pending[btnId]) {
      clearTimeout(pending[btnId]);
      delete pending[btnId];
      fn();
      return;
    }
    const was = { text: b.textContent, cls: b.className };
    b.textContent = label;
    b.className = "big confirm";
    pending[btnId] = setTimeout(() => {
      delete pending[btnId];
      b.textContent = was.text;
      b.className = was.cls;
    }, CONFIRM_MS);
  }

  async function act(btnId, path, body, doing) {
    const b = $(btnId);
    b.disabled = true;
    setMsg("action-msg", doing);
    try {
      const r = await api(path, body);
      setMsg("action-msg", r.output && r.output.length < 120 ? r.output : "");
    } catch (err) {
      setMsg("action-msg", err.message, true);
    } finally {
      b.disabled = false;
      refresh();
    }
  }

  $("btn-live").addEventListener("click", () => {
    if (!status) return;
    if (status.relay.running) twoTap("btn-live", "Tap again to stop", () => act("btn-live", "/api/relay", { op: "stop" }, "Stopping the stream…"));
    else act("btn-live", "/api/relay", { op: "start" }, "Starting the stream…");
  });
  $("btn-rec").addEventListener("click", () => {
    if (!status) return;
    if (status.recording.active) twoTap("btn-rec", "Tap again to stop", () => act("btn-rec", "/api/record", { op: "stop" }, "Stopping the recording…"));
    else act("btn-rec", "/api/record", { op: "start" }, "Starting the recording…");
  });
  $("btn-hflip").addEventListener("click", () => act("btn-hflip", "/api/image", { hflip: !status.image.hflip }, "Flipping…"));
  $("btn-vflip").addEventListener("click", () => act("btn-vflip", "/api/image", { vflip: !status.image.vflip }, "Flipping…"));

  // --- tabs ----------------------------------------------------------------
  function showTab(name) {
    $("sheet-stream").classList.toggle("hidden", name !== "stream");
    $("sheet-osd").classList.toggle("hidden", name !== "osd");
    $("sheet-rec").classList.toggle("hidden", name !== "rec");
    $("sheet-wifi").classList.toggle("hidden", name !== "wifi");
    if (window.wifiTab) { if (name === "wifi") window.wifiTab.open(); else window.wifiTab.close(); }
    if (window.recTab) { if (name === "rec") window.recTab.open(); else window.recTab.close(); }
    if (window.osdTab) {
      if (name === "osd" && !window.osdTab.isOpen()) { $("preview").src = ""; window.osdTab.open(); }
      if (name !== "osd" && window.osdTab.isOpen()) { window.osdTab.close(); if (name === "home") startPreview(); }
    }
    document.querySelectorAll("#tabs button").forEach((b) => b.classList.toggle("on", b.dataset.tab === name));
    if (name === "stream") { loadProfiles(); loadLog(); loadQuality(); }
  }
  document.querySelectorAll("#tabs button").forEach((b) => b.addEventListener("click", () => showTab(b.dataset.tab)));

  async function loadProfiles() {
    try {
      profiles = await api("/api/relay/config");
      renderProfiles();
      $("btn-autostart").classList.toggle("on", !!profiles.autostart);
      setMsg("profiles-msg", "");
    } catch (err) {
      setMsg("profiles-msg", err.message, true);
    }
  }

  function renderProfiles() {
    const box = $("profiles");
    box.innerHTML = "";
    profiles.profiles.forEach((p, i) => {
      const node = $("tpl-profile").content.firstElementChild.cloneNode(true);
      node.querySelector("input[type=radio]").checked = p.name === profiles.active;
      node.querySelector("input[type=radio]").addEventListener("change", () => { profiles.active = p.name; });
      const name = node.querySelector(".p-name"); name.value = p.name;
      name.addEventListener("input", () => { if (profiles.active === p.name) profiles.active = name.value; p.name = name.value; });
      const url = node.querySelector(".p-url"); url.value = p.rtmp_url;
      url.addEventListener("input", () => { p.rtmp_url = url.value; });
      const key = node.querySelector(".p-key"); key.value = p.stream_key;
      key.addEventListener("input", () => { p.stream_key = key.value; });
      node.querySelector(".p-remove").addEventListener("click", () => {
        profiles.profiles.splice(i, 1);
        if (profiles.active === p.name && profiles.profiles.length) profiles.active = profiles.profiles[0].name;
        renderProfiles();
      });
      box.appendChild(node);
    });
  }

  $("btn-add-profile").addEventListener("click", () => {
    if (!profiles) return;
    profiles.profiles.push({ name: `Profile ${profiles.profiles.length + 1}`, rtmp_url: "rtmps://a.rtmps.youtube.com:443/live2", stream_key: "" });
    if (profiles.profiles.length === 1) profiles.active = profiles.profiles[0].name;
    renderProfiles();
  });

  $("btn-save-profiles").addEventListener("click", async () => {
    if (!profiles) return;
    setMsg("profiles-msg", "Saving…");
    try {
      const r = await api("/api/relay/config", { profiles: profiles.profiles, active: profiles.active });
      setMsg("profiles-msg", r.error ? r.error : r.restarted ? "Saved. The stream restarts with the new key." : r.changed ? "Saved." : "Saved (nothing changed for the stream).", !!r.error);
      await loadProfiles();
      refresh();
    } catch (err) {
      setMsg("profiles-msg", err.message, true);
    }
  });

  $("btn-autostart").addEventListener("click", async () => {
    if (!profiles) return;
    const op = profiles.autostart ? "disable" : "enable";
    try {
      await api("/api/relay", { op });
      await loadProfiles();
      refresh();
    } catch (err) {
      setMsg("profiles-msg", err.message, true);
    }
  });

  // --- video quality --------------------------------------------------------
  let quality = null; // {current, sensor, sizes}
  let qEdit = null;   // the values being edited
  let qConfirm = null;

  async function loadQuality() {
    try {
      quality = await api("/api/quality");
      qEdit = Object.assign({}, quality.current);
      renderQuality();
    } catch (err) { setMsg("quality-msg", err.message, true); }
  }

  function qStepper(label, text, onMinus, onPlus) {
    const row = document.createElement("div"); row.className = "stepper";
    const l = document.createElement("span"); l.className = "lbl"; l.textContent = label;
    const m = document.createElement("button"); m.textContent = "−"; m.addEventListener("click", () => { onMinus(); renderQuality(); });
    const v = document.createElement("span"); v.className = "val"; v.textContent = text;
    const p = document.createElement("button"); p.textContent = "+"; p.addEventListener("click", () => { onPlus(); renderQuality(); });
    row.append(l, m, v, p);
    return row;
  }

  function renderQuality() {
    const box = $("quality");
    box.innerHTML = "";
    if (!quality) return;
    const q = qEdit, sensor = quality.sensor;
    const sizes = document.createElement("div"); sizes.className = "choices";
    quality.sizes.forEach(([w, h]) => {
      const b = document.createElement("button");
      b.textContent = `${w}×${h}`;
      b.className = (q.width === w && q.height === h) ? "on" : "";
      b.addEventListener("click", () => { q.width = w; q.height = h; renderQuality(); });
      sizes.appendChild(b);
    });
    box.appendChild(sizes);
    box.appendChild(qStepper("Frames per second", q.fps + " fps",
      () => { q.fps = Math.max(sensor.min_fps, q.fps - 5); }, () => { q.fps = Math.min(sensor.max_fps, q.fps + 5); }));
    const step = q.bitrate >= 2048 ? 512 : 256;
    box.appendChild(qStepper("Bitrate", q.bitrate + " kb/s",
      () => { q.bitrate = Math.max(128, q.bitrate - step); }, () => { q.bitrate = Math.min(8192, q.bitrate + step); }));
    const modes = document.createElement("div"); modes.className = "choices";
    ["CBR", "VBR"].forEach((m) => {
      const b = document.createElement("button");
      b.textContent = m === "CBR" ? "CBR (steady bitrate, best for streaming)" : "VBR (variable)";
      b.className = q.mode === m ? "on" : "";
      b.addEventListener("click", () => { q.mode = m; renderQuality(); });
      modes.appendChild(b);
    });
    box.appendChild(modes);
    const changed = JSON.stringify(q) !== JSON.stringify(quality.current);
    const apply = document.createElement("button");
    apply.className = "apply" + (qConfirm ? " confirm" : "");
    apply.textContent = qConfirm ? "Tap again to apply (10 s off air)" : (changed ? "Apply" : "No changes");
    apply.disabled = !changed;
    apply.addEventListener("click", applyQuality);
    box.appendChild(apply);
  }

  async function applyQuality() {
    if (!qConfirm) {
      qConfirm = setTimeout(() => { qConfirm = null; renderQuality(); }, CONFIRM_MS);
      renderQuality();
      return;
    }
    clearTimeout(qConfirm); qConfirm = null;
    setMsg("quality-msg", "Applying… the encoder restarts (10 to 20 s).");
    renderQuality();
    try {
      const r = await api("/api/quality", qEdit);
      setMsg("quality-msg", r.changed ? `Applied. The encoder came back in ${r.restart_s} s.` : "Nothing changed.");
    } catch (err) {
      setMsg("quality-msg", err.message, true);
    }
    await loadQuality();
    refresh();
  }

  async function loadLog() {
    try {
      const r = await api("/api/log");
      $("log").textContent = r.lines && r.lines.length ? r.lines.join("\n") : "(no lines)";
    } catch (err) {
      $("log").textContent = err.message;
    }
  }

  // --- start -----------------------------------------------------------------
  api("/api/me").then((r) => (r.signed_in ? showApp() : showLogin())).catch(showLogin);
})();
