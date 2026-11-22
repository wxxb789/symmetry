# rf-w6-06a launcher + clone-bootstrap move — plan

Contract: `goal-rf-w6-06a-launcher-move.md` (GDD state root; external to the repository).
Base revision: `b6c7c38397f41d99a0a1e4956e9fb8aaa8977a98` (origin/main, 2026-11-22). Branch:
`refactor/rf-w6-06a-launcher-move`. Owned files are the contract's strict set plus this plan and
`docs/TODO.md`/`docs/archive/CLOSED-TODO.md` if an entry closes (none does; see §6).

Goal shape: move-only. `cmd/symmetry-linux-launcher` and `cmd/symmetry-linux-clone-bootstrap`
become thin process shims; every other declaration moves verbatim into
`internal/launchercore` and `internal/clonebootstrap` with only the package clause, the recorded
rename map, one deleted dead comment and three rename-stale comment corrections (conductor
ruling 2026-11-22) as deltas. 06a alone does not remove RC-1 (ordering
policy fused with the host effect surface); it removes RC-2's blocker by creating importable
homes, and must not be reported as root-cause removal.

## 1. Target layout and rename map

| HEAD file | Source | Deltas |
| --- | --- | --- |
| `internal/launchercore/main_linux_amd64.go` | `cmd/symmetry-linux-launcher/main.go` minus `main`/`run` | `package launchercore`; `runLaunch`→`RunLaunch`, `runCloneCleanup`→`RunCloneCleanup`; dead trailing comment deleted; the two comment references to `runCloneCleanup` corrected to `RunCloneCleanup` |
| `cmd/symmetry-linux-launcher/main.go` | new shim | keeps `main()`/`run()` byte-identical except the two calls now read `launchercore.RunLaunch()` / `launchercore.RunCloneCleanup()`; binary-identity package doc comment kept here |
| `internal/launchercore/main_linux_amd64_test.go` | `cmd/symmetry-linux-launcher/main_test.go` tests 1-5,7 + `openCleanupTestPins`, `newCleanupTestRoot`, `protocolOwnerForCleanupTest` | `package launchercore`; imports trimmed to those the moved tests use |
| `cmd/symmetry-linux-launcher/main_test.go` | same file, test 6 (`TestInstalledLinuxOwnerCleanupRejectsBCallerScratch`) only | imports trimmed; body verbatim |
| `internal/clonebootstrap/{main,filesystem,git,guard,exec}_linux_amd64.go` | the five `cmd/symmetry-linux-clone-bootstrap` sources minus `main` | `package clonebootstrap`; `runBootstrap`→`RunBootstrap`, `statusFDFromArgs`→`StatusFDFromArgs`, `sendStatusPacket`→`SendStatusPacket`, `phaseError`→`PhaseError` with `stage`→`Stage`, `errno`→`Errno`, `cause`→`Cause`; the one comment reference to `runBootstrap` corrected to `RunBootstrap` |
| `cmd/symmetry-linux-clone-bootstrap/main.go` | new shim | keeps `main()` with base ordering: pre-parse the status FD, call `clonebootstrap.RunBootstrap(os.Args)`, on any return emit the same `EncodeCloneBootstrapFailure` frame on that FD, `os.Exit(1)`. The fallback `&PhaseError{Stage: CloneFailureRequest, Errno: EPROTO, Cause: err}` keeps the base EPROTO semantics. Binary-identity package doc comment kept here |
| `internal/clonebootstrap/main_linux_amd64_test.go` | `cmd/symmetry-linux-clone-bootstrap/main_linux_amd64_test.go` | `package clonebootstrap` |

Build tags stay exactly `//go:build linux && amd64` on every moved and new file. Every exported
name in the two internal packages is a rename of a base declaration; no new declaration is
created, and none is deleted except `main`/`run` moving to the shims. No file is split.

Naming decisions (contract leaves these open):

