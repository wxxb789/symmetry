# rf-daemon-state-dir-default — evidence record

Goal `rf-daemon-state-dir-default`, contract `goal-rf-daemon-state-dir-default.md` (GDD state
root, external to the repository). Worktree branch `refactor/rf-daemon-state-dir-default`, tree
left uncommitted by the implement stage; every provenance anchor is therefore a file sha256, never
a revision. Rulings: Q-A strict (`-print-state-dir` loads via `config.LoadInstalled`; the installer
script stays byte-identical), Q-B confirmed (no extra `app.Run` guard keyed on harness kind), Q-C
confirmed (darwin row logic-only, never host-verified, never "supported").
Plan: `docs/plans/2026-11-25-rf-daemon-state-dir-default-plan.md`, sha256
`6c00964374183e0c2922a5866419bdeb1d64e1aac737aa2512d20f51e57a6852`.

Line numbers below are at the delivered bytes.

## 1. Delivered bytes (sha256-only provenance)

| File | Dispatch head | Delivered |
| --- | --- | --- |
| `daemon/internal/config/config.go` | `da3976c5f0e24477f7b530fde55d08c4b7098d324e1f7ebea85870ab94191f6b` | `e1521e6e8895fa8cac8bc6d365bd49ff84dc0ec9afb9f60915e6d24fcf56c856` |
| `daemon/internal/config/config_test.go` | `a8f50257c27377af2cf1d9bf4a4fdfc09a88bf70f079b9b5db22352fddf60321` | `2002fc67321ab4e066466998448ad6c23a3d6b5fe661adc4d8ab20c3ff1dac0b` |
| `daemon/cmd/symmetry-daemon/main.go` | `80a7a7ff32fffc9aad3fe1d6272d25660ef664f3157de498b8d9bb133423147b` | `651124f2f942daf2b25055ade25fd936a9395e3693f2362ba93c4d5f5a803659` |
| `daemon/cmd/symmetry-daemon/main_test.go` | `37124bf55d6b7156da7851339f110f4f7376058b959c918ae8a510322aae0499` | `5ad1ffc041c551f584186ef7e385602440defd30dc34e6ff6e65e1703b5744ef` |
| `docs/runbooks/control-plane-and-daemon.md` | `f29b013f62dd69ea0e0fe1729a4799f897c8ad45ee404320b9a69024f9eb907c` | `efd66e1f98e29970615673a17415d7ef69ef8976faa0babf4dbd060dbcfcd042` |
| `docs/TODO.md` | `d1b95fc7a0103562d4c520c315b29aacbe7164ccb9365223aeee14f392e5790d` | unchanged (same digest) |
| `docs/archive/CLOSED-TODO.md` | `9e79fe6cee3cba6c8dcb6d2ad97fb90a096c7d653ad716fd9b9f2cedefe29f7c` | unchanged (same digest) |
| `scripts/install-linux-owner.sh` | — | untouched: `git diff --stat -- scripts/` and `git status --porcelain -- scripts/` are both empty |
| `daemon/internal/state/state_unix.go` | `119b3a04b7642f50dc70ef6479e1e16643a27122126d3fa25ec90c82eb2f7ed9` | unchanged (same digest); mutated only in the scratch copy of §6 |

`git status --porcelain -uall` lists exactly the five modified files above plus the untracked plan
and this record. No other path changed; no path was added and later removed.

## 2. Clause census and dispositions (re-located by content, delivered bytes)

| Contract anchor (head) | Delivered bytes | Disposition |
| --- | --- | --- |
| `config.go:30` `StateDir` | `config.go:29` `json:"state_dir"` (no omitempty) | untouched |
| `config.go:150-177` `Load` | `:206-244` private `load`; thin `Load` at `:196`, `LoadInstalled` at `:202`, resolver at `:151-177` | default resolved between decode and `Validate` |
| `config.go:196` `requireNotEmpty("state_dir", …)` | `:262` | unchanged: explicit `" "` and programmatic values still refused |
| `config.go:484-489` helper | `:550-555` | unchanged |
| `main.go:131-157` print | `:131-157`; loader swap at `:147` | `config.LoadInstalled`; `:159-216` normalization + symlink rejection unchanged |
| `main.go:240-281` owner commands | `:240-281`; loader swap at `:250` | `config.LoadInstalled` |
| `main.go:70-93` plain start | `:82` `config.Load` | unchanged: default-aware |
| `config_test.go:453-457` `" "` row | `:453-457` | unchanged, still green |
| `main_test.go:56-94` print rejection table (`""` at `:70`) | `:56-94` | unchanged and green; `""` now refuses at the loader |
| runbook `:202-204`, `:216`, `:245-248` | `:202-204`, `:216`, `:245-248` | unchanged; new paragraph `:250-262` |

