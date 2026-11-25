# rf-daemon-state-dir-default — evidence record

Goal `rf-daemon-state-dir-default`, contract `goal-rf-daemon-state-dir-default.md` (GDD state
root, external to the repository). Worktree branch `refactor/rf-daemon-state-dir-default`; every
provenance anchor is therefore a file sha256, never a revision. Rulings: Q-A strict
(`-print-state-dir` loads via `config.LoadInstalled`; the installer script stays byte-identical),
Q-B confirmed (no extra `app.Run` guard keyed on harness kind), Q-C confirmed (darwin row
logic-only, never host-verified, never "supported"). Round-1 conductor ruling (PR #286; Sol
`286-opus-review-1.md`, DS `286-ds-audit-1.md`): R1-R5 applied — M1-M3/M7 re-run and re-quoted on
the delivered bytes, the installed-owner-command refusal test added and pinned by M8 (R2), the §2
`" "` anchor corrected (R3), the two unexported-func doc comments removed (R4), the `path`
parameter renamed `configPath` (R5); §9 records the two accepted residuals.
Plan: `docs/plans/2026-11-25-rf-daemon-state-dir-default-plan.md`, sha256
`0685fced552eaa48d135bd34595d90f3048b9c770ecd8bffb162182f45e6c594`.

Line numbers below are at the delivered bytes.

## 1. Delivered bytes (sha256-only provenance)

| File | Dispatch head | Delivered |
| --- | --- | --- |
| `daemon/internal/config/config.go` | `da3976c5f0e24477f7b530fde55d08c4b7098d324e1f7ebea85870ab94191f6b` | `fd4eae8725b9f30f70b4e9cd6069583e1f1b2f120383c5d1c2175364c172e215` |
| `daemon/internal/config/config_test.go` | `a8f50257c27377af2cf1d9bf4a4fdfc09a88bf70f079b9b5db22352fddf60321` | `558c1b8346e778708310cb67614d8cc351de33fb23938b797a151ee3f25d3949` |
| `daemon/cmd/symmetry-daemon/main.go` | `80a7a7ff32fffc9aad3fe1d6272d25660ef664f3157de498b8d9bb133423147b` | `651124f2f942daf2b25055ade25fd936a9395e3693f2362ba93c4d5f5a803659` |
| `daemon/cmd/symmetry-daemon/main_test.go` | `37124bf55d6b7156da7851339f110f4f7376058b959c918ae8a510322aae0499` | `0fe87d7402f8ad3565259361a17af2ea68c327389d8244335c1cfd3b0f2a11a0` |
| `docs/runbooks/control-plane-and-daemon.md` | `f29b013f62dd69ea0e0fe1729a4799f897c8ad45ee404320b9a69024f9eb907c` | `efd66e1f98e29970615673a17415d7ef69ef8976faa0babf4dbd060dbcfcd042` |
| `docs/TODO.md` | `d1b95fc7a0103562d4c520c315b29aacbe7164ccb9365223aeee14f392e5790d` | unchanged (same digest) |
| `docs/archive/CLOSED-TODO.md` | `9e79fe6cee3cba6c8dcb6d2ad97fb90a096c7d653ad716fd9b9f2cedefe29f7c` | unchanged (same digest) |
| `scripts/install-linux-owner.sh` | — | untouched: `git diff --stat -- scripts/` and `git status --porcelain -- scripts/` are both empty |
| `daemon/internal/state/state_unix.go` | `119b3a04b7642f50dc70ef6479e1e16643a27122126d3fa25ec90c82eb2f7ed9` | unchanged (same digest); mutated only in the scratch copy of §6 |

R1-R5 deltas on the reviewed head bytes: `config.go` lost the two unexported-func doc comments
(R4) and its loader parameter `path` became `configPath` (R5); `main_test.go` gained the `io`
import, the three-command refusal test and its `captureStdout` helper (R2); this record carries the
refreshed citations, the M8 row and §9. `git status --porcelain` lists exactly `config.go`,
`main_test.go` and this record as modified over the apply commit; no other path changed.

## 2. Clause census and dispositions (re-located by content, delivered bytes)

