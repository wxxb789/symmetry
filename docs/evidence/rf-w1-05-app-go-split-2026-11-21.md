# rf-w1-05 app.go split — evidence record (2026-11-21)

Subject: `daemon/internal/app/app.go` at `a3039f0d17fc7c7905b63bdf247cac5bf339b213` (12,048 lines,
475 top-level declarations) split move-only into ten concern sibling files in the same package.
Contract: `goal-rf-w1-05-app-go-split.md`. Gates below ran foreground in
the repository root of this worktree on 2026-11-21; no `-race` run (no cgo on this Windows
host — CI is the race oracle).

## 1. Move-only proof

Produced by the scratch tool `appsplit` (`go/ast` byte-range extraction; standard library only):
each top-level declaration is sliced out of the source **with its doc-comment cluster** and re-emitted
unchanged, on both revisions, then compared keyed by `kind|receiver|name`.

```
appsplit verify origfmt/app.go <12 target files> --moves moves.tsv
  import set: original=35 targets=35 identical=true
  {"original":475,"targets":475,"mismatch":0,"extra":0}
  exit 0
```

- Comparison carrier: extracted declaration text with **all whitespace removed** for the first
  verification run. That run found exactly one non-byte-identical declaration: `systemTimer.Stop`,
  a one-line func whose column padding depends on its neighbours, realigned by `gofmt` after the
  splitter inserted a blank line above it. The review round removed that blank line, and an
  exact-byte re-comparison of the delivered tree (independent `go/ast` tool: doc comment through
  declaration end, **no normalization**) reports 475/475 declarations byte-identical.
- Per-declaration table: `docs/evidence/rf-w1-05-app-go-split-move-manifest-2026-11-21.txt`
  (485 lines: 475 declarations + header), columns
  `kind, receiver, name, original line, target file, target line, text sha256 (first 8 bytes, 16 hex)`;
  the `name` column truncates 4 multi-name blocks with `...`, so rows key by (`kind`, target file,
  target line), unique across all 475 rows.
- Independent coverage check (declaration byte ranges against the original file):
  476 ranges (475 declarations + the import declaration), 0 overlapping bytes, 466,563 / 467,567
  bytes covered; the 1,004 uncovered bytes are a 58-byte file header (`// Package app owns the
  daemon control loop.` + `package app` + the blank line) plus 476 blank-line separators
  (470 × 2 bytes + 6 × 1 byte = 946).
- `git diff --color-moved=dimmed-zebra` shape for the subject file:
  `daemon/internal/app/app.go | 0 insertions(+), 9810 deletions(-)`
  (`git diff --numstat 90465bf -- daemon/internal/app/app.go`) — the delivered core file is a
  strict subsequence of the source revision's lines — with the ten sibling files added. Range
  totals (`git diff --numstat 90465bf`, the PR merge-base, over the delivered tree): 19 changed
  paths, 10,834 insertions, 9,832 deletions — the 11 Go files plus 8 docs/script paths, this record
  included.

## 2. Gate results (exact commands, exit codes)

