const state = {
  session: null,
  tenantId: null,
  view: "jobs",
  jobs: [],
  jobTotal: 0,
  selectedJob: null,
  connections: [],
  selectedConnection: null,
  connectors: [],
  inventoryRevision: "",
  selectedConnector: null,
  activeTest: null,
  testTimer: null,
  auditEvents: [],
  auditNextPageToken: "",
  selectedAuditEvent: null,
};

const elements = {
  application: document.querySelector("#application"),
  authScreen: document.querySelector("#auth-screen"),
  tenant: document.querySelector("#tenant-select"),
  session: document.querySelector("#session-label"),
  refresh: document.querySelector("#refresh-button"),
  logout: document.querySelector("#logout-button"),
  tabs: [...document.querySelectorAll(".tab")],
  views: [...document.querySelectorAll("[data-view-panel]")],
  jobsBody: document.querySelector("#jobs-body"),
  jobsEmpty: document.querySelector("#jobs-empty"),
  jobsError: document.querySelector("#jobs-error"),
  jobsSummary: document.querySelector("#jobs-summary"),
  jobsUpdated: document.querySelector("#jobs-updated"),
  jobDetail: document.querySelector("#job-detail"),
  connectionsBody: document.querySelector("#connections-body"),
  connectionsEmpty: document.querySelector("#connections-empty"),
  connectionsError: document.querySelector("#connections-error"),
  connectionsSummary: document.querySelector("#connections-summary"),
  connectionDetail: document.querySelector("#connection-detail"),
  connectionState: document.querySelector("#connection-state-filter"),
  createConnection: document.querySelector("#create-connection-button"),
  connectorsList: document.querySelector("#connectors-list"),
  catalogEmpty: document.querySelector("#catalog-empty"),
  catalogError: document.querySelector("#catalog-error"),
  catalogSummary: document.querySelector("#catalog-summary"),
  inventoryRevision: document.querySelector("#inventory-revision"),
  connectorDetail: document.querySelector("#connector-detail"),
  auditTab: document.querySelector("#audit-tab"),
  auditBody: document.querySelector("#audit-body"),
  auditEmpty: document.querySelector("#audit-empty"),
  auditError: document.querySelector("#audit-error"),
  auditSummary: document.querySelector("#audit-summary"),
  auditDetail: document.querySelector("#audit-detail"),
  auditRange: document.querySelector("#audit-range-filter"),
  auditOutcome: document.querySelector("#audit-outcome-filter"),
  auditRefresh: document.querySelector("#audit-refresh-button"),
  auditLoadOlder: document.querySelector("#audit-load-older"),
  modal: document.querySelector("#modal"),
  modalEyebrow: document.querySelector("#modal-eyebrow"),
  modalTitle: document.querySelector("#modal-title"),
  modalBody: document.querySelector("#modal-body"),
  modalError: document.querySelector("#modal-error"),
  modalActions: document.querySelector("#modal-actions"),
  toast: document.querySelector("#toast"),
};

