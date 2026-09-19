# Goal 0006 Canonical Progress Receipt - 2026-09-14

## Source Subject

- Source commit: `ad2e943bb19a90feed5c2e5c517021136b127ab2`
- Git tree: `2ace3ee607d5c518a711f3899c853ef5a829b372`
- Tracked manifest SHA-256: `f8b469413bec8057e438bb664b8414e22b3f168e04c1fc2eaf31411d7614d801`
- Tracked worktree at capture: clean (`git diff --quiet`)

The manifest digest is SHA-256 over the UTF-8 output of
`git ls-tree -r --full-tree ad2e943bb19a90feed5c2e5c517021136b127ab2`, joined
with LF. Untracked evidence drafts and local `mise` files are excluded from
this source subject.

## Executed Gates

- `daemon`: `go test -count=1 -timeout 600s ./...` passed.
- `daemon`: `go vet ./...` passed.
- `control`: `mix format --check-formatted` passed.
- `control`: `mix compile --warnings-as-errors` passed.
- `control`: `mix test --no-color` passed: `742 passed`, `20 skipped`.
- Windows Pi `0.85.1`: `mix test test/symmetry_control/pi_control_e2e_test.exs --include skip:true --no-color`
  passed: `7 passed`, `0 skipped`, `0 failed`.

The Pi test uses a pinned, test-only witness and a synthetic numeric IPv4
loopback upstream without provider credentials. It covers ordinary repository
work, containment lease sequencing, crash recovery, candidate validation,
fresh handoff, and retained resume. Normal test runs skip this opt-in module;
CI must explicitly use `--include skip:true`.

## Current Limits

This is a partial progress receipt, not Goal completion evidence. It does not
prove a credentialed provider repository operation, hosted Windows CI, Windows
OpenCode native coverage, cross-machine recovery, or authorized acceptance of
a real user Goal. The Claude local transport smoke has only source/parser
validation in this subject because no real Claude executable and loopback
endpoint were available.

Evidence drafts dated 2026-09-15 are future-dated relative to this 2026-09-14
receipt and are not used here.
