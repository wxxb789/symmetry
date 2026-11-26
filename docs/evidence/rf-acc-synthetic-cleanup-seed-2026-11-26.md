# rf-acc-synthetic-cleanup-seed — evidence record

Contract: `goal-rf-acc-synthetic-cleanup-seed.md` (GDD state root, external to the repository).
Dispatch gate: PR #302, head `8065b96e` (`main`, this branch's base); branch
`refactor/rf-acc-synthetic-cleanup-seed`. Plan:
`docs/plans/2026-11-26-rf-acc-synthetic-cleanup-seed-plan.md`. Every number below is measured at the
delivered bytes unless a row names another revision. The binding acceptance is the conductor's Bind
on the PR head (the contract allows "the next stop verbatim, or green with
`synthetic-goal-cleanup.json` exported"); §7 states what that run must show.

## 1. Subject bytes (sha256)

| File | Dispatch head `8065b96e` | Delivered | Change |
| --- | --- | --- | --- |
| `scripts/ci/linux-v2-native-acceptance.sh` | `70163ae16b80e81bec0aa3ee7e70299c3d739716a037f2d0ad4dd5a3ed57d143` | `ce35bdf3810637a1c8aa2606cb192a651c83fcce4edf24085e5b1ad2eadb9a04` | +10/−0 |
| `daemon/e2e/production_linux_v2_cleanup_witness_test.go` | `1e7a5d3a05d444ccd1b73e52df943c19978eb7b4faf921ef2e49377a6d378dcb` | identical | untouched |
| `daemon/internal/clonebootstrap/filesystem_linux_amd64.go` | `86811678e6741923bfd64031a40e0c7e531e46adb0f6f585301900fe919442af` | identical | untouched (production) |
| `daemon/internal/clonebootstrap/main_linux_amd64.go` | `3fdd0a28f79875c40073e861b2ee67b492d7526611a9a9d536fef1cef3695a70` | identical | untouched (production) |

The dispatch head's script bytes are byte-identical to the script bytes #302's Bind ran
(`70163ae1…` in findings `302-binding-run.md` §2.1), so the recorded stop and this root cause are
read at the same revision. No production byte, no witness assertion, no gate and no case-set list
changed; the delivered change is the script's `synthetic_goal_cleanup` fixture, the plan, this
record, and the two TODO files.

## 2. Root cause (Stage 1)

### 2.1 The chain, end to end

The one-use RELEASE runs the fixed B clone bootstrap
(`daemon/internal/clonebootstrap/main_linux_amd64.go:124`): `openCloneSeed` opens the seed parent
chain with `openReadOnlyAPathDirectory`
(`daemon/internal/clonebootstrap/filesystem_linux_amd64.go:95`; `:309-345`), walking from `/` and
opening every component `O_RDONLY|O_DIRECTORY|O_CLOEXEC|O_NOFOLLOW` (`:326`). Opening a directory
`O_RDONLY` requires *read* on it (not just traverse), and B in the gate's user namespace is the
host B kuid with no supplementary groups (`daemon/native/linux-gate/c-gate/gate.c:1012`
`setresuid(1,1,1)`; `daemon/internal/platform/anchor_gate.go:528` pins the `1 <B_UID> 1` map with
`setgroups` denied).
The witness fixture publishes the case root `/opt/symmetry-owner-test/synthetic-goal-cleanup` as
`root:A_GID 0751` (`scripts/ci/linux-v2-native-acceptance.sh:589` at the dispatch head), so B is the
`other` class with `--x` only: the `openat(<case root>, `O_RDONLY|O_DIRECTORY`)` fails `EACCES`, and
every seed-stage step fails there. `openCloneSeed` wraps it as `open A-owned seed parent without
symlinks: %w` (`:95-97`); the bootstrap returns `fail(CloneFailureSeed, err)`
(`daemon/internal/clonebootstrap/main_linux_amd64.go:126`), its command main encodes that as a
clone-bootstrap failure frame (`daemon/cmd/symmetry-linux-clone-bootstrap/main.go:27`), and the
helper's checkout-receipt read (`daemon/native/linux-gate/protocol/wire_linux.go:600-613`) turns
it into the labelled detail
`stage=clone_checkout arm=checkout_receipt wait_class=other guest_stage=seed errno=13`
(`daemon/native/linux-gate/protocol/gate_lifecycle_linux.go:1108-1113`, `:761-790`, `:835-868`,
rendered by the release detail's `String()` (`daemon/internal/platform/helper_v2_protocol.go:486-505`)).
The journal rows the Bind recorded
(`has_clone_intent:true, has_seed_receipt:true`) are A-side receipts and are consistent: the seed
exists and is B-readable; only its chain is not.

### 2.2 Discrimination — the case root is the only failing component, and no other producer fits

- Every other component of the seed and clone-root chains is B-readable in the fixture: `/opt` and
  `/opt/symmetry-owner-test` are `0755` (`scripts/ci/linux-v2-native-acceptance.sh:588`), `seeds`
  carries the `u:B:r-x` ACL with defaults (`:596-597`), the binding/run directories inherit those
  defaults (created `Mkdirat(…, 0o750)` by
  `daemon/internal/workspace/linux_clone_fs_linux.go:576`), and the bundle itself is published
  B-readable by production (`daemon/internal/workspace/linux_clone_linux.go:2198`,
  `Fchmod(fd, 0o440)` "protect B-readable Linux clone seed"). The clone root `clones` is B-owned
  `0700` (`:595`, `:598-599`). The case root is the single
  B-unreadable component on both walks.
- Not a production pin or gate defect: the same `openCloneSeed`/`openAbsoluteDirectory` code is
  green on the #296 clone legs and on the production chain (§2.4), and the failing syscall needs
  only the fixture's mode to change.
- Not AppArmor: the installed per-program profile is exactly one `userns,` rule under
  `flags=(unconfined)` with no file rules and no globs
  (`daemon/internal/linuxpreflight/preflight_linux.go:224-229`), so it cannot produce a path
  `EACCES`; the Bind's seven AppArmor rows are green.
- Not the credential seed receipt or containment: `has_seed_receipt:true` and the A-side receipt
  checks passed (`daemon/internal/workspace/linux_clone_linux.go:142-160`), and the guest-side
  checks that remain are unchanged by this fix (§3).
- Not mount propagation: the walk uses the same namespace as the rest of the RELEASE, whose earlier
  stages (image pin, gate, seed creation) all passed at the same revision.

### 2.3 Reproduced on the resolute replica (the #296 S5 method)

`path-sha256:dba63390a9da5f52d3a7ffab205e03c8a75c3c703da0942ac059b6dad88a28ec` (Ubuntu 26.04 LTS userspace, the same userspace the fixture
debootstraps) with a scratch probe built
`CGO_ENABLED=0` from `git archive 8065b96e daemon`
(sha256 `18f38d94aa71f53229c2953f677e68a8deeb3ffd775326b004245b6b630c330a`) plus one scratch test
file (`3f723a8dad3903ac017ff887b21bb45f579704630add5c4b87e5bd983d03a904`) that calls the production
`openCloneSeed` + `verifyCloneSeed` and `openAbsoluteDirectory`; binary
`c09291124eb7b1694b7cffcc508e888380310de286d425dbb8e820ab046b39df`. Each shape is entered
`chroot path-sha256:dba63390a9da5f52d3a7ffab205e03c8a75c3c703da0942ac059b6dad88a28ec /usr/bin/setpriv --reuid=52001 --regid=52001
--clear-groups` (the guest's B kuid; no supplementary groups). Fixture shapes are built to the
script's own spec (`fixture-setup.sh`): red = `install -d -o root -g 52000 -m 0751 <case root>`;
green = red plus `setfacl -m "u:52001:r-x,m::r-x"` on the case root (the delivered fix's exact ACL
spec); baseline = the production shape (`0755 root:root` parents). Log
`path-sha256:2122c9399acfc230e8c3a2b6b0cb80e942aa29ddd4e60d325f2095b692c6dfb6` (sha256 `f56b9e653409430bcbc845c9b4f8fb8ad9d576f66e482a60eb38c20278850f82`),
verbatim rows:

```
FIXTURE stat path-sha256:f1318fde4f7a1ea3e06eba80d09db51da4925090fc9aa2fd83f47c8585fbf561 0:52000 751
FIXTURE acl caseroot user::rwx
FIXTURE acl caseroot group::r-x
FIXTURE acl caseroot other::--x
B_TEST red -x 0
B_TEST red -r 1
PROBE seed chain refused: errno=13 text="open A-owned seed parent without symlinks: permission denied"
PROBE clone root refused: errno=13 text="permission denied"
PROBE_MODE=red PROBE_RC=0
FIXTURE acl caseroot user:symmetry-b:r-x
B_TEST green -x 0
B_TEST green -r 0
PROBE seed chain accepted and verified: /opt/symmetry-owner-test/synthetic-goal-cleanup/seeds/binding-native/run-probe-run-000000000000/generation-1.bundle
PROBE clone root accepted: /opt/symmetry-owner-test/synthetic-goal-cleanup/clones mode=040750 uid=52001 gid=52001
PROBE_MODE=green PROBE_RC=0
B_TEST baseline -x 0
B_TEST baseline -r 0
PROBE seed chain accepted and verified: /opt/symmetry-owner-test/production/seeds/binding-native/run-probe-run-000000000000/generation-1.bundle
PROBE clone root accepted: /opt/symmetry-owner-test/production/clones mode=040750 uid=52001 gid=52001
PROBE_MODE=baseline PROBE_RC=0
```

The probe is written as an assertion of the observed capability, so each row's exit is 0 and the
observation text is the evidence: `red` asserts the refusal *is* `EACCES` (`errno=13`, matching the
recorded `errno=13`), `green` requires the full production `openCloneSeed` + `verifyCloneSeed`
(identity, digest, write-denied checks on every ancestor, path re-bind) and the production
`openAbsoluteDirectory` (`O_RDONLY` walk plus B-write check) to pass, and `baseline` shows the
unchanged production code accepts the production-shaped chain. In all three shapes the seed file
itself is `52000:52000 0440` with `user:symmetry-b:r-x #effective:r--`, `mask::r--`, so the
refusal is solely the case-root component.

### 2.4 The green comparison and why the fixture is the wrong side

The #296 clone legs (`daemon/native/linux-gate/protocol/clone_integration_linux_test.go:38`,
`TestInstalledLinuxOwnerCloneCheckoutScratch`) run the same `openCloneSeed` walk and are green in
every run: their seed root sits under `0755` parents with the seed-root ACL
(`scripts/ci/linux-v2-native-acceptance.sh:588`, `:875`) and the test publishes the bundle `0440`
next to an inherited `u:B` entry
(`daemon/native/linux-gate/protocol/clone_integration_linux_test.go:502-516`). The fixture's `0751`
case root is therefore not a hardening the guest supports: production's own publisher calls the
0440 seed "B-readable", so a B-unreadable ancestor contradicts the seed contract. The fix belongs
on the fixture side; a production change (e.g. walking ancestors `O_PATH`) would weaken the walk for
every deployment to accommodate one fixture mode.

## 3. The change (Stage 2, fixture only)

- `scripts/ci/linux-v2-native-acceptance.sh:592`: `setfacl -m "u:$B_UID:r-x,m::r-x"` on the case
  root, beside the arm's existing seed/clone ACL rows, with a 2-line why comment naming the walk.
- `:1459-1465`: three committed assertion rows in the arm's own fixture checks — B must be able to
  read the case root and the seed root, and A must not hold write on the case root — so deleting
  the grant, re-tightening either path, or widening the case root's class reddens the set before
  the witness starts. `fail` aborts the case set, and the rows run inside the
  `synthetic_goal_cleanup` guard, so no other set observes them.
- Containment unchanged: `config.json` stays `root:A 0640` and B-unreadable (the existing
  `B unexpectedly read …` refusal row stays), the source stays A-private, the seed keeps `0440` with
  its ACL mask, and the guest's write-denied checks on every ancestor are untouched. B gains only
  the directory read the bootstrap itself demands. Rejected alternatives (mode `0755`, relocating
  the seed/clone roots, the production walk change) are recorded in the plan §3.

## 4. Check before the change — red-first and replacement proof

The clause test is the `synthetic_goal_cleanup` case set (installed owner, disposable guest,
Control); it cannot run on this host, so the red-first evidence is (i) the recorded Bind stop
verbatim (findings §3.1, re-Bind `45803728792` on `5f744370`) and (ii) the replica characterization
in §2.3, where the *delivered production bytes* refuse the *dispatch-head fixture* with exactly
`errno=13` and accept it once the fix's ACL is applied. The committed red-first row of this change
is the case-root read row: the replica probe's red shape is the dispatch-head fixture, where the
case root is `0751` and B is `other` with `--x` only (`B_TEST red -r 1`), and its green shape is
the delivered fixture (`B_TEST green -r 0`); the seed-root read row guards the arm's pre-existing
seed ACL (`u:$B_UID:r-x,m::r-x` since #296), so it was already satisfiable at the dispatch head.
The rows' in-repo form runs inside the set and aborts it on a regression.

## 5. Local gates

| Gate | Command | Exit | Result |
| --- | --- | --- | --- |
| script syntax | `bash -n scripts/ci/linux-v2-native-acceptance.sh` | 0 | no diagnostic |
| fixture contracts | `(cd daemon && python3 ../scripts/ci/test-linux-v2-{source-subject,go-events,evidence,host-prerequisite-probe,owner-request-keys}.py)` and `python3 scripts/ci/test-rootless-compose-owner-claim.py` | 0 each | 6/6 ok, including the script-parsing evidence test |
| CI structure pins | `python3 scripts/ci/test-ci-structure-extraction.py`; `python3 scripts/ci/test-ci-changed-path-classifier.py` | 0 each | 2/2 ok |
| record cleanliness (CI doc gate) | `python scripts/ci/check-record-cleanliness.py --base 8065b96e --head <final delivered commit>` | 0 | 1 changed record passes the root-only check; the handover bytes carried 8 host paths and were refused (simplify finding 1, redacted in §8) |
| citation existence (CI doc gate) | `python scripts/ci/check-citation-existence.py --base 8065b96e --head <final delivered commit>` | 0 | 1 changed document passes the path and symbol check (699 path citations); 3 pre-existing ignore-covered notices (`CLOSED-TODO.md:737`, `:752`, `:2512`) |
| shellcheck (advisory, CI's own selection) | `shellcheck --severity=warning --format=gcc scripts/ci/*.sh`, one invocation per file, at the base and at the delivered bytes | 1 both | 20 findings before, 20 after; the same finding set with only the line numbers shifted (+3 above the case-root block, +10 below it); none on an added line |
| format | `(cd daemon && gofmt -l .)` (WSL) | 0 | no file listed |
| linux vet | `(cd daemon && GOOS=linux go vet ./...)` (WSL) | 0 | clean log |
| windows vet | same, `GOOS=windows` (WSL) | 0 | clean log |
| comment-cap (committed diff) | `python …/comment-cap.py <scratch clone of the delivered code bytes>` | 0 | 5 changed files, no over-cap block; the only added comment is the 2-line why comment |
| jscpd-changed (delivered range, script + docs) | `python scripts/ci/jscpd-changed.py --base <merge-base> --head HEAD --repo <scratch clone>` | 0 | 5 changed file(s), 1788 scanned source(s), 1245 clone(s) in the corpus, none new among the added lines; re-run at the final bytes below |
| WSL full suite | `sudo -u accept -i bash -lc 'umask 022; cd daemon && go test -count=1 ./...'` on the delivered tree (ext4) | 0 | 31 `ok` + 3 `[no test files]`, 0 `FAIL`; log sha256 `a32ff914ba5317dd2b9e1dfeb37b561a0e05221c9f6f680ba0f7609347fcaa51` |
| Windows full suite | `(cd daemon && go test -count=1 ./...)` on the Windows host, Go 1.27.4 | 0 | 27 `ok` + 3 `[no test files]`, 0 `FAIL`; log sha256 `546726f998e03c0c374d97004fcda31939c7c9277cf9ab7f7440d2fc90a8272a` |
| replica rows | §2.3 commands | 0 each | red/green/baseline observations above |

All rows ran on the delivered bytes (`scripts/ci/linux-v2-native-acceptance.sh` sha256 `ce35bdf3…`
inside the gate tree); the full-suite rows are independent of the doc-only delta that follows them,
and the cheap gates (script syntax, fixture contracts, comment-cap, jscpd) are re-run at the final
bytes in §8.

## 6. Adversarial satisfaction

- The two new B-read script rows cannot be satisfied by an unrelated event: `machine_run_as B test -r <dir>`
  exits non-zero exactly when B's path resolution or read access is denied (measured pair:
  `B_TEST red -r 1` / `B_TEST green -r 0` on the same script spec), and either row's non-zero exit
  aborts the case set through `fail`, so a pass requires B to hold real read access — which is the
  requirement itself, however it is granted (ACL or a wider mode).
- The A-write row cannot pass vacuously: the same guard installs the config and asserts A reads it
  and B reads the case and seed roots (`:1450-1462`), so the case root must exist and resolve
  before the row runs, and `machine_run_as` itself aborts when the guest passwd entry is
  unreadable. Measured pins as A on the replica: delivered group class `r-x` → `test -w` exit 1
  (silent); group class widened to `rwx` → exit 0 (fires); mask-only widening `m::rwx` → exit 0
  (fires) even though the replica's kernel still refuses the underlying `open` there, so the
  predicate never stayed silent on a measured widening.
- The replica rows cannot be satisfied by an unrelated syscall error: the denied shape asserts the
  exact class (`unix.EACCES`, not any error) and the readable shape requires the complete
  production seed open and verify (write-denied on every ancestor, identity, digest, path re-bind)
  and the production clone-root open, so a walk that skipped a check or accepted a writable
  ancestor fails the row.
- The case-set observation cannot be vacuous: each dispatch is gated by
  `scripts/ci/assert-go-test-json.py` on exactly one run and one pass of
  `TestProductionLinuxV2CloneCleanupAuthenticatedScratch` (`run=1 pass=1 skip=0`, `-count=1`), and
  the workflow requires the set's exit 0 plus a sanitized manifest — a skipped or unrun witness
  cannot satisfy the acceptance, and the added fixture rows run before the witness, so a fixture
  regression cannot be masked by a witness that never starts.

## 7. Residuals and the next stop

The set's stop after the seed stage is unmeasurable on this host (the case set needs the installed
owner, the disposable guest and Control). The conductor's Bind on this PR head supplies it: the
`daemon/e2e/production_linux_v2_cleanup_witness_test.go:506` line and its `guest_stage=seed errno=13` detail
must be gone from the log (the replica rows and the fixture assertions cover both B walks that
failed); a green set exports `synthetic-goal-cleanup.json` with a manifest, and otherwise the next
first failing line is recorded verbatim in `docs/TODO.md` under
`## rf-acc-synthetic-cleanup-seed: residuals (2026-11-26)`. The `:495` termination-fallout line
(`dial unixpacket: connect: connection refused`) is the same incident's second stop and is expected
to disappear with a resolved RELEASE (#302 record §2.4). No new residual is created by this change:
the fix removes the refusing component rather than moving it (the clone-root walk, the next
fixture-dependent walk in the sequence, is proven passing in §2.3).

## 8. Artifacts and the final-bytes re-measure

Scratch (outside the repository): `path-sha256:74c365bb1069cc6ae0f6f9c2269f40acd67157089b2d30b0aa4b9b2be6087827` on this host retains the probe source, the
fixture and run scripts, the built probe and the raw logs quoted above, with the WSL-side copies
under its `wsl-out/` (including `run-probe2.out`, sha256 `f56b9e65…`); the WSL scratch tree
(`path-sha256:17f43c385b1dacd26a70a4b3c9203c7dc9eeb226759c3c49e4b6ebd9bd1b641b`) was removed after that copy, and the replica additions
(`/opt/symmetry-owner-test/synthetic-goal-cleanup`, `…/production`, `path-sha256:5c6bf10f2d53df86dd8d92c771d06bdc0c88ea503f2d2593a31427705b4b71a9`) were
removed after the run, returning `path-sha256:dba63390a9da5f52d3a7ffab205e03c8a75c3c703da0942ac059b6dad88a28ec` to its previous state. The
final-bytes re-measure of the cheap gates and the scratch-clone commits (comment-cap, jscpd) is
recorded below.

Final-bytes re-measure (scratch clone of `8065b96e` + the five delivered files; script sha256
`ce35bdf3810637a1c8aa2606cb192a651c83fcce4edf24085e5b1ad2eadb9a04`):

| Gate | Exit | Result |
| --- | --- | --- |
| `bash -n` on the delivered script | 0 | no diagnostic |
| the 8 fixture/structure Python tests above, re-run at the final bytes | 0 each | 8/8 ok |
| comment-cap on the final-bytes commit | 0 | 5 changed files, no over-cap block (markdown is outside its scan, so the record's later prose edits cannot change the verdict) |
| jscpd-changed on the final-bytes commit | 0 | 5 changed file(s), 1788 scanned source(s), 1245 clone(s) in the corpus, 0 tolerated among the added lines, none new (re-run green after every later prose edit; the row's verdict, not its corpus counts, is the gate); notice: `docs/archive/CLOSED-TODO.md` carries added lines but sits above the gate's `--max-lines 5000` ceiling, so it is reported as a notice and never silently skipped (5624 lines at the base, 5659 delivered, unchanged class) |
| record cleanliness on the final-bytes commit | 0 | 1 changed record passes the root-only check (the handover bytes were refused with 8 host paths, redacted above) |
| citation existence on the final-bytes commit | 0 | 1 changed document passes the path and symbol check (699 path citations); 3 pre-existing ignore-covered notices |
| A-write refusal pin (replica, as A) | 0 | delivered shape `test -w` exit 1 (row silent); mask-only `m::rwx` exit 0 and group class `rwx` exit 0 (row fires); log sha256 `d819c45c2cb6b83a7c3f54b02e728164f15a59101a2cd2cbeab5a0d0e299ae81` |

Delivered documentation bytes: plan `15f03ac6625b3e9949c98077c44663808dd32ac7168f1b1da09949f506c5a835`
(85 lines), `docs/TODO.md`
`0740e34b7f56522b0e034fa7b517ffaa4f35ac28e100141a4b669d4108659b82` (+30/−11),
`docs/archive/CLOSED-TODO.md`
`ad7ceca558ae99a6dfc7db34750f91a7cb8569983c230996caafb03f2a1f2ca9` (+35/−0); this record is the
fifth delivered file.
