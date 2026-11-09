// OSD tab: the overlay elements drawn on the preview, moved by dragging,
// sized and switched with the panel below. Uses window.api from app.js.
(() => {
  "use strict";
  const $ = (id) => document.getElementById(id);
  const api = (...a) => window.webui.api(...a);
  const SNAP = 20; // px (stream pixels) from an edge to snap to the 8 px margin
  const MARGIN = 8;

  const LABELS = {
    burnin: "Date & time", textfile: "Text 1", textfile2: "Text 2", textfile3: "Text 3",
    imagefile: "Logo", imagefile2: "G-ball", imagefile3: "Track map",
  };
  const ORDER = ["burnin", "textfile", "textfile2", "textfile3", "imagefile", "imagefile2", "imagefile3"];

  let data = null; // from /api/osd
  let sel = null;
  let drag = null;
  let open = false;

  function autoScale() { return Math.max(1, Math.floor(data.stream.width / 480)); }

  // the box of an element in stream pixels
  function boxOf(name) {
    const e = data.elements[name] || {};
    const W = data.stream.width, H = data.stream.height;
    let w, h, x, y;
    if (name === "burnin") {
      // prudynt draws it at (8, 8) with a TTF font of 8*scale px: "2026-11-09 17:47:23"
      // measures about 8.9*scale px per character and 10*scale px tall, plus 2*scale padding
      const s = e.scale || autoScale();
      const chars = 19;
      w = Math.round(chars * 8.9 * s + 4 * s); h = 10 * s + 4 * s; x = 8; y = 8;
    } else if (name.startsWith("textfile")) {
      const s = e.scale || autoScale();
      const c = data.cell;
      w = (e.cols || 1) * c.w * s + c.pad * 2 * s;
      h = (e.rows || 1) * c.h * s + c.pad * 2 * s;
    } else {
      w = e.width || 100; h = e.height || 100;
    }
    if (name !== "burnin") {
      x = (e.pos_x || 0) >= 0 ? (e.pos_x || 0) : W + (e.pos_x || 0) - w;
      y = (e.pos_y || 0) >= 0 ? (e.pos_y || 0) : H + (e.pos_y || 0) - h;
    }
    return { x, y, w, h };
  }

  // stream position (anchored to the nearer edge) from a box
  function posOf(b) {
    const W = data.stream.width, H = data.stream.height;
    const cx = b.x + b.w / 2, cy = b.y + b.h / 2;
    return {
      pos_x: cx <= W / 2 ? Math.round(b.x) : -Math.round(W - (b.x + b.w)),
      pos_y: cy <= H / 2 ? Math.round(b.y) : -Math.round(H - (b.y + b.h)),
    };
  }

  function clampSnap(b) {
    const W = data.stream.width, H = data.stream.height;
    b.x = Math.max(0, Math.min(W - b.w, b.x));
    b.y = Math.max(0, Math.min(H - b.h, b.y));
    if (b.x <= SNAP) b.x = MARGIN;
    if (W - (b.x + b.w) <= SNAP) b.x = W - MARGIN - b.w;
    if (b.y <= SNAP) b.y = MARGIN;
    if (H - (b.y + b.h) <= SNAP) b.y = H - MARGIN - b.h;
    return b;
  }

  // the overlay must cover exactly the picture, not the black bars around it
  // (in landscape the box is taller than 16:9 and the image is letterboxed)
  function fitLayer() {
    if (!data) return;
    const box = $("osd-preview-box").getBoundingClientRect();
    const layer = $("osd-layer");
    const ar = data.stream.width / data.stream.height;
    let w = box.width, h = w / ar;
    if (h > box.height) { h = box.height; w = h * ar; }
    layer.style.left = ((box.width - w) / 2) + "px";
    layer.style.top = ((box.height - h) / 2) + "px";
    layer.style.width = w + "px";
    layer.style.height = h + "px";
  }
  window.addEventListener("resize", fitLayer);
  if (window.ResizeObserver) new ResizeObserver(fitLayer).observe($("osd-preview-box"));

  function render() {
    fitLayer();
    const layer = $("osd-layer");
    layer.innerHTML = "";
    const W = data.stream.width, H = data.stream.height;
    ORDER.forEach((name) => {
      const e = data.elements[name];
      if (!e) return;
      const b = boxOf(name);
      const div = document.createElement("div");
      div.className = "osd-el" + (e.enabled ? "" : " off") + (sel === name ? " sel" : "") + (name === "burnin" ? " fixed" : "");
      div.dataset.name = name;
      div.style.left = (b.x / W * 100) + "%";
      div.style.top = (b.y / H * 100) + "%";
      div.style.width = (b.w / W * 100) + "%";
      div.style.height = (b.h / H * 100) + "%";
      div.textContent = LABELS[name];
      div.addEventListener("pointerdown", onDown);
      layer.appendChild(div);
    });
    renderChips();
    renderPanel();
  }

  function renderChips() {
    const box = $("osd-chips");
    box.innerHTML = "";
    ORDER.forEach((name) => {
      if (!data.elements[name]) return;
      const b = document.createElement("button");
      b.className = "chipbtn" + (sel === name ? " on" : "") + (data.elements[name].enabled ? "" : " off");
      b.textContent = LABELS[name];
      b.addEventListener("click", () => select(name));
      box.appendChild(b);
    });
  }

  // --- drag ------------------------------------------------------------------
  // select without rebuilding the layer (a rebuild would drop the element
  // that holds the pointer capture and end the drag before it starts)
  function select(name) {
    sel = name;
    document.querySelectorAll(".osd-el").forEach((el) => el.classList.toggle("sel", el.dataset.name === name));
    renderChips();
    renderPanel();
  }
  function onDown(ev) {
    const name = ev.currentTarget.dataset.name;
    select(name);
    if (name === "burnin") return;
    ev.preventDefault();
    const layer = $("osd-layer").getBoundingClientRect();
    const scale = data.stream.width / layer.width;
    drag = { name, start: boxOf(name), px: ev.clientX, py: ev.clientY, scale, el: ev.currentTarget, moved: false, id: ev.pointerId };
    try { ev.currentTarget.setPointerCapture(ev.pointerId); } catch (_) {}
    document.addEventListener("pointermove", onMove);
    document.addEventListener("pointerup", onUp);
    document.addEventListener("pointercancel", onUp);
  }
  function onMove(ev) {
    if (!drag) return;
    const dx = (ev.clientX - drag.px) * drag.scale, dy = (ev.clientY - drag.py) * drag.scale;
    if (Math.abs(dx) + Math.abs(dy) > 2) drag.moved = true;
    const b = clampSnap({ x: drag.start.x + dx, y: drag.start.y + dy, w: drag.start.w, h: drag.start.h });
    const el = document.querySelector(`.osd-el[data-name="${drag.name}"]`);
    if (el) { el.style.left = (b.x / data.stream.width * 100) + "%"; el.style.top = (b.y / data.stream.height * 100) + "%"; }
    drag.last = b;
  }
  async function onUp(ev) {
    if (!drag) return;
    const d = drag; drag = null;
    document.removeEventListener("pointermove", onMove);
    document.removeEventListener("pointerup", onUp);
    document.removeEventListener("pointercancel", onUp);
    try { d.el.releasePointerCapture(d.id); } catch (_) {}
    if (!d.moved || !d.last) return;
    const pos = posOf(d.last);
    await change(d.name, pos);
  }

  // --- changes -------------------------------------------------------------
  async function change(name, fields) {
    setMsg("Saving…");
    try {
      await api("/api/osd/element", Object.assign({ name }, fields));
      Object.assign(data.elements[name], fields);
      setMsg("");
    } catch (err) {
      setMsg(err.message, true);
    }
    render();
  }

  function setMsg(text, isError) {
    const el = $("osd-msg");
    el.textContent = text || "";
    el.className = "msg" + (isError ? " error" : "");
  }

  // --- panel -----------------------------------------------------------------
  function stepper(label, value, onMinus, onPlus, text) {
    const row = document.createElement("div");
    row.className = "stepper";
    const l = document.createElement("span"); l.className = "lbl"; l.textContent = label;
    const m = document.createElement("button"); m.textContent = "−"; m.addEventListener("click", onMinus);
    const v = document.createElement("span"); v.className = "val"; v.textContent = text !== undefined ? text : value;
    const p = document.createElement("button"); p.textContent = "+"; p.addEventListener("click", onPlus);
    row.append(l, m, v, p);
    return row;
  }
  function toggleBtn(label, on, onClick) {
    const b = document.createElement("button");
    b.className = "toggle" + (on ? " on" : "");
    b.textContent = label;
    b.addEventListener("click", onClick);
    return b;
  }

  function renderPanel() {
    const panel = $("osd-panel");
    panel.innerHTML = "";
    if (!sel || !data.elements[sel]) { panel.classList.add("hidden"); return; }
    panel.classList.remove("hidden");
    const name = sel, e = data.elements[name];
    const h = document.createElement("h2"); h.textContent = LABELS[name]; panel.appendChild(h);
    panel.appendChild(toggleBtn("Show on the video", e.enabled, () => change(name, { enabled: !e.enabled })));

    if (name === "burnin" || name.startsWith("textfile")) {
      const s = e.scale || 0;
      panel.appendChild(stepper("Text size", s,
        () => change(name, { scale: Math.max(1, (s || autoScale()) - 1) }),
        () => change(name, { scale: Math.min(10, (s || autoScale()) + 1) }),
        s ? String(s) : `auto (${autoScale()})`));
    }
    if (name.startsWith("textfile")) {
      panel.appendChild(stepper("Columns", e.cols, () => change(name, { cols: Math.max(1, e.cols - 1) }), () => change(name, { cols: Math.min(128, e.cols + 1) })));
      panel.appendChild(stepper("Rows", e.rows, () => change(name, { rows: Math.max(1, e.rows - 1) }), () => change(name, { rows: Math.min(32, e.rows + 1) })));
      const feed = (data.feed && data.feed[name]) || {};
      const lab = document.createElement("div"); lab.className = "lbl"; lab.textContent = "Text (osd-feed template)"; panel.appendChild(lab);
      const ta = document.createElement("textarea"); ta.rows = 3; ta.value = feed.template || ""; ta.spellcheck = false; panel.appendChild(ta);
      const save = document.createElement("button"); save.className = "primary"; save.textContent = "Save text (restarts osd-feed)";
      save.addEventListener("click", async () => {
        setMsg("Saving text…");
        try { await api("/api/osd/feed", { slot: name, template: ta.value }); data.feed[name] = { template: ta.value }; setMsg("Saved."); }
        catch (err) { setMsg(err.message, true); }
      });
      panel.appendChild(save);
    }
    if (name.startsWith("imagefile")) {
      const step = 8;
      const resize = (dir) => {
        let w = Math.max(16, Math.min(data.stream.width, e.width + dir * step)); w += w & 1; // prudynt wants even sizes
        let h = Math.round(e.height * w / e.width); h += h & 1; h = Math.max(16, Math.min(data.stream.height, h));
        change(name, { width: w, height: h });
      };
      panel.appendChild(stepper("Size", `${e.width}×${e.height}`, () => resize(-1), () => resize(1)));
      const feed = (data.feed && data.feed[name]) || {};
      if (name === "imagefile") {
        const lab = document.createElement("div"); lab.className = "lbl"; lab.textContent = `Logo PNG (${data.logo_path || "logo.png"})`; panel.appendChild(lab);
        const inp = document.createElement("input"); inp.type = "file"; inp.accept = "image/png";
        inp.addEventListener("change", async () => {
          if (!inp.files[0]) return;
          setMsg("Uploading…");
          const fd = new FormData(); fd.append("file", inp.files[0]);
          try {
            const r = await fetch("/api/osd/logo", { method: "POST", body: fd });
            const j = await r.json();
            if (!r.ok) throw new Error(j.error || `HTTP ${r.status}`);
            setMsg(`Logo replaced (${j.width}×${j.height}). It shows within a few seconds.`);
          } catch (err) { setMsg(err.message, true); }
        });
        panel.appendChild(inp);
      }
      if (name === "imagefile2" || name === "imagefile3") {
        // what osd-feed draws here is fixed by the slot's role (G-ball /
        // track map); "Show on the video" above is the only switch
        const note = document.createElement("div"); note.className = "lbl";
        note.textContent = name === "imagefile2" ? "Drawn from the RaceBox G sensor. Shows a grid until data arrives." : "Drawn from the RaceBox GPS. Shows a dark box until data arrives.";
        panel.appendChild(note);
      }
    }
  }

  // --- open / close ---------------------------------------------------------
  async function load() {
    try {
      data = await api("/api/osd");
      if (data.feed_error) setMsg(data.feed_error, true);
      render();
    } catch (err) {
      $("osd-layer").innerHTML = "";
      setMsg(err.message, true);
    }
  }

  window.osdTab = {
    open() {
      open = true;
      $("osd-preview").src = `/api/preview.mjpeg?fps=4&t=${Date.now()}`;
      load();
    },
    close() {
      open = false;
      $("osd-preview").removeAttribute("src");
      drag = null;
    },
    isOpen() { return open; },
  };
})();
