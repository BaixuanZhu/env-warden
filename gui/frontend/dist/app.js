// env-warden GUI 前端 —— 原生 JS，无框架依赖。
// 只做渲染与交互；全部业务经 Wails 绑定调用 Go 侧 gui.App（core / internal/ops）。
"use strict";

const $ = (sel) => document.querySelector(sel);
let A; // window.go.gui.App，onload 后可用

// ---- 状态 ----
const state = {
  health: null,
  check: null,          // 最近一次 CheckView
  sel: new Set(),       // 勾选待清理的死条目原文（逐字）
  order: { user: null, system: null }, // 拖拽后的待保存顺序（可见条目数组）
  busy: false,
};
let statusTimer = null;

// ---- 启动 ----
window.addEventListener("DOMContentLoaded", async () => {
  A = window.go && window.go.gui && window.go.gui.App;
  if (!A) { $("#loading").textContent = "Wails 绑定不可用（请用 envwarden gui 启动）"; return; }
  bindToolbar();
  await refreshHealth();
  await runCheck(true);
});

// ---- 工具 ----
function setStatus(msg, kind) {
  const el = $("#status");
  el.textContent = msg || "";
  el.className = kind || "";
  clearTimeout(statusTimer);
  if (msg) statusTimer = setTimeout(() => { el.textContent = ""; el.className = ""; }, 8000);
}

function setBusy(b, msg) {
  state.busy = b;
  for (const btn of document.querySelectorAll("button")) btn.disabled = b;
  if (b) setStatus(msg || "处理中…");
}

function el(tag, cls, text) {
  const n = document.createElement(tag);
  if (cls) n.className = cls;
  if (text !== undefined) n.textContent = text;
  return n;
}

function scopeLabel(scope) { return scope === "system" ? "系统" : "用户"; }

function visibleList(scope) {
  const sv = state.check.scopes.find((s) => s.scope === scope);
  if (!sv) return [];
  const pending = state.order[scope];
  if (pending) return pending;
  return sv.entries.map((e) => e.entry);
}

function entryMeta(scope) {
  // entry 原文 -> 检测信息（存在性 / 展开后 / 原始位置）
  const sv = state.check.scopes.find((s) => s.scope === scope);
  const m = new Map();
  for (const e of sv.entries) m.set(e.entry, e);
  return m;
}

// ---- 顶栏与健康信息 ----
function bindToolbar() {
  $("#btnCheck").addEventListener("click", () => runCheck(false));
  $("#btnDiff").addEventListener("click", showDiff);
  $("#btnBackups").addEventListener("click", showBackups);
  $("#btnBackupNow").addEventListener("click", backupNow);
  $("#btnCleanSel").addEventListener("click", cleanSelected);
  $("#btnSelAll").addEventListener("click", () => {selectAllDead(true);});
  $("#btnSelNone").addEventListener("click", () => {selectAllDead(false);});
  $("#search").addEventListener("input", () => render());
}

async function refreshHealth() {
  try {
    state.health = await A.Health();
    $("#version").textContent = "v" + state.health.version;
    if (state.health.elevated) $("#elevBadge").classList.remove("hidden");
    $("#dirs").textContent =
      "备份: " + state.health.backupDir + " ｜ 快照: " + state.health.snapshotDir;
  } catch (e) {
    setStatus("读取启动信息失败: " + e, "err");
  }
}

// ---- 体检与渲染 ----
async function runCheck(first) {
  if (state.busy) return;
  setBusy(true, "体检中（读取注册表 + 展开检查）…");
  try {
    state.check = await A.RunCheck();
    state.sel.clear();
    state.order.user = null;
    state.order.system = null;
    render();
    $("#search").value = "";
    const n = state.check.problems;
    setStatus(
      first ? "体检完成" : "已重新体检",
      n === 0 ? "ok" : "err"
    );
    if (!first && n > 0) setTimeout(() => setStatus("发现 " + n + " 个问题（详见列表标红项）", "err"), 0);
    if (state.check.snapshotPath) {
      $("#dirs").textContent =
        "快照: " + state.check.snapshotPath + " ｜ 备份: " + state.health.backupDir;
    }
  } catch (e) {
    $("#main").innerHTML = "";
    $("#main").appendChild(el("div", "", "体检失败：" + e)).id = "loading";
    showError(e);
  } finally {
    setBusy(false);
  }
}

