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
| `daemon/internal/linuxreadback/readback.go` | `7653e0dcc3f476599f2f53eefb5c8ac4efb124100dc555531a908f3d591d0619` |
| `daemon/internal/linuxreadback/reader_linux.go` | `0cbdb6a3f723ba476e259f11fd11f15d35c652c99d560d0feea52d35a3d5f791` |
| `daemon/internal/linuxreadback/reader_unsupported.go` | `9cb3aea49d3d12d4414e93c6c52e069e91f34eb5666f44e3bf44e4a1d4fd93ba` |
| `daemon/internal/linuxreadback/local_repro_linux_test.go` | `5d3590beda1e97b4e18bd3ec2aab2fbc28cd953292d2ee34c20d97120e8bd290` |
| `daemon/internal/linuxreadback/cmd/linux-v2-readback-reader/main.go` | `9d5e905930edf91a72bde5717c84617464d89f1f5d306cf4164d0dd82848f1f5` |
| `daemon/internal/linuxreadback/production_import_guard_linux_test.go` | `4d6618e33400dc1bf89a203c0ab510f8ec7bcb80440ce9ca42b3bcab8b3f1dbe` |
| `daemon/native/linux-gate/protocol/readback_helpers_linux_test.go` | `6455746fb8da553fac005fce5e2f059d6fb8427cefe4888f38f731ffe4273b38` |
| `daemon/native/linux-gate/protocol/clone_integration_linux_test.go` | `dfba0761d5048b0b09408b9288fb302ace6a8bdf2e9f6db7e10679fba3b28744` |
| `daemon/native/linux-gate/protocol/launcher_integration_linux_test.go` | `8132b62576ce6d17c046e0fa7e918c172ab82e9586456c9e04c5fd43368bf259` |
| `daemon/e2e/production_linux_v2_native_witness_test.go` | `2403b2f4aa3584d2cfa0b83dfe12e3210a967c0fbf16e1172ba14f005329516b` |
| `scripts/ci/linux-v2-native-acceptance.sh` | `04bb9e8f700bfd672dcf9c7f89cebf1ed2a7b8c5266d77ecf1c2a37c4c882aad` |
| `docs/plans/2026-11-26-rf-acc-b-process-readback-plan.md` | `a7c4dc3e652f36c5c0c263a381a64779837baacf6f9959c0b9c128939ec8fb04` (record revision) |

The readback digests above are the fix revision. The file history: the first pass had the channel/bound names
exported, the scoped simplification review unexported the names no caller outside the package uses
and trimmed the two comment runs the contract's two-line bound refuses, and the apply stage then
deleted the dead `ErrRefused`/`Is` pair, unexported `ChannelEnv`, deleted the three never-set
`ServeOptions` knobs with their flags (`--ready-file`, `--stop-file`, `--max-requests`), hoisted
the request budget to one `maxRequests` bound, replaced the hand-rolled slice lookup with
`slices.Contains`, and renamed the shared request helper from the misleading
`readback_integration_linux_test.go` to `readback_helpers_linux_test.go` (bytes unchanged; the
plan's §8.3 records the flag-list, client-name and check-order amendments). The reproduction rows
and gates were re-run at these digests (§6). The fix stage (review R1-R5) then changed two of
them: `reader_linux.go` (a cancelled context is the clean signal stop and exits 0; an encoded
capture past `maxResponseBytes` becomes the typed `stage=limit` refusal instead of a dropped
response, plan §8.4) and `local_repro_linux_test.go` (the R2/R5 refusal rows with a
wide-descriptor target and the R4 nil-stop pin); their digest rows above are the fix revision,
and every movable §6 row was re-run at it. The PR #296 round-1 fix (S1-S4, plan §8.5) then
changed `reader_linux.go` (the channel dir fd and the exclusive/no-follow/non-blocking channel
opens), `local_repro_linux_test.go` (the five planted-name rows below),
`daemon/native/linux-gate/protocol/readback_helpers_linux_test.go` (dev/ino-only executable
binding), `scripts/ci/linux-v2-native-acceptance.sh`
(`systemctl stop`, no `RuntimeMaxSec`), one comment in
`production_linux_v2_native_witness_test.go`, and added
`production_import_guard_linux_test.go` (S2); the digest rows above are re-measured at the
delivered bytes, and the round-1 rows of §6 were run at those bytes (or at the round-1 bytes for
the red/mutant rows, as §6 records). The simplification review had also dropped a
`readlink`-vanished-descriptor tolerance from the FD census: the inline census it replaces treated
any `readlink` failure as a hard test error, so the reader refuses with `stage=read` instead of
censusing a shorter table — base parity, no weakened predicate.

No production byte changed: `gate.c`, the launcher, the wrapper, the AppArmor profile, the sysctls
and the pre-existing `daemon/internal/**` non-test code are untouched; the new package is imported only by test
files and by the fixture-built helper binary. `docs/TODO.md`, `docs/archive/CLOSED-TODO.md` and this
record are deliberately not hashed.

Scope additions beyond the contract's file list (ratification requested, analogous to
`rf-acc-apparmor-userns-profile`'s R2): `daemon/internal/linuxreadback/**` (the reader/channel
package and its fixture-only `cmd`), and
`daemon/native/linux-gate/protocol/readback_helpers_linux_test.go` (the shared request helper,
renamed from `readback_integration_linux_test.go` by the apply stage; bytes unchanged, digest row
above). They are test-support code: no production package imports the readback package, and the
fixture builds the helper binary only for the disposable guest.

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

## 3. Census (plan §3.2 carries the dispositions; §3.1 is the S1 channel census)

### 3.1 Root-side channel operations (S1; one reason per row)

