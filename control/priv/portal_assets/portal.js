(() => {
  "use strict";

  const columns = [
    ["backlog", "Backlog"],
    ["ready", "Ready"],
    ["in_progress", "In progress"],
    ["review", "Review"],
    ["done", "Done"]
  ];
  const validViews = new Set(["board", "chat", "activity", "resources", "connections"]);
  const workspaceFocusTargets = {
    activity: ["[data-work-item-id]", "workItemId"],
    attention: ["[data-attention-id]", "attentionId"],
    resource: ["[data-resource-id]", "resourceId"],
    "resource-sync": ["[data-sync-resource-id]", "syncResourceId"],
    connection: ["[data-connection-id]", "connectionId"],
    "connection-check": ["[data-check-connection-id]", "checkConnectionId"]
  };

  const icons = {
    activity: '<svg viewBox="0 0 24 24"><path d="M3 12h4l3-8 4 16 3-8h4"/></svg>',
    agent: '<svg viewBox="0 0 24 24"><rect x="5" y="7" width="14" height="12" rx="2"/><path d="M9 11h.01M15 11h.01M8 15h8M12 7V4M9 4h6"/></svg>',
    alert: '<svg viewBox="0 0 24 24"><path d="M12 3 2 21h20L12 3Z"/><path d="M12 9v4M12 17h.01"/></svg>',
    arrowDown: '<svg viewBox="0 0 24 24"><path d="m6 9 6 6 6-6"/></svg>',
    arrowUp: '<svg viewBox="0 0 24 24"><path d="m18 15-6-6-6 6"/></svg>',
    branch: '<svg viewBox="0 0 24 24"><circle cx="6" cy="5" r="2"/><circle cx="18" cy="7" r="2"/><circle cx="6" cy="19" r="2"/><path d="M6 7v10M8 6h5a5 5 0 0 1 5 5v-2"/></svg>',
    check: '<svg viewBox="0 0 24 24"><path d="m5 12 4 4L19 6"/></svg>',
    ci: '<svg viewBox="0 0 24 24"><path d="M4 12a8 8 0 1 0 8-8"/><path d="M4 4v5h5"/></svg>',
    columns: '<svg viewBox="0 0 24 24"><rect x="3" y="4" width="7" height="16" rx="1"/><rect x="14" y="4" width="7" height="16" rx="1"/></svg>',
    connection: '<svg viewBox="0 0 24 24"><path d="M8 12h8M5 8h3v8H5a3 3 0 0 1-3-3v-2a3 3 0 0 1 3-3ZM19 8h-3v8h3a3 3 0 0 0 3-3v-2a3 3 0 0 0-3-3Z"/></svg>',
    external: '<svg viewBox="0 0 24 24"><path d="M14 5h5v5M10 14 19 5M19 13v6H5V5h6"/></svg>',
    logOut: '<svg viewBox="0 0 24 24"><path d="M10 5H5v14h5M14 8l4 4-4 4M8 12h10"/></svg>',
    play: '<svg viewBox="0 0 24 24"><path d="m8 5 11 7-11 7V5Z"/></svg>',
    plus: '<svg viewBox="0 0 24 24"><path d="M12 5v14M5 12h14"/></svg>',
    plug: '<svg viewBox="0 0 24 24"><path d="m8 12 8-8M14 2l8 8M3 21l6-6M2 14l8 8M14 10l-4 4"/></svg>',
    refresh: '<svg viewBox="0 0 24 24"><path d="M20 7v5h-5M4 17v-5h5"/><path d="M18.5 9A7 7 0 0 0 6 6.5L4 9M5.5 15A7 7 0 0 0 18 17.5l2-2.5"/></svg>',
    repository: '<svg viewBox="0 0 24 24"><path d="M5 3h13a1 1 0 0 1 1 1v16H6a2 2 0 0 1-2-2V4a1 1 0 0 1 1-1Z"/><path d="M8 3v17M12 7h4"/></svg>',
    review: '<svg viewBox="0 0 24 24"><path d="M4 5h16v12H8l-4 4V5Z"/><path d="M8 9h8M8 13h5"/></svg>',
    rotate: '<svg viewBox="0 0 24 24"><path d="M3 12a9 9 0 1 0 3-6.7L3 8"/><path d="M3 3v5h5"/></svg>',
    search: '<svg viewBox="0 0 24 24"><circle cx="11" cy="11" r="7"/><path d="m20 20-4-4"/></svg>',
    settings: '<svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.7 1.7 0 0 0 .3 1.9l.1.1-2.8 2.8-.1-.1a1.7 1.7 0 0 0-1.9-.3 1.7 1.7 0 0 0-1 1.6v.2h-4V21a1.7 1.7 0 0 0-1-1.6 1.7 1.7 0 0 0-1.9.3l-.1.1L4.2 17l.1-.1a1.7 1.7 0 0 0 .3-1.9A1.7 1.7 0 0 0 3 14H2.8v-4H3a1.7 1.7 0 0 0 1.6-1 1.7 1.7 0 0 0-.3-1.9L4.2 7 7 4.2l.1.1A1.7 1.7 0 0 0 9 4.6 1.7 1.7 0 0 0 10 3V2.8h4V3a1.7 1.7 0 0 0 1 1.6 1.7 1.7 0 0 0 1.9-.3l.1-.1L19.8 7l-.1.1a1.7 1.7 0 0 0-.3 1.9 1.7 1.7 0 0 0 1.6 1h.2v4H21a1.7 1.7 0 0 0-1.6 1Z"/></svg>',
    trash: '<svg viewBox="0 0 24 24"><path d="M4 7h16M9 7V4h6v3M7 7l1 13h8l1-13M10 11v5M14 11v5"/></svg>',
    user: '<svg viewBox="0 0 24 24"><circle cx="12" cy="8" r="4"/><path d="M4 21a8 8 0 0 1 16 0"/></svg>',
    x: '<svg viewBox="0 0 24 24"><path d="m6 6 12 12M18 6 6 18"/></svg>'
  };

  const state = {
    workspace: null,
    selectedProjectId: null,
    activeView: "board",
    filter: "all",
    query: "",
    activeWorkItemId: null,
    currentDetail: null,
    returnFocus: null,
    editingWorkItemId: null,
    editingResourceId: null,
    editingConnectionId: null,
    editingProjectId: null,
    workspaceRequestId: 0,
    detailRequestId: 0,
    mutationRequestId: 0,
    projectSwitchRequestId: 0,
    projectSwitchTargetId: null,
    backgroundRefreshPending: false,
    pendingOperations: new Set(),
    staleEditors: new Set(),
    persistentStaleEditors: new Set()
  };

  const $ = (selector, root = document) => root.querySelector(selector);
  const $$ = (selector, root = document) => Array.from(root.querySelectorAll(selector));
  const csrfToken = $("meta[name='csrf-token']")?.content || "";
  const chat = { scope: "project", runId: null, contextKey: null, requestId: 0, snapshot: null, drafts: new Map(), pending: new Set(), notices: new Map() };
  const chatMarkup = new WeakMap();
  let submitLockGeneration = 0;

  class PortalRequestError extends Error {
    constructor(message, code, causes = []) {
      super(message);
      this.code = code;
      this.causes = Array.isArray(causes) ? causes : [];
    }
  }

  function icon(name) {
    return icons[name] || icons.activity;
  }

  function injectIcons(root = document) {
    $$('[data-icon]', root).forEach((node) => {
      const name = node.dataset.icon.replace(/-([a-z])/g, (_, letter) => letter.toUpperCase());
      node.innerHTML = icon(name);
    });
  }

  function escapeHtml(value) {
    return String(value ?? "")
      .replaceAll("&", "&amp;")
      .replaceAll("<", "&lt;")
      .replaceAll(">", "&gt;")
      .replaceAll('"', "&quot;")
      .replaceAll("'", "&#039;");
  }

  function formatLabel(value) {
    if (value === "github") return "GitHub";
    if (value === "azure_devops") return "Azure DevOps";
    if (value === "ci") return "CI";
    return String(value || "unknown")
      .replaceAll("_", " ")
      .replace(/\b\w/g, (letter) => letter.toUpperCase());
  }

  function authLabel(value) {
    return value === "gh_cli" ? "GitHub CLI · HTTPS" : value === "entra_id" ? "Microsoft Entra ID" : formatLabel(value);
  }

  async function request(path, options = {}) {
    const response = await fetch(path, {
      credentials: "same-origin",
      headers: {
        Accept: "application/json",
        ...(options.body ? { "Content-Type": "application/json" } : {}),
        ...(options.method && options.method !== "GET" ? { "x-csrf-token": csrfToken } : {}),
        ...(options.headers || {})
      },
      ...options
    });

    if (response.status === 401) {
      window.location.assign("/portal/login");
      throw new PortalRequestError("Session expired", "unauthenticated");
    }

    const body = await response.json().catch(() => ({}));
    if (!response.ok) {
      const fields = body.error?.fields;
      const fieldMessage = fields
        ? Object.entries(fields)
            .map(([field, messages]) => `${formatLabel(field)} ${messages.join(", ")}`)
            .join(". ")
        : null;
      throw new PortalRequestError(
        fieldMessage || body.error?.message || `Request failed (${response.status})`,
        body.error?.code,
        body.error?.causes
      );
    }

    return body;
  }

  function beginMutation(dialog = null) {
    state.workspaceRequestId += 1;
    state.detailRequestId += 1;
    return {
      id: ++state.mutationRequestId,
      projectId: state.selectedProjectId,
      activeView: state.activeView,
      activeWorkItemId: state.activeWorkItemId,
      dialog
    };
  }

  function ownsMutation(owner) {
    return owner.id === state.mutationRequestId &&
      owner.projectId === state.selectedProjectId &&
      owner.activeView === state.activeView &&
      owner.activeWorkItemId === state.activeWorkItemId &&
      (!owner.dialog || owner.dialog.open);
  }

  function invalidateMutationContext() {
    state.mutationRequestId += 1;
  }

  function beginOperation(key) {
    if (state.pendingOperations.has(key)) return false;
    state.pendingOperations.add(key);
    return true;
  }

  function endOperation(key) {
    state.pendingOperations.delete(key);
  }

  function operationPending(key) {
    return state.pendingOperations.has(key);
  }

  function resourceOperationKey(resource) {
    return `resource:${resource.project_id}:${resource.id}`;
  }

  function projectSyncKey(projectId) {
    return `project-sync:${projectId}`;
  }

  function connectionOperationKey(connectionId) {
    return `connection:${connectionId}`;
  }

  function resourceOperationPending(resource) {
    return Boolean(resource && operationPending(resourceOperationKey(resource)));
  }

  function projectResourceOperationPending(project) {
    return Boolean(project?.resources.some(resourceOperationPending));
  }

  function projectSyncBusy(project) {
    return Boolean(project && (
      operationPending(projectSyncKey(project.id)) ||
      projectResourceOperationPending(project)
    ));
  }

  function resourceActionBusy(resource) {
    return Boolean(resource && (
      resourceOperationPending(resource) ||
      operationPending(projectSyncKey(resource.project_id))
    ));
  }

  function resourceEditorBlocked(resource) {
    const current = resource || selectedProject()?.resources.find((candidate) => candidate.id === state.editingResourceId);
    const resourceId = current?.id || state.editingResourceId;
    const projectId = current?.project_id || selectedProject()?.id;
    return Boolean(resourceId && (
      resourceActionBusy(current) ||
      state.staleEditors.has(`resource:${resourceId}`) ||
      state.persistentStaleEditors.has(`resource:${resourceId}`) ||
      state.staleEditors.has(`project:${projectId}`)
    ));
  }

  function connectionBusy(connectionId) {
    return Boolean(connectionId && operationPending(connectionOperationKey(connectionId)));
  }

  function connectionEditorBlocked(connectionId = state.editingConnectionId) {
    return connectionBusy(connectionId) ||
      state.staleEditors.has(`connection:${connectionId}`) ||
      state.persistentStaleEditors.has(`connection:${connectionId}`);
  }

  function captureWorkspaceFocus() {
    const active = document.activeElement;
    if (!(active instanceof Element) || !$(".workspace-content")?.contains(active)) return null;

    const card = active.closest(".work-card[data-id]");
    if (card) return { type: "card", id: card.dataset.id, action: active.dataset.move || null };

    for (const [type, [selector, key]] of Object.entries(workspaceFocusTargets)) {
      const element = active.closest(selector);
      if (element) return { type, id: element.dataset[key] };
    }

    return active.id ? { type: "id", id: active.id } : null;
  }

  function restoreWorkspaceFocus(focus) {
    if (!focus) return;
    let target = null;
    if (focus.type === "card") {
      const card = $$(".work-card[data-id]").find((element) => element.dataset.id === focus.id);
      target = focus.action && card
        ? $$('[data-move]', card).find((element) => element.dataset.move === focus.action)
        : card;
    } else if (focus.type === "id") {
      target = document.getElementById(focus.id);
    } else {
      const [selector, key] = workspaceFocusTargets[focus.type] || [];
      if (selector) target = $$(selector).find((element) => element.dataset[key] === focus.id);
    }
    if (target && !target.disabled) target.focus();
  }

  function restoreLostWorkspaceFocus(focus) {
    if (document.activeElement === document.body) restoreWorkspaceFocus(focus);
  }

  function projectSyncErrorMessage(error) {
    const causes = error.causes?.length ? error.causes : [{ code: error.code, count: 1 }];
    const guidance = {
      stale: ["stale resources", "Reload and retry."],
      state_conflict: ["state conflict", "Reload and resolve the changed project or resource."],
      forbidden: ["permission denied", "Grant the required provider permissions."],
      provider_unauthorized: ["authentication required", "Reauthenticate the provider connection."],
      provider_failure: ["provider failure", "Check provider availability and retry."]
    };
    const knownCauses = causes.filter((cause) => guidance[cause.code]);
    if (!knownCauses.length) return error.message;

    const summary = knownCauses
      .map((cause) => `${guidance[cause.code][0]}${cause.count > 1 ? ` (${cause.count})` : ""}`)
      .join(", ");
    const actions = [...new Set(knownCauses.map((cause) => guidance[cause.code][1]))].join(" ");
    return `Project sync failed: ${summary}. ${actions}`;
  }

  async function loadWorkspace({ silent = false, refreshDetail = true, refreshChat = true, projectId = state.selectedProjectId, resetChatContext = false } = {}) {
    const requestId = ++state.workspaceRequestId;
    if (!silent && !state.workspace) renderLoading();

    const query = projectId ? `?project_id=${encodeURIComponent(projectId)}` : "";
    let workspace;
    try {
      workspace = await request(`/portal/api/workspace${query}`);
    } catch (error) {
      if (requestId !== state.workspaceRequestId) return false;
      throw error;
    }
    if (requestId !== state.workspaceRequestId) return false;

    const focus = captureWorkspaceFocus();
    const resourceForm = $("#resource-form");
    const resourceEditor = $("#resource-dialog").open && state.editingResourceId
      ? { id: state.editingResourceId, version: Number(resourceForm.elements.version.value) }
      : null;
    const connectionForm = $("#connection-form");
    const connectionEditor = $("#connection-dialog").open && state.editingConnectionId
      ? { id: state.editingConnectionId, version: Number(connectionForm.elements.version.value) }
      : null;
    state.workspace = workspace;
    if (resetChatContext) {
      saveChatDraft();
      chat.scope = "project";
      chat.runId = null;
    }
    state.selectedProjectId = workspace.selected_project_id;
    state.staleEditors.clear();
    state.persistentStaleEditors.clear();
    const refreshedProject = workspace.projects.find((project) => project.id === workspace.selected_project_id);
    const refreshedResource = resourceEditor
      ? refreshedProject?.resources.find((resource) => resource.id === resourceEditor.id)
      : null;
    const refreshedConnection = connectionEditor
      ? workspace.connections.find((connection) => connection.id === connectionEditor.id)
      : null;
    if (resourceEditor && (!refreshedResource || refreshedResource.version !== resourceEditor.version)) {
      state.staleEditors.add(`resource:${resourceEditor.id}`);
    }
    if (connectionEditor && (!refreshedConnection || refreshedConnection.version !== connectionEditor.version)) {
      state.staleEditors.add(`connection:${connectionEditor.id}`);
    }
    syncLocation();
    renderWorkspace();
    if ($("#resource-dialog").open) {
      const selectedReference = resourceForm.elements.registered_external_ref.value || resourceForm.elements.external_ref.value;
      const selectedConnection = resourceForm.elements.connection_id.value;
      syncResourceReferenceControl(selectedReference);
      syncResourceConnectionControl(selectedConnection);
      reconcileResourceEditor(refreshedResource);
    }
    if ($("#connection-dialog").open) {
      reconcileConnectionEditor(refreshedConnection);
    }
    restoreWorkspaceFocus(focus);

    if (refreshDetail && state.activeWorkItemId && !dialogOpen()) {
      await openWorkItem(state.activeWorkItemId, { preserveDrawer: true });
    }
    if (state.activeView === "chat") {
      if (refreshChat) await loadChat({ silent: true });
      else prepareChatContext();
    }
    return true;
  }

  function selectedProject() {
    return state.workspace?.projects.find((project) => project.id === state.selectedProjectId) || null;
  }

  function selectedItem(id) {
    return selectedProject()?.work_items.find((item) => item.id === id) || null;
  }

  function renderLoading() {
    if (state.activeView === "board") {
      $("#kanban-board").innerHTML = columns
        .map(([key, label]) => `<section class="kanban-column"><header class="column-header"><div class="column-title"><span class="column-marker ${key}"></span>${label}</div></header><div class="column-cards"><div class="loading-line"></div><div class="loading-line"></div></div></section>`)
        .join("");
    }
  }

  function renderWorkspace() {
    renderProjectSwitcher();
    renderNavigation();
    const project = selectedProject();

    if (!project) {
      renderEmptyWorkspace();
      renderHealth();
      renderAttention();
      renderRuntimes();
      renderActivity();
      renderResourcesView();
      if (state.activeView === "connections") renderConnectionsView();
      return;
    }

    const archived = project.status === "archived";
    $("#new-item-button").disabled = archived;
    $("#resources-add-button").disabled = archived;
    reconcileProjectSyncButton();
    $("#project-settings-button").disabled = false;
    $("#project-key").textContent = project.key;
    $("#project-title").textContent = project.name;
    $("#project-description").textContent = project.description || "Engineering work, execution, and delivery.";
    $("#project-state").textContent = formatLabel(project.status);
    $("#project-state").className = `status-dot-label ${project.status}`;

    const open = project.work_items.filter((item) => item.status !== "done").length;
    const active = project.work_items.filter((item) => item.status === "in_progress").length;
    const review = project.work_items.filter((item) => item.status === "review").length;
    $("#metric-open").textContent = open;
    $("#metric-active").textContent = active;
    $("#metric-review").textContent = review;

    renderBoard(project.work_items);
    renderActivity();
    renderResourcesView();
    if (state.activeView === "connections") renderConnectionsView();
    renderHealth();
    renderAttention();
    renderRuntimes();
    $("#last-refreshed").textContent = new Intl.DateTimeFormat(undefined, { hour: "numeric", minute: "2-digit" }).format(new Date());
  }

  function renderEmptyWorkspace() {
    $("#project-key").textContent = "NO PROJECT";
    $("#project-title").textContent = "Engineering workspace";
    $("#project-description").textContent = "Create a project to connect work, repositories, agents, and CI.";
    $("#project-state").textContent = "Empty";
    $("#metric-open").textContent = "0";
    $("#metric-active").textContent = "0";
    $("#metric-review").textContent = "0";
    $("#new-item-button").disabled = true;
    $("#project-settings-button").disabled = true;
    $("#resources-add-button").disabled = true;
    $("#sync-project-button").disabled = true;
    $("#board-summary").textContent = "0 work items";
    $("#kanban-board").innerHTML = `<div class="empty-workspace"><span class="rail-icon">${icon("columns")}</span><h2>No projects yet</h2><p>Create the first project and start organizing engineering work.</p><button class="button button-primary" id="empty-new-project"><span data-icon="plus"></span><span>New project</span></button></div>`;
    $("#empty-new-project")?.addEventListener("click", () => openProjectDialog());
    injectIcons($("#kanban-board"));
  }

  function renderProjectSwitcher() {
    const select = $("#project-switcher");
    const projects = state.workspace?.projects || [];
    const active = projects.filter((project) => project.status === "active");
    const archived = projects.filter((project) => project.status === "archived");
    const options = [];
    if (active.length) options.push(`<optgroup label="Active">${active.map(projectOption).join("")}</optgroup>`);
    if (archived.length) options.push(`<optgroup label="Archived">${archived.map(projectOption).join("")}</optgroup>`);
    select.innerHTML = options.join("") || '<option value="">No projects</option>';
    select.value = state.selectedProjectId || "";
    select.disabled = projects.length === 0;
  }

  function projectOption(project) {
    return `<option value="${project.id}">${escapeHtml(project.name)} · ${escapeHtml(project.key)}</option>`;
  }

  function renderNavigation() {
    $$('[data-view-panel]').forEach((panel) => { panel.hidden = panel.dataset.viewPanel !== state.activeView; });
    $$('[data-view]').forEach((link) => link.classList.toggle("is-active", link.dataset.view === state.activeView));
    $(".search-control").hidden = state.activeView !== "board";
    $("#new-item-button").hidden = state.activeView !== "board";
    $(".workspace-content").classList.toggle("is-chat", state.activeView === "chat");
  }

  function visibleItems(items) {
    const query = state.query.toLowerCase().trim();

    return items.filter((item) => {
      const matchesQuery = !query || [item.key, item.title, item.description, item.repository, item.assignee?.name]
        .filter(Boolean)
        .some((value) => String(value).toLowerCase().includes(query));

      const matchesFilter =
        state.filter === "all" ||
        (state.filter === "assigned" && item.assignee?.type !== "unassigned") ||
        (state.filter === "attention" && needsAttention(item));

      return matchesQuery && matchesFilter;
    });
  }

  function needsAttention(item) {
    return item.blocked || item.ci_status === "failed" || item.review_status === "changes_requested" || ["waiting_for_input", "failed"].includes(item.execution?.state);
  }

  function renderBoard(items) {
    const visible = visibleItems(items);
    const readOnly = selectedProject()?.status === "archived";
    $("#board-summary").textContent = `${visible.length} work item${visible.length === 1 ? "" : "s"}`;

    $("#kanban-board").innerHTML = columns
      .map(([status, label]) => {
        const cards = visible.filter((item) => item.status === status);
        return `<section class="kanban-column" data-status="${status}"><header class="column-header"><div class="column-title"><span class="column-marker ${status}"></span>${label}</div><span class="column-count">${cards.length}</span></header><div class="column-cards">${cards.length ? cards.map((item, index) => renderCard(item, index, cards.length, readOnly)).join("") : '<div class="empty-column">No work</div>'}</div></section>`;
      })
      .join("");

    $$(".work-card").forEach((card) => bindCard(card, readOnly));
    if (!readOnly) $$(".kanban-column").forEach(bindColumnDrop);
  }

  function renderCard(item, index, count, readOnly) {
    const signals = [];
    if (item.external) signals.push(`<span class="signal external">${icon("external")}${escapeHtml(formatLabel(item.external.provider))} #${escapeHtml(item.external.id)}</span>`);
    if (item.blocked) signals.push(`<span class="signal blocked">${icon("alert")}Blocked</span>`);
    if (item.execution?.state) signals.push(`<span class="signal agent">${icon("agent")}${formatLabel(item.execution.state)}</span>`);
    if (item.pull_request_url) signals.push(`<span class="signal">${icon("branch")}PR</span>`);
    if (item.ci_status === "passed") signals.push(`<span class="signal ci-passed">${icon("check")}CI</span>`);
    if (item.ci_status === "failed") signals.push(`<span class="signal ci-failed">${icon("ci")}CI</span>`);
    if (["required", "changes_requested"].includes(item.review_status)) signals.push(`<span class="signal review">${icon("review")}Review</span>`);
    const ownerIcon = item.assignee?.type === "agent" ? "agent" : "user";
    const owner = item.assignee?.name || "Unassigned";
    const moveButtons = readOnly ? "" : `<div class="card-order" aria-label="Reorder ${escapeHtml(item.key)}"><button type="button" class="card-order-button" data-move="up" title="Move up" aria-label="Move ${escapeHtml(item.key)} up" ${index === 0 ? "disabled" : ""}>${icon("arrowUp")}</button><button type="button" class="card-order-button" data-move="down" title="Move down" aria-label="Move ${escapeHtml(item.key)} down" ${index === count - 1 ? "disabled" : ""}>${icon("arrowDown")}</button></div>`;

    return `<article class="work-card" data-id="${item.id}" draggable="${readOnly ? "false" : "true"}" tabindex="0" aria-label="${escapeHtml(item.key)} ${escapeHtml(item.title)}"><div class="card-topline"><span class="card-key">${escapeHtml(item.key)}</span><span class="priority-indicator ${item.priority}" title="${formatLabel(item.priority)} priority"><i></i><i></i><i></i><i></i></span></div><h3>${escapeHtml(item.title)}</h3><div class="card-signals">${signals.join("")}</div><footer class="card-footer"><span class="assignee">${icon(ownerIcon)}<span>${escapeHtml(owner)}</span></span><span>${escapeHtml(item.repository || "No repository")}</span></footer>${moveButtons}</article>`;
  }

  function bindCard(card, readOnly) {
    card.addEventListener("click", (event) => {
      if (!event.target.closest("button")) openWorkItem(card.dataset.id, { trigger: card });
    });
    card.addEventListener("keydown", (event) => {
      if ((event.key === "Enter" || event.key === " ") && !event.target.closest("button")) {
        event.preventDefault();
        openWorkItem(card.dataset.id, { trigger: card });
      }
    });
    $$("[data-move]", card).forEach((button) => button.addEventListener("click", (event) => {
      event.stopPropagation();
      moveByOffset(card.dataset.id, button.dataset.move === "up" ? -1 : 1);
    }));
    if (readOnly) return;
    card.addEventListener("dragstart", (event) => {
      card.classList.add("is-dragging");
      event.dataTransfer.setData("text/plain", card.dataset.id);
      event.dataTransfer.effectAllowed = "move";
    });
    card.addEventListener("dragend", () => card.classList.remove("is-dragging"));
    card.addEventListener("dragover", (event) => {
      event.preventDefault();
      event.stopPropagation();
      card.classList.add("is-drag-over");
    });
    card.addEventListener("dragleave", () => card.classList.remove("is-drag-over"));
    card.addEventListener("drop", async (event) => {
      event.preventDefault();
      event.stopPropagation();
      card.classList.remove("is-drag-over");
      const movingId = event.dataTransfer.getData("text/plain");
      if (movingId && movingId !== card.dataset.id) await moveWorkItem(movingId, selectedItem(card.dataset.id).status, card.dataset.id);
    });
  }

  function bindColumnDrop(column) {
    column.addEventListener("dragover", (event) => {
      event.preventDefault();
      column.classList.add("is-drag-over");
    });
    column.addEventListener("dragleave", () => column.classList.remove("is-drag-over"));
    column.addEventListener("drop", async (event) => {
      if (event.target.closest(".work-card")) return;
      event.preventDefault();
      column.classList.remove("is-drag-over");
      const workItemId = event.dataTransfer.getData("text/plain");
      if (workItemId) await moveWorkItem(workItemId, column.dataset.status, null);
    });
  }

  async function moveByOffset(id, offset) {
    const item = selectedItem(id);
    if (!item) return;
    const siblings = visibleItems(selectedProject().work_items).filter((candidate) => candidate.status === item.status);
    const index = siblings.findIndex((candidate) => candidate.id === id);
    const target = index + offset;
    if (target < 0 || target >= siblings.length) return;
    const beforeId = offset < 0 ? siblings[target].id : siblings[target + 1]?.id || null;
    await moveWorkItem(id, item.status, beforeId);
  }

  async function moveWorkItem(id, status, beforeId) {
    const item = selectedItem(id);
    if (!item || selectedProject()?.status !== "active") return;
    const owner = beginMutation();
    try {
      await request(`/portal/api/work-items/${id}/move`, {
        method: "PATCH",
        body: JSON.stringify({ version: item.version, status, before_id: beforeId })
      });
      if (!ownsMutation(owner)) return;
      await loadWorkspace({ silent: true });
    } catch (error) {
      await handleMutationError(error, null, owner);
    }
  }

  function renderActivity() {
    const entries = state.workspace?.activity || [];
    $("#activity-summary").textContent = `${entries.length} linked run${entries.length === 1 ? "" : "s"}`;
    $("#activity-list").innerHTML = entries.length
        ? entries.map((entry) => {
          const execution = entry.execution;
          const runtime = entry.runtime;
          const action = activityAction(entry);
          return `<button class="activity-row" type="button" data-work-item-id="${entry.work_item.id}"><span class="activity-state state-badge ${execution.state}">${formatLabel(execution.state)}</span><span class="activity-copy"><strong>${escapeHtml(entry.work_item.key)} · ${escapeHtml(entry.work_item.title)}</strong><span>${escapeHtml(runtime?.runtime_name || "Waiting for compatible runtime")} · Generation ${execution.generation} · ${executionTiming(execution)}</span></span><span class="activity-action">${escapeHtml(action)}</span></button>`;
        }).join("")
      : '<div class="empty-view"><h2>No execution history</h2><p>Started agent work appears here.</p></div>';
    $$('[data-work-item-id]', $("#activity-list")).forEach((button) => button.addEventListener("click", () => openWorkItem(button.dataset.workItemId, { trigger: button })));
  }

  function activityAction(entry) {
    const execution = entry.execution;
    if (execution.state === "waiting_for_input") return "Decision needed";
    if (["failed", "cancelled"].includes(execution.state)) return execution.can_retry ? "Retry available" : "Execution stopped";
    if (execution.state === "completed") {
      if (entry.work_item.status === "done") return "Done";
      if (entry.work_item.status === "review") return "In review";
      return "Ready for review";
    }
    return execution.state === "queued" ? "Waiting for runtime" : "In progress";
  }

  function executionTiming(execution) {
    const timing = execution?.timing;
    if (!timing?.state_since) return "Timing unavailable";
    const terminal = ["completed", "failed", "cancelled"].includes(execution.state);
    const startedAt = timing.started_at || timing.state_since;
    const finishedAt = terminal ? timing.finished_at : new Date().toISOString();
    const start = new Date(startedAt);
    const finish = finishedAt ? new Date(finishedAt) : null;
    if (Number.isNaN(start.valueOf()) || !finish || Number.isNaN(finish.valueOf())) return "Timing unavailable";
    const label = terminal ? "Duration" : execution.state === "queued" && !timing.started_at ? "Queued for" : "Elapsed";
    const datetime = terminal ? timing.finished_at : startedAt;
    return `<time datetime="${escapeHtml(datetime)}" title="${escapeHtml(datetime)}">${label} ${formatDuration(finish - start)}</time>`;
  }

  function formatDuration(milliseconds) {
    const totalMinutes = Math.max(0, Math.floor(milliseconds / 60_000));
    if (totalMinutes < 1) return "&lt;1m";
    if (totalMinutes < 60) return `${totalMinutes}m`;
    const hours = Math.floor(totalMinutes / 60);
    const minutes = totalMinutes % 60;
    return minutes ? `${hours}h ${minutes}m` : `${hours}h`;
  }

  function renderResourcesView() {
    const project = selectedProject();
    const resources = project?.resources || [];
    $("#resources-add-button").disabled = !project || project.status !== "active";
    $("#resource-table").innerHTML = resources.length
      ? resources.map((resource) => `<div class="resource-entry"><button class="resource-row" type="button" data-resource-id="${resource.id}" ${resourceEditorBlocked(resource) ? "disabled" : ""}><span class="resource-kind">${escapeHtml(formatLabel(resource.kind))}</span><span class="resource-primary"><strong>${escapeHtml(resource.name)}</strong><span>${escapeHtml(resource.provider || resource.external_ref || "Manual")}</span></span><span><span class="state-badge ${resource.status}">${formatLabel(resource.status)}</span></span><span><span class="state-badge ${resource.sync_status}">${formatLabel(resource.sync_status)}</span></span><span class="resource-message">${escapeHtml(resource.status_message || "No issues")}</span></button>${resource.connection_id ? `<button class="icon-button resource-sync-button" type="button" data-sync-resource-id="${resource.id}" aria-label="Sync ${escapeHtml(resource.name)}" title="Sync ${escapeHtml(resource.name)}" ${project.status !== "active" || resourceActionBusy(resource) ? "disabled" : ""}>${icon("refresh")}</button>` : ""}</div>`).join("")
      : '<div class="empty-view"><h2>No resources attached</h2><p>Attach a repository, tracker, CI system, runtime, agent, or connection.</p></div>';
    $$('[data-resource-id]', $("#resource-table")).forEach((button) => button.addEventListener("click", () => openResourceDialog(resources.find((resource) => resource.id === button.dataset.resourceId))));
    $$('[data-sync-resource-id]', $("#resource-table")).forEach((button) => button.addEventListener("click", () => syncResource(button.dataset.syncResourceId)));

    const runtimes = state.workspace?.runtimes || [];
    $("#runtime-total").textContent = runtimes.length;
    $("#runtime-table").innerHTML = runtimes.length
      ? runtimes.map((runtime) => `<div class="runtime-row"><span class="rail-icon">${icon("agent")}</span><span class="resource-primary"><strong>${escapeHtml(runtime.runtime_name)}</strong><span>${escapeHtml(runtime.machine_name)} · ${escapeHtml(runtime.agent_profile)} · ${escapeHtml(runtime.workspace)}</span></span><span>${runtime.reserved_capacity}/${runtime.capacity} slots</span><span class="state-badge ${runtime.status}">${formatLabel(runtime.status)}</span></div>`).join("")
      : '<div class="empty-view"><h2>No compatible runtimes</h2><p>Work can be queued, but it will not start until a matching runtime is online.</p></div>';
  }

  function renderConnectionsView() {
    const connections = state.workspace?.connections || [];
    $("#connection-table").innerHTML = connections.length
      ? connections.map((connection) => `<div class="connection-row"><button class="connection-primary" type="button" data-connection-id="${connection.id}" ${connectionEditorBlocked(connection.id) ? "disabled" : ""}><span class="resource-kind">${escapeHtml(formatLabel(connection.provider))}</span><span class="resource-primary"><strong>${escapeHtml(connection.name)}</strong><span>${escapeHtml(connection.account_ref)} · ${escapeHtml(authLabel(connection.auth_type))}</span></span><span><span class="state-badge ${connection.status}">${formatLabel(connection.status)}</span></span><span class="connection-capabilities">${(connection.capabilities || []).map((capability) => `<span>${escapeHtml(formatLabel(capability))}</span>`).join("")}</span><span class="resource-message">${escapeHtml(connection.status_message || "No issues")}</span></button><button class="icon-button connection-check-button" type="button" data-check-connection-id="${connection.id}" aria-label="Check ${escapeHtml(connection.name)}" title="Check ${escapeHtml(connection.name)}" ${connectionBusy(connection.id) ? "disabled" : ""}>${icon("refresh")}</button></div>`).join("")
      : '<div class="empty-view"><h2>No engineering connections</h2></div>';
    $$('[data-connection-id]', $("#connection-table")).forEach((button) => button.addEventListener("click", () => openConnectionDialog(connections.find((connection) => connection.id === button.dataset.connectionId))));
    $$('[data-check-connection-id]', $("#connection-table")).forEach((button) => button.addEventListener("click", () => checkConnection(button.dataset.checkConnectionId)));
  }

  function renderHealth() {
    const health = state.workspace?.health || {};
    const labels = [["connections", "Connections"], ["runtimes", "Runtimes"], ["executions", "Executions"], ["synchronization", "Sync"]];
    $("#health-grid").innerHTML = labels.map(([key, label]) => `<div class="health-item"><span>${label}</span><span class="state-badge ${health[key] || "unknown"}">${formatLabel(health[key] || "unknown")}</span></div>`).join("");
  }

  function renderAttention() {
    const items = selectedProject()?.work_items.filter(needsAttention) || [];
    $("#attention-count").textContent = items.length;
    $("#attention-list").innerHTML = items.length
      ? items.slice(0, 6).map((item) => `<button class="rail-item attention-row" type="button" data-attention-id="${item.id}"><span class="rail-icon">${icon(item.execution?.state === "waiting_for_input" ? "agent" : "alert")}</span><span class="rail-copy"><strong>${escapeHtml(item.key)}</strong><span>${escapeHtml(attentionReason(item))}</span></span></button>`).join("")
      : '<div class="empty-column">No items need attention</div>';
    $$('[data-attention-id]').forEach((button) => button.addEventListener("click", () => openWorkItem(button.dataset.attentionId, { trigger: button })));
  }

  function attentionReason(item) {
    if (item.execution?.state === "waiting_for_input") return "Waiting for a decision";
    if (item.execution?.state === "failed") return "Execution failed";
    if (item.blocked) return item.blocker || "Blocked";
    if (item.ci_status === "failed") return "CI failed";
    if (item.review_status === "changes_requested") return "Changes requested";
    return "Needs attention";
  }

  function renderRuntimes() {
    const runtimes = state.workspace?.runtimes || [];
    $("#runtime-count").textContent = runtimes.length;
    $("#runtime-list").innerHTML = runtimes.length
      ? runtimes.slice(0, 5).map((runtime) => `<div class="rail-item"><span class="rail-icon">${icon("agent")}</span><span class="rail-copy"><strong>${escapeHtml(runtime.runtime_name)}</strong><span>${runtime.reserved_capacity}/${runtime.capacity} slots · ${escapeHtml(runtime.agent_profile)}</span></span><span class="health-dot ${runtime.status}" title="${formatLabel(runtime.status)}"></span></div>`).join("")
      : '<div class="empty-column">No compatible runtimes</div>';
  }

  async function openWorkItem(workItemId, { preserveDrawer = false, trigger = null } = {}) {
    if (!preserveDrawer) invalidateMutationContext();
    const requestId = ++state.detailRequestId;
    const previousDetail = preserveDrawer ? state.currentDetail : null;
    state.activeWorkItemId = workItemId;
    if (!preserveDrawer) {
      state.returnFocus = drawerTrigger(trigger || document.activeElement, workItemId);
      $("#detail-key").textContent = "LOADING";
      $("#detail-title").textContent = "Work item";
      $("#detail-content").innerHTML = '<div class="loading-line"></div>';
      showDrawer();
    }

    try {
      const detail = await request(`/portal/api/work-items/${workItemId}`);
      if (requestId !== state.detailRequestId || state.activeWorkItemId !== workItemId) return;
      const interaction = preserveDrawer ? captureDetailInteraction() : null;
      state.currentDetail = preserveDrawer ? mergeDetailHistory(detail, previousDetail) : detail;
      renderDetail(state.currentDetail, interaction);
    } catch (error) {
      if (requestId !== state.detailRequestId) return;
      showToast(error.message, "error");
      if (!preserveDrawer) closeDrawer();
    }
  }

  function renderDetail(detail, interaction = null) {
    const item = detail.work_item;
    const outcome = detail.outcome;
    const execution = item.execution;
    const archived = selectedProject()?.status === "archived";
    $("#detail-key").textContent = item.key;
    $("#detail-title").textContent = item.title;

    const canStart = item.can_start && !archived;
    const canCancel = execution?.can_cancel;
    const canRetry = execution?.can_retry && !archived;
    const waiting = execution?.waiting;
    const nextAction = !archived && execution?.state === "completed" && item.status !== "review" && item.status !== "done" ? `<button class="button" id="move-review-button">Move to review</button>` : "";

    $("#detail-content").innerHTML = `
      <div class="detail-actions">
        ${execution?.run_id ? `<button class="button" id="open-chat-button">${icon("review")}<span>Open Chat</span></button>` : ""}
        <button class="button" id="edit-item-button" ${archived ? "disabled" : ""}>${icon("settings")}<span>Edit</span></button>
        ${canStart ? `<button class="button button-primary" id="run-item-button">${icon("play")}<span>Start run</span></button>` : ""}
        ${canRetry ? `<button class="button button-primary" id="retry-item-button">${icon("rotate")}<span>Retry</span></button>` : ""}
        ${canCancel ? `<button class="button button-danger" id="cancel-item-button">${icon("x")}<span>Cancel run</span></button>` : ""}
        ${nextAction}
        <select id="detail-status-select" aria-label="Workflow status" ${archived ? "disabled" : ""}>${columns.map(([status, label]) => `<option value="${status}" ${item.status === status ? "selected" : ""}>${label}</option>`).join("")}</select>
        <select id="detail-priority-select" aria-label="Priority" ${archived || item.external ? "disabled" : ""}>${["urgent", "high", "medium", "low", "no_priority"].map((priority) => `<option value="${priority}" ${item.priority === priority ? "selected" : ""}>${formatLabel(priority)}</option>`).join("")}</select>
      </div>
      ${item.assignee.type !== "agent" && !execution ? '<div class="inline-notice">Set Owner type to Agent before starting a run.</div>' : ""}
      ${item.external?.available === false ? '<div class="inline-notice attention">Unavailable in provider. Synchronize the work tracker after restoring access or the external item.</div>' : ""}
      ${waiting ? `<div class="input-request"><p><strong>Agent needs a decision</strong><br>${escapeHtml(waiting.question || "Provide the requested input to continue.")}</p>${waiting.decision || waiting.payload?.decision ? '<p>Review the options, consequences, and recommendation in Chat.</p><button class="button button-primary" id="decision-chat-button" type="button">Review decision in Chat</button>' : '<form id="provide-input-form" class="input-row"><input name="answer" required aria-label="Response"><button class="button button-primary" type="submit">Send</button></form>'}</div>` : ""}
      <section class="detail-summary"><p>${escapeHtml(outcome.summary || item.description || "No outcome summary yet.")}</p></section>
      ${item.external ? `<section class="detail-section"><h3>External work</h3><div class="detail-facts">${detailFact("Provider", escapeHtml(formatLabel(item.external.provider)))}${detailFact("Availability", item.external.available === false ? '<span class="state-badge attention">Unavailable</span>' : '<span class="state-badge healthy">Available</span>')}${detailFact("Reference", `<a class="detail-link" href="${escapeHtml(item.external.url)}" target="_blank" rel="noreferrer">#${escapeHtml(item.external.id)} ${icon("external")}</a>`)}${detailFact("State", escapeHtml(item.external.state))}${detailFact("Assigned human", escapeHtml(item.external.assignee || "Unassigned"))}${detailFact("Labels", escapeHtml(item.external.labels.join(", ") || "None"))}</div></section>` : ""}
      ${outcome.failure ? `<section class="detail-section"><h3>Failure</h3><div class="failure-panel"><pre>${escapeHtml(formatOutcome(outcome.failure))}</pre></div></section>` : ""}
      ${hasOutcome(outcome.result) ? `<section class="detail-section"><h3>Completion result</h3><div class="detail-summary"><pre>${escapeHtml(formatOutcome(outcome.result))}</pre></div></section>` : ""}
      <section class="detail-section"><h3>Execution</h3><div class="detail-facts">
        ${detailFact("Phase", `<span class="state-badge ${outcome.phase}">${formatLabel(outcome.phase)}</span>`)}
        ${detailFact("Generation", execution ? String(execution.generation) : "Not started")}
        ${detailFact("Owner", escapeHtml(outcome.owner.name || "Unassigned"))}
        ${detailFact("Repository", escapeHtml(item.repository || "Not linked"))}
        ${detailFact("CI resource", escapeHtml(item.ci_resource || "Repository provider"))}
        ${detailFact("Branch", escapeHtml(item.branch || "Not created"))}
        ${detailFact("Pull request", item.pull_request_url ? `<a class="detail-link" href="${escapeHtml(item.pull_request_url)}" target="_blank" rel="noreferrer">Open PR ${icon("external")}</a> <span class="state-badge ${escapeHtml(item.delivery.pull_request.status || "unknown")}">${escapeHtml(formatLabel(item.delivery.pull_request.status || "unknown"))}</span>${sourceLabel(item.delivery.pull_request)}` : "Not created")}
        ${detailFact("CI", `<span class="state-badge ${item.ci_status}">${formatLabel(item.ci_status)}</span>${sourceLabel(item.delivery.ci)}`)}
        ${detailFact("Review", `<span class="state-badge ${item.review_status}">${formatLabel(item.review_status)}</span>${sourceLabel(item.delivery.review)}`)}
        ${outcome.blocker ? detailFact("Blocker", `<span class="state-badge attention">${escapeHtml(outcome.blocker)}</span>`) : ""}
      </div></section>
      ${renderListSection("Important findings", outcome.findings, "message")}
      ${renderListSection("Changed artifacts", outcome.changed_artifacts, "path")}
      ${renderTestSection(outcome.tests)}
      <section class="detail-section"><h3>Meaningful activity</h3>${renderTimeline(detail.timeline)}</section>
      <section class="detail-section"><details class="raw-details"><summary>Raw execution data</summary><pre id="raw-execution-data">${escapeHtml(JSON.stringify(detail.raw, null, 2))}</pre>${detail.raw.next_before ? '<button class="button" id="load-history-button" type="button">Load older</button>' : ""}</details></section>`;

    $("#edit-item-button")?.addEventListener("click", () => openWorkItemDialog(item));
    $("#open-chat-button")?.addEventListener("click", () => openRunChat(execution.run_id));
    $("#decision-chat-button")?.addEventListener("click", () => openRunChat(execution.run_id));
    $("#run-item-button")?.addEventListener("click", () => runWorkItem(item));
    $("#retry-item-button")?.addEventListener("click", () => retryWorkItem(item));
    $("#cancel-item-button")?.addEventListener("click", () => cancelWorkItem(item));
    $("#move-review-button")?.addEventListener("click", () => moveWorkItem(item.id, "review", null));
    bindDetailSelect("#detail-status-select", (value) => moveWorkItem(item.id, value, null));
    bindDetailSelect("#detail-priority-select", (value) => updateWorkItem(item, { priority: value }));
    $("#provide-input-form")?.addEventListener("submit", (event) => provideInput(event, item));
    $("#load-history-button")?.addEventListener("click", loadOlderHistory);
    restoreDetailInteraction(interaction);
  }

  function detailFact(label, value) {
    return `<div class="detail-fact"><span>${label}</span><span>${value}</span></div>`;
  }

  function bindDetailSelect(selector, operation) {
    const select = $(selector);
    if (!select) return;
    select.addEventListener("change", async () => {
      const wasDisabled = select.disabled;
      select.disabled = true;
      try {
        await operation(select.value);
      } finally {
        if (select.isConnected) select.disabled = wasDisabled;
      }
    });
  }

  function sourceLabel(delivery) {
    const source = delivery?.source;
    if (!source || source === "none") return "";
    const label = source === "provider" && delivery.provider ? delivery.provider : source;
    return `<small class="source-label">${escapeHtml(formatLabel(label))}</small>`;
  }

  function hasOutcome(value) {
    return value !== null && value !== undefined && (typeof value !== "object" || Object.keys(value).length > 0);
  }

  function formatOutcome(value) {
    return typeof value === "string" ? value : JSON.stringify(value, null, 2);
  }

  function renderListSection(title, values, primaryField) {
    if (!values?.length) return "";
    return `<section class="detail-section"><h3>${title}</h3><ul class="detail-list">${values.map((value) => `<li>${escapeHtml(typeof value === "string" ? value : value[primaryField] || JSON.stringify(value))}${value.severity ? ` <span class="source-label">${escapeHtml(value.severity)}</span>` : ""}</li>`).join("")}</ul></section>`;
  }

  function renderTestSection(values) {
    if (!values?.length) return "";
    return `<section class="detail-section"><h3>Tests</h3><ul class="detail-list">${values.map((test) => `<li><span class="state-badge ${escapeHtml(test.status || "unknown")}">${escapeHtml(formatLabel(test.status))}</span> ${escapeHtml(test.name || JSON.stringify(test))}${test.summary ? ` · ${escapeHtml(test.summary)}` : ""}</li>`).join("")}</ul></section>`;
  }

  function renderTimeline(items) {
    if (!items.length) return '<div class="empty-column">No meaningful activity yet</div>';
    return `<ol class="timeline-list">${items.map((item) => {
      const data = item.data || {};
      const title = data.kind || data.state || data.command_id || item.source;
      const detail = data.payload?.summary || data.payload?.message || data.payload?.question || data.acknowledgement_outcome || "";
      return `<li class="timeline-item">${icon(item.source === "command" ? "review" : item.source === "transition" ? "activity" : "agent")}<span><strong>${escapeHtml(formatLabel(title))}</strong>${detail ? `<br><span>${escapeHtml(detail)}</span>` : ""}<small>Generation ${item.generation}</small></span><time>${escapeHtml(formatTime(item.recorded_at))}</time></li>`;
    }).join("")}</ol>`;
  }

  async function loadOlderHistory() {
    const detail = state.currentDetail;
    const cursor = detail?.raw?.next_before;
    if (!detail || !cursor) return;
    const requestId = state.detailRequestId;
    const workItemId = detail.work_item.id;
    const button = $("#load-history-button");
    button.disabled = true;
    const ownsRequest = () => requestId === state.detailRequestId &&
      state.activeWorkItemId === workItemId &&
      state.currentDetail === detail;
    try {
      const page = await request(`/portal/api/work-items/${workItemId}/timeline?before=${encodeURIComponent(cursor)}`);
      if (!ownsRequest()) return;
      detail.raw.timeline = detail.raw.timeline.concat(page.items);
      detail.raw.next_before = page.next_before;
      detail.rawHistoryLoaded = true;
      const interaction = captureDetailInteraction();
      interaction.rawOpen = true;
      renderDetail(detail, interaction);
    } catch (error) {
      if (!ownsRequest()) return;
      showToast(error.message, "error");
      $("#load-history-button")?.removeAttribute("disabled");
    }
  }

  function formatTime(value) {
    if (!value) return "";
    const date = new Date(value);
    return Number.isNaN(date.valueOf()) ? value : new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric", hour: "numeric", minute: "2-digit" }).format(date);
  }

  async function updateWorkItem(item, changes, form = null) {
    const owner = beginMutation(form?.closest("dialog") || null);
    try {
      await request(`/portal/api/work-items/${item.id}`, { method: "PATCH", body: JSON.stringify({ ...changes, version: item.version }) });
      if (!ownsMutation(owner)) return;
      closeDialog(form);
      await refreshAfterMutation("Work item updated");
    } catch (error) {
      await handleMutationError(error, form, owner);
    }
  }

  async function runWorkItem(item) {
    const action = actionId("start", item.id, "initial");
    const owner = beginMutation();
    try {
      await request(`/portal/api/work-items/${item.id}/run`, { method: "POST", body: JSON.stringify({ action_id: action.id }) });
      clearActionId(action.key);
      if (!ownsMutation(owner)) return;
      await refreshAfterMutation("Agent run queued");
    } catch (error) {
      await handleControlError(error, action.key, null, owner);
    }
  }

  async function cancelWorkItem(item) {
    const generation = item.execution?.generation ?? 0;
    const action = actionId("cancel", item.id, generation);
    const owner = beginMutation();
    try {
      await request(`/portal/api/work-items/${item.id}/cancel`, { method: "POST", body: JSON.stringify({ action_id: action.id, generation }) });
      clearActionId(action.key);
      if (!ownsMutation(owner)) return;
      await refreshAfterMutation("Cancellation requested");
    } catch (error) {
      await handleControlError(error, action.key, null, owner);
    }
  }

  async function retryWorkItem(item) {
    const generation = item.execution?.generation ?? 0;
    const action = actionId("retry", item.id, generation);
    const owner = beginMutation();
    try {
      await request(`/portal/api/work-items/${item.id}/retry`, { method: "POST", body: JSON.stringify({ action_id: action.id, generation }) });
      clearActionId(action.key);
      if (!ownsMutation(owner)) return;
      await refreshAfterMutation("Retry queued");
    } catch (error) {
      await handleControlError(error, action.key, null, owner);
    }
  }

  async function provideInput(event, item) {
    event.preventDefault();
    const form = event.currentTarget;
    const waiting = item.execution?.waiting;
    if (!waiting) return;
    const action = actionId("input", item.id, waiting.transition_id);
    const owner = beginMutation();
    await withSubmitLock(form, async () => {
      try {
        const answer = new FormData(form).get("answer");
        await request(`/portal/api/work-items/${item.id}/input`, {
          method: "POST",
          body: JSON.stringify({ input: { answer }, action_id: action.id, waiting_transition_id: waiting.transition_id })
        });
        clearActionId(action.key);
        if (!ownsMutation(owner)) return;
        await refreshAfterMutation("Input sent");
      } catch (error) {
        await handleControlError(error, action.key, form, owner);
      }
    });
  }

  function actionId(kind, workItemId, scope) {
    const key = `symmetry:${kind}:${workItemId}:${scope}`;
    let id = sessionStorage.getItem(key);
    if (!id) {
      id = crypto.randomUUID();
      sessionStorage.setItem(key, id);
    }
    return { key, id };
  }

  function clearActionId(key) {
    sessionStorage.removeItem(key);
  }

  async function handleControlError(error, actionKeyValue, form = null, owner = null) {
    if (["state_conflict", "invalid_request", "not_found"].includes(error.code)) clearActionId(actionKeyValue);
    await handleMutationError(error, form, owner);
  }

  async function handleMutationError(error, form = null, owner = null, staleTarget = {}) {
    const owns = !owner || ownsMutation(owner);
    const conflict = ["stale", "state_conflict"].includes(error.code);
    if (!conflict) {
      if (!owns) return;
      showFormError(form, error.message);
      showToast(error.message, "error");
      return;
    }

    const resourceId = staleTarget.resourceId || (form?.id === "resource-form" ? form.elements.id.value || state.editingResourceId : null);
    const connectionId = staleTarget.connectionId || (form?.id === "connection-form" ? form.elements.id.value || state.editingConnectionId : null);
    if (resourceId) state.persistentStaleEditors.add(`resource:${resourceId}`);
    if (connectionId) state.persistentStaleEditors.add(`connection:${connectionId}`);
    if (owns) {
      showFormError(form, error.message);
      closeDialog(form);
    }
    if (resourceId) {
      state.staleEditors.add(`resource:${resourceId}`);
      reconcileResourceButtons(resourceId);
    }
    if (connectionId) {
      state.staleEditors.add(`connection:${connectionId}`);
      reconcileConnectionButtons(connectionId);
    }
    const projectId = staleTarget.projectId || owner?.projectId;
    if (state.projectSwitchTargetId !== null || (projectId && state.selectedProjectId !== projectId)) return;

    try {
      const loaded = await loadWorkspace({ silent: true });
      if (loaded) {
        showToast(error.code === "stale" ? "This item changed elsewhere. Current data was reloaded." : `${error.message}. Current data was reloaded.`, "error");
      }
    } catch (refreshError) {
      if (resourceId || connectionId) {
        renderWorkspace();
        if (resourceId) reconcileResourceButtons(resourceId);
        if (connectionId) reconcileConnectionButtons(connectionId);
      }
      showToast(
        `This item changed elsewhere, but current data could not be reloaded: ${refreshError.message}. Refresh the workspace to continue.`,
        "error"
      );
    }
  }

  function showDrawer() {
    document.body.classList.add("drawer-open");
    $("#detail-drawer").removeAttribute("inert");
    $("#detail-drawer").classList.add("is-open");
    $("#detail-drawer").setAttribute("aria-hidden", "false");
    $("#drawer-backdrop").hidden = false;
    window.requestAnimationFrame(() => $("#close-detail-button").focus());
  }

  function closeDrawer() {
    invalidateMutationContext();
    const returnFocus = state.returnFocus;
    state.activeWorkItemId = null;
    state.currentDetail = null;
    state.returnFocus = null;
    state.detailRequestId += 1;
    document.body.classList.remove("drawer-open");
    $("#detail-drawer").classList.remove("is-open");
    $("#detail-drawer").setAttribute("aria-hidden", "true");
    $("#detail-drawer").setAttribute("inert", "");
    $("#drawer-backdrop").hidden = true;
    restoreDrawerTrigger(returnFocus);
  }

  function drawerTrigger(element, workItemId) {
    if (!(element instanceof Element)) return { element: null, kind: "board", workItemId };
    const kind = element.matches("[data-work-item-id]") ? "activity" : element.matches("[data-attention-id]") ? "attention" : "board";
    return { element, kind, workItemId };
  }

  function restoreDrawerTrigger(trigger) {
    if (!trigger) return;
    if (trigger.element?.isConnected && !trigger.element.disabled) {
      trigger.element.focus();
      return;
    }
    const selector = trigger.kind === "activity"
      ? "[data-work-item-id]"
      : trigger.kind === "attention"
        ? "[data-attention-id]"
        : ".work-card[data-id]";
    $$(selector).find((element) => (element.dataset.workItemId || element.dataset.attentionId || element.dataset.id) === trigger.workItemId)?.focus();
  }

  function captureDetailInteraction() {
    const drawer = $("#detail-drawer");
    if (!drawer.classList.contains("is-open")) return null;
    const active = drawer.contains(document.activeElement) ? document.activeElement : null;
    const focusables = drawerFocusables();
    const answer = $('#provide-input-form input[name="answer"]')?.value;
    const raw = $("details.raw-details");

    return {
      answer,
      rawOpen: Boolean(raw?.open),
      focus: active
        ? {
            id: active.id || null,
            name: active.getAttribute("name"),
            index: focusables.indexOf(active),
            selectionStart: typeof active.selectionStart === "number" ? active.selectionStart : null,
            selectionEnd: typeof active.selectionEnd === "number" ? active.selectionEnd : null
          }
        : null
    };
  }

  function restoreDetailInteraction(interaction) {
    if (!interaction) return;
    const answer = $('#provide-input-form input[name="answer"]');
    if (answer && interaction.answer !== undefined) answer.value = interaction.answer;
    if (interaction.rawOpen) $("details.raw-details")?.setAttribute("open", "");
    if (!interaction.focus) return;

    const focusables = drawerFocusables();
    const target =
      (interaction.focus.id && document.getElementById(interaction.focus.id)) ||
      (interaction.focus.name && $(`#detail-drawer [name="${interaction.focus.name}"]`)) ||
      focusables[interaction.focus.index] ||
      $("#close-detail-button");

    target?.focus();
    if (target && interaction.focus.selectionStart !== null && typeof target.setSelectionRange === "function") {
      target.setSelectionRange(interaction.focus.selectionStart, interaction.focus.selectionEnd);
    }
  }

  function mergeDetailHistory(detail, previous) {
    if (!previous || previous.work_item?.id !== detail.work_item?.id || !previous.rawHistoryLoaded) return detail;
    const seen = new Set();
    const timeline = [...detail.raw.timeline, ...previous.raw.timeline].filter((entry) => {
      const key = timelineKey(entry);
      if (seen.has(key)) return false;
      seen.add(key);
      return true;
    });
    return {
      ...detail,
      rawHistoryLoaded: true,
      raw: {
        ...detail.raw,
        timeline,
        next_before: previous.raw.next_before
      }
    };
  }

  function timelineKey(entry) {
    const data = entry.data || {};
    const id = data.event_id || data.transition_id || data.command_id || JSON.stringify(data);
    return `${entry.source}:${entry.run_id || ""}:${entry.generation}:${id}`;
  }

  function drawerFocusables() {
    return $$('button:not([disabled]), a[href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), summary, [tabindex]:not([tabindex="-1"])', $("#detail-drawer"))
      .filter((element) => element.getClientRects().length > 0);
  }

  function trapDrawerFocus(event) {
    if (event.key !== "Tab" || !$("#detail-drawer").classList.contains("is-open") || dialogOpen()) return;
    const focusables = drawerFocusables();
    if (!focusables.length) return;
    const first = focusables[0];
    const last = focusables[focusables.length - 1];
    if (event.shiftKey && (document.activeElement === first || !$("#detail-drawer").contains(document.activeElement))) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first.focus();
    }
  }

  let toastTimeout;
  function showToast(message, tone = "info") {
    const toast = $("#toast");
    toast.textContent = message;
    toast.dataset.tone = tone;
    toast.hidden = false;
    window.clearTimeout(toastTimeout);
    toastTimeout = window.setTimeout(() => { toast.hidden = true; }, 5000);
  }

  function showFormError(form, message) {
    const alert = form?.querySelector("[data-form-error]");
    if (!alert) return;
    alert.textContent = message || "";
    alert.hidden = !message;
  }

  function formPayload(form, includeEmpty = false) {
    const entries = Array.from(new FormData(form).entries());
    const payload = Object.fromEntries(includeEmpty ? entries : entries.filter(([, value]) => value !== ""));
    for (const name of ["last_checked_at", "last_synced_at"]) {
      if (payload[name]) payload[name] = new Date(payload[name]).toISOString();
    }
    return payload;
  }

  async function withSubmitLock(form, operation) {
    if (form.dataset.submitting) return;
    const submit = form.querySelector('[type="submit"]');
    const generation = String(++submitLockGeneration);
    form.dataset.submitting = generation;
    if (submit) submit.disabled = true;
    showFormError(form, "");
    try {
      await operation();
    } finally {
      if (form.dataset.submitting === generation) {
        delete form.dataset.submitting;
        if (form.id === "resource-form") reconcileResourceEditor();
        else if (form.id === "connection-form") reconcileConnectionEditor();
        else if (submit) submit.disabled = false;
      }
    }
  }

  function repositoryOptions(selectedId = "") {
    const repositories = selectedProject()?.resources.filter((resource) => resource.kind === "repository") || [];
    return ['<option value="">Not linked</option>', ...repositories.map((resource) => `<option value="${resource.id}" ${resource.id === selectedId ? "selected" : ""}>${escapeHtml(resource.name)}</option>`)].join("");
  }

  function ciResourceOptions(selectedId = "") {
    const resources = selectedProject()?.resources.filter((resource) => resource.kind === "ci") || [];
    return ['<option value="">Use repository provider</option>', ...resources.map((resource) => `<option value="${resource.id}" ${resource.id === selectedId ? "selected" : ""}>${escapeHtml(resource.name)}</option>`)].join("");
  }

  function connectionOptions(selectedId = "", kind = "repository") {
    const capability = { repository: "repositories", work_tracking: "work_items", ci: "ci" }[kind];
    const connections = (state.workspace?.connections || [])
      .filter((connection) => !capability || connection.capabilities.includes(capability));
    return ['<option value="">Manual</option>', ...connections.map((connection) => `<option value="${connection.id}" ${connection.id === selectedId ? "selected" : ""}>${escapeHtml(connection.name)} · ${escapeHtml(formatLabel(connection.provider))}</option>`)].join("");
  }

  function syncResourceConnectionControl(selectedId = null) {
    const form = $("#resource-form");
    const externalKind = ["repository", "work_tracking", "ci"].includes(form.elements.kind.value);
    const resource = selectedProject()?.resources.find((candidate) => candidate.id === state.editingResourceId);
    const readOnly = selectedProject()?.status !== "active" || resourceEditorBlocked(resource);
    const current = selectedId ?? form.elements.connection_id.value;
    form.elements.connection_id.innerHTML = connectionOptions(current, form.elements.kind.value);
    form.elements.connection_id.value = externalKind ? current : "";
    form.elements.connection_id.disabled = !externalKind || readOnly;
    const connected = Boolean(form.elements.connection_id.value);
    $$('[data-manual-resource-field] input, [data-manual-resource-field] select', form).forEach((input) => {
      input.disabled = connected || readOnly;
    });
  }

  function syncResourceReferenceControl(selectedValue = "") {
    const form = $("#resource-form");
    const input = form.elements.external_ref;
    const select = form.elements.registered_external_ref;
    const kind = form.elements.kind.value;
    const registered = state.workspace?.registered_runtimes || [];
    const resource = selectedProject()?.resources.find((candidate) => candidate.id === state.editingResourceId);
    const readOnly = selectedProject()?.status !== "active" || resourceEditorBlocked(resource);
    const usesRegistry = kind === "agent" || kind === "runtime";

    input.hidden = usesRegistry;
    input.disabled = readOnly || usesRegistry;
    input.required = false;
    select.hidden = !usesRegistry;
    select.disabled = readOnly || !usesRegistry;
    select.required = usesRegistry;

    if (!usesRegistry) {
      input.value = selectedValue || "";
      select.innerHTML = "";
      return;
    }

    const options = kind === "runtime"
      ? registered.map((runtime) => ({ value: runtime.runtime_id, label: `${runtime.runtime_name} · ${runtime.agent_profile} · ${runtime.workspace}` }))
      : [...new Set(registered.map((runtime) => runtime.agent_profile))]
          .sort()
          .map((profile) => ({ value: profile, label: profile }));

    const optionMarkup = [
      `<option value="">Select registered ${kind}</option>`,
      ...options.map((option) => `<option value="${escapeHtml(option.value)}">${escapeHtml(option.label)}</option>`)
    ].join("");
    if (select.innerHTML !== optionMarkup) select.innerHTML = optionMarkup;
    select.value = options.some((option) => option.value === selectedValue) ? selectedValue : "";
  }

  function openWorkItemDialog(item = null) {
    const form = $("#work-item-form");
    invalidateMutationContext();
    form.reset();
    resetSubmitLock(form);
    showFormError(form, "");
    state.editingWorkItemId = item?.id || null;
    form.elements.id.value = item?.id || "";
    form.elements.version.value = item?.version || "";
    $("#work-item-dialog-title").textContent = item ? "Edit work item" : "Create work";
    $("#work-item-submit-label").textContent = item ? "Save" : "Create";
    form.elements.repository_resource_id.innerHTML = repositoryOptions(item?.repository_resource_id || "");
    form.elements.ci_resource_id.innerHTML = ciResourceOptions(item?.ci_resource_id || "");

    if (item) {
      const values = {
        title: item.title,
        description: item.description,
        status: item.status,
        priority: item.priority,
        assignee_type: item.assignee.type,
        assignee_name: item.assignee.name,
        agent_profile: item.assignee.agent_profile,
        workspace: item.workspace,
        repository_resource_id: item.repository_resource_id,
        ci_resource_id: item.ci_resource_id,
        branch: item.branch,
        pull_request_url: item.delivery.pull_request.source === "manual" ? item.pull_request_url : "",
        ci_status: item.delivery.ci.source === "manual" ? item.ci_status : "",
        review_status: item.delivery.review.source === "manual" ? item.review_status : "",
        blocker: item.blocker
      };
      Object.entries(values).forEach(([name, value]) => { if (form.elements[name]) form.elements[name].value = value || ""; });
      form.elements.blocked.checked = item.blocked;
    }

    form.elements.status.disabled = Boolean(item);
    const locked = Boolean(item?.execution?.intent_locked);
    $$('[data-intent-field] input, [data-intent-field] textarea, [data-intent-field] select', form).forEach((input) => { input.disabled = locked; });
    const providerOwned = Boolean(item?.external);
    $$('[data-provider-owned-field] input, [data-provider-owned-field] textarea, [data-provider-owned-field] select', form).forEach((input) => { input.disabled = locked || providerOwned; });
    $("#intent-lock-note").hidden = !locked;
    $("#provider-owner-note").hidden = !providerOwned;
    $("#work-item-dialog").showModal();
  }

  function openProjectDialog(project = null) {
    const form = $("#project-form");
    invalidateMutationContext();
    form.reset();
    resetSubmitLock(form);
    showFormError(form, "");
    state.editingProjectId = project?.id || null;
    form.elements.id.value = project?.id || "";
    form.elements.version.value = project?.version || "";
    $("#project-dialog-title").textContent = project ? "Project settings" : "Create project";
    $("#project-submit-label").textContent = project ? "Save" : "Create";
    form.elements.key.disabled = Boolean(project);
    form.elements.status.closest("label").hidden = !project;
    if (project) {
      for (const name of ["name", "key", "status", "default_agent_profile", "default_workspace", "description"]) form.elements[name].value = project[name] || "";
      const archived = project.status === "archived";
      for (const name of ["name", "default_agent_profile", "default_workspace", "description"]) form.elements[name].disabled = archived;
    } else {
      for (const name of ["name", "default_agent_profile", "default_workspace", "description"]) form.elements[name].disabled = false;
      form.elements.status.value = "active";
      form.elements.default_agent_profile.value = "default";
      form.elements.default_workspace.value = "primary";
    }
    $("#project-dialog").showModal();
  }

  function openResourceDialog(resource = null) {
    if (resourceEditorBlocked(resource)) return;
    const form = $("#resource-form");
    invalidateMutationContext();
    form.reset();
    resetSubmitLock(form);
    showFormError(form, "");
    state.editingResourceId = resource?.id || null;
    form.elements.id.value = resource?.id || "";
    form.elements.version.value = resource?.version || "";
    $("#resource-dialog-title").textContent = resource ? "Update resource" : "Attach resource";
    $("#resource-submit-label").textContent = resource ? "Update" : "Attach";
    if (resource) {
      for (const name of ["kind", "status", "sync_status", "name", "provider", "url", "status_message"]) form.elements[name].value = resource[name] || "";
      form.elements.last_checked_at.value = localDateTime(resource.last_checked_at);
      form.elements.last_synced_at.value = localDateTime(resource.last_synced_at);
    }
    reconcileResourceEditor(resource);
    $("#resource-dialog").showModal();
  }

  function openConnectionDialog(connection = null) {
    if (connectionEditorBlocked(connection?.id)) return;
    const form = $("#connection-form");
    invalidateMutationContext();
    form.reset();
    resetSubmitLock(form);
    showFormError(form, "");
    state.editingConnectionId = connection?.id || null;
    form.elements.id.value = connection?.id || "";
    form.elements.version.value = connection?.version || "";
    $("#connection-dialog-title").textContent = connection ? "Connection settings" : "New connection";
    $("#connection-submit-label").textContent = connection ? "Save" : "Connect";
    if (connection) {
      for (const name of ["provider", "name", "account_ref", "auth_type"]) form.elements[name].value = connection[name] || "";
      $$('input[name="capabilities"]', form).forEach((input) => { input.checked = connection.capabilities.includes(input.value); });
    }

    syncConnectionCapabilityControls();
    syncConnectionAuthControl();
    reconcileConnectionEditor(connection);
    $("#connection-dialog").showModal();
  }

  function syncConnectionAuthControl() {
    const form = $("#connection-form");
    const provider = form.elements.provider.value;
    form.elements.auth_type.value = provider === "github" ? "gh_cli" : "entra_id";
    $("#connection-auth-label").textContent = authLabel(form.elements.auth_type.value);
  }

  function syncConnectionCapabilityControls(changed = null) {
    const form = $("#connection-form");
    const repositories = form.querySelector('input[name="capabilities"][value="repositories"]');
    const changes = form.querySelector('input[name="capabilities"][value="changes"]');

    if (changed === repositories && !repositories.checked) changes.checked = false;
    else if (changes.checked) repositories.checked = true;
  }

  function reconcileResourceEditor(resource = selectedProject()?.resources.find((candidate) => candidate.id === state.editingResourceId)) {
    const form = $("#resource-form");
    const stale = Boolean(state.editingResourceId && state.staleEditors.has(`resource:${state.editingResourceId}`));
    const readOnly = Boolean(form.dataset.submitting) || selectedProject()?.status !== "active" || stale || Boolean(state.editingResourceId && !resource) || resourceEditorBlocked(resource);
    $("#delete-resource-button").hidden = !resource || selectedProject()?.status !== "active";
    $("#delete-resource-button").disabled = readOnly;
    $("#sync-resource-button").hidden = !resource?.connection_id || selectedProject()?.status !== "active";
    $("#sync-resource-button").disabled = selectedProject()?.status !== "active" || resourceActionBusy(resource);
    $$('input, select, textarea', form).forEach((input) => { if (!input.matches('[type="hidden"]')) input.disabled = readOnly; });
    syncResourceReferenceControl(resource?.external_ref || form.elements.external_ref.value);
    syncResourceConnectionControl(resource?.connection_id || form.elements.connection_id.value);
    form.querySelector('[type="submit"]').disabled = readOnly;
    if (stale) showFormError(form, "This resource changed while the editor was open. Close and reopen it to edit the current version.");
  }

  function reconcileConnectionEditor(connection = state.workspace?.connections.find((candidate) => candidate.id === state.editingConnectionId)) {
    const form = $("#connection-form");
    const stale = Boolean(state.editingConnectionId && state.staleEditors.has(`connection:${state.editingConnectionId}`));
    const blocked = Boolean(form.dataset.submitting) || Boolean(state.editingConnectionId && !connection) || connectionEditorBlocked(connection?.id);
    $$('input, select, textarea', form).forEach((input) => { if (!input.matches('[type="hidden"]')) input.disabled = blocked; });
    form.elements.provider.disabled = blocked || Boolean(connection);
    form.elements.account_ref.disabled = blocked || Boolean(connection);
    form.querySelector('[type="submit"]').disabled = blocked;
    $("#delete-connection-button").hidden = !connection;
    $("#delete-connection-button").disabled = blocked;
    if (stale) showFormError(form, "This connection changed while the editor was open. Close and reopen it to edit the current version.");
  }

  function reconcileConnectionButtons(connectionId) {
    $$('[data-connection-id], [data-check-connection-id]', $("#connection-table")).forEach((button) => {
      if (button.dataset.connectionId === connectionId || button.dataset.checkConnectionId === connectionId) {
        button.disabled = button.dataset.connectionId
          ? connectionEditorBlocked(connectionId)
          : connectionBusy(connectionId);
      }
    });
  }

  function reconcileResourceButtons(resourceId = null) {
    const project = selectedProject();
    const resource = resourceId
      ? project?.resources.find((candidate) => candidate.id === resourceId)
      : null;
    const resources = resourceId
      ? null
      : new Map((project?.resources || []).map((candidate) => [candidate.id, candidate]));
    $$('[data-resource-id], [data-sync-resource-id]', $("#resource-table")).forEach((button) => {
      const buttonResourceId = button.dataset.resourceId || button.dataset.syncResourceId;
      if (resourceId && buttonResourceId !== resourceId) return;
      const currentResource = resourceId ? resource : resources.get(buttonResourceId);
      button.disabled = project?.status !== "active" || (button.dataset.resourceId
        ? resourceEditorBlocked(currentResource)
        : resourceActionBusy(currentResource));
    });
  }

  function reconcileProjectSyncButton() {
    const project = selectedProject();
    $("#sync-project-button").disabled = !project || project.status !== "active" || projectSyncBusy(project);
  }

  function reconcileProjectResources(projectId) {
    const project = selectedProject();
    if (project?.id !== projectId) return;
    reconcileResourceButtons();
    if ($("#resource-dialog").open) reconcileResourceEditor();
  }

  function localDateTime(value) {
    if (!value) return "";
    const date = new Date(value);
    if (Number.isNaN(date.valueOf())) return "";
    const local = new Date(date.getTime() - date.getTimezoneOffset() * 60_000);
    return local.toISOString().slice(0, 16);
  }

  function closeDialog(form) {
    if (form?.closest("dialog")?.open) form.closest("dialog").close();
  }

  function resetSubmitLock(form) {
    delete form.dataset.submitting;
    const submit = form.querySelector('[type="submit"]');
    if (submit) submit.disabled = false;
  }

  async function confirmAction(title, message, label) {
    const dialog = $("#confirm-dialog");
    $("#confirm-title").textContent = title;
    $("#confirm-message").textContent = message;
    $("#confirm-action-button").textContent = label;
    dialog.returnValue = "cancel";
    dialog.showModal();
    return new Promise((resolve) => dialog.addEventListener("close", () => resolve(dialog.returnValue === "confirm"), { once: true }));
  }

  function chatScope() {
    if (chat.scope === "run") return { scope: "run", run_id: chat.runId };
    if (chat.scope === "project") return { scope: "project", project_id: state.selectedProjectId };
    return { scope: "workspace" };
  }

  function chatContextKey() {
    return JSON.stringify(chatScope());
  }

  function saveChatDraft() {
    if (!chat.contextKey) return;
    chat.drafts.set(chat.contextKey, Object.fromEntries($$("input, textarea, select", $("#chat-form")).map((field) => [field.id, field.value])));
  }

  function setChatMarkup(root, html) {
    if (chatMarkup.get(root) === html) return;
    root.innerHTML = html;
    chatMarkup.set(root, html);
  }

  function updateChatOptions(select, html, value = select.value) {
    setChatMarkup(select, html);
    if (Array.from(select.options).some((option) => option.value === value)) select.value = value;
  }

  function renderChatSelectors() {
    $("#chat-scope").value = chat.scope;
    $("#chat-run-field").hidden = chat.scope !== "run";
    const projects = state.workspace?.projects || [];
    const items = projects.flatMap((project) => project.work_items).filter((item) => item.execution?.run_id);
    const runProjectId = currentChatRun()?.work_item.project_id || items.find((item) => item.execution.run_id === chat.runId)?.project_id;
    const contextProject = chat.scope === "run" ? projects.find((project) => project.id === runProjectId) : selectedProject();
    $("#chat-project-label").textContent = chat.scope === "workspace" ? "Across your projects" : contextProject?.name || (chat.scope === "run" ? "Pinned run context" : "Select or create a project");
    const pinned = chat.runId && !items.some((item) => item.execution.run_id === chat.runId)
      ? `<option value="${escapeHtml(chat.runId)}">Pinned run · ${escapeHtml(chat.runId.slice(0, 8))}</option>` : "";
    updateChatOptions($("#chat-run-select"), '<option value="">Choose a run</option>' + pinned + items.map((item) => `<option value="${escapeHtml(item.execution.run_id)}">${escapeHtml(item.key)} · ${escapeHtml(item.title)} · Attempt ${item.execution.generation}</option>`).join(""), chat.runId || "");
    updateChatOptions($("#chat-target-project"), projects.filter((project) => project.status === "active").map(projectOption).join(""), $("#chat-target-project").value || state.selectedProjectId);
    renderChatWorkResources();
  }

  function renderChatWorkResources() {
    const projectId = chat.scope === "workspace" ? $("#chat-target-project").value : state.selectedProjectId;
    const project = state.workspace?.projects.find((candidate) => candidate.id === projectId);
    for (const [selector, kind, placeholder] of [["#chat-repository", "repository", "Project default"], ["#chat-ci", "ci", "Repository provider"]]) {
      updateChatOptions($(selector), `<option value="">${placeholder}</option>` + (project?.resources || []).filter((resource) => resource.kind === kind).map((resource) => `<option value="${escapeHtml(resource.id)}">${escapeHtml(resource.name)}</option>`).join(""));
    }
  }

  function prepareChatContext() {
    const key = chatContextKey();
    if (chat.contextKey !== key) {
      saveChatDraft();
      chat.contextKey = key;
      chat.snapshot = null;
      chat.requestId += 1;
      [$("#chat-messages"), $("#chat-run-context"), $("#chat-run-select"), ...$$("select", $("#chat-form"))].forEach((root) => chatMarkup.delete(root));
      $("#chat-form").reset();
      renderChatSelectors();
      const draft = chat.drafts.get(key) || {};
      if (draft["chat-target-project"]) {
        $("#chat-target-project").value = draft["chat-target-project"];
        renderChatWorkResources();
      }
      for (const [id, value] of Object.entries(draft)) {
        const field = document.getElementById(id);
        if (field) field.value = value;
      }
      $("#chat-scroll").scrollTop = 0;
      setChatMarkup($("#chat-messages"), '<div class="chat-empty"><span class="chat-empty-mark">' + icon("review") + '</span><h2>A clear goal goes a long way.</h2><p>Discuss the work, ask for status, or choose Start work to put an agent on it.</p></div>');
      setChatMarkup($("#chat-run-context"), "");
      $("#chat-load-older").hidden = true;
    } else renderChatSelectors();
    renderChatComposer();
    syncLocation();
  }

  function currentChatRun() {
    return chat.scope === "run" ? chat.snapshot?.runs.find((detail) => detail.work_item.execution?.run_id === chat.runId) : null;
  }

  function renderChatComposer() {
    const intent = $("#chat-intent").value;
    const run = currentChatRun();
    const contextMissing = (chat.scope === "project" && !state.selectedProjectId) || (chat.scope === "run" && !chat.runId);
    const targetProjectId = chat.scope === "workspace" ? $("#chat-target-project").value : state.selectedProjectId;
    const target = state.workspace?.projects.find((project) => project.id === targetProjectId);
    const intentUnavailable = (intent === "guidance" && !run?.work_item.execution?.can_guide) || (intent === "start_work" && (chat.scope === "run" || target?.status !== "active"));
    const pending = chat.pending.has(chat.contextKey);
    $("#chat-send").disabled = contextMissing || intentUnavailable || pending;
    $("#chat-start-fields").hidden = intent !== "start_work";
    $("#chat-target-project-field").hidden = chat.scope !== "workspace";
    const notes = {
      discuss: "Discuss without changing execution.",
      status: "Read recorded progress. The worker keeps running.",
      start_work: chat.scope === "run" ? "Choose Project or Workspace context to start new work." : "Creates real work and queues an agent run.",
      guidance: run?.work_item.execution?.can_guide ? "Applied at a safe boundary. Limit: 32,768 UTF-8 bytes." : "Select an active run that supports supervisory guidance."
    };
    $("#chat-intent-note").textContent = notes[intent];
    const notice = chat.notices.get(chat.contextKey);
    $("#chat-send-status").textContent = pending ? "Saving… Wait for durable confirmation." : notice?.message || "Messages are saved to this context.";
    $("#chat-send-status").classList.toggle("is-error", Boolean(notice?.error));
    $("#chat-form").setAttribute("aria-busy", String(pending));
  }

  async function loadChat({ silent = false, older = false } = {}) {
    prepareChatContext();
    const scope = chatScope();
    if ((scope.scope === "run" && !scope.run_id) || (scope.scope === "project" && !scope.project_id)) return;
    const key = chat.contextKey;
    const requestId = ++chat.requestId;
    const params = new URLSearchParams(scope);
    const previous = chat.snapshot;
    if (older && previous?.next_before) params.set("before", previous.next_before);
    $("#chat-load-older").disabled = older;
    try {
      const snapshot = await request(`/portal/api/chat?${params}`);
      if (key !== chat.contextKey || requestId !== chat.requestId) return;
      const messages = new Map();
      // Keep loaded history through refresh, but replace matching receipts with current durable state.
      for (const message of previous?.messages || []) messages.set(message.id, message);
      const gap = !older && snapshot.next_before && messages.size > 0 && snapshot.messages.length > 0 && !snapshot.messages.some((message) => messages.has(message.id));
      for (const message of snapshot.messages || []) messages.set(message.id, message);
      refreshChatReceipts(messages, snapshot, key);
      snapshot.messages = Array.from(messages.values()).sort((a, b) => String(a.inserted_at).localeCompare(String(b.inserted_at)) || String(a.id).localeCompare(String(b.id)));
      if (!older && previous?.historyLoaded && !gap) snapshot.next_before = previous.next_before;
      snapshot.historyLoaded = !gap && (older || previous?.historyLoaded);
      chat.snapshot = snapshot;
      renderChatSelectors();
      renderChatSnapshot({ older });
    } catch (error) {
      if (key !== chat.contextKey || requestId !== chat.requestId) return;
      if (!silent || !chat.snapshot) {
        chat.notices.set(key, { message: `Could not refresh this conversation: ${error.message}. Your draft is kept.`, error: true });
        renderChatComposer();
      }
    } finally {
      if (key === chat.contextKey && requestId === chat.requestId) $("#chat-load-older").disabled = false;
    }
  }

  function refreshChatReceipts(messages, snapshot, contextKey) {
    const currentMessages = new Set(snapshot.messages.map((message) => message.id));
    const commands = new Map();
    for (const message of snapshot.messages) {
      if (message.command?.command_id) commands.set(message.command.command_id, message.command);
    }
    for (const detail of snapshot.runs || []) {
      for (const entry of detail.timeline || []) {
        if (entry.source === "command" && entry.data?.command_id) commands.set(entry.data.command_id, entry.data);
      }
      const command = detail.work_item.execution?.latest_command;
      if (command?.command_id) commands.set(command.command_id, command);
    }
    for (const [id, message] of messages) {
      const command = message.command;
      const current = commands.get(command?.command_id);
      if (current) messages.set(id, { ...message, command: current, receipt_stale: false });
      else if (command && !currentMessages.has(id) && !command.acknowledgement_outcome && !["acknowledged", "cancelled", "superseded"].includes(command.state)) {
        messages.set(id, { ...message, receipt_stale: true });
      }
    }
    const notice = chat.notices.get(contextKey);
    if (notice?.command && !notice.error) {
      const command = commands.get(notice.command.command_id);
      if (command) chat.notices.set(contextKey, { ...notice, command, message: chatCommandLabel(command) });
      else if (!notice.command.acknowledgement_outcome && !["acknowledged", "cancelled", "superseded"].includes(notice.command.state)) {
        chat.notices.set(contextKey, { ...notice, message: "Saved · acknowledgement not refreshed with this page." });
      }
    }
  }

  function chatCommandLabel(command) {
    if (!command) return "Saved";
    const outcome = command.acknowledgement_outcome;
    if (outcome === "applied") return "Applied at a safe boundary";
    if (outcome === "rejected" || outcome === "failed") return `Saved · ${formatLabel(outcome)} by agent`;
    if (command.state === "acknowledged") return "Saved · acknowledged";
    if (["cancelled", "superseded"].includes(command.state)) return `Saved · ${formatLabel(command.state)}`;
    return "Saved · awaiting agent acknowledgement";
  }

  function chatFailureText(failure) {
    const text = [failure, failure?.message, failure?.summary, failure?.error]
      .find((value) => typeof value === "string" && value.trim());
    return text ? text.length > 2_000 ? `${text.slice(0, 2_000)}…` : text : null;
  }

  function chatTimelineMessages() {
    const entries = [...chat.snapshot.messages];
    const eventKinds = new Set(["progress", "summary", "rationale", "finding", "artifact", "test", "pull_request", "ci", "review", "waiting_for_input"]);
    const transitionStates = new Set(["completed", "failed", "cancelled", "paused", "waiting_for_input"]);
    for (const detail of chat.snapshot.runs || []) {
      for (const entry of detail.timeline || []) {
        const data = entry.data || {};
        const payload = data.payload || {};
        const isEvent = entry.source === "event" && eventKinds.has(data.kind);
        const isTransition = entry.source === "transition" && transitionStates.has(data.state);
        if (!isEvent && !isTransition) continue;
        const kind = data.kind || data.state;
        const content = [isTransition && data.state === "failed" ? chatFailureText(payload) : null, payload.summary, payload.message, payload.rationale, payload.question, payload.path, payload.url]
          .find((value) => typeof value === "string" && value.trim());
        const status = typeof payload.status === "string" ? formatLabel(payload.status) : "";
        if (!content && !status && !isTransition) continue;
        entries.push({
          id: `run:${entry.run_id}:${data.event_id || data.transition_id}`,
          role: "assistant", intent: kind, content: content || `${formatLabel(kind)}${status ? `: ${status}` : "."}`,
          inserted_at: entry.recorded_at, work_key: detail.work_item.key, evidence: true
        });
      }
    }
    return entries.sort((a, b) => String(a.inserted_at).localeCompare(String(b.inserted_at)) || String(a.id).localeCompare(String(b.id)));
  }

  function renderChatMessage(message) {
    const human = message.role === "human" || message.role === "user";
    const receipt = message.evidence ? "Recorded execution evidence" : message.receipt_stale ? "Saved · acknowledgement not refreshed with this page." : chatCommandLabel(message.command);
    return `<article class="chat-message ${human ? "from-human" : "from-agent"}${message.evidence ? " from-evidence" : ""}" data-message-id="${escapeHtml(message.id)}"><div class="chat-message-meta"><strong>${human ? "You" : message.evidence ? "Agent update" : "Symmetry"}</strong>${message.work_key ? `<span>${escapeHtml(message.work_key)}</span>` : ""}<span>${escapeHtml(formatLabel(message.intent))}</span><time datetime="${escapeHtml(message.inserted_at)}">${escapeHtml(formatTime(message.inserted_at))}</time></div><div class="chat-message-content">${escapeHtml(message.content)}</div><div class="chat-message-receipt">${escapeHtml(receipt)}</div></article>`;
  }

  function renderChatSnapshot({ older = false } = {}) {
    const scroll = $("#chat-scroll");
    const nearBottom = scroll.scrollHeight - scroll.scrollTop - scroll.clientHeight < 80;
    const oldHeight = scroll.scrollHeight;
    const oldTop = scroll.scrollTop;
    const snapshot = chat.snapshot;
    const messages = chatTimelineMessages();
    const markup = messages.length ? messages.map(renderChatMessage).join("") : '<div class="chat-empty"><span class="chat-empty-mark">' + icon("review") + '</span><h2>A clear goal goes a long way.</h2><p>Discuss the work, ask for status, or choose Start work to put an agent on it.</p><p class="chat-empty-note">Routine choices stay with the agent. Consequential decisions come back to you.</p></div>';
    setChatMarkup($("#chat-messages"), markup);
    $("#chat-load-older").hidden = !snapshot.next_before;
    if (older) scroll.scrollTop = oldTop + scroll.scrollHeight - oldHeight;
    else scroll.scrollTop = nearBottom ? scroll.scrollHeight : oldTop;
    renderChatRuns();
    renderChatComposer();
  }

  function chatDelivery(item) {
    const url = item.pull_request_url;
    const safeUrl = url && /^https?:\/\//i.test(url);
    return `<div class="chat-delivery">${safeUrl ? `<a class="detail-link" href="${escapeHtml(url)}" target="_blank" rel="noreferrer">Open PR ${icon("external")}</a>${sourceLabel(item.delivery?.pull_request)}` : '<span>No pull request yet</span>'}<span>CI <strong class="${item.ci_status === "failed" ? "is-error" : ""}">${escapeHtml(formatLabel(item.ci_status))}</strong>${sourceLabel(item.delivery?.ci)}</span><span>Review <strong>${escapeHtml(formatLabel(item.review_status))}</strong>${sourceLabel(item.delivery?.review)}</span></div>`;
  }

  function renderChatRuns() {
    const root = $("#chat-run-context");
    const active = root.contains(document.activeElement) ? document.activeElement : null;
    const focusId = active?.id;
    const focusAction = active ? ["chatControl", "chatRun", "chatWork", "chatDecision"].find((key) => active.dataset[key]) : null;
    const selection = active && typeof active.selectionStart === "number" ? [active.selectionStart, active.selectionEnd] : null;
    const scrollTop = root.scrollTop;
    const diagnosticOpen = $("details.chat-diagnostics", root)?.open;
    const runs = chat.snapshot?.runs || [];
    const detail = currentChatRun();
    let markup;
    if (detail) {
      const item = detail.work_item;
      const execution = item.execution;
      const outcome = detail.outcome || {};
      const busy = chat.pending.has(chat.contextKey);
      const waiting = execution.waiting;
      const decision = waiting?.decision || waiting?.payload?.decision;
      const failure = chatFailureText(outcome.failure);
      const failureMarkup = failure ? `<section class="failure-panel chat-failure"><strong>Run failed</strong><p>${escapeHtml(failure)}</p></section>` : "";
      const decisionMarkup = waiting ? `<section class="chat-decision"><p class="eyebrow">YOUR DECISION</p><h3>${escapeHtml(waiting.question || "A decision is needed to continue.")}</h3>${decision ? `<p class="source-label">${escapeHtml(formatLabel(decision.reason))}</p><p>${escapeHtml(decision.context)}</p><div class="chat-decision-options">${(decision.options || []).map((option) => `<button class="chat-decision-option" id="chat-option-${escapeHtml(option.id)}" type="button" data-chat-decision="${escapeHtml(option.id)}" ${busy ? "disabled" : ""}><strong>${escapeHtml(option.label)}${decision.recommended_option_id === option.id ? '<span class="chat-recommendation">Recommended</span>' : ""}</strong><span>${escapeHtml(option.consequence)}</span></button>`).join("")}</div>` : '<form id="chat-decision-form"><label class="field"><span>Your response</span><input id="chat-decision-answer" required maxlength="20000"></label><button class="button button-primary" type="submit">Send decision</button></form>'}</section>` : "";
      const summaryMarkup = failureMarkup + (outcome.summary || (!waiting && !failure) ? `<p class="chat-run-summary">${escapeHtml(outcome.summary || "The agent has not recorded a summary yet.")}</p>` : "");
      markup = `<div class="chat-context-heading"><p class="eyebrow">SAME WORK. SHARED STATE.</p><span class="project-key">${escapeHtml(item.key)} · Attempt ${execution.generation}</span><h2>${escapeHtml(item.title)}</h2><span class="state-badge ${escapeHtml(execution.state)}">${escapeHtml(formatLabel(execution.state))}</span></div>${decisionMarkup}${summaryMarkup}${outcome.blocker && !waiting ? `<p class="chat-blocker">${icon("alert")}${escapeHtml(outcome.blocker)}</p>` : ""}${chatDelivery(item)}<div class="chat-run-controls">${execution.can_pause ? `<button class="button" type="button" data-chat-control="pause" ${busy ? "disabled" : ""}>Pause</button>` : ""}${execution.can_resume ? `<button class="button" type="button" data-chat-control="resume" ${busy ? "disabled" : ""}>Resume</button>` : ""}${execution.can_cancel ? `<button class="button button-danger" type="button" data-chat-control="cancel" ${busy ? "disabled" : ""}>Cancel run</button>` : ""}</div>${execution.latest_command ? `<p class="chat-control-status">${escapeHtml(formatLabel(execution.latest_command.kind))}: ${escapeHtml(chatCommandLabel(execution.latest_command))}</p>` : ""}${execution.supervisory_control === false && ["running", "paused"].includes(execution.state) ? '<p class="form-note">This runtime does not support safe-boundary guidance or pause.</p>' : ""}${renderListSection("Important findings", outcome.findings, "message")}${renderListSection("Changed artifacts", outcome.changed_artifacts, "path")}${renderTestSection(outcome.tests)}<button class="button" id="chat-open-details" type="button">Open work details</button><details class="chat-diagnostics"><summary>Execution details &amp; diagnostics</summary><p>Inspect the complete run history and raw execution data in work details.</p><p>Run ${escapeHtml(execution.run_id)}</p><p>Generation ${execution.generation}</p><button class="button" type="button" data-chat-diagnostics>Inspect execution</button></details>`;
    } else {
      markup = `<div class="chat-context-heading"><p class="eyebrow">SHARED WORK STATE</p><h2>${chat.scope === "run" ? "Select a run" : "Work in this context"}</h2><p>Progress, blockers, PRs, and CI are shared with the Board.</p></div>${runs.length ? runs.map((run) => {
        const item = run.work_item;
        return `<article class="chat-work-card"><span class="project-key">${escapeHtml(item.key)}</span><h3>${escapeHtml(item.title)}</h3><span class="state-badge ${escapeHtml(item.execution?.state || item.status)}">${escapeHtml(formatLabel(item.execution?.state || item.status))}</span><p>${escapeHtml(run.outcome?.summary || item.description || "No summary yet.")}</p>${chatDelivery(item)}${item.execution?.run_id ? `<button class="button" type="button" data-chat-run="${escapeHtml(item.execution.run_id)}">Open run Chat</button>` : `<button class="button" type="button" data-chat-work="${escapeHtml(item.id)}">Open work details</button>`}</article>`;
      }).join("") : '<p class="chat-no-work">No work recorded in this context yet. Choose Start work to begin.</p>'}`;
    }
    const transitionId = detail?.work_item.execution?.waiting?.transition_id || "";
    const legacyDraft = root.dataset.waitingTransition === transitionId ? $("#chat-decision-answer")?.value : null;
    setChatMarkup(root, markup);
    root.dataset.waitingTransition = transitionId;
    if (legacyDraft && $("#chat-decision-answer")) $("#chat-decision-answer").value = legacyDraft;
    if (diagnosticOpen) $("details.chat-diagnostics", root)?.setAttribute("open", "");
    if (active && document.activeElement === document.body) {
      const matchingAction = focusAction ? $$("button", root).find((button) => button.dataset[focusAction] === active.dataset[focusAction]) : null;
      const diagnosticTarget = active.matches(".chat-diagnostics summary") ? $(".chat-diagnostics summary", root) : active.hasAttribute("data-chat-diagnostics") ? $("[data-chat-diagnostics]", root) : null;
      const target = (focusId && document.getElementById(focusId)) || matchingAction || diagnosticTarget || root;
      if (target === root) root.tabIndex = -1;
      target.focus({ preventScroll: true });
      if (selection && target?.setSelectionRange && legacyDraft !== null) target.setSelectionRange(...selection);
    }
    root.scrollTop = scrollTop;
  }

  async function openRunChat(runId) {
    saveChatDraft();
    chat.scope = "run";
    chat.runId = runId;
    closeDrawer();
    setView("chat");
  }

  async function openChatWorkDetail(item, trigger) {
    if (item.project_id !== state.selectedProjectId) {
      await switchProject(item.project_id);
      if (state.selectedProjectId !== item.project_id || state.activeView !== "chat") return;
    }
    await openWorkItem(item.id, { trigger });
  }

  async function sendChat(payload, { clearDraft = false } = {}) {
    const key = chat.contextKey;
    const projectSwitchId = state.projectSwitchRequestId;
    const ownsRefresh = () => chat.contextKey === key && state.activeView === "chat" && state.projectSwitchTargetId === null && state.projectSwitchRequestId === projectSwitchId;
    if (chat.pending.has(key)) return;
    chat.pending.add(key);
    chat.notices.delete(key);
    renderChatComposer();
    renderChatRuns();
    let action;
    let saved = false;
    try {
      const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(JSON.stringify(payload)));
      action = actionId("chat", key, Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, "0")).join(""));
      const result = await request("/portal/api/chat/messages", { method: "POST", body: JSON.stringify({ ...payload, action_id: action.id }) });
      saved = true;
      clearActionId(action.key);
      chat.notices.set(key, { command: result.command, message: result.command ? chatCommandLabel(result.command) : "Saved to durable history." });
      if (clearDraft) {
        if (chat.contextKey === key && $("#chat-content").value === payload.content) $("#chat-content").value = "";
        const draft = chat.drafts.get(key);
        if (draft?.["chat-content"] === payload.content) draft["chat-content"] = "";
      }
      if (ownsRefresh()) {
        await loadChat({ silent: true });
        if (ownsRefresh()) await loadWorkspace({ silent: true, refreshChat: false });
      }
    } catch (error) {
      if (action && ["invalid_request", "state_conflict", "stale", "not_found", "unsupported_control"].includes(error.code)) clearActionId(action.key);
      chat.notices.set(key, { message: saved ? `Saved to durable history, but refresh failed: ${error.message}. Refresh to see the latest work.` : `Send not confirmed: ${error.message}. Your draft is kept; retry safely.`, error: true });
      if (ownsRefresh() && ["state_conflict", "stale"].includes(error.code)) await loadChat({ silent: true });
    } finally {
      chat.pending.delete(key);
      if (chat.contextKey === key) {
        renderChatComposer();
        renderChatRuns();
      }
    }
  }

  function bindChatEvents() {
    $("#chat-scope").addEventListener("change", (event) => {
      saveChatDraft();
      chat.scope = event.target.value;
      loadChat();
    });
    $("#chat-run-select").addEventListener("change", (event) => {
      saveChatDraft();
      chat.runId = event.target.value || null;
      loadChat();
    });
    $("#chat-intent").addEventListener("change", renderChatComposer);
    $("#chat-target-project").addEventListener("change", () => { renderChatWorkResources(); renderChatComposer(); });
    $("#chat-load-older").addEventListener("click", () => loadChat({ older: true }));
    $("#chat-form").addEventListener("submit", (event) => {
      event.preventDefault();
      if ($("#chat-send").disabled) return;
      const content = $("#chat-content").value;
      if (!content.trim()) return;
      const intent = $("#chat-intent").value;
      if (intent === "guidance" && new TextEncoder().encode(content).byteLength > 32_768) {
        chat.notices.set(chat.contextKey, { message: "Guidance must be 32,768 UTF-8 bytes or fewer.", error: true });
        renderChatComposer();
        return;
      }
      const payload = { ...chatScope(), intent, content };
      if (intent === "start_work") {
        payload.work = {};
        for (const [field, selector] of [["title", "#chat-work-title"], ["agent_profile", "#chat-agent-profile"], ["workspace", "#chat-workspace"], ["repository_resource_id", "#chat-repository"], ["ci_resource_id", "#chat-ci"]]) {
          if ($(selector).value.trim()) payload.work[field] = $(selector).value.trim();
        }
        if (chat.scope === "workspace") payload.target_project_id = $("#chat-target-project").value;
      }
      if (intent === "guidance") {
        const item = currentChatRun()?.work_item;
        if (!item?.execution?.can_guide) return;
        Object.assign(payload, { work_item_id: item.id, generation: item.execution.generation });
      }
      sendChat(payload, { clearDraft: true });
    });
    $("#chat-run-context").addEventListener("click", async (event) => {
      const button = event.target.closest("button");
      if (!button || button.disabled) return;
      if (button.dataset.chatRun) return openRunChat(button.dataset.chatRun);
      if (button.dataset.chatWork) {
        const detail = chat.snapshot?.runs.find((candidate) => candidate.work_item.id === button.dataset.chatWork);
        if (detail) return openChatWorkDetail(detail.work_item, button);
      }
      const item = currentChatRun()?.work_item;
      if (!item) return;
      if (button.id === "chat-open-details" || button.hasAttribute("data-chat-diagnostics")) return openChatWorkDetail(item, button);
      const intent = button.dataset.chatControl || (button.dataset.chatDecision ? "decision" : null);
      if (!intent) return;
      const key = chat.contextKey;
      const payload = { ...chatScope(), intent, content: intent === "decision" ? `Choose ${button.querySelector("strong").firstChild.textContent.trim()}` : `${formatLabel(intent)} this run.`, work_item_id: item.id, generation: item.execution.generation };
      if (intent === "decision") Object.assign(payload, { waiting_transition_id: item.execution.waiting.transition_id, option_id: button.dataset.chatDecision });
      if (intent === "cancel" && !await confirmAction("Cancel this run", "End this execution and keep its history and artifacts?", "Cancel run")) return;
      if (key !== chat.contextKey) return;
      await sendChat(payload);
    });
    $("#chat-run-context").addEventListener("submit", (event) => {
      if (event.target.id !== "chat-decision-form") return;
      event.preventDefault();
      const item = currentChatRun()?.work_item;
      if (!item?.execution?.waiting) return;
      sendChat({ ...chatScope(), intent: "decision", content: $("#chat-decision-answer").value, work_item_id: item.id, generation: item.execution.generation, waiting_transition_id: item.execution.waiting.transition_id });
    });
  }

  function setView(view, updateHash = true) {
    const nextView = validViews.has(view) ? view : "board";
    if (nextView !== state.activeView) invalidateMutationContext();
    state.activeView = nextView;
    if (updateHash && window.location.hash !== `#${state.activeView}`) history.replaceState(null, "", `${window.location.pathname}${window.location.search}#${state.activeView}`);
    renderNavigation();
    if (state.workspace && state.activeView === "connections") renderConnectionsView();
    if (state.workspace && state.activeView === "chat") loadChat();
  }

  function syncLocation() {
    const url = new URL(window.location.href);
    if (state.selectedProjectId) url.searchParams.set("project_id", state.selectedProjectId);
    else url.searchParams.delete("project_id");
    url.hash = state.activeView;
    if (state.activeView === "chat") {
      url.searchParams.set("chat_scope", chat.scope);
      if (chat.scope === "run" && chat.runId) url.searchParams.set("run_id", chat.runId);
      else url.searchParams.delete("run_id");
    } else {
      url.searchParams.delete("chat_scope");
      url.searchParams.delete("run_id");
    }
    history.replaceState(null, "", url);
  }

  function dialogOpen() {
    return $$('dialog[open]').length > 0;
  }

  function bindEvents() {
    bindChatEvents();
    $$('[data-view]').forEach((link) => link.addEventListener("click", (event) => {
      event.preventDefault();
      setView(link.dataset.view);
    }));
    window.addEventListener("hashchange", () => setView(window.location.hash.slice(1), false));

    $("#project-switcher").addEventListener("change", (event) => switchProject(event.target.value || null));
    $("#work-search").addEventListener("input", (event) => {
      state.query = event.target.value;
      if (selectedProject()) renderBoard(selectedProject().work_items);
    });
    $$(".segment").forEach((button) => button.addEventListener("click", () => {
      state.filter = button.dataset.filter;
      $$(".segment").forEach((candidate) => candidate.classList.toggle("is-active", candidate === button));
      if (selectedProject()) renderBoard(selectedProject().work_items);
    }));
    $("#refresh-button").addEventListener("click", () => loadWorkspaceWithFeedback());
    $("#new-item-button").addEventListener("click", () => {
      if (state.projectSwitchTargetId === null) openWorkItemDialog();
    });
    $("#new-project-button").addEventListener("click", () => {
      if (state.projectSwitchTargetId === null) openProjectDialog();
    });
    $("#project-settings-button").addEventListener("click", () => {
      if (state.projectSwitchTargetId === null) openProjectDialog(selectedProject());
    });
    $("#resources-add-button").addEventListener("click", () => openResourceDialog());
    $("#connections-add-button").addEventListener("click", () => openConnectionDialog());
    $("#sync-project-button").addEventListener("click", syncProject);
    $("#close-detail-button").addEventListener("click", closeDrawer);
    $("#drawer-backdrop").addEventListener("click", closeDrawer);
    $$('[data-close-dialog]').forEach((button) => button.addEventListener("click", () => button.closest("dialog").close()));
    $$('dialog').forEach((dialog) => dialog.addEventListener("close", invalidateMutationContext));
    $("#resource-dialog").addEventListener("close", () => {
      const resourceId = state.editingResourceId;
      const resource = selectedProject()?.resources.find((candidate) => candidate.id === resourceId);
      state.editingResourceId = null;
      if (resourceId &&
          !resourceOperationPending(resource) &&
          !state.persistentStaleEditors.has(`resource:${resourceId}`) &&
          state.staleEditors.delete(`resource:${resourceId}`)) {
        reconcileResourceButtons(resourceId);
      }
    });
    $("#connection-dialog").addEventListener("close", () => {
      const connectionId = state.editingConnectionId;
      state.editingConnectionId = null;
      if (connectionId &&
          !connectionBusy(connectionId) &&
          !state.persistentStaleEditors.has(`connection:${connectionId}`) &&
          state.staleEditors.delete(`connection:${connectionId}`)) {
        reconcileConnectionButtons(connectionId);
      }
    });
    document.addEventListener("keydown", (event) => {
      trapDrawerFocus(event);
      if (event.key === "Escape" && $("#detail-drawer").classList.contains("is-open") && !dialogOpen()) closeDrawer();
    });
    document.addEventListener("visibilitychange", () => {
      if (document.visibilityState === "visible") refreshWorkspaceInBackground(true);
    });

    $("#work-item-form").addEventListener("submit", handleWorkItemSubmit);
    $("#project-form").addEventListener("submit", handleProjectSubmit);
    $("#resource-form").addEventListener("submit", handleResourceSubmit);
    $("#resource-form").elements.kind.addEventListener("change", () => {
      syncResourceReferenceControl();
      syncResourceConnectionControl("");
    });
    $("#resource-form").elements.connection_id.addEventListener("change", () => syncResourceConnectionControl());
    $("#delete-resource-button").addEventListener("click", handleResourceDelete);
    $("#sync-resource-button").addEventListener("click", () => syncResource(state.editingResourceId, $("#resource-dialog")));
    $("#connection-form").addEventListener("submit", handleConnectionSubmit);
    $("#connection-form").elements.provider.addEventListener("change", syncConnectionAuthControl);
    $$('#connection-form input[name="capabilities"]').forEach((input) => {
      input.addEventListener("change", (event) => syncConnectionCapabilityControls(event.target));
    });
    $("#delete-connection-button").addEventListener("click", handleConnectionDelete);
  }

  async function handleWorkItemSubmit(event) {
    event.preventDefault();
    const form = event.currentTarget;
    await withSubmitLock(form, async () => {
      let owner = null;
      const payload = formPayload(form, Boolean(state.editingWorkItemId));
      payload.blocked = form.elements.blocked.checked;
      delete payload.id;
      const version = Number(payload.version);
      delete payload.version;
      delete payload.status;

      try {
        if (state.editingWorkItemId) {
          const item = selectedItem(state.editingWorkItemId) || state.currentDetail?.work_item;
          await updateWorkItem({ ...item, version }, payload, form);
        } else {
          owner = beginMutation(form.closest("dialog"));
          payload.status = form.elements.status.value;
          await request(`/portal/api/projects/${state.selectedProjectId}/work-items`, { method: "POST", body: JSON.stringify(payload) });
          if (!ownsMutation(owner)) return;
          form.reset();
          closeDialog(form);
          await refreshAfterMutation("Work item created");
        }
      } catch (error) {
        await handleMutationError(error, form, owner);
      }
    });
  }

  async function handleProjectSubmit(event) {
    event.preventDefault();
    const form = event.currentTarget;
    await withSubmitLock(form, async () => {
      const owner = beginMutation(form.closest("dialog"));
      const previousProjectId = state.selectedProjectId;
      const payload = formPayload(form, Boolean(state.editingProjectId));
      const version = Number(payload.version);
      delete payload.id;
      delete payload.version;
      if (state.editingProjectId) delete payload.key;

      try {
        const body = state.editingProjectId
          ? await request(`/portal/api/projects/${state.editingProjectId}`, { method: "PATCH", body: JSON.stringify({ ...payload, version }) })
          : await request("/portal/api/projects", { method: "POST", body: JSON.stringify(payload) });
        if (!ownsMutation(owner)) return;
        state.selectedProjectId = body.project.id;
        closeDialog(form);
        const successMessage = state.editingProjectId ? "Project updated" : "Project created";
        const created = !state.editingProjectId;
        state.editingProjectId = null;
        await refreshAfterMutation(successMessage, {
          projectId: body.project.id,
          onFailure: created
            ? () => {
                state.selectedProjectId = previousProjectId;
                syncLocation();
                renderWorkspace();
              }
            : null
        });
      } catch (error) {
        await handleMutationError(error, form, owner);
      }
    });
  }

  async function handleResourceSubmit(event) {
    event.preventDefault();
    const form = event.currentTarget;
    const editingResourceId = state.editingResourceId;
    const resource = selectedProject()?.resources.find((candidate) => candidate.id === editingResourceId);
    const payload = formPayload(form, Boolean(editingResourceId));
    if (!form.elements.registered_external_ref.disabled) payload.external_ref = form.elements.registered_external_ref.value;
    delete payload.registered_external_ref;
    const version = Number(payload.version);
    delete payload.id;
    delete payload.version;
    const operationKey = resource ? resourceOperationKey(resource) : null;
    if (resource && resourceEditorBlocked(resource)) return;
    if (operationKey && !beginOperation(operationKey)) return;
    if (resource) {
      reconcileResourceEditor(resource);
      reconcileResourceButtons(resource.id);
      reconcileProjectSyncButton();
    }

    try {
      await withSubmitLock(form, async () => {
        const owner = beginMutation(form.closest("dialog"));
        try {
          const path = editingResourceId ? `/portal/api/resources/${editingResourceId}` : `/portal/api/projects/${state.selectedProjectId}/resources`;
          const method = editingResourceId ? "PATCH" : "POST";
          if (editingResourceId) payload.version = version;
          await request(path, { method, body: JSON.stringify(payload) });
          const successMessage = editingResourceId ? "Resource updated" : "Resource attached";
          if (ownsMutation(owner)) closeDialog(form);
          if (resource) {
            state.staleEditors.add(`resource:${resource.id}`);
            reconcileResourceButtons(resource.id);
          }
          const projectId = resource?.project_id || owner.projectId;
          if (state.selectedProjectId === projectId && state.projectSwitchTargetId === null) {
            await refreshAfterMutation(successMessage, { projectId });
          }
        } catch (error) {
          await handleMutationError(error, form, owner, { resourceId: resource?.id, projectId: resource?.project_id || owner.projectId });
        }
      });
    } finally {
      if (operationKey) endOperation(operationKey);
      if (resource) reconcileResourceButtons(resource.id);
      reconcileProjectSyncButton();
      if ($("#resource-dialog").open && state.editingResourceId === resource?.id) reconcileResourceEditor(resource);
    }
  }

  async function handleResourceDelete() {
    const resource = selectedProject()?.resources.find((candidate) => candidate.id === state.editingResourceId);
    if (!resource || resourceActionBusy(resource)) return;
    const operationKey = resourceOperationKey(resource);
    if (!beginOperation(operationKey)) return;
    reconcileResourceEditor(resource);
    reconcileResourceButtons(resource.id);
    reconcileProjectSyncButton();
    let owner = null;
    try {
      if (!await confirmAction("Detach resource", `Detach ${resource.name} from this project?`, "Detach")) return;
      owner = beginMutation($("#resource-dialog"));
      await request(`/portal/api/resources/${resource.id}`, { method: "DELETE", body: JSON.stringify({ version: resource.version }) });
      if (ownsMutation(owner)) {
        $("#resource-dialog").close();
        state.editingResourceId = null;
      }
      state.staleEditors.add(`resource:${resource.id}`);
      reconcileResourceButtons(resource.id);
      if (state.selectedProjectId === resource.project_id && state.projectSwitchTargetId === null) {
        await refreshAfterMutation("Resource detached", { projectId: resource.project_id });
      }
    } catch (error) {
      await handleMutationError(error, $("#resource-form"), owner, { resourceId: resource.id, projectId: resource.project_id });
    } finally {
      endOperation(operationKey);
      reconcileResourceButtons(resource.id);
      reconcileProjectSyncButton();
      if ($("#resource-dialog").open && state.editingResourceId === resource.id) reconcileResourceEditor(resource);
    }
  }

  async function handleConnectionSubmit(event) {
    event.preventDefault();
    const form = event.currentTarget;
    const editingConnectionId = state.editingConnectionId;
    const connection = state.workspace?.connections.find((candidate) => candidate.id === editingConnectionId);
    if (connection && connectionEditorBlocked(connection.id)) return;
    syncConnectionCapabilityControls();
    const payload = formPayload(form, Boolean(editingConnectionId));
    payload.capabilities = $$('input[name="capabilities"]:checked', form).map((input) => input.value);
    const version = Number(payload.version);
    delete payload.id;
    delete payload.version;
    if (editingConnectionId) {
      delete payload.provider;
      payload.version = version;
    }
    const operationKey = connection ? connectionOperationKey(connection.id) : null;
    if (operationKey && !beginOperation(operationKey)) return;
    if (connection) {
      reconcileConnectionEditor(connection);
      reconcileConnectionButtons(connection.id);
    }

    try {
      await withSubmitLock(form, async () => {
        const owner = beginMutation(form.closest("dialog"));
        try {
          const path = editingConnectionId ? `/portal/api/connections/${editingConnectionId}` : "/portal/api/connections";
          const method = editingConnectionId ? "PATCH" : "POST";
          await request(path, { method, body: JSON.stringify(payload) });
          const successMessage = editingConnectionId ? "Connection updated" : "Connection created";
          if (ownsMutation(owner)) closeDialog(form);
          if (connection) {
            state.staleEditors.add(`connection:${connection.id}`);
            reconcileConnectionButtons(connection.id);
          }
          if (state.projectSwitchTargetId === null) await refreshAfterMutation(successMessage);
        } catch (error) {
          await handleMutationError(error, form, owner, { connectionId: connection?.id });
        }
      });
    } finally {
      if (operationKey) endOperation(operationKey);
      if (connection) reconcileConnectionButtons(connection.id);
      if ($("#connection-dialog").open && state.editingConnectionId === connection?.id) reconcileConnectionEditor(connection);
    }
  }

  async function handleConnectionDelete() {
    const connection = state.workspace?.connections.find((candidate) => candidate.id === state.editingConnectionId);
    if (!connection || connectionEditorBlocked(connection.id)) return;
    const operationKey = connectionOperationKey(connection.id);
    if (!beginOperation(operationKey)) return;
    reconcileConnectionEditor(connection);
    reconcileConnectionButtons(connection.id);
    let owner = null;
    let deleted = false;

    try {
      if (!await confirmAction("Delete connection", `Delete ${connection.name}?`, "Delete")) return;
      owner = beginMutation($("#connection-dialog"));
      await request(`/portal/api/connections/${connection.id}`, {
        method: "DELETE",
        body: JSON.stringify({ version: connection.version })
      });
      deleted = true;
      if (ownsMutation(owner)) {
        $("#connection-dialog").close();
        state.editingConnectionId = null;
      }
    } catch (error) {
      await handleMutationError(error, $("#connection-form"), owner, { connectionId: connection.id });
    } finally {
      if (deleted) {
        state.staleEditors.add(`connection:${connection.id}`);
        if (state.projectSwitchTargetId === null) await refreshAfterMutation("Connection deleted");
      }
      endOperation(operationKey);
      reconcileConnectionButtons(connection.id);
      if ($("#connection-dialog").open && state.editingConnectionId === connection.id) reconcileConnectionEditor(connection);
    }
  }

  async function checkConnection(connectionId) {
    const operationKey = connectionOperationKey(connectionId);
    if (!beginOperation(operationKey)) return;
    const returnFocus = captureWorkspaceFocus();
    reconcileConnectionButtons(connectionId);
    let checked = false;
    try {
      await request(`/portal/api/connections/${connectionId}/check`, { method: "POST" });
      checked = true;
    } catch (error) {
      showToast(error.message, "error");
    } finally {
      state.staleEditors.add(`connection:${connectionId}`);
      if (state.projectSwitchTargetId === null) {
        if (checked) await refreshAfterMutation("Connection checked");
        else await loadWorkspaceWithFeedback({ silent: true });
      }
      endOperation(operationKey);
      reconcileConnectionButtons(connectionId);
      restoreLostWorkspaceFocus(returnFocus);
    }
  }

  async function syncResource(resourceId, dialog = null) {
    const projectId = state.selectedProjectId;
    const resource = selectedProject()?.resources.find((candidate) => candidate.id === resourceId);
    if (!resource || selectedProject()?.status !== "active" || resourceActionBusy(resource)) return;
    const operationKey = resourceOperationKey(resource);
    if (!beginOperation(operationKey)) return;
    const returnFocus = captureWorkspaceFocus();
    const owner = dialog ? beginMutation(dialog) : null;
    reconcileResourceButtons(resourceId);
    reconcileProjectSyncButton();
    if (dialog) reconcileResourceEditor();
    let synchronized = false;
    try {
      await request(`/portal/api/resources/${resourceId}/sync`, { method: "POST" });
      synchronized = true;
      if (owner && ownsMutation(owner) && state.editingResourceId === resourceId) {
        dialog.close();
        state.editingResourceId = null;
      }
    } catch (error) {
      if (state.selectedProjectId === projectId) showToast(error.message, "error");
    } finally {
      state.staleEditors.add(`resource:${resourceId}`);
      if (state.selectedProjectId === projectId && state.projectSwitchTargetId === null) {
        if (synchronized) await refreshAfterMutation("Resource synchronized", { projectId });
        else await loadWorkspaceWithFeedback({ silent: true, projectId });
      }
      endOperation(operationKey);
      reconcileResourceButtons(resourceId);
      reconcileProjectSyncButton();
      if (dialog?.open && state.editingResourceId === resourceId) reconcileResourceEditor();
      restoreLostWorkspaceFocus(returnFocus);
    }
  }

  async function syncProject() {
    const project = selectedProject();
    if (!project || project.status !== "active" || projectSyncBusy(project)) return;
    const operationKey = projectSyncKey(project.id);
    if (!beginOperation(operationKey)) return;
    const returnFocus = captureWorkspaceFocus();
    reconcileProjectSyncButton();
    reconcileProjectResources(project.id);
    let synchronized = false;
    try {
      await request(`/portal/api/projects/${project.id}/sync`, { method: "POST" });
      synchronized = true;
    } catch (error) {
      if (state.selectedProjectId === project.id) showToast(projectSyncErrorMessage(error), "error");
    } finally {
      state.staleEditors.add(`project:${project.id}`);
      if (state.selectedProjectId === project.id && state.projectSwitchTargetId === null) {
        if (synchronized) await refreshAfterMutation("Project resources synchronized", { projectId: project.id });
        else await loadWorkspaceWithFeedback({ silent: true, projectId: project.id });
      }
      endOperation(operationKey);
      reconcileProjectSyncButton();
      reconcileProjectResources(project.id);
      restoreLostWorkspaceFocus(returnFocus);
    }
  }

  async function switchProject(projectId) {
    chat.requestId += 1;
    const previousProjectId = state.selectedProjectId;
    const requestId = ++state.projectSwitchRequestId;
    state.projectSwitchTargetId = projectId;
    invalidateMutationContext();
    $$('dialog[open]').forEach((dialog) => dialog.close());
    closeDrawer();
    $(".workspace-content").setAttribute("inert", "");
    $("#project-switcher").setAttribute("aria-busy", "true");
    $("#refresh-button").disabled = true;
    $("#new-item-button").disabled = true;
    $("#new-project-button").disabled = true;
    $("#project-settings-button").disabled = true;

    try {
      const loaded = await loadWorkspace({ silent: true, projectId, resetChatContext: true });
      if (!loaded && requestId === state.projectSwitchRequestId) {
        state.selectedProjectId = previousProjectId;
        syncLocation();
        renderWorkspace();
      }
    } catch (error) {
      if (requestId !== state.projectSwitchRequestId) return;
      state.selectedProjectId = previousProjectId;
      syncLocation();
      renderWorkspace();
      showToast(error.message, "error");
    } finally {
      if (requestId === state.projectSwitchRequestId) {
        state.projectSwitchTargetId = null;
        $(".workspace-content").removeAttribute("inert");
        $("#project-switcher").removeAttribute("aria-busy");
        $("#refresh-button").disabled = false;
        $("#new-project-button").disabled = false;
      }
    }
  }

  async function loadWorkspaceWithFeedback(options) {
    try {
      return await loadWorkspace(options);
    } catch (error) {
      showToast(error.message, "error");
      return false;
    }
  }

  async function refreshAfterMutation(successMessage, options = {}) {
    try {
      const loaded = await loadWorkspace({ silent: true, projectId: options.projectId ?? state.selectedProjectId });
      if (loaded) showToast(successMessage, "success");
      return loaded;
    } catch (error) {
      options.onFailure?.();
      showToast(
        `${successMessage}, but refreshing the workspace failed: ${error.message}. The change was saved; refresh the workspace to load it.`,
        "error"
      );
      return false;
    }
  }

  async function refreshWorkspaceInBackground(showErrors = false) {
    if (state.backgroundRefreshPending || state.projectSwitchTargetId !== null) return false;
    state.backgroundRefreshPending = true;
    try {
      if (showErrors) return await loadWorkspaceWithFeedback({ silent: true });
      return await loadWorkspace({ silent: true }).catch(() => false);
    } finally {
      state.backgroundRefreshPending = false;
    }
  }

  async function start() {
    injectIcons();
    bindEvents();
    const params = new URLSearchParams(window.location.search);
    state.selectedProjectId = params.get("project_id");
    chat.scope = ["workspace", "project", "run"].includes(params.get("chat_scope")) ? params.get("chat_scope") : "project";
    chat.runId = params.get("run_id");
    state.activeView = validViews.has(window.location.hash.slice(1)) ? window.location.hash.slice(1) : "board";
    renderNavigation();
    try {
      await loadWorkspace();
      window.setInterval(() => {
        if (document.visibilityState === "visible" && !dialogOpen()) refreshWorkspaceInBackground();
      }, 5_000);
    } catch (error) {
      showToast(error.message, "error");
    }
  }

  start();
})();