| Contract anchor (head) | Delivered bytes | Disposition |
| --- | --- | --- |
| `config.go:30` `StateDir` | `config.go:31` `json:"state_dir"` (no omitempty) | untouched |
| `config.go:150-177` `Load` | `:202-239` private `load`; thin `Load` at `:192`, `LoadInstalled` at `:198`, resolver at `:149-173` | default resolved between decode and `Validate` |
| `config.go:196` `requireNotEmpty("state_dir", …)` | `:258` | unchanged: explicit `" "` and programmatic values still refused |
| `config.go:484-489` helper | `:546-551` | unchanged |
| `main.go:131-157` print | `:131-157`; loader swap at `:147` | `config.LoadInstalled`; `:159-216` normalization + symlink rejection unchanged |
| `main.go:240-281` owner commands | `:240-281`; loader swap at `:250` | `config.LoadInstalled` |
| `main.go:70-93` plain start | `:82` `config.Load` | unchanged: default-aware |
| `config_test.go:453-457` `" "` row | `:454-458` | unchanged, still green |
| `main_test.go:56-94` print rejection table (`""` at `:70`) | `:59-97` (`""` at `:73`) | unchanged and green; `""` now refuses at the loader |
| runbook `:202-204`, `:216`, `:245-248` | `:202-204`, `:216`, `:245-248` | unchanged; new paragraph `:250-262` |

## 3. Resolver table (P1-P3, P8) — inputs, outputs, errors

`defaultStateDir(goos, getenv)` rows, all green in `TestDefaultStateDirResolvesPerGOOS`
(`config_test.go:502-555`), WSL Ubuntu-24.04 (Go 1.27.0) and the Windows host (Go 1.27.4):

| GOOS | env | result |
| --- | --- | --- |
| linux | `XDG_STATE_HOME=/srv/state`, `HOME=path-sha256:f9e409c5c16ecfdd5883da4f3ad045b4af5677740c9e703ad2d47ba231718350` | `/srv/state/symmetry` |
| linux | `XDG_STATE_HOME=/srv/state` | `/srv/state/symmetry` |
| linux | `XDG_STATE_HOME=/srv/state/`, `HOME=path-sha256:f9e409c5c16ecfdd5883da4f3ad045b4af5677740c9e703ad2d47ba231718350` | `/srv/state/symmetry` |
| linux | `XDG_STATE_HOME=state` (relative), `HOME=path-sha256:f9e409c5c16ecfdd5883da4f3ad045b4af5677740c9e703ad2d47ba231718350` | `path-sha256:3d36a489214da59c872a9e533701793d85420ed081ad7b597b31773850fc4d83` |
| linux | `XDG_STATE_HOME=""`, `HOME=path-sha256:f9e409c5c16ecfdd5883da4f3ad045b4af5677740c9e703ad2d47ba231718350` | `path-sha256:3d36a489214da59c872a9e533701793d85420ed081ad7b597b31773850fc4d83` |
| linux | `HOME=path-sha256:f9e409c5c16ecfdd5883da4f3ad045b4af5677740c9e703ad2d47ba231718350` | `path-sha256:3d36a489214da59c872a9e533701793d85420ed081ad7b597b31773850fc4d83` |
| linux | `XDG_STATE_HOME=state`, `HOME=home/u` (relative) | error naming `$HOME` |
| linux | `HOME="  "` (blank) | error naming `$HOME` |
| linux | (both unset) | error naming `$HOME` |
| darwin | `HOME=path-sha256:1a76067a3053a7affbd59fa9898bc14352ec2cf557f1d624cfa2788e984bbcf8` | `path-sha256:078360d99ee56f79906cd569bf3a2990fa87c9368620711ee7d408cb0245dcbf` |
| darwin | `HOME=Users/u` (relative) | error naming `$HOME` |
| darwin | (unset) | error naming `$HOME` |
| windows | `LOCALAPPDATA=path-sha256:dcc742e9c2353fd8b26e137dc579cddf0ff14d143b865e03d4f0a0b21fdc4889` | `path-sha256:dfb8e52beeff3744d1192b58c7631dbdc20e7e0f84461ef46293ef2547d5d5ef` |
| windows | `LOCALAPPDATA=path-sha256:05b7c4e1686fc3427ea63c74239099865d0bcd47a138cd193a31cbc6a4202016` | `path-sha256:effe3f1a2d35155c84a24391b5100444361b6e73e66fef9ee51d7aeede71a308` |
| windows | `LOCALAPPDATA=path-sha256:b3d53b4021875233634c0908a3fb47a40e8c5ffeed55abe6e694c74e9902c1b9` | `path-sha256:dfb8e52beeff3744d1192b58c7631dbdc20e7e0f84461ef46293ef2547d5d5ef` |
| windows | `LOCALAPPDATA=path-sha256:28f709d79730058e2a461518e34126da18ceda07729e792c92f7ca1251e730bf` | `path-sha256:2a240c1fbd1b54538c56c7b7bb1266ebd0aca653796b3f34fd135318f7729bf6` |
| windows | `LOCALAPPDATA=path-sha256:8545a81f99f36eda523c5e88e45567d21aaf5ea733e5595596cb07dd7364e8e4` | `path-sha256:2a240c1fbd1b54538c56c7b7bb1266ebd0aca653796b3f34fd135318f7729bf6` |
| windows | `LOCALAPPDATA=path-sha256:05e24481d49af5ff4b88980216126fdaa1506fb0e7dcdbc60d64e5bcc5a61312` | `path-sha256:c4ce953140e2e022cc32b6ea7e94c3caf71187a882821ebab8e87f3ee1441311` |
| windows | `LOCALAPPDATA` unset | error naming `%LOCALAPPDATA%` |
| windows | `LOCALAPPDATA="  "` | error naming `%LOCALAPPDATA%` |
| windows | `APPDATA`, `USERPROFILE` set, `LOCALAPPDATA` unset | error naming `%LOCALAPPDATA%` (never `%AppData%`) |
| windows | `LOCALAPPDATA=Local\AppData` | error naming `%LOCALAPPDATA%` |
| windows | `LOCALAPPDATA=C:Local` (drive-relative) | error naming `%LOCALAPPDATA%` |
| windows | `LOCALAPPDATA=\\` (bare prefix) | error naming `%LOCALAPPDATA%` |
| freebsd | `HOME=path-sha256:f9e409c5c16ecfdd5883da4f3ad045b4af5677740c9e703ad2d47ba231718350` | error naming GOOS `freebsd`, no path |