function render() {
  if (!state.check) return;
  const main = $("#main");
  main.innerHTML = "";
  const q = $("#search").value.trim().toLowerCase();

  for (const sv of state.check.scopes) main.appendChild(renderScope(sv, q));
  renderNpmBar();
  renderSelBar();
}

function renderScope(sv, q) {
  const card = el("section", "scope-card");
  const dirty = state.order[sv.scope] !== null;

  const head = el("div", "scope-head");
  const title = el("span", "scope-title", scopeLabel(sv.scope) + " PATH");
  const tag = el("span", "tag " + (sv.scope === "system" ? "sys" : "usr"),
    sv.scope === "system" ? "优先解析" : "后解析");
  title.appendChild(tag);
  head.appendChild(title);
  if (sv.dead > 0) head.appendChild(el("span", "scope-dead", sv.dead + " 条死路径"));
  head.appendChild(el("span", "scope-meta", sv.entries.length + " 条 · " + sv.kind));
  head.appendChild(el("span", "spacer"));
  if (q) head.appendChild(el("span", "dirty-tip", "搜索过滤中，拖拽已停用"));
  if (dirty) {
    head.appendChild(el("span", "dirty-tip", "顺序已修改，未保存"));
    const actions = el("span", "dirty-actions");
    const btnSave = el("button", "primary", "保存顺序");
    btnSave.addEventListener("click", () => saveOrder(sv.scope));
    const btnDrop = el("button", "", "放弃");
    btnDrop.addEventListener("click", () => { state.order[sv.scope] = null; render(); });
    actions.append(btnSave, btnDrop);
    head.appendChild(actions);
  }
  card.appendChild(head);

  const meta = entryMeta(sv.scope);
  const ordered = visibleList(sv.scope);
  ordered.forEach((entryText, i) => {
    const m = meta.get(entryText);
    card.appendChild(renderRow(sv.scope, entryText, m, i, q, dirty));
  });
  return card;
}

function renderRow(scope, entryText, m, idx, q, dirty) {
  const row = el("div", "entry" + (m && !m.exists ? " dead" : ""));
  row.draggable = !q; // 过滤中不允许拖拽，避免在子集上产生歧义顺序

  if (!q) {
    const handle = el("span", "handle", "⠿");
    handle.title = "拖拽调整顺序（PATH 顺序 = 命令解析优先级）";
    row.appendChild(handle);
  }
  const pos = el("span", "pos", String(idx + 1));
  pos.title = m ? "原始位置 #" + m.position : "";
  row.appendChild(pos);

  row.appendChild(el("span", "dot " + (m && m.exists ? "alive" : "dead")));
  if (m && !m.exists) {
    const pick = el("input", "pick");
    pick.type = "checkbox";
    pick.checked = state.sel.has(entryText);
    pick.addEventListener("change", () => {
      if (pick.checked) state.sel.add(entryText);
      else state.sel.delete(entryText);
      renderSelBar();
    });
    row.appendChild(pick);
  }

  const main = el("div", "entry-main");
  main.appendChild(el("div", "entry-text", entryText));
  if (m && m.expanded && m.expanded !== m.entry.trim()) {
    const sub = el("div", "entry-sub", "展开: " + m.expanded);
    main.appendChild(sub);
  }
  row.appendChild(main);

  if (m && m.duplicate) row.appendChild(el("span", "dup-badge", "重复"));

  if (q) {
    const hit = entryText.toLowerCase().includes(q) ||
      (m && m.expanded && m.expanded.toLowerCase().includes(q));
    if (!hit) row.classList.add("filtered");
  }

  if (!q) row.addEventListener("dragstart", (e) => {
    e.dataTransfer.setData("text/plain", String(idx));
    e.dataTransfer.effectAllowed = "move";
    row.classList.add("dragging");
  });
  row.addEventListener("dragend", () => {
    row.classList.remove("dragging");
    clearDragMarks();
  });
  row.addEventListener("dragover", (e) => {
    if (!row.draggable) return;
    e.preventDefault();
    row.classList.add("drag-over");
  });
  row.addEventListener("dragleave", () => row.classList.remove("drag-over"));
  row.addEventListener("drop", (e) => {
    e.preventDefault();
    if (!row.draggable) return;
    const from = parseInt(e.dataTransfer.getData("text/plain"), 10);
    row.classList.remove("drag-over");
    if (isNaN(from) || from === idx) return;
    const arr = visibleList(scope).slice();
    const [moved] = arr.splice(from, 1);
    // 统一"插入到目标条目之前"：从上往下拖时目标已因移除左移一位。
    const insertIdx = from < idx ? idx - 1 : idx;
    arr.splice(insertIdx, 0, moved);
    state.order[scope] = arr;
    render();
  });
  return row;
}

