/**
 * The one-time legacy hash repair.
 *
 * The classic workspace addressed its views through `/portal#board` (and
 * `/portal?project_id=…#chat`) locations, and the classic renderer is gone. A
 * deep link that still carries one of those locations is redirected once, at
 * bootstrap, to the new route that serves the same workflow. The mapping reads
 * only the hash and the search parameters the classic surface itself wrote, so a
 * shared link cannot name an arbitrary destination, and every destination is a
 * canonical route the server also serves — the browser performs one document
 * load, one history replacement, and no further redirect.
 */
export function legacyDestination(hash: string, search = ""): string | null {
  if (!hash || hash === "#") return null;
  const fragment = hash.slice(1);
  const params = new URLSearchParams(search);
  const projectId = params.get("project_id");
  const runId = params.get("run_id");
  const chatScope = params.get("chat_scope");
  const projectPath = projectId === null ? "/portal/projects" : `/portal/projects/${encodeURIComponent(projectId)}`;
  const scoped = (pathname: string): string => {
    if (projectId === null) return pathname;
    return `${pathname}?${new URLSearchParams({ projectId })}`;
  };

  if (fragment === "connections") {
    const connectionId = params.get("connectionId");
    return connectionId === null
      ? "/portal/connections"
      : `/portal/connections?${new URLSearchParams({ connectionId })}`;
  }

  if (fragment === "chat") {
    const query = new URLSearchParams();
    const scope =
      chatScope === "workspace" || chatScope === "project" || chatScope === "run"
        ? chatScope
        : runId !== null
          ? "run"
          : projectId !== null
            ? "project"
            : null;
    if (scope !== null) query.set("scope", scope);
    if (projectId !== null) query.set("projectId", projectId);
    if (runId !== null) query.set("runId", runId);
    return query.size === 0 ? "/portal/chat" : `/portal/chat?${query}`;
  }

  // The classic Resources view was the project-resource surface; the new route
  // serves resources beside their connections, scoped by the same project.
  if (fragment === "resources") return scoped("/portal/connections");

  // The classic board and its linked-runs list both addressed work of one
  // project: the board is the new destination, and a run is drill-down detail
  // reached from the work detail. No `/portal/activity` route exists in the
  // fixed design, so none is invented here.
  if (fragment === "board" || fragment === "activity") return projectPath;

  if (fragment === "health" || fragment === "attention") return scoped("/portal/attention");
  if (fragment === "projects") return projectPath;

  const project = /^(?:projects?[/=:])([^/?#]+)$/.exec(fragment)?.[1];
  if (project !== undefined) {
    try {
      return `/portal/projects/${encodeURIComponent(decodeURIComponent(project))}`;
    } catch {
      return "/portal/attention?unavailable=legacy";
    }
  }

  return `/portal/attention?${new URLSearchParams({ unavailable: fragment })}`;
}
