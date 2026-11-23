#!/usr/bin/env python3

import ast
import collections.abc
import dataclasses
import importlib.util
import os
import pathlib
import re
import subprocess
import sys
import tempfile
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[2]
WORKFLOW = ROOT / ".github" / "workflows" / "ci.yml"
CI_DIR = ROOT / "scripts" / "ci"
CLASSIFIER = CI_DIR / "classify-changed-paths.py"
CORPUS_MODULE = CI_DIR / "audited_paths.py"
CITATION_GATE = CI_DIR / "check-citation-existence.py"

OUTPUT_KEYS = ("run-heavy", "run-records", "run-citations")
EVENT_CLAUSE = "github.event_name != 'pull_request'"
FOLD_LITERAL = "github.event_name != 'pull_request' || steps.paths.outputs.{key} == 'true'"

# The classifier variable that computes each output, as the program spells it.
PREDICATE_NAMES = {"run-heavy": "run_heavy", "run-records": "run_records", "run-citations": "run_citations"}

# The job each output gates. The cleanliness and citation gates are deliberately
# independent of the heavy matrix, so they read their own output; every other
# job reads run-heavy. A renamed job falls back to run-heavy and must then fail
# the gating check below loudly rather than pass silently.
PINNED_GATES = {"record-cleanliness": "run-records", "citation-existence": "run-citations"}
DEFAULT_GATE = "run-heavy"
UNCONDITIONAL_JOBS = ("legacy-route-audit",)
COVERAGE_JOBS = ("coverage-control", "coverage-daemon", "coverage-frontend")
COVERAGE_DAEMON_HEADER = "  coverage-daemon:\n    needs: changes\n    if: needs.changes.outputs.run-heavy == 'true'\n"
COVERAGE_CONTROL_HEADER = (
    "  coverage-control:\n    needs: [changes, contracts]\n    if: needs.changes.outputs.run-heavy == 'true'\n"
)
# The Control floor is blocking (rf-w6-10): its job drops the advisory flag, while the Go and
# vitest jobs stay advisory until their own flips (rf-w6-12).
BLOCKING_COVERAGE_JOBS = ("coverage-control",)
ADVISORY_COVERAGE_JOBS = ("coverage-daemon", "coverage-frontend")
# No package is excluded from the daemon cover command: `go test` sets GOCOVERDIR for each
# instrumented test binary, and `internal/execution`'s re-exec'd helper children forward it.
COVERAGE_UPLOADS = {
    "coverage-control": (
        "        if: always()\n"
        "        uses: actions/upload-artifact@v7\n"
        "        with:\n"
        "          name: control-coverage\n"
        "          path: control/cover\n"
    ),
    "coverage-daemon": (
        "        if: always()\n"
        "        uses: actions/upload-artifact@v7\n"
        "        with:\n"
        "          name: daemon-coverage\n"
        "          path: daemon/coverage.out\n"
    ),
    "coverage-frontend": (
        "        if: always()\n"
        "        uses: actions/upload-artifact@v7\n"
        "        with:\n"
        "          name: frontend-coverage\n"
        "          path: frontend/coverage\n"
    ),
}
# Both working directories are load-bearing (the commands write `control/cover/` and
# `daemon/coverage.out` relative to them); `coverage-frontend` must run at the pnpm workspace root.
COVERAGE_DEFAULTS = {
    "coverage-control": "    defaults:\n      run:\n        working-directory: control\n",
    "coverage-daemon": "    defaults:\n      run:\n        working-directory: daemon\n",
}
COVERAGE_COMMANDS = {
    "coverage-control": ("        run: mix test --cover",),
    "coverage-daemon": (
        "          python3 ../scripts/ci/stdio-only-caller-boundary.py \\",
        '            "$(command -v go)" test -count=1 -timeout 1200s -coverprofile=coverage.out ./...',
    ),
    "coverage-frontend": ("        run: pnpm --filter @symmetry/frontend exec vitest run --coverage",),
}

# Updated together with the corpus: an entry without a fixture here fails
# test_every_audited_document_gates_only_run_citations.
AUDITED_FIXTURES = {
    "docs/runbooks": "docs-runbooks-modified",
    "docs/design/quality.md": "quality-design-modified",
    "docs/archive/CLOSED-TODO.md": "closed-todo-modified",
}

# A *new* path under a directory corpus entry; its fixture's pinned triple is the only
# witness that the family derivation, not an exact match, routes the citation job.
AUDITED_FAMILY_FIXTURES = {"docs/runbooks": "docs-runbooks-added"}

# Not audited documents: these paths' run-citations literals are explained by this
# mapping's fixtures rather than by the audited-corpus check.
CITATION_TOOL_FIXTURES = {
    "scripts/ci/check-citation-existence.py": "citation-tool-modified",
    "scripts/ci/test-check-citation-existence.py": "citation-tests-modified",
    "scripts/ci/revision_scope.py": "revision-scope-modified",
    "scripts/ci/audited_paths.py": "audited-paths-modified",
}

JOB_HEADER = re.compile(r"^  ([a-z][a-z0-9-]*):$")
NEEDS_LINE = re.compile(r"^    needs:\s*(\S.*?)\s*$")
CONDITION_LINE = re.compile(r"^    if:\s*(\S.*?)\s*$")
FOLD_LINE = re.compile(r"^      ([a-z][a-z0-9-]*): \$\{\{ (.+?) \}\}\s*$")
JOB_CONDITION = re.compile(r"needs\.changes\.outputs\.(run-heavy|run-records|run-citations) == 'true'")
STEP_CONDITION = re.compile(r"steps\.paths\.outputs\.(run-heavy|run-records|run-citations) == 'true'")

# daemon/main.go must carry enough lines for git's rename detection to pair the
# two sides of the rename fixture when rename detection is left on; a few lines
# would be reported as an unrelated delete and add even without --no-renames.
DAEMON_MAIN = (
    "package main\n"
    "\n"
    'import "fmt"\n'
    "\n"
    + "".join(f"// padding line {index}\n" for index in range(24))
    + '\nfunc main() {\n\tfmt.Println("fixture")\n}\n'
)

BASE_FILES = {
    "AGENTS.md": "# Agent instructions\n\nBase instructions.\n",
    "daemon/main.go": DAEMON_MAIN,
    "docs/example.json": '{"fixture": "base"}\n',
    "docs/archive/CLOSED-TODO.md": "# Closed TODO\n\nBase archive text.\n",
    "docs/design/quality.md": "# Quality\n\nBase design text.\n",
    "docs/runbooks/portal.md": "# Portal\n\nBase runbook text.\n",
    "scripts/ci/audited_paths.py": "# Stand-in bytes: the classifier reads this path, never its content.\n",
    "scripts/ci/check-citation-existence.py": "# Stand-in bytes: the classifier reads this path, never its content.\n",
    "scripts/ci/check-record-cleanliness.py": "# Stand-in bytes: the classifier reads this path, never its content.\n",
    "scripts/ci/model-routing-record-sanitize.py": (
        "# Stand-in bytes: the classifier reads this path, never its content.\n"
    ),
    "scripts/ci/revision_scope.py": "# Stand-in bytes: the classifier reads this path, never its content.\n",
    "scripts/ci/test-check-citation-existence.py": (
        "# Stand-in bytes: the classifier reads this path, never its content.\n"
    ),
}


def require(condition: bool, message: str) -> None:
    """Fail loudly with `message` unless `condition` holds."""
    if not condition:
        raise AssertionError(message)