function clearDragMarks() {
  for (const n of document.querySelectorAll(".drag-over")) n.classList.remove("drag-over");
}

function renderNpmBar() {
  const npm = state.check.npm;
  const npmEl = $("#npmStatus");
  if (npm.status === "ok") {
    npmEl.className = "npm-status ok";
    npmEl.textContent = "npm prefix ✓ " + npm.prefix;
  } else if (npm.status === "notOnPath") {
    npmEl.className = "npm-status notOnPath";
    npmEl.textContent = "⚠ npm prefix 不在 PATH 上：" + npm.prefix + "（npm -g 装的工具将不可用）";
  } else {
    npmEl.className = "npm-status";
    npmEl.textContent = "npm prefix：无法确定（npm 未安装或异常）";
  }

  const cmds = state.check.commands || [];
  const missing = cmds.filter((c) => !c.found).map((c) => c.command);
  const sum = $("#cmdSummary");
  sum.innerHTML = "";
  sum.appendChild(document.createTextNode("工具链 " + (cmds.length - missing.length) + "/" + cmds.length + " 命中"));
  if (missing.length) {
    sum.appendChild(document.createTextNode("，缺失: "));
    sum.appendChild(el("span", "cmd-miss", missing.join(", ")));
  }
}

function renderSelBar() {
  const bar = $("#selBar");
  if (state.sel.size === 0) { bar.classList.add("hidden"); return; }
  bar.classList.remove("hidden");
  $("#selCount").textContent = "已选 " + state.sel.size + " 条死条目";
}

function selectAllDead(on) {
  state.sel.clear();
  if (on) {
    for (const sv of state.check.scopes) {
      for (const e of sv.entries) if (!e.exists) state.sel.add(e.entry);
    }
  }
  render();
}

// ---- 变更操作 ----
async function saveOrder(scope) {
  const ordered = state.order[scope];
  if (!ordered) return;
  const m = entryMeta(scope);
  const first = ordered[0];
  const confirmBody = el("div");
  confirmBody.appendChild(el("p", "hint",
    "将保存" + scopeLabel(scope) + "级 PATH 的新顺序（共 " + ordered.length + " 条，条目原文逐字不变，仅调整优先级）。"));
  confirmBody.appendChild(el("p", "hint", "第一条（最高优先级）：" + first));
  confirmBody.appendChild(el("p", "hint", "保存前会自动备份到 " + state.health.backupDir + "。"));
  showModal("保存顺序", confirmBody, [
    { label: "取消" },
    {
      label: "保存", cls: "primary",
      async onClick() {
        const res = await guard(async () => await A.Reorder(scope, ordered), "调整顺序");
        state.order[scope] = null;
        await runCheck(true);
        setStatus("顺序已保存；备份: " + res.backups.join(", "), "ok");
      },
    },
  ]);
}

