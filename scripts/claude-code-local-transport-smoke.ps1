[CmdletBinding()]
param(
    [Parameter()]
    [string]$ClaudeExecutable = "claude",

    [Parameter()]
    [string]$ProviderEndpoint = "http://localhost:4141/v1",

    [Parameter()]
    [ValidateRange(1, 600)]
    [int]$TimeoutSeconds = 30,

    [Parameter()]
    [string]$ExpectedClaudeVersion = "2.1.259",

    [Parameter()]
    [string]$Prompt = "Reply with exactly OK."
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

Import-Module (Join-Path $PSScriptRoot "claude-code-local-transport-smoke.helpers.psm1") -Force

# This smoke owns only the Claude child process. It neither starts nor stops
# the already-running local endpoint or any Codex process.
$providerUri = $null
if (-not [Uri]::TryCreate($ProviderEndpoint, [UriKind]::Absolute, [ref]$providerUri) -or
    $providerUri.UserInfo.Length -gt 0 -or
    $providerUri.Query.Length -gt 0 -or
    $providerUri.Fragment.Length -gt 0 -or
    $providerUri.Scheme -notin @("http", "https") -or
    $providerUri.AbsolutePath.TrimEnd('/') -ne "/v1") {
    throw "ProviderEndpoint must be an absolute http(s) URL whose path is exactly /v1."
}
$providerHost = $providerUri.Host.Trim('[', ']').ToLowerInvariant()
if ($providerHost -notin @("localhost", "127.0.0.1", "::1")) {
    throw "ProviderEndpoint must use a loopback host (localhost, 127.0.0.1, or ::1)."
}

$resolvedClaude = (Get-Command -Name $ClaudeExecutable -CommandType Application -ErrorAction Stop).Source
$fileVersion = [System.Diagnostics.FileVersionInfo]::GetVersionInfo($resolvedClaude).ProductVersion
if ([string]::IsNullOrWhiteSpace($fileVersion) -or $fileVersion -notmatch ("^{0}(?:\.0)?$" -f [regex]::Escape($ExpectedClaudeVersion))) {
    throw "Claude Code executable '$resolvedClaude' reports version '$fileVersion'; expected '$ExpectedClaudeVersion'."
}

$providerEndpoint = $ProviderEndpoint.TrimEnd('/')
# Claude Code appends /v1/messages itself, so its base URL is the authority
# without the provider's /v1 path.
$endpoint = "{0}://{1}" -f $providerUri.Scheme, $providerUri.Authority
$dummyToken = "symmetry-claude-local-transport-smoke"
$targetModel = "gpt-5.6-luna"
$smokeEnvironment = [ordered]@{
    ANTHROPIC_BASE_URL = $endpoint
    ANTHROPIC_AUTH_TOKEN = $dummyToken
    ANTHROPIC_DEFAULT_SONNET_MODEL = $targetModel
    ANTHROPIC_DEFAULT_OPUS_MODEL = $targetModel
    ANTHROPIC_DEFAULT_HAIKU_MODEL = $targetModel
    ANTHROPIC_SMALL_FAST_MODEL = $targetModel
    CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC = "1"
    DISABLE_TELEMETRY = "1"
    DISABLE_ERROR_REPORTING = "1"
}

# Keep this in sync with the daemon's OS-only environment contract. These
# values are needed for a normal Windows process launch, but no provider or
# Symmetry credential is allowed to cross the process boundary.
$osEssentialEnvironmentKeys = @(
    "ComSpec", "HOME", "HOMEDRIVE", "HOMEPATH", "LANG", "LC_ALL", "LC_CTYPE",
    "PATH", "PATHEXT", "SystemRoot", "SYSTEMROOT", "TEMP", "TERM", "TMP", "TMPDIR",
    "USERPROFILE", "WINDIR"
)
$childEnvironment = [ordered]@{}
foreach ($key in $osEssentialEnvironmentKeys) {
    $value = [Environment]::GetEnvironmentVariable($key, "Process")
    if ($null -ne $value) {
        $childEnvironment[$key] = [string]$value
    }
}
foreach ($entry in $smokeEnvironment.GetEnumerator()) {
    $childEnvironment[$entry.Key] = [string]$entry.Value
}

$tempSettingsPath = $null
$process = $null
$processStarted = $false
$stdout = ""
$stderr = ""
$timedOut = $false
$outputDrained = $false
$processIdentity = $null
$scriptError = $null
$cleanupError = $null

try {
    $tempSettingsPath = Join-Path ([System.IO.Path]::GetTempPath()) (
        "symmetry-claude-transport-{0}.json" -f [System.Guid]::NewGuid().ToString("N")
    )
    $settings = [ordered]@{
        env = $smokeEnvironment
    }
    $utf8WithoutBom = [System.Text.UTF8Encoding]::new($false)
    [System.IO.File]::WriteAllText(
        $tempSettingsPath,
        ($settings | ConvertTo-Json -Depth 4),
        $utf8WithoutBom
    )

    $startInfo = [System.Diagnostics.ProcessStartInfo]::new()
    $startInfo.FileName = $ClaudeExecutable
    $startInfo.UseShellExecute = $false
    $startInfo.CreateNoWindow = $true
    $startInfo.RedirectStandardOutput = $true
    $startInfo.RedirectStandardError = $true
    $startInfo.WorkingDirectory = (Get-Location).Path

    # ProcessStartInfo starts with a copy of the parent environment. Clear it
    # before applying the explicit allowlist so credentials cannot leak into
    # the Claude child through an unreviewed parent variable.
    $startInfo.Environment.Clear()
    foreach ($entry in $childEnvironment.GetEnumerator()) {
        $startInfo.Environment[$entry.Key] = [string]$entry.Value
    }

    # Sentinel: fail closed if a future edit or runtime behavior causes the
    # actual launch environment to diverge from the explicit allowlist.
    $actualEnvironmentNames = @(
        $startInfo.Environment.Keys | ForEach-Object { [string]$_ }
    )
    $unexpectedEnvironmentNames = @(
        $actualEnvironmentNames | Where-Object { $childEnvironment.Keys -notcontains $_ }
    )
    $missingEnvironmentNames = @(
        $childEnvironment.Keys | Where-Object { $actualEnvironmentNames -notcontains [string]$_ }
    )
    if ($unexpectedEnvironmentNames.Count -gt 0 -or $missingEnvironmentNames.Count -gt 0) {
        $unexpected = ($unexpectedEnvironmentNames -join ", ")
        $missing = ($missingEnvironmentNames -join ", ")
        throw "Claude child environment assertion failed; unexpected=[$unexpected]; missing=[$missing]."
    }

    $credentialLikeEnvironmentNames = @(
        $actualEnvironmentNames | Where-Object {
            $isSmokeVariable = $smokeEnvironment.Keys -contains $_
            -not $isSmokeVariable -and
            ($_ -match "^(?i:SYMMETRY_)" -or
                $_ -match "(?i)(?:API_KEY|TOKEN|SECRET|PASSWORD|CREDENTIAL|PRIVATE_KEY)$")
        }
    )
    if ($credentialLikeEnvironmentNames.Count -gt 0) {
        throw "Claude child environment assertion rejected credential-like variables: $($credentialLikeEnvironmentNames -join ', ')."
    }

    # ArgumentList preserves the required empty value for `--tools ""`.
    $arguments = @(
        "--bare",
        "--settings",
        $tempSettingsPath,
        "--tools",
        "",
        "--permission-mode",
        "dontAsk",
        "--permission-prompts",
        "none",
        "--no-session-persistence",
        "--print",
        "--output-format",
        "json",
        "--model",
        "sonnet",
        $Prompt
    )
    foreach ($argument in $arguments) {
        [void]$startInfo.ArgumentList.Add([string]$argument)
    }

    $process = [System.Diagnostics.Process]::new()
    $process.StartInfo = $startInfo
    if (-not $process.Start()) {
        throw "Unable to start Claude Code executable '$ClaudeExecutable'."
    }
    $processStarted = $true
    $processIdentity = Get-ClaudeProcessIdentity -Target $process

    $stdoutTask = $process.StandardOutput.ReadToEndAsync()
    $stderrTask = $process.StandardError.ReadToEndAsync()
    if (-not $process.WaitForExit($TimeoutSeconds * 1000)) {
        $timedOut = $true
        if ($null -eq $processIdentity) {
            $stopResult = Stop-ClaudeProcessWithoutIdentity -Target $process
        }
        else {
            $stopResult = Stop-ClaudeProcessTree -Target $process -ExpectedIdentity $processIdentity
        }
        if (-not $stopResult.CleanupProven) {
            throw $stopResult.Reason
        }
    }

    $output = Wait-ClaudeProcessOutput -StdoutTask $stdoutTask -StderrTask $stderrTask -TimeoutMilliseconds 5000
    $stdout = $output.Stdout
    $stderr = $output.Stderr
    $outputDrained = $true

    if ($timedOut) {
        throw "Claude Code local transport smoke timed out after $TimeoutSeconds seconds."
    }
    if ($process.ExitCode -ne 0) {
        $detail = $stderr.Trim()
        if ($detail.Length -gt 1000) {
            $detail = $detail.Substring(0, 1000)
        }
        throw "Claude Code local transport smoke exited with code $($process.ExitCode). $detail"
    }
    try {
        $response = $stdout | ConvertFrom-Json -ErrorAction Stop
    }
    catch {
        throw "Claude Code local transport response was not valid JSON: $($_.Exception.Message)"
    }
    $responseText = [string]$response.result
    if ($responseText.Trim() -cne "OK") {
        throw "Claude Code local transport response result was '$responseText'; expected exactly OK."
    }

    [pscustomobject]@{
        check = "claude-code-local-transport"
        provider_endpoint = $providerEndpoint
        endpoint = $endpoint
        claude_version = $fileVersion
        model = "$($targetModel) (Claude --model sonnet)"
        exit_code = $process.ExitCode
        response_exactly_ok = $true
        note = "transport-only; native lifecycle and provider capability remain unverified"
    } | ConvertTo-Json -Compress
}
catch {
    $scriptError = $_
}
finally {
    if ($null -ne $process -and $processStarted) {
        try {
            if (-not $outputDrained -or -not $process.HasExited) {
                if ($null -eq $processIdentity) {
                    $stopResult = Stop-ClaudeProcessWithoutIdentity -Target $process
                }
                else {
                    $stopResult = Stop-ClaudeProcessTree -Target $process -ExpectedIdentity $processIdentity
                }
                if (-not $stopResult.CleanupProven) {
                    $cleanupError = $stopResult.Reason
                }
            }
        }
        catch {
            $cleanupError = $_
        }
        try {
            $process.Dispose()
        }
        catch {
            if ($null -eq $cleanupError) {
                $cleanupError = $_
            }
        }
    }
    if ($null -ne $tempSettingsPath -and (Test-Path -LiteralPath $tempSettingsPath)) {
        try {
            Remove-Item -LiteralPath $tempSettingsPath -Force -ErrorAction Stop
        }
        catch {
            if ($null -eq $cleanupError) {
                $cleanupError = $_
            }
        }
    }
}

if ($null -ne $cleanupError -and $null -ne $scriptError) {
    throw "Claude Code local transport smoke failed: $($scriptError.Exception.Message); cleanup also failed: $($cleanupError.Exception.Message)"
}
if ($null -ne $cleanupError) {
    throw "Claude Code local transport smoke cleanup failed: $($cleanupError.Exception.Message)"
}
if ($null -ne $scriptError) {
    throw $scriptError
}
