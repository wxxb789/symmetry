# CLOSED TODO

Done items moved out of `docs/TODO.md`, each kept with the closure statement its own text records —
that the thing is fixed, enforced, closed or resolved — and a named commit, PR or branch where one
is recorded. `docs/TODO.md` holds only undone or in-progress items; where a closed part and an open
residual share an item, the closed part is here and the residual stays in `docs/TODO.md` with a line
saying where it went. This file is separate from `docs/goals/archive/`, which holds archived
contracts. Section names of `docs/TODO.md` are preserved below so context survives the move, and
fragments of items keep their original indentation. Each entry keeps its closure statement and any
closure date it records. Entries in this file older than six months may be deleted.

The blocks below were moved on 2026-11-18. Markers (`- [ ]`, `- [x]`) are reproduced as they were,
not a claim that work remains. The two blocks moved on 2026-11-19 (branch
`fix/portal-return-targets`) name that branch in their closure lines.

The five dead-code closures appended to `## Dead-code candidates` below were moved on 2026-11-19
from branch `chore/deadcode-revalidation`.

The closed class scan appended to `## Paginated reads must be bound to the cursor that served their
pages` below was moved on 2026-11-19 from branch `fix/paginated-reads-cursor-binding`.

The 0007f render-gate fixture item and the 0007h rollout-off residual appended to the `## Deferred
review findings (0007f)` and `(0007h)` sections below were moved on 2026-11-19 from branch
`fix/ci-render-gate-fixtures`.

The closed reversed-import Control flake appended to the second `## Dead-code candidates` section
below was moved on 2026-11-20 from branch `investigate/flake-reversed-import`.

The closed legacy owner-bookmark item appended to the `## Retro review follow-ups (second pass,
2026-11-20)` section below was moved on 2026-11-20 from branch `fix/legacy-owner-bookmarks`.

The closed Control-row annotation item appended to the `## Model-routing evaluation records`
section below was moved on 2026-11-20 from branch `fix/ablation-control-row-annotate`.

## Model-routing evaluation records

- [x] Fixed the half-applied usage-receipt import (serialization gap) reported by the cancelled 0008a round-2
      review. `receipt` in `scripts/ci/model-routing-evaluation.py` derived the candidate usage and published
      `frozen/usage.json` and `run.json` before `write_json(receipt.json)` serialized the receipt, so a valid
      JSON usage row carrying a lone surrogate (`\ud800` in the opaque `metadata` passthrough: `strict_json` and
      `validate_usage` accept it, `canonical_json` refuses it at `encode("utf-8")`) left the frozen usage bytes
      and the run record at the candidate revision while `receipt.json` stayed at the previous one; `accept`
      then refused at `$.evidence.frozen_usage_sha256` and the run could not be re-derived. Fix:
      `command_receipt` serializes the derived receipt and the run record (`run_record_bytes`, shared with
      `save_run`) before the first write, so no document is published unless the receipt for the candidate has
      been serialized; on any refusal the three files stay byte-identical. Reproduction:
      `python3 scripts/ci/test-model-routing-evaluation.py -v EvaluationTests.test_serialization_refusal_publishes_nothing`
      (red before the fix on the `frozen/usage.json` byte equality, green after; re-broken on demand by moving
      `canonical_json(receipt)` back after the writes, which reproduces the same failure). Validation was not
      relaxed: the surrogate document is still refused.

- [x] Rebound the 0008a audit's source manifest to the delivered revision `1c6c715` (audit finding F12) after
      the final rebase left its previous binding unreachable. The manifest and the repair list named the
      pre-rebase feature tip `c45db2c` and the pre-rebase repair commit `1baf76c`; once the branch was rebased
      onto `7a92ca4` and delivered as `1c6c715`, `git show c45db2c:<path>` failed for a reader and eight entries
      described no published revision - including `docs/goals/README.md` (16713 bytes `sha256:aac4ca76…` ->
      17578 bytes `sha256:b764dabf…`) and `.github/workflows/ci.yml` (103330 `sha256:a0ad0020…` -> 103379
      `sha256:4e73a080…`). Rebinding was chosen over marking history because the manifest's own method is
      `git show <bound commit>:<path>`, and only main's history is durable for a reader. All 17 entries now
      re-derive with `git show 1c6c715:<path>`; the scoped digest is `sha256:9dc95d2f…` (was `sha256:34cc3198…`);
      the pre-rebase values, the moved list and the delivered commit mapping (`e4a91c1` F1-F3, `bdbcdc7` F4-F9,
      `abf8210` F10-F11, `993870e` post-audit repairs, squashed as `1c6c715`) are retained in F12; the evidence
      record's quoted audit digest (`sha256:dc878099…`) and manifest digest are updated, and its delivery
      narrative now names the delivered commits. Run-time observations (gate results, link checks, receipt
      re-derivations recorded in the audit) remain bound to the pre-rebase reviewed revision and are labelled as
      history.

