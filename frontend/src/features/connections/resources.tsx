import * as Dialog from "@radix-ui/react-dialog";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { AlertCircle, Plus, X } from "lucide-react";
import { useEffect, useRef, useState, type ReactNode, type RefObject } from "react";
import { Field, StatusBadge } from "../../components/primitives";
import { useDialogOpener } from "../../components/use-dialog-opener";
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

export const providerLabel = (value: string) => ownValue(providerLabels, value) ?? `Unknown provider: ${value || "unspecified"}`;
const kindLabel = (value: string) => ownValue(kindLabels, value) ?? `Unknown kind: ${value}`;
const isLocalKind = (kind: string) => ownValue(localKinds, kind) === true;

export const refusalNotice = (error: unknown) => sharedRefusalNotice(error, refusalGuidance);

// Declared policy: a served member is present when non-empty (no trim), unlike the shared `asText`.
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
  const opener = useDialogOpener(openerRef, fallbackFocus);
  return <Dialog.Root open onOpenChange={(open) => { if (!open) onClose(); }}>
    <Dialog.Portal>
      <Dialog.Overlay className="dialog-overlay" />
      <Dialog.Content className="command-dialog connection-dialog" aria-describedby={descriptionId}
        onCloseAutoFocus={opener.restore}>
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

type ResourceIntent =
  | { kind: "bind"; projectId: string }
  | { kind: "update"; resource: ResourceRow }
  | { kind: "unbind"; resource: ResourceRow };

// Carries the version the dialog opened with: the mutation and its notices must not
// re-read a row a background refresh may have replaced.
type ResourceSubmission =
  | { kind: "bind"; projectId: string; draft: ResourceDraft }
  | { kind: "update"; resourceId: string; version: number; update: { name: string; connection_id: string | null; external_ref: string | null } }
  | { kind: "unbind"; resourceId: string; version: number; name: string };

interface ResourceDraftState {
  connectionId: string;
  name: string;
  reference: string;
}

const emptyToNull = (value: string): string | null => value === "" ? null : value;

function bindDraftIncomplete(draft: ResourceDraftState, kind: string, references: readonly { value: string; label: string }[]): boolean {
  if (!isLocalKind(kind)) return draft.connectionId === "";
  return references.length === 0 || draft.reference === "";
}

function resourceSubmission(intent: ResourceIntent, draft: ResourceDraftState, kind: string): ResourceSubmission {
  switch (intent.kind) {
    case "bind":
      return { kind: "bind", projectId: intent.projectId, draft: {
        kind,
        connection_id: isLocalKind(kind) ? null : emptyToNull(draft.connectionId),
        name: draft.name,
        external_ref: emptyToNull(draft.reference),
      } };
    case "update":
      return { kind: "update", resourceId: intent.resource.id, version: intent.resource.version, update: {
        name: draft.name,
        connection_id: isLocalKind(intent.resource.kind) ? null : emptyToNull(draft.connectionId),
        external_ref: emptyToNull(draft.reference),
      } };
    case "unbind":
      return { kind: "unbind", resourceId: intent.resource.id, version: intent.resource.version, name: intent.resource.name };
  }
}

function resourceDialogCopy(intent: ResourceIntent): { title: string; closeLabel: string; descriptionId: string; description: string } {
  switch (intent.kind) {
    case "bind":
      return {
        title: "Bind resource", closeLabel: "Close bind dialog", descriptionId: "resource-bind-description",
        description: "A resource mirrors something the project already has: a provider resource read through a connection, or a registered runtime. The server validates the reference it is given.",
      };
    case "update":
      return {
        title: "Update resource", closeLabel: "Close update dialog", descriptionId: "resource-update-description",
        description: "The facts below are what this server served when the dialog opened; the reference, the connection and the version are validated again on submit.",
      };
    case "unbind":
      return {
        title: `Unbind ${intent.resource.name}?`, closeLabel: "Close unbind dialog", descriptionId: "resource-unbind-description",
        description: "Unbinding removes this resource from the project. A resource that still backs a work item or a runtime is refused with the server's own reason.",
      };
  }
}

function resourceSavedNotice(submission: ResourceSubmission): string {
  switch (submission.kind) {
    case "bind": return `Resource attached — ${submission.draft.name}.`;
    case "update": return `Resource updated — ${submission.update.name}.`;
    case "unbind": return `Resource detached — ${submission.name}.`;
  }
}

