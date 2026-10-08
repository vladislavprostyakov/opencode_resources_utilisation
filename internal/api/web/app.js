// Мониторинг утилизации ресурсов Kubernetes (frontend)

const state = {
  user: null,
  charts: {},   // podName -> { cpu, mem, storage, data: { cpu:[], mem:[], storage:[] } }
  pollTimer: null,
  metricsSource: null, // { active, available, error }
};

const content = document.getElementById("content");
const userSelect = document.getElementById("user");
const statusEl = document.getElementById("status");

// ---------- DOM helpers ----------
function h(tag, attrs = {}, children = []) {
  const node = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs)) {
    if (k === "class") node.className = v;
    else if (k === "text") node.textContent = v;
    else if (k.startsWith("on") && typeof v === "function") node.addEventListener(k.slice(2), v);
    else if (v !== null && v !== undefined) node.setAttribute(k, v);
  }
  for (const c of [].concat(children)) {
    if (c == null) continue;
    node.appendChild(typeof c === "string" ? document.createTextNode(c) : c);
  }
  return node;
}

function setStatus(msg, isError = false) {
  statusEl.textContent = msg;
  statusEl.className = isError ? "error" : "muted";
}

// ---------- parsing quantities ----------
function cpuToM(v) {
  if (!v) return 0;
  v = String(v).trim();
  if (v.endsWith("m")) return parseFloat(v);
  return parseFloat(v) * 1000;
}
function memToMi(v) {
  if (!v) return 0;
  const m = String(v).trim().match(/^([\d.]+)\s*([A-Za-z]*)$/);
  if (!m) return 0;
  const num = parseFloat(m[1]);
  const mult = { "": 1 / 1048576, Ki: 1 / 1024, Mi: 1, Gi: 1024, Ti: 1024 * 1024,
    K: 1e3 / 1048576, M: 1e6 / 1048576, G: 1e9 / 1048576, T: 1e12 / 1048576 }[m[2]] ?? 1;
  return num * mult;
}
// trimZeros — отбрасывает нулевую дробную часть: "1.00" -> "1", "1.50" -> "1.5".
function trimZeros(s) {
  if (!s.includes(".")) return s;
  return s.replace(/\.?0+$/, "");
}
// memToGiB — значение памяти в гигабайтах: округление до 2 знаков,
// нулевая дробная часть отбрасывается (1.00 -> 1, 1.50 -> 1.5, 1.23 -> 1.23).
function memToGiB(v) {
  return trimZeros((memToMi(v) / 1024).toFixed(2));
}
// isMemResource — байтовый ресурс (память/ephemeral), отображается в GB.
function isMemResource(res) {
  const r = String(res || "").toLowerCase();
  return r.includes("memory") || r.includes("ephemeral-storage");
}
// cpuToCores — значение CPU в ядрах: округление до 2 знаков,
// нулевая дробная часть отбрасывается (2.00 -> 2, 1.50 -> 1.5, 1.234 -> 1.23).
function cpuToCores(v) {
  if (!v) return "0 cores";
  const m = String(v).trim().match(/^([\d.]+)\s*cores?$/);
  if (!m) return String(v); // не распознано — оставляем как есть
  return trimZeros(parseFloat(m[1]).toFixed(2)) + " cores";
}
// isCpuResource — CPU-ресурс (для квоты).
function isCpuResource(res) {
  return String(res || "").toLowerCase().includes("cpu");
}
function pctClass(p) { return p >= 90 ? "bad" : p >= 70 ? "warn" : ""; }
function bar(p) {
  const p2 = Math.max(0, Math.min(100, p || 0));
  return h("div", { class: "bar" }, [h("span", { class: pctClass(p), style: `width:${p2}%` })]);
}

// ---------- API ----------
async function api(path) {
  const res = await fetch(path);
  if (!res.ok) throw new Error((await res.json().catch(() => ({}))).error || res.statusText);
  return res.json();
}