- [ ] Decide whether the 0008c evaluation records keep their raw host paths. The four committed
      receipts (`docs/evaluations/model-routing/0008c-{a-mixed,a-strong,b-mixed,b-strong}.json`,
      `environment.measured.mix_build_path` and `mix_deps_path`) and the committed copy of
      `docs/evidence/goal-0008c-elixir-model-routing-2026-11-17.md` state the operator's absolute
      temp root (the `symmetry-0008c-eval` run store under the local user profile). They are produced,
      committed evidence bound by digests other records name, so the 0008c audit recorded them as
      an open item instead of rewriting history. The runner's sanitisation extension
      (`validate_measured_mapping(..., redact=True)`) now records every host-specific absolute path
      in a measured value as a `sha256:` token, so run records and receipts written from now on
      cannot republish a disposable root or the operator profile. Revalidate before any scrub, and
      never edit a receipt whose digests other records bind. Decided by rendering rather than
      keeping the roots: closed 2026-11-19 on branch
      `chore/eval-record-scrub` under the owner's one-time Q2 authorization (revalidated first, as the entry asked):
      the four receipts and the evidence record were rendered through `scripts/ci/model-routing-record-sanitize.py`
      (`sha256:b8b252f3…`, the tool's bytes on 2026-11-19; #79 (`21cf326`) has since changed it to
      `sha256:52f7c182…`) — two substitutions per receipt (`mix_build_path`/`mix_deps_path` ->
      `<disposable-root>/… (outside the checkout)`) and one in the evidence record — and every digest that binds
      them was recomputed and rebound: the four receipt digests quoted by `0008c-candidate-policy.md` and by the
      evidence record (`9432545d…`/`26d8e003…`/`e79fe173…`/`1be7b419…` ->
      `93050ced…`/`5ee9b87b…`/`ceae34ce…`/`af12819d…`). The tool's `--check` exits 0 on the two receipts whose
      absolute-path matches were all host paths (`0008c-a-strong.json`, `0008c-b-mixed.json`); the other two keep
      only repo-relative prose fixtures that rendering would
      corrupt. Full before/after digest table (including a later second-generation rebind of
      `0008a-controls.json`'s rendered `missing-gate` stderr digest) and the deliberately-left list:
      `docs/evaluations/model-routing/README.md:305`, "Record rendering exception (2026-11-19)".

- [ ] Open: no checker re-derives the commit/path digest bindings quoted by the model-routing records, which is
      how the unreachable 0008a manifest binding survived review. A future increment could add a gate that
      resolves every quoted `git show <commit>:<path>` binding and recomputes every quoted manifest digest
      against the revision it names. This change moves `scripts/ci/model-routing-evaluation.py` and
      `scripts/ci/test-model-routing-evaluation.py` again, so the 0008b/0008c/0008d receipts stay fail-closed
      for submission under the current bytes exactly as their entries below describe; re-execute the runs, never
      re-derive by editing a receipt. Closed by the one-time reference-binding audit of 2026-11-19 (branch
      `chore/eval-record-scrub`; the owner scoped Q5 to a one-time audit, no gate): an uncommitted script parsed
      every model-routing record and 0008-series evidence document and verified the commit/path bindings (commit
      resolves, reachable from `origin/main`, a tag, or GitHub; path exists at that commit), the 0008a source
      manifest (17/17 entries plus bound tree and canonical scoped digest recompute at `1c6c715`), the 0008d
      landing-tip source subject (recomputed at `73e083f`: 1141 paths, exact), 42 tooling-source digests at their
      tags/commits, the receipt frozen review/task digests, and every quoted file digest at HEAD — checked 61,
      broken 0, fixed 0 (this branch's nine-file digest cascade is the only rebinding this audit required),
      0 mismatches, with 15 sealed subject manifests and the sealed-checkout `HEAD:` references classified
      unverifiable-private by design. Two referenced pre-rebase commits (`c45db2c`, `1baf76c`) are GitHub-absent
      and stay labelled history with no main equivalent; five (`4644fd7`, `993870e`, `abf8210`, `bdbcdc7`,
      `a9f4d93`) are unreachable from local refs but still served by the GitHub API, and `cab222b` is preserved
      via the tag `0008c-produced-tooling`. No live binding was broken, so no binding needed rebinding beyond
      this branch's digest cascade (recorded in the README exception section). No CI gate was added; a gate
      proposal would be a new item.

- [ ] Decide whether the four 0008d receipts ever need re-production once the evaluation tooling
      changes again. The TypeScript carrier added gate fields `vitest_tests`/`vitest_files` and the
      derived structured-report cross-check, so `scripts/ci/model-routing-evaluation.py` and its
      regression suite changed after the 0008a/0008b/0008c receipts were produced. Those three
      letters' receipts already refused re-derivation under the 0008c-era bytes (recorded in the
      entry below); the 0008d receipts re-verify with the current bytes only, and any future tooling
      edit invalidates them for submission in exactly the same fail-closed way. Nothing must change
      unless a submission requires re-derivation, in which case the affected runs are re-executed,
      never re-derived by editing a receipt.
      The letter's own runner then changed once before publication (the `vitest_report_detail`
      refusal order that the first full suite pass rejected, described in the closure record), and
      the four receipts were re-derived to the corrected bytes without re-executing a gate or
      re-importing a review, three times: first for the refusal-order fix, then for the cache-predicate
      wiring that the independent reviews asked for, and a third time for a docstring-only change
      (the refusal-order paragraph now names the walk's structural branches). Superseded history:
      runner `sha256:d80e49ca…` then `sha256:3f3b0975…` then `sha256:619f0d37…`, source subject
      `sha256:5abd13ec…` then `sha256:5e1e4e6c…` (both before the subject scope was narrowed to a
      declared set that excludes generated bytecode caches and a worktree `.git` pointer file). The
      published receipts bind the delivered runner `sha256:81504715…` (and the auxiliary tool
      digests); the delivered suite `sha256:dbe1a0fe…` is named in the series receipt's subject
      table rather than bound by a receipt.
      The source subject is now computed by a delivered, reader-runnable tool
      (`python scripts/ci/model-routing-source-subject.py`, scope and entry formula in its
      docstring). Two values are current, each on its own revision: the *evaluated* revision
      (`dacec450` plus this delivery's source paths) is `sha256:e333bb85…` over 1120 source paths,
      and the landing tip after the rebase onto `ca1bdad` (`ca1bdad` plus this delivery, where `main`
      added 21 source paths under `contracts/`, `browser/` and `frontend/`) is
      `sha256:70e03853…` over 1141. History, in the order the run computed it: `sha256:5abd13ec…` (1121 paths, first scope), `sha256:5e1e4e6c…`
      (with the `sha256:3f3b0975…` runner bytes), `sha256:e0976262…` (1123 paths,
      `sha256:619f0d37…` runner) and `sha256:4db67385…` (1118 paths, same runner) - and it was the
      last of those that was published, which a pre-publication audit showed to be stale: it was
      computed before the runner's final docstring edit, so the delivered tree hashed to
      `sha256:245edc15…` (1118 paths, delivered runner `sha256:81504715…`; derived by that audit, and
      a value the run's own records and session transcript do not carry). The repair republished the subject and added the derivation
      test to the suite and the subject tool with its test, which is why the delivered count is two
      paths higher than the tree that value bound.

      **Closed (2026-11-19, `feat/eval-tooling-bindings`):** the decision is recorded in
      `docs/design/quality.md` (Model-cost evaluation, closing paragraph): a receipt is re-produced
      only by re-executing the evaluation under the current tooling and never by editing a receipt,
      an older receipt stays verifiable against the revision that produced it, and the current
      runner refuses it for submission - so the 0008d receipts need re-production only if a
      submission requires it, and then the affected runs are re-executed. This very change moves
      the evaluation runner again, which invalidates the four 0008d receipts for submission under
      the new bytes in exactly that fail-closed way.

- [ ] Decide whether the 0008d published records need a second rendering pass. No host-specific
      absolute path ships in any of them: the four receipts are published as the runner's derived
      bytes (`accept` re-derives each one byte for byte) and both narrative records name the private
      store root only as `<store>`. The store's stored session text was rendered through
      `scripts/ci/model-routing-record-sanitize.py --root <store>` before the runner read it, so the
      derived receipts still carry `path-sha256:` tokens for the text that was rendered (the fragment
      `/||` became `path-sha256:019ba55f…` inside the `0008d-a-mixed.*` pair; the `0008d-a-strong.*`
      pair carries `…e11f3495…`, `…810a366d…` and `…523fbab2…` for its quoted contract paths), and
      the frozen `outcome` claim keeps the literal route `/portal/runtimes`, which is what keeps the
      token correlatable. `--check` therefore still exits 1 for those records - the tool renders
      every absolute path, not only host ones, which its docstring names as its boundary - and it
      exits 0 for the four review records and the `0008d-b-*` receipts. Nothing must change unless a
      future audit wants literal routes in reviewer prose, which needs a root-only rendering mode in
      the tool, never an edit to a bound record.

      **Closed (2026-11-19, `feat/eval-tooling-bindings`):** the sanitize tool gained the requested
      root-only mode (`--root-only`): it renders or flags only host absolute paths (Windows drive and
      UNC roots, POSIX home/root/temp/mount roots) plus any `--root` disposable root, so product
      routes such as `/portal/runtimes` stay literal, and a bare root token with no following
      separator or segment stays literal too. `--check --root-only` exits 0 for every committed
      `0008d-*` record - the four receipts, their four review records and the candidate policy pair -
      which is the second rendering pass the entry asked for: no record was edited, and the
      pre-existing raw host paths in the older 0008-series records stay with their own open entries.

- [ ] Decide whether the shared hoisted dependency tree should be bound. The 0008d TypeScript gates
      resolve their tools through a hoisted pnpm install one level above the run directories (the
      run's `environment.json` records the route), whose bytes sit outside every sealed checkout and
      outside the source subject. The receipts record that route and state that the integrity values
      are the frozen lockfile's own, but the store keeps no provisioning log, no install output and
      no digest of the resolved tree, so that claim cannot be re-verified and a file placed in that
      tree is unbound for a whole run. The honest fix is a recorded provisioning step - log the
      install and bind a digest of the tree the gates resolve - which is its own increment because
      it changes what a receipt binds, not an edit to this letter's records.

      **Closed (2026-11-19, `feat/eval-tooling-bindings`):** the runner gained `bind-dependencies`
      (`--run-dir`, `--tree`, `--log`, `--check`): it refuses a tree inside the run's sealed
      checkout, computes a deterministic digest over every file and link of the resolved tree,
      copies the install log into the run store (`provisioning/install.log`) and records both - tree
      route, tree digest, entry count, log route, log digest and byte count - in `run.json`'s
      `environment.provisioning` block, which every receipt derived from the run states and which
      every `load_run` re-verifies against the stored log bytes; `--check` recomputes the tree
      digest so a preserved tree still re-verifies. Regression tests cover determinism, a changed
      tree, an in-checkout tree, a missing or empty log and a tampered record. The 0008d records
      themselves were not edited; a future TypeScript run executed under this tooling carries the
      bound provisioning step.

- [ ] Decide whether the manifest's tool-cache exclusion should be narrowed. The Source manifest
      excludes `node_modules/.vite`, `node_modules/.vite-temp` and `node_modules/.vitest-cache` so
      that vitest's run state cannot change the revision a gate just verified. Files inside those
      directories are therefore unbound - the seal records no digest for them - so in principle a
      worker could hide code there that a relative import reaches during a gate. No run has written
      such a file and the boundary is stated in the runner docstring, the closure record, the series
      receipt and the candidate policy. The honest narrowing is vitest run state outside the
      checkout (`cacheDir`/`VITEST_CACHE_DIR` or a gate-level redirect, the way the Playwright html
      report is already redirected), which changes a frozen gate's environment and therefore needs
      its own increment with re-derived evidence, not an edit to this letter's records.

      **Closed (2026-11-19, `feat/eval-tooling-bindings`):** vitest run state is redirected outside
      the sealed checkout instead of excluded from the manifest. `frontend/vite.config.ts` and
      `frontend/vitest.config.ts` resolve `SYMMETRY_TOOL_CACHE_DIR` as Vite's `cacheDir` and
      vitest's `fsModuleCachePath`, the runner exports that variable for every gate it executes and
      records the run-relative redirect (`tool-state/<attempt>/<gate id>`, a fresh directory per
      attempt) in the gate result and the receipt, and a gate also exports
      `SYMMETRY_DISABLE_TOOL_CACHES=1`, which the vitest config reads as its test-level
      `cache: false` and as `fsModuleCache: false`, so neither cache channel survives a gate,
      and `prepare` gives the run store an empty `node_modules` so Vite's config-loader temp files
      (`.vite-temp`) land in the run store rather than inside the checkout or the shared dependency
      tree. The `TOOL_CACHE_DIRECTORIES` exclusion was removed from the Source manifest, so run
      state written into the checkout is now bound and fails the gate (fail-closed) instead of
      staying unbound; the runner and manifest tests cover both directions.

- [ ] Decide whether the four 0008c receipts ever need re-production under the current evaluation
      tooling. They bind the produced revision, preserved as the annotated tag
      `0008c-produced-tooling` (commit `cab222b`): all four re-verify with that revision's
      bytes (`git archive 0008c-produced-tooling scripts/ci`, then `accept`, exit 0 each), while
      the post-audit tooling refuses them for submission (`accept` exit 1 at
      `$.evidence.tooling_sources.model-routing-evaluation.py`) — the documented fail-closed
      consequence of changing receipt-bound tooling, labelled in the evidence record with both
      digests. Nothing must change unless a future submission requires receipts produced by the
      current bytes, in which case the runs have to be re-executed, never re-derived by editing a
      receipt.

      **Closed (2026-11-19, `feat/eval-tooling-bindings`):** same decision as the 0008d
      re-production entry above, now codified in `docs/design/quality.md`: the four 0008c receipts
      stay verifiable against the `0008c-produced-tooling` revision and are refused for submission
      by the current runner by design; if a submission requires current bytes, the runs are
      re-executed, never re-derived by editing a receipt.

- [ ] Own the record-cleanliness ratchet baseline: 26 unchanged committed records under
      `docs/evaluations/**` and `docs/evidence/**` still carry a host absolute path, so the
      `record-cleanliness` CI job - which checks only the records a revision range adds or
      modifies - fails the moment a later change touches one of them. Enumerated at `5d6c858`
      with `python3 scripts/ci/model-routing-record-sanitize.py --check --root-only` over
      `git ls-files docs/evaluations docs/evidence`: the four
      `docs/evaluations/model-routing/0008b-go-bounded-regression-{mixed,strong}{,.review}.json`
      receipts; `docs/evidence/goal-0006-linux-owner-design-2026-09-22.md`,
      `goal-0006-final-series-review-2026-11-15.md`,
      `goal-0006i-exact-subject-release-acceptance-2026-11-15.md`,
      `goal-0006j-omp-native-capability-2026-11-14.md`, `goal-0007b-readable-work-views-2026-11-15.md`,
      `goal-0007i-portal-cutover-series-receipt-2026-11-17.md`,
      `goal-0009-source-identity-2026-11-16.json`; the seven `goal-0006b-*` files
      (`acceptance-environment-audit`, `acceptance-source-audit`, `convergence-receipt`,
      `fixture-inventory`, `resumed-go-gates` and `surface-qa` of 2026-09-29, and
      `iteration-closure-2026-10-10`); the two `pi-control-e2e-*-2026-09-13.md` files, the five
      `test-ablation-*-2026-09-29.md` files and `windows-opencode-ci-gate-2026-09-14.md`. Each one
      needs either an explicit decision to leave it as produced or the render-and-rebind pass
      (re-render the text and keep every digest another record quotes consistent - of this list,
      `test-ablation-go-probe-experiment-2026-09-29.md` is the one quoted by another record). Until
      that decision, a change that merely touches one of these records fails the job by design, and
      the remedy is that render pass, never an edit that breaks a binding.

      **Closed (2026-11-19, `chore/render-dirty-records`):** rendered rather than kept as produced.
      A fresh whole-corpus enumeration (`--check --root-only` over `git ls-files docs/evaluations
      docs/evidence`) reproduced exactly the 26 records listed above, and each was rendered in
      place through `scripts/ci/model-routing-record-sanitize.py` in the `--root-only` scope, with
      no disposable root configured for these legacy records. That scope with the #83 run-store
      roots configured renders #83's (`29462e9`) rendered bytes exactly for `0008a-audit.json` and
      the four `0008c` receipts, provided each root is spelled as `29462e9^` spells it:
      the `symmetry-0008a-…` temporary run root with JSON-doubled backslashes for the `0008a`
      records (the plain spelling matches nothing and does not reproduce those bytes) and the
      `symmetry-0008c-eval` temporary run root in either separator spelling for the `0008c`
      receipts, because the root replacement is a literal string match (both roots are readable
      in the `29462e9^` record bytes and are not repeated here). #83's committed bytes
      additionally carry its own digest rebinds (in
      `0008a-controls.json` the `stderr_sha256` rebind `c12881ae…` -> `8503bfa2…`, and the
      receipt and audit digests quoted by `0008c-candidate-policy.md`,
      `docs/evidence/goal-0008c-elixir-model-routing-2026-11-17.md` and
      `docs/evidence/goal-0008a-auditable-acceptance-workflow-2026-11-15.md`). This pass
      rendered 83 host-path substitutions over the 26 files, so every host location, scratch roots
      included, became a deterministic `path-sha256:` token while product routes stay literal.
      Host-shaped specimen paths quoted inside reviewer prose were not spared: six of the 83
      substitutions tokenized the `//etc/passwd` and `\\dir\\file.go` specimens in
      `docs/evaluations/model-routing/0008b-go-bounded-regression-mixed.json` (line 627) and its
      `.review.json` (line 6), and the `C:\\dir\\file` specimen in
      `docs/evaluations/model-routing/0008b-go-bounded-regression-strong.json` (line 487) and its
      `.review.json` (line 6); their pre-render spellings remain recoverable from git history at
      the base commit (`b33188b`). One tool fix was required and ships with a
      regression test: the renderer consumed the backslash that escapes a quoted path's closing
      quote, leaving a bare quote that made `0008b-go-bounded-regression-strong{,.review}.json`
      invalid JSON; an odd trailing backslash now stays outside the token, old-versus-fixed
      rendering differs on exactly those two files, and `--check` verdicts are unchanged on all
      111 corpus files. Every digest that bound a rendered file was recomputed and rebound, each
      new value equal to its file's recomputed `sha256` (old->new table in the branch's PR): the
      review digests inside the two receipts; `0008b-policy.json`'s two `review_sha256` values,
      two `receipt_sha256` values and `review_attempts_sha256`; `0008b-controls.json`'s embedded
      accept output (with its `output_sha256` recomputed over the edited text) and
      `source_accepted_receipt_sha256`; the mixed `review-attempts` record's `review_sha256`; and
      `docs/evidence/goal-0008b-go-model-routing-2026-11-17.md`'s two receipt digests, policy
      digest and controls digest, plus `test-ablation-final-review-2026-09-29.md`'s app-startup
      and Go-probe rows (the cross-record quote this entry named). Evidence: the whole-corpus
      `--check --root-only` sweep reports 0 dirty of 111 files (superseded 2026-11-20: the
      widened UNC leader pattern flags the two `0008b-go-bounded-regression-strong` specimens
      named in the records README's accepted false positives; the records were deliberately
      not re-rendered),
      `python scripts/ci/check-record-cleanliness.py --base origin/main --head HEAD` exits 0,
      re-running the renderer over all 30 touched files is byte-stable, and
      `test-model-routing-record-sanitize.py` (28 tests) with `test-check-record-cleanliness.py`
      (16 tests) pass.

- [ ] Bound the Windows lingering reap in `scripts/ci/model-routing-evaluation.py`. In
      `run_owned_command` (~1738-1768) a timed-out gate is tree-killed and then reaped with
      `process.communicate(timeout=PROCESS_TREE_REAP_SECONDS)`, which on Windows can keep waiting
      on the gate's stdout/stderr pipes while a descendant the snapshot tree kill missed still
      holds the inherited write ends - past the documented reap window and past any caller that
      expects `check` to return once the kill finished (the timeout-tree fixture's survivor
      witness was masked this way until the fixture spawned its descendant with `DEVNULL`).
      Fix direction: close the read ends from a daemon thread, or leave the final handle close to
      process exit, so a pipe-holding survivor can never extend the reap; add a regression case
      with a pipe-holding survivor. Not edited here because PR #79 rewrites this file.

      **Closed (2026-11-19, `fix/eval-runner-windows-reap`, PR #100):** the lingering reap never
      waits on a blocked pipe. `run_owned_command` reaps a timed-out gate with the bounded
      `communicate(timeout=PROCESS_TREE_REAP_SECONDS)` plus `kill` and a bounded `wait` on the
      direct child, so a descendant that escaped the tree kill (or the platform process tree) can
      hold the inherited write ends without extending the reap; the call returns `timed_out=True`
      with the output the POSIX reap read before the kill, while a Windows capture comes back
      empty when a survivor holds the pipes (`communicate` there publishes a reader's pipe data
      only at EOF). In the lingering branch the reap then closes its read ends on POSIX, where
      `communicate` reads on the calling thread so the close cannot queue behind the survivor and
      the finalizer close at return would emit `ResourceWarning: unclosed file`; Windows keeps
      them open, where a close queues behind the reader thread parked in the pipe, and that
      thread closes the stream once the survivor releases the write ends, or process exit does.
      The regression case
      `EvaluationTests.test_lingering_reap_is_bounded_when_a_survivor_holds_the_pipes` spawns a
      gate whose descendant inherits stdout/stderr and outlives the kill (the kill is narrowed to
      the direct child, so the survivor is missed, standing in for a descendant that escapes
      `taskkill /T`), asserts the reap consumes its window (measured duration at least `timeout +
      PROCESS_TREE_REAP_SECONDS - 2s`) and returns within the code's documented worst case
      (`timeout + 2 * PROCESS_TREE_REAP_SECONDS + 5s`, with the failure message reporting the
      measured duration), that the lingering branch waits once on the direct child within its own
      reap window (the wait call is recorded through the delegating process proxy, since `kill`
      alone already sets `returncode`), reports the timeout, on POSIX returns exactly the output
      captured before the kill with both read ends asserted closed, keeps the Windows stdout
      asserted open behind the parked reader, and terminates the survivor: red on
      the pre-fix runner (`py -3.12 scripts/ci/test-model-routing-evaluation.py
      EvaluationTests.test_lingering_reap_is_bounded_when_a_survivor_holds_the_pipes -v` ->
      "run_owned_command did not return within 27.0s while a survivor held the gate's pipes: ...
      survivor running at the bound=True, reap finished 27.8s after the call started, reap
      finished after release=True", FAILED (failures=1) in 29.787s), green after (OK in 15.343s);
      the focused descendant test passes on Windows (OK in 17.824s) and both tests pass under WSL
      Ubuntu `python3` (OK in 28.885s), with the full Windows suite passing (`py -3.12
      scripts/ci/test-model-routing-evaluation.py -v` -> Ran 66 tests in 1167.657s, OK,
      skipped=2: the POSIX-only executable-mode and non-UTF-8 path fixtures).

- [ ] Decide how the Control row in
      `docs/evidence/test-ablation-final-review-2026-09-29.md:148` is bound: its digest
      `e2672276b4f4171391a6df202dc01e9e66ccc705d72e4ee095db7471e7a1c6f9` matches no reachable
      revision of any `test-ablation*` file (every distinct blob revision of the fourteen such
      paths across all refs, under LF/CRLF, BOM and no-final-newline variants: 0 hits), and the
      row has read that way since `72bcf8e`. Observed during the record render pass (PR #101),
      which rebound the app-startup and Go-probe rows in the same table and left this one as
      recorded; pre-existing, not introduced by that pass. Do not edit the record before the
      decision, in a separate maintenance change: rebind the row to the reviewed artifact's
      committed bytes, or annotate it as the as-reviewed artifact rather than the committed bytes.

      **Decided and closed (2026-11-20, owner; branch `fix/ablation-control-row-annotate`):** option
      A - annotate, no rebind. The row keeps
      `e2672276b4f4171391a6df202dc01e9e66ccc705d72e4ee095db7471e7a1c6f9` and every other digest and
      number in the record unchanged; the record now carries the annotation "… the Control row
      above binds the Control C1/C2 test-ablation experiment report as reviewed, meaning the report
      text as delivered to this review, not the committed bytes of
      `docs/evidence/test-ablation-control-experiment-2026-09-29.md` …" (recorded 2026-11-20, the
      paragraph after that table in the record; `:148` still holds the Control row, so this entry's
      citation stays exact). Revalidated before annotating, as the entry asked: a blob walk bounded
      to `fdf332d` (`git rev-list --objects fdf332d`, then `git cat-file blob` over every reachable
      blob) found the digest on no committed blob reachable from that revision - 0 hits over 5,613
      objects (3,269 blobs, 2,344 trees and commits) and 26,088 raw-byte, LF/CRLF, BOM and
      no-final-newline spellings - and
      the fourteen paths whose names carry `test-ablation` (19 distinct blob revisions, 152
      spellings) also gave 0 hits, which reproduces the counts above. The five neighbouring rows of
      that table do bind the committed bytes of their reports (checked against their HEAD blobs),
      so the annotation keeps
      the exception to this row. Docs-only: no behavior changed, so no test was added, modified or
      weakened (the only automatable assertion here would be over prose text, which pins wording
      rather than behavior); the pin this closure names is the deciding check - the
      record-cleanliness gate the path classifier runs for
      `docs/evidence/` changes (`python3 scripts/ci/check-record-cleanliness.py --base <base>
      --head HEAD`) exits 0 over the annotated record, and `python3
      scripts/ci/model-routing-record-sanitize.py --check --root-only
      docs/evidence/test-ablation-final-review-2026-09-29.md` exits 0. No digest needed rebinding:
      the record's own digest is bound by no other file.

## Dead-code candidates

- [ ] Investigate the intermittent Windows failure `TestCloseStopsOwnedChildAfterLeaderExit`
      in `daemon/internal/platform` (`process_windows_test.go:1004`, "owned child <pid>
      survived Close()"). Observed in run 43720680428 job 132831780245 on a docs-only diff,
      green in run 43715830442 and on rerun job 132834151127. Revalidate the close/settle
      window; keep the survived-child assertion.
      Closed by the identity-bound close/settle proof
      recorded in the `daemon/internal/platform` flake-class entry below (the second occurrence,
      run 44055523619 job 133943809688, is classified there); the observation above stays as the
      record, and only the wider 0008b settlement contract recorded there remains open.

- [ ] Investigate the load-sensitive omp harness failure
      `TestFinalAssistantMessageAcceptsReasoningBlocksBesideSingleTextBlock` in
      `daemon/internal/harness/omp` (`adapter_final_message_test.go:44`,
      `context deadline exceeded` after 1.38 s). Observed once during goal 0008b's
      repository-wide `go test ./...` run (1,797 top-level tests: 1,740 pass,
      56 skip, 1 fail) while that package alone then reported `ok` at 4.631 s,
      and not reproduced in isolation: `-count=3` passed on the unchanged base
      clone `5c2e0ada` and on the working tree. Revalidate the deadline budget
      under load; keep the assertion. Closed on `main` by `8082317` (#44): 89 wall-clock budget
      sites across 16 test files now point at one documented `testHangGuard` per package
      (`const testHangGuard = 30 * time.Second`, "turns a hung harness into a failure; never a
      statement about how fast the adapter should be"), no assertion was removed or weakened, and in
      `omp` a failing wait now names the outstanding step (`mustSettle(t, "WaitTurn", err)`) so a
      guard expiry can no longer be reported as this test's behavioural failure. The observation
      above stays as the record; the open action does not.

- [ ] Investigate the intermittent Linux failure `TestLinuxSupervisorWrongHelloSessionRejectsBeforeTargetExec`
      in `daemon/internal/platform` (`containment_supervisor_linux_test.go:1128: decode
      wrong-session helper witness: unexpected end of JSON input`). Observed in run
      43724904655 job 132844651104 on a workflow-only diff, then green on rerun job
      132846453648. Revalidate whether the helper witness can be decoded before it is fully
      written; keep the witness assertion itself and leave the fix to separate maintenance.
      Second occurrence: run 44133750246 job `daemon` on main `3781c78`, the same tree that
      passed the identical job in the PR #56 run minutes earlier; the failing line and message
      match. Mechanism: `writeLinuxSupervisorHelloSessionWitness` created the destination with
      `os.OpenFile(O_WRONLY|O_CREATE|O_EXCL)` and wrote the JSON afterwards, so the poller
      `waitForLinuxSupervisorTestMarker` accepted a zero-byte document as its first successful
      read and decoded it; the helper's create-to-write window is microseconds wide, so only the
      10 ms poll interval landing inside it under load reproduced the failure.
      `daemon/internal/platform/containment_supervisor_linux.go` now assembles each helper-side
      witness document in a temporary file in the destination directory and renames it into
      place (`publishLinuxSupervisorWitnessFile`, `:2318`), so a reader observes either no
      document or a complete one, and the witness assertion itself is unchanged. The same
      publish covers the owner-loss proof reached/result markers (`:2505`, `:2531`), read by the
      same poller, the drop-response fired marker (`:2178`), whose e2e poller compares its first
      successful read to `fired\n`, and the release response-loss reached marker (`:3528`),
      whose content is parsed after a non-empty poll. The scan-injection markers (`:2627`,
      `:2691`, `:2708`) keep `os.WriteFile`: their readers gate on a non-empty read or observe
      after the frame that orders the write, so the zero-byte state the other readers accept is
      already filtered there. The Windows `writeContainmentSupervisorWitnessMarker`
      (`containment_supervisor_windows.go:1486`) has the same create-then-write shape but only
      ordered readers (a same-process `os.Stat` after the handoff returns, and e2e parses after
      the writing process exits), so it carries no observed or reachable race; the adjacent
      Windows `TestCloseStopsOwnedChildAfterLeaderExit` observation (archived in
      `docs/archive/CLOSED-TODO.md`) stays a different defect (an owned child survived
      `Close()`), not a witness-publication race. Still open for
      the owning change: the runner/app resume and stop-receipt barrier markers
      (`daemon/internal/execution/runner.go:1046`, `daemon/internal/app/app.go:2207`) still
      create-then-write documents whose e2e readers poll non-empty and then parse them; no
      occurrence is recorded there, and only a torn read of one small write remains possible
      once the non-empty gate is passed.
      Follow-up slice (branch `fix/barrier-publish-atomicity`, based on da02900): the two writers
      recorded as still open above are the same defect, not a weaker one.
      `waitForResumeBarrier` created the commit/resume and lease-armed reached markers with
      `os.OpenFile(O_CREATE|O_EXCL)` and wrote the four-line marker afterwards
      (`daemon/internal/execution/runner.go:1046`), and `waitForLinuxStopReceiptBarrier` created
      the stop-receipt JSON the same way (`daemon/internal/app/app.go:2207`). Their harness
      readers poll the path, accept any non-empty read and then parse it
      (`daemon/e2e/production_linux_containment_witness_test.go:804-816`, `:2701-2708`), which a
      non-empty but incomplete document still fails, and `waitLinuxV2OwnerWitnessStage`
      (`daemon/e2e/production_linux_v2_owner_cli_test.go:272-297`) turns a readable but
      undecodable stage marker into a hard error; that shape reached the owner-witness stage
      writer at `daemon/internal/app/linux_owner_witness_linux.go:1270`. All three now assemble
      the document in a temporary file in the destination directory and publish it in one step
      (`publishResumeBarrierMarker`, `publishLinuxStopReceiptBarrierMarker`,
      `linkLinuxV2NativeWitnessStageMarker`); the stage marker links the temporary file into
      place, so an existing marker still fails as an exist error for the exact-replay comparison
      and is never replaced, and the replay read keeps the no-follow posture the create had.
      Cleared by ordering rather than changed: the in-process `restoreStageMarkers` reader and
      the `internal/execution` probe/startup fixtures observe presence only, or after the
      writing process completed (`internal/execution/probe_test.go:194-201`,
      `internal/execution/runner_windows_test.go:74-106`), and the owner-witness preflight writes
      control files (`cgroup.kill`) that are never parsed. Residual: a crash between the
      temporary write and its publish leaves a dot-prefixed `.partial-*` file instead of an
      empty marker, which `ensureFresh` then reports as a non-empty barrier directory; both fail
      loudly and no occurrence is recorded.
      Delivered by PR #76 (`docs/close-linux-platform-flakes`, based on `f2a759f`): the move
      changes no code because the fix is already in `main`. The publisher this record names
      (`publishLinuxSupervisorWitnessFile`,
      `daemon/internal/platform/containment_supervisor_linux.go:2318`) is `3377a7d` (#59), the
      resume/stop-receipt barrier and stage-marker follow-up is `9c73a05` (#62); both are
      ancestors of this revision, the publisher covers every reader listed here, and the reader
      helper (`containment_supervisor_linux_test.go:1219`) and the witness assertions are
      unchanged. Both occurrences were re-read from their own CI logs: run 43724904655 attempt 1
      job 132844651104 (`actions/runs/43724904655/attempts/1/logs`, `7_daemon.txt` — the
      attempt's own log, because `gh run view --job` now serves the green rerun) carries
      `containment_supervisor_linux_test.go:1128: decode wrong-session helper witness:
      unexpected end of JSON input` with `--- FAIL: ... (0.01s)`, and run 44133750246 job
      134217597395 carries the same text with `(0.05s)`. Fresh evidence on this revision: WSL
      `Ubuntu-24.04` (`/usr/local/bin/go` -> `/usr/local/lib/go1.27.0/bin/go`, go1.27.0
      linux/amd64, `GOTOOLCHAIN=auto`; the default WSL distro's `/usr/bin/go` is go1.26.8 with
      `GOTOOLCHAIN=local` and refuses this go 1.27 module), `go test -race -count=50 -run
      ^TestLinuxSupervisorWrongHelloSessionRejectsBeforeTargetExec ./internal/platform` — `ok`,
      118.418 s, 50 runs, 0 failures, exit 0. CI on revisions containing the fix kept the
      `daemon` job green with no recurrence (runs 44140380720 `3377a7d`, 44147815717 `9c73a05`,
      44230735630 `d60d50d` and 44253118123 `9192241`; `daemon-windows` too where the trigger ran
      it). The `.partial-*` crash residual stays recorded and needs no further action: the
      publish removes its temporary file on every failure it can observe, a crash can leave only
      the dot-prefixed file, and `ensureFresh` then refuses the non-empty barrier directory
      loudly instead of reusing it — no occurrence is recorded. The archived flake-class block
      below keeps its original `docs/TODO.md` pointer for this item; the entry itself now lives
      in this archive.

*Closed part of the item “Close the `daemon/internal/platform` flake class …” in `docs/TODO.md`; the open residual stayed there until it moved here on 2026-11-19 (PR #76).*

      **Linux terminal reason — fixed at the cause; verification is CI on the new revision, not a
      local green.** `TestProcessGroupLeaderExitAfterSuccessfulDescendantScanRemainsProven` failed
      twice in the same source family with `process_unix_linux_test.go:2119: monitor terminal reason
      = owner_stop, want leader_absent`: run 43898893639 job 133409923040 (`go test -race ./...` on
      the 0007d head, green on rerun of the same job) and run 44027453497 job `daemon` (push of main
      `dacec450`), while the **same tree** was green on the `pull_request` run 44026216492 (merged
      head of #41, `cf9e4a1`). That job runs, from `daemon/`:
      `python3 ../scripts/ci/stdio-only-caller-boundary.py "$(command -v go)" test -race -count=1 -timeout 600s ./...`
      (step "Run race tests with the same stdio-only caller boundary", `.github/workflows/ci.yml`,
      after that job's `gofmt -l .` and `go vet ./...`). Cause: one first-writer-wins field had two
      writers — the stop requester wrote `owner_stop` *before* it signalled `monitorStop`, and the
      monitor wrote `leader_absent` only after a clean scan observed the leader gone — so a caller
      asking whether the leader was proven absent received whichever path arrived first. Now the
      monitor goroutine is the only writer and publishes the reason before it closes `monitorDone`
      (`process_unix.go`: `recordDescendantMonitorTerminalReason`, `stopDescendantMonitor`,
      `monitorDescendants`); the shipped assertion is unchanged.
      `TestProcessGroupStopRequestKeepsCleanLeaderAbsentTerminalReason` forces the two paths into
      that race's order and fails with the same `owner_stop` text against the unpatched revision. On
      this host's Linux (WSL, go1.27.0) the shipped test itself hit `owner_stop` in 2 of 800 `-race`
      runs before the fix and 0 of 800 after; that still only judges a gate once CI runs it on a
      pushed revision.
      **Windows owned-child — untouched.** `TestCloseStopsOwnedChildAfterLeaderExit`
      (`process_windows_test.go:1004`, "owned child <pid> survived Close()") was observed in run
      43720680428 job 132831780245 on a docs-only diff, green in run 43715830442 and on rerun job
      132834151127; this change makes no claim about it, so this entry stays open. The Linux
      supervisor helper-witness flake
      (`TestLinuxSupervisorWrongHelloSessionRejectsBeforeTargetExec`) keeps its own entry in `docs/TODO.md`.
      **Windows owned-child — fixed at the cause; verification is CI on the new revision, not a
      local green.** `TestCloseStopsOwnedChildAfterLeaderExit` failed again in run 44055523619 job
      133943809688 (push of main `ca1bdad`) with `process_windows_test.go:1004: owned child 7824
      survived Close(); tasklist output: "\"cmd.exe\",\"7824\",\"Console\",\"2\",\"4,540 K\""`
      (0.19 s), on a tree whose `daemon-windows` job passed minutes earlier in run 44054200088
      (PR #48, head `95b9fa1`); the earlier occurrence was run 43720680428 job 132831780245
      (docs-only diff), green in run 43715830442 and on rerun job 132834151127. Discriminator: the
      shipped assertion observed `tasklist /FI "PID eq <pid>"` membership, which is neither the
      owned process's creation-time identity nor its termination state — a same-PID process with a
      new creation time satisfies it, and a process already dropped from the process snapshot
      satisfies its negation. On this host (Windows 10.0.26300) the shipped test with the *old*
      assertion passed while `OpenProcess`+`WaitForSingleObject(0)` on the same PID returned `0x102`
      (WAIT_TIMEOUT): the child was still running when `Close()` returned success, so the old
      assertion could not fail for its claimed reason. Cause (product): `jobContainment.Close`
      published a stop proof built from `JobObjectBasicAccountingInformation.ActiveProcesses == 0`
      alone, and that accounting reaches zero when the Job starts terminating its members, not when
      they have stopped. `terminateJobAndProveStop` (`process_windows.go`) now samples the processes
      the Job owned through `JobObjectBasicProcessIdList`, binds each to `ProcessIdentity`
      (pid + creation FILETIME), and reports success only after every sampled process object
      signals within the existing `containmentCloseDeadline`, failing closed otherwise;
      `terminateAndObserveSupervisorJob` shares that proof, so the supervisor's recovery stop is no
      longer accounting-only. In the test, `windowsOwnedProcessRunning` binds the same identity and
      waits on the same process object. Evidence: with the identity-bound assertion and the
      unpatched product the shipped test failed `owned child 47564 survived Close(); the recorded
      process is still running (wait status 0x102)`; with the proof in place the shipped test, the
      whole `internal/platform` package on this host (`go test -count=1 ./internal/platform`,
      exit 0, 9.5 s) and the new `TestOwnedChildObservationBindsCreationTimeIdentity` pass, and that
      new test fails `recycled identity windows:<pid>:<creation+1> was accepted as the live owned
      child` when the observer ignores the creation time (PID-only matching, as shipped). Open: a
      repeat is now a hard, correctly-attributed failure — `Close() error = owned process <pid>
      (<identity>) was still running after the containment close deadline` (the product refusing an
      unproven stop) or the same text from the test's observation — and would mean a process that
      really outlived its Job rather than a listing or recycled-PID artifact. The wider contract
      (`docs/evaluations/model-routing/tasks/0008b-go-close-owned-settle.json`) still owns the
      supervisor protocol's escaped-descendant semantics; this change settles the daemon-side and
      supervisor-side stop proofs only. The same PID-only membership observation still verifies
      child liveness in `internal/execution` (`runner_windows_test.go`, its own
      `windowsProcessExists`); it produced none of today's failures and was not touched here.

- [ ] Make the Control suite's dependence on the staged portal assets explicit. Four
      `PortalControllerTest` cases ("a missing built entry returns an explicit uncacheable 503…",
      "the portal requires an operator session…", "every explicit shell deep link…", "built hashed
      assets are public and immutable…") fail when `control/priv/static/portal/index.html` is not
      staged, with `Workspace assets are unavailable. Build and deploy the frontend assets.` and a
      `File.RenameError` on the same path. Observed while completing 0007e: the 0007e full-suite run
      started before `pnpm --filter @symmetry/frontend build` staged the assets and reported
      851/856 passed with those four failures; the same suite after staging reports 855/856 (the one
      remaining failure is the environment-limited `CommandTest` above). The build output is
      gitignored, so a fresh worktree has it only after a build. Decide whether the suite should
      skip those cases with an explicit reason, stage a fixture bundle, or document the build as a
      suite prerequisite; keep the assertions.
      Closed in `fix/control-staged-assets-dependency` (2026-11-19) as the documented-prerequisite
      option, kept loud rather than skipped: the four cases are tagged `:portal_assets`, and
      `SymmetryControlWeb.PortalBundle`, called from `ConnCase` setup for every web test, fails a
      tagged case before it touches the bundle. The failure states the prerequisite, the staging
      command (`mise exec pnpm@12.17.0 -- pnpm --filter @symmetry/frontend build`), CI's equivalent
      staging (the `control` job downloads the `contracts` job's `portal-assets` artifact into
      `control/priv/static/portal/` before `mix test`) and the explicit exclusion
      (`mix test --exclude portal_assets`) instead of the raw `File.Error`/`File.RenameError` on the
      staged path; the guard also rejects an entry document that is empty, references no built
      JavaScript asset or no stylesheet, or names hashed assets that are not staged (an interrupted
      `frontend/scripts/stage-assets.mjs`, or a build older than the current one). A fixture bundle
      was rejected because these cases assert against the real built entry, and an ExUnit `skip`
      would have hidden a broken CI staging step. Assertions are unchanged and nothing is skipped
      silently: with no staging the four tagged cases failed with the guard's "staged entry
      document is missing" problem line while the file's other nine cases passed
      (`mix test test/symmetry_control_web/controllers/portal_controller_test.exs` — 9/13 passed,
      4 failed), an entry document naming absent hashed assets failed the same four with
      "references assets that are not staged", `--exclude portal_assets` reported "9 passed,
      4 excluded", and with the staged build all thirteen passed. The staged path now has one
      definition, `SymmetryControlWeb.PortalAssets.index_path/0`, read by both
      `PortalController.index/2` and the tests. The prerequisite is recorded in the Elixir row of
      `docs/design/quality.md` (which links `docs/runbooks/portal.md`) and in that runbook's local
      gate list; the limit that a complete but source-stale bundle is not detectable from the
      gitignored staging alone is stated only in the runbook, so freshness stays CI's guarantee
      (the `portal-assets` artifact is built from the same revision).

*Closed remainder of the item “Close the `daemon/internal/platform` flake class …” in `docs/TODO.md`; with the closed part above, nothing of the item stays there.*

- [ ] Close the `daemon/internal/platform` flake class: the Linux descendant-monitor terminal reason
      and the Windows owned-child close window. A rerun, a longer timeout or a relaxed assertion is
      not a fix, and neither side may be closed on the other side's evidence.
      Closed part moved to `docs/archive/CLOSED-TODO.md`.
      **Today's other occurrences in run 44055523619 (daemon job 133943809650 / daemon-windows job
      133943809688; identical tree green in run 44054200088).**
      `TestPIDFDGroupSupportProbeReleasesProcessHandle` (`process_unix_linux_test.go:60`,
      `capability probes retained pidfds: before=2 after=1`, 0.00 s) — test-design defect. The
      assertion counted every pidfd in the process before and after 16 probes, so a baseline pidfd
      no probe ever created plus an unrelated release during the loop moved the count; the failure
      text is its own discriminator (a probe that retained handles would show `after > before`).
      The test now attributes pidfds to this process through `/proc/self/fdinfo` (`Pid:`) and fails
      only on a pidfd that did not exist before the probes, with an unrelated mid-loop release part
      of the scenario. Evidence (WSL Linux, go1.27.0, the only Linux available here): with the probe
      deliberately not releasing its handle the new assertion fails `capability probe retained
      pidfd 10 for process 430 across 16 probes`; with the retired count assertion and only the
      unrelated release it failed `before=1 after=0` against the unchanged product. CI verifies this
      Linux-only test on the new revision.
      `TestFreshCodexGoalCancellationUsesNativeControlBeforeReceipt` (1.84 s),
      `TestGoalAdmissionBuildsPiProviderBridgeOnlyForVerifiedCapability` (5.16 s) and
      `TestGoalAdmissionLeavesProviderAccessNilForNativeHarness` (1.09 s) in `internal/app` failed
      as `native turn did not start` / `native Pi provider turn did not start` — environment,
      load-bound. Each is a bare 1 s/5 s wall-clock wait for an in-process fake adapter; this is the
      family 0007b recorded failing only while the host also ran the browser suite, live daemon
      fixtures and another agent's work and passing alone afterwards
      (`docs/evidence/goal-0007b-readable-work-views-2026-11-15.md`), and the same tree passed in
      run 44054200088, where that package took 120.8 s against the 600 s timeout seen here.
      Resource: hosted-runner CPU/disk for ~230 in-process daemon starts that fsync a journal each
      — what would make it deterministic is waiting on the product's own state transition instead
      of host speed, which belongs in the test, not in the product or the job setup; a longer
      budget is explicitly not the fix, so the waits are unchanged. Instead each of these three
      waits now reports the durable reason (`nativeStartFailureDetail`), which the CI artifact
      lacked because those test daemons discard their log. Only CI can judge a repeat.
      `FAIL github.com/wxxb789/symmetry/daemon/internal/app 600.043s` (`panic: test timed out after
      10m0s`, `running tests: TestLeaseExpiryDuringNativeStartBlocksOpenAndTurn (8m4s)`) —
      test-design defect, fixed. These tests built a `daemon` with no background context, so
      `rootContext` fell back to `context.Background()` and a worker whose terminal transition could
      not be persisted — here because the test's deferred `store.Close()` runs before `t.Cleanup` —
      retried forever while the cleanup joined it, killing the package at the go-test timeout with
      the test's own failure unrecorded. `stopTestDaemon` now performs the daemon's own shutdown
      ordering (cancel the background context, `stopAll`, then join the workers under the
      documented 30 s `testHangGuard`) at the eight cleanup joins in this package; no assertion
      changed. Evidence: with the pre-fix cleanup join and an expired start wait the package
      reproduced `panic: test timed out after 25s` (bounded demo) with the test's own failure
      unreported; with the fix the same scenario reports
      `--- FAIL: TestLeaseExpiryDuringNativeStartBlocksOpenAndTurn (0.21s)` and the package
      completes. The Linux supervisor helper-witness flake
      (`TestLinuxSupervisorWrongHelloSessionRejectsBeforeTargetExec`) keeps its own entry in
      `docs/TODO.md` above.
      Delivered by PR #76 (`docs/close-linux-platform-flakes`, based on `f2a759f`): the move
      changes no code because every occurrence this text records is already fixed in `main`. The
      fixes were re-read against this revision: `d29fe5a` (#49) for the identity-bound Windows stop
      proof, the pidfd attribution (`pidfdsForThisProcess` through `/proc/self/fdinfo`),
      `nativeStartFailureDetail` in the three start waits and `stopTestDaemon`'s shutdown ordering
      at the package's cleanup joins; the closed part above keeps its own record (`0220f2a` #47,
      `d29fe5a` #49, `3781c78` #56). Both job logs were re-read from their own run
      (`gh run view 44055523619 --job 133943809650 --log-failed`: `capability probes retained
      pidfds: before=2 after=1` at 0.00 s; `--job 133943809688 --log-failed`: `harness_test.go:4285:
      native turn did not start`, `harness_test.go:4507: native Pi provider turn did not start`,
      `harness_test.go:4601: native turn did not start`, `panic: test timed out after 10m0s` and
      `process_windows_test.go:1004: owned child 7824 survived Close()`), and the identical tree's
      green run 44054200088 (`95b9fa1`: `daemon` and `daemon-windows` both success) was confirmed.
      Fresh evidence on this revision: WSL `Ubuntu-24.04` (`/usr/local/bin/go` ->
      `/usr/local/lib/go1.27.0/bin/go`, go1.27.0 linux/amd64, `GOTOOLCHAIN=auto`; the default WSL
      distro's `/usr/bin/go` is go1.26.8 with `GOTOOLCHAIN=local` and refuses this go 1.27
      module), `go test -race -count=50 -run
      'TestProcessGroupLeaderExitAfterSuccessfulDescendantScanRemainsProven|TestProcessGroupStopRequestKeepsCleanLeaderAbsentTerminalReason|TestPIDFDGroupSupportProbeReleasesProcessHandle'
      ./internal/platform` — `ok`, 20.091 s, 50 runs per test, 0 failures, exit 0. CI on revisions
      containing the fixes kept the `daemon` job green with no recurrence of any signature in this
      class (runs 44147815717 `9c73a05`, 44230735630 `d60d50d`, 44253118123 `9192241`). The three
      load-bound start waits this text records as "unchanged" were subsequently converted to the
      package's documented hang guard — `time.After(testHangGuard)`, 30 s at `app_test.go:7281`,
      applied at `harness_test.go:4284`/`:4290`, `:4506`, `:4600` by `4094624` (#57) (positions at
      `4094624`; #92 (`535019d`) has since replaced the three start waits with
      `waitForNativeRunning`, `harness_test.go:4739`-`:4774`, and left the fourth position — that
      test's terminal `waitTurnEntered` wait, `4094624` `:4290` and HEAD `:4286` — in place with
      its `time.After(testHangGuard)` select) — so a further occurrence would be a
      new finding, not an open action of this item. The moved
      text's final line keeps its original `docs/TODO.md` pointer; the helper-witness item it
      names is the entry closed above in this archive.

- [ ] Revalidate and remove the unused `JsonDocument` alias in
      `control/lib/symmetry_control/goals/schemas.ex:1431`
      (`SymmetryControl.Goals.GoalExternalWait`). The 0006h focused Control run
      reported the unused alias; a scoped search of that module found only its
      alias declaration. Leave removal and verification to separate maintenance.
      Closed by branch `chore/deadcode-revalidation` (2026-11-19): `JsonDocument`
      was removed from the alias line, which now reads
      `alias SymmetryControl.Goals.{Goal, GoalEvent}`; the file's other modules
      (`GoalRevision`, `WorkOutcome`, `GoalDecision`) keep their own aliases and
      uses. Revalidated with a repository-wide `JsonDocument` token search over
      every file type including generated artifacts (`rg -uuu`: the source
      matches are only `schemas.ex` and docs, and the only BEAM artifacts that
      mention it are the definition and the three modules that use it —
      `Elixir.SymmetryControl.Goals.GoalExternalWait.beam` does not), a
      module-span search of `GoalExternalWait` (lines 1427-end of file: no token,
      and no re-export or alias in that module wraps the type), and a
      fully-qualified `Goals.JsonDocument` search.
      `MIX_ENV=test mix compile --force --warnings-as-errors` exits 0 and the goal
      schema/context suites pass under `--warnings-as-errors`. Elixir 1.20.4 no
      longer emits the unused-alias warning (probed with a minimal unused alias),
      so the searches are the primary record.

- [ ] Revalidate the unused `replay_task` binding at
      `control/test/symmetry_control/orchestration_goal_admission_test.exs:1059`
      and `task` bindings at lines `2079` and `2170`. The 0007a `mix test` run
      reported these compiler warnings (`.tmp/0007a-session/control-test.log`).
      Preserve the fixture calls and leave any binding cleanup to separate
      maintenance.
      The 0009 full Control run reports the same bindings at lines `1351`,
      `2450` and `2541` in `.tmp/0009/control-full.log`; these are current
      coordinates, not additional cleanup work.
      Closed by branch `chore/deadcode-revalidation` (2026-11-19): the three
      destructuring bindings were renamed `_replay_task` (line 1351) and `_task`
      (lines 2450, 2541), so the side-effecting `insert_goal_task/1` fixture calls
      stay and only the dead names go. Revalidated with a repository-wide
      `replay_task` token search (only the binding and docs match), searches for
      `:replay_task`/`"replay_task"` atoms and strings, a
      `String.to_atom`/`Task.Supervisor`/`start_child` search in the file, and the
      compiler itself: with the bindings restored, `mix test --warnings-as-errors`
      fails on exactly the three unused-variable warnings at 1351/2450/2541; with
      the renames the file reports 77 passed, 0 warnings.

- [ ] Revalidate the unused default `kind \\ "omp"` on private
      `register_unverified_omp_runtime/2` in
      `control/test/symmetry_control/orchestration_goal_admission_test.exs:3651`.
      The 0009 full Control run reports that the optional default is never used
      (`.tmp/0009/control-full.log`). Preserve the runtime fixture behavior and
      leave cleanup and verification to separate maintenance.
      Closed by branch `chore/deadcode-revalidation` (2026-11-19): the default was
      removed (`defp register_unverified_omp_runtime(label, kind)`); the function
      is private, both call sites (lines 109 and 128) pass `kind` explicitly, and
      an `apply(`/capture search in the file found no dynamic dispatch, so no `/1`
      call can exist. The compiler confirms: restored,
      `mix test --warnings-as-errors` fails with `default values for the optional
      arguments in the private function register_unverified_omp_runtime/2 are
      never used`; removed, the same command reports 77 passed, 0 warnings.

- [ ] Revalidate and remove the unused `Connection.statuses/0` accessor at
      `control/lib/symmetry_control/integrations/schemas.ex:92`. A scoped search for
      `Connection.statuses` and `Integrations.Connection.statuses` in `control/lib` and
      `control/test` found no caller; the changesets read the `@statuses` module attribute
      directly. Observed while fixing goal 0007d. Leave removal and verification to separate
      maintenance.
      Closed by branch `chore/deadcode-revalidation` (2026-11-19): `def statuses, do:
      @statuses` was removed; `@statuses` stays (the three changesets read it at
      lines 48/66/76) and the module's public surface keeps `providers/0`,
      `capabilities/0` and `auth_types/1`. Revalidated with a repository-wide
      `statuses` word search (no caller of the accessor anywhere; no `defdelegate`
      exists in `control/lib`; no `one_of`, OpenAPI/spec or frontend type
      generation derives from it), a check of every `Connection.` call site
      (`integrations.ex:43-49` uses `providers/0`, `capabilities/0` and
      `auth_types/1`; `portal_integrations_test.exs` uses `Connection.changeset/2`,
      Ecto queries and the portal API only), and
      `MIX_ENV=test mix compile --force --warnings-as-errors` exiting 0 — any
      remaining Elixir caller of the removed function would fail that gate.
      `integrations_test.exs` and `portal_integrations_test.exs` pass under
      `--warnings-as-errors`.

- [ ] Remove or justify the unreferenced CSS rules `.chip-failed` and `.chip-pending` in
      `frontend/src/styles.css:170-171`. A sweep of all 137 class selectors in that file against
      `frontend/src` found these two unreferenced already at `7a92ca4` (untouched by the 0007b,
      0007c and 0007d work), so they are pre-existing dead weight rather than a merge artifact.
      Revalidate before deleting and leave removal to separate maintenance.
      Closed by branch `chore/deadcode-revalidation` (2026-11-19): kept, because both rules are
      reached through constructed class names — `work-surface.tsx:330` and `detail.tsx:303` render
      `chip chip-${ci.tone}` and `runtimes.tsx:59` renders `chip chip-${row.tone}`, with `failed`
      and `pending` among the tones (`frontend/src/features/work/presentation.ts:247`, and
      `runtimes/presentation.ts` for the not-supported, next-turn and estimated capability
      presentations; the rules now sit at `styles.css:216-217`) — so the literal-selector sweep's
      "unreferenced" premise was wrong and no rule was removed.

- [ ] Investigate the intermittent browser failure `project, resource, work-item,
      filtering, ordering, and persistence` in `browser/tests/portal.spec.mjs`: after the
      detach confirmation, `#toast` still showed "Resource updated" instead of "Resource
      detached" (desktop-chromium). Observed once while implementing goal 0007d
      (`portal.spec.mjs` + `workspace-shell.spec.mjs`, 40 cases, 30 passed / 1 failed /
      9 skipped) with two Control compiles running on the same host; the same test passed
      twice alone on the same revision (18.2 s and 17.3 s, exit 0). The classic bundle
      `control/priv/portal_assets/portal.js` and the resource routes are unmodified by
      0007d. Revalidate the classic toast/refresh ordering; keep the assertion.
      Closed by branch `fix/flake-browser-classic-closures` (2026-11-19): the case no longer
      exists to revalidate. On main, the file revision introduced by `7a92ca4` (#31, 0007b)
      still carried it when `5c2e0ad` (#35, 0007c) replaced it in the same file with
      `classic resource attach, update and detach with truthful health`; the last commit to
      modify the file (`671525c`) is byte-identical at the cutover's parent `ca29a8e` and
      holds only that case plus the session-boundary and keyboard walks
      (`git show 671525c:browser/tests/portal.spec.mjs`). The 0007i cutover then deleted the
      file with the classic renderer, its bundle and the `/portal/classic` routes
      (`ca1bdad`); the only server-rendered document left is the script-free login template
      `control/lib/symmetry_control_web/portal_html/login.html.eex` with its extracted
      `control/priv/portal_assets/login.css`. No successor carries the name — `browser/tests/`
      has no `filtering, ordering, and persistence` case (exact-name search and a test-title
      token search both come back empty) — and the deleted spec's coverage is mapped in
      `browser/README.md` (resource attach/update/detach and the truthful health read ->
      `browser/tests/portal-connections.spec.mjs`; the list, board, detail and history reads ->
      `browser/tests/portal-work-views.spec.mjs`). The `#toast` surface the observation names
      went with the classic renderer: no `toast` element exists in `frontend/src`, `control/lib`
      or `control/priv`, and the live receipts are `role="status"` notices — the resource walk's
      successor asserts `Resource attached — …`, and its successful-unbind leg asserts
      `Resource detached — Delta.` — in `browser/tests/portal-connections.spec.mjs`. There is no
      assertion left to keep.

- [ ] Revalidate the load-sensitive `portal.spec.mjs` desktop cases. In the 0007d fix round's
      legacy run (two fixture servers on one host, 31 passed / 2 failed / 7 skipped), "detail
      output encodes untrusted statuses and rejects unsafe links" timed out on its project-create
      toast and "two browser contexts reject a stale project update" hit the 60 s test timeout;
      both pass alone on the same revision on a quiet host (the stale-update case `ok` in 5.0 s),
      and neither surface is touched by the fix diff. Keep the assertions; re-test on a quiet host
      before changing any budget.
      Closed by branch `fix/flake-browser-classic-closures` (2026-11-19): both retired cases are
      absent from the tree, so there is nothing left to re-measure. `detail output encodes
      untrusted statuses and rejects unsafe links` left main's `browser/tests/portal.spec.mjs` at
      `7a92ca4` (#31, 0007b), which retired the classic detail-security probe to the new-route
      successor `injected malicious detail values are rendered as text and never as markup or
      unsafe URLs` in `browser/tests/portal-work-views.spec.mjs`; `two browser contexts reject a
      stale project update` left the same file at `5c2e0ad` (#35, 0007c), whose planning
      successor is `re-syncs to the newest project after a stale update in two contexts` in
      `browser/tests/portal-work-planning.spec.mjs`. Neither name exists anywhere under
      `browser/tests/` (exact-name search), the containing file is deleted with the 0007i cutover
      (`ca1bdad`), and the successor surfaces are named in the migration record in
      `browser/README.md` (the 0007b paragraph). The expect windows this item observed were the
      cases' own; the 60 s test bound is still the global `timeout` at
      `browser/playwright.config.mjs:15` and is not a budget this closure changes. What remains
      open about host-load verdicts is the separate quiesced-host re-measure entry in
      `docs/TODO.md`, which stays open.

- [ ] Investigate the mobile-chromium classic case `project, resource, work-item, filtering,
      ordering, and persistence` in `browser/tests/portal.spec.mjs`. After saving the work-item
      edit dialog, `#toast` stayed hidden with the stale text "Work item created" for the whole
      10 s expect window, while the update itself persisted: the failure snapshot's detail drawer
      shows the new pull request URL, CI `passed` and review `required` that the test set. Observed
      while completing 0007e in the first legacy run (`portal-work-views.spec.mjs` +
      `portal.spec.mjs` + `workspace-shell.spec.mjs` on both widths, 43 passed / 4 failed /
      5 skipped) and reproduced alone on the same revision with the desktop case passing (mobile
      `x` in 17.0 s, desktop `ok` in 12.1 s); it then passed in the second run of the same suites on
      the unchanged classic files (46 passed / 1 failed / 5 skipped), so the mobile case is
      intermittent. The classic route is
      unchanged by 0007e: `control/lib`, `control/priv/portal_assets`,
      `control/lib/symmetry_control_web/portal_html` and `portal_controller.ex` have no diff
      against the base revision. Revalidate the classic toast branch that reports an update when
      the edit dialog was opened from the detail drawer; keep the assertion.
      Closed by branch `fix/flake-browser-classic-closures` (2026-11-19): the same retirement as
      the desktop entry above — main's `browser/tests/portal.spec.mjs` lost `project, resource,
      work-item, filtering, ordering, and persistence` at `5c2e0ad` (#35, 0007c), and the 0007i
      cutover deleted the file (`ca1bdad`). The classic toast branch this item asked to
      revalidate (the detail drawer's work-item update receipt) went with the classic renderer:
      the SPA renders its own `role="status"` notices from its own state, no `toast` element
      exists in `frontend/src`, `control/lib` or `control/priv`, and the live detail-surface
      successor cases assert `statusNotice(detail, "Work item updated.")` in
      `browser/tests/portal-work-planning.spec.mjs`. The mobile-chromium project's successors for
      these flows run in `browser/tests/portal-connections.spec.mjs` and
      `browser/tests/portal-work-views.spec.mjs` per `browser/README.md`; no assertion of this
      item's remains to keep.

- [ ] Investigate the environment-limited Windows command test. `CommandTest` "provider
      token commands can keep stderr diagnostics out of stdout" fails with
      `{:error, {:command_unavailable, "pwsh"}}` on hosts without a working `pwsh`
      (observed while implementing goal 0007d: the full control suite reported 828/829
      passed, 21 skipped, 1 failed on a clean dedicated test database; the current tree
      (control identical to `bbc9b6f`) reports 844/845 passed with the same single failure).
      `System.find_executable("pwsh")` returns `nil` here while `powershell.exe` resolves,
      and putting the `WindowsApps` store alias on PATH makes two cases in that file fail
      instead of one. `Command.run/3` and the test itself are untouched by 0007d.
      Revalidate the Windows shell resolution; keep the assertion.
      Closed (2026-11-19, `fix/windows-command-test`): a test-environment assumption, not a
      product defect. `Command.run/3` is a generic `System.cmd/3` wrapper and no product code
      invokes `pwsh`/`powershell` (Control's only spawn site is `Command.run/3`; the daemon's
      `exec.Command` sites spawn configured harness CLIs, never a shell by that name). On this
      host PowerShell 7.7.0 exists but only as the Microsoft Store app-execution alias
      (`pwsh -NoProfile -Command "Write-Output ok"` exits 0 from cmd/bash; the running
      process is `C:\Program Files\WindowsApps\Microsoft.PowerShell_7.7.0.0_x64__8wekyb3d8bbwe\pwsh.exe`),
      which Erlang cannot resolve: it is a 0-byte reparse point (`dir` shows 0 bytes;
      `fsutil reparsepoint query` reports tag `0x8000001b`, `IO_REPARSE_TAG_APPEXECLINK`), so
      `File.exists?/1` is false, `File.stat/1` reports `{:error, :eio}`,
      `System.find_executable("pwsh")` is nil and `System.cmd/3` on the alias path raises
      `:enoent`; `System.find_executable("powershell.exe")` resolves and runs the same scripts
      with the same stdout/stderr split.
      `shell_command/2` now resolves `windows_shell/0` at run time — a resolvable `pwsh`, else
      `powershell.exe`, else a raise naming the prerequisite (ExUnit has no runtime skip, and a
      compile-time environment skip can freeze in cached BEAM) — with all three assertions
      unchanged. Evidence (`MIX_TEST_PARTITION=24`): before,
      `mix test test/symmetry_control/integrations/command_test.exs` → `2/3 passed`, the
      provider-token case failing with `{:error, {:command_unavailable, "pwsh"}}`, exit 2;
      after, `3 passed`, exit 0 (first run and repeats), with
      `mix format --check-formatted <that file>` clean. The recorded "two cases fail instead of
      one with the WindowsApps alias on PATH" did not reproduce in the current host state (the
      alias is already on PATH and the focused file failed only its provider-token case; 20/20
      probes of the timeout case's 10 ms `Task.yield/2` observed no spawn outcome, so it still
      saw `command_timeout`), and both failure paths are unreachable now that a shell resolves.
      Two focused-run failures during the repeat sweep were host-load artifacts of the shared
      PostgreSQL (`connection not available and request was dropped from queue after 4000ms` at
      the suite's `ecto.create` step), not the case under test.
      The win32 `windows_shell/0` branch is not executed by CI — the `control` job runs on
      `ubuntu-latest` (`.github/workflows/ci.yml:96`, steps at 134-139) — so its evidence above is
      local to this Windows host.

- [ ] Revalidate the unreachable resource synchronization states. `project_resources` has a
      `project_resources_sync_status_check` constraint admitting `syncing` and `stale`, and
      `SymmetryControl.Workspaces.ReadModel` maps them to `syncing`/`attention` labels, but no
      writer sets either value (`Integrations.persist_sync/4`,
      `Integrations.record_resource_failure/3` and the connection/capability resets in
      `Integrations.update_connection/2` and `Workspaces.update_resource/2` only write
      `unknown`, `synced` and `failed`). Observed while implementing goal 0007d; keep the
      constraint and labels, and decide in a separate maintenance change whether the states
      need a writer or the vocabulary needs a migration.
      Closed (2026-11-19, `fix/connections-refusal-guidance`): the premise is stale on main —
      docs-only change, no product code touched. The writers landed on main in `9d9a641` (#42)
      after the 0007d observation: `Integrations.mark_resource_syncing/1`
      (`control/lib/symmetry_control/integrations.ex:683`) commits `sync_status: "syncing"`
      through `update_sync_marker/2` (`:701`, `:709`) before the provider call, and
      `record_resource_failure/3` (`:1651`) persists
      `sync_status: if(code == :not_found, do: "stale", else: "failed")` (`:1676`);
      `ReadModel.synchronization_health/1`
      (`control/lib/symmetry_control/workspaces/read_model.ex:575-584`) maps `stale` to
      `attention` and `syncing` to `syncing`. Both states are pinned by tests from the same
      commit: `control/test/symmetry_control/resource_sync_inflight_marker_test.exs:197,202`
      assert the committed `syncing` marker, and
      `control/test/symmetry_control/resource_sync_identity_loss_test.exs:76,131,134` assert a
      `:not_found` provider answer persists `stale` and the read model reports `attention`.
      Verification (`MIX_ENV=test`, `MIX_TEST_PARTITION=61`):
      `mix test test/symmetry_control/resource_sync_inflight_marker_test.exs test/symmetry_control/resource_sync_identity_loss_test.exs`
      → `Result: 8 passed`, rc 0. The constraint and labels stay as recorded; with real writers
      for both values, neither an added writer nor a vocabulary migration is needed.

- [ ] Revalidate the `capability_not_granted` guidance entry on the resource surface. The new
      `refusalGuidance` key in `frontend/src/features/connections/resources.tsx` is only reachable if
      the server answers that code, but a sync refused for an ungranted capability maps to
      `failure(:capability_not_granted)` → `forbidden` on the wire, so the key is currently dead.
      Either the server should serve the more specific code or the client entry should be removed;
      decide in a separate maintenance change with a test that pins the served code.
      Closed (2026-11-19, `fix/connections-refusal-guidance`, PR #96): the owner chose the
      serve-the-specific-code route. `Protocol` maps `capability_not_granted` to HTTP 403 with
      message `connection capability is not granted` (`protocol.ex:21-22`) — the same status
      class as `forbidden`, because the credential is valid but the bound connection does not
      grant the capability; `failure(:capability_not_granted)` now returns the specific code
      instead of `:forbidden` (`integrations.ex:1704-1705`). The live refusals are the
      read/sync path — `require_capability/2` in `sync_marked_resource/1`
      (`integrations.ex:726`) — and the bind/update path —
      `Workspaces.ensure_connection_capability/2` (`workspaces.ex:1000-1014`), called by
      `prepare_resource_attrs/2` for both create and update (`workspaces.ex:1049`). The
      provider-action classification entries are defensive, not live:
      `record_provider_action_failure/3` (`integrations.ex:1201`) and the `:definite` list
      in `mutation_failure_outcome/1` (`integrations.ex:626-636`) would store a pre-dispatch
      capability refusal as `failed` with the `capability_not_granted` code, like
      `:forbidden`, and `stored_failure/1` (`provider_access.ex:2367`) decodes that stored
      code — but no live flow stores it today, because `authorized_connection/3`
      (`provider_access.ex:1515-1538`) widens every execute context's capabilities before
      dispatch. Pins:
      `portal_resources_test.exs:264` (bind and sync answer 403 `capability_not_granted`),
      `portal_resources_test.exs:306` (a PATCH moving a work-tracking resource onto a
      repositories-only connection answers 403 `capability_not_granted`),
      `resource_sync_identity_loss_test.exs:113`, `integrations_test.exs:947`,
      `integrations_test.exs:976` (a pre-dispatch capability refusal classifies `:definite`),
      and the frontend test `connections.test.tsx` "renders the served capability refusal with
      the capability-specific next step" (a 403 `capability_not_granted` sync refusal renders
      "Grant the connection the capability this resource kind needs, then try again.").
      Red-first: the updated pins failed on the pre-change code with the served `forbidden`
      (`2/5 passed`, 3 failed); after the change the affected control files report `Result:
      123 passed`, rc 0 (`MIX_ENV=test`, `MIX_TEST_PARTITION=61`: `mix test
      test/symmetry_control/resource_sync_identity_loss_test.exs
      test/symmetry_control_web/controllers/portal_resources_test.exs
      test/symmetry_control/integrations_test.exs
      test/symmetry_control_web/controllers/provider_action_controller_test.exs`), with
      `mix format --check-formatted` and `mix compile --warnings-as-errors` rc 0, frontend
      typecheck/lint rc 0 and `vitest run` 413 tests passed, and `pnpm contracts:check` green
      (no schema or fixture enumerates portal error codes). Neither the Go daemon nor the
      browser specs branch on the literal `forbidden` for these paths (the daemon acts on the
      HTTP status and forwards the envelope code), so no other client changed.

- [ ] Record the first hosted docs-only sample (run id, job count, wall-clock) in
      `docs/evidence/ci-process-trim-2026-11-15.md`, whose measurements section currently
      states the hosted after-run and the docs-only sample are unmeasured. Cross-check the
      classifier's skip list against the jobs that actually ran before treating the trim as
      fully accepted.
      Recorded (2026-11-19, branch `docs/runbook-quality-drift`): the first hosted docs-only
      sample is PR #29's run `43726243703` — 12 job records (2 success, 10 skipped), 12 s
      wall-clock (`14:47:12Z`–`14:47:24Z`) on the trim revision — and the first docs-only sample
      recorded on this branch is PR #99's run `44540158144` — 10 job records (2 success, 8
      skipped), 12 s wall-clock (`14:06:18Z`–`14:06:30Z`); the trim revision's after runs
      (PR #28 run `43724904655` attempt 2 and merge push `f47b0c3` run `43726189932`, both
      12/12) are beside them in the record's measurement table, with the status,
      clause-coverage and limits sections updated (the audit-surface limit is annotated
      as true at the trim revision and resolved by #43, `724d915`). Six later PR #99
      samples on this branch carried the record itself (the count at revision
      `83f6036`), so `run-records=true` and `record-cleanliness` executed: runs
      `44540613218` (head `0d0e998`), `44540895110` (head `06502b5`), `44554983915`
      (head `ad87f9b`), `44560447996` (head `b2a1340`), `44565580363` (head `4b12e59`)
      and `44566899850` (head `83f6036`) — 10 job records each, 3 success (`changes`,
      `legacy-route-audit`, `record-cleanliness`), 7 skipped, 19 s / 20 s / 26 s / 22 s /
      15 s / 18 s wall-clock. Cross-check: the workflow's classifier
      block over each pull request's first-parent diff (PR #29: its `docs/TODO.md`-only diff
      against `f47b0c3`; PR #99's first sample: the merge ref's `HEAD^1..HEAD`, the same five `docs/**`
      paths) reports `run-heavy=false` for both, and the current classifier reports
      `run-records=false` for both diffs as well; the executed sets still match, because
      the trim revision has no `record-cleanliness` job — PR #29's ten skips are all
      `run-heavy`-gated trim jobs, while PR #99's eight are its seven `run-heavy`-gated
      jobs plus `record-cleanliness` — and `changes` and the ungated `legacy-route-audit`
      ran in both. Evidence:
      `gh run view 43726243703 --json jobs,createdAt,updatedAt`; `gh run view
      44540158144 --json jobs,createdAt,updatedAt`; `gh run view 43724904655
      --attempt 1 --json jobs`; `gh run view 43724904655 --attempt 2 --json
      jobs`; `gh run view 43726189932 --json jobs`;
      `gh api repos/wxxb789/symmetry/actions/runs?created=2026-11-15T14:40:00Z..2026-11-15T15:10:00Z`
      shows no pull_request run between the trim merge and PR #29's sample;
      `gh api repos/wxxb789/symmetry/actions/runs/44540613218/jobs?per_page=100`;
      `gh api repos/wxxb789/symmetry/actions/runs/44540895110/jobs?per_page=100`;
      `gh api repos/wxxb789/symmetry/actions/runs/44554983915/jobs?per_page=100`;
      `gh api repos/wxxb789/symmetry/actions/runs/44560447996/jobs?per_page=100`;
      `gh api repos/wxxb789/symmetry/actions/runs/44565580363/jobs?per_page=100`;
      `gh api repos/wxxb789/symmetry/actions/runs/44566899850/jobs?per_page=100`
      (heads `0d0e998`, `06502b5`, `ad87f9b`, `b2a1340`, `4b12e59`, `83f6036`; 10 check
      records each: 3 success, 7 skipped);
      `python3
      scripts/ci/model-routing-record-sanitize.py --check --root-only
      docs/evidence/ci-process-trim-2026-11-15.md` exit 0; `bash
      scripts/audit-no-legacy-routes.sh` exit 0 (1327 tracked files, 4 exclusions).

## Deferred review findings (0007i, rebased onto `ca29a8e`)

- [ ] Revalidate the two host limits the 0007i gates worked around, so a future run does not rediscover
      them: the packaged release binary cannot boot on this Windows host because the generated
      `erts-*/bin/erl` shim calls `erlexec` without the Windows `.exe` name (the browser gate therefore
      runs the production configuration through `mix phx.server`, the same release-style instance the
      0007h receipt records), and the browser suite's expiry/mobile projects need the release plus a
      fixture database that only the two release instances provide.
      Revalidated and closed (2026-11-19, `fix/windows-command-test`). First limit: the POSIX launcher
      chain is confirmed broken on Windows — `mix release` (elixir-1.20.4) copies the Windows ERTS bin
      (`erl.exe` and `erlexec.dll`, no `erlexec` executable) and unconditionally writes
      `erts-<vsn>/bin/erl` as a POSIX shim ending `exec "$BINDIR/erlexec"`
      (`Mix.Release.copy_erts/1`); the release assembled here with
      `MIX_ENV=prod mix release --overwrite` fails through that chain at the shim's line 12:
      `<…>/erts-17.0.6/bin/erlexec: No such file or directory`, exit 127 (`bash ./bin/symmetry_control
      eval …`). The release is not un-bootable on Windows, however: the default
      `include_executables_for: [:unix, :windows]` (`mix/tasks/release.ex`, assembly default) also
      generates `bin\symmetry_control.bat`, which launches through `erl.exe`, and the packaged release
      booted here through it — `bin\symmetry_control.bat eval IO.puts(:release_eval_ok)` exits 0 with
      `release_eval_ok`, and `bin\symmetry_control.bat start` served
      `GET http://[::1]:4610/healthz` → `200
      {"status":"ok","checks":{"scheduler":"ok","database":"ok","provider_access":"ok","reaper":"ok"}}`
      against a migrated scratch database (then stopped). The boot environment was the runbook's
      production set plus the serving knobs
      (`docs/runbooks/control-plane-and-daemon.md:73-87`): `DATABASE_URL`, `SECRET_KEY_BASE`,
      `PHX_HOST=127.0.0.1`, `SYMMETRY_ENROLLMENT_TOKEN`, `SYMMETRY_OPERATOR_TOKEN`,
      `SYMMETRY_CONTRACTS_DIR`, `PHX_SERVER=true`, `PORT=4610`, and `RELEASE_DISTRIBUTION=none`;
      an earlier `bin\symmetry_control.bat eval` attempt without `PHX_HOST` aborted in
      `config/runtime.exs` (`System.fetch_env!("PHX_HOST")`, line 150) and left the release tree's
      `erl_crash.dump` (deleted), so the full environment above is what a future run must supply.
      Distribution is the residual host obstacle:
      the release default is `RELEASE_DISTRIBUTION=sname`, and every node-distributed start on this host
      fails with `Protocol 'inet_tcp': register/listen error: etimedout` because epmd cannot bind
      (port 4369 is not bindable here: `:gen_tcp.listen(4369, …)` → `:eaddrinuse`, `epmd` →
      `failed to bind on ipaddr 0.0.0.0`); a serving gate instance needs no distribution, so
      `RELEASE_DISTRIBUTION=none` is the usable recipe and no repository script is affected (the sh
      launcher is used by `docker/control.Dockerfile` and `scripts/ci/linux-v2-native-acceptance.sh` on
      Linux, not on Windows). Second limit: still accurate and already explicit — the
      release+fixture-database browser cases skip on a detected condition with the missing environment
      named (`browser/tests/workspace-shell.spec.mjs:355` `SYMMETRY_SHELL_EXPIRY_URL`;
      `browser/tests/portal-goal-managed.spec.mjs:23-25` and
      `browser/tests/portal-work-planning.spec.mjs:37-39` the fixture environment;
      `browser/tests/portal-goal-attention.spec.mjs:455` `SYMMETRY_GOALS_OFF_URL`), and the mobile
      project (`browser/playwright.config.mjs` Pixel 7) drives the same release target as desktop, so
      no case passes silently without the release.

## Dead-code candidates

- [ ] Align the frontend `return_to` allowlist with the server's per-path query rules, or record
      the asymmetry deliberately. `frontend/src/app/application.ts` (`safeReturnTo`) accepts any
      query string on `/portal/attention|connections|projects/:id|work/:id` and any characters in
      the project id, while `control/lib/symmetry_control_web/portal_session.ex` validates each
      path's query against `projectId`, `connectionId` or the work-search patterns and narrows a
      project id to `[A-Za-z0-9_-]+`. The server stays the authority, so the worst case is a deep
      link falling back to `/portal` rather than an open redirect; observed while completing the
      0007d rebase union (0007b's client filter and 0007d's server rule met for the first time).
      Decide whether the client should narrow its filter or the server should accept the extra
      params, and if the client is narrowed, add a test that keeps the two lists aligned.
      Closed on branch `fix/portal-return-targets` (2026-11-19): `safeReturnTo` now mirrors the
      server's `SymmetryControlWeb.PortalSession` rules parameter for parameter — the same shell
      paths with the `[A-Za-z0-9_-]+` identifier charset, the same per-path query keys, and no
      fragment — and the shared corpus `contracts/fixtures/shell-return-targets.json` is consumed
      by both the frontend unit test (`frontend/src/app/application.test.ts`) and the ExUnit test
      (`control/test/symmetry_control_web/portal_session_test.exs`); its enumerated cases are
      pinned on both sides, so every path or query rule change must add a corpus case.

- [ ] Investigate the intermittent `-race` package timeout in `daemon/internal/app`. Observed in run
      43905338877 job 133432721943 as `panic: test timed out after 10m0s` with
      `FAIL github.com/wxxb789/symmetry/daemon/internal/app 600.061s` on the 0008a head, where the
      same package had taken 365 s earlier on the same pull request and every other package passed;
      green on rerun. This is the same loaded-runner family as the Windows `internal/workspace`
      180 s package timeout: decide whether the package needs a longer explicit budget or fewer
      load-sensitive waits, and keep the tests themselves.
      The second occurrence
      (`FAIL github.com/wxxb789/symmetry/daemon/internal/app 600.043s`, run 44055523619 job
      133943809688) is classified and fixed in the `daemon/internal/platform` flake-class entry
      above (closed part archived in `docs/archive/CLOSED-TODO.md`): a test cleanup joined a
      worker that retried an unpersistable terminal transition forever, so the package died at
      the go-test timeout instead of failing the test.
      The "longer budget or fewer load-sensitive waits" decision stays open: the three start waits
      in that run are unchanged (a longer budget is not the fix), and the recorded next step is a
      wait on the product's own state transition rather than on host speed.
      **Fixed (2026-11-19, `fix/flake-app-race-timeout`):** the "fewer load-sensitive waits" answer
      is implemented for the three start waits. They no longer select on the fixture's turn channel
      with a wall-clock fallback; they poll the daemon's own durable start publication - the
      "running" transition `markRunning` queues immediately before `StartTurn` - fail fast with
      `nativeStartFailureDetail` when the run records a failure, and keep `testHangGuard` as the
      only bound (a hung start, never host speed). The 30 s timeout messages keep naming the
      outstanding step; the second wait in the cancellation test now also reports the durable
      reason. The shared native fixture also sets the logger the other fixtures set, because a
      start-wait failure outlives the test body and a nil logger panicked the whole package binary
      while demonstrating the pre-fix budget. The historical hang itself was the cleanup-join class
      fixed in `d29fe5a` (#49), so the ten-minute package budget is unchanged and was not raised.
      Evidence on this host: with the pre-fix 1 s start budget under 12-way host load all nine runs
      of the three tests failed (`native turn did not start before the ... hang guard expired: no
      durable failure recorded (local state="claimed", pending transitions=0)`); with the guard they
      pass `go test -count=3 -cpu 1,2` (56.5 s) and the same selection passes under
      `-race -count=2 -cpu 1,2` in WSL (19.3 s). The full package completes in the ordinary pass on
      this host (`go test -count=1 -timeout 1200s ./internal/app` - `ok … 297.871s`, exit 0); a
      full-package `-race` shape was not obtainable here (Windows has no cgo for `-race`, and the
      WSL guest hangs this package in `TestClaimAdmissionInputFailsClosedBeforeWorkspaceOrProcessStart`,
      a loopback dial that never answers there and passes on Windows in this same tree), so the
      package-level race gate stays with CI.

- [ ] Revalidate the load-sensitive Go test
      `TestFinalAssistantMessageAcceptsReasoningBlocksBesideSingleTextBlock` in
      `daemon/internal/harness/omp/adapter_final_message_test.go`. It failed with
      `adapter_final_message_test.go:44: context deadline exceeded` in the 0007e `go test ./...` run
      while 20 Go packages and the full Control suite ran concurrently, and passed alone on the same
      revision (`ok … 10.609s`, exit 0). The test itself sets a 1 s `context.WithTimeout` around
      `WaitTurn`, so host load alone can trip it. Keep the assertion; consider a load-tolerant
      deadline rather than a longer test timeout.
      Revalidated and closed by branch `fix/flake-load-sensitive-go` (2026-11-19): no further change
      was needed because `8082317` (#44) already replaced the 1 s `context.WithTimeout` around
      `WaitTurn` with the package's documented `testHangGuard` (30 s, "It only turns a hung harness
      into a failure; it is never a statement about how fast a turn should complete, so it sits
      far above any real scheduling delay, race instrumentation included.") and made the failing
      wait name the outstanding step (`mustSettle(t, "WaitTurn", err)`), so elapsed time no longer
      decides the verdict and a guard expiry can no longer be reported as this test's behavioural
      failure. Re-run on this host while the other regression workers loaded it: plain
      `go test -count=20 -cpu 1,2 -run '^TestFinalAssistantMessageAcceptsReasoningBlocksBesideSingleTextBlock$'
      ./internal/harness/omp` passed (`ok … 18.389s`, exit 0) and the same selection under
      `-race -count=5 -cpu 1,2` passed in WSL (`ok … 4.485s`, exit 0). The pristine 1 s failures
      stay recorded in #44 (8/8 on the pre-fix revision under 3x oversubscription) and in the 0007e
      observation above.

- [ ] Keep `mix test` off the portal acceptance database. With `POSTGRES_DB` pointing at
      the 0007d fixture database, 10 control tests fail because committed fixture rows are
      visible to suite-level aggregates (`sync_all_resources/0`, workspace and portal
      controller reads): 819/829 passed / 10 failed there versus 828/829 passed / 1 failed
      on a clean dedicated test database. The browser harness recreates its own database;
      the suite should use the repository default (`symmetry_control_test`).
      **Closed (2026-11-19, `fix/mix-test-db-guard`):** `mix test` and the test-environment
      `ecto.create`/`ecto.migrate`/`ecto.drop`/`ecto.rollback`/`ecto.load` commands now refuse
      a browser acceptance database before any storage task can touch PostgreSQL.
      `control/lib/mix/tasks/symmetry.check_test_db.ex` is the first step of the `test` alias
      and of the five Ecto aliases in `control/mix.exs`; a step that names the overridden task
      runs the real task, so flag passing is unchanged, and the task is a no-op outside the
      test environment, so the harnesses keep recreating their own databases from the
      development environment. The guard reads the effective database from the repository
      configuration (`config/test.exs` appends `MIX_TEST_PARTITION`) and refuses whole browser
      acceptance database families — `symmetry_0007c_`, `symmetry_0007d_`, `symmetry_0007e_`,
      any suffix, because the planning harness takes its database as a `-DbName` parameter and
      a recorded run used `symmetry_0007c_shell` — while the repository default and the
      runbook's disposable `symmetry_receipt_e2e_<hex>` database stay allowed.
      Unit-tested in `control/test/mix/tasks/symmetry_check_test_db_test.exs`, including the
      `run/1` wiring against the configured Repo database.
      Verified on 2026-11-19: `MIX_TEST_PARTITION=16 POSTGRES_DB=symmetry_0007d_test mix test
      test/mix/tasks/symmetry_check_test_db_test.exs` exits 1 with the Mix error naming the
      effective `symmetry_0007d_test16`, both against a live PostgreSQL and against an
      unreachable `POSTGRES_PORT=1` — the refusal precedes any connection — and
      `POSTGRES_DB=symmetry_0007e_test` refuses the same way; `MIX_ENV=test` `mix ecto.create`/
      `mix ecto.migrate`/`mix ecto.drop` refuse for both names, and `pg_database` holds no
      `symmetry_0007*` database after those runs. With `POSTGRES_DB` unset,
      `MIX_TEST_PARTITION=16 mix test test/mix/tasks/symmetry_check_test_db_test.exs` creates
      and migrates `symmetry_control_test16` and reports `Result: 7 passed` (exit 0);
      `mix format --check-formatted` and `mix compile --warnings-as-errors` pass. In the
      development environment the guard is a no-op: with `POSTGRES_DB=symmetry_0007e_test` and
      an unreachable port the real `mix ecto.create` reaches the adapter with no refusal, and
      `POSTGRES_DB=symmetry_guardmix_probe_dev mix ecto.create --quiet` followed by
      `mix ecto.drop --quiet` both exit 0 and leave no probe database.
      Review round (same branch): `POSTGRES_DB=symmetry_0007c_shell MIX_TEST_PARTITION=16 mix test
      test/mix/tasks/symmetry_check_test_db_test.exs` and the test-environment
      `mix ecto.rollback`/`mix ecto.load` refuse the same way, naming the effective
      `symmetry_0007c_shell16`; with `MIX_ENV=test` the guard file reports `Result: 9 passed`,
      and `mix format --check-formatted` and `mix compile --warnings-as-errors` exit 0.
      **Amendment (2026-11-20, guard review round):** the guard also refuses the compose
      acceptance database `symmetry_dev` (`MIX_ENV=test POSTGRES_DB=symmetry_dev mix
      ecto.migrations` exits 1 with the refusal before any connection) and is the first step of a
      sixth Ecto alias, `ecto.migrations`; its cross-check pins every assigned database value of
      the harness scripts (`run-compose-daemon-tests.ps1` → `symmetry_dev`,
      `run-connection-fixture-tests.ps1` → `symmetry_0007e_test`, `run-planning-tests.ps1` →
      `symmetry_0007c_test`) and extracts those values in either quote style, quoted or bare
      `psql -d` included. `symmetry_dev` is refused as an exact name, not a family: the compose
      harness never partition-suffixes the database it seeds, so a suffixed variant belongs to
      no harness. With `MIX_ENV=test` the guard file reports `Result: 11 passed`.

- [ ] Investigate the load-sensitive timeout fixture
      `EvaluationTests.test_timed_out_gate_terminates_its_descendants` in
      `scripts/ci/test-model-routing-evaluation.py` (`assertFalse(..., "a gate descendant
      survived the per-gate timeout")`). Observed once during the 0008d series-closing repair,
      on the pass whose host was also running four other regression suites and the record
      re-derivations - the concurrency the model-routing runbook itself warns against. The
      fixture spawns a descendant that sleeps 1.0 s, times the gate out at 0.5 s and reads the
      assertion 2.0 s later, while the kill path is a snapshot tree kill
      (`terminate_owned_process_tree` -> `taskkill /F /T`), so a descendant created after the
      kill enumerates the tree is not killed; whether that is what happened in that pass is not
      traceable from its log. Keep the assertion: revalidate the fixture (a readiness handshake
      instead of the 1.0 s sleep, or a documented quiet-host requirement) and leave the runner's
      kill path alone unless it reproduces on a quiet host.
      **Fixed (2026-11-19, `fix/flake-timeout-fixture`):** the witness is now event-driven. The
      descendant records its start in a handshake directory outside the checkout, waits for a
      release the test writes only after the timed-out check returned, and only then writes
      `late-descendant.txt` into the checkout - so a descendant the kill did reach is dead before
      it could write during a slow kill, which is the direction the 1.0 s sleep could not
      distinguish. The gate spawns the descendant with `stdin/stdout/stderr=DEVNULL` so a survivor
      cannot hold the gate's inherited pipes and keep the runner's lingering reap open until its
      own wait cap, and the descendant writes the witness after the release or after that cap, so
      a survivor can never erase its own evidence. A cap-branch write records `late-cap` where the
      release branch records `late`, so a failure self-identifies the P2 lingering-reap class. The
      test polls repeatedly for up to the runner's own `PROCESS_TREE_REAP_SECONDS`, then reads the
      witness once, instead of sleeping 2.0 s, and the gate publishes
      its own spawn marker, so a failure reports what the fixture had established (gate spawned
      descendant / descendant started / witness text) instead of only the assertion; the failure
      message is platform-neutral (Windows `taskkill /T` and POSIX `killpg` are both snapshots).
      The 0.5 s per-gate timeout, the assertion and the runner's kill path are unchanged.
      Evidence, platform-labelled. Windows 10.0.26300 host, loaded by the peer regression workers:
      the pristine fixture failed 1 of 5 runs (an earlier loaded pass failed 1 of 3) with `a gate
      descendant survived the per-gate timeout` and no mechanism in its log, while the new fixture
      passed 6 of 6 under the same load; 20 `taskkill /F /T` tree kills under load (2.0-8.6 s
      each) never let an in-hierarchy descendant write, while the control that killed only the
      parent let the waiting descendant record 3 of 3 times; a miss simulation (a throwaway copy
      of the runner whose `terminate_owned_process_tree` drops `/T`) fails in 20 s with
      `witness='late'` and both markers true, and the unmodified fixture passes in 23 s; a
      backstop-path run on Windows (the round-2 review's: wait cap shortened 300 -> 20 s and no
      release written) fails with the cap-branch witness at 19.1 s; the full
      suite passes (`Ran 57 tests in 2017.722s`, `OK (skipped=2)`, exit 0). WSL Linux (Ubuntu-24.04,
      python3, `/mnt/q` worktree): the same test on the `killpg` path passes
      (`Ran 1 test in 12.740s`, `OK`).
      Residual: a descendant created after the kill enumerates the tree (`taskkill /T`) or after
      the process group was signalled (`killpg`) is still reported as a survivor - correctly, with
      the markers stating the ambiguity instead of the fixture tolerating it - so the fixture
      keeps the documented quiet-host requirement (the model-routing runbook forbids running this
      workflow beside a full test suite on the same host).

- [ ] Investigate the intermittent Control failure `reversed import and delivery updates
      lock WorkItems in UUID order without deadlocking` in
      `control/test/symmetry_control/integrations_test.exs:1204` (`:1161` at the failing run's
      revision; `assert String.contains?(query, "\"work_items\"")` inside
      `assert_sync_waiting_on_work_item/3`, with Postgrex connections disconnecting in the same
      log). Observed in run 43891279765
      job 133383740181 on the 0008a PR (`839/840 passed, 21 skipped`), green on rerun of the
      same job (that run then completed successfully). Revalidate whether the captured SQL can
      be asserted before the sync query is issued; keep the assertion itself and leave the fix
      to separate maintenance.

      Update (2026-11-19, branch `fix/flake-control-reversed-import`): the helper now reads the
      wait from the lock manager instead of the statement text - the failed attempt-1 log names
      the repository lookup in `pg_stat_activity.query` while the import was in a
      delivery-attributed lock wait - accepts a sample only once the import's backend holds an
      ungranted lock and the delivery backend blocks it, and asserts on the accepted sample
      that the import's ungranted lock waits on the delivery backend's transaction
      (`pg_locks.transactionid = (SELECT backend_xid FROM pg_stat_activity WHERE pid = delivery)`)
      while the delivery backend holds a granted `work_items` lock; the raw sample (query, wait
      event, all three lock facts) is printed on deadline and in the accepted-path assertion.
      The waiter-bound xid is the discriminating fact: the `work_items` relation fact alone is
      invariant (a paused delivery always holds those locks), so it cannot separate the
      hypotheses - (a) `pg_stat_activity.query` is not the waiting statement for this backend at
      sample time (prepared-statement/activity reporting under load) while the lock wait is
      real, or (b) a transient pre-sync lock wait. Not reproduced locally (three pre-fix runs
      with seed 310068 left the target test green), so the entry stays open; a recurrence now
      shows the importer's wait event, whether it waited on the delivery transaction, and
      whether the delivery backend held `work_items`.

      **Closed (2026-11-20, branch `investigate/flake-reversed-import`): hypothesis (a), proven.**
      The attempt-1 job log (job `133383740181`) reads through
      `repos/{owner}/{repo}/actions/jobs/<id>/logs` (a default `gh run view` serves the rerun;
      attempt 1 stays readable via `gh run view <run> --attempt 1 --log`): at
      `15:02:09.882 UTC` backend `[180]` is cancelled `while locking tuple (0,152) in relation
      "work_items"` in the import's ordered prelock
      (`SELECT w0."id" FROM "work_items" AS w0 WHERE ... ORDER BY w0."id" FOR UPDATE`) by the
      test's own `Task.shutdown(..., :brutal_kill)` teardown, i.e. the delivery-attributed wait
      was the prelock the assertion exists for, while the failure report at `15:02:09.897` printed
      the sample that had failed the assertion - necessarily taken before that teardown - with the
      pre-sync `matching_repository` lookup as `pg_stat_activity.query`. The fields disagree
      because `pg_stat_get_activity` copies the shared activity array into a materialized result
      when the sampling statement starts (`src/backend/utils/adt/pgstatfuncs.c:331-345` (REL_18_STABLE);
      `src/backend/utils/activity/backend_status.c:813-836`, "if not already done in this
      transaction"), so `query` is fixed at that snapshot while `pg_blocking_pids()`/`pg_locks`
      read live lock data later in the same statement; the prelock's own text is reported at
      Bind/Execute (`src/backend/tcop/postgres.c:1680`, `:2176`) before the row wait, so only a
      sampling statement whose snapshot-to-evaluation span outlasts the import's Bind-to-block
      span can pair the stale text with the live wait - what CI load widened. Hypothesis (b) is
      excluded: nothing between the lookup and the prelock issues a query, and the paused
      delivery holds no lock the import's earlier statements request
      (`control/lib/symmetry_control/integrations.ex:758-799`, `:858-888`, `:1269-1278`). No
      opposite lock order exists to fix either: both sync paths prelock WorkItem rows ascending
      by id in one statement (`:1269-1278`), and the delivery's descending update order
      (`:758-782` collection, `:971-981` writes) runs after that ascending prelock; the failure
      log records no `40P01`/deadlock, and the two Postgrex disconnects are the same
      `after`-block teardown. The deferred maintenance fix is PR #87 (`3c71aec`), exactly what
      the entry asked to leave separate; the lock-order witness tests stay (PR #87 re-predicated
      the wait sample on lock-manager facts). Reproduced outside the suite on PostgreSQL 18.6
      (probe scripts kept with the investigation): a poll in one transaction shows the import
      blocked with `query` = the `matching_repository` lookup statement that precedes the lock
      wait, so `String.contains?(query, "\"work_items\"")` is false; the lock-manager sample on
      the same wait finds the import blocked by the delivery backend, its lock ungranted, its
      wait bound to the delivery backend's transaction, and that backend holding a granted
      `work_items` lock. The local demonstration bounds the snapshot window with a transaction;
      it does not recreate the CI timing itself;
      25 varied-seed runs of the two lock-order tests (`integrations_test.exs:1204`, `:1352`) stay
      green on the committed helper. A paginated sweep as of `2026-11-20T10:24Z` of the 369
      `ci.yml` runs created 2026-11-14..2026-11-20 (the newest fetched run, created `10:22:43Z`,
      was still running; 77 runs that can hold a failed `control` job, the newest of them
      `44687273091` (created `06:21:56Z`) with `control` = success and no `reversed import` line;
      23 failed `control` job logs grepped across all their attempts) finds exactly one occurrence
      of the failure: run `43891279765` attempt 1, job `133383740181`.

## Deferred review findings (0007f)

- [ ] Revalidate the `/portal/connections?projectId=…` session return target.
      `control/lib/symmetry_control_web/portal_session.ex` accepts only `connectionId=…` for the
      connections shell path while the shell's own navigation writes `?projectId=…`, so a session
      expiry collapses that page to `/portal`. The identical gap on `/portal/runtimes?projectId=…`
      was fixed by the 0007f review round (`shell_query?("/portal/runtimes", …)`); extend the
      connections clause and its accept/reject tests in a separate change.
      Closed on branch `fix/portal-return-targets` (2026-11-19):
      `shell_query?("/portal/connections", …)` now accepts `connectionId=…` and `projectId=…` per
      parameter through the shared `all_params?/2` helper (renamed from `work_query?/2`), and both
      the module's accept/reject lists and the shared corpus cover the project-scoped connections
      target.

- [ ] Revalidate the pre-existing capability-vs-state cancel/retry policy in
      `control/lib/symmetry_control_web/portal_json.ex` (`execution_summary/1`): `can_cancel` and
      `can_retry` are derived from task state only, while adapters advertise per-operation support,
      so a runtime advertising `cancel: false` still shows a live Cancel control until the server
      refuses it. Either bind the projection to the advertised support or record the state-only
      policy as intended. Named as a limitation by the 0007f review round; out of that diff's
      scope.
      **Fixed (2026-11-19, `fix/portal-capability-bound-controls`):** the projection is bound to the
      advertised support. `execution_summary/1` joins the state permission with the adapter
      operations the attempt's runtime advertises: `can_cancel` requires `cancel` whenever a runtime
      holds the attempt (claimed and beyond — a queued or assigned cancel is applied by the control
      plane itself, so it needs no runtime), and `can_retry` requires that some runtime assignment
      could select for the task's re-admission — the work item's corrected binding, resolved by
      `Workspaces.retry_binding/2` including the `supervisory_control` requirement the attempt
      being retried imposed, and capability containment — advertise `start`, the operation of the
      fresh retried attempt. A runtime without adapter metadata, without the operation, or with a
      non-`true` value answers false, so a runtime advertising `cancel: false` now serves
      `can_cancel: false`. Cancel reads the runtime's `adapter.operations.cancel` through
      `Orchestration.adapter_operation_supported?/2`; retry asks the same question by SQL
      capability containment of the adapter `start` requirement, against the goal-less assignment
      eligibility the scheduler applies. `Workspaces.ReadModel` passes the runtimes it already
      loads in one query into each execution projection and resolves retry eligibility in one
      batched query (no per-task read, for terminal attempts only), and the command responses
      resolve the same work-item binding in `PortalJSON.execution/2`. Projection tests cover
      state-ok+supported, state-ok+unsupported, state-ok+unknown and state-not-ok for both
      controls, plus the runtime-less local transitions and the retained supervisory requirement;
      the work-detail render test covers the withheld Cancel/Retry.

- [ ] Give the focused render gate's `browser/tests/portal-goal-managed.spec.mjs` case its fixture
      environment in CI. The case writes its real Goal-managed row through
      `scripts/ci/portal-planning-fixture.exs`, so it needs `POSTGRES_*` naming the database the
      release serves plus the Elixir toolchain; `.github/workflows/ci.yml`'s render job has neither,
      so the case reports an environment-named skip there. Either add the setup-beam and
      `mix deps.get`/compile steps to that job, or run the case from a harness job that owns them.
      The local focused-gate run in the 0007f review round executes it for real.
      Closed on branch `fix/ci-render-gate-fixtures` (2026-11-19, PR #98). The control job exports
      `POSTGRES_PORT: 5432` beside its other `POSTGRES_*` variables, so the case's fixture environment
      is complete; `browser/tests/planning-fixtures.mjs` launches the fixture through the
      `SYMMETRY_FIXTURE_MIX` command (the render step sets `mix`, from the job's setup-beam PATH; the
      pinned `mise exec` chain stays the local default) and drops an inherited `PHX_SERVER` so the
      fixture's own application copy cannot bind the release-under-test port; and the control job
      compiles the fixture's development build (`mix compile`) before the render step so the case's
      180 s fixture guard covers the run, not a cold compile. Hosted evidence, run `44540137977`,
      revision `52cb4bb` (the same spec bytes as the docs revision `254f1de`, run `44542025823`;
      both predate the collection guards, so the quoted `:36:3` is that revision's position),
      control job `135617161165`: `✓ 27 [desktop-chromium] › tests/portal-goal-managed.spec.mjs:36:3 ›
      Goal-managed work › a real Goal-managed work item presents no legacy path, and the server
      refuses one (1.7s)` and the same case on mobile (`✓ 74`, 1.6s). The render step's own summary
      moved from `73 passed, 3 skipped` on the previous main revision (run `44524043823`, control job
      `135560685485`) to `93 passed (2.6m)`, `1 skipped`; the one remaining skip is the mobile-project
      concurrency case that is desktop-only by design.

## Deferred review findings (0007h)

*Closed part of the item “Harden the optional-but-consumed members of the served reads …” in `docs/TODO.md`; the open residual stays there.*

      Resolved in `fix/served-read-required-members` (2026-11-18). Every member a surface consumes that
      the projection always emits is now `required` in the schema that describes it, so a rename on the
      serving side fails the bundle gate, the Elixir served-document test and the Effect boundary instead
      of decoding into a silent fallback: `goal-read.schema.json` gained 46 required members — root
      `project_key`, `revision`, `settlement_attention`, `accepted_outcomes`, `external_waits`; revision
      `objective`, `non_goals`, `acceptance_contract`, `authority_policy`, `execution_policy`,
      `context_manifest`, `inserted_at`; work item `number`, `accepted_outcome`, `execution`, `blockers`;
      graph node `number`, `blockers`, `accepted?`; graph edge `blocker`; decision `goal_revision`,
      `question`, `options`, `resolution`; outcome `work_item_id`, `candidate_subject`, `subject_hash`,
      `evidence_ids`, `inserted_at`; accounting `settled_reservation_count`, `released_reservation_count`,
      `known_usage_cost_microusd`, `usage_cost_microusd`; external wait `reason`, `resource_id`,
      `external_ref`, `next_check_at`, `check_seq`; settlement attention `event_id`, `task_id`,
      `goal_revision`, `reason`, `proposed_next_action`, `proposed_next_action_status`; blocker `ids`,
      `details` — and `attention-read.schema.json` gained 19 — root `selected_project_id`, feed
      `next_cursor`, entry `project_id`, `attention_reasons`, `active_tasks`, `nonterminal_tasks`, blocked
      row `project_key`, `project_name`, `project_status`, `work_item_key`, `blocker`, plus the same
      `GoalBlocker` (`ids`, `details`) and `SettlementAttention` (`event_id`, `task_id`, `goal_revision`,
      `reason`, `proposed_next_action`, `proposed_next_action_status`) members, because the Effect
      generator keeps one graph per shared definition name and refuses two spellings of it. Every newly required
      member stays nullable where the projection can serve `null`; no scalar constraint moved.
      `pnpm contracts:generate` regenerated the TypeScript, Effect, Go DTOs and the daemon schema bundle
      (21 schemas, 219 fixtures, both unchanged), and `contracts/scripts/boundary.test.mjs` pins the class:
      a renamed consumed member of the served Goal or attention read is refused, a renamed unconsumed
      member still decodes.
      Still optional, by design rather than by omission: the workspace-read row's consumed members
      (`resources[].id`/`status`/`sync_status`, project `version`/`default_agent_profile`/
      `default_workspace`, the runtime rows) stay optional under `contracts/README.md`'s "keeps accepting
      older partial projections" rule, and the work-detail/chat-read `WorkItemWaitingDecision` packet stays
      optional because it is carried verbatim from the agent's `waiting_for_input` payload and its options
      are advisory display data. Unconsumed members stay optional and additive: goal-read root
      `project_name`, `updated_at`, `lifecycle`, `history`, `execution`, `planning`, `attention_reasons`;
      the goal work item's planning/assignee fields, which the work surfaces read from their own read's
      definition; and the attention read's entry `blockers`/`settlement_attention`/`updated_at` plus its
      `GoalBlocker` and `SettlementAttention` members, which no surface consumes there.

*Closed part of the item “Harden the optional-but-consumed members of the served reads …” in `docs/TODO.md`; its remaining notes stay there.*

      Closed on branch `fix/ci-render-gate-fixtures` (2026-11-19, PR #98). The original text: “the
      rollout-off browser case skips in hosted CI because it needs `SYMMETRY_GOALS_OFF_URL`”. The render
      step now starts a third release instance with `SYMMETRY_GOALS_ROLLOUT_ENABLED=false`
      (`PORT=4002 RELEASE_NODE=symmetry_goals_off`, health-probed like the other two), runs the primary
      release with `SYMMETRY_GOALS_ROLLOUT_ENABLED=true` because the attention suite's plan cases admit
      plans through `accept_plan` (admission-gated), adds `tests/portal-goal-attention.spec.mjs` to the
      render step's spec list and passes `SYMMETRY_GOALS_OFF_URL=http://127.0.0.1:4002`. The case is
      discriminating for the instance's rollout state: creation and the attention read succeed against
      a rollout-on server too, so the case also sends one contract-valid `request_plan` command to
      that instance and requires the server's 409 `goal_admission_disabled` refusal, and pointing the
      URL at a rollout-on server now fails it instead of passing it. Hosted evidence, run `44540137977`,
      revision `52cb4bb` (the same spec bytes as the docs revision `254f1de`, run `44542025823`; the run
      predates the collection guards and the discriminating probe, so the quoted `:454:3` is that
      revision's position), control job `135617161165`: all nine attention cases pass at both
      widths, including `✓ 25 [desktop-chromium] › tests/portal-goal-attention.spec.mjs:454:3 › Goal
      attention workspace › with Goal rollout off a present Goal still serves its real attention entry
      (914ms)` and `✓ 72 [mobile-chromium] › … (832ms)`; the render step reports `93 passed (2.6m)`,
      `1 skipped`.

*Closed part of the item “Harden the optional-but-consumed members of the served reads …” in `docs/TODO.md`; the `:goal_admission_disabled` residual no longer stays there.*

      Closed on branch `retro/b2-control-goals` (2026-11-20, PR #114). The original residual: “the
      retained `:goal_admission_disabled` inline branch is unreachable in production because the
      attention read is not admission-gated”. No read path ever produced that page state, so the
      branch, its `empty_attention_page/0` helper and its direct-call test are deleted; the reason
      itself keeps its producers on the write path (`goals.ex` rolls it back on `request_plan`,
      `accept_plan` and `admit_task`; `protocol.ex` maps it to a 409). The `goal_attention.unavailable`
      member stays on the wire as `null` (the frontend still decodes it) and a failed Goal read is
      answered as its mapped protocol status before the projection runs. The residual entry is
      removed from `docs/TODO.md`.

## Browser acceptance under host load

*Closed part of the item “Re-measure the pre-existing browser suites on a quiesced host …” in `docs/TODO.md`; the open residual stays there.*

      **Fixed product half** (branch `fix/channel-join-read-epoch`). The join is now a read
      epoch: the client's `channel.join()` carries an `ok` hook that performs exactly the read
      an invalidation takes — the unclassified payload's own pass (`realtime.ts:294-296`, the
      `invalidateReads` call `flush` makes for an unclassified frame at `:168`) — issued once
      per completed join. It fires on every rejoin too, because the Phoenix client resends the
      same join push (`Channel.rejoin` -> `joinPush.resend`, whose `reset` keeps the receive
      hooks), which is the same window on a reconnect: the frames published while the channel is
      errored are dropped the same way. The socket-open snapshot read (`:243-250`) stays, polling
      is not reinstated (`syncPolling` still stops on `connected`, `:189-193`), no timer is
      added, and the reconcile is unconditional because nothing on this side can decide it: the
      payload's `cursor` has no store here by design and the join reply carries no position, so
      the epoch event itself justifies the read.
      Evidence on this host, frontend vitest (jsdom), with the new cases run against the
      unpatched bytes and the delivered ones: `src/app/router.test.tsx`'s `channel-join read
      epoch` case drives the real shell and the real `/portal/work/:workItemId` surface over a
      scripted socket whose `publish` drops anything sent before the join (there is no
      subscription to address, as on the server); it opens the transport, posts the run and
      publishes its `run` invalidation inside the window, observes that no read follows (the
      pre-run document still renders, no `Queued` chip), then completes the join. Red on the
      unpatched bytes for exactly that reason (`2 failed | 53 passed (55)`, exit 1: the case
      stops at `router.test.tsx:1173`'s `findByText("Queued")` with the pre-run control bar in
      the DOM dump, and its client-side sibling in `src/services/realtime.test.ts` reports
      `expected "invalidateQueries" to be called 2 times, but got 1 times`); green on the
      delivered bytes. Sabotage: making that reconcile conditional on a received frame
      (`if (pending.length > 0) invalidateReads()`) re-breaks both cases for the same reasons
      (`TestingLibraryElementError: Unable to find an element with the text: Queued`;
      `expected "invalidateQueries" to be called 2 times, but got 1 times`, exit 1), which is
      the point: the read cannot be decided by a frame the client never received, so a
      payload-driven repair is not a repair.
      The same window was checked in the other direction and no second defect was found: while
      the client is not connected the fallback is armed (`syncPolling` stops it only for
      `closed` or `connected`, and `connected` is set in the socket-open hook alone, `:245`), so
      a frame published while the socket is down is covered by a poll tick; a transport
      reconnect always reads (`:248`); and the channel's own rejoin now reconciles through the
      same hook.
      What remains, still not converted: `portal-cutover.spec.mjs:377`'s polling-fallback case,
      whose subject is the fallback itself — its reader's every socket attempt is closed
      (`established === 0`) and it waits `FALLBACK_CADENCE_GUARD_MS` (20 s, four of the client's
      5 s ticks) on the surface. The join epoch does not touch it: a reader that never joins has
      no join to complete. What would show the conversion is an observable mark of a fallback
      pass: the client publishing the read it issued on a tick, the way it already publishes
      its connection status. The case would then wait on that mark plus the read it caused,
      dropping the elapsed bound rather than lengthening it.
      The case that exposed the window (`portal-work-views.spec.mjs:1110`, converted by #58 to
      drive the surface's own refresh control and to wait on the read it serves) is not converted
      back to waiting on the invalidation channel. A browser case does not hold either end of that
      window as a product path: the suite's routed websocket can withhold the join frames, but
      that would make the case's precondition a property of the harness's own timing again, and
      its frame counters (`pushed`, `pushedAt`) can only observe a delivery that happened, never
      the interval between the transport opening and the subscription existing. What would
      justify waiting on the channel there is a page-visible mark of the join epoch — the client
      publishing `subscribed from here`, the way it publishes connection status — after which
      the case could wait on that mark plus the read it causes. The window's deterministic proof
      stays the frontend case above, where both the drop and the join are held.
      **Both remaining cases converted** (branch `feat/visible-refresh-marks`). The product now
      publishes the one fact both cases needed, and it is state an operator reads: the invalidation
      client records every every-read pass it issues — which of its own events caused it, when it
      issued its reads and when those reads finished — and the shell states that record beside its
      connection status (`frontend/src/services/realtime.ts`: `RefreshPass`, `lastRefresh()`, and
      `invalidateReads(source)` writing the record at the same call that issues the reads;
      `frontend/src/app/shell.tsx`: `.reads-status` in the topbar with `data-connection`,
      `data-refresh-source`, `data-refresh-issued-at` and `data-refresh-settled-at`, rendering e.g.
      `Offline, polling · refreshed 14:03:12 (the polling fallback)`). The record is written where
      the work happens — the fallback's own tick, the join's `ok` hook, the socket-open snapshot,
      the tab's return, and the every-read pass an unclassified invalidation takes — so no mark can
      exist without the pass that issues the reads, and only an every-read pass is recorded: a
      change classified to specific keys refreshes those reads alone and cannot speak for the reads
      on screen as a whole. Its completion is the promise `invalidateQueries` returns, so no timer
      was added, and a pass a newer one superseded never records a completion; polling is not
      reinstated (`syncPolling` still stops on `connected`). Frontend coverage: the record's
      sources, its completion, the superseded-pass guard and the classified-change exclusion in
      `src/services/realtime.test.ts`, and the shell's `.reads-status` attributes asserted in
      `src/app/router.test.tsx`'s `channel-join read epoch` case.
      `portal-cutover.spec.mjs`'s polling-fallback case (read at `:338` on this branch) now routes
      the **reader's own** socket (`controlSocket(reader)` with every attempt closed) instead of one
      page's socket standing in for it, and decides on the shell's record plus the state that pass
      served: a fallback pass whose reads finished after the write was requested
      (`data-refresh-source="fallback"`, `settledAt >= createdAt`, both in the reader's own clock)
      and then the new state rendered — no elapsed budget anywhere in the decision.
      `FALLBACK_CADENCE_GUARD_MS` (20 s) is renamed `FALLBACK_READ_GUARD_MS`, its value unchanged
      and its comment now says it is a backstop over a missing pass, never a verdict. The old case
      routed the *writer's* page, so its `established === 0` never spoke for the reader: with the
      reader's socket live the convergence it observed was the push path rather than the fallback
      its note claimed.
      `portal-work-views.spec.mjs`'s disconnected-read case (read at `:1119`) now exercises the
      product's join epoch instead of its own refresh control. The harness holds the browser's own
      connection (`controlSocket(page)` with `hold` set: the page's WebSocket is opened as a mock
      and **no upstream connection is made**, `withheld` counts the bytes that stay with the
      harness, and `release()` connects and replays them in order — the bytes are never parsed, and
      a Phoenix 1.8 frame is a JSON array on the wire, which is why an object-shaped join matcher
      forwarded the join in the first attempt at this hold). The browser therefore is `connected`
      with the server's join clause unrun: the run posts through the API, the server pushes its
      invalidation down a socket with no subscription to address (`pushed === 0`,
      `established === 0` at that moment, `withheld > 0`), and the surface still renders the pre-run
      document — the hosted failure's own observation, now the case's deterministic precondition.
      Releasing the connection then decides on two product facts in order: the shell's record that
      the pass which refreshed the reads is `join` and issued them after the run was published
      (`issuedAt > publishedAt`), and the read that pass caused, served by the server (the detail
      read the case armed before the release). Nothing else can reconcile that surface: the
      fallback is stopped while the transport is open, the frame is gone, and the case dispatches no
      tab return and presses no refresh control. `JOIN_EPOCH_GUARD_MS` (20 s) is a new named
      backstop over the client's ten-second join timeout and its rejoin backoff; it decides nothing.
      The harness hold answers this entry's own earlier objection — that a routed websocket
      withholding join frames only re-makes the precondition timing-dependent — in the only way that
      holds: the hold is set before the shell starts and released by the case after it has observed
      the product's own states, and a hold that failed to apply turns the case red (the surface
      renders the run early), never green.
      Affected suites on the delivered bytes, the render step's own command form
      (`pnpm --filter symmetry-browser-acceptance test tests/portal-cutover.spec.mjs
      tests/portal-work-views.spec.mjs`, both projects, against a `mix phx.server` of this revision
      on a `postgres:18.6` container): the two files, both projects,
      `33 passed`, `1 skipped`, exit 0 (2.9 m), the converted cases green on both: the cutover
      fallback case (desktop 9.6 s, mobile 10.7 s) and the disconnected-read case (desktop 8.0 s,
      mobile 5.2 s). This host cannot boot the packaged release (recorded above), so the runs used
      `mix phx.server` of this revision against a `postgres:18.6` container with
      `SYMMETRY_PORTAL_URL` pointed at it and a private `SYMMETRY_PLAYWRIGHT_OUTPUT` directory —
      the shared results directory is what wedged two earlier attempts at the rehearsal, and the
      private one is what let the suite report at all.
      Gates on the delivered bytes, exit codes: `pnpm --filter @symmetry/frontend typecheck` 0,
      `lint` 0, `test` 0 (18 files, 322 tests), `build` 0.
      Sabotage evidence: first, the fallback tick was made to refresh
      nothing at all (`syncPolling`'s tick body emptied): the converted cutover case failed for its
      own stated reason on both projects (`2 failed`, exit 1: `the fallback pass completed and
      refreshed the reads after the write`, `Received: false`, `Timeout 20000ms exceeded`), while
      the pre-change version of the same case — which routed the *writer's* page and waited on the
      surface — passed the same sabotaged bytes (`2 passed`, exit 0), because its reader's live
      socket delivered the frame. Second, the join's `ok` hook was made to issue nothing
      (`onJoined` emptied): the converted disconnected-read case failed for its own stated reason on
      both projects (`2 failed`, exit 1: `the completed join's pass refreshed the reads`,
      `Received has value: null`, `Timeout 20000ms exceeded`), while the pre-change (#58) version of
      the case passed the same sabotaged bytes (`2 passed`, exit 0) through its own refresh control.
      Both sabotages were reverted and the bundle rebuilt to the delivered asset name
      (`index-BNKze5Le.js`), so the green suite verdict above stands for the restored bytes.

## Open findings from the 0008b model-routing review

*Closed part of the item “Revalidate the open class B finding against `daemon/internal/platform/process_windows.go` …” in `docs/TODO.md`; the open residual stayed there until it moved here on 2026-11-19 (PR #102).*

      **Fixed** (branch `fix/non-durable-close-proof`). `jobContainment.Close`
      now latches the failed physical stop proof into `job.closeErr` in the
      non-durable `containmentSupervisorStopLease` branch
      (`process_windows.go:554-568`; the `errors.Join(stopErr, ...)` at `:563`)
      instead of caching only the daemon-side handle release, so a repeated
      `Close` through the `!durableAuthority` retry branch (`:497-500`) reports
      the failure instead of returning nil after releasing the Job handle. A
      lease with stop/release capability but no authority keeps no receipt that
      could answer for the proof on a later call, and the proof cannot be
      re-derived once that handle is closed, so latching follows the fail-closed
      convention the sibling `!durableAuthority` branch (`:577-579`) already
      applies to the no-supervisor and non-stop-lease paths. The
      durable-authority path (`:531-537`, `:581-591`) and the partial/legacy
      paths (`:602-613`) are unchanged. A scan of every `job.closeErr`
      assignment in the file found this as its only instance of the class: the
      other sites either join `stopErr` (`:577-579`), latch a supervisor failure
      for a Job that has no handle to prove (`:526-542`), or release the handle
      only with / after a durable receipt (`:581-591`).
      Reproduced through the route this entry names — the native Go-test-binary
      `AttachProcess` route, whose supervisor is the stop/release-capable
      `noopContainmentSupervisorLease` with no durable authority — by
      `TestNonDurableStopLeaseCloseRetainsFailedStopProof`
      (`process_windows_test.go:870-946`): it asserts the no-durable-handoff
      precondition and the capability shape, forces the settlement failure
      through the `waitForEmptyJob` seam, then requires the first, second and
      third `Close()` to report it. Red on the unpatched product (`go test
      -count=1 -run '^TestNonDurableStopLeaseCloseRetainsFailedStopProof$'
      ./internal/platform` → exit 1, `repeated Close() reported success without
      the containment stop proof`, `process_windows_test.go:937`), green with
      the fix (exit 0), and the deliberately re-broken branch fails the same
      assertion at the same line. Final-revision gates: `gofmt -l .` → no
      output (exit 0), `go vet ./...` → exit 0, `go test -count=1
      ./internal/platform` → exit 0 (`ok ... 10.606s`).

- [ ] Revalidate the open class B finding against
      `daemon/internal/platform/process_windows.go`: the non-durable
      `containmentSupervisorStopLease` branch of `Close` caches only the handle
      release, so after a settlement failure a repeated `Close` can return nil
      without the proof (HEAD `:535-539`; the sibling `!durableAuthority` branch
      sits at `:506`). Observed at medium severity as `open` in both 0008b class B
      reviews (`0008b-go-native-ownership-settlement-{mixed,strong}.review-attempts.json`,
      findings F4/F2, repaired-revision coordinates `:601-605`/`:524-526` and
      `:607-612`/`:509-511`) and in the candidate policy's negative findings. The
      branch predates the 0008b revision; it is reachable through the native
      Go-test-binary `AttachProcess` route, which returns
      `noopContainmentSupervisorLease` (`containment_supervisor_windows.go:934`),
      and the production daemon's durable-authority path is not affected. Keep the
      finding visible; fix or re-scope in a separate maintenance change.
      *(Superseded by the archived closure — the finding was fixed; see
      `docs/archive/CLOSED-TODO.md`; only (a)–(c) below stay open.)*
      Closed part moved to `docs/archive/CLOSED-TODO.md`.
      **Still open, deliberately unchanged.** (a) The wider settlement contract
      for the `daemon/internal/platform` flake class above (the wider-contract note
      is archived in `docs/archive/CLOSED-TODO.md`) keeps the load-sensitive
      Windows owned-child close window; this change only removes the false-success
      reporting. (b) A first settlement failure on the
      non-durable route is terminal for that containment owner: `Close` and
      `Terminate` keep reporting the latched error and no path re-derives a stop
      it cannot demonstrate, because the Job handle a re-proof would need is
      released best-effort here (retaining it across calls is a different
      design, not this fix). (c) Two adjacent behaviours stay as recorded
      rather than altered: the daemon handle is retained for the retrying call
      when the non-durable lease close itself fails, and the retry branch
      ignores the local handle-release retry result, so a stale local-cleanup
      error is still reported after a successful release — fail-closed polarity
      that can never report success without the proof, the same convention
      `TestCloseRetriesHandleReleaseWithoutReplayingStop` pins for the sibling
      branch.
      Closed by branch `docs/close-class-b-residuals` (2026-11-19): the owner
      accepted (a)–(c), the Class B residuals on
      `daemon/internal/platform/process_windows.go`, as deliberate and exactly as
      recorded above. The main finding stays fixed — the `fix/non-durable-close-proof`
      closure above latched the failed physical stop proof into `job.closeErr`, and
      `TestNonDurableStopLeaseCloseRetainsFailedStopProof` pins the repeated
      `Close` proof; the retry convention (c) names stays pinned by
      `TestCloseRetriesHandleReleaseWithoutReplayingStop`. The wider settlement
      contract (a) asked for was not sponsored, so it remains unbuilt.

## Record and documentation hygiene

- [ ] Decide whether record cleanliness needs an enforced gate, and keep the sanitising
      tool's argv/output scope with the 0008a re-publish. `scripts/ci/model-routing-record-sanitize.py`
      exposes `--check` (write nothing; exit 1 while a host absolute path remains), but no CI
      step invokes it: CI runs only the unit suite
      (`python3 ../scripts/ci/test-model-routing-record-sanitize.py`,
      `.github/workflows/ci.yml:1138`), so published-record cleanliness rests on those unit
      tests (`--check` cases at `scripts/ci/test-model-routing-record-sanitize.py:89-98`)
      plus the manual render step documented at `docs/evaluations/model-routing/README.md:163`.
      The README already scopes the renderer to argv/output capture, which is also its safety
      boundary: it rewrites every absolute path it finds, including path fixtures quoted
      inside recorded prose — harmless for `argv`/`output` fields (the `path-sha256:<digest>`
      token is a deterministic function of the path, so the rendered record stays reproducible
      and unambiguously mapped; note `output_sha256` binds the rendered text, not the
      pre-render bytes) but corrupting for an
      explanation whose meaning depends on the quoted path. Decide whether to add a
      `--check`-style gate over the committed records; the future 0008a re-publish should
      repeat the argv/output-only scope and record any prose fixture it deliberately leaves
      rendered.

      **Closed (2026-11-19, `feat/eval-tooling-bindings`):** the gate exists.
      `scripts/ci/check-record-cleanliness.py` selects the records a revision range adds,
      modifies or retypes under `docs/evaluations/**` and `docs/evidence/**` against an
      explicit comparison base (the pull-request base commit, the pushed `before`, or the
      `origin/main` merge base when the base is empty or all-zero) and runs the sanitize
      tool's own `--check --root-only` on each selected record: the guard executes the tool
      instead of re-implementing its patterns, and the root-only scope keeps the
      argv/output-only boundary, leaving product routes and prose fixtures literal while host
      paths still refuse. `.github/workflows/ci.yml` reports `run-records` from the existing
      changed-path classifier and runs the `record-cleanliness` job whenever those paths or
      the sanitize tool change, independently of the documentation-only fast path, so a
      records-only pull request is exactly the change it checks;
      `scripts/ci/test-check-record-cleanliness.py` (sixteen tests, observed `Ran 16 tests ...
      OK`) pins the selection, the fallback and refusal cases and the tool boundary, and joins
      the model-routing suite in the daemon job. The 0008a re-publish remains its own open entry
      in `docs/TODO.md`, with the argv/output-only scope it asked for; each open item was moved
      separately, so this entry carries no residual.

*Closed part of the item “Revalidate the runbook claims that are true on this tree but rot …” in `docs/TODO.md`; the open residual stayed there until it moved here on 2026-11-19 (PR #99).*

      Third pass (the `gpt-6.2-sol` findings, each re-verified against its
      artifact). Closed by restating them against their owners rather than
      re-asserting them: the receipt recipe's cleanup now owns the generated
      database name and refuses an inherited `MIX_TEST_PARTITION` when the
      effective name is derived (`config/test.exs`), and says the guard covers
      the fixture writes only, not create/migrate/drop; the live-E2E recipe names
      `SYMMETRY_LEASE_DURATION_MS=30000` as the terminal-retry case's prerequisite
      and CI's per-case assertion; the request-hash recipe distinguishes the
      release's environment from the production Compose service, which forwards
      neither `SYMMETRY_REQUEST_HASH_WRITE_MODE` nor
      `SYMMETRY_GOALS_ROLLOUT_ENABLED`; the broker paragraph scopes stdin delivery
      to the legacy task path and says native provider access needs a verified
      adapter bridge; the OpenCode recipe labels the Linux-v2 verifier as the
      installed-owner verification path while production registers the
      unavailable adapter; `portal.md` no longer implies a Goal-side board move
      and states the native run's supervisory flags as `false` rather than
      absent; `connections.md` scope its live smoke to synchronization reads; the
      index rule cites the contract's record-the-conflict requirement; and the
      rot-prone restatements (pnpm pin, Claude guard, CI step inventory, cadence
      and poll defaults, GitHub API version, health fields) now name the file that
      owns each value (`package.json`, the smoke script, `ci.yml`,
      `control/config/runtime.exs`, `frontend/src/services/realtime.ts`,
      `integrations/auth.ex`, `workspaces/read_model.ex`). The `README.md`
      documentation index carries the one convention line for the whole `docs/`
      tree.

- [ ] Verified, no change needed: the 0007b repair receipt's source subject
      `sha256:4a28f41446d2a057e355fffcf2f4c06e8c49475867e26ce66038f66a6502256f` (393 source entries) has not
      silently drifted. Recomputing the receipt's own documented algorithm (recursive roots plus the direct
      files in `frontend/`, `browser/` and `contracts/` plus the explicit file list, every `docs/**` path
      excluded; entry `sha256 + two spaces + path + LF`, PowerShell invariant-culture sort) reproduces it
      exactly on the delivered 0007b merge revision `7a92ca4` and on the published branch commit `efe0fdf`, and
      the same script reproduces the receipt's own historical anchors (`01eb134` full-tree `sha256:1e345643…`,
      `efe0fdf` full-tree `sha256:705e559d…`). The tree recomputed at this change's revision differs
      (`sha256:fdf75910…`, 466 entries; 83 moved + 75 added + 2 removed scope paths contributed by later goals),
      which no `docs/**`-only edit can cause and which no receipt claim asserts as current. The command the
      receipt names (`.tmp/0007b-subject-digest.ps1`) was a throwaway, so a delivered tool for that scope would
      need its own increment if a future submission must re-derive it without re-implementing the documented
      algorithm.
      Corroboration recorded without action: an independent cancelled review parsed the browser HTML
      report's embedded results and confirmed the focused 35/1/0 and broad 79/15/0 rows with no flaky cases -
      evidence for the 0007b receipts' browser rows, not a defect.

- [ ] Re-publish the pre-existing personal absolute paths in the committed 0008a
      evaluation artifacts through `scripts/ci/model-routing-record-sanitize.py`
      instead of rewriting history:
      `docs/evaluations/model-routing/0008a-controls.json` (14 disposable-root
      strings), `docs/evaluations/model-routing/0008a-audit.json` (3), and
      `docs/evidence/goal-0008a-auditable-acceptance-workflow-2026-11-15.md` line
      58 (`EVIDENCE_ROOT='C:/Users/...'`). The 0008b change renders its own new
      records with that helper and records the rendering in the record
      (`record_rendering` in `0008b-controls.json`); audit finding P3-3. Keep each
      record's quoted digest consistent when the older records are re-published.
      Closed 2026-11-19 on branch `chore/eval-record-scrub`: re-published through
      `scripts/ci/model-routing-record-sanitize.py` (`sha256:b8b252f3…`, the tool's bytes on
      2026-11-19; #79 (`21cf326`) has since changed it to `sha256:52f7c182…`) as the
      entry directs — `0008a-controls.json` 32 substitutions (the 14
      disposable-root strings -> `<disposable-root>`, plus 9 toolchain
      interpreter argv entries (one rendered to `path-sha256:4d144641…`) and 9
      operator-worktree argv entries -> the
      deterministic `path-sha256:` tokens), `0008a-audit.json` 5 (3
      disposable-root values + 2 toolchain entries), and the evidence record's
      line 58's `EVIDENCE_ROOT` value -> `EVIDENCE_ROOT='<disposable-root>'`.
      No host path remains and no meaningful prose fixture needed leaving
      unrendered in these three files, so the tool's `--check` exits 0 on all
      three. Quoted-digest consistency was kept: the evidence record's quoted
      audit digest was rebound (`dc878099…` -> `832117d0…`), and one
      rendered-output digest needed a second-generation rebind
      (`0008a-controls.json`'s `missing-gate` `stderr_sha256`, `c12881ae…` ->
      `8503bfa2…`), re-deriving that file's `sha256`. No document holds a
      full-length occurrence of a changed digest outside the README exception
      record (checked across `docs/`, `scripts/`, `contracts/` and `.github/`);
      the truncated `…` prefixes that remain are in that record and in these
      closures. The full table is in
      `docs/evaluations/model-routing/README.md`, "Record rendering exception
      (2026-11-19)".

- [ ] Revalidate the stale 0008-series prerequisite text:
      `docs/goals/0008d-typescript-model-routing.md:13` says "the accepted 0008b
      and 0008c results" while the 0008b candidate policy is produced and awaiting
      the owner's verdict. Pre-existing and not introduced by the 0008b evidence
      change; correct the wording once the 0008b verdict lands. (The `0006h`
      index-row dead link above is the same class of stale documentation and is
      already tracked.)

      **Closed (2026-11-19, `docs/0008-prerequisite-text`):** the frozen-bytes
      convention resolves this item instead of a wording edit: no archived goal
      document has been edited in place after placement
      (`git log --diff-filter=M --name-only -- docs/goals/archive/` prints nothing).
      Placement-time content changes occur during archive moves and are not
      relevant to the frozen-bytes argument. The index's archive notes record the
      archived documents' content as unchanged
      (`docs/goals/README.md:47` 0008a, `:61` 0008d, `:100` 0008b, `:313` the 0007
      series), and owner verdicts are recorded separately as they arrive (`:309`);
      `73e083f` moved this goal document into `archive/` byte-identically. The
      correction therefore lands live: the closure re-points the item to
      `docs/goals/archive/0008d-typescript-model-routing.md:13` — the item text
      stays verbatim — and a new recording note in `docs/goals/README.md` names
      the archived sentence and the verdicts that govern it — the owner accepted
      0008b on 2026-11-18 (`8a689bd`, row `:348`), while 0008c is produced and
      awaiting the owner's verdict with no acceptance recorded (`:349`;
      `docs/evidence/goal-0008c-elixir-model-routing-2026-11-17.md:3`). The 0008b
      half of the archived sentence became true when the verdict landed; the 0008c
      half stays as written, which is the convention's accepted state rather than
      an open action — no 0008c acceptance is declared, and a future verdict is
      recorded in the index row like the others. Check run on this branch:
      `rg -n 'accepted 0008b|0008c'
      docs/goals/archive/0008d-typescript-model-routing.md docs/goals/README.md`
      (the archived line is unchanged; `docs/goals/README.md:117-123` is the new
      note). The cross-referenced `0006h` note stays in `docs/TODO.md` as a
      decision note, untouched here; its index-row link was already repointed
      to the archived document by `c514bd9` (#66).

- [ ] Revalidate the runbook claims that are true on this tree but rot without any
      edit to `docs/runbooks/**` (found while independently resolving every
      checkable runbook claim on `632b369`). A CI job split or rename invalidates
      `daemon-process-stop.md`'s "the daemon job runs `go vet`, a scoped ordinary
      Go pass and the full `-race` suite ... and the `daemon-windows` job runs the
      full Windows suite"; a harness pin move invalidates
      `daemon-opencode-peer-ownership.md`'s "OpenCode 2.3.0 is explicitly
      unsupported" and `control-plane-and-daemon.md`'s "the script-local version
      guard is `-ExpectedClaudeVersion 2.1.281`, which is not the daemon's
      promoted Claude pin" (both follow `docs/design/harness.md`); a pnpm bump
      invalidates every `mise exec pnpm@12.17.0 -- …` command (they follow
      `package.json`'s `packageManager`); a change to the `development-*` fallbacks
      invalidates the sign-in and live-check examples (`control/config/runtime.exs`
      `:63-64`); a rename of the workspace read's health keys invalidates
      `portal.md`'s four signal names (`health/4` in
      `control/lib/symmetry_control/workspaces/read_model.ex:471`); and
      `session-receipt-recovery-e2e.md`'s `mix ecto.drop --force` uses the Ecto
      option meaning "do not ask for confirmation", not `--force-drop`, so an
      Ecto-side change to that option would change what the cleanup line does. The
      runbooks also cite *Commands and gates* in `docs/design/quality.md` as the
      checks CI actually runs while that section still carries the stale frontend
      rows tracked under *Deferred review findings (0007i)* below.

      Closed in the second pass (the review findings of `gpt-6.1-sol` on `#70`,
      each re-verified against its artifact): `connections.md` no longer asserts
      the `vso.code`/`vso.work`/`vso.build` scope mapping it could not support —
      the shipped code requests an Entra resource token
      (`control/lib/symmetry_control/integrations/auth.ex:41-46`) and names no
      scope — and instead states the read-versus-write requirement the code does
      establish (`change.upsert`/`change.update` create or update pull requests in
      `integrations/providers/{github,azure_devops}.ex`), with the scope and
      permission names explicitly left to the provider. Still rot-prone after that
      pass, with no artifact that can re-derive them: the runbooks index's
      precedence rule (it now mirrors `AGENTS.md:24`'s record-the-conflict
      requirement instead of "the code wins", so a change to that contract
      bullet invalidates it), `portal.md`'s board-move sentence (the keyboard
      bindings follow `frontend/src/features/projects/work-surface.tsx:115` and
      the Goal-managed refusal follows `ensure_legacy_work_item/1`), `portal.md`'s
      `playwright-report/` location (follows `browser/playwright.config.mjs` and
      `browser/README.md`), `portal.md`'s CSRF sentence (follows Plug's
      `@unprotected_methods ~w(HEAD GET OPTIONS)`), and
      `chat-and-supervisory-control.md`'s supervisory/intent scoping (follows
      `control_capabilities/4` in `control/lib/symmetry_control/orchestration.ex`
      and the daemon's autonomy envelope in `daemon/internal/app/app.go`).

      *(Partly superseded by the archived third-pass closure, which restated several of these
      against their owners; see `docs/archive/CLOSED-TODO.md`. The revalidatable list below stays
      open.)*
      Closed part moved to `docs/archive/CLOSED-TODO.md`.

      Still revalidatable, with no artifact that can pre-empt a future edit: the
      generated-name regex and the `MIX_TEST_PARTITION` behaviour (Ecto/Ecto
      test-config side), the 45-second skip bound in
      `daemon/e2e/core_test.go`, the list of variables the production Compose
      service forwards, the Pi/native provider-bridge gate's exact shape, the
      installed-owner vocabulary for the Linux-v2 verifier, and the docs-index
      convention paragraph itself (it restates the precedence the design and
      protocol documents already carry).
      Revalidated in full (2026-11-19, branch `docs/runbook-quality-drift`): every claim named
      below was rechecked against its owner, and the claim | owner | verdict | change table with each
      owner's current file:line is in the PR body — the CI inventory sentence against
      `.github/workflows/ci.yml:1171-1345` (`daemon`: gofmt, `go vet`, the scoped ordinary
      pass and the full `-race` pass) and `:1357-1473` (`daemon-windows`: `go test ./...`
      plus its named witnesses); OpenCode 2.3.0 against `docs/design/harness.md:15`,
      `daemon/internal/harness/registry.go:154,326-329` and the mechanical version guard
      `daemon/internal/harness/opencode/adapter.go:283-284` (TestedVersion `:28`); the
      Claude smoke guard against
      `scripts/claude-code-local-transport-smoke.ps1:14` (default `2.1.281`) versus the
      promoted `harness.md:11` blob; the pnpm commands against `package.json:5`; the
      `development-*` examples against `control/config/runtime.exs:63-64`; `portal.md`'s four
      health signals against `control/lib/symmetry_control/workspaces/read_model.ex:504-515`
      (the item's `:471` was stale); `mix ecto.drop --force` against Ecto 3.14.3's
      `mix ecto.drop` option docs (`control/mix.lock`; `--force` = "do not ask for
      confirmation", distinct from `--force-drop`); the generated-name regex and
      `MIX_TEST_PARTITION` against
      `control/config/test.exs:14-15` and
      `control/test/symmetry_control_web/controllers/goal_machine_controller_test.exs:1365`;
      Compose forwarding against `compose.production.yaml:28-36` with the `legacy`/`false`
      defaults in `control/config/runtime.exs:27,86`; the 45-second skip bound against
      `daemon/e2e/core_test.go:196,242-245` and CI's `SYMMETRY_LEASE_DURATION_MS=30000` with
      the named JSON assertion (`.github/workflows/ci.yml:1503,1578-1599`); the Pi
      provider-bridge gate against `daemon/internal/harness/pi/provider_extension.go:459-467`
      and `probe.go:242`; the installed-owner vocabulary against
      `docs/design/quality.md:80-85` (OpenCode side: `registry.go:154`,
      `harness.md:82,120`); and the docs-index
      convention paragraph (`README.md:325-330`) against `AGENTS.md:30-31` and
      `:64-67`. `portal.md`'s `playwright-report/` location and its CSRF
      sentence were rechecked the same way and still match their owners
      (`browser/playwright.config.mjs:6-7,20` for the result and report directories; Plug 1.20.3's
      `@unprotected_methods ~w(HEAD GET OPTIONS)` for the read exemption, `control/mix.lock`).
      Both edited runbooks now name their owner file or package
      (`daemon/e2e/core_test.go` in `control-plane-and-daemon.md`; `daemon/internal/harness/pi`
      in `connections.md`). Evidence: the owner reads above plus the docs-only hosted CI run on
      this branch; no suite beyond that is required for a docs-only diff.

- [ ] Decide whether the 0008b receipts need re-production under the current shared runner. They bind
      `scripts/ci/model-routing-evaluation.py` as produced —
      `sha256:9a9ff8eb65bda21e584c5fef9642f86569b46c8c8da16d3e216443e702425c56` — while `main`'s #42
      extended that runner for the Elixir carrier
      (`sha256:8ae5b2b6ba961924f7702aa4181e8488913b4d63171922bf1cf760e62142db50`), so the current bytes
      refuse the 0008b records for submission at `$.evidence.tooling_sources.model-routing-evaluation.py`,
      exactly as they refuse the committed 0008c receipts (see the model-routing entry above). The
      producing bytes stay reachable from the annotated tag `0008b-produced-tooling` (commit `253a701`,
      the head this branch was rebased from, tagged and pushed so the reference survives the rebase), and
      the post-rebase audit recomputation reports exactly those four
      tooling-digest mismatches and nothing else (`checks: 99, failures: 4`). Re-execute the runs if a
      future submission requires receipts produced by the current bytes; never re-derive by editing a
      receipt.

      **Closed (2026-11-19, `feat/eval-tooling-bindings`):** the same question as the other two
      re-production entries, answered by the same `docs/design/quality.md` paragraph: the 0008b
      receipts stay verifiable against the revision tagged `0008b-produced-tooling` and fail closed
      on submission under the current runner by design; a submission that requires current bytes
      re-executes the runs.

## Deferred review findings (0007i, rebased onto `ca29a8e`)

- [ ] Investigate the intermittent Control failure
      `control recovery selects pending actions after more than one page of terminal no-op Goals` in
      `control/test/symmetry_control/goals_test.exs:4753`. It is recorded from the pre-rebase delivery
      revision `7235708` (`.tmp/gates/control-test-final.log`, 2026-11-17 11:16,
      `884/886 passed, 21 skipped`, exit 2) with `** (EXIT from #PID<...>)` /
      `** (DBConnection.ConnectionError) could not checkout the connection owned by #PID<...>
      ({:sql_sandbox_owner, ...}) ... request was dropped from queue after 111ms`, raised inside
      `Ecto.Repo.Preloader`'s supervised `Task`; the same suite's earlier run (09:44) reported
      `885/886 passed` with the `pwsh` failure alone, so the case is intermittently load-sensitive:
      it admits 101 Goals and cancels each one before asserting
      `Goals.recover_pending_goal_control_actions/1`, and the sandbox owner's connection is preempted
      while the preload tasks queue. This is not the `integrations_test.exs:1161` flake recorded above
      (different test and call site). Revalidate whether the preload can run without a second supervised
      task or the sandbox queue can be given the case's real budget; keep the assertion and the 100-item
      recovery page it checks.
      Closed (2026-11-19, branch `fix/flake-control-recovery-page`, commit `3a87d58`): the Goal
      projection read preloaded its five associations through `Ecto.Repo.Preloader`'s parallel
      tasks, and under the SQL sandbox every task queued for the test process's single owned
      connection, where DBConnection drops a checkout that waits past `2 * queue_target` (100ms)
      when its poll runs - the recorded `dropped from queue after 111ms`. `Goals.ReadModel.load_data/2`
      now preloads with `in_parallel: false`, so the read keeps every query on the caller's
      connection and nothing queues; no queue or ownership timeout was changed, and the
      regression test asserts the mechanism directly: `a Goal projection read keeps every query
      on the calling process` (`control/test/symmetry_control/goals_test.exs`) was red on the
      unfixed bytes (the caller plus five preloader pids) and is green with the fix. Evidence:
      `mix test test/symmetry_control/goals_test.exs:4777` red -> green, `mix test
      test/symmetry_control/goals_test.exs:4753` 2x green, `mix test
      test/symmetry_control/goals_read_model_test.exs` 50 passed; the 100-item recovery-page
      assertion is unchanged.
- [ ] Investigate the intermittent mobile failure `a failed workspace re-read after a write surfaces the
      failure instead of the stale rows` in `browser/tests/portal-connections.spec.mjs`. Recorded from the
      pre-rebase delivery revision `7235708`: mobile failed after its 30 s expect while desktop passed in
      6.9 s in the same run, and both projects passed in the 12:10 run on the pre-repair bundle. The
      retained trace shows the injected `route.abort("internetdisconnected")` never fired (no
      `/portal/api/workspace` request or failure after the route was installed), and the click's
      `refresh()` refetched `["connections"]` but not `["workspace"]` because of the page's guard
      `if (!workspace.isFetching) void workspace.refetch()` (`frontend/src/features/connections/connections.tsx:351-352`;
      the same guard also sits in `onRefreshProject` at `:501`). `workspace.isFetching` is never read
      during render, so React Query's automatic change tracking does not watch it and a render taken while
      a read was in flight leaves the guard snapshot at `true` until something else re-renders the page —
      a state-snapshot race, not a cutover change. Decide whether the guard should read the query's live
      `fetchStatus` at click time (`queryClient.getQueryState(key)?.fetchStatus`) or whether a
      user-initiated refresh may deliberately supersede an in-flight read; keep the case's assertions and
      the injected-failure design.
      Closed by branch `fix/flake-browser-refresh-guard` (2026-11-19): the decision is that a
      user-initiated refresh supersedes an in-flight read, so the guards are removed rather than made
      live. The header refresh now calls `query.refetch()` and `workspace.refetch()` unconditionally,
      and `onRefreshProject` (the failed-read "Try again" retry in both error states, and the re-read
      the three resource dialogs issue when their write is refused) issues `workspace.refetch()`
      without an `isFetching` test; the successful post-write re-read is the resources section's own
      `reconcile()` -> `queryClient.invalidateQueries({ queryKey: ["workspace"] })`, untouched here.
      React Query's manual refetch is what makes the click's read happen: with data present it cancels
      the read it supersedes and starts a new one, and it joins an in-flight initial read instead of
      duplicating it (query-core `Query.fetch`'s `cancelRefetch` branch), so the case's injected abort
      is now decided by the click rather than by a fetch state this page cannot see. The retained trace
      does not discriminate between a stale snapshot and a genuinely in-flight read; the fix is
      deterministic under both, because the click always issues its read. The live-`fetchStatus`
      alternative was rejected: it keeps the silent no-op for a genuinely in-flight read and leaves the
      click depending on a fetch started before the route was installed, which is the race the recorded
      trace shows. Regression tests in `frontend/src/features/connections/connections.test.tsx`: "issues
      a new workspace read when a refresh is clicked while a re-read is in flight" (the header Refresh)
      and "re-reads the project when Try again is clicked while a re-read is in flight" (the stale
      error's retry) each hold a workspace re-read in flight across a page re-render and assert the
      click records its own `/portal/api/workspace` request. On the guarded bytes both fail for the
      recorded reason — no request after the click (`Expected: 3 / Received: 2` for the header
      refresh; Vitest's inline `expected 5 to be 6` for the retry, whose count includes the refused
      read's retries); on the fixed bytes the file passes (53 passed, `--no-file-parallelism`) with
      `tsc --noEmit` and `eslint . --max-warnings 0` exit 0. The browser case keeps its assertions
      and injected-failure design unchanged. Scope: only this page gated a read whose fetch state it
      never rendered; the other refresh guards (`projects`, `runtimes`, `work/detail`, `goals/detail`,
      `chat`) decide on the `isFetching` of the query they pass to `RefreshButton fetching=`, so their
      field is tracked and their handler snapshots stay fresh, and they are left unchanged. Host
      note: this host runs ~14 parallel workers, so local browser verdicts are indicative only; CI
      holds the verdict.

- [ ] Revalidate the pre-existing doc drift in `docs/design/quality.md`: line 41 still says the frontend
      commands "do not exist until goal 0007 builds `frontend/`" and line 116 lists them as a goal-0007
      target, while `.github/workflows/ci.yml:67-70` has run them since 0007a. Observed while recording
      the 0007i gate table; update the quality policy in a separate documentation change.
      Fixed (2026-11-19, branch `docs/runbook-quality-drift`): the *Commands and gates* TS row no
      longer frames the frontend commands as a goal-0007 target or asks for a browser-runner pnpm
      migration; it now states the delivered reality — commands owned by `frontend.md`, run by CI in
      the `contracts` job (`.github/workflows/ci.yml:107-110`; the item's `:67-70` was stale), with
      the browser runner already pnpm
      (`.github/workflows/ci.yml:618`). Line 41 was already corrected on main by `f2a759f`; this
      change corrects :116, and `rg -n 'goal 0007 target|do not exist until goal 0007'
      docs/design/quality.md` is empty on the committed bytes.

## Paginated reads must be bound to the cursor that served their pages

- [ ] The work detail's execution history retained the older pages a reader had expanded without
      binding them to the served first-page cursor, so a read that re-served a different boundary
      rendered those pages beside the new first page: `WorkControlSurface` kept them as
      `{ workItemId, entries }` (`frontend/src/features/work/control.tsx`) and `WorkHistory` rendered
      `[...firstPage, ...retained]` (`frontend/src/features/work/history.tsx`), which presents the
      region between the two as contiguous history. A timeline cursor is signed afresh on every GET
      (`Plug.Crypto.encode/2` embeds `signed_at_ms` in the signed term), so the token cannot witness
      that two reads paginate from the same boundary; the entry the cursor's position names can, and
      it is the last entry of the page the read served (`Orchestration.page_with_next/3` takes
      `List.last(page) |> entry_position()`). The attention feed had the same shape: its loaded
      cursor list outlived the served first-page `next_cursor` the list chains from
      (`frontend/src/features/attention/attention.tsx`), so a re-read of the feed served older pages
      of a replaced cursor beside its new first page.

      **Fixed (2026-11-18, `fix/history-cache-cursor-reset`):** `timelineBoundary`
      (`frontend/src/features/work/presentation.ts`) derives that boundary from the entry's own
      `source` and `<source>_id` and returns null when the entry does not carry the id its source
      owns, so an unprovable boundary never binds pages; `WorkControlSurface` retains the pages under
      the boundary the read that fetched them served — or, when an empty first page proves no
      boundary, under the served cursor's own identity — and returns them only while the served read
      is still on that chain, and `WorkHistory` and `WorkReceipts` render that same bounded state;
      `AttentionPage` renders its loaded pages only while the served `next_cursor` equals the cursor
      its chain was expanded from. No timer, poll or automatic read was added: the pages a moved
      boundary retires are loaded again by the reader's own "Load older history" / "Load more Goal
      attention", which continues from the served boundary.

- [ ] Class scan of the other paginated reads (2026-11-18). The Goal receipt list is bound by
      construction: its pages are keyed by their own `after` sequence numbers
      (`frontend/src/features/goals/detail.tsx`, `ReadModel.events/2`), which are append-only and
      re-read under their own keys, so a page is never served under a boundary it was not fetched
      from. The chat conversation binds its pages by the boundary message's id (`chatHistoryView`'s
      gap in `frontend/src/features/chat/presentation.ts`, rendered by
      `frontend/src/features/chat/chat.tsx`) and therefore still differs from this fix: it keeps the
      pages a moved boundary left behind and states the unloaded region, while the work history and
      the attention feed retire them. Unifying the two changes one surface's reader-visible
      behaviour, so it needs its own decision. Retiring a page is reader-visible too (an expanded
      page disappears when a live run moves the boundary), and it is covered by the frontend suite
      only: the browser suites, which CI runs, carry the live-daemon pagination case and were not
      run on this host.

      **Closed (2026-11-19, `fix/paginated-reads-cursor-binding`):** the scan enumerated every
      paginated read with two search strategies — the server-side pagination parameters, the
      `page_with_next` reads and the generated contract DTOs that carry
      `next_before`/`next_after`/`next_cursor`, and every client consumer that accumulates pages
      (`useInfiniteQuery`, `useQueries`, the retained page state) — and decided each: the work
      detail's execution history (`/portal/api/work-items/:id/timeline`) and the Goal attention feed
      (`/portal/api/attention`) are bound by #68; the Goal receipt list
      (`/portal/api/goals/:id/events`) is bound by construction, its pages keyed by their own
      `after` sequence over an append-only log and re-read under those keys; the chat conversation
      (`/portal/api/chat`) binds its pages by the boundary message's id, and this branch fixed the
      two ways that binding could still lie — the gap proof now requires every retained page to be
      reached from the served boundary, so a later page that did reach the boundary no longer
      excuses a chain a moved boundary left behind, and the note names the attached chain's
      boundary and the newest unreached page, naming each further region as loads reach it; and the
      reader's own "Load older" now reads through the cache under the cursor and boundary captured
      at press time, so a re-serve that re-keys the surface mid-load neither cancels the press nor
      binds its page to the boundary that arrived afterwards. The operator API's paged reads
      (`/api/v1/tasks/:task_id/{timeline,events,transitions,commands}`,
      `/api/v1/goals/:goal_id/events`, `/api/v1/attention`) have no retaining consumer — bounded
      cursor-page drains in `scripts/ci/omo-control-native-evidence.py` and
      `daemon/e2e/core_test.go`, and the browser suites' single reads or cursor drains (e.g.
      `portal-chat.spec.mjs`) for verification — as is the providers' outbound pagination
      (`control/lib/symmetry_control/integrations/providers/`), one-shot aggregation. The residual
      in `docs/TODO.md` is the owner's unification decision and the CI browser runs.
      *(Historical pointer: the unification decision closed on 2026-11-19 — see the entry below —
      leaving the CI browser runs as the residual.)*

*Closed part of the item “Residuals of the completed class scan (2026-11-19)” in `docs/TODO.md`; the open residual stays there; moved 2026-11-19 from branch `fix/chat-history-boundary-id`.*

      (a) the owner's decision on unifying the chat conversation's keep-and-state treatment with
      the work history's and the attention feed's retirement — an expanded page the chat keeps
      renders beside a stated unloaded region, while the other two retire it, and unifying changes
      one surface's reader-visible behaviour. Pre-existing, left as-is: `retainServedPage` detects
      a moved boundary by comparing cursor token bytes, so a re-serve that only re-signs the same
      boundary position still restarts the chain pointer; deciding whether it should compare the
      boundary message's id instead belongs to (a).
      **Decided and fixed (2026-11-19, `fix/chat-history-boundary-id`):** the owner keeps the chat
      conversation's keep-and-state treatment — it does not unify with the work history's and the
      attention feed's retirement — and the pre-existing note is fixed: `retainServedPage`
      (`frontend/src/features/chat/scopes.ts`) now judges the boundary by the served page's oldest
      message, the message the served `next_before` is signed from — the chat read signs the cursor
      afresh from the descending page's last (oldest) message, served reversed as `messages[0]`
      (`last = List.last(page)` in `SymmetryControl.Chat.conversation/1`), and that message's id
      reaches the client without a wire change as `ChatMessage.id` in the generated
      `SymmetryChatReadV1` — so a read that only re-signs the same boundary position keeps the
      chain pointer where the reader's loads reached it — the offered token renews the reader's own
      cursor while it still designates that boundary — while a served page that ends at a different
      message still restarts it from its own boundary; a refused cursor — the server's 24-hour
      `max_age` — restarts the chain pointer from the served page and re-reads that scope's
      captured query, so the next press renews that page's token and the re-walk dedupes against
      the retained pages, and a refusal a later load or a renewed cursor superseded carries only
      its notice: the press's captured boundary fences both the restart and the re-read (fix round
      2, 2026-11-20). A kept limit of that recovery: the token a reset exposes can itself be past
      the server's 24-hour `max_age`, and the button is re-advertised as soon as the refused press
      settles, so a press inside the re-read window can cost one more refused notice — it
      self-heals when the renewal lands, and recovery is not otherwise tracked. A token offered
      while the chain is ended is dropped — the never-continued shape a since-removed `adopt`
      branch had covered is dropped with it: the branch was inert against the page shape the
      shipped read emits (a signed continuation means the served page is the newest 50, signed from
      its oldest message), and the gap the drop leaves is recorded as open work in `docs/TODO.md`.
      Both fallback predicates stated exactly: the served comparison decides by the boundary
      message's id while both sides name one and by the cursor's bytes while either does not, while
      a fetched page binds by its own captured boundary id and compares token bytes only while that
      id is absent.
      Evidence: `frontend/src/features/chat/chat.test.tsx`'s "keeps the chain a
      refresh re-signs at the same boundary and restarts it at a moved one" was red on the previous
      bytes (`expected [ 'cursor-5', 'cursor-5-resigned' ] to deeply equal [ 'cursor-5',
      'cursor-3' ]`), and the cursor renewal is red-first pinned beside it: "renews the served
      cursor at an unchanged boundary only while the reader has not paged past it" was red on the
      fix's first bytes (`expected [ 'cursor-5' ] to deeply equal [ 'cursor-5-resigned' ]`) — its
      extended bytes still red exactly there, with a second press consuming the renewed
      `cursor-5-resigned-2` while the first is held. Both pass on the delivered bytes, and the
      fix-round tests are red-first pinned against the pre-fix bytes in the same way: "restarts the
      chain from the served page when a cursor the reader has paged past is refused" was red on the
      pre-fix `chat.tsx` bytes (`Unable to find an element with the text: message 12`), and "leaves
      the chain alone when the refused press no longer owns it" was red on the round-1 `chat.tsx`
      bytes (`expected 3 to be 2` — the superseded refusal must not re-read the scope). The
      delivered bytes also pin the moved-boundary restart, the renewal of an in-flight press's
      chain and its consumption by a second press, the renewal stopping at a cursor the reader has
      paged past, the refusal-triggered restart from the served page while the press still owns
      the chain boundary it captured, the superseded refusal staying a notice, and the named
      skipped region; the chat suite passes (66 tests, `--no-file-parallelism`), with `tsc --noEmit`
      and `eslint src/features/chat --max-warnings 0` exit 0 (full frontend eslint rc 0 as well).
      The open residuals stay in `docs/TODO.md`: the CI browser suites' live-daemon pagination
      cases and the served token an ended chain drops.

      **Annotation (2026-11-20, branch `docs/retro-followups`):** the `adopt` branch was inert against the
      page shape the shipped read emits only while rows commit in timestamp order. A signed
      continuation does mean the served page is the newest 50, signed from its oldest message, but it
      does not exclude a row older than the served page committing afterwards: `inserted_at` is set at
      insert inside the transaction and the advisory lock is per action, not per scope, so a slow
      concurrent post can land below a page already read, the next read at the same boundary then
      offers a token, and the ended chain drops it — the never-continued shape the branch had covered.
      The gap is tracked as the open TODO entry "An ended chat chain never accepts a later served
      token" (`docs/TODO.md`); this closure is kept as recorded.

## Deferred review findings (0007g)

*Both items of this section moved here on 2026-11-19; `docs/TODO.md` keeps no residual under it.*

- [ ] Give the work detail an entry point into a run's conversation. The classic workspace drawer
      offered "Open Chat" and "Review decision in Chat" for a work item whose execution carries a
      `run_id`; the migrated `/portal/chat` is reachable from the shell's Chat entry (whose run
      picker lists the served runs) but the React work detail added by 0007f has no such link, so a
      waiting decision is only reachable by picking the run in Chat. Adding the link is work-detail
      surface work (0007f's file) and was outside the 0007g diff; `browser/tests/portal.spec.mjs`
      keeps the classic drawer's own buttons until the 0007i cutover.

      **Closed (2026-11-19, `feat/work-detail-chat-link`):** the React work detail
      (`frontend/src/features/work/detail.tsx`) now renders "Open in Chat" beside the execution
      whenever the served read binds that execution to a `run_id`, linking the canonical
      `/portal/chat?scope=run&projectId=…&runId=…`; the label becomes "Review decision in Chat"
      while the served waiting context carries a decision record — the extraction result or the
      payload's `decision`, read exactly as the conversation's own decision intent reads it, the
      same members the classic drawer's decision button checked — and a waiting request without
      one is answered on the detail itself.
      Pure navigation: no server member was added or changed. Pinned by
      `frontend/src/features/work/detail.test.tsx` (exact href, both labels, and no link without a
      run). The item's closing clause about `browser/tests/portal.spec.mjs` was already stale when
      written: no file under `browser/tests/` referenced `Open Chat`, `Review decision`,
      `open-chat-button` or `decision-chat-button` at `ca29a8e`, and the file was deleted by the
      0007i cutover (`ca1bdad`), so nothing kept the classic buttons either way.

- [ ] Redirect the documented `#chat` hash location to `/portal/chat`. The frontend design fixes
      "old `/portal#chat` … redirect once to the new equivalent URL", and
      `frontend/src/app/legacy.ts` still maps the fragment to `/portal/classic#chat` (the classic
      surface's own in-page entry points write that hash). The one-time hash mapping belongs to the
      0007i cutover, which retires the classic writable UI; changing it earlier would split the
      classic page's own navigation from its hash.

      **Closed (2026-11-19, `feat/work-detail-chat-link`):** already implemented at `origin/main`;
      the premise was stale at the 0007i cutover. `legacy.ts` maps `#chat` — with the classic
      `project_id`, `run_id` and `chat_scope` members — to
      `/portal/chat?scope=…&projectId=…&runId=…`, pinned by `frontend/src/app/legacy.test.ts` and
      run once at bootstrap by `frontend/src/main.tsx` (only for `/portal`), with
      `control/lib/symmetry_control_web/portal_session.ex` keeping `/portal` as the carrier of the
      retired search parameters through login. The route is browser-accepted:
      `browser/tests/portal-cutover.spec.mjs` ("every documented legacy hash location lands on its
      new route exactly once") proves `#chat` → `/portal/chat?scope=project&projectId=…` with a
      single document load and exactly one history write, and `/portal/classic` and
      `/assets/portal.js` answer 404 on this revision. No producer of `/portal/classic#chat`
      remains: the classic `portal.js` was deleted in `ca1bdad`
      (`control/priv/portal_assets/` retains only `login.css`), and every other current reference is
      a migration comment, a 404 or `return_to` assertion, or prose.

## Control instances sharing one worktree's build

- [ ] Two Control instances started from the same worktree share `control/_build`, so a foreign
      session's dev server can answer this worktree's browser runs with `# unhandled` error pages
      (observed 2026-11-17 as console-500 assertions and render-wait timeouts during 0007g's
      suite-context repetitions, with zero offset-assertion failures in the same runs). Give each
      instance its own `MIX_BUILD_PATH` when several run at once; the runs that need isolation
      should do it explicitly rather than relying on nobody else being active.

      **Fixed (2026-11-19, `fix/control-instance-build-isolation`):** both harnesses that start a
      Control instance from a worktree now pin that instance's own build path:
      `browser/scripts/run-planning-tests.ps1` and
      `browser/scripts/run-connection-fixture-tests.ps1` export
      `MIX_BUILD_PATH=<worktree>/control/_build/instances/<harness>-<port>` for every mix command
      they run and set it again on the server (planning) or fixture (connections) command they
      start, so instances from one worktree that both serve compile into different build paths:
      each harness refuses a port that already answers, and a concurrent start on one port fails
      closed at bind/readiness. Deps sources stay shared under `control/deps`; only compiled output
      moves. An inherited `MIX_BUILD_PATH` is overridden and reported, never trusted, because a
      stale one from the surrounding shell is the same accident.
      `browser/scripts/control-instance-isolation.test.mjs`
      (`pnpm --filter symmetry-browser-acceptance test:control-isolation`) drives each real harness
      with a recording `mise` stub and asserts that every mix invocation it makes - the database
      tasks and the instance start alike - received the harness's own instance build path, that two
      ports get two paths, and that a foreign inherited value decides nothing; `browser/README.md`
      records the convention, including the path an operator-started Control server needs when
      another instance may run from the same worktree. The planning harness was run end to end on
      this branch (desktop `-g restores`, `1 passed`) against
      `control/_build/instances/planning-<port>`, with its database tasks, server start, readiness,
      login and staged-asset checks all served from that path (`mix` was reached through this
      host's `mixwrap` toolchain because its `mise` cannot install the pinned Elixir); the
      connection-fixture harness is covered by the stub test only on this host, whose fixed fixture
      database port 55444 has no container here.

## Retro review follow-ups

- [ ] Owner decision: should the served `can_start` exclude goal-managed work items? `portal_json.ex`
      serves `can_start` from five conjuncts with no `goal_managed?` term
      (`control/lib/symmetry_control_web/portal_json.ex:257-260`, beside `goal_managed:` at `:255`),
      while the legacy run endpoint refuses every goal-managed item: `POST
      /portal/api/work-items/:work_item_id/run` (`control/lib/symmetry_control_web/router.ex:86`) →
      `Workspaces.launch_work_item/2` (`control/lib/symmetry_control/workspaces.ex:308`, guard at `:317`)
      → `ensure_legacy_work_item/1` (`:865-869`) → 403 `goal_authority_required`
      (`control/lib/symmetry_control_web/protocol.ex:72-73`). A goal-managed item in the shape plan
      admission creates — backlog, agent assignee, no orchestration task (`goals.ex:6699-6714`) — is
      therefore served `can_start: true` once it is ready in an active project with available external
      work, and the work surface renders "Start run" from
      it (`frontend/src/features/work/control.tsx:306`), which the endpoint then refuses. The same
      payload already fences one flag of the family (`can_retry` delegates to
      `Orchestration.retryable_attempt?/1`, which matches `goal_id: nil` only,
      `control/lib/symmetry_control/orchestration.ex:647-650`), while `can_cancel` is unfenced
      (`portal_json.ex:399`) even though the legacy cancel path applies the same guard
      (`workspaces.ex:345-349`). Decide once whether the served `can_*` flags gain the goal fence or
      whether they deliberately describe the legacy path only and the client gates on `goal_managed`,
      then pin the decision with a serving test.

      **Decided and closed (2026-11-20, owner; branch `feat/can-start-goal-fence`):** option A — the
      server fences the flag family; no backward compatibility (the first release is not shipped).
      Served `can_start` and `can_cancel` are false for every Goal-managed work item, resolved from
      the same `WorkItem.goal_managed?/1` predicate `Workspaces.ensure_legacy_work_item/1` refuses
      with (`control/lib/symmetry_control_web/portal_json.ex`: `work_item/2` reuses the value it
      already serves as `goal_managed`, and `legacy_capabilities/3` applies it to the execution
      projection the command-response `execution/2` shares); the run/cancel refusals, the 403 code,
      `can_retry` and the client (no Goal gating added) are unchanged. Pinned by
      `control/test/symmetry_control_web/controllers/portal_workflow_test.exs`: "a Goal-managed item
      serves no start or cancel control, agreeing with the endpoints' refusal" — the plan-admission
      shape and a queued Goal attempt serve `false` beside the endpoints' 403
      `goal_authority_required`, while the same-shaped legacy item keeps `can_start: true` and its
      queued attempt keeps `can_cancel: true` — beside the existing case's `can_retry: false` pin.
      The published descriptions now state the fence
      (`contracts/v1/workspace-read.schema.json`, `contracts/v1/work-detail-read.schema.json`,
      `contracts/v1/chat-read.schema.json`), with the generated bindings refreshed through the
      repository generator.

## Retro review follow-ups (second pass, 2026-11-20)

- [ ] Re-point legacy work-surface owner bookmarks that carry the pre-#111 sentinel. The `owner`
      param is read at `frontend/src/features/projects/filters.ts:50` and normalised at `:80-87`,
      and the row comparison sits at `:94` (`ownerKey(item.assignee) === filters.owner`); #111
      (`db31838`) changed the encoder (`frontend/src/features/work/presentation.ts:99-106`): an
      agent row with no recorded name used to mint `agent:unrecorded` and now mints `agent:`
      (`ownerKeyValue` `:94-97` escapes a recorded name that equals the legacy word or starts with
      `:`, so a row genuinely named `unrecorded` mints `agent::unrecorded`), and `human:` keys
      moved the same way; `human:unrecorded` likewise matches no row. A bookmark or hand-typed
      link carrying `?owner=agent:unrecorded` matches no row, while `ownerKeyLabel` (`:110-126`)
      still reads that key as the row's own absence label: the Work owner select
      (`frontend/src/features/projects/work-surface.tsx:141,170`) shows "Agent (not in this
      project)" above an empty list, even when the project has an unnamed-agent row; the label
      side is already pinned, and
      its own case documents the intent the filter does not keep — "labels the legacy sentinel keys
      old bookmarks carry as the absence they named" (`frontend/src/features/work/presentation.test.ts:81-89`,
      whose comment reads "a URL that still carries it reads as the absence it named rather than as
      a recorded name") — while the filter case covers only `unassigned`
      (`frontend/src/features/projects/filters.test.ts:52-59`, the owner case at `:56`), never the
      legacy value.
      Reproduced at `978abba` against the repository's own modules (a throwaway Node ESM probe
      that imports `filters.ts` and `presentation.ts` through a type-stripping resolve hook; no
      test file added and no dependency install): `ownerKey` printed `"agent:"`,
      `"agent::unrecorded"` and `"agent:builder"` for those three assignees (rows built as
      `{key, title, status: "ready", blocked: false, assignee}` with assignees
      `{type: "agent", name: null, agent_profile: null}`,
      `{type: "agent", name: "unrecorded", agent_profile: null}`,
      `{type: "agent", name: "builder", agent_profile: "builder"}`),
      `filterWorkItems(rows, filtersOf({owner: "agent:unrecorded"})).length` was `0` of `3`,
      `ownerKeyLabel("agent:unrecorded")` printed `"Agent"`, and the currently minted key `agent:`
      matched `1` of `3`. Fix direction: read a filter key through one normalising reader so the
      legacy sentinel word maps to the empty-value sentinel the encoder now mints (or redirect the
      legacy value at the surface), and pin the compatibility case in
      `frontend/src/features/projects/filters.test.ts`. Suggested owner area: portal
      work-surface/frontend.

      **Fixed (2026-11-20, branch `fix/legacy-owner-bookmarks`):** the entry's first-named
      direction, one normalising reader. `ownerKeyFilter`
      (`frontend/src/features/work/presentation.ts:127-139`) maps the legacy sentinel word to the
      empty-value sentinel the encoder mints now (`agent:unrecorded` → `agent:`,
      `human:unrecorded` → `human:`) and returns every other key as written, so the escaped
      `agent::unrecorded` #111 added stays a recorded name and no legacy spelling collapses onto
      it; the reader is a fixed point on every key it returns. Both filter-key reads go through
      it: `workSearch`'s `owner` param (`frontend/src/features/projects/filters.ts:50-51`), the
      reader the router's `validateSearch`, every toolbar `setSearch`, `readSavedViews` and the
      work detail's `listState` share — so a bookmark that still carries the pre-#111 spelling
      reaches the surface, the toolbar's absent-owner check and `sameWorkSearch` as the minted
      key — and `filtersOf`'s filter-state build (`:85-87`), so a search that never passed the
      URL parser re-points as well. The visible effect on the entry's second symptom: the select
      now matches the minted key against `ownerOptions`, renders the row's own "Agent" option,
      and labels the absent fallback from the minted key instead of duplicating the legacy one.
      No surface, label, encoder, storage-schema or contract change; the decision and the
      edge-case table are written out in `docs/plans/2026-11-20-legacy-owner-bookmarks-plan.md`.
      Pinned by `frontend/src/features/projects/filters.test.ts`, case "re-points the legacy
      sentinel bookmarks to the keys the encoder mints now": the legacy URL key maps to the
      minted key, both reader call sites select the unnamed-agent and unnamed-human rows, a
      legacy saved view compares as the same search as the repaired URL, and `agent::unrecorded`
      still selects the row whose recorded name is `unrecorded`. Recorded runs: the case failed
      on the pre-fix bytes (`1 failed | 7 passed` in the file) and passes after (`8 passed`); the
      entry's throwaway Node ESM probe over the repository modules printed
      `filterWorkItems(rows, filtersOf({owner:"agent:unrecorded"})).length = 0 of 3` and
      `filterWorkItems(rows, filtersOf(workSearch({owner:"agent:unrecorded"}))).length = 0 of 3`
      before the fix, `1 of 3` for both shapes after, with `ownerKey` keys
      `["agent:","agent::unrecorded","agent:builder"]` and `ownerKeyLabel("agent:unrecorded")`
      unchanged at "Agent". Gates: `pnpm install --frozen-lockfile` exit 0; frontend typecheck 0,
      lint 0, vitest 494 passed (20 files), build 0. Render gate (`docs/design/quality.md`
      amendment, `SYMMETRY_RENDER_GATE_REQUIRED=1`, the focused release-shell gate's seven spec
      files at both widths against a `MIX_ENV=prod` release of this tree, three instances on
      4000/4001/4002 and a fresh migrated database): three full runs reported `88 passed /
      5 failed / 1 skipped`, `89 passed / 4 failed / 1 skipped` and `92 passed / 1 failed /
      1 skipped`, with an isolation pass of the failing cases at `4 passed / 2 failed`. The
      failure set is unstable and every failure is environmental — a stale `SYMMETRY_FIXTURE_MIX`
      export in the first run, fixture `mix run` database-pool drops under the shared server's
      load, a 60 s test timeout on a fixture case, a Playwright trace-write `ENOENT` in the shared
      temp output directory, and one receipt race — and every failing case passed in at least one
      other run on the same bytes; the owner-filter end-to-end case
      (`browser/tests/portal-work-views.spec.mjs:117`, list/board/filters over real served rows)
      passed at both widths in every run. The gate's environment, its carrier deviations and the
      failure classification are in `docs/plans/2026-11-20-legacy-owner-bookmarks-plan.md`; CI
      remains the merge gate.
