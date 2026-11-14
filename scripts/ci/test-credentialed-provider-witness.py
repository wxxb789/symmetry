#!/usr/bin/env python3
"""Local witness regressions. Requires Git, Mix and compiled Control test dependencies.

Run from the repository root with MIX_ENV=test and the private Mix/Elixir PATH:
  python3 scripts/ci/test-credentialed-provider-witness.py

No database, credentials, gh, broker, application startup or provider calls are used.
The stale-BEAM fixture is a temporary, dependency-free Mix project.
"""

import hashlib
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest import mock


ROOT = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location(
    "credentialed_provider_witness", ROOT / "scripts/ci/credentialed-provider-witness.py"
)
WITNESS = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(WITNESS)


class WitnessLauncherTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix="symmetry-witness-negative-")
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        (self.root / "control/lib").mkdir(parents=True)
        (self.root / "scripts/ci").mkdir(parents=True)
        self.module = self.root / "control/lib/witness_build.ex"
        self.module.write_text('defmodule WitnessBuild do\n  def value, do: "stale"\nend\n')
        (self.root / "control/mix.exs").write_text(
            'defmodule WitnessBuild.MixProject do\n'
            '  use Mix.Project\n'
            '  def project, do: [app: :witness_build, version: "0.1.0", deps: []]\n'
            'end\n'
        )
        self.git("init", "--quiet")
        self.git("add", ".")
        self.git("-c", "user.name=Witness", "-c", "user.email=witness@example.invalid",
                 "-c", "commit.gpgsign=false", "commit", "--quiet", "-m", "local fixture")
        self.environment = os.environ.copy()
        self.environment.update({"MIX_ENV": "test", "MIX_BUILD_PATH": str(self.root / "build"),
                                 "MIX_DEPS_PATH": str(self.root / "deps")})
        self.config = {
            "namespace": "local-negative-controls", "resource_id": "unused-by-launcher",
            "base_branch": "main", "code_base_sha": WITNESS.BASE,
            "code_head_sha": self.git("rev-parse", "HEAD").decode().strip(),
            "changed_source_manifest": [], "allow_manifest_bound_dirty": True,
        }

    def git(self, *args):
        return subprocess.check_output(["git", *args], cwd=self.root, stderr=subprocess.PIPE)

    def manifest(self):
        self.config["changed_source_manifest"] = [{
            "path": "control/lib/witness_build.ex",
            "sha256": hashlib.sha256(self.module.read_bytes()).hexdigest(),
        }]

    def mix(self, *args):
        return subprocess.run(["mix", *args], cwd=self.root / "control", env=self.environment,
                              capture_output=True, check=True).stdout

    def test_launcher_force_compiles_stale_beam_before_first_phase(self):
        self.mix("compile")
        timestamp = self.module.stat().st_mtime_ns
        self.module.write_text('defmodule WitnessBuild do\n  def value, do: "reviewed"\nend\n')
        os.utime(self.module, ns=(timestamp, timestamp))
        self.manifest()
        self.assertIn(b"stale", self.mix("run", "--no-start", "--no-compile", "-e",
                                       "IO.puts(WitnessBuild.value())"))
        evidence = self.root / "evidence"
        config_path = self.root / "config.json"
        config_path.write_text(json.dumps(self.config))
        real_run = subprocess.run
        commands = []

        def local_run(command, **kwargs):
            commands.append(command)
            if command[:2] != ["mix", "run"]:
                return real_run(command, **kwargs)
            # Inspect the actual compiled fixture at the launcher's phase boundary.
            compiled = real_run(["mix", "run", "--no-start", "--no-compile", "-e",
                                 "IO.puts(WitnessBuild.value())"],
                                cwd=kwargs["cwd"], env=kwargs["env"], capture_output=True, check=True)
            self.assertEqual(compiled.stdout.strip(), b"reviewed")
            phase = command[-2]
            receipt = "barrier-crash.json" if phase == "crash" else "phase-" + phase + ".json"
            WITNESS.safe_write(evidence / receipt, {"local_test_phase": phase})
            return subprocess.CompletedProcess(command, 86 if phase == "crash" else 0, b"", b"")

        environment = dict(self.environment, POSTGRES_HOST="127.0.0.1", POSTGRES_PORT="55436",
                           POSTGRES_DB="symmetry_0006h_test", GH_TOKEN="", GITHUB_TOKEN="")
        argv = ["witness", "--config", str(config_path), "--evidence-dir", str(evidence),
                "--allow-live-fixture"]
        with mock.patch.object(WITNESS, "__file__", str(self.root / "scripts/ci/witness.py")), \
                mock.patch.dict(os.environ, environment, clear=True), \
                mock.patch.object(sys, "argv", argv), \
                mock.patch.object(WITNESS.subprocess, "run", side_effect=local_run), \
                mock.patch("builtins.print"):
            WITNESS.main()
        compile_index = commands.index(["mix", "compile", "--force"])
        first_phase = next(i for i, command in enumerate(commands) if command[:2] == ["mix", "run"])
        self.assertLess(compile_index, first_phase)
        receipt = json.loads((evidence / "compile.json").read_text())
        self.assertEqual(receipt["exit_code"], 0)
        self.assertEqual(receipt["manifest_sha256"], WITNESS.source(self.root, self.config)["manifest_sha256"])

    def test_compile_failure_prevents_every_phase_and_records_safe_status(self):
        self.manifest()
        evidence = self.root / "evidence"
        config_path = self.root / "config.json"
        config_path.write_text(json.dumps(self.config))
        real_run = subprocess.run
        commands = []

        def local_run(command, **kwargs):
            if command[0] == "mix":
                commands.append(command)
                return subprocess.CompletedProcess(command, 1, b"", b"private compiler diagnostic")
            return real_run(command, **kwargs)

        environment = dict(self.environment, POSTGRES_HOST="127.0.0.1", POSTGRES_PORT="55436",
                           POSTGRES_DB="symmetry_0006h_test", GH_TOKEN="", GITHUB_TOKEN="")
        with mock.patch.object(WITNESS, "__file__", str(self.root / "scripts/ci/witness.py")), \
                mock.patch.dict(os.environ, environment, clear=True), \
                mock.patch.object(sys, "argv", ["witness", "--config", str(config_path),
                    "--evidence-dir", str(evidence), "--allow-live-fixture"]), \
                mock.patch.object(WITNESS.subprocess, "run", side_effect=local_run):
            with self.assertRaisesRegex(RuntimeError, "validated_source_compile_failed"):
                WITNESS.main()
        self.assertEqual(commands, [["mix", "compile", "--force"]])
        receipt = (evidence / "compile.json").read_text()
        self.assertEqual(json.loads(receipt)["exit_code"], 1)
        self.assertNotIn("private compiler diagnostic", receipt)

    def test_compile_revalidates_manifest_after_compiler_returns(self):
        self.manifest()
        observed = WITNESS.source(self.root, self.config)
        evidence = self.root / "evidence"
        evidence.mkdir()
        real_run = subprocess.run

        def changed_during_compile(command, **kwargs):
            if command[0] == "mix":
                self.module.write_text("unreviewed source\n")
                return subprocess.CompletedProcess(command, 0, b"", b"")
            return real_run(command, **kwargs)

        with mock.patch.object(WITNESS.subprocess, "run", side_effect=changed_during_compile):
            with self.assertRaisesRegex(RuntimeError, "changed_source_digest_mismatch"):
                WITNESS.compile_validated_source(self.root, self.config, observed, evidence,
                                                 self.environment, False)

    def test_manifest_still_rejects_unlisted_dirty_production_source(self):
        self.manifest()
        (self.root / "control/lib/unreviewed.ex").write_text("unreviewed\n")
        with self.assertRaisesRegex(RuntimeError, "dirty_source_missing_from_manifest"):
            WITNESS.source(self.root, self.config)


