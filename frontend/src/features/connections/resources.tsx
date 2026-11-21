import * as Dialog from "@radix-ui/react-dialog";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { AlertCircle, Plus, X } from "lucide-react";
import { useEffect, useRef, useState, type ReactNode, type RefObject } from "react";
import { restoreFocus } from "../../components/focus";
import { EmptyState, ErrorState, LoadingState } from "../../components/workspace";
import {
  ownValue,
  refusalNotice as sharedRefusalNotice,
  ServerRefusal,
  type Api,
  type Connection,
  type ResourceDraft,
  type Workspace,
} from "../../services/api";
import { safeUrl } from "../../services/served";

// This module owns the project-resource surface on `/portal/connections` and
// the display primitives both surfaces on that page share (status badges, field
// errors, refusal notices, the dialog frame). The connection surface imports
// them so the import direction stays one-way and there is exactly one refusal
// map.

const knownStatuses: Record<string, true> = { healthy: true, degraded: true, offline: true, unknown: true };
const knownSyncStatuses: Record<string, true> = { unknown: true, syncing: true, synced: true, stale: true, failed: true };
const providerLabels: Record<string, string> = { github: "GitHub", azure_devops: "Azure DevOps" };
const kindLabels: Record<string, string> = { repository: "Repository", work_tracking: "Work tracking", ci: "CI", agent: "Agent", runtime: "Runtime", connection: "Connection" };
// Kinds this surface creates. `connection` is a served local kind this surface
// renders but does not offer: it carries no provider state to synchronize and
// no registered reference to select.
const bindableKinds: readonly string[] = ["repository", "work_tracking", "ci", "agent", "runtime"];
// The kind -> connection capability rule the server enforces
// (`ensure_connection_capability/2`): a connection is offered for a kind only
// when it grants that capability, and the server re-checks every write.
const kindCapabilities: Record<string, string> = { repository: "repositories", work_tracking: "work_items", ci: "ci" };
const localKinds: Record<string, true> = { agent: true, runtime: true };
// The server owns the reason; the console adds the operator's next step.
const refusalGuidance: Record<string, string> = {
  stale: "Reload to see the server's current values.",
  state_conflict: "Reload and resolve the changed state.",
  not_found: "Reload the page to see the server's current state.",
  forbidden: "Grant the required provider permissions, then test the connection again.",
  not_connected: "Bind the resource to a connection first.",
  capability_not_granted: "Grant the connection the capability this resource kind needs, then try again.",
  provider_unauthorized: "Reauthenticate the provider session on this server, then test the connection again.",
  provider_failure: "Check provider availability, then test the connection again.",
  validation_failed: "The server rejected this submission; re-read the current values and try again.",
};

export const formatLabel = (value: string) => value.replaceAll("_", " ");
export const providerLabel = (value: string) => ownValue(providerLabels, value) ?? `Unknown provider: ${value || "unspecified"}`;
const kindLabel = (value: string) => ownValue(kindLabels, value) ?? `Unknown kind: ${value}`;
const isLocalKind = (kind: string) => ownValue(localKinds, kind) === true;

export function StatusBadge({ status, scope }: { status: string; scope: "connection" | "resource" | "sync" }) {
  // Own keys only: a served status may be `constructor`, and a plain index
  // would render Object's own member as a "known" label.
  const known = scope === "sync" ? ownValue(knownSyncStatuses, status) : ownValue(knownStatuses, status);
  const tone = status === "healthy" || status === "synced" ? "status-complete" : status === "degraded" || status === "failed" || status === "offline" ? "status-attention" : "";
  return <span className={`status-badge ${tone}`} data-status={status}>{known ? formatLabel(status).replace(/(^|\s)\S/g, (letter) => letter.toUpperCase()) : `Unknown ${scope} status: ${status || "unspecified"}`}</span>;
}

export function FieldErrors({ error, field }: { error: unknown; field: string }) {
  const messages = (error instanceof ServerRefusal && error.fields?.[field]) || [];
  if (messages.length === 0) return null;
  return <p className="field-error" role="alert">{messages.join(" ")}</p>;
}

export const refusalNotice = (error: unknown) => sharedRefusalNotice(error, refusalGuidance);

// Declared policy: a served member is present when non-empty (no trim); the shared `asText` trims that case away.
const presentText = (value: unknown): string | null => typeof value === "string" && value !== "" ? value : null;
const asVersion = (value: unknown): number => typeof value === "number" && Number.isFinite(value) ? value : 0;

