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
