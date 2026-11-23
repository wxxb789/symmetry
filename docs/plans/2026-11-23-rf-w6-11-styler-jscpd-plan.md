# rf-w6-11 Styler adoption + changed-file jscpd gate — plan

Base: `cafd5488` (`origin/main` at dispatch; the dependency list of the contract's
`## Dependencies` line — W2-17, W3-03, W3-14, W5-05, W5-06, W6-09, W1-02/#138,
#147, W1-11/#151, W1-12/#149 — is merged into this base). Contract:
`goal-rf-w6-11-styler-jscpd.md` (D12, D16). Shape reference: the `rf-w6-09` plan and
evidence pair.

## What lands

| file | change |
| --- | --- |
| `control/mix.exs`, `control/mix.lock` | `{:styler, "~> 1.12", only: [:dev, :test], runtime: false}` (resolved 1.12.2) |
| `control/.formatter.exs` | `plugins: [Styler]` (keys stay sorted by the key order the file already used) |
| `control/**` | the mechanical `mix format` diff (188 files, +5311/−6304 at the frozen head), every hunk reviewed |
| `package.json`, `pnpm-lock.yaml` | `jscpd@4.3.0` devDependency, lockfile entry from canonical registry URLs |
| `.jscpd.json` | the committed jscpd scope: corpus ignores, detection mode, format table |
| `scripts/ci/jscpd-changed.py` | the changed-file gate (script, exit 0/1/2) |
| `scripts/ci/test-jscpd-changed.py` | the gate's suite (23 tests, real Git fixtures) |
| `docs/plans/2026-11-23-rf-w6-11-styler-jscpd-plan.md`, `docs/evidence/rf-w6-11-styler-jscpd-2026-11-23.md` | this plan and the evidence record |
| `docs/TODO.md` | untouched: neither the base nor the head tree holds a Styler/jscpd/W6-11 entry to close |

`.github/workflows/ci.yml` is **not** in the owned set (contract §Owned files, D16
handoff): W6-12 inserts the single step whose exact text this slice delivers in the
evidence record.

## Implementation choices the acceptance contract leaves open

1. **Corpus = the whole head tree, verdict = the lines the range adds.** The contract
   fixes "changed-file" as the gate boundary but not the comparison side. Scanning
   only the changed files would miss the common case (a changed file copying a block
   out of an *untouched* file), and scanning the working tree would let a dirty
   checkout change the verdict. The gate therefore materializes the `--head`
   revision's tree with `git archive` and matches each clone's interval against the
   added-line ranges of `git diff --unified=0 --no-renames`, read with
   `--no-ext-diff --no-textconv` (fix stage, REV-04) so an inherited
   `GIT_EXTERNAL_DIFF`, a `diff.external` driver or a text-conversion filter cannot
   replace the patch and hide the range's added lines.
   The scan scope — `.jscpd.json` — is read from the comparison base when the base
   holds one (fix stage, REV-03): a range that rewrites the scope cannot exempt its own
   files, and the edit applies from the next range on.
2. **"New" is the criterion, and it is measured on the added lines.** A clone that
   merely re-covers lines a reformat rewrote is tolerated: the mechanical `mix format`
   pass re-touches every line of the 187 files it rewrites, so an added-line test
   alone would refuse the pass itself. A clone is refused when one occurrence holds a
   run of at least `--min-lines` (10) consecutive added lines the file did not already
   hold *as often* at the comparison base: the rule counts line-boundary-aligned
   occurrences of the line's whitespace-free text in the same path's base revision
   (fix stage, REV-01/REV-02), so a reflow or a line join is not new content, an
   uncommented block the base held only commented is, and a second copy of a block the
   file already carried is (a rename can no longer launder it through the old path's
   deleted text). Measured on this PR's changed set: 105 clones reach added
   lines, 3 are refused — the three blocks the new gate script shared verbatim with
   `check-citation-existence.py`/`check-record-cleanliness.py`, which the script was
   then restructured to avoid (the shared preamble is a follow-up in
   `revision_scope.py`, outside this slice's owned set).
2. **Explicit format table, because jscpd 4.3.0 does not know Elixir.** Measured:
   `@jscpd/tokenizer@4.2.6`'s default `EXT_TO_FORMAT` map has no `ex`/`exs` entry,
   so a default run scans none of `control/**` (the tree this slice formats), and
   `formatsExts` *replaces* the default map (finder `getFormatByFile`) rather than
   extending it. `.jscpd.json` therefore declares the repository's languages
   (`elixir: [ex, exs]` plus fifteen of the sixteen default-mapped formats the tree
   uses, each with the extensions the inventory produced). Consequence, asserted by the suite:
   an extension outside the table is never scanned, and a *changed* file in such an
   extension is reported as a notice instead of being silently skipped. The fix stage
   (REV-05) extends the table to the repository's data and configuration extensions the
   inventory had missed (`jsonl`, `lock`, `mod`, `in`, `sum`, `conf`, `eex`,
   `Dockerfile`), and the suite pins one of them (a `.jsonl` clone is refused) plus the
   notice for a file still outside the table.
3. **`mode: "mild"`, the audit's default-equivalent mode** (jscpd's own default), so
   the trend numbers the audit reported stay comparable; the suite pins the mode by
   mutating it (`strict` admits clones `mild` rejects and the fixture reddens).
4. **jscpd is the pinned devDependency, never `npx`** (contract §Conductor decision
   W6-review). The gate resolves `node_modules/jscpd`'s own `bin` entry from its
   `package.json` and refuses (exit 2) when the tree has no installation.
5. **Notice versus refusal.** A run whose report is missing, whose report lists no
   scanned source, or whose jscpd invocation failed is *refused* (exit 2) — a green
   verdict always rests on a report that scanned at least one file of the head tree.
   The missing-report case has one measured exception (fix stage, REV-06): when every
   changed file with added lines is shorter than `--min-lines`, no clone of that length
   can live in them, and the range is green with `nothing to check`. A changed, unscanned
   file is a notice naming the reason (shorter than the line threshold, or outside the
   configured scope), because the corpus is allowed to hold formats the table does not
   name.
6. **Fixture design.** The suite builds real throwaway repositories (`testsupport.
   gitfixture`) and runs the *real* gate as a subprocess against the *committed*
   `.jscpd.json`, so the assertions pin observable behaviour (exit code + message)
   rather than internal helpers. Mutated configs are written into the fixture to
   show what each committed key buys.

## Edge-case semantics

| case | verdict |
| --- | --- |
| range changes no file | exit 0, "nothing to check" |
| changed file deleted, or only its lines removed | exit 0 (nothing added to match) |
| range whose changed files all hold fewer than `--min-lines` lines | exit 0, "nothing to check" (fix stage, REV-06: no clone of that length fits in them) |
| rename without `--no-renames` | deletion plus addition; the new path's added lines are checked like any other, and a renamed file appending a verbatim copy of its own block is refused (fix stage, REV-01) |
| range rewrites `.jscpd.json` | judged by the comparison base's scope; the run reports which revision governs, and the edit applies from the next range (fix stage, REV-03) |
| corpus holds a member tarfile refuses (an absolute-target symlink) | exit 2, "cannot materialize the corpus" (fix stage, REV-07) |
| inherited `GIT_EXTERNAL_DIFF`, `diff.external` or a text-conversion filter | ignored: the added-line diff is read with `--no-ext-diff --no-textconv` (fix stage, REV-04) |
| binary changed file | no hunks, so no ranges to match |
| added lines outside every clone | exit 0 (a clone elsewhere in the same file is not refused) |
| clone inside an unchanged file | exit 0 (repo-wide duplication is a trend number, per D16) |
| new file whose whole body is added | every line is an added line; a copied block reddens it |
| changed file above `--max-lines`/`--max-size` | notice; the limits are passed explicitly so only files above 5000 lines/1 MB are affected |
| changed file in an undeclared format | notice naming the scope (the fix stage extends the table to the repository's data/configuration extensions, REV-05) |
| `--base` missing / all-zero | `revision_scope`'s fallback: `origin/main`, then `HEAD^`, narrowed to the merge base |

## Evidence plan (foreground, serialized; never a measurement harness beside the suite)

1. `mix format --check-formatted` (after the pass) — green, plus mutation check (a):
   re-introduce an alias-order violation in a lib file and show it reddens.
2. `MIX_ENV=test mix compile --warnings-as-errors`, then the full `mix test` — green,
   with the test-name inventory compared base→head.
3. `mix credo --strict` per-rule counts on the base tree and the head tree.
4. `python scripts/ci/test-jscpd-changed.py` — green; mutation check (c): drop
   `--max-lines`/`--max-size` from `JSCPD_LIMITS` and show the suite reddens.
5. The gate on this PR's own changed set — green; mutation check (b): inject a
   12-line/60-token clone into a changed control file and show the gate reddens.
6. `python scripts/ci/test-ci-changed-path-classifier.py` (green, unmodified),
   `python scripts/ci/check-lockfile-registry.py`, `git diff --check`, the record
   cleanliness gate over this change's own records, and the DLP scans (host-profile
   paths, forbidden escape sequence).
7. Repo-wide jscpd number at the head, trend-only (D16), with the audit's thresholds.

## Out of scope (with reason)

- the `ci.yml` step itself (W6-12 owns that file; the step text is delivered here);
- repo-wide jscpd enforcement, `daemon/.golangci.yml` `dupl` thresholds (W2/W3);
- the W2-W4 dedup work, the Credo/coverage flips (W6-10), duration ratchets (W6-12);
- non-control formatting: the plugin and the pass are `control/`-scoped by
  `control/.formatter.exs`'s own `inputs`.

## Residual risks and limits

- `mode: mild` (like the audit) treats a block whose comments differ as non-cloned;
  the mode is pinned by test, and `strict` remains a one-key change.
- A copy whose every line keeps its multiplicity — the range copies a block and deletes
  an equal-content block elsewhere in the same file — is tolerated: the count rule
  cannot tell a net-zero reshuffle from a move (fix stage, REV-01's rule). Any copy that
  grows a line's multiplicity is refused.
- A whitespace-only line is always held (the empty needle has no occurrences), so a
  blank-only run can never refuse a range; one fixture pins the tolerance.
- Only the first range that introduces `.jscpd.json` is judged by the scope it writes:
  there is no earlier scope to read, the window is one range wide, and every later range
  is judged by the base's table.
- jscpd's clone intervals are token-anchored, and for chained clones a side's
  `start`/`end` is occasionally inverted (85 of 2548 sides over the whole corpus); a
  side is therefore only treated as reached when its own interval overlaps added
  lines, and the tolerated/refused decision never reads the fragment text.
- The changed-file gates share a CLI/range preamble; this gate's copy is restructured
  to the minimum it needs. Extracting it into `revision_scope.py` — the `--name-status
  -z` field reader this gate repeats from `revision_scope.changed_paths` included — is
  the follow-up and lies outside this slice's owned set.
- The gate's runtime is linear in the head tree (a `git archive` plus one jscpd scan
  per run); on the CI runner that is the cost of the changed-file verdict.
- The report's key names (`duplicates`, `statistics.formats.*.sources`) are jscpd 4's;
  a jscpd major upgrade would need the pinned-version re-review the lockfile implies.
- The corpus is the head *tree*, so a clone that only exists in the diff context (for
  example because base carried a block the head moved) is judged on head's bytes.

## Outcome on the final tree (2026-11-23)

| what | result |
| --- | --- |
| the pass | 188 `control` files, `+5311 −6304` at the frozen head; two hunks rejected and re-expressed (§4 of the plan / evidence §4) |
| `mix format --check-formatted` | 0; the alias-order mutation reddens it (1) and the revert restores 0 |
| `MIX_ENV=test mix compile --warnings-as-errors` | 0 |
| full `mix test` | 0 — `1163 passed, 21 skipped` (331 s), the portal-bundle cases included (apply-stage re-run; the fix stage changes no `control/**` byte) |
| test names | 130 files under `control/test` (109 `_test.exs`), 1173 names at the base and 1174 at the head: multiset identical for the format pass, plus the apply stage's one documentation assertion. **No test renamed or removed**; the added name is disclosed in evidence §13 |
| `mix credo --strict` | alias class −46 (AliasOrder 26→0, AliasUsage 33→13); total 554→564 (MaxLineLength +120, the formatter's line joining); report-only |
| the gate's suite | 32 tests green (`scripts/ci/test-jscpd-changed.py`; 23 before the fix stage) |
| the gate on this PR's changed set | refused 3 clones before the script's preamble was restructured (real new duplication), green afterwards (100 clones tolerated, 0 new); at the frozen head `b9d4b4a4..482a37f8`: 195 changed files, 1677 scanned sources, 1272 corpus clones, 100 tolerated, 0 new; on the fix-stage tree (throwaway clone, the extended scope): 195 changed files, 1700 scanned sources, 1273 corpus clones, 100 tolerated, 0 new |
| repo-wide jscpd (trend-only) | base 4.53 % (1275 clones, 1829 sources) → head 4.53 % (1273 clones, 1848 sources); 244 → 250 clones with a `control/` side (6235 → 6300 cloned lines) — the audit's "0 touching control/" was vacuous: jscpd 4.3.0 does not scan Elixir by default |
| other gates | ruff 0, classifier suite 0 (31 tests), lockfile provenance 0 (405 URLs), `git diff --check` 0, record cleanliness 0, DLP scans clean |
| CI step | delivered in evidence §9 for W6-12; `.github/workflows/ci.yml` untouched |
