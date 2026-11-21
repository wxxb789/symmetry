# rf-w2-16 clone-aliasing test plan — reflection independence coverage for the eleven platform clones

Contract: the GDD goal contract `goal-rf-w2-16-clone-aliasing-test.md` (2026-11-21, at the GDD
state root) and its binding amendments in `refactor/w2-contract-review-opus-1.md` P1-6 and
`refactor/w2-contract-review-ds-1.md` A2/A3. Baseline for every coordinate: the worktree HEAD
`4dd374bdcdbe09a05287e77ad90faaa949fcddcd` (#143, the W1-04 platform split, which moved the
clone functions to the homes this plan cites). Branch: `refactor/rf-w2-16-clone-aliasing-test`.
Area: new test files in `daemon/internal/platform` only; no production change.

The increment converts P-08's prospective risk ("adding one pointer/slice/map field to an
evidence struct silently creates an aliasing authority bug that no test and no linter detects")
into a gate: a reflection-driven table test that populates every field of each covered value,
deep-clones it through the production clone function, writes through every pointer, slice
element and map entry the original still holds, and requires the clone's rendered content to be
unchanged. A field the clone copies shallowly is exactly a field whose mutation becomes visible
inside the clone, so a newly added uncovered field fails red without editing the test.

## The eleven covered clones (1:1 case table)

| # | Clone | Home at `4dd374b` (build) | Test case |
| --- | --- | --- | --- |
| 1 | `cloneLinuxOwnerAnchorExitProof` | `anchor_server.go:44` (`linux && amd64`) | `TestCloneAliasingLinuxAMD64Evidence/cloneLinuxOwnerAnchorExitProof` |
| 2 | `LinuxOwnerAnchorProducerFence.Clone` | `anchor_types.go:115` (`linux && amd64`) | `.../LinuxOwnerAnchorProducerFence.Clone` |
| 3 | `LinuxOwnerAnchorStopEvidence.Clone` | `anchor_types.go:246` (`linux && amd64`) | `.../LinuxOwnerAnchorStopEvidence.Clone` |
| 4 | `cloneLinuxOwnerProcessIdentity` | `helper_v2_run.go:127` (`linux && amd64`) | `.../cloneLinuxOwnerProcessIdentity` |
| 5 | `cloneLinuxOwnerGateIdentity` | `helper_v2_run.go:135` (`linux && amd64`) | `.../cloneLinuxOwnerGateIdentity` |
| 6 | `cloneLinuxOwnerGateIdentityIfValid` | `helper_v2_run.go:143` (`linux && amd64`) | `.../cloneLinuxOwnerGateIdentityIfValid` |
| 7 | `cloneLinuxOwnerHelperV2StopEvidence` | `helper_v2_stop.go:1540` (`linux && amd64`) | `.../cloneLinuxOwnerHelperV2StopEvidence` |
| 8 | `cloneLinuxCgroupV2RemovalAuthorization` | `linux_cgroup_v2_linux.go:1399` (`linux`) | `TestCloneAliasingLinuxCgroupEvidence/cloneLinuxCgroupV2RemovalAuthorization` |
| 9 | `cloneLinuxCgroupV2PreGateRemovalAuthorization` | `linux_cgroup_v2_linux.go:1412` (`linux`) | `.../cloneLinuxCgroupV2PreGateRemovalAuthorization` |
| 10 | `cloneLinuxCgroupStopReceipt` | `linux_cgroup_v2_linux.go:1443` (`linux`) | `.../cloneLinuxCgroupStopReceipt` |
| 11 | `cloneContainmentSessionID` | `containment_supervisor_windows.go:771` (`windows`) | `TestCloneAliasingWindowsContainmentSessionID` |

The seven `linux && amd64` clones operate on `LinuxOwnerAnchorProducerFence` (`anchor_run.go:127`,
19 fields), `LinuxOwnerAnchorStopEvidence` (`anchor_run.go:155`, 25 fields),
`LinuxOwnerHelperV2StopEvidence` (`helper_v2_stop.go:28`, 16 fields) and the authority value
types `LinuxCgroupExitProof` (`authority/authority.go:817`), `LinuxCgroupProcessIdentity`
(`:810`) and `LinuxCgroupGateIdentity` (`:889`). The three `linux` clones operate on
`LinuxCgroupV2RemovalAuthorization` (`linux_cgroup_v2_linux.go:63`),
`LinuxCgroupV2PreGateRemovalAuthorization` (`:74`) and `authority.StopReceipt`
(`authority/authority.go:1080`). `cloneContainmentSessionID` copies one `*uint32`.

