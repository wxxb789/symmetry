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
| 9 | `production_linux_v2_native_witness_test.go:3195-3259,3289-3319` `/proc/<B>/fdinfo/*` + leaf-descendant `platform.ProcessIdentity` (`linuxV2RequireNoWritableCgroupFDs*`) | ptrace | **exempt, deferred**: not an ns/`*` read; reached only after the capture path that has never run green; converting the leaf-wide audit needs a second reader return channel (per-FD `flags`/`mnt_id`) and preserves its race tolerances; residual named in `docs/TODO.md` with the reader step's A-capability row as the discriminator |
| 10 | `production_linux_v2_native_witness_test.go:2723,2770` (detached B child), `production_linux_v2_owner_cli_test.go:3474-3488` (`linuxV2OpenExactPIDFD` + `linuxV2ReadPIDNamespaceIdentity` on released B), `production_linux_v2_helper_exit_witness_test.go:466` (post-release gate leaf audit) | ptrace | **exempt, deferred**: same class, not ns/`*` named by the contract, no green run reaches them; same residual entry |
| 11 | `platform.ProcessIdentity`/`fd` reads whose targets are A-credentialed (`anchor/helper/mirror/launcher/daemon`, parked gate, owner-death helpers), `linuxV2Cgroup2MountIDs` (`mountinfo`), `linuxV2ReadProcessCredentials`/`Groups` (`status`/`stat`), `cmdline` | none / measured A-legal | **exempt: not the same class or measured A-legal** (§2.2) |

### 3.3 Fixture reader step (`scripts/ci/linux-v2-native-acceptance.sh`, reader step only)

- build `symmetry-ci-linux-v2-readback-reader` in the existing off-host build stage; stage it at
  `/usr/libexec/symmetry-test/linux-v2-readback-reader` (root:root 0555, parent dirs pinned like
  the PID-reuse helper's);
- runtime channel `/run/symmetry-readback` (root:A 0770, created inside the guest because `/run` is
  a boot-time tmpfs);
- `start_readback_reader()`: `systemd-run --no-block` transient unit
  (`Restart=no`, `KillMode=control-group`, `TimeoutStopSec=30s`, `RuntimeMaxSec` bound), then the
  ready proof: unit active, `MainPID` real and its `/proc/<MainPID>/exe` identical to the staged
  helper, `ready` file root:A 0440 equal to `ready\n`, and the printed A capability readback row;
- `stop_readback_reader()`: write the root-owned `stop` file, wait for the unit to leave `active`
  with `Result=success`, assert the ready file is gone (the reader removes it), print the bounded
  reader journal tail;
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
  asserts: `A_DIRECT_READ=denied`, `READER_CAPTURE=ok` (environ bytes and descriptor census
  verified with the same `CheckEnvironment` the tests use), `PID_MISMATCH=refused`, and the mutant
  `MUTANT_ENV_BYTE=red` (one corrupted expected byte turns the shared assertion red).
- Conductor-side (recorded, not run here): the Bind run's `shared` reader step rows (ready/clean
  exit, capability readback) and the two cleared stop lines with the next stop verbatim.

## 6. Out of scope (with reason)

- Production code, `gate.c`, the launcher, AppArmor, sysctls: no production byte changes
  (contract invariants; root cause §5).
- The deferred e2e sites of §3.2 #9/#10 and their residual: out of the contract's named
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
  ready-file removal) because the CI guest itself only exists on the runner.
