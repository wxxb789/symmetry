# Control C1/C2 test ablation experiment

Date: 2026-09-29  
Track: `st_01a0ed7f`  
Baseline revision: working tree based on `d14debbf6ae3ded09e5013ef90952d347c01474d`

**Historical initial experiment; amended by final integration.** Independent
review found that ten removed fixture assertions had exact public error contracts
not represented by the manifest's boolean validity check. A dedicated table now
restores those tags and tuple details. The final contract-validation file has 12
tests, not the intermediate 11 described below. The prior subset missed an
isolated wrong-tag mutation; the corrected subset detected it, and the final full
suite passed 800 tests with no failures. See the
[final verification receipt](test-ablation-final-gates-2026-09-29.md) and
[resolved independent review](test-ablation-final-review-2026-09-29.md).
The original commands and intermediate results below remain historical evidence,
not the final coverage-parity decision.

## Scope and environment

This experiment covered only C1 and C2 from
`docs/evidence/test-ablation-control-2026-09-29.md`. Existing dirty work was
preserved. No Git index, commit, push, dependency install, full Control suite,
or existing PostgreSQL service was used.

Local evidence is exactly Elixir 1.19.6 on Erlang/OTP 28 and PostgreSQL 17.11
inside WSL `symmetry-race`; it is not the CI runtime matrix. Every Mix command
used `ERL_FLAGS='+S 4:4'` and private build path
`/tmp/symmetry-ablation-mix-st01a0ed7f`.

The experiment created only cluster `/tmp/symmetry-ablation-pg-st01a0ed7f`,
listener `127.0.0.1:55439`, database `symmetry_ablation_st01a0ed7f`, and user
`postgres` with an unrecorded local experiment-only password. The first server
start exited 1 because `/run/postgresql` was absent. The same owned cluster then
started with private socket directory `-k /tmp/symmetry-ablation-pg-st01a0ed7f`.
At the end, `pg_ctl ... stop -m fast` succeeded, both owned cluster and build
directories were removed, and `pg_isready` confirmed port 55439 was closed.

Common test environment:

```sh
POSTGRES_HOST=127.0.0.1 POSTGRES_PORT=55439 POSTGRES_USER=postgres \
POSTGRES_PASSWORD=<local-experiment-only> \
POSTGRES_DB=symmetry_ablation_st01a0ed7f ERL_FLAGS='+S 4:4' \
MIX_BUILD_PATH=/tmp/symmetry-ablation-mix-st01a0ed7f
```

## Bound source snapshots

| Path | Baseline SHA-256 | Final SHA-256 |
| --- | --- | --- |
| `control/test/symmetry_control/goals/contract_validation_test.exs` | `554f2fa1c5f3bbfb6cab9d38cdcb531ec474bf01378c603d72fc51beea135f00` | `596086083e84a3ee24076c18b31db8717e377c3f3df5f893531b6386277c5ee1` |
| `control/test/symmetry_control/goals/contract_fixture_manifest_test.exs` | `6053aab06a4c1fb683c390e5e33c29b1c5df742c3aa5e633ae198d48b7d76aab` | unchanged |
| `control/test/symmetry_control_web/controllers/goal_controller_test.exs` | `3dc28247c2ca2000008f9446add9757ee0ada45ae56823d792a75169a24ff1ce` | `8e9dcf349d09248259b5cdaca511d4192ff0a4b999f46ce42d7b4ce6c152a09d` |
| `control/test/symmetry_control_web/plugs/strict_evidence_json_test.exs` | `8b158518f7aefd99658d8cc3fa67cac4f66175993a427644e23a0024a81af0bf` | unchanged |
| `control/lib/symmetry_control/goals/contract_validation.ex` | `5b5170dd95621b43c1eb0b3ddc36ab5bf8a65017af8747ebbffcbf4640e5e755` | identical after restoration |
| `control/lib/symmetry_control_web/plugs/strict_evidence_json.ex` | `8d52c3c189e3d2cd7f33089f66965e0a7fbd888400da96c31285cfece5c05ab9` | identical after restoration |

All final and temporary source edits used `apply_patch` (a shell wrapper over
`patch`, because no standalone executable was installed). Temporary production
edits were limited exactly to the two `lib/` paths above and were restored.