// ---------- metrics source ----------
async function loadMetricsSource() {
  try {
    state.metricsSource = await api("/api/metrics-source");
  } catch (e) {
    state.metricsSource = { active: "", available: [], error: e.message };
  }
}

function metricsSourceLabel() {
  const s = state.metricsSource;
  if (!s) return "";
  if (s.active) return ` — источник: ${s.active}`;
  if (s.error) return ` — метрики недоступны: ${s.error}`;
  return "";
}

// ---------- load users ----------
async function loadUsers() {
  try {
    const data = await api("/api/users");
    userSelect.innerHTML = "";
    if (!data.users || data.users.length === 0) {
      userSelect.appendChild(h("option", { value: "" }, "Пользователи не найдены"));
      userSelect.disabled = true;
      return;
    }
    userSelect.appendChild(h("option", { value: "" }, "— выберите пользователя —"));
    userSelect.appendChild(h("option", { value: "all" }, "Все пользователи"));
    for (const u of data.users) {
      userSelect.appendChild(h("option", { value: u }, u));
    }
    userSelect.disabled = false;
  } catch (e) {
    userSelect.appendChild(h("option", { value: "" }, "Ошибка: " + e.message));
    setStatus("Не удалось загрузить список пользователей: " + e.message, true);
  }
}

// ---------- select user ----------
userSelect.addEventListener("change", () => {
  const u = userSelect.value;
  if (!u) {
    stopPolling();
    content.innerHTML = '<p class="muted">Выберите пользователя, чтобы увидеть информацию об утилизации ресурсов.</p>';
    return;
  }
  loadUser(u);
});

async function loadUser(user) {
  stopPolling();
  content.innerHTML = '<p class="muted">Загрузка данных…</p>';
  try {
    const isAll = user === "all";
    const info = isAll
      ? await api("/api/users/all")
      : await api("/api/user/" + encodeURIComponent(user));
    state.user = user;
    await loadMetricsSource();
    if (isAll) {
      renderAllUsers(info);
    } else {
      renderUser(info);
      startPolling();
    }
  } catch (e) {
    content.innerHTML = "";
    content.appendChild(h("div", { class: "card" }, [h("p", { class: "error" }, "Ошибка: " + e.message)]));
  }
}

// ---------- render user ----------
function renderUser(info) {
  content.innerHTML = "";
  content.appendChild(renderClusterCard(info.cluster));
  content.appendChild(renderQuotaCard(info));
  content.appendChild(renderTotalsCard(info.totals));
  content.appendChild(renderPVCCard(info.pvcs));
  content.appendChild(renderPodsCard(info));
}

// ---------- render all users ----------
function renderAllUsers(info) {
  content.innerHTML = "";
  content.appendChild(renderClusterCard(info.cluster));
  content.appendChild(renderQuotaCard(info));
  content.appendChild(renderFreeCard(info.free));
  content.appendChild(renderTotalsCard(info.totals));
  content.appendChild(renderUsersTable(info.users));
}

function renderFreeCard(f) {
  if (!f) return null;
  const srcLabel = f.source === "quota"
    ? "от установленной квоты namespace"
    : "от доступных ресурсов кластера";
  return h("div", { class: "card" }, [
    h("h2", {}, `Свободные ресурсы (${srcLabel})`),
    h("div", { class: "grid" }, [
      metric("Свободно CPU", f.cpu ? cpuToCores(f.cpu) : "—"),
      metric("Свободно памяти", f.memory ? memToGiB(f.memory) + " GB" : "—"),
      metric("Свободно ephemeral storage", f.ephemeral ? memToGiB(f.ephemeral) + " GB" : "—"),
    ]),
  ]);
}

