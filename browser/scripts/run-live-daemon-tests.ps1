param(
  [string]$ControlUrl = "http://127.0.0.1:4000",
  [string]$OperatorToken = "development-operator-token",
  [string]$EnrollmentToken = "development-enrollment-token"
)

$ErrorActionPreference = "Stop"
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot "../..")).Path
$daemonRoot = Join-Path $repoRoot "daemon"
$runId = [guid]::NewGuid().ToString("N")
$fixtureSuffix = $runId.Substring(0, 8)
$runRoot = Join-Path $env:TEMP ("symmetry-goal-02-live-" + $runId)
$daemonBinary = Join-Path $runRoot "symmetry-daemon.exe"
$agentBinary = Join-Path $runRoot "symmetry-fake-agent.exe"

New-Item -ItemType Directory -Force -Path $runRoot | Out-Null

Push-Location $daemonRoot
try {
  go build -o $daemonBinary ./cmd/symmetry-daemon
  go build -o $agentBinary ./cmd/symmetry-fake-agent
} finally {
  Pop-Location
}

function New-FixtureConfig {
  param(
    [string]$Name,
    [string]$Profile,
    [string]$Workspace
  )

  $stateDir = Join-Path $runRoot "$Name-state"
  $workspaceDir = Join-Path $runRoot "$Name-workspace"
  New-Item -ItemType Directory -Force -Path $stateDir, $workspaceDir | Out-Null

  $config = @{
    control_plane_url = $ControlUrl
    allow_insecure_http = $true
    state_dir = $stateDir
    machine_name = "goal2-$Name-$fixtureSuffix-machine"
    agent_profiles = @{
      $Profile = @{
        command = $agentBinary
        args = @()
        input_mode = "json"
        interactive = $true
        event_format = "jsonl"
        env_allowlist = @("SYMMETRY_FAKE_AGENT_MODE")
      }
    }
    workspaces = @{
      $Workspace = @{
        policy = "existing_checkout"
        path = $workspaceDir
        cleanup = "never"
      }
    }
    runtime = @{
      runtime_key = "goal2-$Name-$fixtureSuffix-runtime"
      name = "Goal 2 $Name runtime"
      capacity = 1
      agent_profile = $Profile
      workspace = $Workspace
    }
  }

  $path = Join-Path $runRoot "$Name-daemon.json"
  $config | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath $path -Encoding utf8NoBOM
  return $path
}

$waitConfig = New-FixtureConfig -Name "wait" -Profile "goal2-wait-$fixtureSuffix" -Workspace "goal2-wait-$fixtureSuffix"
$retryConfig = New-FixtureConfig -Name "retry" -Profile "goal2-retry-$fixtureSuffix" -Workspace "goal2-retry-$fixtureSuffix"
$cancelConfig = New-FixtureConfig -Name "cancel" -Profile "goal2-cancel-$fixtureSuffix" -Workspace "goal2-cancel-$fixtureSuffix"
$historyConfig = New-FixtureConfig -Name "history" -Profile "goal2-history-$fixtureSuffix" -Workspace "goal2-history-$fixtureSuffix"
$processes = @()

try {
  $processes += Start-Process -FilePath $daemonBinary `
    -ArgumentList @("-config", $waitConfig) `
    -WorkingDirectory $runRoot `
    -WindowStyle Hidden `
    -RedirectStandardOutput (Join-Path $runRoot "wait.stdout.log") `
    -RedirectStandardError (Join-Path $runRoot "wait.stderr.log") `
    -Environment @{
      SYMMETRY_ENROLLMENT_TOKEN = $EnrollmentToken
      SYMMETRY_FAKE_AGENT_MODE = "wait_input"
    } `
    -PassThru

  $processes += Start-Process -FilePath $daemonBinary `
    -ArgumentList @("-config", $cancelConfig) `
    -WorkingDirectory $runRoot `
    -WindowStyle Hidden `
    -RedirectStandardOutput (Join-Path $runRoot "cancel.stdout.log") `
    -RedirectStandardError (Join-Path $runRoot "cancel.stderr.log") `
    -Environment @{
      SYMMETRY_ENROLLMENT_TOKEN = $EnrollmentToken
      SYMMETRY_FAKE_AGENT_MODE = "slow"
    } `
    -PassThru

  $processes += Start-Process -FilePath $daemonBinary `
    -ArgumentList @("-config", $retryConfig) `
    -WorkingDirectory $runRoot `
    -WindowStyle Hidden `
    -RedirectStandardOutput (Join-Path $runRoot "retry.stdout.log") `
    -RedirectStandardError (Join-Path $runRoot "retry.stderr.log") `
    -Environment @{
      SYMMETRY_ENROLLMENT_TOKEN = $EnrollmentToken
      SYMMETRY_FAKE_AGENT_MODE = "fail_once_then_evidence_success"
    } `
    -PassThru

  $processes += Start-Process -FilePath $daemonBinary `
    -ArgumentList @("-config", $historyConfig) `
    -WorkingDirectory $runRoot `
    -WindowStyle Hidden `
    -RedirectStandardOutput (Join-Path $runRoot "history.stdout.log") `
    -RedirectStandardError (Join-Path $runRoot "history.stderr.log") `
    -Environment @{
      SYMMETRY_ENROLLMENT_TOKEN = $EnrollmentToken
      SYMMETRY_FAKE_AGENT_MODE = "history_success"
    } `
    -PassThru

  $env:SYMMETRY_PORTAL_URL = $ControlUrl
  $env:SYMMETRY_OPERATOR_TOKEN = $OperatorToken
  $env:SYMMETRY_LIVE_DAEMON = "1"
  $env:SYMMETRY_LIVE_FIXTURE_SUFFIX = $fixtureSuffix

  Push-Location (Join-Path $repoRoot "browser")
  try {
    npm run test:live
    if ($LASTEXITCODE -ne 0) {
      throw "Live daemon browser tests failed with exit code $LASTEXITCODE. Logs: $runRoot"
    }
  } finally {
    Pop-Location
  }
} finally {
  foreach ($process in $processes) {
    if ($process -and -not $process.HasExited) {
      Stop-Process -Id $process.Id -ErrorAction SilentlyContinue
    }
  }
}

Write-Host "Live daemon browser tests passed. Logs: $runRoot"
