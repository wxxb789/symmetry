# Native harness boundary

Normative target. Go adapters translate native protocols into Symmetry v1.
Do not require native CLIs to speak the existing fake-agent JSON dialect.

## Fixed transport choices

| Harness | Transport choice | v1 control policy |
| --- | --- | --- |
| Codex | `codex app-server`, stdio JSON-RPC | native thread/turn lifecycle; capability-probed steering, interrupt and approval mapping |
| Claude Code | headless CLI with documented JSON/stream-JSON output and explicit session resume | next-turn guidance; process cancellation; no claimed safe pause or interactive approval bridge without verified native support |
| pi | native `--mode rpc`, JSONL stdin/stdout | prompt/steer/abort/session RPC mapping; no inferred OS pause |
| OpenCode | daemon-owned loopback `serve`, HTTP API + SSE | session/prompt/abort/event mapping; API-version-verified permissions |

Primary sources checked 2026-09-08:
[Codex app-server](https://developers.openai.com/codex/app-server/),
[Claude Code programmatic use](https://code.claude.com/docs/en/headless),
[pi RPC](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/rpc.md),
[OpenCode server](https://opencode.ai/docs/server/).
These choose the integration architecture, not untested compatibility claims.
Implementation records exact installed CLI version and sanitized native fixtures
in `daemon/internal/harness/testdata/<kind>/<version>/`. Release support is an
explicit tested version set/range; an unknown version cannot advertise unverified
controls. Read the installed protocol/help/schema to fill exact wire DTO fields;
do not choose a different integration architecture without amendment.

## Go API shape

Use a concrete registry with four constructors, not a dynamic plugin loader.
The consumer interface expresses lifecycle; transport-specific DTOs stay private.

```go
type Adapter interface {
    Probe(context.Context) (Capabilities, error)
    Start(context.Context, StartRequest, EventSink) (Session, error)
}

type Session interface {
    Control(context.Context, ControlRequest) (ControlReceipt, error)
    Wait(context.Context) (TaskResult, error)
    Close(context.Context) error
}
```

StartRequest contains admission identity, local workspace, decoded context,
model profile, limits and optional validated local resume handle. It does not
contain server credentials. Session owns at most one native turn at a time.
EventSink accepts bounded typed events and returns persistence errors. Do not
acknowledge events that could not be durably journaled. Reuse existing process
runner/process-tree termination and outbox; avoid a parallel persistence path.

Capabilities are produced by executable/version probing and tested transport
behavior, not arbitrary configuration booleans. A configured `generic` adapter
remains for legacy commands/fake-agent tests, but cannot claim native resume,
guidance, approvals or usage without an explicit tested bridge.

## Session and workspace recovery

Persist launch intent before starting. After native session creation, persist
local_handle_id -> native session handle + workspace fingerprint + native version
before sending session_started. Workspace fingerprint binds machine-local
canonical directory, repository identity and daemon worktree ownership; a Git
commit is a separate moving artifact identity, not the session's directory key.

If a crash occurs between native creation and handle persistence, classify the
launch as uncertain; reconcile the existing process/workspace before creating a
replacement. Never start a second agent merely because an RPC timed out.
Restart recovery retains current conservative process-tree cleanup/fencing;
resume occurs as a new admitted execution after prior execution is known stopped.
Do not pretend a new daemon reattached to an unverifiable live child.

Resume requires same machine, compatible native version, same repository and
workspace fingerprint, no other active owner, retained native data, and current
goal authorization. A native "resume rejected" is typed and visible. A fresh
session handoff preserves committed work and reconstructs context. Changing model
or harness is an explicit new admission, with lineage preserved.

Default goal-managed workspaces use existing git_worktree policy. A failed run's
only artifact cannot be cleaned up under cleanup=always; retention reason is
durable. Cross-machine handoff requires an artifact published to an authorized
Git location by a separately allowed action. A reachable authorized Git commit
is a necessary safety precondition, not a promise that cross-machine handoff
is available. The current verified scheduling path is source-machine-local;
cross-machine handoff remains unsupported unless a separately verified adapter
and capability path is present. A local commit is not globally reachable just
because its SHA is known.

## Controls and permissions

Implement cancel using native abort when available, then bounded process-tree
termination fallback. Both are cancellation, not safe pause. Implement guidance
with native steer only when semantics are verified; otherwise store guidance for
the next turn. Pause capability is false until an adapter proves a safe retained
boundary and matching resume semantics under Symmetry's existing lifecycle.
Goal-level scheduling pause works even when native safe pause is unsupported.

Native permission requests are mapped only for adapters whose verified API has a
response path. Persist request identity and exact tool/action digest; reconnect
does not reuse approval for a different native request. No bypass flags, global
allow-all or inferred permission expansion to make unattended execution work.
For unsupported approval bridges, surface the limitation and require a native
operator interaction or an explicitly configured narrow permission policy.

OpenCode server binds loopback, uses per-daemon authentication where supported,
and is owned/terminated by the daemon. If a version cannot isolate/authenticate
that server adequately for the deployment, do not mark it supported there.
Never expose the native server as a public Symmetry API. Keep credentials local.

## Failure vocabulary and validation

Normalize `auth`, `quota`, `rate_limit`, `network`, `unsupported_version`,
`resume_rejected`, `context_overflow`, `missing_result`, `process_failure`,
`cancelled`, `unknown_outcome`. Retry transient failures within approved admission
limits; auth/config failures do not spin. Context overflow may require a fresh
snapshot/session, not blind replay. Agent failure usage is still accounted.

Evidence required for each supported adapter: a real small repository task,
native stream replay, malformed/interleaved events, version rejection, process
cancel/drain, session resume or explicit unavailability, duplicate event replay,
usage semantics or explicit unknown, and retained artifact recovery. Test any
advertised guidance/approval/pause independently. Golden fixtures complement,
not replace, a credentialed native smoke run. Do not check credentials into Git.

Linux and Windows must both be covered for release support; tests skipped on an
unavailable machine remain pending. Native macOS daemon support is not added by
this design. A container on macOS can run the Linux deployment as today.

## Existing chat compatibility

The current Chat start path requires generic supervisory_control. Replace that
blanket requirement for native admissions with required start/events/result
capabilities and operation-specific checks. Preserve the legacy generic path.
Show next-turn guidance separately from native steering, and hide or explain
unavailable run-pause controls. Scheduling a goal pause does not require native
pause. Existing chat intent and command acknowledgement tests must remain valid.
