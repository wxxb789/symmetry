#!/usr/bin/env python3
"""Pin the writer/fixture agreement on which owner-witness cases route to a Goal admission.

Both sides are read from the committed bytes, statically; a shape the parser cannot read is
refused, never guessed around. See docs/plans/2026-11-26-rf-ci-owner-request-key-check-plan.md §2.
"""

from __future__ import annotations

import ast
import pathlib
import re
import unittest

DIRECTORY = pathlib.Path(__file__).resolve().parent
WRITER = DIRECTORY / "write-linux-v2-owner-request.py"
FIXTURE = DIRECTORY / "provision-linux-v2-control-claims.exs"

CASES_OPENER = re.compile(r"^  @cases \[$")
REGION_END = re.compile(r"^  def ")
ROW_START = re.compile(
    r'^\s*\{"(?P<name>[^"{}]*)(?:#\{(?P<variable>[a-z_]+)\})?", "[^"]+", :[a-z_]+, :(?P<role>[a-z_]+),$'
)
ROW_DESCRIPTION = re.compile(r'^\s*"[^"]*"\},?$')
ENUM_MAP = re.compile(r"^\s*Enum\.map\(@([a-z_]+), fn ([a-z_]+) ->$")
ROW_STRUCTURE = re.compile(r"^\s*(?:\] \+\+|end\) \+\+|\[|\])$")
GOAL_ROUTE = re.compile(
    r"Enum\.split_with\(@cases, fn \{[^}]*repository_role[^}]*\} ->\s*"
    r"repository_role not in \[:([a-z_]+), :([a-z_]+)\]\s*end\)"
)
REQUEST_CASE_RULE = re.compile(
    r"cond do\s*case_name in\s*\[(?P<fixed>(?:\s*\"[^\"]+\",?)+)\s*\]\s*->\s*case_name\s*"
    r'String\.starts_with\?\(case_name,\s*"(?P<prefix>[^"]+)"\)\s*->\s*"(?P<collapsed>[^"]+)"\s*'
    r"true\s*->\s*case_name\s*end"
)
STRING_ATTRIBUTE_END = re.compile(r"^  \]$", re.MULTILINE)
STRING_ATTRIBUTE_VALUES = re.compile(r'^\s*"([^"]*)",?$', re.MULTILINE)


class Refused(Exception):
    """The committed bytes do not carry the pinned shape; the check never guesses around it."""


def parse_string_attribute(text: str, name: str) -> list[str]:
    opener = re.compile(rf"^  @{re.escape(name)} \[$", re.MULTILINE)
    start = opener.search(text)
    if start is None:
        raise Refused(f"{FIXTURE.name}: no `@{name} [` string list for the interpolated row")
    body = text[start.end() :]
    end = STRING_ATTRIBUTE_END.search(body)
    if end is None:
        raise Refused(f"{FIXTURE.name}: `@{name} [` has no `  ]` terminator")
    values = STRING_ATTRIBUTE_VALUES.findall(body[: end.start()])
    if not values:
        raise Refused(f"{FIXTURE.name}: `@{name} [` holds no string values")
    return values


