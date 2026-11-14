# Operator-owned, local-only fixture. Execute inside the running Control release
# with rpc 'Code.eval_file("/repo/scripts/ci/omo-control-native-fixture.exs")'.
# Workspaces allocates the actual repository identity; Goals owns the plan
# Decision, approval, WorkItem, context, Task and budget reservation. The plan
# proposal is explicitly operator-authored, not model-produced authority. Its
# approvals apply only to this local test fixture, never acceptance of the
# user's requested Goal; the real native result remains progress.
defmodule OmOControlNativeFixture do
  alias SymmetryControl.{Goals, Repo, RequestHash, Workspaces}
  alias SymmetryControl.Goals.{GoalDecision, ContextSnapshot}
  alias SymmetryControl.Orchestration.Task

  @origin "wxxb789/symmetry-0006h-provider-fixture"
  @baseline %{
    "commit" => "6adc9ef1a2f704590ee4172cecb4e0bb6c70408c",
    "tree_digest" => "sha256:1831626e62b934f63cf3abd59d3e5ed73e5a21d898eccd6e7d349ae116a39d92"
  }
  @actor "operator:local-native-evidence"
  @artifact "omo-control-native.txt"
  @bytes "OmO production daemon Control evidence\n"

  def run do
    # Never render exception/changeset/connection contents. The DB identity is a
    # guard, not a configurable production target; no connection string is read.
    out = System.fetch_env!("SYMMETRY_OMO_CONTROL_FIXTURE_OUT")
    mode = System.get_env("SYMMETRY_OMO_CONTROL_FIXTURE_MODE", "create")
    db = Repo.query!("SELECT current_database()", []).rows
    if db != [["symmetry_0006k_e2e"]], do: throw(:wrong_database)

    result =
      case mode do
        "create" -> create(out)
        "readback" -> readback()
        _ -> throw(:invalid_fixture_mode)
      end

    encoded = Jason.encode!(result)
    File.write!(out, encoded <> "\n")
    IO.puts(encoded)
  rescue
    _ ->
      failure(%{
        detail: "exception contents withheld; inspect local Control with operator authority"
      })

      raise "local native fixture failed (details withheld)"
  catch
    :throw, reason when is_atom(reason) ->
      failure(%{reason: Atom.to_string(reason)})
      raise "local native fixture rejected"

    :throw, {:domain_error, stage, reason} ->
      failure(%{
        stage: stage,
        reason:
          if(is_atom(reason), do: Atom.to_string(reason), else: "domain error details withheld")
      })

      raise "local native fixture domain rejection"
  end

  defp failure(fields) do
    encoded =
      Jason.encode!(Map.merge(%{schema_version: 1, classification: "fixture_failed"}, fields))

    case System.get_env("SYMMETRY_OMO_CONTROL_FIXTURE_OUT") do
      nil -> :ok
      out -> File.write(out <> ".failure.json", encoded <> "\n", [:exclusive])
    end

    IO.puts(encoded)
  end

  defp domain({:ok, value}, _stage), do: value
  defp domain({:error, reason}, stage), do: throw({:domain_error, stage, reason})

  defp created({:ok, receipt, :created}, _stage), do: receipt

  defp created({:ok, _receipt, :replayed}, stage),
    do: throw({:domain_error, stage, :fixture_label_already_used})

  defp created({:error, reason}, stage), do: throw({:domain_error, stage, reason})

  defp create(out) do
    # A receipt already on disk is not permission to create another paid task.
    if File.exists?(out), do: throw(:fixture_output_already_exists)
    label = System.fetch_env!("SYMMETRY_OMO_CONTROL_FIXTURE_LABEL")
    unless Regex.match?(~r/^[a-z0-9-]{1,40}$/, label), do: throw(:invalid_fixture_label)
    mutation = identity(label <> ":goal")
    profile = "omo-control-evidence"

    result =
      Repo.transaction(fn ->
        project =
          Workspaces.create_project(%{
            name: "Local OmO Control witness",
            key: "ON" <> (Ecto.UUID.generate() |> String.replace("-", "") |> String.slice(0, 6)),
            default_agent_profile: profile,
            default_workspace: "omo-control-workspace"
          })
          |> domain("Workspaces.create_project")

        resource =
          Workspaces.create_resource(project.id, %{
            kind: "repository",
            name: @origin,
            external_ref: @origin,
            url: "https://github.com/" <> @origin,
            metadata: %{"fixture_only" => true, "authorized_baseline" => @baseline}
          })
          |> domain("Workspaces.create_resource")

        subject = Map.put(@baseline, "resource_id", resource.id)

        seam =
          "operator-authored PlanProposal resolved through real scoped Goal Decision; no direct Task, WorkItem, context or reservation insert"

        acceptance = %{
          "schema_version" => "symmetry.acceptance.v1",
          "description" => "Operator review is required; this witness never accepts an outcome.",
          "predicates" => [%{"id" => "operator-review", "kind" => "operator_acceptance"}]
        }

        result_example = %{
          "schema_version" => "symmetry.task_result.v1",
          "result_id" => identity(label <> ":result"),
          "kind" => "progress",
          "summary" => "Read README.md and wrote the bounded witness artifact.",
          "subject" => subject,
          "subject_hash" => digest(subject),
          "evidence_refs" => [],
          "blocker" => nil,
          "proposed_next_action" => nil,
          "proposal" => nil,
          "reason" => nil,
          "diagnostics" => []
        }

        objective =
          "Perform exactly one bounded repository task using only read and write. Read README.md, " <>
            "then write exactly the UTF-8 bytes specified by this JSON string to #{@artifact}: " <>
            Jason.encode!(@bytes) <>
            ". Do not change any other file, execute commands, delegate, " <>
            "access credentials, make network/tool-provider actions, commit or publish. Do not claim " <>
            "Goal acceptance. After successful real tools, return this progress TaskResult as the " <>
            "entire final JSON text: " <>
            Jason.encode!(result_example) <>
            ". If tools fail, return a " <>
            "schema-valid failed or blocked result describing the actual failure, never the example success."

        attrs = %{
          schema_version: "symmetry.goal_create.v1",
          mutation_id: mutation,
          title:
            "Read README.md; write only omo-control-native.txt with the exact approved work-contract bytes; return truthful progress JSON for the admitted Subject. No commands, delegation, credentials, publication or Goal acceptance.",
          initial_revision: %{
            objective: objective,
            non_goals: ["No publication", "No model approval", "No automatic admission"],
            acceptance_contract: acceptance,
            authority_policy: %{
              "operator_required_for_scope_change" => true,
              "operator_required_for_completion" => true,
              "publication_allowed" => false,
              "allowed_actions" => []
            },
            execution_policy: %{
              "automatic_execution" => false,
              "max_parallel_tasks" => 1,
              "max_task_admissions" => 1,
              "max_run_attempts_per_task" => 1,
              "budget_limit_microusd" => nil,
              "per_run_cost_limit_microusd" => nil,
              "budget_mode" => "soft",
              "hard_cost_limit_required" => false,
              "allowed_runtime_ids" => [],
              "allowed_model_profiles" => [profile],
              "final_acceptance" => "operator",
              "allowed_actions" => [],
              "allowed_resource_ids" => [resource.id]
            },
            context_manifest: %{
              "byte_budget" => 32768,
              "required_source_kinds" => ["repository", "approved_goal", "work_contract"],
              "include_advisory_recall" => false
            },
            reason: "Explicit operator-owned local-only evidence fixture"
          }
        }

        created = Goals.create_goal(project.id, attrs, @actor) |> created("Goals.create_goal")
        # Deterministic mutation detects a lost fixture receipt rather than doing
        # new work; an operator must recover its existing IDs from the DB.
        goal_id = created.goal.id

        proposal = %{
          schema_version: "symmetry.plan.v1",
          proposal_id: identity(label <> ":proposal"),
          goal_id: goal_id,
          expected_revision: 1,
          items: [
            %{
              key: "bounded-native-file-work",
              title: "Read the baseline and write one local artifact",
              description: objective,
              required: true,
              integration: true,
              repository_resource_id: resource.id,
              acceptance: acceptance,
              depends_on_keys: [],
              model_profile: profile,
              baseline: %{kind: "subject", subject: subject},
              change_target: nil
            }
          ]
        }

        decision =
          command(
            goal_id,
            "request_decision",
            %{
              kind: "plan",
              work_item_id: nil,
              subject_hash: nil,
              proposal: proposal
            },
            identity(label <> ":decision")
          )

        decision_id = decision.response["decision"]["id"]
        decision_row = Repo.get!(GoalDecision, decision_id)

        resolved =
          command(
            goal_id,
            "resolve_decision",
            %{
              decision_id: decision_id,
              expected_decision_version: decision_row.lock_version,
              option_id: "accept",
              comment: "Operator fixture approval; no model was called."
            },
            identity(label <> ":approval")
          )

        planned =
          command(
            goal_id,
            "accept_plan",
            %{
              proposal: proposal,
              proposal_hash: digest(proposal),
              decision_id: decision_id
            },
            identity(label <> ":plan")
          )

        {:ok, projected} = Goals.fetch_goal(goal_id)
        [item] = projected.work_items

        active =
          command(goal_id, "activate", %{approved_revision: 1}, identity(label <> ":activate"))

        admitted =
          command(
            goal_id,
            "admit_task",
            %{
              work_item_id: item.id,
              purpose: "implement",
              model_profile: profile,
              session_mode: "fresh",
              requested_session_id: nil,
              validation_of_task_id: nil
            },
            identity(label <> ":admission")
          )

        task = Repo.get!(Task, admitted.response["task"]["id"])
        snapshot = Repo.get!(ContextSnapshot, task.context_snapshot_id)

        if task.input["subject"] != subject or task.input["provider_scope"] != nil or
             task.max_run_attempts != 1,
           do: Repo.rollback(:fixture_admission_contract_mismatch)

        %{
          schema_version: 1,
          classification: "operator_fixture_admitted",
          fixture_label: label,
          database: "symmetry_0006k_e2e",
          actor_ref: @actor,
          project_id: project.id,
          repository_resource_id: resource.id,
          authorized_origin: @origin,
          subject: subject,
          subject_hash: digest(subject),
          goal_id: goal_id,
          goal_revision: 1,
          work_item_id: item.id,
          task_id: task.id,
          context_snapshot_id: snapshot.id,
          admission_id: task.admission_key,
          admission: task.input,
          model_profile: profile,
          workspace: task.workspace,
          expected_artifact: %{
            path: @artifact,
            utf8: @bytes,
            content_digest:
              "sha256:" <> Base.encode16(:crypto.hash(:sha256, @bytes), case: :lower)
          },
          result_contract:
            "progress preserves the admitted committed Subject; the new local file is uncommitted and never an accepted candidate",
          fixture_only_creation_seam: seam,
          model_calls: 0,
          authority_source:
            "explicit local test-fixture approval only; not acceptance of the user's requested Goal",
          user_goal_acceptance: false,
          operations: [
            "Workspaces.create_project",
            "Workspaces.create_resource",
            seam,
            "Goals.create_goal",
            "Goals.command request_decision",
            "Goals.command resolve_decision",
            "Goals.command accept_plan",
            "Goals.command activate",
            "Goals.command admit_task"
          ],
          receipts: %{
            created: created,
            decision: decision,
            resolved: resolved,
            planned: planned,
            active: active,
            admitted: admitted
          },
          context: snapshot.payload,
          script_digest:
            "sha256:" <>
              Base.encode16(:crypto.hash(:sha256, File.read!(__ENV__.file)), case: :lower),
          usage: "unknown; soft budget is not a billing or hard-cost assertion"
        }
      end)
      |> domain("fixture transaction")

    result
  end

  defp command(goal_id, kind, payload, mutation_id) do
    {:ok, goal} = Goals.fetch_goal(goal_id)

    Goals.command(
      goal_id,
      %{
        schema_version: "symmetry.goal_command.v1",
        mutation_id: mutation_id,
        expected_version: goal.version,
        expected_revision: goal.current_revision,
        kind: kind,
        payload: payload
      },
      @actor
    )
    |> created("Goals.command " <> kind)
  end

  defp readback do
    fixture =
      System.fetch_env!("SYMMETRY_OMO_CONTROL_FIXTURE_IN") |> File.read!() |> Jason.decode!()

    task_id = fixture["task_id"]
    task = Repo.get!(Task, task_id)
    subject = fixture["subject"]

    unless task.goal_id == fixture["goal_id"] and task.input["subject"] == subject and
             Map.drop(subject, ["resource_id"]) == @baseline,
           do: throw(:readback_identity_mismatch)

    # Sanitized read-only authoritative rows. Fence secrets never leave the DB.
    rows =
      Repo.query!(
        """
        SELECT jsonb_build_object(
          'run_id', r.id, 'generation', r.generation, 'state', r.state,
          'runtime_id', r.runtime_id, 'claimed_runtime_epoch', r.claimed_runtime_epoch,
          'claim_id', r.claim_id, 'result', r.result, 'failure', r.failure,
          'harness_session_id', r.harness_session_id,
          'harness_binding_id', r.harness_binding_id,
          'attach_receipts', (SELECT coalesce(jsonb_agg(jsonb_build_object('id', a.id,
            'request_hash', encode(a.request_hash, 'hex'), 'response', a.response)), '[]')
            FROM harness_session_attach_receipts a WHERE a.run_id = r.id),
          'stop_receipts', (SELECT coalesce(jsonb_agg(jsonb_build_object('id', s.id,
            'binding_id', s.binding_id, 'request_hash', encode(s.request_hash, 'hex'), 'response', s.response)), '[]')
            FROM harness_session_stop_receipts s WHERE s.run_id = r.id),
          'evidence', (SELECT coalesce(jsonb_agg(jsonb_build_object('id', e.id, 'kind', e.kind,
            'subject_hash', encode(e.subject_hash, 'hex'), 'source_ref', e.source_ref,
            'verdict', e.verdict, 'payload', e.payload)), '[]') FROM run_evidence e WHERE e.run_id = r.id),
          'usage', (SELECT coalesce(jsonb_agg(jsonb_build_object('id', u.id, 'usage_key', u.usage_key,
            'provider', u.provider, 'model', u.model, 'input_tokens', u.input_tokens,
            'output_tokens', u.output_tokens, 'cost_microusd', u.cost_microusd,
            'cost_basis', u.cost_basis)), '[]') FROM run_usage u WHERE u.run_id = r.id)
        ) FROM runs r WHERE r.task_id = $1 ORDER BY r.generation
        """,
        [Ecto.UUID.dump!(task_id)]
      ).rows
      |> Enum.map(&hd/1)

    count =
      Repo.query!("SELECT count(*) FROM tasks WHERE goal_id = $1", [Ecto.UUID.dump!(task.goal_id)]).rows
      |> hd()
      |> hd()

    %{
      schema_version: 1,
      classification: "authoritative_db_readback",
      task_id: task.id,
      goal_id: task.goal_id,
      task_state: task.state,
      task_result: task.result,
      admission: task.input,
      runs: rows,
      task_admission_count: count,
      operations: ["read-only Task fetch/count", "read-only Run/attach/stop/evidence/usage query"],
      usage: "unknown; this readback does not establish billing"
    }
  end

  defp digest(value), do: "sha256:" <> Base.encode16(RequestHash.canonical(value), case: :lower)

  defp identity(value) do
    <<a::32, b::16, _::4, c::12, _::2, d::14, e::48, _::binary>> =
      :crypto.hash(:sha256, "omo-control-fixture:" <> value)

    Enum.map_join([{a, 8}, {b, 4}, {bor_version(c), 4}, {bor_variant(d), 4}, {e, 12}], "-", fn {n,
                                                                                                width} ->
      n |> Integer.to_string(16) |> String.downcase() |> String.pad_leading(width, "0")
    end)
  end

  defp bor_version(n), do: Bitwise.bor(n, 0x5000)
  defp bor_variant(n), do: Bitwise.bor(n, 0x8000)
end

OmOControlNativeFixture.run()
