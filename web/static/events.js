// Drives the event log on both the global "Events" page and a single link's
// detail page: debounced search, a type filter, and infinite scroll via
// IntersectionObserver against the paginated /admin/api/events endpoint.
(function () {
  const root = document.getElementById("eventsRoot");
  if (!root) return;

  const linkID = root.dataset.link || "";
  const showLinkCol = root.dataset.showLinkCol === "1";

  const searchInput = document.getElementById("eventsSearch");
  const typeSelect = document.getElementById("eventsType");
  const tbody = document.getElementById("eventsBody");
  const status = document.getElementById("eventsStatus");
  const sentinel = document.getElementById("eventsSentinel");

  let offset = 0;
  let loading = false;
  let hasMore = true;
  let requestSeq = 0;

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
      "<tr>" +
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
          tbody.innerHTML = "<tr><td colspan=\"8\" class=\"muted\">No events match.</td></tr>";
        } else {
          tbody.insertAdjacentHTML("beforeend", events.map(rowHTML).join(""));
        }
        offset += events.length;
        hasMore = !!data.has_more;
        status.textContent = hasMore ? "" : (offset === 0 ? "" : "End of results.");
      })
      .catch(() => {
        status.textContent = "Could not load events.";
      })
      .finally(() => {
        loading = false;
      });
  }

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
