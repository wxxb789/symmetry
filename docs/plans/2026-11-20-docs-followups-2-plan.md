# docs-followups-2 plan — record retro-discovered follow-ups and the path-triggered pre-PR verification rule

Contract: `goal-docs-followups-2.md` (GDD, 2026-11-20). Baseline for every coordinate in this
plan and in the delivered entries: `origin/main` at `978abba93e93c56f87d9f36811ecd7739ef1f719`
(`978abba`, the worktree's HEAD). Docs-only increment: `docs/TODO.md`, `docs/design/quality.md`
and this plan file.

## Deliverable shape

1. `docs/TODO.md`: one verified entry per contract item whose claim reproduces at `978abba`
   (items a, b, c, e), plus a stated reason for any dropped item (item d), under a new
   `## Retro review follow-ups (second pass, 2026-11-20)` heading placed immediately after the
   first pass's `## Retro review follow-ups` section, so the two retro sweeps read as one
   provenance group without merging their records.
2. `docs/design/quality.md`: the owner-adopted (Q10C) path-triggered pre-PR verification rule,
   written as an `Amendment (...)` paragraph in the "Commands and gates" section — the file's
   existing shape for later owner decisions (see the 2026-11-17 CI-consolidation amendment) —
   with no host path and no new gate.
3. No source, test, CI or archive edit: the recorded defects stay unfixed (contract clause 4).

## Item map (verified at `978abba`, evidence in `answer.md`)

| Item | Claim at origin/main | Verification | Verdict |
| --- | --- | --- | --- |
| a | Go `protocol.CanonicalizeJSON` writes C0 escapes with lowercase hex (`\u001f`), Jason 1.4.5 writes uppercase (`\u001F`), so the same value has two canonical digests | scratch Go test in `daemon/internal/protocol/` (deleted after the run) + `elixir -pa …/jason/ebin` probe in WSL | write |
| b | `_WINDOWS_TAIL` (no bare-backslash exclusion) absorbs a following JSON `\n` escape and the text behind it, while `_POSIX_BYTE` (bare backslash excluded) does not | tool run over two scratch JSONL inputs (Windows vs POSIX spelling), digest identifies the swallowed span | write |
| c | legacy `?owner=agent:unrecorded` bookmarks match no row after #111 (`db31838`); the label still renders "Agent" | scratch Node ESM probe importing the repo's `filters.ts`/`presentation.ts` through a type-stripping resolve hook | write |
| d | retro G4 contract's PR↔SHA mapping shuffled | `gh pr view` for #79/#82/#89/#90; `grep` over `docs/` for those PR numbers and for the four merge SHAs | drop: no repo doc repeats the mapping, and the four `#79 (21cf326)` references in `docs/` are correct |
| e | Windows-host `actionlint` with ShellCheck over `ci.yml` does not terminate within 10 minutes | `timeout 620 actionlint -color -shellcheck=shellcheck .github/workflows/ci.yml` on this host with the `mise`-installed 1.7.12 binary; substitute run with `-shellcheck= -pyflakes=` | write as a decision note (see below) |

## Exact edge-case semantics the entries state

- **(a)** Lowercase-vs-uppercase is not a defect inside either language: each language is
  self-consistent (`{"value":"\u001F"}` and `{"value":"\u001f"}` canonicalize to the same bytes
  in Go). The recorded defect is the cross-language pair: `RequestHash.canonical/1`
  (`control/lib/symmetry_control/request_hash.ex:5`, `Jason.encode!/1`) vs
  `CanonicalizeJSON` (`daemon/internal/protocol/goal_subject.go:102`) over the same logical
  envelope. Reachability is stated with evidence: the daemon's text validation accepts any valid
  UTF-8 (`goal_validation.go:66-76`), and the escaping test that exists pins only `\u0000`, whose
  escape carries no hex letter (`goal_subject_test.go:64-73`). Fix direction: pick one canonical
  case and pin both languages against one shared fixture; the entry does not pick the winner
  because the choice is a contract decision.
- **(b)** The swallowed span is identified by digest, not by prose: the emitted token hashes the
  matched text (path bytes, then the escape, then the word behind it), so the entry quotes both the
  emitted digest and the digest the path-only render would carry. The exact byte spellings of the
  two scratch inputs live outside the repository, in this run's scratch files (`%TEMP%/gdd-docs2/`)
  and recorded write calls, not in `answer.md`, because a faithful Windows or POSIX path literal in
  a repository file is itself a specimen for the record sanitizer the entry is about. The entry
  also records why the POSIX exclusion is not directly portable: a raw Windows spelling uses a bare
  backslash as its separator, so "backslash never a path byte" is only safe once the fix decides
  how to read an escape-shaped run; the two readings are named, not silently chosen.
- **(c)** The entry records both directions of the change: the minted key (`agent:` for an absent
  name, `agent::unrecorded` for a recorded name equal to the legacy sentinel) and the surviving
  label path (`ownerKeyLabel` still labels `agent:unrecorded` "Agent" and `human:unrecorded`
  "Human owner not recorded"), so the defect is stated as "chip renders, list is empty".
- **(e)** Decision note, not a tracked item: there is no owner action to track (the limitation is
  this host's, the artifact is a workflow file that CI already executes), which matches the
  file's rule that a marker-less bullet is a decision note. The note carries the exact command,
  the tool version, the substitute recipe and the fact that no CI job invokes `actionlint`
  (verified: a repository grep for `actionlint` matches `docs/` only), so a later reader does not
  infer a CI gate that does not exist.
- **Quality rule** (contract clause 2, verbatim scope): a change touching `frontend/` or
  `browser/` runs the local browser render gate before its pull request; a change touching
  `daemon/`, platform-specific code, or `scripts/ci/` runs the affected suites on Linux (WSL or a
  Linux host) before its pull request; CI remains the floor, not the first execution of
  browser-rendered or Linux-only behavior. Named artifacts stay generic (no host paths, no
  machine names); the existing "Run affected focused checks during development" sentence is left
  as is, and the amendment states the path trigger it lacked.

## Evidence plan (all commands run at `978abba`)

1. Scratch Go probe in `daemon/internal/protocol/`, deleted before the report; its file is not
   part of the increment. `go test ./internal/protocol/ -run TestScratchProbeC0EscapeCase -v
   -count=1`.
2. Elixir probe executed in WSL against the Jason 1.4.5 beams of an existing checkout
   (`_build/test/lib/jason/ebin`), version confirmed from the dependency's `mix.exs`.
3. Sanitizer reproduction: two scratch inputs outside the repository, the tool invoked exactly as
   `scripts/ci/model-routing-record-sanitize.py <file>`; digests recomputed independently with
   `python -c` over the candidate spans.
4. Frontend reproduction: scratch Node ESM probe plus a scratch resolve hook in the temp
   directory (no `node_modules` install, no repository file added); it imports the repository's
   `filters.ts`/`presentation.ts` bytes through Node's type stripping.
5. actionlint: `timeout 620` bounded run (expected non-zero, killed by the bound) and the
   substitute run over both workflow files, whose exit code and wall time are recorded.
6. Gates after the edits: `git diff --check`; `model-routing-record-sanitize.py --check
   --root-only` per changed file over the added lines; record-cleanliness stays a vacuous run
   because no `docs/evaluations/**` or `docs/evidence/**` path changes.

## Out of scope (contract clause 4)

Fixing any recorded defect, CI changes, archive edits, and the retro contracts themselves (they
live outside the repository, and item d's drop is stated in the entry report only, never written
into a repo record about an out-of-repo file).

## Design trade-offs and decisions

1. **(e) as a decision note rather than a tracked item.** A tracked item needs an owner action;
   this limitation's action is "run the substitute", which the note itself records, and the
   workflow files still run in CI regardless. The file's own rule ("a bullet that carries
   no marker is a decision note") is the tie-breaker, so the note survives archival rules.
2. **No faithful Windows or POSIX path literal in the repository entries.** The sanitizer that
   entry (b) is about flags any drive-rooted or host-rooted literal (verified: the first draft
   tripped `--check --root-only` on `docs/TODO.md` and on this plan), and the increment's own
   clause asks for no host paths in added lines. The entries therefore carry the shape plus the
   digests (exact, comparable), while the exact input bytes stay in the run's scratch files and
   recorded write calls, outside the repository. Trade-off: a reader cannot paste the input from
   the entry alone; the digests and the cited rule lines still decide whether the claim holds.
3. **The quality rule as an amendment paragraph, not a new table row.** The table lists per-surface
   commands; the contract's rule is a path-to-verification trigger that spans surfaces and names no
   command, so it fits the paragraph amendments the file already carries (2026-11-17, 2026-11-20).
4. **Item (d) is dropped, not recorded.** Only an out-of-repo contract carries the wrong mapping;
   the repo's four `#79 (21cf326)` references are correct. Writing a repo record about an
   out-of-repo file would add a citation the repository cannot verify.
5. **No abstracted helper or script for these checks.** The verification is five one-off
   reproductions; a committed harness would be new dead weight for a docs-only increment.

## Residual risks

- (a) The Elixir side runs Jason 1.4.5 from a dependency checkout rather than this repository's
  `control/` build (Elixir is not installed on this host); the version matches `control/mix.lock`
  (`jason 1.4.5`), and the probe calls only `Jason.encode!/1`.
- (b) The Windows-family reproduction uses the recorded (JSON-escaped) spelling; the raw
  single-backslash spelling is covered in the entry as the reason the fix cannot copy the POSIX
  rule.
- (e) Non-termination is bounded observation, not a proof of a hang; the entry states the bound
  and the observed wall time and does not claim a cause.
- The actionlint observation is a local limitation with no CI counterpart; the contract's
  parenthetical "CI runs it on Linux" is not repeated in the record because no CI job invokes
  `actionlint` (see the finding in the report).
