param(
  [string]$PortalUrl = "http://127.0.0.1:4030",
  [int]$Port = 4030,
  [int]$DbPort = 55441,
  [string]$DbName = "symmetry_0007c_test",
  # Extra Playwright arguments for focused iteration (for example
  # "-SpecArgs '--project=desktop-chromium'"). The acceptance run uses the
  # default: both Playwright projects, no filters.
  [string]$SpecArgs = ""
)

# 0007c safe work-planning browser acceptance. Starts the real control plane
# (dev configuration, real PostgreSQL, real router/controllers) on a loopback
# port this harness owns, runs the real-API Playwright spec, then stops the
# server it started and reports the log.
#
# The run fails closed before any browser flow: the target must be the loopback
# port this harness starts, that port must be free before anything starts, the
# acceptance database is recreated and migrated for this run, the server
# compiles into this instance's own build path (MIX_BUILD_PATH under
# control/_build/instances, never the shared control/_build/dev), and it must
# answer /healthz before the spec runs. The spec itself skips without the
# harness environment and the summary assertion below turns a skipped run into
# a failure.
$ErrorActionPreference = "Stop"
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot "../..")).Path
$controlRoot = Join-Path $repoRoot "control"
$runRoot = Join-Path $env:TEMP ("symmetry-0007c-planning-" + [guid]::NewGuid().ToString("N").Substring(0, 8))
New-Item -ItemType Directory -Force -Path $runRoot | Out-Null
$serverLog = Join-Path $runRoot "control.log"
$specLog = Join-Path $runRoot "spec.log"

# The pinned toolchain every mix invocation of this harness runs under. The
# control-instance isolation contract test drives this script with a recording
# `mise` stub and asserts these exact argv bytes on every recorded invocation
# (assertInstanceBuildPath in control-instance-isolation.test.mjs), so the pin
# lives once, here, for both the PowerShell calls and the cmd launcher below.
$mixExecutable = "mise"
$mixPrefix = @("exec", "elixir@1.20.4-otp-29", "erlang@29.1.2", "--", "mix")

$target = [Uri]$PortalUrl
# Uri.Host brackets an IPv6 literal ('[::1]'); the IDN host form is the
# unbracketed name the allowlist and the probe below compare. IPv6 loopback is
# refused on purpose: this harness starts the repository dev configuration, and
# control/config/dev.exs binds http to {127, 0, 0, 1} only, so `http://[::1]`
# could never answer here - it would reach the readiness loop and fail as
# "control plane did not become ready" instead of at this preflight.
if ($target.Scheme -ne "http" -or $target.IdnHost -notin @("127.0.0.1", "localhost")) {
  throw "PortalUrl must be http on 127.0.0.1 or localhost, got '$PortalUrl'"
}
if ($target.Port -ne $Port) {
  throw "PortalUrl port ($($target.Port)) disagrees with the harness port ($Port): refusing to target a process this harness does not own"
}
# The port preflight below is check-then-use: two same-port starts both pass it
# and then share one _build/instances/planning-<port> path and this database
# until one fails at bind, which is the 2026-11-17 compile-interference window.
# The named mutex is taken before the preflight and every mix invocation and
# held through cleanup, so a second same-port harness fails closed here before
# any mix call. An abandoned mutex means the previous owner died without
# releasing it, so acquiring it is safe.
$portLockName = "Local\symmetry-browser-harness-port-$Port"
$portLock = New-Object System.Threading.Mutex($false, $portLockName)
$portLockHeld = $false
try { $portLockHeld = $portLock.WaitOne(0) } catch [System.Threading.AbandonedMutexException] { $portLockHeld = $true }
if (-not $portLockHeld) {
  throw "port $Port is already owned by another harness instance (lock $portLockName is held): refusing to share its database and instance build path"
}
if (@(Get-NetTCPConnection -State Listen -LocalPort $Port -ErrorAction SilentlyContinue).Count -gt 0) {
  throw "port $Port is already in use: the control plane this harness starts cannot answer PortalUrl"
}
$probe = New-Object System.Net.Sockets.TcpClient
try {
  $attempt = $probe.BeginConnect($target.IdnHost, $Port, $null, $null)
  if ($attempt.AsyncWaitHandle.WaitOne(1000) -and $probe.Connected) {
    throw "port $Port already accepts connections: refusing to run against a foreign listener"
  }
} finally {
  $probe.Close()
}

# Per-run operator token: a foreign control plane on this port cannot satisfy
# the login the spec performs with it.
$operatorToken = [guid]::NewGuid().ToString("N")

