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
})();
