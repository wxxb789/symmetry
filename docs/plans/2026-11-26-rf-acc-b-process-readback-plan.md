# rf-acc-b-process-readback — implement plan

Contract: `goal-rf-acc-b-process-readback.md` (GDD state root, external to the repository). Base
revision `8453da12` (PR #293 merge, the ruling's dispatch gate `main 8453da12`); branch
`refactor/rf-acc-b-process-readback`. The accepted plan is the contract; this file resolves the
implementation choices it leaves open (which one mechanism, its exact protocol and bounds, the
per-site census dispositions) and records the probe evidence that closed them. Root cause and the
measured read matrix: `findings/environ-root-cause.md` (EnvironRoot) in the GDD state root.

## 1. Clause census at the dispatch head

| Contract clause | Dispatch head | State |
| --- | --- | --- |
| Change: verify env/FD set without A caps | `daemon/native/linux-gate/protocol/clone_integration_linux_test.go:712` (`/proc/<pid>/environ`), `:727` (`/proc/<pid>/fd`), `launcher_integration_linux_test.go:1379` (`environ`), `:1390` (`fd`); `assertCloneNativeBProcess:682-800`, `assertNativeBProcess:1361-1425` | Both read ptrace-/DAC-gated files as A; the two failing rows are exactly those `environ` reads (runs 45696662454 §, 45688173130) |
| Change: namespace identity | `daemon/e2e/production_linux_v2_native_witness_test.go:3075-3097` (`linuxV2OpenNamespace` user+net, `:3416-3418`), `:3046,:3136` (`platform.ProcessIdentity(target.PID)`, same gate through `/proc/<pid>/ns/pid`) | Same class, latent (no recorded run reaches `linuxV2CaptureLiveProcess`) |
| Invariants: gate/B/one-use RELEASE untouched, `gate.c` byte-identical | `daemon/native/linux-gate/c-gate/gate.c`, `gate_census_linux_test.go` | Not touched by any step below; no new A-visible channel to B's `mm` |
| Invariants: A gains no capability, no DAC bypass | `scripts/ci/linux-v2-native-acceptance.sh` A transient units | The reader is root-run and target-bounded (§2.3); A's credentials, units and `RunGoTest` env gain no capability |
| Acceptance: local gates | gofmt, `go vet` (linux) | §5 rows |
| Acceptance: WSL reproduction | `findings/probe-gate-sequence.py` shape | §5 rows (in-package reproduction + mutant) |
| Acceptance: conductor Bind clears `:252`/`:451` | run log rows | Fixture reader step + converted assertions (§3, §4) |
| Owned: e2e `ns/*` census | `production_linux_v2_native_witness_test.go`, `production_linux_v2_owner_cli_test.go`, `production_linux_v2_helper_exit_witness_test.go` | §3.2 census table |
| Owned: plan/evidence/TODO | `docs/plans/`, `docs/evidence/`, `docs/TODO.md`, `docs/archive/CLOSED-TODO.md` | this file, `docs/evidence/rf-acc-b-process-readback-2026-11-26.md` |

## 2. Mechanism (contract Change (a); the one mechanism, justified)

### 2.1 Why (a), why not (b)/(c)

- (b) B self-report through an existing trusted channel: the clone case's B is the test binary and
  already writes `B_READY`/`B_COMMIT` on the transferred stdout, but the boundary case's B is
  `/usr/bin/cat` and has no such channel; the contract's invariant wants one mechanism and
  byte-exact FD census for both. A test-helper self-report also cannot prove the *boundary* case's
  claim (that the trusted image retained nothing) without changing what that leg executes.
- (c) production changes: ruled out by the contract (`gate.c` byte-identical, no production wire
  field); the root cause (§5) shows no gate-side change can make B's files A-readable.
- (a) fixture-level root reader: the acceptance fixture already runs root steps (`machine_run`,
  `machine_run_as`, root go-test legs) and already stages a root-run guest helper with a
  request/ready exchange (`literal-pid-reuse-helper.c`, `start_literal_pid_reuse_watcher`). The
  reader reuses that shape: a root-run helper inside the guest, a root:A channel directory, a
  ready-file handshake, and an explicit stop with a clean-exit proof.

### 2.2 What A can and cannot read (measured; WSL Ubuntu 24.04 kernel 6.18.58)

Measured against a userns-dropped child (map `1 52001 1`, dropped to ns uid/gid 1, exec'd, dumpable
so all entries are owned by 52001:52001), reader process plain A (uid 52000, CapEff 0) and A with
`CAP_SYS_PTRACE` retained:

| read | plain A | A + CAP_SYS_PTRACE |
| --- | --- | --- |
| `/proc/<pid>/status`, `stat`, `cmdline`, `mountinfo`, `uid_map`, `gid_map` | OK | OK |
| `/proc/<pid>/environ` (0400, owner B) | DENIED (EACCES) | DENIED (EACCES, DAC) |
| `/proc/<pid>/fd` dir (0500, owner userns-root→global root) and readlinks | DENIED (EACCES) | DENIED (EACCES, DAC) |
| `/proc/<pid>/fdinfo/<fd>` (0444, `proc_fdinfo_permission`) | DENIED (EACCES, ptrace) | OK |
| `/proc/<pid>/exe` follow/readlink (`call_proc_get_link`, `PTRACE_MODE_READ_FSCREDS`) | DENIED (EACCES) | OK |
| `/proc/<pid>/ns/{user,net,pid}` stat/readlink (`fs/proc/namespaces.c`) | DENIED (EACCES) | OK |
| `/proc/<pid>/{maps,auxv}` | DENIED | — |

The retained CI run's `/proc/<pid>/exe` follow *passed* for the guest's A (the failure rows are the
later `environ` opens), which upstream kernels gate on `PTRACE_MODE_READ_FSCREDS`; the reader step
therefore prints A's `CapEff/CapPrm/CapBnd/NoNewPrivs` readback (a diagnostic, not an assertion) so
the residual that depends on that question is measured by the Bind run instead of assumed.

### 2.3 Reader bounds (no capability, no DAC bypass)

Root-run, started and stopped by the fixture for each case set, `RuntimeMaxSec` bounded, and it
serves only requests that satisfy all of:

1. request file: regular, single-linked, owner A, mode 0600, ≤ 64 KiB, name `[0-9a-f]{32}.req.json`
   (written temp+rename, so a partial file is never parsed);
2. `pid > 1`, `start_time > 0`, canonical `boot_id`, optional exact `linux:v2:…` identity,
   `leaf` absolute/clean below the fixed `--leaf-root`, `executable` absolute/clean;
3. a pidfd for the PID (opened by the reader; `ESRCH` ⇒ `target_missing`) then, in order: boot ID
   equals the request, `/proc/<pid>/stat` start time equals the request, the recomputed
   `linux:v2` identity (target PID-namespace inode, PID, start time, boot ID) equals the request's
   when given, `/proc/<pid>/status` uids/gids are exactly the configured B, the PID is in the
   named leaf's `cgroup.procs`, `/proc/<pid>/exe` dev/ino equals the request's expected values;
   the executable expectation is optional as a group (§8.1); the reader always reports the exe it
   observed;
4. re-check of start time and pidfd liveness after the reads; environ ≤ 256 KiB, ≤ 4096
   descriptors; namespaces restricted to `user`, `net`;
5. any mismatch is a typed refusal (`request`, `target_missing`, `identity`, `credentials`, `leaf`,
   `executable`, `read`, `limit`) written as the response — never a partial capture and never a
   skip.

Only the operator-created leaf subtree can be named, and only B-credentialed members of it are
read: A cannot migrate or create B- or root-credentialed processes, so the reader is not an
arbitrary-target oracle. The reader never logs file contents (only stage/pid/size counters).

## 3. Public/API shape

### 3.1 New package and helper binary

- `daemon/internal/linuxreadback/` (test-support package, imported only by tests):
  - `readback.go`: request/capture/descriptor/namespace types, the closed refusal vocabulary,
    `Capture(channel string, want Want, timeout time.Duration) (Capture, error)` (A-side client),
    `CheckEnvironment(capture, expected []string) error` (byte-exact, the assertion the tests and
    the mutant row share), channel/file constants and bounds.
  - `reader_linux.go` (linux&&amd64): `Serve(ctx, ServeOptions) error` — the root-side loop above.
  - `reader_unsupported.go` (other platforms): `Serve` returning `errors.ErrUnsupported`, the
    `linuxpreflight`/`tcp_peer_v2_unsupported` precedent, so `go build ./...` stays green off-linux.
  - `local_repro_linux_test.go` (linux&&amd64): env-gated `TestReadbackLocalReproduction` (root,
    WSL) with the A-side client child and the userns-dropped B child; skipped unless
    `SYMMETRY_LINUX_READBACK_LOCAL_REPRO=1`.
- `daemon/internal/linuxreadback/cmd/linux-v2-readback-reader/main.go`: `serve` subcommand for the
  fixture (`--channel`, `--leaf-root`, `--a-uid`, `--a-gid`, `--b-uid`, `--b-gid`,
  `--max-requests`, `--ready-file`, `--stop-file`), flags validated, ready file
  (`ready\n`, root:A 0440) written after the channel is usable, clean exit on the root-owned stop
  file.

### 3.2 e2e census — same-class A→B `/proc` reads, per site

| # | site | gate | disposition |
| --- | --- | --- | --- |
| 1 | `clone_integration_linux_test.go:712` `/proc/<B>/environ` | ptrace + DAC | **converted** (reader environ) |
| 2 | `clone_integration_linux_test.go:727-757` `/proc/<B>/fd` dir + readlinks | ptrace + DAC (0500) | **converted** (reader descriptor census) |
| 3 | `clone_integration_linux_test.go:697` `/proc/<B>/exe` stat | ptrace | **converted** (reader executable binding) |
| 4 | `launcher_integration_linux_test.go:1379` `/proc/<B>/environ` | ptrace + DAC | **converted** |
| 5 | `launcher_integration_linux_test.go:1384` `/proc/<B>/exe` stat | ptrace | **converted** |
| 6 | `launcher_integration_linux_test.go:1390-1410` `/proc/<B>/fd` dir + readlinks | ptrace + DAC | **converted** |
| 7 | `production_linux_v2_native_witness_test.go:3075,3080,3085,3089` `/proc/<B>/ns/{user,net}` | ptrace | **converted** (reader namespace capture) |
| 8 | `production_linux_v2_native_witness_test.go:3046,3136` `platform.ProcessIdentity(B)` (`/proc/<B>/ns/pid` readlink) | ptrace | **converted** (reader identity; the request carries the journal's exact identity) |
| 9 | `production_linux_v2_native_witness_test.go:3179-3258` (`/proc/<B>/fdinfo/*` + identity rechecks) and `:3260-3324` (`linuxV2RequireNoWritableCgroupFDsInLeaf`; the leaf-descendant `platform.ProcessIdentity` at `:3277` reads `/proc/<B>/ns/pid`) | ptrace | **exempt, deferred**: the descendant identity is the same ns/pid class as converted rows 7-8, outside plan §7.2 option (i)'s conversion surface; reached only after the capture path that has never run green; converting the leaf-wide audit needs the reader's per-FD `flags`/`mnt_id` return plus a leaf-wide capture loop and preserves its race tolerances; residual named in `docs/TODO.md` with the reader step's A-capability row as the discriminator |
| 10 | `production_linux_v2_native_witness_test.go:2724,2771` (detached B child), `production_linux_v2_owner_cli_test.go:3474-3488` (`linuxV2OpenExactPIDFD` + `linuxV2ReadPIDNamespaceIdentity`; the ns/pid read is `:3603`), `production_linux_v2_helper_exit_witness_test.go:427` (`platform.ProcessIdentity(gate.PID)`, ns/pid), `:431` (`platform.CaptureLinuxUserNamespaceIdentity`, ns/user), `:466` (post-release gate leaf audit) and `:489` (released-B exe readlink) | ptrace | **exempt, deferred**: same ptrace-gated class, outside plan §7.2 option (i)'s named conversion surface; no green run reaches them, and at `CapEff 0` the helper-exit witness's first stop is its `:427` ns/pid read, before the `:466` leaf audit; same residual entry |
| 11 | `production_linux_v2_owner_cli_test.go` root PID-reuse watcher/replacement targets: `runProductionLinuxV2LiteralPIDReuse`'s `platform.ProcessIdentity`/`linuxV2OpenExactPIDFD`/`linuxV2ReadPIDNamespaceIdentity` reads (`:2843`, `:2855`, `:2863`, `:2894`, `:2941`) and `linuxV2OpenPIDReuseExchange`'s exe stat + identity + pidfd binding (`:3008`, `:3023`/`:3026`, `:3028`/`:3029`, `:3039`) | ptrace | **exempt, deferred**: ptrace-gated class, but the target is the root-run clone3 replacement/watcher (`:3018-3021` requires real/effective uid 0), not the B class the reader serves (`reader_linux.go:484-490` refuses any non-B credential vector), so converting needs a second root-target reader contract; the leg is reached only by `run_literal_pid_reuse_witness` in `shared` (`linux-v2-native-acceptance.sh:3177`, after that set's recorded stop); same residual entry |
| 12 | `production_linux_v2_owner_cli_test.go:4229` `platform.ProcessIdentity(live.PID)` (B target, `runProductionLinuxV2MigrationDenial`) | ptrace | **exempt, deferred**: same B class as converted rows 7-8, but outside plan §7.2 option (i)'s named capture-path surface and unreached by any recorded run (the `migration` set's `native_release` hold is still unreached, `docs/TODO.md`); conversion must carry the channel into that leg and preserve its live-pidfd race tolerance; same residual entry |
| 13 | `platform.ProcessIdentity`/`fd` reads whose targets are A-credentialed (`anchor/helper/mirror/launcher/daemon`, parked gate, owner-death helpers), `linuxV2Cgroup2MountIDs` (`mountinfo`), `linuxV2ReadProcessCredentials`/`Groups` (`status`/`stat`), `cmdline` | none / measured A-legal | **exempt: not the same class or measured A-legal** (§2.2) |

### 3.3 Fixture reader step (`scripts/ci/linux-v2-native-acceptance.sh`, reader step only)

- build `symmetry-ci-linux-v2-readback-reader` in the existing off-host build stage; stage it at
  `/usr/libexec/symmetry-test/linux-v2-readback-reader` (root:root 0555, parent dirs pinned like
  the PID-reuse helper's);
- runtime channel `/run/symmetry-readback` (root:A 0770, created inside the guest because `/run` is
  a boot-time tmpfs);
- `start_readback_reader()`: `systemd-run --no-block` transient unit
  (`Restart=no`, `KillMode=control-group`, `TimeoutStopSec=30s`; no `RuntimeMaxSec` — the workflow's
  `timeout-minutes: 180` is the only bound around the case sets, so a 3600 s runtime cap would fire
  on a slow-but-valid `shared` and turn the stop proof into `Result=timeout`, plan §8.5), then the
  ready proof: unit active, `MainPID` real and its `/proc/<MainPID>/exe` identical to the staged
  helper, `ready` file root:A 0440 equal to `ready\n`, and the printed A capability readback row;
- `stop_readback_reader()`: `systemctl stop` the unit (SIGTERM; R4 makes a cancelled context the
  reader's clean exit), wait for the unit to leave `active` with `Result=success` and
  `ExecMainStatus=0`, assert the ready file is gone (the reader removes it), print the bounded
  reader journal tail; no root write ever enters the channel;
- `run_go_test` gets one `--setenv=SYMMETRY_LINUX_OWNER_READBACK_CHANNEL=$READBACK_CHANNEL` in the
  common environment block (every mode; unused by modes that never request a capture);
- both calls sit around the `case "$fixture_case_set"` dispatch, so every case set that reaches any
  converted leg has the reader running.

## 4. Converted assertions (byte-exact preserved)

- `assertCloneNativeBProcess`: PID/start identity (`stat`), status credentials, `uid_map`/`gid_map`
  stay A-side (measured A-legal). The reader request carries `gate.Identity()`'s PID/start
  time/boot ID, the leaf, and the A-observed `nativeImage` dev/ino; the capture must return the
  exact `environ` bytes (compared with `CheckEnvironment` against the release vector including the
  trailing NUL) and the descriptor census (numbers + readlink targets) that the existing checks
  then apply unchanged (`0..2` stdio set, no extra descriptor re-targeting stdio, forbidden
  authority targets).
- `assertNativeBProcess`: same, with the empty vector and the stdio-only descriptor policy.
- e2e `linuxV2CaptureLiveProcess`: the journal's `target.Identity` is sent with the request; the
  capture's identity must equal it (replacing the two `platform.ProcessIdentity` reads), and the
  `user`/`net` namespaces come from the capture with non-zero device/inode. The pidfd liveness,
  status/cmdline/leaf/checkout checks and the leaf audit are unchanged.

## 5. Evidence plan (rows recorded in the evidence record)

- `gofmt -l`, `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go vet ./...` in `daemon/`, and
  `go test -c` compile rows for `./native/linux-gate/protocol`, `./e2e`,
  `./internal/linuxreadback` (artifacts deleted; never `-o /dev/null`).
- `bash -n` and `shellcheck --severity=warning` on the fixture; `python3
  scripts/ci/test-linux-v2-evidence.py` in WSL (it extracts `print_witness_output_lines` and the
  case-set lines from the fixture by exact text).
- WSL reproduction (root, kernel 6.18.58, cgroup2, userns): the env-gated in-package test builds
  the userns-dropped child, runs the reader (in-process and, when
  `SYMMETRY_LINUX_READBACK_LOCAL_READER_BINARY` is set, the built helper), and the A-side client
  asserts: `A_DIRECT_ENVIRON=denied`/`A_DIRECT_FD=denied`, `READER_CAPTURE=ok` (environ bytes and
  descriptor census verified with the same `CheckEnvironment` the tests use), the mutant
  `MUTANT_ENV_BYTE=red` (one corrupted expected byte turns the shared assertion red), the
  `PID_MISMATCH` identity refusal, the refusal rows (`REFUSAL_CREDENTIALS`,
  `REFUSAL_LEAF_OUTSIDE`, `REFUSAL_LEAF_MEMBERSHIP`, `REFUSAL_EXECUTABLE`) and `RESPONSE_LIMIT`
  (a long-target descriptor census past the response bound gets the typed `limit` refusal).
- Conductor-side (recorded, not run here): the Bind run's `shared` reader step rows (ready/clean
  exit, capability readback) and the two cleared stop lines with the next stop verbatim.

## 6. Out of scope (with reason)

- Production code, `gate.c`, the launcher, AppArmor, sysctls: no production byte changes
  (contract invariants; root cause §5).
- The deferred e2e sites of §3.2 #9-#12 and their residual: out of the contract's named
  `ns/*` conversion surface, unreachable in any recorded run, and their conversion needs a second
  reader return channel plus preserved race tolerances; named in `docs/TODO.md` so the increment
  that first reaches them converts them with attacked evidence.
- B's dumpability after exec (side finding of the root cause, not required by this contract).
- WSL2 unsupported-peer-host behaviour and `TestInstalledLinuxOwnerUnsupportedPeerHostRefusesNative`
  (unchanged; its A-side `readUserNamespace` stays the typed failure it pins).

## 7. Ambiguities resolved (recorded rather than guessed silently)

1. Mechanism: (a) as ruled, but implemented as a long-lived root helper service rather than a
   per-leg root step, because the target B process exists only during the A-credentialed leg; the
   fixture's own root-run guest helper pattern (`start_literal_pid_reuse_watcher`) supplies the
   precedent. The alternative — the test re-execing a root reader — has no guest sudo path (the
   root cause records that A has no sudoers entry), so it is not available.
2. e2e conversion breadth: options were (i) convert the named `ns/*` sites plus the capture-path
   identity, deferring the fd-set/identity audit, or (ii) convert the complete e2e live-observation
   path. (i) is chosen: it is the contract's named surface, it is the only version whose diff stays
   within what this increment can compile-check and bind, and the deferred sites are enumerated
   above with the trigger that unblocks them. Recommendation if the owner wants the wider change:
   it consumes the same reader with an added per-FD `flags`/`mnt_id` return and a leaf-wide capture
   loop.
3. Mutant home: the shared `CheckEnvironment` is exercised by the env-gated WSL reproduction with a
   single corrupted expected byte, because the real native tests cannot run off the installed
   fixture.

## 8. Amendments recorded during implementation

### 8.1 Executable expectation is optional (native tests bind it; e2e observes it)

The native tests have A's own stat of the approved image, so their request binds
`executable` + dev/ino and the reader refuses any other executable. The e2e observer cannot read
B's magic link (`PTRACE_MODE_READ_FSCREDS`) and must not gain that read, so its request omits the
expectation and the reader reports the observed exe path/dev/ino instead; the e2e binding remains
PID + start time + boot ID + the journal's exact `linux:v2` identity + B credentials + leaf
membership. Bounds and refusals are otherwise unchanged.

### 8.2 Local row substitutions (recorded, not silent)

- The in-package reproduction's child maps `0 0 1` beside the B extent `1 <B> 1`: a
  `clone(CLONE_NEWUSER)` + exec loses the namespace capability set while the creator uid is
  unmapped (measured), so the child re-execs itself after the map is written and then drops to
  namespace uid/gid 1. The exact single-extent gate map is measured separately by
  `findings/probe-gate-sequence.py` and the visibility matrix, which both run in the same kernel.
- The fixture's reader step is validated against real systemd off-guest (unit start, ready-file
  mode/owner/helper-identity proof, root-owned stop file, `Result=success` + `ExecMainStatus=0` +
  ready-file removal) because the CI guest itself only exists on the runner; the stop file is
  replaced by the SIGTERM stop in §8.5.

### 8.3 Apply-stage amendments (simplification findings)

- §3.1's flag list shrinks to the six flags every invocation passes: `--ready-file`, `--stop-file`
  and `--max-requests` were never passed and are deleted. The ready/stop markers stay at
  `<channel>/ready` / `<channel>/stop` (the stop marker is later deleted in §8.5), the reader logs
  to stderr, and the request budget is the single compile-time `maxRequests` (4096) bound in the
  package.
- §3.1's A-side client sketch `Capture(channel string, want Want, timeout time.Duration)` is
  implemented as `Read(channel string, want Want, timeout time.Duration) (Capture, error)`:
  `Capture` is the capture type, so the sketched name cannot exist in Go; callers use `Read`.
- §2.3 item 3 lists boot ID before start time; `readTarget` checks the start time first and the
  boot ID second. Both bindings exist and both refuse `stage=identity`; only the refusal reason
  when both mismatch differs, accepted as implemented rather than reordering the code.

### 8.4 Fix-stage amendments (review R1-R5)

- `Serve` treats a cancelled context (SIGTERM/SIGINT) as the clean stop it is for the fixture: it
  logs `stop signal requests=N` and returns nil, so the unit records `Result=success` with
  `ExecMainStatus=0` and only a real failure exits non-zero (R4).
- The encoded-response bound is no longer a silent drop: `publishResponse` refuses `stage=limit`
  when the encoded capture exceeds `maxResponseBytes`, and `serveRequest` turns that typed error
  into the same refusal file the client decodes, so an oversized census receives the typed `limit`
  refusal instead of the client's 60 s timeout (R5).
- The env-gated reproduction adds refusal rows (`credentials`, leaf-outside `request`, leaf
  membership `leaf`, `executable`), a wide-descriptor target whose long-target census pushes the
  encoded capture past the response bound (R2, R5), and an in-process stop that requires a nil
  return (R4 pin).

### 8.5 Fix-stage amendments, PR #296 round 1 (S1-S4)

- The reader resolves every channel name through one root-opened dir fd: openat with
  `O_CREAT|O_EXCL|O_NOFOLLOW` for the ready file and response temps, `O_NOFOLLOW|O_NONBLOCK` for
  requests, `fstat` on the created/opened fd (REG, nlink 1, expected owner), fd-only
  fchmod/fchown, `Renameat`/`Unlinkat` on the dir fd (S1); the reproduction adds the adversarial
  rows (`SYMLINK_RESP_TEMP`, `SYMLINK_REQUEST`, `FIFO_REQUEST`, `HARDLINK_REQUEST`,
  `FIFO_RESP_TEMP`) with the old-bytes red run and the five removal mutants recorded in the
  evidence record §3.1/§6.
- The stop file is gone: `stop_readback_reader` uses `systemctl stop` (SIGTERM, R4 nil-on-cancel),
  so no root-side write ever enters the A-writable channel; `RuntimeMaxSec` is also gone because
  the workflow's `timeout-minutes: 180` is the only bound around the case sets and a 3600 s cap
  would fire on a slow-but-valid `shared` (S4; §3.3).
- The new `production_import_guard_linux_test.go` runs `go list -deps ./cmd/...` and fails if any
  production cmd reaches `linuxreadback` (S2), with the red mutant recorded in evidence §6.
- `readback_helpers_linux_test.go` drops the `capture.Executable` string equality and keeps the
  dev/ino binding, because the reader's gate is dev/ino and the returned path is the kernel's
  canonical readlink (S4; evidence §3).

### 8.6 S5/S6 (round 2/3; protocol test files)

The diagnostic Bind's new stop was root-caused to the gate's fd exec: `execveat(..., AT_EMPTY_PATH)`
makes `AT_EXECFN` `/dev/fd/<n>`, and Ubuntu 26.04's uutils multicall exits 1 with
`coreutils: unknown program '4'` before reading anything, so the boundary leg's `/usr/bin/cat`
subject died and the reader correctly refused `stage=leaf`. The fix releases a staged
single-purpose ELF (the test binary) for that leg and adds its stdin-blocking helper; S6 flips two
`MSG_EOR` expectations to the repo's never-set convention. The evidence record's §7 S5/S6 sections
carry the root cause, the exact red→green strings and the replica run rows (`findings/296-s5.md`);
no fixture change was needed and the reader's leaf check is unchanged.