## Baseline and candidate green

Exact baseline command:

```sh
cd control
mix test test/symmetry_control/goals/contract_fixture_manifest_test.exs \
  test/symmetry_control/goals/contract_validation_test.exs \
  test/symmetry_control_web/plugs/strict_evidence_json_test.exs \
  test/symmetry_control_web/controllers/goal_controller_test.exs
```

Result: exit 0, 35 tests, 0 failures. After candidate removal, the same command
returned exit 0 with 32 tests and 0 failures.

## Mutation matrix: expected-red, never green

| Candidate | Temporary mutation and mutated SHA-256 | Exact command | Detection | Exit |
| --- | --- | --- | --- | ---: |
| C1 | `validate_admission/2` dispatched to usage schema; `ec9c7c56d9f76af367d25aea0fee1290e00c8fad301c9086c0dd04cf69b98ef9` | `mix test test/symmetry_control/goals/contract_fixture_manifest_test.exs test/symmetry_control/goals/contract_validation_test.exs` | Manifest rejected `admission.basic`; retained atom-normalization assertion also failed. 12 tests, 2 failures. | 2 |
| C2 protocol | `json_mutation_path?/1` returned false for `api/v1`; `57232a91cc3dd538d2f677949ce2ca6b29fb2a907070fd5fed96572e347c92da` | `mix test --force test/symmetry_control_web/plugs/strict_evidence_json_test.exs test/symmetry_control_web/controllers/goal_controller_test.exs` | Direct Plug survivor did not halt; real Goal creation returned 201 instead of 400. 20 tests, 2 failures. | 2 |
| C2 portal | `json_mutation_path?/1` returned false for `portal/api`; `df8dfc2fcdd4753cdc265d4445f1ce59d87d647b702e64c6c04d758097c1aa73` | same forced C2 command | Direct Plug survivor did not halt; real portal creation returned 201 instead of 400. 20 tests, 2 failures. | 2 |
| C2 recursion | Duplicate-pair check disabled in `materialize_pairs/2`; `69d36d317d4154072451bd5ee4f5b970e6fc9302bd8cdbb72761f3dd4165ce59` | `mix test --force test/symmetry_control_web/plugs/strict_evidence_json_test.exs` | Nested escaped-key test materialized the duplicate; both direct prefix cases failed. 12 tests, 3 failures. | 2 |

Each expected-red run was immediately followed by `apply_patch` restoration.
Restored production hashes equal their baseline hashes byte-for-byte.

An initial protocol classifier mutation failed only the direct Plug test. It
revealed that the old endpoint assertion used an escaped route and an otherwise
invalid two-field Goal payload, so an unrelated 400 could satisfy it after
strict parsing was bypassed. That was not accepted as mutation proof. Both
surviving endpoint assertions now use canonical real routes and otherwise-valid
Goal-create JSON with only the duplicate injected. Repeated forced mutations
then made both endpoint survivors fail with 201. Escaped prefix decoding remains
covered by unchanged direct Plug tests.

## Decisions and removals

### C1: partial removal

`contract_validation_test.exs` drops two fixture-only declarations and direct
manifest-fixture portions of two retained declarations, decreasing from 13 to
11 tests. Those outcomes are already dispatched through every public validator
for all 184 manifest entries by `contract_fixture_manifest_test.exs`, including
unique IDs, manifest version/nonempty checks, semantic validity, and declared
semantic error codes.

Survivors retain all identified non-manifest obligations: Unicode code-point and
invalid UTF-8 path bounds; synthesized malformed maps and schema-invalid
composites; recursive atom-key normalization and atom-value rejection; batch
outer-run identity; decision, Goal-revision, and final-authority semantics;
strict-policy composition; plan canonicalization/hash and malformed-item safety;
explicit absolute root/no-CWD fallback; and resolved-schema caching. The C1
mutation proves the manifest survivor detects a validator mapping regression.

### C2: partial removal

`goal_controller_test.exs` drops the protocol command duplicate-member test and
the portal command half, decreasing from 9 to 8 tests. One real protocol create
and one real portal create remain and now use otherwise-valid payloads. The
unchanged direct Plug suite retains recursive escaped duplicates, escaped path
decoding, both prefixes, JSON and `+json`, exact safe envelopes, raw-body
materialization, malformed/non-object handling, GET/non-target bypass, and size
limits.

