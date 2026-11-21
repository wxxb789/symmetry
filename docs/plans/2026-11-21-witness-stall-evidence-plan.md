# witness-stall-evidence plan — export the restarted daemon's sanitized log tails and record the containment stall

Contract: `goal-witness-stall-evidence.md` (GDD, 2026-11-21). Repo coordinates below resolve at this
worktree's HEAD `a7afdf4` (`a7afdf452df1dae94eefc7ed366773e6da56c94a`); the CI coordinates resolve the
run/job named where they appear. Tooling-only increment: two files change
(`scripts/ci/export-linux-v2-redacted-evidence.py`, `scripts/ci/test-linux-v2-evidence.py`), one file
gains an open entry (`docs/TODO.md`), plus this plan. No Go, workflow, contract or schema change.

## Deliverable shape

1. **Exporter** (`scripts/ci/export-linux-v2-redacted-evidence.py`, 932 lines): in `legacy` mode, when
   the CI-supplied run verdict argument is `failed`, additionally export each witness case's
   restarted-daemon log files — `<witness-root>/<case>/second-daemon.stdout.log` and
   `.../second-daemon.stderr.log`, written into the case root by every restarted-daemon start:
   `startLinuxResumeBarrierDaemonWitness`
   (`daemon/e2e/production_linux_containment_witness_test.go:1723-1754`, the lease-arm/replay
   barrier), `startProductionDaemonWitness`
   (`daemon/e2e/production_daemon_crash_witness_test.go:386-394`, the bind/prepare/clean-seal
   restarts) and `startLinuxReplayDaemonWitness`
   (`daemon/e2e/production_linux_containment_replay_witness_test.go:754-762`, the response-lost and
   helper-crash replays) — i.e. beside the `witness-evidence.json` the exporter already reads
   (`:848`) — as bounded, sanitized JSON
   documents `<case>-second-daemon-stdout.json` / `<case>-second-daemon-stderr.json` in the same
   private output directory. The upload step already globs `*.json` from that directory
   (`.github/workflows/ci.yml:1838-1845`), so no workflow change is needed.
   Each exported document is `{"version":1,"case":<case>,"daemon":"second","stream":"stdout"|"stderr",
   "truncated":<bool>,"dropped_lines":<int>,"messages":[<record>,…]}`; every record is projected to an
   allowlist (`time`, `level`, `msg`, `state`, `stage`, `reason`, `outcome`, `kind`, `generation`,
   `bytes`, `attempt`, `runtime_epoch`, `skew_ms`, `lease_safety_margin_ms`) with a per-field shape
   and literal-value gate (see "Record projection"); every other field (notably `error`, `run_id`,
   `command_id`, `runtime_id`, `machine_id`, any path-bearing key) is dropped before publication.
   Manifest `files` entries for these documents carry
   `"kind":"daemon_log_tail"`, `"daemon":"second"`, `"stream"`, `"records"`, `"dropped_lines"` and
   `"truncated"`; the existing `raw_logs_included:false` stays untouched and true, because only the
   projection is published, never log bytes.
2. **Exporter tests** (`scripts/ci/test-linux-v2-evidence.py`): new cases covering the projection and
   its redaction (host paths, configured credential values, unstructured lines), the failure-only
   trigger, the tail bound with its partial-line handling, and the source-file safety gates.
3. **`docs/TODO.md`: one new open entry** recording the observed stall as required by contract clause 1:
   run `44794227912` job `136491073605`, the exact error lines, the classification evidence, the suspect
   path (`daemon/internal/app/cleanup_glue.go:118-143` → `observeContainmentStop` → `waitForLinuxStopReceiptBarrier`
   at `:203`), the rule that the next occurrence must be diagnosed from the newly exported logs, and the
   explicit instruction not to raise the 45 s bound. It stays open: the stall itself is out of scope.
4. **Not in this increment**: any change to `daemon/e2e` (the witness already writes both log files),
   any workflow change, any timeout change, any contract/protocol schema, any fix of the stall.

## Exact edge-case semantics

