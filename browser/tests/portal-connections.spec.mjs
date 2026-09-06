import { allowBrowserError, expect, test } from "./fixtures.mjs";

const operatorToken = process.env.SYMMETRY_OPERATOR_TOKEN || "development-operator-token";

async function login(page) {
  await page.goto("/portal/login");
  await page.getByLabel("Operator token").fill(operatorToken);
  await Promise.all([
    page.waitForURL(/\/portal(?:\?|#|$)/),
    page.getByRole("button", { name: "Open workspace" }).click()
  ]);
}

test("a connected provider can be inspected, bound, synchronized, and assigned", async ({ page }) => {
  const project = {
    id: "10000000-0000-0000-0000-000000000001",
    key: "EXT",
    name: "Connected engineering",
    description: "",
    status: "active",
    version: 1,
    default_agent_profile: "codex",
    default_workspace: "primary",
    resources: [],
    work_items: []
  };
  const workspace = {
    selected_project_id: project.id,
    projects: [project],
    connections: [],
    runtimes: [],
    registered_runtimes: [],
    activity: [],
    health: {
      connections: "unknown",
      runtimes: "offline",
      executions: "idle",
      synchronization: "unknown"
    }
  };

  await page.route("**/portal/api/**", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const payload = request.postDataJSON?.() || {};

    if (request.method() === "GET" && url.pathname === "/portal/api/workspace") {
      await route.fulfill({ status: 200, json: workspace });
      return;
    }

    if (request.method() === "POST" && url.pathname === "/portal/api/connections") {
      expect(payload.auth_type).toBe("gh_cli");
      expect(payload).not.toHaveProperty("credential");
      expect(payload.capabilities).toEqual(["repositories", "work_items", "changes", "ci"]);
      const connection = {
        id: "20000000-0000-0000-0000-000000000001",
        provider: "github",
        name: payload.name,
        account_ref: payload.account_ref,
        auth_type: payload.auth_type,
        capabilities: payload.capabilities,
        status: "unknown",
        status_message: null,
        metadata: {},
        last_checked_at: null,
        version: 1
      };
      workspace.connections = [connection];
      await route.fulfill({ status: 201, json: { connection } });
      return;
    }

    if (request.method() === "POST" && url.pathname.endsWith("/check")) {
      const connection = {
        ...workspace.connections[0],
        status: "healthy",
        last_checked_at: "2026-09-05T10:00:00Z",
        version: 2
      };
      workspace.connections = [connection];
      workspace.health.connections = "healthy";
      await route.fulfill({ status: 200, json: { connection } });
      return;
    }

    if (request.method() === "POST" && url.pathname.endsWith("/resources")) {
      expect(payload.connection_id).toBe(workspace.connections[0].id);
      const resource = {
        id: payload.kind === "repository"
          ? "30000000-0000-0000-0000-000000000001"
          : "30000000-0000-0000-0000-000000000002",
        project_id: project.id,
        connection_id: workspace.connections[0].id,
        kind: payload.kind,
        name: payload.name,
        provider: "github",
        external_ref: payload.external_ref,
        url: null,
        status: "unknown",
        sync_status: "unknown",
        status_message: null,
        metadata: {},
        last_checked_at: null,
        last_synced_at: null,
        version: 1
      };
      project.resources.push(resource);
      await route.fulfill({ status: 201, json: { resource } });
      return;
    }

    if (request.method() === "POST" && url.pathname.endsWith("/sync")) {
      const resourceId = url.pathname.split("/").at(-2);
      const index = project.resources.findIndex((resource) => resource.id === resourceId);
      project.resources[index] = {
        ...project.resources[index],
        url: "https://github.com/acme/symmetry/issues",
        status: "healthy",
        sync_status: "synced",
        last_checked_at: "2026-09-05T10:05:00Z",
        last_synced_at: "2026-09-05T10:05:00Z",
        version: 2
      };
      if (project.resources[index].kind === "work_tracking") {
        project.work_items = [externalWorkItem(project, project.resources[index])];
      } else if (project.resources[index].kind === "repository" && project.work_items[0]) {
        const item = project.work_items[0];
        item.pull_request_url = "https://github.com/acme/symmetry/pull/42";
        item.ci_status = "passed";
        item.review_status = "approved";
        item.delivery = {
          pull_request: {
            value: item.pull_request_url,
            url: item.pull_request_url,
            status: "open",
            source: "provider",
            provider: "github",
            generation: null
          },
          ci: {
            value: "passed",
            url: null,
            status: "passed",
            source: "provider",
            provider: "github",
            generation: null
          },
          review: {
            value: "approved",
            url: null,
            status: "approved",
            source: "provider",
            provider: "github",
            generation: null
          }
        };
      }
      workspace.health.synchronization = "synced";
      await route.fulfill({ status: 200, json: { resource: project.resources[index] } });
      return;
    }

    if (request.method() === "PATCH" && url.pathname.includes("/work-items/")) {
      const item = project.work_items[0];
      Object.assign(item, {
        repository_resource_id: payload.repository_resource_id || null,
        repository: project.resources.find((resource) => resource.id === payload.repository_resource_id)?.external_ref || null,
        ci_resource_id: payload.ci_resource_id || null,
        ci_resource: null,
        assignee: {
          type: payload.assignee_type,
          name: payload.assignee_name || null,
          agent_profile: payload.agent_profile || null
        },
        workspace: payload.workspace || null,
        branch: payload.branch || null,
        pull_request_url: payload.pull_request_url || null,
        can_start: payload.assignee_type === "agent",
        version: item.version + 1
      });
      await route.fulfill({ status: 200, json: { work_item: item } });
      return;
    }

    if (request.method() === "POST" && url.pathname.endsWith("/run")) {
      const item = project.work_items[0];
      item.can_start = false;
      item.execution = {
        state: "queued",
        generation: 1,
        can_cancel: true,
        can_retry: false,
        intent_locked: true,
        waiting: null
      };
      await route.fulfill({ status: 202, json: { work_item: item, execution: item.execution } });
      return;
    }

    if (request.method() === "GET" && url.pathname.includes("/work-items/")) {
      const item = project.work_items[0];
      await route.fulfill({
        status: 200,
        json: {
          work_item: item,
          outcome: {
            phase: "not_started",
            summary: null,
            owner: item.assignee,
            blocker: null,
            findings: [],
            changed_artifacts: [],
            tests: [],
            result: null,
            failure: null
          },
          timeline: [],
          raw: { task: null, timeline: [], next_before: null }
        }
      });
      return;
    }

    await route.fulfill({ status: 404, json: { error: { code: "not_found", message: "not found" } } });
  });

  await login(page);
  await page.getByRole("link", { name: "Connections" }).click();
  await page.getByRole("button", { name: "New connection" }).click();

  const connectionDialog = page.locator("#connection-dialog");
  await expect(page.getByRole("dialog", { name: "New connection" })).toBeVisible();
  await connectionDialog.getByLabel("Provider").selectOption("github");
  await connectionDialog.getByLabel("Name").fill("GitHub engineering");
  await connectionDialog.getByLabel("Account or organization").fill("acme");
  const repositoriesCapability = connectionDialog.getByLabel("Repositories");
  const changesCapability = connectionDialog.getByLabel("Changes");

  await changesCapability.check();
  await expect(repositoriesCapability).toBeChecked();
  await repositoriesCapability.uncheck();
  await expect(changesCapability).not.toBeChecked();
  await changesCapability.check();
  await expect(repositoriesCapability).toBeChecked();
  await connectionDialog.getByLabel("Work items").check();
  await connectionDialog.getByLabel("CI").check();
  await connectionDialog.getByRole("button", { name: "Connect" }).click();

  await expect(page.locator("#connection-table")).toContainText("GitHub engineering");
  await expect(page.locator("#connection-table")).toContainText("GitHub CLI · HTTPS");
  await expect(connectionDialog.getByLabel("Credential")).toHaveCount(0);

  workspace.connections[0].capabilities = ["changes"];
  await page.reload();
  await page.getByRole("link", { name: "Connections" }).click();
  await page.locator(".connection-primary").click();
  await expect(page.getByRole("dialog", { name: "Connection settings" })).toBeVisible();
  await expect(connectionDialog.getByLabel("Changes")).toBeChecked();
  await expect(connectionDialog.getByLabel("Repositories")).toBeChecked();
  await connectionDialog.getByLabel("Repositories").uncheck();
  await expect(connectionDialog.getByLabel("Changes")).not.toBeChecked();
  await connectionDialog.getByLabel("Changes").check();
  await expect(connectionDialog.getByLabel("Repositories")).toBeChecked();
  const currentConnection = workspace.connections[0];
  workspace.connections = [];
  const removedConnectionRefresh = page.waitForResponse((response) =>
    response.request().method() === "GET" && new URL(response.url()).pathname === "/portal/api/workspace"
  );
  await page.evaluate(() => document.dispatchEvent(new Event("visibilitychange")));
  await removedConnectionRefresh;
  await expect(connectionDialog.getByRole("button", { name: "Save" })).toBeDisabled();
  await expect(connectionDialog.locator("[data-form-error]")).toContainText("changed while the editor was open");
  await connectionDialog.getByRole("button", { name: "Cancel" }).click();
  workspace.connections = [currentConnection];
  await page.getByRole("button", { name: "Refresh workspace" }).click();
  workspace.connections[0].capabilities = ["repositories", "work_items", "changes", "ci"];

  await page.getByRole("button", { name: "Check GitHub engineering" }).click();
  await expect(page.locator("#connection-table")).toContainText("Healthy");

  await page.getByRole("link", { name: "Resources" }).click();
  await page.getByRole("button", { name: "Attach resource" }).click();
  const resourceDialog = page.locator("#resource-dialog");
  await resourceDialog.getByLabel("Type").selectOption("repository");
  await resourceDialog.getByLabel("Provider connection").selectOption(workspace.connections[0].id);
  await resourceDialog.getByLabel("Name").fill("symmetry");
  await resourceDialog.getByLabel("Reference", { exact: true }).fill("acme/symmetry");
  await resourceDialog.getByRole("button", { name: "Attach" }).click();

  await page.getByRole("button", { name: "Attach resource" }).click();
  await resourceDialog.getByLabel("Type").selectOption("work_tracking");
  await resourceDialog.getByLabel("Provider connection").selectOption(workspace.connections[0].id);
  await resourceDialog.getByLabel("Name").fill("GitHub Issues");
  await resourceDialog.getByLabel("Reference", { exact: true }).fill("acme/symmetry");
  await resourceDialog.getByRole("button", { name: "Attach" }).click();

  await page.getByRole("button", { name: "Sync GitHub Issues" }).click();
  await expect(page.locator("#resource-table")).toContainText("Synced");

  await page.getByRole("link", { name: "Board" }).click();
  const card = page.locator(".work-card", { hasText: "External work stays authoritative" });
  await expect(card).toContainText("GitHub #101");
  await card.click();
  await expect(page.locator("#detail-content")).toContainText("Ada Lovelace");
  await expect(page.locator("#detail-priority-select")).toBeDisabled();
  await page.getByRole("button", { name: "Edit" }).click();
  const workItemDialog = page.locator("#work-item-dialog");
  await expect(workItemDialog.getByLabel("Title")).toBeDisabled();
  await expect(workItemDialog.getByLabel("Description")).toBeDisabled();
  await expect(workItemDialog.getByLabel("Priority")).toBeDisabled();
  await expect(workItemDialog.getByLabel("Owner type")).toBeEnabled();
  await workItemDialog.getByLabel("Owner type").selectOption("agent");
  await workItemDialog.getByLabel("Owner", { exact: true }).fill("Codex");
  await workItemDialog.getByLabel("Agent profile").fill("codex");
  await workItemDialog.getByLabel("Workspace").fill("primary");
  await workItemDialog.locator('select[name="repository_resource_id"]').selectOption({ label: "symmetry" });
  await workItemDialog.getByLabel("Branch").fill("codex/external-101");
  await workItemDialog.getByLabel("Pull request URL").fill("https://github.com/acme/symmetry/pull/42");
  await workItemDialog.getByRole("button", { name: "Save" }).click();

  await page.getByRole("button", { name: "Close details" }).click();
  await page.getByRole("link", { name: "Resources" }).click();
  await page.getByRole("button", { name: "Sync symmetry" }).click();
  await page.getByRole("link", { name: "Board" }).click();
  await card.click();
  await expect(page.locator("#detail-content")).toContainText("Open PR");
  await expect(page.locator("#detail-content")).toContainText("Open");
  await expect(page.locator("#detail-content")).toContainText("Passed");
  await expect(page.locator("#detail-content")).toContainText("Approved");
  await expect(page.locator("#detail-content .source-label")).toHaveText(["GitHub", "GitHub", "GitHub"]);

  project.work_items[0].external.available = false;
  project.work_items[0].can_start = false;
  await page.reload();
  await page.getByRole("link", { name: "Board" }).click();
  await card.click();
  await expect(page.locator("#detail-content")).toContainText("Unavailable in provider");
  await expect(page.getByRole("button", { name: "Start run" })).toHaveCount(0);

  project.work_items[0].external.available = true;
  project.work_items[0].can_start = true;
  await page.reload();
  await page.getByRole("link", { name: "Board" }).click();
  await card.click();
  await expect(page.getByRole("button", { name: "Start run" })).toBeVisible();
  await page.getByRole("button", { name: "Start run" }).click();
  await expect(page.locator("#toast")).toContainText("Agent run queued");

  const dimensions = await page.evaluate(() => ({
    clientWidth: document.documentElement.clientWidth,
    scrollWidth: document.documentElement.scrollWidth
  }));
  expect(dimensions.scrollWidth).toBeLessThanOrEqual(dimensions.clientWidth);
});

test("form generations and connection deletion remain single-flight", async ({ page }) => {
  const project = {
    id: "10000000-0000-0000-0000-000000000001",
    key: "LOCK",
    name: "Lock testing",
    description: "",
    status: "active",
    version: 1,
    default_agent_profile: "codex",
    default_workspace: "primary",
    resources: [],
    work_items: []
  };
  const workspace = {
    selected_project_id: project.id,
    projects: [project],
    connections: [],
    runtimes: [],
    registered_runtimes: [],
    activity: [],
    health: { connections: "unknown", runtimes: "offline", executions: "idle", synchronization: "unknown" }
  };
  const createGates = [deferred(), deferred()];
  const updateGate = deferred();
  const deleteGate = deferred();
  const calls = { create: 0, update: 0, delete: 0, check: 0 };

  await page.route("**/portal/api/**", async (route) => {
    const request = route.request();
    const url = new URL(request.url());

    if (request.method() === "GET" && url.pathname === "/portal/api/workspace") {
      await route.fulfill({ status: 200, json: workspace });
      return;
    }

    if (request.method() === "POST" && url.pathname === "/portal/api/connections") {
      const index = calls.create;
      calls.create += 1;
      const payload = request.postDataJSON();
      await createGates[index].promise;
      const connection = {
        id: `20000000-0000-0000-0000-00000000000${index + 1}`,
        provider: "github",
        name: payload.name,
        account_ref: payload.account_ref,
        auth_type: "gh_cli",
        capabilities: payload.capabilities,
        status: "unknown",
        status_message: null,
        metadata: {},
        last_checked_at: null,
        version: 1
      };
      workspace.connections.push(connection);
      await route.fulfill({ status: 201, json: { connection } });
      return;
    }

    if (request.method() === "PATCH" && url.pathname.includes("/connections/")) {
      calls.update += 1;
      const connection = workspace.connections.find((candidate) => url.pathname.endsWith(candidate.id));
      const payload = request.postDataJSON();
      await updateGate.promise;
      Object.assign(connection, { name: payload.name, version: connection.version + 1 });
      await route.fulfill({ status: 200, json: { connection } });
      return;
    }

    if (request.method() === "POST" && url.pathname.endsWith("/check")) {
      calls.check += 1;
      await route.fulfill({ status: 200, json: { connection: workspace.connections[0] } });
      return;
    }

    if (request.method() === "DELETE" && url.pathname.includes("/connections/")) {
      calls.delete += 1;
      const connectionId = url.pathname.split("/").at(-1);
      await deleteGate.promise;
      workspace.connections = workspace.connections.filter((connection) => connection.id !== connectionId);
      await route.fulfill({ status: 204, body: "" });
      return;
    }

    await route.fulfill({ status: 404, json: { error: { code: "not_found", message: "not found" } } });
  });

  await login(page);
  await page.getByRole("link", { name: "Connections" }).click();
  const dialog = page.locator("#connection-dialog");

  await page.getByRole("button", { name: "New connection" }).click();
  await dialog.getByLabel("Name").fill("First connection");
  await dialog.getByLabel("Account or organization").fill("first");
  await dialog.getByLabel("Repositories").check();
  await dialog.getByRole("button", { name: "Connect" }).click();
  await expect.poll(() => calls.create).toBe(1);
  await dialog.getByRole("button", { name: "Close" }).click();

  await page.getByRole("button", { name: "New connection" }).click();
  await dialog.getByLabel("Name").fill("Second connection");
  await dialog.getByLabel("Account or organization").fill("second");
  await dialog.getByLabel("Repositories").check();
  await dialog.getByRole("button", { name: "Connect" }).click();
  await expect.poll(() => calls.create).toBe(2);
  const secondSubmit = dialog.getByRole("button", { name: "Connect" });
  await expect(secondSubmit).toBeDisabled();

  const firstCreateCompleted = page.waitForResponse((response) =>
    response.request().method() === "POST" && new URL(response.url()).pathname === "/portal/api/connections"
  );
  createGates[0].resolve();
  await firstCreateCompleted;
  await expect(secondSubmit).toBeDisabled();
  await dialog.locator("form").evaluate((form) => form.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true })));
  expect(calls.create).toBe(2);

  createGates[1].resolve();
  await expect(dialog).not.toHaveJSProperty("open", true);
  const secondConnection = workspace.connections.find((connection) => connection.name === "Second connection");
  const secondConnectionRow = page.locator(`[data-connection-id="${secondConnection.id}"]`);
  const secondConnectionCheck = page.locator(`[data-check-connection-id="${secondConnection.id}"]`);
  await secondConnectionRow.click();
  await dialog.getByLabel("Name").fill("Updated connection");
  await dialog.getByRole("button", { name: "Save" }).click();
  await expect.poll(() => calls.update).toBe(1);
  await expect(dialog.getByRole("button", { name: "Save" })).toBeDisabled();
  await expect(dialog.getByRole("button", { name: "Delete" })).toBeDisabled();
  await expect(secondConnectionCheck).toBeDisabled();
  await dialog.locator("form").evaluate((form) => form.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true })));
  await dialog.getByRole("button", { name: "Delete" }).evaluate((button) => button.dispatchEvent(new MouseEvent("click", { bubbles: true })));
  await secondConnectionCheck.evaluate((button) => button.dispatchEvent(new MouseEvent("click", { bubbles: true })));
  expect(calls).toMatchObject({ update: 1, delete: 0, check: 0 });
  updateGate.resolve();
  await expect(dialog).not.toHaveJSProperty("open", true);
  await expect(secondConnectionRow).toContainText("Updated connection");

  await secondConnectionRow.click();
  await dialog.getByRole("button", { name: "Delete" }).click();
  await expect(dialog.getByLabel("Name")).toBeDisabled();
  await expect(dialog.getByRole("button", { name: "Save" })).toBeDisabled();
  await expect(dialog.getByRole("button", { name: "Delete" })).toBeDisabled();
  await dialog.getByRole("button", { name: "Delete" }).evaluate((button) => button.dispatchEvent(new MouseEvent("click", { bubbles: true })));
  await expect(page.locator("#confirm-dialog")).toHaveJSProperty("open", true);
  await page.locator("#confirm-action-button").click();
  await expect.poll(() => calls.delete).toBe(1);
  await dialog.getByRole("button", { name: "Delete" }).evaluate((button) => button.dispatchEvent(new MouseEvent("click", { bubbles: true })));
  expect(calls.delete).toBe(1);
  await expect(page.locator("#confirm-dialog")).not.toHaveJSProperty("open", true);
  deleteGate.resolve();
  await expect(dialog).not.toHaveJSProperty("open", true);
  await expect(page.locator(`[data-connection-id="${secondConnection.id}"]`)).toHaveCount(0);
});