## 3. Resolver table (P1-P3, P8) — inputs, outputs, errors

`defaultStateDir(goos, getenv)` rows, all green in `TestDefaultStateDirResolvesPerGOOS`
(`config_test.go:502-552`), WSL Ubuntu-24.04 (Go 1.27.0) and the Windows host (Go 1.27.4):

| GOOS | env | result |
| --- | --- | --- |
| linux | `XDG_STATE_HOME=/srv/state`, `HOME=/home/u` | `/srv/state/symmetry` |
| linux | `XDG_STATE_HOME=/srv/state/`, `HOME=/home/u` | `/srv/state/symmetry` |
| linux | `XDG_STATE_HOME=state` (relative), `HOME=/home/u` | `/home/u/.local/state/symmetry` |
| linux | `XDG_STATE_HOME=""`, `HOME=/home/u` | `/home/u/.local/state/symmetry` |
| linux | `HOME=/home/u` | `/home/u/.local/state/symmetry` |
| linux | `XDG_STATE_HOME=state`, `HOME=home/u` (relative) | error naming `$HOME` |
| linux | `HOME="  "` (blank) | error naming `$HOME` |
| linux | (both unset) | error naming `$HOME` |
| darwin | `HOME=/Users/u` | `/Users/u/Library/Application Support/Symmetry` |
| darwin | `HOME=Users/u` (relative) | error naming `$HOME` |
| darwin | (unset) | error naming `$HOME` |
| windows | `LOCALAPPDATA=C:\Users\u\AppData\Local` | `C:\Users\u\AppData\Local\Symmetry` |
| windows | `LOCALAPPDATA=C:\Users\u\AppData\Local\` | `C:\Users\u\AppData\Local\Symmetry` |
| windows | `LOCALAPPDATA=C:\` | `C:\Symmetry` |
| windows | `LOCALAPPDATA=\\server\share` | `\\server\share\Symmetry` |
| windows | `LOCALAPPDATA` unset | error naming `%LOCALAPPDATA%` |
| windows | `LOCALAPPDATA="  "` | error naming `%LOCALAPPDATA%` |
| windows | `APPDATA`, `USERPROFILE` set, `LOCALAPPDATA` unset | error naming `%LOCALAPPDATA%` (never `%AppData%`) |
| windows | `LOCALAPPDATA=Local\AppData` | error naming `%LOCALAPPDATA%` |
| windows | `LOCALAPPDATA=C:Local` (drive-relative) | error naming `%LOCALAPPDATA%` |
| windows | `LOCALAPPDATA=\\` (bare prefix) | error naming `%LOCALAPPDATA%` |
| freebsd | `HOME=/home/u` | error naming GOOS `freebsd`, no path |

Every non-windows success row additionally asserts `path.IsAbs(result)`; every failure row asserts
the error text, so no probe output can carry a relative or cwd-derived path.

`Load`-level rows (`TestLoadDefaultsAndRefusesStateDir`, `:554-610`):
explicit `/var/lib/symmetry` with `HOME`/`XDG_STATE_HOME`/`LOCALAPPDATA` all cleared loads verbatim
(P4 positive); an absent key with the same cleared env fails `resolve default state_dir: …` naming
the host component, `$HOME` or `%LOCALAPPDATA%` (P4 negative); an absent key with the host
component present resolves the host default (P7 first half).

`LoadInstalled` rows (`TestLoadInstalledRequiresAnExplicitStateDir`, `:612-654`): explicit loads;
`absent` and `""` both return exactly
`state_dir must be explicitly configured for the installed owner path` even with a resolvable
default in the environment; `" "` stays explicit and is refused by validation
(`state_dir must not be empty`).

## 4. Characterization baseline and before/after transcripts

Baseline on a scratch copy of the dispatch head, `HOME=/home/u XDG_STATE_HOME=/srv/state` in the
process env, configs with `state_dir` absent / `""` / `" "` / `/var/lib/symmetry`:

```
explicit: Load state_dir: "/var/lib/symmetry"
absent: Load error: state_dir must not be empty
empty: Load error: state_dir must not be empty
space: Load error: state_dir must not be empty
absent print: exit=1 stdout bytes=0 stderr=[-print-state-dir: load daemon config: state_dir must not be empty]
explicit print: exit=0 stdout=[/var/lib/symmetry] stderr=[]
owner -verify-linux-owner-install absent: exit=1 stdout=[] stderr=[-verify-linux-owner-install: load config: state_dir must not be empty]
owner -verify-legacy-owner-drained absent: exit=1 stdout=[] stderr=[-verify-legacy-owner-drained: load config: state_dir must not be empty]
owner -recover-containment-only absent: exit=1 stdout=[] stderr=[-recover-containment-only: load config: state_dir must not be empty]
```

Delivered bytes, same probe configs, binary built from the worktree (WSL `-u accept`, env
`HOME=/home/u XDG_STATE_HOME=/srv/state`):

```
print absent: exit=1 stdout-bytes=0 stderr=[-print-state-dir: load daemon config: state_dir must be explicitly configured for the installed owner path]
print explicit-empty: exit=1 stdout-bytes=0 stderr=[-print-state-dir: load daemon config: state_dir must be explicitly configured for the installed owner path]
print explicit: exit=0 stdout-bytes=17 stdout=[/var/lib/symmetry] stderr=[]
owner -verify-linux-owner-install absent: exit=1 stdout=[] stderr=[-verify-linux-owner-install: load config: state_dir must be explicitly configured for the installed owner path]
owner -verify-legacy-owner-drained absent: exit=1 stdout=[] stderr=[-verify-legacy-owner-drained: load config: state_dir must be explicitly configured for the installed owner path]
owner -recover-containment-only absent: exit=1 stdout=[] stderr=[-recover-containment-only: load config: state_dir must be explicitly configured for the installed owner path]
```

The plain-start half (P7): `TestLoadDefaultStateDirCreatesAPrivateStore` (`main_test.go:114-136`)
resolves the host default through `Load`, opens it with `state.New`, and asserts the leaf is a
directory with mode 0700 on unix (on Windows the assertion is `state.New` itself: its creation path
applies and verifies the account-only DACL via `secureWindowsPath`, and reverifies before
returning). `TestLoadDefaultStateDirReSecuresAPermissiveStore` (`:138-162`) pre-creates the leaf at
0755 and asserts `state.New` re-secures it to 0700; it skips on Windows because Go cannot express
POSIX mode bits there. Both were green on both hosts in the §5 gates.

P6 rows re-ran unchanged: `TestPrintStateDirResolvesAgainstRootWithoutCreatingState` (exit 0) and
the full `TestPrintStateDirRejectsTraversalAndSymlinkWithoutOutput` table (exit 0, zero stdout on
every row, including `""` and the symlinked-component row).

Verbatim per-row probe output (final bytes, WSL): 22/22 `defaultStateDir` rows PASS, 3/3 `Load`
rows PASS, 4/4 `LoadInstalled` rows PASS, 5/5 print/store rows PASS
(`go test -v -count=1 -run 'TestDefaultStateDirResolvesPerGOOS|TestLoadDefaultsAndRefusesStateDir|TestLoadInstalledRequiresAnExplicitStateDir' ./internal/config/`
→ `ok 0.059s`; `-run 'TestPrintStateDir|TestLoadDefaultStateDir' ./cmd/symmetry-daemon/` → `ok 0.142s`).

## 5. Host gates (final bytes)

Windows host (this workstation, Go 1.27.4 windows/amd64), working directory `daemon/`:

| Gate | Command | Exit | Result |
| --- | --- | --- | --- |
| format | `test -z "$(gofmt -l .)"` | 0 | no file listed |
| vet | `go vet ./...` | 0 | clean |
| focused suites | `go test -count=1 ./internal/config/ ./cmd/...` | 0 | `ok config 1.234s`, `ok cmd/symmetry-daemon 0.693s`, `ok cmd/symmetry-fake-agent 12.057s` |

WSL Ubuntu-24.04 (`-u accept`, Go 1.27.0 linux/amd64), gate as the contract writes it from the
repository root:

| Gate | Command | Exit | Result |
| --- | --- | --- | --- |
| linux vet | `(cd daemon && GOOS=linux go vet ./...)` | 0 | clean |
| focused suites (real Linux print + Store rows) | `(cd daemon && go test -count=1 ./internal/config/ ./cmd/...)` | 0 | `ok config 0.450s`, `ok cmd/symmetry-daemon 0.151s`, `ok cmd/symmetry-fake-agent 6.400s`, `? cmd/symmetry-linux-clone-bootstrap [no test files]`, `ok cmd/symmetry-linux-launcher 0.039s` |

No scratch branch, no credentialed run, no e2e suite; the `daemon` and `daemon-windows` CI jobs
remain the merge gates.

## 6. Mutants (M1-M7)

Each mutant was applied to a scratch copy of the delivered bytes outside the goal worktree with the
`apply_patch` editor, probed, then restored from the worktree and digest-verified (`restore=EQ` per
row, digests in §1). The delivered files were never mutated. M1-M6 ran in WSL (`daemon` job
semantics); M7 ran on the Windows host (`daemon-windows` job semantics).

| id | mutation | probe (red) | observed red (verbatim) | restored digest |
| --- | --- | --- | --- | --- |
| M1 | resolve the default unconditionally (override an explicit `state_dir`) | `go test -run TestLoadDefaultsAndRefusesStateDir ./internal/config/` | `config_test.go:567: Load() explicit state_dir error = resolve default state_dir: state_dir default requires $HOME to be set to an absolute path` | `config.go e1521e6e…` |
| M2 | accept a relative `XDG_STATE_HOME` | `go test -run TestDefaultStateDirResolvesPerGOOS ./internal/config/` | `config_test.go:545: defaultStateDir("linux") = "state/symmetry", want "/home/u/.local/state/symmetry"` (plus the relative-home row: `:537: error = <nil>, want one naming "$HOME"`) | `config.go e1521e6e…` |
| M3 | fall back to cwd when `HOME` is missing | `go test -run TestDefaultStateDirResolvesPerGOOS ./internal/config/` | `config_test.go:537: defaultStateDir("linux") error = <nil>, want one naming "$HOME"` ×3 (relative, blank, missing home) | `config.go e1521e6e…` |
| M4 | installed commands load through default-aware `Load` | `go test -run TestPrintStateDirRefusesAnAbsentStateDirWithoutOutput ./cmd/symmetry-daemon/` | `main_test.go:107: print command error = <nil>, want the installed-owner explicit requirement` | `main.go 651124f2…` |
| M5 | `-print-state-dir` skips `rejectLinuxStateDirSymlinks` | `go test -run TestPrintStateDirRejectsTraversalAndSymlinkWithoutOutput ./cmd/symmetry-daemon/` | `main_test.go:89: unsafe StateDir was printed` (the symlinked-component row) | `main.go 651124f2…` |
| M6 | Store directory created/secured with 0755 | `go test -run 'TestLoadDefaultStateDirCreatesAPrivateStore\|TestLoadDefaultStateDirReSecuresAPermissiveStore' ./cmd/symmetry-daemon/` | `main_test.go:134: resolved state dir mode = -rwxr-xr-x, want 0700`; `main_test.go:160: re-secured state dir Lstat = … want mode 0700` | `state_unix.go 119b3a04…` |
| M7 | windows branch falls back to `%AppData%` when `LOCALAPPDATA` is unset (Windows host) | `go test -run TestDefaultStateDirResolvesPerGOOS ./internal/config/` | `config_test.go:537: defaultStateDir("windows") error = <nil>, want one naming "%LOCALAPPDATA%"` (`windows_lookalike_variables_are_refused`) | `config.go e1521e6e…` |

M4 reddens because the probe's resolvable default is printed instead of refused; M5 because the
symlink rejection is what makes the existing negative table pass; M6 because both P7 rows assert
0700 and the mutant leaves the leaf group/world-readable. No mutant reddened a row outside its
expected probe.

## 7. TODO census and out-of-scope confirmations

At the dispatch head:
`git grep -n -i 'xdg\|localappdata\|state_dir\|print-state' docs/TODO.md docs/archive/CLOSED-TODO.md`
returns no hits; `git grep -n 'StateDir'` on the same two files returns only `docs/TODO.md:302`
(narrative inside another goal's entry). Both files are digest-identical to the head (§1), so no
TODO/CLOSED-TODO edit was required or made.

Not touched, verified by `git status --porcelain -uall`: `scripts/**` (installer byte-identical),
`daemon/native/**`, `daemon/internal/launchercore/**`, `daemon/internal/platform/**`,
`daemon/internal/linuxpreflight/**`, `daemon/internal/app/**`, `daemon/internal/state/**`,
`daemon/e2e/**`, `control/**`, `contracts/**`, `docs/design/**`. Workspace `root`/`seed_root`
semantics are untouched: the only change to `Load` is the empty-`state_dir` branch, and `Validate`
is byte-identical to the head.

## 8. Scoped simplification review

Candidates added by this diff and their disposition:

- `load(path, installed bool)` shared body — retained: the contract mandates one shared body; the
  flag is the minimal divergence (decode, extra-object check and `Validate` stay single-sourced).
  Splitting it into two wrappers would duplicate the decode block.
- `windowsStateDirBase` + `isASCIILetter` — retained: the helper owns the windows acceptance shape
  (validate raw, then trim) and the ASCII-only drive-letter rule; inlining would spread that
  predicate over two branches of the resolver switch. Both are exercised through the 11 windows
  rows above.
- `hostDefaultStateDir` / `resolvedHostDefault` test helpers — retained (one per test package,
  ~12 lines each): a shared test helper would need a new package outside this goal's owned files.
- `LoadInstalled` — retained per the contract; no option-style surface was added.
- No abstraction was removed and no test was weakened; "nothing unnecessary found" beyond the
  candidates listed, each retained with the responsibility it owns.
