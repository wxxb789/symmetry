import * as Dialog from "@radix-ui/react-dialog";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useRouteContext, useSearch } from "@tanstack/react-router";
import { AlertCircle, Cable, Plus, Trash2, X } from "lucide-react";
import { useEffect, useLayoutEffect, useRef, useState, type RefObject } from "react";
import { connectionsOptions } from "../../app/application";
import { useSessionToken } from "../../app/session";
import {
  errorMessage,
  ServerRefusal,
  type Api,
  type Connection,
  type ConnectionDraft,
  type ConnectionProvider,
  type ConnectionResource,
} from "../../services/api";
import { EmptyState, ErrorState, LoadingState, PageHeading, RefreshButton } from "../../components/workspace";

const knownStatuses: Record<string, true> = { healthy: true, degraded: true, offline: true, unknown: true };
const knownSyncStatuses: Record<string, true> = { unknown: true, syncing: true, synced: true, stale: true, failed: true };
const authTypes: Record<string, string> = { gh_cli: "GitHub CLI · HTTPS", entra_id: "Microsoft Entra ID" };
const providerLabels: Record<string, string> = { github: "GitHub", azure_devops: "Azure DevOps" };
const capabilityLabels: Record<string, string> = { repositories: "Repositories", work_items: "Work items", changes: "Changes", ci: "CI" };
// The server owns the reason; the console adds the operator's next step.
const refusalGuidance: Record<string, string> = {
  stale: "Reload to see the server's current values.",
  state_conflict: "Reload and resolve the changed state.",
  not_found: "Reload the page to see the server's current connections.",
  forbidden: "Grant the required provider permissions, then test the connection again.",
  not_connected: "Bind the resource to a connection first.",
  provider_unauthorized: "Reauthenticate the provider session on this server, then test the connection again.",
  provider_failure: "Check provider availability, then test the connection again.",
  validation_failed: "Correct the highlighted fields and submit again.",
};

const formatLabel = (value: string) => value.replaceAll("_", " ");
const sentenceLabel = (value: string) => formatLabel(value).replace(/(^|\s)\S/g, (letter) => letter.toUpperCase());
const providerLabel = (value: string) => providerLabels[value] ?? `Unknown provider: ${value || "unspecified"}`;
const authLabel = (value: string) => authTypes[value] ?? `Unknown authentication: ${value || "unspecified"}`;
const capabilityLabel = (value: string) => capabilityLabels[value] ?? `Unknown capability: ${value}`;
const noPendingRequests: { check: number; remove: number } = { check: 0, remove: 0 };

// Radix restores focus on close only to a Dialog.Trigger, and none of these
// dialogs has one: the invoking control is captured before Radix moves keyboard
// focus inside, and focused again when the dialog closes. That control can be
// disabled while its write is still pending — the dialog stays closable so the
// operator can keep working — and focusing a disabled control never moves
// focus, which would drop keyboard focus to the body. Those closes fall back to
// a control the pending write cannot disable.
function restoreFocus(event: Event, opener: HTMLElement | null, fallback: HTMLElement | null) {
  const target = opener?.isConnected && !opener.matches(":disabled")
    ? opener
    : fallback?.isConnected && !fallback.matches(":disabled") ? fallback : null;
  if (!target) return;
  event.preventDefault();
  target.focus();
}

function StatusBadge({ status, scope }: { status: string; scope: "connection" | "resource" | "sync" }) {
  const known = scope === "sync" ? knownSyncStatuses[status] : knownStatuses[status];
  const tone = status === "healthy" || status === "synced" ? "status-complete" : status === "degraded" || status === "failed" || status === "offline" ? "status-attention" : "";
  return <span className={`status-badge ${tone}`} data-status={status}>{known ? sentenceLabel(status) : `Unknown ${scope} status: ${status || "unspecified"}`}</span>;
}

function FieldErrors({ error, field }: { error: unknown; field: string }) {
  const messages = (error instanceof ServerRefusal && error.fields?.[field]) || [];
  if (messages.length === 0) return null;
  return <p className="field-error" role="alert">{messages.join(" ")}</p>;
}