The delivered bytes resolve every channel name through the one root-held channel directory fd
(`openChannelDir`, `reader_linux.go:74-85`), so no reader operation resolves a channel name by
path after startup. The old bytes' by-path ops (`os.Remove(ready)`, the response temp's
`os.OpenFile(O_CREATE|O_TRUNC|O_WRONLY)`, `os.Open(request)`, `os.ReadDir(channel)`,
`os.Rename(temp, final)`) are all replaced; the old-bytes red rows and the five removal mutants
are §6.

| reader op | site | why an A-planted name is never followed |
| --- | --- | --- |
| stat channel path (startup) | `validateChannelPath` `readback.go:144`, called from `reader_linux.go:154` | one by-path `Stat` before the dir fd opens; the fixed `/run/symmetry-readback` name is root-created under root-only `/run`, so A holds no name there and a non-dir/failed stat fails closed |
| open channel dir | `reader_linux.go:75` | `O_DIRECTORY|O_NOFOLLOW`: A has write only inside the 0770 dir, never over its name (`/run` is root-only), so a swapped channel path is ELOOP |
| unlink any name | `unlink` `:90-92`; call sites `:41,175,183,188,204,246,249,322,345,350,367,371` | `Unlinkat(dirfd, name, 0)` removes exactly the name; it never follows |
| list requests | `requestNames` `:95-109` | `Seek`+`ReadDir` through the held fd; opens no child; `*.req.json`+nonce filter, directories skipped |
| open request | `openRequest` `:121` | `openat(dirfd, name, O_RDONLY|O_NOFOLLOW|O_NONBLOCK|O_CLOEXEC)`: a swapped symlink is ELOOP; a FIFO opens without blocking |
| request identity gate | `readRequestFile` `:260-263` | `Fstat` on the opened fd: REG, nlink 1, owner A:A, mode 0600, 0 < size ≤ 64 KiB |
| read request | `:266` | `io.ReadFull` through the already-gated fd; no second open by name |
| create ready / response temp | `createExclusive` `:141` (via `:176`, `:337`) | `O_CREAT|O_EXCL|O_NOFOLLOW|O_WRONLY|O_CLOEXEC`: any pre-existing name is EEXIST (ELOOP for a symlink), never a write through |
| ready / response identity gate | `:186`, `:348` | `Fstat` on the exclusive-create fd: REG, nlink 1, uid 0 |
| set ready / response owner+mode | `:195,:198`, `:357,:360` | fd-only `fchmod`/`fchown`; there is no by-name chmod/chown anywhere |
| publish response | `:370` | `Renameat(dirfd, temp, dirfd, final)`: rename does not follow, and a swapped temp only moves A's own bytes |
| stop | `Serve` `:45-48` | no channel probe at all: ctx cancellation only (fixture SIGTERM; R4 nil-on-cancel) |

| fixture op (`scripts/ci/linux-v2-native-acceptance.sh`) | site | why an A-planted name is never followed |
| --- | --- | --- |
| create channel dir | `:2146` | `mkdir -m 0770 /run/symmetry-readback`: A holds no name over root-only `/run`, so no plant can pre-exist the create |
| chown channel dir | `:2147` | by-name chown of the just-created dir under root-only `/run`; A cannot replace the name, and entries A creates inside are handled by the reader's unlink/O_EXCL ops above |
| stat channel dir | `:2148-2150` | read-only check |
| A write probe | `:2152` | `test -w` as A; read-only |
| ready proof | `:2181-2184` | `! -L` then `stat`/`cmp` by name: a followed target must still be a root-owned 0440 nlink-1 file with the exact `ready\n` bytes, which only the reader's own file satisfies (A cannot create or hardlink root-owned files), so a forged pass is impossible and a removal fails closed |
| stop | `:2207` | `systemctl stop` (SIGTERM); the old `touch "$READBACK_CHANNEL/stop"` and its `rm -f` are deleted, so no root-side write enters the A-writable channel |
| stop proof | `:2221` | `[[ ! -e "$channel/ready" ]]`; a planted `ready` name fails this check closed |

