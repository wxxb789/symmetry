# rf-w2-14 authority split plan — authority.go split + clone/equal unification

Contract: the GDD goal contract `goal-rf-w2-14-authority-split.md` (2026-11-21, at the GDD
state root) plus its binding review amendments (`refactor/w2-contract-review-opus-1.md`
P1-5/P2-6/P3, `refactor/w2-contract-review-ds-1.md` GO + note N3). Baseline for every
coordinate in this plan: HEAD `4dd374bdcdbe09a05287e77ad90faaa949fcddcd` (= `origin/main`,
= the commit that landed W1-04/#143). Area: `daemon/internal/authority` only, plus this
plan file. The GDD state paths this plan names are provenance for how the slice was
produced: they do not resolve under the repository root, and every reproduction step
that can run in the repository is listed in the evidence section below; the tools that
stay outside it are labelled as such there and in the "Role rules" section.

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

`apply` + `gofmt` change no declaration byte. The exhaustive declaration-multiset check
(raw and go/printer-normalized digests per declaration and per comment group, base
`authority.go` vs the three new files — 248 = 248 lines, `diff` exit 0) ran outside this
repository (the W1-04 `pfs declset` proof in the GDD state root; the reviewer re-ran it
independently with an own `go/ast` tool). The only line delta is the regenerated
per-file header (package line, recomputed import block and its blank lines): 2187 = 2173 + 14.

The logic stage then brings `supervisor.go` to 875 lines; the apply stage's accepted
simplify finding R1 replaces the `Supervisor.core()` field literal with a struct
conversion and takes it to 873: production totals 2147 = 2173 − 26 (net delta for the
whole slice, reported not gated), and the supervisor family alone shrinks by 40 lines
relative to the move-only state.

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
and the `e2e` witness tests. Counts are scoped: `internal/state` + `internal/app` hold
118 `authority.SupervisorHandoff` (39 + 79, `rg -o 'authority\.SupervisorHandoff\b'` on
the exact symbol) and 110 `authority.StopReceipt` (19 + 91) occurrences; repo-wide the
same exact symbols appear in Go files 184 non-test / 119 test (`SupervisorHandoff`) and
154 / 126 (`StopReceipt`) times. Nothing in the slice renames, re-signs or deletes an
exported symbol.

## Role rules (the classification that produced the split)

The split was produced by the W1-04 declaration splitter driven by an ordered rule table.
The rule table (`classify.py`) and the splitter (`pfs apply -src <pristine> -dst
<package>`) live in the GDD state root, outside this repository; they are provenance for
how the split was made, not a reproduction step a repo reader can run. The rule table
extended the splitter with one flag: `preludeFile` names the single target that receives
the source prelude (the package doc comment), because the split has no residual file to
inherit it and duplicating the package doc into three files would also break the comment
multiset. The splitter refuses to run if any declaration is unassigned or assigned twice;
all 165 declarations were assigned.

The move-only claim is reproduced from this repository by declaration identity and the
exported API, not by re-running the splitter:

- `git show 4dd374bd:daemon/internal/authority/authority.go` recovers the whole 2173-line
  base file; a reviewer compares it declaration by declaration against the three head
  files.
- The exhaustive per-declaration byte-identity comparison (raw text, doc comment
  included, keyed by kind+receiver+name) ran reviewer-side with a `go/ast` tool that is
  not in this repository; its result — 146 of the 165 base declarations byte-identical —
  is recorded in the PR #158 description section "Pure-move evidence".
- `go doc -all ./internal/authority` at base and head prints the same 834 lines (`diff`
  empty); the exact command is in the evidence section.
- `git diff --color-moved=zebra 4dd374bd..HEAD -- daemon/internal/authority` is a
  reviewer aid only: it shows moved lines, but cannot see an edit inside a moved block.

The ordered rules that classified the 165 declarations:

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
  a fixed-JSON unmarshal→marshal round-trip arm per family. The base-vs-head marshal
  harness (`run-durable-bytes.sh`) lives in the GDD state root, outside this repository;
  the in-repo oracles are the legacy JSON byte-shape pins
  (`TestAddingLinuxCgroupV2KeepsLegacyAuthorityJSONByteShape`,
  `TestStopReceiptBindsOwnerAndPreservesLegacyJSON`) plus the 29 byte-identical type
  declarations, which leave JSON field order and tags literal by construction.
- **Type-declaration identity.** Every pre-existing `type` is textually identical; the
  only added type is `supervisorCore`.
- **Pins land first** (review amendment): per-field `Equal` sensitivity for the four
  durable records driven by reflection over every exported field (mutating any durable
  field flips `Equal`; mutating `SupervisorHandoff.ScanUpdate` does not), `SameLaunch`
  coverage, clone independence (deep-populated value, clone, mutate every reachable
  pointer in the original, clone unchanged and sharing no pointer), and conversion
  parity (`ToSupervisor`, `supervisorFromHandoff`, `supervisorFromReleaseProof` against
  the base result field by field).
- **Move-only proof.** Declaration-multiset equality (reviewer-side, above) is the
  primary proof; the empty `go doc -all` diff and `git diff --color-moved=zebra`
  between `4dd374bd` and HEAD are the in-repo checks.
- **No local `-race`** (no cgo on this host); the daemon race job is the CI-only oracle.

### Delivery decomposition (conductor waiver, 2026-11-21)

The dispatch forbade commits, while the binding review amendment asked for a pins commit,
a move-only commit and a logic commit. The tree advanced through exactly those states in
order, but the states were captured outside git as a patch series plus tree-id sidecars
in the GDD state root (`refactor/tools/w2-14-authority-split/...`); no ref reaches the
captured trees, so a `git gc` can prune them. The conductor waived the three-commit
sequencing on 2026-11-21 instead of rebuilding the branch history: the PR is
squash-merged, so intermediate commits would not survive on main, and a restructured push
needs conductor authority. The pure-move evidence that replaces the commit split — the
reviewer-side top-level declaration comparison (146 of the 165 base declarations
byte-identical, 2 removed and 12 added unexported, 17 supervisor-family declarations
rewritten) plus the empty `go doc -all` diff — is recorded in the PR #158 description
section "Pure-move evidence". The sidecar patches are provenance, not reproduction steps:
this repository carries the pinned tests, the declaration-identity evidence and the API
diff a reviewer checks.

## Evidence plan and measured results (exact commands, run at the final tree)

Runnable from this repository (from the repo root unless a `cd daemon` is shown;
`daemon/` is its own Go module):

| Gate | Command | Result |
| --- | --- | --- |
| format | `gofmt -l daemon/internal/authority` | empty, exit 0 |
| vet | `cd daemon && go vet ./...` | exit 0 |
| vet (linux target) | `cd daemon && GOOS=linux go vet ./...` | exit 0 |
| builds | `cd daemon && GOOS=linux go build ./...`; `GOOS=windows go build ./...` | exit 0 each |
| package suites | `cd daemon && go test -count=1 ./internal/authority/... ./internal/state/...` | `ok` for both, exit 0 |
| exported API | `base=$(mktemp -d); git archive 4dd374bd daemon \| tar -x -C "$base"; (cd daemon && go doc -all ./internal/authority) > "$base/head.doc"; (cd "$base/daemon" && go doc -all ./internal/authority) > "$base/base.doc"; wc -l "$base/base.doc" "$base/head.doc"; diff "$base/base.doc" "$base/head.doc"` | 834 lines each; `diff` empty, exit 0 |
| base file | `git show 4dd374bd:daemon/internal/authority/authority.go` | the 2173-line base file for declaration-level comparison |
| move aid | `git diff --color-moved=zebra 4dd374bd..HEAD -- daemon/internal/authority` | reviewer aid only (cannot see edits inside a moved block) |
| validation pins | overlay recipe under "Validation call-site pins (P3-1)" below | M9/M10/M11 red; green without overlay |
| citations | `python scripts/ci/check-citation-existence.py --base 4dd374bd --head HEAD` | exit 0 (the one changed audited document passes; ignore-rule notices only) |
| whitespace | `git diff --check` | exit 0 |
| host-path hygiene | `git diff 4dd374bd..HEAD \| grep -icE "users[/][a-z]"` | 0 (the bracket form cannot self-match the rows that name it) |

External or reviewer-side evidence (GDD state root or reviewer scratch; not in this
repository):

| Oracle | Result |
| --- | --- |
| per-declaration byte identity (`pfs declset` multiset, re-run reviewer-side with an own `go/ast` tool) | base 165 / head 175: 146 byte-identical (all of `linux_clone.go`, `cgroup.go` and the 29 type declarations), 2 removed (`hasV2SupervisorFields`, `validateV2SupervisorFields`), 12 added unexported, 17 supervisor-family declarations rewritten (the 15 methods plus the `supervisorFromHandoff` and `supervisorFromReleaseProof` functions); recorded in PR #158 "Pure-move evidence" |
| durable-bytes harness (`run-durable-bytes.sh <repo> <outdir>`) | 52 output lines incl. 10 frozen-literal round-trips, identical `sha256 62f437f5…` on both sides; superseded in-repo by the legacy-JSON pins plus type identity |
| behaviour differential (`run-differential.sh`, 3000 randomized v2 records/receipts/proofs through the exported API) | 3001 lines each side, identical `sha256 139d7cca…`, `differential.diff` 0 bytes; superseded in-repo by the pins plus the suites |
| citation-gate controls (`run-citation-gate.sh` with `CONTROL=none` / `positive` / `negative`) | gate exit 0 / 0 / 1 (negative control caught as designed); the in-repo gate command is the row above |
| pins' sensitivity planted defects (`planted-red*.log`) | three planted defects (Equal field drop, clone pointer share, conversion field drop) each caught by its own pin; superseded in-repo for the validation call sites by the P3-1 overlay table below |
| split rule table (`classify.py`) and splitter (`pfs apply`) | produced the split; the in-repo reproduction is the declaration-identity block under "Role rules" |

## Validation call-site pins (P3-1, added after review 1)

Review 1 found four rewritten validation call sites unpinned (mutants M9–M12). The pin
`TestValidatePinsV2HelperBindingAndMirrorOnlyV1Records`
(`daemon/internal/authority/authority_v2_test.go`) covers three of them with five cases:
a v2 `Supervisor` and a v2 release proof with the helper binding cleared
(`SupervisorPID=0`, `SupervisorIdentity=""`, `Mirror=nil`, `StopReceipt=nil`), and a v1
`Supervisor`, a v1 `SupervisorHandoff` and a v1 release proof carrying only the v2-only
`Mirror` field. Every fixture validates before the one field under test is touched, and
each case asserts its exact rejection message.

Mutation recipe (runnable from the repository root; any POSIX shell):

```sh
mut=$(mktemp -d)
cp daemon/internal/authority/supervisor.go "$mut/supervisor.go"
# apply the one-line edit from the table below
printf '{"Replace": {"%s/daemon/internal/authority/supervisor.go": "%s/supervisor.go"}}\n' "$PWD" "$mut" > "$mut/overlay.json"
(cd daemon && go test -overlay="$mut/overlay.json" -count=1 \
    -run TestValidatePinsV2HelperBindingAndMirrorOnlyV1Records ./internal/authority/)
```

| Mutant | One-line edit in `supervisor.go` | Observed |
| --- | --- | --- |
| M9 | `hasV2Fields` drops the `\|\| core.Mirror != nil` term | FAIL: the three v1 "carrying only Mirror" cases are accepted |
| M10 | `Supervisor.Validate`: `validateV2(true)` becomes `validateV2(false)` | FAIL: `Validate() accepted v2 supervisor without a bound helper` |
| M11 | `SupervisorHandoffReleaseProof.Validate`: `validateV2(true)` becomes `validateV2(false)` | FAIL: error is `supervisor handoff release proof lacks its exact durable stop receipt`, want `supervisor handoff release proof is invalid` |
| M12 | `ToSupervisor`: delete the trailing `result.Validate()` block | green: the full `./internal/authority/...` and `./internal/state/...` suites pass |

M11 is observable only through the error identity: the release-proof validator's
trailing receipt check rejects an unbound proof anyway (no v2 stop receipt can validate
against a zero helper, because its issuer identity must carry a positive PID), so the
mutation changes which rejection the proof gets, not whether it is rejected. The pin
asserts the exact error, which is the observable the mutation changes.

M12 is an equivalent mutant for the current code, so it has no killing test: every
handoff that passes `SupervisorHandoff.Validate()`, the scan-update rejection and the
bound-helper check converts to a `Supervisor` whose `Validate()` passes.
`supervisorFromCore` copies all 23 shared fields (pinned by
`TestSupervisorConversionParityAgainstBaseFieldSet`), so the converted result's
`validateV2(true)` differs from the handoff's `validateV2(false)` only in the
`requireHelper && SupervisorPID == 0` term, which the bound-helper check excludes; the v1
supervisor checks are the handoff's own checks — the handoff's predicates are equal or
stronger (`LaunchToken` must be valid hex, identity boundedness follows from
`validOptionalBounded` plus the non-empty bound identity, and `SupervisorPID > 0` comes
from the bound-helper check) — and `StopReceipt.ValidFor(result)` reads the same
projection `StopReceipt.ValidForHandoff(handoff)` does. The trailing `result.Validate()`
stays as the commit-boundary belt, but no input distinguishes it from the earlier guards.

## Design trade-offs

- **Why a core value at all.** The three records share 23 fields but differ in JSON field
  order and in their non-core fields, and anonymous embedding is forbidden, so a struct
  conversion between the three records cannot apply — field *order* is part of the
  underlying type. `Supervisor` is the exception: its field names, types and order are
  identical to `supervisorCore`'s (only the tags differ, and conversions ignore tags), so
  its extractor is the conversion `supervisorCore(value)` and adding or dropping a field
  on either side is a compile error instead of a silent divergence (apply-stage finding
  R1). The handoff and the release proof keep one-off extractor literals (5 dense lines
  each, matching the file's existing long-comparison style). The core buys one place each
  for clone, equal, same-launch, receipt matching, release-proof matching, v2 field
  validation and the conversion mapping.
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

## Comment-policy sweep (2026-11-22)

Module intent removed from this slice's files (`daemon/internal/authority/linux_clone.go`,
`daemon/internal/authority/supervisor.go`) by the 2026-11-22 comment-policy sweep is recorded in
[`docs/evidence/comment-policy-sweep-2026-11-22-moved-intent.md`](../evidence/comment-policy-sweep-2026-11-22-moved-intent.md)
("ws4-authority"); the sweep's plan is
[`docs/plans/2026-11-22-comment-policy-sweep-plan.md`](2026-11-22-comment-policy-sweep-plan.md).