function renderUsersTable(users) {
  const card = h("div", { class: "card" }, [h("h2", {}, `Сводка по пользователям: ${users.length}`)]);
  if (!users.length) {
    card.appendChild(h("p", { class: "muted" }, "Пользователи не найдены."));
    return card;
  }
  const rows = users.map(u => h("tr", {}, [
    h("td", {}, u.user),
    h("td", {}, String(u.pod_count)),
    h("td", {}, cpuToCores(u.cpu_request)),
    h("td", {}, cpuToCores(u.cpu_limit)),
    h("td", {}, memToGiB(u.memory_request) + " GB"),
    h("td", {}, memToGiB(u.memory_limit) + " GB"),
    h("td", {}, memToGiB(u.ephemeral_storage_request) + " GB"),
    h("td", {}, memToGiB(u.ephemeral_storage_limit) + " GB"),
  ]));
  card.appendChild(h("table", {}, [
    h("thead", {}, h("tr", {}, ["Пользователь", "Подов", "CPU request", "CPU limit", "Memory request", "Memory limit", "Ephemeral request", "Ephemeral limit"].map(t => h("th", {}, t)))),
    h("tbody", {}, rows),
  ]));
  return card;
}

function renderClusterCard(c) {
  if (!c) return h("div", { class: "card" }, [h("h2", {}, "Кластер"), h("p", { class: "muted" }, "Данные о кластере недоступны.")]);
  return h("div", { class: "card" }, [
    h("h2", {}, `Кластер (нод: ${c.node_count})`),
    h("div", { class: "grid" }, [
      metric("Всего CPU (allocatable)", cpuToCores(c.total_cpu)),
      metric("Всего памяти (allocatable)", memToGiB(c.total_memory) + " GB"),
      metric("Зарезервировано CPU (requests)", cpuToCores(c.requested_cpu)),
      metric("Зарезервировано память (requests)", memToGiB(c.requested_memory) + " GB"),
      metric("Доступно CPU (после requests)", cpuToCores(c.available_cpu)),
      metric("Доступно памяти (после requests)", memToGiB(c.available_memory) + " GB"),
    ]),
  ]);
}

function metric(label, value, sub) {
  return h("div", { class: "metric" }, [
    h("div", { class: "label" }, label),
    h("div", { class: "value" }, value ?? "—"),
    sub ? h("div", { class: "sub" }, sub) : null,
  ]);
}

function renderQuotaCard(info) {
  const card = h("div", { class: "card" }, [h("h2", {}, `Квота namespace «${info.namespace}»`)]);
  const q = info.quota;
  if (!q || !q.items || q.items.length === 0) {
    card.appendChild(h("p", { class: "muted" }, "Квота для namespace не установлена."));
    return card;
  }
  const rows = q.items.map(it => {
    const mem = isMemResource(it.resource);
    const cpu = isCpuResource(it.resource);
    const fmt = (v) => (mem ? memToGiB(v) + " GB" : cpu ? cpuToCores(v) : v);
    return h("tr", {}, [
      h("td", {}, it.resource),
      h("td", {}, fmt(it.limit)),
      h("td", {}, fmt(it.used)),
      h("td", {}, fmt(it.remaining)),
      h("td", {}, [h("div", {}, `${(it.utilization_pct || 0).toFixed(1)}%`), bar(it.utilization_pct)]),
    ]);
  });
  card.appendChild(h("table", {}, [
    h("thead", {}, h("tr", {}, ["Ресурс", "Лимит", "Использовано", "Осталось", "Утилизация"].map(t => h("th", {}, t)))),
    h("tbody", {}, rows),
  ]));

  const up = info.quota_utilization_pct || {};
  const userRows = Object.entries(up).map(([res, p]) => h("tr", {}, [
    h("td", {}, res),
    h("td", {}, `${p.toFixed(1)}%`),
    h("td", {}, [bar(p)]),
  ]));
  if (userRows.length && info.user) {
    card.appendChild(h("h3", {}, `Утилизация квоты пользователем «${info.user}»`));
    card.appendChild(h("table", {}, [
      h("thead", {}, h("tr", {}, ["Ресурс", "Доля пользователя", ""].map(t => h("th", {}, t)))),
      h("tbody", {}, userRows),
    ]));
  }
  return card;
}