def first_index(lines: list[str], predicate: collections.abc.Callable[[str], bool], message: str) -> int:
    for index, line in enumerate(lines):
        if predicate(line):
            return index
    raise AssertionError(message)


def job_blocks(workflow_text: str) -> dict[str, list[str]]:
    """The workflow's jobs, keyed by name: header line through the next header."""
    lines = workflow_text.splitlines()
    jobs_index = first_index(lines, lambda line: line == "jobs:", "the workflow has no top-level `jobs:` line")
    blocks: dict[str, list[str]] = {}
    name: str | None = None
    start = jobs_index
    for index in range(jobs_index + 1, len(lines)):
        match = JOB_HEADER.match(lines[index])
        if match is None:
            continue
        if name is not None:
            blocks[name] = lines[start:index]
        name = match.group(1)
        start = index
    if name is not None:
        blocks[name] = lines[start:]
    require(bool(blocks), "no job headers found after the workflow's `jobs:` line")
    return blocks


def module_level_names(program_text: str, filename: str) -> set[str]:
    names: set[str] = set()
    for node in ast.parse(program_text, filename=filename).body:
        targets: tuple[ast.expr, ...] = ()
        if isinstance(node, ast.Assign):
            targets = tuple(node.targets)
        elif isinstance(node, ast.AnnAssign):
            targets = (node.target,)
        for target in targets:
            if isinstance(target, ast.Name):
                names.add(target.id)
    return names


def imported_names(program_text: str, filename: str, module: str) -> set[str]:
    names: set[str] = set()
    for node in ast.parse(program_text, filename=filename).body:
        if isinstance(node, ast.ImportFrom) and node.level == 0 and node.module == module:
            names.update(alias.name for alias in node.names)
    return names


def predicate_assignment(tree: ast.Module, output: str) -> ast.Assign | None:
    for node in tree.body:
        if not (isinstance(node, ast.Assign) and len(node.targets) == 1 and isinstance(node.targets[0], ast.Name)):
            continue
        if node.targets[0].id == PREDICATE_NAMES[output]:
            return node
    return None


def predicate_literals(program_text: str) -> dict[str, tuple[str, ...]]:
    """The string constants of the program's three predicates; parsed, never executed."""
    tree = ast.parse(program_text, filename=CLASSIFIER.name)
    literals: dict[str, tuple[str, ...]] = {}
    for output in OUTPUT_KEYS:
        assignment = predicate_assignment(tree, output)
        if assignment is None:
            continue
        literals[output] = tuple(
            child.value.decode("utf-8") if isinstance(child.value, bytes) else child.value
            for child in ast.walk(assignment.value)
            if isinstance(child, ast.Constant) and isinstance(child.value, (str, bytes))
        )
    require(
        set(literals) == set(OUTPUT_KEYS),
        f"the classifier must assign {sorted(PREDICATE_NAMES.values())}; found {sorted(literals)}",
    )
    return literals


def loaded_names(node: ast.AST) -> set[str]:
    return {child.id for child in ast.walk(node) if isinstance(child, ast.Name) and isinstance(child.ctx, ast.Load)}


def check_corpus_single_definition() -> None:
    definers = sorted(
        path.name
        for path in sorted(CI_DIR.glob("*.py"))
        if "AUDITED_PATHS" in module_level_names(path.read_text(encoding="utf-8"), path.name)
    )
    require(
        definers == [CORPUS_MODULE.name],
        f"AUDITED_PATHS must be defined exactly once under scripts/ci, in "
        f"{CORPUS_MODULE.name}; it is assigned in {definers}",
    )
    for consumer in (CITATION_GATE, CLASSIFIER):
        require(
            "AUDITED_PATHS" in imported_names(consumer.read_text(encoding="utf-8"), consumer.name, CORPUS_MODULE.stem),
            f"{consumer.name} must import AUDITED_PATHS from {CORPUS_MODULE.name}",
        )
    gate_text = CITATION_GATE.read_text(encoding="utf-8")
    require(
        "AUDITED_PATHS" in loaded_names(ast.parse(gate_text, filename=CITATION_GATE.name)),
        f"{CITATION_GATE.name} imports AUDITED_PATHS but never loads it: the audited corpus "
        "must be the pathspec the gate actually selects documents with",
    )


def check_run_citations_consumes_corpus(program_text: str, literals: tuple[str, ...]) -> None:
    """A literal copy is equal today and diverges as the corpus grows; the `Name` load is the pin."""
    assignment = predicate_assignment(ast.parse(program_text, filename=CLASSIFIER.name), "run-citations")
    require(assignment is not None, f"the classifier assigns no {PREDICATE_NAMES['run-citations']} predicate")
    require(
        "AUDITED_PATHS" in loaded_names(assignment),
        f"the {PREDICATE_NAMES['run-citations']} predicate must load AUDITED_PATHS from the shared "
        f"corpus module {CORPUS_MODULE.name}; an inlined copy of the tuple is not the shared source",
    )
    for tool in CITATION_TOOL_FIXTURES:
        require(
            any(covers(literal, tool) for literal in literals),
            f"the {PREDICATE_NAMES['run-citations']} predicate must still name the citation tool path {tool!r}",
        )


def load_audited_paths() -> tuple[str, ...]:
    require(CORPUS_MODULE.is_file(), f"the shared corpus module {CORPUS_MODULE} does not exist")
    spec = importlib.util.spec_from_file_location("audited_paths_under_test", CORPUS_MODULE)
    require(spec is not None and spec.loader is not None, f"cannot load {CORPUS_MODULE}")
    module = importlib.util.module_from_spec(spec)
    # The module has no imports of its own, so it needs no sys.path entry.
    spec.loader.exec_module(module)
    audited = getattr(module, "AUDITED_PATHS", None)
    require(isinstance(audited, tuple) and audited, f"{CORPUS_MODULE.name} declares no non-empty AUDITED_PATHS tuple")
    for entry in audited:
        require(
            isinstance(entry, str) and entry and not entry.startswith("/") and ".." not in entry.split("/"),
            f"unparsable AUDITED_PATHS entry {entry!r}",
        )
    # Loading the gate the way a run does (its directory on sys.path) pins that the value
    # it ends up with is the shared tuple.
    require(CITATION_GATE.is_file(), f"the citation gate {CITATION_GATE} does not exist")
    sys.path.insert(0, str(CI_DIR))
    try:
        gate_spec = importlib.util.spec_from_file_location("check_citation_existence_under_test", CITATION_GATE)
        require(gate_spec is not None and gate_spec.loader is not None, f"cannot load {CITATION_GATE}")
        gate = importlib.util.module_from_spec(gate_spec)
        gate_spec.loader.exec_module(gate)
    finally:
        sys.path.remove(str(CI_DIR))
    require(
        getattr(gate, "AUDITED_PATHS", None) == audited,
        f"{CITATION_GATE.name} must import the shared corpus {audited!r}; it holds "
        f"{getattr(gate, 'AUDITED_PATHS', None)!r}",
    )
    return audited


def covers(literal: str, entry: str) -> bool:
    """Whether a classifier literal names the corpus entry itself or a path under it."""
    return literal == entry or literal == entry + "/" or literal.startswith(entry + "/")


PATH_SHAPED = re.compile(r"[A-Za-z0-9._-]+/[A-Za-z0-9._-]+")


def path_literals(literals: tuple[str, ...]) -> tuple[str, ...]:
    """The literals that spell a path; drops the composed `b"/"` separator of the family derivation."""
    return tuple(literal for literal in literals if PATH_SHAPED.search(literal))


