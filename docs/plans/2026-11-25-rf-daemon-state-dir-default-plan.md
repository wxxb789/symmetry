# rf-daemon-state-dir-default — implement plan

Contract: `goal-rf-daemon-state-dir-default.md` (GDD state root, external to the
repository). Branch `refactor/rf-daemon-state-dir-default`. Conductor rulings
Q-A (strict: `-print-state-dir` is the installed probe and loads via
`config.LoadInstalled`; `scripts/install-linux-owner.sh` stays byte-identical),
Q-B (confirmed: no extra `app.Run` guard keyed on harness kind; the strict
service `ExecStartPre` and the installer probe are the installed proof) and Q-C
(confirmed: the darwin row is logic-only and table-tested; the runbook keeps the
macOS deferral and never calls it supported).

The accepted plan is the contract; this file resolves only the implementation
choices it leaves open, the clause census at the dispatch head, the probes and
the out-of-scope set. Provenance is sha256-only; every head anchor below is a
file digest, not a revision.

## 1. Clause census at the dispatch head (re-located by content)

Dispatch-head digests: `config.go` `da3976c5`, `config_test.go` `a8f50257`,
`main.go` `80a7a7ff`, `main_test.go` `37124bf5`, runbook `f29b013f`,
`docs/TODO.md` `d1b95fc7`, `docs/archive/CLOSED-TODO.md` `9e79fe6c`.

| Contract anchor | Head bytes | Content | Disposition |
| --- | --- | --- | --- |
| `config.go:30` | `:30` | `StateDir string \`json:"state_dir"\``, no omitempty | untouched |
| `config.go:150-177` `Load` | `:150-177` | open / strict decode / one-object check / `Validate` | converted to shared `load(path, installed)`; default resolved between decode and `Validate` |
| `config.go:196` | `:196` | `requireNotEmpty("state_dir", …)` inside `Validate` | unchanged (programmatic `Config` and `app.Run` keep refusing) |
| `config.go:484-489` helper | `:484-489` | `requireNotEmpty` | unchanged |
| `main.go:131-157` `runPrintStateDirCommand` | `:131-157`, `Load` at `:147` | installer probe | `LoadInstalled`; normalization + symlink rejection `:159-216` unchanged |
| `main.go:240-281` `runConfiguredOwnerCommand` | `:240-281`, `Load` at `:250` | three installed-owner commands | `LoadInstalled` |
| `main.go:26-32` dispatch | `:26-32`, `:71-84` | print / owner / plain start | untouched; plain start keeps `Load` |
| `config_test.go:453-457` | `:453-457` | `state_dir=" "` refused | unchanged and still green |
| `main_test.go:56-94` table, `""` `:70` | `:56-94`, `:70` | print rejection rows | unchanged; `""` now refuses at the loader with the explicit-requirement error |
| `main_test.go:96-110` helper | `:96-117` | `writeStateDirCommandConfig` | reused by the new probes |
| runbook `:202-204` macOS deferral, `:216` example, `:245-248` | `:202-204`, `:216`, `:245-248` | deferral, explicit example, fixture note | unchanged; the new paragraph follows `:248` |

`config.Load` production callers at the head: `main.go:82` (plain start, stays
`Load`), `main.go:147` and `main.go:250` (both swap), and
`daemon/e2e/pi-control-witness/main.go:53` (out of scope; its generated configs
always write an explicit `state_dir`).

TODO census at the dispatch head:
`git grep -n -i 'xdg\|localappdata\|state_dir\|print-state' docs/TODO.md docs/archive/CLOSED-TODO.md`
returns no hits; the broader `git grep -n 'StateDir'` on the same two files
returns only `docs/TODO.md:302` (narrative inside another goal's entry, not a
tracked item). No TODO/CLOSED-TODO edit.

## 2. Choices the contract leaves open

1. **Error wording** — each error names the missing component:
   `state_dir default requires $HOME to be set to an absolute path` (linux,
   darwin), `state_dir default requires %LOCALAPPDATA% to be set to an absolute
   path` (windows), `state_dir default is not defined for GOOS %q` (any other
   GOOS). `Load` wraps the resolver failure as `resolve default state_dir: %w`;
   `LoadInstalled`'s empty-value error is the contract's exact string,
   unwrapped.