// The served rows this surface reads, typed from the generated workspace-read
// DTO: every consumed member is declared in workspace-read.schema.json, stays
// optional (a project row keeps accepting older partial projections) and is
// value-checked here, so a row that omits or malforms a member degrades instead
// of inventing one. Unconsumed served members stay additive.
type ServedProjectRow = Workspace["projects"][number];
type ServedResourceRow = NonNullable<ServedProjectRow["resources"]>[number];

// The decoded row the surface renders; nothing here is invented.
interface ResourceRow {
  id: string;
  kind: string;
  name: string;
  connectionId: string | null;
  provider: string | null;
  externalRef: string | null;
  url: string | null;
  status: string;
  syncStatus: string;
  statusMessage: string | null;
  lastSyncedAt: string | null;
  version: number;
}

interface ProjectRow { id: string; key: string; name: string; status: string; resources: ResourceRow[] }
interface RegisteredRuntime { id: string; name: string; profile: string | null; workspace: string | null }

function decodeResources(resources: readonly ServedResourceRow[] | undefined): ResourceRow[] {
  return (resources ?? []).flatMap((resource) => {
    const id = presentText(resource.id);
    if (id === null) return [];
    return [{
      id,
      kind: presentText(resource.kind) ?? "",
      name: presentText(resource.name) ?? id,
      connectionId: presentText(resource.connection_id),
      provider: presentText(resource.provider),
      externalRef: presentText(resource.external_ref),
      url: presentText(resource.url),
      status: presentText(resource.status) ?? "unknown",
      syncStatus: presentText(resource.sync_status) ?? "unknown",
      statusMessage: presentText(resource.status_message),
      lastSyncedAt: presentText(resource.last_synced_at),
      version: asVersion(resource.version),
    }];
  });
}

function decodeProjects(workspace: Workspace | undefined): ProjectRow[] {
  return (workspace?.projects ?? []).flatMap((project) => {
    const id = presentText(project.id);
    if (id === null) return [];
    return [{
      id,
      key: presentText(project.key) ?? "",
      name: presentText(project.name) ?? id,
      status: presentText(project.status) ?? "unknown",
      resources: decodeResources(project.resources),
    }];
  });
}

function decodeRuntimes(workspace: Workspace | undefined): RegisteredRuntime[] {
  return (workspace?.registered_runtimes ?? []).flatMap((runtime) => {
    const id = presentText(runtime.runtime_id);
    const name = presentText(runtime.runtime_name);
    if (id === null || name === null) return [];
    return [{ id, name, profile: presentText(runtime.agent_profile), workspace: presentText(runtime.workspace) }];
  });
}

const eligibleConnections = (kind: string, connections: readonly Connection[]): readonly Connection[] => {
  const capability = ownValue(kindCapabilities, kind);
  return capability === undefined ? [] : connections.filter((connection) => connection.capabilities.includes(capability));
};

const runtimeLabel = (runtime: RegisteredRuntime) =>
  [runtime.name, runtime.profile ?? "No agent profile", runtime.workspace ?? "No workspace"].join(" · ");

// A local resource takes its reference from the workspace's registered
// runtimes: the profile for `agent`, the runtime id for `runtime`.
function referenceOptions(kind: string, runtimes: readonly RegisteredRuntime[]): readonly { value: string; label: string }[] {
  if (kind === "runtime") return runtimes.map((runtime) => ({ value: runtime.id, label: runtimeLabel(runtime) }));
  if (kind !== "agent") return [];
  const seen = new Set<string>();
  return runtimes.flatMap((runtime) => {
    if (runtime.profile === null || seen.has(runtime.profile)) return [];
    seen.add(runtime.profile);
    return [{ value: runtime.profile, label: runtimeLabel(runtime) }];
  });
}

const resourceSubject = (resourceId: string) => `resource:${resourceId}`;
const projectSubject = (projectId: string) => `project:${projectId}`;

interface DialogFrameProps {
  title: string;
  closeLabel: string;
  descriptionId: string;
  description: ReactNode;
  openerRef: RefObject<HTMLElement | null>;
  fallbackFocus: RefObject<HTMLElement | null>;
  onClose: () => void;
  children: ReactNode;
}

