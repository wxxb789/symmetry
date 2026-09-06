# Goal 3 Engineering Connections Runbook

Symmetry connects GitHub and Azure DevOps to the existing project, Kanban, and
agent-execution workflow. Connections reference local CLI authentication.
Projects bind one or more connected repository, work-tracking, and CI resources,
so a project may use Azure Boards for work, GitHub repositories for code, and
either provider for CI.

## Authentication configuration

Symmetry does not accept or persist GitHub PATs, Azure DevOps PATs, provider
access tokens, refresh tokens, or client secrets. The control-plane process
must run where the authenticated `gh` and `az` CLIs are available. Tokens are
requested at call time and kept only in the provider request process; they are
never written to PostgreSQL or included in daemon or agent payloads.

The shipped control image includes both CLIs and persists their login state in
the `provider_gh_state` and `provider_azure_state` volumes. Authenticate the
local Compose control service with:

```sh
docker compose exec control gh auth login --web --hostname github.com --git-protocol https
docker compose exec control az login --use-device-code
```

Those volumes contain CLI-managed login state, not credentials stored by
Symmetry. `docker compose down -v` removes both the local database and these CLI
login volumes, so the providers must be authenticated again afterward.

For a production Azure host with managed identity, use `az login --identity`
inside the control runtime instead of an interactive account.

Automatic polling runs every five minutes by default. Override it with a value
of at least 30 seconds:

```sh
export SYMMETRY_INTEGRATION_SYNC_INTERVAL_MS=300000
```

## GitHub

Authenticate GitHub CLI and force HTTPS before creating a connection:

```sh
gh auth login --web --hostname github.com --git-protocol https
gh auth status --hostname github.com
```

Create the connection with an account or organization login and only the
capabilities it needs. Symmetry verifies `gh` is configured for HTTPS and uses
`gh auth token --hostname github.com` once per resource synchronization, then
reuses the resulting authorization header for that synchronization's HTTPS API
requests.

Symmetry clears `GH_TOKEN` and `GITHUB_TOKEN` for these commands and accepts only
the `gho_` OAuth credential produced by the browser login flow. PAT-backed
`gh auth login --with-token` sessions are rejected.

The connection account or organization is immutable after creation. Create a
new connection rather than silently rebinding existing resources to a different
GitHub account.

Grant read access corresponding to the selected capabilities:

- `Repositories`: repository metadata and contents metadata;
- `Work items`: Issues;
- `Changes`: pull requests and reviews;
- `CI`: Actions workflow runs.

Repository, work-tracking, and CI resources use `owner/repository` as their
external reference. GitHub Issues are imported as external work items; pull
request URLs produced by agents are resolved against the bound repository, and
review plus Actions status are refreshed from GitHub.

Symmetry sends `X-GitHub-Api-Version: 2026-03-10`:

- <https://cli.github.com/manual/gh_auth_login>
- <https://cli.github.com/manual/gh_auth_token>
- <https://docs.github.com/rest/about-the-rest-api/api-versions>

## Azure DevOps

Authenticate Azure CLI with Microsoft Entra ID before creating a connection:

```sh
az login
az account show
```

Symmetry requests a short-lived access token for the Azure DevOps resource
`499b84ac-1321-427f-aa17-267ca6975798` through
`az account get-access-token`. Azure DevOps PATs are not accepted. Select only
the capabilities the connection needs:

- `Repositories`: `vso.code`;
- `Work items`: `vso.work`;
- `Changes`: `vso.code`;
- `CI`: `vso.build`.

Work-tracking resources use the Azure DevOps project name as their external
reference, and repository resources use `project/repository`. Explicit Azure
Pipelines CI resources use `project/pipeline/<definition-id>` for a specific
pipeline definition, including a pipeline whose source repository is GitHub.
Symmetry validates the definition before marking the resource healthy. An Azure
Repos resource may instead use its own connection's `CI` capability without a
separate CI resource. Azure Boards items are imported through WIQL and batched
Work Item reads. Pull-request votes and Azure Pipelines builds are normalized
into the same review and CI states as GitHub.

Pipeline correlation accepts the source/head commit, an Azure Repos PR merge
commit, or the PR source commit reported in provider trigger metadata. Unknown
or future provider result values remain `unknown`; they are never treated as a
successful build. History lookup follows Azure continuation pages up to a
bounded limit; exceeding it degrades the resource with a diagnostic instead of
silently replacing a known result with a false success.

- <https://learn.microsoft.com/azure/devops/integrate/get-started/authentication/authentication-guidance>
- <https://learn.microsoft.com/rest/api/azure/devops/wit/wiql/query-by-wiql?view=azure-devops-rest-7.1>

## Ownership boundaries

For imported work, the external provider owns the external title, description,
state, labels, human assignee, URL, revision data, and the priority derived from
provider labels or tags. The portal exposes those fields as read-only provider
data. Symmetry keeps a separate local workflow for its Kanban and owns agent
assignment, agent profile, workspace, branch, run history, lease/generation
state, policy, usage, and generated artifacts.