## Public / API shape

No production symbol changes. Three new `_test.go` files (see "File layout"); no exported or
unexported production declaration is added, removed or edited, and the eleven clone functions
stay byte-identical - the acceptance runs against the clones as shipped.

## Harness semantics (the mechanism, per case)

1. **Populate.** `cloneAliasingCaseFor[T]` gives the case a fresh `*T`; the driver populates
   `*T` field by field by reflection: pointers allocate and recurse, slices get two populated
   elements, maps get one populated entry, structs recurse, bools become `true`, strings become
   a marker, integers become a small non-zero sentinel, `time.Time` becomes a fixed instant. A
   field that cannot be set (unexported) or whose kind the harness does not know fails the case
   loudly, so a future field forces a harness decision instead of a silent skip.
2. **Overrides.** Five `(type, field)` overrides pin the fields the covered types' own
   `Validate` methods define: `LinuxCgroupParentIdentity.BootID`/`.Path`,
   `LinuxCgroupIdentity.Version`/`.LeafName`, and `LinuxCgroupGateIdentity.BirthKind`
   (`= authority.LinuxCgroupTargetBirthTrustedAGate`). They exist because
   `cloneLinuxOwnerGateIdentityIfValid` returns `nil` when `LinuxCgroupGateIdentity.Validate`
   (`authority/authority.go:1317`) refuses the value; without them that case would degenerate.
   Any field added later still gets populated and mutated by kind - the overrides name value
   formats, not clone coverage.
3. **Per-case populate skips, stated with their real limits.** Every exit proof is populated
   one nesting level deep, except where a case lists a dotted field path in
   `cloneAliasingCase.skipExitProofs`. Each listed path must name a
   `*authority.LinuxCgroupExitProof` field and must be reached during population (an unused
   entry fails the case), so the skips are per clone, not type-global. Three reasons put a path
   on a list:
   - **Truncation by design.** Every platform clone that receives an exit proof outside the
     authority package truncates the nested field (`cloneLinuxOwnerAnchorExitProof`,
     `anchor_server.go:44-49` sets `cloned.HelperExit = nil`), so
     `cloneLinuxOwnerAnchorExitProof`, `LinuxOwnerAnchorProducerFence.Clone`,
     `LinuxOwnerAnchorStopEvidence.Clone` and the anchor-facing half of
     `cloneLinuxOwnerHelperV2StopEvidence` leave those paths nil; populating them would make
     the fidelity comparison assert the drop instead of independence.
   - **Shared under recorded debt.** `cloneLinuxCgroupStopReceipt`
     (`linux_cgroup_v2_linux.go:1443-1457`) shares `LinuxProof.GateExit.HelperExit` and
     `LinuxProof.LauncherExit.HelperExit` with its input through the `LinuxProof` struct copy
     at `:1449` (only the stored `LinuxProof.HelperExit` is deep-copied, `:1451-1452`), and
     that struct copy shares one nesting deeper in turn. The alias is live on a valid shape: a
     mirror stop proof demands `proof.HelperExit != nil` and a `GateExit` anchored on that same
     exit (`authority/authority.go:1470-1474` via `ValidForOwnedDescendant`, `:1174-1184`),
     and every v2 removal receipt must carry such a proof (`StopReceipt.ValidForHandoff`,
     `authority.go:1822-1833`); the platform constructs the shape at `anchor_run.go:1531`,
     validated at `execution/linux_owner_v2_linux.go:1338` and sealed into the mirror proof at
     `:1360` (also `app/linux_owner_recovery_linux.go:1115-1130`).
     `cloneLinuxOwnerHelperV2StopEvidence`'s `PreGate.Proof.LauncherExit.HelperExit` is shared
     the same way through the `PreGate.Proof` struct copy (`helper_v2_stop.go:1563-1567`), and
     the helper-kind pre-gate shape requires it nil anyway.
   - **Beyond the valid nesting.** `LinuxCgroupExitProof.Validate`
     (`authority/authority.go:1164`) rejects a second helper-exit nesting level, so the copy of
     a single-level helper exit stops there; the harness leaves the next level nil rather than
     build a shape no production value carries.
   Recorded debt (production changes are out of contract scope, so not fixed here): a later
   production goal must deep-copy `GateExit.HelperExit` in `cloneLinuxCgroupStopReceipt` (audit
   the `LauncherExit` mirror too; `authority.go:2027` also accepts its non-nil `HelperExit`) and
   decide per path whether `cloneLinuxOwnerHelperV2StopEvidence` should deep-copy or keep
   dropping the nested exits it trims today, then remove the matching `skipExitProofs` entries.
   Removing such an entry before the matching production fix makes the covering case red, which
   the contract's failure control forbids. The `RunReapEvidence` exits are *not* skipped: the
   deep-copy loop at `helper_v2_stop.go:1547-1550` keeps them independent, so deleting that
   loop or making `LinuxCgroupExitProof.Clone`
   (`authority/authority.go:1195-1202`) shallow turns the suite red (both verified with
   `go test -overlay`; see the evidence table).
   The *outer* `HelperExit` pointers of `LinuxOwnerAnchorStopEvidence`,
   `LinuxOwnerHelperV2StopEvidence`, `LinuxCgroupStopProof`,
   `LinuxCgroupPreGateStopProof` and `LinuxCgroupPreGateStopReceipt`, and the `LauncherExit`
   exit proofs of `Supervisor`/`SupervisorHandoff`, are populated and mutated normally.
