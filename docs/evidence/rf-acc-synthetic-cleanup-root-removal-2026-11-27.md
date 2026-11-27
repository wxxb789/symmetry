# rf-acc-synthetic-cleanup-root-removal — evidence record

Contract: `goal-rf-acc-synthetic-cleanup-root-removal.md` (GDD state root, outside the repository).
Dispatch base: `d4e455f1` (main, the merge of PR #304); the Bind 2 subject was `f3dbae66`. Plan:
`docs/plans/2026-11-27-rf-acc-synthetic-cleanup-root-removal-plan.md`. Local verification is not
the credentialed acceptance; the conductor re-Binds the corrected PR head (§9).

## 1. Subject bytes (sha256)

| File | Before (dispatch base) | Delivered |
| --- | --- | --- |
| `scripts/ci/linux-v2-native-acceptance.sh` | `aac479e5fb80bcd0cc222f1afe96a2e9b010081cc0d2903e84c940e8e61c516d` (= the Bind 2 run input) | `60d63a8b0dd1a7d0fb72c920d68ab6a82f841d654e0f4dc2525ad85dd0cbba2d` |
| `scripts/ci/test-linux-v2-evidence.py` | `d410828e7de0c5c90f8f7ef8f0559ce9a744ad12187840e0b64b2e822850075d` | `e1d3d68a73c6a893f31bc3f254118201d2d10c1589d1febabc4ce20aff592246` |
| `daemon/e2e/production_linux_v2_cleanup_witness_test.go` | `1e7a5d3a05d444ccd1b73e52df943c19978eb7b4faf921ef2e49377a6d378dcb` | `8cea49859f3bec47e3fc25c27586c3f70fc04a411e1f54fb5f80b2afcd188f7f` |

`git rev-parse d4e455f1:daemon f3dbae66:daemon 07be175e:daemon` all return
`22d5cc959d3b2980b23bd79e6947a31c19dbc728`, so the probe binaries built from the dispatch base
exercise the exact daemon bytes the Bind 2 run executed. The plan file and this record are bound by
the PR body receipt and the committed-blob gates (§10).

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

The witness then calls the authenticated anchor at
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

### 2.2 The refusal is swallowed into a status, and no removal happens

The anchor's clone-cleanup goroutine maps any worker error onto the status string
`clone_cleanup_unproven` (`daemon/internal/platform/anchor_server.go:1191-1206`, `:1205`); the A-side
client rejects any non-conforming response with one status-blind message
(`daemon/internal/platform/linux_owner_anchor_linux.go:81`), which is the exact text the Bind 2
witness printed. The owner-service journal carried no worker detail (Bind 2 §2: only the unit's
`Starting`/`Started` lines in the last 40 lines), so the Bind evidence alone cannot name the
refusing check — the reproduction in §3-§5 names it. No removal happened and none could: the refusals
precede the launcher spawn (`runCloneCleanupChild`), the witness's own durable Begin is the only
cleanup write in the RunJournal (the projection in Bind 2 §2 reads `cleanup_pending`), and the
target-removal proof is persisted only after a green root proof
(`production_linux_v2_cleanup_witness_test.go:963`, `RecordLinuxCloneTargetRemoval`), which never
ran.

### 2.3 The wrong layer is the fixture

The installed preflight's clone-root contract (`verifyLinuxCloneRoot`,
`daemon/internal/linuxpreflight/git_ownership_linux.go:381`; `validateLinuxCloneRootPermissions`,
`:438`) requires A `r-x` without write and B `rwx` on access and inherited entries. B owns the clone
root, so its access/inherited permissions are read from the owner class
(`permissionsFromLinuxACL`, `daemon/internal/linuxpreflight/acl_linux.go:276`: `subject.UID ==
identity.UID` returns `userObj`) — that is, from `user::`/`default:user::`, not from the named
`d:u:B:rwx` entry or the mask. A non-writable default mask therefore still satisfies the installed
contract (§5 row) while making the bootstrap's `umask 077` effective again. The cleanup's exact-mode
checks are load-bearing and mirrored by production itself (`requireOwnedBDirectory(…, true)` rejects
group write on the pre-provisioned clone root,
`daemon/internal/clonebootstrap/filesystem_linux_amd64.go:391`; `verifyLinuxCloneSeedDirectory`
demands seed directories exactly `0750`). The fixture — not the cleanup, the witness or the anchor —
is the side that must move; the anchor's behavior is fail-closed and only lacks attribution.

## 3. Discrimination

