# rf-w1-05 app.go split — evidence record (2026-11-21)

Subject: `daemon/internal/app/app.go` at `a3039f0d17fc7c7905b63bdf247cac5bf339b213` (12,048 lines,
475 top-level declarations) split move-only into eleven concern siblings in the same package.
Contract: `goal-rf-w1-05-app-go-split.md`. Gates below ran foreground in
`Q:/repos/wt-w1-05-app-go-split/symmetry` on 2026-11-21; no `-race` run (no cgo on this Windows
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

- Comparison carrier: extracted declaration text with **all whitespace removed**. `gofmt` re-aligns
  exactly one declaration (`systemTimer.Stop`, a one-line func whose column padding depends on its
  neighbours); whitespace carries no program meaning in Go and every other declaration compares
  byte-identical without normalization.
- Per-declaration table: `docs/evidence/rf-w1-05-app-go-split-move-manifest-2026-11-21.txt`
  (485 lines: 475 declarations + header), columns
  `kind, receiver, name, original line, target file, target line, text sha256 (8 hex)`.
- Independent coverage check (declaration byte ranges against the original file):
  476 ranges (475 declarations + the import declaration), 0 overlapping bytes, 466,563 / 467,567
  bytes covered; the 1,004 uncovered bytes are 474 blank-line separators plus the 47-byte file
  header (`// Package app owns the daemon control loop.` + `package app`).
- `git diff --color-moved=dimmed-zebra` shape for the subject file:
  `daemon/internal/app/app.go | 2 insertions(+), 9811 deletions(-)` with the ten sibling files added
  (9,995 insertions / 9,824 deletions across 15 changed paths including the docs and this record).

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
| package tests (final tree) | `go test -count=1 ./internal/app/...` | `ok github.com/wxxb789/symmetry/daemon/internal/app 390.465s`, exit 0 |
| whitespace | `git diff --check` | exit 0 (only `core.autocrlf` advisories) |
| move proof | `appsplit verify … --moves moves.tsv` | exit 0, 475/475 |
| citation gate | `python3 scripts/ci/check-citation-existence.py --base <changed range> --head HEAD` | exit 0 |
| exporter unit tests | `python3 scripts/ci/test-linux-v2-evidence.py` (`scripts/ci/`) | 36 tests, 55 failures + 5 errors — **identical** on the unmodified base revision (63.9 s base vs 105.3 s working tree); the failures are the Windows-host `os.getuid` limitation, not a regression (only three comment lines changed in that script) |

Gates added by the binding review amendments: `GOOS=linux GOARCH=arm64` and `GOOS=darwin`
builds (both exit 0), AST declaration-set comparison (above), and the docs-citation update
(section 4).

## 3. Concerns, counts, and the review round

| file | lines | declarations |
|---|---:|---:|
| `app.go` | 2239 | 94 |
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
| total | 12,217 | 475 |

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
| `docs/plans/2026-11-21-witness-stall-evidence-plan.md` | `app.go:2148-2159`, `app.go:460` | `cleanup_glue.go:118-143`, `app.go:311` |
| `scripts/ci/export-linux-v2-redacted-evidence.py` (comment block) | `app.go:460`, `app.go:7385, 7465`, `app.go:8978`, `app.go:3651-3877`, `app.go:7586` | `app.go:311`, `outbox.go:124, 186`, `goal_delivery.go:357`, `events.go:530` + `app.go:1441-1667`, `app.go:2012` |

Not re-pointed by decision: `docs/goals/archive/**` and `docs/evidence/**` historical records keep
the revision they were written against (a dated evidence record cites that revision by design;
`goal-0006b-resumed-test-review-2026-09-29.md` even carries the pre-move SHA-256 of `app.go`).
`docs/archive/CLOSED-TODO.md` citations (`app.go:2207` twice) stay for the same reason — the archive
records a repair made against the pre-split tree. The CI citation gate audits only runbooks,
`docs/design/quality.md` and `docs/archive/CLOSED-TODO.md` modified within the compared range; a
local probe worktree run of that script over the changed range reported
`2 changed document(s) pass the path and symbol check`, exit 0.

## 5. Residual risks and known limits

- **`-race` is not covered locally** (no cgo on this Windows host): CI's `daemon` job
  (`go test -race ./...`, ~6.5 min) remains the race oracle, exactly as the contract states.
- **gofmt alignment of one one-line func** is the single non-byte-identical declaration
  (`systemTimer.Stop`); the comparison therefore normalizes whitespace package-wide rather than
  special-casing it, so a whitespace-only difference anywhere else would also pass — accepted
  because whitespace is not program content in Go and `gofmt -l` is clean.
- **The split tool lives outside the repository** (scratch path, not committed). Its inputs are
  `decls.json` (from `appsplit scan`) and the rule list/override table in `classify.py`; the
  committed move manifest is the durable output. A future rebase regenerates the split with the same
  two commands (`appsplit split`, `appsplit verify`) against the rebased `app.go`.
- **The exporter unit suite cannot pass on this host** (Windows lacks `os.getuid`); the base
  revision fails identically (55 failures + 5 errors both before and after), so the changed comment
  block is covered by inspection plus the Linux-only suite in CI.
- **Line numbers in prose rot with the next edit anywhere above them.** The re-pointed citations
  above are correct at this revision; the new `app.go`/siblings are the owner files named by the
  refactor program's wave-2 accounting.
- **File sizes are uneven** (`probe.go` 356 lines vs `native_run.go` 2417). Accepted: the contract
  fixes the eleven concern names, and further splitting of `native_run.go` is exactly the W2-10 /
  W4-01 work this increment unblocks.
