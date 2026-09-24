// Workspace shell interactions.
// - #404: context-panel collapse and the narrow-viewport side drawer.
// - #405: topbar project/branch/model/effort chips hydrated from live backend
//   state; model & effort switch through the same `/model` `/effort` submit
//   commands the TUI uses — no new CLI semantics. Every load re-reads server
//   state, which is what makes the workspace survive refresh.
(function () {
  "use strict";
  var nativeFetch = window.fetch.bind(window);

  // ── #404 chrome: collapse + side drawer ──
  var collapseBtn = document.querySelector("[data-ws-collapse]");
  if (collapseBtn) {
    var contextNarrow = window.matchMedia("(max-width: 1440px)");
    if (contextNarrow.matches) {
      document.body.classList.add("ws-right-collapsed");
      collapseBtn.setAttribute("aria-expanded", "false");
    }
    collapseBtn.addEventListener("click", function () {
      var opening = document.body.classList.contains("ws-right-collapsed") || (contextNarrow.matches && !document.body.classList.contains("ws-right-pinned"));
      document.body.classList.toggle("ws-right-collapsed", !opening);
      document.body.classList.toggle("ws-right-pinned", opening && contextNarrow.matches);
      collapseBtn.setAttribute("aria-expanded", String(opening));
    });
  }

  // ── auth: mirrors the fragment-token bootstrap in serve/index.html ──
  function bootstrapFragmentToken() {
    var fragment = new URLSearchParams(window.location.hash.slice(1));
    var token = fragment.get("token");
    if (!token) return Promise.resolve();
    fragment.delete("token");
    var cleanHash = fragment.toString();
    window.history.replaceState(null, "", window.location.pathname + window.location.search + (cleanHash ? "#" + cleanHash : ""));
    return nativeFetch("/auth/token", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ token: token })
    }).then(function (r) {
      if (!r.ok) throw new Error("auth failed (HTTP " + r.status + ")");
    });
  }
  var authReady = bootstrapFragmentToken();

  function getJSON(url) {
    return authReady.then(function () {
      return nativeFetch(url, { headers: { accept: "application/json" } });
    }).then(function (r) {
      if (!r.ok) throw new Error(url + " -> HTTP " + r.status);
      return r.json();
    });
  }

  function postCommand(input) {
    return authReady.then(function () {
      return nativeFetch("/submit", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ input: input })
      });
    }).then(function (r) {
      if (r.status === 204) return;
      return r.text().then(function (text) {
        throw new Error(text || "HTTP " + r.status);
      });
    });
  }

  function postJSON(url, body) {
    return authReady.then(function () {
      return nativeFetch(url, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify(body || {})
      });
    }).then(function (r) {
      if (!r.ok) {
        return r.text().then(function (text) {
          throw new Error(text || "HTTP " + r.status);
        });
      }
    });
  }

  function postJSONResult(url, body) {
    return authReady.then(function () {
      return nativeFetch(url, {
        method: "POST",
        headers: { "content-type": "application/json", accept: "application/json" },
        body: JSON.stringify(body || {})
      });
    }).then(function (r) {
      return r.text().then(function (value) {
        var payload = {};
        try { payload = value ? JSON.parse(value) : {}; } catch (_) { payload = { message: value }; }
        if (!r.ok) throw new Error(payload.message || value || "HTTP " + r.status);
        return payload;
      });
    });
  }

  // ── #405 selector elements ──
  var el = {
    project: document.querySelector("[data-ws-project]"),
    projectName: document.querySelector("[data-ws-project-name]"),
    branch: document.querySelector("[data-ws-branch]"),
    branchName: document.querySelector("[data-ws-branch-name]"),
    branchMenu: document.querySelector("[data-ws-branch-menu]"),
    model: document.querySelector("[data-ws-model]"),
    modelName: document.querySelector("[data-ws-model-name]"),
    modelMenu: document.querySelector("[data-ws-model-menu]"),
    effort: document.querySelector("[data-ws-effort]"),
    effortValue: document.querySelector("[data-ws-effort-value]"),
    effortMenu: document.querySelector("[data-ws-effort-menu]"),
    notice: document.querySelector("[data-ws-notice]"),
    taskList: document.querySelector("[data-ws-task-list]"),
    newTask: document.querySelector("[data-ws-new-task]"),
    sideProjectName: document.querySelector("[data-ws-side-project-name]"),
    timeline: document.querySelector("[data-ws-timeline]"),
    sessionSearch: document.querySelector("[data-ws-session-search]"),
    sessionProject: document.querySelector("[data-ws-session-project]"),
    sessionStatus: document.querySelector("[data-ws-session-status]"),
    empty: document.querySelector("[data-ws-empty]"),
    emptyTitle: document.querySelector("[data-ws-empty-title]"),
    emptyCopy: document.querySelector("[data-ws-empty-copy]"),
    historyRetry: document.querySelector("[data-ws-history-retry]"),
    contextDiff: document.querySelector("[data-ws-context-diff], .ws-diff"),
    tabs: document.querySelectorAll("[data-ws-tab]"),
    panels: document.querySelectorAll("[data-ws-panel]"),
    diffList: document.querySelector("[data-ws-diff-list]"),
    terminalList: document.querySelector("[data-ws-terminal-list]"),
    reviewList: document.querySelector("[data-ws-review-list]"),
    composer: document.querySelector("[data-ws-composer]"),
    input: document.querySelector("[data-ws-input]"),
    send: document.querySelector("[data-ws-send]"),
    cancel: document.querySelector("[data-ws-cancel]"),
    permission: document.querySelector("[data-ws-permission]"),
    permissionLabel: document.querySelector("[data-ws-permission-label]"),
    cacheStatus: document.querySelector("[data-ws-cache-status]"),
    cacheStatusText: document.querySelector("[data-ws-cache-status-text]"),
    setup: document.querySelector("[data-ws-setup]"),
    setupProvider: document.querySelector("[data-ws-setup-provider]"),
    setupDescription: document.querySelector("[data-ws-setup-description]"),
    setupModel: document.querySelector("[data-ws-setup-model]"),
    setupKey: document.querySelector("[data-ws-setup-key]"),
    setupStatus: document.querySelector("[data-ws-setup-status]"),
    setupSubmit: document.querySelector("[data-ws-setup-submit]")
  };

  // Sidebar/project shared state (GUI-3): whether the CURRENT session is
  // running drives the 运行中 pill for the highlighted task row.
  var sessionRunning = false;
  var composerBusy = false;
  var runningSyncSeq = 0;
  var EFFORT_LABELS = { low: "低", medium: "中", high: "高", max: "max" };
  var EFFORT_LEVELS = ["low", "medium", "high"];
  var TASK_PILLS = {
    running: ["运行中", "ws-state-running"],
    done: ["完成", "ws-state-done"],
    // /sessions reports a branch whose last turn was interrupted as
    // status:"recovered" (serve.go); the sidebar must render it, not crash.
    recovered: ["待恢复", "ws-state-recovered"],
    empty: ["空会话", "ws-state-empty"]
  };
  var openMenu = null; // currently open dropdown element or null

  // GUI-9 (#412): cache status is a projection of observed events only. A
  // null value means the corresponding layer has not produced telemetry yet;
  // it must never be replaced with a demo number.
  var cacheView = {
    l1Hit: null,
    l1Miss: null,
    l2Hits: null,
    l3Observed: null,
    reason: ""
  };

  function renderCacheBar() {
    if (!el.cacheStatusText) return;
    var observed = cacheView.l1Hit !== null || cacheView.l2Hits !== null || cacheView.l3Observed !== null;
    if (el.cacheStatus) el.cacheStatus.hidden = !observed;
    if (!observed) return;
    var parts = [];
    if (cacheView.l1Hit !== null) parts.push("L1 prefix：命中 " + cacheView.l1Hit + " · 未命中 " + cacheView.l1Miss);
    if (cacheView.l2Hits !== null) parts.push("L2 语义切片：复用 " + cacheView.l2Hits + " slices");
    if (cacheView.l3Observed !== null) parts.push("L3 安全复用：" + (cacheView.l3Observed ? "已观测" : "未命中"));
    if (cacheView.reason) parts.push("原因：" + cleanVisibleText(cacheView.reason));
    el.cacheStatusText.textContent = parts.join("  ·  ");
    if (el.cacheStatus) {
      var dot = el.cacheStatus.querySelector(".ws-dot");
      if (dot) dot.classList.toggle("is-on", observed);
    }
  }

  function updateCacheView(data) {
    if (!data || typeof data !== "object") return;
    var usage = data.usage || {};
    if (data.kind === "usage" || data.usage) {
      if (Number.isFinite(Number(usage.cacheHitTokens)) || Number.isFinite(Number(usage.cacheMissTokens))) {
        cacheView.l1Hit = Number(usage.cacheHitTokens || 0);
        cacheView.l1Miss = Number(usage.cacheMissTokens || 0);
      }
      var diagnostics = usage.cacheDiagnostics || {};
      if (Array.isArray(diagnostics.prefixChangeReasons) && diagnostics.prefixChangeReasons.length) {
        cacheView.reason = diagnostics.prefixChangeReasons.join(", ");
      }
    }
    var kernel = data.kernelCache || {};
    if (kernel.layer) {
      if (kernel.layer === "L2") {
        if (kernel.op === "hit" || kernel.op === "inject") cacheView.l2Hits = Array.isArray(kernel.sliceIds) ? kernel.sliceIds.length : 0;
        if (kernel.op === "miss" || kernel.op === "degraded") cacheView.l2Hits = 0;
      }
      if (kernel.layer === "L3") cacheView.l3Observed = kernel.op === "hit";
      if (kernel.reason) cacheView.reason = String(kernel.reason);
    }
    if (data.code === "semantix_reuse" && data.detail) {
      try {
        var reuse = JSON.parse(data.detail);
        if (Number.isFinite(Number(reuse.hits))) cacheView.l2Hits = Number(reuse.hits);
      } catch (_) { /* malformed optional detail is ignored */ }
    }
    renderCacheBar();
  }

  renderCacheBar();

  // GUI-4 (#407) + GUI-5 (#408): consume the versioned workspace stream as a
  var sessionRows = [];
  var SESSION_STATUS_LABELS = { running: "运行中", done: "已完成", recovered: "待恢复", empty: "空会话" };
  // transport contract and project it into the live workflow timeline.
  // Unknown event names and malformed payloads are deliberately ignored so a
  // newer server cannot crash an older workspace page.
  var workspaceEvents = null;
  var lastEventSeq = 0;
  var eventTaskID = "";
  var historyHydrating = false;
  var bufferedWorkspaceEvents = [];
  var historyRecoveryPending = false;
  var canonicalEventTypes = {
    user_message: true,
    assistant_message: true,
    plan: true,
    tool_start: true,
    tool_result: true,
    diff: true,
    permission_request: true,
    task_status: true,
    cache_status: true,
    error: true,
    unknown: true
  };

  // GUI-5 (#408): the timeline is a projection of the versioned stream. The
  // stream sequence is the only ordering authority; tool IDs are the only
  // merge key. This keeps deltas idempotent without creating a second history
  // store or guessing at events the server did not publish.
  var MAX_INLINE_CHARS = 1200;
  var MAX_RENDER_CHARS = 200000;
  var workflow = {
    assistant: null,
    plan: null,
    tools: Object.create(null),
    diffs: Object.create(null),
    approvals: Object.create(null),
    asks: Object.create(null),
    localUser: null,
    active: false
  };

  function clearNode(node) {
    while (node && node.firstChild) node.removeChild(node.firstChild);
  }

  function activateWorkflow() {
    if (workflow.active) return;
    workflow.active = true;
    if (el.empty) el.empty.hidden = true;
  }

  function resetWorkflow() {
    workflow.assistant = null;
    workflow.plan = null;
    workflow.tools = Object.create(null);
    workflow.diffs = Object.create(null);
    workflow.approvals = Object.create(null);
    workflow.asks = Object.create(null);
    workflow.localUser = null;
    workflow.active = false;
    cacheView = { l1Hit: null, l1Miss: null, l2Hits: null, l3Observed: null, reason: "" };
    renderCacheBar();
    clearNode(el.timeline);
    if (el.empty) el.empty.hidden = false;
    if (el.emptyTitle) el.emptyTitle.textContent = "开始一个真实任务";
    if (el.emptyCopy) el.emptyCopy.textContent = "描述你希望在当前项目中完成的工作。Semantix 会保留会话、展示工具执行，并在修改前遵循权限策略。";
    if (el.historyRetry) el.historyRetry.hidden = true;
    renderDiffList();
    renderTerminalList();
    renderReviewList();
  }

  function activateContextTab(name) {
    var active = String(name || "files");
    if (!el.tabs || !el.tabs.length) return;
    el.tabs.forEach(function (tab) {
      var selected = tab.getAttribute("data-ws-tab") === active;
      tab.classList.toggle("is-active", selected);
      tab.setAttribute("aria-selected", String(selected));
      tab.setAttribute("tabindex", selected ? "0" : "-1");
    });
    if (el.panels) el.panels.forEach(function (panel) {
      var visible = panel.getAttribute("data-ws-panel") === active;
      panel.classList.toggle("is-active", visible);
      panel.hidden = !visible;
    });
  }

  function initContextTabs() {
    if (!el.tabs || !el.tabs.length) return;
    el.tabs.forEach(function (tab, index) {
      tab.addEventListener("click", function () { activateContextTab(tab.getAttribute("data-ws-tab")); });
      tab.addEventListener("keydown", function (event) {
        var next = index;
        if (event.key === "ArrowRight" || event.key === "ArrowDown") next = (index + 1) % el.tabs.length;
        else if (event.key === "ArrowLeft" || event.key === "ArrowUp") next = (index + el.tabs.length - 1) % el.tabs.length;
        else if (event.key === "Home") next = 0;
        else if (event.key === "End") next = el.tabs.length - 1;
        else return;
        event.preventDefault();
        el.tabs[next].focus();
        activateContextTab(el.tabs[next].getAttribute("data-ws-tab"));
      });
    });
    activateContextTab("files");
  }

  function makeEvent(kind, label, icon) {
    if (!el.timeline) return null;
    // Status, cache, and provider-error frames can arrive during reconnects
    // before a turn has produced user-visible work. Keep the useful preview
    // in place until a real workflow card arrives.
    if (["status", "error", "cache"].indexOf(kind) === -1) activateWorkflow();
    var article = document.createElement("article");
    article.className = "ws-event ws-event--" + kind;
    article.setAttribute("data-ws-event-kind", kind);
    var head = document.createElement("header");
    head.className = "ws-event__head";
    var iconEl = document.createElement("span");
    iconEl.className = "ws-event__icon";
    iconEl.setAttribute("aria-hidden", "true");
    iconEl.textContent = icon || "•";
    var labelEl = document.createElement("strong");
    labelEl.className = "ws-event__label";
    labelEl.textContent = label || "事件";
    var statusEl = document.createElement("span");
    statusEl.className = "ws-event__status";
    var body = document.createElement("div");
    body.className = "ws-event__body";
    head.appendChild(iconEl);
    head.appendChild(labelEl);
    head.appendChild(statusEl);
    article.appendChild(head);
    article.appendChild(body);
    el.timeline.appendChild(article);
    return { article: article, head: head, label: labelEl, status: statusEl, body: body };
  }

  function setStatus(card, text, state) {
    if (!card || !card.status) return;
    card.status.textContent = text || "";
    card.status.className = "ws-event__status" + (state ? " is-" + state : "");
    if (state === "failed") card.article.classList.add("ws-event--error");
  }

  function cleanVisibleText(value) {
    var text = String(value == null ? "" : value);
    return text
      .replace(/<reasoning-language\b[^>]*>[\s\S]*?<\/reasoning-language\s*>/gi, "")
      .replace(/<\/?(?:reasoning-language|response-language|display-language)\b[^>]*>/gi, "")
      .trim();
  }

  function appendExpandable(parent, value, label, className) {
    if (!parent || value === undefined || value === null) return;
    var text = cleanVisibleText(value);
    if (!text) return;
    var bounded = text.length > MAX_RENDER_CHARS ? text.slice(0, MAX_RENDER_CHARS) + "\n…（输出已限制为 200000 字符）" : text;
    if (bounded.length <= MAX_INLINE_CHARS) {
      var inline = document.createElement("pre");
      inline.className = className || "ws-event__detail";
      inline.textContent = bounded;
      parent.appendChild(inline);
      return;
    }
    var details = document.createElement("details");
    details.className = "ws-event__detail";
    var summary = document.createElement("summary");
    summary.className = "ws-event__toggle";
    summary.textContent = (label || "展开内容") + "（" + bounded.length + " 字符）";
    var pre = document.createElement("pre");
    pre.className = className || "ws-event__detail";
    pre.textContent = bounded.slice(0, MAX_INLINE_CHARS) + "\n…";
    details.addEventListener("toggle", function () {
      if (details.open && pre.textContent.indexOf("\n…") !== -1) pre.textContent = bounded;
    });
    details.appendChild(summary);
    details.appendChild(pre);
    parent.appendChild(details);
  }

  var diffStatusLabels = { added: "新增", modified: "修改", deleted: "删除" };
  var syntaxKeywords = {
    go: "break case chan const continue default defer else fallthrough for func go goto if import interface map package range return select struct switch type var",
    js: "break case catch class const continue debugger default delete do else export extends finally for function if import in instanceof let new return super switch this throw try typeof var void while with yield async await",
    ts: "break case catch class const continue debugger default delete do else export extends finally for function if import in instanceof let new return super switch this throw try typeof var void while with yield async await interface type enum implements public private readonly",
    py: "and as assert async await break case class continue def del elif else except finally for from global if import in is lambda match nonlocal not or pass raise return try while with yield",
    rs: "as async await break const continue crate dyn else enum extern false fn for if impl in let loop match mod move mut pub ref return self Self static struct super trait true type unsafe use where while",
    json: "true false null",
    sh: "if then else elif fi for while in do done case esac function",
    bash: "if then else elif fi for while in do done case esac function"
  };

  function syntaxClass(token, language) {
    if (/^(?:\"|\\'|`)/.test(token)) return "ws-syntax-string";
    if (/^(?:\/\/|#)/.test(token)) return "ws-syntax-comment";
    if (/^\d/.test(token)) return "ws-syntax-number";
    if ((syntaxKeywords[language] || "").split(" ").indexOf(token) !== -1) return "ws-syntax-keyword";
    return "";
  }

  function appendHighlightedCode(parent, text, language) {
    var source = String(text || "");
    var re = /("(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|`(?:\\.|[^`\\])*`|\/\/.*|#.*|\b\d+(?:\.\d+)?\b|\b[A-Za-z_$][\w$]*\b)/g;
    var cursor = 0;
    var match;
    while ((match = re.exec(source)) !== null) {
      if (match.index > cursor) parent.appendChild(document.createTextNode(source.slice(cursor, match.index)));
      var token = match[0];
      var className = syntaxClass(token, language);
      if (!className) parent.appendChild(document.createTextNode(token));
      else {
        var span = document.createElement("span");
        span.className = className;
        span.textContent = token;
        parent.appendChild(span);
      }
      cursor = match.index + token.length;
    }
    if (cursor < source.length) parent.appendChild(document.createTextNode(source.slice(cursor)));
  }

  function appendDiffRows(parent, rows, language) {
    rows.forEach(function (row) {
      if (!row || typeof row !== "object") return;
      var kind = String(row.kind || "meta");
      var line = document.createElement("div");
      line.className = "ws-diff-line ws-diff-line--" + kind;
      var oldNo = document.createElement("span");
      oldNo.className = "ws-diff-line__number";
      oldNo.textContent = row.oldLine ? String(row.oldLine) : "";
      var newNo = document.createElement("span");
      newNo.className = "ws-diff-line__number";
      newNo.textContent = row.newLine ? String(row.newLine) : "";
      var marker = document.createElement("span");
      marker.className = "ws-diff-line__marker";
      marker.textContent = kind === "added" ? "+" : kind === "deleted" ? "−" : kind === "context" ? " " : "";
      var code = document.createElement("code");
      code.className = "ws-diff-line__code";
      if (kind === "hunk" || kind === "meta") code.textContent = String(row.text || "");
      else appendHighlightedCode(code, row.text, language);
      line.appendChild(oldNo);
      line.appendChild(newNo);
      line.appendChild(marker);
      line.appendChild(code);
      parent.appendChild(line);
    });
  }

  function copyText(value, button) {
    var text = String(value || "");
    var success = function () {
      if (!button) return;
      var original = button.textContent;
      button.textContent = "已复制";
      setTimeout(function () { button.textContent = original; }, 1400);
    };
    var fallback = function () {
      var input = document.createElement("textarea");
      input.value = text;
      input.setAttribute("readonly", "true");
      input.style.position = "fixed";
      input.style.opacity = "0";
      document.body.appendChild(input);
      input.select();
      try { if (document.execCommand("copy")) success(); } catch (_) {}
      document.body.removeChild(input);
    };
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).then(success).catch(fallback);
      return;
    }
    fallback();
  }

  function renderDiff(parent, fileDiff, options) {
    if (!parent || !fileDiff || typeof fileDiff !== "object") return;
    options = options || {};
    var view = document.createElement("section");
    view.className = "ws-diff-view" + (options.context ? " ws-diff-view--context" : "");
    if (!options.hideHeader) {
      var head = document.createElement("header");
      head.className = "ws-diff-view__head";
      var identity = document.createElement("div");
      identity.className = "ws-diff-view__identity";
      var path = document.createElement("strong");
      path.className = "ws-diff-view__path";
      path.textContent = String(fileDiff.path || "未命名文件");
      var status = document.createElement("span");
      status.className = "ws-diff-view__status ws-diff-view__status--" + String(fileDiff.status || "unknown");
      status.textContent = diffStatusLabels[fileDiff.status] || "变更";
      identity.appendChild(path);
      identity.appendChild(status);
      var stats = document.createElement("span");
      stats.className = "ws-diff-view__stats";
      stats.textContent = "+" + Number(fileDiff.added || 0) + " / −" + Number(fileDiff.removed || 0);
      var copy = document.createElement("button");
      copy.type = "button";
      copy.className = "ws-diff-view__copy";
      copy.textContent = "复制 Diff";
      copy.setAttribute("aria-label", "复制 " + String(fileDiff.path || "文件") + " 的 Diff");
      copy.addEventListener("click", function () { copyText(fileDiff.diff, copy); });
      head.appendChild(identity);
      head.appendChild(stats);
      head.appendChild(copy);
      view.appendChild(head);
    }
    var body = document.createElement("div");
    body.className = "ws-diff-view__body";
    if (fileDiff.binary) body.textContent = "二进制文件，服务端未生成文本 Diff。";
    else {
      var rows = Array.isArray(fileDiff.lines) ? fileDiff.lines : [];
      if (rows.length) {
        var limit = 180;
        var language = String(fileDiff.language || "text").toLowerCase();
        appendDiffRows(body, rows.slice(0, limit), language);
        if (rows.length > limit) {
          var details = document.createElement("details");
          details.className = "ws-diff-view__fold";
          var summary = document.createElement("summary");
          summary.textContent = "展开剩余 " + (rows.length - limit) + " 行";
          details.appendChild(summary);
          var rest = document.createElement("div");
          appendDiffRows(rest, rows.slice(limit), language);
          details.appendChild(rest);
          body.appendChild(details);
        }
      } else if (fileDiff.diff) {
        // Legacy servers still expose the exact unified diff. Do not derive
        // status or line numbers client-side; show the raw source instead.
        var legacy = document.createElement("pre");
        legacy.className = "ws-diff-view__legacy";
        legacy.textContent = String(fileDiff.diff);
        body.appendChild(legacy);
      } else body.textContent = "没有可展示的文本 Diff。";
    }
    view.appendChild(body);
    parent.appendChild(view);
  }

  function renderContextDiff(fileDiff) {
    if (!el.contextDiff || !fileDiff) return;
    clearNode(el.contextDiff);
    renderDiff(el.contextDiff, fileDiff, { context: true });
  }

  function syncTreeSelection(path) {
    var target = String(path || "").replace(/\\/g, "/");
    if (!target) return;
    var rows = document.querySelectorAll(".ws-tree .tree-row");
    var exact = null;
    var fallback = null;
    rows.forEach(function (row) {
      row.classList.remove("is-selected");
      var name = row.querySelector(".name");
      if (!name) return;
      var label = String(name.textContent || "").replace(/\\/g, "/");
      var declared = row.getAttribute("data-ws-path");
      if (declared && declared === target) exact = row;
      if (!fallback && (label === target || target.endsWith("/" + label))) fallback = row;
    });
    var selected = exact || fallback;
    if (selected) selected.classList.add("is-selected");
  }

  function renderDiffList() {
    if (!el.diffList) return;
    clearNode(el.diffList);
    var paths = Object.keys(workflow.diffs);
    if (!paths.length) {
      var empty = document.createElement("p");
      empty.className = "ws-context-empty";
      empty.textContent = "收到变更后，Diff 会显示在这里。";
      el.diffList.appendChild(empty);
      return;
    }
    paths.forEach(function (path) {
      renderDiff(el.diffList, workflow.diffs[path]);
    });
  }

  function terminalCommand(args) {
    if (!args) return "";
    var raw = args;
    if (typeof args === "string") {
      try { raw = JSON.parse(args); } catch (_) { return args; }
    }
    if (raw && typeof raw === "object") {
      if (typeof raw.command === "string") return raw.command;
      if (typeof raw.cmd === "string") return raw.cmd;
      if (Array.isArray(raw.argv)) return raw.argv.map(String).join(" ");
    }
    return String(args);
  }

  function renderTerminalList() {
    if (!el.terminalList) return;
    clearNode(el.terminalList);
    var records = Object.keys(workflow.tools).map(function (key) { return workflow.tools[key]; }).filter(function (record) {
      return record && record.terminal;
    });
    if (!records.length) {
      var empty = document.createElement("p");
      empty.className = "ws-context-empty";
      empty.textContent = "执行命令后，终端输出会显示在这里。";
      el.terminalList.appendChild(empty);
      return;
    }
    records.forEach(function (record) {
      var entry = document.createElement("article");
      entry.className = "ws-terminal-entry";
      var head = document.createElement("header");
      head.className = "ws-terminal-head";
      head.textContent = record.name || "终端";
      var status = document.createElement("span");
      status.className = "ws-terminal-status";
      var state = record.state === "running" ? "进行中" : record.err ? "失败" : "已完成";
      status.textContent = state;
      if (record.err) status.classList.add("is-failed");
      else if (record.state === "done") status.classList.add("is-done");
      if (record.execution && typeof record.execution.exitCode === "number") {
        status.textContent += " · 退出 " + record.execution.exitCode;
      }
      head.appendChild(status);
      entry.appendChild(head);
      var command = document.createElement("div");
      command.className = "ws-terminal-command";
      command.textContent = "$ " + terminalCommand(record.args);
      entry.appendChild(command);
      var output = record.output || (record.execution && record.execution.outputTail) || record.err || "";
      if (output) {
        var pre = document.createElement("pre");
        pre.className = "ws-terminal-output";
        pre.textContent = String(output);
        entry.appendChild(pre);
      }
      el.terminalList.appendChild(entry);
    });
  }

  function reviewRisk(record) {
    if (record.execution && record.execution.mutationRisk) return String(record.execution.mutationRisk);
    if (record.state === "running") return "等待服务端完成执行并返回风险状态";
    return "风险由服务端权限策略判定";
  }

  function resolveReviewApproval(id, allow) {
    var record = workflow.approvals[id];
    if (!record || record.status !== "pending") return;
    record.status = "submitting";
    renderReviewList();
    postJSON("/approve", { id: id, allow: !!allow, session: false, persist: false }).then(function () {
      record.status = allow ? "approved" : "rejected";
      renderReviewList();
    }).catch(function (err) {
      record.status = "pending";
      renderReviewList();
      showNotice("Review 决策未提交：" + err.message, "error");
    });
  }

  function askSelection(record, questionID) {
    if (!record.selected[questionID]) record.selected[questionID] = [];
    return record.selected[questionID];
  }

  function askReady(record) {
    var questions = record && record.ask && Array.isArray(record.ask.questions) ? record.ask.questions : [];
    return questions.length > 0 && questions.every(function (question, index) {
      var id = String(question.id || "question-" + index);
      return askSelection(record, id).length > 0;
    });
  }

  function submitAskAnswer(record) {
    if (!record || record.status !== "pending" || !askReady(record)) return;
    var questions = Array.isArray(record.ask.questions) ? record.ask.questions : [];
    var answers = questions.map(function (question, index) {
      var id = String(question.id || "question-" + index);
      var selected = askSelection(record, id);
      var options = Array.isArray(question.options) ? question.options : [];
      return {
        questionId: id,
        selected: selected.map(function (optionIndex) {
          return options[optionIndex] && String(options[optionIndex].label || "");
        }).filter(Boolean)
      };
    });
    record.status = "submitting";
    renderAskCard(record);
    postJSON("/answer", { id: String(record.ask.id), answers: answers }).then(function () {
      record.status = "answered";
      renderAskCard(record);
    }).catch(function (err) {
      record.status = "pending";
      renderAskCard(record);
      showNotice("选择未提交：" + err.message, "error");
    });
  }

  function renderAskCard(record) {
    if (!record || !record.card || !record.ask) return;
    clearNode(record.card.body);
    var questions = Array.isArray(record.ask.questions) ? record.ask.questions : [];
    if (!questions.length) {
      record.card.body.textContent = "服务端未提供可选问题。";
      setStatus(record.card, "无选项", "failed");
      return;
    }
    questions.forEach(function (question, questionIndex) {
      var questionID = String(question.id || "question-" + questionIndex);
      var block = document.createElement("section");
      block.className = "ws-ask__question";
      var prompt = document.createElement("div");
      prompt.className = "ws-ask__prompt";
      var header = String(question.header || "").trim();
      prompt.textContent = header ? header + " · " + cleanVisibleText(question.prompt || "") : cleanVisibleText(question.prompt || "");
      block.appendChild(prompt);
      var options = document.createElement("div");
      options.className = "ws-ask__options";
      var selected = askSelection(record, questionID);
      var optionList = Array.isArray(question.options) ? question.options : [];
      optionList.forEach(function (option, optionIndex) {
        var button = document.createElement("button");
        button.type = "button";
        button.className = "ws-ask__option";
        button.disabled = record.status !== "pending";
        button.setAttribute("aria-pressed", String(selected.indexOf(optionIndex) !== -1));
        if (selected.indexOf(optionIndex) !== -1) button.classList.add("is-selected");
        var label = document.createElement("strong");
        label.className = "ws-ask__option-label";
        label.textContent = String(option && option.label || "选项 " + (optionIndex + 1));
        button.appendChild(label);
        if (option && option.description) {
          var description = document.createElement("span");
          description.className = "ws-ask__option-description";
          description.textContent = cleanVisibleText(option.description);
          button.appendChild(description);
        }
        button.addEventListener("click", function () {
          if (record.status !== "pending") return;
          var next = selected.slice();
          var current = next.indexOf(optionIndex);
          if (question.multi) {
            if (current === -1) next.push(optionIndex);
            else next.splice(current, 1);
          } else {
            next = [optionIndex];
          }
          record.selected[questionID] = next;
          renderAskCard(record);
        });
        options.appendChild(button);
      });
      if (!optionList.length) {
        var noOptions = document.createElement("p");
        noOptions.className = "ws-ask__empty";
        noOptions.textContent = "服务端未提供选项。";
        options.appendChild(noOptions);
      }
      block.appendChild(options);
      record.card.body.appendChild(block);
    });
    var submit = document.createElement("button");
    submit.type = "button";
    submit.className = "ws-ask__submit";
    submit.textContent = record.status === "answered" ? "已提交" : record.status === "submitting" ? "提交中…" : record.status === "cancelled" ? "已取消" : "提交选择";
    submit.disabled = record.status !== "pending" || !askReady(record);
    submit.addEventListener("click", function () { submitAskAnswer(record); });
    record.card.body.appendChild(submit);
    var status = record.status === "answered" ? "已提交" : record.status === "submitting" ? "提交中" : record.status === "cancelled" ? "已取消" : "待选择";
    var state = record.status === "answered" ? "done" : record.status === "cancelled" ? "cancelled" : "running";
    setStatus(record.card, status, state);
  }

  function renderAskRequest(ask) {
    if (!ask || !ask.id || !Array.isArray(ask.questions)) return;
    var id = String(ask.id);
    var record = workflow.asks[id];
    if (!record) {
      record = { ask: ask, selected: Object.create(null), status: "pending", card: makeEvent("ask", "需要选择", "?") };
      workflow.asks[id] = record;
    } else {
      record.ask = ask;
    }
    renderAskCard(record);
  }

  function renderReviewList() {
    if (!el.reviewList) return;
    clearNode(el.reviewList);
    var changes = Object.keys(workflow.tools).map(function (key) { return workflow.tools[key]; }).filter(function (record) {
      return record && record.fileDiff;
    });
    var approvals = Object.keys(workflow.approvals).map(function (key) { return workflow.approvals[key]; });
    if (!changes.length && !approvals.length) {
      var empty = document.createElement("p");
      empty.className = "ws-context-empty";
      empty.textContent = "暂无待审阅变更或权限请求。";
      el.reviewList.appendChild(empty);
      return;
    }
    changes.forEach(function (record) {
      var entry = document.createElement("article");
      entry.className = "ws-review-entry";
      var head = document.createElement("header");
      head.className = "ws-review-head";
      head.textContent = "文件变更";
      var status = document.createElement("span");
      status.className = "ws-review-status";
      status.textContent = record.state === "running" ? "待执行" : record.err ? "执行失败" : "已报告";
      if (record.err) status.classList.add("is-rejected");
      else if (record.state === "done") status.classList.add("is-approved");
      head.appendChild(status);
      entry.appendChild(head);
      var body = document.createElement("div");
      body.className = "ws-review-body";
      var path = document.createElement("div");
      path.className = "ws-review-path";
      path.textContent = String(record.fileDiff.path || "未命名文件") + "  +" + Number(record.fileDiff.added || 0) + " / −" + Number(record.fileDiff.removed || 0);
      body.appendChild(path);
      var risk = document.createElement("div");
      risk.className = "ws-review-risk";
      risk.textContent = reviewRisk(record);
      body.appendChild(risk);
      entry.appendChild(body);
      el.reviewList.appendChild(entry);
    });
    approvals.forEach(function (record) {
      var approval = record.approval || {};
      var entry = document.createElement("article");
      entry.className = "ws-review-entry";
      var head = document.createElement("header");
      head.className = "ws-review-head";
      head.textContent = "权限请求 · " + String(approval.tool || "工具");
      var status = document.createElement("span");
      status.className = "ws-review-status";
      status.textContent = record.status === "approved" ? "已通过" : record.status === "rejected" ? "已拒绝" : record.status === "cancelled" ? "已取消" : record.status === "submitting" ? "提交中" : "待确认";
      if (record.status === "approved") status.classList.add("is-approved");
      if (record.status === "rejected") status.classList.add("is-rejected");
      if (record.status === "cancelled") status.classList.add("is-cancelled");
      head.appendChild(status);
      entry.appendChild(head);
      var body = document.createElement("div");
      body.className = "ws-review-body";
      body.textContent = String(approval.subject || approval.reason || "服务端要求确认后才会执行。");
      if (approval.reason && approval.subject) {
        var reason = document.createElement("div");
        reason.className = "ws-review-risk";
        reason.textContent = String(approval.reason);
        body.appendChild(reason);
      }
      if (record.status === "pending" && approval.id) {
        var actions = document.createElement("div");
        actions.className = "ws-review-actions";
        [true, false].forEach(function (allow) {
          var button = document.createElement("button");
          button.type = "button";
          button.textContent = allow ? "通过" : "拒绝";
          button.setAttribute("data-ws-review-allow", String(allow));
          button.addEventListener("click", function () { resolveReviewApproval(String(approval.id), allow); });
          actions.appendChild(button);
        });
        body.appendChild(actions);
      }
      entry.appendChild(body);
      el.reviewList.appendChild(entry);
    });
  }

  function toolLabel(name) {
    var labels = {
      read_file: "读取文件", read_files: "读取文件", glob: "查找文件",
      grep: "搜索内容", bash: "执行命令", shell: "执行命令",
      write_file: "写入文件", edit_file: "编辑文件", apply_patch: "应用补丁",
      todo_write: "更新计划", complete_step: "完成计划项"
    };
    return labels[name] || name || "工具";
  }

  function toolKey(tool, seq) {
    return tool && tool.id ? String(tool.id) : "anonymous-tool-" + String(seq || lastEventSeq);
  }

  function renderTool(card, record) {
    if (!card || !record) return;
    record.progressEl = null;
    clearNode(card.body);
    if (record.args) appendExpandable(card.body, record.args, "展开参数", "ws-tool-preview");
    if (record.fileDiff) renderDiff(card.body, record.fileDiff);
    if (record.output) appendExpandable(card.body, record.output, "展开输出", "ws-tool-preview");
    if (record.err) appendExpandable(card.body, record.err, "展开错误", "ws-tool-preview");
    if (record.truncated) {
      var note = document.createElement("div");
      note.className = "ws-timeline__notice";
      note.textContent = "输出过长，服务端已截断显示。";
      card.body.appendChild(note);
    }
  }

  function parsePlan(value) {
    if (!value) return null;
    var raw = value;
    if (typeof value === "string") {
      try { raw = JSON.parse(value); } catch (_) {
        return value.split(/\r?\n/).map(function (line) {
          return line.replace(/^\s*(?:[-*]|\d+[.)])\s*/, "").trim();
        }).filter(Boolean).map(function (content) { return { content: content, status: "pending" }; });
      }
    }
    var items = Array.isArray(raw) ? raw : raw.todos || raw.items || raw.plan;
    if (!Array.isArray(items)) return null;
    return items.map(function (item) {
      if (typeof item === "string") return { content: item, status: "pending" };
      return { content: String(item.content || item.title || item.label || ""), status: String(item.status || "pending") };
    }).filter(function (item) { return item.content; });
  }

  function renderPlan(items) {
    if (!el.timeline || !items) return;
    if (!workflow.plan) workflow.plan = makeEvent("plan", "计划", "✓");
    if (!workflow.plan) return;
    clearNode(workflow.plan.body);
    var list = document.createElement("ol");
    list.className = "ws-plan";
    items.forEach(function (item) {
      var li = document.createElement("li");
      li.textContent = cleanVisibleText(item.content);
      if (item.status === "completed" || item.status === "done") li.className = "is-done";
      list.appendChild(li);
    });
    workflow.plan.body.appendChild(list);
    setStatus(workflow.plan, items.length + " 项", "done");
  }

  function renderAssistant(data) {
    var kind = data.kind || "text";
    if (!workflow.assistant) workflow.assistant = makeEvent("assistant", "semantix", "✦");
    if (!workflow.assistant) return;
    if (kind === "message") {
      workflow.assistant.text = cleanVisibleText(data.text || "");
      workflow.assistant.reasoning = cleanVisibleText(data.reasoning || "");
      workflow.assistant.finalized = true;
      clearNode(workflow.assistant.body);
      workflow.assistant.textEl = null;
      workflow.assistant.reasoningEl = null;
      if (workflow.assistant.text) appendExpandable(workflow.assistant.body, workflow.assistant.text, "展开回复");
      if (workflow.assistant.reasoning) appendExpandable(workflow.assistant.body, workflow.assistant.reasoning, "展开思考过程");
    } else if (workflow.assistant.finalized) {
      // A completed message is authoritative. Ignore a late duplicate delta
      // instead of appending it a second time to the visible answer.
      return;
    } else if (kind === "reasoning") {
      var reasoningDelta = cleanVisibleText(data.reasoning || data.text || "");
      workflow.assistant.reasoning = (workflow.assistant.reasoning || "") + reasoningDelta;
      if (!workflow.assistant.reasoningEl) {
        workflow.assistant.reasoningEl = document.createElement("pre");
        workflow.assistant.reasoningEl.className = "ws-event__detail";
        workflow.assistant.body.appendChild(workflow.assistant.reasoningEl);
      }
      workflow.assistant.reasoningEl.appendChild(document.createTextNode(reasoningDelta));
    } else {
      var textDelta = cleanVisibleText(data.text || "");
      workflow.assistant.text = (workflow.assistant.text || "") + textDelta;
      if (!workflow.assistant.textEl) {
        workflow.assistant.textEl = document.createElement("div");
        workflow.assistant.textEl.className = "ws-event__text";
        workflow.assistant.body.appendChild(workflow.assistant.textEl);
      }
      workflow.assistant.textEl.appendChild(document.createTextNode(textDelta));
    }
    setStatus(workflow.assistant, kind === "message" ? "已完成" : "生成中", kind === "message" ? "done" : "running");
  }

  function renderToolEvent(kind, tool, seq) {
    if (!tool) return;
    var key = toolKey(tool, seq);
    var record = workflow.tools[key];
    if (!record) {
      record = { args: "", output: "", err: "", truncated: false, progress: "", fileDiff: null, terminal: false, name: "" };
      workflow.tools[key] = record;
    }
    if (!record.card) {
      // A record restored by /workspace/replay is panel-only (no timeline
      // card yet, to avoid duplicating /history). The first live frame for
      // the same tool id materializes its card.
      record.card = makeEvent("tool", toolLabel(record.name || tool.name), "⚙");
    }
    if (tool.name) {
      record.name = String(tool.name);
      record.terminal = record.terminal || tool.name === "bash" || tool.name === "shell" || !!tool.execution;
      if (record.card) record.card.label.textContent = toolLabel(tool.name) + " · " + tool.name;
    }
    if (tool.args) record.args = String(tool.args);
    if (tool.fileDiff) {
      record.fileDiff = tool.fileDiff;
      workflow.diffs[String(tool.fileDiff.path || key)] = tool.fileDiff;
      renderContextDiff(tool.fileDiff);
      syncTreeSelection(tool.fileDiff.path);
    }
    if (tool.execution) record.execution = tool.execution;
    if (kind === "tool_start") {
      record.state = "running";
      setStatus(record.card, "进行中", "running");
    } else if (kind === "tool_result") {
      record.output = String(tool.output || "").slice(0, MAX_RENDER_CHARS);
      record.err = String(tool.err || "").slice(0, MAX_RENDER_CHARS);
      record.truncated = !!tool.truncated;
      record.state = record.err ? "failed" : "done";
      setStatus(record.card, record.err ? "失败" : "已完成", record.err ? "failed" : "done");
      if (tool.durationMs) record.card.status.textContent += " · " + tool.durationMs + " ms";
    } else {
      var chunk = String(tool.output || "");
      if (record.progress.length < MAX_RENDER_CHARS) {
        var previousLength = record.progress.length;
        record.progress += chunk.slice(0, MAX_RENDER_CHARS - record.progress.length);
        if (record.progress.length < previousLength + chunk.length) record.truncated = true;
      } else if (chunk) {
        record.truncated = true;
      }
      record.output = record.progress;
      record.state = "running";
      setStatus(record.card, "进行中", "running");
    }
    record.card.article.setAttribute("data-ws-tool-state", record.state || "running");
    if (kind === "tool_progress") {
      if (!record.progressEl) {
        renderTool(record.card, record);
        record.progressEl = document.createElement("pre");
        record.progressEl.className = "ws-tool-preview";
        record.card.body.appendChild(record.progressEl);
      }
      record.progressEl.textContent = record.output + (record.truncated ? "\n…（输出已限制）" : "");
    } else {
      renderTool(record.card, record);
    }
    renderDiffList();
    renderTerminalList();
    renderReviewList();
    if (tool.name === "todo_write" && tool.args) renderPlan(parsePlan(tool.args));
  }

  function renderStatus(kind, data) {
    var card;
    if (kind === "error") {
      var errorText = cleanVisibleText(data.err || data.text || data.detail);
      if (!workflow.active && isProviderAuthError(errorText)) {
        showNotice("Provider 尚未通过鉴权，已保留工作流预览。", "warn");
        return;
      }
      card = makeEvent("error", "执行失败", "!");
      if (card) { appendExpandable(card.body, errorText, "查看错误"); setStatus(card, "失败", "failed"); }
      return;
    }
    if (kind === "cache_status") {
      updateCacheView(data);
      card = makeEvent("cache", "缓存状态", "◌");
      if (card) {
        var u = data.usage || {};
        var hit = Number(u.cacheHitTokens || 0), miss = Number(u.cacheMissTokens || 0);
        var kernel = data.kernelCache || {};
        var detail = kernel.layer ? (kernel.layer + " " + (kernel.op || "observed") + (Array.isArray(kernel.sliceIds) ? " · " + kernel.sliceIds.length + " slices" : "")) : (data.usage ? "L1 命中 " + hit + " · 未命中 " + miss : "已观测");
        card.body.textContent = detail;
        setStatus(card, "已更新", "done");
      }
      return;
    }
    if (kind === "plan") { renderPlan(parsePlan(data.plan || data.text || data.detail)); return; }
    var statusText = cleanVisibleText(data.text || data.detail || data.phase || data.outcome || "");
    if (!workflow.active && isProviderAuthError(statusText)) {
      showNotice("Provider 尚未通过鉴权，已保留工作流预览。", "warn");
      return;
    }
    card = makeEvent(kind === "retry" ? "retry" : "status", kind === "retry" ? "重试" : "任务状态", kind === "retry" ? "↻" : "•");
    if (!card) return;
    var text = statusText;
    if (kind === "retry") text = "第 " + (data.retryAttempt || "?") + " / " + (data.retryMax || "?") + " 次重试" + (data.retryScope ? " · " + data.retryScope : "");
    if (text) card.body.textContent = String(text);
    setStatus(card, kind === "retry" ? "等待中" : "已记录", kind === "retry" ? "running" : "done");
  }

  function isProviderAuthError(text) {
    return /(authentication|unauthori[sz]ed|invalid api key|api key|http\s*401|令牌|鉴权|密钥)/i.test(String(text || ""));
  }

  // ── tiny view helpers ──
  function setState(chip, state) {
    chip.setAttribute("data-ws-state", state);
    chip.setAttribute("aria-busy", state === "loading" ? "true" : "false");
  }

  function setValue(span, text) {
    span.textContent = text;
  }

  function showNotice(message, kind) {
    if (!el.notice) return;
    el.notice.textContent = message;
    el.notice.className = "ws-notice ws-notice--" + (kind || "error");
    el.notice.hidden = false;
    clearTimeout(showNotice.timer);
    showNotice.timer = setTimeout(function () { el.notice.hidden = true; }, 6000);
  }

  function updateComposerControls() {
    var busy = composerBusy || sessionRunning;
    if (el.send) {
      el.send.disabled = busy || !el.input || !String(el.input.value || "").trim();
      el.send.hidden = busy;
    }
    if (el.cancel) el.cancel.hidden = !busy;
  }

  function setComposerRunning(running) {
    composerBusy = !!running;
    updateComposerControls();
  }

  // #403: /status is the single source of truth for whether the active
  // session is still running. turn_done/error frames only clear the optimistic
  // composerBusy flag, but sessionRunning was latched by the last refreshTasks
  // — without a re-sync the composer stayed disabled (and cancel stuck) until
  // a manual refresh or task switch. Re-read /status so the composer and the
  // current-row pill reflect the real backend state again.
  function refreshRunningState() {
    var requestSeq = ++runningSyncSeq;
    return getJSON("/status").then(function (status) {
      if (requestSeq !== runningSyncSeq) return; // a newer request superseded this one
      sessionRunning = !!status.running;
      updateComposerControls();
      if (sessionRows.length) renderTasks(sessionRows);
    }).catch(function () { /* transient /status failure: keep current latch */ });
  }

  function addOptimisticUserMessage(text) {
    var card = makeEvent("user", "用户", "›");
    if (!card) return;
    card.body.textContent = text;
    setStatus(card, "发送中", "running");
    card.article.setAttribute("data-ws-local-user", "true");
    workflow.localUser = { text: text, card: card };
  }

  function sendComposer() {
    if (!el.input) return;
    var text = String(el.input.value || "").trim();
    if (!text) return;
    if (sessionRunning || composerBusy) {
      showNotice("当前任务正在运行，请等待完成或先中止。", "warn");
      return;
    }
    addOptimisticUserMessage(text);
    setComposerRunning(true);
    postJSON("/submit", { input: text }).then(function () {
      el.input.value = "";
      refreshTasks();
    }).catch(function (err) {
      setComposerRunning(false);
      if (workflow.localUser && workflow.localUser.card) {
        setStatus(workflow.localUser.card, "未发送", "failed");
      }
      showNotice("发送失败：" + err.message, "error");
      workflow.localUser = null;
    }).finally(function () {
      if (el.input) el.input.dispatchEvent(new Event("input"));
    });
  }

  function cancelComposer() {
    if (!composerBusy && !sessionRunning) return;
    postJSON("/cancel").then(function () {
      showNotice("已请求中止当前任务。", "warn");
    }).catch(function (err) {
      showNotice("中止失败：" + err.message, "error");
    });
  }

  function renderPermission(mode) {
    if (!el.permissionLabel) return;
    var labels = { ask: "每次确认", auto: "自动确认", yolo: "完全访问" };
    var value = labels[String(mode || "ask").toLowerCase()] || "服务端权限";
    el.permissionLabel.textContent = value;
    if (el.permission) el.permission.title = "当前权限：" + value + "。权限由服务端策略控制。";
  }

  function initComposer() {
    if (!el.composer || !el.input) return;
    if (el.send) el.send.addEventListener("click", function (event) { event.preventDefault(); sendComposer(); });
    if (el.cancel) el.cancel.addEventListener("click", cancelComposer);
    el.composer.addEventListener("submit", function (event) { event.preventDefault(); sendComposer(); });
    el.input.addEventListener("input", function () { setComposerRunning(composerBusy); });
    el.input.addEventListener("keydown", function (event) {
      if (event.key === "Enter" && !event.shiftKey) {
        event.preventDefault();
        sendComposer();
      }
      if (event.key === "Escape" && (composerBusy || sessionRunning)) {
        event.preventDefault();
        cancelComposer();
      }
    });
    if (el.permission) el.permission.addEventListener("click", function () {
      showNotice("权限由服务端策略控制；高风险操作会单独请求确认。", "warn");
    });
    setComposerRunning(false);
  }

  function handleWorkspaceEvent(message) {
    if (historyHydrating) {
      bufferedWorkspaceEvents.push({ data: message.data });
      return;
    }
    var payload;
    try {
      payload = JSON.parse(message.data || "");
    } catch (_) {
      return;
    }
    if (!payload || payload.v !== 1 || !Number.isSafeInteger(payload.seq) || payload.seq < 1) return;
    if (eventTaskID && payload.task_id !== eventTaskID) return;
    if (!eventTaskID && typeof payload.task_id === "string") eventTaskID = payload.task_id;
    if (payload.seq <= lastEventSeq) return;
    if (lastEventSeq && payload.seq > lastEventSeq + 1) {
      showNotice("事件流存在缺口，正在恢复会话。", "warn");
      recoverWorkspace();
      return;
    }
    lastEventSeq = payload.seq;
    if (!canonicalEventTypes[payload.type]) return;
    var data;
    try { data = typeof payload.data === "string" ? JSON.parse(payload.data || "{}") : payload.data; } catch (_) { return; }
    if (!data || typeof data !== "object") return;
    // The inner eventwire frame remains the source of truth. The renderer only
    // projects it into cards; it never treats text as markup or invents data.
    if (data.kind === "turn_started") {
      workflow.assistant = null;
      workflow.plan = null;
      workflow.tools = Object.create(null);
      workflow.diffs = Object.create(null);
      workflow.approvals = Object.create(null);
      workflow.asks = Object.create(null);
      renderDiffList();
      renderTerminalList();
      renderReviewList();
    }
    switch (payload.type) {
      case "user_message":
        if (workflow.localUser && workflow.localUser.text === String(data.text || "")) {
          setStatus(workflow.localUser.card, "已发送", "done");
          workflow.localUser = null;
          break;
        }
        var user = makeEvent("user", "用户", "›");
        if (user) { user.body.textContent = cleanVisibleText(data.text || ""); setStatus(user, "已发送", "done"); }
        break;
      case "assistant_message":
        renderAssistant(data);
        break;
      case "plan":
        renderStatus("plan", data);
        break;
      case "tool_start":
      case "tool_result":
        renderToolEvent(data.kind === "tool_progress" ? "tool_progress" : payload.type, data.tool, payload.seq);
        break;
      case "error":
        composerBusy = false;
        updateComposerControls();
        refreshRunningState(); // #403: turn aborted — release the running latch
        if (workflow.localUser) {
          setStatus(workflow.localUser.card, "未发送", "failed");
          workflow.localUser = null;
        }
        renderStatus("error", data);
        break;
      case "cache_status":
        renderStatus("cache_status", data);
        break;
      case "task_status":
        if (data.kind === "turn_done") {
          var cancelled = !!data.cancelled || String(data.outcome || "").toLowerCase() === "cancelled";
          composerBusy = false;
          updateComposerControls();
          refreshRunningState(); // #403: turn finished — re-sync the running latch
          if (workflow.localUser) {
            setStatus(workflow.localUser.card, cancelled ? "已取消" : "已发送", cancelled ? "cancelled" : "done");
            workflow.localUser = null;
          }
          if (cancelled) {
            Object.keys(workflow.approvals).forEach(function (id) {
              var record = workflow.approvals[id];
              if (record && (record.status === "pending" || record.status === "submitting")) record.status = "cancelled";
            });
            Object.keys(workflow.asks).forEach(function (id) {
              var askRecord = workflow.asks[id];
              if (askRecord && (askRecord.status === "pending" || askRecord.status === "submitting")) {
                askRecord.status = "cancelled";
                renderAskCard(askRecord);
              }
            });
            renderReviewList();
          }
        }
        if (data.code === "semantix_reuse") updateCacheView(data);
        renderStatus(data.kind === "retrying" ? "retry" : "task_status", data);
        break;
      case "unknown":
        // Forward-compatible frames remain visible as a neutral status card;
        // malformed/unknown inner data still cannot break the page.
        renderStatus("task_status", { text: data.text || data.detail || "收到未识别事件" });
        break;
      case "permission_request":
        if (data.kind === "ask_request" && data.ask) {
          renderAskRequest(data.ask);
          break;
        }
        var approval = data.approval || {};
        if (approval.id) {
          workflow.approvals[String(approval.id)] = { approval: approval, status: "pending" };
          renderReviewList();
        }
        var permission = makeEvent("status", "需要确认", "?");
        if (permission) {
          permission.body.textContent = cleanVisibleText(approval.subject || approval.reason || approval.tool || "等待用户确认");
          setStatus(permission, "等待中", "running");
        }
        break;
    }
  }

  function renderHistory(messages) {
    resetWorkflow();
    if (!Array.isArray(messages)) throw new Error("历史记录格式无效");
    messages.forEach(function (message, index) {
      var role = String(message && message.role || "");
      if (role === "user") {
        var user = makeEvent("user", "用户", "›");
        if (user) { user.body.textContent = cleanVisibleText(message.content || ""); setStatus(user, "已发送", "done"); }
        return;
      }
      if (role === "assistant") {
        workflow.assistant = null;
        if (message.content || message.reasoning) renderAssistant({ kind: "message", text: message.content, reasoning: message.reasoning });
        (message.toolCalls || []).forEach(function (tool, toolIndex) {
          renderToolEvent("tool_start", { id: tool.id, name: tool.name, args: tool.arguments }, "history-" + index + "-" + toolIndex);
        });
        workflow.assistant = null;
        return;
      }
      if (role === "tool") {
        renderToolEvent("tool_result", { id: message.toolCallId, name: message.toolName, output: message.content }, "history-" + index);
        return;
      }
      if (role === "notice") {
        var notice = makeEvent("status", "会话记录", "↪");
        if (notice) { notice.body.textContent = cleanVisibleText(message.content || ""); setStatus(notice, "已记录", "done"); }
      }
    });
  }

  function showHistoryError(err) {
    resetWorkflow();
    if (el.emptyTitle) el.emptyTitle.textContent = "无法加载会话";
    if (el.emptyCopy) el.emptyCopy.textContent = cleanVisibleText(err && err.message || "请检查服务状态后重试。");
    if (el.historyRetry) el.historyRetry.hidden = false;
  }

  function hydrateHistory(replayFrames) {
    historyHydrating = true;
    bufferedWorkspaceEvents = [];
    return getJSON("/history").then(renderHistory).then(function () {
      if (Array.isArray(replayFrames) && replayFrames.length) applyReplayFrames(replayFrames);
      historyHydrating = false;
      var pending = bufferedWorkspaceEvents.splice(0);
      pending.forEach(handleWorkspaceEvent);
    }).catch(function (err) {
      historyHydrating = false;
      bufferedWorkspaceEvents = [];
      showHistoryError(err);
    });
  }

  // #403: /workspace/replay is the JSON sibling of the SSE replay window. The
  // live context panels (Diff / Terminal / Review / cache status) are pure
  // projections of stream frames, so a fresh page (F5) or reconnect would
  // otherwise lose every panel once a run's frames are no longer live. Loading
  // the retained window lets the shell rebuild those panels from real observed
  // frames — without re-adding timeline cards that /history already owns.
  function loadWorkspaceReplay() {
    return getJSON("/workspace/replay").then(function (snapshot) {
      return Array.isArray(snapshot && snapshot.frames) ? snapshot.frames : [];
    }).catch(function () {
      // Older/standalone servers may not expose the endpoint: the shell still
      // hydrates the conversation from /history and stays live-only.
      return [];
    });
  }

  // applyReplayFrames folds a retained-frame window back into the workflow
  // projection. Unlike handleWorkspaceEvent it never touches the timeline:
  // user/assistant/tool cards for those frames already came from /history, so
  // rendering them again would duplicate the conversation. It only rebuilds
  // panel state (diffs, terminal executions, review changes, cache bar).
  //
  // It must NOT advance the shared lastEventSeq: frames that also arrived over
  // the live stream while hydrating are flushed afterwards by
  // handleWorkspaceEvent, which renders their timeline content exactly as it
  // would without replay — demoting them here would drop in-flight text that
  // the base flow keeps. A local cursor only dedupes within this window.
  function applyReplayFrames(frames) {
    if (!Array.isArray(frames)) return;
    var cursor = lastEventSeq;
    var lastDiff = null;
    frames.forEach(function (raw) {
      var payload;
      try { payload = typeof raw === "string" ? JSON.parse(raw) : raw; } catch (_) { return; }
      if (!payload || payload.v !== 1 || !Number.isSafeInteger(payload.seq) || payload.seq < 1) return;
      if (eventTaskID && payload.task_id !== eventTaskID) return;
      if (!eventTaskID && typeof payload.task_id === "string") eventTaskID = payload.task_id;
      if (payload.seq <= cursor) return;
      cursor = payload.seq;
      if (!canonicalEventTypes[payload.type]) return;
      var data;
      try { data = typeof payload.data === "string" ? JSON.parse(payload.data || "{}") : payload.data; } catch (_) { return; }
      if (!data || typeof data !== "object") return;
      if (data.kind === "turn_started") {
        workflow.tools = Object.create(null);
        workflow.diffs = Object.create(null);
        return;
      }
      if (payload.type === "tool_start" || payload.type === "tool_result") {
        var tool = data.tool;
        if (!tool) return;
        var kind = data.kind === "tool_progress" ? "tool_progress" : payload.type;
        var key = toolKey(tool, payload.seq);
        var record = workflow.tools[key];
        if (!record) {
          record = { args: "", output: "", err: "", truncated: false, progress: "", fileDiff: null, terminal: false, name: "" };
          workflow.tools[key] = record;
        }
        if (tool.name) {
          record.name = String(tool.name);
          record.terminal = record.terminal || tool.name === "bash" || tool.name === "shell" || !!tool.execution;
        }
        if (tool.args) record.args = String(tool.args);
        if (tool.fileDiff) {
          record.fileDiff = tool.fileDiff;
          workflow.diffs[String(tool.fileDiff.path || key)] = tool.fileDiff;
          lastDiff = tool.fileDiff;
        }
        if (tool.execution) record.execution = tool.execution;
        if (kind === "tool_start") {
          record.state = "running";
        } else if (kind === "tool_result") {
          record.output = String(tool.output || "").slice(0, MAX_RENDER_CHARS);
          record.err = String(tool.err || "").slice(0, MAX_RENDER_CHARS);
          record.truncated = !!tool.truncated;
          record.state = record.err ? "failed" : "done";
        } else {
          var chunk = String(tool.output || "");
          if (record.progress.length < MAX_RENDER_CHARS) record.progress += chunk.slice(0, MAX_RENDER_CHARS - record.progress.length);
          else if (chunk) record.truncated = true;
          record.output = record.progress;
          record.state = "running";
        }
        if (record.card) renderTool(record.card, record); // keep an existing history card in sync
        return;
      }
      if (payload.type === "cache_status") { updateCacheView(data); return; }
      if (payload.type === "task_status" && data.code === "semantix_reuse") updateCacheView(data);
    });
    renderDiffList();
    renderTerminalList();
    renderReviewList();
    if (lastDiff) renderContextDiff(lastDiff);
  }

  function connectWorkspaceEvents() {
    if (workspaceEvents) workspaceEvents.close();
    resetWorkflow();
    eventTaskID = "";
    lastEventSeq = 0;
    historyHydrating = true;
    bufferedWorkspaceEvents = [];
    if (!window.EventSource) {
      hydrateHistory();
      return;
    }
    var hydrated = false;
    workspaceEvents = new EventSource("/workspace/events?live=1");
    Object.keys(canonicalEventTypes).forEach(function (type) {
      workspaceEvents.addEventListener(type, handleWorkspaceEvent);
    });
    workspaceEvents.addEventListener("open", function () {
      if (hydrated) return;
      hydrated = true;
      // Fetch the retained window before hydrating /history so replay frames
      // (which only rebuild panels, never timeline cards) land between the
      // /history render and the buffered-live flush, deduplicating by seq.
      loadWorkspaceReplay().then(function (frames) {
        return hydrateHistory(frames);
      });
    });
    workspaceEvents.addEventListener("error", function () {
      if (!hydrated) hydrateHistory();
      recoverWorkspace();
    });
  }

  function recoverWorkspace() {
    if (historyRecoveryPending) return;
    historyRecoveryPending = true;
    if (workspaceEvents) workspaceEvents.close();
    refreshTasks();
    setTimeout(function () {
      historyRecoveryPending = false;
      connectWorkspaceEvents();
    }, 750);
  }

  function closeMenus() {
    if (!openMenu) return;
    openMenu.hidden = true;
    var btn = openMenu.parentElement && openMenu.parentElement.querySelector("button");
    if (btn) btn.setAttribute("aria-expanded", "false");
    openMenu = null;
  }

  function toggleMenu(menu, button) {
    var opening = menu.hidden;
    closeMenus();
    if (opening) {
      menu.hidden = false;
      button.setAttribute("aria-expanded", "true");
      openMenu = menu;
    }
  }

  function fillList(menu, items, onPick) {
    while (menu.firstChild) menu.removeChild(menu.firstChild);
    items.forEach(function (item) {
      var li = document.createElement("li");
      li.setAttribute("role", "option");
      li.className = "ws-pop__row";
      if (item.disabled) li.setAttribute("aria-disabled", "true");
      li.textContent = item.label;
      if (item.hint) {
        var hint = document.createElement("span");
        hint.className = "ws-pop__hint";
        hint.textContent = item.hint;
        li.appendChild(hint);
      }
      if (item.active) li.classList.add("is-active");
      if (!item.disabled && onPick) {
        li.addEventListener("click", function () { closeMenus(); onPick(item); });
      }
      menu.appendChild(li);
    });
  }

  // ── loaders: one per chip family ──
  // refreshTasks reads /status + /sessions together: the project chip name,
  // the sidebar project row, and the 运行中 pills all derive from the same
  // real backend state (#405 #406).
  function refreshTasks() {
    setState(el.project, "loading");
    return getJSON("/status").then(function (status) {
      var cwd = String(status.cwd || "");
      var name = cwd.split(/[\\/]/).filter(Boolean).pop() || "semantix";
      setValue(el.projectName, name);
      if (el.sideProjectName) el.sideProjectName.textContent = name;
      sessionRunning = !!status.running;
      renderPermission(status.toolApprovalMode);
      updateComposerControls();
      setState(el.project, "ok");
      return getJSON("/sessions").then(renderTasks);
    }).catch(function () {
      setValue(el.projectName, "未知项目");
      setState(el.project, "error");
      renderPermission("ask");
      updateComposerControls();
      renderTasks(null);
    });
  }

  function deriveTaskState(s) {
    if ((s.current && sessionRunning) || s.in_flight) return "running";
    if ((s.turns || 0) > 0) return "done";
    return "empty";
  }

  function renderTasks(sessions) {
    if (!el.taskList) return;
    while (el.taskList.firstChild) el.taskList.removeChild(el.taskList.firstChild);
    if (!Array.isArray(sessions)) {
      var err = document.createElement("li");
      err.className = "ws-tasks-note";
      err.textContent = "任务列表不可用。";
      el.taskList.appendChild(err);
      return;
    }
    sessionRows = sessions;
    refreshSessionProjectFilter();
    var visible = filterSessions();
    if (!visible.length) {
      var empty = document.createElement("li");
      empty.className = "ws-tasks-note";
      empty.textContent = sessions.length ? "没有符合筛选条件的会话。" : "还没有任务：点击上方「新建任务」开始第一个会话。";
      el.taskList.appendChild(empty);
      return;
    }
    visible.forEach(function (s) {
      var li = document.createElement("li");
      var row = document.createElement("button");
      row.type = "button";
      row.className = "ws-task-row" + (s.current ? " is-current" : "");

      var dot = document.createElement("span");
      dot.className = "ws-dot" + (s.current ? " is-on" : "");

      var title = document.createElement("span");
      title.className = "ws-task-title";
      title.textContent = s.title || s.name;

      var meta = document.createElement("span");
      meta.className = "ws-task-meta";
      var updated = formatSessionTime(s.updated_at);
      meta.textContent = (s.turns ? s.turns + " 轮" : "") + (updated ? " · " + updated : "");

      var stateKey = sessionStatus(s);
      // Forward-compatible guard: an unknown status must degrade to the
      // neutral empty pill instead of throwing on TASK_PILLS[stateKey].
      var pillStyle = TASK_PILLS[stateKey] || TASK_PILLS.empty;
      var pill = document.createElement("span");
      pill.className = "ws-state-pill " + pillStyle[1];
      pill.textContent = pillStyle[0];

      row.appendChild(dot);
      row.appendChild(title);
      row.appendChild(meta);
      row.appendChild(pill);
      li.appendChild(row);

      if (s.current) {
        row.setAttribute("aria-current", "true");
        row.title = "当前任务";
      } else {
        row.title = "切换到该任务（会话内容保留）";
        row.addEventListener("click", function () { switchTask(s); });
      }
      if (s.failure) row.title += "；恢复提示：" + s.failure;
      el.taskList.appendChild(li);
    });
  }

  function switchTask(s) {
    postJSON("/resume", { path: s.path }).then(refreshTasks).then(connectWorkspaceEvents).catch(function (err) {
      showNotice("切换任务失败：" + err.message, "error");
    });
  }

  function loadBranches() {
    setState(el.branch, "loading");
    return getJSON("/branches").then(function (data) {
      var branches = Array.isArray(data.branches) ? data.branches : [];
      // Session branches are informational for the shell: read-only display
      // keeps fork/resume flows in the sessions picker where they belong.
      fillList(el.branchMenu, branches.map(function (b, i) {
        return {
          label: b.name || b.id || "(未命名分支)",
          hint: i === 0 ? "当前" : "",
          disabled: true,
          active: i === 0
        };
      }));
      setState(el.branch, "ok");
      var gitBranch = String(data.gitBranch || "").trim();
      if (gitBranch) {
        setValue(el.branchName, gitBranch);
        el.branch.title = "当前工作区分支：" + gitBranch + "（只读展示）";
        return;
      }
      if (!branches.length) {
        setValue(el.branchName, "无分支");
        el.branch.title = "当前会话没有分支记录（只读展示）";
        setState(el.branch, "empty");
        return;
      }
      var current = branches[0];
      var label = current.name || current.id;
      setValue(el.branchName, label);
      el.branch.title = "当前会话分支：" + label + "（只读展示）";
    }).catch(function () {
      setValue(el.branchName, "不可用");
      setState(el.branch, "error");
    });
  }

  function sessionStatus(s) {
    var status = String(s && s.status || "").toLowerCase();
    return SESSION_STATUS_LABELS[status] ? status : deriveTaskState(s);
  }

  function formatSessionTime(value) {
    if (!value) return "";
    var date = new Date(value);
    if (isNaN(date.getTime())) return "";
    return date.toLocaleString([], { month: "numeric", day: "numeric", hour: "2-digit", minute: "2-digit" });
  }

  function refreshSessionProjectFilter() {
    if (!el.sessionProject) return;
    var selected = el.sessionProject.value;
    while (el.sessionProject.options.length > 1) el.sessionProject.remove(1);
    var projects = [];
    sessionRows.forEach(function (s) {
      var project = String(s.project || "").trim();
      if (project && projects.indexOf(project) === -1) projects.push(project);
    });
    projects.sort().forEach(function (project) {
      var option = document.createElement("option");
      option.value = project; option.textContent = project;
      el.sessionProject.appendChild(option);
    });
    el.sessionProject.value = projects.indexOf(selected) >= 0 ? selected : "";
  }

  function filterSessions() {
    var keyword = String(el.sessionSearch && el.sessionSearch.value || "").trim().toLowerCase();
    var project = String(el.sessionProject && el.sessionProject.value || "");
    var status = String(el.sessionStatus && el.sessionStatus.value || "");
    return sessionRows.filter(function (s) {
      var haystack = [s.name, s.title, s.failure, s.project].join(" ").toLowerCase();
      return (!keyword || haystack.indexOf(keyword) !== -1) && (!project || s.project === project) && (!status || sessionStatus(s) === status);
    });
  }

  function renderEffort(level, hasModel) {
    var label = level ? (EFFORT_LABELS[level] || level) : hasModel ? "默认" : "—";
    setValue(el.effortValue, label);
    setState(el.effort, "ok");
    el.effort.title = "推理强度：" + label;
    var rows = EFFORT_LEVELS.map(function (lv) {
      return { label: EFFORT_LABELS[lv], level: lv, active: lv === level };
    });
    if (level && !EFFORT_LABELS[level]) {
      rows.push({ label: level, level: level, active: true });
    } else if (!level) {
      rows.push({ label: "未设置（跟随模型默认）", disabled: !hasModel, active: false });
    }
    fillList(el.effortMenu, rows, pickEffort);
  }

  function initSessionFilters() {
    [el.sessionSearch, el.sessionProject, el.sessionStatus].forEach(function (control) {
      if (!control) return;
      control.addEventListener("input", function () { renderTasks(sessionRows); });
      control.addEventListener("change", function () { renderTasks(sessionRows); });
    });
    document.addEventListener("keydown", function (event) {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "k" && el.sessionSearch) {
        event.preventDefault(); el.sessionSearch.focus();
      }
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "n" && el.newTask) {
        event.preventDefault(); el.newTask.click();
      }
    });
  }

  function loadModels() {
    setState(el.model, "loading");
    return getJSON("/models").then(function (data) {
      renderEffort(String(data.effort || "").toLowerCase(), !!data.current);
      var models = Array.isArray(data.models) ? data.models : [];
      if (!models.length) {
        // #405 acceptance: an explicit, visible unavailable-model signal.
        setValue(el.modelName, "模型不可用");
        setState(el.model, "empty");
        el.model.title = "没有任何已配置的可用模型；请在 provider 设置中添加后刷新";
        fillList(el.modelMenu, [{ label: "模型不可用：未配置任何模型", disabled: true }]);
        showNotice("模型不可用：未发现已配置的聊天模型，请检查 provider 配置。", "warn");
        return;
      }
      var activeRef = null;
      fillList(el.modelMenu, models.map(function (m) {
        var ref = m.ref || (m.provider + "/" + m.model);
        var label = m.model && m.provider ? m.model + " · " + m.provider : ref;
        if (m.active) activeRef = ref;
        return { label: label, ref: ref, active: !!m.active };
      }), pickModel);
      setValue(el.modelName, data.label || (activeRef || "").split("/").pop());
      setState(el.model, "ok");
      el.model.title = "当前模型：" + (activeRef || data.label || "");
    }).catch(function () {
      setValue(el.modelName, "不可用");
      setState(el.model, "error");
      el.model.title = "无法读取模型列表";
    });
  }

  // ── actions: reuse the CLI command surface (`POST /submit` intercepting
  // /model and /effort), so switching never forks argument semantics. ──
  function pickModel(item) {
    setState(el.model, "loading");
    postCommand("/model " + item.ref).then(loadModels).catch(function (err) {
      setState(el.model, "error");
      showNotice("切换模型失败：" + err.message, "error");
    });
  }

  function pickEffort(item) {
    setState(el.effort, "loading");
    postCommand("/effort " + item.level).then(loadModels).catch(function (err) {
      setState(el.effort, "error");
      showNotice("切换推理强度失败：" + err.message, "error");
    });
  }

  var setupPresets = [];

  function selectedSetupPreset() {
    var id = String(el.setupProvider && el.setupProvider.value || "");
    return setupPresets.find(function (preset) { return preset.id === id; });
  }

  function renderSetupModels() {
    var preset = selectedSetupPreset();
    clearNode(el.setupModel);
    if (!preset) return;
    (preset.models || []).forEach(function (model) {
      var option = document.createElement("option");
      option.value = model;
      option.textContent = model;
      option.selected = model === preset.defaultModel;
      el.setupModel.appendChild(option);
    });
    if (el.setupDescription) el.setupDescription.textContent = preset.description || "";
  }

  function setSetupStatus(message, failed) {
    if (!el.setupStatus) return;
    el.setupStatus.textContent = message || "";
    el.setupStatus.classList.toggle("is-error", !!failed);
  }

  function submitProviderSetup() {
    var preset = selectedSetupPreset();
    if (!preset || !el.setupModel || !el.setupKey) return;
    var request = {
      presetId: preset.id,
      defaultModel: el.setupModel.value,
      apiKey: el.setupKey.value
    };
    el.setupSubmit.disabled = true;
    setSetupStatus("正在保存并激活 Provider…", false);
    postJSON("/setup/providers", request).then(function () {
      el.setupKey.value = "";
      setSetupStatus("正在测试连接…", false);
      return postJSONResult("/setup/providers/test", { presetId: request.presetId, defaultModel: request.defaultModel });
    }).then(function (result) {
      setSetupStatus(result.message || "连接成功", false);
      setTimeout(function () {
        el.setup.hidden = true;
        refreshTasks();
        loadModels();
      }, 450);
    }).catch(function (err) {
      el.setupKey.value = "";
      setSetupStatus(cleanVisibleText(err.message || "保存或连接失败，请重试。"), true);
    }).finally(function () {
      el.setupSubmit.disabled = false;
    });
  }

  function initProviderSetup() {
    if (!el.setup || !el.setupProvider || !el.setupModel || !el.setupSubmit) return;
    getJSON("/setup/providers").then(function (data) {
      setupPresets = Array.isArray(data.presets) ? data.presets : [];
      var providers = Array.isArray(data.providers) ? data.providers : [];
      var activeName = String(data.activeModel || data.defaultModel || "").split("/")[0];
      var active = providers.find(function (provider) { return provider.name === activeName; });
      if (active && active.keyConfigured) return;
      clearNode(el.setupProvider);
      setupPresets.forEach(function (preset) {
        var option = document.createElement("option");
        option.value = preset.id;
        option.textContent = preset.label;
        el.setupProvider.appendChild(option);
      });
      if (!setupPresets.length) return;
      renderSetupModels();
      el.setup.hidden = false;
      el.setupProvider.addEventListener("change", renderSetupModels);
      el.setupSubmit.addEventListener("click", submitProviderSetup);
      el.setupKey.focus();
    }).catch(function () {
      // The setup surface is intentionally unavailable on non-loopback serve.
    });
  }

  function initSelectors() {
    if (!el.project || !el.model) return;
    el.model.addEventListener("click", function () { toggleMenu(el.modelMenu, el.model); });
    el.effort.addEventListener("click", function () { toggleMenu(el.effortMenu, el.effort); });
    el.branch.addEventListener("click", function () { toggleMenu(el.branchMenu, el.branch); });
    document.addEventListener("keydown", function (e) { if (e.key === "Escape") closeMenus(); });
    document.addEventListener("click", function (e) {
      if (openMenu && openMenu.parentElement && !openMenu.parentElement.contains(e.target)) closeMenus();
    });
    if (el.newTask) {
      // 创建任务后自动进入新会话：/new 在服务端完成会话切换，这里只负责刷新侧栏 (#406).
      el.newTask.addEventListener("click", function () {
        setState(el.newTask, "loading");
        postJSON("/new").then(refreshTasks).then(connectWorkspaceEvents).catch(function (err) {
          showNotice("创建任务失败：" + err.message, "error");
        }).finally(function () {
          setState(el.newTask, "ok");
        });
      });
    }
    refreshTasks();
    loadBranches();
    loadModels(); // effort arrives piggybacked on GET /models
  }

  initContextTabs();
  initComposer();
  initSessionFilters();
  initSelectors();
  initProviderSetup();
  if (el.historyRetry) el.historyRetry.addEventListener("click", connectWorkspaceEvents);
  connectWorkspaceEvents();

  // ── #404 side drawer (narrow viewports only) ──
  var sideToggle = document.querySelector("[data-ws-side-toggle]");
  var side = document.getElementById("ws-side");
  var sideScrim = document.querySelector("[data-ws-side-close]");
  if (!sideToggle || !side) return;

  function isNarrow() {
    return window.matchMedia("(max-width: 900px)").matches;
  }

  function setSideOpen(open) {
    var active = Boolean(open && isNarrow());
    document.body.classList.toggle("ws-side-open", active);
    sideToggle.setAttribute("aria-expanded", String(active));
    side.setAttribute("aria-hidden", String(isNarrow() && !active));
  }

  sideToggle.addEventListener("click", function () {
    setSideOpen(!document.body.classList.contains("ws-side-open"));
  });
  if (sideScrim) sideScrim.addEventListener("click", function () { setSideOpen(false); });
  document.addEventListener("keydown", function (event) {
    if (event.key === "Escape") setSideOpen(false);
  });
  window.addEventListener("resize", function () { setSideOpen(document.body.classList.contains("ws-side-open")); });
  setSideOpen(false);
})();
