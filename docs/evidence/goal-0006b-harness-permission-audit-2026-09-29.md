# Goal 0006b native harness permission audit

Date: 2026-09-29

Mode: read-only investigation; no native model work was submitted

Acceptance environment: owner-selected local WSL2

Actual investigator route: `PI_PROVIDER=ghc`, `PI_MODEL=gpt-5.6-sol-fast`, `PI_REASONING_LEVEL=medium`

## Decision and policy conflict

The requested default is implementable for the four known native transports: every native turn should run without a native filesystem sandbox and without a native approval prompt, while retaining the admitted workspace as its working directory. This is **not** permission to weaken the daemon boundary. RunKey/fence checks, distinct-UID credential locality, Linux cgroup/process ownership, durable process/native-handle barriers, `StopReceipt`, cancellation, output settlement, clone validation, and A-owned promotion remain unchanged.

`docs/design/harness.md` currently says, "No bypass flags, global allow-all or inferred permission expansion to make unattended execution work." That directly contradicts the owner's 2026-09-29 instruction. Amend only that native permission policy: known native adapters default to their version-pinned full-access/no-prompt setting and always receive the admitted working directory; generic commands encode that policy in operator-supplied argv/config because Symmetry cannot invent semantics for an unknown executable. Preserve the approval-response identity, loopback authentication, credential-locality, lifecycle, and containment requirements.

Here `full access` disables the native agent's permission/sandbox layer. The working directory is an execution starting point, not filesystem confinement. The fixed B UID remains a shared native-run filesystem trust domain, so another B process can access B-owned sibling paths; no per-clone confidentiality is claimed. Cgroup ownership, A/B credential separation, leases, daemon admission, A-owned journals and validated artifact promotion remain enforced independently.

## Shared cwd path

`app.startGoalAssignment` obtains and persists `prepared.Path`, builds a direct `execution.Invocation` with `Dir: prepared.Path`, and passes both `StartRequest.Workspace: prepared.Path` and the invocation to the selected adapter (`daemon/internal/app/app.go`). `execution.Runner` executes argv directly, never through a shell. `newHarnessRegistry` binds each adapter to `AgentProfile.Command`; Pi and generic consume configured `AgentProfile.Args` as described below.

A Pi resume recovers the retained workspace/fingerprint before constructing `StartRequest`; new Linux Pi/OpenCode runs receive the B-owned standalone clone as `prepared.Path`. Linux v2 binds the exact RunKey/fence and clone grant before launch. Permission changes belong inside each adapter's native configuration, not in the execution runner or containment code.

## Per-harness matrix