function resourceRefusalNotice(intent: ResourceIntent, error: unknown, frozen: boolean): string {
  const notice = refusalNotice(error);
  if (intent.kind === "update" && frozen) return `${notice} This resource changed elsewhere; close and reopen it to edit the server's current values.`;
  if (intent.kind === "unbind" && frozen) return `${notice} Close and reopen this dialog to act on the server's current state.`;
  return notice;
}

interface BindFieldsProps {
  kind: string;
  draft: ResourceDraftState;
  connections: readonly Connection[];
  references: readonly { value: string; label: string }[];
  pending: boolean;
  error: unknown;
  onChange: (patch: Partial<ResourceDraftState>) => void;
  onKind: (kind: string) => void;
}

function BindResourceFields({ kind, draft, connections, references, pending, error, onChange, onKind }: BindFieldsProps) {
  const local = isLocalKind(kind);
  const eligible = eligibleConnections(kind, connections);
  return <>
      <Field id="resource-kind" label="Type" error={error} field="kind">
        <select id="resource-kind" name="kind" value={kind} disabled={pending}
          onChange={(event) => {
            // Read the DOM value before the deferred updater runs: React resets
            // currentTarget once the event handler returns.
            onKind(event.currentTarget.value);
          }}>
          {bindableKinds.map((value) => <option key={value} value={value}>{kindLabel(value)}</option>)}
        </select>
      </Field>

      <Field id="resource-connection" label="Provider connection" error={error} field="connection_id">
        <select id="resource-connection" name="connection_id" value={local ? "" : draft.connectionId} disabled={local || pending || eligible.length === 0}
          onChange={(event) => { onChange({ connectionId: event.currentTarget.value }); }}>
          {local
            ? <option value="">Not required for local resources</option>
            : eligible.length === 0
              ? <option value="">No connection grants the required capability</option>
              : eligible.map((connection) => <option key={connection.id} value={connection.id}>{connection.name}</option>)}
        </select>
      </Field>

      <Field id="resource-name" label="Name" error={error} field="name">
        <input id="resource-name" name="name" value={draft.name} required disabled={pending}
          onChange={(event) => { onChange({ name: event.currentTarget.value }); }} />
      </Field>

      <Field id="resource-reference" label="Reference" error={error} field="external_ref">
        {local
          ? <select id="resource-reference" name="external_ref" value={draft.reference} disabled={pending || references.length === 0}
              onChange={(event) => { onChange({ reference: event.currentTarget.value }); }}>
              {references.length === 0
                ? <option value="">No registered {kindLabel(kind).toLowerCase()} is available</option>
                : references.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
            </select>
          : <input id="resource-reference" name="external_ref" value={draft.reference} disabled={pending}
              onChange={(event) => { onChange({ reference: event.currentTarget.value }); }} />}
      </Field>
  </>;
}

interface UpdateFieldsProps {
  resource: ResourceRow;
  draft: ResourceDraftState;
  connections: readonly Connection[];
  runtimes: readonly RegisteredRuntime[];
  blocked: boolean;
  error: unknown;
  onChange: (patch: Partial<ResourceDraftState>) => void;
}

function UpdateResourceFields({ resource, draft, connections, runtimes, blocked, error, onChange }: UpdateFieldsProps) {
  const local = isLocalKind(resource.kind);
  const eligible = eligibleConnections(resource.kind, connections);
  const current = connections.find((connection) => connection.id === resource.connectionId);
  const currentIneligible = current !== undefined && !eligible.some((candidate) => candidate.id === current.id);
  // The served binding stays selectable even when the connection no longer grants
  // the capability: hiding it would silently change the resource on save.
  const choices = currentIneligible && current !== undefined ? [...eligible, current] : eligible;
  const references = referenceOptions(resource.kind, runtimes);
  const referenceKnown = references.some((option) => option.value === draft.reference);
  return <>
      <dl className="connection-facts">
        <div><dt>Type</dt><dd>{kindLabel(resource.kind)}</dd></div>
        <div><dt>Provider</dt><dd>{resource.provider === null ? "No provider connection" : providerLabel(resource.provider)}</dd></div>
        <div><dt>Connection</dt><dd>{resource.connectionId === null ? "Local resource" : current?.name ?? `Unknown connection: ${resource.connectionId}`}</dd></div>
        <div><dt>Health</dt><dd><StatusBadge status={resource.status} scope="resource" /></dd></div>
        <div><dt>Sync state</dt><dd><StatusBadge status={resource.syncStatus} scope="sync" /></dd></div>
        <div><dt>Last synced</dt><dd>{resource.lastSyncedAt ?? "Never synced"}</dd></div>
        <div><dt>Status detail</dt><dd>{resource.statusMessage ?? "No issues reported"}</dd></div>
      </dl>

      <Field id="resource-name" label="Name" error={error} field="name">
        <input id="resource-name" name="name" value={draft.name} required disabled={blocked}
          onChange={(event) => { onChange({ name: event.currentTarget.value }); }} />
      </Field>

      <Field id="resource-connection" label="Provider connection" error={error} field="connection_id">
        <select id="resource-connection" name="connection_id" value={local ? "" : draft.connectionId} disabled={local || blocked}
          onChange={(event) => { onChange({ connectionId: event.currentTarget.value }); }}>
          {local
            ? <option value="">Not required for local resources</option>
            : <>
              {draft.connectionId !== "" && !choices.some((candidate) => candidate.id === draft.connectionId) &&
                <option value={draft.connectionId}>{`Unknown connection: ${draft.connectionId}`}</option>}
              {draft.connectionId === "" &&
                <option value="">{choices.length === 0 ? "No connection grants the required capability" : "No provider connection"}</option>}
              {choices.map((connection) => <option key={connection.id} value={connection.id}>
                {connection.id === resource.connectionId && currentIneligible ? `${connection.name} (capability not granted)` : connection.name}
              </option>)}
            </>}
        </select>
      </Field>

      <Field id="resource-reference" label="Reference" error={error} field="external_ref">
        {local
          ? <select id="resource-reference" name="external_ref" value={draft.reference} disabled={blocked}
              onChange={(event) => { onChange({ reference: event.currentTarget.value }); }}>
              {draft.reference !== "" && !referenceKnown && <option value={draft.reference}>{`${draft.reference} (not registered)`}</option>}
              {references.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
            </select>
          : <input id="resource-reference" name="external_ref" value={draft.reference} disabled={blocked}
              onChange={(event) => { onChange({ reference: event.currentTarget.value }); }} />}
      </Field>
  </>;
}

interface ResourceDialogProps {
  intent: ResourceIntent;
  api: Api;
  csrf: string;
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

function ResourceDialog(props: ResourceDialogProps) {
  const { api, csrf, connections, runtimes, editorId, openerRef, fallbackFocus, onSaved, onRefresh, onWriteStart, onWriteSettled, onSubjectPending, onClose } = props;
  // The facts and the version this dialog opened with: a background refresh that
  // loads another operator's change must not turn the draft into a silent
  // overwrite — the server still answers with its own stale refusal.
  const [intent] = useState(() => props.intent);
  const [kind, setKind] = useState("repository");
  const [draft, setDraft] = useState<ResourceDraftState>(() => intent.kind === "bind"
    ? { connectionId: eligibleConnections("repository", connections)[0]?.id ?? "", name: "", reference: "" }
    : { connectionId: intent.resource.connectionId ?? "", name: intent.resource.name, reference: intent.resource.externalRef ?? "" });
  const bindReferences = intent.kind === "bind" ? referenceOptions(kind, runtimes) : [];
  const set = (patch: Partial<ResourceDraftState>) => { setDraft((current) => ({ ...current, ...patch })); };
  const chooseKind = (next: string) => {
    setKind(next);
    set({ connectionId: isLocalKind(next) ? "" : eligibleConnections(next, connections)[0]?.id ?? "", reference: isLocalKind(next) ? referenceOptions(next, runtimes)[0]?.value ?? "" : "" });
  };
  const pendingSubject = intent.kind === "bind" ? projectSubject(intent.projectId) : resourceSubject(intent.resource.id);
  const save = useMutation({
    // The receipt is not read: the notice names the submission, and the project read
    // the surface already holds shows the applied state.
    mutationFn: (submission: ResourceSubmission): Promise<unknown> => {
      switch (submission.kind) {
        case "bind": return api.createResource(submission.projectId, submission.draft, csrf, new AbortController().signal);
        case "update": return api.updateResource(submission.resourceId, { version: submission.version, ...submission.update }, csrf, new AbortController().signal);
        case "unbind": return api.deleteResource(submission.resourceId, submission.version, csrf, new AbortController().signal);
      }
    },
    // The pending counts live on the page, so the exclusion holds after this dialog
    // closes, after a re-read and after a remount.
    onMutate: () => { onWriteStart(); onSubjectPending(pendingSubject, true); },
    onSettled: () => { onWriteSettled(); onSubjectPending(pendingSubject, false); },
    onSuccess: (_receipt, submission) => { onSaved(editorId, resourceSavedNotice(submission)); },
    // A refused write is never rendered as success, so re-read the server's state and
    // let the surface show what the server actually holds.
    onError: onRefresh,
  });
  const pending = save.isPending;
  const failure = save.error;
  const stale = failure instanceof ServerRefusal && failure.code === "stale";
  const terminal = failure instanceof ServerRefusal &&
    (failure.code === "stale" || failure.code === "state_conflict" || failure.code === "not_found");
  // A versioned subject freezes on the refusal that invalidates the version it opened
  // with — update `stale`, unbind terminal; a bind draft carries no version.
  const frozen = intent.kind === "update" ? stale : intent.kind === "unbind" && terminal;
  const blocked = pending || frozen;
  const submitBlocked = blocked || (intent.kind === "bind" && bindDraftIncomplete(draft, kind, bindReferences));
  const copy = resourceDialogCopy(intent);
  const notice = failure && resourceRefusalNotice(intent, failure, frozen);
  return <DialogFrame title={copy.title} closeLabel={copy.closeLabel} descriptionId={copy.descriptionId} description={copy.description} openerRef={openerRef} fallbackFocus={fallbackFocus} onClose={onClose}>
    {intent.kind === "unbind" ? <>
      {notice && <p className="notice notice-error" role="alert">{notice}</p>}
      <div className="dialog-actions">
        <button className="button" type="button" disabled={blocked} aria-busy={pending} onClick={() => { save.mutate(resourceSubmission(intent, draft, kind)); }}>Unbind resource</button>
        <button className="button" type="button" onClick={onClose} disabled={pending}>Cancel</button>
      </div>
    </> : <form className="connection-form" onSubmit={(event) => {
      event.preventDefault();
      if (submitBlocked) return;
      save.mutate(resourceSubmission(intent, draft, kind));
    }}>
      {intent.kind === "bind"
        ? <BindResourceFields kind={kind} draft={draft} connections={connections} references={bindReferences} error={failure} pending={pending} onChange={set} onKind={chooseKind} />
        : <UpdateResourceFields resource={intent.resource} draft={draft} connections={connections} runtimes={runtimes} error={failure} blocked={blocked} onChange={set} />}
      {notice && <p className="notice notice-error" role="alert">{notice}</p>}
      <div className="dialog-actions">
        <button className="button" type="submit" disabled={submitBlocked} aria-busy={pending}>{intent.kind === "bind" ? "Bind" : "Save"}</button>
        <button className="button" type="button" onClick={onClose} disabled={pending}>Cancel</button>
      </div>
    </form>}
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

interface ResourceEditor { id: number; kind: "bind" | "update" | "unbind"; resourceId: string }

function dialogIntentOf(editor: ResourceEditor, target: ResourceRow | undefined, projectId: string): ResourceIntent | null {
  if (editor.kind === "bind") return { kind: "bind", projectId };
  if (target === undefined) return null;
  return { kind: editor.kind, resource: target };
}

function ProjectResourcesReady({ api, csrf, project, headingRef, workspace, workspaceError, connections, writePending, pendingSubjects, projectRef, onRefreshProject, onWriteStart, onWriteSettled, onSubjectPending }: ProjectResourcesProps & { project: ProjectRow; headingRef: RefObject<HTMLHeadingElement | null> }) {
  const projectId = project.id;
  const rows = project.resources;
  const runtimes = decodeRuntimes(workspace);
  const queryClient = useQueryClient();
  const archived = project.status === "archived";
  const [notice, setNotice] = useState<string | null>(null);
  const [editor, setEditor] = useState<ResourceEditor | null>(null);
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
  const dialogIntent = editor === null ? null : dialogIntentOf(editor, editorTarget, projectId);
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
    {editor !== null && dialogIntent !== null && <ResourceDialog key={editor.id} intent={dialogIntent} api={api} csrf={csrf} connections={connections}
      runtimes={runtimes} editorId={editor.id} openerRef={openerRef} fallbackFocus={headingRef}
      onSaved={saved} onRefresh={onRefreshProject} onWriteStart={onWriteStart} onWriteSettled={onWriteSettled} onSubjectPending={onSubjectPending} onClose={closeEditor} />}
  </>;
}