def fixture_rows(text: str) -> list[tuple[str, str]]:
    lines = text.splitlines()
    start = next((index for index, line in enumerate(lines) if CASES_OPENER.match(line)), None)
    if start is None:
        raise Refused(f"{FIXTURE.name}: no `@cases [` table opener")
    end = next((index for index in range(start + 1, len(lines)) if REGION_END.match(lines[index])), None)
    if end is None:
        raise Refused(f"{FIXTURE.name}: the `@cases` table has no following top-level `def` line")
    rows: list[tuple[str, str]] = []
    seen: set[str] = set()
    interpolations: dict[str, str] = {}
    index = start + 1
    while index < end:
        line = lines[index]
        mapping = ENUM_MAP.match(line)
        if mapping is not None:
            if mapping.group(2) in interpolations:
                raise Refused(f"{FIXTURE.name}: `{mapping.group(2)}` is bound by more than one `Enum.map`")
            interpolations[mapping.group(2)] = mapping.group(1)
            index += 1
            continue
        if not line.strip() or ROW_STRUCTURE.match(line):
            index += 1
            continue
        opener = ROW_START.match(line)
        if opener is None:
            raise Refused(f"{FIXTURE.name}: unparseable `@cases` line {line!r}")
        variable = opener.group("variable")
        names = [opener.group("name")]
        if variable is not None:
            attribute = interpolations.get(variable)
            if attribute is None:
                raise Refused(f"{FIXTURE.name}: the `#{variable}` row has no `Enum.map(@…, fn {variable} ->`")
            names = [opener.group("name") + value for value in parse_string_attribute(text, attribute)]
        while True:
            if line.rstrip().endswith(("}", "},")):
                index += 1
                break
            if index >= end:
                raise Refused(f"{FIXTURE.name}: the `{names[0]}` row has no closing `}}`")
            index += 1
            line = lines[index]
            if ROW_DESCRIPTION.match(line) is None:
                raise Refused(f"{FIXTURE.name}: unparseable continuation of the `{names[0]}` row: {line!r}")
        role = opener.group("role")
        for name in names:
            if name in seen:
                raise Refused(f"{FIXTURE.name}: duplicate `@cases` name {name!r}")
            seen.add(name)
            rows.append((name, role))
    if not rows:
        raise Refused(f"{FIXTURE.name}: the `@cases` table holds no rows")
    return rows


def fixture_goal_roles(text: str) -> set[str]:
    match = GOAL_ROUTE.search(text)
    if match is None:
        raise Refused(
            f"{FIXTURE.name}: no `Enum.split_with(@cases, … repository_role not in [:role, :role] end)` "
            "goal-route predicate"
        )
    return {match.group(1), match.group(2)}


def fixture_claim_case_rule(text: str) -> tuple[frozenset[str], str, str]:
    match = REQUEST_CASE_RULE.search(text)
    if match is None:
        raise Refused(f"{FIXTURE.name}: no `request_case = cond …` rule of the pinned shape")
    return frozenset(re.findall(r'"([^"]+)"', match.group("fixed"))), match.group("prefix"), match.group("collapsed")


def writer_goal_cases(text: str) -> set[str]:
    try:
        module = ast.parse(text)
    except SyntaxError as error:
        raise Refused(f"{WRITER.name}: not valid Python ({error.msg})") from error
    assignments = {
        statement.targets[0].id: statement.value
        for statement in module.body
        if isinstance(statement, ast.Assign)
        and len(statement.targets) == 1
        and isinstance(statement.targets[0], ast.Name)
    }
    cases = assignments.get("CASES")
    if not isinstance(cases, ast.Dict):
        raise Refused(f"{WRITER.name}: no module-level `CASES` dict assignment")
    case_names: set[str] = set()
    for key in cases.keys:
        if not (isinstance(key, ast.Constant) and isinstance(key.value, str)):
            raise Refused(f"{WRITER.name}: `CASES` carries a non-string key (line {key.lineno})")
        case_names.add(key.value)
    raw = assignments.get("GOAL_ADMISSION_CASES")
    if raw is None:
        raise Refused(f"{WRITER.name}: no module-level `GOAL_ADMISSION_CASES` assignment")
    try:
        goals = ast.literal_eval(raw)
    except (ValueError, TypeError) as error:
        raise Refused(f"{WRITER.name}: `GOAL_ADMISSION_CASES` is not a literal set of strings ({error})") from error
    if not isinstance(goals, set) or not goals or not all(isinstance(name, str) for name in goals):
        raise Refused(f"{WRITER.name}: `GOAL_ADMISSION_CASES` is not a non-empty literal set of strings")
    unknown = goals - case_names
    if unknown:
        raise Refused(f"{WRITER.name}: GOAL_ADMISSION_CASES names no `CASES` case: {sorted(unknown)}")
    return goals


