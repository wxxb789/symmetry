# rf-acc-synthetic-cleanup-root-removal — evidence record

Contract: `goal-rf-acc-synthetic-cleanup-root-removal.md` (GDD state root, outside the repository).
Dispatch base: `d4e455f1` (main, the merge of PR #304). Plan:
`docs/plans/2026-11-27-rf-acc-synthetic-cleanup-root-removal-plan.md`. Local verification is not
the credentialed acceptance; the conductor re-Binds the corrected PR head (§9).

## 1. Subject bytes (sha256)

| File | Before (dispatch base) | Delivered |
| --- | --- | --- |
| `scripts/ci/linux-v2-native-acceptance.sh` | `aac479e5fb80bcd0cc222f1afe96a2e9b010081cc0d2903e84c940e8e61c516d` | `a7c453cb75958c218bd7a09dac4e56a1f749aef2f008e22886b23ac46307c360` |
| `scripts/ci/test-linux-v2-evidence.py` | `d410828e7de0c5c90f8f7ef8f0559ce9a744ad12187840e0b64b2e822850075d` | `204e8cd6715cf389b7c21db7c129201311c5cc35daa0810ba0449fd812d75721` |
| `daemon/e2e/production_linux_v2_cleanup_witness_test.go` | `1e7a5d3a05d444ccd1b73e52df943c19978eb7b4faf921ef2e49377a6d378dcb` | `7c486e5415aa24c841016d0a9019d4a46309099f76358adb9af41e021ad4b7d9` |

`git rev-parse d4e455f1:daemon f3dbae66:daemon 07be175e:daemon` all return
`22d5cc959d3b2980b23bd79e6947a31c19dbc728`, so the probe binaries built from the dispatch base
exercise the daemon bytes from historical #304 Bind 2. That was
[run 45938637085](https://github.com/wxxb789/symmetry/actions/runs/45938637085) at run head
`2119226c6657681aff68ae9a5bb253249fc28b42`, with subject parent `f3dbae66` plus the scratch workflow
and GH shim. It is the red baseline for this diagnosis, not #305 acceptance (§9 and `## Bind`).
The scratch-clone receipt in the PR body identifies the tested plan and record blobs (§10).

## 2. Root cause (Stage 1)

### 2.1 The chain end to end

The one-use RELEASE leaves a B checkout created by the clone bootstrap. `createFreshCloneTarget`
sets `unix.Umask(0o077)` (`daemon/internal/clonebootstrap/filesystem_linux_amd64.go:216`) and
creates the layout with `unix.Mkdirat(runFD, generationName, 0o770)` (`:244`) and
`unix.Mkdirat(parentFD, name, 0o770)` (`openOrCreateBDirectory`, `:360`, `:365`); the B worker then
lets Git and its own shell create entries under `umask 022`. When the parent directory carries a
default ACL, the kernel takes the new entry's group bits from the default **mask**, not from the
umask-reduced mode: with the Bind 2 fixture's `d:u:$B_UID:rwx,d:m::rwx` defaults
(`scripts/ci/linux-v2-native-acceptance.sh:600-601`), `binding-…`, `run-…` and `generation-1` land
at mode `0770` and files created with mode `0666` (Git's and the setup shell's request) land at
`0660` (§4 rows; a `0644` request lands at `0640` in both shapes — §5.1 re-measurement).

On the historical subject the witness calls the Anchor at
`daemon/e2e/production_linux_v2_cleanup_witness_test.go:879` (`:882` is the failure line). Inside
the anchor worker `daemon/native/linux-gate/protocol/clone_cleanup_linux.go`:

1. `verifyCleanupPathFD(rootFD, BUID, BGID, "B clone root")` (`:320`, `:332`) accepts the clone
   root (`0750`, access mask `r-x`).
2. The target-descriptor check `verifyCleanupPathFD(targetFD, …)` (`:274`) refuses the
   `0770` `generation-1` with `pinned B clone target is not the exact B-owned directory`.
3. Even with that passed, the RunKey walk `verifyTargetNameIdentity` (`:410`) →
   `openCleanupRelativeDirectory` (`:358`, `:399`) refuses the `0770` `binding-…`/`run-…` ancestors
   with `cleanup ancestor is unsafe or crosses the pinned B root filesystem`.

The launcher core re-checks the same properties (`daemon/internal/launchercore/main_linux_amd64.go`
`RunCloneCleanup:137`, `verifyCleanupDirectoryFD:243`, `:253`, `cleanupTargetDescriptor:259`,
`removeCloneTargetTree:381`, `verifyCleanupRootNamed:467`, `openCleanupRelativeDirectory:523`,
`:564`, `removePinnedCloneTarget:618`, walker `validateEntry:1548`, `:1554`, `:1558`), and the
removal walk additionally refuses every group-writable file, which the Bind 2 shape's Git- and
shell-created `0660` files (`0666` requests under the `rwx` mask, §5.1) already are.

### 2.2 The client does not distinguish cleanup execution from State authority

The anchor maps the reproduced cleanup execution refusal to its default `clone_cleanup_unproven`
status. An `errLinuxOwnerAnchorCleanupAuthority` from the earlier State-authority path instead
maps to `cleanup_authority_unproven` (`daemon/internal/platform/anchor_server.go:1089–1094`,
`:1202–1209`). Both produce the same client message
(`daemon/internal/platform/linux_owner_anchor_linux.go:75–81`). The historical owner-service tail
has only `Starting`/`Started` lines and does not identify which boundary refused. The probes in
§3–§5 establish that the exact target shape would prevent removal if the worker reached it;
the refusal precedes `runCloneCleanupChild`. Separately, the historical RunJournal projected
`cleanup_pending` with no persisted removal proof. Its fatal assertion prevented the later
`RecordLinuxCloneTargetRemoval` call (`production_linux_v2_cleanup_witness_test.go:963` on the
delivered subject).

### 2.3 The wrong layer is the fixture

The installed preflight's clone-root contract (`verifyLinuxCloneRoot`,
`daemon/internal/linuxpreflight/git_ownership_linux.go:381`; `validateLinuxCloneRootPermissions`,
`:438`) requires A `r-x` without write and B `rwx` on access and inherited entries. B owns the clone
root, so its access/inherited permissions are read from the owner class
(`permissionsFromLinuxACL`, `daemon/internal/linuxpreflight/acl_linux.go:276`: `subject.UID ==
identity.UID` returns `userObj`) — that is, from `user::`/`default:user::`, not from the named
`d:u:B:rwx` entry or the mask. The inherited ACL still governs creation, bounded by the requested
mode. With `mkdir(0770)` under `umask 077`, `rwx` gave `0770`, `r-x` gave `0750`, and only the
no-default-ACL control gave `0700` (§5.1). The narrowed mask still satisfies preflight (§5).
The exact-mode checks are load-bearing and mirrored by production itself:
`requireOwnedBDirectory(…, true)` rejects group write on the pre-provisioned clone root
(`daemon/internal/clonebootstrap/filesystem_linux_amd64.go:391`); `verifyLinuxCloneSeedDirectory`
demands seed directories exactly `0750`. The fixture — not the cleanup, the witness or the anchor —
is the side that must move; the anchor's behavior is fail-closed and only lacks attribution.

## 3. Discrimination

| Candidate | Verdict | Evidence |
| --- | --- | --- |
| A real removal happened but was not proven | Not supported | The historical journal has no removal proof. On the reproduced input (§4), the worker refuses before the launcher can unlink the pinned target |
| A wrong subject or path | Not required to reproduce | The production checks in §4 refuse the mode of the pinned target on the RunKey-derived path |
| A permission failure of the *remover* | Not the reproduced refusal | The launcher-side rows run as root; the recorded target-shape diagnostic is a mode refusal, not an access errno |
| Earlier State-authority or request refusal | Unobserved | The historical log supplies neither raw status nor independent State-verifier readback. The target-shape defect suffices if dispatch reaches the worker |
| A proof-shape mismatch between anchor and witness reader | No proof was observed | The status-blind client message does not establish a proof-shape mismatch; the witness now reports an unsafe component before the Anchor call (§7, plan §3) |

Lab method (the #296 S5 / #304 method): three scratch `_test.go` probes calling the unexported
production checks, built `CGO_ENABLED=0` from `git archive d4e455f1 daemon`
(sha256 `b264608721850bc22f10600c592491664597c9dff3042f5f6de8616089546577`) inside a mirroring
Resolute chroot (`VERSION_CODENAME=resolute`), run as A (`setpriv --reuid=52000 --regid=52000
--clear-groups`) or root:

| Probe | Package | Calls |
| --- | --- | --- |
| `omp-probe-anchor.test` | `native/linux-gate/protocol` | `verifyCleanupPathFD` (root, target), `verifyTargetNameIdentity` |
| `omp-probe-preflight.test` | `internal/linuxpreflight` | `effectiveLinuxPathPermissions`, `defaultLinuxACLPermissions`, `VerifyNoUntrustedACLWrite`, `validateLinuxCloneRootPermissions` |
| `omp-probe-walk.test` | `internal/launchercore` | `verifyCleanupDirectoryFD`, `verifyCleanupRootNamed`, `cleanupTargetDescriptor`, `validateEntry`, `removePinnedCloneTarget` |

Binary sha256: anchor `142f30f8ba28fa2426fe4628c6fa19a3cc77bd0fca5bb74ea2dd13c0b02723c9`,
preflight `b47cab9a4c704bcbe016a0806d78639f32f2e7f86e24fa1beec977ea4c414248`, walk
`0ee2b7d21dd15ebb80f134176257f491d11577f5d3b98d35b315fe0635bef995`. The fixture creates the
B-side entries with the exact `mkdir(2)`/`open(2)` calls the bootstrap and worker make (`os.mkdir`
/ `open` under `umask 077`/`022`), because the guest chroot's `/usr/bin/mkdir` is uutils coreutils
and its `-m` path adds setgid+sticky (`mkdir -m 770` → `3770`) — a fixture-construction hazard, not
a production shape.

## 4. Red rows — the Bind 2 shape reproduces the stop (logs `7ff742ba…`, exit 0 per row)

Fixture: clones `52001:52001 750` with `default:user:52001:rwx`, `default:mask::rwx`; created
`binding-ompbbbbbbbb`/`run-omprrrrrrrr`/`generation-1` = `52001:52001 770`; `.git/config` and
`candidate.txt` (Python `open(..., "wb")`, mode `0666` request) = `660`; `pack-omp.pack`
(`chmod 0444`) = `444`. Shell redirects and Git creation were measured separately in §5.1.

```
PROBE stat clones           mode=040750 uid=52001 gid=52001 dev=2096
PROBE stat target           mode=040770 uid=52001 gid=52001 dev=2096
PROBE access target A-read=<nil> A-write=permission denied
PROBE CHECK root-shape err=<nil>
PROBE CHECK target-shape err=pinned B clone target is not the exact B-owned directory
PROBE CHECK target-name-walk err=cleanup ancestor is unsafe or crosses the pinned B root filesystem
PROBE CHECK launcher-root-shape err=<nil> identity={Device:2096 Inode:306922}
PROBE CHECK launcher-root-rewalk err=<nil>
PROBE CHECK launcher-target-shape present=false err=verify inherited B target O_PATH FD: inherited B cleanup directory has unsafe type, owner, group, or writable mode
PROBE CHECK entry-policy group-writable-dir   err="B clone directory has sticky or group/world write permissions"
PROBE CHECK entry-policy group-writable-file  err="B clone regular file is hard-linked or group/world writable"
```

The two refusal strings are exactly the checks of §2.1; the probe exits 0 by asserting them.

## 5. Delivered-shape rows (log `1f6c3e83…`, exit 0)

Fixture: the delivered `setfacl -m "u:52000:r-x,m::r-x,d:u:52000:r-x,d:m::r-x"` defaults; created
dirs `52001:52001 750`, `0666` opens land at `640`, `chmod 0444` at `444`, hook `chmod 0755` at
`755`.

```
PROBE stat target           mode=040750 uid=52001 gid=52001 dev=2096
PROBE access target A-read=<nil> A-write=permission denied
PROBE CHECK root-shape err=<nil>
PROBE CHECK target-shape err=<nil>
PROBE CHECK target-name-walk err=<nil>
PROBE acl aAccess=05 bAccess=07 aDefault=05 bDefault=07
PROBE CHECK untrusted-write err=<nil>
PROBE CHECK clone-root-contract err=<nil>
PROBE CHECK launcher-target-shape present=true err=<nil>
PROBE CHECK launcher-removal err=<nil>
PROBE kept .../cleanup-sibling-omp/sentinel.txt "sibling retained\n"
PROBE kept .../run-ompoooooooo/generation-1/sentinel.txt "other run retained\n"
```

`launcher-removal` is production's unchanged `removePinnedCloneTarget` walk: the tree is removed,
its pinned inode has `nlink=0`, and the sibling and independent checkout survive. The entry-policy
probes reported group-writable directory/file, setgid directory and foreign-owner directory
refusals. The preflight row shows the delivered fixture still satisfies the installed contract
(A `r-x`, B `rwx`, no untrusted writer).

### 5.1 Mask-to-mode re-measurement (Resolute chroot, exit 0)

The §12 probe re-measures the mode rows on kernel `6.18.58.1-microsoft-standard-WSL2`. Each parent
is B-owned mode `0750`, and the creations run as B under `umask 077` (directories) or `022` (files).

| Creation | Red shape (`d:m::rwx`) | Delivered shape (`d:m::r-x`) |
| --- | --- | --- |
| `os.mkdir(0o770)` under `umask 077` (the bootstrap's `Mkdirat`) | `0770` | `0750` |
| shell redirect / `git init` `.git/config` / `os.open(0o666\|0o664)` | `0660` | `0640` |
| `os.open(0o644)` | `0640` | `0640` |

`default:other::---` in both shapes strips world read, so no row lands `0644`: the `0660` rows are
`0666`/`0664`-request creations and a `0644` request gives `0640` in both shapes. For the directory
comparison, the no-default-ACL control gives `0700` for that same call and umask, not `0750`.

## 6. Committed pins, red/green in the resolute chroot (lesson 160)

The historical full guard wrapper extracted from the pre-round script (sha256
`a0b0c91343e65cf50c7838fbc1e9eed47491e16dd860247f215883f837838257`) ran against real fixture shapes
through the chroot rootfs:

| Shape | Exit | Guard output |
| --- | ---: | --- |
| delivered (`d:m::r-x`) | 0 | `GUARD_BLOCK_OK` |
| widened default mask (`setfacl -m d:m::rwx`) | 1 | `synthetic cleanup clone root must keep a non-writable default ACL mask so B-created binding/run/generation directories stay non-writable` |
| widened mode (`chmod 0770`) | 1 | `synthetic cleanup clone root must stay B-owned mode 0750` |
| extra default reader (mode unchanged, `d:u:52002:r-x`) | 1 | the non-writable-mask message (exact-ACL mismatch) |

On the delivered script (§1), the unchanged clone guard at `:1474–1478` was extracted and rerun in
Resolute with real `stat` and `getfacl`. Delivered still exited 0; `d:u:52002:r-x` with the original
mask and mode still exited 1. Two additional rows differed from the table above: `52002:52001:750`
and `52001:52002:750` with the exact ACL each exited 1 with the B-owned-mode diagnostic. A foreign
reader can preserve the mask and a foreign owner or group can preserve the mode; the exact text
and full stat tuple exclude those ways of passing a less specific check.

`SyntheticCleanupGuardTest` executes the full guard block with stubbed command results, pins the
fixture's `setfacl` arguments and passed all 12 rows after restoration (one unittest, exit 0, same
delivered script and harness digests as §1). `clone-acl` changes only the default mask;
`clone-default-reader` adds only the named default reader. `clone-uid` and `clone-gid` each change
one stat column at mode `0750`.

## 7. Mutants

Earlier rounds (apply/fix) on the pre-round code identified under §10:

| Mutant | Applied to | Result |
| --- | --- | --- |
| guard checks deleted | script copy | `SyntheticCleanupGuardTest` exit 1, 2 failures (both new cases) |
| mode guard inverted (`==` → `!=`) | script copy | exit 1, 3 failures |
| fixture spec reverted to `d:u:$B_UID:rwx,d:m::rwx` | script copy | exit 1, 9 failures (stub argument pin) |
| `stat.Mode&0o022 != 0` → `== 0` | witness copy | `TestLinuxV2CleanupShapeRefusalRows` exit 1 (`clean-b-owned` row) |
| owner check deleted | witness copy | exit 1 (`foreign-owner` row) |
| per-component refusal inverted (`refusal != ""` → `refusal == ""`) at `daemon/e2e/production_linux_v2_cleanup_witness_test.go:1724` | witness copy `d7317ba8280a87816ed6b430da08598e2362f71f3063493b2f5fc3781bb70337` | `TestLinuxV2CleanupShapeWalkRows` exit 1, 3 failures (`clean-tree`, `ancestor-refusal`, `open-error`) |
| walk treats the borrowed root FD as owned (`current, owned := rootFD, false` → `rootFD, true`) at `daemon/e2e/production_linux_v2_cleanup_witness_test.go:1706` | witness copy `825b46af2060d3a51f0b0202f0ce70a0ad5c43ab655fee0279845748a568bb0c` | `TestLinuxV2CleanupShapeWalkRows` exit 1, 2 failures (`ancestor-refusal`, `open-error`: `open "binding-omp" … bad file descriptor`) |

This fix round (each red exit 1; targeted unmodified Go and Python each exit 0 after the overlays
were removed, using the code digests in §1):

| Overlay | Changed check | Refusal or leak observed by the test |
| --- | --- | --- |
| `lose-owned` | assign `current, owned = next, false` | retained directory FDs: `clean-tree`, `ancestor-refusal`, `target-refusal`, `open-error` |
| `omit-final-close` | delete the final owned-descriptor close | retained `generation-1` FD on successful walks, including the two retry rows |
| `stop-before-final` | iterate `components[:len(components)-1]` | `target-refusal` got nil instead of the full `binding-omp/run-omp/generation-1` diagnostic |
| `skip-final-validation` | break immediately after acquiring the final component | `target-refusal` got nil despite opening the target |
| `ignore-setuid` | use `0o3000` instead of `0o7000` | `setuid` got no refusal for `04750` |
| `clone-mask-only` | compare only the `default:mask::` line | `clone-default-reader` unexpectedly accepted |
| `ignore-clone-uid` | drop UID from the observed and expected tuples | `clone-uid` unexpectedly accepted |
| `ignore-clone-gid` | drop GID from the observed and expected tuples | `clone-gid` unexpectedly accepted |

The FD assertion examines live links under each canonical, private fixture root, not the process's
FD total (unrelated FDs could close while a walk FD leaks). No other code opens that subtest tree;
the one allowed FD must still refer to the original root inode. The unsafe-target row changes only
`generation-1` after creating safe ancestors and requires the exact path and reason, so an
earlier ancestor refusal cannot satisfy it.

Oracle gap: the witness call site at
`daemon/e2e/production_linux_v2_cleanup_witness_test.go:574` runs only inside the credentialed
`synthetic_goal_cleanup` case set, so deleting that line reddens no locally runnable test; the
predicates it calls are committed-tested (the rows above), the call itself is covered only by the
conductor's Bind. Carried in `docs/TODO.md`.

Separate oracle gap, accepted by the conductor: the kernel-error arm of `Fstat` after a successful
component `O_PATH` open (`:1719–1723`) is not fault-injected. The outer boundary is the credentialed
witness at `:574–576`: any error from the walk is fatal before durable Begin or the Anchor call.
This boundary is not a test of the kernel-error arm; the conductor ruled out a fault fixture for
this round. Carried in `docs/TODO.md`.

## 8. Local gates (delivered code digests in §1)

| Platform and command | Exit | Observation |
| --- | ---: | --- |
| Host, WSL and Resolute Bash: `bash -n scripts/ci/linux-v2-native-acceptance.sh` | 0 / 0 / 0 | no syntax errors |
| WSL Python: `python3 -B scripts/ci/test-linux-v2-evidence.py` | 0 | 57 tests OK |
| Host and WSL Go: `gofmt -l` on the changed witness | 0 / 0 | no files listed |
| Host and WSL Go, from `daemon/`: `go vet ./e2e` | 0 / 0 | affected package |
| Host and WSL Go: `go test -count=1 ./e2e` | 0 / 0 | affected package; WSL includes the shape rows but not credentialed acceptance |
| WSL Go: `go test -count=1 -run '^TestLinuxV2CleanupShape(Refusal\|Walk)Rows$' -v ./e2e` | 0 | 10 predicate rows, 5 walk rows |
| WSL Go: `TMPDIR=<symlink> go test -count=1 -run '^TestLinuxV2CleanupShapeWalkRows$' ./e2e` | 0 | test temp root canonicalized; witness open policy unchanged |

Head-wide suites (the daemon race suite and the credentialed case set) are CI's and the conductor's,
not claimed here.

## 9. Acceptance still pending

Delivered, pending the conductor's Bind on the fixed PR head. Run `45956915653` is the green
credentialed result on the prior `dd20a485` subject only (see `## Bind`). This round changed
executed input bytes. Re-run `synthetic_goal_cleanup` once on the new head and verify the removal,
A-side cleanup, journal retirement and passed manifest there, or record the first failing witness
line verbatim in `docs/TODO.md`. Neither the #304 baseline nor the local gates count as acceptance.

## 10. Committed-blob gates

The apply-stage receipt (`f1d8c92c`) is **PARTIAL and SUPERSEDED**. Its Go witness blob matched
`dd20a485`, but both CI scripts were stale: Git blobs `b98f29794448f735d018fec498ea44911b6c997c`
instead of `5dbcc0b596aea97fc450975e37e5fac17c765046` (shell, line 600), and
`1bcf03d34a7f4f03e440a0c1f8c0d0edee8faa85` instead of `89bbbfc9fdbe6332ff501bf206d25e8254ce470b`
(Python, line 2123). That receipt cannot bind the coordinated fixture and harness change.

The following are prior results, not this round's. The subsequent docs-only fix receipt re-ran the
three gates against the seven paths later committed at `dd20a4855f54185221eee4753cc3931d252c34de`;
its evidence blob SHA-256 was `746d315606a101a71370c88411ff409e09862470d2e97c259eadf1cb9bcba3ac`.
The two executed inputs for that revision are hashed in `## Bind`; the Python harness SHA-256 was
`e1d3d68a73c6a893f31bc3f254118201d2d10c1589d1febabc4ce20aff592246`.

| Command | Exit | Observation |
| --- | ---: | --- |
| `python -B scripts/ci/jscpd-changed.py --base d4e455f1 --head HEAD` | 0 | 7 changed files, 1792 scanned sources, 1245 clones in the corpus, none new; the gate notices `docs/archive/CLOSED-TODO.md` is outside its scan scope |
| `python -B scripts/ci/check-record-cleanliness.py --base d4e455f1 --head HEAD` | 0 | 1 changed record passes the root-only check |
| `python -B "$comment_cap" "$scratch_clone" d4e455f1` | 0 | no over-cap comment block among the added lines |

This round's fresh seven-path scratch-clone gate revision, digests and exit codes are in the PR
body receipt (the tested record cannot contain its own digest). No local gate substitutes for the
exact-head credentialed Bind.

## 11. Residuals

- Production observation (decision note, not fixed here): an install-accepted clone root whose
  default ACL mask grants write produces a checkout whose directories and files are group-writable,
  which the exact cleanup checks refuse. This fails closed; that path cannot remove the clone
  without operator intervention. Candidates: reject a writable default mask in the install preflight, set
  the created modes explicitly in the clone bootstrap, or make the cleanup mode checks ACL-aware.
  The same shape remains on the production, daemon-service-workspace and clone-test fixture roots
  (`scripts/ci/linux-v2-native-acceptance.sh:691-693`, `:878`). The shared legs reach targets in the
  clone-test root through script `:1975` and `:3169–3175`; the native tests read that root at
  `daemon/native/linux-gate/protocol/clone_cleanup_linux_test.go:70,134`. Their targets are
  provisioned at script `:695–757`, before the writable default mask is installed at `:878`.
- The anchor's status-blind client error (`linux_owner_anchor_linux.go:81`) stays; the witness-side
  shape assertion is the attribution available without a production protocol change.

## 12. Safe reproduction — mask-matrix probe

Run `bash probe-mask-matrix.sh` as root in the Resolute replica, with no path argument. The probe
creates its own scratch directory and removes only that directory. Probe SHA-256
`7f22ddd13984bfa7e7ca1daf09f924f5be459d9b5f4fcd0e59ffc0846101a274`; the output with its final
`exit=0` line is `ccf3d996f6006ecb5597e3893ea93b23b0b07b9c7022aba2dc4b6b1fa1286cec` (§5.1).

```bash
#!/usr/bin/env bash
set -euo pipefail
unset TMPDIR
W=$(mktemp -d --tmpdir symmetry-mask-matrix.XXXXXXXX)
trap 'rm -rf -- "$W"' EXIT
chmod 0711 "$W"
A=52000
B=52001
cat > "$W/create.py" <<'PY'
import os, sys
base = sys.argv[1]
os.umask(0o077)
os.mkdir(os.path.join(base, "bootstrap-dir"), 0o770)
os.umask(0o022)
for name, mode in (("open0644.txt", 0o644), ("open0666.txt", 0o666), ("open0664.txt", 0o664)):
    os.close(os.open(os.path.join(base, name), os.O_CREAT | os.O_WRONLY, mode))
PY
chmod 0644 "$W/create.py"
mkdir -- "$W/red" "$W/green" "$W/no-acl"
chown "$B:$B" "$W/red" "$W/green" "$W/no-acl"
chmod 0750 "$W/red" "$W/green" "$W/no-acl"
setfacl -m "u:$A:r-x,m::r-x,d:u:$A:r-x,d:u:$B:rwx,d:m::rwx" "$W/red"
setfacl -m "u:$A:r-x,m::r-x,d:u:$A:r-x,d:m::r-x" "$W/green"
setfacl -k -- "$W/no-acl"
as_b() { setpriv --reuid="$B" --regid="$B" --clear-groups "$@"; }
for d in red green; do
  as_b bash -c 'umask 022; printf "shell\n" > "$1/sh-redirect.txt"; printf "candidate\n" > "$1/candidate.txt"; git init -q "$1/gitinit"' bash "$W/$d"
done
for d in red green no-acl; do
  as_b python3 "$W/create.py" "$W/$d"
  printf 'MASK-MATRIX %s mkdir(0770) under umask 077: %s\n' "$d" "$(stat -c %a "$W/$d/bootstrap-dir")"
done
for d in red green; do
  for f in sh-redirect.txt candidate.txt gitinit/.git/config open0644.txt open0666.txt open0664.txt; do
    printf 'MASK-MATRIX %s %s: %s\n' "$d" "$f" "$(stat -c %a "$W/$d/$f")"
  done
done
printf 'MASK-MATRIX kernel %s\n' "$(uname -r)"
```

## Bind 2

[Run 45959717137](https://github.com/wxxb789/symmetry/actions/runs/45959717137), attempt 1, is
**BINDING** on PR #305 head `897363bba56fc3aa8f2bccab2e8b14cad03d06f2` and resolves §9. It supersedes
[the first Bind](https://github.com/wxxb789/symmetry/actions/runs/45956915653) on `dd20a485`.

Setup passed for `synthetic_goal_cleanup`, the only case set. The live clone-root guards accepted
`B:B 0750` and the exact access/default ACL with a non-writable default mask. All three historical
stops (`:882` root proof, `:506` seed EACCES and `:499` stage none) have zero matches in the
2,607-line log, including each frame and diagnostic checked separately. Log 2447 reads
`verified TestProductionLinuxV2CloneCleanupAuthenticatedScratch: run=1 pass=1 skip=0`.

The final assertions passed: the pinned B target had zero links and no pathname, the sibling and
independent checkout remained intact, A removed its seed/base/scratch without losing the promotion
ref, and the RunJournal was retired. `synthetic-goal-cleanup.json` exported and the manifest passed.
Artifact `15179869761` (`linux-v2-native-evidence-45959717137-1`) matched the API ZIP SHA-256
`cb07bfb082b06cb01b1ef1cf55752374e28c8cae94d446e1544ec356e648df67`.

The standalone Go predicate/walk tests and the negative rows in `scripts/ci/test-linux-v2-evidence.py`
belong to the `daemon` job in `.github/workflows/ci.yml`, not the acceptance guest; see “Not covered
by this Bind” in `305-binding-run-2.md` for the individual rows and CI results.

Run head `5d27bcb0a792cd05b18ce520bd5df5e50fb2ae30` matched all 1,902 PR entries by tree metadata
and blob SHA-256. These are the only executed input deltas from Bind 1; the Python harness and four
docs also changed, while the other tracked blobs and both scratch-only files did not.

| Input changed since Bind 1 | Run `5d27bcb0` SHA-256 = PR `897363bb` SHA-256 |
| --- | --- |
| `daemon/e2e/production_linux_v2_cleanup_witness_test.go` | `7c486e5415aa24c841016d0a9019d4a46309099f76358adb9af41e021ad4b7d9` |
| `scripts/ci/linux-v2-native-acceptance.sh` | `a7c453cb75958c218bd7a09dac4e56a1f749aef2f008e22886b23ac46307c360` |