Every non-windows success row additionally asserts `path.IsAbs(result)`; every failure row asserts
the error text, so no probe output can carry a relative or cwd-derived path.

`Load`-level rows (`TestLoadDefaultsAndRefusesStateDir`, `:557-613`):
explicit `path-sha256:cc65a3cf01d383c4c711ecb8654f50a81c4734aa094917dbd23ddcc0def09716` with `HOME`/`XDG_STATE_HOME`/`LOCALAPPDATA` all cleared loads verbatim
(P4 positive); an absent key with the same cleared env fails `resolve default state_dir: …` naming
the host component, `$HOME` or `%LOCALAPPDATA%` (P4 negative); an absent key with the host
component present resolves the host default (P7 first half).

`LoadInstalled` rows (`TestLoadInstalledRequiresAnExplicitStateDir`, `:615-657`): explicit loads;
`absent` and `""` both return exactly
`state_dir must be explicitly configured for the installed owner path` even with a resolvable
default in the environment; `" "` stays explicit and is refused by validation
(`state_dir must not be empty`).

The three installed-owner commands re-impose that refusal end-to-end:
`TestConfiguredOwnerCommandsRefuseAnAbsentStateDirWithoutOutput` (`main_test.go:165-186`) drives
`runConfiguredOwnerCommand` for `-verify-legacy-owner-drained`, `-verify-linux-owner-install` and
`-recover-containment-only` with the absent-`state_dir` fixture and asserts the
explicit-requirement error (`:179`) and zero captured stdout (`:182`); M8 is the discriminating
mutant.

## 4. Characterization baseline and before/after transcripts

Baseline on a scratch copy of the dispatch head, `HOME=path-sha256:f9e409c5c16ecfdd5883da4f3ad045b4af5677740c9e703ad2d47ba231718350`, `XDG_STATE_HOME=/srv/state` in the
process env, configs with `state_dir` absent / `""` / `" "` / `path-sha256:cc65a3cf01d383c4c711ecb8654f50a81c4734aa094917dbd23ddcc0def09716`:

