# rf-w5-06 test-only seam removal plan — no test-only accessor on the request path (CW-12 remainder, M8)

Contract: the GDD goal contract `goal-rf-w5-06-test-only-seam-removal.md` (2026-11-21, GDD state
root) including its binding conductor decisions: the Wave-5 draft round (D1 byte-identical
surfaces, D2 report-only dLOC) and the W5 review reconcile round (decision H — option (a)
mandated, the two vacuous refute sites fixed, the mutation record fixed at three mutations).
Baseline for every coordinate: `origin/main` `fc8e95a24ccace6646e7da099bc03e110c87ae17` (this
worktree's HEAD, `fc8e95a2`); the contract's plan-time references were re-derived against main at
dispatch, as its status line requires. Branch: `refactor/rf-w5-06-test-only-seam-removal`.

Shape reference: `a503eab7` (rf-w2-03 controller plumbing, #163) — a control-web refactor closed
by a plan file under `docs/plans/`, no in-repo goal archive and no in-repo evidence file; the
load-bearing numbers live in this plan's evidence section and in the run's final report, which
the harness records as `answer.md` (precedent also: `docs/plans/2026-11-21-rf-w2-16-clone-aliasing-test-plan.md`
ambiguity 2). The GDD contract and its index live outside this repository; this stage does not
move them.

## What the increment removes and what it keeps

Base state (measured on this tree, `git grep -n`):

| # | Seam | Base location | Kind |
| --- | --- | --- | --- |
| 1 | `raw_body_private_key/0` — public accessor for `@raw_body_private_key :symmetry_control_raw_evidence_json` | `control/lib/symmetry_control_web/plugs/strict_evidence_json.ex:113-120` (`@doc` 113-118, `@spec` 119, `def` 120) | test-only accessor on the request path; **deleted** |
| 2 | `conn.private[:claim_server_time]` — the claim clock override | read `control/lib/symmetry_control_web/controllers/daemon_controller.ex:126-130` (`Map.get` 127, fallback 129); written only by `control/test/symmetry_control_web/controllers/protocol_controller_test.exs:644` | test-only private-key seam; **kept and documented** (decision H option (a)) |

Measured caller census at the base revision, whole repository:

- `raw_body_private_key` — 12 hits. Lib: the attribute `:107`, the accessor `:119`/`:120`, and two
  internal uses of the **attribute** (`materialize/1` `:131`, `capture/2` `:175`). Test: five sites
  in one file, `test/symmetry_control_web/plugs/strict_evidence_json_test.exs:26,32,48,226,285`.
  Docs: two mentions in `docs/plans/2026-11-21-rf-w2-02-strict-json-decoder-plan.md` (`:27`,`:55`),
  a historical plan describing the base state of a merged slice. So the accessor is a leaf whose
  only callers are one test file; `mix compile --warnings-as-errors` would not have flagged it.
- `claim_server_time` — 2 hits: the read at `daemon_controller.ex:127` and the write at
  `protocol_controller_test.exs:644`, inside `assert_error(...)` `:642-652` in the test
  "claim replay returns ownership_lost when its response render observes an expired lease" `:621`.
  Production never sets the key; the `DateTime.utc_now()` fallback is production's only path.

Drift from the contract's plan-time coordinates (recorded, all re-derived here): the accessor is
`@doc` 113-118 + `@spec` 119 + `def` 120 (contract: `:119-120`, which names the spec/def; the
`@doc` block above them carries the same rationale sentence and goes with them); internal uses
`131`/`175` and test call sites `26,32,48,226,285` match the contract exactly;
`claim_response_time/1` is `:126-130` (contract: `:131-136`) and is consumed at `:112`
(contract: `:117`) — the controller shifted after W2-03/#163, which merged before this worktree
was cut; the contract's commit ancestry is satisfied (HEAD = main = `fc8e95a2`, which contains
`#163`, `#156`, `#180`).

## Public / API shape after the increment

No production API change. `SymmetryControlWeb.Plugs.StrictEvidenceJSON` keeps its complete
remaining surface (`init/1`, `call/2`, `materialize/1`) and loses exactly one public zero-arity
function it existed to serve the test file. The private attribute and both internal reads/writes
of `@raw_body_private_key` are untouched, so the capture key itself does not change: a captured
evidence body still lands in `conn.private[:symmetry_control_raw_evidence_json]`.

The plug test pins that key with a module attribute:

```elixir
# The plug's private connection key for a captured evidence body. Pinned here
# because the plug exposes no accessor: a plug that captures under a renamed
# key must redden this file's capture assertions and its not-captured assertions alike.
@raw_body_private_key :symmetry_control_raw_evidence_json
```

Its five accessor call sites become the two read forms
`conn.private[@raw_body_private_key] == body` (`:31`,`:37`,`:53`) and
`refute Map.has_key?(conn.private, @raw_body_private_key)` (`:231`,`:291`), and each refute site
gains the strengthened value assertion
`refute Enum.any?(conn.private, fn {_key, value} -> value == body end)` (`:232`,`:292`). No test
name changes.

The clock seam keeps its code and gains one comment at the read site (prod file, comment only):

```elixir
# Test-only clock seam: `protocol_controller_test.exs:644` (the
# expired-at-response replay case) puts this private key in the conn, and
# production never does, so the fallback below is production's only path.
defp claim_response_time(conn) do
  case Map.get(conn.private, :claim_server_time) do
```

The comment carries the contract's citation form: the literal `protocol_controller_test.exs:644`
plus the case description at the read site — Change (2)'s "comment naming
`protocol_controller_test.exs:644`" — so the read site itself names the writer that sets the key;
the same citation is also recorded in the seam table above and in the report.

## Edge-case semantics the contract demands

- **No test-only accessor remains on the request path.** After the change, `raw_body_private_key/0`
  has no definition and no caller (mutation (c) is the grep record).
- **The test still pins the captured-body key.** A rename of the plug's `@raw_body_private_key`
  atom reddens the three capture assertions; the rename + spurious-capture composite (a plug that
  captures under a renamed key in a case that must not capture, record rows a-1/a-2) also reddens
  the two strengthened refute sites; no test-side helper re-derives the key from the plug (that
  would be the forbidden "new test-only API instead of the constant").
- **The two refute sites stop being vacuous.** `refute Map.has_key?(conn.private, key)` alone
  passes whenever the plug captures under a *renamed* key. Each site also asserts the stronger
  property that no `conn.private` value equals the request body, so a plug that starts capturing
  in a case that must not capture reddens whichever key it used.
- **No wire/response byte changes.** The plug's route predicate, capture/materialize semantics,
  the strict decoder and the protocol envelope are untouched; existing test names stay
  byte-identical (D1).
- **The clock seam keeps exactly two branches.** The `%DateTime{}` branch stays reachable only
  from the test that sets the private key, and `DateTime.utc_now()` stays the only production
  path; the deterministic `409 ownership_lost` case
  (`protocol_controller_test.exs` "claim replay returns ownership_lost when its response render
  observes an expired lease") stays unmodified and remains green.

## Base-vs-HEAD control-flow audit (conductor decision, lesson 32)

Removing a seam removes a branch; every affected path is audited base → HEAD. The deleted
accessor is a leaf function (`def raw_body_private_key, do: @raw_body_private_key`), so its
deletion removes no branch anywhere: the audit below covers every branch the seam sat next to.

| Path | Base | HEAD | Preserved |
| --- | --- | --- | --- |
| evidence capture, binary body | `capture/2` `:175` `Conn.put_private(@raw_body_private_key, body)` | identical line | yes |
| evidence capture, empty body | `capture/2` captures `""` (no branch; the empty case is decided in `materialize/1`) | identical | yes |
| `materialize/1`, captured `""` | `:132` → `{:ok, %{}}` | identical | yes |
| `materialize/1`, captured binary | `:133` → `StrictJSONDecoder.decode_object/1` | identical | yes |
| `materialize/1`, captured non-binary | `:134` → `{:error, :malformed_json}` | identical | yes |
| `materialize/1`, not captured | `:135` → `{:error, :not_captured}` | identical | yes |
| accessor itself | `raw_body_private_key/0` returns the attribute | deleted; no caller in lib or test | n/a (leaf, zero callers after the test-side constant) |
| clock, private key set | `claim_response_time/1` `:127-128` → the `%DateTime{}` value (test writer `protocol_controller_test.exs:644`) | identical code; the branch keeps working | yes |
| clock, private key unset (production) | `:129` → `DateTime.utc_now()` | identical code, now documented as production's only path | yes |

The test-side constant must keep pinning the same boundary the accessor pinned: it names the
private key the plug writes at `capture/2` and reads at `materialize/1`, and it is asserted at
both boundaries — captured (three sites) and not-captured (two sites, now strengthened).

## Evidence plan (exact commands)

All gates foreground, via a host-local scratch runner in the pattern of the program's mix gate
scripts (portable Elixir 1.20.4 / OTP 29.0.6 via `erl.exe` directly, `MIX_DEPS_PATH` pointing at
the main checkout's `deps/` — `mix.lock` verified identical — and a dedicated
`MIX_TEST_PARTITION=rf-w5-06`, local PostgreSQL on 5432). The script lives outside the repository
(GDD state scratch, `rf-w5-06-mix.sh`); no repository file is added for tooling.

| Gate | Command | Expected |
| --- | --- | --- |
| format | `mix format --check-formatted` | exit 0, no output |
| compile | `MIX_ENV=test mix compile --warnings-as-errors` | exit 0, no warnings |
| two suites (the oracle pair) | `mix test test/symmetry_control_web/plugs/strict_evidence_json_test.exs test/symmetry_control_web/controllers/protocol_controller_test.exs` | exit 0; 24 + 31 tests, all pass, `protocol_controller_test.exs` unmodified |
| web tree | `mix test test/symmetry_control_web/` | exit 0, all pass |
| whitespace | `git diff --check` | exit 0 |
| mutation (a) | rename the plug's `@raw_body_private_key` atom **and** make the strict-mutation branch capture (a plug that starts capturing under a renamed key in a case that must not capture) | capture assertions `:31,:37,:53` and the strengthened refute assertions `:232,:292` redden (HEAD coordinates); the bare `Map.has_key?` forms `:231,:291` stay green, proving the strengthening load-bearing |
| mutation (b) | remove the `DateTime.utc_now()` fallback from `claim_response_time/1` | the controller suite reddens (the fallback is production's only path) |
| mutation (c) | `git grep -n "raw_body_private_key" -- control/lib control/test` / `git grep -n "raw_body_private_key/0" -- control/lib control/test` | `raw_body_private_key/0` absent; remaining hits are the private attribute and its two internal uses |
| full suite | **not this stage** — the conductor's gate-runner owns `mix test` (never two full suites on this host) | — |

Mutations are planted edits reverted before handover; the three-mutation record (a)-(c) is fixed
by the contract (the W5 reconcile round) and is present in the evidence section below and in the
final report.

## Measured results (this tree, `fc8e95a2` + working-tree change)

Every row is a command actually run in this worktree. The byte hashes cited here are
delivered-bytes hashes of `control/lib` and `control/test` against `fc8e95a2`, taken with the
prefix flags the gate artifact used —
`git diff --src-prefix=i/ --dst-prefix=w/ fc8e95a2 -- control/lib control/test | sha256sum`
(a plain `git diff` prints `a/`,`b/` headers instead and hashes identical content differently:
`d8ccf7c5ead1a51344f1c1c5b30caea7cbfa9f3162b58b104e5bd22168617c49` instead of
`51097c07e40a845994aa6ae8d5e0fdd60a34cbcc6cb39ae4923226a9ee0a17ec` on the implement-stage
bytes). The mutation runs and the first gate round ran on the revision whose delivered-bytes hash
is `ff31a72a1675767ceb7f0381faeaee88e0187eeb88c2728881a3096c8e0664d5`; all mutation edits were
reverted and the tree was verified back to that hash (`cmp`-identical diff). The delivered bytes
then tightened the seam comment to carry the literal `protocol_controller_test.exs:644` citation
(comment text only, no executable and no test line), giving the implement-stage delivered-bytes
hash `51097c07e40a845994aa6ae8d5e0fdd60a34cbcc6cb39ae4923226a9ee0a17ec` (reproduced at apply
time); every gate row below was re-run on those delivered bytes. Runner: GDD-state scratch
`rf-w5-06-mix.sh` (Elixir 1.20.4 /
OTP 29.0.6, `MIX_TEST_PARTITION=rf-w5-06`, deps fetched locally, PostgreSQL on 5432).

**Apply-stage note (`rf-w5-06` apply, 2026-11-21).** The accepted simplify findings R1, PD-A and
PD-B are applied on top of `cc7170bc`: R1 reworded the test-side constant's rationale comment in
`strict_evidence_json_test.exs` (comment text only; assertions, test names and line counts
unchanged), and PD-A/PD-B corrected this plan's coordinates and hash-reproduction command to the
delivered form. The shipped working tree's delivered-bytes hash is therefore
`6eae05f72b9b809b8f1db6e80a695f284f0dbd2fecb73281d97957a0edcb1463`; the apply-stage re-runs on
that revision are green: format exit 0, compile exit 0, the plug test 24 passed, the two suites
55 passed, the web tree `--exclude portal_assets` 275 passed / 1 skipped / 4 excluded, and
`git diff --check` exit 0. Committed history is untouched: the range `fc8e95a2..cc7170bc` still
hashes to `51097c07e40a845994aa6ae8d5e0fdd60a34cbcc6cb39ae4923226a9ee0a17ec`.

| Gate | Result |
| --- | --- |
| format | `mix format --check-formatted` **exit 0**, empty output (run after the mutation reverts **exit 0**; re-run on the delivered bytes **exit 0**) |
| compile | `MIX_ENV=test mix compile --warnings-as-errors` **exit 0**, no warnings (re-run on the delivered bytes: recompiled 1 file, exit 0) |
| two suites | `mix test <plug test> <protocol controller test>` **exit 0**, `Result: 55 passed` (24 + 31) in 3.4 s, seed 959548 (re-run after the reverts exit 0; on the delivered bytes **exit 0**, `55 passed` in 3.7 s) |
| test-name parity | base vs HEAD `test "..."` lists byte-identical: 24 and 31 names (`cmp` clean on the `git show HEAD:` extraction) |
| web tree | `mix test test/symmetry_control_web/` **exit 2**: `275/279 passed, 1 skipped`, 4 failed — all four are the `:portal_assets` cases of `PortalControllerTest`, red in `ConnCase` setup at `PortalBundle.require_staged!` because no staged frontend bundle exists on this host (no Node/pnpm; `test/support/portal_bundle.ex` states the prerequisite and its documented escape). With the repo's documented exclusion: `mix test test/symmetry_control_web/ --exclude portal_assets` **exit 0**, `275 passed, 1 skipped, 4 excluded` in 104.0 s; post-revert re-run **exit 0**, same counts in 57.3 s; **delivered bytes exit 0**, same counts in 37.6 s |
| whitespace | `git diff --check` **exit 0** |
| mutation (a) | three planted runs (same mutant class, all reverted): **(a-1)** rename the plug's `@raw_body_private_key` atom only → **exit 2**, `22/24` passed: the two capture tests red with `code: assert conn.private[@raw_body_private_key] == body`, `left: nil` (the test-side constant pins the key; no accessor involved). **(a-2)** rename **plus** a strict-mutation capture-with-replay mutant (the plug starts capturing every strict JSON mutation body under its — renamed — key) → **exit 2**, `20/24` passed: the two capture tests **and both refute tests** red, and the failing assertion at both refute sites is exactly `code: refute Enum.any?(conn.private, fn {_key, value} -> value == body end)` (the bare `Map.has_key?` form stays green because the key was renamed). **(a-3)** (a-2) with the two strengthened lines removed → **exit 2**, `22/24`: only the two capture tests red; both refute tests pass → the strengthened assertion is load-bearing and the previous form was vacuous under a renamed key |
| mutation (b) | replace `_ -> DateTime.utc_now()` with a raise in `claim_response_time/1` → **exit 2**, `18/31` passed: 13 controller tests red, including "an expired lease returns ownership_lost instead of a zero remaining lease" with `** (RuntimeError) claim_server_time missing on the production path` raised from `daemon_controller.ex:133` via `claim/2:112` → the fallback is production's only path |
| mutation (c) | all three scoped to `-- control/lib control/test` → **no match (exit 1)**: `git grep -n "raw_body_private_key/0"`; `git grep -n "def raw_body_private_key"`; `git grep -n "StrictEvidenceJSON.raw_body_private_key"`. Remaining `raw_body_private_key` hits in the code trees: lib `:107` (attribute), `:122` (`materialize/1`), `:166` (`capture/2`); test `:13` (test-side attribute), `:31,:37,:53,:231,:291` — unscoped, the token also appears as prose in this plan and in `docs/plans/2026-11-21-rf-w2-02-strict-json-decoder-plan.md:27,55` |
| full suite | **not run here** — assigned to the conductor's gate-runner (never two full suites on this host) |
| dLOC (report-only, D2) | `git diff --numstat` on the delivered bytes: `daemon_controller.ex` +3/−0 (comment only), `strict_evidence_json.ex` +0/−9 (272 → 263 lines), plug test +12/−5 (352 → 359 lines); slice total +15/−14, production lib removed **9** lines and added 3 comment lines, test file net +7 |

Gate-invocation note (reconciliation with the contract's literal command): the contract lists
`mix test test/symmetry_control_web/`. On this host that command cannot be green for any revision
of this slice: four `:portal_assets` cases need the staged frontend bundle, and the repository's
own support module instructs operators to exclude them explicitly when no staging is available
(`mix test --exclude portal_assets`) rather than touching their assertions. The excluded set is
exactly the four failing cases, both were run (the unexcluded run is recorded above with its four
setup failures), and no other case is excluded.

## Closure and reconciliation (implement stage)

- **No TODO entry closes.** `docs/TODO.md` has no entry for this seam (`rg` for `CW-12`,
  `raw_body`, `claim_server_time`, `test-only` matches only the unrelated W2-06 provider
  entry-points item at `docs/TODO.md:927`), so `docs/TODO.md` and
  `docs/archive/CLOSED-TODO.md` are untouched.
- **No design, protocol or grammar document goes stale.** `rg -n "StrictEvidenceJSON"` over
  `docs/design/` and `docs/*.md` finds nothing; `docs/protocol-v1.md` governs the wire, which is
  unchanged.
- **Revision-bound historical records are unaffected.**
  `docs/evidence/goal-0009-source-identity-2026-11-16.json` (file hashes) and the 2026-09-29
  ablation records name these files by path, hash or case count; they bind to the revisions they
  record, and the case inventory is unchanged (24 + 31 test names, byte-identical lists).
- **The GDD contract and its index live outside this repository**; the implement stage leaves them
  in place (the ledger/conductor own lifecycle transitions). Repository-side closure of this slice
  is this plan file, part of the owned set and shipped with the change.

## Design trade-offs

- **Test-side constant vs. keeping a narrower API vs. moving the key into a shared module.** The
  contract mandates the constant: a literal in the one test that inspects the capture keeps the
  boundary pinned with zero production surface. A "narrower" accessor would still be a test-only
  API on the request path; moving `@raw_body_private_key` into a public constants module would
  create a new shared symbol for one consumer. Chosen: the constant.
- **Strengthened refute vs. positive control in the same test.** Both were allowed. The value
  assertion is chosen because it detects the *renamed-key spurious capture* class directly in the
  case that must not capture, needs no fixture that the case does not already build, and keeps the
  diff at one line per site. A positive control (a second conn that must capture) would restate
  the capture assertions of the first test in a test whose subject is a non-capturing path.
- **Clock seam kept and documented (option (a)) vs. removed (option (b)).** Decision H: option (b)
  would delete the only deterministic test of the expired-at-response branch — an assertion
  deletion the owner rules forbid without a replacement oracle, and the contract explicitly does
  not authorize it. The surviving seam is a documented private read keyed by a private atom, not
  an API: no new symbol is reachable from production call sites.
- **Comment placement for the seam.** At the read site (`claim_response_time/1`), as the contract
  requires, not at the test writer: the reader who might otherwise "clean up" the fallback is the
  reader of the helper.

## Out of scope (with reasons)

- The strict decoder's single-pass logic (`StrictJSONDecoder`) — W2-02's merges, frozen here.
- `protocol_controller_test.exs` — the unmodified oracle under decision H (W3-03/W3-14 own it).
- The served-document gates (W1-13), `portal_json.ex` / `Protocol.error_details` (W5-05/W6-09).
- Option (b) — not authorized.
- Other seams in other files: none measured in this surface; no scan beyond the census above.
- `docs/TODO.md`: no entry of this program's tracker names this seam (`rg` for `CW-12`,
  `raw_body`, `test-only seam` over `docs/TODO.md` finds nothing), so no TODO line moves.
- `docs/plans/2026-11-21-rf-w2-02-strict-json-decoder-plan.md`'s two mentions of
  `raw_body_private_key/0` are the historical base-state description of a merged slice (like every
  plan in `docs/plans/`, they bind to the revision they name); they are not updated.

## Ambiguities resolved (options and the chosen reading)

1. **Contract line numbers vs. this tree.** The contract's `:119-120` (accessor) and `:131-136`
   (clock helper) predate the W2-03/#163 merge; the dispatch instruction is to re-derive. The
   re-derived coordinates above are the operative ones; no contract clause depends on a line
   number, because the acceptance is stated by symbol (`raw_body_private_key/0`, the refute sites,
   the deterministic 409 case).
2. **Mutation (a)'s shape.** A bare atom rename in the plug cannot redden the refute sites (cases
   that must not capture do not consult the key at all), so "rename the plug's atom → both the
   test-constant capture assertions and the strengthened refute assertions redden" is executed as
   one planted revision with two parts: the rename plus a capture on the strict-mutation branch.
   That is exactly the composite the P2 fix describes ("a plug that starts capturing under a
   renamed key in a case that must not capture still reddens"), and the record additionally shows
   the discriminating control — the same spurious capture under the **original** key also reddens
   the strengthened refute, while the bare `Map.has_key?` form stays green under the rename.
   Reported inside mutation (a), not as a fourth mutation.
3. **Comment citation form.** See "Public / API shape" above: the read-site comment carries the
   literal `protocol_controller_test.exs:644` citation plus the case description (contract Change
   (2)); the same citation is recorded in this plan and the report.
4. **Evidence file location.** The contract's owned set is STRICT and does not include
   `docs/evidence/`; this plan carries the evidence section and the run's `answer.md` carries the
   report (shape reference `a503eab7` / `rf-w2-16`). No repository file outside the owned set is
   created or edited.

## Comment-policy sweep (2026-11-22)

Module intent removed from `control/lib/symmetry_control_web/controllers/daemon_controller.ex` by the 2026-11-22 comment-policy sweep is recorded below (workstream record "ws8-control-lib").

- `control/lib/symmetry_control_web/controllers/daemon_controller.ex` (510-516): the
  wire-contract half — *turning this check into a served 400 would change an error
  response outside the slice's byte-identical wire contract* (the in-file note keeps only
  "preserve the pre-merge error shape"; the plan path is in debt below).

The sweep's plan is [`docs/plans/2026-11-22-comment-policy-sweep-plan.md`](2026-11-22-comment-policy-sweep-plan.md).
