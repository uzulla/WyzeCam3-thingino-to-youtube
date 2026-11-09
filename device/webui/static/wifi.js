// Wi-Fi tab: the connection now, the saved networks (order = priority), a
// scan to add one. Save rewrites wpa_supplicant.conf and reloads it.
(() => {
  "use strict";
  const $ = (id) => document.getElementById(id);
  const api = (...a) => window.webui.api(...a);
  let data = null;   // {current, networks, sd_copy}
  let edit = null;   // networks being edited [{ssid, priority, open, has_psk, psk?}]
  let scan = null;   // [{ssid, signal_dbm, secure}]
  let confirmSave = null;
  let saving = false;

  function setMsg(text, isError) { const el = $("wifi-msg"); el.textContent = text || ""; el.className = "msg" + (isError ? " error" : ""); }

  async function load() {
    try {
      data = await api("/api/wifi");
      edit = data.networks.map((n) => Object.assign({}, n));
      render();
    } catch (err) { setMsg(err.message, true); }
  }

  function render() {
    const c = data.current || {};
    $("wifi-current").textContent = c.ssid ? `${c.ssid} · ${c.rssi_dbm != null ? c.rssi_dbm + " dBm · " : ""}${c.ip || "no IP yet"}` : `not connected (${c.state || "unknown"})`;
    const list = $("wifi-list");
    list.innerHTML = "";
    // highest priority first; equal priority keeps the file's order
    const order = edit.map((n, i) => ({ n, i })).sort((a, b) => (b.n.priority - a.n.priority) || (a.i - b.i));
    order.forEach(({ n, i }, pos) => {
      const row = document.createElement("div"); row.className = "wifi-row";
      const name = document.createElement("span"); name.className = "rec-name";
      name.textContent = n.ssid + (n.ssid === c.ssid ? "  ✓ connected" : "") + (n.open ? "  (open)" : "");
      const up = document.createElement("button"); up.textContent = "▲"; up.title = "Try this one first"; up.disabled = pos === 0;
      up.addEventListener("click", () => {
        // give it a priority above the one currently first
        const top = order[0].n;
        n.priority = (top.priority || 0) + 1;
        render();
      });
      const del = document.createElement("button"); del.className = "danger"; del.textContent = "Remove";
      del.addEventListener("click", () => { edit.splice(i, 1); render(); });
      row.append(name, up, del);
      list.appendChild(row);
    });
    if (!edit.length) { const p = document.createElement("p"); p.className = "help"; p.textContent = "No networks. Add one below before saving."; list.appendChild(p); }

    const changed = JSON.stringify(edit) !== JSON.stringify(data.networks);
    const removesCurrent = c.ssid && data.networks.some((n) => n.ssid === c.ssid) && !edit.some((n) => n.ssid === c.ssid);
    const save = $("wifi-save");
    save.disabled = saving || !changed;
    save.textContent = saving ? "Saving…" : confirmSave ? (removesCurrent ? "Tap again: this removes the network you are on" : "Tap again to save") : (changed ? "Save and apply" : "No changes");
    save.className = "primary" + (confirmSave && !saving ? " confirm" : "");
    $("wifi-sdnote").textContent = data.sd_copy ? "The SD card's wpa_supplicant.conf is updated too." : "";

    const sl = $("wifi-scan-list");
    sl.innerHTML = "";
    (scan || []).forEach((s) => {
      const b = document.createElement("button");
      b.className = "chipbtn";
      b.textContent = `${s.ssid}  ${s.signal_dbm} dBm${s.secure ? "" : " (open)"}`;
      b.addEventListener("click", () => { $("wifi-ssid").value = s.ssid; $("wifi-open").checked = !s.secure; $("wifi-psk").value = ""; $("wifi-psk").disabled = !s.secure; $("wifi-psk").focus(); });
      sl.appendChild(b);
    });
  }

  $("wifi-scan").addEventListener("click", async () => {
    const b = $("wifi-scan"); b.disabled = true; b.textContent = "Scanning…";
    try { scan = (await api("/api/wifi/scan")).networks; setMsg(scan.length ? "" : "Nothing found. Try again."); }
    catch (err) { setMsg(err.message, true); }
    b.disabled = false; b.textContent = "Scan";
    render();
  });
  $("wifi-open").addEventListener("change", () => { $("wifi-psk").disabled = $("wifi-open").checked; });

  $("wifi-add").addEventListener("click", () => {
    const ssid = $("wifi-ssid").value.trim(), psk = $("wifi-psk").value, open = $("wifi-open").checked;
    if (!ssid) { setMsg("Type or pick a network name.", true); return; }
    if (!open && psk.length < 8) { setMsg("The password needs 8 to 63 characters.", true); return; }
    const existing = edit.find((n) => n.ssid === ssid);
    if (existing) { existing.open = open; if (!open) existing.psk = psk; existing.has_psk = !open; setMsg(`Updated ${ssid}.`); }
    else { edit.push({ ssid, priority: 0, open, has_psk: !open, psk: open ? undefined : psk }); setMsg(`Added ${ssid}. Save to apply.`); }
    $("wifi-ssid").value = ""; $("wifi-psk").value = ""; $("wifi-open").checked = false; $("wifi-psk").disabled = false;
    render();
  });

  $("wifi-save").addEventListener("click", async () => {
    if (!confirmSave) {
      confirmSave = setTimeout(() => { confirmSave = null; render(); }, 3000);
      render();
      return;
    }
    clearTimeout(confirmSave); confirmSave = null;
    saving = true;
    setMsg("Saving…");
    render();
    try {
      await api("/api/wifi/networks", { networks: edit.map((n) => ({ ssid: n.ssid, priority: n.priority, open: !!n.open, psk: n.psk || undefined })) });
      saving = false;
      setMsg("Saved. The camera reloaded its network list.");
      await load();
    } catch (err) { saving = false; setMsg(err.message, true); render(); }
  });

  window.wifiTab = { open() { scan = null; load(); }, close() {} };
})();
