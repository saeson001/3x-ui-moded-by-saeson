package controller

// clientSyncHTML is the standalone client-sync UI page. It is registered on the
// /api router group, so it is served at /api/client-sync (auth required, same as
// the rest of the panel API). It calls /api/clients/export, /api/clients/import,
// /api/clients/diff and /api/clients/sorted to provide cross-VPS client
// migration, and a three-way diff view so two instances that already both have
// clients can be aligned by email and reconciled without touching DB primary
// keys.
const clientSyncHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>客户端同步 — 3x-ui</title>
<style>
*{box-sizing:border-box;margin:0;padding:0}
body{font-family:system-ui,-apple-system,"Microsoft YaHei",sans-serif;background:#f5f7fa;color:#1a1c1e;padding:24px}
h1{font-size:22px;margin-bottom:6px}
.sub{color:#6b7280;font-size:13px;margin-bottom:20px}
.card{background:#fff;border:1px solid #e2e5ea;border-radius:12px;padding:20px;margin-bottom:16px;box-shadow:0 1px 3px rgba(0,0,0,.04)}
.card h2{font-size:16px;margin-bottom:10px;display:flex;align-items:center;gap:8px}
.row{display:flex;gap:10px;align-items:center;flex-wrap:wrap}
button{padding:9px 18px;border:0;border-radius:8px;font-size:14px;cursor:pointer;font-weight:500}
.btn-p{background:#2563eb;color:#fff}.btn-p:hover{background:#1d4ed8}
.btn-s{background:#e5e7eb;color:#374151}.btn-s:hover{background:#d1d5db}
.btn-g{background:#059669;color:#fff}.btn-g:hover{background:#047857}
.btn-r{background:#dc2626;color:#fff}.btn-r:hover{background:#b91c1c}
.btn-sm{padding:6px 12px;font-size:13px}
input[type=file]{font-size:13px}
table{width:100%;border-collapse:collapse;font-size:13px}
th,td{padding:8px 10px;text-align:left;border-bottom:1px solid #e5e7eb}
th{background:#f9fafb;font-weight:600;color:#374151;position:sticky;top:0}
tbody tr:hover{background:#f9fafb}
.scroll{max-height:400px;overflow-y:auto;border-radius:8px;border:1px solid #e5e7eb}
.badge{display:inline-block;padding:2px 8px;border-radius:4px;font-size:11px;font-weight:600}
.badge-ok{background:#d1fae5;color:#065f46}
.badge-no{background:#fee2e2;color:#991b1b}
.msg{padding:10px 14px;border-radius:8px;margin:10px 0;font-size:13px}
.msg-ok{background:#d1fae5;color:#065f46;border:1px solid #a7f3d0}
.msg-err{background:#fee2e2;color:#991b1b;border:1px solid #fecaca}
.msg-info{background:#dbeafe;color:#1e40af;border:1px solid #bfdbfe}
code{background:#f3f4f6;padding:1px 6px;border-radius:4px;font-size:12px}
.tip{font-size:12px;color:#6b7280;margin-top:6px}
.tag{display:inline-block;background:#e0e7ff;color:#3730a3;padding:1px 6px;border-radius:3px;font-size:11px;margin-left:4px}
</style>
</head>
<body>
<h1>🔄 客户端同步工具</h1>
<p class="sub">跨 VPS 客户端导入导出 · 按邮箱排序对齐 · 3x-ui moded by saeson</p>
<p class="tip">访问地址：<code>面板地址/api/client-sync</code>（需已登录）</p>

<div class="card">
<h2>📤 导出客户端</h2>
<p class="tip">导出当前 VPS 的所有客户端（按邮箱排序），下载 JSON 文件后可在另一台 VPS 上导入。</p>
<div class="row" style="margin-top:10px">
<button class="btn-p" onclick="doExport()">导出客户端 JSON</button>
<span id="exportStatus" style="font-size:13px;color:#6b7280"></span>
</div>
<div id="exportResult" style="margin-top:10px"></div>
</div>

<div class="card">
<h2>📥 导入客户端</h2>
<p class="tip">上传从其他 VPS 导出的 JSON 文件。已存在的客户端（按邮箱匹配）会自动跳过，新客户端会创建并关联到同名入站。</p>
<div class="row" style="margin-top:10px">
<input type="file" id="importFile" accept=".json">
<button class="btn-g" onclick="doImport()">导入</button>
<button class="btn-s" onclick="document.getElementById('importFile').value=''">清空</button>
</div>
<div id="importResult" style="margin-top:10px"></div>
</div>

<div class="card">
<h2>🔍 对比对端（排序对齐 / 查差异）</h2>
<p class="tip">在另一台 VPS 的客户端同步页点「导出客户端 JSON」，把那个文件上传到这里，或直接粘贴 JSON。工具按邮箱对齐两边，列出三方差异，并能一键把对端多出来的用户导入本机。</p>
<div class="row" style="margin-top:10px">
<input type="file" id="diffFile" accept=".json">
<button class="btn-p" onclick="doDiff()">对比</button>
<button class="btn-s" onclick="document.getElementById('diffFile').value=''">清空</button>
</div>
<textarea id="diffPaste" placeholder="…也可以把对端的导出 JSON 直接粘贴到这里（上传文件优先）"
  style="width:100%;height:74px;margin-top:10px;font-family:ui-monospace,Menlo,Consolas,monospace;font-size:12px;border:1px solid #e2e5ea;border-radius:8px;padding:8px;resize:vertical"></textarea>
<div id="diffResult" style="margin-top:10px"></div>
</div>

<div class="card">
<h2>📋 当前客户端列表（按邮箱排序）</h2>
<p class="tip">此列表按邮箱字母序排列。两台 VPS 使用同一排序规则，相同用户名的客户端会出现在相同位置，方便对照比对。</p>
<div class="row" style="margin-top:10px">
<button class="btn-s btn-sm" onclick="loadSorted()">刷新列表</button>
<span id="sortedStatus" style="font-size:13px;color:#6b7280"></span>
</div>
<div id="sortedList" style="margin-top:10px"></div>
</div>

<div class="card">
<h2>📖 使用说明</h2>
<h3 style="font-size:13px;margin:10px 0 4px">场景一：新 VPS 直接复制客户端</h3>
<ol style="padding-left:20px;font-size:13px;line-height:1.8">
<li>在 VPS-A 面板点「导出客户端 JSON」，下载文件</li>
<li>在 VPS-B 面板上传该文件并点「导入」</li>
<li>VPS-B 需先建好与 VPS-A <b>同名的入站（Remark 一致）</b>，否则该客户端会被跳过</li>
<li>新导入的客户端按邮箱顺序追加，UUID / flow / 额度 / 到期时间原样保留</li>
</ol>
<h3 style="font-size:13px;margin:12px 0 4px">场景二：两台 VPS 都已有客户端，排序对齐 + 补齐差异</h3>
<ol style="padding-left:20px;font-size:13px;line-height:1.8">
<li>在 VPS-B 导出 JSON，回到 VPS-A 的「🔍 对比对端」上传它</li>
<li>工具按<b>邮箱</b>对齐两边，给出三方差异：只在本机 / 只在对端 / 两边都有</li>
<li>「两边都有」里 UUID 不一致 = 同邮箱但凭据不同，导入会被跳过，需人工核对</li>
<li>点「一键导入对端多出来的 N 个」把差异补齐；缺入站的客户端会先建同名入站再导入</li>
<li>反向操作同理：在 VPS-B 上传 VPS-A 的导出，即可把 A 多的用户同步到 B</li>
</ol>
<p class="tip" style="margin-top:8px">注意：面板自带的客户端列表按<b>入库顺序</b>（自增 ID）显示，无法按邮箱重排。本工具的对齐是在<b>对比视图</b>里做的，不改动数据库主键，不会破坏流量统计与关联关系。</p>
<p class="tip" style="margin-top:4px">API：<code>GET /api/clients/export</code> · <code>POST /api/clients/import</code> · <code>POST /api/clients/diff</code> · <code>GET /api/clients/sorted</code></p>
</div>

<script>
async function api(path, opts) {
  const res = await fetch(path, {
    ...opts,
    headers: opts && opts.headers ? opts.headers : {'Content-Type':'application/json'}
  });
  return res.json();
}

async function doExport() {
  const el = document.getElementById('exportStatus');
  const res = document.getElementById('exportResult');
  el.textContent = '正在导出...';
  res.innerHTML = '';
  try {
    const d = await api('/api/clients/export');
    if (!d.success) {
      el.textContent = '';
      res.innerHTML = '<div class="msg msg-err">' + (d.msg||'导出失败') + '</div>';
      return;
    }
    const count = d.obj ? d.obj.clientCount : 0;
    el.textContent = '已导出 ' + count + ' 个客户端';
    // Auto-download
    const blob = new Blob([JSON.stringify(d.obj, null, 2)], {type:'application/json'});
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = 'clients-export-' + new Date().toISOString().slice(0,10) + '.json';
    a.click();
    URL.revokeObjectURL(url);
    res.innerHTML = '<div class="msg msg-ok">✅ 已下载 clients-export.json（' + count + ' 个客户端）</div>';
  } catch(e) {
    el.textContent = '';
    res.innerHTML = '<div class="msg msg-err">网络错误: ' + e.message + '</div>';
  }
}

async function doImport() {
  const file = document.getElementById('importFile').files[0];
  const res = document.getElementById('importResult');
  if (!file) { res.innerHTML = '<div class="msg msg-err">请先选择 JSON 文件</div>'; return; }
  res.innerHTML = '<div class="msg msg-info">正在导入...</div>';
  try {
    const text = await file.text();
    const data = JSON.parse(text);
    const d = await api('/api/clients/import', {method:'POST', body: JSON.stringify(data)});
    if (!d.success) {
      res.innerHTML = '<div class="msg msg-err">' + (d.msg||'导入失败') + '</div>';
      return;
    }
    const created = d.obj ? d.obj.created : 0;
    const skipped = d.obj ? d.obj.skipped : 0;
    res.innerHTML = '<div class="msg msg-ok">✅ 导入完成：新建 ' + created + ' 个，跳过 ' + skipped + ' 个（已存在或无同名入站）</div>';
    loadSorted();
  } catch(e) {
    res.innerHTML = '<div class="msg msg-err">导入失败: ' + e.message + '</div>';
  }
}

async function loadSorted() {
  const el = document.getElementById('sortedStatus');
  const res = document.getElementById('sortedList');
  el.textContent = '加载中...';
  res.innerHTML = '';
  try {
    const d = await api('/api/clients/sorted');
    if (!d.success) {
      el.textContent = '';
      res.innerHTML = '<div class="msg msg-err">' + (d.msg||'获取失败') + '</div>';
      return;
    }
    const clients = d.obj || [];
    el.textContent = clients.length + ' 个客户端';
    if (clients.length === 0) {
      res.innerHTML = '<div class="msg msg-info">暂无客户端</div>';
      return;
    }
    let html = '<div class="scroll"><table><thead><tr><th>#</th><th>邮箱</th><th>UUID</th><th>状态</th><th>关联入站</th></tr></thead><tbody>';
    clients.forEach((c, i) => {
      const statusBadge = c.enable
        ? '<span class="badge badge-ok">启用</span>'
        : '<span class="badge badge-no">禁用</span>';
      const inbounds = (c.inboundNames||[]).map(n => '<span class="tag">' + n + '</span>').join('') || '<span style="color:#9ca3af">无</span>';
      const u = c.uuid || '';
      const uuidShort = u ? '<code title="' + u + '">' + u.slice(0,8) + '…</code>' : '<span style="color:#9ca3af">—</span>';
      html += '<tr><td>' + (i+1) + '</td><td>' + c.email + '</td><td>' + uuidShort + '</td><td>' + statusBadge + '</td><td>' + inbounds + '</td></tr>';
    });
    html += '</tbody></table></div>';
    res.innerHTML = html;
  } catch(e) {
    el.textContent = '';
    res.innerHTML = '<div class="msg msg-err">网络错误: ' + e.message + '</div>';
  }
}

async function diffText() {
  const file = document.getElementById('diffFile').files[0];
  if (file) return file.text();
  return document.getElementById('diffPaste').value.trim();
}

async function doDiff() {
  const res = document.getElementById('diffResult');
  let data;
  try {
    const text = await diffText();
    if (!text) { res.innerHTML = '<div class="msg msg-err">请先选择对端的导出 JSON 文件，或粘贴 JSON</div>'; return; }
    data = JSON.parse(text);
  } catch(e) { res.innerHTML = '<div class="msg msg-err">JSON 解析失败: ' + e.message + '</div>'; return; }
  res.innerHTML = '<div class="msg msg-info">正在对比...</div>';
  try {
    const d = await api('/api/clients/diff', {method:'POST', body: JSON.stringify(data)});
    if (!d.success) { res.innerHTML = '<div class="msg msg-err">' + (d.msg||'对比失败') + '</div>'; return; }
    renderDiff(d.obj);
  } catch(e) { res.innerHTML = '<div class="msg msg-err">网络错误: ' + e.message + '</div>'; }
}

function tags(arr) {
  return (arr||[]).map(n => '<span class="tag">' + n + '</span>').join('') || '<span style="color:#9ca3af">—</span>';
}

function onlyTable(title, rows, cls) {
  if (!rows.length) return '<p class="tip" style="margin-top:6px">' + title + '：无</p>';
  let h = '<details open><summary style="cursor:pointer;font-weight:600;font-size:13px;margin-top:8px">'
        + title + '（' + rows.length + '）</summary><div class="scroll" style="margin-top:6px"><table>'
        + '<thead><tr><th>#</th><th>邮箱</th><th>UUID</th><th>状态</th><th>关联入站</th></tr></thead><tbody>';
  rows.forEach((c, i) => {
    const badge = c.enable
      ? '<span class="badge badge-ok">启用</span>'
      : '<span class="badge badge-no">禁用</span>';
    const u = c.uuid || '';
    const us = u ? '<code title="' + u + '">' + u.slice(0,8) + '…</code>' : '<span style="color:#9ca3af">—</span>';
    h += '<tr><td>' + (i+1) + '</td><td>' + c.email + '</td><td>' + us + '</td><td>' + badge + '</td><td>' + tags(c.inboundNames) + '</td></tr>';
  });
  return h + '</tbody></table></div></details>';
}

function bothTable(rows) {
  if (!rows.length) return '<p class="tip" style="margin-top:6px">两边都有：无</p>';
  let h = '<details><summary style="cursor:pointer;font-weight:600;font-size:13px;margin-top:8px">两边都有（' + rows.length + '）</summary>'
        + '<div class="scroll" style="margin-top:6px"><table>'
        + '<thead><tr><th>#</th><th>邮箱</th><th>UUID 一致性</th><th>启用一致性</th><th>本机入站</th><th>对端入站</th></tr></thead><tbody>';
  rows.forEach((c, i) => {
    const um = c.uuidMatch
      ? '<span class="badge badge-ok">一致</span>'
      : '<span class="badge badge-no" title="同邮箱但凭据不同：本机 ' + (c.localUuid||'—').slice(0,8) + '… / 对端 ' + (c.remoteUuid||'—').slice(0,8) + '…">不一致</span>';
    const em = c.enableMatch
      ? '<span class="badge badge-ok">一致</span>'
      : '<span class="badge badge-no">本机' + (c.localEnable?'启用':'禁用') + ' / 对端' + (c.remoteEnable?'启用':'禁用') + '</span>';
    const miss = (c.missingInboundNames||[]).map(n => '<span class="tag" style="background:#fee2e2;color:#991b1b" title="本机没有这个入站（Remark），需先建同名入站才能导入">' + n + '缺</span>').join('');
    h += '<tr><td>' + (i+1) + '</td><td>' + c.email + '</td><td>' + um + '</td><td>' + em + '</td><td>' + tags(c.inboundNamesLocal) + '</td><td>' + tags(c.inboundNamesRemote) + miss + '</td></tr>';
  });
  return h + '</tbody></table></div></details>';
}

let lastDiff = null;
function renderDiff(o) {
  const res = document.getElementById('diffResult');
  lastDiff = o;
  let html = '<div class="msg msg-ok">本机 <b>' + o.localCount + '</b> 个 / 对端 <b>' + o.remoteCount + '</b> 个 / '
           + '两边都有 <b>' + (o.both||[]).length + '</b> / 只在本机 <b>' + (o.onlyLocal||[]).length + '</b> / '
           + '只在对端 <b>' + (o.onlyRemote||[]).length + '</b>'
           + (o.uuidMismatches ? ' / <b style="color:#b91c1c">UUID 不一致 ' + o.uuidMismatches + '</b>' : '') + '</div>';
  if ((o.missingInboundNames||[]).length) {
    html += '<div class="msg msg-err">本机缺少这些入站（Remark 名）：'
          + o.missingInboundNames.map(n => '<span class="tag" style="background:#fee2e2;color:#991b1b">' + n + '</span>').join(' ')
          + '。对端引用了这些入站的客户端无法导入，需先在本机建同名入站。</div>';
  }
  if ((o.onlyRemote||[]).length) {
    html += '<div class="row" style="margin-top:8px"><button class="btn-g btn-sm" onclick="importOnlyRemote()">📥 一键导入对端多出来的 ' + o.onlyRemote.length + ' 个</button>'
          + '<span id="diffImportStatus" style="font-size:13px;color:#6b7280"></span></div>';
  }
  // Primary view: merged table ordered as user specified — shared first (in
  // local panel order), then local-only (local order), then remote-only
  // (remote order). This is the "left side wins, extras at the end" layout.
  html += mergedTable(o.both||[], o.onlyLocal||[], o.onlyRemote||[]);
  // Detail drill-down, collapsed by default — only needed if the user wants to
  // inspect UUID/enable/inbound per-email drift between the two sides.
  html += '<details style="margin-top:10px"><summary style="cursor:pointer;font-weight:600;font-size:13px">🔎 详细对比（UUID / 启用 / 入站差异）</summary>';
  html += onlyTable('🟢 只在本机', o.onlyLocal||[]);
  html += onlyTable('🔵 只在对端（可导入本机）', o.onlyRemote||[]);
  html += bothTable(o.both||[]);
  html += '</details>';
  res.innerHTML = html;
}

function uuidShort(u) {
  u = u || '';
  if (!u) return '<span style="color:#9ca3af">—</span>';
  return '<code title="' + u + '">' + u.slice(0,8) + '…</code>';
}

function statusBadge(b) {
  return b ? '<span class="badge badge-ok">启用</span>' : '<span class="badge badge-no">禁用</span>';
}

// One row in the merged table. src controls the tag color and column content.
function mergedRow(i, c, src) {
  let email, uuid, enable, inbounds, tag;
  if (src === 'both') {
    email = c.email; uuid = c.localUuid || ''; enable = c.localEnable;
    inbounds = c.inboundNamesLocal;
    const um = c.uuidMatch
      ? '<span class="badge badge-ok" title="两台 UUID 一致">✓</span>'
      : '<span class="badge badge-no" title="UUID 不一致：本机 ' + (c.localUuid||'—').slice(0,8) + '… / 对端 ' + (c.remoteUuid||'—').slice(0,8) + '…">⚠</span>';
    tag = '<span class="badge" style="background:#d1fae5;color:#065f46">🟢 两边都有</span> ' + um;
  } else if (src === 'onlyLocal') {
    email = c.email; uuid = c.uuid || ''; enable = c.enable;
    inbounds = c.inboundNames;
    tag = '<span class="badge" style="background:#e5e7eb;color:#374151">⚪ 仅本地</span>';
  } else {
    email = c.email; uuid = c.uuid || ''; enable = c.enable;
    inbounds = c.inboundNames;
    tag = '<span class="badge" style="background:#fef3c7;color:#92400e">🟠 仅对端</span>';
  }
  return '<tr><td>' + (i+1) + '</td><td>' + email + '</td><td>' + uuidShort(uuid) + '</td><td>' + statusBadge(!!enable) + '</td><td>' + tags(inbounds) + '</td><td>' + tag + '</td></tr>';
}

// Group header rendered as a full-width row between sections so the three
// blocks are visually separated but still part of one continuous list.
function mergedGroupHeader(title, count, color) {
  return '<tr><td colspan="6" style="background:' + color + ';padding:6px 10px;font-size:12px;font-weight:600;color:#1f2937">'
       + title + '（' + count + '）</td></tr>';
}

function mergedTable(both, onlyLocal, onlyRemote) {
  const total = (both||[]).length + (onlyLocal||[]).length + (onlyRemote||[]).length;
  if (!total) return '<p class="tip" style="margin-top:6px">合并视图：无</p>';
  let idx = 0;
  let h = '<div style="margin-top:8px"><div style="font-weight:600;font-size:13px;margin-bottom:6px">📋 合并视图（本机顺序优先，独有项排末尾）— 共 ' + total + ' 条</div>'
        + '<div class="scroll" style="max-height:520px"><table>'
        + '<thead><tr><th style="width:40px">#</th><th>客户端</th><th>UUID</th><th>状态</th><th>关联入站</th><th>来源</th></tr></thead><tbody>';
  if ((both||[]).length) {
    h += mergedGroupHeader('① 两边都有（按本机顺序）', both.length, '#ecfdf5');
    both.forEach(c => { h += mergedRow(++idx, c, 'both'); });
  }
  if ((onlyLocal||[]).length) {
    h += mergedGroupHeader('② 仅本地（本机有、对端无）', onlyLocal.length, '#f3f4f6');
    onlyLocal.forEach(c => { h += mergedRow(++idx, c, 'onlyLocal'); });
  }
  if ((onlyRemote||[]).length) {
    h += mergedGroupHeader('③ 仅对端（对端有、本机无）', onlyRemote.length, '#fffbeb');
    onlyRemote.forEach(c => { h += mergedRow(++idx, c, 'onlyRemote'); });
  }
  h += '</tbody></table></div></div>';
  return h;
}

async function importOnlyRemote() {
  const st = document.getElementById('diffImportStatus');
  if (!lastDiff || !(lastDiff.onlyRemote||[]).length) { st.textContent = '无可导入项'; return; }
  const payload = {
    version: 1,
    exportedAt: Math.floor(Date.now()/1000),
    clients: lastDiff.onlyRemote.map(c => ({
      email: c.email, id: c.uuid || '', enable: !!c.enable, inboundNames: c.inboundNames || []
    }))
  };
  st.textContent = '导入中...';
  try {
    const d = await api('/api/clients/import', {method:'POST', body: JSON.stringify(payload)});
    if (!d.success) { st.textContent = ''; return; }
    st.textContent = '已导入 ' + (d.obj.created||0) + ' 个，跳过 ' + (d.obj.skipped||0) + ' 个'
      + (d.obj.noInbound ? '（其中 ' + d.obj.noInbound + ' 个因本机无同名入站）' : '');
    loadSorted();
  } catch(e) { st.textContent = '失败: ' + e.message; }
}

// Auto-load sorted list on page open
loadSorted();
</script>
</body>
</html>`
