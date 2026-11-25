# rf-acc-b-process-readback — evidence record

Goal `rf-acc-b-process-readback` (contract: `goal-rf-acc-b-process-readback.md` under the GDD
contracts root; dispatch gate main `8453da12`). Branch `refactor/rf-acc-b-process-readback`, base
`8453da12`. Plan: `docs/plans/2026-11-26-rf-acc-b-process-readback-plan.md`. Root cause and the
recorded read matrix: `findings/environ-root-cause.md` (EnvironRoot). All paths are relative to the
repository or to the GDD state root. Local rows ran on WSL Ubuntu-24.04 (kernel
`6.18.58.1-microsoft-standard-WSL2`, WSL2 shares the host kernel with the other installed
distro); gates that must not depend on host layout ran on the pinned toolchain
(`go1.27.4 windows/amd64` cross-compiling to `linux/amd64`).

## 1. Delivered files and digests

| file | sha256 at the delivered bytes |
| --- | --- |
| `daemon/internal/linuxreadback/readback.go` | `780970abf799073fba9d114b6632ed20260cc5c25b845c97eac3d6e0e49298dd` |
| `daemon/internal/linuxreadback/reader_linux.go` | `29f0c2c1321c3ef205efd18b5f4fb67369ebd727f0e39b0eed1b2f706353b2d9` |
| `daemon/internal/linuxreadback/reader_unsupported.go` | `9cb3aea49d3d12d4414e93c6c52e069e91f34eb5666f44e3bf44e4a1d4fd93ba` |
| `daemon/internal/linuxreadback/local_repro_linux_test.go` | `0802c8a9f52181646bb4ab62a2a7fd9f3a1dab6b53eb88e69a003214734c01bc` |
| `daemon/internal/linuxreadback/cmd/linux-v2-readback-reader/main.go` | `d740a787549f2b6cf676f47fe2c6a4a9a70a7a19f8f28df9a581291275c0f8ae` |
| `daemon/native/linux-gate/protocol/readback_integration_linux_test.go` | `170ea85fdeda954892543d5c05eab1b7f58478b21bfd0de715c95ccfb49a3de8` |
| `daemon/native/linux-gate/protocol/clone_integration_linux_test.go` | `4ce7d2216f990c9c67d24be8944249c906871a571db7a53c4e5086a72aeb9d5b` |
| `daemon/native/linux-gate/protocol/launcher_integration_linux_test.go` | `11629ac47f874cc1dc7781eaa94b7b69dd77f1e2fe08b731aa5fa565d55e36f4` |
| `daemon/e2e/production_linux_v2_native_witness_test.go` | `3c26bf3f729342b8e088ba9470236485f09cb6924bf05267df7cb44243e3e558` |
| `scripts/ci/linux-v2-native-acceptance.sh` | `ff40b2b1d88b7fbb2266fabf7d29bfd50427424b329cd767dc39e9b007b529f1` |
| `docs/plans/2026-11-26-rf-acc-b-process-readback-plan.md` | `65f4d3407e183ae59c7b8b2582b7b8fd39e3316855c054c89d421f470e78510e` (record revision) |

The readback digests above are the post-simplification revision: the first pass had the
channel/bound names exported, and the scoped simplification review unexported the names no caller
outside the package uses, then trimmed the two comment runs the contract's two-line bound refuses
(the reproduction rows and gates were re-run at these digests, §6). The same review dropped a
`readlink`-vanished-descriptor tolerance from the FD census: the inline census it replaces treated
any `readlink` failure as a hard test error, so the reader refuses with `stage=read` instead of
censusing a shorter table — base parity, no weakened predicate.

No production byte changed: `gate.c`, the launcher, the wrapper, the AppArmor profile, the sysctls
and `daemon/internal/**` non-test code are untouched; the new package is imported only by test
files and by the fixture-built helper binary. `docs/TODO.md`, `docs/archive/CLOSED-TODO.md` and this
record are deliberately not hashed.

