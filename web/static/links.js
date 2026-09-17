// Select-all + count-aware "Delete selected" button for the links table
// (mirrors the same pattern on the event log — see events.js).
(function () {
  const selectAll = document.getElementById("linksSelectAll");
  const deleteBtn = document.getElementById("linksDeleteSelected");
  if (!selectAll || !deleteBtn) return;

  const checkboxes = () => Array.from(document.querySelectorAll(".link-select"));

  function update() {
    const all = checkboxes();
    const checked = all.filter((cb) => cb.checked);
    deleteBtn.disabled = checked.length === 0;
    deleteBtn.textContent = checked.length > 0 ? "Delete selected (" + checked.length + ")" : "Delete selected";
    selectAll.checked = all.length > 0 && checked.length === all.length;
    selectAll.indeterminate = checked.length > 0 && checked.length < all.length;
  }

  selectAll.addEventListener("change", () => {
    checkboxes().forEach((cb) => (cb.checked = selectAll.checked));
    update();
  });
  checkboxes().forEach((cb) => cb.addEventListener("change", update));
  update();
})();

// Shows/hides the type-specific fields on the "create link" form based on the
// selected link type.
(function () {
  const select = document.getElementById("typeSelect");
  const groups = document.querySelectorAll("[data-show-for]");
  if (!select) return;

  function update() {
    const type = select.value;
    groups.forEach((g) => {
      const shown = g.dataset.showFor.split(" ").includes(type);
      g.style.display = shown ? "" : "none";
      g.querySelectorAll("input, select").forEach((el) => (el.disabled = !shown));
    });
  }

  select.addEventListener("change", update);
  update();
})();

// QR code popup: click the QR icon beside a link to show a scannable code
// for its share URL.
(function () {
  const modal = document.getElementById("qrModal");
  if (!modal) return;
  const img = document.getElementById("qrModalImg");
  const title = document.getElementById("qrModalTitle");
  const closeBtn = modal.querySelector(".qr-modal-close");

  document.querySelectorAll(".qr-btn").forEach((btn) => {
    btn.addEventListener("click", () => {
      img.src = btn.dataset.qrUrl;
      title.textContent = "QR code — " + btn.dataset.qrName;
      modal.hidden = false;
    });
  });

  function close() {
    modal.hidden = true;
    img.src = "";
  }
  closeBtn.addEventListener("click", close);
  modal.addEventListener("click", (e) => {
    if (e.target === modal) close();
  });
  document.addEventListener("keydown", (e) => {
    if (e.key === "Escape" && !modal.hidden) close();
  });
})();
