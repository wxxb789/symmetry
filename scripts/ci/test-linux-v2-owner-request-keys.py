#!/usr/bin/env python3
"""Pin the writer/fixture agreement on the goal-route and provider-scope case sets.

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
    r"Enum\.split_with\(@cases, fn \{(?P<head>[^}]*)\} ->\s*"
    r"repository_role not in \[(?P<roles>:[a-z_]+(?:, :[a-z_]+)*)\]\s*end\)"
)
PROVIDER_SYNC = re.compile(r'"provider_sync" => repository_role == :(?P<role>[a-z_]+)')
PROVIDER_ACCESS_RULE = re.compile(
    r'PROVIDER_ACCESS_CASES = \{name for name, \(_, profile, _\) in CASES\.items\(\) if profile == "(?P<profile>[^"]+)"\}'
)
REQUEST_CASE_RULE = re.compile(
    r"cond do\s*case_name in\s*\[(?P<fixed>(?:\s*\"[^\"]+\",?)+)\s*\]\s*->\s*case_name\s*"
    r'String\.starts_with\?\(case_name,\s*"(?P<prefix>[^"]+)"\)\s*->\s*"(?P<collapsed>[^"]+)"\s*'
    r"true\s*->\s*case_name\s*end"
)
STRING_ATTRIBUTE_END = re.compile(r"^  \]$", re.MULTILINE)
STRING_ATTRIBUTE_VALUES = re.compile(r'^\s*"([^"]*)",?$', re.MULTILINE)


class Refused(Exception):
    pass


def parse_string_attribute(text: str, name: str) -> list[str]:
    opener = re.compile(rf"^[ \t]*@{re.escape(name)} \[$", re.MULTILINE)
    declarations = list(opener.finditer(text))
    if not declarations:
        raise Refused(f"{FIXTURE.name}: no `@{name} [` string list for the interpolated row")
    if len(declarations) > 1:
        raise Refused(f"{FIXTURE.name}: `@{name} [` is declared {len(declarations)} times, not once")
    body = text[declarations[0].end() :]
    end = STRING_ATTRIBUTE_END.search(body)
    if end is None:
        raise Refused(f"{FIXTURE.name}: `@{name} [` has no `  ]` terminator")
    values: list[str] = []
    for line in body[: end.start()].splitlines():
        stripped = line.strip()
        if not stripped or stripped.startswith("#"):
            continue
        element = STRING_ATTRIBUTE_VALUES.match(line)
        if element is None:
            raise Refused(f"{FIXTURE.name}: unparseable `@{name} [` element line {line!r}")
        if "#{" in element.group(1):
            raise Refused(f"{FIXTURE.name}: interpolated `@{name} [` element line {line!r}")
        values.append(element.group(1))
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
            f"{FIXTURE.name}: no `Enum.split_with(@cases, … repository_role not in [:role, …] end)` "
            "goal-route predicate"
        )
    bindings = [binding.strip() for binding in match.group("head").split(",")]
    if len(bindings) != 5 or bindings[3] != "repository_role":
        raise Refused(
            f"{FIXTURE.name}: the goal-route predicate must bind repository_role as the 4th of 5 "
            f"tuple elements (the `@cases` role column), got {match.group('head')!r}"
        )
    return set(re.findall(r":([a-z_]+)", match.group("roles")))


def fixture_provider_role(text: str) -> str:
    matches = PROVIDER_SYNC.findall(text)
    if len(matches) != 1:
        raise Refused(
            f"{FIXTURE.name}: `provider_sync` must be derived exactly once from the role column, "
            f"got {len(matches)} matches"
        )
    return matches[0]


def fixture_claim_case_rule(text: str) -> tuple[frozenset[str], str, str]:
    match = REQUEST_CASE_RULE.search(text)
    if match is None:
        raise Refused(f"{FIXTURE.name}: no `request_case = cond …` rule of the pinned shape")
    fixed: set[str] = set()
    for element in re.findall(r'"([^"]*)"', match.group("fixed")):
        if "#{" in element:
            raise Refused(f"{FIXTURE.name}: interpolated `case_name in [` element {element!r}")
        fixed.add(element)
    return frozenset(fixed), match.group("prefix"), match.group("collapsed")


def writer_assignments(text: str) -> dict[str, ast.expr]:
    try:
        module = ast.parse(text)
    except SyntaxError as error:
        raise Refused(f"{WRITER.name}: not valid Python ({error.msg})") from error
    return {
        statement.targets[0].id: statement.value
        for statement in module.body
        if isinstance(statement, ast.Assign)
        and len(statement.targets) == 1
        and isinstance(statement.targets[0], ast.Name)
    }


def writer_case_names(cases: ast.Dict) -> set[str]:
    case_names: set[str] = set()
    for key in cases.keys:
        if not (isinstance(key, ast.Constant) and isinstance(key.value, str)):
            raise Refused(f"{WRITER.name}: `CASES` carries a non-string key (line {key.lineno})")
        case_names.add(key.value)
    return case_names


def writer_goal_cases(text: str) -> set[str]:
    assignments = writer_assignments(text)
    cases = assignments.get("CASES")
    if not isinstance(cases, ast.Dict):
        raise Refused(f"{WRITER.name}: no module-level `CASES` dict assignment")
    case_names = writer_case_names(cases)
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


def writer_provider_cases(text: str) -> set[str]:
    assignments = writer_assignments(text)
    cases = assignments.get("CASES")
    if not isinstance(cases, ast.Dict):
        raise Refused(f"{WRITER.name}: no module-level `CASES` dict assignment")
    writer_case_names(cases)
    matches = PROVIDER_ACCESS_RULE.findall(text)
    if len(matches) != 1:
        raise Refused(
            f"{WRITER.name}: `PROVIDER_ACCESS_CASES` must be the pinned profile-rule shape, got "
            f"{len(matches)} matches"
        )
    profile = matches[0]
    provider: set[str] = set()
    for key, value in zip(cases.keys, cases.values):
        if not (
            isinstance(value, ast.Tuple)
            and len(value.elts) == 3
            and all(isinstance(element, ast.Constant) for element in value.elts)
        ):
            raise Refused(f"{WRITER.name}: `CASES[{key.value!r}]` is not a 3-literal row (line {value.lineno})")
        if value.elts[1].value == profile:
            provider.add(key.value)
    if not provider:
        raise Refused(f"{WRITER.name}: no `CASES` row uses the PROVIDER_ACCESS_CASES profile {profile!r}")
    return provider


def check_agreement(writer_text: str, fixture_text: str) -> None:
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
    provider_role = fixture_provider_role(fixture_text)
    fixture_provider = {dispatched_as(name) for name, role in rows if role == provider_role}
    if not fixture_provider <= fixture_goals:
        raise Refused(
            f"{FIXTURE.name}: provider-scoped rows must be goal-routed: "
            f"{sorted(fixture_provider - fixture_goals)}"
        )
    writer_provider = writer_provider_cases(writer_text)
    if not writer_provider <= writer_goals:
        raise Refused(
            f"{WRITER.name}: PROVIDER_ACCESS_CASES rows must be goal-routed: "
            f"{sorted(writer_provider - writer_goals)}"
        )
    if writer_provider != fixture_provider:
        raise Refused(
            f"{WRITER.name} PROVIDER_ACCESS_CASES and {FIXTURE.name}'s provider-scoped rows disagree\n"
            f"writer ({len(writer_provider)}): {sorted(writer_provider)}\n"
            f"fixture ({len(fixture_provider)}): {sorted(fixture_provider)}"
        )
    legacy_collisions = sorted({dispatched_as(name) for name, role in rows if role not in goal_roles} & writer_goals)
    if legacy_collisions:
        raise Refused(
            f"{FIXTURE.name}: legacy rows route to a writer case keyed as a goal admission: {legacy_collisions}"
        )


class OwnerRequestKeyAgreement(unittest.TestCase):
    writer_text: str
    fixture_text: str

    def setUp(self) -> None:
        self.writer_text = WRITER.read_text(encoding="utf-8")
        self.fixture_text = FIXTURE.read_text(encoding="utf-8")

    def mutate(self, text: str, anchor: str, replacement: str) -> str:
        self.assertEqual(1, text.count(anchor), anchor)
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
        # The unprovisioned refusal survives the prefix rule only through the fixed-name list;
        # losing that exemption dispatches it under the goal-keyed `stage_replay` case.
        mutated = self.mutate(self.fixture_text, '          "stage_replay_unprovisioned_refusal"\n', "")
        with self.assertRaises(Refused) as refused:
            check_agreement(self.writer_text, mutated)
        self.assertIn("legacy rows route to a writer case keyed as a goal admission", str(refused.exception))

    def test_a_legacy_pi_provider_row_is_refused(self) -> None:
        mutated = self.mutate(
            self.fixture_text,
            '"pi_provider_action", "pi-native", :pi, :pi_provider,',
            '"pi_provider_action", "pi-native", :pi, :opencode,',
        )
        with self.assertRaises(Refused) as refused:
            check_agreement(self.writer_text, mutated)
        self.assertIn("pi_provider_action", str(refused.exception))

    def test_a_provider_role_drift_is_refused(self) -> None:
        mutated = self.mutate(
            self.fixture_text,
            '"provider_sync" => repository_role == :pi_provider',
            '"provider_sync" => repository_role == :pi_durability',
        )
        with self.assertRaises(Refused) as refused:
            check_agreement(self.writer_text, mutated)
        self.assertIn("provider-scoped rows disagree", str(refused.exception))

    def test_dropping_a_writer_provider_case_is_refused(self) -> None:
        mutated = self.mutate(self.writer_text, '    "helper_sigkill_detached_child",\n', "")
        with self.assertRaises(Refused) as refused:
            check_agreement(mutated, self.fixture_text)
        self.assertIn("helper_sigkill_detached_child", str(refused.exception))

    def test_a_writer_profile_drift_is_refused(self) -> None:
        mutated = self.mutate(
            self.writer_text,
            '"helper_sigkill_detached_child": ("pi", "pi-native", "native_turn_started"),',
            '"helper_sigkill_detached_child": ("pi", "pi-durability", "native_turn_started"),',
        )
        with self.assertRaises(Refused) as refused:
            check_agreement(mutated, self.fixture_text)
        self.assertIn("helper_sigkill_detached_child", str(refused.exception))

    def test_unparseable_shapes_are_refused_with_their_location(self) -> None:
        mutations = (
            (
                "fixture",
                "repository_role not in [:pi_provider, :pi_durability, :opencode_goal]",
                "repository_role not in goal_roles",
                "goal-route predicate",
            ),
            (
                "fixture",
                '"provider_sync" => repository_role == :pi_provider',
                '"provider_sync" => repository_role == provider_role',
                "`provider_sync` must be derived exactly once",
            ),
            (
                "writer",
                'PROVIDER_ACCESS_CASES = {name for name, (_, profile, _) in CASES.items() if profile == "pi-native"}',
                "PROVIDER_ACCESS_CASES = {name for name in CASES}",
                "`PROVIDER_ACCESS_CASES` must be the pinned profile-rule shape",
            ),
            (
                "writer",
                '"pi_provider_action": ("pi", "pi-native", "native_turn_started"),',
                '"pi_provider_action": "pi",',
                "is not a 3-literal row",
            ),
            (
                "fixture",
                'String.starts_with?(case_name, "stage_replay_") -> "stage_replay"',
                "String.starts_with?(case_name, stage_prefix) -> stage_case",
                "no `request_case = cond",
            ),
            (
                "fixture",
                '"claude_containment", "claude-native", :claude_code, :opencode_goal,',
                '"claude_containment", "claude-native", :claude_code, goal_role,',
                "unparseable `@cases` line",
            ),
            (
                "writer",
                "GOAL_ADMISSION_CASES = {",
                "GOAL_ADMISSION_CASES = {*CASES,",
                "not a literal set of strings",
            ),
            ("fixture", "  ] ++\n", "  ] ++\n  @cases [\n", "unparseable `@cases` line"),
            (
                "fixture",
                '"handoff_committed",\n    "lease_armed",\n    "checkout_ready",\n    "native_release",\n',
                ':handoff_committed,\n    "lease_armed",\n    "checkout_ready",\n    "native_release",\n',
                "unparseable `@stage_replay_holds [` element line",
            ),
            (
                "fixture",
                "_name, _profile, _harness, repository_role, _description",
                "_name, _profile, repository_role, _harness, _description",
                "repository_role as the 4th of 5",
            ),
            (
                "fixture",
                "_name, _profile, _harness, repository_role, _description",
                "_name, repository_role, _harness, _description",
                "repository_role as the 4th of 5",
            ),
            (
                "fixture",
                '"leaf_bound",\n',
                '"leaf_#{stage}",\n',
                "interpolated `@stage_replay_holds [` element line",
            ),
            (
                "fixture",
                '          "stage_replay_native_release_codex",\n',
                '          "stage_replay_#{codex}",\n',
                "interpolated `case_name in [` element",
            ),
            (
                "fixture",
                "  ]\n  @ack_loss_stages [\n",
                '  ]\n  @stage_replay_holds [\n    "duplicate",\n  ]\n  @ack_loss_stages [\n',
                "declared 2 times",
            ),
        )
        for side, anchor, replacement, reason in mutations:
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
                self.assertIn(reason, str(refused.exception))


if __name__ == "__main__":
    unittest.main(verbosity=2)