Scope additions beyond the contract's file list (ratification requested, analogous to
`rf-acc-apparmor-userns-profile`'s R2): `daemon/internal/linuxreadback/**` (the reader/channel
package and its fixture-only `cmd`), and
`daemon/native/linux-gate/protocol/readback_integration_linux_test.go` (the shared request helper).
They are test-support code: no production package imports the readback package, and the fixture
builds the helper binary only for the disposable guest.

## 2. Mechanism and bounds

Contract Change (a): the fixture starts a bounded root reader inside the guest for the whole case
set (`scripts/ci/linux-v2-native-acceptance.sh`, `start_readback_reader`/`stop_readback_reader`);
A requests a capture over a root:A mode-0770 channel and the reader answers with the exact bytes or
a typed refusal. Plan §2.3 is the bound list: A-owned single-linked 0600 request files only;
`pid > 1`, start time, boot ID, the exact `linux:v2` identity, B's four-uid/four-gid status vector,
membership in an operator leaf below the fixed `--leaf-root`, and (when supplied) the A-approved
executable dev/ino; a pidfd plus before/after start-time and liveness re-checks bind the reads to
one process; environ ≤ 256 KiB, ≤ 4096 descriptors, namespaces limited to `user`/`net`. The reader
never logs file content. A's credentials, its systemd units, and the gate's one-use RELEASE are
unchanged.

## 3. Census — same-class A→B `/proc` reads (plan §3.2 carries the dispositions)

Measured on WSL kernel 6.18.58.1 against a userns-dropped child (map `1 52001 1`, dropped to
namespace uid/gid 1, exec'd, dumpable; all entries owned by 52001:52001), as plain A (uid 52000,
`CapEff 0`) and as A with `CAP_SYS_PTRACE` retained (`probe-visibility.py`,
`probe-a-with-ptrace.py` in the retained probe directory):

| read | plain A | A + CAP_SYS_PTRACE |
| --- | --- | --- |
| `status`, `stat`, `cmdline`, `mountinfo`, `uid_map`, `gid_map` | OK | OK |
| `environ` (0400 owner B) | DENIED (EACCES) | DENIED (EACCES, DAC on the 0400 inode) |
| `fd` directory (0500 owner userns-root→global root) + readlinks | DENIED (EACCES) | DENIED (EACCES, DAC on the 0500 directory) |
| `fdinfo/<fd>` (0444, `proc_fdinfo_permission`) | DENIED (EACCES, ptrace) | OK |
| `exe` follow/readlink (`call_proc_get_link`, `PTRACE_MODE_READ_FSCREDS`) | DENIED (EACCES) | OK |
| `ns/{user,net,pid}` stat/readlink (`fs/proc/namespaces.c`) | DENIED (EACCES) | OK |
| `maps`, `auxv` | DENIED | — |

`findings/probe-gate-sequence.py` re-run on the same kernel reproduces the exact gate shape and the
recorded rows verbatim (root OK; A denied; A+`sys_ptrace,dac_read_search` OK; A `stat
/proc/<pid>/ns/user` EACCES). The retained CI run shows the guest's A passing the `/proc/<pid>/exe`
follow before failing on `environ`; upstream kernels gate that link, so the guest's A transient
units carry an effective ptrace capability today. `start_readback_reader` therefore prints A's
`NoNewPrivs/CapEff/CapPrm/CapBnd/CapAmb` readback as a diagnostic row so the residual below is
settled by the Bind run rather than assumed; the converted legs never consult it.

Converted sites: the two native assertions' `environ`, `fd` census and `exe` binding (plan rows
1-6), the e2e capture's `ns/user` + `ns/net` and its two `platform.ProcessIdentity(B)` reads (rows
7-8, `linuxV2CaptureReadbackTarget`, the old `linuxV2OpenNamespace` deleted). Deferred with
reasons (rows 9-10): `linuxV2RequireNoWritableCgroupFDs*`'s fdinfo/descendant-identity audit,
`linuxV2CaptureDetachedChild`'s child identity, the pid-reuse/marker `linuxV2OpenExactPIDFD` +
`linuxV2ReadPIDNamespaceIdentity` on a released B, and the helper-exit leaf audit — none is an
`ns/*` read in the contract's named surface, none is reached by any recorded run, and converting
them needs the reader's per-FD `flags`/`mnt_id` return plus a leaf-wide capture loop; the trigger
and the A-capability discriminator are recorded in `docs/TODO.md`.