test("provider operations remain single-flight across rerenders and respect dialog ownership", async ({ page }) => {
  const connection = {
    id: "20000000-0000-0000-0000-000000000001",
    provider: "github",
    name: "GitHub engineering",
    account_ref: "acme",
    auth_type: "gh_cli",
    capabilities: ["repositories", "work_items", "changes", "ci"],
    status: "healthy",
    status_message: null,
    metadata: {},
    last_checked_at: null,
    version: 1
  };
  const resources = [
    connectedResource("30000000-0000-0000-0000-000000000001", "symmetry", "repository", connection.id),
    connectedResource("30000000-0000-0000-0000-000000000002", "GitHub Issues", "work_tracking", connection.id)
  ];
  const project = {
    id: "10000000-0000-0000-0000-000000000001",
    key: "EXT",
    name: "Connected engineering",
    description: "",
    status: "active",
    version: 1,
    default_agent_profile: "codex",
    default_workspace: "primary",
    resources,
    work_items: []
  };
  const otherProject = {
    ...project,
    id: "10000000-0000-0000-0000-000000000002",
    key: "TWO",
    name: "Second project",
    resources: [],
    work_items: []
  };
  const workspace = {
    selected_project_id: project.id,
    projects: [project, otherProject],
    connections: [connection],
    runtimes: [],
    registered_runtimes: [],
    activity: [],
    health: { connections: "healthy", runtimes: "offline", executions: "idle", synchronization: "unknown" }
  };
  let checkGate = deferred();
  const resourceGates = [deferred(), deferred()];
  let projectGate = deferred();
  let projectFailure = null;
  let workspaceFailure = null;
  let delayedWorkspaceFailure = null;
  const calls = { check: 0, resource: 0, project: 0 };

  await page.route("**/portal/api/**", async (route) => {
    const request = route.request();
    const url = new URL(request.url());

    if (request.method() === "GET" && url.pathname === "/portal/api/workspace") {
      if (delayedWorkspaceFailure?.armed) {
        const failure = delayedWorkspaceFailure;
        delayedWorkspaceFailure = null;
        failure.started.resolve();
        await failure.release.promise;
        await route.fulfill({
          status: 503,
          json: { error: { code: "provider_failure", message: "Old project refresh failed" } }
        });
        return;
      }
      if (workspaceFailure) {
        const failure = workspaceFailure;
        workspaceFailure = null;
        await route.fulfill(failure);
        return;
      }
      await route.fulfill({
        status: 200,
        json: { ...workspace, selected_project_id: url.searchParams.get("project_id") || workspace.selected_project_id }
      });
      return;
    }

    if (request.method() === "POST" && url.pathname.endsWith("/check")) {
      calls.check += 1;
      await checkGate.promise;
      await route.fulfill({ status: 200, json: { connection } });
      return;
    }

    if (request.method() === "POST" && url.pathname.includes("/resources/") && url.pathname.endsWith("/sync")) {
      const gate = resourceGates[calls.resource];
      calls.resource += 1;
      await gate.promise;
      await route.fulfill({ status: 200, json: { resource: resources[0] } });
      return;
    }

    if (request.method() === "POST" && url.pathname.includes("/projects/") && url.pathname.endsWith("/sync")) {
      calls.project += 1;
      if (projectFailure) {
        await route.fulfill({ status: 422, json: projectFailure });
        return;
      }
      await projectGate.promise;
      if (delayedWorkspaceFailure) delayedWorkspaceFailure.armed = true;
      await route.fulfill({ status: 200, json: { resources } });
      return;
    }

    await route.fulfill({ status: 404, json: { error: { code: "not_found", message: "not found" } } });
  });

  await login(page);
  await page.getByRole("link", { name: "Connections" }).click();
  await page.getByRole("button", { name: "Check GitHub engineering" }).click();
  await expect.poll(() => calls.check).toBe(1);
  const connectionRow = page.locator(`[data-connection-id="${connection.id}"]`);
  await expect(connectionRow).toBeDisabled();
  await connectionRow.evaluate((button) => button.dispatchEvent(new MouseEvent("click", { bubbles: true })));
  await expect(page.locator("#connection-dialog")).not.toHaveJSProperty("open", true);
  await page.getByRole("button", { name: "Refresh workspace" }).click();
  const checkButton = page.getByRole("button", { name: "Check GitHub engineering" });
  await expect(checkButton).toBeDisabled();
  await checkButton.evaluate((button) => button.dispatchEvent(new MouseEvent("click", { bubbles: true })));
  expect(calls.check).toBe(1);
  checkGate.resolve();
  await expect(checkButton).toBeEnabled();
  const connectionRefresh = page.waitForResponse((response) =>
    response.request().method() === "GET" && new URL(response.url()).pathname === "/portal/api/workspace"
  );
  await connectionRow.focus();
  await page.evaluate(() => document.dispatchEvent(new Event("visibilitychange")));
  await connectionRefresh;
  await expect(connectionRow).toBeFocused();

  allowBrowserError(page, /status of 503/);
  workspaceFailure = {
    status: 503,
    json: { error: { code: "provider_failure", message: "Workspace refresh failed" } }
  };
  await checkButton.click();
  await expect(page.locator("#toast")).toContainText("Connection checked, but refreshing the workspace failed: Workspace refresh failed");
  await expect(page.locator("#toast")).toContainText("The change was saved");
  await expect(checkButton).toBeEnabled();
  await expect(connectionRow).toBeDisabled();
  await page.getByRole("button", { name: "Refresh workspace" }).click();
  await expect(connectionRow).toBeEnabled();

  checkGate = deferred();
  await checkButton.click();
  await expect.poll(() => calls.check).toBe(3);
  const checkedWorkspace = page.waitForResponse((response) =>
    response.request().method() === "GET" && new URL(response.url()).pathname === "/portal/api/workspace"
  );
  checkGate.resolve();
  await checkedWorkspace;
  await expect(checkButton).toBeFocused();

  await page.getByRole("link", { name: "Resources" }).click();
  await page.evaluate(() => {
    window.hiddenConnectionMutations = 0;
    window.hiddenConnectionObserver = new MutationObserver((mutations) => {
      window.hiddenConnectionMutations += mutations.length;
    });
    window.hiddenConnectionObserver.observe(document.querySelector("#connection-table"), {
      attributes: true,
      characterData: true,
      childList: true,
      subtree: true
    });
  });
  connection.status = "degraded";
  workspace.health.connections = "degraded";
  await page.getByRole("button", { name: "Refresh workspace" }).click();
  await expect(page.locator("#health-grid")).toContainText("Degraded");
  expect(await page.evaluate(() => window.hiddenConnectionMutations)).toBe(0);
  await page.getByRole("link", { name: "Connections" }).click();
  await expect(page.locator("#connection-table")).toContainText("Degraded");
  await page.evaluate(() => window.hiddenConnectionObserver.disconnect());
  await page.getByRole("link", { name: "Resources" }).click();

  await page.getByRole("button", { name: "Sync symmetry" }).click();
  await expect.poll(() => calls.resource).toBe(1);
  await expect(page.locator(`[data-resource-id="${resources[0].id}"]`)).toBeDisabled();
  await expect(page.getByRole("button", { name: "Sync project resources" })).toBeDisabled();
  await page.getByRole("button", { name: "Sync project resources" }).evaluate((button) => button.click());
  expect(calls.project).toBe(0);
  await page.getByRole("button", { name: "Refresh workspace" }).click();
  const rowSyncButton = page.getByRole("button", { name: "Sync symmetry" });
  await expect(rowSyncButton).toBeDisabled();
  await rowSyncButton.evaluate((button) => button.dispatchEvent(new MouseEvent("click", { bubbles: true })));
  expect(calls.resource).toBe(1);
  await page.locator(`[data-resource-id="${resources[1].id}"]`).click();
  const resourceDialog = page.locator("#resource-dialog");
  resourceGates[0].resolve();
  await expect(resourceDialog).toHaveJSProperty("open", true);
  await expect(resourceDialog.getByLabel("Name")).toHaveValue("GitHub Issues");

  await resourceDialog.getByRole("button", { name: "Close" }).click();
  await page.locator(`[data-resource-id="${resources[0].id}"]`).click();
  await resourceDialog.getByRole("button", { name: "Sync", exact: true }).click();
  await expect.poll(() => calls.resource).toBe(2);
  await expect(resourceDialog.getByLabel("Name")).toBeDisabled();
  await expect(resourceDialog.getByRole("button", { name: "Update" })).toBeDisabled();
  await expect(resourceDialog.getByRole("button", { name: "Detach" })).toBeDisabled();
  await resourceDialog.getByRole("button", { name: "Close" }).click();
  await page.locator(`[data-resource-id="${resources[1].id}"]`).click();
  resourceGates[1].resolve();
  await expect(resourceDialog).toHaveJSProperty("open", true);
  await expect(resourceDialog.getByLabel("Name")).toHaveValue("GitHub Issues");
  await resourceDialog.getByRole("button", { name: "Close" }).click();

  await page.getByRole("button", { name: "Sync project resources" }).click();
  await expect.poll(() => calls.project).toBe(1);
  await expect(page.locator(`[data-resource-id="${resources[0].id}"]`)).toBeDisabled();
  await expect(page.getByRole("button", { name: "Sync symmetry" })).toBeDisabled();
  await page.getByRole("button", { name: "Sync symmetry" }).evaluate((button) => button.dispatchEvent(new MouseEvent("click", { bubbles: true })));
  expect(calls.resource).toBe(2);
  await page.getByRole("button", { name: "Refresh workspace" }).click();
  const projectSyncButton = page.getByRole("button", { name: "Sync project resources" });
  await expect(projectSyncButton).toBeDisabled();
  await projectSyncButton.evaluate((button) => button.dispatchEvent(new MouseEvent("click", { bubbles: true })));
  expect(calls.project).toBe(1);
  projectGate.resolve();
  await expect(projectSyncButton).toBeEnabled();

  projectFailure = {
    error: {
      code: "multiple_failures",
      message: "resources failed to synchronize for multiple reasons",
      causes: [
        { code: "forbidden", count: 1, http_status: 403 },
        { code: "provider_unauthorized", count: 1, http_status: 502 }
      ]
    },
    results: [
      {
        resource_id: resources[0].id,
        status: "failed",
        reason: "provider_unauthorized",
        error: { code: "provider_unauthorized", message: "sensitive provider response body", http_status: 502 }
      }
    ]
  };
  allowBrowserError(page, /status of 422/);
  await projectSyncButton.click();
  await expect(page.locator("#toast")).toHaveText(
    "Project sync failed: permission denied, authentication required. Grant the required provider permissions. Reauthenticate the provider connection."
  );
  await expect(page.locator("#toast")).not.toContainText("sensitive provider response body");

  projectFailure = null;
  projectGate = deferred();
  delayedWorkspaceFailure = { armed: false, started: deferred(), release: deferred() };
  const delayedFailure = delayedWorkspaceFailure;
  await page.locator("#toast").evaluate((toast) => { toast.hidden = true; });
  await projectSyncButton.click();
  await expect.poll(() => calls.project).toBe(3);
  projectGate.resolve();
  await delayedFailure.started.promise;
  await page.locator("#project-switcher").selectOption(otherProject.id);
  await expect(page.locator("#project-title")).toHaveText("Second project");
  const oldRefreshResponse = page.waitForResponse((response) =>
    response.status() === 503 && new URL(response.url()).pathname === "/portal/api/workspace"
  );
  delayedFailure.release.resolve();
  await oldRefreshResponse;
  await expect(page.getByRole("button", { name: "Sync project resources" })).toBeEnabled();
  await expect(page.locator("#toast")).toBeHidden();
  await page.locator("#project-switcher").selectOption(project.id);
  await expect(page.locator("#project-title")).toHaveText("Connected engineering");

  project.status = "archived";
  await page.getByRole("button", { name: "Refresh workspace" }).click();
  await expect(page.getByRole("button", { name: "Sync project resources" })).toBeDisabled();
  await expect(page.getByRole("button", { name: "Sync symmetry" })).toBeDisabled();
  await expect(page.getByRole("button", { name: "Sync GitHub Issues" })).toBeDisabled();
});