| Family | Exact existing/default machine configuration | Cwd, turn, and resume propagation | Pinned support and minimal required behavior |
| --- | --- | --- | --- |
| Codex | argv: `codex app-server --stdio`. `thread/start`: `approvalPolicy: "on-request"`, `approvalsReviewer: "user"`, `sandbox: "workspace-write"`, `cwd: <workspace>`, `ephemeral: false`; config keys `sandbox_workspace_write.writable_roots=[]`, `sandbox_workspace_write.network_access=false`, `sandbox_workspace_write.exclude_tmpdir_env_var=true`, `sandbox_workspace_write.exclude_slash_tmp=true`. Response validation requires `on-request`, `user`, and `sandbox.type == "workspaceWrite"`. | `Invocation.Dir`, `thread/start.cwd`, and every `turn/start.cwd` equal the absolute `StartRequest.Workspace`; both returned CWDs are identity-checked. Resume is rejected before launch, so no live resume request exists. | Pinned 0.156.1 source/schema supports request `sandbox: "danger-full-access"` (`SandboxPolicy::DangerFullAccess`) and `approvalPolicy: "never"` (`AskForApproval::Never`). Require response `sandbox.type == "dangerFullAccess"` and `approvalPolicy == "never"`; remove the four workspace-write config keys/assertions but retain exact CWD validation. Do not use a process CLI bypass: app-server has exact thread fields. Existing 0.156.1 smoke observed a `readOnly` downgrade, so WSL2 acceptance must inspect the granted fields. |
| Claude Code | argv: `claude --print --verbose --input-format stream-json --output-format stream-json --json-schema <compact TaskResult schema> --session-id <uuid>`. No permission field/flag is sent. The candidate remains unregistered; registry probing is fail-closed. | `Invocation.Dir` is the absolute workspace. `Open` waits for the supplied UUID and `StartTurn` writes one stream-JSON user frame to that same process, inheriting cwd. Resume is rejected; no candidate resume argv exists. | Add fixed flag `--dangerously-skip-permissions` (flag presence is the machine value). Current official help and installed 2.1.284 support it. The pinned 2.1.281 evidence records transport flags but not permission help, so recapture exact pinned WSL2 help and run the candidate smoke before claiming support/registration. Do not use `--allow-dangerously-skip-permissions`, which enables the option without selecting it. Any future resume argv must carry the same bypass and validated cwd. |
| Pi | fresh argv: `pi --mode rpc <audited profile args>`; resume argv: `pi --mode rpc --session <absolute retained session file> <audited profile args>`. The profile validator admits audited model/tool/resource options and rejects transport, prompt, history/session, extension-loader, API-key, and unknown overrides. | `Invocation.Dir` is the absolute workspace on fresh and resume. `Open` sends `get_state`; `StartTurn` sends RPC `prompt`. Resume requires matching workspace fingerprint/version and validates returned session ID/file/state before prompting. | Pi 0.87.1 already has **no tool sandbox or per-tool approval gate**. Built-in `bash` executes in supplied cwd and write/edit act directly. Add no yolo flag. `--no-approve`/`-na` is not a permission bypass: pinned source maps it to `projectTrustOverride=false` and help says "Ignore project-local files"; `--approve` trusts them. Keep profile args unchanged by default. Explicit no-tool/deny settings remain operator overrides. |
| OpenCode | argv: `opencode serve --hostname 127.0.0.1 --port 0 --pure`. Env replaces caller credentials with `OPENCODE_SERVER_USERNAME=opencode` and `OPENCODE_SERVER_PASSWORD=<random>` but has no permission value. `Open`: `POST /api/session` with `location.directory:<workspace>`. `StartTurn`: prompt `id`, `prompt.text`, `delivery:"steer"`, `resume:true`. | `Invocation.Dir` and `location.directory` equal the absolute workspace; the response directory is checked. Prompt/events use that session. Native resume is rejected. | Pinned 1.18.32 source JSON-decodes `OPENCODE_PERMISSION` and merges it into permission config. Official config accepts scalar `"allow"` for all permissions. Add/replace exactly `OPENCODE_PERMISSION="allow"`, where the env value contains JSON quotes (`OPENCODE_PERMISSION=\"allow\"` as a Go literal). This is stronger than `--auto`, which preserves explicit denies. Keep basic auth, loopback/peer verification, and cwd. Future resume must use the same env/cwd. |
| Generic | argv is exactly operator-configured `AgentProfile.Command` plus `AgentProfile.Args`; the adapter does not parse or alter it. It forwards `StartRequest.Invocation` and treats output as opaque. | Legacy launch sets `Invocation.Dir: prepared.Path`. There is no native turn/resume protocol. | No versioned API defines a yolo flag. Do not guess one. Policy must require the configured command/argv/config itself to select full access and mark that operator-owned/unverified. If machine-verifiable yolo is mandatory, generic is ineligible rather than falsely advertised. Add a regression proving argv/cwd pass through unchanged. |

## Evidence and version limits