| Candidate | Verdict | Evidence |
| --- | --- | --- |
| A real removal happened but was not proven | Refuted | The refusal in §4 precedes the launcher spawn; the Bind 2 journal stayed `cleanup_pending` with no target-removal proof, and the pinned target inode could not have been unlinked before the worker ran |
| A wrong subject or path | Refuted | §4: the root FD, the RunKey-derived relative path and the target device/inode all pass; the refusing checks are mode/owner predicates on the exact pinned inodes |
| A permission failure of the *remover* | Refuted | The launcher-side rows in §4/§5 run as root and the removal-eligibility checks are caller-independent mode predicates; the red refusal text is the mode predicate, not an errno |
| The State readback verifier refused the request | Refuted as the operative cause | The verifier reads the same journal the witness had just read back with the same predicates (`daemon/internal/state/linux_clone_cleanup_reader_linux_amd64.go:144`, `:154`, `:217`), requires the exclusive daemon flock the witness's open `state.New` Store holds (`:295`; `state_unix.go:17`), and the fixture installs the pinned `0700` dirs and `0600` lock; the worker-side refusal is deterministic on the recorded shape either way |
| A proof-shape mismatch between anchor and witness reader | Partly true, as the *symptom* | The client cannot see `clone_cleanup_unproven`; this increment adds the witness-side named assertion (§5, plan §3) so the next shape refusal is attributable |

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
`candidate.txt` (mode `0666` shell redirects) = `660`; `pack-omp.pack` (`chmod 0444`) = `444`.

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

`launcher-removal` is the production `removePinnedCloneTarget` walk: the whole tree is removed, the
pinned inode reads `nlink=0`, the sibling and the independent RunKey checkout are intact, and the
entry-policy rows from §4 still refuse group-writable dirs/files, set-ID/sticky modes, foreign
owners and cross-device entries — the delivered shape passes the walk without relaxing it. The
preflight row proves the delivered fixture still satisfies the installed clone-root contract
(A `r-x`, B `rwx`, no untrusted writer).

### 5.1 Mask-to-mode re-measurement (fix round r1, log `a7d8b090…`, exit 0)

The arithmetic behind every mode row above and in §4, re-measured on the target platform (kernel
`6.18.58.1-microsoft-standard-WSL2`; B-owned `0750` clone root carrying each default set; the
creations run as B under `umask 077`/`022`) with the appendix probe (§12, sha256
`382cfe8dbff029dc63dcad4e3a00ac54ad764506404d14491bb670ad408fbdbf`):