def check_family_fixture_coverage(audited: tuple[str, ...]) -> None:
    """Each directory entry's new-path fixture is the witness that the family, not a literal, routes it."""
    directory_entries = sorted(entry for entry in audited if (ROOT / entry).is_dir())
    require(
        sorted(AUDITED_FAMILY_FIXTURES) == directory_entries,
        f"every directory corpus entry {directory_entries} needs a new-path fixture; "
        f"found {sorted(AUDITED_FAMILY_FIXTURES)}",
    )
    for entry in directory_entries:
        fixture = FIXTURES_BY_NAME[AUDITED_FAMILY_FIXTURES[entry]]
        require(len(fixture.paths) == 1, f"family fixture {fixture.name} must change exactly one path")
        (added,) = fixture.paths
        modified = FIXTURES_BY_NAME[AUDITED_FIXTURES[entry]]
        require(
            added.startswith(entry + "/") and added not in modified.paths,
            f"family fixture {fixture.name} must add a new path under {entry!r}; its diff touches {added!r}",
        )


# -- the workflow's job model -------------------------------------------------


@dataclasses.dataclass(frozen=True)
class Job:
    name: str
    needs: tuple[str, ...]
    condition: str | None


@dataclasses.dataclass(frozen=True)
class WorkflowModel:
    jobs: dict[str, Job]
    folds: dict[str, str]


def parse_needs(raw: str, job: str) -> tuple[str, ...]:
    """A `needs:` value: one job name, or a `[a, b]` list, normalized to a tuple."""
    body = raw.strip()
    if body.startswith("["):
        require(body.endswith("]"), f"job {job}: `needs:` list {raw!r} is not closed")
        body = body[1:-1]
    names = tuple(part.strip() for part in body.split(",") if part.strip())
    require(bool(names), f"job {job}: `needs:` declares no job")
    for name in names:
        require(re.fullmatch(r"[a-z][a-z0-9-]*", name) is not None, f"job {job}: unparsable `needs:` entry {name!r}")
    return names


def parse_model(workflow_text: str) -> WorkflowModel:
    """The job model: every job's needs and whitespace-normalized `if:`, plus the changes outputs."""
    blocks = job_blocks(workflow_text)
    jobs: dict[str, Job] = {}
    for name, block in blocks.items():
        needs: tuple[str, ...] = ()
        condition: str | None = None
        for line in block:
            match = NEEDS_LINE.match(line)
            if match is not None:
                needs = parse_needs(match.group(1), name)
                continue
            match = CONDITION_LINE.match(line)
            if match is not None:
                condition = re.sub(r"\s+", " ", match.group(1))
        jobs[name] = Job(name=name, needs=needs, condition=condition)
    folds: dict[str, str] = {}
    in_outputs = False
    for line in blocks.get("changes", []):
        if line == "    outputs:":
            in_outputs = True
            continue
        if in_outputs:
            match = FOLD_LINE.match(line)
            if match is not None:
                folds[match.group(1)] = match.group(2).strip()
    return WorkflowModel(jobs=jobs, folds=folds)


def check_output_folds(model: WorkflowModel) -> None:
    """The changes job declares exactly the three outputs, each as the pinned fold."""
    require(
        set(model.folds) == set(OUTPUT_KEYS),
        f"the changes job must declare exactly the outputs {sorted(OUTPUT_KEYS)}; found {sorted(model.folds)}",
    )
    for key in OUTPUT_KEYS:
        expected = FOLD_LITERAL.format(key=key)
        require(
            model.folds[key] == expected, f"the {key} fold changed: expected {expected!r}, found {model.folds[key]!r}"
        )


def check_job_gating(model: WorkflowModel) -> None:
    """Every job's gating matches the pinned routing, and nothing is left unrecognized."""
    jobs = model.jobs
    missing = [name for name in (*UNCONDITIONAL_JOBS, "changes") if name not in jobs]
    require(not missing, f"the workflow is missing the jobs {missing}")
    unconditional = sorted(
        name for name, job in jobs.items() if name != "changes" and not job.needs and job.condition is None
    )
    require(
        unconditional == sorted(UNCONDITIONAL_JOBS),
        f"{sorted(UNCONDITIONAL_JOBS)} must be the only unconditional jobs besides the classifier; "
        f"found {unconditional}",
    )
    for name, job in jobs.items():
        if job.condition is not None:
            require(
                JOB_CONDITION.fullmatch(job.condition) is not None,
                f"job {name} has an unrecognized `if:` shape: {job.condition!r}",
            )
    for name, job in jobs.items():
        if name == "changes" or name in UNCONDITIONAL_JOBS:
            continue
        gate = PINNED_GATES.get(name, DEFAULT_GATE)
        require("changes" in job.needs, f"job {name} must depend on `changes`; needs={job.needs!r}")
        expected = f"needs.changes.outputs.{gate} == 'true'"
        require(
            job.condition == expected, f"job {name} must gate on {gate}: expected {expected!r}, found {job.condition!r}"
        )


def evaluate_job_condition(condition: str, outputs: dict[str, bool]) -> bool:
    """A gated job's `if:` value; refusing anything but the three known shapes."""
    match = JOB_CONDITION.fullmatch(condition)
    require(match is not None, f"refusing to guess at job condition {condition!r}")
    return outputs[match.group(1)]


def evaluate_fold(expression: str, event_name: str, step_outputs: dict[str, bool]) -> bool:
    """A changes-output fold for one event, clause by clause; refusing unknown clauses."""
    values: list[bool] = []
    for clause in (part.strip() for part in expression.split("||")):
        if clause == EVENT_CLAUSE:
            values.append(event_name != "pull_request")
            continue
        match = STEP_CONDITION.fullmatch(clause)
        require(match is not None, f"refusing to guess at fold clause {clause!r} of {expression!r}")
        values.append(step_outputs[match.group(1)])
    return any(values)


def effective_outputs(model: WorkflowModel, event_name: str, step_outputs: dict[str, bool]) -> dict[str, bool]:
    """The changes job's outputs for one event, from the step outputs the program wrote."""
    require(
        set(model.folds) == set(OUTPUT_KEYS),
        f"the changes job must declare exactly the outputs {sorted(OUTPUT_KEYS)}; found {sorted(model.folds)}",
    )
    require(
        set(step_outputs) == set(OUTPUT_KEYS),
        f"expected the classifier outputs {sorted(OUTPUT_KEYS)}; got {sorted(step_outputs)}",
    )
    return {key: evaluate_fold(model.folds[key], event_name, step_outputs) for key in OUTPUT_KEYS}


def runs(model: WorkflowModel, outputs: dict[str, bool], job_name: str) -> bool:
    """Whether a job runs with the folded outputs; its needs run first."""
    if job_name == "changes":
        return True
    job = model.jobs[job_name]
    for need in job.needs:
        require(need in model.jobs, f"job {job_name} needs unknown job {need!r}")
        if not runs(model, outputs, need):
            return False
    return job.condition is None or evaluate_job_condition(job.condition, outputs)


def ran_jobs(model: WorkflowModel, event_name: str, step_outputs: dict[str, bool]) -> set[str]:
    """The jobs that run for one event, the changes classifier itself excluded."""
    outputs = effective_outputs(model, event_name, step_outputs)
    return {name for name in model.jobs if name != "changes" and runs(model, outputs, name)}