- **Trigger is the run verdict, not a per-case verdict.** The exporter receives the CI's own verdict on
  the witness matrix (`evidence_status` in `.github/workflows/ci.yml:1813-1826`), and that is the only
  failure signal it can see: the real failing run's own artifact shows why — the failing case's document
  (`lease-arm-before-resume.json` in artifact `production-linux-containment-witness-44794227912-1`)
  stops at stage `daemon_crashed_after_lease_arm_with_durable_mirror_seal_before_resume` with 5 journal
  and 9 process snapshots, and the manifest still reports `"complete": true`,
  `"incomplete_evidence_files": []`. The exporter reads files, not the go-test event stream, so "the
  failing case's logs" is implemented as "every case's restarted-daemon log tails, on a run the CI
  judged failed". The decider is the written log, not a live daemon: a case exports a tail exactly
  when `<case>/second-daemon.{stdout,stderr}.log` exists when the exporter runs. In the legacy case
  set seven cases reach a restarted-daemon start — `bind-before-commit`, `prepare-before-bind`,
  `daemon-after-resume-clean-seal-boundary`, the failing `lease-arm-before-resume`,
  `stop-response-lost`, `release-response-lost` and `helper-dead-mirror`; `commit-before-resume`
  and `daemon-after-resume` return before the restart (production witness `:1000-1012` and the
  unsealed-mirror branch), so they publish nothing.
  Passed runs publish no log documents (the tail of a green run is noise, and the contract scopes
  the export to failing cases).
- **Bounded tail.** At most 65536 bytes are read from the end of each log file
  (`LEGACY_DAEMON_LOG_TAIL_BYTES`); when the file is larger, `"truncated":true`. When the tail starts
  mid-line, the partial first line is discarded and is **not** counted as dropped (it is a truncation
  artifact, not an unreadable line). A file with no newline in its tail yields zero messages and
  `"truncated":true`.
- **Record projection.** A line is exported as a record only if it is strictly parsed JSON (duplicate
  keys rejected) whose `time` matches RFC3339-with-fraction
  (`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]{1,9})?(Z|[+-][0-9]{2}:[0-9]{2})$`,
  ASCII digits only)
  and whose `level` is one of `DEBUG|INFO|WARN|ERROR` — the shapes the daemon's own
  `slog.NewJSONHandler` writer (`daemon/internal/app/app.go:310`) produces on stdout
  (`daemon/cmd/symmetry-daemon/main.go:89`). `msg` is kept only when it is one of the message
  literals of a `daemon.log.<Level>("...")` call site under `daemon/internal/app`; `state`,
  `stage`, `reason`, `outcome`, `kind` are kept only when the value is one of the literals their
  daemon producers write: terminal states (`completed|failed|cancelled`), the `queueFailure` stages
  plus `process_exit`, the `stopRecoveredJournal`/`terminateForLease` lease and ownership reasons
  (spaces included) plus the `protocol.ReconcileDecisionKind` decisions, the acknowledgement
  outcomes, and the `newIDWithRetry` kinds (`terminal transition`, `transition`, `acknowledgement`,
  `control_failure_event`). A text value outside its field's set is dropped while the record keeps
  its time+level (and its other surviving fields), so made-up or credential-shaped values never
  publish. The test suite scans the log and producer call sites and fails when a daemon literal is
  missing from its field's set; a value reaching a field through a variable is covered by the
  maintenance rule documented beside the sets. `generation`, `bytes`, `attempt`, `runtime_epoch`, `skew_ms`,
  `lease_safety_margin_ms` are kept only as non-boolean integers in `±2^31`. Everything else is
  dropped, including `error`, `run_id`, `command_id`, `runtime_id`, `machine_id` and all path-bearing
  keys: the exporter already treats IDs, paths and log text as intentionally private
  (`project_pid_reuse`'s drop list, `project_journal`'s `run_id`/`path` skip), and `error` values are
  free text that the repository cannot bound. A line that is not a JSON object, or that lacks a safe
  `time`/`level`, is not exported at all and increments `dropped_lines`; this is how a Go panic or a
  `main.go` stderr sentence surfaces — as a count, never as text.
