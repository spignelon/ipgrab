// Renders captured event locations on a Leaflet map for a single link's
// detail page. `points` is embedded by the server-side template.
(function () {
  if (typeof L === "undefined" || typeof points === "undefined" || !points.length) return;

  const map = L.map("map");
  L.tileLayer("https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png", {
    attribution: "&copy; OpenStreetMap contributors",
    maxZoom: 19,
  }).addTo(map);

  const bounds = [];
  points.forEach((p) => {
    const marker = L.circleMarker([p.lat, p.lon], {
      radius: p.gps ? 8 : 6,
      color: p.gps ? "#16a34a" : "#2563eb",
      fillColor: p.gps ? "#16a34a" : "#2563eb",
      fillOpacity: 0.7,
    }).addTo(map);
    marker.bindPopup(p.label + (p.gps ? " (GPS)" : " (IP-based)"));
    bounds.push([p.lat, p.lon]);
  });

  if (bounds.length === 1) {
    map.setView(bounds[0], 11);
  } else {
    map.fitBounds(bounds, { padding: [30, 30] });
  }

  // Auto-refresh: events.js dispatches this whenever it prepends newly
  // captured rows, so the map picks up a fresh hit without a page reload.
  // Only wired up when the map already exists — a link with zero points at
  // page load doesn't render the map section at all (see link_detail.html),
  // so the very first point still needs one reload to appear.
  document.addEventListener("netra:new-events", (evt) => {
    (evt.detail || []).forEach((e) => {
      const gps = e.GPSLat != null && e.GPSLon != null;
      const lat = gps ? e.GPSLat : e.Lat;
      const lon = gps ? e.GPSLon : e.Lon;
      if (!lat && !lon) return;
      const marker = L.circleMarker([lat, lon], {
        radius: gps ? 8 : 6,
        color: gps ? "#16a34a" : "#2563eb",
        fillColor: gps ? "#16a34a" : "#2563eb",
        fillOpacity: 0.7,
      }).addTo(map);
      const label = e.IP + " — " + (e.City || "") + " (" + new Date(e.Timestamp).toLocaleString() + ")";
      marker.bindPopup(label + (gps ? " (GPS)" : " (IP-based)"));
    });
  });
})();
