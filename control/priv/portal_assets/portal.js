(() => {
  "use strict";

  const columns = [
    ["backlog", "Backlog"],
    ["ready", "Ready"],
    ["in_progress", "In progress"],
    ["review", "Review"],
    ["done", "Done"]
  ];

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
    editingProjectId: null,
    workspaceRequestId: 0,
    detailRequestId: 0,
    mutationRequestId: 0
  };

  const $ = (selector, root = document) => root.querySelector(selector);
  const $$ = (selector, root = document) => Array.from(root.querySelectorAll(selector));
  const csrfToken = $("meta[name='csrf-token']")?.content || "";

  class PortalRequestError extends Error {
    constructor(message, code, status, fields) {
      super(message);
      this.code = code;
      this.status = status;
      this.fields = fields;
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
    return String(value || "unknown")
      .replaceAll("_", " ")
      .replace(/\b\w/g, (letter) => letter.toUpperCase());
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
      throw new PortalRequestError("Session expired", "unauthenticated", 401);
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
        response.status,
        fields
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

  async function loadWorkspace({ silent = false, refreshDetail = true } = {}) {
    const requestId = ++state.workspaceRequestId;
    const selectedAtStart = state.selectedProjectId;
    if (!silent) renderLoading();

    const query = selectedAtStart ? `?project_id=${encodeURIComponent(selectedAtStart)}` : "";
    const workspace = await request(`/portal/api/workspace${query}`);
    if (requestId !== state.workspaceRequestId || state.selectedProjectId !== selectedAtStart) return;

    state.workspace = workspace;
    state.selectedProjectId = workspace.selected_project_id;
    syncLocation();
    renderWorkspace();
    if ($("#resource-dialog").open) {
      const form = $("#resource-form");
      const selectedReference = form.elements.registered_external_ref.value || form.elements.external_ref.value;
      syncResourceReferenceControl(selectedReference);
    }

    if (refreshDetail && state.activeWorkItemId && !dialogOpen()) {
      await openWorkItem(state.activeWorkItemId, { preserveDrawer: true });
    }
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
      return;
    }

    const archived = project.status === "archived";
    $("#new-item-button").disabled = archived;
    $("#resources-add-button").disabled = archived;
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
    const siblings = selectedProject().work_items.filter((candidate) => candidate.status === item.status);
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
      ? resources.map((resource) => `<button class="resource-row" type="button" data-resource-id="${resource.id}"><span class="resource-kind">${escapeHtml(formatLabel(resource.kind))}</span><span class="resource-primary"><strong>${escapeHtml(resource.name)}</strong><span>${escapeHtml(resource.provider || resource.external_ref || "Manual")}</span></span><span><span class="state-badge ${resource.status}">${formatLabel(resource.status)}</span></span><span><span class="state-badge ${resource.sync_status}">${formatLabel(resource.sync_status)}</span></span><span class="resource-message">${escapeHtml(resource.status_message || "No issues")}</span></button>`).join("")
      : '<div class="empty-view"><h2>No resources attached</h2><p>Attach a repository, tracker, CI system, runtime, agent, or connection.</p></div>';
    $$('[data-resource-id]', $("#resource-table")).forEach((button) => button.addEventListener("click", () => openResourceDialog(resources.find((resource) => resource.id === button.dataset.resourceId))));

    const runtimes = state.workspace?.runtimes || [];
    $("#runtime-total").textContent = runtimes.length;
    $("#runtime-table").innerHTML = runtimes.length
      ? runtimes.map((runtime) => `<div class="runtime-row"><span class="rail-icon">${icon("agent")}</span><span class="resource-primary"><strong>${escapeHtml(runtime.runtime_name)}</strong><span>${escapeHtml(runtime.machine_name)} · ${escapeHtml(runtime.agent_profile)} · ${escapeHtml(runtime.workspace)}</span></span><span>${runtime.reserved_capacity}/${runtime.capacity} slots</span><span class="state-badge ${runtime.status}">${formatLabel(runtime.status)}</span></div>`).join("")
      : '<div class="empty-view"><h2>No compatible runtimes</h2><p>Work can be queued, but it will not start until a matching runtime is online.</p></div>';
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
        <button class="button" id="edit-item-button" ${archived ? "disabled" : ""}>${icon("settings")}<span>Edit</span></button>
        ${canStart ? `<button class="button button-primary" id="run-item-button">${icon("play")}<span>Start run</span></button>` : ""}
        ${canRetry ? `<button class="button button-primary" id="retry-item-button">${icon("rotate")}<span>Retry</span></button>` : ""}
        ${canCancel ? `<button class="button button-danger" id="cancel-item-button">${icon("x")}<span>Cancel run</span></button>` : ""}
        ${nextAction}
        <select id="detail-status-select" aria-label="Workflow status" ${archived ? "disabled" : ""}>${columns.map(([status, label]) => `<option value="${status}" ${item.status === status ? "selected" : ""}>${label}</option>`).join("")}</select>
        <select id="detail-priority-select" aria-label="Priority" ${archived ? "disabled" : ""}>${["urgent", "high", "medium", "low", "no_priority"].map((priority) => `<option value="${priority}" ${item.priority === priority ? "selected" : ""}>${formatLabel(priority)}</option>`).join("")}</select>
      </div>
      ${item.assignee.type !== "agent" && !execution ? '<div class="inline-notice">Set Owner type to Agent before starting a run.</div>' : ""}
      ${waiting ? `<div class="input-request"><p><strong>Agent needs a decision</strong><br>${escapeHtml(waiting.question || "Provide the requested input to continue.")}</p><form id="provide-input-form" class="input-row"><input name="answer" required aria-label="Response"><button class="button button-primary" type="submit">Send</button></form></div>` : ""}
      <section class="detail-summary"><p>${escapeHtml(outcome.summary || item.description || "No outcome summary yet.")}</p></section>
      ${outcome.failure ? `<section class="detail-section"><h3>Failure</h3><div class="failure-panel"><pre>${escapeHtml(formatOutcome(outcome.failure))}</pre></div></section>` : ""}
      ${hasOutcome(outcome.result) ? `<section class="detail-section"><h3>Completion result</h3><div class="detail-summary"><pre>${escapeHtml(formatOutcome(outcome.result))}</pre></div></section>` : ""}
      <section class="detail-section"><h3>Execution</h3><div class="detail-facts">
        ${detailFact("Phase", `<span class="state-badge ${outcome.phase}">${formatLabel(outcome.phase)}</span>`)}
        ${detailFact("Generation", execution ? String(execution.generation) : "Not started")}
        ${detailFact("Owner", escapeHtml(outcome.owner.name || "Unassigned"))}
        ${detailFact("Repository", escapeHtml(item.repository || "Not linked"))}
        ${detailFact("Branch", escapeHtml(item.branch || "Not created"))}
        ${detailFact("Pull request", item.pull_request_url ? `<a class="detail-link" href="${escapeHtml(item.pull_request_url)}" target="_blank" rel="noreferrer">Open PR ${icon("external")}</a>${sourceLabel(item.delivery.pull_request.source)}` : "Not created")}
        ${detailFact("CI", `<span class="state-badge ${item.ci_status}">${formatLabel(item.ci_status)}</span>${sourceLabel(item.delivery.ci.source)}`)}
        ${detailFact("Review", `<span class="state-badge ${item.review_status}">${formatLabel(item.review_status)}</span>${sourceLabel(item.delivery.review.source)}`)}
        ${outcome.blocker ? detailFact("Blocker", `<span class="state-badge attention">${escapeHtml(outcome.blocker)}</span>`) : ""}
      </div></section>
      ${renderListSection("Important findings", outcome.findings, "message")}
      ${renderListSection("Changed artifacts", outcome.changed_artifacts, "path")}
      ${renderTestSection(outcome.tests)}
      <section class="detail-section"><h3>Meaningful activity</h3>${renderTimeline(detail.timeline)}</section>
      <section class="detail-section"><details class="raw-details"><summary>Raw execution data</summary><pre id="raw-execution-data">${escapeHtml(JSON.stringify(detail.raw, null, 2))}</pre>${detail.raw.next_before ? '<button class="button" id="load-history-button" type="button">Load older</button>' : ""}</details></section>`;

    $("#edit-item-button")?.addEventListener("click", () => openWorkItemDialog(item));
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

  function sourceLabel(source) {
    return source && source !== "none" ? `<small class="source-label">${escapeHtml(formatLabel(source))}</small>` : "";
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
    try {
      const page = await request(`/portal/api/work-items/${workItemId}/timeline?before=${encodeURIComponent(cursor)}`);
      if (requestId !== state.detailRequestId || state.activeWorkItemId !== workItemId || state.currentDetail?.work_item.id !== workItemId) return;
      detail.raw.timeline = detail.raw.timeline.concat(page.items);
      detail.raw.next_before = page.next_before;
      detail.rawHistoryLoaded = true;
      const interaction = captureDetailInteraction();
      interaction.rawOpen = true;
      renderDetail(detail, interaction);
    } catch (error) {
      showToast(error.message, "error");
      button.disabled = false;
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
      showToast("Work item updated", "success");
      await loadWorkspace({ silent: true });
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
      showToast("Agent run queued", "success");
      await loadWorkspace({ silent: true });
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
      showToast("Cancellation requested", "success");
      await loadWorkspace({ silent: true });
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
      showToast("Retry queued", "success");
      await loadWorkspace({ silent: true });
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
        showToast("Input sent", "success");
        await loadWorkspace({ silent: true });
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

  async function handleMutationError(error, form = null, owner = null) {
    if (owner && !ownsMutation(owner)) return;
    showFormError(form, error.message);
    showToast(error.code === "stale" ? "This item changed elsewhere. Current data was reloaded." : error.message, "error");
    if (["stale", "state_conflict"].includes(error.code)) {
      closeDialog(form);
      await loadWorkspace({ silent: true });
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
    if (form.dataset.submitting === "true") return;
    const submit = form.querySelector('[type="submit"]');
    form.dataset.submitting = "true";
    if (submit) submit.disabled = true;
    showFormError(form, "");
    try {
      await operation();
    } finally {
      delete form.dataset.submitting;
      if (submit) submit.disabled = false;
    }
  }

  function repositoryOptions(selectedId = "") {
    const repositories = selectedProject()?.resources.filter((resource) => resource.kind === "repository") || [];
    return ['<option value="">Not linked</option>', ...repositories.map((resource) => `<option value="${resource.id}" ${resource.id === selectedId ? "selected" : ""}>${escapeHtml(resource.name)}</option>`)].join("");
  }

  function syncResourceReferenceControl(selectedValue = "") {
    const form = $("#resource-form");
    const input = form.elements.external_ref;
    const select = form.elements.registered_external_ref;
    const kind = form.elements.kind.value;
    const registered = state.workspace?.registered_runtimes || [];
    const readOnly = selectedProject()?.status !== "active";
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
    $("#intent-lock-note").hidden = !locked;
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
    const form = $("#resource-form");
    const readOnly = selectedProject()?.status !== "active";
    invalidateMutationContext();
    form.reset();
    resetSubmitLock(form);
    showFormError(form, "");
    state.editingResourceId = resource?.id || null;
    form.elements.id.value = resource?.id || "";
    form.elements.version.value = resource?.version || "";
    $("#resource-dialog-title").textContent = resource ? "Update resource" : "Attach resource";
    $("#resource-submit-label").textContent = resource ? "Update" : "Attach";
    $("#delete-resource-button").hidden = !resource || readOnly;
    if (resource) {
      for (const name of ["kind", "status", "sync_status", "name", "provider", "url", "status_message"]) form.elements[name].value = resource[name] || "";
      form.elements.last_checked_at.value = localDateTime(resource.last_checked_at);
      form.elements.last_synced_at.value = localDateTime(resource.last_synced_at);
    }
    $$('input, select, textarea', form).forEach((input) => { if (!input.matches('[type="hidden"]')) input.disabled = readOnly; });
    syncResourceReferenceControl(resource?.external_ref || "");
    form.querySelector('[type="submit"]').disabled = readOnly;
    $("#resource-dialog").showModal();
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

  function setView(view, updateHash = true) {
    const nextView = ["board", "activity", "resources"].includes(view) ? view : "board";
    if (nextView !== state.activeView) invalidateMutationContext();
    state.activeView = nextView;
    if (updateHash && window.location.hash !== `#${state.activeView}`) history.replaceState(null, "", `${window.location.pathname}${window.location.search}#${state.activeView}`);
    renderNavigation();
  }

  function syncLocation() {
    const url = new URL(window.location.href);
    if (state.selectedProjectId) url.searchParams.set("project_id", state.selectedProjectId);
    else url.searchParams.delete("project_id");
    url.hash = state.activeView;
    history.replaceState(null, "", url);
  }

  function dialogOpen() {
    return $$('dialog[open]').length > 0;
  }

  function bindEvents() {
    $$('[data-view]').forEach((link) => link.addEventListener("click", (event) => {
      event.preventDefault();
      setView(link.dataset.view);
    }));
    window.addEventListener("hashchange", () => setView(window.location.hash.slice(1), false));

    $("#project-switcher").addEventListener("change", async (event) => {
      invalidateMutationContext();
      state.selectedProjectId = event.target.value || null;
      closeDrawer();
      await loadWorkspaceWithFeedback();
    });
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
    $("#new-item-button").addEventListener("click", () => openWorkItemDialog());
    $("#new-project-button").addEventListener("click", () => openProjectDialog());
    $("#project-settings-button").addEventListener("click", () => openProjectDialog(selectedProject()));
    $("#resources-add-button").addEventListener("click", () => openResourceDialog());
    $("#close-detail-button").addEventListener("click", closeDrawer);
    $("#drawer-backdrop").addEventListener("click", closeDrawer);
    $$('[data-close-dialog]').forEach((button) => button.addEventListener("click", () => button.closest("dialog").close()));
    $$('dialog').forEach((dialog) => dialog.addEventListener("close", invalidateMutationContext));
    document.addEventListener("keydown", (event) => {
      trapDrawerFocus(event);
      if (event.key === "Escape" && $("#detail-drawer").classList.contains("is-open") && !dialogOpen()) closeDrawer();
    });
    document.addEventListener("visibilitychange", () => { if (document.visibilityState === "visible") loadWorkspaceWithFeedback({ silent: true }); });

    $("#work-item-form").addEventListener("submit", handleWorkItemSubmit);
    $("#project-form").addEventListener("submit", handleProjectSubmit);
    $("#resource-form").addEventListener("submit", handleResourceSubmit);
    $("#resource-form").elements.kind.addEventListener("change", () => syncResourceReferenceControl());
    $("#delete-resource-button").addEventListener("click", handleResourceDelete);
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
          showToast("Work item created", "success");
          await loadWorkspace({ silent: true });
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
        showToast(state.editingProjectId ? "Project updated" : "Project created", "success");
        state.editingProjectId = null;
        await loadWorkspace({ silent: true });
      } catch (error) {
        await handleMutationError(error, form, owner);
      }
    });
  }

  async function handleResourceSubmit(event) {
    event.preventDefault();
    const form = event.currentTarget;
    await withSubmitLock(form, async () => {
      const owner = beginMutation(form.closest("dialog"));
      const payload = formPayload(form, Boolean(state.editingResourceId));
      if (!form.elements.registered_external_ref.disabled) payload.external_ref = form.elements.registered_external_ref.value;
      delete payload.registered_external_ref;
      const version = Number(payload.version);
      delete payload.id;
      delete payload.version;
      try {
        const path = state.editingResourceId ? `/portal/api/resources/${state.editingResourceId}` : `/portal/api/projects/${state.selectedProjectId}/resources`;
        const method = state.editingResourceId ? "PATCH" : "POST";
        if (state.editingResourceId) payload.version = version;
        await request(path, { method, body: JSON.stringify(payload) });
        if (!ownsMutation(owner)) return;
        closeDialog(form);
        showToast(state.editingResourceId ? "Resource updated" : "Resource attached", "success");
        state.editingResourceId = null;
        await loadWorkspace({ silent: true });
      } catch (error) {
        await handleMutationError(error, form, owner);
      }
    });
  }

  async function handleResourceDelete() {
    const resource = selectedProject()?.resources.find((candidate) => candidate.id === state.editingResourceId);
    if (!resource) return;
    if (!await confirmAction("Detach resource", `Detach ${resource.name} from this project?`, "Detach")) return;
    const owner = beginMutation($("#resource-dialog"));
    try {
      await request(`/portal/api/resources/${resource.id}`, { method: "DELETE", body: JSON.stringify({ version: resource.version }) });
      if (!ownsMutation(owner)) return;
      $("#resource-dialog").close();
      state.editingResourceId = null;
      showToast("Resource detached", "success");
      await loadWorkspace({ silent: true });
    } catch (error) {
      await handleMutationError(error, $("#resource-form"), owner);
    }
  }

  async function loadWorkspaceWithFeedback(options) {
    try {
      await loadWorkspace(options);
    } catch (error) {
      showToast(error.message, "error");
    }
  }

  async function start() {
    injectIcons();
    bindEvents();
    const params = new URLSearchParams(window.location.search);
    state.selectedProjectId = params.get("project_id");
    state.activeView = ["board", "activity", "resources"].includes(window.location.hash.slice(1)) ? window.location.hash.slice(1) : "board";
    renderNavigation();
    try {
      await loadWorkspace();
      window.setInterval(() => {
        if (document.visibilityState === "visible" && !dialogOpen()) loadWorkspace({ silent: true }).catch(() => {});
      }, 5_000);
    } catch (error) {
      showToast(error.message, "error");
    }
  }

  start();
})();