4. **Clone** through the production function, exactly as shipped.
5. **Fidelity.** The clone's deep rendering must equal the populated original's rendering before
   any mutation. The rendering dereferences pointers recursively, so it compares content, not
   addresses; a clone that drops a field or returns a zero value fails here. This is also the
   guard that makes the independence check meaningful (a clone that copies nothing would
   trivially pass an independence-only check).
6. **Mutate.** The driver writes a mutated sentinel through every settable location reachable
   from the original through a pointer, slice element or map entry, recursing into the pointees
   so that shallow-copied inner pointers are also caught. The map arm recurses into every
   entry's value (the entry itself is not addressable, but a pointer, slice or interface it
   holds still reaches writable pointees) and then deletes one key: the recursion catches a map
   copied entry-wise over shared pointees (e.g. `maps.Clone`), the deletion catches a map
   copied with a shared header, and a fully deep-copied map stays green. It returns the number
   of write sites; zero write sites fails the case, so a populate that silently produced a
   value-only graph cannot make the case vacuous.
7. **Independence.** After the mutation, the clone's rendering must still equal the rendering
   captured before it. Sharing one pointee, backing array or map with the original changes that
   rendering and fails with before/after images.

## File layout (reconciling ds-1 A2 with opus P1-6)

The covered clones live in three build universes, measured on this tree:
`linux_cgroup_v2_linux.go:1` is `//go:build linux` (not `linux && amd64`), the seven
anchor/helper clones are `//go:build linux && amd64`, and `containment_supervisor_windows.go` is
`//go:build windows`. ds-1's A2 list of exactly two files rests on the premise that the cgroup
clone file is `linux && amd64`; the stricter, technically accurate reading is the one the
contract's own binding line names ("linux && amd64, linux, windows as the clones require") and
opus P1-6 spells out:

- `clone_aliasing_linux_amd64_test.go` (`//go:build linux && amd64`) - the seven anchor/helper
  cases;
- `clone_aliasing_linux_test.go` (`//go:build linux`) - the three cgroup cases **and the shared
  harness**; the file is present in every universe that compiles the amd64 file (amd64 implies
  linux), and it compiles on linux/arm64 where the cgroup clones exist;
- `clone_aliasing_windows_test.go` (`//go:build windows`) - the single `cloneContainmentSessionID`
  case, written inline (the shared harness is `//go:build linux` and the case is a one-field
  pointer copy).