```
explicit: Load state_dir: "path-sha256:cc65a3cf01d383c4c711ecb8654f50a81c4734aa094917dbd23ddcc0def09716"
absent: Load error: state_dir must not be empty
empty: Load error: state_dir must not be empty
space: Load error: state_dir must not be empty
absent print: exit=1 stdout bytes=0 stderr=[-print-state-dir: load daemon config: state_dir must not be empty]
explicit print: exit=0 stdout=[path-sha256:cc65a3cf01d383c4c711ecb8654f50a81c4734aa094917dbd23ddcc0def09716] stderr=[]
owner -verify-linux-owner-install absent: exit=1 stdout=[] stderr=[-verify-linux-owner-install: load config: state_dir must not be empty]
owner -verify-legacy-owner-drained absent: exit=1 stdout=[] stderr=[-verify-legacy-owner-drained: load config: state_dir must not be empty]
owner -recover-containment-only absent: exit=1 stdout=[] stderr=[-recover-containment-only: load config: state_dir must not be empty]
```

Apply-stage build, same probe configs, binary built from the worktree (WSL `-u accept`, env
`HOME=path-sha256:f9e409c5c16ecfdd5883da4f3ad045b4af5677740c9e703ad2d47ba231718350`, `XDG_STATE_HOME=/srv/state`); R1-R5 changes no
observable behavior here (`main.go` is byte-identical and the R4/R5 `config.go` edits are comments
and one parameter name):

```
print absent: exit=1 stdout-bytes=0 stderr=[-print-state-dir: load daemon config: state_dir must be explicitly configured for the installed owner path]
print explicit-empty: exit=1 stdout-bytes=0 stderr=[-print-state-dir: load daemon config: state_dir must be explicitly configured for the installed owner path]
print explicit: exit=0 stdout-bytes=17 stdout=[path-sha256:cc65a3cf01d383c4c711ecb8654f50a81c4734aa094917dbd23ddcc0def09716] stderr=[]
owner -verify-linux-owner-install absent: exit=1 stdout=[] stderr=[-verify-linux-owner-install: load config: state_dir must be explicitly configured for the installed owner path]
owner -verify-legacy-owner-drained absent: exit=1 stdout=[] stderr=[-verify-legacy-owner-drained: load config: state_dir must be explicitly configured for the installed owner path]
owner -recover-containment-only absent: exit=1 stdout=[] stderr=[-recover-containment-only: load config: state_dir must be explicitly configured for the installed owner path]
```

The plain-start half (P7): `TestLoadDefaultStateDirCreatesAPrivateStore` (`main_test.go:115-137`)
resolves the host default through `Load`, opens it with `state.New`, and asserts the leaf is a
directory with mode 0700 on unix (on Windows the assertion is `state.New` itself: its creation path
applies and verifies the account-only DACL via `secureWindowsPath`, and reverifies before
returning). `TestLoadDefaultStateDirReSecuresAPermissiveStore` (`:139-163`) pre-creates the leaf at
0755 and asserts `state.New` re-secures it to 0700; it skips on Windows because Go cannot express
POSIX mode bits there. Both were green on both hosts in the §5 gates.

P6 rows re-ran unchanged: `TestPrintStateDirResolvesAgainstRootWithoutCreatingState` (exit 0) and
the full `TestPrintStateDirRejectsTraversalAndSymlinkWithoutOutput` table (exit 0, zero stdout on
every row, including `""` and the symlinked-component row).

Verbatim per-row probe output (R1-R5 bytes): on WSL, `grep -c -- '--- PASS'` = 35 with 0 FAIL for
`go test -v -count=1 -run 'TestDefaultStateDirResolvesPerGOOS|TestLoadDefaultsAndRefusesStateDir|TestLoadInstalledRequiresAnExplicitStateDir' ./internal/config/`
(`ok 0.056s`) and 21 with 0 FAIL for
`-run 'TestPrintStateDir|TestLoadDefaultStateDir|TestConfiguredOwnerCommands' ./cmd/symmetry-daemon/`
(`ok 0.076s`) — 25/25 `defaultStateDir` rows, 3/3 `Load` subtests, 4/4 `LoadInstalled` subtests,
5/5 print/store tests, 3/3 owner-command refusal subtests. The Windows host runs the same two
commands to 35/0 (`ok 0.152s`) and 5/0 (`ok 0.264s`); the Linux-only print rows and the POSIX
re-secure row skip there by design.

## 5. Host gates (R1-R5 bytes)

Windows host (this workstation, Go 1.27.4 windows/amd64), working directory `daemon/`:

| Gate | Command | Exit | Result |
| --- | --- | --- | --- |
| format | `gofmt -l .` | 0 | no file listed |
| vet | `go vet ./...` | 0 | clean |
| focused suites | `go test -count=1 ./internal/config/ ./cmd/symmetry-daemon/` | 0 | `ok config 1.040s`, `ok cmd/symmetry-daemon 0.458s` |