export function DialogFrame({ title, closeLabel, descriptionId, description, openerRef, fallbackFocus, onClose, children }: DialogFrameProps) {
  return <Dialog.Root open onOpenChange={(open) => { if (!open) onClose(); }}>
    <Dialog.Portal>
      <Dialog.Overlay className="dialog-overlay" />
      <Dialog.Content className="command-dialog connection-dialog" aria-describedby={descriptionId}
        onCloseAutoFocus={(event) => {
          const target = openerRef.current;
          openerRef.current = null;
          restoreFocus(event, target, fallbackFocus.current);
        }}>
        <div className="dialog-heading">
          <Dialog.Title>{title}</Dialog.Title>
          <Dialog.Close asChild><button className="icon-button" type="button" aria-label={closeLabel}><X size={20} aria-hidden="true" /></button></Dialog.Close>
        </div>
        <Dialog.Description id={descriptionId} className="muted">{description}</Dialog.Description>
        {children}
      </Dialog.Content>
    </Dialog.Portal>
  </Dialog.Root>;
}

interface ResourceRowProps {
  row: ResourceRow;
  connections: readonly Connection[];
  locked: boolean;
  syncPending: boolean;
  onSync: () => void;
  onUpdate: (opener: HTMLElement) => void;
  onUnbind: (opener: HTMLElement) => void;
}

function ResourceRowItem({ row, connections, locked, syncPending, onSync, onUpdate, onUnbind }: ResourceRowProps) {
  const connection = connections.find((candidate) => candidate.id === row.connectionId);
  const url = safeUrl(row.url);
  return <li className="resource-row">
    <div className="resource-main">
      <div className="resource-title">
        <h3>{row.name}</h3>
        <span className="item-key">{kindLabel(row.kind)}</span>
      </div>
      <p className="muted text-small">
        {row.connectionId === null ? "Local resource" : connection?.name ?? `Unknown connection: ${row.connectionId}`}
        {" · "}{row.externalRef ?? "No external reference"}
      </p>
      {row.url !== null && <p className="text-small">{url === null ? row.url : <a href={url} rel="noreferrer">{row.url}</a>}</p>}
      {row.statusMessage !== null && <p className="blocker"><AlertCircle size={14} aria-hidden="true" />{row.statusMessage}</p>}
      <p className="muted text-small">{row.lastSyncedAt === null ? "Never synced" : `Last synced ${row.lastSyncedAt}`}</p>
    </div>
    <div className="resource-state">
      <StatusBadge status={row.status} scope="resource" />
      <StatusBadge status={row.syncStatus} scope="sync" />
    </div>
    <div className="resource-actions">
      <button className="button" type="button" disabled={locked} aria-busy={syncPending} onClick={onSync}>Sync {row.name}</button>
      <button className="button" type="button" disabled={locked} onClick={(event) => { onUpdate(event.currentTarget); }}>Update {row.name}</button>
      <button className="button" type="button" disabled={locked} onClick={(event) => { onUnbind(event.currentTarget); }}>Unbind {row.name}</button>
    </div>
  </li>;
}

interface BindDialogProps {
  api: Api;
  csrf: string;
  projectId: string;
  connections: readonly Connection[];
  runtimes: readonly RegisteredRuntime[];
  editorId: number;
  openerRef: RefObject<HTMLElement | null>;
  fallbackFocus: RefObject<HTMLElement | null>;
  onSaved: (editorId: number, notice: string) => void;
  onRefresh: () => void;
  onWriteStart: () => void;
  onWriteSettled: () => void;
  onSubjectPending: (subject: string, pending: boolean) => void;
  onClose: () => void;
}

