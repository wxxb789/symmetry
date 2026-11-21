# rf-w5-09 platform dead test-only helpers plan — move the four callerless wrappers into their tests

Contract: the GDD goal contract `goal-rf-w5-09-platform-dead-helpers.md` (2026-11-21) with its
binding conductor decisions of the same date (frozen surfaces, D2 report-only LOC delta,
ordering, the lesson-32 control-flow audit, the W5 review reconcile). Baseline for every
coordinate: `origin/main` at `fc8e95a24ccace6646e7da099bc03e110c87ae17` (this worktree's HEAD;
`#160`/`3ccc62f5` is in history, so the dispatch dependency is satisfied). Branch:
`refactor/rf-w5-09-platform-dead-helpers`. Area: `daemon/internal/platform`.

The increment deletes four production wrappers whose only callers are tests and re-declares each
body **byte-identically** inside the `_test.go` file that calls it. No production behaviour, error
string, comment or call target changes: the moved wrappers keep calling the same production
primitives (`installLinuxSupervisorScanInjection`, `parseWindowsProcessIdentity` +
`randomContainmentToken`/`randomContainmentSecret` + `validateSupervisorEndpoint`,
`readLinuxOwnerChildPIDsBounded`, `getNativeHandleInformation`).

## Owned set (STRICT, re-derived at this head)