# Instance build isolation: this instance compiles into its own per-port path
# under control/_build/instances (never the shared control/_build/dev), an
# inherited MIX_BUILD_PATH is overridden and reported rather than trusted, and
# deps sources stay shared under control/deps - only compiled output moves.
# The rationale (the 2026-11-17 cross-instance observation included) and the
# asserted contract are recorded in browser/README.md and
# browser/scripts/control-instance-isolation.test.mjs.
$instanceBuildPath = Join-Path $controlRoot ("_build/instances/planning-" + $Port)
if ($env:MIX_BUILD_PATH) {
  Write-Host "ignoring inherited MIX_BUILD_PATH '$env:MIX_BUILD_PATH': this instance compiles into $instanceBuildPath"
}
$env:MIX_BUILD_PATH = $instanceBuildPath

$env:POSTGRES_HOST = "127.0.0.1"
$env:POSTGRES_PORT = [string]$DbPort
$env:POSTGRES_USER = "postgres"
$env:POSTGRES_PASSWORD = "postgres"
$env:POSTGRES_DB = $DbName

Push-Location $controlRoot
try {
  # The acceptance database is dedicated to this harness and recreated on every
  # run, so a growing workspace from earlier runs cannot slow the reads or make
  # the run less reproducible.
  & $mixExecutable @mixPrefix ecto.drop --quiet
  if ($LASTEXITCODE -ne 0) { throw "mix ecto.drop failed with exit code ${LASTEXITCODE}: refusing to run on an inherited database" }
  & $mixExecutable @mixPrefix ecto.create --quiet
  if ($LASTEXITCODE -ne 0) { throw "mix ecto.create failed with exit code ${LASTEXITCODE}: refusing to run on an inherited database" }
  & $mixExecutable @mixPrefix ecto.migrate --quiet
  if ($LASTEXITCODE -ne 0) { throw "mix ecto.migrate failed with exit code ${LASTEXITCODE}: refusing to run against an unmigrated database" }
} finally {
  Pop-Location
}

