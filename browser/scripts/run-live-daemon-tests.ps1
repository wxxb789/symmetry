param(
  [string]$ControlUrl = "http://127.0.0.1:4000",
  [string]$OperatorToken = "development-operator-token",
  [string]$EnrollmentToken = "development-enrollment-token"
)

$ErrorActionPreference = "Stop"
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot "../..")).Path
$daemonRoot = Join-Path $repoRoot "daemon"
$browserRoot = Join-Path $repoRoot "browser"
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
    [string]$Workspace,
    [string]$WorkspacePolicy = "existing_checkout",
    [string]$WorkspaceRepository = "",
    [string]$WorkspaceRoot = "",
    [string]$WorkspaceRef = "",
    [string]$HarnessKind = "",
    [string]$HarnessCommand = "",
    [string]$HarnessVersion = "",
    [string]$AdapterVersion = "",
    [string]$RepositoryResourceID = "",
    [int]$AdapterProtocolVersion = 0
  )

  $stateDir = Join-Path $runRoot "$Name-state"
  $workspaceDir = Join-Path $runRoot "$Name-workspace"
  New-Item -ItemType Directory -Force -Path $stateDir, $workspaceDir | Out-Null

  $command = if ($HarnessCommand) { $HarnessCommand } else { $agentBinary }
  [string[]]$envAllowlist = if ($HarnessCommand) { @() } else { @("SYMMETRY_FAKE_AGENT_MODE") }

  $config = @{
    control_plane_url = $ControlUrl
    allow_insecure_http = $true
    state_dir = $stateDir
    machine_name = "goal2-$Name-$fixtureSuffix-machine"
    agent_profiles = @{
      $Profile = @{
        command = $command
        args = @()
        input_mode = "json"
        interactive = $true
        event_format = "jsonl"
        env_allowlist = $envAllowlist
      }
    }
    workspaces = @{
      $Workspace = @{
        policy = $WorkspacePolicy
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

  if ($WorkspaceRepository) { $config.workspaces[$Workspace].repository = $WorkspaceRepository }
  if ($WorkspaceRoot) { $config.workspaces[$Workspace].root = $WorkspaceRoot }
  if ($WorkspaceRef) { $config.workspaces[$Workspace].ref = $WorkspaceRef }

  if ($HarnessKind) {
    $config.runtime.harness_kind = $HarnessKind
    # The daemon re-probes the executable and refuses a configured version that
    # does not equal the probed one; the configured value is only there because
    # adapter metadata must name all four members.
    $config.runtime.harness_version = $HarnessVersion
    $config.runtime.adapter_version = $AdapterVersion
    $config.runtime.adapter_protocol_version = $AdapterProtocolVersion
    if ($RepositoryResourceID) { $config.runtime.repository_resource_id = $RepositoryResourceID }
  }

  $path = Join-Path $runRoot "$Name-daemon.json"
  $config | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath $path -Encoding utf8NoBOM
  return $path
}

$waitConfig = New-FixtureConfig -Name "wait" -Profile "goal2-wait-$fixtureSuffix" -Workspace "goal2-wait-$fixtureSuffix"
$retryConfig = New-FixtureConfig -Name "retry" -Profile "goal2-retry-$fixtureSuffix" -Workspace "goal2-retry-$fixtureSuffix"
$cancelConfig = New-FixtureConfig -Name "cancel" -Profile "goal2-cancel-$fixtureSuffix" -Workspace "goal2-cancel-$fixtureSuffix"
$historyConfig = New-FixtureConfig -Name "history" -Profile "goal2-history-$fixtureSuffix" -Workspace "goal2-history-$fixtureSuffix"
$raceConfig = New-FixtureConfig -Name "race" -Profile "goal2-race-$fixtureSuffix" -Workspace "goal2-race-$fixtureSuffix"

# The real-harness runtime: an installed harness adapter registers its own
# capability record (harness identity, version and per-operation support) so the
# runtime page can be compared with the daemon's own registration payload. It is
# never the target of a dispatched run: the acceptance work items bind the fake
# fixtures above. Every pinned executable is probed on this host; when none of
# the resolved binaries matches its repository pin the daemon still registers an
# honest all-unsupported record, which is the same comparison.
$harnessProvider = @(
  @{ Command = (Get-Command pi -ErrorAction SilentlyContinue).Source; Kind = "pi"; AdapterVersion = "pi-rpc-v1"; ProtocolVersion = 1 },
  @{ Command = (Get-Command omo -ErrorAction SilentlyContinue).Source; Kind = "omo"; AdapterVersion = "omo-rpc-v1"; ProtocolVersion = 1 }
) | Where-Object { $_.Command } | Select-Object -First 1

# The probed native version line, read from the same executable the daemon will
# resolve. It must equal the daemon's own probe result; a mismatch makes the
# daemon refuse to start, which the acceptance run reports as a failure rather
# than passing with an invented version.
$harnessVersion = $null
if ($harnessProvider) {
  $versionOutput = & $harnessProvider.Kind --version 2>$null | Select-Object -First 1
  if ($LASTEXITCODE -eq 0 -and $versionOutput) { $harnessVersion = ($versionOutput -split "`n")[0].Trim() }
}

$harnessRuntimeKey = $null
$harnessRepositoryFile = Join-Path $runRoot "harness-repository.json"
$processes = @()

# Creates and starts the real-harness runtime. The adapter's repository binding
# is the resource the provisioning case created with the real browser session,
# so this runs after that case and before the capability comparison.
function Start-HarnessDaemon {
  $repository = Get-Content -LiteralPath $harnessRepositoryFile -Raw | ConvertFrom-Json
  $script:harnessRuntimeKey = "goal2-harness-$fixtureSuffix"
  # The daemon clones the bound repository for each run, so the fixture is a
  # real local git checkout rather than a synthetic URL.
  $repositoryPath = Join-Path $runRoot "harness-repository"
  New-Item -ItemType Directory -Force -Path $repositoryPath | Out-Null
  if (-not (Test-Path -LiteralPath (Join-Path $repositoryPath ".git"))) {
    git -C $repositoryPath init --quiet --initial-branch=main
    Set-Content -LiteralPath (Join-Path $repositoryPath "README.md") -Value "Live harness acceptance fixture repository."
    git -C $repositoryPath add README.md
    git -C $repositoryPath -c user.email=fixture@example.invalid -c user.name=fixture commit --quiet -m "fixture"
  }
  # A native Goal-capable runtime must obtain its working directory per run from
  # the bound repository, so the workspace uses the git worktree policy.
  $script:harnessConfig = New-FixtureConfig -Name "harness" -Profile "goal2-harness-$fixtureSuffix" -Workspace "goal2-harness-$fixtureSuffix" `
    -WorkspacePolicy "git_worktree" -WorkspaceRepository ((Resolve-Path $repositoryPath).Path) `
    -WorkspaceRoot (Join-Path $runRoot "harness-worktrees") `
    -WorkspaceRef "main" `
    -HarnessKind $harnessProvider.Kind -HarnessCommand $harnessProvider.Command -HarnessVersion $harnessVersion `
    -AdapterVersion $harnessProvider.AdapterVersion -AdapterProtocolVersion $harnessProvider.ProtocolVersion `
    -RepositoryResourceID $repository.repository_resource_id
  $script:processes += Start-Process -FilePath $daemonBinary `
    -ArgumentList @("-config", $harnessConfig) `
    -WorkingDirectory $runRoot `
    -WindowStyle Hidden `
    -RedirectStandardOutput (Join-Path $runRoot "harness.stdout.log") `
    -RedirectStandardError (Join-Path $runRoot "harness.stderr.log") `
    -Environment @{
      SYMMETRY_ENROLLMENT_TOKEN = $EnrollmentToken
    } `
    -PassThru
}

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

  $processes += Start-Process -FilePath $daemonBinary `
    -ArgumentList @("-config", $raceConfig) `
    -WorkingDirectory $runRoot `
    -WindowStyle Hidden `
    -RedirectStandardOutput (Join-Path $runRoot "race.stdout.log") `
    -RedirectStandardError (Join-Path $runRoot "race.stderr.log") `
    -Environment @{
      SYMMETRY_ENROLLMENT_TOKEN = $EnrollmentToken
      SYMMETRY_FAKE_AGENT_MODE = "success"
    } `
    -PassThru

  $env:SYMMETRY_PORTAL_URL = $ControlUrl
  $env:SYMMETRY_OPERATOR_TOKEN = $OperatorToken
  $env:SYMMETRY_LIVE_DAEMON = "1"
  $env:SYMMETRY_LIVE_FIXTURE_SUFFIX = $fixtureSuffix
  $env:SYMMETRY_LIVE_HARNESS_REPOSITORY_FILE = $harnessRepositoryFile
  Remove-Item -LiteralPath $harnessRepositoryFile -ErrorAction SilentlyContinue
  # Screenshots and observation JSON land in the run folder unless an existing
  # environment override is already set.
  if (-not $env:SYMMETRY_PLAYWRIGHT_OUTPUT) {
    $env:SYMMETRY_PLAYWRIGHT_OUTPUT = Join-Path $runRoot "playwright-results"
  }
  $playwrightReport = if ($env:PLAYWRIGHT_HTML_OUTPUT_DIR) {
    $env:PLAYWRIGHT_HTML_OUTPUT_DIR
  } else {
    Join-Path $browserRoot "playwright-report"
  }

  Push-Location $browserRoot
  try {
    if ($harnessProvider -and $harnessVersion) {
      # Phase 1 provisions the repository resource with the real browser
      # session; the native adapter runtime cannot start without it.
      mise exec pnpm@12.17.0 -- pnpm exec playwright test tests/portal-live-daemon.spec.mjs `
        --project=desktop-chromium --grep "provisions the repository resource"
      if ($LASTEXITCODE -ne 0) {
        throw "Live daemon repository provisioning failed with exit code $LASTEXITCODE. Logs: $runRoot."
      }
      if (-not (Test-Path -LiteralPath $harnessRepositoryFile)) {
        throw "The provisioning case did not write $harnessRepositoryFile."
      }
      Start-HarnessDaemon
      Start-Sleep -Seconds 6
      $env:SYMMETRY_LIVE_HARNESS_BINDING = $harnessRuntimeKey
    }

    # Both Chromium projects: the waiting-input/retry lifecycle matrix and the
    # real history cursor scenario run at desktop and narrow widths so each
    # control flow and the populated history capture exist at both; the
    # cancel/completion race, the lost-response replay and the capability
    # comparison skip narrow width inside the spec. `pnpm run test:live` stays
    # the desktop-only convenience script; this runner is the one the
    # acceptance evidence is produced with.
    mise exec pnpm@12.17.0 -- pnpm exec playwright test tests/portal-live-daemon.spec.mjs `
      --project=desktop-chromium --project=mobile-chromium
    if ($LASTEXITCODE -ne 0) {
      throw "Live daemon browser tests failed with exit code $LASTEXITCODE. Logs: $runRoot. Screenshots/JSON: $env:SYMMETRY_PLAYWRIGHT_OUTPUT. HTML report: $playwrightReport"
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

Write-Host "Live daemon browser tests passed."
Write-Host "Logs:             $runRoot"
Write-Host "Screenshots/JSON: $env:SYMMETRY_PLAYWRIGHT_OUTPUT"
Write-Host "HTML report:      $playwrightReport"
