# rf-w2-14 authority split plan — authority.go split + clone/equal unification

Contract: the GDD goal contract `goal-rf-w2-14-authority-split.md` (2026-11-21, at the GDD
state root) plus its binding review amendments (`refactor/w2-contract-review-opus-1.md`
P1-5/P2-6/P3, `refactor/w2-contract-review-ds-1.md` GO + note N3). Baseline for every
coordinate in this plan: HEAD `4dd374bdcdbe09a05287e77ad90faaa949fcddcd` (= `origin/main`,
= the commit that landed W1-04/#143). Area: `daemon/internal/authority` only, plus this
plan file.

The slice moves the 165 top-level declarations of the 2173-line `authority.go` into three
contract-named family files (`linux_clone.go`, `cgroup.go`, `supervisor.go`), then
unifies the four copy-paste clusters findings F-26 measured on the three supervisor-family
durable records (`Supervisor`, `SupervisorHandoff`, `SupervisorHandoffReleaseProof`)
behind one unexported `supervisorCore` value and one conversion builder.

## What moves (measured on this tree)

| Target (`//go:build`) | Decls (type/method/func/const) | Lines after `gofmt` |
| --- | --- | --- |
| `linux_clone.go` (none) | 68 (12/40/16/0) | 673 |
| `cgroup.go` (none) | 55 (11/31/13/0) | 601 |
| `supervisor.go` (none) | 42 (6/19/16/1) | 913 |
| `authority.go` (deleted) | 165 | 2173 |

`apply` + `gofmt` change no declaration byte: the declaration-multiset check
(`pfs declset`, W1-04's proof) compares raw and go/printer-normalized digests per
declaration and per comment group, base `authority.go` vs the three new files —
248 = 248 lines, `diff` exit 0. The only line delta is the regenerated per-file header
(package line, recomputed import block and its blank lines): 2187 = 2173 + 14.

The logic stage then brings `supervisor.go` to 875 lines: production totals
2149 = 2173 − 24 (net delta for the whole slice, reported not gated), and the
supervisor family alone shrinks by 38 lines relative to the move-only state.

Default (`GOOS=linux`, `GOOS=windows`, `GOOS=darwin`) and linux/arm64 builds see exactly
the same declarations as before; the package had no build constraints, and no new file
introduces one.

## Public / API shape

Unchanged, and provable as such:

- every existing `type` declaration stays textually identical (per-name AST/raw digest
  comparison; the only added type is the unexported `supervisorCore` the contract
  mandates);
- `go doc -all ./internal/authority` diff is empty apart from the added unexported type,
  which `go doc -all` does not print for a non-exported identifier;
- JSON field order and tags stay literal: the three record declarations are moved, not
  edited, and `supervisorCore` is a *value* used by behaviours only — it is never
  embedded (anonymous or named) in a durable record, so no encoding/decoding path
  changes.

The exported surface is consumed by `state`, `app`, `protocol`-adjacent callers, `cmd`
and the `e2e` witness tests (118 `authority.SupervisorHandoff`, 110 `authority.StopReceipt`
references outside the package); nothing in the slice renames, re-signs or deletes an
exported symbol.

## Role rules (the mechanically re-runnable part)

`refactor/tools/w2-14-authority-split/classify.py` holds the ordered rule table and emits
`map.json` consumed by the W1-04 splitter (`pfs apply -src <pristine> -dst <package>`),
extended with one flag: `preludeFile` names the single target that receives the source
prelude (the package doc comment), because the split has no residual file to inherit it
and duplicating the package doc into three files would also break the comment multiset.
`pfs apply` refuses to run if any declaration is unassigned or assigned twice; all 165
declarations are assigned.

1. **Type and method declarations follow their subject**: `LinuxClone*` →
   `linux_clone.go`; `LinuxCgroup*` → `cgroup.go`; `Supervisor*`/`StopReceipt` →
   `supervisor.go`. `SupervisorScanUpdate` travels with the supervisor family (review
   amendment) even though its source lines sit inside the old "cgroup 775-960" range.
2. **Functions follow the subject they are named for**: `LinuxClone*`/`linuxClone*`,
   `validLinuxClone*`, `equalLinuxClone*`, `validRequiredAuthorityString` and
   `countLinuxCloneAbortHistories` → `linux_clone.go`; `cloneLinuxCgroup*`,
   `equalLinuxCgroup*`, `validBootID`, `validCanonicalCgroupPath`,
   `validRandomLeafName` → `cgroup.go`; everything else (the bridge comparators for
   supervisor types, the conversions, the v2 field validator, the generic `valid*`/
   `clone*` leaf helpers) → `supervisor.go`.
3. **The principal file carries what cannot move**: `supervisor.go` also takes the single
   shared `const` block (one Go declaration; splitting it would be a logic edit) and the
   package doc prelude, so the package keeps exactly one documented home.

## `supervisorCore` (the logic stage)

The 23 fields the three durable records share (`version`…`stop_receipt`, in `Supervisor`'s
declaration order) become `supervisorCore`: a value snapshot of the shared durable core,
**not** a serialized shape. Helpers, each used by all three records:

| Helper | Replaces |
| --- | --- |
| extractor per record (`Supervisor.core()`, `SupervisorHandoff.core()`, `SupervisorHandoffReleaseProof.core()`) | the field lists inside the three conversions |
| `cloneCorePointers(core)` — five deep copies returned in record declaration order — used by `Clone` | `Supervisor.Clone` + `SupervisorHandoff.Clone` + `SupervisorHandoffReleaseProof.Clone` |
| `core.equal(other)` | `Supervisor.Equal` + `SupervisorHandoff.Equal` + `SupervisorHandoffReleaseProof.Equal` |
| `core.sameLaunch(other)` | `SupervisorHandoff.SameLaunch` |
| `core.matchesStoppedReceipt(receipt)` | the shared identity block of `StopReceipt.ValidFor` / `StopReceipt.ValidForHandoff` |
| `core.matchesReleaseProof(proof)` | the shared identity block of `SupervisorHandoffReleaseProof.ValidFor` / `.ValidForSupervisor` |
| `core.validateV2(requireHelper)` | `validateV2SupervisorFields(...)` (18 positional parameters) |
| `core.hasV2Fields()` | `hasV2SupervisorFields(...)` (8 positional parameters) |
| `supervisorFromCore(core)` — **the one conversion builder** | the field mapping inside `ToSupervisor`, `supervisorFromHandoff`, `supervisorFromReleaseProof` |

Behaviour that must survive the unification, field for field (each also pinned by a test):

- `SupervisorHandoff.ScanUpdate` is `json:"-"` and is **excluded** from handoff equality
  (`authority.go:1021-1023`; `state` relies on it for compare-and-set). The core never
  carries it, and `Handoff.Clone` keeps deep-copying it.
- `ToSupervisor` order of checks (validate → reject scan update → require bound helper →
  build → validate result) and its deep copies.
- `supervisorFromHandoff` is the compare-only projection: it deliberately builds **no**
  stop receipt (only `LinuxCgroupStopProof.ValidateFor` consumes it). Preserved exactly.
- `supervisorFromReleaseProof` forces `Version: SupervisorVersionV2`, has no secret and
  deliberately does not copy `CreatorSessionID`; it deep-copies the receipt. Preserved
  exactly.
- v1 branches, `StopReceipt.Equal`, `SupervisorHandoffAbortProof.Validate/ValidFor`,
  `SupervisorHandoff.ReleaseProof`, and every `LinuxCgroup*`/`LinuxClone*` behaviour are
  untouched.
- No new validation semantics: `validateV2` is the same predicate list, moved.

LOC deltas are reported, not gated (opus P3 amendment).

## Edge-case semantics the contract demands

- **Durable bytes.** Representative `Supervisor`/`SupervisorHandoff`/`ReleaseProof`/
  `StopReceipt`/clone/cgroup values marshal byte-identically before and after, including
  a fixed-JSON unmarshal→marshal round-trip arm per family
  (`run-durable-bytes.sh`: base = `git archive HEAD`, head = this worktree).
- **Type-declaration identity.** Every pre-existing `type` is textually identical; the
  only added type is `supervisorCore`.
- **Pins land first** (review amendment): per-field `Equal` sensitivity for the four
  durable records driven by reflection over every exported field (mutating any durable
  field flips `Equal`; mutating `SupervisorHandoff.ScanUpdate` does not), `SameLaunch`
  coverage, clone independence (deep-populated value, clone, mutate every reachable
  pointer in the original, clone unchanged and sharing no pointer), and conversion
  parity (`ToSupervisor`, `supervisorFromHandoff`, `supervisorFromReleaseProof` against
  the base result field by field).
- **Move-only proof.** Declaration-multiset equality (above) is the primary proof;
  `git diff --color-moved=zebra` over a scratch commit is the reviewer aid.
- **No local `-race`** (no cgo on this host); the daemon race job is the CI-only oracle.

### Delivery decomposition without committing

The dispatch forbids commits, while the review amendment asks for a pins commit, a
move-only commit and a logic commit. Chosen reading: the tree advances through those
three states in exactly that order, and each state is preserved outside the repository as
a patch plus the git tree object id it was captured from
(`refactor/tools/w2-14-authority-split/snapshot.sh` writes `stage-<name>.diff` and
`stage-<name>.tree`):

| State | Tree id sidecar | Patch (incremental step from the previous captured tree) |
| --- | --- | --- |
| pins + plan | `stage-0-pins.tree` | `stage-0-pins.diff` (HEAD → pins + plan; the pins commit carries the complete pin set, including the one assertion added after the first pin run) |
| move-only split | `stage-A-move-only.tree` | `stage-A-move-only.diff` (pins → move-only; production files only) |
| unification | `stage-B-unification.tree` | `stage-B-unification.diff` (move-only → unification; production files only) |

`git apply` in that order on a clean checkout of HEAD reproduces the deliverable tree
exactly; the replayed tree was verified equal to the tree id in
`stage-B-unification.tree`. The cumulative views of the last two steps are kept beside
the series as `stage-*-cumulative.diff` (base → that state) for reviewers. The tree ids
live in the sidecar files rather than in this plan, so the captured trees cover this
document as written. Option rejected: committing here, which the dispatch explicitly
forbids; also rejected: a single undifferentiated diff, which would make the move-only
claim unverifiable.

## Evidence plan and measured results (exact commands, run at the final tree)

| Gate | Command | Result |
| --- | --- | --- |
| format | `gofmt -l daemon/internal/authority` | empty, exit 0 |
| vet | `cd daemon && go vet ./...` | exit 0 |
| builds | `cd daemon && GOOS=linux go build ./...`; `GOOS=windows go build ./...` | exit 0 each |
| package suites | `cd daemon && go test -count=1 ./internal/authority/...` | `ok ... 0.270s`, exit 0; the same command is green at the pins-only and move-only states (`stage0-suite.log`, `stageA-suite.log`) |
| movement | `pfs declset` base `authority.go` vs the three files (multiset) | 248 = 248 lines, `diff` exit 0 |
| movement (aid) | scratch `git diff --color-moved=zebra -M` HEAD vs the move-only tree over the four production files | 2187 insertions / 2173 deletions: 2172 deleted and 2179 added lines recognized as moved; the 8 added and 1 removed unmatched lines are import-block lines only |
| durable bytes | `run-durable-bytes.sh <repo> <outdir>` | 52 output lines incl. 10 frozen-literal round-trips, identical `sha256 62f437f5…` on both sides, exit 0 |
| behaviour differential (extra oracle) | `run-differential.sh <repo> <outdir>`: 3000 randomized v2 records/receipts/proofs through the exported API, base vs head | 3001 lines each side, identical `sha256 139d7cca…`, `differential.diff` 0 bytes, exit 0 |
| API | `go doc -all ./internal/authority` base vs final | 834 lines, `diff` empty |
| type declarations | `pfs declset` type-decl digests base vs final | all 29 pre-existing types identical; exactly one added type (`supervisorCore`) |
| citations | `run-citation-gate.sh` with `CONTROL=none` / `positive` / `negative` | gate exit 0 / 0 / 1 (negative control caught as designed), script expectation met in all three runs |
| pins' sensitivity | three planted defects (Equal field drop, clone pointer share, conversion field drop) | each caught by its own pin (`planted-red*.log`) |
| whitespace | `git diff --check` | exit 0 |
| host-path hygiene | `git diff \| grep -ic "users/"` | 0 |

## Design trade-offs

- **Why a core value at all.** The three records share 23 fields but differ in JSON field
  order and in their non-core fields, and anonymous embedding is forbidden, so Go's
  struct-conversion trick (conversion to an identically-typed struct) cannot apply either
  — field *order* is part of the underlying type. The core therefore pays a one-off
  extractor per record (5 dense lines each, matching the file's existing long-comparison
  style) and buys one place each for clone, equal, same-launch, receipt matching,
  release-proof matching, v2 field validation and the conversion mapping.
- **A pointer-clone helper instead of three rebuilders.** `Clone` needs no field-by-field
  rebuild: one helper deep-clones the five shared pointer fields and returns them in the
  assignment order every record's `Clone` uses. A newly added, unwired pointer field
  would still compile, so the guard against that is the reflection pin, not the compiler:
  an aliased `StopReceipt` turns `TestSupervisorRecordsCloneIndependence` red.
- **Reflection rejected** for field copying/matching: the codebase copies these durable
  values explicitly, and reflection would hide the field list that the pins freeze.
- **Behaviour preservation over cleanup.** The `supervisorFromHandoff` receipt drop and
  the `supervisorFromReleaseProof` mappings are quirks; changing them is not requested,
  so they are preserved, commented and pinned rather than "fixed" inside a move slice.
- **Principal-file placement** (package doc, const block, generic leaf helpers) keeps the
  contract's three-file set instead of inventing a fourth `validators.go`.

## Out of scope (with reasons)

- **Protocol/control consumers, workspace, platform**: the package boundary and the
  exported API do not change; consumers are untouched.
- **New validation semantics**: `validateV2`, `Validate`, `ValidFor*` keep their exact
  predicates and branch order.
- **`internal/state`, `internal/app`, `internal/platform`**: unchanged, including their
  `authority.go`-adjacent comments; the line coordinates they cite are their own.
- **`docs/evidence/**`**: revision-bound historical records (one hashes `authority.go`);
  the review amendment forbids editing them.
- **`docs/archive/CLOSED-TODO.md`, `docs/TODO.md`**: no TODO entry is closed by this
  slice, and the audited citation corpus holds no bound authority citation at this head
  (re-verified by running the gate, with controls, at the final tree).
- **Test weakening**: no existing assertion is deleted or converted; the three suites are
  extended, and none of their names change.

## Ambiguities resolved (options and the chosen reading)

1. **`clone.go` vs `linux_clone.go`.** The contract names three files including
   `clone.go`; the review amendment renames that family file to `linux_clone.go` to match
   `authority_linux_clone_test.go` and to avoid colliding with the `Clone()` methods the
   core unifies. Chosen: `linux_clone.go` (the amendment is binding and the stricter
   reading).
2. **Which file owns the shared `const` block and the package doc.** Options: a fourth
   file (outside the contract's set), leaving them in a residual `authority.go` (the
   contract deletes it), or the principal family file. Chosen: `supervisor.go`, with the
   splitter's `preludeFile` flag making the placement explicit and checkable.
3. **Where the pins live.** The contract's owned set is the three existing test files;
   the pins use the v2 fixtures and the fully populated field set, so they land in
   `authority_v2_test.go` (the v1 suite already covers legacy JSON bytes).
4. **Move-only proof by construction.** The split is produced by a rule table plus an
   AST-preserving splitter, never by hand; `--color-moved` is reported as an aid only,
   because it cannot see a body edit inside a moved block.