$server = $null
$specStatus = 1
$stopFailed = $false
try {
  # `set "NAME=value"` is the cmd form that does not fold the space before `&&`
  # into the value. The server runs the repository's dev configuration so the
  # built frontend assets staged into control/priv/static/portal are served and
  # the real router, controllers and PostgreSQL handle every request.
  #
  # The launcher loops instead of exiting with the server: this host has killed
  # the BEAM mid-run (process gone, empty stderr, no crash dump), and a restart
  # keeps the remaining cases meaningful while the log records every restart.
  # A restart never turns a failure into a pass - every case still has to pass.
  # MIX_BUILD_PATH is set again here, beside PORT, so the launcher states the
  # instance's own build path instead of relying on what it inherited.
  $serverCommand = ":serve`r`n" +
    "echo [harness] control plane start %TIME%`r`n" +
    "set `"MIX_BUILD_PATH=$instanceBuildPath`" && " +
    "set `"PORT=$Port`" && " +
    "set `"SYMMETRY_OPERATOR_TOKEN=$operatorToken`" && " +
    "cd /d `"$controlRoot`" && " +
    "$mixExecutable $($mixPrefix -join ' ') phx.server`r`n" +
    "echo [harness] control plane exited with %ERRORLEVEL% at %TIME%`r`n" +
    "timeout /t 2 /nobreak > nul`r`n" +
    "goto serve"
  $serverLauncher = Join-Path $runRoot "control.cmd"
  Set-Content -LiteralPath $serverLauncher -Value $serverCommand -Encoding ascii
  # stdin comes from an empty file: an inherited console handle that closes
  # under the harness kills a BEAM whose standard_error device disappears.
  $serverStdin = Join-Path $runRoot "control.stdin"
  Set-Content -LiteralPath $serverStdin -Value "" -NoNewline
  $server = Start-Process -FilePath "cmd.exe" -PassThru -NoNewWindow -RedirectStandardOutput $serverLog -RedirectStandardError "$serverLog.err" `
    -RedirectStandardInput $serverStdin -ArgumentList "/c", $serverLauncher

  $ready = $false
  $lastState = "no answer yet"
  foreach ($attempt in 1..90) {
    if ($server.HasExited) { throw "control plane exited before it became ready; see $serverLog.err" }
    try {
      $health = Invoke-WebRequest -UseBasicParsing -SkipHttpErrorCheck -Uri "$PortalUrl/healthz" -TimeoutSec 5
      if ($health.StatusCode -eq 200) { $ready = $true; break }
      $lastState = "healthz returned $($health.StatusCode)"
    } catch {
      $lastState = "healthz not reachable: $($_.Exception.Message)"
    }
    Start-Sleep -Seconds 2
  }
  if (-not $ready) { throw "control plane did not become ready at $PortalUrl ($lastState)" }

  # The listener must serve the real application (the server-rendered login
  # form) and the staged SPA build — the shell document and the script it names;
  # the build itself is staged from the working tree into
  # control/priv/static/portal before the run, so an unstaged build fails here
  # instead of inside the browser. The authenticated shell routes are exercised
  # by the spec itself, not probed here.
  $login = Invoke-WebRequest -UseBasicParsing -SkipHttpErrorCheck -Uri "$PortalUrl/portal/login" -TimeoutSec 10
  if ($login.StatusCode -ne 200 -or $login.Content -notmatch "Operator token") {
    throw "login page did not serve the operator form (status $($login.StatusCode))"
  }
  $shellIndex = Join-Path $controlRoot "priv/static/portal/index.html"
  if (-not (Test-Path -LiteralPath $shellIndex)) {
    throw "the built frontend is not staged at control/priv/static/portal; run the frontend build before this harness"
  }
  $assetPath = ([regex]::Match((Get-Content -Raw $shellIndex), 'src="(?<path>[^"]+\.js)"').Groups["path"].Value)
  if (-not $assetPath) { throw "the staged shell document names no built script" }
  $asset = Invoke-WebRequest -UseBasicParsing -SkipHttpErrorCheck -Uri "$PortalUrl$assetPath" -TimeoutSec 10
  if ($asset.StatusCode -ne 200 -or ([string]$asset.Content) -notmatch "createElement") {
    throw "the staged shell script was not served (status $($asset.StatusCode) at $assetPath)"
  }

  Push-Location $repoRoot
  try {
    $env:SYMMETRY_PORTAL_URL = $PortalUrl
    $env:SYMMETRY_OPERATOR_TOKEN = $operatorToken
    $env:SYMMETRY_PLANNING_HARNESS = "1"
    # Per-run artifact directory: every capture, screenshot and failure context
    # of this run stays together with the logs below.
    $env:SYMMETRY_PLAYWRIGHT_OUTPUT = Join-Path $runRoot "playwright"
    # --offline: pnpm verifies the pinned lockfile against the public registry by
    # default, which this host cannot reach; that check then stalls for minutes.
    # Offline it fails in about a second, prints the lockfile warning, and the
    # script still runs against the node_modules installed for this lockfile.
    $playwrightArgs = @("tests/portal-work-planning.spec.mjs")
    if ($SpecArgs.Trim()) { $playwrightArgs += $SpecArgs.Trim().Split(" ") }
    mise exec pnpm@12.17.0 -- pnpm --offline --filter symmetry-browser-acceptance test @playwrightArgs 2>&1 | Tee-Object -FilePath $specLog
    $specStatus = $LASTEXITCODE
  } finally {
    Pop-Location
  }

  if ($server.HasExited) {
    Write-Host "control plane exited during the spec run; see $serverLog.err"
    $specStatus = 1
  }

  # The launcher echoes its own command line, so count only the script's own
  # output lines (the echo prints the text without the leading bracket).
  $starts = @(Select-String -Path $serverLog -Pattern "^\[harness\] control plane start" -ErrorAction SilentlyContinue).Count
  if ($starts -gt 1) {
    Write-Host "control plane restarted $($starts - 1) time(s) during this run; every case still had to pass: see $serverLog"
  }

  # The spec skips itself when the harness environment is absent (so the
  # repository-wide commands still collect it); a skipped run exits 0, so the
  # acceptance gate reads the summary instead of trusting the exit code.
  $summary = Select-String -Path $specLog -Pattern "(\d+) passed" -ErrorAction SilentlyContinue | Select-Object -Last 1
  $passed = if ($summary) { [int]$summary.Matches[0].Groups[1].Value } else { 0 }
  $skipped = [bool](Select-String -Path $specLog -Quiet -Pattern "\d+ skipped" -ErrorAction SilentlyContinue)
  if ($passed -lt 1 -or $skipped) {
    Write-Host "spec summary reports $passed passed (skipped=$skipped); a skipped or empty run is not acceptance: see $specLog"
    $specStatus = 1
  }

  Write-Host "instance build path: $instanceBuildPath"
  Write-Host "control log: $serverLog"
  Write-Host "spec log: $specLog"
  Write-Host "browser artifacts: $env:SYMMETRY_PLAYWRIGHT_OUTPUT"
} finally {
  if ($server) { taskkill /PID $server.Id /T /F 2>$null | Out-Null }
  foreach ($attempt in 1..10) {
    if (@(Get-NetTCPConnection -State Listen -LocalPort $Port -ErrorAction SilentlyContinue).Count -eq 0) { break }
    Start-Sleep -Milliseconds 500
  }
  if (@(Get-NetTCPConnection -State Listen -LocalPort $Port -ErrorAction SilentlyContinue).Count -gt 0) {
    Write-Host "port $Port is still held after stopping this run's server; a process this harness does not own was left alone"
    $stopFailed = $true
  }
}

$portLock.ReleaseMutex()
if ($stopFailed) { exit 1 }
exit $specStatus
