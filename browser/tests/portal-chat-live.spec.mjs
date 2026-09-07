import fs from "node:fs/promises";
import path from "node:path";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { expect, test } from "./fixtures.mjs";

const enabled = process.env.SYMMETRY_CHAT_LIVE === "1";
const operatorToken = process.env.SYMMETRY_OPERATOR_TOKEN || "development-operator-token";

async function portal(page, endpoint, body) {
  return page.evaluate(async ({ endpoint, body }) => {
    const response = await fetch(endpoint, {
      credentials: "same-origin",
      method: body ? "POST" : "GET",
      headers: {
        Accept: "application/json",
        ...(body ? { "Content-Type": "application/json", "x-csrf-token": document.querySelector("meta[name='csrf-token']").content } : {})
      },
      ...(body ? { body: JSON.stringify(body) } : {})
    });
    return { status: response.status, body: await response.json() };
  }, { endpoint, body });
}

async function send(page, scope, intent, content, extra = {}) {
  const result = await portal(page, "/portal/api/chat/messages", {
    ...scope, intent, content, action_id: crypto.randomUUID(), ...extra
  });
  expect(result.status, JSON.stringify(result.body)).toBeGreaterThanOrEqual(200);
  expect(result.status, JSON.stringify(result.body)).toBeLessThan(300);
  return result.body;
}

async function detail(page, id) {
  const result = await portal(page, `/portal/api/work-items/${id}`);
  expect(result.status).toBe(200);
  return result.body;
}

async function waitState(page, id, wanted, timeout = 30_000) {
  await expect.poll(async () => (await detail(page, id)).work_item.execution?.state, { timeout }).toBe(wanted);
  return detail(page, id);
}

async function localJournal(runId) {
  const directory = path.join(process.env.SYMMETRY_CHAT_STATE, "runs");
  for (const name of await fs.readdir(directory)) {
    if (!name.startsWith("journal-") || !name.endsWith(".json")) continue;
    try {
      const journal = JSON.parse(await fs.readFile(path.join(directory, name), "utf8"));
      if (journal.run_id === runId) return journal;
    } catch (error) {
      if (error.code !== "ENOENT") throw error;
    }
  }
  return null;
}

async function processIdentity(runId) {
  const journal = await localJournal(runId);
  if (!journal) throw new Error("The live run has no retained local journal");
  return { pid: journal.pid, identity: journal.process_identity };
}