## 4. Converted assertions (byte-exact preserved)

- `assertCloneNativeBProcess`: unchanged PID/start, credential, capability, NNP and
  `uid_map`/`gid_map` checks stay A-side; the request carries `gate.Identity()`'s PID/start/boot ID,
  the scratch leaf and the A-observed `nativeImage` dev/ino, and the capture must return that image,
  the exact environment bytes (`CheckEnvironment`: join with NUL plus trailing NUL) and the
  descriptor census the existing policy then judges unchanged (stdio set 0..2, no extra descriptor
  re-targeting stdio, the same forbidden authority targets).
- `assertNativeBProcess`: same, with the empty vector and the stdio-only descriptor policy.
- e2e `linuxV2CaptureLiveProcess`: request carries the journal's exact identity; the capture's
  identity must equal it (both former `platform.ProcessIdentity` positions), `ns/user`/`ns/net`
  must be non-zero nsfs identities, and the pidfd liveness/environment/clone/leaf/cmdline audit and
  the deferred leaf audit are unchanged. The e2e request omits the executable expectation (plan
  §8.1): the observer cannot stat B's magic link and the reader reports the observed exe instead.

## 5. Fixture reader step

Build+stage: `go build … ./internal/linuxreadback/cmd/linux-v2-readback-reader` in the existing
build stage; installed at `/usr/libexec/symmetry-test/linux-v2-readback-reader` with the pinned
`0:0:555:1` stat check and the shared root-owned parent-directory checks. Runtime: channel
`/run/symmetry-readback` (root:A 0770, created in-guest because `/run` is a boot tmpfs), transient
unit `symmetry-ci-readback-reader.service` (`Restart=no`, `KillMode=control-group`,
`TimeoutStopSec=30s`, `RuntimeMaxSec=3600`), ready proof (unit active, real `MainPID`, ready file
`0:<A_GID>:440:1` equal to `ready\n`, and `/proc/<MainPID>/exe` identical to the staged helper),
stop proof (root-owned stop file; unit `inactive` with `Result=success` and `ExecMainStatus=0`;
ready file gone; bounded reader journal tail printed). `run_go_test` exports
`SYMMETRY_LINUX_OWNER_READBACK_CHANNEL` in its common environment so every mode has the channel;
the reader is started before the case dispatch and stopped after it, so every case set that reaches
a converted leg has it running.

## 6. Local rows (recorded commands and outputs)

