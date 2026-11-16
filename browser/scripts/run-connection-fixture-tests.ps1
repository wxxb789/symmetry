param(
  [string]$PortalUrl = "http://127.0.0.1:4021",
  [int]$Port = 4021,
  [string]$OperatorToken = "development-operator-token"
)

# Connection-management browser acceptance on the fixed design's
# /portal/connections surface. Starts the real control plane with the controlled
# provider fixture transport (no provider host is contacted), runs the real-API
# Playwright spec, then stops the fixture server and reports the fixture log.
$ErrorActionPreference = "Stop"
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot "../..")).Path
$controlRoot = Join-Path $repoRoot "control"
$runRoot = Join-Path $env:TEMP ("symmetry-0007d-connections-" + [guid]::NewGuid().ToString("N").Substring(0, 8))
New-Item -ItemType Directory -Force -Path $runRoot | Out-Null
$fixtureLog = Join-Path $runRoot "fixture-calls.jsonl"
$serverLog = Join-Path $runRoot "control.log"

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
  mise exec elixir@1.20.4-otp-29 erlang@29.1.2 -- mix ecto.create --quiet
  mise exec elixir@1.20.4-otp-29 erlang@29.1.2 -- mix ecto.migrate --quiet
} finally {
  Pop-Location
}

$server = $null
try {
  # `set "NAME=value"` is the cmd form that does not fold the space before `&&`
  # into the value; plain `set NAME=value && …` would set "4021 " and "… .jsonl ".
  $fixtureCommand = "set `"PHX_SERVER=true`" && set `"PORT=$Port`" && " +
    "set `"SYMMETRY_FIXTURE_TRANSPORT_LOG=$fixtureLog`" && " +
    "set `"SYMMETRY_FIXTURE_UNAUTHORIZED_ACCOUNTS=expired-fixture`" && " +
    "cd /d `"$controlRoot`" && " +
    "mise exec elixir@1.20.4-otp-29 erlang@29.1.2 -- mix run --no-halt ../scripts/ci/portal-connection-fixture.exs"
  $server = Start-Process -FilePath "cmd.exe" -PassThru -NoNewWindow -RedirectStandardOutput $serverLog -RedirectStandardError "$serverLog.err" `
    -ArgumentList "/c", $fixtureCommand

  $ready = $false
  foreach ($attempt in 1..60) {
    if ($server.HasExited) { throw "fixture control plane exited before it became ready; see $serverLog.err" }
    try {
      # The fixture stops the orchestration scheduler and reconciler on purpose,
      # so /healthz reports those two as down. Readiness here is the endpoint
      # answering the portal login document.
      Invoke-WebRequest -UseBasicParsing -Uri "$PortalUrl/portal/login" -TimeoutSec 3 | Out-Null
      $ready = $true
      break
    } catch {
      Start-Sleep -Seconds 2
    }
  }
  if (-not $ready) { throw "control plane did not become ready at $PortalUrl/portal/login" }

  Push-Location $repoRoot
  try {
    $env:SYMMETRY_PORTAL_URL = $PortalUrl
    $env:SYMMETRY_OPERATOR_TOKEN = $OperatorToken
    # --offline: pnpm verifies the pinned lockfile against the public registry by
    # default, which this host cannot reach; that check then stalls for minutes.
    # Offline it fails in about a second, prints "Lockfile failed supply-chain
    # policy check" with a node_modules warning, and the script still runs.
    # node_modules is already installed for this exact lockfile
    # (pnpm-lock.yaml sha256 2a0c9ef0…), so the warning is expected here.
    mise exec pnpm@12.17.0 -- pnpm --offline --filter symmetry-browser-acceptance test tests/portal-connections.spec.mjs
    $specStatus = $LASTEXITCODE
  } finally {
    Pop-Location
  }

  # The fixture's provider tokens are canaries: no token value may reach the
  # control plane's log, its error log or the recorded provider-call log. The
  # browser side (responses, DOM, web storage, console) is asserted by the spec.
  $canaries = @("gho_fixture_connection_token_canary", "fixture-ado-access-token-canary")
  $leaks = @()
  foreach ($file in @($serverLog, "$serverLog.err", $fixtureLog)) {
    if (-not (Test-Path $file)) { continue }
    foreach ($canary in $canaries) {
      if (Select-String -Path $file -SimpleMatch -Quiet $canary) { $leaks += "$file contains $canary" }
    }
  }
  if ($leaks.Count -gt 0) {
    Write-Host ("canary scan FAILED: " + ($leaks -join "; "))
    $specStatus = 1
  } else {
    Write-Host "canary scan: no fixture credential value in the control logs or the provider-call log"
  }

  Write-Host "fixture transport log: $fixtureLog"
  Write-Host "control log: $serverLog"
  exit $specStatus
} finally {
  # The fixture runs as cmd.exe → mise → mix → beam. Stopping the cmd.exe wrapper
  # leaves beam listening on the port, so the whole tree is killed and the port is
  # then re-checked until the control plane is gone.
  if ($server) { taskkill /PID $server.Id /T /F 2>$null | Out-Null }
  foreach ($attempt in 1..10) {
    $listeners = @(Get-NetTCPConnection -State Listen -LocalPort $Port -ErrorAction SilentlyContinue)
    if ($listeners.Count -eq 0) { break }
    foreach ($listener in $listeners) {
      $owner = Get-Process -Id $listener.OwningProcess -ErrorAction SilentlyContinue
      if ($owner -and $owner.ProcessName -match "beam|erl|escript|cmd|mise") {
        taskkill /PID $listener.OwningProcess /T /F 2>$null | Out-Null
      }
    }
    Start-Sleep -Milliseconds 500
  }
}
