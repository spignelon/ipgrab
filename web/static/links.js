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
