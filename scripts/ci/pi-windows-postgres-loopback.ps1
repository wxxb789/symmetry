$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$ready = $false

try {
  function Add-PostgresCandidate(
    [System.Collections.Generic.List[object]] $Candidates,
    [System.Collections.Generic.HashSet[string]] $Seen,
    [string] $Path,
    [string] $Source
  ) {
    if ([string]::IsNullOrWhiteSpace($Path)) { return }

    try {
      $normalized = [System.IO.Path]::GetFullPath($Path.Trim().Trim('"')).TrimEnd('\')
    }
    catch {
      return
    }

    if (-not (Test-Path -LiteralPath $normalized -PathType Container)) { return }
    if ($Seen.Add($normalized)) {
      $Candidates.Add([pscustomobject]@{ Path = $normalized; Source = $Source }) | Out-Null
    }
  }

  $candidates = [System.Collections.Generic.List[object]]::new()
  $seenCandidates = [System.Collections.Generic.HashSet[string]]::new([System.StringComparer]::OrdinalIgnoreCase)

  Add-PostgresCandidate $candidates $seenCandidates $env:PGBIN 'PGBIN'

  foreach ($toolName in @('pg_ctl', 'initdb', 'createdb')) {
    foreach ($command in @(Get-Command "$toolName.exe" -All -ErrorAction SilentlyContinue)) {
      if (-not [string]::IsNullOrWhiteSpace([string]$command.Source)) {
        Add-PostgresCandidate $candidates $seenCandidates ([System.IO.Path]::GetDirectoryName($command.Source)) "PATH:$toolName"
      }
    }
  }

  foreach ($registryPattern in @(
    'HKLM:\SOFTWARE\PostgreSQL\Installations\*'
    'HKLM:\SOFTWARE\WOW6432Node\PostgreSQL\Installations\*'
    'HKCU:\SOFTWARE\PostgreSQL\Installations\*'
    'HKCU:\SOFTWARE\WOW6432Node\PostgreSQL\Installations\*'
  )) {
    foreach ($registryKey in @(Get-Item -Path $registryPattern -ErrorAction SilentlyContinue)) {
      $properties = Get-ItemProperty -LiteralPath $registryKey.PSPath -ErrorAction SilentlyContinue
      # `Base Directory` is optional on an Installations key; reading it through PSObject keeps
      # Set-StrictMode from turning an absent value into a step failure.
      $baseDirectory = $null
      if ($null -ne $properties) {
        $baseDirectoryProperty = $properties.PSObject.Properties['Base Directory']
        $baseDirectory = if ($null -eq $baseDirectoryProperty) { $null } else { [string]$baseDirectoryProperty.Value }
      }
      if (-not [string]::IsNullOrWhiteSpace($baseDirectory)) {
        Add-PostgresCandidate $candidates $seenCandidates (Join-Path $baseDirectory 'bin') "registry:$registryPattern"
      }
    }
  }

  foreach ($programFiles in @(
    [Environment]::GetEnvironmentVariable('ProgramFiles')
    [Environment]::GetEnvironmentVariable('ProgramFiles(x86)')
    [Environment]::GetEnvironmentVariable('ProgramW6432')
  )) {
    if ([string]::IsNullOrWhiteSpace($programFiles)) { continue }

    $postgresRoot = Join-Path $programFiles 'PostgreSQL'
    foreach ($versionDirectory in @(Get-ChildItem -LiteralPath $postgresRoot -Directory -ErrorAction SilentlyContinue | Sort-Object -Property Name -Descending)) {
      Add-PostgresCandidate $candidates $seenCandidates (Join-Path $versionDirectory.FullName 'bin') "program-files:$postgresRoot"
    }
  }

  $selected = $null
  $candidateRecords = [System.Collections.Generic.List[string]]::new()
  foreach ($candidate in $candidates) {
    $candidateTools = @{
      pg_ctl = Join-Path $candidate.Path 'pg_ctl.exe'
      initdb = Join-Path $candidate.Path 'initdb.exe'
      createdb = Join-Path $candidate.Path 'createdb.exe'
    }
    $missing = @(
      foreach ($toolName in @('pg_ctl', 'initdb', 'createdb')) {
        if (-not (Test-Path -LiteralPath $candidateTools[$toolName] -PathType Leaf)) {
          "$toolName.exe"
        }
      }
    )
    $candidateRecords.Add("$($candidate.Path);source=$($candidate.Source);missing=$($missing -join ',')") | Out-Null
    if ($null -eq $selected -and $missing.Count -eq 0) {
      $selected = [pscustomobject]@{
        Bin = $candidate.Path
        Source = $candidate.Source
        PgCtl = $candidateTools.pg_ctl
        InitDb = $candidateTools.initdb
        CreateDb = $candidateTools.createdb
      }
    }
  }

  $candidateSummary = if ($candidateRecords.Count -gt 0) { $candidateRecords -join ' | ' } else { 'none' }
  Add-Content -LiteralPath $env:PI_E2E_LOG -Value "postgres_candidate_bins=$candidateSummary"
  Add-Content -LiteralPath $env:PI_E2E_PROVENANCE -Value "postgres_candidate_bins=$candidateSummary"

  if ($null -eq $selected) {
    $message = 'Windows runner lacks a complete PostgreSQL toolset (pg_ctl.exe, initdb.exe, createdb.exe) in one discovered bin directory'
    Add-Content -LiteralPath $env:PI_E2E_LOG -Value "postgres_status=unavailable; $message"
    Add-Content -LiteralPath $env:PI_E2E_PROVENANCE -Value "postgres_status=unavailable; $message"
    throw $message
  }
  else {
    $pgCtl = $selected.PgCtl
    $initDb = $selected.InitDb
    $createDb = $selected.CreateDb
    if ($env:POSTGRES_PORT -notmatch '^[0-9]+$') {
      throw "POSTGRES_PORT is not a decimal port: $env:POSTGRES_PORT"
    }
    $postgresPort = [int]$env:POSTGRES_PORT
    if ($postgresPort -lt 1024 -or $postgresPort -gt 65535) {
      throw "POSTGRES_PORT is outside the valid TCP range: $postgresPort"
    }
    $env:PI_E2E_POSTGRES_BIN = $selected.Bin
    $env:PI_E2E_POSTGRES_PGCTL = $selected.PgCtl
    "PI_E2E_POSTGRES_BIN=$($selected.Bin)" | Out-File -FilePath $env:GITHUB_ENV -Encoding utf8 -Append
    "PI_E2E_POSTGRES_PGCTL=$($selected.PgCtl)" | Out-File -FilePath $env:GITHUB_ENV -Encoding utf8 -Append
    Add-Content -LiteralPath $env:PI_E2E_LOG -Value "postgres_bin=$($selected.Bin);source=$($selected.Source)"
    Add-Content -LiteralPath $env:PI_E2E_PROVENANCE -Value "postgres_bin=$($selected.Bin);source=$($selected.Source)"
    Add-Content -LiteralPath $env:PI_E2E_PROVENANCE -Value "postgres_pg_ctl=$($selected.PgCtl)"
    Add-Content -LiteralPath $env:PI_E2E_PROVENANCE -Value "postgres_initdb=$($selected.InitDb)"
    Add-Content -LiteralPath $env:PI_E2E_PROVENANCE -Value "postgres_createdb=$($selected.CreateDb)"

    $versionOutput = & $pgCtl --version 2>&1
    $versionStatus = $LASTEXITCODE
    $version = ($versionOutput -join "`n").Trim()
    if ($versionStatus -ne 0 -or [string]::IsNullOrWhiteSpace($version)) {
      throw "pg_ctl.exe --version failed with status $versionStatus"
    }
    Add-Content -LiteralPath $env:PI_E2E_LOG -Value "postgres_version=$version"
    Add-Content -LiteralPath $env:PI_E2E_PROVENANCE -Value "postgres_version=$version"

    $env:PGPASSWORD = $env:POSTGRES_PASSWORD

    $initOutput = & $initDb -D $env:PI_E2E_POSTGRES_DATA -U $env:POSTGRES_USER --auth=trust --no-locale --encoding=UTF8 2>&1
    $initStatus = $LASTEXITCODE
    $initOutput | Tee-Object -FilePath $env:PI_E2E_LOG -Append | Out-Host
    if ($initStatus -ne 0) {
      throw "initdb.exe failed with status $initStatus"
    }

    # The postmaster inherits pg_ctl's stdout/stderr. Capturing them
    # with & ... 2>&1 keeps the pipe open and never returns, so wait
    # on pg_ctl alone and read its redirected output afterwards.
    $startOut = Join-Path $env:RUNNER_TEMP 'pg_ctl-start.out'
    $startErr = Join-Path $env:RUNNER_TEMP 'pg_ctl-start.err'
    $startProcess = Start-Process -FilePath $pgCtl -NoNewWindow -PassThru `
      -ArgumentList @('-D', "`"$env:PI_E2E_POSTGRES_DATA`"", '-o', "`"-h $env:POSTGRES_HOST -p $env:POSTGRES_PORT`"", '-l', "`"$env:PI_E2E_POSTGRES_LOG`"", '-w', '-t', '30', 'start') `
      -RedirectStandardOutput $startOut -RedirectStandardError $startErr
    if (-not $startProcess.WaitForExit(60000)) {
      throw 'pg_ctl.exe start did not exit within 60 seconds'
    }
    $startStatus = $startProcess.ExitCode
    Get-Content -LiteralPath $startOut, $startErr -ErrorAction SilentlyContinue | Tee-Object -FilePath $env:PI_E2E_LOG -Append | Out-Host
    if ($startStatus -ne 0) {
      throw "pg_ctl.exe start failed with status $startStatus"
    }

    $createOutput = & $createDb -h $env:POSTGRES_HOST -p $env:POSTGRES_PORT -U $env:POSTGRES_USER $env:POSTGRES_DB 2>&1
    $createStatus = $LASTEXITCODE
    $createOutput | Tee-Object -FilePath $env:PI_E2E_LOG -Append | Out-Host
    if ($createStatus -ne 0) {
      throw "createdb.exe failed with status $createStatus"
    }

    Add-Content -LiteralPath $env:PI_E2E_LOG -Value "postgres_status=ready on $env:POSTGRES_HOST`:$env:POSTGRES_PORT"
    Add-Content -LiteralPath $env:PI_E2E_PROVENANCE -Value "postgres_status=ready on $env:POSTGRES_HOST`:$env:POSTGRES_PORT"
    $ready = $true
  }
}
catch {
  $message = "Windows PostgreSQL preflight failed: $($_.Exception.Message)"
  Add-Content -LiteralPath $env:PI_E2E_LOG -Value $message
  Add-Content -LiteralPath $env:PI_E2E_PROVENANCE -Value "postgres_status=unavailable; $message"
  Write-Output "::error::$message; Pi Control E2E acceptance gate failed"
  throw
}

"ready=$($ready.ToString().ToLowerInvariant())" | Out-File -FilePath $env:GITHUB_OUTPUT -Encoding utf8 -Append
if (Test-Path -LiteralPath variable:/LASTEXITCODE) { if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE } }
