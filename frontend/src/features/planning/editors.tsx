import * as Dialog from "@radix-ui/react-dialog";
import { useMutation } from "@tanstack/react-query";
import { X } from "lucide-react";
import { useRef, useState } from "react";
import { Field, FieldErrors } from "../../components/primitives";
import { useDialogOpener } from "../../components/use-dialog-opener";
import { type Api, type Project, type ProjectChanges, type ProjectDraft, type WorkItemChanges, type WorkItemDraft } from "../../services/api";
import {
  acknowledgeWorkItemCreation,
  deliveryValueEditable,
  executionDefiningWritesRefused,
  failureMode,
  ownerDiscardNote,
  projectCreatePayload,
  projectFormStateOf,
  projectUpdatePayload,
  providerOwnedControlsOf,
  refusalNotice,
  workItemDraftOf,
  workItemFormStateOf,
  workItemUpdatePayload,
  withoutProviderOwned,
  workItemCreationId,
  type ProjectFormState,
  type VersionedProject,
  type VersionedWorkItem,
  type WorkItemFormState,
} from "./write-policy";

function DialogHeading({ title, onClose }: { title: string; onClose: () => void }) {
  return <div className="dialog-heading">
    <Dialog.Title>{title}</Dialog.Title>
    <Dialog.Close asChild><button className="icon-button" type="button" aria-label="Close dialog" onClick={onClose}><X size={20} aria-hidden="true" /></button></Dialog.Close>
  </div>;
}

type ProjectIntent = { kind: "create"; payload: ProjectDraft } | { kind: "update"; payload: ProjectChanges };

interface ProjectEditorProps {
  /** No project creates; a project must carry its served revision to be edited. */
  project: VersionedProject | null;
  api: Api;
  csrf: string;
  /** Called with the confirmed identity and the notice wording for the applied change. */
  onSaved: (receipt: { id: string }, notice: string) => void;
  /** Called for an authoritative refusal: the owner closes the editor and re-reads the server state. */
  onRefused: (message: string) => void;
  onClose: () => void;
  onWriteStart: () => void;
  onWriteSettled: () => void;
}

/**
 * Project create or settings. The draft and the submission intent are snapshots
 * taken when the dialog opens and when the operator submits: a background
 * refresh cannot rewrite either, and an explicit retry re-sends exactly the
 * failed intent, including the version it was edited from.
 */