test("resource mutations, refreshes, and project switches preserve one coherent UI owner", async ({ page }) => {
  const connection = {
    id: "20000000-0000-0000-0000-000000000021",
    provider: "github",
    name: "GitHub engineering",
    account_ref: "acme",
    auth_type: "gh_cli",
    capabilities: ["repositories", "changes"],
    status: "healthy",
    status_message: null,
    metadata: {},
    last_checked_at: null,
    version: 1
  };
  const resource = connectedResource(
    "30000000-0000-0000-0000-000000000021",
    "symmetry",
    "repository",
    connection.id
  );
  const project = {
    id: resource.project_id,
    key: "RACE",
    name: "Resource races",
    description: "",
    status: "active",
    version: 1,
    default_agent_profile: "codex",
    default_workspace: "primary",
    resources: [resource],
    work_items: []
  };
  const otherProject = {
    ...project,
    id: "10000000-0000-0000-0000-000000000022",
    key: "OTHER",
    name: "Other project",
    resources: [],
    work_items: []
  };
  const workspace = {
    selected_project_id: project.id,
    projects: [project, otherProject],
    connections: [connection],
    runtimes: [],
    registered_runtimes: [],
    activity: [],
    health: { connections: "healthy", runtimes: "offline", executions: "idle", synchronization: "unknown" }
  };
  const updateGate = deferred();
  let failNextWorkspace = false;
  let blockWorkspaceRefresh = false;
  let switchFailure = null;
  let resourceSyncGate = null;
  let staleUpdate = null;
  const calls = { workspace: 0, workspaceProjects: [], update: 0, delete: 0, resourceSync: 0, projectSync: 0 };

  await page.route("**/portal/api/**", async (route) => {
    const request = route.request();
    const url = new URL(request.url());

    if (request.method() === "GET" && url.pathname === "/portal/api/workspace") {
      calls.workspace += 1;
      calls.workspaceProjects.push(url.searchParams.get("project_id") || project.id);
      if (switchFailure && url.searchParams.get("project_id") === otherProject.id) {
        const failure = switchFailure;
        switchFailure = null;
        failure.started.resolve();
        await failure.release.promise;
        if (failure.fail) {
          await route.fulfill({ status: 503, json: { error: { code: "provider_failure", message: "Project switch failed" } } });
        } else {
          await route.fulfill({ status: 200, json: { ...workspace, selected_project_id: otherProject.id } });
        }
        return;
      }
      if (blockWorkspaceRefresh) {
        await route.fulfill({ status: 503, json: { error: { code: "provider_failure", message: "Workspace refresh failed" } } });
        return;
      }
      if (failNextWorkspace) {
        failNextWorkspace = false;
        await route.fulfill({ status: 503, json: { error: { code: "provider_failure", message: "Workspace refresh failed" } } });
        return;
      }
      await route.fulfill({
        status: 200,
        json: { ...workspace, selected_project_id: url.searchParams.get("project_id") || project.id }
      });
      return;
    }

    if (request.method() === "PATCH" && url.pathname === `/portal/api/resources/${resource.id}`) {
      calls.update += 1;
      if (staleUpdate) {
        const failure = staleUpdate;
        staleUpdate = false;
        failure.started.resolve();
        await failure.release.promise;
        blockWorkspaceRefresh = true;
        await route.fulfill({ status: 409, json: { error: { code: "stale", message: "Resource changed" } } });
        return;
      }
      const payload = request.postDataJSON();
      await updateGate.promise;
      Object.assign(resource, { name: payload.name, version: resource.version + 1 });
      failNextWorkspace = true;
      await route.fulfill({ status: 200, json: { resource } });
      return;
    }

    if (request.method() === "DELETE" && url.pathname === `/portal/api/resources/${resource.id}`) {
      calls.delete += 1;
      await route.fulfill({ status: 200, json: { deleted_resource_id: resource.id } });
      return;
    }

    if (request.method() === "POST" && url.pathname === `/portal/api/resources/${resource.id}/sync`) {
      calls.resourceSync += 1;
      if (resourceSyncGate) {
        const gate = resourceSyncGate;
        gate.started.resolve();
        await gate.release.promise;
        resourceSyncGate = null;
      }
      await route.fulfill({ status: 200, json: { resource } });
      return;
    }

    if (request.method() === "POST" && url.pathname === `/portal/api/projects/${project.id}/sync`) {
      calls.projectSync += 1;
      await route.fulfill({ status: 200, json: { resources: [resource] } });
      return;
    }

    await route.fulfill({ status: 404, json: { error: { code: "not_found", message: "not found" } } });
  });

  allowBrowserError(page, /status of (409|503)/);
  await login(page);
  await page.getByRole("link", { name: "Resources" }).click();

  const resourceRow = page.locator(`[data-resource-id="${resource.id}"]`);
  await resourceRow.click();
  const resourceDialog = page.locator("#resource-dialog");
  const resourceName = resourceDialog.getByLabel("Name");
  await resourceName.fill("local draft");
  resource.name = "server update";
  resource.version = 2;
  const staleRefresh = calls.workspace;
  await page.evaluate(() => document.dispatchEvent(new Event("visibilitychange")));
  await expect.poll(() => calls.workspace).toBeGreaterThan(staleRefresh);
  await expect(resourceName).toHaveValue("local draft");
  await expect(resourceDialog.locator('input[name="version"]')).toHaveValue("1");
  await expect(resourceDialog.getByRole("button", { name: "Update" })).toBeDisabled();
  await expect(resourceDialog.locator("[data-form-error]")).toContainText("changed while the editor was open");
  await resourceDialog.getByRole("button", { name: "Close" }).click();
  await expect(resourceRow).toContainText("server update");
  await expect(resourceRow).toBeEnabled();

  const resourceSync = page.getByRole("button", { name: "Sync server update" });
  await resourceSync.focus();
  const focusRefresh = calls.workspace;
  await page.evaluate(() => document.dispatchEvent(new Event("visibilitychange")));
  await expect.poll(() => calls.workspace).toBeGreaterThan(focusRefresh);
  await expect(resourceSync).toBeFocused();

  await resourceRow.click();
  await resourceName.fill("saved update");
  await resourceDialog.getByRole("button", { name: "Update" }).click();
  await expect.poll(() => calls.update).toBe(1);
  await expect(resourceDialog.getByRole("button", { name: "Update" })).toBeDisabled();
  await expect(resourceDialog.getByRole("button", { name: "Detach" })).toBeDisabled();
  await expect(resourceDialog.getByRole("button", { name: "Sync", exact: true })).toBeDisabled();
  await expect(page.getByRole("button", { name: "Sync project resources" })).toBeDisabled();
  await resourceDialog.getByRole("button", { name: "Update" }).evaluate((button) => button.dispatchEvent(new Event("click", { bubbles: true })));
  await resourceDialog.getByRole("button", { name: "Detach" }).evaluate((button) => button.dispatchEvent(new Event("click", { bubbles: true })));
  await resourceDialog.getByRole("button", { name: "Sync", exact: true }).evaluate((button) => button.dispatchEvent(new Event("click", { bubbles: true })));
  await page.getByRole("button", { name: "Sync project resources" }).evaluate((button) => button.dispatchEvent(new Event("click", { bubbles: true })));
  expect(calls).toMatchObject({ update: 1, delete: 0, resourceSync: 0, projectSync: 0 });

  await resourceDialog.getByRole("button", { name: "Close" }).click();
  await expect(resourceDialog).not.toHaveJSProperty("open", true);
  updateGate.resolve();
  await expect(page.locator("#toast")).toContainText("Resource updated, but refreshing the workspace failed: Workspace refresh failed");
  await expect(page.locator("#toast")).toContainText("The change was saved");
  await expect(resourceRow).toBeDisabled();
  await expect(page.locator("#resource-table")).toContainText("server update");

  await page.getByRole("button", { name: "Refresh workspace" }).click();
  await expect(page.locator("#resource-table")).toContainText("saved update");
  await expect(resourceRow).toBeEnabled();

  const savedResourceSync = page.getByRole("button", { name: "Sync saved update" });
  await savedResourceSync.click();
  await expect.poll(() => calls.resourceSync).toBe(1);
  await expect(savedResourceSync).toBeFocused();

  resourceSyncGate = { started: deferred(), release: deferred() };
  const delayedResourceSync = resourceSyncGate;
  await savedResourceSync.click();
  await delayedResourceSync.started.promise;
  switchFailure = { started: deferred(), release: deferred(), fail: false };
  const successfulSwitch = switchFailure;
  await page.locator("#project-switcher").selectOption(otherProject.id);
  await successfulSwitch.started.promise;
  const oldProjectRefreshes = calls.workspaceProjects.filter((id) => id === project.id).length;
  const resourceSyncResponse = page.waitForResponse((response) =>
    response.request().method() === "POST" && new URL(response.url()).pathname === `/portal/api/resources/${resource.id}/sync`
  );
  delayedResourceSync.release.resolve();
  await resourceSyncResponse;
  await expect(savedResourceSync).toBeEnabled();
  expect(calls.workspaceProjects.filter((id) => id === project.id)).toHaveLength(oldProjectRefreshes);
  successfulSwitch.release.resolve();
  await expect(page.locator("#project-title")).toHaveText(otherProject.name);
  await page.locator("#project-switcher").selectOption(project.id);
  await expect(page.locator("#project-title")).toHaveText(project.name);

  await resourceRow.click();
  switchFailure = { started: deferred(), release: deferred(), fail: true };
  const failedSwitch = switchFailure;
  await page.locator("#project-switcher").evaluate((select, projectId) => {
    select.value = projectId;
    select.dispatchEvent(new Event("change", { bubbles: true }));
  }, otherProject.id);
  await failedSwitch.started.promise;
  await expect(resourceDialog).not.toHaveJSProperty("open", true);
  await expect(page.locator(".workspace-content")).toHaveAttribute("inert", "");
  await expect(page.locator("#new-item-button")).toBeDisabled();
  await expect(page.locator("#new-project-button")).toBeDisabled();
  await expect(page.locator("#project-settings-button")).toBeDisabled();
  await page.locator("#new-project-button").evaluate((button) => button.click());
  await expect(page.locator("#project-dialog")).not.toHaveJSProperty("open", true);
  await resourceRow.click({ force: true });
  await expect(resourceDialog).not.toHaveJSProperty("open", true);
  failedSwitch.release.resolve();
  await expect(page.locator("#toast")).toHaveText("Project switch failed");
  await expect(page.locator("#project-switcher")).toHaveValue(project.id);
  await expect(page.locator("#project-title")).toHaveText(project.name);
  await expect(page).toHaveURL(new RegExp(`project_id=${project.id}.*#resources`));
  await expect(page.locator(".workspace-content")).not.toHaveAttribute("inert", "");
  await expect(resourceRow).toBeEnabled();

  staleUpdate = { started: deferred(), release: deferred() };
  const staleResourceUpdate = staleUpdate;
  await resourceRow.click();
  await resourceDialog.getByLabel("Name").fill("stale write");
  await resourceDialog.getByRole("button", { name: "Update" }).click();
  await staleResourceUpdate.started.promise;
  await resourceDialog.getByRole("button", { name: "Close" }).click();
  staleResourceUpdate.release.resolve();
  await expect(page.locator("#toast")).toContainText("current data could not be reloaded: Workspace refresh failed");
  await expect(page.locator("#toast")).not.toContainText("Current data was reloaded");
  await expect(resourceDialog).not.toHaveJSProperty("open", true);
  await expect(resourceRow).toBeDisabled();
  await resourceRow.click({ force: true });
  await expect(resourceDialog).not.toHaveJSProperty("open", true);
  blockWorkspaceRefresh = false;
  await page.getByRole("button", { name: "Refresh workspace" }).click();
  await expect(resourceRow).toBeEnabled();
});