async function cleanSelected() {
  const targets = [...state.sel];
  if (!targets.length) return;
  const byScope = { user: [], system: [] };
  for (const sv of state.check.scopes) {
    for (const e of sv.entries) {
      if (!e.exists && state.sel.has(e.entry)) byScope[sv.scope].push(e.entry);
    }
  }
  const body = el("div");
  body.appendChild(el("p", "hint", "以下 " + targets.length + " 条死条目将被精确移除（逐字节匹配，其余条目逐字保留）："));
  const ul = el("ul");
  for (const t of targets) ul.appendChild(el("li", "", t));
  body.appendChild(ul);
  body.appendChild(el("p", "hint", "移除前会强制自动备份到 " + state.health.backupDir + "。"));

  showModal("清理死条目", body, [
    { label: "取消" },
    {
      label: "确认清理", cls: "danger",
      async onClick() {
        const backs = [];
        for (const scope of ["system", "user"]) {
          if (!byScope[scope].length) continue;
          const res = await guard(async () => await A.Clean(scope, byScope[scope]), "清理" + scopeLabel(scope) + "级");
          backs.push(...res.backups);
        }
        await runCheck(true);
        setStatus("清理完成；备份: " + backs.join(", "), "ok");
      },
    },
  ]);
}

async function backupNow() {
  try {
    const paths = await guard(async () => await A.BackupNow(), "备份");
    setStatus("已备份: " + paths.join(", "), "ok");
  } catch (e) {
    showError(e);
  }
}

async function doRestore(item) {
  const body = el("div");
  body.appendChild(el("p", "hint",
    "将把" + scopeLabel(item.scope) + "级 Environment 整键写回该备份时刻的状态（" +
    new Date(item.at).toLocaleString() + "）。"));
  body.appendChild(el("p", "hint",
    "还原前会自动备份当前值；未出现在备份中的值保持不变；Path 值类型以备份为准。"));
  showModal("一键还原", body, [
    { label: "取消" },
    {
      label: "还原", cls: "danger",
      async onClick() {
        const res = await guard(async () => await A.Restore(item.path), "还原");
        await runCheck(true);
        setStatus("已还原；还原前备份: " + res.backups.join(", "), "ok");
      },
    },
  ]);
}

// ---- 弹窗：快照对比 ----
async function showDiff() {
  let d;
  try {
    d = await A.DiffLatest();
  } catch (e) {
    showError(e);
    return;
  }
  const body = el("div");
  body.appendChild(el("p", "hint",
    "基准 " + fmtTime(d.oldAt) + "（" + d.oldLabel + "） → 当前 " + fmtTime(d.newAt) +
    "；共 " + d.changes + " 处变化。"));

  let any = false;
  for (const s of d.scopes) {
    const block = el("div", "diff-block");
    block.appendChild(el("h4", "", scopeLabel(s.scope) + "级 PATH"));
    const empty = !s.added.length && !s.removed.length && !s.orderChanged && !s.kindChanged;
    if (empty) { block.appendChild(el("div", "diff-empty", "无变化")); body.appendChild(block); continue; }
    any = true;
    for (const a of s.added) block.appendChild(el("div", "diff-line add", "[新增] #" + a.position + " " + a.entry));
    for (const r of s.removed) block.appendChild(el("div", "diff-line rem", "[删除] #" + r.position + " " + r.entry));
    if (s.orderChanged) {
      block.appendChild(el("div", "diff-line", "[顺序] 命令解析优先级变化："));
      block.appendChild(el("div", "diff-order", "旧: " + s.oldOrder.join(" → ")));
      block.appendChild(el("div", "diff-order", "新: " + s.newOrder.join(" → ")));
    }
    if (s.kindChanged) {
      block.appendChild(el("div", "diff-line kind",
        "[类型] " + s.oldKind + " → " + s.newKind + "（%VAR% 展开行为已改变）"));
    }
    body.appendChild(block);
  }
  if (!any) body.appendChild(el("div", "diff-empty", "两次体检之间 PATH 无任何变化。"));
  showModal("上次体检以来 PATH 变了什么", body, [{ label: "关闭" }]);
}