def pinned_ran_jobs(job_names: collections.abc.Iterable[str], outputs: dict[str, bool]) -> set[str]:
    """The jobs the pinned routing says run for one event's folded outputs."""
    ran = {name for name in job_names if name in UNCONDITIONAL_JOBS}
    for name in job_names:
        if name == "changes" or name in UNCONDITIONAL_JOBS:
            continue
        if outputs[PINNED_GATES.get(name, DEFAULT_GATE)]:
            ran.add(name)
    return ran


def check_coverage_plane_routing(
    model: WorkflowModel,
    outputs_by_fixture: collections.abc.Mapping[str, dict[str, bool]],
) -> None:
    for name in COVERAGE_JOBS:
        require(name in model.jobs, f"the workflow has no coverage job {name!r}")
    for fixture_name, outputs in outputs_by_fixture.items():
        ran = ran_jobs(model, "pull_request", outputs)
        heavy = outputs["run-heavy"]
        for name in COVERAGE_JOBS:
            require(
                (name in ran) == heavy,
                f"fixture {fixture_name}: coverage job {name} must run exactly when run-heavy is "
                f"true (run-heavy={heavy}); it {'did not run' if heavy else 'ran'}",
            )


def check_coverage_jobs_are_advisory(workflow_text: str) -> None:
    blocks = job_blocks(workflow_text)
    for name in ADVISORY_COVERAGE_JOBS:
        require(name in blocks, f"the workflow has no coverage job {name!r}")
        require(
            any(line == "    continue-on-error: true" for line in blocks[name]),
            f"coverage job {name} must carry the job-level `continue-on-error: true`: a coverage "
            "failure must not be able to gate a pull request",
        )


def check_the_control_coverage_floor_is_blocking(workflow_text: str) -> None:
    blocks = job_blocks(workflow_text)
    for name in BLOCKING_COVERAGE_JOBS:
        require(name in blocks, f"the workflow has no coverage job {name!r}")
        require(
            not any(line.startswith("    continue-on-error:") for line in blocks[name]),
            f"coverage job {name} must drop its job-level `continue-on-error`: its floor is blocking, "
            "so a coverage count below the mix.exs threshold has to fail the job",
        )


def check_coverage_artifacts(workflow_text: str) -> None:
    blocks = job_blocks(workflow_text)
    for name in COVERAGE_JOBS:
        require(name in blocks, f"the workflow has no coverage job {name!r}")
        block = "\n".join(blocks[name])
        require(
            COVERAGE_UPLOADS[name] in block,
            f"coverage job {name} must carry its pinned upload step:\n{COVERAGE_UPLOADS[name]}",
        )
        if name in COVERAGE_DEFAULTS:
            require(
                COVERAGE_DEFAULTS[name] in block,
                f"coverage job {name} must keep the working directory its command resolves against:\n"
                f"{COVERAGE_DEFAULTS[name]}",
            )
        for line in COVERAGE_COMMANDS[name]:
            require(line in block, f"coverage job {name} must keep its pinned command line {line.strip()!r}")


STEP_HEADER = re.compile(r"^      - ")
STEP_KEY = re.compile(r"^        ([a-z][a-z0-9-]*): (.+?)\s*$")
CLASSIFIER_INVOCATION = f"run: python3 {CLASSIFIER.relative_to(ROOT).as_posix()}"


def classifier_step_keys(workflow_text: str) -> dict[str, str]:
    """The scalar keys of the classifier step; its ``if:`` is routing, not an implementation detail."""
    block = job_blocks(workflow_text).get("changes")
    require(block is not None, "the workflow has no `changes:` job")
    anchor = first_index(
        block,
        lambda line: line.strip() == CLASSIFIER_INVOCATION,
        f"the changes job has no step whose `{CLASSIFIER_INVOCATION}` runs the classifier program",
    )
    headers = [index for index in range(anchor) if STEP_HEADER.match(block[index])]
    require(bool(headers), "the classifier step has no `- ` header line above it")
    keys: dict[str, str] = {}
    for line in block[headers[-1] : anchor]:
        match = STEP_KEY.match(line)
        if match is not None:
            keys[match.group(1)] = match.group(2)
    return keys


def check_classifier_step(keys: dict[str, str], changes_block: list[str]) -> None:
    require(
        keys.get("id") == "paths",
        "the classifier step must carry `id: paths`: the outputs' fold reads "
        f"`steps.paths.outputs`; found {keys.get('id')!r}",
    )
    require(
        keys.get("if") == "github.event_name == 'pull_request'",
        "the classifier step must run only on pull requests - the event whose "
        f"diff (HEAD^1..HEAD) the program classifies; found {keys.get('if')!r}",
    )
    require(keys.get("shell") == "bash", f"the classifier step must pin `shell: bash`; found {keys.get('shell')!r}")
    require(
        not any("python3 - <<'PY'" in line for line in changes_block),
        "the changes job must not carry the classifier program in a `python3 - <<'PY'` heredoc "
        f"again: the step runs {CLASSIFIER.relative_to(ROOT).as_posix()}",
    )


# -- fixture repositories -----------------------------------------------------


def write_fixture_file(repo: pathlib.Path, relative: str, content: str) -> None:
    path = repo / relative
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(content, encoding="utf-8", newline="\n")


def rewrite_fixture_file(relative: str, content: str) -> collections.abc.Callable[[pathlib.Path], None]:
    def action(repo: pathlib.Path) -> None:
        write_fixture_file(repo, relative, content)

    return action


def rewrite_fixture_files(*pairs: tuple[str, str]) -> collections.abc.Callable[[pathlib.Path], None]:
    def action(repo: pathlib.Path) -> None:
        for relative, content in pairs:
            write_fixture_file(repo, relative, content)

    return action


def rename_across_docs_boundary(repo: pathlib.Path) -> None:
    # AGENTS.md is a Markdown file, and the heavy predicate exempts every `*.md`
    # path, so a rename of one reports run-heavy=false on both sides and pins
    # nothing about --no-renames. A file outside docs/ that is not Markdown does:
    # with --no-renames both sides are reported and the deleted daemon/main.go
    # trips the heavy predicate; with rename detection left on git reports only
    # docs/daemon-main.go and run-heavy would be false.
    git(repo, "mv", "daemon/main.go", "docs/daemon-main.go")
    write_fixture_file(repo, "docs/daemon-main.go", DAEMON_MAIN + "// relocated\n")


def delete_fixture_file(relative: str) -> collections.abc.Callable[[pathlib.Path], None]:
    def action(repo: pathlib.Path) -> None:
        (repo / relative).unlink()

    return action


@dataclasses.dataclass(frozen=True)
class Fixture:
    name: str
    intent: str
    expected: tuple[bool, bool, bool]
    paths: tuple[str, ...]
    action: collections.abc.Callable[[pathlib.Path], None]