test("Azure DevOps uses Entra ID and normalizes Boards, Repos, and Pipelines", async ({ page }) => {
  const project = {
    id: "10000000-0000-0000-0000-000000000010",
    key: "AZDO",
    name: "Azure delivery",
    description: "",
    status: "active",
    version: 1,
    default_agent_profile: "codex",
    default_workspace: "primary",
    resources: [],
    work_items: []
  };
  const workspace = {
    selected_project_id: project.id,
    projects: [project],
    connections: [],
    runtimes: [],
    registered_runtimes: [],
    activity: [],
    health: { connections: "unknown", runtimes: "offline", executions: "idle", synchronization: "unknown" }
  };
  const resourceIds = {
    repository: "30000000-0000-0000-0000-000000000010",
    work_tracking: "30000000-0000-0000-0000-000000000011",
    ci: "30000000-0000-0000-0000-000000000012"
  };
  const expectedReferences = {
    repository: "Platform/symmetry",
    work_tracking: "Platform",
    ci: "Platform/pipeline/42"
  };
  let checkFailure = false;

  await page.route("**/portal/api/**", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const payload = request.postDataJSON?.() || {};

    if (request.method() === "GET" && url.pathname === "/portal/api/workspace") {
      await route.fulfill({ status: 200, json: workspace });
      return;
    }

    if (request.method() === "POST" && url.pathname === "/portal/api/connections") {
      expect(payload.provider).toBe("azure_devops");
      expect(payload.auth_type).toBe("entra_id");
      expect(payload.capabilities.sort()).toEqual(["changes", "ci", "repositories", "work_items"]);
      expect(payload).not.toHaveProperty("credential");
      expect(payload).not.toHaveProperty("token");
      expect(payload).not.toHaveProperty("pat");
      const connection = {
        id: "20000000-0000-0000-0000-000000000010",
        provider: "azure_devops",
        name: payload.name,
        account_ref: payload.account_ref,
        auth_type: payload.auth_type,
        capabilities: payload.capabilities,
        status: "unknown",
        status_message: null,
        metadata: {},
        last_checked_at: null,
        version: 1
      };
      workspace.connections = [connection];
      await route.fulfill({ status: 201, json: { connection } });
      return;
    }

    if (request.method() === "POST" && url.pathname.endsWith("/check")) {
      const connection = workspace.connections[0];
      if (checkFailure) {
        Object.assign(connection, {
          status: "degraded",
          status_message: "Microsoft Entra ID authentication failed. Run az login again.",
          version: connection.version + 1
        });
        workspace.health.connections = "degraded";
        await route.fulfill({
          status: 502,
          json: {
            error: { code: "provider_unauthorized", message: "Provider authentication failed. Reauthenticate the connection." },
            connection
          }
        });
      } else {
        Object.assign(connection, {
          status: "healthy",
          status_message: null,
          last_checked_at: "2026-09-06T08:00:00Z",
          version: connection.version + 1
        });
        workspace.health.connections = "healthy";
        await route.fulfill({ status: 200, json: { connection } });
      }
      return;
    }

    if (request.method() === "POST" && url.pathname.endsWith("/resources")) {
      expect(payload.connection_id).toBe(workspace.connections[0].id);
      expect(payload.external_ref).toBe(expectedReferences[payload.kind]);
      const resource = {
        id: resourceIds[payload.kind],
        project_id: project.id,
        connection_id: payload.connection_id,
        kind: payload.kind,
        name: payload.name,
        provider: "azure_devops",
        external_ref: payload.external_ref,
        url: null,
        status: "unknown",
        sync_status: "unknown",
        status_message: null,
        metadata: {},
        last_checked_at: null,
        last_synced_at: null,
        version: 1
      };
      project.resources.push(resource);
      await route.fulfill({ status: 201, json: { resource } });
      return;
    }

    if (request.method() === "POST" && url.pathname.includes("/resources/") && url.pathname.endsWith("/sync")) {
      const resourceId = url.pathname.split("/").at(-2);
      const resource = project.resources.find((candidate) => candidate.id === resourceId);
      Object.assign(resource, {
        status: "healthy",
        sync_status: "synced",
        last_checked_at: "2026-09-06T08:05:00Z",
        last_synced_at: "2026-09-06T08:05:00Z",
        version: resource.version + 1
      });
      if (resource.kind === "work_tracking") {
        project.work_items = [azureExternalWorkItem(project, resource)];
      } else if (resource.kind === "repository" && project.work_items[0]) {
        const item = project.work_items[0];
        item.pull_request_url = "https://dev.azure.com/acme/Platform/_git/symmetry/pullrequest/42";
        item.review_status = "approved";
        item.delivery.pull_request = {
          value: item.pull_request_url,
          url: item.pull_request_url,
          status: "open",
          source: "provider",
          provider: "azure_devops",
          generation: null
        };
        item.delivery.review = {
          value: "approved",
          url: item.pull_request_url,
          status: "approved",
          source: "provider",
          provider: "azure_devops",
          generation: null
        };
      } else if (resource.kind === "ci" && project.work_items[0]) {
        const item = project.work_items[0];
        item.ci_status = "passed";
        item.delivery.ci = {
          value: "passed",
          url: "https://dev.azure.com/acme/Platform/_build/results?buildId=42",
          status: "passed",
          source: "provider",
          provider: "azure_devops",
          generation: null
        };
      }
      workspace.health.synchronization = "synced";
      await route.fulfill({ status: 200, json: { resource } });
      return;
    }

    if (request.method() === "PATCH" && url.pathname.includes("/work-items/")) {
      const item = project.work_items[0];
      expect(payload.repository_resource_id).toBe(resourceIds.repository);
      expect(payload.ci_resource_id).toBe(resourceIds.ci);
      Object.assign(item, {
        repository_resource_id: payload.repository_resource_id,
        repository: "Platform/symmetry",
        ci_resource_id: payload.ci_resource_id,
        ci_resource: "Azure Pipelines",
        assignee: {
          type: payload.assignee_type,
          name: payload.assignee_name || null,
          agent_profile: payload.agent_profile || null
        },
        workspace: payload.workspace || null,
        branch: payload.branch || null,
        can_start: payload.assignee_type === "agent",
        version: item.version + 1
      });
      await route.fulfill({ status: 200, json: { work_item: item } });
      return;
    }

    if (request.method() === "GET" && url.pathname.includes("/work-items/")) {
      const item = project.work_items[0];
      await route.fulfill({
        status: 200,
        json: {
          work_item: item,
          outcome: {
            phase: "not_started",
            summary: null,
            owner: item.assignee,
            blocker: null,
            findings: [],
            changed_artifacts: [],
            tests: [],
            result: null,
            failure: null
          },
          timeline: [],
          raw: { task: null, timeline: [], next_before: null }
        }
      });
      return;
    }

    await route.fulfill({ status: 404, json: { error: { code: "not_found", message: "not found" } } });
  });

  await login(page);
  await page.getByRole("link", { name: "Connections" }).click();
  await page.getByRole("button", { name: "New connection" }).click();
  const connectionDialog = page.locator("#connection-dialog");
  await connectionDialog.getByLabel("Provider").selectOption("azure_devops");
  await expect(connectionDialog.locator("#connection-auth-label")).toHaveText("Microsoft Entra ID");
  await expect(connectionDialog.locator('input[name="credential"], input[name="token"], input[name="pat"]')).toHaveCount(0);
  await connectionDialog.getByLabel("Name").fill("Azure engineering");
  await connectionDialog.getByLabel("Account or organization").fill("acme");
  await connectionDialog.getByLabel("Repositories").check();
  await connectionDialog.getByLabel("Work items").check();
  await connectionDialog.getByLabel("Changes").check();
  await connectionDialog.getByLabel("CI").check();
  await connectionDialog.getByRole("button", { name: "Connect" }).click();
  await expect(page.locator("#connection-table")).toContainText("Azure DevOps");
  await expect(page.locator("#connection-table")).toContainText("Microsoft Entra ID");

  await page.getByRole("button", { name: "Check Azure engineering" }).click();
  await expect(page.locator("#connection-table")).toContainText("Healthy");
  await expect(page.locator("#health-grid")).toContainText("ConnectionsHealthy");

  await page.getByRole("link", { name: "Resources" }).click();
  const resourceDialog = page.locator("#resource-dialog");
  for (const resource of [
    { kind: "repository", name: "Azure Repo", reference: expectedReferences.repository },
    { kind: "work_tracking", name: "Azure Boards", reference: expectedReferences.work_tracking },
    { kind: "ci", name: "Azure Pipelines", reference: expectedReferences.ci }
  ]) {
    await page.getByRole("button", { name: "Attach resource" }).click();
    await resourceDialog.getByLabel("Type").selectOption(resource.kind);
    await resourceDialog.getByLabel("Provider connection").selectOption(workspace.connections[0].id);
    await resourceDialog.getByLabel("Name").fill(resource.name);
    await resourceDialog.getByLabel("Reference", { exact: true }).fill(resource.reference);
    await resourceDialog.getByRole("button", { name: "Attach" }).click();
  }

  await page.getByRole("button", { name: "Sync Azure Boards" }).click();
  await page.getByRole("link", { name: "Board" }).click();
  const card = page.locator(".work-card", { hasText: "Azure work stays authoritative" });
  await expect(card).toContainText("Azure DevOps #101");
  await card.click();
  await expect(page.locator("#detail-content")).toContainText("Ada Lovelace");
  await expect(page.locator("#detail-content")).toContainText("Azure DevOps");
  await expect(page.locator("#detail-content")).toContainText("Available");
  await expect(page.locator("#detail-priority-select")).toBeDisabled();
  await page.getByRole("button", { name: "Edit" }).click();
  const workItemDialog = page.locator("#work-item-dialog");
  await expect(workItemDialog.getByLabel("Title")).toBeDisabled();
  await expect(workItemDialog.getByLabel("Description")).toBeDisabled();
  await workItemDialog.getByLabel("Owner type").selectOption("agent");
  await workItemDialog.getByLabel("Owner", { exact: true }).fill("Codex");
  await workItemDialog.getByLabel("Agent profile").fill("codex");
  await workItemDialog.getByLabel("Workspace").fill("primary");
  await workItemDialog.getByLabel("Repository").selectOption(resourceIds.repository);
  await workItemDialog.getByLabel("CI resource").selectOption(resourceIds.ci);
  await workItemDialog.getByLabel("Branch").fill("codex/azure-101");
  await workItemDialog.getByRole("button", { name: "Save" }).click();

  await page.getByRole("button", { name: "Close details" }).click();
  await page.getByRole("link", { name: "Resources" }).click();
  await page.getByRole("button", { name: "Sync Azure Repo" }).click();
  await page.getByRole("button", { name: "Sync Azure Pipelines" }).click();
  await expect(page.locator("#resource-table")).toContainText("Synced");
  await expect(page.locator("#health-grid")).toContainText("SyncSynced");

  await page.getByRole("link", { name: "Board" }).click();
  await card.click();
  await expect(page.locator("#detail-content")).toContainText("Platform/symmetry");
  await expect(page.locator("#detail-content")).toContainText("Azure Pipelines");
  await expect(page.locator("#detail-content")).toContainText("Open PR");
  await expect(page.locator("#detail-content")).toContainText("Passed");
  await expect(page.locator("#detail-content")).toContainText("Approved");
  await expect(page.locator("#detail-content .source-label")).toHaveText(["Azure DevOps", "Azure DevOps", "Azure DevOps"]);

  await page.getByRole("button", { name: "Close details" }).click();
  await page.getByRole("link", { name: "Connections" }).click();
  checkFailure = true;
  allowBrowserError(page, /status of 502/);
  await page.getByRole("button", { name: "Check Azure engineering" }).click();
  await expect(page.locator("#toast")).toHaveText("Provider authentication failed. Reauthenticate the connection.");
  await expect(page.locator("#connection-table")).toContainText("Degraded");
  await expect(page.locator("#connection-table")).toContainText("Microsoft Entra ID authentication failed. Run az login again.");
  await expect(page.locator("#health-grid")).toContainText("ConnectionsDegraded");
  await expect(page).toHaveURL(/\/portal/);
});