No evidence-batch HTTP assertion, authentication assertion, fixture, production
behavior, acceptance condition, or historical evidence was removed.

## Final green verification

```sh
cd control
mix test --force \
  test/symmetry_control/goals/contract_fixture_manifest_test.exs \
  test/symmetry_control/goals/contract_validation_test.exs \
  test/symmetry_control_web/plugs/strict_evidence_json_test.exs \
  test/symmetry_control_web/controllers/goal_controller_test.exs
```

Result: exit 0, 32 tests, 0 failures, seed 411005.

```sh
cd control
mix format --check-formatted \
  test/symmetry_control/goals/contract_validation_test.exs \
  test/symmetry_control_web/controllers/goal_controller_test.exs
```

Result: exit 0. `git diff --check` for both test files also exited 0. Final test
diff: 19 insertions, 377 deletions.

## Parent broad-check bootstrap handoff

The owned fixture had already been stopped and removed before authorization for
one complete Control run arrived. Per instruction, this track did not recreate
it and did not run `mix test --seed 0`. The parent can perform that check with a
new fixture using these exact WSL commands from the repository root:

```sh
set -eu
cluster=/tmp/symmetry-ablation-pg-parent-control-final-20260929
build=/tmp/symmetry-ablation-mix-parent-control-final-20260929
port=55439
db=symmetry_ablation_control_final_20260929

# Refuse to adopt an existing directory or listener.
test ! -e "$cluster"
if /usr/bin/pg_isready -h 127.0.0.1 -p "$port" >/dev/null 2>&1; then
  echo "refusing occupied PostgreSQL port $port" >&2
  exit 74
fi

mkdir "$cluster"
chown postgres:postgres "$cluster"
su -s /bin/sh postgres -c   "/usr/bin/initdb -D $cluster --auth-local=trust --auth-host=trust --no-locale --encoding=UTF8"
su -s /bin/sh postgres -c   "/usr/bin/pg_ctl -D $cluster -o '-h 127.0.0.1 -p $port -k $cluster' -l $cluster/server.log start"
/usr/bin/pg_isready -h 127.0.0.1 -p "$port"

cleanup() {
  set +e
  su -s /bin/sh postgres -c "/usr/bin/pg_ctl -D $cluster stop -m fast" >/dev/null 2>&1
  rm -rf "$cluster" "$build"
}
trap cleanup EXIT HUP INT TERM

cd /mnt/q/repos/symmetry/control
export POSTGRES_HOST=127.0.0.1
export POSTGRES_PORT="$port"
export POSTGRES_USER=postgres
export POSTGRES_PASSWORD=ablation-local-only
export POSTGRES_DB="$db"
export ERL_FLAGS='+S 4:4'
export MIX_BUILD_PATH="$build"

elixir --version
psql --version
sha256sum   test/symmetry_control/goals/contract_validation_test.exs   test/symmetry_control/goals/contract_fixture_manifest_test.exs   test/symmetry_control_web/controllers/goal_controller_test.exs   test/symmetry_control_web/plugs/strict_evidence_json_test.exs   lib/symmetry_control/goals/contract_validation.ex   lib/symmetry_control_web/plugs/strict_evidence_json.ex

set +e
mix test --seed 0
code=$?
set -e
echo "CONTROL_MIX_TEST_SEED_0_EXIT=$code"

cleanup
trap - EXIT HUP INT TERM
test ! -e "$cluster"
test ! -e "$build"
if /usr/bin/pg_isready -h 127.0.0.1 -p "$port" >/dev/null 2>&1; then
  echo "owned PostgreSQL listener remains on $port" >&2
  exit 75
fi
exit "$code"
```

The parent should record ExUnit's final tests/failures/skips line and the echoed
exit code. If the run fails, its output is new broad-check evidence; this report
does not classify any such failure as pre-existing.

## Remaining limits and ownership

No full Control suite, release, CI-version runtime, native credentialed test, or
non-C1/C2 test ran in this track; the parent retains those gates. This proves
only the listed same-boundary regressions. The disposable database no longer
exists, so there is no live fixture or secret to hand back. No production path
remains modified or owned by this track. Final write ownership is the two test
files and this report.