| gate | command (cwd) | result |
|---|---|---|
| gofmt | `gofmt -l ./internal/app/` (`daemon/`) | no output, exit 0 |
| vet | `go vet ./...` (`daemon/`) | exit 0 |
| build windows/amd64 | `GOOS=windows GOARCH=amd64 go build ./...` | exit 0 |
| build linux/amd64 | `GOOS=linux GOARCH=amd64 go build ./...` | exit 0 |
| build linux/arm64 | `GOOS=linux GOARCH=arm64 go build ./...` | exit 0 |
| build darwin/arm64 | `GOOS=darwin GOARCH=arm64 go build ./...` | exit 0 |
| package tests (pre-doc-edits run) | `go test -count=1 ./internal/app/...` | `ok github.com/wxxb789/symmetry/daemon/internal/app 235.635s`, exit 0 |
| package tests (post-split tree, pre-review-fix) | `go test -count=1 ./internal/app/...` | `ok github.com/wxxb789/symmetry/daemon/internal/app 239.075s`, exit 0 |
| review-fix round (delivered tree) | `gofmt -l ./internal/app/`; `GOOS=windows GOARCH=amd64 go build ./...`; `GOOS=linux GOARCH=amd64 go build ./...`; `go test -count=1 ./internal/app/...`; `git diff --check` | gofmt: no output; builds: exit 0; tests: `ok github.com/wxxb789/symmetry/daemon/internal/app 219.724s`, exit 0; diff check: exit 0 |
| whitespace | `git diff --check` | exit 0 (only `core.autocrlf` advisories) |
| move proof | `appsplit verify … --moves moves.tsv` | exit 0, 475/475 |
| move proof (review-fix round) | exact-byte `go/ast` comparison: source `app.go` vs `app.go` + 10 siblings, keyed `kind|receiver|name`, doc comment through declaration end, no normalization | `byte-identical 475, diff 0, missing 0, extra 0`, exit 0 |
| citation gate | `python3 scripts/ci/check-citation-existence.py --base a3039f0d17fc --head 3b73fdfeff` | `no audited document added or modified between a3039f0d17fc and 3b73fdfeffef; nothing to check`, exit 0 |
| exporter unit tests | `python3 scripts/ci/test-linux-v2-evidence.py` (`scripts/ci/`) | 36 tests, 55 failures + 5 errors — **identical** on the unmodified base revision (63.9 s base vs 105.3 s working tree); the failures are the Windows-host `os.getuid` limitation, not a regression (that script's diff is comment-only: 18 comment lines added, 13 removed, 0 code lines) |

Gates added by the binding review amendments: `GOOS=linux GOARCH=arm64` and `GOOS=darwin`
builds (both exit 0), AST declaration-set comparison (above), and the docs-citation update
(section 4).

## 3. Concerns, counts, and the review round

| file | lines | declarations |
|---|---:|---:|
| `app.go` | 2238 | 94 |
| `native_run.go` | 2417 | 90 |
| `goal_admission.go` | 1584 | 29 |
| `outbox.go` | 1175 | 52 |
| `commands.go` | 1009 | 42 |
| `recovery_glue.go` | 894 | 35 |
| `goal_delivery.go` | 892 | 30 |
| `events.go` | 603 | 39 |
| `lease.go` | 585 | 23 |
| `cleanup_glue.go` | 463 | 22 |
| `probe.go` | 356 | 19 |
| total | 12,216 | 475 |

`options.go` is contract-listed but uncreated: the option composition surface stays in `app.go`
(see the plan's "Deliverable shape").

Concern assignment is mechanical and re-runnable: an ordered regex rule list plus an explicit
override table over the 475 declarations (both artifacts are whole files, reproduced by re-running
`classify.py` from `decls.json`). An independent read-only reviewer (separate model, 475
declarations reviewed, callers cross-checked against `recovery.go`, `cleanup.go`, `supervisory.go`,
`linux_owner_v2_linux.go`, `deterministic_validation.go`) returned 40 misfiled and 28 ambiguous
entries. 50 corrections were applied and 4 recommendations declined, each with a stated reason in
the plan's "Ambiguous placements" section. No declaration moved between files after the final
verification run.

## 4. Documents re-pointed (citations the split invalidates)

| document | old citation | new citation |
|---|---|---|
| `docs/design/harness.md` | `daemon/internal/app/app.go` (start/events/cancel admission) | `startGoalAdmission` (`daemon/internal/app/goal_admission.go:263`) |
| `docs/TODO.md` | `daemon/internal/app/app.go:2148-2159`, `:2227`, `:2258`, `:2242`/`:2245`, `:71` | `daemon/internal/app/cleanup_glue.go:118-143`, `:285`, `:203`, `:234`, `:218`/`:221`, `:21` |
| `docs/plans/2026-11-21-witness-stall-evidence-plan.md` | `app.go:2148-2159`, `app.go:460` | `cleanup_glue.go:118-143`, `app.go:310` |
| `scripts/ci/export-linux-v2-redacted-evidence.py` (comment block) | `app.go:460`, `app.go:7385, 7465`, `app.go:8978`, `app.go:3651-3877`, `app.go:7586` | `app.go:310`, `outbox.go:166, 246`, `goal_delivery.go:357`, `events.go:530` + `app.go:1440-1666`, `app.go:2011` |
| `scripts/ci/export-linux-v2-redacted-evidence.py` (comment block, finish pass) | `app.go:2857, 2891, 3604, 10375`; `app.go:9922-10073, 10918, 11468, 11752`; `app.go:7918-7982, 8940-8975, 9524`; `app.go:7349, 7410, 7414`; `app.go:7936-7972, 8939-8969, 9549`; the four-file `daemon.log` call-site enumeration | `app.go:1188, 1222, 1393` and `lease.go:579`; `lease.go:126-277` and `outbox.go:734, 834, 1110`; `native_run.go:1566-1630, 2181-2216` and `commands.go:696`; `outbox.go:130, 191, 195`; `native_run.go:1584-1620, 2180-2210` and `commands.go:721`; the twelve call-site files |

Review round: the fix for the one `gofmt` realignment removed a blank line inside `app.go`, so every
`app.go` line number after it is one lower than the first-pass mapping; the new-citation column
above and the move manifest's target-line column are stated for the delivered revision.

Not re-pointed by decision: `docs/goals/archive/**` and `docs/evidence/**` historical records keep
the revision they were written against (a dated evidence record cites that revision by design;
`goal-0006b-resumed-test-review-2026-09-29.md` even carries the pre-move SHA-256 of `app.go`).
`docs/archive/CLOSED-TODO.md` citations (`app.go:2207` twice) stay for the same reason — the archive
records a repair made against the pre-split tree. The CI citation gate audits only runbooks,
`docs/design/quality.md` and `docs/archive/CLOSED-TODO.md` modified within the compared range; none
of those paths changed in this range, so running the gate over the changed range reports
`no audited document added or modified between a3039f0d17fc and 3b73fdfeffef; nothing to check`,
exit 0 — the re-pointed citations above are outside every gate and were verified against the
delivered files instead (each finish-pass line range in section 4 is byte-identical to its cited
base-revision range).

## 5. Residual risks and known limits

- **`-race` is not covered locally** (no cgo on this Windows host): CI's `daemon` job
  (`go test -race ./...`, ~6.5 min) remains the race oracle, exactly as the contract states.
- **The OMO receipt source freeze now covers the split** (`scripts/ci/omo-control-native-evidence.py`
  `SOURCE_PATHS`, conductor decision on opus P2-1): the ten siblings are listed next to
  `daemon/internal/app/app.go`, so a parent attestation issued at or after this revision binds the
  same code the pre-split `app.go` held. Receipts issued against earlier revisions stay bound to
  the revision they recorded. The change is list-only; the script's unit suite
  (`python scripts/ci/test-omo-control-native-evidence.py`) reports `Ran 10 tests in 7.011s`, `OK`,
  exit 0, and pins UUID/build-identity parsing, not the list.
- **The first run's single `gofmt` realignment was removed in the review round.** `systemTimer.Stop`
  had been re-aligned because the splitter inserted a blank line above the one-line func; dropping
  that line restores the source revision's exact bytes, so the delivered tree's 475 declarations
  compare byte-identical with no whitespace normalization (exact-byte `go/ast` re-comparison, §1),
  and `gofmt -l` stays clean.
- **The split tool lives outside the repository** (scratch path, not committed). Its inputs are
  `decls.json` (from `appsplit scan`) and the rule list/override table in `classify.py`; the
  committed move manifest is the durable output. A future rebase regenerates the split with the same
  two commands (`appsplit split`, `appsplit verify`) against the rebased `app.go`.
- **The exporter unit suite cannot pass on this host** (Windows lacks `os.getuid`); the base
  revision fails identically (55 failures + 5 errors both before and after), so the changed comment
  block is covered by inspection plus the Linux-only suite in CI.
- **Line numbers in prose rot with the next edit anywhere above them.** The re-pointed citations
  above are correct at this revision; the new `app.go` and its sibling files are the owner files
  named by the refactor program's wave-2 accounting. This round exercised that: removing the blank
  line above
  `systemTimer.Stop` lowered every later `app.go` line number by one, and §4's table plus the move
  manifest were re-pointed accordingly.
- **File sizes are uneven** (`probe.go` 356 lines vs `native_run.go` 2417). Accepted: the contract
  fixes the eleven concern names, and further splitting of `native_run.go` is exactly the W2-10 /
  W4-01 work this increment unblocks.