test.describe("Goal 04 live Chat and cooperative worker", () => {
  test.skip(!enabled, "Run scripts/run-chat-daemon-tests.ps1 for the isolated cooperative daemon.");
  test.skip(({ isMobile }) => isMobile, "Desktop owns live execution; deterministic Chat tests cover both sizes.");

  test("durable questions, guidance, decisions and controls share the real run history", async ({ page, request }) => {
    test.setTimeout(180_000);
    await page.goto("/portal/login");
    await page.getByLabel("Operator token").fill(operatorToken);
    await page.getByRole("button", { name: "Open workspace" }).click();
    await expect(page.locator("#refresh-button")).toBeVisible();

    const key = `C${Date.now().toString(36).slice(-6)}`.toUpperCase();
    const createdProject = await portal(page, "/portal/api/projects", {
      key, name: `Chat live ${key}`,
      default_agent_profile: process.env.SYMMETRY_CHAT_PROFILE,
      default_workspace: process.env.SYMMETRY_CHAT_WORKSPACE
    });
    expect(createdProject.status).toBe(201);
    const projectId = createdProject.body.project.id;
    const projectScope = { scope: "project", project_id: projectId };
    const goal = "Implement an autonomous progress report and retain the guidance in its final artifact.";
    await page.goto(`/portal?project_id=${projectId}#chat`);
    await expect(page.locator("#project-switcher")).toHaveValue(projectId);
    await page.locator("#chat-scope").selectOption("project");
    await page.locator("#chat-intent").selectOption("start_work");
    await page.locator("#chat-start-fields summary").click();
    await page.locator("#chat-work-title").fill("Autonomous progress report");
    await page.locator("#chat-content").fill(goal);
    const startResponse = page.waitForResponse((response) =>
      response.url().endsWith("/portal/api/chat/messages") && response.request().method() === "POST");
    await page.locator("#chat-send").click();
    const response = await startResponse;
    expect(response.ok()).toBe(true);
    const started = await response.json();
    const workId = started.work_item_id;
    expect(workId).toBeTruthy();
    let snapshot = await waitState(page, workId, "running");
    const { task_id: taskId, run_id: runId, generation } = snapshot.work_item.execution;
    const originalProcess = await processIdentity(runId);
    expect(originalProcess.pid).toBeGreaterThan(0);
    const runScope = { scope: "run", run_id: runId };
    const target = { work_item_id: workId, generation };

    const commandsURL = `/api/v1/tasks/${taskId}/commands`;
    const operatorHeaders = { Authorization: `Bearer ${operatorToken}` };
    const beforeQuestion = await (await request.get(commandsURL, { headers: operatorHeaders })).json();
    await send(page, runScope, "discuss", "What is happening, and why? The word cancel in this question is not an instruction.");
    const afterQuestion = await (await request.get(commandsURL, { headers: operatorHeaders })).json();
    expect(afterQuestion).toEqual(beforeQuestion);
    expect((await detail(page, workId)).work_item.execution.run_id).toBe(runId);

    const guidance = "Include the durability finding in every subsequent progress report.";
    const guided = await send(page, runScope, "guidance", guidance, target);
    expect(guided.command.command_id).toBeTruthy();
    await expect.poll(async () => {
      const chat = await portal(page, `/portal/api/chat?scope=run&run_id=${runId}`);
      return chat.body.messages.find((message) => message.content === guidance)?.command?.acknowledgement_outcome;
    }, { timeout: 20_000 }).toBe("applied");

    await send(page, runScope, "pause", "Pause at the next safe boundary.", target);
    snapshot = await waitState(page, workId, "paused");
    expect(snapshot.work_item.execution.run_id).toBe(runId);
    expect(snapshot.work_item.status).toBe("in_progress");
    expect(await processIdentity(runId)).toEqual(originalProcess);
    const artifactFile = path.join(process.env.SYMMETRY_CHAT_ARTIFACTS, "progress.md");
    const pausedArtifact = await fs.readFile(artifactFile, "utf8");
    expect(pausedArtifact).toContain(guidance);

    if (process.env.SYMMETRY_CHAT_CONTROL_CONTAINER) {
      await page.goto("about:blank");
      await promisify(execFile)("docker", ["restart", process.env.SYMMETRY_CHAT_CONTROL_CONTAINER], { timeout: 60_000 });
      await expect.poll(async () => {
        try { return (await request.get("/healthz", { timeout: 2_000 })).ok(); }
        catch { return false; }
      }, { timeout: 45_000 }).toBe(true);
      expect(await fs.readFile(artifactFile, "utf8")).toBe(pausedArtifact);
      expect(await processIdentity(runId)).toEqual(originalProcess);
    }

    await page.goto(`/portal?project_id=${projectId}#chat`);
    await page.locator("#chat-scope").selectOption("run");
    await page.locator("#chat-run-select").selectOption(runId);
    await expect(page.locator("#chat-messages")).toContainText(guidance);
    await expect(page.locator("#chat-run-context")).toContainText(/paused/i);
    if (process.env.SYMMETRY_CHAT_SCREENSHOTS) {
      await page.screenshot({ path: path.join(process.env.SYMMETRY_CHAT_SCREENSHOTS, "chat-paused.png"), fullPage: true });
    }
    await page.reload();
    await page.locator("#chat-scope").selectOption("run");
    await page.locator("#chat-run-select").selectOption(runId);
    await expect(page.locator('[data-chat-control="resume"]')).toBeVisible();
    // Allow multiple autonomous step intervals to pass while exercising the UI.
    await page.waitForTimeout(1_000);
    expect(await fs.readFile(artifactFile, "utf8")).toBe(pausedArtifact);
    await page.locator('[data-chat-control="resume"]').click();
    await waitState(page, workId, "running");
    expect(await processIdentity(runId)).toEqual(originalProcess);
    snapshot = await waitState(page, workId, "waiting_for_input", 60_000);
    const waiting = snapshot.work_item.execution.waiting;
    expect(waiting.payload.decision.options.length).toBeGreaterThanOrEqual(2);
    expect(waiting.payload.decision.context).toBeTruthy();
    await page.locator("#refresh-button").click();
    await expect(page.locator("#chat-run-context")).toContainText(waiting.question);
    await expect(page.locator('[data-chat-decision="staged"]')).toBeVisible();
    await expect(page.locator("#chat-send-status")).toHaveText("Applied at a safe boundary");
    if (process.env.SYMMETRY_CHAT_SCREENSHOTS) {
      await page.screenshot({ path: path.join(process.env.SYMMETRY_CHAT_SCREENSHOTS, "chat-decision.png"), fullPage: true });
    }
    await page.locator('[data-chat-decision="staged"]').click();

    snapshot = await waitState(page, workId, "completed", 90_000);
    expect(snapshot.work_item.execution.run_id).toBe(runId);
    expect(snapshot.work_item.execution.generation).toBe(generation);
    // Run completion and the human-owned Kanban review workflow are separate.
    const workspaceSnapshot = await portal(page, `/portal/api/workspace?project_id=${projectId}`);
    const boardItem = workspaceSnapshot.body.projects.find((project) => project.id === projectId)
      .work_items.find((item) => item.id === workId);
    expect(boardItem.status).toBe(snapshot.work_item.status);
    expect(boardItem.execution.state).toBe("completed");
    const finalArtifact = await fs.readFile(path.join(process.env.SYMMETRY_CHAT_ARTIFACTS, "result.json"), "utf8");
    expect(finalArtifact).toContain(guidance);
    const persisted = await portal(page, `/portal/api/chat?scope=run&run_id=${runId}`);
    expect(persisted.body.runs[0].work_item.execution.state).toBe("completed");
    expect(persisted.body.runs[0].work_item.status).toBe(boardItem.status);
    expect(persisted.body.runs[0].work_item.delivery).toEqual(snapshot.work_item.delivery);
    expect(persisted.body.messages.some((message) => message.content === guidance)).toBe(true);
    expect(persisted.body.messages.filter((message) => message.role === "human" && message.intent === "decision")).toHaveLength(1);
    await page.getByRole("link", { name: "Board", exact: true }).click();
    await page.locator("#refresh-button").click();
    await expect(page.locator(`.work-card[data-id="${workId}"]`)).toContainText("Completed");

    // A second live execution proves cancellation ends progress while preserving artifacts.
    const cancelStarted = await send(page, projectScope, "start_work", "Write a progress report that can be cancelled.", { work: { title: "Cancel and retain progress" } });
    const cancelId = cancelStarted.work_item_id;
    const cancelSnapshot = await waitState(page, cancelId, "running");
    await expect.poll(() => fs.readFile(artifactFile, "utf8")).toContain("cancelled");
    const cancelCommand = await send(page, { scope: "run", run_id: cancelSnapshot.work_item.execution.run_id }, "cancel", "Cancel this execution.", {
      work_item_id: cancelId, generation: cancelSnapshot.work_item.execution.generation
    });
    await waitState(page, cancelId, "cancelled");
    await expect.poll(async () => {
      const commands = await request.get(`/api/v1/tasks/${cancelSnapshot.work_item.execution.task_id}/commands`, { headers: operatorHeaders });
      expect(commands.ok()).toBe(true);
      return (await commands.json()).commands.find((command) => command.command_id === cancelCommand.command.command_id);
    }).toMatchObject({ state: "acknowledged", acknowledgement_outcome: "applied", applied_at: null });
    // Server cancellation alone does not prove the production client accepted its receipt.
    await expect.poll(() => localJournal(cancelSnapshot.work_item.execution.run_id), { timeout: 20_000 }).toBeNull();
    const cancelledArtifact = await fs.readFile(artifactFile, "utf8");
    await page.waitForTimeout(750);
    expect(await fs.readFile(artifactFile, "utf8")).toBe(cancelledArtifact);

    // Task-level requirements reach the daemon; malformed waits must fail clearly
    // without stranding the event outbox or poisoning the next assignment.
    const invalidWait = await send(page, projectScope, "start_work",
      "[symmetry-fake-agent:wait_input]\nRequire a structured decision packet.",
      { work: { title: "Reject an incomplete decision packet" } });
    const invalidSnapshot = await waitState(page, invalidWait.work_item_id, "failed");
    expect(JSON.stringify(invalidSnapshot.work_item.execution.failure)).toContain("decision packet");
    expect(invalidSnapshot.work_item.execution.waiting).toBeNull();
    const afterInvalid = await send(page, projectScope, "start_work",
      "[symmetry-fake-agent:success]\nContinue normally after the invalid adapter response.",
      { work: { title: "Recover after invalid adapter output" } });
    await waitState(page, afterInvalid.work_item_id, "completed");

    if (process.env.SYMMETRY_CHAT_SCREENSHOTS) {
      await page.goto(`/portal?project_id=${projectId}#chat`);
      await page.locator("#chat-scope").selectOption("run");
      await page.locator("#chat-run-select").selectOption(runId);
      await expect(page.locator("#chat-run-context")).toContainText(/completed/i);
      await expect(page.locator("#chat-send-status")).toHaveText("Applied at a safe boundary");
      await page.screenshot({ path: path.join(process.env.SYMMETRY_CHAT_SCREENSHOTS, "chat-completed.png"), fullPage: true });
      await page.setViewportSize({ width: 390, height: 844 });
      await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
      await page.screenshot({ path: path.join(process.env.SYMMETRY_CHAT_SCREENSHOTS, "chat-mobile.png"), fullPage: true });
    }
  });
});