function BindResourceDialog({ api, csrf, projectId, connections, runtimes, editorId, openerRef, fallbackFocus, onSaved, onRefresh, onWriteStart, onWriteSettled, onSubjectPending, onClose }: BindDialogProps) {
  const [kind, setKind] = useState("repository");
  const [connectionId, setConnectionId] = useState(() => eligibleConnections("repository", connections)[0]?.id ?? "");
  const [name, setName] = useState("");
  const [reference, setReference] = useState("");
  const local = isLocalKind(kind);
  const eligible = eligibleConnections(kind, connections);
  const references = referenceOptions(kind, runtimes);
  const save = useMutation({
    mutationFn: (draft: ResourceDraft) => api.createResource(projectId, draft, csrf, new AbortController().signal),
    // The pending counts live on the page, so the exclusion holds after this
    // dialog closes, after a re-read and after a remount.
    onMutate: () => { onWriteStart(); onSubjectPending(projectSubject(projectId), true); },
    onSettled: () => { onWriteSettled(); onSubjectPending(projectSubject(projectId), false); },
    onSuccess: (_receipt, draft) => { onSaved(editorId, `Resource attached — ${draft.name}.`); },
    // A refused write is never rendered as success: re-read the server's state
    // so the surface shows what the server actually holds.
    onError: onRefresh,
  });
  const incomplete = local ? references.length === 0 || reference === "" : connectionId === "";
  return <DialogFrame title="Bind resource" closeLabel="Close bind dialog" descriptionId="resource-bind-description" openerRef={openerRef} fallbackFocus={fallbackFocus} onClose={onClose}
    description="A resource mirrors something the project already has: a provider resource read through a connection, or a registered runtime. The server validates the reference it is given.">
    <form className="connection-form" onSubmit={(event) => {
      event.preventDefault();
      if (save.isPending || incomplete) return;
      save.mutate({
        kind,
        connection_id: local ? null : connectionId === "" ? null : connectionId,
        name,
        external_ref: reference === "" ? null : reference,
      });
    }}>
      <label htmlFor="resource-kind">Type</label>
      <select id="resource-kind" name="kind" value={kind} disabled={save.isPending} onChange={(event) => {
        // Read the DOM value before the deferred updater runs: React resets
        // currentTarget once the event handler returns.
        const next = event.currentTarget.value;
        setKind(next);
        setConnectionId(isLocalKind(next) ? "" : eligibleConnections(next, connections)[0]?.id ?? "");
        setReference(isLocalKind(next) ? referenceOptions(next, runtimes)[0]?.value ?? "" : "");
      }}>
        {bindableKinds.map((value) => <option key={value} value={value}>{kindLabel(value)}</option>)}
      </select>
      <FieldErrors error={save.error} field="kind" />

      <label htmlFor="resource-connection">Provider connection</label>
      <select id="resource-connection" name="connection_id" value={local ? "" : connectionId} disabled={local || save.isPending || eligible.length === 0}
        onChange={(event) => { const next = event.currentTarget.value; setConnectionId(next); }}>
        {local
          ? <option value="">Not required for local resources</option>
          : eligible.length === 0
            ? <option value="">No connection grants the required capability</option>
            : eligible.map((connection) => <option key={connection.id} value={connection.id}>{connection.name}</option>)}
      </select>
      <FieldErrors error={save.error} field="connection_id" />

      <label htmlFor="resource-name">Name</label>
      <input id="resource-name" name="name" value={name} required disabled={save.isPending}
        onChange={(event) => { const next = event.currentTarget.value; setName(next); }} />
      <FieldErrors error={save.error} field="name" />

      <label htmlFor="resource-reference">Reference</label>
      {local
        ? <select id="resource-reference" name="external_ref" value={reference} disabled={save.isPending || references.length === 0}
            onChange={(event) => { const next = event.currentTarget.value; setReference(next); }}>
            {references.length === 0
              ? <option value="">No registered {kindLabel(kind).toLowerCase()} is available</option>
              : references.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
          </select>
        : <input id="resource-reference" name="external_ref" value={reference} disabled={save.isPending}
            onChange={(event) => { const next = event.currentTarget.value; setReference(next); }} />}
      <FieldErrors error={save.error} field="external_ref" />

      {save.error && <p className="notice notice-error" role="alert">{refusalNotice(save.error)}</p>}
      <div className="dialog-actions">
        <button className="button" type="submit" disabled={save.isPending || incomplete} aria-busy={save.isPending}>Bind</button>
        <button className="button" type="button" onClick={onClose} disabled={save.isPending}>Cancel</button>
      </div>
    </form>
  </DialogFrame>;
}

interface UpdateDialogProps {
  api: Api;
  csrf: string;
  resource: ResourceRow;
  connections: readonly Connection[];
  runtimes: readonly RegisteredRuntime[];
  editorId: number;
  openerRef: RefObject<HTMLElement | null>;
  fallbackFocus: RefObject<HTMLElement | null>;
  onSaved: (editorId: number, notice: string) => void;
  onRefresh: () => void;
  onWriteStart: () => void;
  onWriteSettled: () => void;
  onSubjectPending: (subject: string, pending: boolean) => void;
  onClose: () => void;
}

function UpdateResourceDialog({ api, csrf, resource, connections, runtimes, editorId, openerRef, fallbackFocus, onSaved, onRefresh, onWriteStart, onWriteSettled, onSubjectPending, onClose }: UpdateDialogProps) {
  // The facts and the version this dialog opened with: a background refresh that
  // loads another operator's change must not turn the draft into a silent
  // overwrite — the server still answers with its own stale refusal.
  const [subject] = useState(resource);
  const local = isLocalKind(subject.kind);
  const eligible = eligibleConnections(subject.kind, connections);
  const current = connections.find((connection) => connection.id === subject.connectionId);
  const currentIneligible = current !== undefined && !eligible.some((candidate) => candidate.id === current.id);
  // The served binding stays selectable even when the connection no longer
  // grants the capability: hiding it would silently change the resource on
  // save, while keeping it lets the server refuse the submission it refuses.
  const choices = currentIneligible && current !== undefined ? [...eligible, current] : eligible;
  const [connectionId, setConnectionId] = useState(subject.connectionId ?? "");
  const [name, setName] = useState(subject.name);
  const [reference, setReference] = useState(subject.externalRef ?? "");
  const references = referenceOptions(subject.kind, runtimes);
  const referenceKnown = references.some((option) => option.value === reference);
  const save = useMutation({
    mutationFn: (update: { name: string; connection_id: string | null; external_ref: string | null }) =>
      api.updateResource(subject.id, { version: subject.version, ...update }, csrf, new AbortController().signal),
    onMutate: () => { onWriteStart(); onSubjectPending(resourceSubject(subject.id), true); },
    onSettled: () => { onWriteSettled(); onSubjectPending(resourceSubject(subject.id), false); },
    onSuccess: (_receipt, update) => { onSaved(editorId, `Resource updated — ${update.name}.`); },
    onError: onRefresh,
  });
  const stale = save.error instanceof ServerRefusal && save.error.code === "stale";
  const blocked = save.isPending || stale;
  return <DialogFrame title="Update resource" closeLabel="Close update dialog" descriptionId="resource-update-description" openerRef={openerRef} fallbackFocus={fallbackFocus} onClose={onClose}
    description="The facts below are what this server served when the dialog opened; the reference, the connection and the version are validated again on submit.">
    <form className="connection-form" onSubmit={(event) => {
      event.preventDefault();
      if (blocked) return;
      save.mutate({ name, connection_id: local ? null : connectionId === "" ? null : connectionId, external_ref: reference === "" ? null : reference });
    }}>
      <dl className="connection-facts">
        <div><dt>Type</dt><dd>{kindLabel(subject.kind)}</dd></div>
        <div><dt>Provider</dt><dd>{subject.provider === null ? "No provider connection" : providerLabel(subject.provider)}</dd></div>
        <div><dt>Connection</dt><dd>{subject.connectionId === null ? "Local resource" : current?.name ?? `Unknown connection: ${subject.connectionId}`}</dd></div>
        <div><dt>Health</dt><dd><StatusBadge status={subject.status} scope="resource" /></dd></div>
        <div><dt>Sync state</dt><dd><StatusBadge status={subject.syncStatus} scope="sync" /></dd></div>
        <div><dt>Last synced</dt><dd>{subject.lastSyncedAt ?? "Never synced"}</dd></div>
        <div><dt>Status detail</dt><dd>{subject.statusMessage ?? "No issues reported"}</dd></div>
      </dl>

      <label htmlFor="resource-name">Name</label>
      <input id="resource-name" name="name" value={name} required disabled={blocked}
        onChange={(event) => { const next = event.currentTarget.value; setName(next); }} />
      <FieldErrors error={save.error} field="name" />

      <label htmlFor="resource-connection">Provider connection</label>
      <select id="resource-connection" name="connection_id" value={local ? "" : connectionId} disabled={local || blocked}
        onChange={(event) => { const next = event.currentTarget.value; setConnectionId(next); }}>
        {local
          ? <option value="">Not required for local resources</option>
          : <>
            {connectionId !== "" && !choices.some((candidate) => candidate.id === connectionId) &&
              <option value={connectionId}>{`Unknown connection: ${connectionId}`}</option>}
            {connectionId === "" &&
              <option value="">{choices.length === 0 ? "No connection grants the required capability" : "No provider connection"}</option>}
            {choices.map((connection) => <option key={connection.id} value={connection.id}>
              {connection.id === subject.connectionId && currentIneligible ? `${connection.name} (capability not granted)` : connection.name}
            </option>)}
          </>}
      </select>
      <FieldErrors error={save.error} field="connection_id" />

      <label htmlFor="resource-reference">Reference</label>
      {local
        ? <select id="resource-reference" name="external_ref" value={reference} disabled={blocked}
            onChange={(event) => { const next = event.currentTarget.value; setReference(next); }}>
            {reference !== "" && !referenceKnown && <option value={reference}>{`${reference} (not registered)`}</option>}
            {references.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
          </select>
        : <input id="resource-reference" name="external_ref" value={reference} disabled={blocked}
            onChange={(event) => { const next = event.currentTarget.value; setReference(next); }} />}
      <FieldErrors error={save.error} field="external_ref" />

      {save.error && <p className="notice notice-error" role="alert">{stale
        ? `${refusalNotice(save.error)} This resource changed elsewhere; close and reopen it to edit the server's current values.`
        : refusalNotice(save.error)}</p>}
      <div className="dialog-actions">
        <button className="button" type="submit" disabled={blocked} aria-busy={save.isPending}>Save</button>
        <button className="button" type="button" onClick={onClose} disabled={save.isPending}>Cancel</button>
      </div>
    </form>
  </DialogFrame>;
}

interface UnbindDialogProps {
  api: Api;
  csrf: string;
  resource: ResourceRow;
  editorId: number;
  openerRef: RefObject<HTMLElement | null>;
  fallbackFocus: RefObject<HTMLElement | null>;
  onSaved: (editorId: number, notice: string) => void;
  onRefresh: () => void;
  onWriteStart: () => void;
  onWriteSettled: () => void;
  onSubjectPending: (subject: string, pending: boolean) => void;
  onClose: () => void;
}

function UnbindResourceDialog({ api, csrf, resource, editorId, openerRef, fallbackFocus, onSaved, onRefresh, onWriteStart, onWriteSettled, onSubjectPending, onClose }: UnbindDialogProps) {
  // The version the operator confirmed when this dialog opened; a background
  // refresh that loads another operator's change must not turn the confirmed
  // intent into a removal of a revision the operator never saw.
  const [subject] = useState(resource);
  const remove = useMutation({
    mutationFn: () => api.deleteResource(subject.id, subject.version, csrf, new AbortController().signal),
    onMutate: () => { onWriteStart(); onSubjectPending(resourceSubject(subject.id), true); },
    onSettled: () => { onWriteSettled(); onSubjectPending(resourceSubject(subject.id), false); },
    onSuccess: () => { onSaved(editorId, `Resource detached — ${subject.name}.`); },
    onError: onRefresh,
  });
  const terminal = remove.error instanceof ServerRefusal &&
    (remove.error.code === "stale" || remove.error.code === "state_conflict" || remove.error.code === "not_found");
  const blocked = remove.isPending || terminal;
  return <DialogFrame title={`Unbind ${subject.name}?`} closeLabel="Close unbind dialog" descriptionId="resource-unbind-description" openerRef={openerRef} fallbackFocus={fallbackFocus} onClose={onClose}
    description="Unbinding removes this resource from the project. A resource that still backs a work item or a runtime is refused with the server's own reason.">
    {remove.error && <p className="notice notice-error" role="alert">{terminal
      ? `${refusalNotice(remove.error)} Close and reopen this dialog to act on the server's current state.`
      : refusalNotice(remove.error)}</p>}
    <div className="dialog-actions">
      <button className="button" type="button" disabled={blocked} aria-busy={remove.isPending} onClick={() => { remove.mutate(); }}>Unbind resource</button>
      <button className="button" type="button" onClick={onClose} disabled={remove.isPending}>Cancel</button>
    </div>
  </DialogFrame>;
}

function ProjectResourcesHeading({ count, headingRef, children }: { count: number; headingRef: RefObject<HTMLHeadingElement | null>; children?: ReactNode }) {
  return <div className="section-heading">
    <div className="heading-title">
      <h2 id="project-resources-heading" ref={headingRef} tabIndex={-1}>Project resources</h2>
      <span className="count">{count}</span>
    </div>
    {children}
  </div>;
}

interface ProjectResourcesProps {
  api: Api;
  csrf: string;
  /** `search.projectId ?? workspace.selected_project_id`: the live project scope. */
  projectId: string | null;
  workspace: Workspace | undefined;
  workspaceError: unknown;
  workspacePending: boolean;
  /** Re-reads the live project query the page holds, whatever key scopes it. */
  onRefreshProject: () => void;
  connections: readonly Connection[];
  /** The page-level pending count: any connection or resource write holds it. */
  writePending: boolean;
  /** Per-subject counts keyed by `resource:{id}` and `project:{id}`. */
  pendingSubjects: Readonly<Record<string, number>>;
  /** The live project scope, readable by continuations that outlive this render. */
  projectRef: RefObject<string | null>;
  onWriteStart: () => void;
  onWriteSettled: () => void;
  onSubjectPending: (subject: string, pending: boolean) => void;
}

export function ProjectResources(props: ProjectResourcesProps) {
  const headingRef = useRef<HTMLHeadingElement | null>(null);
  const project = props.projectId === null
    ? undefined
    : decodeProjects(props.workspace).find((candidate) => candidate.id === props.projectId);
  // The section element itself is stable: it is the labelled region an operator
  // or a test can hold on to while the project read resolves.
  return <section className="project-resources" aria-labelledby="project-resources-heading">
    {project === undefined
      ? <>
        <ProjectResourcesHeading count={0} headingRef={headingRef} />
        {props.workspace === undefined && props.workspacePending && <LoadingState label="Loading project resources…" />}
        {props.workspace === undefined && !props.workspacePending && <ErrorState error={props.workspaceError} retry={props.onRefreshProject} />}
        {props.workspace !== undefined && props.workspaceError !== null && props.workspaceError !== undefined &&
          <ErrorState error={props.workspaceError} retry={props.onRefreshProject} stale />}
        {props.workspace !== undefined && (props.workspaceError === null || props.workspaceError === undefined) &&
          <EmptyState title="Project not found">This project is unavailable or you no longer have access.</EmptyState>}
      </>
      // The surface belongs to one project instance: a switch remounts it, so an
      // open dialog, a pending draft and a notice never leak into another project.
      : <ProjectResourcesReady key={project.id} {...props} project={project} headingRef={headingRef} />}
  </section>;
}

function ProjectResourcesReady({ api, csrf, project, headingRef, workspace, workspaceError, connections, writePending, pendingSubjects, projectRef, onRefreshProject, onWriteStart, onWriteSettled, onSubjectPending }: ProjectResourcesProps & { project: ProjectRow; headingRef: RefObject<HTMLHeadingElement | null> }) {
  const projectId = project.id;
  const rows = project.resources;
  const runtimes = decodeRuntimes(workspace);
  const queryClient = useQueryClient();
  const archived = project.status === "archived";
  const [notice, setNotice] = useState<string | null>(null);
  const [editor, setEditor] = useState<{ id: number; kind: "bind" | "update" | "unbind"; resourceId: string } | null>(null);
  const editorSequence = useRef(0);
  // The editor the operator still has open; a late response only closes and
  // navigates the dialog it belongs to.
  const openEditorRef = useRef(0);
  const openerRef = useRef<HTMLElement | null>(null);
  // This section's own lifetime, and the live project scope: a continuation
  // that settles for a project the operator left is discarded instead of
  // acting on the newly selected project.
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; };
  }, []);
  const live = () => mounted.current && projectRef.current === projectId;
  // A refused or accepted write is real even when the operator has moved to
  // another project: the project it belongs to must re-read before it is shown
  // again, so returning to it serves the post-write state instead of the
  // pre-write cache. Invalidating the workspace reads refetches the ones this
  // page has open (the server-selected scope included) and marks every other
  // project's entry stale. The refetch is a manual one: query-core 5.107.1's
  // `refetchQueries` defaults `cancelRefetch: true`, so a read already in
  // flight for a key with cached data is cancelled and superseded by the
  // write's own read; a first read holding no data is joined instead
  // (query.js:353-360).
  const reconcile = () => { void queryClient.invalidateQueries({ queryKey: ["workspace"] }); };
  const editorTarget = editor === null || editor.kind === "bind" ? undefined : rows.find((row) => row.id === editor.resourceId);
  const editorSubjectRemoved = editor !== null && editor.kind !== "bind" && workspace !== undefined && editorTarget === undefined;
  const openEditor = (kind: "bind" | "update" | "unbind", resourceId: string, invoking: HTMLElement) => {
    openerRef.current = invoking;
    editorSequence.current += 1;
    openEditorRef.current = editorSequence.current;
    setNotice(null);
    setEditor({ id: editorSequence.current, kind, resourceId });
  };
  const closeEditor = () => { openEditorRef.current = 0; setEditor(null); };
  const saved = (editorId: number, message: string) => {
    // The write is real, so the project read is reconciled even when its editor
    // has closed; the notice and the dialog belong to the editor generation.
    reconcile();
    if (editorId !== openEditorRef.current) return;
    closeEditor();
    if (live()) setNotice(message);
  };
  const syncResource = useMutation({
    mutationFn: (resourceId: string) => api.syncResource(resourceId, csrf, new AbortController().signal),
    onMutate: (resourceId) => { onWriteStart(); onSubjectPending(resourceSubject(resourceId), true); },
    onSettled: (_receipt, _error, resourceId) => { onWriteSettled(); onSubjectPending(resourceSubject(resourceId), false); },
    onSuccess: (receipt, resourceId) => {
      reconcile();
      if (!live()) return;
      const name = rows.find((row) => row.id === resourceId)?.name ?? receipt.resourceId;
      setNotice(receipt.syncStatus === "synced" && receipt.lastSyncedAt !== null
        ? `Resource synchronized — ${name}.`
        : `${name} reported ${receipt.syncStatus}; the project read shows the server's current state.`);
    },
    onError: (error) => {
      reconcile();
      if (!live()) return;
      setNotice(refusalNotice(error));
    },
  });
  const syncProject = useMutation({
    mutationFn: () => api.syncProject(projectId, csrf, new AbortController().signal),
    onMutate: () => { onWriteStart(); onSubjectPending(projectSubject(projectId), true); },
    onSettled: () => { onWriteSettled(); onSubjectPending(projectSubject(projectId), false); },
    onSuccess: (receipt) => {
      reconcile();
      if (!live()) return;
      const count = receipt.syncedResourceIds.length;
      setNotice(`Project resources synchronized: ${count} resource${count === 1 ? "" : "s"} updated.`);
    },
    onError: (error) => {
      reconcile();
      if (!live()) return;
      setNotice(refusalNotice(error));
    },
  });
  const projectSyncPending = (pendingSubjects[projectSubject(projectId)] ?? 0) > 0;
  const blocked = archived || writePending || projectSyncPending;
  return <>
    <ProjectResourcesHeading count={rows.length} headingRef={headingRef}>
      <div className="heading-actions">
        <button className="button" type="button" disabled={blocked} onClick={(event) => { openEditor("bind", "", event.currentTarget); }}>
          <Plus size={16} aria-hidden="true" />Bind resource
        </button>
        <button className="button" type="button" disabled={blocked} aria-busy={projectSyncPending} onClick={() => { syncProject.mutate(); }}>Synchronize project</button>
      </div>
    </ProjectResourcesHeading>
    {/* A failed re-read with cached rows present is shown as a failed refresh:
        the rows below are the last loaded data, never presented as current. */}
    {workspaceError !== null && workspaceError !== undefined && <ErrorState error={workspaceError} retry={onRefreshProject} stale />}
    <p className="filter-note">Showing {project.name} <span className="item-key">{project.key}</span>.</p>
    {archived && <p className="filter-note">This project is archived, so binding, updating, unbinding and synchronizing its resources are disabled.</p>}
    {editorSubjectRemoved && <p className="notice notice-error" role="status">The resource being edited was removed elsewhere before the change was saved; the unsaved draft was discarded.</p>}
    {notice && <p className="notice" role="status">{notice}<button className="button" type="button" onClick={() => { setNotice(null); }}>Dismiss</button></p>}
    {rows.length === 0
      ? <EmptyState title="No resources yet">Bind a repository, work tracking, CI, agent or runtime resource to this project.</EmptyState>
      : <ul className="resource-list" aria-label="Project resources">
        {rows.map((row) => {
          const pending = (pendingSubjects[resourceSubject(row.id)] ?? 0) > 0;
          return <ResourceRowItem key={row.id} row={row} connections={connections} locked={blocked || pending} syncPending={pending}
            onSync={() => { syncResource.mutate(row.id); }}
            onUpdate={(invoking) => { openEditor("update", row.id, invoking); }}
            onUnbind={(invoking) => { openEditor("unbind", row.id, invoking); }} />;
        })}
      </ul>}
    {editor !== null && editor.kind === "bind" && <BindResourceDialog key={editor.id} api={api} csrf={csrf} projectId={projectId} connections={connections}
      runtimes={runtimes} editorId={editor.id} openerRef={openerRef} fallbackFocus={headingRef}
      onSaved={saved} onRefresh={onRefreshProject} onWriteStart={onWriteStart} onWriteSettled={onWriteSettled} onSubjectPending={onSubjectPending} onClose={closeEditor} />}
    {editor !== null && editor.kind === "update" && editorTarget !== undefined && <UpdateResourceDialog key={editor.id} api={api} csrf={csrf}
      resource={editorTarget} connections={connections} runtimes={runtimes} editorId={editor.id} openerRef={openerRef} fallbackFocus={headingRef}
      onSaved={saved} onRefresh={onRefreshProject} onWriteStart={onWriteStart} onWriteSettled={onWriteSettled} onSubjectPending={onSubjectPending} onClose={closeEditor} />}
    {editor !== null && editor.kind === "unbind" && editorTarget !== undefined && <UnbindResourceDialog key={editor.id} api={api} csrf={csrf}
      resource={editorTarget} editorId={editor.id} openerRef={openerRef} fallbackFocus={headingRef}
      onSaved={saved} onRefresh={onRefreshProject} onWriteStart={onWriteStart} onWriteSettled={onWriteSettled} onSubjectPending={onSubjectPending} onClose={closeEditor} />}
  </>;
}
