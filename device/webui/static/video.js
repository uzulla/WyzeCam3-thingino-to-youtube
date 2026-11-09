// Smooth live preview: prudynt's fragmented MP4 (/api/preview.mp4, the main
// stream as it goes to YouTube) fed to a <video> through MediaSource. Works
// in Chrome/Android and desktop browsers; Safari on iPhone has only
// ManagedMediaSource (iOS 17.1+), tried too. When anything fails the caller
// falls back to the MJPEG pictures. Based on Thingino's preview-fmp4.js.
(() => {
  "use strict";
  const LIVE_LAG = 3;      // seconds behind the newest data before we jump forward
  const KEEP_BEHIND = 20;  // seconds of played video kept in the buffer
  const MAX_PENDING = 4 << 20; // bytes waiting for a complete box: more means a broken stream

  let session = 0;
  let abort = null;
  let video = null;
  let ms = null;
  let sb = null;
  let watchdog = null;
  let onElError = null; // the <video> error handler of the current session

  const MS = window.ManagedMediaSource || window.MediaSource;

  function supported() {
    return !!MS && typeof MS.isTypeSupported === "function" && MS.isTypeSupported('video/mp4; codecs="avc1.4d401f, mp4a.40.2"');
  }

  function boxAt(u8, off) {
    if (off + 8 > u8.length) return null;
    const size = ((u8[off] << 24) | (u8[off + 1] << 16) | (u8[off + 2] << 8) | u8[off + 3]) >>> 0;
    if (size < 8 || off + size > u8.length) return null;
    return { type: String.fromCharCode(u8[off + 4], u8[off + 5], u8[off + 6], u8[off + 7]), size };
  }
  function concat(a, b) { const out = new Uint8Array(a.length + b.length); out.set(a, 0); out.set(b, a.length); return out; }
  const hex = (n) => n.toString(16).padStart(2, "0");

  // codecs from the init segment: avcC gives profile/compat/level, mp4a means AAC
  function codecsOf(u8) {
    let v = "avc1.4d401f", audio = false;
    for (let i = 0; i + 8 < u8.length; i++) {
      if (u8[i] === 0x61 && u8[i + 1] === 0x76 && u8[i + 2] === 0x63 && u8[i + 3] === 0x43) v = `avc1.${hex(u8[i + 5])}${hex(u8[i + 6])}${hex(u8[i + 7])}`;
      else if (u8[i] === 0x6d && u8[i + 1] === 0x70 && u8[i + 2] === 0x34 && u8[i + 3] === 0x61) audio = true;
    }
    return audio ? `video/mp4; codecs="${v}, mp4a.40.2"` : `video/mp4; codecs="${v}"`;
  }

  // append - one segment into the source buffer. Resolves true when it went
  // in, false when the source is gone or the append failed (the caller then
  // stops instead of reading and dropping for ever). Listeners and the timer
  // are removed either way: ten appends a second must not pile them up.
  function append(buf, my) {
    return new Promise((resolve) => {
      let settled = false;
      const theSb = sb;
      let timer = null;
      const finish = (ok) => {
        if (settled) return;
        settled = true;
        clearTimeout(timer);
        if (theSb) { theSb.removeEventListener("updateend", onEnd); theSb.removeEventListener("error", onErr); }
        resolve(ok);
      };
      const onEnd = () => finish(true);
      const onErr = () => finish(false);
      const go = () => {
        if (my !== session || !sb || !ms || ms.readyState !== "open") return finish(false);
        if (sb.updating) { sb.addEventListener("updateend", go, { once: true }); return; }
        sb.addEventListener("updateend", onEnd);
        sb.addEventListener("error", onErr);
        timer = setTimeout(() => finish(false), 5000);
        try { sb.appendBuffer(buf); } catch (e) {
          if (e && e.name === "QuotaExceededError" && !sb.updating && video && video.buffered.length) {
            // the buffer is full: drop what was played, then try this one again
            try { sb.remove(video.buffered.start(0), Math.max(video.buffered.start(0), video.currentTime - 2)); } catch (_) {}
            sb.addEventListener("updateend", () => { try { sb.appendBuffer(buf); } catch (_) { finish(false); } }, { once: true });
            return;
          }
          finish(false);
        }
      };
      go();
    });
  }

  // stay close to the live edge and keep the buffer small
  function tick(my) {
    if (my !== session || !video || !sb) return;
    try {
      const b = video.buffered;
      if (b.length) {
        const end = b.end(b.length - 1);
        if (video.currentTime < b.start(0)) video.currentTime = b.start(0); // the stream's clock does not start at 0
        if (end - video.currentTime > LIVE_LAG) video.currentTime = end - 0.5;
        const start = b.start(0);
        if (!sb.updating && video.currentTime - start > KEEP_BEHIND) sb.remove(start, video.currentTime - KEEP_BEHIND / 2);
      }
      if (video.paused) video.play().catch(() => {});
    } catch (_) { /* a closed source buffer: the stop() already ran */ }
  }

  async function run(url, my, onFail) {
    abort = new AbortController();
    let resp;
    try {
      resp = await fetch(url, { signal: abort.signal, cache: "no-store" });
    } catch (_) { return onFail("no connection"); }
    if (my !== session) return;
    if (!resp.ok || !resp.body) return onFail(`HTTP ${resp.status}`);
    if (!(resp.headers.get("Content-Type") || "").startsWith("video/mp4")) return onFail("not a video stream");
    const reader = resp.body.getReader();
    let buf = new Uint8Array(0);
    let init = false;
    try {
      for (;;) {
        const { done, value } = await reader.read();
        if (my !== session) return;
        if (done) return onFail("stream ended");
        buf = concat(buf, value);
        if (buf.length > MAX_PENDING) return onFail("bad stream (no complete box in 4 MB)");
        let off = 0;
        if (!init) {
          // wait for ftyp + moov, then open the source buffer with the right codecs
          let ftypEnd = -1, moovEnd = -1;
          for (;;) {
            const box = boxAt(buf, off);
            if (!box) break;
            if (box.type === "ftyp") ftypEnd = off + box.size;
            else if (box.type === "moov" && off === ftypEnd) moovEnd = off + box.size;
            off += box.size;
          }
          if (moovEnd < 0) continue;
          const codecs = codecsOf(buf.subarray(0, moovEnd));
          try {
            sb = ms.addSourceBuffer(codecs);
            sb.mode = "segments";
          } catch (_) { return onFail("codec not supported: " + codecs); }
          const ok = await append(buf.subarray(0, moovEnd).slice(), my);
          if (my !== session) return;
          if (!ok) return onFail("could not start playback");
          buf = buf.subarray(moovEnd);
          init = true;
          off = 0;
          watchdog = setInterval(() => tick(my), 1000);
        }
        for (;;) {
          const box = boxAt(buf, off);
          if (!box) break;
          if (box.type === "moof") {
            const mdat = boxAt(buf, off + box.size);
            if (!mdat || mdat.type !== "mdat") break; // the pair is not complete yet
            const end = off + box.size + mdat.size;
            const ok = await append(buf.subarray(off, end).slice(), my);
            if (my !== session) return;
            if (!ok) return onFail("playback stopped (buffer error)");
            off = end;
          } else {
            off += box.size;
          }
        }
        buf = buf.subarray(off);
      }
    } catch (_) {
      if (my === session) onFail("stream stopped");
    }
  }

  function stop() {
    session++;
    clearInterval(watchdog); watchdog = null;
    if (abort) { abort.abort(); abort = null; }
    if (ms && ms.readyState === "open") { try { ms.endOfStream(); } catch (_) {} }
    sb = null; ms = null;
    if (video) {
      if (onElError) video.removeEventListener("error", onElError);
      onElError = null;
      try { if (video.src) URL.revokeObjectURL(video.src); } catch (_) {}
      video.removeAttribute("src");
      video.load();
      video = null;
    }
  }

  // start - play url in el; onFail(reason) is called once when it cannot
  function start(el, url, onFail) {
    stop();
    if (!supported()) { onFail("no MediaSource"); return; }
    const my = session;
    let failed = false;
    const fail = (why) => { if (failed || my !== session) return; failed = true; stop(); onFail(why); };
    video = el;
    ms = new MS();
    if (window.ManagedMediaSource && ms instanceof window.ManagedMediaSource) el.disableRemotePlayback = true; // iOS wants this
    el.src = URL.createObjectURL(ms);
    ms.addEventListener("sourceopen", () => { if (my === session) run(url, my, fail); }, { once: true });
    onElError = () => fail("video element error");
    el.addEventListener("error", onElError);
    el.play().catch(() => {}); // autoplay: the element is muted
  }

  window.livePlayer = { start, stop, supported };
})();