FIXTURES = (
    Fixture(
        "docs-runbooks-modified",
        "a modified runbook is documentation-only but an audited document",
        (False, False, True),
        ("docs/runbooks/portal.md",),
        rewrite_fixture_file("docs/runbooks/portal.md", "# Portal\n\nEdited runbook text.\n"),
    ),
    Fixture(
        "docs-runbooks-added",
        "a new file under a directory corpus entry still gates the citation job",
        (False, False, True),
        ("docs/runbooks/new-runbook.md",),
        rewrite_fixture_file("docs/runbooks/new-runbook.md", "# New runbook\n\nAdded runbook text.\n"),
    ),
    Fixture(
        "root-markdown-modified",
        "a root Markdown file is documentation-only for every output",
        (False, False, False),
        ("AGENTS.md",),
        rewrite_fixture_file("AGENTS.md", "# Agent instructions\n\nEdited instructions.\n"),
    ),
    Fixture(
        "docs-json-modified",
        "a docs/** file that is not Markdown is documentation-only for every output",
        (False, False, False),
        ("docs/example.json",),
        rewrite_fixture_file("docs/example.json", '{"fixture": "edited"}\n'),
    ),
    Fixture(
        "evaluations-record-added",
        "an added evaluation record gates the cleanliness job",
        (False, True, False),
        ("docs/evaluations/model-routing/x.md",),
        rewrite_fixture_file("docs/evaluations/model-routing/x.md", '{"route": "fixture"}\n'),
    ),
    Fixture(
        "evidence-record-added",
        "an added evidence record gates the cleanliness job",
        (False, True, False),
        ("docs/evidence/x.md",),
        rewrite_fixture_file("docs/evidence/x.md", "Fixture evidence.\n"),
    ),
    Fixture(
        "sanitize-tool-modified",
        "a changed record sanitize tool gates the cleanliness job",
        (True, True, False),
        ("scripts/ci/model-routing-record-sanitize.py",),
        rewrite_fixture_file("scripts/ci/model-routing-record-sanitize.py", "# Stand-in bytes, edited.\n"),
    ),
    Fixture(
        "cleanliness-tool-modified",
        "a changed cleanliness gate gates its own job",
        (True, True, False),
        ("scripts/ci/check-record-cleanliness.py",),
        rewrite_fixture_file("scripts/ci/check-record-cleanliness.py", "# Stand-in bytes, edited.\n"),
    ),
    Fixture(
        "quality-design-modified",
        "a modified audited design file gates only the citation job",
        (False, False, True),
        ("docs/design/quality.md",),
        rewrite_fixture_file("docs/design/quality.md", "# Quality\n\nEdited design text.\n"),
    ),
    Fixture(
        "closed-todo-modified",
        "a modified audited archive file gates only the citation job",
        (False, False, True),
        ("docs/archive/CLOSED-TODO.md",),
        rewrite_fixture_file("docs/archive/CLOSED-TODO.md", "# Closed TODO\n\nEdited archive text.\n"),
    ),
    Fixture(
        "daemon-code-modified",
        "a daemon change gates only the heavy matrix",
        (True, False, False),
        ("daemon/main.go",),
        rewrite_fixture_file("daemon/main.go", DAEMON_MAIN + "// fixture edit\n"),
    ),
    Fixture(
        "mixed-docs-and-code",
        "a code change beside an audited document gates the heavy matrix and the citation job",
        (True, False, True),
        ("daemon/main.go", "docs/runbooks/portal.md"),
        rewrite_fixture_files(
            ("daemon/main.go", DAEMON_MAIN + "// mixed edit\n"),
            ("docs/runbooks/portal.md", "# Portal\n\nEdited together with code.\n"),
        ),
    ),
    Fixture(
        "rename-across-docs-boundary",
        "a non-Markdown file moved into docs/ with a small edit: --no-renames reports both sides",
        (True, False, False),
        ("daemon/main.go", "docs/daemon-main.go"),
        rename_across_docs_boundary,
    ),
    Fixture(
        "runbook-deleted",
        "a deleted runbook is an audited document change",
        (False, False, True),
        ("docs/runbooks/portal.md",),
        delete_fixture_file("docs/runbooks/portal.md"),
    ),
    Fixture(
        "revision-scope-modified",
        "the shared revision-range module gates the record and citation jobs and the heavy matrix",
        (True, True, True),
        ("scripts/ci/revision_scope.py",),
        rewrite_fixture_file("scripts/ci/revision_scope.py", "# Stand-in bytes, edited.\n"),
    ),
    Fixture(
        "citation-tool-modified",
        "a changed citation gate gates its own job",
        (True, False, True),
        ("scripts/ci/check-citation-existence.py",),
        rewrite_fixture_file("scripts/ci/check-citation-existence.py", "# Stand-in bytes, edited.\n"),
    ),
    Fixture(
        "citation-tests-modified",
        "a changed citation fixture suite gates the citation job",
        (True, False, True),
        ("scripts/ci/test-check-citation-existence.py",),
        rewrite_fixture_file("scripts/ci/test-check-citation-existence.py", "# Stand-in bytes, edited.\n"),
    ),
    Fixture(
        "audited-paths-modified",
        "a changed corpus module gates the citation job that imports it",
        (True, False, True),
        ("scripts/ci/audited_paths.py",),
        rewrite_fixture_file("scripts/ci/audited_paths.py", "# Stand-in bytes, edited.\n"),
    ),
)

FIXTURES_BY_NAME = {fixture.name: fixture for fixture in FIXTURES}
require(len(FIXTURES_BY_NAME) == len(FIXTURES), "fixture names must be unique")
require(set(PINNED_GATES.values()) <= set(OUTPUT_KEYS), "every pinned gate must name a classifier output")
require(DEFAULT_GATE in OUTPUT_KEYS, "the default gate must name a classifier output")
require(
    set(CITATION_TOOL_FIXTURES) <= set(BASE_FILES),
    "every citation tool path must be a stand-in file in the fixture base",
)


def git(repo: pathlib.Path, *arguments: str) -> bytes:
    return subprocess.check_output(["git", "-C", str(repo), *arguments], stderr=subprocess.PIPE)


def run_classifier(program: pathlib.Path, repo: pathlib.Path, output_directory: pathlib.Path) -> dict[str, bool]:
    """Run a classifier program in `repo`; return the GITHUB_OUTPUT key=value map it wrote."""
    handle, output_name = tempfile.mkstemp(prefix="github-output-", dir=output_directory)
    os.close(handle)
    output_file = pathlib.Path(output_name)
    environment = {**os.environ, "GITHUB_OUTPUT": str(output_file)}
    result = subprocess.run(
        [sys.executable, str(program)],
        cwd=str(repo),
        env=environment,
        capture_output=True,
        text=True,
    )
    require(
        result.returncode == 0,
        f"the classifier exited {result.returncode} in {repo.name}:\n{result.stdout}{result.stderr}",
    )
    outputs: dict[str, bool] = {}
    for line in output_file.read_text(encoding="utf-8").splitlines():
        key, separator, value = line.partition("=")
        require(
            bool(separator) and re.fullmatch(r"[a-z][a-z0-9-]*", key) is not None,
            f"unparsable GITHUB_OUTPUT line {line!r} from {repo.name}",
        )
        require(key not in outputs, f"the classifier wrote {key!r} twice in {repo.name}")
        require(value in ("true", "false"), f"the classifier wrote a non-boolean {key}={value!r} in {repo.name}")
        outputs[key] = value == "true"
    require(
        set(outputs) == set(OUTPUT_KEYS),
        f"the classifier must emit exactly {sorted(OUTPUT_KEYS)}; emitted {sorted(outputs)} in {repo.name}",
    )
    return outputs


def triple_of(outputs: dict[str, bool]) -> tuple[bool, bool, bool]:
    return tuple(outputs[key] for key in OUTPUT_KEYS)


def check_fixture_triple(outputs: dict[str, bool], fixture: Fixture) -> None:
    """The comparison every pinned fixture (and every program mutation) must satisfy."""
    actual = triple_of(outputs)
    require(
        actual == fixture.expected,
        f"fixture {fixture.name} ({fixture.intent}): expected {fixture.expected}, got {actual}",
    )


