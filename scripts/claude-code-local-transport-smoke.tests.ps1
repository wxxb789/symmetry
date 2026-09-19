[CmdletBinding()]
param()

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$helperPath = Join-Path $PSScriptRoot "claude-code-local-transport-smoke.helpers.psm1"
$smokePath = Join-Path $PSScriptRoot "claude-code-local-transport-smoke.ps1"
Import-Module $helperPath -Force

function Assert-True {
    param(
        [Parameter(Mandatory = $true)]
        [bool]$Condition,

        [Parameter(Mandatory = $true)]
        [string]$Message
    )

    if (-not $Condition) {
        throw $Message
    }
}

$pendingStdout = [System.Threading.Tasks.TaskCompletionSource[string]]::new()
$pendingStderr = [System.Threading.Tasks.TaskCompletionSource[string]]::new()
$stopwatch = [System.Diagnostics.Stopwatch]::StartNew()
$timedOut = $false
try {
    Wait-ClaudeProcessOutput `
        -StdoutTask $pendingStdout.Task `
        -StderrTask $pendingStderr.Task `
        -TimeoutMilliseconds 50 | Out-Null
}
catch {
    $timedOut = $_.Exception.Message -like "*did not close within 50 milliseconds*"
}
$stopwatch.Stop()
Assert-True $timedOut "pending Claude output did not fail with the bounded drain error"
Assert-True ($stopwatch.Elapsed.TotalSeconds -lt 2) "pending Claude output exceeded the test's bounded wait"

$successfulStdout = [System.Threading.Tasks.TaskCompletionSource[string]]::new()
$successfulStderr = [System.Threading.Tasks.TaskCompletionSource[string]]::new()
$successfulStdout.SetResult('{"result":"OK"}')
$successfulStderr.SetResult("")
$output = Wait-ClaudeProcessOutput `
    -StdoutTask $successfulStdout.Task `
    -StderrTask $successfulStderr.Task `
    -TimeoutMilliseconds 1000
Assert-True ($output.Stdout -ceq '{"result":"OK"}') "successful stdout was not preserved"
Assert-True ($output.Stderr -ceq "") "successful stderr was not preserved"

$faultedStdout = [System.Threading.Tasks.TaskCompletionSource[string]]::new()
$faultedStderr = [System.Threading.Tasks.TaskCompletionSource[string]]::new()
$faultedStdout.SetException([System.InvalidOperationException]::new("synthetic output failure"))
$faultedStderr.SetResult("")
$faulted = $false
try {
    Wait-ClaudeProcessOutput `
        -StdoutTask $faultedStdout.Task `
        -StderrTask $faultedStderr.Task `
        -TimeoutMilliseconds 1000 | Out-Null
}
catch {
    $faulted = $_.Exception.Message -like "Claude Code output stream read failed: *"
}
Assert-True $faulted "faulted Claude output did not fail closed"

$identityUnavailableRoot = $null
try {
    $identityUnavailableInfo = [System.Diagnostics.ProcessStartInfo]::new()
    $identityUnavailableInfo.FileName = (Get-Command pwsh -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
    $identityUnavailableInfo.UseShellExecute = $false
    $identityUnavailableInfo.CreateNoWindow = $true
    [void]$identityUnavailableInfo.ArgumentList.Add("-NoLogo")
    [void]$identityUnavailableInfo.ArgumentList.Add("-NoProfile")
    [void]$identityUnavailableInfo.ArgumentList.Add("-Command")
    [void]$identityUnavailableInfo.ArgumentList.Add("Start-Sleep -Seconds 30")
    $identityUnavailableRoot = [System.Diagnostics.Process]::new()
    $identityUnavailableRoot.StartInfo = $identityUnavailableInfo
    if (-not $identityUnavailableRoot.Start()) {
        throw "unable to start identity-unavailable cleanup regression process"
    }
    $identityUnavailablePID = $identityUnavailableRoot.Id
    # Fault injection: skip identity capture and exercise the direct handle path.
    $identityUnavailableDecision = Stop-ClaudeProcessWithoutIdentity -Target $identityUnavailableRoot
    Assert-True $identityUnavailableDecision.CleanupProven "identity-unavailable live root was not directly terminated"
    Assert-True $identityUnavailableRoot.HasExited "identity-unavailable live root remains running"
    try {
        $unexpectedProcess = [System.Diagnostics.Process]::GetProcessById($identityUnavailablePID)
        try {
            throw "identity-unavailable root PID $identityUnavailablePID still resolves after cleanup"
        }
        finally {
            $unexpectedProcess.Dispose()
        }
    }
    catch [ArgumentException] {
    }
}
finally {
    if ($null -ne $identityUnavailableRoot) {
        try {
            if (-not $identityUnavailableRoot.HasExited) {
                $identityUnavailableRoot.Kill($true)
                [void]$identityUnavailableRoot.WaitForExit(5000)
            }
        }
        catch {
        }
        $identityUnavailableRoot.Dispose()
    }
}

$childPIDPath = [System.IO.Path]::GetTempFileName()
$exitedRoot = $null
$descendant = $null
$descendantIdentity = $null
$rootOutputTask = $null
$rootErrorTask = $null
try {
    $rootStartInfo = [System.Diagnostics.ProcessStartInfo]::new()
    $rootStartInfo.FileName = (Get-Command pwsh -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
    $rootStartInfo.UseShellExecute = $false
    $rootStartInfo.CreateNoWindow = $true
    $rootStartInfo.RedirectStandardOutput = $true
    $rootStartInfo.RedirectStandardError = $true
    $rootStartInfo.Environment["SYMMETRY_TEST_CHILD_PID_PATH"] = $childPIDPath
    [void]$rootStartInfo.ArgumentList.Add("-NoLogo")
    [void]$rootStartInfo.ArgumentList.Add("-NoProfile")
    [void]$rootStartInfo.ArgumentList.Add("-Command")
    [void]$rootStartInfo.ArgumentList.Add(@'
$child = [System.Diagnostics.Process]::Start((Get-Command pwsh -CommandType Application | Select-Object -First 1).Source, '-NoLogo -NoProfile -Command "Start-Sleep -Seconds 30"')
[System.IO.File]::WriteAllText($env:SYMMETRY_TEST_CHILD_PID_PATH, [string]$child.Id)
exit 0
'@)
    $exitedRoot = [System.Diagnostics.Process]::new()
    $exitedRoot.StartInfo = $rootStartInfo
    if (-not $exitedRoot.Start()) {
        throw "unable to start root-exit cleanup regression process"
    }
    $exitedRootIdentity = Get-ClaudeProcessIdentity -Target $exitedRoot
    $rootOutputTask = $exitedRoot.StandardOutput.ReadToEndAsync()
    $rootErrorTask = $exitedRoot.StandardError.ReadToEndAsync()
    if (-not $exitedRoot.WaitForExit(5000)) {
        throw "root-exit cleanup regression process did not exit"
    }
    if (-not (Test-Path -LiteralPath $childPIDPath)) {
        throw "root-exit cleanup regression did not publish descendant PID"
    }
    $descendantPID = [int](Get-Content -LiteralPath $childPIDPath -Raw).Trim()
    $descendant = [System.Diagnostics.Process]::GetProcessById($descendantPID)
    $descendantIdentity = Get-ClaudeProcessIdentity -Target $descendant

    $pipeHolder = $false
    try {
        Wait-ClaudeProcessOutput `
            -StdoutTask $rootOutputTask `
            -StderrTask $rootErrorTask `
            -TimeoutMilliseconds 100 | Out-Null
    }
    catch {
        $pipeHolder = $_.Exception.Message -like "*did not close within 100 milliseconds*"
    }
    Assert-True $pipeHolder "root-exit descendant did not keep the redirected output pipe boundedly pending"
    Assert-True (-not $descendant.HasExited) "root-exit descendant-pipe holder exited before cleanup decision"

    $cleanupDecision = Stop-ClaudeProcessTree -Target $exitedRoot -ExpectedIdentity $exitedRootIdentity
    Assert-True (-not $cleanupDecision.CleanupProven) "root-exit descendant-pipe scenario was reported as clean"
    Assert-True ($cleanupDecision.Reason -like "*cleanup unproven*") "root-exit cleanup did not report cleanup unproven"
}
finally {
    if ($null -ne $descendant) {
        try {
            if (-not $descendant.HasExited -and (Test-ClaudeProcessIdentity -Target $descendant -ExpectedIdentity $descendantIdentity)) {
                $descendant.Kill($true)
                [void]$descendant.WaitForExit(5000)
            }
        }
        catch {
        }
        $descendant.Dispose()
    }
    if ($null -ne $exitedRoot) {
        try {
            if (-not $exitedRoot.HasExited) {
                $exitedRoot.Kill($true)
                [void]$exitedRoot.WaitForExit(5000)
            }
        }
        catch {
        }
        $exitedRoot.Dispose()
    }
    if (Test-Path -LiteralPath $childPIDPath) {
        Remove-Item -LiteralPath $childPIDPath -Force
    }
}

$smokeSource = Get-Content -LiteralPath $smokePath -Raw
$helperSource = Get-Content -LiteralPath $helperPath -Raw
Assert-True ($smokeSource -notmatch "GetAwaiter\(\)\.GetResult\(\)") "Claude smoke still contains an unbounded task result wait"
Assert-True ($helperSource -match "cleanup unproven") "Claude smoke helper does not expose unproven descendant cleanup"

Write-Output "Claude local transport smoke helper tests passed."