Deletions: `daemon/internal/platform/containment_supervisor_linux.go`,
`containment_supervisor_windows.go`, `helper_v2_run.go`, `native_launch_windows.go`.
Moved bodies: `containment_supervisor_linux_test.go`, `containment_supervisor_windows_test.go`,
`linux_owner_helper_v2_linux_test.go`, `native_launch_windows_test.go`. Plus this plan file.
No `docs/TODO.md` entry covers this slice (the two test-only entries there are W5-06's provider
entry points and W1-06's state surfaces), so neither TODO file is edited.

## Helper table (measured on this tree, `git grep -nw` + `wc -l`)

| helper | base production site | span | lines | callers (all test-only) | call target kept |
| --- | --- | ---: | ---: | --- | --- |
| `installLinuxSupervisorScanFailureInjection` | `containment_supervisor_linux.go:476` | 476-478 | 3 | `containment_supervisor_linux_test.go:2864,2926,3008` | `installLinuxSupervisorScanInjection` (`:488`, production, env-gated, now `:485`) |
| `containmentEndpointForIdentity` | `containment_supervisor_windows.go:688` | 688-722 | 35 | `containment_supervisor_windows_test.go:396,446,473,834,1031,1035` | `parseWindowsProcessIdentity`, `randomContainmentToken`, `randomContainmentSecret`, `validateSupervisorEndpoint` |
| `readLinuxOwnerChildPIDs` | `helper_v2_run.go:197` | 197-200 | 4 | `linux_owner_helper_v2_linux_test.go:77,189` | `readLinuxOwnerChildPIDsBounded` (`:202`) |
| `nativeHandleInformation` | `native_launch_windows.go:274` | 274-284 | 11 | `native_launch_windows_test.go:141,157,403,427` | `getNativeHandleInformation` (`native_launch_windows.go:29`) |

Measured total: **53 production lines across 4 files** (the plan's `−45` is D2 report-only; the
re-derived number at `9ac85ac0` and at this head is 53) = 3 + 35 + 4 + 11.

Measured shape after the move (re-derived on the delivered tree, `pfs declset`/`pfs inventory` +
`wc -l`):

| helper | deleted | new declaration site | body sha256 (base = HEAD) |
| --- | --- | --- | --- |
| `installLinuxSupervisorScanFailureInjection` | `containment_supervisor_linux.go` 476-479 (−4) | `containment_supervisor_linux_test.go:3530-3532` (+4) | `4c7f05c337df00b7…` |
| `containmentEndpointForIdentity` | `containment_supervisor_windows.go` 688-723 (−36) | `containment_supervisor_windows_test.go:1077-1111` (+36) | `15e85437924240a1…` |
| `readLinuxOwnerChildPIDs` | `helper_v2_run.go` 197-201 (−5) | `linux_owner_helper_v2_linux_test.go:707-710` (+5) | `0390e789f8cdb1a4…` |
| `nativeHandleInformation` | `native_launch_windows.go` 274-285 (−12) | `native_launch_windows_test.go:563-573` (+12) | `a6273796a1ec12d4…` |

Each production deletion is the declaration span plus exactly one adjacent blank line (so
53 declaration lines + 4 blank separators = **−57 production lines**; test side +57, no other
file changes). The moved inner comment (`containmentEndpointForIdentity`'s "retained only for
tests and diagnostics") travels with the body: its `pfs declset` digest is unchanged across the
two revisions and only its file attribution changes.

Not moved, deliberately:

- `installLinuxSupervisorScanFailureInjectionAfterInitial` (`containment_supervisor_linux.go:480`)
  has a **live production call site** (`supervisor_linux_helper.go:615`) and stays;
- `installLinuxSupervisorScanInjection` (`:488`), `readLinuxOwnerChildPIDsBounded`
  (`helper_v2_run.go:202`) and the env parser are production and stay (W5-01's territory);
- `getNativeHandleInformation` (`native_launch_windows.go:29`) stays in production: it is the
  proc handle the moved test-local body must call, and `native_launch_windows.go` is owned for
  deletions only. Leaving a package-level `var` used only by tests is not a compile or vet
  diagnostic (`go vet` does not flag unused package-level declarations), and deleting it would
  change the production file beyond the contract's four named helpers.

## Public / API shape

Unchanged: no exported name appears or disappears, no signature, struct, JSON, protocol or
`//go:build` line changes, and no new file is created. The package's exported surface is
identical because the four declarations are unexported and were already unreachable from
non-test code. The moved bodies need no new imports in their destination files:

- `containment_supervisor_linux_test.go` (`//go:build linux`) — the body only forwards;
- `containment_supervisor_windows_test.go` (`//go:build windows`) — already imports `errors`;
- `linux_owner_helper_v2_linux_test.go` (`//go:build linux && amd64`) — the body only forwards;
- `native_launch_windows_test.go` (`//go:build windows`) — already imports `unsafe` and
  `golang.org/x/sys/windows`.

Build-constraint coverage is exact: each moved body lands in a test file whose constraint is at
least as strict as the source file's (`linux` → `linux`; `linux && amd64` → `linux && amd64`;
`windows` → `windows`), so no target's file set gains or loses a compilation unit.

## Exact edge-case semantics the contract demands

- **Byte identity.** Each moved body travels as the exact source bytes it occupied at the
  baseline (leading tabs, inner comments, error strings, argument lists). The primary proof is a
  package-wide declaration-multiset comparison (`pfs declset internal/platform/*.go`): the sorted
  `decl<TAB>kind<TAB>name<TAB>sha256(raw)` + `comment<TAB>sha256` multiset must be **equal**
  before and after, which is only possible if every declaration's bytes and every comment's bytes
  survived exactly once.
- **Placement.** Each body is appended at the end of its consuming `_test.go` file, separated by
  one blank line, under no new comment. Appending is chosen over placing the helper next to its
  first caller because it leaves every pre-existing line coordinate in those four files unchanged
  (`docs/archive/CLOSED-TODO.md:517,582,586` and `docs/evidence/goal-0006b-*.md:144` cite
  `containment_supervisor_linux_test.go` and `linux_owner_helper_v2_linux_test.go` lines) and
  because a new comment group would itself change the comment multiset that proves the move.
- **Deletion shape.** Each production span plus exactly one adjacent blank line is removed, so the
  surrounding declarations keep a single blank-line separator and `gofmt -l` stays empty.
- **No test function body changes.** Only helper declarations are added to the test files; no
  assertion, table case, name or skip is added, removed or reworded, so the mutation clause is
  inapplicable by construction (contract, W5 review reconcile).
- **Control-flow preservation (lesson 32).** Moving a body across files is the move-proof case:
  every early return and short-circuit is enumerated base-vs-HEAD per helper in the evidence
  section below, and the wrapper's no-op-when-unset behaviour is asserted to come from the
  production callee (the moved wrapper only forwards 6 arguments), so a witness hook can never be
  armed outside witness mode.
- **No new suppression.** Zero `//nolint`/`credo:disable` added; no test file loses a declaration.

## Evidence plan (exact commands, run on the final tree)

| Gate | Command | Carrier |
| --- | --- | --- |
| format | `gofmt -l daemon/internal/platform` (empty output) | host |
| vet | `go vet ./...` | host, windows/amd64 |
| builds | `GOOS=windows go build ./...`, `GOOS=linux go build ./...`, `GOOS=linux GOARCH=arm64 go build ./...` | host, from `daemon/` |
| package tests (Windows subset) | `go test -count=1 ./internal/platform/...` | host |
| package tests (Linux half) | `wsl -d Ubuntu-24.04 -u accept` → `cd /mnt/q/repos/wt-w5-09-platform-dead-helpers/symmetry/daemon && go test -count=1 ./internal/platform/...` | WSL |
| consumer compile | `./cmd/symmetry-daemon/...` compile (both GOOS, via the `./...` builds and a `go vet ./cmd/...`) | host |
| reference scan | `git grep -nw <helper>` per helper: zero non-test references after | host |
| move proof (primary) | `pfs declset internal/platform/*.go` before vs after, `diff` exit 0, identical sha256 | host |
| move proof (attribution) | `pfs declset -f` over the eight owned files, before vs after: the four `decl` lines change file attribution, nothing else moves | host |
| prod shrink | `wc -l` of the four production files (−53 total) and `pfs inventory` per file | host |
| whitespace | `git diff --check` | host |
| host path leak | profile-directory grep over the branch diff and the new files | host |

## Executed gates (final tree, 2026-11-21; artifacts under the GDD state tree `refactor/tools/w5-09-platform-dead-helpers/`)

| gate | command | exit | counts |
| --- | --- | ---: | --- |
| format | `gofmt -l daemon/internal/platform` | 0 | 0 paths |
| vet | `go vet ./...` (host windows/amd64, go1.27.4) | 0 | 0 diagnostics |
| vet (consumer) | `go vet ./cmd/...` | 0 | 0 diagnostics |
| builds | `GOOS=windows GOARCH=amd64 go build ./...`, `GOOS=linux GOARCH=amd64 go build ./...`, `GOOS=linux GOARCH=arm64 go build ./...` | 0 | 0 diagnostics each |
| package tests (Windows) | `go test -count=1 ./internal/platform/...` | 0 | `ok … 6.969s`; `-v`: 132 RUN, 93 top-level PASS, 0 FAIL, 1 SKIP (`TestProductionContainmentSupervisorReplaysReceiptAfterLostRecoverResponse`, env gate `SYMMETRY_PRODUCTION_CONTAINMENT_WITNESS`) |
| package tests (Linux, contract command) | `go test -count=1 ./internal/platform/...` (WSL Ubuntu-24.04, go1.27.0) | 1 | 5 named `tcp_peer*` loopback failures (`dial tcp4 127.0.0.1:<port>: connect: connection timed out`) then `panic: test timed out after 10m0s`; this host's documented loopback limitation (`docs/archive/CLOSED-TODO.md:3391-3393`), the failing files are untouched by this slice |
| package tests (Linux, host pattern) | `go test -count=1 -skip 'Loopback\|TCPPeer' ./internal/platform/...` (WSL) | 0 | `ok … 19.467s`/`19.915s`; `-v`: 206 top-level PASS, 171 subtest PASS, 0 FAIL, 1 SKIP (`TestLinuxOwnerAnchorRunReaperRejectsForeignChildBeforeAdoptingIt`, requires the installed A UID/GID) |
| move proof (primary) | `pfs declset internal/platform/*.go` before vs after | 0 | 1987 = 1987 lines, `diff` empty, identical sha256 `9a9e8ed0ff4bf2a50709f5c3ba291e63c7a24c7b3f02aee60c08d3ea06d1442d` |
| move proof (attribution) | `pfs declset -f` over the eight owned files, base vs head | n/a (diff shown) | exactly 4 `decl` re-attributions + `containmentEndpointForIdentity`'s inner comment group; raw and printer digests identical in every moved record |
| move proof (negative control) | operand flip inside the moved wrapper, scratch copy | 1 | `pfs declset -f` diff = 1 record, digest changes (`4c7f05c3…` → `33a2b158…`); copy outside the repo, tree untouched |
| body identity | per-helper sha256 of the base span vs the test-file span | 0 | 4/4 identical; 53 lines total |
| call sites | argument-text multisets of each helper's callers, base vs head | 0 | identical: 3 / 7 / 2 / 6 occurrences |
| reference scan | `git grep -nw <helper>` | 1 (no non-test hit) | every hit is a `_test.go` file |
| hunk audit | `git diff -U0` hunk starts | 0 | 8 hunks: 4 deletions inside the four base spans, 4 EOF insertions in the consuming test files |
| prod shrink | `wc -l` on the four production files | 0 | 968→964, 876→840, 375→370, 642→630 = **−57** (53 declaration lines + 4 blank separators); test side +57 |
| build constraints | `git diff -U0 \| grep '^[+-]//go:build'` | 1 (no hit) | no file added, removed or re-gated |
| suppressions | `git diff \| grep -E 'nolint\|credo:disable'` | 1 (no hit) | 0 new suppressions |
| whitespace | `git diff --check` | 0 | — |
| host path leak | account-name / profile-directory / drive-letter grep over the diff and the new plan | 1 (no hit) | 0 hits; no backslash followed by the letter u in any written text |
| race | not run locally (`-race` needs cgo, absent on this host) | — | CI daemon race job is the oracle, per contract |

The SKIP list of both platform test runs is recorded verbatim in the evidence section; the
contract requires 0 new skips (the diff adds none). CI-only oracle: the daemon race job (`-race`
needs cgo, absent on this host, as the contract states). No measurement harness is part of this
slice, so the "never run a measurement harness next to the full suite" sequencing rule does not
bind; the WSL run is executed alone, not concurrently with a host suite.

## Design trade-offs

- **Move vs delete.** The contract allows deleting a helper where a test-local equivalent is
  clearer. All four are kept as moved bodies: each is a two-call forwarding wrapper or a thin
  error-mapping helper, and inlining them into the ~40 existing call sites would be a test-function
  body edit, which the contract forbids (`no test function body changes`). Deletion of
  `containmentEndpointForIdentity` and `nativeHandleInformation` would additionally drop the
  `validateSupervisorEndpoint` / `GetHandleInformation` checks those tests rely on.
- **Append at EOF vs adjacency** — see "Placement" above; coordinates preservation and
  comment-multiset integrity decide it.
- **Evidence carrier.** Evidence lives in this plan file plus the stage's `answer.md` (the
  contract's clause 2 artifact), following `#160`/rf-w2-11 (the most recently merged slice of the
  same kind), **not** in a new `docs/evidence/` file: the contract's STRICT owned set does not
  include one, and rf-w2-09's record itself notes that writing one is an out-of-set edit. A
  `docs/evidence/rf-w5-09-*.md` file was rejected for that reason; the conductor can fold the
  convention into the owned set if it wants the record there.
- **`pfs declset` as the primary proof** rather than `git diff --color-moved`: the colouring is a
  reviewer aid (rf-w1-04's own plan says so), cannot see an intra-block edit, and is not rendered
  by GitHub. The multiset equality is mechanically re-runnable and strictly stronger for this
  intra-package, same-bytes move.

## Out of scope (with reasons)

- **W5-01** (witness-config consolidation and gate semantics): the env parser
  `linuxSupervisorScanFailureInjectionFromEnv`, `installLinuxSupervisorScanInjection` and the
  `AfterInitial` variant with its live call site are frozen here; W5-01 rebases over this slice.
- **W6-03** (`isGoTestBinary` fork): serialized after W5-01 and this slice; not touched.
- **Other platform test seams** (`containment_supervisor_production_windows_test.go`,
  `containment_supervisor_receipt_windows_test.go`, the `process_*` files): out of the owned set.
- **`getNativeHandleInformation` and other production decls with no non-test user**: outside the
  contract's four named helpers; deleting them would exceed the measured 53-line acceptance and
  change a production file beyond "deletions only" for the four spans.
- **The gitignored 2026-09 build artifacts in the checkout** (findings P-09's note): not a code
  change; the tree stays as found.
- **Any behaviour, protocol, journal, fence, credential, provider-field or error-text change:**
  none, by construction (multiset-equal declarations).

## Ambiguities resolved

1. **"53 measured vs plan's −45."** D2 makes the delta report-only; this plan reports 53 (the
   re-derived span total at this head) and never gates on it.
2. **Evidence record location.** Options: a new `docs/evidence/` file (outside the STRICT owned
   set, rf-w2-09 precedent) or the plan file + `answer.md` (rf-w2-11 precedent, inside the owned
   set). Chosen: the owned-set option; recorded here so the conductor can rule otherwise.
3. **Where the moved body goes inside the test file.** Options: next to the first caller, in a new
   section with a comment, or appended at EOF. Chosen: appended at EOF with no new comment — the
   only option that preserves every existing test coordinate and the comment multiset.
