# OmO 5.1.69 native evidence

These are independent OmO classic RPC observations, not Pi compatibility claims.
The executable identifies itself as
`omo 5.1.69 (engine: senpi 2026.11.10-2; scheme nodef)` on both platforms.

| Platform | Executable | Bytes | SHA-256 |
| --- | --- | ---: | --- |
| Windows 11 / 10.0.26300 / AMD64 | `C:/Users/lhan/.local/bin/omo.exe` | 202352128 | `04338dd63b24c53156492e1a0777d996034416c4131a26e1ba48eb07b5429464` |
| WSL Ubuntu-24.04 / Ubuntu 24.04.5 / x86_64 / kernel 6.18.58.1-microsoft-standard-WSL2 | `/root/.local/bin/omo` | 211998896 | `3a74aba613cf02e740073b302171c0fb9d9155f5ff6069f91bea23972ffa1444` |

`windows.json` and `linux.json` retain original request objects and byte-preserved
native JSON strings from the first fresh-session turn, through `agent_idle`.
Only the witness's `get_session_stats` request and matching response are excluded;
the complete local receipts retain those observations. Each fixture records its
source receipt SHA-256, full platform identity, executable identity and exclusion.
`TestPinnedNativeTranscript` replays each transcript on its native platform;
Windows paths are not rewritten into Linux handles or vice versa.

The real provider was the approved Tencent LiteLLM relay, model
`tencent-litellm/deepseek-v4.1-flash-fast`. Its URL and credential were loaded from
the operator's existing harness configuration. The credential was passed only in
`SYMMETRY_OMO_PROBE_KEY`; isolated `models.json` contains the environment-variable
reference, never its value. No remote repository write was performed. The source
workspace was the authorized local clone of
`wxxb789/symmetry-0006h-provider-fixture`.

Reproduce the direct native characterization from the repository root with the
approved key and URL already in the environment:

```text
python3 scripts/ci/omo-native-evidence.py --binary <absolute-pinned-binary> --repository <authorized-local-clone> --out <new-local-receipt.json>
```

For Linux, run that command inside `wsl.exe -d Ubuntu-24.04` and transport only the
two named relay environment variables via `WSLENV`. The script proves actual
read/write work, discarded-ACK replay, conflicting-payload rejection, native
process restart and retained reopen, fresh-session handoff, stream cancellation,
and unrelated-session preservation. Those are not daemon/Control restart proofs.

The direct turn's final JSON describes the observed artifact, not a Symmetry
`TaskResult`. `TestNativeAcceptance` separately exercises the real adapter and
Runner, strict `TaskResult`, repository Subject/artifact integrity, flushed and
reopened journals, retained resume, fresh handoff and native cancellation. Its
opt-in and exact command are documented in `omo/native_test.go`; a skipped run is
not evidence. Native reported zero cost with `costUnknown: true`; accounting
remains unknown, never free or authoritative zero.

## Promoted profile and receipt boundary

`adapter-windows.json` and `adapter-linux.json` retain the final public
`Adapter.Start`/Runner acceptance receipts. Each records the binary path, size,
digest, platform, native handle, session directory, workspace fingerprint,
durable admission/local-handle IDs, Subject/hash, exact artifact bytes/digests,
and physical stop/drain result. Both platforms promote only start, events,
cancel, retained resume and same-machine fresh handoff. The fixed argv enables
only `read,write`; profile tool overrides are rejected.

Reproduce from `daemon/`, with `SYMMETRY_OMO_PROBE_KEY` and
`SYMMETRY_OMO_PROBE_URL` supplied only through the process environment:

```text
SYMMETRY_OMO_NATIVE_ACCEPTANCE=1 SYMMETRY_OMO_NATIVE_EXECUTABLE=<absolute-pinned-binary> SYMMETRY_OMO_NATIVE_REPOSITORY=<authorized-local-clone> SYMMETRY_OMO_NATIVE_OUTPUT=<new-local-receipt.json> go test ./internal/harness/omo -run '^TestNativeAcceptance$' -count=1 -v -timeout=15m
```

