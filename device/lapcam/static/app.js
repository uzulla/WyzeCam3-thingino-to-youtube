// lapcam app: one page, plain JS. Talks to /api/* (lapcam's Go server).
(() => {
  "use strict";
  const $ = (id) => document.getElementById(id);
  const STATUS_MS = 2000;
  const CONFIRM_MS = 3000;

  let status = null;
  let pollTimer = null;
  let polling = false; // one /api/status in flight at a time
  let profiles = null; // {profiles:[...], active, autostart}
  const pending = {}; // button id -> timeout for "tap again"

  // --- api -----------------------------------------------------------------
  async function api(path, body) {
    const opt = body === undefined ? {} : { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) };
    const r = await fetch(path, opt);
    let data = null;
    try { data = await r.json(); } catch (_) { data = null; }
    if (r.status === 401) {
      if (path === "/api/login") throw new Error((data && data.error) || "wrong password");
      showLogin();
      throw new Error("sign in first");
    }
    if (!r.ok) throw new Error((data && data.error) || `HTTP ${r.status}`);
    return data;
  }

  window.lapcam = { api };

  // --- screens -------------------------------------------------------------
  function showLogin() {
    stopPolling();
    stopPreview();
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
  // Two ways to show the picture: "video" (fragmented MP4 through
  // MediaSource: smooth, with the stream's own frame rate; Android, PC,
  // newer iPhones) and "mjpeg" (4 still pictures a second; works everywhere).
  // The choice is kept on the phone; video falls back to mjpeg by itself.
  let previewRetry = null;
  let previewMode = (() => {
    try {
      const saved = localStorage.getItem("previewMode");
      if (saved === "video" || saved === "mjpeg") return saved;
    } catch (_) {}
    return window.livePlayer && window.livePlayer.supported() ? "video" : "mjpeg";
  })();
  let videoFailedOnce = false;
  let videoRetries = 0; // transient failures retried before falling back to pictures
  const VIDEO_RETRIES = 2;
  // reasons that mean "this browser cannot", as opposed to a hiccup of the camera or the Wi-Fi
  const PERMANENT = /no MediaSource|codec not supported|video element error|not a video stream/;

  function renderPreviewMode() {
    const b = $("btn-preview-mode");
    b.textContent = previewMode === "video" ? "Video" : "Pictures";
    b.title = previewMode === "video" ? "Smooth video. Tap for still pictures (lighter)" : "Still pictures, 4 a second. Tap for smooth video";
    b.classList.toggle("hidden", !(window.livePlayer && window.livePlayer.supported()));
  }

  function startPreview() {
    stopPreview();
    // only on the Camera screen of a signed-in, visible page: the login
    // screen or another tab must not pull pictures (a 401 there used to
    // bounce between "sign in" and "start" without end)
    if ($("app").classList.contains("hidden") || currentTab !== "home" || document.visibilityState !== "visible") return;
    $("preview-off").classList.add("hidden");
    renderPreviewMode();
    if (previewMode === "video" && window.livePlayer && window.livePlayer.supported()) {
      $("preview").classList.add("hidden");
      $("preview-video").classList.remove("hidden");
      window.livePlayer.start($("preview-video"), "/api/preview.mp4", (why) => {
        if (why === "HTTP 401") { showLogin(); return; } // the session is gone (lapcam restarted): sign in again
        console.warn("video preview failed:", why);
        if (!PERMANENT.test(why) && videoRetries < VIDEO_RETRIES) {
          // a hiccup (encoder restart, Wi-Fi, a slot taken): try the video again shortly
          videoRetries++;
          clearTimeout(previewRetry);
          previewRetry = setTimeout(startPreview, 3000);
          return;
        }
        // this browser cannot do the video, or it keeps failing: pictures for this page load
        if (!videoFailedOnce) setMsg("action-msg", "Smooth video is not working here (" + why + "); showing pictures.");
        videoFailedOnce = true;
        previewMode = "mjpeg";
        startPreview();
      });
      return;
    }
    $("preview-video").classList.add("hidden");
    $("preview").classList.remove("hidden");
    $("preview").src = `/api/preview.mjpeg?fps=4&t=${Date.now()}`;
  }
  function stopPreview() {
    clearTimeout(previewRetry);
    if (window.livePlayer) window.livePlayer.stop();
    // not src = "": an empty src makes the browser load the page URL as an
    // image, fire "error", and that handler would restart the preview
    $("preview").removeAttribute("src");
  }
  $("btn-preview-mode").addEventListener("click", () => {
    previewMode = previewMode === "video" ? "mjpeg" : "video";
    videoRetries = 0;
    try { localStorage.setItem("previewMode", previewMode); } catch (_) {}
    startPreview();
  });
  $("preview").addEventListener("error", () => {
    if (!$("preview").getAttribute("src") || previewMode !== "mjpeg") return; // cleared on purpose, or not in use
    $("preview-off").classList.remove("hidden");
    clearTimeout(previewRetry);
    previewRetry = setTimeout(() => refresh().then(startPreview), 3000); // startPreview checks the screen itself
  });
  // a phone that comes back from the background has a dead stream: restart it
  document.addEventListener("visibilitychange", () => {
    if ($("app").classList.contains("hidden")) return;
    if (document.visibilityState === "visible") {
      if (window.osdTab && window.osdTab.isOpen()) window.osdTab.open(); else if (currentTab === "home") startPreview();
      refresh();
    } else {
      // in the background the pictures would only use the camera's Wi-Fi
      stopPreview();
      if (window.osdTab && window.osdTab.isOpen()) $("osd-preview").removeAttribute("src");
    }
  });
  let currentTab = "home";

  // --- status --------------------------------------------------------------
  function startPolling() { stopPolling(); refresh(); pollTimer = setInterval(refresh, STATUS_MS); }
  function stopPolling() { clearInterval(pollTimer); pollTimer = null; }

  async function refresh() {
    if (polling) return; // a slow Wi-Fi must not pile requests up
    polling = true;
    try {
      status = await api("/api/status");
      render();
    } catch (err) {
      if (err.message !== "sign in first") setMsg("status-errors", `No answer from the camera: ${err.message}`, true);
    } finally {
      polling = false;
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
    const ver = $("st-version"), v = st.versions || {};
    // the binary on the card and the firmware image are released together:
    // different numbers mean one of them was updated without the other
    let vt = v.lapcam || "-";
    if (v.image) vt += v.mismatch ? ` · image ${v.image} (does not match: update the card or the firmware)` : ` · image ${v.image}`;
    else vt += " · no lapcam image (installed by hand)";
    if (v.thingino) vt += ` · Thingino ${v.thingino}`;
    ver.textContent = vt; ver.className = v.mismatch ? "bad" : "";
    $("st-load").textContent = st.load1 || "-";
    $("st-mem").textContent = st.mem_free_mb;
    $("ap-banner").classList.toggle("hidden", !st.ap_mode);
    setMsg("status-errors", st.errors && st.errors.length ? st.errors.join(" · ") : "", true);
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
    currentTab = name;
    // one preview at a time: the Camera screen's picture stops behind other tabs
    if (name === "home") { videoRetries = 0; startPreview(); } else stopPreview();
    $("sheet-stream").classList.toggle("hidden", name !== "stream");
    $("sheet-osd").classList.toggle("hidden", name !== "osd");
    $("sheet-rec").classList.toggle("hidden", name !== "rec");
    $("sheet-wifi").classList.toggle("hidden", name !== "wifi");
    if (window.wifiTab) { if (name === "wifi") window.wifiTab.open(); else window.wifiTab.close(); }
    if (window.recTab) { if (name === "rec") window.recTab.open(); else window.recTab.close(); }
    if (window.osdTab) {
      if (name === "osd" && !window.osdTab.isOpen()) window.osdTab.open();
      if (name !== "osd" && window.osdTab.isOpen()) window.osdTab.close();
    }
    document.querySelectorAll("#tabs button").forEach((b) => b.classList.toggle("on", b.dataset.tab === name));
    if (name === "stream") { loadProfiles(); loadLog(); loadQuality(); }
  }
  document.querySelectorAll("#tabs button").forEach((b) => b.addEventListener("click", () => showTab(b.dataset.tab)));

  async function loadProfiles() {
    try {
      profiles = await api("/api/relay/config");
      if (!Array.isArray(profiles.profiles)) profiles.profiles = [];
      renderProfiles();
      $("btn-autostart").classList.toggle("on", !!profiles.autostart);
      setMsg("profiles-msg", profiles.warning || "", !!profiles.warning);
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
      node.querySelector(".p-show").addEventListener("click", (e) => {
        key.type = key.type === "password" ? "text" : "password";
        e.target.textContent = key.type === "password" ? "Show" : "Hide";
      });
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
    if (!Array.isArray(quality.sizes)) quality.sizes = [];
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