export function ProjectEditor({ project, api, csrf, onSaved, onRefused, onClose, onWriteStart, onWriteSettled }: ProjectEditorProps) {
  // These dialogs carry no Radix trigger: the invoking control is captured when
  // the editor mounts and focused again on close, unless a pending write
  // disabled it — then focus is left where Radix would put it.
  const { restore } = useDialogOpener();
  // The revision this editor was opened from; never re-derived from a refresh.
  const [target] = useState(() => project === null ? null : { id: project.id, version: project.version });
  const [form, setForm] = useState<ProjectFormState>(() => projectFormStateOf(project));
  // The frozen submission: build once at submit, replay verbatim on request.
  const [intent, setIntent] = useState<ProjectIntent | null>(null);
  // A synchronous guard, not only the disabled control: two submits in one tick
  // (Enter plus click, a queued submit event) would both read the rendered
  // pending flag as false, and the duplicate would reach the server.
  const inFlightRef = useRef(false);
  const save = useMutation({
    mutationFn: (submitted: ProjectIntent) => submitted.kind === "create"
      ? api.createProject(submitted.payload, csrf, new AbortController().signal)
      : api.updateProject(project?.id ?? "", submitted.payload, csrf, new AbortController().signal),
    onSuccess: (receipt, submitted) => {
      if (submitted.kind === "create") onSaved(receipt, "Project created.");
      else onSaved(receipt, submitted.payload.status === "archived" ? "Project archived." : submitted.payload.status === "active" && project?.status === "archived" ? "Project restored." : "Project updated.");
    },
    onError: (error) => {
      if (failureMode(error) === "authoritative") onRefused(refusalNotice(error));
    },
    onSettled: () => { inFlightRef.current = false; onWriteSettled(); },
  });
  const failure = save.error;
  const retryable = failure !== null && failureMode(failure) === "retryable";
  const pending = save.isPending;
  const set = (patch: Partial<ProjectFormState>) => { setForm((current) => ({ ...current, ...patch })); };
  const submit = () => {
    if (pending || inFlightRef.current) return;
    const submitted: ProjectIntent = target === null || project === null
      ? { kind: "create", payload: projectCreatePayload(form) }
      : { kind: "update", payload: projectUpdatePayload(form, target.version) };
    setIntent(submitted);
    inFlightRef.current = true;
    onWriteStart();
    save.mutate(submitted);
  };
  const close = () => { if (!pending) onClose(); };
  // Only the operator's own dismissals close this form. Radix also asks to
  // close on unattributable layer events (a window resize, which a full-page
  // capture triggers); ignoring that request means an open draft can never be
  // discarded silently, while Escape, the overlay, the close control and
  // Cancel stay the operator's dismissals.
  return <Dialog.Root open>
    <Dialog.Portal>
      <Dialog.Overlay className="dialog-overlay" />
      <Dialog.Content className="command-dialog planning-dialog" aria-describedby="project-editor-description"
        onEscapeKeyDown={() => { close(); }}
        onPointerDownOutside={() => { close(); }}
        onCloseAutoFocus={(event) => { restore(event); }}>
        <DialogHeading title={project === null ? "New project" : "Project settings"} onClose={close} />
        <Dialog.Description id="project-editor-description" className="muted">
          {project === null
            ? "A project groups work items and their engineering resources. The key is fixed at creation."
            : `Version ${target?.version ?? project.version}. Changes are refused if another operator saved first.`}
        </Dialog.Description>
        <form className="planning-form" aria-busy={pending} onSubmit={(event) => { event.preventDefault(); submit(); }}>
          <Field id="project-name" label="Name" error={failure} field="name">
            <input id="project-name" name="name" value={form.name} required maxLength={120} disabled={pending}
              onChange={(event) => { const name = event.currentTarget.value; set({ name }); }} />
          </Field>

          {project === null && <>
            <Field id="project-key" label="Key" error={failure} field="key">
              <input id="project-key" name="key" value={form.key} required maxLength={8} disabled={pending}
                onChange={(event) => { const key = event.currentTarget.value; set({ key }); }} />
            </Field>
          </>}

          {project !== null && <>
            <Field id="project-status" label="Status" error={failure} field="status">
              <select id="project-status" name="status" value={form.status} disabled={pending}
                onChange={(event) => { const status = event.currentTarget.value; set({ status }); }}>
                <option value="active">Active</option>
                <option value="archived">Archived</option>
              </select>
            </Field>
          </>}

          <Field id="project-agent" label="Default agent" error={failure} field="default_agent_profile">
            <input id="project-agent" name="default_agent_profile" value={form.default_agent_profile} maxLength={120} disabled={pending}
              onChange={(event) => { const default_agent_profile = event.currentTarget.value; set({ default_agent_profile }); }} />
          </Field>

          <Field id="project-workspace" label="Workspace" error={failure} field="default_workspace">
            <input id="project-workspace" name="default_workspace" value={form.default_workspace} maxLength={240} disabled={pending}
              onChange={(event) => { const default_workspace = event.currentTarget.value; set({ default_workspace }); }} />
          </Field>

          <Field id="project-description" label="Description" error={failure} field="description">
            <textarea id="project-description" name="description" value={form.description} rows={4} maxLength={4000} disabled={pending}
              onChange={(event) => { const description = event.currentTarget.value; set({ description }); }} />
          </Field>

          {failure !== null && <p className="notice notice-error" role="alert">{refusalNotice(failure)}</p>}
          <div className="dialog-actions">
            <button className="button button-primary" type="submit" disabled={pending}>{pending ? "Saving…" : project === null ? "Create" : "Save"}</button>
            {retryable && intent !== null && <button className="button" type="button" disabled={pending}
              onClick={() => { if (inFlightRef.current) return; inFlightRef.current = true; onWriteStart(); save.mutate(intent); }}>Try again</button>}
            <button className="button" type="button" onClick={close} disabled={pending}>Cancel</button>
          </div>
        </form>
      </Dialog.Content>
    </Dialog.Portal>
  </Dialog.Root>;
}

