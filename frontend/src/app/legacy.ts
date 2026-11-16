export function legacyDestination(hash: string, search = ""): string | null {
  if (!hash || hash === "#") return null;
  const fragment = hash.slice(1);
  if (fragment === "connections") {
    const connectionId = new URLSearchParams(search).get("connectionId");
    return connectionId
      ? `/portal/connections?${new URLSearchParams({ connectionId })}`
      : "/portal/connections";
  }
  if (["chat", "board", "resources", "activity", "health"].includes(fragment)) {
    return `/portal/classic${search}#${fragment}`;
  }
  const projectId = new URLSearchParams(search).get("project_id");
  if (fragment === "attention") return projectId ? `/portal/attention?${new URLSearchParams({ projectId })}` : "/portal/attention";
  if (fragment === "projects") return projectId ? `/portal/projects/${encodeURIComponent(projectId)}` : "/portal/projects";
  const project = /^(?:projects?[/=:])([^/?#]+)$/.exec(fragment)?.[1];
  if (project) {
    try {
      return `/portal/projects/${encodeURIComponent(decodeURIComponent(project))}`;
    } catch {
      return "/portal/attention?unavailable=legacy";
    }
  }
  return `/portal/attention?${new URLSearchParams({ unavailable: fragment })}`;
}
