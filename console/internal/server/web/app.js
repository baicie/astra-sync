const state = { jobs: [], total: 0, selectedName: null };

const elements = {
  body: document.querySelector("#jobs-body"),
  empty: document.querySelector("#empty-state"),
  error: document.querySelector("#list-error"),
  summary: document.querySelector("#summary"),
  updated: document.querySelector("#updated-label"),
  scope: document.querySelector("#scope-label"),
  placeholder: document.querySelector("#detail-placeholder"),
  detail: document.querySelector("#detail-content"),
  refresh: document.querySelector("#refresh-button"),
};

const stateLabels = {
  JOB_STATE_CREATED: "Created",
  JOB_STATE_INITIALIZING: "Initializing",
  JOB_STATE_RUNNING: "Running",
  JOB_STATE_CANCELING: "Canceling",
  JOB_STATE_CANCELED: "Canceled",
  JOB_STATE_FINISHED: "Finished",
  JOB_STATE_FAILED: "Failed",
};

const desiredLabels = {
  DESIRED_STATE_STOPPED: "Stopped",
  DESIRED_STATE_RUNNING: "Running",
};

async function getJSON(path) {
  const response = await fetch(path, { headers: { Accept: "application/json" } });
  if (!response.ok) throw new Error(await response.text() || `Request failed (${response.status})`);
  const scope = response.headers.get("X-Astra-Namespace");
  if (scope) elements.scope.textContent = scope;
  return response.json();
}

async function loadJobs() {
  elements.refresh.disabled = true;
  elements.error.hidden = true;
  elements.empty.hidden = true;
  elements.summary.textContent = "Loading...";
  try {
    const response = await getJSON("/api/jobs?page=1&page_size=50");
    state.jobs = response.items || [];
    state.total = response.total || 0;
    renderList(state.total);
    if (state.selectedName) {
      const selected = state.jobs.find((job) => job.name === state.selectedName);
      if (selected) renderDetail(selected);
    }
  } catch (error) {
    elements.body.innerHTML = "";
    elements.summary.textContent = "Unavailable";
    elements.error.textContent = error.message;
    elements.error.hidden = false;
  } finally {
    elements.refresh.disabled = false;
  }
}

function renderList(total) {
  elements.summary.textContent = `${total} ${total === 1 ? "job" : "jobs"}`;
  elements.updated.textContent = `Updated ${new Date().toLocaleTimeString()}`;
  elements.empty.hidden = state.jobs.length !== 0;
  elements.body.innerHTML = state.jobs.map((job) => {
    const status = job.status || {};
    const selected = job.name === state.selectedName ? " selected" : "";
    return `<tr class="${selected}" data-name="${escapeHTML(job.name)}">
      <td><span class="job-name">${escapeHTML(job.name)}</span><span class="job-namespace">${escapeHTML(job.namespace || "")}</span></td>
      <td>${badge(stateLabels[status.state] || "Unknown", status.state)}</td>
      <td>${desiredLabels[status.desiredState] || "Unknown"}</td>
      <td>${escapeHTML(`${job.spec?.source?.connector || "-"} -> ${job.spec?.sink?.connector || "-"}`)}</td>
      <td>v${job.version || 0}</td>
      <td>${formatTime(job.updatedAt)}</td>
    </tr>`;
  }).join("");
  elements.body.querySelectorAll("tr").forEach((row) => row.addEventListener("click", () => selectJob(row.dataset.name)));
}

async function selectJob(name) {
  state.selectedName = name;
  const job = state.jobs.find((candidate) => candidate.name === name);
  if (job) renderDetail(job);
  try {
    const fresh = await getJSON(`/api/jobs/${encodeURIComponent(name)}`);
    state.jobs = state.jobs.map((candidate) => candidate.name === name ? fresh : candidate);
    renderList(state.total);
    renderDetail(fresh);
  } catch (error) {
    showDetailError(error.message);
  }
}

async function loadStatus() {
  if (!state.selectedName) return;
  const button = document.querySelector("#status-refresh");
  if (button) button.disabled = true;
  try {
    const status = await getJSON(`/api/jobs/${encodeURIComponent(state.selectedName)}/status`);
    state.jobs = state.jobs.map((job) => job.name === state.selectedName ? { ...job, status } : job);
    renderList(state.total);
    renderDetail(state.jobs.find((job) => job.name === state.selectedName));
  } catch (error) {
    showDetailError(error.message);
  } finally {
    if (button) button.disabled = false;
  }
}

function renderDetail(job) {
  const status = job.status || {};
  elements.placeholder.hidden = true;
  elements.detail.hidden = false;
  const failure = status.failure ? `<div class="failure"><strong>${escapeHTML(status.failure.reason || "Execution failed")}</strong>${escapeHTML(status.failure.rootCause || "No root cause supplied")}</div>` : `<span class="muted">No failure recorded</span>`;
  elements.detail.innerHTML = `<div class="detail-header"><p class="eyebrow">JOB DETAIL</p><h3>${escapeHTML(job.name)}</h3><span class="detail-subtitle">${escapeHTML(job.namespace || "")} / UID ${escapeHTML(job.uid || "-")}</span><div style="margin-top:14px"><button class="button button-quiet" id="status-refresh" type="button">Refresh status</button></div></div>
    <div class="detail-section"><h4>Execution</h4><div class="detail-grid">
      ${detailValue("State", stateLabels[status.state] || "Unknown")}${detailValue("Desired", desiredLabels[status.desiredState] || "Unknown")}
      ${detailValue("Epoch", status.epoch || 0)}${detailValue("Restarts", status.restartCount || 0)}
      ${detailValue("Started", formatTime(status.startTime))}${detailValue("Ended", formatTime(status.endTime))}
    </div></div>
    <div class="detail-section"><h4>Pipeline</h4><div class="detail-grid">
      ${detailValue("Source", job.spec?.source?.connector || "-")}${detailValue("Sink", job.spec?.sink?.connector || "-")}
      ${detailValue("Delivery", job.spec?.delivery?.guarantee || "-")}${detailValue("Version", `v${job.version || 0}`)}
    </div></div>
    <div class="detail-section"><h4>Checkpoint / Failure</h4>${status.lastCheckpoint ? detailValue("Last checkpoint", `#${status.lastCheckpoint.id}`) : "<span class=\"muted\">No checkpoint recorded</span>"}<div style="height:12px"></div>${failure}</div>`;
  document.querySelector("#status-refresh").addEventListener("click", loadStatus);
}

function showDetailError(message) {
  elements.placeholder.hidden = true;
  elements.detail.hidden = false;
  elements.detail.innerHTML = `<div class="error-state">${escapeHTML(message)}</div>`;
}

function detailValue(label, value) { return `<div><div class="detail-label">${escapeHTML(label)}</div><div class="detail-value">${escapeHTML(String(value))}</div></div>`; }
function badge(label, state) { const kind = state === "JOB_STATE_RUNNING" ? "running" : state === "JOB_STATE_FAILED" ? "failed" : state === "JOB_STATE_INITIALIZING" || state === "JOB_STATE_CANCELING" ? "warning" : "neutral"; return `<span class="badge badge-${kind}">${escapeHTML(label)}</span>`; }
function formatTime(value) { return value ? new Date(value).toLocaleString() : "-"; }
function escapeHTML(value) { return String(value).replace(/[&<>'"]/g, (char) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", "'": "&#39;", '"': "&quot;" }[char])); }

elements.refresh.addEventListener("click", loadJobs);
loadJobs();