- **Redaction is not a substring filter.** The published bytes are re-validated by the exporter's own
  `validate_document` (`:249`), i.e. the same strict-JSON parse, forbidden-key-name scan and configured
  credential-value scan every other published document passes. In the CI legacy invocation
  (`.github/workflows/ci.yml:1815-1825`) no `--secret-values-file` is passed, so that credential scan
  is vacuous there: the leak barrier is the literal-value enumeration itself, not a configured
  credential check. Every credential-shaped value outside the daemon's own literals is dropped before
  publication; the one remaining channel is a credential that is byte-for-byte equal to a daemon
  literal, and when the export is given such a value through `--secret-values-file` it fails closed
  (non-zero exit, no manifest) exactly as it does for the other documents, instead of publishing and
  redacting around it. Host paths need no masking because no emitted field can carry one: no daemon
  literal contains `/`, and `/home/runner/...` is not in any value set.
- **Source safety.** Log files are read through the same boundary as every other allowlisted source:
  `lstat` (regular file, mode `0o600`, no symlink), `os.open` with `O_NOFOLLOW|O_CLOEXEC`, `fstat` with
  the inode/dev re-check against the `lstat` result, then the bounded tail read. Two intentional
  differences from `read_regular`, both commented in the code: the file may be **empty** (an empty
  stderr log is a legitimate observation, published as zero messages) and its **size is not capped**
  (only the tail is needed, so a long-lived daemon's multi-megabyte log is read, not rejected). A
  missing file is skipped, never reported as missing evidence: most legacy cases never start a second
  daemon (`commit-before-resume` returns before the restart when the mirror is unsealed,
  `production_linux_containment_witness_test.go:1000-1012`).
- **Documentation/`TODO` semantics.** The new TODO entry is an open tracked item (`- [ ]`), not a
  closure: the contract forbids fixing the stall and forbids changing the 45 s bound, so the entry
  records the occurrence, its classification and the diagnosis rule for the next occurrence.

## Evidence plan (contract clause 5)

All gates run on this host; the exporter suite is POSIX-only (its first subprocess fails on Windows with
`module 'os' has no attribute 'getuid'`, measured before any change: 27 tests, 45 failures + 5 errors),
so it runs in WSL Ubuntu-24.04.

1. **Exporter tests, red before / green after** (contract clause 2, recorded in `answer.md`):
   `wsl -d Ubuntu-24.04 -- python3 scripts/ci/test-linux-v2-evidence.py` — first with only the new test
   cases in place and the exporter restored from `HEAD` (they fail: `Ran 5 tests … FAILED (failures=4,
   errors=2)`), then with the exporter change (the same names pass and the whole suite reports 32 tests,
   `OK`). The exporter file is byte-compared before/after the temporary restore.
   **Fix round (2026-11-21, value allowlists):** the shape gates were replaced by per-field literal
   value sets taken from the daemon producers, the drift scan
   (`test_daemon_log_allowlists_match_daemon_producers`) and a no-`--secret-values-file`
   credential-shaped-value case were added, and the record fixtures were moved to real producer
   values. Against the pre-fix exporter the changed/added cases fail under `-k daemon_log`
   (`Ran 9 tests … FAILED (failures=3)`); with the fix the suite reports 36 tests, `OK`. Each of
   the five projection-gate mutants M2 (`value check removed`), M4 (`integer bound removed`),
   M6 (`time/level gate removed`), M7 (`bool-as-int accepted`) and M10 (`time accepted as any
   string`) exits non-zero under the same filter, and an injected new log-message or
   `terminateForLease` literal in a scratch daemon tree reddens the drift scan. Windows stays
   POSIX-only: the pre-fix suite reports 54 failures + 5 errors (34 tests) and the fixed suite
   55 failures + 5 errors (36 tests); no pre-existing test changed its Windows outcome, the new
   drift scan passes there, and every other failure is the documented `os.getuid` limitation.
2. **Mutation check for the failure control** (contract clause 3): replace the projection with a raw
   passthrough (`messages = [{"raw_line": line.decode()} …]`) in the changed exporter, re-run the
   redaction case, and record that it reddens on the `/home/runner` assertion; restore the exporter and
   re-check its digest, so "unredacted log content in the artifact" is demonstrated to be caught rather
   than asserted.
3. **`go vet ./daemon/e2e/...` on Windows** (contract gate) — the tree's Go files are untouched, so this
   is a no-change confirmation on the OS the contract names.
4. **WSL Ubuntu-24.04 `go build` of `daemon/e2e`** (contract gate) — the witness itself needs CI's
   privileged runner and is recorded as CI-judged; the compile/type check of the unchanged package is
   the reachable part. `daemon/e2e` holds only `_test.go` files for the package root, so the command is
   run in the shape that actually compiles it — `go test -c -o /dev/null ./e2e` builds the Linux test
   binary (nothing executes) and `go vet ./e2e/...` type-checks the test files — beside the shape the
   contract names; every exit code is reported.
5. **`actionlint -shellcheck=`** — applicable only to changed workflows; no workflow changes here, so
   the gate is reported as not applicable with that reason.
6. **`git diff --check`** — whitespace/conflict hygiene over the delivered diff.

No measurement or benchmark harness is involved, so the host-load sequencing rule (never run a
measurement harness beside the full test suite) has nothing to serialize; the WSL suite and the Go
builds are still run one at a time.

## Design trade-offs and decisions

1. **Sanitized projection instead of raw log upload.** The contract's failure control is explicit:
   unredacted log content in the artifact is a failed increment, and the repo's exporter is built on
   allowlists (`SAFE_TEXT`, per-field projections, forbidden-key scan). A substring-based masker over
   free text would be a second, weaker redaction rule living beside the existing one; reusing the
   existing shapes (strict JSON, bounded values, `validate_document`) keeps one rule set.
2. **Message text without `error` text.** `msg` is the field that names what the daemon did, and in
   this daemon it is always an identifier; `error` is the field that carries paths, URLs and
   quotes. Dropping `error` is what makes "host paths are not in the artifact" a provable statement
   rather than a hope; a future need for error text should be a deliberate contract (e.g. an
   allowlist of error kinds), not an exception here.
3. **`truncated` + `dropped_lines` counters instead of silent loss.** The reader of the artifact must be
   able to tell a quiet daemon from a tail that was cut or that contained unprojectable lines; both
   counters are content-free and bounded.
4. **Failure-only export gated on the run verdict.** See "Trigger" above; the alternative (export always)
   would add two tail documents per case to every green run's artifact and still not tell the reader
   which case failed, since the exporter cannot see per-case verdicts.
5. **No workflow change.** The artifact upload already globs `*.json`; a changed workflow would pull in
   the `actionlint` gate and the workflow-parity reviews for no behavioural gain.
6. **No Go change.** The restarted daemon's logs already exist (`:1723-1754`); the gap is that the
   exporter ignores them. Adding daemon-side logging to the receipt/barrier path would change product
   code for a test-only witness and is out of contract scope.

## Out of scope (contract clause 4)

Fixing the stall (no reproduction exists: the same revision is green in 0.56 s on main — run
`44795802448`, job `136495892251`, `PASS: …/daemon_sigkill_after_lease_arm_before_resume (0.56s)` — while
the failing run took 110.19 s); raising or lowering the 45 s bound; first/third-daemon and
cleanup-recovery daemon logs (the exported set is the `LEGACY_DAEMON_LOG_LABEL` constant plus its stream
pair, so widening it later is a one-line edit); the raw go-test events artifact (diagnostic-only, 3-day
retention, already uploaded on failure); any `daemon/internal/platform` or CI-timeout work.

## Residual risks

- The tail is 64 KiB by construction: if the daemon's last useful line is further back than that on a
  noisy failing run, it is cut off and only `truncated:true` says so. The bound is a constant, chosen to
  fit the failing run's scale (the whole failing daemon ran ~110 s at `slog.LevelInfo`).
- A stall that happens *before* any daemon-side activity (e.g. the daemon never restarted) yields an
  empty or absent tail; the export then shows `records:0`/no file. That is the honest limit of a log
  tail as evidence.
- Unstructured fatal output (Go panic dumps, the `main.go` stderr sentences) is counted, never
  published; the artifact tells the reader that unreadable lines existed but not what they said.
- The exporter suite cannot be run on Windows at all (`os.getuid`), so the gate depends on the WSL
  distribution being present on this host; it is (Ubuntu-24.04, running).