function refusalNotice(error: unknown): string {
  if (!(error instanceof ServerRefusal)) return errorMessage(error);
  const guidance = refusalGuidance[error.code];
  return `${error.message} (${error.code})${guidance ? ` — ${guidance}` : ""}`;
}

function ResourceRows({ resources }: { resources: readonly ConnectionResource[] }) {
  if (resources.length === 0) return <p className="muted">No project resource is bound to this connection yet.</p>;
  return <ul className="bound-resource-list" aria-label="Bound resources">
    {resources.map((resource) => <li key={resource.id} className="bound-resource-row">
      <div className="bound-resource-main">
        <span className="item-key">{resource.project_key}</span>
        <h4>{resource.name}</h4>
        <p className="muted text-small">{formatLabel(resource.kind)} · {resource.external_ref ?? "no external reference"}
          {resource.project_status === "archived" && " · archived project, so its health and sync state are not reset"}
        </p>
        {resource.status_message && <p className="blocker"><AlertCircle size={14} aria-hidden="true" />{resource.status_message}</p>}
      </div>
      <div className="bound-resource-state">
        <StatusBadge status={resource.status} scope="resource" />
        <StatusBadge status={resource.sync_status} scope="sync" />
      </div>
    </li>)}
  </ul>;
}

function ConnectionRow({ connection, selected, onSelect }: { connection: Connection; selected: boolean; onSelect: () => void }) {
  const attention = connection.resources.filter((resource) => resource.status !== "healthy" || resource.sync_status !== "synced").length;
  return <li className="connection-row">
    <button type="button" className={`connection-card ${selected ? "connection-card-selected" : ""}`} aria-pressed={selected} onClick={onSelect}>
      <span className="connection-main">
        <span className="connection-title"><span className="item-key">{providerLabel(connection.provider)}</span><h2>{connection.name}</h2></span>
        <span className="muted">{connection.account_ref} · {authLabel(connection.auth_type)}</span>
        <span className="connection-capabilities">{connection.capabilities.map((capability) => <span key={capability} className="capability-chip">{capabilityLabel(capability)}</span>)}</span>
        {connection.status_message && <span className="blocker"><AlertCircle size={14} aria-hidden="true" />{connection.status_message}</span>}
      </span>
      <span className="connection-meta">
        <StatusBadge status={connection.status} scope="connection" />
        <span className="muted text-small">{connection.last_checked_at ? `Checked ${connection.last_checked_at}` : "Never checked"}</span>
        <span className="muted text-small">{connection.resources.length} bound · {attention} needing attention</span>
      </span>
    </button>
  </li>;
}

interface EditorProps {
  connection: Connection | null;
  providers: readonly ConnectionProvider[];
  api: Api;
  csrf: string;
  editorId: number;
  fallbackFocus: RefObject<HTMLElement | null>;
  onSaved: (disposition: string, connectionId: string, editorId: number, capabilitiesChanged: boolean) => void;
  onWriteStart: () => void;
  onWriteSettled: () => void;
  onClose: () => void;
}

