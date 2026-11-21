import * as Dialog from "@radix-ui/react-dialog";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { X } from "lucide-react";
import { useEffect, useLayoutEffect, useRef, useState, type RefObject } from "react";
import { workspaceOptions } from "../../app/application";
import { useSessionToken } from "../../app/session";
import { restoreFocus } from "../../components/focus";
import {
  errorMessage,
  refusalNotice,
  ServerRefusal,
  type Api,
  type WorkDetail,
  type WorkItemAssignment,
  type Workspace,
} from "../../services/api";
import { asText, ownerLabel } from "./presentation";

/**
 * Assignment on `/portal/work/:workItemId`, rendered in both the desktop drawer
 * and the narrow page. The served facts are read from the detail envelope; the
 * editor offers the item's own project's resources, read through the same
 * `workspaceOptions` query the project surfaces use, so another project's rows
 * can never appear. Provider-owned fields (title, description, priority, work
 * state) have no control here: the server refuses those edits and this surface
 * never proposes one.
 */

type WorkItemDetail = WorkDetail["work_item"];
type ProjectRead = Workspace["projects"][number];

/** A resource row as offered for assignment: the identity the operator selects. */
interface AssignmentResource {
  id: string;
  name: string;
  externalRef: string | null;
}

const ownerTypeChoices = [
  { value: "unassigned", label: "Unassigned" },
  { value: "human", label: "Human" },
  { value: "agent", label: "Agent" },
] as const;

/**
 * The item's project's resources of one kind. The project is located in the read
 * by the item's own `project_id`, and a row that names a different project is
 * dropped, so a sibling project's rows stay unofferable even in a malformed read.
 */
function resourcesOfKind(project: ProjectRead | null, projectId: string | null, kind: string): AssignmentResource[] {
  if (project === null) return [];
  return (project.resources ?? []).flatMap((row) => {
    const id = asText(row.id);
    if (id === null || asText(row.kind) !== kind) return [];
    const owner = asText(row.project_id);
    if (owner !== null && owner !== projectId) return [];
    return [{ id, name: asText(row.name) ?? id, externalRef: asText(row.external_ref) }];
  });
}

/**
 * The current binding stays offerable when the project read cannot list it (a
 * filtered read, or a resource of another kind): the editor keeps it selected
 * and labelled instead of showing an empty select that a save would read as a
 * request to unlink. An unreadable project read offers only the empty option,
 * exactly as the surface states, and blocks the submit.
 */
function bindingOptions(resources: readonly AssignmentResource[], current: string | null, kind: "repository" | "ci", readable: boolean) {
  // The served label names the identity, not a guess: the resource name and its
  // exact reference. Served order is kept and nothing is invented.
  const options = readable
    ? resources.map((resource) => ({ id: resource.id, label: `${resource.name} · ${resource.externalRef ?? "no external reference"}` }))
    : [];
  if (!readable || current === null || options.some((option) => option.id === current)) return options;
  const kindLabel = kind === "repository" ? "repository resources" : "CI resources";
  return [{ id: current, label: `${current} · current binding, not among this project's ${kindLabel}` }, ...options];
}

// The server owns the refusal reason; this surface adds the operator's next step
// for the codes the assignment endpoint answers with.
const assignmentGuidance: Record<string, string> = {
  provider_owned: "The provider keeps the title, description and priority; only the assignment fields here are editable.",
  stale: "The work item changed elsewhere; close the editor and reopen it from the re-read detail.",
  state_conflict: "This item or its project is in a state that refuses the write; re-read both, then retry.",
  validation_failed: "Correct the highlighted fields and submit again.",
};

/** Empty text means "server default": the field is submitted as null, never as "". */
function present(value: string): string | null {
  const trimmed = value.trim();
  return trimmed === "" ? null : trimmed;
}

type ProjectReadState = "ready" | "loading" | "unavailable";

/**
 * Why the editor cannot submit. The project read is the source of the resource
 * options, so an unreadable read offers only the empty option and blocks the
 * write; a served item without a version cannot be fenced at all.
 */