type WorkItemIntent = { kind: "create"; payload: WorkItemDraft } | { kind: "update"; payload: WorkItemChanges };

interface WorkItemEditorProps {
  project: Project;
  /** No item creates; an item must carry its served revision to be edited. */
  item: VersionedWorkItem | null;
  api: Api;
  csrf: string;
  onSaved: (receipt: { id: string }, notice: string) => void;
  onRefused: (message: string) => void;
  onClose: () => void;
  onWriteStart: () => void;
  onWriteSettled: () => void;
  /**
   * The presentation this editor is mounted into. `"drawer"` portals the
   * dialog's DOM into the detail drawer's host node: the drawer is then an
   * ancestor of the open editor, so Radix's aria-hidden bookkeeping keeps the
   * editor visible whichever of the two dialogs mounted first. `"page"` uses
   * the default body portal.
   */
  presentation: "page" | "drawer";
  /** The drawer's host node, owned by the detail page; required by `"drawer"`. */
  drawerHost?: HTMLElement | null;
}

interface WorkItemDraftState {
  form: WorkItemFormState;
  set: (patch: Partial<WorkItemFormState>) => void;
  failure: unknown;
  /** The create refusal when this browser cannot mint an item identity. */
  identityFailure: string | null;
  pending: boolean;
  retryable: boolean;
  owned: ReadonlySet<string>;
  delivery: { pullRequest: boolean; ci: boolean; review: boolean };
  hasIntent: boolean;
  targetVersion: number | null;
  submit: () => void;
  retry: () => void;
  close: () => void;
  /** The opener restore this dialog's close runs (Radix's own target when the opener is gone or disabled). */
  restore: (event: Event) => void;
}

/**
 * Work-item create or edit. Fields the served external-tracker contract owns
 * are rendered read-only, delivery values owned by a provider or by reported
 * evidence are rendered read-only with their source, and the update never
 * carries a status (Control refuses move fields on this path; the board moves).
 *
 * The draft state lives here, outside the responsive presentation branches:
 * the presentation props gate and place only the dialog shell below, so a
 * breakpoint change cannot discard the form, the frozen intent or a pending
 * answer.
 */
export function WorkItemEditor(props: WorkItemEditorProps) {
  const draft = useWorkItemDraft(props);
  // The drawer renders its host node in the commit it mounts; the shell needs
  // that node to exist before it can portal into it.
  if (props.presentation === "drawer" && !props.drawerHost) return null;
  return <WorkItemEditorDialog {...props} draft={draft} />;
}

