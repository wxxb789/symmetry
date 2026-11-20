param(
  [string]$PortalUrl = "http://127.0.0.1:4021",
  [int]$Port = 4021
)

# Connection-management browser acceptance on the fixed design's
# /portal/connections surface. Starts the real control plane with the controlled
# provider fixture transport (no provider host is contacted), runs the real-API
# Playwright spec, then stops the fixture server and reports the fixture log.
#
# The run fails closed before any browser flow: the target must be the loopback
# port this harness starts its own fixture on, that port must be free, the
# fixture compiles into this instance's own build path (MIX_BUILD_PATH under
# control/_build/instances, never the shared control/_build/dev), the fixture
# must publish this run's identity file, and /healthz must report the full
# topology healthy. The spec itself re-checks the identity file and logs in
# with this run's operator token before any provider check.
$ErrorActionPreference = "Stop"
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot "../..")).Path
$controlRoot = Join-Path $repoRoot "control"
$runRoot = Join-Path $env:TEMP ("symmetry-0007e-connections-" + [guid]::NewGuid().ToString("N").Substring(0, 8))
New-Item -ItemType Directory -Force -Path $runRoot | Out-Null
$fixtureLog = Join-Path $runRoot "fixture-calls.jsonl"
$serverLog = Join-Path $runRoot "control.log"
$specLog = Join-Path $runRoot "spec.log"
$identityFile = Join-Path $runRoot "fixture-identity.json"
$stateFile = Join-Path $runRoot "fixture-state.json"

# The provider steering document the spec rewrites between flows: the fixture
# re-reads it on every provider request, so an empty object here is the empty
# state. .NET writes it without a BOM, which the fixture's JSON decode requires.
[System.IO.File]::WriteAllText($stateFile, "{}")

$target = [Uri]$PortalUrl
if ($target.Scheme -ne "http" -or $target.Host -notin @("127.0.0.1", "localhost", "::1")) {
  throw "PortalUrl must be a loopback http URL, got '$PortalUrl'"
}
if ($target.Port -ne $Port) {
  throw "PortalUrl port ($($target.Port)) disagrees with the fixture port ($Port): refusing to target a process this harness does not own"
}
if (@(Get-NetTCPConnection -State Listen -LocalPort $Port -ErrorAction SilentlyContinue).Count -gt 0) {
  throw "port $Port is already in use: the fixture cannot be the process answering PortalUrl"
}
# Docker forwards host ports without a socket this shell can enumerate, so the
# port is also probed by connecting: any answer means the fixture cannot become
# the process answering PortalUrl.
$probe = New-Object System.Net.Sockets.TcpClient
try {
  $attempt = $probe.BeginConnect("127.0.0.1", $Port, $null, $null)
  if ($attempt.AsyncWaitHandle.WaitOne(1000) -and $probe.Connected) {
    throw "port $Port already accepts connections: the fixture cannot be the process answering PortalUrl"
  }
} finally {
  $probe.Close()
}

# Per-run identity and operator token. Only the fixture process started below
# receives them, so the spec can prove the target is this run's fixture before
# any provider check and a normal Control instance cannot satisfy the run.
$operatorToken = [guid]::NewGuid().ToString("N")
$identity = [guid]::NewGuid().ToString("N")

# Instance build isolation: two Control instances started from the same
# worktree must not share control/_build, where a compile another instance runs
# (or the dev code reloader that compile feeds) decides what this run's fixture
# loads and reloads - the 2026-11-17 observation was a foreign session's
# server answering this worktree's browser runs with `# unhandled` error pages.
# This harness owns the fixture it starts and pins its compiled output per
# port, so instances from one worktree that both serve compile into different
# build paths: the preflight above refuses a port that already answers, and a
# concurrent start on one port fails closed at bind/readiness. Deps sources
# stay shared under control/deps; only compiled output moves. An inherited
# MIX_BUILD_PATH is overridden explicitly, never trusted: a stale one from the
# surrounding shell would point this instance at another directory, which is
# the isolation this harness must own itself.
$instanceBuildPath = Join-Path $controlRoot ("_build/instances/connections-" + $Port)
if ($env:MIX_BUILD_PATH) {
  Write-Host "ignoring inherited MIX_BUILD_PATH '$env:MIX_BUILD_PATH': this instance compiles into $instanceBuildPath"
}
$env:MIX_BUILD_PATH = $instanceBuildPath

$env:POSTGRES_HOST = "127.0.0.1"
$env:POSTGRES_PORT = "55444"
$env:POSTGRES_USER = "postgres"
$env:POSTGRES_PASSWORD = "postgres"
# Run-owned database name, like the per-port build path above: every run drops
# and recreates the database it configures, so a name several runs share lets a
# second concurrent run on another port destroy the first run's projects,
# connections, resources and work items mid-spec. The `symmetry_0007e_` family
# prefix stays reserved by Mix.Tasks.Symmetry.CheckTestDb.
$env:POSTGRES_DB = "symmetry_0007e_test_$Port"