function ConnectionEditor({ connection, providers, api, csrf, editorId, fallbackFocus, onSaved, onWriteStart, onWriteSettled, onClose }: EditorProps) {
  const queryClient = useQueryClient();
  const first = providers[0];
  // The capabilities this editor was opened from; the notice after a successful
  // update compares the submission against them.
  const [editedCapabilities] = useState<readonly string[] | null>(() => connection ? [...connection.capabilities] : null);
  // Radix restores focus on close only to a Dialog.Trigger, and this dialog has
  // no trigger of its own; capture the invoking control before Radix moves
  // keyboard focus into the dialog, and give it back when the dialog closes.
  const opener = useRef<HTMLElement | null>(document.activeElement instanceof HTMLElement ? document.activeElement : null);
  // The draft belongs to the editor generation that created it: the update is
  // submitted against the version it was edited from, so a background refresh
  // that loads another operator's change cannot turn the stale draft into a
  // silent overwrite — the server still answers with its own stale refusal.
  const [editedTarget] = useState(() => connection ? { id: connection.id, version: connection.version } : null);
  const [draft, setDraft] = useState<ConnectionDraft>(() => ({
    provider: connection?.provider ?? first?.id ?? "",
    name: connection?.name ?? "",
    account_ref: connection?.account_ref ?? "",
    capabilities: connection?.capabilities ?? first?.capabilities ?? [],
  }));
  const provider = providers.find((candidate) => candidate.id === draft.provider);
  const save = useMutation({
    mutationFn: (payload: ConnectionDraft) => editedTarget
      ? api.updateConnection(editedTarget.id, { version: editedTarget.version, name: payload.name, capabilities: [...payload.capabilities] }, csrf, new AbortController().signal)
      : api.createConnection(payload, csrf, new AbortController().signal),
    // The server resets bound resource health and sync state only when the
    // update changes the capabilities; the receipt notice must not claim a
    // reset that did not happen.
    onSuccess: (receipt, payload) => onSaved(
      receipt.disposition,
      receipt.connectionId,
      editorId,
      editedCapabilities !== null &&
        (payload.capabilities.length !== editedCapabilities.length ||
          payload.capabilities.some((capability) => !editedCapabilities.includes(capability))),
    ),
    // A refused write is never rendered as success: re-read the server's state
    // so the console shows what the server actually holds.
    onError: () => { void queryClient.invalidateQueries({ queryKey: ["connections"] }); },
    // The page-level pending count drops only when the request settles, so the
    // exclusion holds even after this editor closes while the write is pending.
    onSettled: () => onWriteSettled(),
  });
  // Capability coupling is server-owned policy: "changes" requires
  // "repositories", so selecting one selects the other and dropping
  // "repositories" also drops "changes".
  const toggleCapability = (capability: string) => setDraft((current) => {
    const capabilities = current.capabilities.includes(capability)
      ? current.capabilities.filter((value) => value !== capability && !(capability === "repositories" && value === "changes"))
      : capability === "changes" && !current.capabilities.includes("repositories")
        ? [...current.capabilities, "repositories", capability]
        : [...current.capabilities, capability];
    return { ...current, capabilities };
  });
  const failure = save.error;
  const stale = failure instanceof ServerRefusal && failure.code === "stale";
  const blocked = save.isPending || stale;
  return <Dialog.Root open onOpenChange={(open) => { if (!open) onClose(); }}>
    <Dialog.Portal>
      <Dialog.Overlay className="dialog-overlay" />
      <Dialog.Content className="command-dialog connection-dialog" aria-describedby="connection-dialog-description"
        onCloseAutoFocus={(event) => {
          const target = opener.current;
          opener.current = null;
          restoreFocus(event, target, fallbackFocus.current);
        }}>
        <div className="dialog-heading">
          <Dialog.Title>{connection ? "Connection settings" : "New connection"}</Dialog.Title>
          <Dialog.Close asChild><button className="icon-button" type="button" aria-label="Close connection dialog"><X size={20} aria-hidden="true" /></button></Dialog.Close>
        </div>
        <Dialog.Description id="connection-dialog-description" className="muted">
          Connections authenticate through this server's provider session. No credential is entered, displayed or stored in the browser.
        </Dialog.Description>
        <form className="connection-form" onSubmit={(event) => { event.preventDefault(); if (!blocked) { onWriteStart(); save.mutate(draft); } }}>
          <label htmlFor="connection-provider">Provider</label>
          <select id="connection-provider" name="provider" value={draft.provider} disabled={Boolean(connection) || save.isPending}
            onChange={(event) => {
              // Read the DOM value before the deferred updater runs: React
              // resets currentTarget once the event handler returns.
              const providerId = event.currentTarget.value;
              const next = providers.find((candidate) => candidate.id === providerId);
              setDraft((current) => ({ ...current, provider: next?.id ?? "", capabilities: next?.capabilities ?? current.capabilities }));
            }}>
            {providers.map((option) => <option key={option.id} value={option.id}>{providerLabel(option.id)}</option>)}
          </select>
          <FieldErrors error={failure} field="provider" />

          <label htmlFor="connection-name">Name</label>
          <input id="connection-name" name="name" value={draft.name} disabled={blocked} required
            onChange={(event) => { const name = event.currentTarget.value; setDraft((current) => ({ ...current, name })); }} />
          <FieldErrors error={failure} field="name" />

          <label htmlFor="connection-account">Account or organization</label>
          <input id="connection-account" name="account_ref" value={draft.account_ref} disabled={Boolean(connection) || blocked} required
            onChange={(event) => { const accountRef = event.currentTarget.value; setDraft((current) => ({ ...current, account_ref: accountRef })); }} />
          <FieldErrors error={failure} field="account_ref" />

          <fieldset className="capability-fieldset" disabled={blocked}>
            <legend>Capabilities</legend>
            <p className="muted text-small">Authentication: {authLabel(provider?.auth_type ?? connection?.auth_type ?? "")}. The server requires repositories for changes.</p>
            {(provider?.capabilities ?? connection?.capabilities ?? []).map((capability) => <label key={capability} className="capability-option">
              <input type="checkbox" name="capabilities" value={capability} checked={draft.capabilities.includes(capability)} onChange={() => toggleCapability(capability)} />
              {capabilityLabel(capability)}
            </label>)}
          </fieldset>
          <FieldErrors error={failure} field="capabilities" />

          {failure && <p className="notice notice-error" role="alert">{stale
            ? `${refusalNotice(failure)} This connection changed elsewhere; close and reopen it to edit the server's current values.`
            : refusalNotice(failure)}</p>}
          <div className="dialog-actions">
            <button className="button" type="submit" disabled={blocked}>{save.isPending ? "Saving…" : connection ? "Save" : "Connect"}</button>
            <button className="button" type="button" onClick={onClose} disabled={save.isPending}>Cancel</button>
          </div>
        </form>
      </Dialog.Content>
    </Dialog.Portal>
  </Dialog.Root>;
}