def build_fixture_repository(root: pathlib.Path, fixture: Fixture, program: pathlib.Path) -> dict[str, bool]:
    """One fixture's repository: exactly two commits, then the classifier run on its diff."""
    repo = root / fixture.name
    repo.mkdir()
    git(repo, "init", "--quiet")
    git(repo, "config", "user.name", "Fixture")
    git(repo, "config", "user.email", "fixture@example.invalid")
    git(repo, "config", "commit.gpgsign", "false")
    for relative, content in BASE_FILES.items():
        write_fixture_file(repo, relative, content)
    git(repo, "add", "-A")
    git(repo, "commit", "--quiet", "-m", "fixture base")
    fixture.action(repo)
    git(repo, "add", "-A")
    git(repo, "commit", "--quiet", "-m", f"fixture diff: {fixture.intent}")
    commits = git(repo, "rev-list", "--count", "HEAD").strip()
    require(commits == b"2", f"fixture {fixture.name} must be a two-commit repository; found {commits!r} commits")
    changed = tuple(
        part
        for part in git(repo, "diff", "--name-only", "--no-renames", "-z", "HEAD^1", "HEAD").decode("utf-8").split("\0")
        if part
    )
    require(
        sorted(changed) == sorted(fixture.paths),
        f"fixture {fixture.name} changed {sorted(changed)}, expected {sorted(fixture.paths)}",
    )
    return run_classifier(program, repo, root)


