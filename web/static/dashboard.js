// Renders the dashboard Chart.js graphs from the `stats` object embedded by
// the server-side template (see dashboard.html).
(function () {
  if (typeof Chart === "undefined" || typeof stats === "undefined") return;

  const palette = ["#2563eb", "#7c3aed", "#0891b2", "#db2777", "#65a30d", "#ea580c", "#4338ca", "#0d9488"];

  function labels(buckets) { return (buckets || []).map((b) => b.label); }
  function counts(buckets) { return (buckets || []).map((b) => b.count); }

  const timeCtx = document.getElementById("chartTime");
  if (timeCtx) {
    new Chart(timeCtx, {
      type: "line",
      data: {
        labels: labels(stats.events_by_day),
        datasets: [{
          label: "Events",
          data: counts(stats.events_by_day),
          borderColor: palette[0],
          backgroundColor: "rgba(37,99,235,0.1)",
          tension: 0.3,
          fill: true,
        }],
      },
      options: { plugins: { legend: { display: false } }, scales: { y: { beginAtZero: true } } },
    });
  }

  const countryCtx = document.getElementById("chartCountry");
  if (countryCtx) {
    new Chart(countryCtx, {
      type: "bar",
      data: {
        labels: labels(stats.by_country),
        datasets: [{ label: "Events", data: counts(stats.by_country), backgroundColor: palette[1] }],
      },
      options: { plugins: { legend: { display: false } }, scales: { y: { beginAtZero: true } } },
    });
  }

  const deviceCtx = document.getElementById("chartDevice");
  if (deviceCtx) {
    new Chart(deviceCtx, {
      type: "doughnut",
      data: {
        labels: labels(stats.by_device),
        datasets: [{ data: counts(stats.by_device), backgroundColor: palette }],
      },
    });
  }

  const browserCtx = document.getElementById("chartBrowser");
  if (browserCtx) {
    new Chart(browserCtx, {
      type: "doughnut",
      data: {
        labels: labels(stats.by_browser),
        datasets: [{ data: counts(stats.by_browser), backgroundColor: palette }],
      },
    });
  }
})();