interface DetailProps {
  connection: Connection;
  api: Api;
  csrf: string;
  writePending: boolean;
  requests: { check: number; remove: number };
  closeButtonRef: RefObject<HTMLButtonElement | null>;
  onRequest: (connectionId: string, kind: "check" | "remove", pending: boolean) => void;
  onEdit: () => void;
  onNotice: (notice: string) => void;
  onRemoved: () => void;
}

function ConnectionDetail({ connection, api, csrf, writePending, requests, closeButtonRef, onRequest, onEdit, onNotice, onRemoved }: DetailProps) {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const [confirmingDelete, setConfirmingDelete] = useState(false);
  // The version the operator confirmed when the removal dialog opened; a
  // background refresh that loads another operator's change must not turn the
  // confirmed intent into a delete of a revision the operator never saw.
  const [deleteTarget, setDeleteTarget] = useState<{ id: string; version: number } | null>(null);
  const deleteOpener = useRef<HTMLElement | null>(null);
  const refresh = () => queryClient.invalidateQueries({ queryKey: ["connections"] });
  // This detail belongs to exactly one connection (the list keys it by
  // connection id), and its requests belong to that connection, not to the
  // instance: the page holds the pending state by connection id so a remount
  // while the operator inspects another connection cannot forget a running
  // check or removal, and it drops a continuation whose connection the
  // operator is no longer showing instead of letting it act on newer state.
  const check = useMutation({
    mutationFn: () => api.checkConnection(connection.id, csrf, new AbortController().signal),
    onMutate: () => onRequest(connection.id, "check", true),
    onSettled: () => onRequest(connection.id, "check", false),
    // The server answers a provider check with `checked`; a replayed receipt is
    // not part of that surface, so the read after the check is the only notice
    // the operator needs.
    onSuccess: async () => { await refresh(); },
    onError: async (error) => {
      onNotice(refusalNotice(error));
      await refresh();
    },
  });
  const remove = useMutation({
    mutationFn: (target: { id: string; version: number }) => api.deleteConnection(target.id, target.version, csrf, new AbortController().signal),
    onMutate: () => onRequest(connection.id, "remove", true),
    onSettled: () => onRequest(connection.id, "remove", false),
    onSuccess: async () => {
      setConfirmingDelete(false);
      await refresh();
      onRemoved();
    },
    onError: async (error) => {
      setConfirmingDelete(false);
      onNotice(`${refusalNotice(error)} The server's current state is shown instead.`);
      await refresh();
    },
  });
  const busy = check.isPending || remove.isPending || requests.check > 0 || requests.remove > 0 || writePending;
  return <aside className="connection-detail" aria-label="Connection detail">
    <div className="section-heading">
      <h2>{connection.name}</h2>
      <button ref={closeButtonRef} className="icon-button" type="button" aria-label="Close connection detail" onClick={() => void navigate({ to: "/connections", search: {} })}><X size={18} aria-hidden="true" /></button>
    </div>
    <dl className="connection-facts">
      <div><dt>Provider</dt><dd>{providerLabel(connection.provider)}</dd></div>
      <div><dt>Account</dt><dd>{connection.account_ref}</dd></div>
      <div><dt>Authentication</dt><dd>{authLabel(connection.auth_type)}</dd></div>
      <div><dt>Health</dt><dd><StatusBadge status={connection.status} scope="connection" /></dd></div>
      <div><dt>Last checked</dt><dd>{connection.last_checked_at ?? "Never checked"}</dd></div>
      <div><dt>Version</dt><dd>{connection.version}</dd></div>
    </dl>
    <p className="muted text-small">Capabilities: {connection.capabilities.map(capabilityLabel).join(", ") || "none"}</p>
    {connection.status_message && <p className="notice notice-error" role="alert"><AlertCircle size={18} aria-hidden="true" />{connection.status_message}</p>}
    <div className="connection-actions">
      <button className="button" type="button" disabled={busy} onClick={() => check.mutate()}>{check.isPending || requests.check > 0 ? "Checking…" : "Test connection"}</button>
      <button className="button" type="button" disabled={busy} onClick={onEdit}>Edit capabilities</button>
      <button className="button" type="button" disabled={busy} onClick={(event) => {
        deleteOpener.current = event.currentTarget;
        setDeleteTarget({ id: connection.id, version: connection.version });
        setConfirmingDelete(true);
      }}><Trash2 size={16} aria-hidden="true" />Remove</button>
    </div>
    <div className="section-heading"><h3>Bound resources <span className="count">{connection.resources.length}</span></h3></div>
    <p className="muted text-small">A capability change resets the health and sync state of bound resources in active projects, so nothing keeps looking synchronized until it is synchronized again.</p>
    <ResourceRows resources={connection.resources} />
    <Dialog.Root open={confirmingDelete} onOpenChange={(open) => { if (!open) setConfirmingDelete(false); }}>
      <Dialog.Portal>
        <Dialog.Overlay className="dialog-overlay" />
        <Dialog.Content className="command-dialog" aria-describedby="connection-delete-description"
          onCloseAutoFocus={(event) => {
            const target = deleteOpener.current;
            deleteOpener.current = null;
            restoreFocus(event, target, closeButtonRef.current);
          }}>
          <div className="dialog-heading"><Dialog.Title>Remove {connection.name}?</Dialog.Title>
            <Dialog.Close asChild><button className="icon-button" type="button" aria-label="Close removal dialog"><X size={20} aria-hidden="true" /></button></Dialog.Close></div>
          <Dialog.Description id="connection-delete-description" className="muted">
            A connection that still has bound resources cannot be removed. An already removed or changed connection is refused with the server's own reason.
          </Dialog.Description>
          <div className="dialog-actions">
            <button className="button" type="button" disabled={remove.isPending || requests.remove > 0} onClick={() => { if (deleteTarget) remove.mutate(deleteTarget); }}>{remove.isPending || requests.remove > 0 ? "Removing…" : "Remove connection"}</button>
            <button className="button" type="button" onClick={() => setConfirmingDelete(false)} disabled={remove.isPending}>Cancel</button>
          </div>
          {remove.error && <p className="notice notice-error" role="alert">{refusalNotice(remove.error)}</p>}
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  </aside>;
}