function deferred() {
  let resolve;
  const promise = new Promise((done) => { resolve = done; });
  return { promise, resolve };
}

function connectedResource(id, name, kind, connectionId) {
  return {
    id,
    project_id: "10000000-0000-0000-0000-000000000001",
    connection_id: connectionId,
    kind,
    name,
    provider: "github",
    external_ref: "acme/symmetry",
    url: null,
    status: "healthy",
    sync_status: "unknown",
    status_message: null,
    metadata: {},
    last_checked_at: null,
    last_synced_at: null,
    version: 1
  };
}

function azureExternalWorkItem(project, resource) {
  const item = externalWorkItem(project, resource);
  return {
    ...item,
    title: "Azure work stays authoritative",
    description: "Imported from Azure Boards.",
    external: {
      ...item.external,
      provider: "azure_devops",
      url: "https://dev.azure.com/acme/Platform/_workitems/edit/101"
    }
  };
}

function externalWorkItem(project, resource) {
  return {
    id: "40000000-0000-0000-0000-000000000001",
    key: `${project.key}-1`,
    project_id: project.id,
    title: "External work stays authoritative",
    description: "Imported from GitHub.",
    status: "ready",
    priority: "high",
    position: 0,
    assignee: { type: "unassigned", name: null, agent_profile: null },
    workspace: null,
    blocked: false,
    blocker: null,
    repository_resource_id: null,
    repository: null,
    ci_resource_id: null,
    ci_resource: null,
    branch: null,
    pull_request_url: null,
    ci_status: "unknown",
    review_status: "none",
    delivery: {
      pull_request: { value: null, url: null, status: null, source: "none", generation: null },
      ci: { value: "unknown", url: null, status: "unknown", source: "none", generation: null },
      review: { value: "none", url: null, status: "none", source: "none", generation: null }
    },
    external: {
      provider: "github",
      id: "101",
      url: "https://github.com/acme/symmetry/issues/101",
      state: "open",
      available: true,
      assignee: "Ada Lovelace",
      labels: ["agent-ready", "priority:high"],
      updated_at: "2026-09-05T10:05:00Z",
      resource_id: resource.id,
      provider_owned_fields: ["title", "description", "priority", "state", "labels", "assignee"]
    },
    version: 1,
    can_start: false,
    execution: null,
    updated_at: "2026-09-05T10:05:00Z"
  };
}