| Creation | Red shape (`d:m::rwx`) | Delivered shape (`d:m::r-x`) |
| --- | --- | --- |
| `os.mkdir(0o770)` under `umask 077` (the bootstrap's `Mkdirat`) | `0770` | `0750` |
| shell redirect / `git init` `.git/config` / `os.open(0o666\|0o664)` | `0660` | `0640` |
| `os.open(0o644)` | `0640` | `0640` |

`default:other::---` in both shapes strips world read, so no row lands `0644`: the `0660` rows are
`0666`/`0664`-request creations and a `0644` request gives `0640` in both shapes.

## 6. Committed pins, red/green in the resolute chroot (lesson 160)

The guard block extracted verbatim from the delivered script (sha256
`a0b0c91343e65cf50c7838fbc1e9eed47491e16dd860247f215883f837838257`) ran against real fixture shapes
through the chroot rootfs:

| Shape | Exit | Guard output |
| --- | ---: | --- |
| delivered (`d:m::r-x`) | 0 | `GUARD_BLOCK_OK` |
| widened default mask (`setfacl -m d:m::rwx`) | 1 | `synthetic cleanup clone root must keep a non-writable default ACL mask so B-created binding/run/generation directories stay non-writable` |
| widened mode (`chmod 0770`) | 1 | `synthetic cleanup clone root must stay B-owned mode 0750` |
| extra default reader (mode unchanged, `d:u:52002:r-x`) | 1 | the non-writable-mask message (exact-ACL mismatch) |

The Python guard test executes the same block with stubbed command results, pins the delivered
`setfacl` arguments, and adds the `clone-mode`/`clone-acl` cases; `python3 -B
scripts/ci/test-linux-v2-evidence.py SyntheticCleanupGuardTest` passes 1 test (all rows) on the
delivered bytes.

## 7. Mutants

| Mutant | Applied to | Result |
| --- | --- | --- |
| guard checks deleted | script copy | `SyntheticCleanupGuardTest` exit 1, 2 failures (both new cases) |
| mode guard inverted (`==` → `!=`) | script copy | exit 1, 3 failures |
| fixture spec reverted to `d:u:$B_UID:rwx,d:m::rwx` | script copy | exit 1, 9 failures (stub argument pin) |
| `stat.Mode&0o022 != 0` → `== 0` | witness copy | `TestLinuxV2CleanupShapeRefusalRows` exit 1 (`clean-b-owned` row) |
| owner check deleted | witness copy | exit 1 (`foreign-owner` row) |
| per-component refusal inverted (`refusal != ""` → `refusal == ""`) at `daemon/e2e/production_linux_v2_cleanup_witness_test.go:1724` | witness copy `d7317ba8280a87816ed6b430da08598e2362f71f3063493b2f5fc3781bb70337` | `TestLinuxV2CleanupShapeWalkRows` exit 1, 3 failures (`clean-tree`, `ancestor-refusal`, `open-error`) |
| walk treats the borrowed root FD as owned (`current, owned := rootFD, false` → `rootFD, true`) at `daemon/e2e/production_linux_v2_cleanup_witness_test.go:1706` | witness copy `825b46af2060d3a51f0b0202f0ce70a0ad5c43ab655fee0279845748a568bb0c` | `TestLinuxV2CleanupShapeWalkRows` exit 1, 2 failures (`ancestor-refusal`, `open-error`: `open "binding-omp" … bad file descriptor`) |

Oracle gap (fix round r1): the witness call site at
`daemon/e2e/production_linux_v2_cleanup_witness_test.go:574` runs only inside the credentialed
`synthetic_goal_cleanup` case set, so deleting that line reddens no locally runnable test; the
predicates it calls are committed-tested (the rows above), the call itself is covered only by the
conductor's Bind. Carried in `docs/TODO.md`.

## 8. Local gates (Windows and WSL, from the delivered worktree)

| Platform and command | Exit | Observation |
| --- | ---: | --- |
| Host Bash: `bash -n scripts/ci/linux-v2-native-acceptance.sh` | 0 | no syntax errors |
| WSL Bash: `bash -n` on the same bytes | 0 | no syntax errors |
| WSL Python: `python3 -B scripts/ci/test-linux-v2-evidence.py` | 0 | 57 tests OK |
| Host Go 1.27.4, from `daemon/`: `go vet ./...` | 0 | whole module, Windows |
| Host Go: `gofmt -l e2e/` | 0 | no files listed |
| WSL Go (same module), from `daemon/`: `go vet ./...` | 0 | whole module including the linux witness |
| WSL Go: `go test -count=1 ./e2e` | 0 | package ok (env-gated witnesses skip; the committed shape rows run) |
| WSL Go: `go test -count=1 -run '^TestLinuxV2CleanupShapeRefusalRows$' ./e2e` | 0 | 9 rows pass |
| WSL Go: `go test -count=1 -run '^TestLinuxV2CleanupShapeWalkRows$' ./e2e` | 0 | 4 rows pass (clean tree, root refusal, ancestor refusal, open error) |
| Fix round r1 (docs only), same code bytes (script `60d63a8b…`, python `e1d3d68a…`, witness `8cea4985…`): WSL `python3 -B scripts/ci/test-linux-v2-evidence.py`; WSL `go test -count=1 -run '^TestLinuxV2CleanupShapeRefusalRows$\|^TestLinuxV2CleanupShapeWalkRows$' ./e2e` | 0 / 0 | 57 tests OK; the 9 refusal and 4 walk rows pass |

Head-wide suites (the daemon race suite and the credentialed case set) are CI's and the conductor's,
not claimed here.

## 9. Acceptance still pending

The conductor's re-Bind must run `synthetic_goal_cleanup` once on the PR head and show that the
`:882 … Linux owner anchor returned no exact root clone-removal proof` stop is gone. The expected
next observation is a passing `TestProductionLinuxV2CloneCleanupAuthenticatedScratch` through
launcher removal, the A-side cleanup and journal retirement, or — if anything later refuses — the
next failing witness line verbatim in the goal's residual section of `docs/TODO.md`. The local
probes cannot establish either result.

## 10. Committed-blob gates

The scratch clone holds `d4e455f1` plus the delivered paths, with the shared jscpd install
symlinked per brief-rules; the checks run there against this record's delivered bytes (fix round
r1, exit codes as run):

| Command | Exit | Observation |
| --- | ---: | --- |
| `python -B scripts/ci/jscpd-changed.py --base d4e455f1 --head HEAD` | 0 | 7 changed files, 1792 scanned sources, 1245 clones in the corpus, none new; the gate notices `docs/archive/CLOSED-TODO.md` is outside its scan scope |
| `python -B scripts/ci/check-record-cleanliness.py --base d4e455f1 --head HEAD` | 0 | 1 changed record passes the root-only check |
| `python -B "$comment_cap" "$scratch_clone" d4e455f1` | 0 | no over-cap comment block among the added lines |