No unconstrained `clone_aliasing_test.go` exists. Each file carries exactly the constraint of
the clones it covers, so `GOOS=linux GOARCH=arm64 go vet` type-checks the cgroup cases and the
Windows file is the only one compiled on the Windows host.

## Edge-case semantics the contract demands

- **A new uncovered field fails red.** Populate and mutate iterate all struct fields by
  reflection (`reflect.Type.NumField`), so a pointer/slice/map field added to a covered struct
  is populated and written through; the unmodified clone shares it and the independence
  assertion fails. Demonstrated by the planted-red runs below.
- **Shallow comparisons do not count.** Every assertion compares deep renderings captured
  through pointers, slice elements and map entries; no case compares struct values, and the
  mutation step explicitly writes through references rather than assigning fields.
- **No production change.** The only production files touched are transient planted-red edits,
  reverted before the tree is handed over; the final diff holds three new test files plus this
  plan.
- **Validate-gated clone.** `cloneLinuxOwnerGateIdentityIfValid` is covered with a value its own
  `Validate` accepts (overrides above), so the case exercises the copy path, not the `nil` path.

## Evidence (exact commands and measured results, on these bytes)

Run from `daemon/` on this host unless noted; every heavy run was serialized with the others.

| Gate | Command | Result |
| --- | --- | --- |
| format | `gofmt -l .` (repo root), `gofmt -l internal/platform` | exit 0, no paths |
| vet (host) | `go vet ./...` | exit 0, no diagnostics |
| vet (test files, cross) | `GOOS=linux GOARCH=amd64 go vet ./internal/platform/`, `GOOS=linux GOARCH=arm64 go vet ./internal/platform/` | exit 0 each (compiles the new test files per target) |
| build | `GOOS=linux go build ./...`, `GOOS=windows go build ./...` | exit 0 each |
| package tests (host) | `go test -count=1 ./internal/platform/...` | exit 0, `ok 15.491s` (runs the Windows case) |
| execution tests | `go test -count=1 ./internal/execution/...` | exit 0, `ok 13.874s` |
| Linux cases (WSL) | `wsl -d Ubuntu-24.04 -u accept -- bash -lc 'cd <worktree>/daemon && go test -count=1 -run TestCloneAliasing -v ./internal/platform/'` | exit 0, 10/10 Linux subtests green |
| Linux suite (WSL, supplementary) | same, without `-run` | exit 1, 600 s timeout: four pre-existing `TestVerifyLoopbackTCPPeer*` failures (~134 s each) that reproduce with the three new files removed from the tree; CI's daemon jobs are the contract's Linux oracle |
| planted-red 1 (WSL) | add `PlantedAliasingField *authority.LinuxCgroupProcessIdentity` to `LinuxOwnerAnchorStopEvidence` (`anchor_run.go`), run the `-run TestCloneAliasing` command | exit 1, `LinuxOwnerAnchorStopEvidence.Clone` red (126 writes) and `cloneLinuxOwnerHelperV2StopEvidence` red (498 writes) |
| planted-red 2 (WSL) | add `PlantedAliasingField map[string]string` to `LinuxCgroupV2PreGateRemovalAuthorization` (`linux_cgroup_v2_linux.go`), same run | exit 1, `cloneLinuxCgroupV2PreGateRemovalAuthorization` red (56 writes) |
| planted-red 3 (WSL, supplementary) | remove `cloned.Launcher = cloneLinuxOwnerProcessIdentity(fence.Launcher)` from `LinuxOwnerAnchorProducerFence.Clone` (`anchor_types.go`), same run | exit 1, `LinuxOwnerAnchorProducerFence.Clone` red (53 writes) plus the two nested-input cases |
| planted-red 4 (WSL, `go test -overlay`) | remove the `RunReapEvidence` deep-copy loop (`helper_v2_stop.go:1547-1550`) in a `/tmp` scratch copy, run the `-run TestCloneAliasing` command | exit 1, `cloneLinuxOwnerHelperV2StopEvidence` red (522 writes) |
| planted-red 5 (WSL, `go test -overlay`) | make `LinuxCgroupExitProof.Clone` return its input unchanged (`authority/authority.go:1195-1202`) in a scratch copy, same run | exit 1, `cloneLinuxOwnerHelperV2StopEvidence` red (522 writes), `cloneLinuxCgroupV2RemovalAuthorization` (305), `cloneLinuxCgroupV2PreGateRemovalAuthorization` (69) |
| whitespace | `git diff --check` (new files recorded with `git add -N`) | exit 0 |

