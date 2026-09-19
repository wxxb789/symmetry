# Committed Goal session receipt testing

This opt-in test connects the production Go control client to a real Phoenix
HTTP listener and PostgreSQL. It checks committed protocol-fixture receipt
recovery across HTTP listener restarts, not native harness execution.

The fixture deliberately constructs a claimed Goal Run. It does not prove
scheduler admission, native start/stop, daemon journal recovery, a BEAM process
restart, a PostgreSQL restart, retained resume, or Goal completion.

## Behavior

The test has four bounded phases:

1. Forward the first attach request to Phoenix, observe HTTP 201, discard that
   real response, and return a transport failure to the Go caller.
2. Restart the listener. Read back and replay the exact attach request through
   the Go client, checking HTTP 200 and the original immutable receipt. Reject
   changed authority and identity without changing the database rows.
3. Apply the production terminal transition and Goal settlement. Send the stop
   request, discard its first HTTP 201 response, and confirm that PostgreSQL
   already contains the stop receipt and the released session.
4. Restart the listener again. Replay the stop request with HTTP 200 and the
   original response. The attach readback remains the original busy snapshot,
   while current session state remains available and its version is unchanged.

The response-loss transport never substitutes JSON or a fake server. The
production client still owns request encoding and strict response decoding.
Each fixture operation, request and database observation checks out an unboxed
connection. A transaction-ID check rejects accidental use of an outer rollback
transaction. The normal test suite continues using its existing sandbox.

## Isolation

Use a new, disposable database named
`symmetry_receipt_e2e_<32 lowercase hexadecimal characters>`. The test checks
the effective database name and requires initially empty fixture tables before
writing committed state. Never point it at a shared development or test database.

Goal history and session receipts have immutable DELETE guards. Cleanup drops
only the database created for this invocation after the test process exits;
it does not disable triggers, truncate shared tables or delete immutable rows.
CI uses a unique database within its disposable PostgreSQL service.

The Go driver reads a temporary private JSON file containing generated fixture
authentication and fence values. These values never go in command arguments or
test reports. No native model credentials are required.

## Run

Use the Go, Elixir and OTP versions configured in CI. Configure `POSTGRES_HOST`,
`POSTGRES_PORT`, `POSTGRES_USER` and `POSTGRES_PASSWORD` for a PostgreSQL test
instance where the test account can create and drop databases. From `control/`:

```powershell
$previousDatabase = $env:POSTGRES_DB
$previousMixEnv = $env:MIX_ENV
$previousGate = $env:SYMMETRY_GOAL_RECEIPT_E2E
$env:POSTGRES_DB = 'symmetry_receipt_e2e_' + [guid]::NewGuid().ToString('N')
$env:MIX_ENV = 'test'
$env:SYMMETRY_GOAL_RECEIPT_E2E = '1'
try {
    mix test test/symmetry_control_web/controllers/goal_machine_controller_test.exs --only goal_receipt_e2e
    if ($LASTEXITCODE -ne 0) { throw 'Goal receipt E2E failed' }
} finally {
    mix ecto.drop --force
    $env:POSTGRES_DB = $previousDatabase
    $env:MIX_ENV = $previousMixEnv
    $env:SYMMETRY_GOAL_RECEIPT_E2E = $previousGate
}
```

```bash
(
  export POSTGRES_DB="symmetry_receipt_e2e_$(openssl rand -hex 16)"
  export SYMMETRY_GOAL_RECEIPT_E2E=1
  trap 'MIX_ENV=test mix ecto.drop --force' EXIT
  mix test test/symmetry_control_web/controllers/goal_machine_controller_test.exs --only goal_receipt_e2e
)
```

`mix test` creates and migrates the selected database through the existing test
alias. The test builds its Go driver from the current checkout before creating
the claimed Run; it does not accept an externally supplied driver binary.

Without the opt-in, the test skips and supplies no committed-recovery evidence.
An enabled test fails on missing prerequisites, invalid database isolation,
transport errors outside the injected loss, or receipt mismatches. Record the
tested commit and actual pass/skip result when reporting evidence.

## Recorded Windows check

Observed on 2026-09-11 against production baseline `ba4954f`, with the following
test-source SHA-256 values:

| Source | SHA-256 |
| --- | --- |
| `daemon/internal/control/goal_receipt_e2e_test.go` | `fc339032ae59ad0c3a45f7f0f45035ad4c03709e1f54b70c7b0b38a4b167107f` |
| `control/test/symmetry_control_web/controllers/goal_machine_controller_test.exs` | `4c633734060ffb95f3cf8e7a64457a99d4234bc17fa30f5489bf31c78c150ad3` |

Environment: Windows amd64, Go 1.27.0, Elixir 1.20.4 / OTP 29.0.6, and local
Docker PostgreSQL `postgres:18.6-alpine`. The final four-phase run passed one
test with seven unrelated tests excluded, in 5.4 seconds. The normal controller
suite passed seven tests and skipped the opt-in test. Enabling the test against
a non-isolated database was separately observed to fail before fixture writes.

Both disposable databases used during integration were dropped after their test
processes exited. Review found no unresolved findings after the final changes.
This records Windows-hosted client/listener evidence only; the new Linux CI step
is not counted as passed by this local check. No coding-harness process or model
was involved.