No gate in §8 substitutes for them.

## 11. Residuals

- Production observation (decision note, not fixed here): an install-accepted clone root whose
  default ACL mask grants write produces a checkout whose directories and files are group-writable,
  which the exact cleanup checks refuse. Fail-closed, but the clone is uncleanaable without
  operator intervention. Candidates: reject a writable default mask in the install preflight, set
  the created modes explicitly in the clone bootstrap, or make the cleanup mode checks ACL-aware.
  The same shape remains on the production, daemon-service-workspace and clone-test fixture roots
  (`scripts/ci/linux-v2-native-acceptance.sh:691-693`, `:878`); no cleanup leg touches them
  today, and this increment's owned scope is the `synthetic_goal_cleanup` arm only.
- The anchor's status-blind client error (`linux_owner_anchor_linux.go:81`) stays; the witness-side
  shape assertion is the attribution available without a production protocol change.

## 12. Fix-round r1 appendix — mask-matrix probe (verbatim)

Scratch probe behind §5.1, sha256
`382cfe8dbff029dc63dcad4e3a00ac54ad764506404d14491bb670ad408fbdbf`, run as
`bash probe-mask-matrix.sh <scratch>` in the target WSL distro as root; its log `a7d8b090…` (19
lines, exit 0) is the table's source. It creates a B-owned `0750` clone root with each default-ACL
set and makes the entries through the same calls the bootstrap, Git and the setup shell use
(`os.mkdir(0o770)` under `umask 077`; redirects and `os.open` under `umask 022`).

```bash
#!/usr/bin/env bash
# r1 re-measurement: B-owned clone root, two default-ACL shapes, bootstrap/worker creation modes.
set -eu
W=${1:?usage: probe-mask-matrix.sh <work-dir>}
A=52000
B=52001
rm -rf "$W"
mkdir -p "$W"
cat > "$W/create.py" <<'PY'
import os, sys
base = sys.argv[1]
os.umask(0o077)
os.mkdir(os.path.join(base, "bootstrap-dir"), 0o770)
os.umask(0o022)
for name, mode in (("open0644.txt", 0o644), ("open0666.txt", 0o666), ("open0664.txt", 0o664)):
    os.close(os.open(os.path.join(base, name), os.O_CREAT | os.O_WRONLY, mode))
PY
chmod 0755 "$W/create.py"
mkdir "$W/red" "$W/green"
chown "$B:$B" "$W/red" "$W/green"
chmod 0750 "$W/red" "$W/green"
setfacl -m "u:$A:r-x,m::r-x,d:u:$A:r-x,d:u:$B:rwx,d:m::rwx" "$W/red"
setfacl -m "u:$A:r-x,m::r-x,d:u:$A:r-x,d:m::r-x" "$W/green"
as_b() { setpriv --reuid="$B" --regid="$B" --clear-groups bash -c "$1"; }
for d in red green; do
  as_b "umask 022; printf 'shell\n' > '$W/$d/sh-redirect.txt'; printf 'candidate\n' > '$W/$d/candidate.txt'; mkdir '$W/$d/gitinit'"
  as_b "umask 022; git init -q '$W/$d/gitinit'"
  as_b "python3 '$W/create.py' '$W/$d'"
done
for d in red green; do
  printf 'MASK-MATRIX %s default-mask %s\n' "$d" "$(getfacl -cpn "$W/$d" | sed -n 's/^default:mask:://p')"
  printf 'MASK-MATRIX %s clone-root %s\n' "$d" "$(stat -c %a "$W/$d")"
  printf 'MASK-MATRIX %s os.mkdir-0770-under-umask-077 %s\n' "$d" "$(stat -c %a "$W/$d/bootstrap-dir")"
  printf 'MASK-MATRIX %s shell-redirect %s\n' "$d" "$(stat -c %a "$W/$d/sh-redirect.txt")"
  printf 'MASK-MATRIX %s candidate.txt %s\n' "$d" "$(stat -c %a "$W/$d/candidate.txt")"
  printf 'MASK-MATRIX %s git-init-.git-config %s\n' "$d" "$(stat -c %a "$W/$d/gitinit/.git/config")"
  printf 'MASK-MATRIX %s open-0644 %s\n' "$d" "$(stat -c %a "$W/$d/open0644.txt")"
  printf 'MASK-MATRIX %s open-0666 %s\n' "$d" "$(stat -c %a "$W/$d/open0666.txt")"
  printf 'MASK-MATRIX %s open-0664 %s\n' "$d" "$(stat -c %a "$W/$d/open0664.txt")"
done
printf 'MASK-MATRIX kernel %s\n' "$(uname -r)"
```