On Windows set `TMP` to `TEMP`; clear unrelated
`SYMMETRY_CLAUDE_LIVE_SMOKE*` variables. On Linux use the named WSL distribution
and start Go through a Python `subprocess.run(..., close_fds=True)` boundary.
The fixture README artifact is verified against the real repository Subject;
the three native-created artifacts remain untracked and are checked byte for
byte. The negative witness changes the Subject tree digest and verifies that
`ReadSubjectArtifact` rejects the README read with `ErrSubjectMismatch`; the
receipt's `wrong_digest_rejected` field records that boundary, not rejection of
an expected output-artifact content digest or full Control artifact acceptance.
Fresh and retained semantic results cannot be published without a matching
native journal header.

The direct capture script strictly decodes both native streams as UTF-8. Invalid
bytes fail capture without archiving replacement text or byte contents; the
failed observation identifies stdout or stderr, and both readers keep draining.

These receipts do not assert Control admission, a Control stop certificate,
daemon power-loss recovery, cross-machine handoff or provider-broker access.
Native steer, safe pause, approval responses, shell/terminal child tools,
authoritative billable usage and hard cost limits remain unsupported. Generic
daemon/Control restart gates are separate evidence, not credentialed OmO E2E.

## Separate Goal/Control recovery witness

`scripts/ci/omo-control-native-fixture.exs` creates an operator-owned local test
Goal through the actual Workspaces and Goals APIs, including plan Decision,
approval, immutable context and one admission. Run its `create` mode in the
task-owned Control release with a new fixture label/output path; its database
guard permits only `symmetry_0006k_e2e`. These fixture approvals never accept the
user's Goal. Its paired `readback` mode returns sanitized authoritative rows,
not lease secrets or a provider credential.

With the approved relay key/URL and local enrollment/operator tokens supplied
only by their named environment variables, run on each actual native platform:

```text
python3 scripts/ci/omo-control-native-evidence.py --daemon <built-production-daemon> --native-binary <absolute-pinned-binary> --repository <authorized-local-clone> --control-url <task-owned-loopback-Control> --fixture-json <new-operator-fixture.json> --out <new-private-root> --host-label <Windows-or-Ubuntu-24.04>
```

The default `stop-ack` boundary holds the committed attachment response until
the actual native process is observed, loses that response, requires the
daemon's original-fence readback before one provider turn, then crashes only
after native physical stop and a committed stop receipt. It requires exact
stop replay, one original Run/admission, one native user input, one write,
immutable Subject/context and byte-checked artifact after reconnect.
The held attachment boundary also captures every tracked and untracked workspace
file, including ignored files and preexisting daemon ownership metadata, before
native input. Final paths, sizes and hashes must equal that baseline plus only
the authorized artifact; no metadata filename is exempt. Earlier receipts that
lacked this baseline remain failed or unverified, never retroactively passed.
Optional `--control-restart-trigger` and `--db-readback-trigger` request blocking parent
handshakes for an identity-attested real Control restart and paired direct DB
readback; the runner never executes service commands or connects to PostgreSQL.
Absent handshakes remain explicitly unverified. Every failed private root is
preserved; creating another fixture is never recovery of failed paid work.

For a Windows worktree observed from WSL, `--source-attestation` explicitly
supplies the parent's observed Git HEAD and exact relevant source blobs. The
runner checks every size/digest locally and records this origin; it does not
claim Linux Git resolved Windows worktree metadata or that hashes alone prove
the daemon was built from those blobs.

On Linux the pinned self-contained client may re-exec a byte-identical cached
image inside the private HOME. Provenance binds `/proc/<pid>/exe` size/digest
and the process creation identity; every actual argument must still match the
admitted profile. A matching cache pathname or `argv[0]` alone is not evidence.

The failed Windows `adapter-windows-v4.json` remains in the local session
evidence directory. Native `repetitive-turns` interrupted a partial second
TaskResult after the artifact write, and the adapter published no semantic
success. A later unchanged-task diagnostic run passed; that does not prove
native rule-driven intervention support. Final witness summaries describe the
different actual tasks, without changing schema, Subject, artifact assertions
or identity/settlement gates. Such intervention remains unverified and
uncorrelated native work remains fail-closed.