function editorBlockedReason(readState: ProjectReadState, projectId: string | null, error: unknown, version: number | undefined): string | null {
  if (readState === "loading") return "Loading this work item's project resources…";
  if (readState === "unavailable") {
    const consequence = "the resource fields offer only their empty option";
    if (projectId === null) return `The served work item names no project, so ${consequence}.`;
    if (error !== null && error !== undefined) {
      return `This work item's project resources could not be read, so ${consequence}. ${errorMessage(error)}`;
    }
    return `The project read does not include this work item's project, so ${consequence}.`;
  }
  if (typeof version !== "number") return "The served work item carried no version, so the assignment cannot be fenced. Refresh the page to read its current revision.";
  return null;
}

export function AssignmentSection({ api, item }: { api: Api; item: WorkItemDetail }) {
  const csrf = useSessionToken();
  // The stable control a close falls back to when the invoking control is
  // disabled by a pending write (a background re-read can flip `intent_locked`
  // while the dialog is open): the section heading is always focusable.
  const headingRef = useRef<HTMLHeadingElement | null>(null);
  // Continuations outlive their editor: a save that settles after the operator
  // selected another item must not write into that item's surface, so every
  // write into this section is fenced on the live subject and the mount.
  const mounted = useRef(true);
  const subjectRef = useRef(item.id);
  useLayoutEffect(() => { subjectRef.current = item.id; }, [item.id]);
  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; };
  }, []);
  const projectId = asText(item.project_id);
  // The read is keyed by the item's own project id; a null scope would resolve to
  // the server-selected project, which is never this item's project by argument.
  const projectQuery = useQuery({ ...workspaceOptions(api, projectId), enabled: projectId !== null });
  const project = projectId === null ? null : projectQuery.data?.projects.find((entry) => entry.id === projectId) ?? null;
  const readState: ProjectReadState = project !== null ? "ready" : projectId !== null && projectQuery.isPending ? "loading" : "unavailable";
  const blockedReason = editorBlockedReason(readState, projectId, projectQuery.error, item.version);
  const [editingFor, setEditingFor] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const intentLocked = item.execution?.intent_locked === true;
  // The editor belongs to the item it was opened for: another selection renders
  // no editor, and a late continuation for the old item cannot mark the new one.
  const editorOpen = editingFor === item.id;
  const saved = (workItemId: string, version: number) => {
    if (!mounted.current || subjectRef.current !== workItemId) return;
    setEditingFor(null);
    setNotice(`Assignment saved at version ${version}; the served facts above reload from the server.`);
  };
  return <section className="detail-section" aria-labelledby="assignment-section">
    <h2 id="assignment-section" ref={headingRef} tabIndex={-1}>Assignment</h2>
    <dl className="detail-facts">
      <div><dt>Owner</dt><dd>{ownerLabel(item.assignee)}</dd></div>
      <div><dt>Workspace</dt><dd>{asText(item.workspace) ?? "Project default"}</dd></div>
      <div><dt>Repository</dt><dd>{asText(item.repository) ?? "Not linked"}</dd></div>
      <div><dt>CI resource</dt><dd>{asText(item.ci_resource) ?? "Repository provider"}</dd></div>
    </dl>
    {intentLocked && <p className="notice" role="status">A live execution holds this item's intent, so assignment is locked until the execution settles.</p>}
    {notice !== null && <p className="notice" role="status">{notice}<button className="button" type="button" onClick={() => { setNotice(null); }}>Dismiss</button></p>}
    <div className="assignment-actions">
      <button className="button" type="button" disabled={intentLocked} onClick={() => { setNotice(null); setEditingFor(item.id); }}>Edit assignment</button>
    </div>
    {editorOpen && <AssignmentEditor
      api={api}
      csrf={csrf}
      item={item}
      project={project}
      blockedReason={blockedReason}
      fallbackFocus={headingRef}
      repositoryResources={resourcesOfKind(project, projectId, "repository")}
      ciResources={resourcesOfKind(project, projectId, "ci")}
      onSaved={saved}
      onClose={() => { setEditingFor(null); }}
    />}
  </section>;
}

interface AssignmentDraft {
  ownerType: string;
  owner: string;
  agentProfile: string;
  workspace: string;
  repository: string;
  ci: string;
}

