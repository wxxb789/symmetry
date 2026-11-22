import { act, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { attentionReadKey, goalEventsReadKey } from "../../app/read-cache";
import { jsonResponse, portal } from "../../app/portal-double";
import { renderRoute } from "../../app/test-harness";
import { readGoalProjection } from "../../services/api.fixtures";

const projectId = "11111111-1111-4111-8111-111111111111";
const goalId = "33333333-3333-4333-8333-333333333333";

const projects = [{ id: projectId, key: "ONE", name: "Project One", description: null, status: "active", work_items: [] }];

const NOTICE_WAIT_MS = 5_000;
const FOLLOW_UP_WAIT_MS = 4_000;
const WAIT_BUDGETS = [NOTICE_WAIT_MS, FOLLOW_UP_WAIT_MS];

// The config case pays a cold `vite.config` module graph: the 2026-11-21 audit measured
// 8.2 s against vitest's 5 s default (30 s is 3.7x it).
const CONFIG_IMPORT_BUDGET_MS = 30_000;

afterEach(() => { localStorage.clear(); });

beforeEach(() => {
  portal.seed({ workspace: { selectedProjectId: projectId, projects } });
});

function servedGoal(version: number, title: string) {
  return jsonResponse(readGoalProjection({
    id: goalId,
    projectId,
    projectName: "Project One",
    title,
    state: "active",
    version,
    currentRevision: 1,
    allowedActions: ["pause"],
  }));
}

function pauseReceipt() {
  return jsonResponse({ goal_id: goalId, mutation_id: "m", event: { id: "m", sequence: 2, kind: "pause", revision: 1 } });
}

// A real-timer window inside `act`, for assertions about what did not change.
async function observeFor(ms: number) {
  const settled = Promise.withResolvers<void>();
  setTimeout(settled.resolve, ms);
  await act(async () => { await settled.promise; });
}

describe("goal acceptance settle", () => {
  it("announces acceptance only after the moved marker holds across two reads of the goal query", async () => {
    let commandApplied = false;
    let readsAfterCommand = 0;
    let movedReads = 0;
    portal.serve((request) => {
      const url = request.url;
      if (url.pathname === `/portal/api/goals/${goalId}`) {
        if (!commandApplied) return servedGoal(3, "Lagging goal");
        readsAfterCommand += 1;
        // The served projection lags the applied command: its first three reads after
        // the command still serve the pre-command marker, then the moved one.
        const moved = readsAfterCommand > 3;
        if (moved) movedReads += 1;
        return servedGoal(moved ? 4 : 3, "Lagging goal");
      }
      if (url.pathname === `/portal/api/goals/${goalId}/commands`) {
        commandApplied = true;
        return pauseReceipt();
      }
      if (url.pathname === `/portal/api/goals/${goalId}/events`) return jsonResponse({ entries: [], next_after: null });
    });

    const user = userEvent.setup();
    const { application } = renderRoute(`/portal/goals/${goalId}`);
    const invalidated = vi.spyOn(application.queryClient, "invalidateQueries");
    await screen.findByRole("heading", { level: 1, name: "Lagging goal" });

    const sentAt = Date.now();
    await user.click(screen.getByRole("button", { name: "Pause" }));
    await user.type(screen.getByLabelText("Reason"), "Operator pause");
    await user.click(screen.getByRole("button", { name: "Send Pause" }));
    expect(await screen.findByText("Pause accepted by the server.", {}, { timeout: NOTICE_WAIT_MS })).toBeVisible();
    expect(movedReads).toBe(2);
    expect(Date.now() - sentAt).toBeLessThan(2_000);
    expect(invalidated).toHaveBeenCalledWith({ queryKey: goalEventsReadKey(goalId) });
    expect(invalidated).toHaveBeenCalledWith({ queryKey: attentionReadKey() });

    // A cleared interval yields at most an in-flight read; a live 120 ms cadence yields ~3.
    const readsAtNotice = readsAfterCommand;
    await observeFor(400);
    expect(readsAfterCommand - readsAtNotice).toBeLessThan(3);
  });

  it("announces acceptance at the bound when the served marker never moves", async () => {
    let commandApplied = false;
    let readsAfterCommand = 0;
    portal.serve((request) => {
      const url = request.url;
      if (url.pathname === `/portal/api/goals/${goalId}`) {
        if (commandApplied) readsAfterCommand += 1;
        return servedGoal(3, "Unmoved goal");
      }
      if (url.pathname === `/portal/api/goals/${goalId}/commands`) {
        commandApplied = true;
        return pauseReceipt();
      }
      if (url.pathname === `/portal/api/goals/${goalId}/events`) return jsonResponse({ entries: [], next_after: null });
    });

    const user = userEvent.setup();
    renderRoute(`/portal/goals/${goalId}`);
    await screen.findByRole("heading", { level: 1, name: "Unmoved goal" });

    const sentAt = Date.now();
    await user.click(screen.getByRole("button", { name: "Pause" }));
    await user.type(screen.getByLabelText("Reason"), "Operator pause");
    await user.click(screen.getByRole("button", { name: "Send Pause" }));
    expect(await screen.findByText("Pause accepted by the server.", {}, { timeout: NOTICE_WAIT_MS })).toBeVisible();
    expect(Date.now() - sentAt).toBeGreaterThanOrEqual(2_400);
    expect(readsAfterCommand).toBeGreaterThan(3);

    // A cleared interval yields at most an in-flight read; a live 120 ms cadence yields ~3.
    const readsAtNotice = readsAfterCommand;
    await observeFor(400);
    expect(readsAfterCommand - readsAtNotice).toBeLessThan(3);
  });

  it("neither confirms nor aborts on a refused goal read and breaks the consecutive chain", async () => {
    let commandApplied = false;
    let readsAfterCommand = 0;
    let movedReads = 0;
    portal.serve((request) => {
      const url = request.url;
      if (url.pathname === `/portal/api/goals/${goalId}`) {
        if (!commandApplied) return servedGoal(3, "Refused-read goal");
        readsAfterCommand += 1;
        // Every second read after the command is refused, so a confirmation needs a
        // moved read after each refusal: reads 3, 5 and 6 of the sequence.
        if (readsAfterCommand === 2 || readsAfterCommand === 4) {
          return jsonResponse({ error: { code: "conflict", message: "the goal read was refused" } }, 409);
        }
        if (readsAfterCommand === 1) return servedGoal(3, "Refused-read goal");
        movedReads += 1;
        return servedGoal(4, "Refused-read goal");
      }
      if (url.pathname === `/portal/api/goals/${goalId}/commands`) {
        commandApplied = true;
        return pauseReceipt();
      }
      if (url.pathname === `/portal/api/goals/${goalId}/events`) return jsonResponse({ entries: [], next_after: null });
    });

    const user = userEvent.setup();
    renderRoute(`/portal/goals/${goalId}`);
    await screen.findByRole("heading", { level: 1, name: "Refused-read goal" });

    const sentAt = Date.now();
    await user.click(screen.getByRole("button", { name: "Pause" }));
    await user.type(screen.getByLabelText("Reason"), "Operator pause");
    await user.click(screen.getByRole("button", { name: "Send Pause" }));
    expect(await screen.findByText("Pause accepted by the server.", {}, { timeout: NOTICE_WAIT_MS })).toBeVisible();
    expect(movedReads).toBe(3);
    expect(Date.now() - sentAt).toBeLessThan(2_400);
  });

  it("clears the pending reads with the observer on unmount and still runs the follow-up invalidations", async () => {
    let commandApplied = false;
    let readsAfterCommand = 0;
    portal.serve((request) => {
      const url = request.url;
      if (url.pathname === `/portal/api/goals/${goalId}`) {
        if (commandApplied) readsAfterCommand += 1;
        return servedGoal(3, "Unmounted goal");
      }
      if (url.pathname === `/portal/api/goals/${goalId}/commands`) {
        commandApplied = true;
        return pauseReceipt();
      }
      if (url.pathname === `/portal/api/goals/${goalId}/events`) return jsonResponse({ entries: [], next_after: null });
    });

    const user = userEvent.setup();
    const { application, unmount } = renderRoute(`/portal/goals/${goalId}`);
    const invalidated = vi.spyOn(application.queryClient, "invalidateQueries");
    await screen.findByRole("heading", { level: 1, name: "Unmounted goal" });

    const sentAt = Date.now();
    await user.click(screen.getByRole("button", { name: "Pause" }));
    await user.type(screen.getByLabelText("Reason"), "Operator pause");
    await user.click(screen.getByRole("button", { name: "Send Pause" }));
    await waitFor(() => { expect(readsAfterCommand).toBeGreaterThan(2); });
    const readsAtUnmount = readsAfterCommand;
    unmount();

    await waitFor(() => {
      expect(invalidated).toHaveBeenCalledWith({ queryKey: goalEventsReadKey(goalId) });
      expect(invalidated).toHaveBeenCalledWith({ queryKey: attentionReadKey() });
    }, { timeout: FOLLOW_UP_WAIT_MS });
    expect(Date.now() - sentAt).toBeGreaterThanOrEqual(2_400);

    // A cleared interval yields at most an in-flight read; a live 120 ms cadence yields ~20.
    expect(readsAfterCommand - readsAtUnmount).toBeLessThan(3);
  });
});

describe("frontend test budget", () => {
  it("keeps every wait in this file strictly below the effective per-test budget", ({ task }) => {
    expect(task.timeout).toBe(15_000);
    for (const budget of WAIT_BUDGETS) expect(budget).toBeLessThan(task.timeout);
  });

  it("carries the explicit per-test budget in vitest.config", { timeout: CONFIG_IMPORT_BUDGET_MS }, async () => {
    // Loaded inside the case so the cold config module graph is charged to this case's
    // own budget: a filtered run can start here, and a static import would hoist it.
    const vitestConfig = (await import("../../../vitest.config")).default;
    expect(vitestConfig.test?.testTimeout).toBe(15_000);
  });
});
