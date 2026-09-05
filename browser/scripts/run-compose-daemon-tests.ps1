param(
  [string]$ControlUrl = "http://127.0.0.1:4000",
  [string]$OperatorToken = "development-operator-token"
)

$ErrorActionPreference = "Stop"
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot "../..")).Path
$browserRoot = Join-Path $repoRoot "browser"

function Invoke-ComposePsql {
  param([string]$Sql)

  Push-Location $repoRoot
  try {
    $result = docker compose exec -T postgres psql -U symmetry -d symmetry_dev -At -F "|" -c $Sql
    if ($LASTEXITCODE -ne 0) { throw "PostgreSQL verification failed" }
    return ($result | Out-String).Trim()
  } finally {
    Pop-Location
  }
}

function Get-ProjectFingerprint {
  param([string]$ProjectId)

  Invoke-ComposePsql @"
SELECT md5(jsonb_build_object(
  'project', to_jsonb(p) - 'inserted_at' - 'updated_at',
  'resources', COALESCE((SELECT jsonb_agg(to_jsonb(r) - 'inserted_at' - 'updated_at' ORDER BY r.id) FROM project_resources r WHERE r.project_id = p.id), '[]'::jsonb),
  'work_items', COALESCE((SELECT jsonb_agg(to_jsonb(w) - 'inserted_at' - 'updated_at' ORDER BY w.id) FROM work_items w WHERE w.project_id = p.id), '[]'::jsonb),
  'tasks', COALESCE((SELECT jsonb_agg(to_jsonb(t) - 'inserted_at' - 'updated_at' ORDER BY t.id) FROM tasks t WHERE t.id IN (SELECT orchestration_task_id FROM work_items WHERE project_id = p.id)), '[]'::jsonb),
  'runs', COALESCE((SELECT jsonb_agg(to_jsonb(rn) - 'inserted_at' - 'updated_at' ORDER BY rn.id) FROM runs rn WHERE rn.task_id IN (SELECT orchestration_task_id FROM work_items WHERE project_id = p.id)), '[]'::jsonb),
  'events', COALESCE((SELECT jsonb_agg(to_jsonb(e) - 'inserted_at' ORDER BY e.id) FROM run_events e WHERE e.run_id IN (SELECT rn.id FROM runs rn WHERE rn.task_id IN (SELECT orchestration_task_id FROM work_items WHERE project_id = p.id))), '[]'::jsonb),
  'transitions', COALESCE((SELECT jsonb_agg(to_jsonb(tn) - 'inserted_at' ORDER BY tn.id) FROM run_transitions tn WHERE tn.run_id IN (SELECT rn.id FROM runs rn WHERE rn.task_id IN (SELECT orchestration_task_id FROM work_items WHERE project_id = p.id))), '[]'::jsonb),
  'commands', COALESCE((SELECT jsonb_agg(to_jsonb(c) - 'inserted_at' - 'updated_at' ORDER BY c.id) FROM commands c WHERE c.task_id IN (SELECT orchestration_task_id FROM work_items WHERE project_id = p.id)), '[]'::jsonb)
)::text)
FROM projects p
WHERE p.id = '$ProjectId';
"@
}

$env:SYMMETRY_PORTAL_URL = $ControlUrl
$env:SYMMETRY_OPERATOR_TOKEN = $OperatorToken
$env:SYMMETRY_COMPOSE_DAEMON = "1"

Push-Location $repoRoot
try {
  docker compose up -d --build
  if ($LASTEXITCODE -ne 0) { throw "Compose stack failed to build or start" }
} finally {
  Pop-Location
}

& curl.exe --retry 30 --retry-all-errors --retry-delay 1 --silent --show-error --fail "$ControlUrl/portal/login" | Out-Null
if ($LASTEXITCODE -ne 0) { throw "Control did not become ready before Compose acceptance" }

Push-Location $browserRoot
try {
  npm run test:compose
  if ($LASTEXITCODE -ne 0) { throw "Compose daemon browser test failed" }
} finally {
  Pop-Location
}

$record = Invoke-ComposePsql "SELECT p.id, p.key, p.name FROM projects p WHERE p.key LIKE 'DC%' ORDER BY p.inserted_at DESC LIMIT 1;"
$parts = $record.Split("|")
if ($parts.Count -ne 3) { throw "Could not identify the Compose acceptance workspace" }

$projectId = $parts[0]
$before = Get-ProjectFingerprint $projectId

Push-Location $repoRoot
try {
  docker compose restart control
  if ($LASTEXITCODE -ne 0) { throw "Control restart failed" }
} finally {
  Pop-Location
}

& curl.exe --retry 20 --retry-all-errors --retry-delay 1 --silent --show-error --fail "$ControlUrl/portal/login" | Out-Null
if ($LASTEXITCODE -ne 0) { throw "Control did not become ready after restart" }

$after = Get-ProjectFingerprint $projectId
if ($before -ne $after) { throw "Database fingerprint changed across control restart: $before -> $after" }

$env:SYMMETRY_COMPOSE_PROJECT_ID = $projectId
$env:SYMMETRY_COMPOSE_PROJECT_KEY = $parts[1]
$env:SYMMETRY_COMPOSE_PROJECT_NAME = $parts[2]

Push-Location $browserRoot
try {
  npx playwright test tests/portal-compose-restart.spec.mjs --project=desktop-chromium
  if ($LASTEXITCODE -ne 0) { throw "Compose restart browser test failed" }
} finally {
  Pop-Location
}

Write-Host "Compose daemon and restart acceptance passed. Database fingerprint: $after"
