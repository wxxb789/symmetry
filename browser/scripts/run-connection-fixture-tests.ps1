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
# fixture must publish this run's identity file, and /healthz must report the
# full topology healthy. The spec itself re-checks the identity file and logs in
# with this run's operator token before any provider check.
$ErrorActionPreference = "Stop"
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot "../..")).Path
$controlRoot = Join-Path $repoRoot "control"
$runRoot = Join-Path $env:TEMP ("symmetry-0007d-connections-" + [guid]::NewGuid().ToString("N").Substring(0, 8))
New-Item -ItemType Directory -Force -Path $runRoot | Out-Null
$fixtureLog = Join-Path $runRoot "fixture-calls.jsonl"
$serverLog = Join-Path $runRoot "control.log"
$specLog = Join-Path $runRoot "spec.log"
$identityFile = Join-Path $runRoot "fixture-identity.json"

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

$env:POSTGRES_HOST = "127.0.0.1"
$env:POSTGRES_PORT = "55440"
$env:POSTGRES_USER = "postgres"
$env:POSTGRES_PASSWORD = "postgres"
$env:POSTGRES_DB = "symmetry_0007d_test"

Push-Location $controlRoot
try {
  # The acceptance database is dedicated to this fixture, and previous runs leave
  # projects, connections, resources and work items behind (other portal specs use
  # the same database). A growing connection list makes every page read slower and
  # the run less reproducible, and the rows are referenced across tables, so the
  # fixture recreates the database instead of deleting rows selectively.
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
  $fixtureCommand = "set `"PHX_SERVER=true`" && set `"PORT=$Port`" && " +
    "set `"SYMMETRY_OPERATOR_TOKEN=$operatorToken`" && " +
    "set `"SYMMETRY_FIXTURE_IDENTITY=$identity`" && " +
    "set `"SYMMETRY_FIXTURE_IDENTITY_FILE=$identityFile`" && " +
    "set `"SYMMETRY_FIXTURE_TRANSPORT_LOG=$fixtureLog`" && " +
    "set `"SYMMETRY_FIXTURE_UNAUTHORIZED_ACCOUNTS=expired-fixture`" && " +
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
  # 10 = the file's 5 flows on the desktop and mobile projects.
  $summary = Select-String -Path $specLog -Pattern "(\d+) passed" -ErrorAction SilentlyContinue | Select-Object -Last 1
  $passed = if ($summary) { [int]$summary.Matches[0].Groups[1].Value } else { 0 }
  $skipped = [bool](Select-String -Path $specLog -Quiet -Pattern "\d+ skipped" -ErrorAction SilentlyContinue)
  if ($passed -ne 10 -or $skipped) {
    Write-Host "spec summary reports $passed passed (skipped=$skipped); expected 10 passed and no skips: see $specLog"
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
