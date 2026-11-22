import { useQuery } from "@tanstack/react-query";
import { useMatch, useRouteContext } from "@tanstack/react-router";
import { createContext, useContext, type ReactNode } from "react";
import { workDetailOptions } from "./application";

const SelectedProjectContext = createContext<string | undefined>(undefined);

function useRouterSelectedProject() {
  const { application } = useRouteContext({ from: "__root__" });
  const projectId = useMatch({ from: "/projects/$projectId", shouldThrow: false, select: (match) => match.params.projectId });
  const workScope = useMatch({ from: "/work/$workItemId", shouldThrow: false, select: (match) => ({ workItemId: match.params.workItemId, claimedProjectId: match.search.projectId }) });
  // The served projection owns a work item's project: this reads the same query key the
  // work detail page reads, so no new request, and the URL claim only covers the gap.
  const servedWork = useQuery({ ...workDetailOptions(application.api, workScope?.workItemId ?? ""), enabled: workScope !== undefined });
  const searchProjectId = useMatch({ from: "/attention", shouldThrow: false, select: (match) => match.search.projectId });
  const chatProjectId = useMatch({ from: "/chat", shouldThrow: false, select: (match) => match.search.projectId });
  const connectionsProjectId = useMatch({ from: "/connections", shouldThrow: false, select: (match) => match.search.projectId });
  const runtimesProjectId = useMatch({ from: "/runtimes", shouldThrow: false, select: (match) => match.search.projectId });
  return projectId ?? servedWork.data?.work_item.project_id ?? workScope?.claimedProjectId ?? searchProjectId ?? chatProjectId ?? connectionsProjectId ?? runtimesProjectId;
}

export function SelectedProjectProvider({ children }: { children: ReactNode }) {
  const selectedProjectId = useRouterSelectedProject();
  return <SelectedProjectContext.Provider value={selectedProjectId}>{children}</SelectedProjectContext.Provider>;
}

export function useSelectedProject(): string | undefined {
  return useContext(SelectedProjectContext);
}