function AssignmentEditor({ api, csrf, item, project, blockedReason, fallbackFocus, repositoryResources, ciResources, onSaved, onClose }: {
  api: Api;
  csrf: string;
  item: WorkItemDetail;
  project: ProjectRead | null;
  blockedReason: string | null;
  fallbackFocus: RefObject<HTMLElement | null>;
  repositoryResources: readonly AssignmentResource[];
  ciResources: readonly AssignmentResource[];
  onSaved: (workItemId: string, version: number) => void;
  onClose: () => void;
}) {
  const queryClient = useQueryClient();
  // Radix restores focus on close only to a Dialog.Trigger: capture the invoking
  // control before Radix moves keyboard focus inside the dialog.
  const opener = useRef<HTMLElement | null>(document.activeElement instanceof HTMLElement ? document.activeElement : null);
  // The identity and version this editor was opened from: the save is submitted
  // against the version the operator saw, so a background refresh cannot turn the
  // displayed draft into a write against a revision that was never shown.
  const [target] = useState(() => ({ id: item.id, version: typeof item.version === "number" ? item.version : null }));
  // The draft starts from the served assignment; every value is the server's, and
  // an empty text field is submitted as null so the server applies its default.
  const [draft, setDraft] = useState<AssignmentDraft>(() => ({
    ownerType: asText(item.assignee?.type) ?? "unassigned",
    owner: asText(item.assignee?.name) ?? "",
    agentProfile: asText(item.assignee?.agent_profile) ?? "",
    workspace: asText(item.workspace) ?? "",
    repository: asText(item.repository_resource_id) ?? "",
    ci: asText(item.ci_resource_id) ?? "",
  }));
  const itemProjectId = asText(item.project_id);
  const save = useMutation({
    mutationFn: async (payload: { draft: AssignmentDraft; version: number }) => {
      // Only the assignment fields are sent: no provider-owned value and no field
      // this surface does not show can ride along with the version fence.
      const assignment: WorkItemAssignment = {
        version: payload.version,
        assignee_type: payload.draft.ownerType,
        assignee_name: present(payload.draft.owner),
        agent_profile: present(payload.draft.agentProfile),
        workspace: present(payload.draft.workspace),
        repository_resource_id: payload.draft.repository === "" ? null : payload.draft.repository,
        ci_resource_id: payload.draft.ci === "" ? null : payload.draft.ci,
      };
      return api.assignWorkItem(target.id, assignment, csrf, new AbortController().signal);
    },
    onSuccess: (receipt) => { onSaved(receipt.workItemId, receipt.version); },
    // A settled write re-reads both reads this surface steers from — the work
    // detail and the project read the option sets came from — even when the
    // operator has already left the item and this editor is unmounted.
    onSettled: () => {
      void Promise.all([
        queryClient.invalidateQueries({ queryKey: ["work-item", { workItemId: target.id }] }),
        ...(itemProjectId === null ? [] : [queryClient.invalidateQueries({ queryKey: ["workspace", { projectId: itemProjectId }] })]),
      ]);
    },
  });
  const refusal = save.error;
  const stale = refusal instanceof ServerRefusal && refusal.code === "stale";
  const intentLocked = item.execution?.intent_locked === true;
  const blocked = save.isPending || stale || intentLocked || blockedReason !== null || target.version === null;
  // A small invariant only a broken read could reach; the submit is blocked above,
  // so the guard keeps the payload fenced rather than trusting the render.
  const version = target.version;
  const projectAgentProfile = asText(project?.default_agent_profile);
  const projectWorkspace = asText(project?.default_workspace);
  const ownerPlaceholder = draft.ownerType === "unassigned"
    ? "Unassigned"
    : draft.ownerType === "human" ? "No name" : projectAgentProfile ?? "Server default";
  const ownerTypeOptions = ownerTypeChoices.some((choice) => choice.value === draft.ownerType)
    ? ownerTypeChoices
    : [...ownerTypeChoices, { value: draft.ownerType, label: `Unknown owner type: ${draft.ownerType}` }];
  const ownerTypes = draft.ownerType === "agent";
  // The served project read is the only source of resource options; without it
  // the selects offer their empty option alone and the submit above is blocked.
  const optionsReadable = project !== null;
  return <Dialog.Root open onOpenChange={(open) => { if (!open) onClose(); }}>
    <Dialog.Portal>
      <Dialog.Overlay className="dialog-overlay" />
      <Dialog.Content className="command-dialog assignment-dialog" aria-describedby="assignment-dialog-description"
        onCloseAutoFocus={(event) => {
          const origin = opener.current;
          opener.current = null;
          restoreFocus(event, origin, fallbackFocus.current);
        }}>
        <div className="dialog-heading">
          <Dialog.Title>Assignment</Dialog.Title>
          <Dialog.Close asChild><button className="icon-button" type="button" aria-label="Close assignment dialog"><X size={20} aria-hidden="true" /></button></Dialog.Close>
        </div>
        <Dialog.Description id="assignment-dialog-description" className="muted">
          Assignment only. The provider keeps ownership of the title, description, priority and work state.
        </Dialog.Description>
        <form className="assignment-form" onSubmit={(event) => {
          event.preventDefault();
          if (!blocked && version !== null) save.mutate({ draft, version });
        }}>
          <label htmlFor="assignment-owner-type">Owner type</label>
          <select id="assignment-owner-type" name="assignee_type" value={draft.ownerType} disabled={save.isPending || stale}
            onChange={(event) => { const ownerType = event.currentTarget.value; setDraft((current) => ({ ...current, ownerType })); }}>
            {ownerTypeOptions.map((choice) => <option key={choice.value} value={choice.value}>{choice.label}</option>)}
          </select>

          <label htmlFor="assignment-owner">Owner</label>
          <input id="assignment-owner" name="assignee_name" value={draft.owner} placeholder={ownerPlaceholder}
            disabled={save.isPending || stale || draft.ownerType === "unassigned"}
            onChange={(event) => { const owner = event.currentTarget.value; setDraft((current) => ({ ...current, owner })); }} />

          <label htmlFor="assignment-agent-profile">Agent profile</label>
          <input id="assignment-agent-profile" name="agent_profile" value={draft.agentProfile} placeholder={projectAgentProfile ?? "Server default"}
            disabled={save.isPending || stale || !ownerTypes}
            onChange={(event) => { const agentProfile = event.currentTarget.value; setDraft((current) => ({ ...current, agentProfile })); }} />

          <label htmlFor="assignment-workspace">Workspace</label>
          <input id="assignment-workspace" name="workspace" value={draft.workspace} placeholder={projectWorkspace ?? "Server default"}
            disabled={save.isPending || stale || !ownerTypes}
            onChange={(event) => { const workspace = event.currentTarget.value; setDraft((current) => ({ ...current, workspace })); }} />

          <label htmlFor="assignment-repository">Repository</label>
          <select id="assignment-repository" name="repository_resource_id" value={draft.repository} disabled={save.isPending || stale}
            onChange={(event) => { const repository = event.currentTarget.value; setDraft((current) => ({ ...current, repository })); }}>
            <option value="">Not linked</option>
            {bindingOptions(repositoryResources, asText(item.repository_resource_id), "repository", optionsReadable)
              .map((option) => <option key={option.id} value={option.id}>{option.label}</option>)}
          </select>

          <label htmlFor="assignment-ci">CI resource</label>
          <select id="assignment-ci" name="ci_resource_id" value={draft.ci} disabled={save.isPending || stale}
            onChange={(event) => { const ci = event.currentTarget.value; setDraft((current) => ({ ...current, ci })); }}>
            <option value="">Use repository provider</option>
            {bindingOptions(ciResources, asText(item.ci_resource_id), "ci", optionsReadable)
              .map((option) => <option key={option.id} value={option.id}>{option.label}</option>)}
          </select>

          <p className="muted text-small">An empty Agent profile or Workspace field is filled from the project's defaults by the server. The exact reference and kind are re-checked server-side.</p>
          {intentLocked && <p className="notice" role="status">A live execution holds this item's intent, so assignment is locked until the execution settles.</p>}
          {blockedReason !== null && <p className="notice" role="status">{blockedReason}</p>}
          {refusal !== null && <p className="notice notice-error" role="alert">{stale
            ? `${refusalNotice(refusal, assignmentGuidance)} Close and reopen the editor to work from the re-read assignment.`
            : refusalNotice(refusal, assignmentGuidance)}</p>}
          <div className="dialog-actions">
            <button className="button" type="submit" disabled={blocked}>{save.isPending ? "Saving…" : "Save assignment"}</button>
            <button className="button" type="button" onClick={onClose} disabled={save.isPending}>Cancel</button>
          </div>
        </form>
      </Dialog.Content>
    </Dialog.Portal>
  </Dialog.Root>;
}