### 3.2 Same-class A→B `/proc` reads

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
7-8, `linuxV2CaptureReadbackTarget`, the old `linuxV2OpenNamespace` deleted). Deferred rows 9-12
are the same ptrace-gated class but lie outside plan §7.2 option (i)'s conversion surface and are
unreached by any recorded run: `linuxV2RequireNoWritableCgroupFDs`'s fdinfo audit and its in-leaf
descendant `platform.ProcessIdentity` (`/proc/<B>/ns/pid`,
`production_linux_v2_native_witness_test.go:3277`), `linuxV2CaptureDetachedChild`'s child
identity, the pid-reuse/marker `linuxV2OpenExactPIDFD` + `linuxV2ReadPIDNamespaceIdentity` on a
released B (the ns/pid read is `production_linux_v2_owner_cli_test.go:3603`), the post-release
`platform.ProcessIdentity` / `platform.CaptureLinuxUserNamespaceIdentity` / exe readlink plus the
helper-exit leaf audit (`production_linux_v2_helper_exit_witness_test.go:427,431,489,466`), the
S3 owner_cli additions, and the migration witness. Converting the first group needs the reader's
per-FD `flags`/`mnt_id` return plus a leaf-wide capture loop; at `CapEff 0` the helper-exit
witness's first stop is its `:427` released-B ns/pid read. The S3 root-target rows
(`runProductionLinuxV2LiteralPIDReuse` reads `production_linux_v2_owner_cli_test.go:2843,2855,
2863,2894,2941`; `linuxV2OpenPIDReuseExchange` reads `:3008,3023/3026,3028/3029,3039`) are ptrace-gated
but target the root-run clone3 replacement/watcher (`:3018-3021` requires real/effective uid 0) —
not the B class the reader serves (`reader_linux.go:484-490` refuses any non-B vector) — so they
need a second root-target reader contract; row 12's migration witness
(`runProductionLinuxV2MigrationDenial:4229`, a B target) is the reader's class but its
`native_release` hold is still unreached. The trigger and the A-capability discriminator are
recorded in `docs/TODO.md`.

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
`TimeoutStopSec=30s`; no `RuntimeMaxSec`), ready proof (unit active, real `MainPID`, ready file
`0:<A_GID>:440:1` equal to `ready\n`, and `/proc/<MainPID>/exe` identical to the staged helper),
stop proof (`systemctl stop`; unit `inactive` with `Result=success` and `ExecMainStatus=0`; ready
file gone; bounded reader journal tail printed). The `RuntimeMaxSec=3600` cap is removed because
the workflow's `timeout-minutes: 180` (`.github/workflows/linux-native-v2-acceptance.yml:22`) is
the only bound around a case set — there is no per-set timeout to cite — so a 3600 s cap could
fire on a slow-but-valid `shared` and turn the clean-stop proof into `Result=timeout`; the
cancellation stop itself is still bounded by `TimeoutStopSec=30s` (S4). The channel ops this step
performs are the §3.1 fixture rows. `run_go_test` exports
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
| reader unit proof | `scratch/rf-acc-b-fix/unit-proof-fixed.sh` (WSL Ubuntu-24.04 root, real systemd) | `root readback reader ready: unit=… MainPID=491566` … `readback-reader: stop requests=0` … `UNIT-PROOF-OK` (the start/ready/stop proof blocks are the fixture's own) |
| SIGTERM stop probe | `scratch/rf-acc-b-fix/sigterm-probe.sh <reader>` (WSL root: `systemd-run` transient unit, then `systemctl stop`) | pre-fix `reader-baseline`: `state=failed result=exit-code exec_main_status=1`; fix revision: `state=inactive result=success exec_main_status=0`, journal `readback-reader: stop signal requests=0` (R4) |
| WSL reproduction, helper-binary mode | `SYMMETRY_LINUX_READBACK_LOCAL_REPRO=1 SYMMETRY_LINUX_READBACK_LOCAL_READER_BINARY=<reader> readback.test -test.run '^TestReadbackLocalReproduction$' -test.v` | `A_DIRECT_ENVIRON=denied`, `A_DIRECT_FD=denied`, `READER_CAPTURE=ok environ_bytes=98 descriptors=9 user_ns=4:4026532219 net_ns=4:4026531833`, `MUTANT_ENV_BYTE=red`, `PID_MISMATCH=refused stage=identity`, `REFUSAL_CREDENTIALS=refused stage=credentials`, `REFUSAL_LEAF_OUTSIDE=refused stage=request`, `REFUSAL_LEAF_MEMBERSHIP=refused stage=leaf`, `REFUSAL_EXECUTABLE=refused stage=executable`, `RESPONSE_LIMIT=refused stage=limit`, `READBACK_LOCAL_CLIENT=ok`, `--- PASS` (exit 0) |
| WSL reproduction, in-process mode | same without the binary variable | identical rows plus `readback-reader: stop signal requests=7`, `--- PASS` (exit 0) |
| fix-round-2 reproduction, delivered bytes (restore run) | `scratch/fix296r2/run-repro.sh` (WSL root, reader-binary mode, `SYMMETRY_LINUX_READBACK_LOCAL_REPRO=1`) | all rows hold, including `SYMLINK_RESP_TEMP=refused victim=intact`, `SYMLINK_REQUEST=refused stage=request`, `FIFO_REQUEST=refused stage=request`, `HARDLINK_REQUEST=refused stage=request`, `FIFO_RESP_TEMP=refused not-blocked`, `READBACK_LOCAL_CLIENT=ok` (`scratch/fix296r2/pristine.log`, exit 0) |
| red-first, old reader bytes, full client | same test file over a scratch tree whose only old file is `f30f3ef8:reader_linux.go` (sha256 `702bd2cbb341488c29896cb6ca7065c200fb43d1075a400e237f4885ca01784f`) | `READBACK_LOCAL_FAIL: the reader published through the planted response-temp symlink`; test `FAIL` exit 1 — the old bytes follow the planted response-temp symlink (`scratch/fix296r2/red-old-bytes.log`) |
| red-first, old bytes, row-isolated (`scratch/fix296r2/run-isolated-old.sh`, `isolate-row.py`) | each adversarial row alone against the old bytes | `SYMLINK_RESP_TEMP`: FAILs `the reader published through the planted response-temp symlink`; `SYMLINK_REQUEST`/`FIFO_REQUEST`/`HARDLINK_REQUEST`: already refused by the old `Lstat`+nlink gate (green on the old bytes — these rows pin the new code and their red proofs are the mutants; `red-SYMLINK_REQUEST.log`, `red-FIFO_REQUEST.log`, `red-HARDLINK_REQUEST.log`, exit 0); `FIFO_RESP_TEMP`: FAILs `the reader blocked on the planted response-temp FIFO` (the old `O_CREATE|O_TRUNC|O_WRONLY` open hangs; `red-FIFO_RESP_TEMP.log`, exit 124) |
| channel-op mutants, one at a time on the delivered bytes (`scratch/fix296r2/run-mutants.sh`, `mutate.py`) | each single removal re-applied to a fresh copy, rebuilt, run | `no-nofollow-request`: `SYMLINK_REQUEST` FAILs `reader answered the planted name with a capture`; `no-excl-response`: rows 1-4 stay green and `FIFO_RESP_TEMP` FAILs `the reader blocked on the planted response-temp FIFO` (O_NOFOLLOW alone still refuses the symlink); `no-nonblock-request`: `FIFO_REQUEST` FAILs `reader did not answer the planted name within the local timeout`; `no-nlink-request`: `HARDLINK_REQUEST` FAILs `reader answered the planted name with a capture`; `no-excl-nofollow-response`: `SYMLINK_RESP_TEMP` FAILs `published through the planted response-temp symlink`; the restore run above is exit 0 (`scratch/fix296r2/mut-*.log`) |
| production-import guard (S2) | delivered tree: `go test -count=1 ./internal/linuxreadback -run '^TestProductionCommandsDoNotImportReadback$'`; mutant: the same over a copy whose `cmd/symmetry-daemon` blank-imports the package (`scratch/fix296/guard.sh`) | delivered tree: `ok … 6.705s` (exit 0); mutant: `FAIL … production_import_guard_linux_test.go:30: a production cmd depends on the test-only readback package: github.com/wxxb789/symmetry/daemon/internal/linuxreadback` (exit 1); the guard also runs ungated in the full `./...` suites below |
| refusal-row mutants (scratch copy) | `scratch/rf-acc-b-fix/mutant-run.sh` over a copy of `daemon/` | M1 `verifyLeafMembership` → `return nil`: reproduction FAILs `READBACK_LOCAL_FAIL: a target outside the named leaf was captured`; M2 four-uid loop → `range 0`: FAILs `an A-credentialed target is not the typed credentials refusal: … stage leaf`; M3 exe dev/ino guard dropped: FAILs `a wrong executable inode was accepted as the approved image`; the restored copy is identical to the delivered `reader_linux.go`/`local_repro_linux_test.go`/`readback.go` by sha256 and the reproduction PASSes |
| duplication + record cleanliness (scratch commit of the delivered files) | `python scripts/ci/jscpd-changed.py --base 8453da12 --head HEAD`; `python scripts/ci/check-record-cleanliness.py --base 8453da12 --head HEAD` | jscpd: 14 changed files, 1765 scanned sources, 1262 corpus clones, none new, exit 0 (notice: `CLOSED-TODO.md`'s markdown lies outside the configured scope, so its clones are not checked); cleanliness: 1 changed record passes the root-only check, exit 0 |
| gate-shape probe | `python3 findings/probe-gate-sequence.py` | root OK; `read as A(52000) … Permission denied`; `read as B(52001) … Permission denied`; `read as A+2caps: OK`; `A stat ns/user: Permission denied` |
| capability census | `python3 probe-visibility.py` / `probe-a-with-ptrace.py` | the §3 matrix |
| fix-round-2 full module suites | `go test ./...` from `daemon/`: Windows (go1.27.4 windows/amd64) and WSL Ubuntu-24.04 as unprivileged `symmetry-a` (umask 022, private `TMPDIR`) | Windows exit 0 (0 FAIL lines); WSL exit 0. The root-run WSL pass failed two packages on environment guards (`internal/platform`'s unprivileged-A requirement; `native/linux-gate/protocol`'s gate-census fd inheritance); both are identical at base `f30f3ef8` and pass in the CI-shaped unprivileged environment |
| fix-round-2 race + static gates | `go test -race -count=1 ./internal/linuxreadback/...` (WSL, `ok … 12.107s`); `gofmt -l .`; `go vet ./...`; `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go vet ./...`; `bash -n scripts/ci/linux-v2-native-acceptance.sh` | all exit 0 |
| fix-round-2 record/duplication gates (scratch commit `cfc32bdf`, parent `f30f3ef8`, base `8453da12`) | `scripts/ci/jscpd-changed.py --repo <scratch> --base 8453da12 --head HEAD`; `check-record-cleanliness.py`; `model-routing-record-sanitize.py --check --root-only` on the changed record; `python scratch/comment-cap.py <scratch> 8453da12` | jscpd exit 0 (15 changed files, 1766 scanned sources, 1261 corpus clones, none new); cleanliness exit 0 (1 changed record passes); sanitize exit 0; comment-cap exit 0 |
| PR body refresh | `gh pr view 296 --repo wxxb789/symmetry --json body --jq .body` → `findings/296-body.md` | 139 lines, 0 HTML comments (full rewrite in the round-2 records pass) |
| round-2 records pass (T1-T6; scratch clone commit of the records on `57c8c775`) | `check-record-cleanliness.py --base 8453da12 --head HEAD`; `model-routing-record-sanitize.py --check --root-only` on the changed records; `python scratch/comment-cap.py <worktree>` | cleanliness exit 0 (3 changed records pass); sanitize: evidence exit 0, `docs/TODO.md` exit 0, `docs/archive/CLOSED-TODO.md` exit 1 (identical at base `8453da12`; pre-existing quoted specimens, none on branch-added lines); comment-cap exit 0 |

The reproduction's child is the gate-shaped userns child of plan §8.2 (the local row maps `0 0 1`
beside `1 <B> 1` so the post-exec drop to the B extent is possible in Go; the exact single-extent
map is measured by the two probes above). The wide-descriptor target is the same userns child shape
with `maxResponseBytes/len(path) + 16` non-CLOEXEC descriptors on a >1.9 KiB path, so its encoded
capture necessarily exceeds `maxResponseBytes` and the reader must answer `stage=limit`; the pre-fix
baseline for that row is the reader's `response write failed … encoded readback response exceeds its
bound` followed by the client's 30 s timeout. The mutant row corrupts one expected environment byte
(`mutated[0][0] ^ 1`) and requires the shared `CheckEnvironment` to redden — the single test the
contract asks for.

The PR #296 round-1 fix adds the five planted-name rows of §6: the two response-temp plants
(`SYMLINK_RESP_TEMP`, `FIFO_RESP_TEMP`) and the request-side plants (`SYMLINK_REQUEST`,
`FIFO_REQUEST`, `HARDLINK_REQUEST`). On the old bytes only the response-temp plants red (the old
reader's `Lstat` gate already refused the request-side names, so those rows pin the new
open-then-fstat shape and each reddens on its own removal mutant, §6); `FIFO_RESP_TEMP` is the row
that reddens when `O_EXCL` alone is removed, because the old bytes' open hangs on the planted FIFO
instead of refusing.

Every row above that the delivered bytes can move was re-run after the simplification review, after
the apply stage and again at the fix revision's §1 digests (gofmt/vet/compile, the protocol unit
rows, the fixture syntax/contract suite, the reader unit proof and both reproduction modes); only
the run-specific identifiers differ between passes (`MainPID=450468` → `450765` → `457132` →
`457470` → `491566`, the userns inode numbers, nonces, the child PID). The kernel-shape probes and the capability
census (`findings/probe-gate-sequence.py`, `probe-visibility.py`, `probe-a-with-ptrace.py`) are
unchanged rows: no production or kernel-facing byte moved, and their host kernel is the same.
The PR #296 round-1 red/mutant rows were run at the round-1 bytes and the restore run at the
delivered bytes (§1 digests); the module and static gates were re-run at the delivered bytes
(the fix-round-2 rows of the table).

## 7. Runs

No run was dispatched from this stage: the contract's acceptance binds the conductor's single
scratch/Bind `shared` run on the PR head — delivered as the `Bind 2` subsection below — and the
fixture's reader step, the unit proof and the conversion rows above are the local evidence that
step consumes.

### Bind296 diagnostic run — one scratch run on the candidate head (`shared`)

**Diagnostic only, superseded by the post-fix Bind — not binding**: the conductor ruled this run
diagnostic (it ran on `f30f3ef8`, before the S1-S4 fix); the binding row is the `Bind 2`
subsection below (`45737002125` on `57c8c775`). The run's rows are recorded verbatim below,
including the new stop it found. S5
(leaf-membership stop) — see the conductor's S5 section; the round 2/3 fix is the S5 subsection
below.

Run **`45717008719`** (workflow "Scratch containment goal-admission probe", event `push`,
attempt 1, created 22:53:36Z, conclusion `failure`; `total_count=1` right after creation, after
settlement and after the branch deletion) ran the `shared` set exactly once on the exact
PR-head bytes plus the two scratch-only files
(`.github/workflows/scratch-rf-acc-containment-goal-admission.yml`, sha256
`bb6dcaf5e216aa7273dc472d76851fce2eb29cfaa7c335b6939f6053db948e0b`; `scripts/ci/scratch-gh-shim`,
sha256 `507340af870448533257a82366718f36a010a27c6ef9c5daba94667788ed740a`; both recovered
byte-exact from `b0ba2694`, the workflow's `case_sets` array the only delta). Run head
`1e206551e744bc58a127257085aacc34d374c327`, parent exactly `f30f3ef8`.

Run-input sha256 equality (run head vs candidate head `f30f3ef8`; every changed code or script
file of this branch):

| file | run-head sha256 | candidate-head sha256 (`f30f3ef8`; diagnostic, not binding) | equal? |
| --- | --- | --- | --- |
| `daemon/e2e/production_linux_v2_native_witness_test.go` | `3c26bf3f729342b8e088ba9470236485f09cb6924bf05267df7cb44243e3e558` | `3c26bf3f729342b8e088ba9470236485f09cb6924bf05267df7cb44243e3e558` | yes |
| `daemon/internal/linuxreadback/cmd/linux-v2-readback-reader/main.go` | `9d5e905930edf91a72bde5717c84617464d89f1f5d306cf4164d0dd82848f1f5` | `9d5e905930edf91a72bde5717c84617464d89f1f5d306cf4164d0dd82848f1f5` | yes |
| `daemon/internal/linuxreadback/local_repro_linux_test.go` | `5d4e7ccff4fe6d4dc2489ce5a4acda689ebe46b1589620f2b5a9640bd21b71a6` | `5d4e7ccff4fe6d4dc2489ce5a4acda689ebe46b1589620f2b5a9640bd21b71a6` | yes |
| `daemon/internal/linuxreadback/readback.go` | `7653e0dcc3f476599f2f53eefb5c8ac4efb124100dc555531a908f3d591d0619` | `7653e0dcc3f476599f2f53eefb5c8ac4efb124100dc555531a908f3d591d0619` | yes |
| `daemon/internal/linuxreadback/reader_linux.go` | `702bd2cbb341488c29896cb6ca7065c200fb43d1075a400e237f4885ca01784f` | `702bd2cbb341488c29896cb6ca7065c200fb43d1075a400e237f4885ca01784f` | yes |
| `daemon/internal/linuxreadback/reader_unsupported.go` | `9cb3aea49d3d12d4414e93c6c52e069e91f34eb5666f44e3bf44e4a1d4fd93ba` | `9cb3aea49d3d12d4414e93c6c52e069e91f34eb5666f44e3bf44e4a1d4fd93ba` | yes |
| `daemon/native/linux-gate/protocol/clone_integration_linux_test.go` | `4ce7d2216f990c9c67d24be8944249c906871a571db7a53c4e5086a72aeb9d5b` | `4ce7d2216f990c9c67d24be8944249c906871a571db7a53c4e5086a72aeb9d5b` | yes |
| `daemon/native/linux-gate/protocol/launcher_integration_linux_test.go` | `11629ac47f874cc1dc7781eaa94b7b69dd77f1e2fe08b731aa5fa565d55e36f4` | `11629ac47f874cc1dc7781eaa94b7b69dd77f1e2fe08b731aa5fa565d55e36f4` | yes |
| `daemon/native/linux-gate/protocol/readback_helpers_linux_test.go` | `170ea85fdeda954892543d5c05eab1b7f58478b21bfd0de715c95ccfb49a3de8` | `170ea85fdeda954892543d5c05eab1b7f58478b21bfd0de715c95ccfb49a3de8` | yes |
| `scripts/ci/linux-v2-native-acceptance.sh` | `ff40b2b1d88b7fbb2266fabf7d29bfd50427424b329cd767dc39e9b007b529f1` | `ff40b2b1d88b7fbb2266fabf7d29bfd50427424b329cd767dc39e9b007b529f1` | yes |
| `.github/workflows/scratch-rf-acc-containment-goal-admission.yml` | `bb6dcaf5e216aa7273dc472d76851fce2eb29cfaa7c335b6939f6053db948e0b` | absent (scratch-only) | scratch-only |
| `scripts/ci/scratch-gh-shim` | `507340af870448533257a82366718f36a010a27c6ef9c5daba94667788ed740a` | absent (scratch-only) | scratch-only |

Reader step: the ready proof passed — `root readback reader ready:
unit=symmetry-ci-readback-reader.service MainPID=7205` (22:57:48.4632989Z) after A's capability
readback row (`CapEff`/`CapPrm` `0000000000000000`, `CapBnd` `00000002fdecafff`, `CapAmb`
`0000000000000000`, `NoNewPrivs` 0). The stop proof was **not reached**: the set aborted at its
first failing gate before `stop_readback_reader` (its single call site after the case dispatch),
so the stop row remains unmeasured and no `readback-reader: stop …` journal line exists in the
run log.

Both old stops are cleared: neither `clone_integration_linux_test.go:252` nor
`launcher_integration_linux_test.go:451` appears anywhere in the 3,014-line run log, which
carries no `open /proc/` or `permission denied` line at all;
`TestInstalledLinuxOwnerCloneCheckoutScratch` passes (0.16s) and the boundary assertion's next
stop is verbatim `launcher_integration_linux_test.go:452: native process did not retain the
exact B-only boundary: linux readback refused at stage leaf: process is not a member of the
named leaf` (the reader's `verifyLeafMembership` refusal, `reader_linux.go:427`, reached after
the identity and credential checks). Case-set row: `Case set shared exited 1` /
`Failed case sets: shared`.

Evidence artifact `linux-v2-native-evidence-45717008719-1` (605 B, sha256
`0dcbf1414844049f218e59dd11343f5b763c04f3d10fd38bca4bbc71a44da728`; member
`shared/unprovisioned-owner-refusal.json`, 693 B, sha256
`56f670a22af5369efe20d11064ed9a1387decfb702f07d49f6c1079f2f26898c`); host-prerequisite artifact
sha256 `daf66d6217e278e3055169238350ae39cade95dd79806796dd7833b57b1504fc` with `source_commit`
= the run head and `status: "passed"`; the full run log is retained at the GDD state root
`scratch/bind296/run-45717008719.log`
(sha256 `c387627d71ee2795a7c951cdee9b4606257e1643b210135d835fa39d703d04db`).

### S5 — the leaf-membership stop (round 2/3 fix; findings/296-s5.md)

The diagnostic Bind's new stop was root-caused in a `resolute` replica (Ubuntu 26.04 userspace,
uutils multicall `coreutils 0.8.0`) and fixed in the protocol test files (no fixture change: the
`scratch` role already exports `SYMMETRY_LINUX_CLONE_TEST_ACK_ROOT`
(`linux-v2-native-acceptance.sh:1950`), installs it A-owned 0700 with the B `r-x` ACL
(`:754-755`, `:872-873`), and the helper's name is outside the shared `-test.run` regex
(`:3145`)).

Root cause: the gate executes the pinned image with `execveat(image, "", argv, envp,
AT_EMPTY_PATH)` (`c-gate/gate.c:1373`), so the kernel reports `AT_EXECFN` as `/dev/fd/<n>`; Ubuntu
26.04's uutils multicall resolves its applet from `AT_EXECFN` (anti-`env -a` hardening) →
`file_stem("/dev/fd/4")` = `"4"` → `coreutils: unknown program '4'` and `exit(1)` before reading
anything. B became a zombie in ~3-19 ms, left `cgroup.procs`, and the reader correctly refused
`stage=leaf` (`reader_linux.go:413/:427` at the diagnostic run's bytes); reproduced verbatim by
driving the distro `cat` with the gate's exact shape (`image_fd=4`, argv0
`/usr/lib/cargo/bin/coreutils/cat`, empty env). It is not a timeout (`WEXITSTATUS 1`, no unit), and
B's stdin was a live test-held pipe, not `/dev/null`.

Fix: `launcher_integration_linux_test.go:436` stages this test binary (`os.Executable()`, a
single-purpose ELF, via `copyScratchNativeImageForB`) into the A-owned ACK root, `:447` releases it
with the same vector (same stdio, `Env: []string{}`, `Dir: "/"`), and the new helper
`TestInstalledLinuxOwnerBoundaryNativeImageHelper` (`:765`) blocks on the test-held stdin pipe
with the measured fd set exactly `0,1,2`, byte-echoing stdin and writing the missing-path stderr;
the clone caller/function rename is `clone_integration_linux_test.go:86`/`:603`, and `:577`'s
never-executed stale-token command names the same staged image. The reader's
`verifyLeafMembership` leaf check and every existing assertion stay as delivered.

The second S5 leg (conductor follow-up, same worker): the owner-loss pair also releases a staged
single-purpose ELF — `:1231` stages `native-test-owner-loss-<pid>` in the same A-owned ACK root
and `:1239` releases it as `-test.run=^TestInstalledLinuxOwnerNativeImageIdleHelper$` with the
empty env and `Dir: "/"`; the new `:781` helper idles on `time.Sleep(time.Hour)` and is stopped
only by the kernel parent-death fence. Every assertion of the pair (pidfd identity, parent, B
credentials/caps/no-new-privs, start time, fixed B maps, exact leaf membership, clean exit, fence
death, reap by pidfd, emptied leaf) is unchanged; `trustedSleepPath()` remains only for the two
legs that never execute their image (`:225`, `:522`). Red→green (findings/296-s5.md §4): delivered
bytes in both replicas — `:1127: A helper exited unsuccessfully after RELEASE: exit status 1`
(`TEST_EXIT=1`, the delivered helper's inverted `MSG_EOR` check at the then-`:1209`); fixed bytes
— `--- PASS: TestLinuxOwnerNativeOwnerDeathScratch` in resolute and noble (`TEST_EXIT=0`, run30),
and the leg is referenced by no fixture case set.

Provenance: the legs now release an A-owned staged test ELF, pinned by the gate's `O_PATH` fd plus
approved identity and bound by the reader's exe dev/ino after RELEASE. A swap after RELEASE fails
closed; the legs no longer exercise a root-owned distro binary.

Red→green (findings/296-s5.md §4, exact strings/exit codes): `resolute`, delivered test bytes, 4
runs — `TEST_EXIT=1`, `launcher_integration_linux_test.go:452: native process did not retain the
exact B-only boundary: linux readback refused at stage leaf: process is not a member of the named
leaf` (one earlier run of the same bytes: `:455 … exact gate PID 522 is not in the pinned run
leaf`); fixed bytes, 3 runs — `--- PASS: TestInstalledLinuxOwnerBoundaryScratch` / `PASS` /
`TEST_EXIT=0`. Noble replica, delivered bytes — `TEST_EXIT=1`,
`launcher_integration_linux_test.go:723: A owner helper did not exit cleanly: exit status 1`
(standalone driver stderr `:795: receive owner-death instruction: n=1 flags=0x0 err=<nil>`); fixed
bytes, 4 runs — PASS. The delivered gate and reader bytes were unchanged throughout; the replica
commands and exit codes are findings/296-s5.md §5.

### S6 — the MSG_EOR polarity (round 2/3 fix; findings/296-s5.md)

The noble red above was an inverted expectation, not a framing change:
`launcher_integration_linux_test.go:835` and `:1264` expected `flags&unix.MSG_EOR == 0` (pass when
the bit appears); the fix flips both to `!= 0` (fail when it appears), matching the repo's
`wire_linux_test.go:716` convention ("Linux AF_UNIX recvmsg never reports MSG_EOR") and the same
polarity already used at `:281`/`:609`/`:647`/`:1007`. Red-first on noble (`:723`/`:795`) and on
the delivered owner-loss leg (`:1127`), green after (PASS rows above).

### Bind 2 — the binding run on the fixed head `57c8c775` (`shared`)

The conductor's Bind (round 2) ran the `shared` set once on the exact PR-head bytes plus the two
scratch-only files: run **`45737002125`** (workflow "Scratch containment goal-admission probe",
event `push`, attempt 1, created 01:37:28Z, conclusion `failure`; `total_count=1` right after
creation, after settlement, and after the branch deletion). Run head
`523af22ab175d941818ae921b599c71799aab44c`, parent exactly `57c8c775`; the scratch-only files are
`.github/workflows/scratch-rf-acc-containment-goal-admission.yml` (sha256
`bb6dcaf5e216aa7273dc472d76851fce2eb29cfaa7c335b6939f6053db948e0b`) and `scripts/ci/scratch-gh-shim`
(sha256 `507340af870448533257a82366718f36a010a27c6ef9c5daba94667788ed740a`), recovered byte-exact
from `b0ba2694` with the workflow's `case_sets` array (`→ shared`) the only delta.

Run-input sha256 equality (run head vs PR head; every changed code or script file of this branch):

| file | run-head sha256 | PR-head sha256 | equal? |
| --- | --- | --- | --- |
| `daemon/e2e/production_linux_v2_native_witness_test.go` | `2403b2f4aa3584d2cfa0b83dfe12e3210a967c0fbf16e1172ba14f005329516b` | `2403b2f4aa3584d2cfa0b83dfe12e3210a967c0fbf16e1172ba14f005329516b` | yes |
| `daemon/internal/linuxreadback/cmd/linux-v2-readback-reader/main.go` | `9d5e905930edf91a72bde5717c84617464d89f1f5d306cf4164d0dd82848f1f5` | `9d5e905930edf91a72bde5717c84617464d89f1f5d306cf4164d0dd82848f1f5` | yes |
| `daemon/internal/linuxreadback/local_repro_linux_test.go` | `5d3590beda1e97b4e18bd3ec2aab2fbc28cd953292d2ee34c20d97120e8bd290` | `5d3590beda1e97b4e18bd3ec2aab2fbc28cd953292d2ee34c20d97120e8bd290` | yes |
| `daemon/internal/linuxreadback/production_import_guard_linux_test.go` | `4d6618e33400dc1bf89a203c0ab510f8ec7bcb80440ce9ca42b3bcab8b3f1dbe` | `4d6618e33400dc1bf89a203c0ab510f8ec7bcb80440ce9ca42b3bcab8b3f1dbe` | yes |
| `daemon/internal/linuxreadback/readback.go` | `7653e0dcc3f476599f2f53eefb5c8ac4efb124100dc555531a908f3d591d0619` | `7653e0dcc3f476599f2f53eefb5c8ac4efb124100dc555531a908f3d591d0619` | yes |
| `daemon/internal/linuxreadback/reader_linux.go` | `0cbdb6a3f723ba476e259f11fd11f15d35c652c99d560d0feea52d35a3d5f791` | `0cbdb6a3f723ba476e259f11fd11f15d35c652c99d560d0feea52d35a3d5f791` | yes |
| `daemon/internal/linuxreadback/reader_unsupported.go` | `9cb3aea49d3d12d4414e93c6c52e069e91f34eb5666f44e3bf44e4a1d4fd93ba` | `9cb3aea49d3d12d4414e93c6c52e069e91f34eb5666f44e3bf44e4a1d4fd93ba` | yes |
| `daemon/native/linux-gate/protocol/clone_integration_linux_test.go` | `dfba0761d5048b0b09408b9288fb302ace6a8bdf2e9f6db7e10679fba3b28744` | `dfba0761d5048b0b09408b9288fb302ace6a8bdf2e9f6db7e10679fba3b28744` | yes |
| `daemon/native/linux-gate/protocol/launcher_integration_linux_test.go` | `8132b62576ce6d17c046e0fa7e918c172ab82e9586456c9e04c5fd43368bf259` | `8132b62576ce6d17c046e0fa7e918c172ab82e9586456c9e04c5fd43368bf259` | yes |
| `daemon/native/linux-gate/protocol/readback_helpers_linux_test.go` | `6455746fb8da553fac005fce5e2f059d6fb8427cefe4888f38f731ffe4273b38` | `6455746fb8da553fac005fce5e2f059d6fb8427cefe4888f38f731ffe4273b38` | yes |
| `scripts/ci/linux-v2-native-acceptance.sh` | `04bb9e8f700bfd672dcf9c7f89cebf1ed2a7b8c5266d77ecf1c2a37c4c882aad` | `04bb9e8f700bfd672dcf9c7f89cebf1ed2a7b8c5266d77ecf1c2a37c4c882aad` | yes |
| `.github/workflows/scratch-rf-acc-containment-goal-admission.yml` | `bb6dcaf5e216aa7273dc472d76851fce2eb29cfaa7c335b6939f6053db948e0b` | absent (scratch-only) | scratch-only |
| `scripts/ci/scratch-gh-shim` | `507340af870448533257a82366718f36a010a27c6ef9c5daba94667788ed740a` | absent (scratch-only) | scratch-only |

The eleven code/script digests equal §1's delivered-byte rows; the PR's other four changed files
are record-only docs. Every recorded stop of the two original classes is gone: the run log
(3,655 lines, sha256 `bf00da4fc22b7ea862e017e473637bd6b3482c41dc5c22db951c4692c7db5d95`, retained
at the GDD state root `scratch/bind296r2/run-45737002125.log`) carries 0 occurrences of
`clone_integration_linux_test.go:252`, `launcher_integration_linux_test.go:451`/`:452`/`:455`/`:465`,
`launcher_integration_linux_test.go:723`/`:736`, `open /proc/`, `/proc/`, `permission denied`,
`stage leaf`, or `not a member of the named leaf` (the log's only `environ` matches are two
unrelated `contracts`-job lines). All four converted owner legs verified green
(`verified TestInstalledLinuxOwnerBoundaryScratch: run=1 pass=1 skip=0` and its three siblings),
and the reader's start proof passed after A's capability row (`CapEff`/`CapPrm` 0):
`root readback reader ready: unit=symmetry-ci-readback-reader.service MainPID=7149`
(01:40:54.8336087Z). The stop proof was **not reached** — the set aborts at the next gate before
`stop_readback_reader`'s single call site (`scripts/ci/linux-v2-native-acceptance.sh:3224`; the
stop block at `:2202-2226` uses `systemctl stop` and requires `ActiveState=inactive`,
`Result=success`, `ExecMainStatus=0` and the removed ready file) — so the stop row remains
unmeasured, not failed. First failing line (01:41:05.4424414Z), quoted whole:

```
witness output:     production_linux_v2_native_witness_test.go:91: pre-existing RunJournal c869cf0f-884e-4160-b99a-aedd1dd6d659/1 is not physically released and safe for a fresh run: prior RunJournal may still own or require recovery of a process or leaf
```

The refused journal was inserted by the immediately preceding
`TestProductionLinuxV2AlternateCgroupMountRefusal` leg (goal `f900d3b4…` "Linux v2
stage_replay_alternate_mount owner witness", run insert 01:41:01.8457627Z), which itself verified
green; the pi leg's fresh-run precondition
(`daemon/e2e/production_linux_v2_native_witness_test.go:1968-1976`) then refused the shared A
StateDir, before any readback capture of that leg. The check text (`:1973`) and the leg ordering
(`run_alternate_mount_refusal_witness` → `run_native_owner_witness pi_provider_action`) are
identical in `2f7a38c6` (origin/main) — a pre-existing wall this run is the first `shared` run to
reach (the diagnostic run aborted at the boundary leg; the last real-workflow `shared` dispatch,
`45573253632` on main, failed earlier in the three protocol legs). Case-set row: `Case set shared
exited 1` / `Failed case sets: shared`.

Evidence artifact `linux-v2-native-evidence-45737002125-1` (1767 B, id `15080110623`, sha256
`2406f9ab51388d3672b3ad7dd9e310552b8b100a67176873f916049ce6603758`; members
`shared/alternate-mount-refusal.json` 2117 B `881783c7…` and
`shared/unprovisioned-owner-refusal.json` 693 B `2a3213a3…`); host-prerequisite artifact
`linux-v2-host-prerequisite-45737002125-1` (676 B, id `15078907824`, sha256
`7933e2e80a2cc8a17948943b187fba5534ccacb64fe39e1eeb5e6163d3413985`, member `dc9c90a2…`, 822 B).
The scratch branch was deleted once (`git ls-remote` prints nothing, exit 0); `refs/pull/296/head`
and the origin PR branch stayed `57c8c775` throughout (01:27:01Z, 01:37:06Z, 01:37:26Z,
01:44:59Z).

## 8. Limits and residuals

- The deferred e2e same-class sites (plan §3.2 rows 9-12) are recorded in `docs/TODO.md` with the
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
- The R2/R4/R5 and PR #296 round-1 pins (the refusal/limit rows, the nil-stop check, the five
  planted-name rows and the channel mutants in §6) live in that env-gated reproduction and in the
  scratch unit proof, so a regression there reddens only the local WSL rows as root: recorded as
  an oracle gap. The S2 production-import guard is not in that gap — it runs ungated in the full
  `./...` suites on linux/amd64. CI reaches the rest of the same code through the compile rows
  and, at the Bind, the fixture's reader step.
- The plan and this record also carry record-only edits after the code digests in §1; the affected
  rows were re-derived at the delivered bytes (§6) and every cited line anchor was re-measured
  against them.