/** The draft, its frozen submission intent, and the create identity it replays. */
function useWorkItemDraft({ project, item, api, csrf, onSaved, onRefused, onClose, onWriteStart, onWriteSettled }: WorkItemEditorProps): WorkItemDraftState {
  const { restore } = useDialogOpener();
  const [target] = useState(() => item === null ? null : { id: item.id, version: item.version });
  const [form, setForm] = useState<WorkItemFormState>(() => workItemFormStateOf(project, item));
  const [intent, setIntent] = useState<WorkItemIntent | null>(null);
  const [identityFailure, setIdentityFailure] = useState<string | null>(null);
  const inFlightRef = useRef(false);
  /** Truthful notes about fields the submission could not carry. */
  const notesRef = useRef<readonly string[]>([]);
  const owned = providerOwnedControlsOf(item ?? {});
  const delivery = {
    pullRequest: deliveryValueEditable(item?.delivery?.pull_request),
    ci: deliveryValueEditable(item?.delivery?.ci),
    review: deliveryValueEditable(item?.delivery?.review),
  };
  const save = useMutation({
    mutationFn: (submitted: WorkItemIntent) => submitted.kind === "create"
      ? api.createWorkItem(project.id, submitted.payload, csrf, new AbortController().signal)
      : api.updateWorkItem(item?.id ?? "", submitted.payload, csrf, new AbortController().signal),
    onSuccess: (receipt, submitted) => {
      // An acknowledged create releases its identity: the next draft mints a
      // new one, while an unacknowledged draft keeps replaying this one.
      if (submitted.kind === "create") acknowledgeWorkItemCreation(project.id);
      // The disposition is the server's own statement: a replayed creation
      // returned the row the original submission created, not a new one.
      const base = submitted.kind === "create"
        ? receipt.disposition === "replayed"
          ? "Work item already existed; the server returned the original row, so this submission was not applied."
          : "Work item created."
        : "Work item updated.";
      const notes = notesRef.current;
      if (notes.length === 0) { onSaved(receipt, base); return; }
      onSaved(receipt, `${base} ${notes.join(" ")}`);
    },
    onError: (error) => {
      if (failureMode(error) === "authoritative") onRefused(refusalNotice(error));
    },
    onSettled: () => { inFlightRef.current = false; onWriteSettled(); },
  });
  const failure = save.error;
  const pending = save.isPending;
  const set = (patch: Partial<WorkItemFormState>) => { setForm((current) => ({ ...current, ...patch })); };
  const submit = () => {
    if (pending || inFlightRef.current) return;
    setIdentityFailure(null);
    let submitted: WorkItemIntent;
    if (target === null || item === null) {
      // The creation identity travels with the frozen intent, not with the
      // form: every attempt of this draft replays the same identity verbatim.
      // A browser that cannot mint one refuses visibly instead of sending a
      // create the server could apply twice.
      const creationId = workItemCreationId(project.id);
      if (creationId === null) {
        setIdentityFailure("This browser cannot mint an item identity, so the request was not sent.");
        return;
      }
      const draft = workItemDraftOf(form, { includeStatus: true, deliveryEditable: delivery });
      // The owner rewrite discards the same fields on create: the values
      // travel in the payload, so the note names them there too.
      const createNote = ownerDiscardNote(form.assignee_type, draft);
      notesRef.current = createNote === null ? [] : [createNote];
      submitted = { kind: "create", payload: { ...draft, creation_id: creationId } };
    } else {
      const intendedDraft = workItemUpdatePayload(form, target.version, delivery);
      const payload = withoutProviderOwned(intendedDraft, owned);
      // Only a field the operator actually changed counts: the served row is
      // the comparison, read through the typed members of each side so a
      // control whose served shape differs (the owner is served as `assignee`)
      // stays unreported instead of producing a false claim about a dropped
      // change.
      const intendedValue = (key: string): unknown => {
        switch (key) {
          case "title": return intendedDraft.title;
          case "description": return intendedDraft.description;
          case "priority": return intendedDraft.priority;
          case "assignee_type": return intendedDraft.assignee_type;
          case "assignee_name": return intendedDraft.assignee_name;
          case "agent_profile": return intendedDraft.agent_profile;
          case "workspace": return intendedDraft.workspace;
          case "repository_resource_id": return intendedDraft.repository_resource_id;
          case "ci_resource_id": return intendedDraft.ci_resource_id;
          case "branch": return intendedDraft.branch;
          case "blocked": return intendedDraft.blocked;
          case "blocker": return intendedDraft.blocker;
          case "pull_request_url": return intendedDraft.pull_request_url;
          case "ci_status": return intendedDraft.ci_status;
          case "review_status": return intendedDraft.review_status;
          case "version": return intendedDraft.version;
          default: return undefined;
        }
      };
      const servedValue = (key: string): unknown => {
        switch (key) {
          case "title": return item.title;
          case "description": return item.description;
          case "priority": return item.priority;
          case "assignee_type": return item.assignee?.type;
          case "assignee_name": return item.assignee?.name;
          case "agent_profile": return item.assignee?.agent_profile;
          case "workspace": return item.workspace;
          case "repository_resource_id": return item.repository_resource_id;
          case "ci_resource_id": return item.ci_resource_id;
          case "branch": return item.branch;
          case "blocked": return item.blocked;
          case "blocker": return item.blocker;
          case "pull_request_url": return item.pull_request_url;
          case "ci_status": return item.ci_status;
          case "review_status": return item.review_status;
          case "version": return item.version;
          default: return undefined;
        }
      };
      const dropped = Object.keys(intendedDraft).filter((key) => {
        if (key === "version" || key in payload) return false;
        const before = servedValue(key);
        return before !== undefined && JSON.stringify(intendedValue(key)) !== JSON.stringify(before);
      });
      const notes = dropped.length === 0
        ? []
        : [dropped.length === 1
            ? `The server reports ${dropped[0]} as owned elsewhere, so it was not sent.`
            : `The server reports ${dropped.join(", ")} as owned elsewhere, so they were not sent.`];
      // The owner rewrite discards what the payload carries for the non-agent
      // owner types (workspaces.ex forces these NULL); the values are named
      // from the payload that was actually sent, and a deliberate clear (null)
      // is applied by the server, never reported as not applied.
      const ownerNote = ownerDiscardNote(form.assignee_type, payload);
      if (ownerNote !== null) notes.push(ownerNote);
      // A delivery value whose served source became a provider or reported
      // evidence while the form was open is no longer offered: the typed value
      // cannot be sent, and the notice names it instead of reporting success.
      const claimed: string[] = [];
      if (!delivery.pullRequest && form.pull_request_url !== "" && form.pull_request_url !== servedValue("pull_request_url")) claimed.push("pull request URL");
      if (!delivery.ci && form.ci_status !== "" && form.ci_status !== servedValue("ci_status")) claimed.push("CI status");
      if (!delivery.review && form.review_status !== "" && form.review_status !== servedValue("review_status")) claimed.push("review status");
      if (claimed.length > 0) {
        notes.push(`The server now owns the ${claimed.join(", ")}, so the value you typed was not sent.`);
      }
      notesRef.current = notes;
      submitted = { kind: "update", payload };
    }
    setIntent(submitted);
    inFlightRef.current = true;
    onWriteStart();
    save.mutate(submitted);
  };
  const retry = () => {
    if (intent === null || inFlightRef.current) return;
    inFlightRef.current = true;
    onWriteStart();
    save.mutate(intent);
  };
  const close = () => { if (!pending) onClose(); };
  return {
    form,
    set,
    failure,
    identityFailure,
    pending,
    retryable: failure !== null && failureMode(failure) === "retryable",
    owned,
    delivery,
    hasIntent: intent !== null,
    targetVersion: target === null ? null : target.version,
    submit,
    retry,
    close,
    restore,
  };
}