Provider synchronization is one-way. It never writes issue or work-item fields
back to GitHub or Azure DevOps, so there is no second mutable copy competing for
authority. Manual PR, CI, and review values remain available for unconnected
work. For connected delivery, provider-observed values take precedence and are
marked with `source: provider` in the portal API.

If a previously imported item is absent from a complete provider sync, Symmetry
retains its run history and last provider fields but marks it unavailable. New
runs and retries remain blocked until a later sync observes the item again.

Pull-request state is normalized to `unknown`, `open`, `closed`, or `merged`.
CI state is normalized to `unknown`, `pending`, `passed`, or `failed`.

## Agent provider capabilities

An agent profile must use `input_mode: "json"` and explicitly set
`provider_access: true` before its runtime advertises broker support. JSON input
without that opt-in remains a normal structured-input profile and is not
eligible for connected tasks.

```json
{
  "input_mode": "json",
  "provider_access": true,
  "interactive": true,
  "event_format": "jsonl"
}
```

At launch, Control snapshots only the work item's connected resource IDs;
manual resources are excluded and a later connection cannot widen the task. At
claim time, Control validates every snapshotted resource and returns a signed
broker token plus the exact operations it may use. The token is bound to the
run, task, runtime epoch, generation, and claim ID. It is never placed in the
process environment, command arguments, logs, or the daemon journal. Each
broker request revalidates the current database lease; renewal keeps the
capability usable, while lease expiry, cancellation, completion, a newer
generation, or daemon re-registration revokes it.

Connected change operations use server-owned scope. The source branch is the
work item's branch snapshot and the target branch is the repository's synced
default branch. An agent may supply pull-request title and body, but it cannot
choose a different branch pair or update another pull request in the same
repository. Set the work-item branch and synchronize the repository before
starting the run.

The agent calls the broker URL from its initial stdin envelope. It reuses one
UUID `action_id` only when retrying an unknown outcome for the same request.
Control records transport failures, HTTP 5xx responses, and invalid successful
mutation responses as `unknown`; a retry with the same action ID and normalized
input may redispatch only while the execution fence and capability remain live.
Pre-dispatch authentication/configuration failures and definitive HTTP 4xx
responses are stored as failed and replayed for that ID. A different action for
the same run/resource is blocked until an unknown outcome is resolved. If the
run remains live and the agent makes a new logical attempt after a definitive
failure, it must use a new action ID. Branch and PR URL fields are rejected at
this boundary:

```json
{
  "action_id": "7be912f7-4e72-457d-bbfb-04291788aa40",
  "resource_id": "repository-resource-uuid",
  "operation": "change.upsert",
  "input": {
    "title": "Implement connected delivery",
    "body": "Optional pull-request body"
  }
}
```

Control commits a durable, credential-free action intent before contacting the
provider. A supervised broker worker owns execution, and a per-intent
PostgreSQL advisory session lock prevents concurrent dispatch across Control
instances without holding a database transaction during provider I/O. While
the execution remains live, the same action ID and normalized request replay a
stored success or sanitized definitive failure; the same ID with different
input returns `idempotency_conflict`. An accepted orphan is resumed. A live
executing intent returns `state_conflict`; after its advisory owner disappears,
recovery records the interrupted outcome as `unknown` before allowing a
same-ID retry. After the execution fence is lost, matching replays return
`ownership_lost`. This ordering also gives cancellation and permission changes
a deterministic boundary: a change that commits before intent acceptance
blocks dispatch, while an action accepted first is allowed to finish.

## Portal workflow

1. Open `/portal`, select **Connections**, and create a GitHub or Azure DevOps
   connection.
2. Run **Check connection** and inspect the health message.
3. Select a project, open **Resources**, and attach repository, work-tracking,
   and CI resources to that connection.
4. Run resource or project synchronization. Imported work appears on the board.
5. Edit the imported item, choose its repository and source branch, and
   optionally select an independent CI resource. Synchronize the repository so
   its default target branch is current. Leaving **CI resource** empty uses the
   repository provider's CI capability.
6. Assign the item to an agent and start it through the normal orchestration
   path.
7. When the agent reports a pull-request URL, repository synchronization reads
   pull-request and review state. The selected CI resource independently reads
   pipeline state without sending CLI access tokens to the daemon.

For a mixed-provider flow, bind Azure Boards as work tracking, a GitHub
repository for changes, and an Azure CI resource such as
`Platform/pipeline/42`. The portal reports the concrete provider beside each
pull-request, review, and CI projection so their separate authority remains
visible.

Authentication, permission, missing-resource, and transport failures persist as
degraded connection/resource health with an actionable status message.

## Verification

Run the control-plane gate:

```sh
cd control
mix format --check-formatted
mix compile --warnings-as-errors
mix test
MIX_ENV=prod mix release --overwrite
```

Run the browser acceptance suite at desktop and mobile widths against a running
control plane:

```sh
cd browser
npm test
```

The deterministic provider tests do not require live GitHub or Azure DevOps
sessions. A live smoke test should use non-production accounts with the minimum
read permissions needed by the selected capabilities.