WSL Ubuntu-24.04 (`-u accept`, Go 1.27.0 linux/amd64), working directory `daemon/`:

| Gate | Command | Exit | Result |
| --- | --- | --- | --- |
| linux vet | `GOOS=linux go vet ./internal/config/ ./cmd/symmetry-daemon/` | 0 | clean |
| focused suites | `go test -count=1 ./internal/config/ ./cmd/symmetry-daemon/` | 0 | `ok config 0.295s`, `ok cmd/symmetry-daemon 0.079s` |

No scratch branch, no credentialed run, no e2e suite; the `daemon` and `daemon-windows` CI jobs
remain the merge gates.

## 6. Mutants (M1-M8)

Each mutant was applied to a scratch copy of the delivered bytes outside the goal worktree with the
`apply_patch` editor, probed, then restored from the worktree and digest-verified (`restore=EQ` per
row; mutated and restored digests per row, delivered digests in §1). The delivered files were never
mutated. All eight rows were re-run on the R1-R5 bytes: M1-M6 and M8 in WSL (`daemon` job
semantics); M7 on the Windows host (`daemon-windows` job semantics). Red line numbers cite the
delivered `config_test.go` (`558c1b83…`) and `main_test.go` (`0fe87d74…`) bytes.

| id | mutation | probe (red) | observed red (verbatim) | mutated → restored digest |
| --- | --- | --- | --- | --- |
| M1 | resolve the default unconditionally (override an explicit `state_dir`) | `go test -count=1 -run TestLoadDefaultsAndRefusesStateDir ./internal/config/` | `config_test.go:570: Load() explicit state_dir error = resolve default state_dir: state_dir default requires $HOME to be set to an absolute path` | `config.go 0c060adb…` → `fd4eae87…` |
| M2 | accept a relative `XDG_STATE_HOME` | `go test -count=1 -run TestDefaultStateDirResolvesPerGOOS ./internal/config/` | `config_test.go:548: defaultStateDir("linux") = "state/symmetry", want "path-sha256:3d36a489…"`; `config_test.go:540: defaultStateDir("linux") error = <nil>, want one naming "$HOME"` | `config.go 1fe6862b…` → `fd4eae87…` |
| M3 | fall back to cwd (`os.Getwd`) when `HOME` is missing | `go test -count=1 -run TestDefaultStateDirResolvesPerGOOS ./internal/config/` | `config_test.go:540: defaultStateDir("linux") error = <nil>, want one naming "$HOME"` ×3 (relative, blank, missing home) | `config.go c633dc51…` → `fd4eae87…` |
| M4 | `-print-state-dir` loads through default-aware `Load` (`main.go:147`) | `go test -count=1 -run TestPrintStateDirRefusesAnAbsentStateDirWithoutOutput ./cmd/symmetry-daemon/` | `main_test.go:108: print command error = <nil>, want the installed-owner explicit requirement` | `main.go 3315191b…` → `651124f2…` |
| M5 | `-print-state-dir` skips `rejectLinuxStateDirSymlinks` | `go test -count=1 -run TestPrintStateDirRejectsTraversalAndSymlinkWithoutOutput ./cmd/symmetry-daemon/` | `main_test.go:90: unsafe StateDir was printed` (symlinked-component row) | `main.go 1431943b…` → `651124f2…` |
| M6 | Store directory created/secured with 0755 (`ensurePrivateDirectory`, `secureDirectory`, `verifyPrivateDirectory`) | `go test -count=1 -run 'TestLoadDefaultStateDirCreatesAPrivateStore\|TestLoadDefaultStateDirReSecuresAPermissiveStore' ./cmd/symmetry-daemon/` | `main_test.go:135: resolved state dir mode = -rwxr-xr-x, want 0700`; `main_test.go:161: re-secured state dir Lstat = … want mode 0700` | `state.go 5ee142a7…` → `0c924deb…`; `state_unix.go 37097091…` → `119b3a04…` |
| M7 | windows branch falls back to `%AppData%` when `LOCALAPPDATA` is unset (Windows host) | `go test -count=1 -run TestDefaultStateDirResolvesPerGOOS ./internal/config/` | `config_test.go:540: defaultStateDir("windows") error = <nil>, want one naming "%LOCALAPPDATA%"` (`windows lookalike variables are refused`) | `config.go bb6d855c…` → `fd4eae87…` |
| M8 | installed-owner commands load through default-aware `Load` (`main.go:250`) | `go test -count=1 -timeout 120s -run TestConfiguredOwnerCommandsRefuseAnAbsentStateDirWithoutOutput ./cmd/symmetry-daemon/` | `main_test.go:179: -verify-legacy-owner-drained error = verify private state before legacy owner drain: StateDir ancestor is not immutable against untrusted identities: directory /tmp has an untrusted owner, group, or ACL writer: mode grants write access to an untrusted group or other user, want the installed-owner explicit requirement`; `main_test.go:179: -verify-linux-owner-install error = Linux owner preflight process does not run with the exact configured non-root A UID/GID and distinct B identity, want the installed-owner explicit requirement`; `main_test.go:179: -recover-containment-only error = <nil>, want the installed-owner explicit requirement` | `main.go d24a5953…` → `651124f2…` |