Push-Location $controlRoot
try {
  # The acceptance database is dedicated to this run, and a rerun on the same
  # port finds the previous run's projects, connections, resources and work
  # items still there. A growing connection list makes every page read slower
  # and the run less reproducible, and the rows are referenced across tables,
  # so the fixture recreates its own database instead of deleting rows
  # selectively.
  mise exec elixir@1.20.4-otp-29 erlang@29.1.2 -- mix ecto.drop --quiet
  if ($LASTEXITCODE -ne 0) { throw "mix ecto.drop failed with exit code ${LASTEXITCODE}: refusing to run on an inherited database" }
  mise exec elixir@1.20.4-otp-29 erlang@29.1.2 -- mix ecto.create --quiet
  if ($LASTEXITCODE -ne 0) { throw "mix ecto.create failed with exit code ${LASTEXITCODE}: refusing to run on an inherited database" }
  mise exec elixir@1.20.4-otp-29 erlang@29.1.2 -- mix ecto.migrate --quiet
  if ($LASTEXITCODE -ne 0) { throw "mix ecto.migrate failed with exit code ${LASTEXITCODE}: refusing to run against an unmigrated database" }
} finally {
  Pop-Location
}

$server = $null
$fixturePid = $null
$specStatus = 1
$stopFailed = $false
try {
  # `set "NAME=value"` is the cmd form that does not fold the space before `&&`
  # into the value; plain `set NAME=value && …` would set "4021 " and "… .jsonl ".
  # --no-start: the fixture script installs its transports before it starts the
  # application, so no boot-time provider recovery can reach a real transport.
  # MIX_BUILD_PATH is set again here, beside PORT, so the fixture's own build
  # path is stated in the command that starts it instead of inherited silently.
  $fixtureCommand = "set `"MIX_BUILD_PATH=$instanceBuildPath`" && " +
    "set `"PHX_SERVER=true`" && set `"PORT=$Port`" && " +
    "set `"SYMMETRY_OPERATOR_TOKEN=$operatorToken`" && " +
    "set `"SYMMETRY_FIXTURE_IDENTITY=$identity`" && " +
    "set `"SYMMETRY_FIXTURE_IDENTITY_FILE=$identityFile`" && " +
    "set `"SYMMETRY_FIXTURE_TRANSPORT_LOG=$fixtureLog`" && " +
    "set `"SYMMETRY_FIXTURE_STATE_FILE=$stateFile`" && " +
    "set `"SYMMETRY_FIXTURE_UNAUTHORIZED_ACCOUNTS=expired-fixture`" && " +
    # The periodic sweep stays enabled (the real syncer child is part of the
    # application's supervision topology; /healthz itself reports the database,
    # scheduler, reconciler and provider access, not the syncer), but its
    # 5-minute cadence would re-synchronize the acceptance rows mid-run and turn
    # a failure or unavailability a test is observing back into a success. One
    # hour keeps it out of the run's window.
    "set `"SYMMETRY_INTEGRATION_SYNC_INTERVAL_MS=3600000`" && " +
    "cd /d `"$controlRoot`" && " +
    "mise exec elixir@1.20.4-otp-29 erlang@29.1.2 -- mix run --no-halt --no-start ../scripts/ci/portal-connection-fixture.exs"
  $server = Start-Process -FilePath "cmd.exe" -PassThru -NoNewWindow -RedirectStandardOutput $serverLog -RedirectStandardError "$serverLog.err" `
    -ArgumentList "/c", $fixtureCommand

  # Readiness: the fixture publishes this run's identity with the port it was
  # started on, its log carries the transport-install marker, and /healthz
  # answers ok for the real database, scheduler, reconciler and provider access.
  # The fixture keeps that topology: nothing here stops application children.
  $ready = $false
  $lastState = "no identity file yet"
  foreach ($attempt in 1..60) {
    if ($server.HasExited) { throw "fixture control plane exited before it became ready; see $serverLog.err" }
    $identityDocument = $null
    if (Test-Path $identityFile) {
      try { $identityDocument = Get-Content -Raw $identityFile | ConvertFrom-Json } catch { $identityDocument = $null }
    }
    $installed = (Test-Path $serverLog) -and (Select-String -Path $serverLog -SimpleMatch -Quiet "connection fixture transport installed")
    $identified = (Test-Path $serverLog) -and (Select-String -Path $serverLog -SimpleMatch -Quiet $identity)
    if ($identityDocument -and $identityDocument.identity -eq $identity -and [string]$identityDocument.port -eq [string]$Port -and $installed -and $identified) {
      try {
        $health = Invoke-WebRequest -UseBasicParsing -SkipHttpErrorCheck -Uri "$PortalUrl/healthz" -TimeoutSec 5
        if ($health.StatusCode -eq 200) { $ready = $true; break }
        $lastState = "healthz returned $($health.StatusCode)"
      } catch {
        $lastState = "healthz not reachable: $($_.Exception.Message)"
      }
    } else {
      $lastState = "identity not published yet (file=$([bool]$identityDocument) installed=$installed identified=$identified)"
    }
    Start-Sleep -Seconds 2
  }
  if (-not $ready) { throw "control plane did not become ready at $PortalUrl ($lastState)" }

  # The identity handshake above proves the listener on $Port is this run's
  # fixture; record its PID so cleanup stops exactly that process tree.
  $listeners = @(Get-NetTCPConnection -State Listen -LocalPort $Port -ErrorAction Stop | Select-Object -ExpandProperty OwningProcess -Unique)
  if ($listeners.Count -ne 1) { throw "expected exactly one listener on port $Port, found $($listeners.Count)" }
  $fixturePid = $listeners[0]

  Push-Location $repoRoot
  try {
    $env:SYMMETRY_PORTAL_URL = $PortalUrl
    $env:SYMMETRY_OPERATOR_TOKEN = $operatorToken
    $env:SYMMETRY_FIXTURE_IDENTITY = $identity
    $env:SYMMETRY_FIXTURE_IDENTITY_FILE = $identityFile
    $env:SYMMETRY_FIXTURE_STATE_FILE = $stateFile
    # --offline: pnpm verifies the pinned lockfile against the public registry by
    # default, which this host cannot reach; that check then stalls for minutes.
    # Offline it fails in about a second, prints "Lockfile failed supply-chain
    # policy check" with a node_modules warning, and the script still runs.
    # node_modules is already installed for this exact lockfile
    # (pnpm-lock.yaml sha256 2a0c9ef0…), so the warning is expected here.
    # The spec output is kept so the summary below can prove the file ran
    # instead of skipping.
    mise exec pnpm@12.17.0 -- pnpm --offline --filter symmetry-browser-acceptance test tests/portal-connections.spec.mjs 2>&1 | Tee-Object -FilePath $specLog
    $specStatus = $LASTEXITCODE
  } finally {
    Pop-Location
  }

  if ($server.HasExited) {
    Write-Host "fixture control plane exited during the spec run; see $serverLog.err"
    $specStatus = 1
  }

  # The spec skips itself when the harness environment is absent (so the
  # repository-wide commands still collect it); a skipped run exits 0, so the
  # acceptance gate reads the summary instead of trusting the exit code.
  # 22 = the file's 11 flows on the desktop and mobile projects.
  $summary = Select-String -Path $specLog -Pattern "(\d+) passed" -ErrorAction SilentlyContinue | Select-Object -Last 1
  $passed = if ($summary) { [int]$summary.Matches[0].Groups[1].Value } else { 0 }
  $skipped = [bool](Select-String -Path $specLog -Quiet -Pattern "\d+ skipped" -ErrorAction SilentlyContinue)
  if ($passed -ne 22 -or $skipped) {
    Write-Host "spec summary reports $passed passed (skipped=$skipped); expected 22 passed and no skips: see $specLog"
    $specStatus = 1
  }

  # The fixture's provider tokens are canaries: no token value may reach the
  # control plane's log, its error log or the recorded provider-call log. The
  # browser side (responses, DOM, web storage, cookies through the cookie jar,
  # request headers and bodies, console) is asserted by the spec. A missing
  # scan file or an empty provider-call log fails the run instead of passing
  # the scan without evidence.
  $canaries = @("gho_fixture_connection_token_canary", "fixture-ado-access-token-canary")
  $leaks = @()
  foreach ($file in @($serverLog, "$serverLog.err", $fixtureLog)) {
    if (-not (Test-Path $file)) { $leaks += "missing scan file: $file"; continue }
    foreach ($canary in $canaries) {
      if (Select-String -Path $file -SimpleMatch -Quiet $canary) { $leaks += "$file contains $canary" }
    }
  }
  if ((Test-Path $fixtureLog) -and (Get-Item $fixtureLog).Length -eq 0) {
    $leaks += "provider-call log $fixtureLog is empty: the canary scan has no recorded evidence"
  }
  if ($leaks.Count -gt 0) {
    Write-Host ("canary scan FAILED: " + ($leaks -join "; "))
    $specStatus = 1
  } else {
    Write-Host "canary scan: no fixture credential value in the control logs or the provider-call log"
  }

  Write-Host "fixture transport log: $fixtureLog"
  Write-Host "fixture state file: $stateFile"
  Write-Host "instance build path: $instanceBuildPath"
  Write-Host "control log: $serverLog"
} finally {
  # Stop only the process tree this harness started (cmd.exe → mise → mix → beam)
  # and the listener PID recorded while the identity handshake proved it was this
  # run's fixture. No other process on the port is ever killed: if the port is
  # still held after that, the run reports it instead of guessing.
  if ($server) { taskkill /PID $server.Id /T /F 2>$null | Out-Null }
  if ($fixturePid) { taskkill /PID $fixturePid /T /F 2>$null | Out-Null }
  foreach ($attempt in 1..10) {
    if (@(Get-NetTCPConnection -State Listen -LocalPort $Port -ErrorAction SilentlyContinue).Count -eq 0) { break }
    Start-Sleep -Milliseconds 500
  }
  if (@(Get-NetTCPConnection -State Listen -LocalPort $Port -ErrorAction SilentlyContinue).Count -gt 0) {
    Write-Host "port $Port is still held after stopping this run's fixture; a process this harness does not own was left alone"
    $stopFailed = $true
  }
}

if ($stopFailed) { exit 1 }
exit $specStatus