ELIXIR_CONTROLS = r'''
file = Path.expand("../scripts/ci/credentialed-provider-witness.exs")
{:__block__, _, expressions} = file |> File.read!() |> Code.string_to_quoted!()
# Load the real policy modules, never the script's live entrypoint.
Enum.each(expressions, fn
  {:defmodule, _, _} = definition -> Code.eval_quoted(definition, [], file: file)
  _entrypoint -> :ok
end)
ExUnit.start(autorun: false)

defmodule CredentialedProviderWitness.LocalNegativeControls do
  use ExUnit.Case, async: false
  alias CredentialedProviderWitness, as: Witness
  alias CredentialedProviderWitness.HTTP
  alias CredentialedProviderWitness.Failure

  setup do
    Process.delete({HTTP, :observed_targets})
    resource_id = Ecto.UUID.generate()
    subject = %{"resource_id" => resource_id, "commit" => String.duplicate("a", 40),
                "tree_digest" => "sha256:" <> String.duplicate("b", 64)}
    authorization = %{
      "action_id" => Ecto.UUID.generate(), "resource_id" => resource_id,
      "connection_id" => Ecto.UUID.generate(), "project_id" => Ecto.UUID.generate(),
      "run_id" => Ecto.UUID.generate(), "task_id" => Ecto.UUID.generate(),
      "runtime_id" => Ecto.UUID.generate(), "work_item_id" => Ecto.UUID.generate(),
      "operation" => "change.upsert", "subject" => subject,
      "target" => %{"kind" => "branches", "source_branch" => "local-current", "target_branch" => "main"},
      "input" => %{"title" => "local current", "body" => "local payload"}
    }
    input = Map.merge(authorization["input"], Map.take(authorization["target"], ["source_branch", "target_branch"]))
    binding = %{
      "v" => 1, "action_id" => authorization["action_id"], "run_id" => authorization["run_id"],
      "task_id" => authorization["task_id"], "subject" => subject,
      "repository" => %{"id" => 1_457_073_269, "node_id" => "R_kgDOVtksdQ"},
      "operation" => "change.upsert", "input" => input, "target" => authorization["target"],
      "title" => authorization["input"]["title"], "body" => authorization["input"]["body"]
    }
    intent = authorization |> Map.take(~w(action_id resource_id connection_id project_id run_id task_id runtime_id work_item_id operation))
             |> Map.new(fn {key, value} -> {String.to_existing_atom(key), value} end)
             |> Map.merge(%{id: Ecto.UUID.generate(), provider: "github", resource_kind: "repository",
                            resource_external_ref: "wxxb789/symmetry-0006h-provider-fixture",
                            account_ref: "wxxb789", input: input, action_binding: binding, state: "accepted"})
    %{authorization: authorization, intent: intent, binding: binding}
  end

  defp payload(binding) do
    digest = binding |> SymmetryControl.RequestHash.canonical() |> Base.encode16(case: :lower)
    %{"title" => binding["title"], "body" => binding["body"] <>
      "\n\n<!-- symmetry-provider-action:v1:#{binding["action_id"]}:#{digest} -->"}
  end

  defp pull_request(binding) do
    %{"number" => 18, "html_url" => "https://github.com/wxxb789/symmetry-0006h-provider-fixture/pull/18",
      "head" => %{"sha" => binding["subject"]["commit"], "ref" => binding["target"]["source_branch"], "repo" => binding["repository"]},
      "base" => %{"ref" => "main", "repo" => binding["repository"]}}
  end

  test "unrelated accepted intent is rejected without adopting or rewriting it", data do
    foreign = %{data.intent | action_id: Ecto.UUID.generate(), resource_id: Ecto.UUID.generate()}
    before = :erlang.term_to_binary(foreign)
    assert_raise Failure, fn ->
      Witness.authorize_pending_intents!([foreign], %{data.intent.action_id => data.authorization})
    end
    assert :erlang.term_to_binary(foreign) == before
    refute Process.whereis(SymmetryControl.Integrations.ProviderAccess)
  end

  test "current accepted executing and unknown intents retain exact recovery authorization", data do
    for state <- ["accepted", "executing", "unknown"] do
      assert :ok == Witness.authorize_pending_intents!([%{data.intent | state: state}],
                     %{data.intent.action_id => data.authorization})
    end
  end

  test "invocation state pins all four actions to exact configured Subjects and resource", data do
    subject_hash = fn subject ->
      bytes = ["{\"commit\":", Jason.encode!(subject["commit"]), ",\"resource_id\":",
               Jason.encode!(subject["resource_id"]), ",\"tree_digest\":", Jason.encode!(subject["tree_digest"]), "}"]
      "sha256:" <> Base.encode16(:crypto.hash(:sha256, bytes), case: :lower)
    end
    success = Map.put(data.authorization, "subject_hash", subject_hash.(data.authorization["subject"]))
    failure_subject = Map.put(success["subject"], "commit", String.duplicate("c", 40))
    failure = success |> Map.put("subject", failure_subject) |> Map.put("subject_hash", subject_hash.(failure_subject))
    config = %{"namespace" => "local-controls", "resource_id" => success["resource_id"],
               "success_subject" => success["subject"], "failure_subject" => failure_subject,
               "source_branch" => "local-current", "failure_branch" => "local-failure"}
    state = %{"project_id" => success["project_id"], "connection_id" => success["connection_id"],
              "resource_id" => success["resource_id"], "success" => success, "failure" => failure,
              "actions" => Map.new(~w(success failure timeout crash), &{&1, Ecto.UUID.generate()})}
    authorizations = Witness.invocation_authorizations(config, state)
    assert map_size(authorizations) == 4
    assert authorizations[state["actions"]["failure"]]["subject"] == failure_subject
    assert authorizations[state["actions"]["crash"]]["subject"] == success["subject"]
    assert authorizations[state["actions"]["timeout"]]["operation"] == "change.update"
    for changed <- [Map.put(state, "resource_id", Ecto.UUID.generate()),
                    put_in(state, ["success", "subject", "tree_digest"], "foreign"),
                    put_in(state, ["actions", "crash"], state["actions"]["success"])] do
      assert_raise Failure, fn -> Witness.invocation_authorizations(config, changed) end
    end
  end

  test "outbound identities and complete Subject cannot borrow a fixture authorization", data do
    for field <- [:action_id, :resource_id, :connection_id, :project_id, :run_id, :task_id,
                  :runtime_id, :work_item_id, :operation] do
      assert_raise Failure, fn ->
        Witness.authorize_intent!(Map.put(data.intent, field, Ecto.UUID.generate()), data.authorization)
      end
    end
    for field <- ["resource_id", "commit", "tree_digest"] do
      binding = put_in(data.binding, ["subject", field], "foreign")
      assert_raise Failure, fn ->
        Witness.authorize_intent!(%{data.intent | action_binding: binding}, data.authorization)
      end
    end
    assert_raise Failure, fn ->
      Witness.authorize_intent!(%{data.intent | input: Map.put(data.intent.input, "title", "foreign")}, data.authorization)
    end
  end

  test "HTTP POST policy accepts only the exact authorized operation marker payload and branches", data do
    context = Witness.authorize_intent!(data.intent, data.authorization)
    uri = URI.parse("https://api.github.com/repos/wxxb789/symmetry-0006h-provider-fixture/pulls")
    body = Map.merge(payload(data.binding), %{"head" => "local-current", "base" => "main"})
    assert :ok == HTTP.authorize_write!(:post, uri, body, context)
    for changed <- [Map.put(body, "head", "foreign"), Map.put(body, "base", "foreign"),
                    Map.put(body, "title", "foreign"), Map.put(body, "body", "foreign")] do
      assert_raise Failure, fn -> HTTP.authorize_write!(:post, uri, changed, context) end
    end
    foreign = put_in(context, ["action_binding", "action_id"], Ecto.UUID.generate())
    assert_raise Failure, fn -> HTTP.authorize_write!(:post, uri, body, foreign) end
    update = put_in(context, ["action_binding", "operation"], "change.update")
    assert_raise Failure, fn -> HTTP.authorize_write!(:post, uri, body, update) end
  end

  test "HTTP PATCH policy requires a same-action exact Subject and repository target read", data do
    context = Witness.authorize_intent!(data.intent, data.authorization)
    uri = URI.parse("https://api.github.com/repos/wxxb789/symmetry-0006h-provider-fixture/pulls/18")
    body = payload(data.binding)
    assert_raise Failure, fn -> HTTP.authorize_write!(:patch, uri, body, context) end
    pr = pull_request(data.binding)
    HTTP.remember_targets({:ok, 200, %{}, [put_in(pr, ["head", "sha"], "foreign")]}, context)
    assert_raise Failure, fn -> HTTP.authorize_write!(:patch, uri, body, context) end
    HTTP.remember_targets({:ok, 200, %{}, [put_in(pr, ["base", "repo", "id"], 99)]}, context)
    assert_raise Failure, fn -> HTTP.authorize_write!(:patch, uri, body, context) end
    HTTP.remember_targets({:ok, 200, %{}, [pr]}, context)
    assert :ok == HTTP.authorize_write!(:patch, uri, body, context)
    assert_raise Failure, fn -> HTTP.authorize_write!(:patch, %{uri | path: uri.path <> "1"}, body, context) end
    other = put_in(context, ["action_binding", "action_id"], Ecto.UUID.generate())
    assert_raise Failure, fn -> HTTP.authorize_write!(:patch, uri, payload(other["action_binding"]), other) end
  end

  test "HTTP update policy retains exact invocation target and read-only recovery phases", data do
    url = "https://github.com/wxxb789/symmetry-0006h-provider-fixture/pull/18"
    authorization = Map.put(data.authorization, "operation", "change.update")
    input = Map.put(authorization["input"], "pull_request_url", url)
    binding = data.binding |> Map.put("operation", "change.update") |> Map.put("input", input)
    intent = %{data.intent | operation: "change.update", input: input, action_binding: binding}
    context = Witness.authorize_intent!(intent, authorization)
    HTTP.remember_targets({:ok, 200, %{}, pull_request(binding)}, context)
    uri = URI.parse("https://api.github.com/repos/wxxb789/symmetry-0006h-provider-fixture/pulls/18")
    assert :ok == HTTP.authorize_write!(:patch, uri, payload(binding), context)
    other = put_in(context, ["action_binding", "input", "pull_request_url"], url <> "1")
    assert_raise Failure, fn -> HTTP.authorize_write!(:patch, uri, payload(other["action_binding"]), other) end
    controller = self()
    for phase <- ["recover", "finish"] do
      {:ok, agent} = Agent.start_link(fn -> %{phase: phase, controller: controller} end,
                                     name: CredentialedProviderWitness.State)
      try do
        for method <- [:post, :patch] do
          destination = if method == :post, do: String.replace_suffix(URI.to_string(uri), "/18", ""), else: URI.to_string(uri)
          assert_raise RuntimeError, "provider_phase_read_only", fn ->
            HTTP.request(method, destination, [], payload(binding))
          end
          assert_receive {:read_only_provider_write_blocked, ^controller, ^method}
        end
      after
        Agent.stop(agent)
      end
    end
  end
end

result = ExUnit.run()
System.halt(if result.failures == 0, do: 0, else: 1)
'''


class WitnessHTTPPolicyTests(unittest.TestCase):
    def test_actual_elixir_http_policy_and_pending_intent_negative_controls(self):
        environment = os.environ.copy()
        environment["MIX_ENV"] = "test"
        result = subprocess.run(["mix", "run", "--no-start", "--no-compile", "--eval", ELIXIR_CONTROLS],
                                cwd=ROOT / "control", env=environment,
                                stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, check=False)
        self.assertEqual(result.returncode, 0, result.stdout)


if __name__ == "__main__":
    unittest.main()