def check_agreement(writer_text: str, fixture_text: str) -> tuple[set[str], set[str]]:
    writer_goals = writer_goal_cases(writer_text)
    rows = fixture_rows(fixture_text)
    goal_roles = fixture_goal_roles(fixture_text)
    fixed, prefix, collapsed = fixture_claim_case_rule(fixture_text)

    def dispatched_as(name: str) -> str:
        if name in fixed:
            return name
        return collapsed if name.startswith(prefix) else name

    fixture_goals = {dispatched_as(name) for name, role in rows if role in goal_roles}
    if writer_goals != fixture_goals:
        raise Refused(
            f"{WRITER.name} GOAL_ADMISSION_CASES and {FIXTURE.name}'s goal-routed rows disagree\n"
            f"writer ({len(writer_goals)}): {sorted(writer_goals)}\n"
            f"fixture ({len(fixture_goals)}): {sorted(fixture_goals)}"
        )
    legacy_collisions = sorted({dispatched_as(name) for name, role in rows if role not in goal_roles} & writer_goals)
    if legacy_collisions:
        raise Refused(
            f"{FIXTURE.name}: legacy rows route to a writer case keyed as a goal admission: {legacy_collisions}"
        )
    return writer_goals, fixture_goals


class OwnerRequestKeyAgreement(unittest.TestCase):
    """The committed pair plus one mutation witness per drift direction."""

    writer_text: str
    fixture_text: str

    def setUp(self) -> None:
        self.writer_text = WRITER.read_text(encoding="utf-8")
        self.fixture_text = FIXTURE.read_text(encoding="utf-8")

    def mutate(self, text: str, anchor: str, replacement: str) -> str:
        self.assertIn(anchor, text)
        return text.replace(anchor, replacement, 1)

    def test_the_committed_pair_agrees(self) -> None:
        check_agreement(self.writer_text, self.fixture_text)

    def test_dropping_a_writer_goal_case_is_refused(self) -> None:
        mutated = self.mutate(self.writer_text, '    "codex_containment",\n', "")
        with self.assertRaises(Refused) as refused:
            check_agreement(mutated, self.fixture_text)
        self.assertIn("codex_containment", str(refused.exception))

    def test_a_fixture_only_goal_case_is_refused(self) -> None:
        probe = (
            '    {"goal_case_drift_probe", "pi-native", :pi, :pi_durability,\n'
            '     "Drift probe row that must be refused. Do not publish."}\n'
        )
        mutated = self.mutate(self.fixture_text, "  ] ++\n", probe + "  ] ++\n")
        with self.assertRaises(Refused) as refused:
            check_agreement(self.writer_text, mutated)
        self.assertIn("goal_case_drift_probe", str(refused.exception))

    def test_a_legacy_row_routed_into_a_goal_case_is_refused(self) -> None:
        # The unprovisioned refusal is the legacy row that survives the prefix rule only through
        # the fixed-name list; losing that exemption dispatches it under the goal-keyed
        # `stage_replay` case.
        mutated = self.mutate(self.fixture_text, '          "stage_replay_unprovisioned_refusal"\n', "")
        with self.assertRaises(Refused) as refused:
            check_agreement(self.writer_text, mutated)
        self.assertIn("legacy rows route to a writer case keyed as a goal admission", str(refused.exception))

    def test_unparseable_shapes_are_refused_with_their_location(self) -> None:
        mutations = (
            (
                "fixture",
                "repository_role not in [:pi_durability, :opencode_goal]",
                "repository_role not in goal_roles",
            ),
            (
                "fixture",
                'String.starts_with?(case_name, "stage_replay_") -> "stage_replay"',
                "String.starts_with?(case_name, stage_prefix) -> stage_case",
            ),
            (
                "fixture",
                '"claude_containment", "claude-native", :claude_code, :opencode_goal,',
                '"claude_containment", "claude-native", :claude_code, goal_role,',
            ),
            ("writer", "GOAL_ADMISSION_CASES = {", "GOAL_ADMISSION_CASES = {*CASES,"),
            ("fixture", "  ] ++\n", "  ] ++\n  @cases [\n"),
        )
        for side, anchor, replacement in mutations:
            with self.subTest(anchor=anchor):
                writer_text = self.writer_text
                fixture_text = self.fixture_text
                if side == "writer":
                    writer_text = self.mutate(self.writer_text, anchor, replacement)
                else:
                    fixture_text = self.mutate(self.fixture_text, anchor, replacement)
                with self.assertRaises(Refused) as refused:
                    check_agreement(writer_text, fixture_text)
                self.assertIn(WRITER.name if side == "writer" else FIXTURE.name, str(refused.exception))


if __name__ == "__main__":
    unittest.main(verbosity=2)
