import { errorMessage, Network, ServerRefusal, UnexpectedResponse, type MoveIntent, type Project, type ProjectChanges, type ProjectDraft, type WorkItem, type WorkItemChanges, type WorkItemDraft } from "../../services/api";

/**
 * The served external-tracker contract names the fields it owns. A field in
 * that list is never offered as writable here, even though Control's own write
 * refusal is narrower (title, description and priority): the served statement,
 * not a restated policy, decides what this surface presents as provider-owned.
 */
const providerOwnedControls: Record<string, readonly string[]> = {
  title: ["title"],
  description: ["description"],
  priority: ["priority"],
  state: ["status"],
  assignee: ["assignee_type", "assignee_name"],
  labels: [],
};

export function providerOwnedControlsOf(item: { external?: { provider_owned_fields?: readonly string[] } | null }): ReadonlySet<string> {
  const owned = new Set<string>();
  for (const field of item.external?.provider_owned_fields ?? []) {
    for (const control of providerOwnedControls[field] ?? []) owned.add(control);
  }
  return owned;
}

/** Goal membership is served, never inferred: the same all-or-nothing predicate Control refuses writes with. */
export function isGoalManaged(item: { goal_managed?: boolean }): boolean {
  return item.goal_managed === true;
}

/**
 * A delivery value is operator-editable only while it is not owned by a
 * provider or by reported execution evidence; the served source states which.
 */
export function deliveryValueEditable(value: { source: string } | null | undefined): boolean {
  return value === null || value === undefined || value.source === "none" || value.source === "manual";
}

export function workItemEditable(projectStatus: string | undefined, item: WorkItem): boolean {
  return !isGoalManaged(item) && projectStatus !== "archived" && typeof item.version === "number";
}

/**
 * A row that does not carry the served revision cannot be written: the version
 * is the optimistic-lock token, and only the server can say what it is. An
 * older partial projection therefore stays readable without offering a write
 * path that the server would refuse as malformed.
 */
export function hasServedRevision(row: { version?: number }): row is { version: number } {
  return typeof row.version === "number";
}

/** A row whose served revision is present; the write paths require it. */
export type VersionedProject = Project & { version: number };
export type VersionedWorkItem = WorkItem & { version: number };

/**
 * The pending work-item creation identity per project. A create that has not
 * been acknowledged — neither created nor replayed — keeps its identity, so
 * closing and reopening the draft after a lost response still replays the
 * original attempt instead of minting a fresh identity and duplicating the row.
 * An acknowledged create clears it, so the next draft gets a new identity, and
 * the store is in memory only: the draft itself does not survive a reload, and
 * this surface keeps no browser-local store.
 */
const pendingCreationIds = new Map<string, string>();

/** The identity every attempt of this project's pending create draft replays. */
export function workItemCreationId(projectId: string): string {
  const pending = pendingCreationIds.get(projectId);
  if (pending !== undefined) return pending;
  const minted = crypto.randomUUID();
  pendingCreationIds.set(projectId, minted);
  return minted;
}

/** A create the server acknowledged (created or replayed) releases its identity. */
export function acknowledgeWorkItemCreation(projectId: string): void {
  pendingCreationIds.delete(projectId);
}

/** Test seam: module-scope pending identities must not leak between cases. */
export function resetPendingWorkItemCreations(): void {
  pendingCreationIds.clear();
}

/**
 * A board move is always a legacy write. A Goal-managed item has no move path
 * at all; a provider-owned state column is not writable, so only ordering
 * (which does not change a provider-owned field) stays available for it.
 */
export function workItemMovePolicy(projectStatus: string | undefined, item: WorkItem): { statusChange: boolean; reorder: boolean } {
  if (isGoalManaged(item) || projectStatus === "archived" || !hasServedRevision(item)) return { statusChange: false, reorder: false };
  return { statusChange: !providerOwnedControlsOf(item).has("status"), reorder: true };
}

/**
 * How a refusal is presented. `fields` keeps the editor open for correction,
 * `retryable` keeps the frozen intent for an explicit retry, and `authoritative`
 * closes the editor and re-reads the server's current state.
 */
export type FailureMode = "fields" | "retryable" | "authoritative";