| row | command | result |
| --- | --- | --- |
| gofmt | `gofmt -l .` in `daemon/` | empty (exit 0) |
| vet (linux) | `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go vet ./...` in `daemon/` | exit 0, no output |
| vet (other platforms) | `GOOS=windows GOARCH=amd64 go vet ./internal/linuxreadback/...`; `GOOS=darwin GOARCH=arm64 …` | exit 0 (the `reader_unsupported.go` stub keeps `./...` green) |
| compile changed tests | `go test -c` for `./native/linux-gate/protocol`, `./e2e`, `./internal/linuxreadback` (`-o` into the private probe directory) | exit 0 each; binaries produced, never `-o /dev/null` |
| protocol unit rows (no fixture) | `protocol.test -test.run TestOwnerConfig…\|TestLinuxOwnerReleaseV2…\|TestFakeUserNamespace…\|TestValidateReleaseGuardLive…\|TestNativeExecStatus…` | 5/5 PASS |
| fixture syntax | `bash -n scripts/ci/linux-v2-native-acceptance.sh` | exit 0 |
| shellcheck (advisory CI metric) | `shellcheck --severity=warning --format=gcc scripts/ci/linux-v2-native-acceptance.sh` | baseline `8453da12`: 17 findings; delivered bytes: 17 findings (none in the new functions) |
| fixture contract suite | `python3 scripts/ci/test-linux-v2-evidence.py` (WSL) | `Ran 55 tests … OK` |
| reader unit proof | `unit-proof.sh` (WSL Ubuntu-24.04 root, real systemd) | `root readback reader ready: unit=… MainPID=450468` … `readback-reader: stop requests=0` … `UNIT-PROOF-OK` (the start/ready/stop proof blocks are the fixture's own) |
| WSL reproduction, helper-binary mode | `SYMMETRY_LINUX_READBACK_LOCAL_REPRO=1 SYMMETRY_LINUX_READBACK_LOCAL_READER_BINARY=<reader> readback.test -test.run '^TestReadbackLocalReproduction$' -test.v` | `A_DIRECT_ENVIRON=denied`, `A_DIRECT_FD=denied`, `READER_CAPTURE=ok environ_bytes=98 descriptors=9 user_ns=4:4026532241 net_ns=4:4026531833`, `MUTANT_ENV_BYTE=red`, `PID_MISMATCH=refused stage=identity`, `READBACK_LOCAL_CLIENT=ok`, `--- PASS` (exit 0) |
| WSL reproduction, in-process mode | same without the binary variable | identical rows, `--- PASS` (exit 0) |
| gate-shape probe | `python3 findings/probe-gate-sequence.py` | root OK; `read as A(52000) … Permission denied`; `read as B(52001) … Permission denied`; `read as A+2caps: OK`; `A stat ns/user: Permission denied` |
| capability census | `python3 probe-visibility.py` / `probe-a-with-ptrace.py` | the §3 matrix |

The reproduction's child is the gate-shaped userns child of plan §8.2 (the local row maps `0 0 1`
beside `1 <B> 1` so the post-exec drop to the B extent is possible in Go; the exact single-extent
map is measured by the two probes above). The mutant row corrupts one expected environment byte
(`mutated[0][0] ^ 1`) and requires the shared `CheckEnvironment` to redden — the single test the
contract asks for.

Every row above was re-run after the simplification pass at the §1 digests (gofmt/vet/compile, the
protocol unit rows, the fixture syntax/contract suite, the reader unit proof and both reproduction
modes); only the run-specific identifiers differ between passes (`MainPID=450468` → `450765`, the
userns inode numbers, nonces, the child PID).

## 7. Runs

No run was dispatched from this stage: the contract's acceptance binds the conductor's single
scratch/Bind `shared` run on the PR head, and the fixture's reader step, the unit proof and the
conversion rows above are the local evidence that step consumes. Pending in that run: the reader
step's ready/stop rows and A capability readback, and the two cleared stop lines
(`clone_integration_linux_test.go:252` / `launcher_integration_linux_test.go:451`) with the next
stop recorded verbatim.

## 8. Limits and residuals

- The deferred e2e same-class sites (plan §3.2 rows 9-10) are recorded in `docs/TODO.md` with the
  reader extension they need and the A-capability row that decides their urgency.
- The guest's A effective capability set is measured, not changed: if the Bind run shows `CapEff 0`,
  the deferred fdinfo/identity sites become the next wall the moment an e2e leg first reaches them,
  and the residual entry names exactly that trigger.
- `assertCloneNativeBProcess`/`assertNativeBProcess` now require the fixture's reader; a fixture-less
  invocation fails at `ChannelFromEnv`/`scratchLeafPath` instead of the raw read, which is the
  contract's "a reader failure fails the test, never skips it".
- The local reproduction writes its channel under `/tmp` and its leaf under `/sys/fs/cgroup`; it is
  env-gated (`SYMMETRY_LINUX_READBACK_LOCAL_REPRO=1`) and therefore never runs in CI, which
  exercises only the compile rows for it.
- The plan and this record carry record-only edits after the code digests in §1; if a later fix
  stage changes code bytes, the digests and the affected rows must be re-derived.