All eight probes exited 1. M8's mutated `main.go` hashed `d24a59535c3b4dc8447a1e31ed2b00fdbb882f00365d95dc2e563648b5fc4adc`
and its restored file `651124f2f942daf2b25055ade25fd936a9395e3693f2362ba93c4d5f5a803659`, byte-identical
to the delivered digest in §1 (`restore=EQ`). M4 reddens because the probe's resolvable default is
printed instead of refused; M5 because the symlink rejection is what makes the existing negative
table pass; M6 because both P7 rows assert 0700 and the mutant leaves the leaf group/world-readable;
M8 because all three owner commands proceed past the loader (two through Linux preflight failures,
one returning nil). No mutant reddened a row outside its expected probe.

## 7. TODO census and out-of-scope confirmations

At the dispatch head:
`git grep -n -i 'xdg\|localappdata\|state_dir\|print-state' docs/TODO.md docs/archive/CLOSED-TODO.md`
returns no hits; `git grep -n 'StateDir'` on the same two files returns only `docs/TODO.md:302`
(narrative inside another goal's entry). Both files are digest-identical to the head (§1), so no
TODO/CLOSED-TODO edit was required or made.

Not touched, verified by `git status --porcelain`: `scripts/**` (installer byte-identical),
`daemon/native/**`, `daemon/internal/launchercore/**`, `daemon/internal/platform/**`,
`daemon/internal/linuxpreflight/**`, `daemon/internal/app/**`, `daemon/internal/state/**`,
`daemon/e2e/**`, `control/**`, `contracts/**`, `docs/design/**`. Workspace `root`/`seed_root`
semantics are untouched: the only change to `Load` is the empty-`state_dir` branch, and `Validate`
is byte-identical to the head.

## 8. Scoped simplification review

Candidates added by this diff and their disposition:

- `load(configPath string, installed bool)` shared body — retained: the contract mandates one
  shared body; the flag is the minimal divergence (decode, extra-object check and `Validate` stay
  single-sourced). Splitting it into two wrappers would duplicate the decode block.
- `windowsStateDirBase` + `isASCIILetter` — retained: the helper owns the windows acceptance shape
  (validate raw, then trim) and the ASCII-only drive-letter rule; inlining would spread that
  predicate over two branches of the resolver switch. Both are exercised through the 12 windows
  rows above.
- `hostDefaultStateDir` / `resolvedHostDefault` test helpers — retained (one per test package,
  ~12 lines each): a shared test helper would need a new package outside this goal's owned files.
- `captureStdout` test helper (R2) — retained: the three installed-owner commands write to
  `os.Stdout` directly, so the zero-stdout half of their refusal contract needs the swap-and-pipe
  seam; no production surface was changed to make it testable.
- `LoadInstalled` — retained per the contract; no option-style surface was added.
- No abstraction was removed and no test was weakened; "nothing unnecessary found" beyond the
  candidates listed, each retained with the responsibility it owns.

## 9. Residual risks (recorded by the round-1 ruling, no change)

- **`LoadInstalled` error order** — the explicit requirement is returned before `Validate`, so a
  config that is both invalid and `state_dir`-missing reports the `state_dir` message first.
  Fail-closed-first is deliberate; no test asserted the old order.
- **Windows UNC canonicalisation looseness** — `windowsStateDirBase` accepts a server-only `\\x`
  prefix (content after the two separators) and lets a non-canonical value with parent components
  pass through. Unreachable for a real `%LOCALAPPDATA%` and not a security boundary (the value
  comes from the user's own environment).