The `-overlay` probes run against scratch copies under WSL `/tmp` and replace no repository
file. Removing any single `skipExitProofs` entry turns its covering subtest red as well
(verified path by path for the shared paths, and by inspection of the literal
`cloned.HelperExit = nil` for the truncated ones).

The planted edits are reverted (verified with `git status --porcelain` showing only the new test
files and the plan). No `-race` locally: no cgo on this Windows host; the daemon race jobs
(both GOOS) are the CI-only oracle the contract names.

## Design trade-offs

- **Rendering instead of `reflect.DeepEqual` against a second populated instance.** DeepEqual
  would work for pure deep copies but not for `cloneLinuxOwnerAnchorExitProof`, whose content
  normalization (drop `HelperExit`) is by design: the render comparison measures independence
  and fidelity without depending on a comparison copy that would have to encode each clone's
  normalization. The rendering is deterministic (type-tagged, sorted map entries, fixed
  instant, no addresses), so before/after images are comparable.
- **Fidelity check included.** The contract's acceptance is independence plus planted-red; a
  fidelity assertion is ~6 lines and closes the failure control's other reading ("a test that
  only compares shallow copies" could also be satisfied by a clone that returns nothing).
- **Populate by kind, not by hand.** A hand-written fixture per struct would silently skip a
  newly added field - exactly the failure the contract wants red. The kind-driven filler treats
  unknown kinds and unexported fields as errors, so harness gaps surface as failures.
- **Harness in the `linux` file.** The alternative (a fourth `linux || windows` harness file)
  would add a file the reviews do not name; duplicating the harness per universe would collide
  on linux/amd64. The chosen layout keeps exactly the three amendment-named files.
- **Windows case inline.** Sharing the harness would require a fourth file or constraining the
  windows file to linux; the clone is a one-field pointer copy, so four assertions catch the
  aliasing it could regress.

## Out of scope (with reasons)

- P-08 option (a) (restrict evidence types to value-only fields): touches
  `internal/authority` consumers; the contract excludes it.
- Any production clone change, including `cloneLinuxCgroupStopReceipt`'s shared
  `LinuxProof.GateExit.HelperExit` and `LinuxProof.LauncherExit.HelperExit` (a live, reachable
  shape - see the per-case populate skips above) and `helper_v2_stop.go`'s nested exit-proof
  truncations: recorded as explicit debt in `docs/TODO.md` and in the harness comment, not
  edited here (the contract forbids production changes in this goal).
- `internal/authority`'s own clones (`Supervisor`, `SupervisorHandoff`,
  `LinuxCgroupPreGateStopReceipt`, `SupervisorHandoffReleaseProof`, ...): W2-14's scope; this
  test only exercises the clones that consume those values.

## Ambiguities resolved (options and the chosen reading)

1. **File set: ds-1 A2 ("exactly two files") vs opus P1-6 (three constraint-matched files).**
   The contract instructs "where the two differ, the stricter one wins" and its own binding line
   lists three constraints. ds-1's premise is factually wrong at `4dd374b`
   (`linux_cgroup_v2_linux.go:1` is `//go:build linux`), so a single `linux && amd64` file would
   drop the cgroup cases on arm64 and hide them from the arch that can genuinely compile them.
   Chosen: the three files above. Both reviews' substantive requirements - constraint-matched
   files, no unconstrained file, a real Linux run for the red check - are satisfied.
2. **`answer.md` is the harness's run artifact.** The judging artifact (the planted-red output
   and the gate table) is written into the implement stage's final message, which the run
   harness records as `answer.md` (precedent: the W1-04 implement run); no repository file
   named `answer.md` is created.
3. **Contract file location.** The contract and its index live in the GDD state tree, outside
   this repository; the implement stage does not move them (W1-04 precedent). Repository-side
   closure is this plan file; no other document cites the new test files or the clone
   coordinates, so no citation goes stale.
