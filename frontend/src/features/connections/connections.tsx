import * as Dialog from "@radix-ui/react-dialog";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useRouteContext, useRouter, useRouterState, useSearch } from "@tanstack/react-router";
import { AlertCircle, Cable, Plus, Trash2, X } from "lucide-react";
import { useEffect, useLayoutEffect, useRef, useState, type RefObject } from "react";
import { connectionsOptions, workspaceOptions } from "../../app/application";
import { connectionsReadKey } from "../../app/read-cache";
import { useSelectedProject } from "../../app/selected-project";
import { useSessionToken } from "../../app/session";
import {
  ownValue,
  type Api,
  type Connection,
  type ConnectionDraft,
  type ConnectionProvider,
  type ConnectionResource,
} from "../../services/api";
import { isStaleRefusal, refusalNotice, resourceRefusalGuidance } from "../../services/refusals";
import { restoreFocus } from "../../components/focus";
import { FieldErrors, StatusBadge, formatLabel } from "../../components/primitives";
import { EmptyState, ErrorState, LoadingState, PageHeading, RefreshButton } from "../../components/workspace";
import { DialogFrame, ProjectResources, providerLabel } from "./resources";

const authTypes: Record<string, string> = { gh_cli: "GitHub CLI · HTTPS", entra_id: "Microsoft Entra ID" };
const capabilityLabels: Record<string, string> = { repositories: "Repositories", work_items: "Work items", changes: "Changes", ci: "CI" };
// Own keys only: a served auth type or capability may name a prototype member
// (`constructor`), which a plain index would return as Object's own function.
const authLabel = (value: string) => ownValue(authTypes, value) ?? `Unknown authentication: ${value || "unspecified"}`;
const capabilityLabel = (value: string) => ownValue(capabilityLabels, value) ?? `Unknown capability: ${value}`;
const noPendingRequests: { check: number; remove: number } = { check: 0, remove: 0 };

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
      ? api.updateConnection(editedTarget.id, { version: editedTarget.version, name: payload.name, capabilities: [...payload.capabilities] }, csrf)
      : api.createConnection(payload, csrf),
    // The server resets bound resource health and sync state only when the
    // update changes the capabilities; the receipt notice must not claim a
    // reset that did not happen.
    onSuccess: (receipt, payload) => { onSaved(
        receipt.disposition,
        receipt.connectionId,
        editorId,
        editedCapabilities !== null &&
          (payload.capabilities.length !== editedCapabilities.length ||
            payload.capabilities.some((capability) => !editedCapabilities.includes(capability))),
      ); },
    // A refused write is never rendered as success: re-read the server's state
    // so the console shows what the server actually holds.
    onError: () => { void queryClient.invalidateQueries({ queryKey: connectionsReadKey() }); },
    // The page-level pending count drops only when the request settles, so the
    // exclusion holds even after this editor closes while the write is pending.
    onSettled: () => { onWriteSettled(); },
  });
  // Capability coupling is server-owned policy: "changes" requires
  // "repositories", so selecting one selects the other and dropping
  // "repositories" also drops "changes".
  const toggleCapability = (capability: string) => { setDraft((current) => {
      const capabilities = current.capabilities.includes(capability)
        ? current.capabilities.filter((value) => value !== capability && !(capability === "repositories" && value === "changes"))
        : capability === "changes" && !current.capabilities.includes("repositories")
          ? [...current.capabilities, "repositories", capability]
          : [...current.capabilities, capability];
      return { ...current, capabilities };
    }); };
  const failure = save.error;
  const stale = isStaleRefusal(failure);
  const blocked = save.isPending || stale;
  return <DialogFrame title={connection ? "Connection settings" : "New connection"} closeLabel="Close connection dialog" descriptionId="connection-dialog-description" openerRef={opener} fallbackFocus={fallbackFocus} onClose={onClose}
    description="Connections authenticate through this server's provider session. No credential is entered, displayed or stored in the browser.">
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
          <input type="checkbox" name="capabilities" value={capability} checked={draft.capabilities.includes(capability)} onChange={() => { toggleCapability(capability); }} />
          {capabilityLabel(capability)}
        </label>)}
      </fieldset>
      <FieldErrors error={failure} field="capabilities" />

      {failure && <p className="notice notice-error" role="alert">{stale
        ? `${refusalNotice(failure, resourceRefusalGuidance)} This connection changed elsewhere; close and reopen it to edit the server's current values.`
        : refusalNotice(failure, resourceRefusalGuidance)}</p>}
      <div className="dialog-actions">
        <button className="button" type="submit" disabled={blocked}>{save.isPending ? "Saving…" : connection ? "Save" : "Connect"}</button>
        <button className="button" type="button" onClick={onClose} disabled={save.isPending}>Cancel</button>
      </div>
    </form>
  </DialogFrame>;
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
  const refresh = () => queryClient.invalidateQueries({ queryKey: connectionsReadKey() });
  const check = useMutation({
    mutationFn: () => api.checkConnection(connection.id, csrf),
    onMutate: () => { onRequest(connection.id, "check", true); },
    onSettled: () => { onRequest(connection.id, "check", false); },
    // The server answers a provider check with `checked`; a replayed receipt is
    // not part of that surface, so the read after the check is the only notice
    // the operator needs.
    onSuccess: async () => { await refresh(); },
    onError: async (error) => {
      onNotice(refusalNotice(error, resourceRefusalGuidance));
      await refresh();
    },
  });
  const remove = useMutation({
    mutationFn: (target: { id: string; version: number }) => api.deleteConnection(target.id, target.version, csrf),
    onMutate: () => { onRequest(connection.id, "remove", true); },
    onSettled: () => { onRequest(connection.id, "remove", false); },
    onSuccess: async () => {
      setConfirmingDelete(false);
      await refresh();
      onRemoved();
    },
    onError: async (error) => {
      setConfirmingDelete(false);
      onNotice(`${refusalNotice(error, resourceRefusalGuidance)} The server's current state is shown instead.`);
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
      <button className="button" type="button" disabled={busy} onClick={() => { check.mutate(); }}>{check.isPending || requests.check > 0 ? "Checking…" : "Test connection"}</button>
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
            <button className="button" type="button" onClick={() => { setConfirmingDelete(false); }} disabled={remove.isPending}>Cancel</button>
          </div>
          {remove.error && <p className="notice notice-error" role="alert">{refusalNotice(remove.error, resourceRefusalGuidance)}</p>}
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
  const router = useRouter();
  const queryClient = useQueryClient();
  const query = useQuery(connectionsOptions(application.api));
  // The project directory and the selected project's resources + registered
  // runtimes. The shell's project picker issues exactly this key, so the page
  // adds no request; an absent URL scope falls back to the served selection.
  const scopeProjectId = useSelectedProject();
  const workspace = useQuery(workspaceOptions(application.api, scopeProjectId ?? null));
  const selectedProjectId = scopeProjectId ?? workspace.data?.selected_project_id ?? null;
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
  // Resource writes hold per-subject counts (`resource:{id}`, `project:{id}`)
  // on the page, so their exclusion survives a re-read or a remount of the
  // resource section; the page-level write counter covers the rest.
  const [pendingSubjects, setPendingSubjects] = useState<Record<string, number>>({});
  // The control the dialogs fall back to when their invoking control is
  // disabled by the pending write (see restoreFocus).
  const detailCloseRef = useRef<HTMLButtonElement | null>(null);
  // A settled removal clears the selection; the detail unmounts with the next
  // render and takes any focus inside it — the pending-close handoff targets
  // the detail's own close control — so the page-level control that survives
  // the detail takes that focus instead of leaving the keyboard without a
  // surviving position.
  const newConnectionRef = useRef<HTMLButtonElement | null>(null);
  const removalFocusPending = useRef(false);
  // Every editor open gets its own identity: a mutation response only closes
  // and navigates for the editor the operator still has open.
  const editorSequence = useRef(0);
  const [openEditorId, setOpenEditorId] = useState(0);
  const [notice, setNotice] = useState<string | null>(null);
  // A refresh is the operator's own request: React Query's manual refetch
  // cancels the read it supersedes and starts a new one whenever cached data
  // exists for the key; a first read still holding no data is joined instead
  // (query-core 5.107.1, query.js:353-360). Do not gate a refresh on the
  // workspace read's `isFetching`: that field is not read during render (the
  // list query's is, at the refresh control and the status line), so a handler
  // can hold a stale in-flight snapshot and skip a read the operator asked for
  // (the failed workspace re-read that docs/archive/CLOSED-TODO.md records).
  const refresh = () => {
    void query.refetch();
    void workspace.refetch();
  };
  const connections = query.data?.connections ?? [];
  const selected = connections.find((connection) => connection.id === search.connectionId);
  // The editor renders from the subject it was opened for, re-read from the
  // authoritative list; when that connection is gone the dialog closes and the
  // operator is told instead of losing the draft silently.
  const editSubject = editingSubject === null ? undefined : connections.find((connection) => connection.id === editingSubject);
  const editSubjectRemoved = editingSubject !== null && query.data !== undefined && editSubject === undefined;
  // Mount state is not "still here" under lazy routes (this page stays mounted
  // while the destination chunk loads): the router's current location decides.
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; };
  }, []);
  // The router's matching is not the pathname's text: it matches `/connections/` and another case,
  // so ask which route the current location resolves to instead of comparing the pathname.
  const stillOnConnections = () => mounted.current && router.matchRoutes(router.latestLocation.pathname, router.latestLocation.search).at(-1)?.routeId === "/connections";
  // The live selection, readable by continuations that outlive this render: a
  // response that arrives after the operator chose another subject must not
  // navigate them back or act on the newer subject.
  const selectionRef = useRef<string | null>(search.connectionId ?? null);
  useLayoutEffect(() => { selectionRef.current = search.connectionId ?? null; }, [search.connectionId]);
  // The live project scope, readable by continuations that outlive this render:
  // a response that arrives after the operator switched projects is discarded
  // instead of acting on the newly selected project.
  const projectRef = useRef<string | null>(selectedProjectId);
  useLayoutEffect(() => { projectRef.current = selectedProjectId; }, [selectedProjectId]);
  useEffect(() => {
    if (!query.data || !search.connectionId || selected) return;
    void navigate({ to: "/connections", search: {}, replace: true });
  }, [navigate, query.data, search.connectionId, selected]);
  // The removal continuation outranks the shell's navigation focus (which moves
  // the page heading when clearing the selection changes the URL): the operator
  // was working in the removed detail, so the surviving page control takes the
  // focus. Both effects wait for the same idle navigation, and this passive
  // effect runs after the shell's heading layout effect, so the handoff is the
  // last word of the settled transition.
  const navigationStatus = useRouterState({ select: (state) => state.status });
  useEffect(() => {
    if (selected !== undefined || navigationStatus !== "idle" || !removalFocusPending.current) return;
    removalFocusPending.current = false;
    newConnectionRef.current?.focus();
  }, [selected, navigationStatus]);
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
  const beginWrite = () => { setPendingWrites((count) => count + 1); };
  const endWrite = () => { setPendingWrites((count) => Math.max(0, count - 1)); };
  const trackRequest = (connectionId: string, kind: "check" | "remove", pending: boolean) => { setPendingRequests((current) => {
      const counts = current[connectionId] ?? noPendingRequests;
      return { ...current, [connectionId]: { ...counts, [kind]: Math.max(0, counts[kind] + (pending ? 1 : -1)) } };
    }); };
  const trackSubject = (subject: string, pending: boolean) => { setPendingSubjects((current) => ({ ...current, [subject]: Math.max(0, (current[subject] ?? 0) + (pending ? 1 : -1)) })); };
  // A continuation that settles belongs to the connection the operator is
  // looking at: the page drops it when the operator moved to another
  // connection, even though the request outlives its detail instance. An empty
  // selection is not "moved to another connection": the change-discovery read
  // removes a connection another operator deleted, and that reconcile clears
  // the selection before the refusal its own subject caused arrives — the
  // operator still issued the request, so its reason must be stated.
  const noticeFor = (connectionId: string, notice: string) => {
    if (stillOnConnections() && (selectionRef.current === connectionId || selectionRef.current === null)) setNotice(notice);
  };
  const clearSelectionFor = (connectionId: string) => {
    if (!stillOnConnections() || selectionRef.current !== connectionId) return;
    removalFocusPending.current = true;
    void navigate({ to: "/connections", search: {} });
  };
  const saved = (disposition: string, connectionId: string, editor: number, capabilitiesChanged: boolean) => {
    if (editor !== editorSequence.current) {
      // A late response for an editor that has since closed: the write is real,
      // so re-read the list, but never close or navigate the open editor.
      void queryClient.invalidateQueries({ queryKey: connectionsReadKey() });
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
    void queryClient.invalidateQueries({ queryKey: connectionsReadKey() })
      .then(() => {
        if (!stillOnConnections() || editorSequence.current !== generation || selectionRef.current !== selection) return;
        void navigate({ to: "/connections", search: { connectionId } });
      });
  };
  const attention = connections.filter((connection) => connection.status !== "healthy").length;
  return <>
    <PageHeading title="Connections" description="GitHub and Azure DevOps connections this server authenticates through.">
      {/* The provider catalog arrives with this query: until it is loaded the
          create draft would have no provider to choose or submit. */}
      <button ref={newConnectionRef} className="button" type="button" disabled={query.data === undefined} onClick={() => { openEditor("create", null); }}><Plus size={16} aria-hidden="true" />New connection</button>
      <RefreshButton fetching={query.isFetching} refresh={refresh} />
    </PageHeading>
    {notice && <p className="notice" role="status">{notice}<button className="button" type="button" onClick={() => { setNotice(null); }}>Dismiss</button></p>}
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
            onEdit={() => { openEditor("edit", selected.id); }} onNotice={(notice) => { noticeFor(selected.id, notice); }} onRemoved={() => { clearSelectionFor(selected.id); }} />
        : <aside className="connection-detail" aria-label="Connection detail">
          <Cable size={28} aria-hidden="true" />
          <h2>Select a connection</h2>
          <p className="muted">Inspect authentication type, capabilities, health, bound resource state and the reason behind a failed provider check.</p>
        </aside>}
      <p className="sr-only" role="status">{query.isFetching && !query.isPending ? "Refreshing connections" : ""}</p>
    </div>}
    <ProjectResources
      api={application.api} csrf={csrf} projectId={selectedProjectId}
      workspace={workspace.data} workspaceError={workspace.error} workspacePending={workspace.isPending}
      onRefreshProject={() => { void workspace.refetch(); }}
      connections={connections} writePending={pendingWrites > 0} pendingSubjects={pendingSubjects}
      projectRef={projectRef} onWriteStart={beginWrite} onWriteSettled={endWrite} onSubjectPending={trackSubject} />
    {creating && <ConnectionEditor connection={null} providers={query.data?.providers ?? []} api={application.api} csrf={csrf} editorId={openEditorId} fallbackFocus={detailCloseRef} onSaved={saved} onWriteStart={beginWrite} onWriteSettled={endWrite} onClose={closeEditor} />}
    {editSubject && <ConnectionEditor connection={editSubject} providers={query.data?.providers ?? []} api={application.api} csrf={csrf} editorId={openEditorId} fallbackFocus={detailCloseRef} onSaved={saved} onWriteStart={beginWrite} onWriteSettled={endWrite} onClose={closeEditor} />}
  </>;
}
