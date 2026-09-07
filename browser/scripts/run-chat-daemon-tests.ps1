param(
  [string]$ControlUrl = "http://127.0.0.1:4000",
  [string]$OperatorToken = "development-operator-token",
  [string]$EnrollmentToken = "development-enrollment-token",
  [string]$ControlContainer = ""
)

$ErrorActionPreference = "Stop"
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot "../..")).Path
$fixtureSuffix = [guid]::NewGuid().ToString("N").Substring(0, 8)
$runRoot = Join-Path $env:TEMP "symmetry-goal-04-$fixtureSuffix"
$daemonBinary = Join-Path $runRoot "symmetry-daemon.exe"
$agentBinary = Join-Path $runRoot "symmetry-fake-agent.exe"
$profile = "chat-$fixtureSuffix"
$workspace = "chat-$fixtureSuffix"
$workspacePath = Join-Path $runRoot "workspace"
$statePath = Join-Path $runRoot "state"
New-Item -ItemType Directory -Force -Path $runRoot, $workspacePath, $statePath | Out-Null

Push-Location (Join-Path $repoRoot "daemon")
try {
  go build -o $daemonBinary ./cmd/symmetry-daemon
  if ($LASTEXITCODE -ne 0) { throw "Daemon build failed" }
  go build -o $agentBinary ./cmd/symmetry-fake-agent
  if ($LASTEXITCODE -ne 0) { throw "Fake-agent build failed" }
} finally { Pop-Location }

$configPath = Join-Path $runRoot "daemon.json"
@{
  control_plane_url = $ControlUrl
  allow_insecure_http = $true
  state_dir = $statePath
  machine_name = "chat-$fixtureSuffix"
  agent_profiles = @{
    $profile = @{
      command = $agentBinary
      args = @()
      input_mode = "json"
      interactive = $true
      supervisory_control = $true
      event_format = "jsonl"
      env_allowlist = @("SYMMETRY_FAKE_AGENT_MODE", "SYMMETRY_FAKE_AGENT_STEPS", "SYMMETRY_FAKE_AGENT_STEP_MS", "SYMMETRY_FAKE_AGENT_DECISION_AT")
    }
  }
  workspaces = @{
    $workspace = @{ policy = "existing_checkout"; path = $workspacePath; cleanup = "never" }
  }
  runtime = @{
    runtime_key = "chat-$fixtureSuffix"
    name = "Chat acceptance runtime"
    capacity = 1
    agent_profile = $profile
    workspace = $workspace
  }
} | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath $configPath -Encoding utf8NoBOM

$process = $null
$environmentNames = @("SYMMETRY_PORTAL_URL", "SYMMETRY_OPERATOR_TOKEN", "SYMMETRY_CHAT_LIVE", "SYMMETRY_CHAT_PROFILE", "SYMMETRY_CHAT_WORKSPACE", "SYMMETRY_CHAT_ARTIFACTS", "SYMMETRY_CHAT_STATE", "SYMMETRY_CHAT_SCREENSHOTS", "SYMMETRY_CHAT_CONTROL_CONTAINER")
$previousEnvironment = @{}
foreach ($name in $environmentNames) { $previousEnvironment[$name] = [Environment]::GetEnvironmentVariable($name, "Process") }
try {
  $process = Start-Process -FilePath $daemonBinary -ArgumentList @("-config", ('"' + $configPath + '"')) `
    -WorkingDirectory $runRoot -WindowStyle Hidden `
    -RedirectStandardOutput (Join-Path $runRoot "daemon.stdout.log") `
    -RedirectStandardError (Join-Path $runRoot "daemon.stderr.log") `
    -Environment @{
      SYMMETRY_ENROLLMENT_TOKEN = $EnrollmentToken
      SYMMETRY_FAKE_AGENT_MODE = "autonomous"
      SYMMETRY_FAKE_AGENT_STEPS = "120"
      SYMMETRY_FAKE_AGENT_STEP_MS = "250"
      SYMMETRY_FAKE_AGENT_DECISION_AT = "65"
    } -PassThru

  $env:SYMMETRY_PORTAL_URL = $ControlUrl
  $env:SYMMETRY_OPERATOR_TOKEN = $OperatorToken
  $env:SYMMETRY_CHAT_LIVE = "1"
  $env:SYMMETRY_CHAT_PROFILE = $profile
  $env:SYMMETRY_CHAT_WORKSPACE = $workspace
  $env:SYMMETRY_CHAT_ARTIFACTS = $workspacePath
  $env:SYMMETRY_CHAT_STATE = $statePath
  $env:SYMMETRY_CHAT_SCREENSHOTS = $runRoot
  $env:SYMMETRY_CHAT_CONTROL_CONTAINER = $ControlContainer
  Push-Location (Join-Path $repoRoot "browser")
  try {
    npx playwright test tests/portal-chat-live.spec.mjs --project=desktop-chromium
    if ($LASTEXITCODE -ne 0) { throw "Chat live acceptance failed. Logs and artifacts: $runRoot" }
  } finally { Pop-Location }
} finally {
  if ($process -and -not $process.HasExited) { Stop-Process -Id $process.Id -ErrorAction SilentlyContinue }
  foreach ($name in $environmentNames) { [Environment]::SetEnvironmentVariable($name, $previousEnvironment[$name], "Process") }
}

Write-Host "Chat live acceptance passed. Logs and artifacts: $runRoot"