function renderTotalsCard(t) {
  return h("div", { class: "card" }, [
    h("h2", {}, `Суммарно по подам пользователя (подов: ${t.pod_count})`),
    h("div", { class: "grid" }, [
      metric("Memory request", memToGiB(t.memory_request) + " GB"),
      metric("Memory limit", memToGiB(t.memory_limit) + " GB"),
      metric("CPU request", cpuToCores(t.cpu_request)),
      metric("CPU limit", cpuToCores(t.cpu_limit)),
      metric("Ephemeral Storage request", memToGiB(t.ephemeral_storage_request) + " GB"),
      metric("Ephemeral Storage limit", memToGiB(t.ephemeral_storage_limit) + " GB"),
    ]),
  ]);
}

function renderPVCCard(pvcs) {
  const card = h("div", { class: "card" }, [h("h2", {}, `Persistent Volume Claims (PVC): ${pvcs.length}`)]);
  if (!pvcs.length) {
    card.appendChild(h("p", { class: "muted" }, "Пользователь не запросил persistent volumes."));
    return card;
  }
  const rows = pvcs.map(p => h("tr", {}, [
    h("td", {}, p.name),
    h("td", {}, p.status),
    h("td", {}, p.capacity ? memToGiB(p.capacity) + " GB" : "—"),
    h("td", {}, (p.access_modes || []).join(", ")),
    h("td", {}, p.storage_class || "—"),
    h("td", {}, p.usage ? memToGiB(p.usage) + " GB" : "—"),
    h("td", {}, p.utilization != null ? [`${p.utilization.toFixed(1)}%`, bar(p.utilization)] : "—"),
  ]));
  card.appendChild(h("table", {}, [
    h("thead", {}, h("tr", {}, ["Имя", "Статус", "Ёмкость", "Access modes", "Storage class", "Использовано*", "% утилизации"].map(t => h("th", {}, t)))),
    h("tbody", {}, rows),
  ]));
  card.appendChild(h("p", { class: "muted" }, "* Использование оценивается по ephemeral storage usage пода (приближённо)."));
  return card;
}

function renderPodsCard(info) {
  const card = h("div", { class: "card" }, [h("h2", {}, `Поды пользователя «${info.user}»: ${info.pods.length}`)]);
  if (!info.pods.length) {
    card.appendChild(h("p", { class: "muted" }, "Поды не найдены."));
    return card;
  }
  for (const pod of info.pods) card.appendChild(renderPod(pod));
  return card;
}

function renderPod(pod) {
  const badgeClass = pod.status === "Running" ? "running" : pod.status === "Pending" ? "pending" : "failed";
  const head = h("div", { class: "pod-head", onclick: () => togglePod(pod.name) }, [
    h("span", { class: "name" }, pod.name),
    h("span", { class: `badge ${badgeClass}` }, pod.status),
    h("span", { class: "badge" }, `CPU req/lim: ${cpuToCores(pod.cpu_request)} / ${cpuToCores(pod.cpu_limit)}`),
    h("span", { class: "badge" }, `Mem req/lim: ${memToGiB(pod.memory_request)} GB / ${memToGiB(pod.memory_limit)} GB`),
    h("span", { class: "badge" }, `Eph req/lim: ${memToGiB(pod.ephemeral_storage_request)} GB / ${memToGiB(pod.ephemeral_storage_limit)} GB`),
    h("span", { class: "badge" }, `Рестарты: ${pod.restart_count}`),
  ]);

  const body = h("div", { class: "pod-body" });
  body.appendChild(renderPodDetails(pod));
  body.appendChild(renderPodCharts(pod));
  body.appendChild(renderPodEvents(pod));
  body.appendChild(renderPodLogs(pod));

  const wrap = h("div", { class: "pod", id: "pod-" + pod.name }, [head, body]);
  return wrap;
}

function togglePod(name) {
  const el = document.getElementById("pod-" + name);
  if (!el) return;
  const open = el.classList.toggle("open");
  if (open) initPodCharts(name);
}

