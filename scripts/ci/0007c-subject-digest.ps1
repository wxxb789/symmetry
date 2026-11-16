# Recomputes the 0007c evidence-subject digest with the algorithm the receipt
# documents: recursively enumerate the listed roots, add the direct files in
# frontend/, browser/, contracts/ and scripts/, add the explicit files,
# deduplicate full paths, hash every file's UTF-8 bytes, then hash the UTF-8
# (no BOM) manifest of "<lowercase sha256>  <repo-relative path with / separators>\n"
# sorted by path with PowerShell's invariant-culture string sort.
#
# Scope "source" (default) is the durable source subject: no path under docs/ is
# ever in scope, so editing the receipt or any other document cannot change the
# value it records. Scope "full" additionally includes the explicit docs/ files
# the receipt names, for a full-tree comparison.
#
# Algorithm of record for
# docs/evidence/goal-0007c-safe-work-planning-2026-11-16.md. Invoke it from the
# repository root:
#   pwsh -NoProfile -File scripts/ci/0007c-subject-digest.ps1 [-Scope source|full]
# The recorded values are bound to PowerShell 7: the sort uses ICU collation
# (Sort-Object -Culture ""), so a Windows PowerShell 5.1 host produces a
# different value for the same tree.
param([ValidateSet("source", "full")][string]$Scope = "source")
$ErrorActionPreference = "Stop"

$roots = @(
  "frontend/src", "browser/tests", "browser/scripts", "contracts/v1", "contracts/ts",
  "contracts/scripts", "contracts/fixtures", "contracts/generated",
  "daemon/internal/contracts", "daemon/internal/protocol",
  "control/lib", "control/test", "scripts/ci"
)
$directDirs = @("frontend", "browser", "contracts", "scripts")
$explicit = @(
  "package.json", "pnpm-lock.yaml", "pnpm-workspace.yaml", ".github/workflows/ci.yml",
  "control/mix.exs", "control/mix.lock", "control/AGENTS.md", "frontend/AGENTS.md",
  "contracts/README.md", "browser/README.md", "AGENTS.md",
  "docs/goals/0007c-safe-work-planning.md", "docs/design/frontend.md", "docs/design/quality.md"
)

$paths = New-Object System.Collections.Generic.List[string]
foreach ($root in $roots) {
  Get-ChildItem -LiteralPath $root -Recurse -File -ErrorAction SilentlyContinue | ForEach-Object { $paths.Add($_.FullName) }
}
foreach ($dir in $directDirs) {
  Get-ChildItem -LiteralPath $dir -File -ErrorAction SilentlyContinue | ForEach-Object { $paths.Add($_.FullName) }
}
foreach ($file in $explicit) {
  if (Test-Path -LiteralPath $file) { $paths.Add((Resolve-Path -LiteralPath $file).Path) }
}

$root = (Get-Location).Path
$entries = @()
foreach ($full in ($paths | Sort-Object -Unique)) {
  $relative = $full.Substring($root.Length + 1).Replace("\", "/")
  if ($Scope -eq "source" -and ($relative -eq "docs" -or $relative.StartsWith("docs/"))) { continue }
  $entries += [pscustomobject]@{ Path = $relative; Hash = (Get-FileHash -LiteralPath $full -Algorithm SHA256).Hash.ToLower() }
}

$sorted = $entries | Sort-Object -Property Path -Culture ""
$manifest = ($sorted | ForEach-Object { "$($_.Hash)  $($_.Path)" }) -join "`n"
$bytes = [System.Text.Encoding]::UTF8.GetBytes($manifest + "`n")
$sha = [System.Security.Cryptography.SHA256]::Create()
$digest = ($sha.ComputeHash($bytes) | ForEach-Object { $_.ToString("x2") }) -join ""
"scope=$Scope"
"entries=$($sorted.Count)"
"digest=sha256:$digest"
"exit=0"