const jobStateLabels = {
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

const connectionStateLabels = {
  CONNECTION_STATE_ACTIVE: "Active",
  CONNECTION_STATE_DISABLED: "Disabled",
};

const compatibilityLabels = {
  CONNECTION_COMPATIBILITY_COMPATIBLE: "Compatible",
  CONNECTION_COMPATIBILITY_REVALIDATION_REQUIRED: "Revalidation required",
  CONNECTION_COMPATIBILITY_CONNECTOR_UNAVAILABLE: "Connector unavailable",
};

const auditOutcomeFilters = {
  changed: ["CHANGED"],
  "no-change": ["NO_CHANGE"],
  replayed: ["REPLAYED"],
  allowed: ["ALLOWED"],
  denied: ["DENIED", "INVALID_POLICY_INPUT", "INVALID_SCOPE", "PERMISSION_DENIED", "POLICY_STALE", "TENANT_DENIED", "UNAUTHENTICATED", "UNMAPPED_METHOD"],
};

const auditRangeMilliseconds = {
  "24h": 24 * 60 * 60 * 1000,
  "7d": 7 * 24 * 60 * 60 * 1000,
  "30d": 30 * 24 * 60 * 60 * 1000,
};

class APIError extends Error {
  constructor(message, status, code) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

async function api(path, options = {}) {
  const method = options.method || "GET";
  const headers = { Accept: "application/json", ...(options.headers || {}) };
  if (state.tenantId) headers["X-Astra-Tenant-ID"] = state.tenantId;
  const request = { method, headers, credentials: "same-origin" };
  if (options.body !== undefined) {
    headers["Content-Type"] = "application/json";
    headers["X-CSRF-Token"] = state.session?.csrfToken || "";
    headers["Idempotency-Key"] = options.idempotencyKey || crypto.randomUUID();
    request.body = JSON.stringify(options.body);
  }
  const response = await fetch(path, request);
  if (response.status === 204) return null;
  const contentType = response.headers.get("Content-Type") || "";
  const payload = contentType.includes("application/json") ? await response.json() : { error: await response.text() };
  if (!response.ok) {
    if (response.status === 401) showAuthentication();
    throw new APIError(payload.error || `Request failed (${response.status})`, response.status, payload.code || "UNKNOWN");
  }
  return payload;
}

async function initialize() {
  bindGlobalEvents();
  try {
    state.session = await api("/api/session");
  } catch (error) {
    if (error.status === 401) return;
    showAuthentication();
    return;
  }
  if (!state.session.tenants?.length) {
    elements.authScreen.hidden = false;
    elements.authScreen.querySelector("a").hidden = true;
    elements.authScreen.querySelector("h1").textContent = "No tenant access";
    return;
  }
  elements.application.hidden = false;
  elements.authScreen.hidden = true;
  elements.logout.hidden = state.session.authMode !== "oidc";
  elements.session.textContent = state.session.principalId || "Authenticated";
  renderTenantSelector();
  await loadTenantData();
}

function bindGlobalEvents() {
  elements.tabs.forEach((tab) => tab.addEventListener("click", () => switchView(tab.dataset.view)));
  elements.tenant.addEventListener("change", async () => {
    state.tenantId = elements.tenant.value;
    resetSelections();
    await loadTenantData();
  });
  elements.refresh.addEventListener("click", refreshCurrentView);
  elements.logout.addEventListener("click", logout);
  elements.connectionState.addEventListener("change", () => loadConnections());
  elements.createConnection.addEventListener("click", openCreateConnection);
  elements.auditRange.addEventListener("change", () => loadAudit(true));
  elements.auditOutcome.addEventListener("change", () => loadAudit(true));
  elements.auditRefresh.addEventListener("click", () => loadAudit(true));
  elements.auditLoadOlder.addEventListener("click", () => loadAudit(false));
  elements.modal.addEventListener("close", clearModal);
}

function showAuthentication() {
  clearTestPoll();
  elements.application.hidden = true;
  elements.authScreen.hidden = false;
}

function renderTenantSelector() {
  elements.tenant.innerHTML = state.session.tenants.map((tenant) =>
    `<option value="${escapeAttribute(tenant.id)}">${escapeHTML(tenant.displayName || tenant.namespace)}</option>`
  ).join("");
  state.tenantId = state.session.tenants[0].id;
  elements.tenant.value = state.tenantId;
}

function selectedTenant() {
  return state.session?.tenants?.find((tenant) => tenant.id === state.tenantId);
}

function hasPermission(permission) {
  return selectedTenant()?.permissions?.includes(permission) === true;
}

async function loadTenantData() {
  elements.refresh.disabled = true;
  elements.auditTab.hidden = !hasPermission("audit.read");
  if (state.view === "audit" && !hasPermission("audit.read")) switchView("jobs");
  const tasks = [loadJobs()];
  if (hasPermission("connectors.read")) tasks.push(loadCatalog());
  if (hasPermission("connections.read")) tasks.push(loadConnections());
  else {
    state.connections = [];
    renderConnections();
  }
  if (state.view === "audit" && hasPermission("audit.read")) tasks.push(loadAudit(true));
  await Promise.allSettled(tasks);
  elements.createConnection.hidden = !hasPermission("connections.create");
  elements.refresh.disabled = false;
}

async function refreshCurrentView() {
  elements.refresh.disabled = true;
  try {
    if (state.view === "jobs") await Promise.all([loadJobs(), ensureConnectionData()]);
    if (state.view === "connections") await Promise.all([loadConnections(), loadCatalog()]);
    if (state.view === "catalog") await loadCatalog();
    if (state.view === "audit") await loadAudit(true);
  } finally {
    elements.refresh.disabled = false;
  }
}

function switchView(view) {
  if (view === "audit" && !hasPermission("audit.read")) return;
  state.view = view;
  elements.tabs.forEach((tab) => tab.classList.toggle("active", tab.dataset.view === view));
  elements.views.forEach((panel) => { panel.hidden = panel.dataset.viewPanel !== view; });
  if (view === "audit" && state.auditEvents.length === 0) void loadAudit(true);
}

function resetSelections() {
  clearTestPoll();
  state.selectedJob = null;
  state.selectedConnection = null;
  state.selectedConnector = null;
  state.selectedAuditEvent = null;
  state.auditEvents = [];
  state.auditNextPageToken = "";
  elements.jobDetail.innerHTML = placeholder("Select a job");
  elements.connectionDetail.innerHTML = placeholder("Select a connection");
  elements.connectorDetail.innerHTML = placeholder("Select a connector");
  elements.auditDetail.innerHTML = placeholder("Select an event");
  renderAudit();
}

async function loadJobs() {
  setLoading(elements.jobsError, elements.jobsEmpty);
  try {
    const response = await api("/api/jobs?page=1&page_size=100");
    state.jobs = response.items || [];
    state.jobTotal = response.total || 0;
    renderJobs();
    if (state.selectedJob) {
      const selected = state.jobs.find((job) => job.name === state.selectedJob.name);
      if (selected) {
        state.selectedJob = selected;
        renderJobDetail(selected);
      }
    }
  } catch (error) {
    state.jobs = [];
    elements.jobsBody.innerHTML = "";
    showPanelError(elements.jobsError, error.message);
    elements.jobsSummary.textContent = "Unavailable";
  }
}

function renderJobs() {
  elements.jobsSummary.textContent = `${state.jobTotal} ${state.jobTotal === 1 ? "job" : "jobs"}`;
  elements.jobsUpdated.textContent = `Updated ${new Date().toLocaleTimeString()}`;
  elements.jobsEmpty.hidden = state.jobs.length !== 0;
  elements.jobsError.hidden = true;
  elements.jobsBody.innerHTML = state.jobs.map((job) => {
    const status = job.status || {};
    return `<tr data-name="${escapeAttribute(job.name)}" class="${state.selectedJob?.name === job.name ? "selected" : ""}">
      <td><span class="resource-name">${escapeHTML(job.name)}</span><span class="resource-subtitle">${escapeHTML(job.namespace || "")}</span></td>
      <td>${statusBadge(jobStateLabels[status.state] || "Unknown", status.state)}</td>
      <td>${escapeHTML(desiredLabels[status.desiredState] || "Unknown")}</td>
      <td>${escapeHTML(`${job.spec?.source?.connector || "-"} -> ${job.spec?.sink?.connector || "-"}`)}</td>
      <td>v${Number(job.version || 0)}</td><td>${formatTime(job.updatedAt)}</td></tr>`;
  }).join("");
  elements.jobsBody.querySelectorAll("tr").forEach((row) => row.addEventListener("click", () => selectJob(row.dataset.name)));
}

async function selectJob(name) {
  try {
    state.selectedJob = await api(`/api/jobs/${encodeURIComponent(name)}`);
    state.jobs = state.jobs.map((job) => job.name === name ? state.selectedJob : job);
    await ensureConnectionData();
    renderJobs();
    renderJobDetail(state.selectedJob);
  } catch (error) {
    elements.jobDetail.innerHTML = `<div class="error-state">${escapeHTML(error.message)}</div>`;
  }
}

function renderJobDetail(job) {
  const status = job.status || {};
  const source = endpointConnectionState(job.spec?.source, "CONNECTOR_ROLE_SOURCE");
  const sink = endpointConnectionState(job.spec?.sink, "CONNECTOR_ROLE_SINK");
  const actionButtons = [];
  if (hasPermission("jobs.update")) actionButtons.push(`<button class="button button-quiet" id="edit-job-button" type="button">Edit connections</button>`);
  if (status.desiredState !== "DESIRED_STATE_RUNNING" && hasPermission("jobs.start")) actionButtons.push(`<button class="button button-primary" id="start-job-button" type="button">Start</button>`);
  if (status.desiredState === "DESIRED_STATE_RUNNING" && hasPermission("jobs.stop")) actionButtons.push(`<button class="button button-danger" id="stop-job-button" type="button">Stop</button>`);
  const failure = status.failure ? `<div class="failure"><strong>${escapeHTML(status.failure.reason || "Execution failed")}</strong><span>${escapeHTML(status.failure.rootCause || "No root cause supplied")}</span></div>` : `<span class="muted">No failure recorded</span>`;
  elements.jobDetail.innerHTML = `<div class="detail-header"><p class="eyebrow">JOB</p><h2>${escapeHTML(job.name)}</h2><span class="detail-subtitle">UID ${escapeHTML(job.uid || "-")}</span><div class="detail-actions">${actionButtons.join("")}</div></div>
    <section class="detail-section"><h3>Execution</h3><div class="detail-grid">${detailValue("State", jobStateLabels[status.state] || "Unknown")}${detailValue("Desired", desiredLabels[status.desiredState] || "Unknown")}${detailValue("Epoch", status.epoch || 0)}${detailValue("Version", `v${job.version || 0}`)}${detailValue("Started", formatTime(status.startTime))}${detailValue("Updated", formatTime(job.updatedAt))}</div></section>
    <section class="detail-section"><h3>Connections</h3>${endpointRow("Source", job.spec?.source, source)}${endpointRow("Sink", job.spec?.sink, sink)}</section>
    <section class="detail-section"><h3>Failure</h3>${failure}</section>`;
  document.querySelector("#edit-job-button")?.addEventListener("click", () => openJobEditor(job));
  document.querySelector("#start-job-button")?.addEventListener("click", () => confirmJobState(job, "start"));
  document.querySelector("#stop-job-button")?.addEventListener("click", () => confirmJobState(job, "stop"));
}

function endpointRow(label, endpoint, availability) {
  const reference = endpoint?.connectionRef || "Not configured";
  const badge = availability.available ? `<span class="badge badge-success">Available</span>` :
    endpoint?.connectionRef ? `<span class="badge badge-danger">Blocking</span>` : `<span class="badge badge-neutral">None</span>`;
  return `<div class="endpoint-row"><div><span class="detail-label">${escapeHTML(label)} · ${escapeHTML(endpoint?.connector || "-")}</span><strong>${escapeHTML(reference)}</strong></div>${badge}</div>`;
}

function endpointConnectionState(endpoint, role) {
  if (!endpoint?.connectionRef) return { available: false, connection: null };
  const connection = state.connections.find((candidate) => candidate.name === endpoint.connectionRef);
  return { available: isConnectionChoice(connection, endpoint.connector, role), connection };
}

async function confirmJobState(job, action) {
  openModal({ eyebrow: "JOB ACTION", title: `${capitalize(action)} ${job.name}`,
    body: `<div class="confirmation"><strong>${escapeHTML(job.name)}</strong><span>Current version v${Number(job.version)}</span></div>`,
    primaryLabel: capitalize(action), danger: action === "stop", onSubmit: async () => {
      await api(`/api/jobs/${encodeURIComponent(job.name)}/${action}`, { method: "POST", body: { expectedVersion: Number(job.version) } });
      closeModal();
      showToast(`Job ${action} accepted`);
      await loadJobs();
      await selectJob(job.name);
    }});
}

function openJobEditor(job) {
  const source = job.spec?.source || {};
  const sink = job.spec?.sink || {};
  const sourceOptions = connectionSelectOptions(source.connector, "CONNECTOR_ROLE_SOURCE", source.connectionRef);
  const sinkOptions = connectionSelectOptions(sink.connector, "CONNECTOR_ROLE_SINK", sink.connectionRef);
  openModal({ eyebrow: "JOB", title: `Edit ${job.name}`,
    body: `<div class="form-grid"><div class="field"><label for="job-source-connection">Source connection</label><span class="field-meta">${escapeHTML(source.connector || "-")}</span><select id="job-source-connection">${sourceOptions}</select><div class="field-issue" id="source-connection-issue"></div></div>
      <div class="field"><label for="job-sink-connection">Sink connection</label><span class="field-meta">${escapeHTML(sink.connector || "-")}</span><select id="job-sink-connection">${sinkOptions}</select><div class="field-issue" id="sink-connection-issue"></div></div></div>`,
    primaryLabel: "Validate and save", onReady: updateJobConnectionIssues, onSubmit: async () => {
      const spec = structuredClone(job.spec || {});
      spec.source = { ...(spec.source || {}), connectionRef: document.querySelector("#job-source-connection").value };
      spec.sink = { ...(spec.sink || {}), connectionRef: document.querySelector("#job-sink-connection").value };
      updateJobConnectionIssues();
      if (document.querySelector(".field-issue.blocking")) throw new Error("Unavailable connection references must be replaced");
      const validation = await api(`/api/jobs/${encodeURIComponent(job.name)}/validate`, { method: "POST", body: { expectedVersion: Number(job.version), purpose: "UPDATE", spec } });
      if (!validation.valid) {
        const issues = (validation.issues || []).map((issue) => `${issue.fieldPath || "spec"}: ${issue.message || issue.code}`).join("\n");
        throw new Error(issues || "Job validation failed");
      }
      try {
        const updated = await api(`/api/jobs/${encodeURIComponent(job.name)}`, { method: "PUT", body: { expectedVersion: Number(job.version), spec } });
        closeModal();
        state.selectedJob = updated;
        showToast("Job connections updated");
        await loadJobs();
        await selectJob(job.name);
      } catch (error) {
        if (error.status === 409) {
          await selectJob(job.name);
          throw new Error("Job changed. The latest version has been loaded.");
        }
        throw error;
      }
    }});
  document.querySelector("#job-source-connection")?.addEventListener("change", updateJobConnectionIssues);
  document.querySelector("#job-sink-connection")?.addEventListener("change", updateJobConnectionIssues);
}

function connectionSelectOptions(connector, role, storedReference) {
  const choices = state.connections.filter((connection) => isConnectionChoice(connection, connector, role));
  const requirement = connectionRequirement(connector, role);
  const options = requirement !== "CONNECTION_REQUIREMENT_REQUIRED" ? [`<option value="">No connection</option>`] : [`<option value="">Select a connection</option>`];
  choices.forEach((connection) => options.push(`<option value="${escapeAttribute(connection.name)}" ${connection.name === storedReference ? "selected" : ""}>${escapeHTML(connection.displayName || connection.name)}</option>`));
  if (storedReference && !choices.some((connection) => connection.name === storedReference)) {
    options.push(`<option value="${escapeAttribute(storedReference)}" selected data-unavailable="true">${escapeHTML(storedReference)} · unavailable</option>`);
  }
  return options.join("");
}

function updateJobConnectionIssues() {
  for (const [selectId, issueId] of [["job-source-connection", "source-connection-issue"], ["job-sink-connection", "sink-connection-issue"]]) {
    const select = document.querySelector(`#${selectId}`);
    const issue = document.querySelector(`#${issueId}`);
    if (!select || !issue) continue;
    const unavailable = select.selectedOptions[0]?.dataset.unavailable === "true";
    issue.textContent = unavailable ? "Stored reference is unavailable" : "";
    issue.classList.toggle("blocking", unavailable);
  }
}

async function loadConnections() {
  if (!hasPermission("connections.read")) return;
  setLoading(elements.connectionsError, elements.connectionsEmpty);
  try {
    const query = new URLSearchParams({ page_size: "100" });
    const response = await api(`/api/connections?${query}`);
    state.connections = response.connections || [];
    renderConnections();
    if (state.selectedConnection) {
      const selected = state.connections.find((connection) => connection.name === state.selectedConnection.name);
      if (selected) {
        state.selectedConnection = selected;
        renderConnectionDetail(selected);
      }
    }
  } catch (error) {
    state.connections = [];
    elements.connectionsBody.innerHTML = "";
    showPanelError(elements.connectionsError, error.message);
    elements.connectionsSummary.textContent = "Unavailable";
  }
}

async function ensureConnectionData() {
  if (hasPermission("connections.read") && state.connections.length === 0) await loadConnections();
  if (hasPermission("connectors.read") && state.connectors.length === 0) await loadCatalog();
}

function renderConnections() {
  const filter = elements.connectionState.value;
  const visible = filter ? state.connections.filter((connection) => connection.state === `CONNECTION_STATE_${filter}`) : state.connections;
  elements.connectionsSummary.textContent = `${visible.length} ${visible.length === 1 ? "connection" : "connections"}`;
  elements.connectionsEmpty.hidden = visible.length !== 0;
  elements.connectionsError.hidden = true;
  elements.connectionsBody.innerHTML = visible.map((connection) => `<tr data-name="${escapeAttribute(connection.name)}" class="${state.selectedConnection?.name === connection.name ? "selected" : ""}">
    <td><span class="resource-name">${escapeHTML(connection.displayName || connection.name)}</span><span class="resource-subtitle">${escapeHTML(connection.name)}</span></td>
    <td>${escapeHTML(connection.connector || "-")}</td><td>${connectionBadge(connection.state)}</td><td>g${Number(connection.generation || 0)}</td>
    <td>${compatibilityBadge(connection.compatibility)}</td><td>${formatTime(connection.updatedAt)}</td></tr>`).join("");
  elements.connectionsBody.querySelectorAll("tr").forEach((row) => row.addEventListener("click", () => selectConnection(row.dataset.name)));
}

async function selectConnection(name) {
  clearTestPoll();
  try {
    state.selectedConnection = await api(`/api/connections/${encodeURIComponent(name)}`);
    state.connections = state.connections.map((connection) => connection.name === name ? state.selectedConnection : connection);
    renderConnections();
    renderConnectionDetail(state.selectedConnection);
  } catch (error) {
    elements.connectionDetail.innerHTML = `<div class="error-state">${escapeHTML(error.message)}</div>`;
  }
}

function renderConnectionDetail(connection) {
  const counts = connection.referenceCounts || {};
  const actions = [];
  if (hasPermission("connections.update")) actions.push(`<button class="button button-quiet" id="edit-connection-button" type="button">Edit</button>`);
  if (hasPermission("connections.rotate") && secretOptions(descriptorFor(connection.connector)).length) actions.push(`<button class="button button-quiet" id="rotate-connection-button" type="button">Rotate</button>`);
  if (connection.state === "CONNECTION_STATE_DISABLED" && hasPermission("connections.disable")) actions.push(`<button class="button button-primary" id="enable-connection-button" type="button">Enable</button>`);
  if (connection.state === "CONNECTION_STATE_ACTIVE" && hasPermission("connections.disable")) actions.push(`<button class="button button-danger" id="disable-connection-button" type="button">Disable</button>`);
  if (hasPermission("connections.test")) actions.push(`<button class="button button-quiet" id="test-connection-button" type="button">Test</button>`);
  if (hasPermission("connections.delete")) actions.push(`<button class="button button-danger" id="delete-connection-button" type="button">Delete</button>`);
  const settings = (connection.publicSettings || []).length ? `<dl class="settings-list">${connection.publicSettings.map((setting) => `<div><dt>${escapeHTML(setting.key)}</dt><dd>${escapeHTML(setting.value)}</dd></div>`).join("")}</dl>` : `<span class="muted">No public settings</span>`;
  const test = state.activeTest?.connectionUid === connection.uid ? state.activeTest : connection.lastTest;
  elements.connectionDetail.innerHTML = `<div class="detail-header"><p class="eyebrow">CONNECTION</p><h2>${escapeHTML(connection.displayName || connection.name)}</h2><span class="detail-subtitle">${escapeHTML(connection.name)} · UID ${escapeHTML(connection.uid || "-")}</span><div class="detail-actions">${actions.join("")}</div></div>
    <section class="detail-section"><h3>Status</h3><div class="detail-grid">${detailValue("State", connectionStateLabels[connection.state] || "Unknown")}${detailValue("Connector", connection.connector || "-")}${detailValue("Version", `v${connection.version || 0}`)}${detailValue("Generation", `g${connection.generation || 0}`)}${detailValue("Compatibility", compatibilityLabels[connection.compatibility] || "Unknown")}${detailValue("Secret", connection.secretConfigured ? "Configured" : "Not configured")}</div></section>
    <section class="detail-section"><h3>Settings</h3>${settings}</section>
    <section class="detail-section"><h3>References</h3><div class="detail-grid">${detailValue("Jobs", counts.jobs || 0)}${detailValue("Executions", counts.executions || 0)}${detailValue("Tests", counts.tests || 0)}${detailValue("Cleanup", counts.cleanupObligations || 0)}</div></section>
    <section class="detail-section"><h3>Latest test</h3>${renderTest(test)}</section>`;
  document.querySelector("#edit-connection-button")?.addEventListener("click", () => openEditConnection(connection));
  document.querySelector("#rotate-connection-button")?.addEventListener("click", () => openRotateConnection(connection));
  document.querySelector("#enable-connection-button")?.addEventListener("click", () => confirmConnectionAction(connection, "enable"));
  document.querySelector("#disable-connection-button")?.addEventListener("click", () => confirmConnectionAction(connection, "disable"));
  document.querySelector("#test-connection-button")?.addEventListener("click", () => startConnectionTest(connection));
  document.querySelector("#delete-connection-button")?.addEventListener("click", () => openDeleteConnection(connection));
}

function renderTest(test) {
  if (!test?.operationId) return `<span class="muted">No test recorded</span>`;
  const stateLabel = humanEnum(test.state || "Unknown");
  const kind = test.state === "CONNECTION_TEST_STATE_SUCCEEDED" ? "success" :
    ["CONNECTION_TEST_STATE_QUEUED", "CONNECTION_TEST_STATE_RUNNING"].includes(test.state) ? "warning" : "danger";
  return `<div class="test-result"><div>${statusBadge(stateLabel, `badge-${kind}`)}<span>${escapeHTML(humanEnum(test.phase || ""))}</span></div><span class="detail-subtitle">Generation ${Number(test.generation || 0)} · ${escapeHTML(test.resultCode ? humanEnum(test.resultCode) : "Pending")}</span></div>`;
}

function openCreateConnection() {
  if (!state.connectors.length) {
    showToast("Connector catalog is unavailable", true);
    return;
  }
  openModal({ eyebrow: "CONNECTION", title: "Create connection",
    body: `<div class="form-grid"><div class="field"><label for="connection-name">Name</label><input id="connection-name" maxlength="63" required /></div><div class="field"><label for="connection-connector">Connector</label><select id="connection-connector">${state.connectors.map((connector) => `<option value="${escapeAttribute(connector.name)}">${escapeHTML(connector.displayName || connector.name)}</option>`).join("")}</select></div><div class="field"><label for="connection-display-name">Display name</label><input id="connection-display-name" maxlength="256" /></div><div class="field field-wide"><label for="connection-description">Description</label><textarea id="connection-description" maxlength="2048"></textarea></div></div><div id="descriptor-connection-fields"></div>`,
    primaryLabel: "Create", onReady: renderCreateConnectionFields, onSubmit: async () => {
      const connector = document.querySelector("#connection-connector").value;
      const payload = { name: document.querySelector("#connection-name").value.trim(), connector,
        displayName: document.querySelector("#connection-display-name").value.trim(), description: document.querySelector("#connection-description").value.trim(),
        settings: collectConnectionSettings(), secretBinding: collectSecretBinding() };
      if (!payload.name) throw new Error("Connection name is required");
      if (!payload.secretBinding) delete payload.secretBinding;
      const created = await api("/api/connections", { method: "POST", body: payload });
      closeModal();
      showToast("Connection created in Disabled state");
      await loadConnections();
      await selectConnection(created.name);
    }});
  document.querySelector("#connection-connector")?.addEventListener("change", renderCreateConnectionFields);
}

function renderCreateConnectionFields() {
  const connector = document.querySelector("#connection-connector")?.value;
  const container = document.querySelector("#descriptor-connection-fields");
  if (!container) return;
  container.innerHTML = renderDescriptorConnectionFields(descriptorFor(connector), new Map(), true, false);
}

function openEditConnection(connection) {
  const values = new Map((connection.publicSettings || []).map((setting) => [setting.key, setting.value]));
  const locked = connection.state === "CONNECTION_STATE_ACTIVE";
  openModal({ eyebrow: "CONNECTION", title: `Edit ${connection.name}`,
    body: `<div class="form-grid"><div class="field"><label for="connection-display-name">Display name</label><input id="connection-display-name" maxlength="256" value="${escapeAttribute(connection.displayName || "")}" /></div><div class="field field-wide"><label for="connection-description">Description</label><textarea id="connection-description" maxlength="2048">${escapeHTML(connection.description || "")}</textarea></div></div>${renderDescriptorConnectionFields(descriptorFor(connection.connector), values, false, locked)}`,
    primaryLabel: "Save", onSubmit: async () => {
      try {
        const updated = await api(`/api/connections/${encodeURIComponent(connection.name)}`, { method: "PUT", body: {
          expectedVersion: Number(connection.version), displayName: document.querySelector("#connection-display-name").value.trim(),
          description: document.querySelector("#connection-description").value.trim(), settings: collectConnectionSettings(),
        }});
        closeModal();
        showToast("Connection updated");
        await loadConnections();
        await selectConnection(updated.name);
      } catch (error) {
        if (error.status === 409) {
          await selectConnection(connection.name);
          throw new Error("Connection changed. The latest version has been loaded.");
        }
        throw error;
      }
    }});
}

function renderDescriptorConnectionFields(descriptor, values, includeSecret, locked) {
  if (!descriptor) return `<div class="inline-error">Connector descriptor unavailable</div>`;
  const options = connectionOptions(descriptor);
  const publicOptions = options.filter((option) => option.sensitivity !== "CONNECTOR_OPTION_SENSITIVITY_SECRET");
  const secretFields = secretOptions(descriptor);
  const controls = publicOptions.map((option, index) => renderOptionControl(option, values.get(option.key), index, locked)).join("");
  const secret = includeSecret && secretFields.length ? `<div class="form-section"><div class="form-section-heading"><h3>Secret reference</h3><span class="configured-marker">Write only</span></div><div class="form-grid"><div class="field"><label for="secret-name">Kubernetes Secret name</label><input id="secret-name" autocomplete="off" maxlength="253" /></div><div class="field"><label for="secret-uid">Kubernetes Secret UID</label><input id="secret-uid" autocomplete="off" maxlength="256" /></div>${secretFields.map((option, index) => `<div class="field"><label for="secret-key-${index}">${escapeHTML(optionLabel(option))} key</label><input id="secret-key-${index}" data-secret-logical="${escapeAttribute(option.key)}" autocomplete="off" maxlength="253" /></div>`).join("")}</div></div>` : "";
  return `<div class="form-section"><div class="form-section-heading"><h3>Connection settings</h3>${locked ? `<span class="configured-marker">Disable to change</span>` : ""}</div><div class="form-grid">${controls || `<span class="muted">No public settings</span>`}</div></div>${secret}`;
}

function renderOptionControl(option, storedValue, index, locked) {
  const id = `connection-option-${index}`;
  const value = storedValue ?? option.defaultValue ?? "";
  const required = option.required ? "required" : "";
  const readOnly = locked ? "disabled" : "";
  let control;
  if (option.valueType === "CONNECTOR_OPTION_TYPE_BOOLEAN") {
    control = `<label class="toggle"><input id="${id}" type="checkbox" data-option-key="${escapeAttribute(option.key)}" data-option-type="boolean" ${String(value).toLowerCase() === "true" ? "checked" : ""} ${readOnly} /><span>${escapeHTML(optionLabel(option))}</span></label>`;
    return `<div class="field field-toggle">${control}<span class="field-meta">${escapeHTML(option.key)}</span></div>`;
  }
  if (option.valueType === "CONNECTOR_OPTION_TYPE_ENUM") {
    control = `<select id="${id}" data-option-key="${escapeAttribute(option.key)}" ${required} ${readOnly}><option value=""></option>${(option.enumValues || []).map((entry) => `<option value="${escapeAttribute(entry)}" ${entry === value ? "selected" : ""}>${escapeHTML(entry)}</option>`).join("")}</select>`;
  } else {
    const type = option.valueType === "CONNECTOR_OPTION_TYPE_INTEGER" ? "number" : "text";
    const bounds = `${option.minimum !== undefined ? `min="${Number(option.minimum)}"` : ""} ${option.maximum !== undefined ? `max="${Number(option.maximum)}"` : ""}`;
    control = `<input id="${id}" type="${type}" data-option-key="${escapeAttribute(option.key)}" value="${escapeAttribute(value)}" ${required} ${readOnly} ${bounds} />`;
  }
  return `<div class="field"><label for="${id}">${escapeHTML(optionLabel(option))}${option.required ? " *" : ""}</label><span class="field-meta">${escapeHTML(option.key)}</span>${control}</div>`;
}

function collectConnectionSettings() {
  return [...elements.modalBody.querySelectorAll("[data-option-key]")].map((control) => {
    const value = control.dataset.optionType === "boolean" ? String(control.checked) : control.value;
    return { key: control.dataset.optionKey, value };
  }).filter((setting) => setting.value !== "");
}

function collectSecretBinding() {
  const name = document.querySelector("#secret-name")?.value.trim() || "";
  const uid = document.querySelector("#secret-uid")?.value.trim() || "";
  const mappings = [...elements.modalBody.querySelectorAll("[data-secret-logical]")]
    .map((input) => ({ logicalField: input.dataset.secretLogical, secretKey: input.value.trim() }))
    .filter((mapping) => mapping.secretKey);
  if (!name && !uid && mappings.length === 0) return undefined;
  if (!name || !uid || mappings.length === 0) throw new Error("Secret name, UID, and field mappings are required");
  return { provider: "kubernetes", secretName: name, secretUid: uid, fields: mappings };
}

function openRotateConnection(connection) {
  const fields = secretOptions(descriptorFor(connection.connector));
  openModal({ eyebrow: "CREDENTIAL ROTATION", title: `Rotate ${connection.name}`,
    body: `<div class="form-grid"><div class="field"><label for="secret-name">Kubernetes Secret name</label><input id="secret-name" autocomplete="off" maxlength="253" /></div><div class="field"><label for="secret-uid">Kubernetes Secret UID</label><input id="secret-uid" autocomplete="off" maxlength="256" /></div>${fields.map((option, index) => `<div class="field"><label for="secret-key-${index}">${escapeHTML(optionLabel(option))} key</label><input id="secret-key-${index}" data-secret-logical="${escapeAttribute(option.key)}" autocomplete="off" maxlength="253" /></div>`).join("")}</div><label class="confirmation-check"><input id="rotation-confirm" type="checkbox" />Confirm generation rotation</label>`,
    primaryLabel: "Rotate", danger: true, onSubmit: async () => {
      if (!document.querySelector("#rotation-confirm").checked) throw new Error("Rotation confirmation is required");
      try {
        await api(`/api/connections/${encodeURIComponent(connection.name)}/rotate`, { method: "POST", body: { expectedVersion: Number(connection.version), secretBinding: collectSecretBinding() } });
        closeModal();
        showToast("Connection generation rotated");
        await loadConnections();
        await selectConnection(connection.name);
      } catch (error) {
        if (error.status === 409) {
          await selectConnection(connection.name);
          throw new Error("Connection changed. The latest version has been loaded.");
        }
        throw error;
      }
    }});
}

function confirmConnectionAction(connection, action) {
  openModal({ eyebrow: "CONNECTION ACTION", title: `${capitalize(action)} ${connection.name}`,
    body: `<div class="confirmation"><strong>${escapeHTML(connection.displayName || connection.name)}</strong><span>Generation ${Number(connection.generation)}</span></div><label class="confirmation-check"><input id="action-confirm" type="checkbox" />Confirm ${escapeHTML(action)}</label>`,
    primaryLabel: capitalize(action), danger: action === "disable", onSubmit: async () => {
      if (!document.querySelector("#action-confirm").checked) throw new Error("Confirmation is required");
      try {
        await api(`/api/connections/${encodeURIComponent(connection.name)}/${action}`, { method: "POST", body: { expectedVersion: Number(connection.version) } });
        closeModal();
        showToast(`Connection ${action}d`);
        await loadConnections();
        await selectConnection(connection.name);
      } catch (error) {
        if (error.status === 409) {
          await selectConnection(connection.name);
          throw new Error("Connection changed. The latest version has been loaded.");
        }
        throw error;
      }
    }});
}

function openDeleteConnection(connection) {
  openModal({ eyebrow: "DELETE CONNECTION", title: connection.name,
    body: `<div class="confirmation danger-copy"><strong>${escapeHTML(connection.displayName || connection.name)}</strong><span>${Number(connection.referenceCounts?.jobs || 0)} job references</span></div><div class="field"><label for="delete-confirm-name">Connection name</label><input id="delete-confirm-name" autocomplete="off" /></div>`,
    primaryLabel: "Delete", danger: true, onSubmit: async () => {
      if (document.querySelector("#delete-confirm-name").value !== connection.name) throw new Error("Connection name does not match");
      try {
        await api(`/api/connections/${encodeURIComponent(connection.name)}`, { method: "DELETE", body: { expectedVersion: Number(connection.version) } });
        closeModal();
        state.selectedConnection = null;
        elements.connectionDetail.innerHTML = placeholder("Select a connection");
        showToast("Connection deleted");
        await loadConnections();
      } catch (error) {
        if (error.status === 409) {
          await selectConnection(connection.name);
          throw new Error("Connection changed. The latest version has been loaded.");
        }
        throw error;
      }
    }});
}

async function startConnectionTest(connection) {
  try {
    state.activeTest = await api(`/api/connections/${encodeURIComponent(connection.name)}/test`, { method: "POST", body: { expectedVersion: Number(connection.version) } });
    renderConnectionDetail(connection);
    showToast("Connection test queued");
    pollConnectionTest(connection.name, state.activeTest.operationId);
  } catch (error) {
    if (error.status === 409) await selectConnection(connection.name);
    showToast(error.message, true);
  }
}

function pollConnectionTest(connectionName, operationId) {
  clearTestPoll();
  const poll = async () => {
    try {
      const result = await api(`/api/connection-tests/${encodeURIComponent(operationId)}`);
      state.activeTest = result;
      if (state.selectedConnection?.name === connectionName) renderConnectionDetail(state.selectedConnection);
      if (["CONNECTION_TEST_STATE_QUEUED", "CONNECTION_TEST_STATE_RUNNING"].includes(result.state)) {
        state.testTimer = window.setTimeout(poll, 1800);
      } else {
        showToast(`Connection test ${humanEnum(result.state).toLowerCase()}`);
        await loadConnections();
        if (state.selectedConnection?.name === connectionName) await selectConnection(connectionName);
      }
    } catch (error) {
      showToast(error.message, true);
    }
  };
  state.testTimer = window.setTimeout(poll, 1200);
}

function clearTestPoll() {
  if (state.testTimer) window.clearTimeout(state.testTimer);
  state.testTimer = null;
}

async function loadAudit(reset) {
  if (!hasPermission("audit.read")) return;
  if (!reset && !state.auditNextPageToken) return;
  setLoading(elements.auditError, elements.auditEmpty);
  elements.auditRefresh.disabled = true;
  elements.auditLoadOlder.disabled = true;
  try {
    const parameters = new URLSearchParams({ page_size: "50" });
    if (reset) {
      const range = auditRangeMilliseconds[elements.auditRange.value] || auditRangeMilliseconds["24h"];
      parameters.set("from", new Date(Date.now() - range).toISOString());
      const outcomes = auditOutcomeFilters[elements.auditOutcome.value] || [];
      outcomes.forEach((outcome) => parameters.append("outcome", outcome));
      state.selectedAuditEvent = null;
      elements.auditDetail.innerHTML = placeholder("Select an event");
    } else {
      parameters.set("page_token", state.auditNextPageToken);
    }
    const response = await api(`/api/audit-events?${parameters}`);
    const events = response.events || [];
    state.auditEvents = reset ? events : [...state.auditEvents, ...events];
    state.auditNextPageToken = response.nextPageToken || "";
    renderAudit();
  } catch (error) {
    if (reset) {
      state.auditEvents = [];
      state.auditNextPageToken = "";
      elements.auditBody.innerHTML = "";
      elements.auditSummary.textContent = "Unavailable";
    }
    showPanelError(elements.auditError, error.message);
    elements.auditLoadOlder.hidden = true;
  } finally {
    elements.auditRefresh.disabled = false;
    elements.auditLoadOlder.disabled = false;
  }
}

function renderAudit() {
  const count = state.auditEvents.length;
  elements.auditSummary.textContent = `${count} ${count === 1 ? "event" : "events"}`;
  elements.auditEmpty.hidden = count !== 0;
  elements.auditError.hidden = true;
  elements.auditLoadOlder.hidden = state.auditNextPageToken === "";
  elements.auditBody.innerHTML = state.auditEvents.map((event) => `<tr data-event-id="${escapeAttribute(event.eventId)}" class="${state.selectedAuditEvent?.eventId === event.eventId ? "selected" : ""}">
    <td>${escapeHTML(formatTime(event.occurredAt))}</td>
    <td><strong class="resource-name">${escapeHTML(auditEventLabel(event.eventType))}</strong><span class="resource-subtitle">${escapeHTML(event.eventType || "-")}</span></td>
    <td>${auditOutcomeBadge(event.outcome)}</td>
    <td>${escapeHTML(event.actorId || "-")}</td>
    <td>${escapeHTML(auditResource(event.attributes || {}))}</td>
  </tr>`).join("");
  elements.auditBody.querySelectorAll("tr").forEach((row) => row.addEventListener("click", () => selectAuditEvent(row.dataset.eventId)));
  if (state.selectedAuditEvent) {
    const selected = state.auditEvents.find((event) => event.eventId === state.selectedAuditEvent.eventId);
    if (selected) {
      state.selectedAuditEvent = selected;
      renderAuditDetail(selected);
    }
  }
}

function selectAuditEvent(eventId) {
  const event = state.auditEvents.find((candidate) => candidate.eventId === eventId);
  if (!event) return;
  state.selectedAuditEvent = event;
  renderAudit();
  renderAuditDetail(event);
}

function renderAuditDetail(event) {
  const attributes = Object.entries(event.attributes || {}).sort(([left], [right]) => left.localeCompare(right));
  elements.auditDetail.innerHTML = `<div class="detail-header">
    <p class="eyebrow">AUDIT EVENT</p>
    <h2>${escapeHTML(auditEventLabel(event.eventType))}</h2>
    <div class="detail-subtitle">${escapeHTML(event.eventType || "-")}</div>
    <div class="detail-actions">${auditOutcomeBadge(event.outcome)}</div>
  </div>
  <section class="detail-section"><h3>Identity</h3><div class="detail-grid">
    ${detailValue("Occurred", formatTime(event.occurredAt))}
    ${detailValue("Actor", event.actorId || "-")}
    ${detailValue("Event ID", event.eventId || "-")}
    ${detailValue("Request ID", event.requestId || "-")}
  </div></section>
  <section class="detail-section"><h3>Attributes</h3>${attributes.length ? `<dl class="settings-list">${attributes.map(([key, value]) => `<div><dt>${escapeHTML(humanize(key))}</dt><dd>${escapeHTML(value)}</dd></div>`).join("")}</dl>` : `<span class="detail-subtitle">No projected attributes</span>`}</section>`;
}

function auditEventLabel(value) {
  return value ? value.split(".").map(humanize).join(" ") : "Unknown event";
}

function auditResource(attributes) {
  if (attributes.namespace && attributes.name) return `${attributes.namespace}/${attributes.name}`;
  for (const key of ["name", "uid", "connectionUid", "jobUid", "operationId"]) {
    if (attributes[key]) return attributes[key];
  }
  return "-";
}

function auditOutcomeBadge(value) {
  const denied = ["DENIED", "INVALID_POLICY_INPUT", "INVALID_SCOPE", "PERMISSION_DENIED", "POLICY_STALE", "TENANT_DENIED", "UNAUTHENTICATED", "UNMAPPED_METHOD"];
  const kind = denied.includes(value) ? "badge-danger" : ["CHANGED", "ALLOWED"].includes(value) ? "badge-success" : value === "REPLAYED" ? "badge-warning" : "badge-neutral";
  return statusBadge(humanize(value || "Unknown"), kind);
}

async function loadCatalog() {
  if (!hasPermission("connectors.read")) return;
  setLoading(elements.catalogError, elements.catalogEmpty);
  try {
    const response = await api("/api/connectors?page_size=100");
    state.connectors = response.descriptors || [];
    state.inventoryRevision = response.inventoryRevision || "";
    renderCatalog();
    if (state.selectedConnector) {
      state.selectedConnector = descriptorFor(state.selectedConnector.name);
      if (state.selectedConnector) renderConnectorDetail(state.selectedConnector);
    }
  } catch (error) {
    state.connectors = [];
    elements.connectorsList.innerHTML = "";
    showPanelError(elements.catalogError, error.message);
    elements.catalogSummary.textContent = "Unavailable";
  }
}

function renderCatalog() {
  elements.catalogSummary.textContent = `${state.connectors.length} ${state.connectors.length === 1 ? "connector" : "connectors"}`;
  elements.inventoryRevision.textContent = state.inventoryRevision ? `Inventory ${shortRevision(state.inventoryRevision)}` : "";
  elements.catalogEmpty.hidden = state.connectors.length !== 0;
  elements.catalogError.hidden = true;
  elements.connectorsList.innerHTML = state.connectors.map((connector) => `<button type="button" class="connector-row ${state.selectedConnector?.name === connector.name ? "selected" : ""}" data-name="${escapeAttribute(connector.name)}"><span><strong>${escapeHTML(connector.displayName || connector.name)}</strong><small>${escapeHTML(connector.name)} · ${escapeHTML(connector.artifactVersion || "-")}</small></span><span class="role-tags">${(connector.roles || []).map((role) => `<span>${escapeHTML(humanEnum(role))}</span>`).join("")}</span></button>`).join("");
  elements.connectorsList.querySelectorAll("button").forEach((button) => button.addEventListener("click", () => selectConnector(button.dataset.name)));
}

async function selectConnector(name) {
  try {
    const response = await api(`/api/connectors/${encodeURIComponent(name)}`);
    state.selectedConnector = response.connectorDescriptor;
    state.connectors = state.connectors.map((connector) => connector.name === name ? state.selectedConnector : connector);
    renderCatalog();
    renderConnectorDetail(state.selectedConnector);
  } catch (error) {
    elements.connectorDetail.innerHTML = `<div class="error-state">${escapeHTML(error.message)}</div>`;
  }
}

function renderConnectorDetail(connector) {
  const jobOptions = (connector.options || []).filter((option) => option.owner === "CONNECTOR_OPTION_OWNER_JOB");
  const connectionFields = connectionOptions(connector);
  elements.connectorDetail.innerHTML = `<div class="detail-header"><p class="eyebrow">CONNECTOR</p><h2>${escapeHTML(connector.displayName || connector.name)}</h2><span class="detail-subtitle">${escapeHTML(connector.name)} · ${escapeHTML(connector.artifactVersion || "-")}</span></div>
    <section class="detail-section"><h3>Runtime contract</h3><div class="tag-list">${(connector.roles || []).map((value) => `<span>${escapeHTML(humanEnum(value))}</span>`).join("")}${(connector.executionModes || []).map((value) => `<span>${escapeHTML(humanEnum(value))}</span>`).join("")}</div><div class="revision-block"><span>Descriptor</span><code>${escapeHTML(shortRevision(connector.descriptorRevision || "-"))}</code><span>Connection schema</span><code>${escapeHTML(shortRevision(connector.connectionSchemaRevision || "-"))}</code></div></section>
    <section class="detail-section"><h3>Connection fields</h3>${renderOptionTable(connectionFields)}</section>
    <section class="detail-section"><h3>Job fields</h3>${renderOptionTable(jobOptions)}</section>
    <section class="detail-section"><h3>Capabilities</h3><div class="tag-list">${(connector.capabilities || []).map((value) => `<span>${escapeHTML(humanEnum(value))}</span>`).join("") || `<span>None</span>`}</div></section>`;
}

function renderOptionTable(options) {
  if (!options.length) return `<span class="muted">None</span>`;
  return `<div class="option-table">${options.map((option) => `<div><span><strong>${escapeHTML(optionLabel(option))}</strong><small>${escapeHTML(option.key)}</small></span><span>${escapeHTML(humanEnum(option.valueType))}</span><span>${option.required ? "Required" : "Optional"}</span><span>${escapeHTML(humanEnum(option.sensitivity))}</span></div>`).join("")}</div>`;
}

function descriptorFor(name) {
  return state.connectors.find((connector) => connector.name === name) || null;
}

function connectionOptions(descriptor) {
  return (descriptor?.options || []).filter((option) => option.owner === "CONNECTOR_OPTION_OWNER_CONNECTION");
}

function secretOptions(descriptor) {
  return connectionOptions(descriptor).filter((option) => option.sensitivity === "CONNECTOR_OPTION_SENSITIVITY_SECRET");
}

function connectionRequirement(connector, role) {
  const descriptor = descriptorFor(connector);
  return descriptor?.connectionRequirements?.find((entry) => entry.role === role)?.requirement || "CONNECTION_REQUIREMENT_OPTIONAL";
}

function isConnectionChoice(connection, connector, role) {
  if (!connection || connection.connector !== connector || connection.state !== "CONNECTION_STATE_ACTIVE" ||
      connection.compatibility !== "CONNECTION_COMPATIBILITY_COMPATIBLE") return false;
  const descriptor = descriptorFor(connector);
  return !descriptor || (descriptor.roles || []).includes(role);
}

function optionLabel(option) {
  const value = option.labelKey || option.key || "Option";
  return value.includes(".") ? humanize(value.split(".").at(-1)) : humanize(value);
}

function openModal({ eyebrow, title, body, primaryLabel, danger = false, onReady, onSubmit }) {
  elements.modalEyebrow.textContent = eyebrow;
  elements.modalTitle.textContent = title;
  elements.modalBody.innerHTML = body;
  elements.modalError.hidden = true;
  elements.modalError.textContent = "";
  elements.modalActions.innerHTML = `<button class="button button-quiet" id="modal-cancel" type="button">Cancel</button><button class="button ${danger ? "button-danger" : "button-primary"}" id="modal-primary" type="button">${escapeHTML(primaryLabel)}</button>`;
  document.querySelector("#modal-cancel").addEventListener("click", closeModal);
  document.querySelector("#modal-primary").addEventListener("click", async (event) => {
    event.currentTarget.disabled = true;
    elements.modalError.hidden = true;
    try {
      await onSubmit();
    } catch (error) {
      elements.modalError.textContent = error.message || "Request failed";
      elements.modalError.hidden = false;
    } finally {
      if (elements.modal.open) event.currentTarget.disabled = false;
    }
  });
  elements.modal.showModal();
  onReady?.();
}

function closeModal() {
  if (elements.modal.open) elements.modal.close();
}

function clearModal() {
  elements.modalBody.replaceChildren();
  elements.modalActions.replaceChildren();
  elements.modalError.textContent = "";
}

async function logout() {
  try {
    await api("/auth/logout", { method: "POST", body: {} });
  } finally {
    window.location.assign("/");
  }
}

let toastTimer;
function showToast(message, danger = false) {
  window.clearTimeout(toastTimer);
  elements.toast.textContent = message;
  elements.toast.classList.toggle("toast-danger", danger);
  elements.toast.hidden = false;
  toastTimer = window.setTimeout(() => { elements.toast.hidden = true; }, 3500);
}

function setLoading(errorElement, emptyElement) {
  errorElement.hidden = true;
  emptyElement.hidden = true;
}

function showPanelError(element, message) {
  element.textContent = message;
  element.hidden = false;
}

function placeholder(label) {
  return `<div class="detail-placeholder"><strong>${escapeHTML(label)}</strong></div>`;
}

function detailValue(label, value) {
  return `<div><span class="detail-label">${escapeHTML(label)}</span><strong class="detail-value">${escapeHTML(String(value))}</strong></div>`;
}

function connectionBadge(value) {
  return statusBadge(connectionStateLabels[value] || "Unknown", value === "CONNECTION_STATE_ACTIVE" ? "badge-success" : "badge-neutral");
}

function compatibilityBadge(value) {
  const kind = value === "CONNECTION_COMPATIBILITY_COMPATIBLE" ? "badge-success" : "badge-warning";
  return statusBadge(compatibilityLabels[value] || "Unknown", kind);
}

function statusBadge(label, stateOrClass) {
  let kind = stateOrClass;
  if (!String(kind).startsWith("badge-")) {
    kind = stateOrClass === "JOB_STATE_RUNNING" ? "badge-success" : stateOrClass === "JOB_STATE_FAILED" ? "badge-danger" :
      ["JOB_STATE_INITIALIZING", "JOB_STATE_CANCELING"].includes(stateOrClass) ? "badge-warning" : "badge-neutral";
  }
  return `<span class="badge ${escapeAttribute(kind)}">${escapeHTML(label)}</span>`;
}

function formatTime(value) {
  return value ? new Date(value).toLocaleString() : "-";
}

function shortRevision(value) {
  return value.length > 18 ? `${value.slice(0, 16)}...` : value;
}

function humanEnum(value) {
  if (!value) return "";
  const parts = String(value).split("_");
  const meaningful = parts.filter((part) => !["CONNECTOR", "CONNECTION", "OPTION", "TYPE", "SENSITIVITY", "STATE", "TEST", "RESULT", "CODE", "ROLE", "EXECUTION", "MODE", "CAPABILITY"].includes(part));
  return humanize((meaningful.length ? meaningful : parts).join(" "));
}

function humanize(value) {
  return String(value).replace(/[_-]+/g, " ").replace(/\b\w/g, (letter) => letter.toUpperCase());
}

function capitalize(value) {
  return value.charAt(0).toUpperCase() + value.slice(1);
}

function escapeHTML(value) {
  return String(value ?? "").replace(/[&<>'"]/g, (character) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", "'": "&#39;", '"': "&quot;" }[character]));
}

function escapeAttribute(value) {
  return escapeHTML(value).replace(/`/g, "&#96;");
}

initialize();