function fmtTime(iso) {
  const d = new Date(iso);
  return isNaN(d) ? iso : d.toLocaleString();
}

// ---- 弹窗：备份与还原 ----
async function showBackups() {
  let items;
  try {
    items = await A.ListBackups();
  } catch (e) { showError(e); return; }
  const body = el("div");
  if (!items.length) {
    body.appendChild(el("p", "hint", "还没有备份。点「立即备份」或在清理 / 还原时自动生成（目录: " + state.health.backupDir + "）。"));
  } else {
    const table = el("table", "backups");
    const thead = el("thead");
    const hr = el("tr");
    for (const h of ["时间", "作用域", "文件", ""]) hr.appendChild(el("th", "", h));
    thead.appendChild(hr);
    table.appendChild(thead);
    const tbody = el("tbody");
    for (const it of items) {
      const tr = el("tr");
      tr.appendChild(el("td", "", new Date(it.at).toLocaleString()));
      tr.appendChild(el("td", "", scopeLabel(it.scope)));
      const td = el("td", "path", it.path);
      td.title = it.path;
      tr.appendChild(td);
      const act = el("td");
      const btn = el("button", "", "还原…");
      btn.addEventListener("click", () => { closeModal(); doRestore(it); });
      act.appendChild(btn);
      tr.appendChild(act);
      tbody.appendChild(tr);
    }
    table.appendChild(tbody);
    body.appendChild(table);
    body.appendChild(el("p", "hint", "还原前会自动备份当前值；也可双击 .reg 文件用 regedit 导入。"));
  }
  showModal("备份与还原", body, [{ label: "关闭" }]);
}

// ---- 弹窗与错误 ----
function showModal(title, bodyNode, buttons) {
  const mask = $("#modalMask");
  const modal = $("#modal");
  modal.innerHTML = "";
  const head = el("div", "modal-head");
  head.appendChild(el("span", "", title));
  const close = el("button", "modal-close", "✕");
  close.addEventListener("click", closeModal);
  head.appendChild(close);
  const body = el("div", "modal-body");
  if (typeof bodyNode === "string") body.innerHTML = bodyNode;
  else body.appendChild(bodyNode);
  const foot = el("div", "modal-foot");
  for (const b of buttons) {
    const btn = el("button", b.cls || "", b.label);
    if (b.onClick) btn.addEventListener("click", async () => {
      try { await b.onClick(); closeModal(); } catch (e) { closeModal(); showError(e); }
    });
    else btn.addEventListener("click", closeModal);
    foot.appendChild(btn);
  }
  modal.append(head, body, foot);
  mask.classList.remove("hidden");
}

function closeModal() { $("#modalMask").classList.add("hidden"); }
$("#modalMask").addEventListener("click", (e) => { if (e.target === e.currentTarget) closeModal(); });

function showError(err) {
  const msg = String(err && err.message || err);
  const body = el("div");
  body.appendChild(el("p", "hint", msg));
  const buttons = [{ label: "关闭" }];
  if (msg.includes("管理员")) {
    buttons.unshift({
      label: "以管理员重启", cls: "primary",
      async onClick() {
        await A.RelaunchElevated();
      },
    });
  }
  showModal("操作失败", body, buttons);
  setStatus(msg, "err");
}

// guard：统一的操作包装（忙碌态 + 错误弹窗），成功返回结果。
async function guard(fn, label) {
  setBusy(true, (label || "操作") + "中…");
  try {
    return await fn();
  } catch (e) {
    throw e; // 由调用方的 showModal onClick 统一弹错
  } finally {
    setBusy(false);
  }
}