- Codex: `daemon/internal/harness/codex/adapter.go` constructs launch/thread/turn requests; `rpc.go` defines and validates fields. `testdata/codex/0.156.1/schema-manifest.json` pins the generated v2 schema and old permission mismatch. Pinned upstream [`protocol.rs`](https://github.com/openai/codex/blob/rust-v0.156.1/codex-rs/protocol/src/protocol.rs) defines `Never` and `danger-full-access`; [`thread.rs`](https://github.com/openai/codex/blob/rust-v0.156.1/codex-rs/app-server-protocol/src/protocol/v2/thread.rs) exposes `cwd`, `approvalPolicy`, and `sandbox` on thread start/resume. Installed 0.158.0 help corroborates newer support but is not evidence for the 0.156.1 pin.
- Claude: `daemon/internal/harness/claude_candidate_adapter.go` owns argv/cwd. [Official headless docs](https://code.claude.com/docs/en/headless) say `-p` loads working-directory configuration and document resume/process behavior. Installed 2.1.284 help lists permission mode `bypassPermissions` and `--dangerously-skip-permissions`; the pinned 2.1.281 gap remains explicit.
- Pi: `daemon/internal/harness/pi/adapter.go` owns RPC argv/resume/cwd. Pinned 0.87.1 [`args.ts`](https://github.com/earendil-works/pi/blob/v0.87.1/packages/coding-agent/src/cli/args.ts) proves `--no-approve` semantics; pinned `bash.ts` executes against cwd with no approval callback. [Official RPC docs](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/rpc.md) state normal CLI options select working folder/session behavior and installed help is authoritative.
- OpenCode: `daemon/internal/harness/opencode/{adapter.go,types.go,client.go}` own serve argv/env, location, prompt, auth, and peer checks. Pinned 1.18.32 [`config.ts`](https://github.com/anomalyco/opencode/blob/v1.18.32/packages/opencode/src/config/config.ts) lines 559-565 decode `OPENCODE_PERMISSION`; [permission docs](https://opencode.ai/docs/permissions/) specify scalar `permission:"allow"`; [server docs](https://opencode.ai/docs/server/) specify loopback serve and password auth.
- Generic/shared: `daemon/internal/harness/generic.go`, `daemon/internal/app/app.go`, and `daemon/internal/execution/runner.go` prove direct argv/cwd propagation without a shell or generic permission dialect.

Installed inspection made no paid/native work call: Codex 0.158.0, Claude 2.1.284, and Pi 0.87.1 were read with `--version`/`--help`. OpenCode's shim had no selected version, so no installed claim is made; pinned source/fixtures are used. A `mise` deferred Codex-uninstall warning appeared while invoking the shim; it changed no repository file and is not capability evidence.

## Disjoint implementation sets

The parent/integration owner owns shared policy and app contracts.

1. **Codex worker:** `daemon/internal/harness/codex/{adapter.go,rpc.go,adapter_test.go,rpc_sandbox_test.go}`; update pinned evidence only from an actual WSL2 run.
2. **Claude worker:** `daemon/internal/harness/{claude_candidate_adapter.go,claude_candidate_adapter_test.go,claude_candidate_cmdline_windows_test.go}` plus native candidate evidence. Registration remains a separate capability decision after pinned proof.
3. **Pi worker:** normally no production edit; only `daemon/internal/harness/pi/{argv_test.go,adapter_test.go}` to pin no extra permission flag and cwd/resume argv.
4. **OpenCode worker:** `daemon/internal/harness/opencode/{adapter.go,adapter_test.go}`; reuse env-replacement logic rather than shared config.
5. **Generic worker:** `daemon/internal/harness/harness_test.go` only; no guessed flag.
6. **Parent/shared owner:** `docs/design/harness.md` and, only if needed, `daemon/internal/app/harness_test.go`. Do not modify `types.go`, execution containment, Linux owner/clone, state journals, cancellation, or StopReceipt paths.

## Exact proof targets

Run from `daemon/` on local WSL2 after implementation:

```text
go test ./internal/harness/codex -run 'Test(AdapterStagesNativeTransportAndRequiresMatchingStructuredCompletion|OpenRejectsSandboxDowngrade|ThreadStartWorkspacePolicyCannotEscapeAdmission)' -count=1
go test ./internal/harness -run 'Test(ClaudeCandidateStagesFreshTransportAndBarriers|GenericAdapter.*)' -count=1
go test ./internal/harness/pi -run 'Test(PiRPCArgs|AdapterStagesPiRPC|AdapterResumesExactPiSession)' -count=1
go test ./internal/harness/opencode -run 'Test(StartUsesOwnedServeInvocationPersistsBeforeOutputAndWithholdsSecrets|OpenStartTurnAndWaitStayFailClosed|StartTurnAdmitsPromptBeforeOpeningDurableReplay)' -count=1
go test ./internal/app -run 'Test(NewHarnessRegistryBindsConcretePiAndOpenCodeAdapters|FreshCodexNativeStart|PiResumeAdmission)' -count=1
```

Update Codex test names for the new full-access response. Assert exact machine values:

- Codex: request/response `approvalPolicy=="never"`, request `sandbox=="danger-full-access"`, response `sandbox.type=="dangerFullAccess"`, exact thread/turn cwd, and no `sandbox_workspace_write.*` entries.
- Claude: exactly one `--dangerously-skip-permissions`; `Invocation.Dir` unchanged.
- Pi: fresh/resume argv exactly as above, no synthetic permission option, workspace `Dir` unchanged.
- OpenCode: exactly one env `OPENCODE_PERMISSION="allow"`, replacing caller input; credentials remain secret; process `Dir` and `location.directory` unchanged.
- Generic: command, args, env, and `Dir` unchanged.

Then run full changed packages and `go test ./internal/app ./internal/harness/... -count=1`. Acceptance uses exact pinned WSL2 binaries and existing opt-in native smoke surfaces. A smoke must inspect effective permissions, cwd, and cleanup; fixtures/newer help cannot promote a pin. Claude needs pinned 2.1.281 help capture; OpenCode needs a selected 1.18.32 WSL2 binary. Launch/config/cwd proof needs no model call, but release capability remains fail-closed until the design's credentialed lifecycle evidence is separately met.

## Bounded conclusion

The smallest correct implementation changes native policy at each known adapter boundary: two Codex request/validation values, one Claude argv flag, no Pi production change, and one OpenCode env value. Generic remains user-configured. Shared runner, authority, containment, persistence, resume fencing, cancellation, and stop-proof code are out of scope and must not change.