2. **Windows acceptance shape** — the raw value is validated first
   (drive-absolute: ASCII letter + `:` + `\` or `/` at byte 2; UNC: `\\` prefix
   with content after it), then trailing separators are trimmed; so `C:`
   (drive-relative) is refused while `path-sha256:28f709d79730058e2a461518e34126da18ceda07729e792c92f7ca1251e730bf` trims to a root and joins clean.
   The join uses a literal `\` so every windows row is testable off-Windows.
3. **Shared body** — `Load`/`LoadInstalled` delegate to unexported
   `load(path string, installed bool)`; the only divergence is the empty
   `StateDir` branch. `LoadInstalled` is the only new exported symbol; its doc
   states the fail-closed reason (one hazard line, not a step label).
4. **Test seam** — `defaultStateDir(goos, getenv)` takes the environment as a
   function; `Load` passes `runtime.GOOS` and `os.Getenv`. Load-level rows use
   written JSON fixtures plus `t.Setenv`, so the windows and darwin rows run on
   both hosts through GOOS injection.
5. **P7 placement** — the Store probe lives in the owned `main_test.go`: unix
   rows assert the 0700 mode bits on the resolved default, the windows row's
   assertion is `state.New` itself (its creation path applies and verifies the
   account-only DACL via `secureWindowsPath`); the 0755 negative skips on
   Windows because Go cannot express POSIX mode bits there.

## 3. Baseline observed before the change (scratch copy of the head)

Probe configs omit `state_dir` (or set `""` / `" "` / explicit) with
`HOME=path-sha256:f9e409c5c16ecfdd5883da4f3ad045b4af5677740c9e703ad2d47ba231718350`, `XDG_STATE_HOME=/srv/state` in the process env; base bytes:

- `Load` explicit → `Load state_dir: "path-sha256:cc65a3cf01d383c4c711ecb8654f50a81c4734aa094917dbd23ddcc0def09716"`.
- `Load` absent / `""` / `" "` → `Load error: state_dir must not be empty`.
- `-print-state-dir` absent → exit 1, 0 stdout bytes,
  stderr `-print-state-dir: load daemon config: state_dir must not be empty`.
- `-print-state-dir` explicit → exit 0, stdout `path-sha256:cc65a3cf01d383c4c711ecb8654f50a81c4734aa094917dbd23ddcc0def09716`.

After the change: `Load` absent/`""` resolves the per-user default; `" "` and
explicit stay byte-identical; both `-print-state-dir` rows refuse an absent
`state_dir` at the loader (the explicit row unchanged). The discriminating
reddens are mutants M1-M7.

## 4. Probes and evidence plan

- P1-P4, P8: `defaultStateDir` table rows (injected GOOS + env) in
  `config_test.go`; P4 also has `Load` explicit-vs-absent rows with all three
  environment components cleared.
- P5: `LoadInstalled` explicit/absent/`""`/`" "` rows in `config_test.go`;
  `-print-state-dir` absent-with-resolvable-default row in `main_test.go` and
  the built-binary transcript in the evidence record (stdout bytes = 0, exit
  1).
- P6: existing print rows (`TestPrintStateDirResolvesAgainstRootWithoutCreatingState`,
  `TestPrintStateDirRejectsTraversalAndSymlinkWithoutOutput`) re-run unchanged.
- P7: the two Store probes in `main_test.go` (fresh default → 0700 / windows
  apply+verify; pre-existing 0755 → re-secured to 0700).
- Gates: Windows host `gofmt -l` / `go vet ./...` /
  `go test -count=1 ./internal/config/ ./cmd/...`; WSL Ubuntu-24.04
  `GOOS=linux go vet ./...` + the same `go test` (real Linux print and Store
  rows). Mutants M1-M6 on a WSL scratch copy, M7 on a Windows scratch copy;
  each row records the reddening probe and the restored file's sha256.
- Evidence record: `docs/evidence/rf-daemon-state-dir-default-2026-11-25.md` —
  resolver table (inputs → outputs → errors), verbatim probe outputs, the seven
  mutant rows with restored digests, both gate transcripts, sha256-only
  provenance, no host path carrying a user profile name.

## 5. Out of scope (with reason)

- Workspace `root`/`seed_root` (stay explicit and required); the owner-config
  format and the installer script bytes; wrapper/launcher/anchor/clone-cleanup;
  `daemon/internal/app/**` (no harness-kind guard, Q-B); `daemon/e2e/**`;
  `control/**`, `contracts/**`, `docs/design/**` (no `state_dir` requirement to
  fix there); e2e/AC/devcontainer flows; macOS host verification (no host).