function renderPodDetails(pod) {
  const rows = [
    ["Создан", pod.created_at],
    ["Время работы (uptime)", pod.uptime],
    ["Нода", pod.node || "—"],
    ["Статус", pod.status],
    ["Всего перезапусков", String(pod.restart_count)],
  ];
  const table = h("table", {}, [
    h("tbody", {}, rows.map(([k, v]) => h("tr", {}, [h("td", { class: "muted" }, k), h("td", {}, v)]))),
  ]);
  const wrap = h("div", {}, [table]);

  if (pod.restart_reasons && pod.restart_reasons.length) {
    wrap.appendChild(h("ul", { class: "reasons" }, pod.restart_reasons.map(r => h("li", {}, r))));
  }

  if (pod.containers && pod.containers.length) {
    const crows = pod.containers.map(c => h("tr", {}, [
      h("td", {}, c.name),
      h("td", {}, c.image || "—"),
      h("td", {}, c.ready ? "yes" : "no"),
      h("td", {}, String(c.restart_count)),
      h("td", {}, c.state),
      h("td", {}, c.last_state || "—"),
      h("td", {}, c.last_state_reason || "—"),
    ]));
    wrap.appendChild(h("h3", {}, "Контейнеры"));
    wrap.appendChild(h("table", {}, [
      h("thead", {}, h("tr", {}, ["Имя", "Образ", "Ready", "Рестарты", "Состояние", "Последнее состояние", "Причина"].map(t => h("th", {}, t)))),
      h("tbody", {}, crows),
    ]));
  }
  return wrap;
}

// ---------- charts (live) ----------
function renderPodCharts(pod) {
  const wrap = h("div", {}, [h("h3", {}, `Графики утилизации (обновление каждые 5 сек)${metricsSourceLabel()}`)]);
  wrap.appendChild(h("p", { class: "muted", id: `metrics-status-${pod.name}`, style: "font-size:12px;margin:4px 0" }, ""));
  const grid = h("div", { class: "charts" }, [
    chartBox(pod.name, "cpu", "CPU, millicores"),
    chartBox(pod.name, "mem", "Память, GB"),
    chartBox(pod.name, "storage", "Ephemeral Storage, GB"),
  ]);
  wrap.appendChild(grid);
  return wrap;
}

function chartBox(podName, key, title) {
  return h("div", { class: "chart-box" }, [
    h("div", { class: "label muted", style: "font-size:12px;margin-bottom:6px" }, title),
    h("div", { class: "canvas-wrap" }, [h("canvas", { id: `chart-${podName}-${key}` })]),
  ]);
}

function makeChart(podName, key, color) {
  const canvas = document.getElementById(`chart-${podName}-${key}`);
  if (!canvas || typeof Chart === "undefined") return null;
  return new Chart(canvas, {
    type: "line",
    data: { labels: [], datasets: [{ label: key, data: [], borderColor: color, backgroundColor: color + "33", fill: true, tension: 0.3, pointRadius: 2 }] },
    options: {
      responsive: true,
      maintainAspectRatio: false,
      animation: false,
      plugins: { legend: { display: false } },
      scales: { x: { ticks: { maxTicksLimit: 6, color: "#8b98b0" } }, y: { beginAtZero: true, ticks: { color: "#8b98b0" } } },
    },
  });
}

function initPodCharts(podName) {
  if (state.charts[podName]) return;
  state.charts[podName] = {
    cpu: makeChart(podName, "cpu", "#4f9cff"),
    mem: makeChart(podName, "mem", "#3ecf8e"),
    storage: makeChart(podName, "storage", "#f5a623"),
    data: { cpu: [], mem: [], storage: [] },
  };
  pollPodMetrics(podName);
}