function WorkItemEditorDialog({ project, item, presentation, drawerHost, draft }: WorkItemEditorProps & { draft: WorkItemDraftState }) {
  const { form, set, failure, identityFailure, pending, retryable, owned, delivery, hasIntent, targetVersion, submit, retry, close, restore } = draft;
  const resources = project.resources ?? [];
  const repositories = resources.filter((resource) => resource.kind === "repository");
  const ciResources = resources.filter((resource) => resource.kind === "ci");
  const someProviderOwned = owned.size > 0;
  // Same draft-survival rule as the project editor: only the operator's own
  // dismissals close this form.
  return <Dialog.Root open>
    <Dialog.Portal container={presentation === "drawer" ? drawerHost ?? undefined : undefined}>
      <Dialog.Overlay className="dialog-overlay" />
      <Dialog.Content className="command-dialog planning-dialog" aria-describedby="work-item-editor-description"
        onEscapeKeyDown={() => { close(); }}
        onPointerDownOutside={() => { close(); }}
        onCloseAutoFocus={(event) => { restore(event); }}>
        <DialogHeading title={item === null ? "New work item" : "Edit work item"} onClose={close} />
        <Dialog.Description id="work-item-editor-description" className="muted">
          {item === null
            ? `Created in ${project.name} with the status you choose.`
            : `Version ${targetVersion ?? item.version}. Status and order move on the board; this form edits the recorded fields.`}
        </Dialog.Description>
        <form className="planning-form" aria-busy={pending} onSubmit={(event) => { event.preventDefault(); submit(); }}>
          <Field id="work-item-title" label="Title" error={failure} field="title">
            <input id="work-item-title" name="title" value={form.title} required maxLength={240} disabled={pending || owned.has("title")}
              onChange={(event) => { const title = event.currentTarget.value; set({ title }); }} />
          </Field>

          <Field id="work-item-description" label="Description" error={failure} field="description">
            <textarea id="work-item-description" name="description" value={form.description} rows={4} maxLength={20000} disabled={pending || owned.has("description")}
              onChange={(event) => { const description = event.currentTarget.value; set({ description }); }} />
          </Field>

          {item === null && <>
            <Field id="work-item-status" label="Status" error={failure} field="status">
              <select id="work-item-status" name="status" value={form.status} disabled={pending}
                onChange={(event) => { const status = event.currentTarget.value; set({ status }); }}>
                <option value="backlog">Backlog</option>
                <option value="ready">Ready</option>
                <option value="in_progress">In progress</option>
                <option value="review">Review</option>
                <option value="done">Done</option>
              </select>
            </Field>
          </>}

          <Field id="work-item-priority" label="Priority" error={failure} field="priority">
            <select id="work-item-priority" name="priority" value={form.priority} disabled={pending || owned.has("priority")}
              onChange={(event) => { const priority = event.currentTarget.value; set({ priority }); }}>
              <option value="urgent">Urgent</option>
              <option value="high">High</option>
              <option value="medium">Medium</option>
              <option value="low">Low</option>
              <option value="no_priority">No priority</option>
            </select>
          </Field>

          <Field id="work-item-owner-type" label="Owner type" error={failure} field="assignee_type">
            <select id="work-item-owner-type" name="assignee_type" value={form.assignee_type} disabled={pending || owned.has("assignee_type")}
              onChange={(event) => { const assignee_type = event.currentTarget.value; set({ assignee_type }); }}>
              <option value="unassigned">Unassigned</option>
              <option value="human">Human</option>
              <option value="agent">Agent</option>
            </select>
          </Field>

          <Field id="work-item-owner" label="Owner" error={failure} field="assignee_name">
            <input id="work-item-owner" name="assignee_name" value={form.assignee_name} maxLength={120} disabled={pending || owned.has("assignee_name")}
              onChange={(event) => { const assignee_name = event.currentTarget.value; set({ assignee_name }); }} />
          </Field>

          <Field id="work-item-agent" label="Agent profile" error={failure} field="agent_profile">
            <input id="work-item-agent" name="agent_profile" value={form.agent_profile} maxLength={120} disabled={pending}
              onChange={(event) => { const agent_profile = event.currentTarget.value; set({ agent_profile }); }} />
          </Field>

          <Field id="work-item-workspace" label="Workspace" error={failure} field="workspace">
            <input id="work-item-workspace" name="workspace" value={form.workspace} maxLength={240} disabled={pending}
              onChange={(event) => { const workspace = event.currentTarget.value; set({ workspace }); }} />
          </Field>

          <Field id="work-item-repository" label="Repository" error={failure} field="repository_resource_id">
            <select id="work-item-repository" name="repository_resource_id" value={form.repository_resource_id} disabled={pending}
              onChange={(event) => { const repository_resource_id = event.currentTarget.value; set({ repository_resource_id }); }}>
              <option value="">Not linked</option>
              {repositories.map((resource) => <option key={resource.id} value={resource.id}>{resource.name ?? "Unnamed resource"}</option>)}
            </select>
          </Field>

          <Field id="work-item-ci" label="CI resource" error={failure} field="ci_resource_id">
            <select id="work-item-ci" name="ci_resource_id" value={form.ci_resource_id} disabled={pending}
              onChange={(event) => { const ci_resource_id = event.currentTarget.value; set({ ci_resource_id }); }}>
              <option value="">Use repository provider</option>
              {ciResources.map((resource) => <option key={resource.id} value={resource.id}>{resource.name ?? "Unnamed resource"}</option>)}
            </select>
          </Field>

          <Field id="work-item-branch" label="Branch" error={failure} field="branch">
            <input id="work-item-branch" name="branch" value={form.branch} maxLength={500} disabled={pending}
              onChange={(event) => { const branch = event.currentTarget.value; set({ branch }); }} />
          </Field>

          <Field id="work-item-pull-request" label="Pull request URL" error={failure} field="pull_request_url">
            {delivery.pullRequest
              ? <input id="work-item-pull-request" name="pull_request_url" type="url" value={form.pull_request_url} disabled={pending}
                onChange={(event) => { const pull_request_url = event.currentTarget.value; set({ pull_request_url }); }} />
              : <output id="work-item-pull-request">{form.pull_request_url || "Not recorded"} · {item?.delivery?.pull_request.source ?? "provider"} value, not editable here</output>}
          </Field>

          <Field id="work-item-ci-status" label="CI" error={failure} field="ci_status">
            {delivery.ci
              ? <select id="work-item-ci-status" name="ci_status" value={form.ci_status} disabled={pending}
                onChange={(event) => { const ci_status = event.currentTarget.value; set({ ci_status }); }}>
                <option value="">Not recorded</option>
                <option value="unknown">Unknown</option>
                <option value="pending">Pending</option>
                <option value="passed">Passed</option>
                <option value="failed">Failed</option>
              </select>
              : <output id="work-item-ci-status">{form.ci_status || "Not recorded"} · {item?.delivery?.ci.source ?? "provider"} value, not editable here</output>}
          </Field>

          <Field id="work-item-review-status" label="Review" error={failure} field="review_status">
            {delivery.review
              ? <select id="work-item-review-status" name="review_status" value={form.review_status} disabled={pending}
                onChange={(event) => { const review_status = event.currentTarget.value; set({ review_status }); }}>
                <option value="">Not recorded</option>
                <option value="none">None</option>
                <option value="required">Required</option>
                <option value="changes_requested">Changes requested</option>
                <option value="approved">Approved</option>
              </select>
              : <output id="work-item-review-status">{form.review_status || "Not recorded"} · {item?.delivery?.review.source ?? "provider"} value, not editable here</output>}
          </Field>

          <label className="checkbox-field" htmlFor="work-item-blocked">
            <input id="work-item-blocked" name="blocked" type="checkbox" checked={form.blocked} disabled={pending}
              onChange={(event) => { const blocked = event.currentTarget.checked; set({ blocked }); }} />Blocked
          </label>
          <FieldErrors error={failure} field="blocked" />

          <Field id="work-item-blocker" label="Blocker" error={failure} field="blocker">
            <input id="work-item-blocker" name="blocker" value={form.blocker} maxLength={1000} disabled={pending}
              onChange={(event) => { const blocker = event.currentTarget.value; set({ blocker }); }} />
          </Field>

          {someProviderOwned && <p className="muted text-small">Fields owned by the external work tracker ({[...owned].join(", ")}) are shown read-only; edit them in the tracker.</p>}
          {item !== null && executionDefiningWritesRefused(item) && <p className="muted text-small">Control refuses changes to execution-defining fields (title, description, owner, agent profile, workspace, repository, CI resource, branch, pull request URL) while this item's orchestration task is not failed or cancelled; a cleared value is refused the same way.</p>}
          {failure !== null && failureMode(failure) !== "authoritative" && <p className="notice notice-error" role="alert">{refusalNotice(failure)}</p>}
          {identityFailure !== null && <p className="notice notice-error" role="alert">{identityFailure}</p>}
          <div className="dialog-actions">
            <button className="button button-primary" type="submit" disabled={pending}>{pending ? "Saving…" : item === null ? "Create" : "Save"}</button>
            {retryable && hasIntent && <button className="button" type="button" disabled={pending} onClick={retry}>Try again</button>}
            <button className="button" type="button" onClick={close} disabled={pending}>Cancel</button>
          </div>
        </form>
      </Dialog.Content>
    </Dialog.Portal>
  </Dialog.Root>;
}
