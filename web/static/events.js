// Drives the event log on both the global "Events" page and a single link's
// detail page: debounced search, a type filter, infinite scroll via
// IntersectionObserver against the paginated /admin/api/events endpoint, and
// single/bulk delete against /admin/api/events/delete.
(function () {
  const root = document.getElementById("eventsRoot");
  if (!root) return;

  const linkID = root.dataset.link || "";
  const showLinkCol = root.dataset.showLinkCol === "1";
  const csrfToken = root.dataset.csrf || "";
  const totalCols = (showLinkCol ? 8 : 7) + 2; // data columns + checkbox + delete

  const searchInput = document.getElementById("eventsSearch");
  const typeSelect = document.getElementById("eventsType");
  const tbody = document.getElementById("eventsBody");
  const status = document.getElementById("eventsStatus");
  const sentinel = document.getElementById("eventsSentinel");
  const selectAll = document.getElementById("eventsSelectAll");
  const deleteSelectedBtn = document.getElementById("eventsDeleteSelected");

  const trashIcon =
    '<svg viewBox="0 0 24 24" width="14" height="14" fill="currentColor" aria-hidden="true">' +
    '<path d="M9 3h6l1 2h4v2H4V5h4l1-2zm-3 6h12l-1 12a2 2 0 0 1-2 2H9a2 2 0 0 1-2-2L6 9zm3 2v8h2v-8H9zm4 0v8h2v-8h-2z"/>' +
    "</svg>";

  let offset = 0;
  let loading = false;
  let hasMore = true;
  let requestSeq = 0;
  let initialLoadDone = false;
  const eventsById = new Map();

  function esc(s) {
    const d = document.createElement("div");
    d.textContent = s == null ? "" : String(s);
    return d.innerHTML;
  }

  function typeBadge(t) {
    return '<span class="badge">' + esc(t) + "</span>";
  }

  function rowHTML(e) {
    const loc = [e.City, e.Region, e.Country].filter(Boolean).join(", ");
    const gps = e.GPSLat != null && e.GPSLon != null
      ? '<span class="pill pill-on">' + e.GPSLat.toFixed(5) + ", " + e.GPSLon.toFixed(5) + "</span>"
      : '<span class="muted">&mdash;</span>';
    const linkCell = showLinkCol
      ? "<td><a href=\"/admin/links/" + e.LinkID + "\">" + esc(e.LinkLabel || ("#" + e.LinkID)) + "</a></td>"
      : "";
    return (
      "<tr data-id=\"" + e.ID + "\" class=\"events-row-clickable\">" +
      "<td class=\"select-cell\"><input type=\"checkbox\" class=\"event-select\" value=\"" + e.ID + "\"></td>" +
      "<td class=\"event-delete-cell\"><button type=\"button\" class=\"btn btn-ghost btn-sm event-delete-btn\" data-id=\"" + e.ID + "\" title=\"Delete event\">" + trashIcon + "</button></td>" +
      "<td>" + esc(new Date(e.Timestamp).toLocaleString()) + "</td>" +
      linkCell +
      "<td>" + typeBadge(e.Type) + "</td>" +
      "<td class=\"mono\">" + esc(e.IP) + "</td>" +
      "<td>" + esc(loc) + "</td>" +
      "<td>" + esc(e.ISP) + "</td>" +
      "<td>" + esc(e.Device) + " / " + esc(e.OS) + " / " + esc(e.Browser) + "</td>" +
      "<td>" + gps + "</td>" +
      "</tr>"
    );
  }

  function buildURL() {
    const params = new URLSearchParams();
    if (linkID) params.set("link", linkID);
    if (searchInput.value.trim()) params.set("q", searchInput.value.trim());
    if (typeSelect.value) params.set("type", typeSelect.value);
    params.set("offset", String(offset));
    return "/admin/api/events?" + params.toString();
  }

  function reset() {
    offset = 0;
    hasMore = true;
    tbody.innerHTML = "";
    eventsById.clear();
    selectAll.checked = false;
    selectAll.indeterminate = false;
    updateDeleteButton();
    load();
  }

  function load() {
    if (loading || !hasMore) return;
    loading = true;
    status.textContent = "Loading…";
    const seq = ++requestSeq;

    fetch(buildURL())
      .then((r) => r.json())
      .then((data) => {
        if (seq !== requestSeq) return; // a newer search superseded this request
        const events = data.events || [];
        if (events.length === 0 && offset === 0) {
          tbody.innerHTML = "<tr><td colspan=\"" + totalCols + "\" class=\"muted\">No events match.</td></tr>";
        } else {
          events.forEach((e) => eventsById.set(e.ID, e));
          tbody.insertAdjacentHTML("beforeend", events.map(rowHTML).join(""));
        }
        offset += events.length;
        hasMore = !!data.has_more;
        status.textContent = hasMore ? "" : (offset === 0 ? "" : "End of results.");
        initialLoadDone = true;
      })
      .catch(() => {
        status.textContent = "Could not load events.";
      })
      .finally(() => {
        loading = false;
      });
  }

  // Auto-refresh: check for events newer than what's already loaded and
  // prepend just those (the API's default order is newest-first, so a fresh
  // offset=0 fetch always has any new rows at the top). Bumps `offset` by
  // however many rows were prepended, so a later scroll-triggered load()
  // still fetches the correct next page instead of re-fetching/duplicating
  // rows that shifted down.
  function refreshNew() {
    if (!initialLoadDone || loading) return;
    const params = new URLSearchParams();
    if (linkID) params.set("link", linkID);
    if (searchInput.value.trim()) params.set("q", searchInput.value.trim());
    if (typeSelect.value) params.set("type", typeSelect.value);
    params.set("offset", "0");

    fetch("/admin/api/events?" + params.toString())
      .then((r) => r.json())
      .then((data) => {
        const events = data.events || [];
        const newOnes = events.filter((e) => !eventsById.has(e.ID));
        if (newOnes.length === 0) return;
        // Replace the "No events match" placeholder if that's all there was.
        if (tbody.children.length === 1 && tbody.querySelector("td.muted")) {
          tbody.innerHTML = "";
        }
        newOnes.forEach((e) => eventsById.set(e.ID, e));
        tbody.insertAdjacentHTML("afterbegin", newOnes.map(rowHTML).join(""));
        offset += newOnes.length;
        document.dispatchEvent(new CustomEvent("netra:new-events", { detail: newOnes }));
      })
      .catch(() => {});
  }

  if (window.Netra && window.Netra.onAutoRefresh) {
    window.Netra.onAutoRefresh(refreshNew);
  }

  function selectedCheckboxes() {
    return Array.from(tbody.querySelectorAll(".event-select:checked"));
  }

  function updateDeleteButton() {
    const n = selectedCheckboxes().length;
    deleteSelectedBtn.disabled = n === 0;
    deleteSelectedBtn.textContent = n > 0 ? "Delete selected (" + n + ")" : "Delete selected";

    const all = tbody.querySelectorAll(".event-select");
    if (all.length === 0) {
      selectAll.checked = false;
      selectAll.indeterminate = false;
    } else {
      selectAll.checked = n === all.length;
      selectAll.indeterminate = n > 0 && n < all.length;
    }
  }

  function deleteEvents(ids) {
    const body = new URLSearchParams();
    body.set("csrf_token", csrfToken);
    ids.forEach((id) => body.append("ids", id));
    return fetch("/admin/api/events/delete", { method: "POST", body }).then((r) => {
      if (!r.ok) throw new Error("delete failed");
      return r.json();
    });
  }

  selectAll.addEventListener("change", () => {
    tbody.querySelectorAll(".event-select").forEach((cb) => (cb.checked = selectAll.checked));
    updateDeleteButton();
  });

  tbody.addEventListener("change", (e) => {
    if (e.target.classList.contains("event-select")) updateDeleteButton();
  });

  tbody.addEventListener("click", (e) => {
    const btn = e.target.closest(".event-delete-btn");
    if (!btn) return;
    if (!confirm("Delete this event? This cannot be undone.")) return;
    const id = btn.dataset.id;
    btn.disabled = true;
    deleteEvents([id])
      .then(() => {
        const row = tbody.querySelector('tr[data-id="' + id + '"]');
        if (row) row.remove();
        updateDeleteButton();
        if (!tbody.querySelector("tr")) reset();
      })
      .catch(() => {
        status.textContent = "Could not delete event.";
        btn.disabled = false;
      });
  });

  deleteSelectedBtn.addEventListener("click", () => {
    const ids = selectedCheckboxes().map((cb) => cb.value);
    if (ids.length === 0) return;
    if (!confirm("Delete " + ids.length + " selected event" + (ids.length > 1 ? "s" : "") + "? This cannot be undone.")) return;
    deleteSelectedBtn.disabled = true;
    deleteEvents(ids)
      .then(() => {
        ids.forEach((id) => {
          const row = tbody.querySelector('tr[data-id="' + id + '"]');
          if (row) row.remove();
        });
        updateDeleteButton();
        if (!tbody.querySelector("tr")) reset();
      })
      .catch(() => {
        status.textContent = "Could not delete selected events.";
        updateDeleteButton();
      });
  });

  // Event detail popup: click any row (outside its checkbox/delete button)
  // to see every field captured for that event, including the raw request
  // headers and JS-side fingerprint (timezone, screen, language, platform)
  // that don't otherwise fit in the table's summary columns.
  function buildEventModal() {
    const modal = document.createElement("div");
    modal.className = "event-modal";
    modal.id = "eventModal";
    modal.hidden = true;
    modal.innerHTML =
      '<div class="event-modal-card">' +
      '<button type="button" class="event-modal-close" aria-label="Close">&times;</button>' +
      '<h3 id="eventModalTitle"></h3>' +
      '<div id="eventModalBody"></div>' +
      "</div>";
    document.body.appendChild(modal);

    const close = () => { modal.hidden = true; };
    modal.querySelector(".event-modal-close").addEventListener("click", close);
    modal.addEventListener("click", (e) => { if (e.target === modal) close(); });
    document.addEventListener("keydown", (e) => {
      if (e.key === "Escape" && !modal.hidden) close();
    });
    return modal;
  }

  function fieldRow(label, value) {
    if (value === null || value === undefined || value === "") return "";
    return "<dt>" + esc(label) + "</dt><dd>" + esc(value) + "</dd>";
  }

  // Header keys captured server-side at event time (see internal/handlers/
  // public.go's capture()) — shown separately from the JS-side fingerprint,
  // which is merged into the same blob under "js_fingerprint" once a GPS or
  // Clone link's beacon reports back (see internal/db/queries.go).
  const RAW_HEADER_LABELS = {
    "X-Forwarded-For": "X-Forwarded-For",
    "X-Real-IP": "X-Real-IP",
    "DNT": "Do Not Track",
    "Sec-CH-UA": "Client Hints (UA)",
    "Sec-CH-UA-Mobile": "Client Hints (Mobile)",
    "Sec-CH-UA-Platform": "Client Hints (Platform)",
  };

  function renderEventModal(e) {
    const modal = window._eventModal || (window._eventModal = buildEventModal());
    modal.querySelector("#eventModalTitle").textContent =
      "Event #" + e.ID + " — " + e.Type;

    let headers = {};
    try { headers = JSON.parse(e.HeadersJSON || "{}"); } catch (err) { /* malformed/empty */ }
    const fp = headers.js_fingerprint || null;

    const loc = [e.City, e.Region, e.Country].filter(Boolean).join(", ");
    const coords = e.Lat || e.Lon ? e.Lat.toFixed(4) + ", " + e.Lon.toFixed(4) : "";

    let html = "";

    html += '<div class="event-modal-section"><h4>Overview</h4><dl class="event-modal-grid">' +
      fieldRow("Time", new Date(e.Timestamp).toLocaleString()) +
      (showLinkCol ? fieldRow("Link", (e.LinkLabel || e.LinkSlug || ("#" + e.LinkID))) : "") +
      fieldRow("Type", e.Type) +
      "</dl></div>";

    html += '<div class="event-modal-section"><h4>Network &amp; location</h4><dl class="event-modal-grid">' +
      fieldRow("IP address", e.IP) +
      fieldRow("Location", loc) +
      fieldRow("Approx. coordinates", coords) +
      fieldRow("ISP", e.ISP) +
      fieldRow("Org", e.Org) +
      fieldRow("ASN", e.ASN) +
      fieldRow("Referer", e.Referer) +
      "</dl></div>";

    html += '<div class="event-modal-section"><h4>Device</h4><dl class="event-modal-grid">' +
      fieldRow("Device", e.Device) +
      fieldRow("OS", e.OS) +
      fieldRow("Browser", e.Browser) +
      fieldRow("User-Agent", e.UserAgent) +
      fieldRow("Accept-Language", e.AcceptLanguage) +
      "</dl></div>";

    if (e.GPSLat != null && e.GPSLon != null) {
      html += '<div class="event-modal-section"><h4>GPS capture (precise)</h4><dl class="event-modal-grid">' +
        fieldRow("Latitude", e.GPSLat) +
        fieldRow("Longitude", e.GPSLon) +
        fieldRow("Accuracy", e.GPSAccuracy != null ? "±" + Math.round(e.GPSAccuracy) + "m" : "") +
        "</dl></div>";
    }

    if (fp) {
      html += '<div class="event-modal-section"><h4>Browser fingerprint (JS-side)</h4><dl class="event-modal-grid">' +
        fieldRow("Timezone", fp.tz) +
        fieldRow("Screen size", fp.screen) +
        fieldRow("Language", fp.lang) +
        fieldRow("Platform", fp.platform) +
        "</dl></div>";
    }

    const rawRows = Object.keys(RAW_HEADER_LABELS)
      .map((k) => fieldRow(RAW_HEADER_LABELS[k], headers[k]))
      .join("");
    if (rawRows) {
      html += '<div class="event-modal-section"><h4>Raw request headers</h4><dl class="event-modal-grid">' +
        rawRows + "</dl></div>";
    }

    modal.querySelector("#eventModalBody").innerHTML = html;
    modal.hidden = false;
  }

  tbody.addEventListener("click", (e) => {
    if (e.target.closest(".select-cell") || e.target.closest(".event-delete-cell") || e.target.closest("a")) return;
    const row = e.target.closest("tr[data-id]");
    if (!row) return;
    const ev = eventsById.get(Number(row.dataset.id));
    if (ev) renderEventModal(ev);
  });

  let debounceTimer;
  searchInput.addEventListener("input", () => {
    clearTimeout(debounceTimer);
    debounceTimer = setTimeout(reset, 300);
  });
  typeSelect.addEventListener("change", reset);

  const observer = new IntersectionObserver((entries) => {
    if (entries[0].isIntersecting) load();
  });
  observer.observe(sentinel);

  load();
})();
