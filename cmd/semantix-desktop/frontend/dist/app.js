(function () {
  "use strict";
  var open = document.getElementById("open");
  var status = document.getElementById("status");
  var projects = document.getElementById("projects");
  var recent = document.querySelector(".recent");

  function api() { return window.go && window.go.main && window.go.main.App; }
  function fail(err) { status.textContent = String(err && err.message || err || "无法打开项目"); open.disabled = false; }
  function launch(path) {
    open.disabled = true;
    status.textContent = "正在启动项目运行时…";
    var call = path ? api().OpenRecent(path) : api().OpenProject();
    Promise.resolve(call).then(function (opened) {
      if (!path && opened === false) {
        open.disabled = false;
        status.textContent = "";
      }
    }).catch(fail);
  }
  function render(items) {
    if (!Array.isArray(items) || !items.length) return;
    recent.hidden = false;
    items.forEach(function (item) {
      var row = document.createElement("button");
      row.className = "project";
      var name = document.createElement("span");
      name.textContent = item.path.split(/[\\/]/).filter(Boolean).pop() || item.path;
      var path = document.createElement("small");
      path.textContent = item.path;
      row.appendChild(name); row.appendChild(path);
      row.addEventListener("click", function () { launch(item.path); });
      projects.appendChild(row);
    });
  }
  open.addEventListener("click", function () { launch(""); });
  document.addEventListener("keydown", function (event) {
    if (event.ctrlKey && event.key.toLowerCase() === "o") { event.preventDefault(); launch(""); }
  });
  var wait = setInterval(function () {
    if (!api()) return;
    clearInterval(wait);
    api().RecentProjects().then(render).catch(function () {});
  }, 50);
})();
