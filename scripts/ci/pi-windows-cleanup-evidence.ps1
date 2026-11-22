$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$cleanupFailed = $false

function Write-CleanupLog([string] $Message) {
  Add-Content -LiteralPath $env:PI_E2E_LOG -Value $Message
  Write-Output $Message
}

$e2eOutcome = [string]$env:PI_E2E_OUTCOME
Write-CleanupLog "cleanup verification e2e_outcome=$e2eOutcome"
if ($e2eOutcome -ne 'success') {
  if ([string]::IsNullOrWhiteSpace($e2eOutcome)) {
    Write-CleanupLog 'cleanup verification failed: Windows Pi E2E outcome was unavailable'
  }
  else {
    Write-CleanupLog "cleanup verification failed: Windows Pi E2E outcome was $e2eOutcome"
  }
  $cleanupFailed = $true
}

$markerPattern = Join-Path $env:PI_E2E_TEMP 'symmetry-pi-control-run-*\process-marker.json'
$markers = $null
try {
  $markers = @(Get-ChildItem -Path $markerPattern -File -ErrorAction Stop)
}
catch {
  Write-CleanupLog "cleanup verification could not enumerate markers: $($_.Exception.Message)"
  $cleanupFailed = $true
}

if ($null -ne $markers) {
  foreach ($marker in $markers) {
    $markerState = 'unreadable'
    $markerLength = 'unknown'
    try {
      $contents = (Get-Content -LiteralPath $marker.FullName -Raw -ErrorAction Stop).Trim()
      $markerLength = $contents.Length
    }
    catch {
      Write-CleanupLog "cleanup verification residual marker path=$($marker.FullName) chars=$markerLength state=$markerState"
      $cleanupFailed = $true
      continue
    }

    if ($contents -eq 'not_started') {
      $markerState = 'not_started'
    }
    elseif ($contents -eq 'starting') {
      $markerState = 'starting'
    }
    else {
      try {
        $record = $contents | ConvertFrom-Json -ErrorAction Stop
        $pidProperty = $record.PSObject.Properties['pid']
        $identityProperty = $record.PSObject.Properties['process_identity']
        $pidValue = if ($null -eq $pidProperty) { $null } else { $pidProperty.Value }
        $identityValue = if ($null -eq $identityProperty) { $null } else { $identityProperty.Value }
        if (($pidValue -is [int] -or $pidValue -is [long]) -and
          [int64]$pidValue -gt 0 -and [int64]$pidValue -le [int]::MaxValue -and
          $identityValue -is [string] -and
          ([string]$identityValue -match ("^windows:{0}:[0-9a-fA-F]{{16}}$" -f [int64]$pidValue))) {
          $markerState = 'json_valid'
          $validatedPid = [int64]$pidValue
          $validatedIdentity = [string]$identityValue
        }
        else {
          $markerState = 'json_invalid'
        }
      }
      catch {
        $markerState = 'json_invalid'
      }
    }
    Write-CleanupLog "cleanup verification residual marker path=$($marker.FullName) chars=$markerLength state=$markerState"
    if ($markerState -eq 'json_valid') {
      Write-CleanupLog "cleanup verification residual marker validated pid=$validatedPid process_identity=$validatedIdentity"
    }
    $cleanupFailed = $true
  }

  if ($e2eOutcome -eq 'success' -and $markers.Count -eq 0) {
    Write-CleanupLog 'cleanup verification accepted: successful Windows Pi E2E suite-level test/on_exit containment cleanup and applicable stop-receipt assertions removed every run root; no process markers remain'
  }
  elseif ($markers.Count -gt 0) {
    Write-CleanupLog "cleanup verification failed: $($markers.Count) residual process marker(s) remain"
  }
}

$markerCount = if ($null -eq $markers) { 'unknown' } else { [string]$markers.Count }
Add-Content -LiteralPath $env:PI_E2E_PROVENANCE -Value 'orphan_cleanup_verification_mode=successful_e2e_outcome_and_zero_markers;numeric_termination=disabled'
Add-Content -LiteralPath $env:PI_E2E_PROVENANCE -Value "orphan_cleanup_e2e_outcome=$e2eOutcome"
Add-Content -LiteralPath $env:PI_E2E_PROVENANCE -Value "orphan_cleanup_marker_count=$markerCount"
Add-Content -LiteralPath $env:PI_E2E_PROVENANCE -Value "orphan_cleanup_status=$(if ($cleanupFailed) { 'failed' } else { 'passed' })"
if ($cleanupFailed) { exit 1 }
if (Test-Path -LiteralPath variable:/LASTEXITCODE) { if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE } }
