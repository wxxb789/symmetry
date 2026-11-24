#!/usr/bin/env python3

import dataclasses
import hashlib
import os
import pathlib
import posixpath
import re
import shutil
import subprocess
import tempfile
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[2]
WORKFLOW = ROOT / ".github" / "workflows" / "ci.yml"
ACCEPTANCE = ROOT / ".github" / "workflows" / "linux-native-v2-acceptance.yml"
ACTION = ROOT / ".github" / "actions" / "contracts-build" / "action.yml"

JOB_HEADER = re.compile(r"^  ([a-z][a-z0-9-]*):$")
KEY = re.compile(r"^(\s*)([A-Za-z_][A-Za-z0-9_-]*):(?: (.*))?$")
GATE_ENTRY = re.compile(r'"([^"=]+)=\$(?:env:)?([A-Za-z_][A-Za-z0-9_]*)"')
OUTCOME_BINDING = re.compile(r"\$\{\{ steps\.[A-Za-z_][A-Za-z0-9_]*\.outcome \}\}")
ASSERTION_JOB = re.compile(r"assert-folded-gates\.py --job ([a-z][a-z0-9-]*)")
NATIVE_CALL = re.compile(r"(^|[^&\w])&\s*\$[A-Za-z_]|\bStart-Process\b")
STATUS_CHECK = re.compile(r"\$LASTEXITCODE\b|\.ExitCode\b")
INLINE_RUN_KEY = re.compile(r"^(?: {8}| {6}- )run: \|$")

WORKFLOW_INDENT = (6, 8)
ACTION_INDENT = (4, 6)
CLASSIFIER_HEREDOC = "python3 - <<'PY'"
MAX_INLINE_BODY_LINES = 80
ACTION_CALL = "uses: ./.github/actions/contracts-build"
ACTION_JOB = "contracts-build"
ROOT_WORK_DIRECTORIES = (".", "${{ github.workspace }}")
ABSORBED_BY_THE_ACTION = (
    "Check lockfile registry provenance",
    "pnpm install --frozen-lockfile",
    "pnpm contracts:check",
    "pnpm --filter @symmetry/frontend",
    "name: portal-assets",
    "name: control-release-linux",
)
PROPAGATION_GUARD = (
    "if (Test-Path -LiteralPath variable:/LASTEXITCODE) { if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE } }"
)
POWERSHELL_PROLOGUE = ("$ErrorActionPreference = 'Stop'", "Set-StrictMode -Version Latest")
BASH_PROLOGUE = ("#!/usr/bin/env bash", "set -Eeuo pipefail")

POSTGRES_PROGRAM = "scripts/ci/pi-windows-postgres-loopback.ps1"
REGISTRY_SHIM = (
    "function Get-Item { param([string] $Path, $ErrorAction) "
    "[pscustomobject]@{ PSPath = 'HKCU:\\SOFTWARE\\PostgreSQL\\Installations\\shim' } }; "
    "function Get-ItemProperty { param([string] $LiteralPath, $ErrorAction) "
    "[pscustomobject]@{ Version = '18' } }; "
)
PSOBJECT_REGISTRY_READ = (
    "      # `Base Directory` is optional on an Installations key; reading it through PSObject keeps\n"
    "      # Set-StrictMode from turning an absent value into a step failure.\n"
    "      $baseDirectory = $null\n"
    "      if ($null -ne $properties) {\n"
    "        $baseDirectoryProperty = $properties.PSObject.Properties['Base Directory']\n"
    "        $baseDirectory = if ($null -eq $baseDirectoryProperty) { $null }"
    " else { [string]$baseDirectoryProperty.Value }\n"
    "      }\n"
)
DIRECT_REGISTRY_READ = "      $baseDirectory = $properties.'Base Directory'\n"
MISSING_VALUE_ERROR = "cannot be found on this object"

NATIVE_SITE_COUNTS = {
    "pi-windows-postgres-loopback.ps1": 4,
    "pi-windows-cleanup-evidence.ps1": 0,
}

# After the extraction the moved bodies live only inside these programs, so their bytes are pinned whole.
PROGRAM_SHA256 = {
    "compose-stack-smoke.sh": "1cdf4203ddf26726b4e1ea75bd9298f31d789be34eb7162ec919dec95f9e653d",
    "verify-linux-pi-pidfd-cleanup.sh": "10fc56095ad58247550036692a6d16ca0e6f5433ec536f5b1e4cd0ed0659cc05",
    "daemon-recovery-after-control-restart.sh": "b2a2d850203600fa3c0b549f72ba956392bdf4ae7d8de31fc38244801de1e4e8",
    "pi-windows-postgres-loopback.ps1": "1ce5cd4f57f093b1d6a9a244b343cece9a4346ccc3b9854aa2ffa1208debeed7",
    "pi-windows-cleanup-evidence.ps1": "d95b53e145989aad5d675c8236e1a9205d08f7a546c2c74654d81e2ec4a78b1d",
}


@dataclasses.dataclass(frozen=True)
class ExtractedStep:
    job: str
    program: str
    keys: tuple[tuple[str, str], ...]
    env: tuple[tuple[str, str], ...] = ()


EXTRACTED_STEPS = (
    ExtractedStep(
        job="compose",
        program="scripts/ci/compose-stack-smoke.sh",
        keys=(
            ("name", "Smoke-test local HTTP and production HTTPS Compose stacks"),
            ("shell", "bash"),
            ("working-directory", "${{ github.workspace }}"),
            ("run", "bash scripts/ci/compose-stack-smoke.sh"),
        ),
    ),
    ExtractedStep(
        job="control",
        program="scripts/ci/verify-linux-pi-pidfd-cleanup.sh",
        keys=(
            ("name", "Verify Linux Pi cleanup pidfd group signaling"),
            ("id", "linux_pi_pidfd_cleanup"),
            ("if", "always()"),
            ("shell", "bash"),
            ("working-directory", "control"),
            ("run", "bash ../scripts/ci/verify-linux-pi-pidfd-cleanup.sh"),
        ),
    ),
    ExtractedStep(
        job="pi-control-e2e-windows",
        program="scripts/ci/pi-windows-postgres-loopback.ps1",
        keys=(
            ("name", "Start isolated PostgreSQL loopback"),
            ("id", "windows_postgres"),
            ("shell", "pwsh"),
            ("working-directory", "control"),
            ("run", "pwsh -NoProfile -File ../scripts/ci/pi-windows-postgres-loopback.ps1"),
        ),
    ),
    ExtractedStep(
        job="pi-control-e2e-windows",
        program="scripts/ci/pi-windows-cleanup-evidence.ps1",
        keys=(
            ("name", "Verify Windows Pi Control cleanup evidence"),
            ("if", "always()"),
            ("shell", "pwsh"),
            ("working-directory", "control"),
            ("env", ""),
            ("run", "pwsh -NoProfile -File ../scripts/ci/pi-windows-cleanup-evidence.ps1"),
        ),
        env=(("PI_E2E_OUTCOME", "${{ steps.windows_pi_e2e.outcome }}"),),
    ),
    ExtractedStep(
        job="integration",
        program="scripts/ci/daemon-recovery-after-control-restart.sh",
        keys=(
            ("name", "Verify daemon recovers a live run after control restart"),
            ("working-directory", "daemon"),
            ("shell", "bash"),
            ("env", ""),
            ("run", "bash ../scripts/ci/daemon-recovery-after-control-restart.sh"),
        ),
        env=(
            ("SYMMETRY_E2E", '"1"'),
            ("SYMMETRY_E2E_URL", "http://127.0.0.1:4000"),
            ("SYMMETRY_E2E_AGENT", "/tmp/symmetry-fake-agent"),
            ("SYMMETRY_E2E_RESTART_MARKER", "/tmp/symmetry-control-restart.marker"),
        ),
    ),
)

ACTION_INPUTS = ("build-control-release",)
ACTION_STEPS = (
    "name: Check lockfile registry provenance",
    "run: pnpm install --frozen-lockfile",
    "run: pnpm contracts:check",
    "run: pnpm --filter @symmetry/frontend typecheck",
    "run: pnpm --filter @symmetry/frontend lint",
    "run: pnpm --filter @symmetry/frontend test",
    "run: pnpm --filter @symmetry/frontend build",
    "uses: actions/upload-artifact@v7",
    "uses: erlef/setup-beam@v1",
    "name: Build production Control release",
    "uses: actions/upload-artifact@v7",
)
ACTION_CONDITIONS = (
    ("uses: erlef/setup-beam@v1", "inputs.build-control-release == 'true'"),
    ("name: Build production Control release", "inputs.build-control-release == 'true'"),
    ("uses: actions/upload-artifact@v7", "inputs.build-control-release == 'true'"),
)
ACTION_ARTIFACTS = (
    ("portal-assets", "control/priv/static/portal", None),
    ("control-release-linux", "${{ runner.temp }}/control-release-linux.tar.gz", "error"),
)
CONTRACTS_HEADERS = {
    "ci.yml": ("needs: changes", "if: needs.changes.outputs.run-heavy == 'true'", "runs-on: ubuntu-latest"),
    "linux-native-v2-acceptance.yml": ("runs-on: ubuntu-latest",),
}
CONTRACTS_INPUTS = {"ci.yml": "true", "linux-native-v2-acceptance.yml": "false"}
# The advisory audit stays with the caller that wants it: `always()` is what reaches it after a failed setup.
CONTRACTS_ADVISORY_STEP = {
    "name": "Measure production dependency advisories (advisory)",
    "if": "always()",
    "continue-on-error": "true",
    "run": "|",
}

ADVISORY_STEPS = frozenset(
    {
        ("control", "Measure Hex advisories (advisory)"),
        ("daemon", "Measure golangci-lint findings (GOOS=linux, advisory)"),
        ("daemon", "Measure govulncheck findings (advisory)"),
        ("legacy-route-audit", "Measure ruff findings in scripts/ci (advisory)"),
        ("legacy-route-audit", "Measure shellcheck findings in scripts/ci (advisory)"),
        ("legacy-route-audit", "Measure actionlint findings for the workflows (advisory)"),
        ("daemon-windows", "Measure golangci-lint findings (GOOS=windows, advisory)"),
        ("contracts", "Measure production dependency advisories (advisory)"),
    }
)
ADVISORY_JOBS: frozenset[str] = frozenset()
CREDO_GATE_STEP = "Enforce the committed Credo strict baseline"
CREDO_GATE_INVOCATION = 'python3 ../scripts/ci/assert-credo-strict-baseline.py --log "$credo_log"'
CREDO_STRICT_INVOCATION = 'mix credo --strict 2>&1 | tee "$credo_log"'
DURATION_GATE_STEP = "Summarize Go test durations"
DURATION_GATE_CONDITION = "always() && steps.daemon_race_tests.outcome != 'skipped'"
DURATION_GATE_INVOCATION = 'python3 ../scripts/ci/go-test-durations.py "$RUNNER_TEMP/daemon-go-race.json"'
JSCPD_GATE_STEP = "Refuse code clones this change adds to the files it touches"
JSCPD_SUITE_INVOCATION = "python3 scripts/ci/test-jscpd-changed.py"
JSCPD_GATE_INVOCATION = 'python3 scripts/ci/jscpd-changed.py --base "$JSCPD_BASE" --head HEAD'


def require(condition: bool, message: str) -> None:
    if not condition:
        raise AssertionError(message)


def job_blocks(workflow_text: str) -> dict[str, list[str]]:
    lines = workflow_text.split("\n")
    starts = [(match.group(1), index) for index, line in enumerate(lines) if (match := JOB_HEADER.match(line))]
    blocks = {}
    for position, (name, start) in enumerate(starts):
        end = starts[position + 1][1] if position + 1 < len(starts) else len(lines)
        blocks[name] = lines[start:end]
    return blocks


def split_steps(lines: list[str], dash_indent: int) -> list[list[str]]:
    prefix = " " * dash_indent + "- "
    steps: list[list[str]] = []
    current: list[str] | None = None
    for line in lines:
        if line.startswith(prefix):
            if current is not None:
                steps.append(current)
            current = [line]
            continue
        if current is not None:
            current.append(line)
    if current is not None:
        steps.append(current)
    return steps


def normalized_step(step_lines: list[str], dash_indent: int, key_indent: int) -> list[str]:
    prefix = " " * dash_indent + "- "
    head = step_lines[0]
    require(head.startswith(prefix), f"a step does not start with its dash: {head!r}")
    return [" " * key_indent + head[len(prefix) :], *step_lines[1:]]


def scalars_at(lines: list[str], key_indent: int) -> dict[str, str]:
    scalars = {}
    for line in lines:
        match = KEY.match(line)
        if match is not None and len(match.group(1)) == key_indent:
            scalars[match.group(2)] = match.group(3) or ""
    return scalars


def nested_at(lines: list[str], key: str, key_indent: int) -> dict[str, str]:
    nested: dict[str, str] = {}
    in_block = False
    for line in lines:
        match = KEY.match(line)
        if match is not None and len(match.group(1)) == key_indent:
            in_block = match.group(2) == key and match.group(3) in (None, "")
            continue
        if in_block and match is not None and len(match.group(1)) == key_indent + 2:
            nested[match.group(2)] = match.group(3) or ""
    return nested


def block_body(lines: list[str], key_index: int, key_indent: int) -> list[str]:
    body: list[str] = []
    indent = None
    for line in lines[key_index + 1 :]:
        if line.strip() and len(line) - len(line.lstrip(" ")) <= key_indent:
            break
        if line.strip() and indent is None:
            indent = len(line) - len(line.lstrip(" "))
        body.append(line[indent:] if line.strip() and indent is not None else "")
    while body and body[-1] == "":
        body.pop()
    return body


def run_body_at(lines: list[str], key_indent: int) -> list[str] | None:
    for index, line in enumerate(lines):
        if line == " " * key_indent + "run: |":
            return block_body(lines, index, key_indent)
    return None


def step_name(step_lines: list[str], indent: tuple[int, int]) -> str:
    scalars = scalars_at(normalized_step(step_lines, *indent), indent[1])
    name = scalars.get("name")
    if name is None:
        raise AssertionError(f"an unnamed step cannot be pinned: {step_lines[0]!r}")
    return name


def inline_run_bodies(workflow_text: str) -> list[tuple[int, list[str]]]:
    lines = workflow_text.split("\n")
    bodies = []
    for index, line in enumerate(lines):
        if INLINE_RUN_KEY.match(line):
            bodies.append((index + 1, block_body(lines, index, 8)))
    return bodies


def program_of_invocation(run_line: str, program: str) -> str:
    tokens = run_line.split()
    require(bool(tokens), f"{program}: the run line is empty")
    if tokens[0] == "bash":
        require(len(tokens) == 2, f"{program}: the bash invocation changed shape: {run_line!r}")
        return tokens[1]
    require(
        tokens[:3] == ["pwsh", "-NoProfile", "-File"] and len(tokens) == 4,
        f"{program}: the pwsh invocation changed shape: {run_line!r}",
    )
    return tokens[3]


def resolved_invocation_target(work_directory: str, program: str) -> str:
    base = "" if work_directory in ROOT_WORK_DIRECTORIES else work_directory
    return posixpath.normpath(posixpath.join(base, program))


def find_step(job_lines: list[str], name: str) -> list[str]:
    for step_lines in split_steps(job_lines, WORKFLOW_INDENT[0]):
        normalized = normalized_step(step_lines, *WORKFLOW_INDENT)
        if scalars_at(normalized, WORKFLOW_INDENT[1]).get("name") == name:
            return normalized
    raise AssertionError(f"the job no longer carries the step {name!r}")


def assertion_gates(job_lines: list[str], job: str) -> set[str]:
    gates: set[str] = set()
    for step_lines in split_steps(job_lines, WORKFLOW_INDENT[0]):
        normalized = normalized_step(step_lines, *WORKFLOW_INDENT)
        body = run_body_at(normalized, WORKFLOW_INDENT[1])
        if body is None:
            continue
        text = "\n".join(line for line in body if not line.lstrip().startswith("#"))
        match = ASSERTION_JOB.search(text)
        if match is None:
            continue
        require(match.group(1) == job, f"job {job} runs an assertion that names another job: {match.group(1)}")
        env = nested_at(normalized, "env", WORKFLOW_INDENT[1])
        for name, variable in GATE_ENTRY.findall(text):
            value = env.get(variable)
            require(
                value is not None,
                f"job {job}: the gate {name!r} reads {variable}, which the step's env does not define",
            )
            require(
                OUTCOME_BINDING.fullmatch(value) is not None,
                f"job {job}: the gate {name!r} reads {variable}={value!r}, which is not a steps.<id>.outcome",
            )
            gates.add(name)
    return gates


def advisory_value(line: str, prefix: str, scope: str) -> bool:
    if not line.startswith(prefix + "continue-on-error:"):
        return False
    require(line == prefix + "continue-on-error: true", f"{scope}: refusing to guess at the advisory value {line!r}")
    return True


def workflow_advisories(workflow_text: str) -> tuple[set[tuple[str, str]], set[str]]:
    steps: set[tuple[str, str]] = set()
    jobs: set[str] = set()
    prefix = " " * WORKFLOW_INDENT[1]
    for job, block in job_blocks(workflow_text).items():
        if any(advisory_value(line, "    ", job) for line in block):
            jobs.add(job)
        for step_lines in split_steps(block, WORKFLOW_INDENT[0]):
            normalized = normalized_step(step_lines, *WORKFLOW_INDENT)
            if any(advisory_value(line, prefix, job) for line in normalized):
                steps.add((job, step_name(step_lines, WORKFLOW_INDENT)))
    return steps, jobs


def action_advisories(action_text: str) -> set[tuple[str, str]]:
    steps = set()
    prefix = " " * ACTION_INDENT[1]
    for step_lines in split_steps(action_text.split("\n"), ACTION_INDENT[0]):
        normalized = normalized_step(step_lines, *ACTION_INDENT)
        if any(advisory_value(line, prefix, ACTION_JOB) for line in normalized):
            steps.add((ACTION_JOB, step_name(step_lines, ACTION_INDENT)))
    return steps


def action_steps(action_text: str) -> list[list[str]]:
    return [
        normalized_step(step_lines, *ACTION_INDENT)
        for step_lines in split_steps(action_text.split("\n"), ACTION_INDENT[0])
    ]


def action_step_signature(normalized: list[str]) -> str:
    scalars = scalars_at(normalized, ACTION_INDENT[1])
    if "name" in scalars:
        return f"name: {scalars['name']}"
    if "uses" in scalars:
        return f"uses: {scalars['uses']}"
    body = run_body_at(normalized, ACTION_INDENT[1])
    return f"run: {scalars.get('run') or (body[0] if body else '')}"


def action_step_signatures(action_text: str) -> list[str]:
    return [action_step_signature(normalized) for normalized in action_steps(action_text)]


def action_step_conditions(action_text: str) -> list[tuple[str, str]]:
    conditions = []
    for normalized in action_steps(action_text):
        condition = scalars_at(normalized, ACTION_INDENT[1]).get("if")
        if condition is not None:
            conditions.append((action_step_signature(normalized), condition))
    return conditions


def action_artifacts(action_text: str) -> list[tuple[str, str, str | None]]:
    artifacts = []
    for step_lines in split_steps(action_text.split("\n"), ACTION_INDENT[0]):
        normalized = normalized_step(step_lines, *ACTION_INDENT)
        scalars = scalars_at(normalized, ACTION_INDENT[1])
        if scalars.get("uses") != "actions/upload-artifact@v7":
            continue
        with_values = nested_at(normalized, "with", ACTION_INDENT[1])
        name, path = with_values.get("name"), with_values.get("path")
        require(name is not None and path is not None, f"an upload-artifact step lacks a name or path: {with_values!r}")
        artifacts.append((name, path, with_values.get("if-no-files-found")))
    return artifacts


def action_input_block(action_text: str, name: str) -> list[str]:
    lines = action_text.split("\n")
    for index, line in enumerate(lines):
        if line != f"  {name}:":
            continue
        block: list[str] = []
        for candidate in lines[index + 1 :]:
            if candidate.strip() and len(candidate) - len(candidate.lstrip(" ")) <= 2:
                break
            block.append(candidate)
        return block
    return []


def check_extracted_steps(workflow_text: str) -> None:
    jobs = job_blocks(workflow_text)
    for step in EXTRACTED_STEPS:
        require(step.job in jobs, f"the workflow is missing the job {step.job}")
        expected = dict(step.keys)
        normalized = find_step(jobs[step.job], expected["name"])
        scalars = scalars_at(normalized, WORKFLOW_INDENT[1])
        require(scalars == expected, f"{step.program}: the step keys changed: {scalars!r} != {expected!r}")
        if step.env:
            require(
                tuple(nested_at(normalized, "env", WORKFLOW_INDENT[1]).items()) == step.env,
                f"{step.program}: the step env changed",
            )
        work_directory = scalars["working-directory"]
        target = resolved_invocation_target(work_directory, program_of_invocation(scalars["run"], step.program))
        require(target == step.program, f"{step.program} resolves to {target} from {work_directory!r}")


def check_programs(root: pathlib.Path) -> None:
    for step in EXTRACTED_STEPS:
        program = root / step.program
        require(program.is_file(), f"{step.program} does not exist")
        lines = program.read_text(encoding="utf-8").split("\n")
        prologue = POWERSHELL_PROLOGUE if step.program.endswith(".ps1") else BASH_PROLOGUE
        require(tuple(lines[: len(prologue)]) == prologue, f"{step.program}: the prologue changed")
        require(
            len([line for line in lines if line.strip()]) > len(prologue),
            f"{step.program} is effectively empty",
        )
        digest = hashlib.sha256("\n".join(lines).encode("utf-8")).hexdigest()
        expected_digest = PROGRAM_SHA256[pathlib.Path(step.program).name]
        require(digest == expected_digest, f"{step.program}: the body changed (sha256 {digest})")
        if not step.program.endswith(".ps1"):
            continue
        stripped = [line for line in lines if line.strip()]
        require(stripped[-1] == PROPAGATION_GUARD, f"{step.program}: the propagation guard is not the last line")
        sites = 0
        pending: int | None = None
        for number, line in enumerate(lines, start=1):
            if NATIVE_CALL.search(line):
                require(pending is None, f"{step.program}:{number}: the previous native call is unchecked")
                pending = number
                sites += 1
                continue
            if pending is not None and STATUS_CHECK.search(line):
                pending = None
        require(pending is None, f"{step.program}:{pending}: the last native call is unchecked")
        expected_sites = NATIVE_SITE_COUNTS[pathlib.Path(step.program).name]
        require(sites == expected_sites, f"{step.program}: expected {expected_sites} native call sites, found {sites}")


def check_registry_key_without_base_directory(program: pathlib.Path) -> None:
    pwsh = shutil.which("pwsh")
    if pwsh is None:
        raise unittest.SkipTest("pwsh is not installed on this host; the reproduction is CI-side")
    with tempfile.TemporaryDirectory() as temporary:
        root = pathlib.Path(temporary)
        log, provenance = root / "log", root / "provenance"
        output, environment = root / "github-output", root / "github-env"
        for path in (log, provenance, output, environment):
            path.write_text("", encoding="utf-8")
        env = dict(os.environ)
        env.update(
            {
                "PI_E2E_LOG": str(log),
                "PI_E2E_PROVENANCE": str(provenance),
                "GITHUB_OUTPUT": str(output),
                "GITHUB_ENV": str(environment),
            }
        )
        for variable in ("PGBIN", "POSTGRES_PORT"):
            env.pop(variable, None)
        subprocess.run(
            [pwsh, "-NoProfile", "-NonInteractive", "-Command", REGISTRY_SHIM + f"& '{program}'"],
            capture_output=True,
            text=True,
            timeout=300,
            check=False,
            env=env,
        )
        provenance_text = provenance.read_text(encoding="utf-8")
    require(
        "postgres_candidate_bins=" in provenance_text,
        f"the key without Base Directory aborted the discovery run: {provenance_text!r}",
    )
    require(
        MISSING_VALUE_ERROR not in provenance_text,
        f"the registry read failed on a missing value: {provenance_text!r}",
    )


def check_inline_bodies(workflow_text: str) -> None:
    bodies = inline_run_bodies(workflow_text)
    require(len(bodies) > 40, f"the workflow only carries {len(bodies)} inline program blocks")
    for number, body in bodies:
        require(len(body) <= MAX_INLINE_BODY_LINES, f"ci.yml:{number}: the inline body grew to {len(body)} lines")
    require(CLASSIFIER_HEREDOC not in workflow_text, "the classifier heredoc returned")


def check_action(action_text: str, workflows: dict[str, str]) -> None:
    require("using: composite" in action_text, "the contracts action is not a composite action")
    for name in ACTION_INPUTS:
        block = action_input_block(action_text, name)
        require(block != [], f"the contracts action lost its {name} input")
        require("    required: true" in block, f"the {name} input must stay required")
    require(
        tuple(action_step_signatures(action_text)) == ACTION_STEPS,
        f"the contracts action step list changed: {action_step_signatures(action_text)!r}",
    )
    require(
        tuple(action_step_conditions(action_text)) == ACTION_CONDITIONS,
        f"the contracts action step conditions changed: {action_step_conditions(action_text)!r}",
    )
    require(
        tuple(action_artifacts(action_text)) == ACTION_ARTIFACTS,
        f"the contracts action artifacts changed: {action_artifacts(action_text)!r}",
    )
    for label, workflow_text in workflows.items():
        blocks = job_blocks(workflow_text)
        require("contracts" in blocks, f"{label} lost its contracts job")
        body = "\n".join(blocks["contracts"])
        build_release = CONTRACTS_INPUTS[label]
        require(ACTION_CALL in body, f"{label}: the contracts job does not call the action")
        require(
            f'          build-control-release: "{build_release}"' in body, f"{label}: build-control-release changed"
        )
        for key in CONTRACTS_HEADERS[label]:
            require(f"    {key}\n" in body, f"{label}: the contracts job changed its {key!r} header")
        for absorbed in ABSORBED_BY_THE_ACTION:
            require(absorbed not in body, f"{label}: the contracts job still spells {absorbed!r}")
    acceptance_job = job_blocks(workflows["linux-native-v2-acceptance.yml"])["linux-native-v2-acceptance"]
    require(
        "    needs: contracts\n" in "\n".join(acceptance_job) + "\n",
        "the acceptance job lost its contracts prerequisite",
    )


def check_caller_advisory_step(workflow_text: str) -> None:
    normalized = find_step(job_blocks(workflow_text)["contracts"], CONTRACTS_ADVISORY_STEP["name"])
    scalars = scalars_at(normalized, WORKFLOW_INDENT[1])
    require(scalars == CONTRACTS_ADVISORY_STEP, f"the contracts advisory step changed: {scalars!r}")


def check_blocking_gate_step(
    workflow_text: str, job: str, step_name: str, invocation: str, *, condition: str | None = None
) -> list[str]:
    normalized = find_step(job_blocks(workflow_text)[job], step_name)
    scalars = scalars_at(normalized, WORKFLOW_INDENT[1])
    require(
        "continue-on-error" not in scalars,
        f"the {step_name!r} step must not carry a step-level continue-on-error: its verdict has to fail the job",
    )
    if condition is not None:
        require(
            scalars.get("if") == condition,
            f"the {step_name!r} step must keep its reachability `{condition}`; found {scalars.get('if')!r}",
        )
    body = run_body_at(normalized, WORKFLOW_INDENT[1])
    require(body is not None, f"the {step_name!r} step must keep a `run: |` body")
    commands = [line for line in body if line.strip() and not line.lstrip().startswith("#")]
    require(
        bool(commands) and commands[-1] == invocation,
        f"the {step_name!r} step's body must end in {invocation!r}; "
        f"found {commands[-1] if commands else '<an empty body>'!r}",
    )
    return commands


def check_credo_gate_step(workflow_text: str) -> None:
    commands = check_blocking_gate_step(workflow_text, "control", CREDO_GATE_STEP, CREDO_GATE_INVOCATION)
    require(
        CREDO_STRICT_INVOCATION in commands,
        f"the Credo gate step must run {CREDO_STRICT_INVOCATION!r}; found {commands!r}",
    )
    require(
        not any(re.search(r"\bexit\s+0\b", line) for line in commands),
        "the Credo gate step must not keep an `exit 0` skip path: every run has to reach the gate program",
    )


def check_duration_gate_step(workflow_text: str) -> None:
    check_blocking_gate_step(
        workflow_text, "daemon", DURATION_GATE_STEP, DURATION_GATE_INVOCATION, condition=DURATION_GATE_CONDITION
    )


def check_jscpd_gate_step(workflow_text: str) -> None:
    commands = check_blocking_gate_step(workflow_text, "contracts", JSCPD_GATE_STEP, JSCPD_GATE_INVOCATION)
    require(
        JSCPD_SUITE_INVOCATION in commands,
        f"the jscpd gate step must also run {JSCPD_SUITE_INVOCATION!r}; found {commands!r}",
    )


def check_folded_gate_completeness(workflow_text: str, action_text: str) -> None:
    steps, jobs = workflow_advisories(workflow_text)
    steps |= action_advisories(action_text)
    require(jobs == ADVISORY_JOBS, f"the job-level advisory jobs changed: {sorted(jobs)!r}")
    require(ADVISORY_STEPS - steps == set(), f"stale advisory allowlist entries: {sorted(ADVISORY_STEPS - steps)!r}")
    for job, name in sorted(steps):
        if (job, name) in ADVISORY_STEPS:
            continue
        require(
            job != ACTION_JOB,
            f"the action's advisory step {name!r} must be added to the explicit allowlist",
        )
        require(
            name in assertion_gates(job_blocks(workflow_text)[job], job),
            f"{job}: the advisory step {name!r} is neither asserted by assert-folded-gates.py nor allowlisted",
        )


class ExtractedProgramTests(unittest.TestCase):
    def test_every_extracted_step_keeps_its_keys_its_invocation_and_its_effective_directory(self) -> None:
        check_extracted_steps(WORKFLOW.read_text(encoding="utf-8"))

    def test_every_program_exists_carries_its_prologue_and_propagates_native_status(self) -> None:
        check_programs(ROOT)

    def test_no_inline_run_body_exceeds_the_ceiling_and_the_classifier_heredoc_never_returns(self) -> None:
        check_inline_bodies(WORKFLOW.read_text(encoding="utf-8"))


class ContractsActionTests(unittest.TestCase):
    def test_the_action_and_both_call_sites_match_the_pinned_shape(self) -> None:
        check_action(
            ACTION.read_text(encoding="utf-8"),
            {
                "ci.yml": WORKFLOW.read_text(encoding="utf-8"),
                "linux-native-v2-acceptance.yml": ACCEPTANCE.read_text(encoding="utf-8"),
            },
        )

    def test_the_caller_keeps_the_advisory_step_with_its_always_reachability(self) -> None:
        check_caller_advisory_step(WORKFLOW.read_text(encoding="utf-8"))


class FoldedGateCompletenessTests(unittest.TestCase):
    def test_every_advisory_step_is_asserted_by_its_job_or_allowlisted(self) -> None:
        check_folded_gate_completeness(WORKFLOW.read_text(encoding="utf-8"), ACTION.read_text(encoding="utf-8"))


class CredoGateStepTests(unittest.TestCase):
    def test_the_credo_gate_step_is_not_advisory_and_ends_in_the_gate_program(self) -> None:
        check_credo_gate_step(WORKFLOW.read_text(encoding="utf-8"))

    def test_the_duration_gate_step_is_not_advisory_and_ends_in_the_gate_program(self) -> None:
        check_duration_gate_step(WORKFLOW.read_text(encoding="utf-8"))

    def test_the_jscpd_gate_step_is_not_advisory_and_ends_in_the_gate_program(self) -> None:
        check_jscpd_gate_step(WORKFLOW.read_text(encoding="utf-8"))


class MutationTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        cls.workflow_text = WORKFLOW.read_text(encoding="utf-8")
        cls.action_text = ACTION.read_text(encoding="utf-8")
        cls.workflows = {
            "ci.yml": cls.workflow_text,
            "linux-native-v2-acceptance.yml": ACCEPTANCE.read_text(encoding="utf-8"),
        }

    def test_mutation_inlining_a_sixty_line_block_into_an_extracted_step_reddens_the_step_pin(self) -> None:
        inline = "        run: |\n" + "".join(f"          echo {index}\n" for index in range(60))
        mutated = self.workflow_text.replace(
            "        run: bash scripts/ci/compose-stack-smoke.sh\n",
            inline,
        )
        self.assertNotEqual(mutated, self.workflow_text, "the mutation must edit the workflow text")
        self.assertTrue(any(len(body) > 30 for _, body in inline_run_bodies(mutated)))
        with self.assertRaises(AssertionError):
            check_extracted_steps(mutated)

    def test_mutation_renaming_an_extracted_program_reddens_the_invocation_resolution(self) -> None:
        mutated = self.workflow_text.replace(
            "        run: bash ../scripts/ci/verify-linux-pi-pidfd-cleanup.sh\n",
            "        run: bash ../scripts/ci/verify-linux-pi-pidfd-cleanup-renamed.sh\n",
        )
        self.assertNotEqual(mutated, self.workflow_text, "the mutation must edit the workflow text")
        with self.assertRaises(AssertionError):
            check_extracted_steps(mutated)

    def test_mutation_dropping_the_portal_assets_upload_reddens_the_action_check(self) -> None:
        upload = (
            "    - uses: actions/upload-artifact@v7\n"
            "      with:\n"
            "        name: portal-assets\n"
            "        path: control/priv/static/portal\n"
        )
        self.assertIn(upload, self.action_text, "the mutation's anchor must be the portal-assets upload")
        mutated = self.action_text.replace(upload, "")
        self.assertNotEqual(mutated, self.action_text, "the mutation must edit the action text")
        with self.assertRaises(AssertionError):
            check_action(mutated, self.workflows)

    def test_mutation_dropping_the_inputs_required_flag_reddens_the_input_check(self) -> None:
        flag = "    required: true\n"
        self.assertEqual(self.action_text.count(flag), 1, "the mutation's anchor must be the input's flag")
        mutated = self.action_text.replace(flag, "")
        self.assertNotEqual(mutated, self.action_text, "the mutation must edit the action text")
        with self.assertRaises(AssertionError):
            check_action(mutated, self.workflows)

    def test_mutation_flipping_the_release_upload_condition_reddens_the_action_check(self) -> None:
        upload = (
            "    - uses: actions/upload-artifact@v7\n"
            "      if: inputs.build-control-release == 'true'\n"
            "      with:\n"
            "        name: control-release-linux\n"
        )
        self.assertIn(upload, self.action_text, "the mutation's anchor must be the release upload")
        mutated = self.action_text.replace(upload, upload.replace("== 'true'", "== 'false'"))
        self.assertNotEqual(mutated, self.action_text, "the mutation must edit the action text")
        with self.assertRaises(AssertionError):
            check_action(mutated, self.workflows)

    def test_mutation_narrowing_the_caller_advisory_reachability_reddens_the_step_pin(self) -> None:
        guard = "      - name: Measure production dependency advisories (advisory)\n        if: always()\n"
        self.assertIn(guard, self.workflow_text, "the mutation's anchor must be the contracts advisory step")
        mutated = self.workflow_text.replace(guard, guard.replace("if: always()", "if: success()"))
        self.assertNotEqual(mutated, self.workflow_text, "the mutation must edit the workflow text")
        with self.assertRaises(AssertionError):
            check_caller_advisory_step(mutated)

    def test_mutation_an_eighty_one_line_anonymous_run_step_reddens_the_inline_ceiling(self) -> None:
        anchor = "      - uses: actions/checkout@v7\n"
        self.assertIn(anchor, self.workflow_text, "the mutation's anchor must be a step head")
        inline = "      - run: |\n" + "".join(f"          echo {index}\n" for index in range(81))
        mutated = self.workflow_text.replace(anchor, anchor + inline, 1)
        self.assertNotEqual(mutated, self.workflow_text, "the mutation must edit the workflow text")
        with self.assertRaises(AssertionError):
            check_inline_bodies(mutated)

    def test_mutation_shortening_a_program_reddens_the_sha256_pin(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            for step in EXTRACTED_STEPS:
                target = root / step.program
                target.parent.mkdir(parents=True, exist_ok=True)
                shutil.copyfile(ROOT / step.program, target)
            program = root / "scripts" / "ci" / "compose-stack-smoke.sh"
            program.write_text("#!/usr/bin/env bash\nset -Eeuo pipefail\necho noop\n", encoding="utf-8")
            with self.assertRaises(AssertionError):
                check_programs(root)

    def test_mutation_the_direct_base_directory_read_reddens_the_missing_value_check(self) -> None:
        text = (ROOT / POSTGRES_PROGRAM).read_text(encoding="utf-8")
        self.assertIn(PSOBJECT_REGISTRY_READ, text, "the mutation's anchor must be the PSObject read")
        with tempfile.TemporaryDirectory() as temporary:
            mutated = pathlib.Path(temporary) / "pi-windows-postgres-loopback.ps1"
            mutated.write_text(
                text.replace(PSOBJECT_REGISTRY_READ, DIRECT_REGISTRY_READ), encoding="utf-8", newline="\n"
            )
            with self.assertRaises(AssertionError):
                check_registry_key_without_base_directory(mutated)

    def test_mutation_marking_an_unlisted_step_advisory_reddens_the_completeness_check(self) -> None:
        mutated = self.workflow_text.replace(
            "      - run: scripts/audit-no-legacy-routes.sh\n",
            "      - name: A new quiet step\n        continue-on-error: true\n"
            "        run: scripts/audit-no-legacy-routes.sh\n",
        )
        self.assertNotEqual(mutated, self.workflow_text, "the mutation must edit the workflow text")
        with self.assertRaises(AssertionError):
            check_folded_gate_completeness(mutated, self.action_text)

    def test_mutation_a_dash_first_advisory_key_is_discovered_and_then_requires_its_gate(self) -> None:
        mutated = self.workflow_text.replace(
            "      - run: scripts/audit-no-legacy-routes.sh\n",
            "      - continue-on-error: true\n        name: A new quiet step\n"
            "        run: scripts/audit-no-legacy-routes.sh\n",
        )
        self.assertNotEqual(mutated, self.workflow_text, "the mutation must edit the workflow text")
        with self.assertRaises(AssertionError):
            check_folded_gate_completeness(mutated, self.action_text)

    def test_mutation_commenting_out_a_gate_member_reddens_the_completeness_check(self) -> None:
        flagged = self.workflow_text.replace(
            "      - name: Prepare pinned OpenCode release\n",
            "      - name: Prepare pinned OpenCode release\n        continue-on-error: true\n",
        )
        self.assertNotEqual(flagged, self.workflow_text, "the mutation must flag the gate's step advisory")
        commented = flagged.replace(
            '            "Prepare pinned OpenCode release=$PREPARE_OUTCOME"\n',
            '            # "Prepare pinned OpenCode release=$PREPARE_OUTCOME"\n',
        )
        self.assertNotEqual(commented, flagged, "the mutation must comment out the gate member")
        with self.assertRaises(AssertionError):
            check_folded_gate_completeness(commented, self.action_text)

    def test_an_advisory_step_inside_a_folded_gate_array_is_accepted_without_an_allowlist_entry(self) -> None:
        mutated = self.workflow_text.replace(
            "      - name: Prepare pinned OpenCode release\n",
            "      - name: Prepare pinned OpenCode release\n        continue-on-error: true\n",
        )
        self.assertNotEqual(mutated, self.workflow_text, "the mutation must edit the workflow text")
        check_folded_gate_completeness(mutated, self.action_text)

    def test_mutation_removing_an_advisory_step_reddens_the_stale_allowlist_check(self) -> None:
        mutated = self.workflow_text.replace("        continue-on-error: true\n", "", 1)
        self.assertNotEqual(mutated, self.workflow_text, "the mutation must edit the workflow text")
        with self.assertRaises(AssertionError):
            check_folded_gate_completeness(mutated, self.action_text)

    def test_mutation_marking_the_control_coverage_job_advisory_reddens_the_completeness_check(self) -> None:
        mutated = self.workflow_text.replace(
            "  coverage-control:\n    needs: [changes, contracts]\n    if: needs.changes.outputs.run-heavy == 'true'\n",
            "  coverage-control:\n    needs: [changes, contracts]"
            "\n    if: needs.changes.outputs.run-heavy == 'true'\n    continue-on-error: true\n",
        )
        self.assertNotEqual(mutated, self.workflow_text, "the mutation must edit the workflow text")
        with self.assertRaises(AssertionError):
            check_folded_gate_completeness(mutated, self.action_text)

    def test_mutation_refusing_an_unknown_advisory_value_is_loud(self) -> None:
        mutated = self.workflow_text.replace(
            "        continue-on-error: true\n",
            "        continue-on-error: ${{ always() }}\n",
            1,
        )
        self.assertNotEqual(mutated, self.workflow_text, "the mutation must edit the workflow text")
        with self.assertRaises(AssertionError):
            check_folded_gate_completeness(mutated, self.action_text)

    def test_mutation_replacing_the_credo_gate_invocation_with_exit_zero_reddens_the_step_pin(self) -> None:
        mutated = self.workflow_text.replace(
            f"          {CREDO_GATE_INVOCATION}\n",
            "          exit 0\n",
        )
        self.assertNotEqual(mutated, self.workflow_text, "the mutation must edit the workflow text")
        self.assertNotIn(CREDO_GATE_INVOCATION, mutated)
        with self.assertRaises(AssertionError):
            check_credo_gate_step(mutated)

    def test_mutation_restoring_the_credo_steps_advisory_flag_reddens_the_step_pin(self) -> None:
        mutated = self.workflow_text.replace(
            f"      - name: {CREDO_GATE_STEP}\n",
            f"      - name: {CREDO_GATE_STEP}\n        continue-on-error: true\n",
        )
        self.assertNotEqual(mutated, self.workflow_text, "the mutation must edit the workflow text")
        with self.assertRaises(AssertionError):
            check_credo_gate_step(mutated)

    def test_mutation_dropping_the_credo_strict_flag_reddens_the_step_pin(self) -> None:
        mutated = self.workflow_text.replace(
            f"          {CREDO_STRICT_INVOCATION}\n",
            '          mix credo 2>&1 | tee "$credo_log"\n',
        )
        self.assertNotEqual(mutated, self.workflow_text, "the mutation must edit the workflow text")
        self.assertNotIn(f"          {CREDO_STRICT_INVOCATION}\n", mutated)
        with self.assertRaises(AssertionError):
            check_credo_gate_step(mutated)

    def test_mutation_inserting_an_exit_zero_skip_branch_reddens_the_step_pin(self) -> None:
        anchor = "          set -Eeuo pipefail\n"
        self.assertIn(anchor, self.workflow_text, "the mutation's anchor must be the step's prologue")
        mutated = self.workflow_text.replace(
            anchor,
            anchor + "          if [ ! -f .credo.exs ]; then exit 0; fi\n",
        )
        self.assertNotEqual(mutated, self.workflow_text, "the mutation must edit the workflow text")
        with self.assertRaises(AssertionError):
            check_credo_gate_step(mutated)

    def test_mutation_restoring_the_duration_gate_steps_advisory_flag_reddens_the_step_pin(self) -> None:
        mutated = self.workflow_text.replace(
            f"      - name: {DURATION_GATE_STEP}\n",
            f"      - name: {DURATION_GATE_STEP}\n        continue-on-error: true\n",
        )
        self.assertNotEqual(mutated, self.workflow_text, "the mutation must edit the workflow text")
        with self.assertRaises(AssertionError):
            check_duration_gate_step(mutated)

    def test_mutation_narrowing_the_duration_gate_steps_reachability_reddens_the_step_pin(self) -> None:
        mutated = self.workflow_text.replace(
            f"        if: {DURATION_GATE_CONDITION}\n",
            "        if: success()\n",
        )
        self.assertNotEqual(mutated, self.workflow_text, "the mutation must edit the workflow text")
        with self.assertRaises(AssertionError):
            check_duration_gate_step(mutated)

    def test_mutation_replacing_the_duration_gate_invocation_reddens_the_step_pin(self) -> None:
        mutated = self.workflow_text.replace(
            f"          {DURATION_GATE_INVOCATION}\n",
            "          exit 0\n",
        )
        self.assertNotEqual(mutated, self.workflow_text, "the mutation must edit the workflow text")
        with self.assertRaises(AssertionError):
            check_duration_gate_step(mutated)

    def test_mutation_dropping_the_jscpd_gate_step_reddens_the_step_pin(self) -> None:
        step = (
            f"      - name: {JSCPD_GATE_STEP}\n"
            "        env:\n"
            "          JSCPD_BASE: ${{ github.event_name == 'pull_request' && 'HEAD^1' || github.event.before }}\n"
            "        run: |\n"
            f"          {JSCPD_SUITE_INVOCATION}\n"
            f"          {JSCPD_GATE_INVOCATION}\n"
        )
        self.assertIn(step, self.workflow_text, "the mutation's anchor must be the jscpd gate step")
        mutated = self.workflow_text.replace(step, "")
        self.assertNotEqual(mutated, self.workflow_text, "the mutation must edit the workflow text")
        with self.assertRaises(AssertionError):
            check_jscpd_gate_step(mutated)

    def test_mutation_replacing_the_jscpd_gate_invocation_with_exit_zero_reddens_the_step_pin(self) -> None:
        mutated = self.workflow_text.replace(
            f"          {JSCPD_GATE_INVOCATION}\n",
            "          exit 0\n",
        )
        self.assertNotEqual(mutated, self.workflow_text, "the mutation must edit the workflow text")
        with self.assertRaises(AssertionError):
            check_jscpd_gate_step(mutated)

    def test_mutation_restoring_the_jscpd_steps_advisory_flag_reddens_the_step_pin(self) -> None:
        mutated = self.workflow_text.replace(
            f"      - name: {JSCPD_GATE_STEP}\n",
            f"      - name: {JSCPD_GATE_STEP}\n        continue-on-error: true\n",
        )
        self.assertNotEqual(mutated, self.workflow_text, "the mutation must edit the workflow text")
        with self.assertRaises(AssertionError):
            check_jscpd_gate_step(mutated)


class FailClosedPowerShellTests(unittest.TestCase):
    def test_the_propagation_guard_fails_where_the_bare_program_passes(self) -> None:
        pwsh = shutil.which("pwsh")
        if pwsh is None:
            self.skipTest("pwsh is not installed on this host; the reproduction is CI-side")
        guarded = "\n".join(
            (
                *POWERSHELL_PROLOGUE,
                "",
                "& pwsh -NoProfile -NonInteractive -Command 'exit 7'",
                PROPAGATION_GUARD,
                "",
            )
        )
        bare = "\n".join((*POWERSHELL_PROLOGUE, "", "& pwsh -NoProfile -NonInteractive -Command 'exit 7'", ""))
        with tempfile.TemporaryDirectory() as temporary:
            guarded_path = pathlib.Path(temporary, "guarded.ps1")
            bare_path = pathlib.Path(temporary, "bare.ps1")
            guarded_path.write_text(guarded, encoding="utf-8", newline="\n")
            bare_path.write_text(bare, encoding="utf-8", newline="\n")
            guarded_result = subprocess.run(
                [pwsh, "-NoProfile", "-NonInteractive", "-File", str(guarded_path)],
                capture_output=True,
                text=True,
                timeout=120,
                check=False,
            )
            bare_result = subprocess.run(
                [pwsh, "-NoProfile", "-NonInteractive", "-File", str(bare_path)],
                capture_output=True,
                text=True,
                timeout=120,
                check=False,
            )
        self.assertEqual(guarded_result.returncode, 7, guarded_result.stderr)
        self.assertEqual(bare_result.returncode, 0, bare_result.stderr)

    def test_a_registry_key_without_base_directory_is_skipped_like_the_base_body(self) -> None:
        check_registry_key_without_base_directory(ROOT / POSTGRES_PROGRAM)


if __name__ == "__main__":
    unittest.main()