export function failureMode(error: unknown): FailureMode {
  if (error instanceof ServerRefusal) {
    return error.code === "validation_failed" ? "fields" : "authoritative";
  }
  if (error instanceof Network) return "retryable";
  if (error instanceof UnexpectedResponse) return error.transient ? "retryable" : "authoritative";
  return "authoritative";
}

// The server owns the reason; the console adds the operator's next step.
const refusalGuidance: Record<string, string> = {
  stale: "The server's current state is shown instead.",
  state_conflict: "The server's current state is shown instead.",
  provider_owned: "Fields owned by the external work tracker are edited in that tracker.",
  goal_authority_required: "Goal-managed work is changed through Goal admission, not this surface.",
  not_found: "The server's current state is shown instead.",
  validation_failed: "Correct the highlighted fields and submit again.",
  invalid_request: "Correct the request and submit again.",
  forbidden: "Refresh the page and sign in again if the session was renewed elsewhere.",
};

export function refusalNotice(error: unknown): string {
  if (!(error instanceof ServerRefusal)) return errorMessage(error);
  const guidance = refusalGuidance[error.code];
  return `${error.message} (${error.code})${guidance ? ` — ${guidance}` : ""}`;
}

export interface BoardMoveRequest {
  intent: MoveIntent;
  announcement: string;
}

export interface BoardColumnSnapshot {
  state: string;
  items: readonly WorkItem[];
}

/**
 * The rendered board is the operator's frame of reference: a column move
 * targets the neighbouring rendered column, and an ordering move targets the
 * rendered neighbours. The server still computes final positions across every
 * item; this only decides the confirmed intent.
 */
export function boardMoveRequest(input: {
  direction: "left" | "right" | "up" | "down";
  item: WorkItem;
  columns: readonly BoardColumnSnapshot[];
  stateLabel: (state: string) => string;
  /** The served revision the move is confirmed against. */
  version: number;
}): BoardMoveRequest | null {
  const { direction, item, columns, stateLabel, version } = input;
  const columnIndex = columns.findIndex((column) => column.items.some((candidate) => candidate.id === item.id));
  const column = columns[columnIndex];
  if (column === undefined) return null;
  const index = column.items.findIndex((candidate) => candidate.id === item.id);
  if (index === -1) return null;
  if (direction === "left" || direction === "right") {
    const neighbour = columns[columnIndex + (direction === "left" ? -1 : 1)];
    if (neighbour === undefined) return null;
    return {
      intent: { version, status: neighbour.state, beforeId: null },
      announcement: `Moved to ${stateLabel(neighbour.state)}`,
    };
  }
  if (direction === "up") {
    const before = column.items[index - 1];
    if (before === undefined) return null;
    return { intent: { version, status: item.status, beforeId: before.id }, announcement: "Moved up" };
  }
  const after = column.items[index + 2];
  if (column.items[index + 1] === undefined) return null;
  return { intent: { version, status: item.status, beforeId: after?.id ?? null }, announcement: "Moved down" };
}

export interface ProjectFormState {
  key: string;
  name: string;
  description: string;
  default_agent_profile: string;
  default_workspace: string;
  status: string;
}

export function projectFormStateOf(project: Project | null): ProjectFormState {
  return {
    key: project?.key ?? "",
    name: project?.name ?? "",
    description: project?.description ?? "",
    default_agent_profile: project?.default_agent_profile ?? "",
    default_workspace: project?.default_workspace ?? "",
    status: project?.status ?? "active",
  };
}

/** The create changeset's own cast list, with empty optionals left to the server defaults. */
export function projectCreatePayload(form: ProjectFormState): ProjectDraft {
  return {
    key: form.key.trim(),
    name: form.name,
    description: form.description,
    default_agent_profile: form.default_agent_profile.trim() || "default",
    default_workspace: form.default_workspace.trim() || "primary",
  };
}

/** The update changeset's cast list; the key is not updatable and is never sent. */
export function projectUpdatePayload(form: ProjectFormState, version: number): ProjectChanges {
  return {
    version,
    name: form.name,
    description: form.description,
    default_agent_profile: form.default_agent_profile.trim() || "default",
    default_workspace: form.default_workspace.trim() || "primary",
    status: form.status,
  };
}

