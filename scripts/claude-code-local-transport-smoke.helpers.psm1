Set-StrictMode -Version Latest

function Get-ClaudeProcessIdentity {
    [CmdletBinding()]
    param(
        [Parameter(Mandatory = $true)]
        [System.Diagnostics.Process]$Target
    )

    try {
        [pscustomobject]@{
            Id        = $Target.Id
            StartTime = $Target.StartTime.ToUniversalTime()
        }
    }
    catch {
        throw "Claude Code process identity could not be captured: $($_.Exception.Message)"
    }
}

function Test-ClaudeProcessIdentity {
    [CmdletBinding()]
    param(
        [Parameter(Mandatory = $true)]
        [System.Diagnostics.Process]$Target,

        [Parameter(Mandatory = $true)]
        [pscustomobject]$ExpectedIdentity
    )

    if ($Target.Id -ne [int]$ExpectedIdentity.Id -or $Target.HasExited) {
        return $false
    }

    try {
        $actualStartTime = $Target.StartTime.ToUniversalTime()
        return $actualStartTime -eq [DateTime]$ExpectedIdentity.StartTime
    }
    catch {
        return $false
    }
}

function Stop-ClaudeProcessWithoutIdentity {
    [CmdletBinding()]
    param(
        [Parameter(Mandatory = $true)]
        [System.Diagnostics.Process]$Target
    )

    if ($Target.HasExited) {
        return [pscustomobject]@{
            Terminated     = $false
            CleanupProven  = $false
            Reason         = "Claude Code root process exited before identity-free cleanup could be proven; cleanup unproven."
        }
    }

    try {
        # The direct Process handle is the only safe authority when creation
        # identity was unavailable. Do not fall back to PID-based taskkill.
        $Target.Kill($true)
    }
    catch {
        return [pscustomobject]@{
            Terminated     = $false
            CleanupProven  = $false
            Reason         = "Claude Code root process could not be terminated through its direct handle; cleanup unproven: $($_.Exception.Message)"
        }
    }

    if (-not $Target.WaitForExit(5000)) {
        return [pscustomobject]@{
            Terminated     = $false
            CleanupProven  = $false
            Reason         = "Claude Code root process did not exit after direct-handle termination; cleanup unproven."
        }
    }

    [pscustomobject]@{
        Terminated     = $true
        CleanupProven  = $true
        Reason         = $null
    }
}

function Stop-ClaudeProcessTree {
    [CmdletBinding()]
    param(
        [Parameter(Mandatory = $true)]
        [System.Diagnostics.Process]$Target,

        [Parameter(Mandatory = $true)]
        [pscustomobject]$ExpectedIdentity
    )

    if ($Target.HasExited) {
        return [pscustomobject]@{
            Terminated     = $false
            CleanupProven  = $false
            Reason         = "Claude Code root process $($ExpectedIdentity.Id) exited before descendant cleanup could be proven; cleanup unproven."
        }
    }
    if (-not (Test-ClaudeProcessIdentity -Target $Target -ExpectedIdentity $ExpectedIdentity)) {
        return [pscustomobject]@{
            Terminated     = $false
            CleanupProven  = $false
            Reason         = "Claude Code root process identity changed before cleanup; cleanup unproven."
        }
    }

    try {
        $Target.Kill($true)
    }
    catch {
        if (-not $Target.HasExited) {
            # .NET process-tree termination is the primary path. Use the
            # Windows tree-aware fallback only while the original root is
            # still alive and its creation identity remains verified.
            if (-not (Test-ClaudeProcessIdentity -Target $Target -ExpectedIdentity $ExpectedIdentity)) {
                return [pscustomobject]@{
                    Terminated     = $false
                    CleanupProven  = $false
                    Reason         = "Claude Code root process identity changed before taskkill fallback; cleanup unproven."
                }
            }
            $killInfo = [System.Diagnostics.ProcessStartInfo]::new()
            $killInfo.FileName = "taskkill.exe"
            $killInfo.UseShellExecute = $false
            $killInfo.CreateNoWindow = $true
            $killInfo.RedirectStandardOutput = $true
            $killInfo.RedirectStandardError = $true
            [void]$killInfo.ArgumentList.Add("/PID")
            [void]$killInfo.ArgumentList.Add([string]$ExpectedIdentity.Id)
            [void]$killInfo.ArgumentList.Add("/T")
            [void]$killInfo.ArgumentList.Add("/F")
            $killer = [System.Diagnostics.Process]::new()
            try {
                $killer.StartInfo = $killInfo
                if (-not $killer.Start()) {
                    throw "Unable to start hidden taskkill fallback for Claude process $($ExpectedIdentity.Id)."
                }
                if (-not $killer.WaitForExit(5000)) {
                    try { $killer.Kill($true) } catch { }
                    throw "taskkill fallback did not exit for Claude process $($ExpectedIdentity.Id)."
                }
                $killError = $killer.StandardError.ReadToEnd().Trim()
                $killExitCode = $killer.ExitCode
            }
            finally {
                $killer.Dispose()
            }
            if ($killExitCode -ne 0 -and -not $Target.HasExited) {
                throw "taskkill fallback failed for Claude process $($ExpectedIdentity.Id): $killError"
            }
        }
    }

    if (-not $Target.WaitForExit(5000)) {
        throw "Claude Code process did not exit after timeout termination."
    }

    [pscustomobject]@{
        Terminated     = $true
        CleanupProven  = $true
        Reason         = $null
    }
}

function Wait-ClaudeProcessOutput {
    [CmdletBinding()]
    param(
        [Parameter(Mandatory = $true)]
        [System.Threading.Tasks.Task[string]]$StdoutTask,

        [Parameter(Mandatory = $true)]
        [System.Threading.Tasks.Task[string]]$StderrTask,

        [Parameter(Mandatory = $true)]
        [ValidateRange(1, 60000)]
        [int]$TimeoutMilliseconds
    )

    $outputTask = [System.Threading.Tasks.Task]::WhenAll(
        [System.Threading.Tasks.Task[]]@($StdoutTask, $StderrTask)
    )
    try {
        if (-not $outputTask.Wait($TimeoutMilliseconds)) {
            throw "Claude Code output streams did not close within $TimeoutMilliseconds milliseconds after process exit."
        }
    }
    catch {
        if ($_.Exception.Message -like "Claude Code output streams did not close within *") {
            throw
        }
        throw "Claude Code output stream read failed: $($_.Exception.Message)"
    }

    try {
        [pscustomobject]@{
            Stdout = [string]$StdoutTask.Result
            Stderr = [string]$StderrTask.Result
        }
    }
    catch {
        throw "Claude Code output stream read failed: $($_.Exception.Message)"
    }
}

Export-ModuleMember -Function Wait-ClaudeProcessOutput
Export-ModuleMember -Function Get-ClaudeProcessIdentity, Test-ClaudeProcessIdentity, Stop-ClaudeProcessWithoutIdentity, Stop-ClaudeProcessTree