class ChangedPathClassifierTests(unittest.TestCase):
    """Each expectation below is a measured run of the workflow's own classifier program."""

    maxDiff = None

    @classmethod
    def setUpClass(cls) -> None:
        cls.temporary = tempfile.TemporaryDirectory(prefix="ci-changed-path-classifier-")
        cls.addClassCleanup(cls.temporary.cleanup)
        cls.root = pathlib.Path(cls.temporary.name)
        cls.workflow_text = WORKFLOW.read_text(encoding="utf-8")
        cls.program_text = CLASSIFIER.read_text(encoding="utf-8")
        cls.model = parse_model(cls.workflow_text)
        cls.audited = load_audited_paths()
        # A mutated copy runs from this directory (its own `sys.path[0]`), so the shared corpus
        # module must be beside it or every mutation fails on the import, not on its clause.
        (cls.root / CORPUS_MODULE.name).write_text(
            CORPUS_MODULE.read_text(encoding="utf-8"), encoding="utf-8", newline="\n"
        )
        cls.outputs: dict[str, dict[str, bool]] = {}
        for fixture in FIXTURES:
            cls.outputs[fixture.name] = build_fixture_repository(cls.root, fixture, CLASSIFIER)
            measured = cls.outputs[fixture.name]
            print(
                f"fixture {fixture.name}: " + " ".join(f"{key}={str(measured[key]).lower()}" for key in OUTPUT_KEYS),
                flush=True,
            )

    def run_program(self, program_text: str, fixture: Fixture) -> dict[str, bool]:
        """Run a mutated copy of the classifier text against one fixture's repository."""
        program_file = self.root / f"mutated-{fixture.name}.py"
        program_file.write_text(program_text, encoding="utf-8", newline="\n")
        return run_classifier(program_file, self.root / fixture.name, self.root)

    # -- the classifier program, executed on fixture diffs --------------------

    def test_every_fixture_diff_reports_its_pinned_classifier_triple(self) -> None:
        for fixture in FIXTURES:
            with self.subTest(fixture=fixture.name):
                check_fixture_triple(self.outputs[fixture.name], fixture)

    def test_every_audited_document_gates_only_run_citations(self) -> None:
        literals = predicate_literals(self.program_text)
        self.assertEqual(
            set(self.audited),
            set(AUDITED_FIXTURES),
            "the citation gate's audited corpus changed; teach this suite the new entries",
        )
        for entry in self.audited:
            with self.subTest(audited=entry):
                for output in ("run-heavy", "run-records"):
                    self.assertFalse(
                        any(covers(literal, entry) for literal in literals[output]),
                        f"{entry} is audited but the {output} predicate mentions it",
                    )
                fixture = FIXTURES_BY_NAME[AUDITED_FIXTURES[entry]]
                self.assertTrue(
                    any(covers(path, entry) for path in fixture.paths), f"fixture {fixture.name} does not touch {entry}"
                )
                check_fixture_triple(self.outputs[fixture.name], fixture)
        for literal in path_literals(literals["run-citations"]):
            with self.subTest(literal=literal):
                self.assertTrue(
                    any(covers(literal, entry) for entry in (*self.audited, *CITATION_TOOL_FIXTURES)),
                    f"the run-citations predicate mentions {literal}, which no audited path "
                    "or citation tool path explains",
                )

    def test_the_audited_corpus_has_one_definition_consumed_by_both_users(self) -> None:
        check_corpus_single_definition()

    def test_the_run_citations_predicate_consumes_the_shared_corpus(self) -> None:
        check_run_citations_consumes_corpus(self.program_text, predicate_literals(self.program_text)["run-citations"])

    def test_every_directory_corpus_entry_covers_a_new_path(self) -> None:
        check_family_fixture_coverage(self.audited)

    def test_every_citation_tool_path_gates_only_run_citations(self) -> None:
        literals = predicate_literals(self.program_text)
        for tool, fixture_name in CITATION_TOOL_FIXTURES.items():
            with self.subTest(citation_tool=tool):
                self.assertTrue(
                    any(covers(literal, tool) for literal in literals["run-citations"]),
                    f"{tool} is a citation tool path but run-citations never mentions it",
                )
                self.assertFalse(
                    any(covers(literal, tool) for literal in literals["run-heavy"]),
                    f"{tool} is a citation tool path but run-heavy mentions it",
                )
                fixture = FIXTURES_BY_NAME[fixture_name]
                self.assertIn(tool, fixture.paths)
                check_fixture_triple(self.outputs[fixture_name], fixture)

    # -- the workflow's outputs and the routing they gate ---------------------

    def test_the_changes_job_folds_the_three_outputs_outside_pull_requests(self) -> None:
        check_output_folds(self.model)
        empty = dict.fromkeys(OUTPUT_KEYS, False)
        self.assertEqual(effective_outputs(self.model, "push", empty), dict.fromkeys(OUTPUT_KEYS, True))
        self.assertEqual(effective_outputs(self.model, "pull_request", empty), empty)
        partial = {"run-heavy": True, "run-records": False, "run-citations": True}
        self.assertEqual(effective_outputs(self.model, "pull_request", partial), partial)
        self.assertEqual(effective_outputs(self.model, "workflow_dispatch", partial), dict.fromkeys(OUTPUT_KEYS, True))

    def test_every_gated_job_gates_on_its_pinned_classifier_output(self) -> None:
        check_job_gating(self.model)

    def test_the_classifier_step_runs_the_program_only_on_pull_requests(self) -> None:
        check_classifier_step(classifier_step_keys(self.workflow_text), job_blocks(self.workflow_text)["changes"])

    def test_pull_request_runs_exactly_the_jobs_its_outputs_gate(self) -> None:
        check_output_folds(self.model)
        check_job_gating(self.model)
        for fixture in FIXTURES:
            with self.subTest(fixture=fixture.name):
                outputs = effective_outputs(self.model, "pull_request", self.outputs[fixture.name])
                self.assertEqual(
                    ran_jobs(self.model, "pull_request", self.outputs[fixture.name]),
                    pinned_ran_jobs(self.model.jobs, outputs),
                )

    def test_non_pull_request_events_run_every_job_except_the_classifier(self) -> None:
        check_output_folds(self.model)
        check_job_gating(self.model)
        everything = {name for name in self.model.jobs if name != "changes"}
        for event in ("push", "workflow_dispatch"):
            for fixture in FIXTURES:
                with self.subTest(event=event, fixture=fixture.name):
                    self.assertEqual(ran_jobs(self.model, event, self.outputs[fixture.name]), everything)

    def test_the_coverage_jobs_follow_the_heavy_matrix_across_every_fixture(self) -> None:
        check_coverage_plane_routing(self.model, self.outputs)
        check_coverage_jobs_are_advisory(self.workflow_text)
        check_the_control_coverage_floor_is_blocking(self.workflow_text)
        check_coverage_artifacts(self.workflow_text)

    # -- mutations: the assertions must fail when a routing is changed --------

    def test_mutation_dropping_the_evaluations_clause_breaks_the_records_fixture(self) -> None:
        fixture = FIXTURES_BY_NAME["evaluations-record-added"]
        mutated = self.program_text.replace(
            'path.startswith(b"docs/evaluations/")', 'path.startswith(b"docs/no-longer-evaluations/")'
        )
        self.assertNotEqual(mutated, self.program_text, "the mutation must edit the extracted program")
        outputs = self.run_program(mutated, fixture)
        self.assertEqual(
            triple_of(outputs),
            (False, False, False),
            "dropping the clause must flip run-records for the evaluations fixture",
        )
        with self.assertRaises(AssertionError):
            check_fixture_triple(outputs, fixture)

    def test_mutation_making_the_heavy_predicate_ignore_documentation_breaks_the_docs_fixture(self) -> None:
        fixture = FIXTURES_BY_NAME["docs-runbooks-modified"]
        mutated = self.program_text.replace('path.startswith(b"docs/") or path.endswith(b".md")', "False")
        self.assertNotEqual(mutated, self.program_text, "the mutation must edit the extracted program")
        outputs = self.run_program(mutated, fixture)
        self.assertEqual(
            triple_of(outputs),
            (True, False, True),
            "ignoring docs/ and *.md must flip run-heavy for the docs-only fixture",
        )
        with self.assertRaises(AssertionError):
            check_fixture_triple(outputs, fixture)

    def test_mutation_regating_citation_existence_on_run_heavy_breaks_its_routing(self) -> None:
        mutated = self.workflow_text.replace(
            "needs.changes.outputs.run-citations == 'true'", "needs.changes.outputs.run-heavy == 'true'"
        )
        self.assertNotEqual(mutated, self.workflow_text, "the mutation must edit the workflow text")
        model = parse_model(mutated)
        self.assertEqual(model.jobs["citation-existence"].condition, "needs.changes.outputs.run-heavy == 'true'")
        with self.assertRaises(AssertionError):
            check_job_gating(model)

    def test_mutation_regating_a_coverage_job_off_the_heavy_output_breaks_its_routing(self) -> None:
        self.assertIn(
            COVERAGE_DAEMON_HEADER,
            self.workflow_text,
            "the mutation's anchor must be the coverage job's own gated header",
        )
        mutated = self.workflow_text.replace(
            COVERAGE_DAEMON_HEADER,
            COVERAGE_DAEMON_HEADER.replace(
                "needs.changes.outputs.run-heavy == 'true'",
                "needs.changes.outputs.run-citations == 'true'",
            ),
        )
        self.assertNotEqual(mutated, self.workflow_text, "the mutation must edit the workflow text")
        model = parse_model(mutated)
        self.assertEqual(model.jobs["coverage-daemon"].condition, "needs.changes.outputs.run-citations == 'true'")
        with self.assertRaises(AssertionError):
            check_coverage_plane_routing(model, self.outputs)

    def test_mutation_dropping_a_coverage_jobs_advisory_flag_breaks_the_advisory_check(self) -> None:
        advisory_line = "    continue-on-error: true\n"
        self.assertIn(
            COVERAGE_DAEMON_HEADER + advisory_line,
            self.workflow_text,
            "the mutation's anchor must be the coverage job's gated, advisory header",
        )
        mutated = self.workflow_text.replace(COVERAGE_DAEMON_HEADER + advisory_line, COVERAGE_DAEMON_HEADER)
        self.assertNotEqual(mutated, self.workflow_text, "the mutation must edit the workflow text")
        with self.assertRaises(AssertionError):
            check_coverage_jobs_are_advisory(mutated)

    def test_mutation_restoring_the_control_coverage_jobs_advisory_flag_breaks_the_blocking_check(self) -> None:
        advisory_line = "    continue-on-error: true\n"
        self.assertIn(
            COVERAGE_CONTROL_HEADER,
            self.workflow_text,
            "the mutation's anchor must be the control coverage job's gated header",
        )
        self.assertNotIn(
            COVERAGE_CONTROL_HEADER + advisory_line,
            self.workflow_text,
            "the control coverage job's floor is blocking; the mutation must restore the flag",
        )
        mutated = self.workflow_text.replace(COVERAGE_CONTROL_HEADER, COVERAGE_CONTROL_HEADER + advisory_line)
        self.assertNotEqual(mutated, self.workflow_text, "the mutation must edit the workflow text")
        with self.assertRaises(AssertionError):
            check_the_control_coverage_floor_is_blocking(mutated)

    def test_mutation_dropping_a_coverage_uploads_always_flag_breaks_the_artifact_check(self) -> None:
        upload = COVERAGE_UPLOADS["coverage-daemon"]
        self.assertIn(upload, self.workflow_text, "the mutation's anchor must be a coverage upload step")
        mutated = self.workflow_text.replace(upload, upload.replace("        if: always()\n", ""))
        self.assertNotEqual(mutated, self.workflow_text, "the mutation must edit the workflow text")
        with self.assertRaises(AssertionError):
            check_coverage_artifacts(mutated)

    def test_mutation_dropping_a_coverage_jobs_working_directory_breaks_the_artifact_check(self) -> None:
        defaults = COVERAGE_DEFAULTS["coverage-daemon"]
        self.assertIn(defaults, self.workflow_text, "the mutation's anchor must be a coverage job's defaults block")
        mutated = self.workflow_text.replace(defaults, "")
        self.assertNotEqual(mutated, self.workflow_text, "the mutation must edit the workflow text")
        with self.assertRaises(AssertionError):
            check_coverage_artifacts(mutated)

    def test_mutation_conditioning_legacy_route_audit_breaks_its_unconditional_assertion(self) -> None:
        mutated = self.workflow_text.replace(
            "  legacy-route-audit:\n",
            "  legacy-route-audit:\n    if: needs.changes.outputs.run-heavy == 'true'\n",
        )
        self.assertNotEqual(mutated, self.workflow_text, "the mutation must edit the workflow text")
        model = parse_model(mutated)
        self.assertEqual(model.jobs["legacy-route-audit"].condition, "needs.changes.outputs.run-heavy == 'true'")
        with self.assertRaises(AssertionError):
            check_job_gating(model)

    def test_mutation_dropping_an_output_fold_breaks_the_fold_assertion(self) -> None:
        dropped = (
            "      run-records: ${{ github.event_name != 'pull_request' "
            "|| steps.paths.outputs.run-records == 'true' }}\n"
        )
        mutated = self.workflow_text.replace(dropped, "")
        self.assertNotEqual(mutated, self.workflow_text, "the mutation must edit the workflow text")
        model = parse_model(mutated)
        self.assertEqual(set(model.folds), {"run-heavy", "run-citations"})
        with self.assertRaises(AssertionError):
            check_output_folds(model)

    def test_mutation_dropping_the_records_revision_scope_clause_breaks_its_fixture(self) -> None:
        fixture = FIXTURES_BY_NAME["revision-scope-modified"]
        mutated = self.program_text.replace(
            'or path == b"scripts/ci/check-record-cleanliness.py"\n    or path == b"scripts/ci/revision_scope.py"',
            'or path == b"scripts/ci/check-record-cleanliness.py"',
        )
        self.assertNotEqual(mutated, self.program_text, "the mutation must edit the extracted program")
        outputs = self.run_program(mutated, fixture)
        self.assertEqual(
            triple_of(outputs),
            (True, False, True),
            "dropping the records clause must flip run-records for the shared module",
        )
        with self.assertRaises(AssertionError):
            check_fixture_triple(outputs, fixture)

    def test_mutation_dropping_the_citations_revision_scope_clause_breaks_its_fixture(self) -> None:
        fixture = FIXTURES_BY_NAME["revision-scope-modified"]
        mutated = self.program_text.replace(
            'or path == b"scripts/ci/test-check-citation-existence.py"\n    or path == b"scripts/ci/revision_scope.py"',
            'or path == b"scripts/ci/test-check-citation-existence.py"',
        )
        self.assertNotEqual(mutated, self.program_text, "the mutation must edit the extracted program")
        outputs = self.run_program(mutated, fixture)
        self.assertEqual(
            triple_of(outputs),
            (True, True, False),
            "dropping the citations clause must flip run-citations for the shared module",
        )
        with self.assertRaises(AssertionError):
            check_fixture_triple(outputs, fixture)

    def test_mutation_making_a_non_markdown_docs_path_heavy_breaks_the_docs_json_fixture(self) -> None:
        fixture = FIXTURES_BY_NAME["docs-json-modified"]
        mutated = self.program_text.replace(
            'path.startswith(b"docs/") or path.endswith(b".md")',
            '(path.startswith(b"docs/") and not path.endswith(b".json")) or path.endswith(b".md")',
        )
        self.assertNotEqual(mutated, self.program_text, "the mutation must edit the extracted program")
        outputs = self.run_program(mutated, fixture)
        self.assertEqual(
            triple_of(outputs),
            (True, False, False),
            "excusing only the .md arm for docs/ must flip run-heavy for the non-Markdown docs-json fixture",
        )
        with self.assertRaises(AssertionError):
            check_fixture_triple(outputs, fixture)

    def test_mutation_making_the_corpus_family_exact_match_only_breaks_the_added_runbook_fixture(self) -> None:
        fixture = FIXTURES_BY_NAME["docs-runbooks-added"]
        mutated = self.program_text.replace(
            'path == entry.encode() or path.startswith(entry.encode() + b"/")',
            "path == entry.encode()",
        )
        self.assertNotEqual(mutated, self.program_text, "the mutation must edit the extracted program")
        outputs = self.run_program(mutated, fixture)
        self.assertEqual(
            triple_of(outputs),
            (False, False, False),
            "an exact-match-only family must flip run-citations for a new path under a directory entry",
        )
        with self.assertRaises(AssertionError):
            check_fixture_triple(outputs, fixture)

    def test_mutation_dropping_the_corpus_modules_citations_clause_breaks_its_fixture(self) -> None:
        fixture = FIXTURES_BY_NAME["audited-paths-modified"]
        mutated = self.program_text.replace('or path == b"scripts/ci/audited_paths.py"\n', "")
        self.assertNotEqual(mutated, self.program_text, "the mutation must edit the extracted program")
        outputs = self.run_program(mutated, fixture)
        self.assertEqual(
            triple_of(outputs),
            (True, False, False),
            "dropping the corpus module's clause must flip run-citations for its own fixture",
        )
        with self.assertRaises(AssertionError):
            check_fixture_triple(outputs, fixture)

    def test_mutation_inlining_the_corpus_literal_breaks_the_consumption_check(self) -> None:
        fixture = FIXTURES_BY_NAME["docs-runbooks-added"]
        copies = ", ".join(f'"{entry}"' for entry in self.audited)
        mutated = self.program_text.replace("from audited_paths import AUDITED_PATHS\n", "").replace(
            "for entry in AUDITED_PATHS", f"for entry in ({copies})"
        )
        self.assertNotEqual(mutated, self.program_text, "the mutation must edit the extracted program")
        # The inlined copy routes the fixture exactly as the import does, so the consumption
        # check is the pin, not a fixture triple.
        check_fixture_triple(self.run_program(mutated, fixture), fixture)
        with self.assertRaises(AssertionError):
            check_run_citations_consumes_corpus(mutated, predicate_literals(mutated)["run-citations"])

    def test_mutation_running_the_classifier_step_off_pull_requests_breaks_the_step_check(self) -> None:
        mutated = self.workflow_text.replace(
            "        id: paths\n        if: github.event_name == 'pull_request'",
            "        id: paths\n        if: github.event_name == 'push'",
        )
        self.assertNotEqual(mutated, self.workflow_text, "the mutation must edit the workflow text")
        self.assertEqual(classifier_step_keys(mutated)["if"], "github.event_name == 'push'")
        with self.assertRaises(AssertionError):
            check_classifier_step(classifier_step_keys(mutated), job_blocks(mutated)["changes"])

    def test_mutation_returning_the_classifier_to_a_heredoc_breaks_the_step_anchor(self) -> None:
        mutated = self.workflow_text.replace(
            f"        {CLASSIFIER_INVOCATION}",
            "        run: |\n          python3 - <<'PY'\n          pass\n          PY",
        )
        self.assertNotEqual(mutated, self.workflow_text, "the mutation must edit the workflow text")
        with self.assertRaises(AssertionError):
            classifier_step_keys(mutated)

    def test_mutation_reintroducing_a_heredoc_step_in_the_changes_job_breaks_the_step_check(self) -> None:
        heredoc_step = "      - run: |\n          python3 - <<'PY'\n          pass\n          PY\n"
        mutated = self.workflow_text.replace(
            "      - name: Classify changed paths\n",
            heredoc_step + "      - name: Classify changed paths\n",
        )
        self.assertNotEqual(mutated, self.workflow_text, "the mutation must edit the workflow text")
        self.assertEqual(classifier_step_keys(mutated)["id"], "paths")
        with self.assertRaises(AssertionError):
            check_classifier_step(classifier_step_keys(mutated), job_blocks(mutated)["changes"])

    def test_a_heredoc_in_an_unrelated_job_stays_green(self) -> None:
        heredoc_step = "      - run: |\n          python3 - <<'PY'\n          pass\n          PY\n"
        mutated = self.workflow_text.replace(
            "      - run: python3 scripts/ci/test-no-legacy-routes-audit.py\n",
            "      - run: python3 scripts/ci/test-no-legacy-routes-audit.py\n" + heredoc_step,
        )
        self.assertNotEqual(mutated, self.workflow_text, "the mutation must edit the workflow text")
        self.assertIn("python3 - <<'PY'", mutated)
        check_classifier_step(classifier_step_keys(mutated), job_blocks(mutated)["changes"])


if __name__ == "__main__":
    sys.exit(unittest.main())