export interface WorkItemFormState {
  title: string;
  description: string;
  status: string;
  priority: string;
  assignee_type: string;
  assignee_name: string;
  agent_profile: string;
  workspace: string;
  repository_resource_id: string;
  ci_resource_id: string;
  branch: string;
  blocked: boolean;
  blocker: string;
  pull_request_url: string;
  ci_status: string;
  review_status: string;
}

export function workItemFormStateOf(project: Project, item: WorkItem | null): WorkItemFormState {
  return {
    title: item?.title ?? "",
    description: item?.description ?? "",
    status: item?.status ?? "backlog",
    priority: item?.priority ?? "no_priority",
    assignee_type: item?.assignee?.type ?? "unassigned",
    assignee_name: item?.assignee?.name ?? "",
    agent_profile: item?.assignee?.agent_profile ?? "",
    workspace: item?.workspace ?? project.default_workspace ?? "",
    repository_resource_id: item?.repository_resource_id ?? "",
    ci_resource_id: item?.ci_resource_id ?? "",
    branch: item?.branch ?? "",
    blocked: item?.blocked ?? false,
    blocker: item?.blocker ?? "",
    pull_request_url: item?.delivery?.pull_request.value ?? item?.pull_request_url ?? "",
    ci_status: item?.delivery?.ci.value ?? "",
    review_status: item?.delivery?.review.value ?? "",
  };
}

/** The draft union is built once and can be replayed verbatim: a retry never re-derives it. */
export function workItemDraftOf(form: WorkItemFormState, options: { includeStatus: boolean; deliveryEditable: { pullRequest: boolean; ci: boolean; review: boolean } }): WorkItemDraft {
  return {
    title: form.title,
    ...(form.description === "" ? {} : { description: form.description }),
    ...(options.includeStatus ? { status: form.status } : {}),
    priority: form.priority,
    assignee_type: form.assignee_type,
    ...(form.assignee_name === "" ? {} : { assignee_name: form.assignee_name }),
    ...(form.agent_profile === "" ? {} : { agent_profile: form.agent_profile }),
    ...(form.workspace === "" ? {} : { workspace: form.workspace }),
    repository_resource_id: form.repository_resource_id === "" ? null : form.repository_resource_id,
    ci_resource_id: form.ci_resource_id === "" ? null : form.ci_resource_id,
    ...(form.branch === "" ? {} : { branch: form.branch }),
    blocked: form.blocked,
    ...(form.blocker === "" ? {} : { blocker: form.blocker }),
    ...(options.deliveryEditable.pullRequest && form.pull_request_url !== "" ? { pull_request_url: form.pull_request_url } : {}),
    ...(options.deliveryEditable.ci && form.ci_status !== "" ? { ci_status: form.ci_status } : {}),
    ...(options.deliveryEditable.review && form.review_status !== "" ? { review_status: form.review_status } : {}),
  };
}

/**
 * The update payload is the draft plus the fields the operator *cleared*: Ecto
 * treats an empty string as "no change", so a deliberate clear travels as null
 * for the fields Control can clear. The delivery statuses and the pull request
 * URL are not clearable through Control's update changeset (its validators
 * refuse nil), so an empty control there is left out and reported instead.
 */
export function workItemUpdatePayload(form: WorkItemFormState, version: number, deliveryEditable: { pullRequest: boolean; ci: boolean; review: boolean }): WorkItemChanges {
  const draft = workItemDraftOf(form, { includeStatus: false, deliveryEditable });
  const cleared: Record<string, null> = {};
  for (const key of ["description", "assignee_name", "agent_profile", "workspace", "branch", "blocker"] as const) {
    if (form[key] === "" && !(key in draft)) cleared[key] = null;
  }
  return { ...draft, ...cleared, version };
}

/**
 * Drops the fields the served external-tracker contract owns before an update
 * is sent: the editor renders them read-only, so a submission must not carry a
 * value the operator could not edit. Control refuses a changed provider-owned
 * field; omitting even the unchanged ones keeps the payload equal to the
 * operator's actual intent.
 */
export function withoutProviderOwned(payload: WorkItemChanges, providerOwned: ReadonlySet<string>): WorkItemChanges {
  if (providerOwned.size === 0) return payload;
  const retained = Object.entries(payload).filter(([key]) => key === "version" || !providerOwned.has(key));
  // The retained entries are a subset of the typed payload's own keys.
  return Object.fromEntries(retained) as WorkItemChanges;
}