export function ConnectionsPage() {
  const { application } = useRouteContext({ from: "__root__" });
  const csrf = useSessionToken();
  const search = useSearch({ from: "/connections" });
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const query = useQuery(connectionsOptions(application.api));
  const [creating, setCreating] = useState(false);
  // The connection an open edit dialog belongs to: a background refresh that
  // removes that connection must close the dialog, and a stale subject must not
  // reopen an editor, unsolicited, on the next selected connection.
  const [editingSubject, setEditingSubject] = useState<string | null>(null);
  const [pendingWrites, setPendingWrites] = useState(0);
  // A check or a removal is a request against one connection, not against one
  // detail instance: the counts are kept by connection id so the exclusion
  // survives the detail unmounting and remounting while the operator inspects
  // another connection.
  const [pendingRequests, setPendingRequests] = useState<Record<string, { check: number; remove: number }>>({});
  // The control the dialogs fall back to when their invoking control is
  // disabled by the pending write (see restoreFocus).
  const detailCloseRef = useRef<HTMLButtonElement | null>(null);
  // Every editor open gets its own identity: a mutation response only closes
  // and navigates for the editor the operator still has open.
  const editorSequence = useRef(0);
  const [openEditorId, setOpenEditorId] = useState(0);
  const [notice, setNotice] = useState<string | null>(null);
  const refresh = () => { if (!query.isFetching) void query.refetch(); };
  const connections = query.data?.connections ?? [];
  const selected = connections.find((connection) => connection.id === search.connectionId);
  // The editor renders from the subject it was opened for, re-read from the
  // authoritative list; when that connection is gone the dialog closes and the
  // operator is told instead of losing the draft silently.
  const editSubject = editingSubject === null ? undefined : connections.find((connection) => connection.id === editingSubject);
  const editSubjectRemoved = editingSubject !== null && query.data !== undefined && editSubject === undefined;
  // This page's own lifetime: a continuation that settles after the operator
  // navigated away must not pull them back to this route.
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; };
  }, []);
  // The live selection, readable by continuations that outlive this render: a
  // response that arrives after the operator chose another subject must not
  // navigate them back or act on the newer subject.
  const selectionRef = useRef<string | null>(search.connectionId ?? null);
  useLayoutEffect(() => { selectionRef.current = search.connectionId ?? null; }, [search.connectionId]);
  useEffect(() => {
    if (!query.data || !search.connectionId || selected) return;
    void navigate({ to: "/connections", search: {}, replace: true });
  }, [navigate, query.data, search.connectionId, selected]);
  const openEditor = (kind: "create" | "edit", connectionId: string | null) => {
    setNotice(null);
    editorSequence.current += 1;
    setOpenEditorId(editorSequence.current);
    if (kind === "create") { setCreating(true); setEditingSubject(null); }
    else { setCreating(false); setEditingSubject(connectionId); }
  };
  const closeEditor = () => { editorSequence.current += 1; setOpenEditorId(0); setCreating(false); setEditingSubject(null); };
  // Editor saves hold the page-level pending count until they settle, even
  // after their dialog closes: while any check, update or removal is pending,
  // none of the others can start.
  const beginWrite = () => setPendingWrites((count) => count + 1);
  const endWrite = () => setPendingWrites((count) => Math.max(0, count - 1));
  const trackRequest = (connectionId: string, kind: "check" | "remove", pending: boolean) =>
    setPendingRequests((current) => {
      const counts = current[connectionId] ?? noPendingRequests;
      return { ...current, [connectionId]: { ...counts, [kind]: Math.max(0, counts[kind] + (pending ? 1 : -1)) } };
    });
  // A continuation that settles belongs to the connection the operator is
  // looking at: the page drops it when the operator has left that connection,
  // even though the request outlives its detail instance.
  const noticeFor = (connectionId: string, notice: string) => {
    if (mounted.current && selectionRef.current === connectionId) setNotice(notice);
  };
  const clearSelectionFor = (connectionId: string) => {
    if (mounted.current && selectionRef.current === connectionId) void navigate({ to: "/connections", search: {} });
  };
  const saved = (disposition: string, connectionId: string, editor: number, capabilitiesChanged: boolean) => {
    if (editor !== editorSequence.current) {
      // A late response for an editor that has since closed: the write is real,
      // so re-read the list, but never close or navigate the open editor.
      void queryClient.invalidateQueries({ queryKey: ["connections"] });
      return;
    }
    closeEditor();
    // Closing bumped the generation: the re-read below settles against the
    // generation it started in, so another editor opening while it is in flight
    // keeps the operator's attention instead of being hijacked by a selection
    // move.
    const generation = editorSequence.current;
    setNotice(disposition === "replayed"
      ? "This connection submission was already applied; the server returned the existing connection."
      : disposition === "created" ? "Connection created."
      : capabilitiesChanged ? "Connection capabilities updated. Bound resource health and sync state was reset where the change affects it."
      : "Connection updated.");
    // Re-read the authoritative list before selecting the changed connection,
    // and only move the operator if neither the selection nor the editor
    // generation changed while the re-read was in flight.
    const selection = selectionRef.current;
    void queryClient.invalidateQueries({ queryKey: ["connections"] })
      .then(() => {
        if (!mounted.current || editorSequence.current !== generation || selectionRef.current !== selection) return;
        void navigate({ to: "/connections", search: { connectionId } });
      });
  };
  const attention = connections.filter((connection) => connection.status !== "healthy").length;
  return <>
    <PageHeading title="Connections" description="GitHub and Azure DevOps connections this server authenticates through.">
      {/* The provider catalog arrives with this query: until it is loaded the
          create draft would have no provider to choose or submit. */}
      <button className="button" type="button" disabled={query.data === undefined} onClick={() => openEditor("create", null)}><Plus size={16} aria-hidden="true" />New connection</button>
      <RefreshButton fetching={query.isFetching} refresh={refresh} />
    </PageHeading>
    {notice && <p className="notice" role="status">{notice}<button className="button" type="button" onClick={() => setNotice(null)}>Dismiss</button></p>}
    {editSubjectRemoved && <p className="notice notice-error" role="status">The connection being edited was removed elsewhere before the change was saved; the unsaved draft was discarded.</p>}
    {query.isPending && <LoadingState label="Loading connections…" />}
    {query.isError && <ErrorState error={query.error} retry={refresh} stale={query.data !== undefined} />}
    {query.data && <div className="connection-layout">
      <section aria-labelledby="connection-list-heading">
        <div className="section-heading">
          <h2 id="connection-list-heading">Connections <span className="count">{connections.length}</span></h2>
          <span className="muted text-small">{attention} needing attention</span>
        </div>
        {connections.length === 0
          ? <EmptyState title="No connections yet">Connect GitHub or Azure DevOps to bind provider resources to a project.</EmptyState>
          : <ul className="connection-list" aria-label="Connections">{connections.map((connection) => <ConnectionRow key={connection.id}
            connection={connection} selected={connection.id === search.connectionId}
            onSelect={() => {
              setNotice(null);
              // A removed editor subject must not reopen itself if that row
              // comes back: selecting a connection supersedes it.
              setEditingSubject(null);
              void navigate({ to: "/connections", search: { connectionId: connection.id } });
            }} />)}</ul>}
      </section>
      {selected
        ? <ConnectionDetail key={selected.id} connection={selected} api={application.api} csrf={csrf} writePending={pendingWrites > 0}
            requests={pendingRequests[selected.id] ?? noPendingRequests} closeButtonRef={detailCloseRef} onRequest={trackRequest}
            onEdit={() => openEditor("edit", selected.id)} onNotice={(notice) => noticeFor(selected.id, notice)} onRemoved={() => clearSelectionFor(selected.id)} />
        : <aside className="connection-detail" aria-label="Connection detail">
          <Cable size={28} aria-hidden="true" />
          <h2>Select a connection</h2>
          <p className="muted">Inspect authentication type, capabilities, health, bound resource state and the reason behind a failed provider check.</p>
        </aside>}
      <p className="sr-only" role="status">{query.isFetching && !query.isPending ? "Refreshing connections" : ""}</p>
    </div>}
    {creating && <ConnectionEditor connection={null} providers={query.data?.providers ?? []} api={application.api} csrf={csrf} editorId={openEditorId} fallbackFocus={detailCloseRef} onSaved={saved} onWriteStart={beginWrite} onWriteSettled={endWrite} onClose={closeEditor} />}
    {editSubject && <ConnectionEditor connection={editSubject} providers={query.data?.providers ?? []} api={application.api} csrf={csrf} editorId={openEditorId} fallbackFocus={detailCloseRef} onSaved={saved} onWriteStart={beginWrite} onWriteSettled={endWrite} onClose={closeEditor} />}
  </>;
}