async function pollPodMetrics(podName) {
  const c = state.charts[podName];
  if (!c) return;
  try {
    const m = await api(`/api/user/${encodeURIComponent(state.user)}/pods/${encodeURIComponent(podName)}/metrics`);
    if (m.available === false) {
      if (m.error) console.warn(`[metrics] ${podName}: ${m.error}`);
      const el = document.getElementById(`metrics-status-${podName}`);
      if (el) el.textContent = "Метрики недоступны: " + (m.error || "нет источника");
      return;
    }
    const el = document.getElementById(`metrics-status-${podName}`);
    if (el) el.textContent = "";
    const label = new Date().toLocaleTimeString();
    const push = (arr, val) => {
      arr.push(val);
      if (arr.length > 60) arr.shift();
    };
    push(c.data.cpu, cpuToM(m.cpu));
    push(c.data.mem, memToMi(m.memory) / 1024);
    push(c.data.storage, memToMi(m.ephemeral_storage) / 1024);
    updateChart(c.cpu, label, c.data.cpu);
    updateChart(c.mem, label, c.data.mem);
    updateChart(c.storage, label, c.data.storage);
  } catch (e) { /* metrics недоступны — игнорируем */ }
}

function updateChart(chart, label, data) {
  if (!chart) return;
  chart.data.labels.push(label);
  chart.data.datasets[0].data = data.slice();
  if (chart.data.labels.length > 60) chart.data.labels.shift();
  chart.update("none");
}

// ---------- events ----------
function renderPodEvents(pod) {
  const wrap = h("div", {}, [h("h3", {}, `События кластера (${(pod.events || []).length})`)]);
  if (!pod.events || !pod.events.length) {
    wrap.appendChild(h("p", { class: "muted" }, "Событий нет."));
    return wrap;
  }
  const rows = pod.events.map(e => h("tr", {}, [
    h("td", {}, e.type),
    h("td", {}, e.reason),
    h("td", {}, e.message),
    h("td", {}, String(e.count)),
    h("td", {}, e.last_timestamp || "—"),
  ]));
  wrap.appendChild(h("table", {}, [
    h("thead", {}, h("tr", {}, ["Тип", "Причина", "Сообщение", "Кол-во", "Время"].map(t => h("th", {}, t)))),
    h("tbody", {}, rows),
  ]));
  return wrap;
}

// ---------- logs ----------
function renderPodLogs(pod) {
  const wrap = h("div", {}, [h("h3", {}, "Логи пода")]);
  const tailSel = h("select", {}, [
    h("option", { value: "100" }, "Последние 100 строк"),
    h("option", { value: "200", selected: "selected" }, "Последние 200 строк"),
    h("option", { value: "1000" }, "Последние 1000 строк"),
    h("option", { value: "10000" }, "Последние 10000 строк"),
  ]);
  const allChk = h("label", { style: "font-size:13px" }, [h("input", { type: "checkbox" }), " все контейнеры"]);
  const btn = h("button", {}, "Загрузить логи");
  const box = h("div", { class: "logs" }, "Нажмите «Загрузить логи».");
  btn.addEventListener("click", () => loadLogs(pod.name, tailSel.value, allChk.querySelector("input").checked, box));
  wrap.appendChild(h("div", { class: "logs-controls" }, [
    h("label", { style: "font-size:13px" }, "Количество строк:"),
    tailSel, allChk, btn,
  ]));
  wrap.appendChild(box);
  return wrap;
}

async function loadLogs(podName, tail, all, box) {
  box.textContent = "Загрузка…";
  try {
    const data = await api(`/api/user/${encodeURIComponent(state.user)}/pods/${encodeURIComponent(podName)}/logs?tail=${tail}&all=${all}`);
    box.textContent = data.logs || "(пусто)";
  } catch (e) {
    box.textContent = "Ошибка загрузки логов: " + e.message;
  }
}

// ---------- polling ----------
function startPolling() {
  stopPolling();
  state.pollTimer = setInterval(() => {
    for (const name of Object.keys(state.charts)) pollPodMetrics(name);
  }, 5000);
}

function stopPolling() {
  if (state.pollTimer) { clearInterval(state.pollTimer); state.pollTimer = null; }
  for (const [name, c] of Object.entries(state.charts)) {
    if (c.cpu) c.cpu.destroy();
    if (c.mem) c.mem.destroy();
    if (c.storage) c.storage.destroy();
  }
  state.charts = {};
}

// ---------- init ----------
loadUsers();