- `internal/launchercore/main_linux_amd64.go` / `main_linux_amd64_test.go` mirror the repo's
  `*_linux_amd64.go` convention (the contract's own example) for a linux && amd64-only source.
- The bootstrap shim is `cmd/symmetry-linux-clone-bootstrap/main.go`; the launcher shim keeps
  the contract-named `cmd/symmetry-linux-launcher/main.go`.
- The `symmetry-linux-*-core`/bootstrap package doc comments describe the installed binaries, so
  they stay on the shim package clauses with unchanged bytes; the internal packages get no new
  doc comment. No other comment is added, moved or edited; the three comment references the
  rename made stale (`runCloneCleanup` ×2, `runBootstrap` ×1) are corrected in place per the
  conductor ruling.

## 2. Acceptance script

Exactly one argument: `scripts/ci/linux-v2-native-acceptance.sh:2436`
`./cmd/symmetry-linux-launcher` → `./internal/launchercore` (the `TestCloneCleanupRejectsCrossMountEntry`
walker invocation, mode `cleanup-mounts-root`). Line `:2433` (installed-wrapper denial test) keeps
`./cmd/symmetry-linux-launcher`. Two-token delta co-owned with the conductor per C1; recorded in
`answer.md`.

## 3. Delegation decision (stage template, optional `ce-work mode:return-to-caller`)

Not eligible, so the implement loop runs inline here. The template's conjunction fails: the
dispatch's gate placeholder is `-`/empty (no `--gates` command supplied), and the template
condition requires the gate to be "a real command, not `-` (no gate supplied)"; the runtime's
skill catalog does not expose a `return-to-caller` mode parameter either. Recorded rather than
approximated.

## 4. Evidence plan (judging artifact: `answer.md` in the GDD run dir)

- AST declaration-set/comment/import comparison base vs HEAD (go/ast + go/printer; a scratch
  program under the GDD tmp root, never committed): decl multiset equal after the rename map,
  comment groups equal except the one deleted dead comment plus the three rename-stale text
  corrections, import/package deltas confined to the two shims and the new internal files.
  Baseline captured before the move: base vs pre-move
  tree = 0 differences across 173 decls / 17 comment groups / 8 import sets / 8 package clauses.
- Per-function control-flow audit table for the contract's function list, backed by the
  normalized per-decl text equality (the same program's decl records).
- ABI table base vs HEAD: argv forms, FD numbers, fixed environment, exit codes, launcher
  ready-failure stage names, `LaunchStatus*` values, gate/bootstrap paths, bounds, installed
  names.
- `git diff --color-moved=zebra --stat` summary and moved-line count.
- Gate matrix from `daemon/` on Windows (gofmt, vet, linux/windows builds+vet, shim builds,
  `go list`), WSL race run over the five packages with the recorded SKIP list, `go test -run='^$'
  -list` name check for all 10 test names, `bash -n` + one-argument diff for the acceptance
  script, and the resolver check for both pinned invocations.
- Independent read-only audit worker: its own comparison of base vs HEAD (no reuse of the
  scratch program), ABI/comment spot checks; reconcile both reports.

## 5. Out of scope

The 06b syscall/fd seam and any new test; the W6-05 legacy containment deletion;
`native/linux-gate/protocol/**`; `internal/authority/**`; witness tests and acceptance case sets;
any comment-policy sweep beyond the dead comment and the three rename-stale reference
corrections; `docs/goals/0010-refactor-program.md`'s
W6-06 row (conductor-owned; flagged in the report as the document this slice makes stale).

## 6. TODO bookkeeping

No `docs/TODO.md` entry closes here: no 06a entry is open, and this slice changes no
`docs/TODO.md` line. The W6-06 rows in `docs/goals/0010-refactor-program.md:312` and
`docs/plans/2026-11-21-refactor-program-plan.md:562` are conductor-owned program records, not
this slice's; the conductor reconciles them.

## 7. Verification limits

The hosted `daemon` job's `go test -race ./...` (the CI evidence) can only run on the PR; locally
the WSL race run over the five named packages is the substitute oracle. The acceptance script's
native run needs the Linux v2 acceptance host and is out of 06a's oracle (contract: no live
linux-native-v2 run required). WSL runs Go 1.27.0; the Windows host toolchain is 1.27.4.
