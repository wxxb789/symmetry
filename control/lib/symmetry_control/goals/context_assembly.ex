defmodule SymmetryControl.Goals.ContextAssembly do
  @moduledoc false

  import Ecto.Query
  import SymmetryControl.Goals.ContractValidation, only: [validate_contract: 3]
  import SymmetryControl.Goals.Internal, only: [normalize_map: 1, rollback: 1, value: 2, value: 3]

  alias SymmetryControl.Goals.ContextSnapshot
  alias SymmetryControl.Goals.Decisions
  alias SymmetryControl.Goals.GoalDecision
  alias SymmetryControl.Goals.RunEvidence
  alias SymmetryControl.Orchestration.Run
  alias SymmetryControl.Orchestration.Task
  alias SymmetryControl.Repo
  alias SymmetryControl.RequestHash
  alias SymmetryControl.Workspaces.WorkItem

  @nonterminal_task_states [
    "queued",
    "assigned",
    "claimed",
    "running",
    "waiting_for_input",
    "paused",
    "cancelling"
  ]

  @doc false
  def safe_document?(value) when is_map(value) do
    Enum.all?(value, fn {key, nested} ->
      case key do
        key when is_atom(key) or is_binary(key) ->
          not sensitive_document_key?(key) and
            safe_document?(nested)

        _ ->
          false
      end
    end)
  end

  def safe_document?(value) when is_list(value), do: Enum.all?(value, &safe_document?/1)
  def safe_document?(_value), do: true

  @doc false
  def safe_evidence_document(evidence) do
    if safe_document?(evidence), do: :ok, else: {:error, :invalid_request}
  end

  defp sensitive_document_key?(key) do
    normalized =
      key
      |> to_string()
      |> Macro.underscore()
      |> String.replace(~r/[^a-z0-9]/, "")

    normalized != "tokenestimate" and
      Enum.any?(
        [
          "token",
          "secret",
          "password",
          "credential",
          "authorization",
          "rawtranscript",
          "apikey",
          "privatekey",
          "sessionfilename",
          "localhandle"
        ],
        &String.contains?(normalized, &1)
      )
  end

  defp sanitize_machine_context(value) when is_map(value) do
    Map.new(value, fn {key, nested} -> {key, sanitize_machine_context(nested)} end)
    |> Enum.reject(fn {key, _value} ->
      (is_atom(key) or is_binary(key)) and sensitive_document_key?(key)
    end)
    |> Map.new()
  end

  defp sanitize_machine_context(value) when is_list(value),
    do: Enum.map(value, &sanitize_machine_context/1)

  defp sanitize_machine_context(value), do: value

  @doc false
  def context_wire_envelope(snapshot, goal, item, task) do
    document =
      (snapshot.payload || %{})
      |> sanitize_machine_context()
      |> normalize_map()

    expected_hash = context_content_hash(document)
    expected_subject = task && value(task.input || %{}, "subject")

    with true <- document["schema_version"] == "symmetry.context_snapshot.v1",
         true <- document["snapshot_id"] == snapshot.id,
         true <- document["goal_id"] == goal.id,
         true <- document["goal_revision"] == snapshot.goal_revision,
         true <- document["work_item_id"] == if(item, do: item.id, else: nil),
         true <- document["content_hash"] == context_digest(snapshot.content_hash),
         true <- document["content_hash"] == expected_hash,
         true <-
           is_nil(expected_subject) or document["subject"] == normalize_map(expected_subject),
         :ok <- validate_contract(:context_snapshot, document, []) do
      {:ok, document}
    else
      false -> {:error, :invalid_context_snapshot}
      {:error, _reason} = error -> error
    end
  end

  @doc false

  def context_snapshot_document(
        goal,
        revision,
        item,
        purpose,
        _model_profile,
        subject,
        validation_bindings,
        snapshot_id,
        next_action,
        inserted_at,
        handoff_source
      ) do
    byte_budget = value(revision.context_manifest || %{}, "byte_budget", 32_768)

    approved_goal = %{
      "goal_id" => goal.id,
      "revision" => revision.revision,
      "objective" => revision.objective,
      "authority_policy" => revision.authority_policy
    }

    work_contract = %{
      "title" => item.title,
      "description" => item.description || "Approved work item.",
      "purpose" => purpose,
      "acceptance" => item.acceptance_contract,
      "validation_bindings" => validation_bindings,
      "change_target" => item.change_target
    }

    document = %{
      "schema_version" => "symmetry.context_snapshot.v1",
      "snapshot_id" => snapshot_id,
      "goal_id" => goal.id,
      "goal_revision" => revision.revision,
      "work_item_id" => item.id,
      "content_hash" => "sha256:" <> String.duplicate("0", 64),
      "created_at" => DateTime.to_iso8601(inserted_at),
      "approved_goal" => approved_goal,
      "work_contract" => work_contract,
      "subject" => subject,
      "sources" =>
        context_sources!(
          goal,
          revision,
          item,
          subject,
          approved_goal,
          work_contract,
          inserted_at,
          handoff_source
        ),
      "current_decisions" => context_decisions(goal.id, revision.revision),
      "validated_evidence" => context_evidence(goal.id, revision.revision, item.id, subject),
      "failed_attempts" => context_failed_attempts(goal.id, revision.revision, item.id),
      "advisory_recall" => advisory_recall(goal, revision, item, subject),
      "next_action" => next_action,
      "size" => %{
        "mandatory_bytes" => 0,
        "optional_bytes" => 0,
        "total_bytes" => 0,
        "byte_budget" => byte_budget,
        "token_estimate" => nil
      }
    }

    sized = stabilize_context_size(document, byte_budget)

    if sized["size"]["mandatory_bytes"] > byte_budget do
      rollback(:context_budget_exceeded)
    end

    Map.put(sized, "content_hash", context_content_hash(sized))
  end

  @doc false

  def planning_context_snapshot_document(
        goal,
        revision,
        resource,
        subject,
        snapshot_id,
        inserted_at,
        handoff_source
      ) do
    byte_budget = value(revision.context_manifest || %{}, "byte_budget", 32_768)

    approved_goal = %{
      "goal_id" => goal.id,
      "revision" => revision.revision,
      "objective" => revision.objective,
      "authority_policy" => revision.authority_policy
    }

    work_contract = %{
      "title" => "Plan the approved Goal",
      "description" =>
        "Produce a bounded PlanProposal for the approved Goal. This does not approve or admit work.",
      "purpose" => "plan",
      "acceptance" => Decisions.planning_acceptance_contract(),
      "validation_bindings" => [],
      "change_target" => nil
    }

    document = %{
      "schema_version" => "symmetry.context_snapshot.v1",
      "snapshot_id" => snapshot_id,
      "goal_id" => goal.id,
      "goal_revision" => revision.revision,
      "work_item_id" => nil,
      "content_hash" => "sha256:" <> String.duplicate("0", 64),
      "created_at" => DateTime.to_iso8601(inserted_at),
      "approved_goal" => approved_goal,
      "work_contract" => work_contract,
      "subject" => subject,
      "sources" =>
        planning_context_sources!(
          goal,
          revision,
          resource,
          subject,
          approved_goal,
          work_contract,
          inserted_at,
          handoff_source
        ),
      "current_decisions" => context_decisions(goal.id, revision.revision),
      "validated_evidence" => [],
      "failed_attempts" => [],
      "advisory_recall" => [],
      "next_action" => nil,
      "size" => %{
        "mandatory_bytes" => 0,
        "optional_bytes" => 0,
        "total_bytes" => 0,
        "byte_budget" => byte_budget,
        "token_estimate" => nil
      }
    }

    sized = stabilize_context_size(document, byte_budget)

    if sized["size"]["mandatory_bytes"] > byte_budget do
      rollback(:context_budget_exceeded)
    end

    Map.put(sized, "content_hash", context_content_hash(sized))
  end

  defp planning_context_sources!(
         goal,
         revision,
         resource,
         subject,
         approved_goal,
         work_contract,
         inserted_at,
         handoff_source
       ) do
    observed_at = DateTime.to_iso8601(inserted_at)
    required_kinds = value(revision.context_manifest || %{}, "required_source_kinds", [])

    sources = [
      %{
        "resource_id" => resource.id,
        "source_kind" => "approved_goal",
        "source_revision" => "goal:#{goal.id}:#{revision.revision}",
        "content_hash" => context_digest(RequestHash.canonical(approved_goal)),
        "observed_at" => observed_at,
        "trust" => "trusted_policy",
        "required" => "approved_goal" in required_kinds,
        "content" => %{"kind" => "pointer", "value" => "goal:#{goal.id}:#{revision.revision}"}
      },
      %{
        "resource_id" => resource.id,
        "source_kind" => "work_contract",
        "source_revision" => "goal:#{goal.id}:#{revision.revision}:plan",
        "content_hash" => context_digest(RequestHash.canonical(work_contract)),
        "observed_at" => observed_at,
        "trust" => "trusted_policy",
        "required" => "work_contract" in required_kinds,
        "content" => %{
          "kind" => "pointer",
          "value" => "goal:#{goal.id}:#{revision.revision}:plan"
        }
      },
      %{
        "resource_id" => resource.id,
        "source_kind" => "repository_subject",
        "source_revision" => subject["commit"],
        "content_hash" => subject["tree_digest"],
        "observed_at" => observed_at,
        "trust" => "repository_untrusted",
        "required" => "repository_subject" in required_kinds,
        "content" => %{"kind" => "pointer", "value" => "repository:" <> resource.id}
      },
      %{
        "resource_id" => resource.id,
        "source_kind" => "repository",
        "source_revision" => subject["commit"],
        "content_hash" => subject["tree_digest"],
        "observed_at" => observed_at,
        "trust" => "repository_untrusted",
        "required" => "repository" in required_kinds,
        "content" => %{"kind" => "pointer", "value" => "repository:" <> resource.id}
      }
    ]

    sources = sources ++ handoff_context_source(handoff_source, inserted_at)
    source_kinds = sources |> Enum.map(& &1["source_kind"]) |> MapSet.new()

    unless is_list(required_kinds) and
             Enum.all?(required_kinds, &MapSet.member?(source_kinds, &1)) do
      rollback(:required_context_source_missing)
    end

    sources
  end

  defp context_sources!(
         goal,
         revision,
         item,
         subject,
         approved_goal,
         work_contract,
         inserted_at,
         handoff_source
       ) do
    observed_at = DateTime.to_iso8601(inserted_at)
    required_kinds = value(revision.context_manifest || %{}, "required_source_kinds", [])

    sources = [
      %{
        "resource_id" => item.repository_resource_id,
        "source_kind" => "approved_goal",
        "source_revision" => "goal:#{goal.id}:#{revision.revision}",
        "content_hash" => context_digest(RequestHash.canonical(approved_goal)),
        "observed_at" => observed_at,
        "trust" => "trusted_policy",
        "required" => "approved_goal" in required_kinds,
        "content" => %{"kind" => "pointer", "value" => "goal:#{goal.id}:#{revision.revision}"}
      },
      %{
        "resource_id" => item.repository_resource_id,
        "source_kind" => "work_contract",
        "source_revision" => "work-item:#{item.id}",
        "content_hash" => context_digest(RequestHash.canonical(work_contract)),
        "observed_at" => observed_at,
        "trust" => "trusted_policy",
        "required" => "work_contract" in required_kinds,
        "content" => %{"kind" => "pointer", "value" => "work-item:#{item.id}"}
      },
      %{
        "resource_id" => item.repository_resource_id,
        "source_kind" => "repository_subject",
        "source_revision" => subject["commit"],
        "content_hash" => subject["tree_digest"],
        "observed_at" => observed_at,
        "trust" => "repository_untrusted",
        "required" => "repository_subject" in required_kinds,
        "content" => %{
          "kind" => "pointer",
          "value" => "repository:" <> item.repository_resource_id
        }
      },
      %{
        "resource_id" => item.repository_resource_id,
        "source_kind" => "repository",
        "source_revision" => subject["commit"],
        "content_hash" => subject["tree_digest"],
        "observed_at" => observed_at,
        "trust" => "repository_untrusted",
        "required" => "repository" in required_kinds,
        "content" => %{
          "kind" => "pointer",
          "value" => "repository:" <> item.repository_resource_id
        }
      }
    ]

    sources = sources ++ handoff_context_source(handoff_source, inserted_at)
    source_kinds = sources |> Enum.map(& &1["source_kind"]) |> MapSet.new()

    unless is_list(required_kinds) and
             Enum.all?(required_kinds, &MapSet.member?(source_kinds, &1)) do
      rollback(:required_context_source_missing)
    end

    sources
  end

  defp handoff_context_source(nil, _inserted_at), do: []

  defp handoff_context_source(source, inserted_at) do
    [
      %{
        "resource_id" => source.subject["resource_id"],
        "source_kind" => "handoff_source",
        "source_revision" => "run:" <> source.run_id,
        "content_hash" =>
          %{
            run_id: source.run_id,
            task_id: source.task_id,
            result_id: source.result_id,
            subject_hash: context_digest(source.subject_hash),
            evidence_ids: source.evidence_ids
          }
          |> RequestHash.canonical()
          |> context_digest(),
        "observed_at" => DateTime.to_iso8601(inserted_at),
        "trust" => "advisory",
        "required" => true,
        "content" => %{"kind" => "pointer", "value" => "run:" <> source.run_id}
      }
    ]
  end

  defp advisory_recall(goal, revision, item, subject) do
    if value(revision.context_manifest || %{}, "include_advisory_recall", false) == true do
      Repo.all(
        from(snapshot in ContextSnapshot,
          join: source_item in WorkItem,
          on: source_item.id == snapshot.work_item_id,
          join: task in Task,
          on: task.context_snapshot_id == snapshot.id,
          where:
            snapshot.goal_id == ^goal.id and
              source_item.repository_resource_id == ^item.repository_resource_id and
              task.state not in ^@nonterminal_task_states,
          order_by: [
            asc:
              fragment(
                "CASE WHEN ? = ? THEN 0 ELSE 1 END",
                snapshot.work_item_id,
                type(^item.id, :binary_id)
              ),
            desc: snapshot.goal_revision,
            desc: snapshot.inserted_at,
            asc: snapshot.id
          ],
          limit: 256,
          select: %{
            id: snapshot.id,
            work_item_id: snapshot.work_item_id,
            goal_revision: snapshot.goal_revision,
            content_hash: snapshot.content_hash,
            payload: snapshot.payload,
            inserted_at: snapshot.inserted_at
          }
        )
      )
      |> Enum.map(fn snapshot ->
        %{
          "resource_id" => item.repository_resource_id,
          "source_kind" => "context_snapshot",
          "source_revision" => "snapshot:" <> snapshot.id,
          "content_hash" => context_digest(snapshot.content_hash),
          "observed_at" => DateTime.to_iso8601(snapshot.inserted_at),
          "trust" => "advisory",
          "required" => false,
          "stale" =>
            snapshot.goal_revision != revision.revision or snapshot.work_item_id != item.id or
              normalize_map(value(snapshot.payload || %{}, "subject", %{})) != subject,
          "content" => %{"kind" => "pointer", "value" => "context_snapshot:" <> snapshot.id}
        }
      end)
    else
      []
    end
  end

  defp stabilize_context_size(document, byte_budget) do
    mandatory =
      document
      |> Map.put("advisory_recall", [])
      |> stabilize_mandatory_context_size(byte_budget)

    mandatory_bytes = mandatory["size"]["mandatory_bytes"]

    if mandatory_bytes > byte_budget do
      rollback(:context_budget_exceeded)
    end

    recall = value(document, "advisory_recall", [])

    selected =
      Enum.reduce_while(recall, [], fn source, selected ->
        candidate =
          document
          |> Map.put("advisory_recall", selected ++ [source])
          |> stabilize_optional_context_size(byte_budget, mandatory_bytes)

        if candidate["size"]["total_bytes"] <= byte_budget do
          {:cont, selected ++ [source]}
        else
          {:halt, selected}
        end
      end)

    document
    |> Map.put("advisory_recall", selected)
    |> stabilize_optional_context_size(byte_budget, mandatory_bytes)
  end

  defp stabilize_mandatory_context_size(document, byte_budget, previous \\ nil, attempts \\ 0)

  defp stabilize_mandatory_context_size(_document, _byte_budget, _previous, attempts)
       when attempts >= 16,
       do: rollback(:context_budget_exceeded)

  defp stabilize_mandatory_context_size(document, byte_budget, previous, attempts) do
    bytes = byte_size(Jason.encode!(document))

    size = %{
      "mandatory_bytes" => bytes,
      "optional_bytes" => 0,
      "total_bytes" => bytes,
      "byte_budget" => byte_budget,
      "token_estimate" => nil
    }

    if size == previous do
      document
    else
      document
      |> Map.put("size", size)
      |> stabilize_mandatory_context_size(byte_budget, size, attempts + 1)
    end
  end

  defp stabilize_optional_context_size(
         document,
         byte_budget,
         mandatory_bytes,
         previous \\ nil,
         attempts \\ 0
       )

  defp stabilize_optional_context_size(
         _document,
         _byte_budget,
         _mandatory_bytes,
         _previous,
         attempts
       )
       when attempts >= 16,
       do: rollback(:context_budget_exceeded)

  defp stabilize_optional_context_size(document, byte_budget, mandatory_bytes, previous, attempts) do
    bytes = byte_size(Jason.encode!(document))

    size = %{
      "mandatory_bytes" => mandatory_bytes,
      "optional_bytes" => max(bytes - mandatory_bytes, 0),
      "total_bytes" => bytes,
      "byte_budget" => byte_budget,
      "token_estimate" => nil
    }

    if size == previous do
      document
    else
      document
      |> Map.put("size", size)
      |> stabilize_optional_context_size(byte_budget, mandatory_bytes, size, attempts + 1)
    end
  end

  defp context_content_hash(document) when is_map(document) do
    document
    |> Map.delete("content_hash")
    |> RequestHash.canonical()
    |> context_digest()
  end

  @doc false
  def context_digest(<<_::binary-size(32)>> = digest),
    do: "sha256:" <> Base.encode16(digest, case: :lower)

  def context_digest(value) when is_binary(value) do
    if Regex.match?(~r/^sha256:[0-9a-f]{64}$/, value), do: value, else: nil
  end

  def context_digest(_value), do: nil

  defp context_decisions(goal_id, revision) do
    Repo.all(
      from(decision in GoalDecision,
        where: decision.goal_id == ^goal_id and decision.goal_revision == ^revision,
        order_by: [asc: decision.inserted_at, asc: decision.id],
        select: %{id: decision.id, action_hash: decision.action_hash, state: decision.state}
      )
    )
    |> Enum.map(fn decision ->
      %{
        "decision_id" => decision.id,
        "action_hash" => context_digest(decision.action_hash),
        "state" => decision.state
      }
    end)
  end

  defp context_evidence(goal_id, revision, work_item_id, subject) do
    subject_hash = RequestHash.canonical(subject)

    Repo.all(
      from(evidence in RunEvidence,
        join: run in Run,
        on: run.id == evidence.run_id,
        join: task in Task,
        on: task.id == run.task_id,
        where:
          task.goal_id == ^goal_id and task.goal_revision == ^revision and
            task.work_item_id == ^work_item_id and
            task.purpose == "validate" and task.state == "completed" and
            run.generation == task.current_generation and run.state == "completed" and
            evidence.verdict == "passed" and evidence.subject_hash == ^subject_hash,
        order_by: [asc: evidence.inserted_at, asc: evidence.id],
        select: %{
          id: evidence.id,
          subject_hash: evidence.subject_hash,
          verdict: evidence.verdict,
          payload: evidence.payload
        }
      )
    )
    |> Enum.map(fn evidence ->
      %{
        "evidence_id" => evidence.id,
        "predicate_id" => value(evidence.payload || %{}, "predicate_id"),
        "subject_hash" => context_digest(evidence.subject_hash),
        "verdict" => evidence.verdict
      }
    end)
  end

  defp context_failed_attempts(goal_id, revision, work_item_id) do
    Repo.all(
      from(run in Run,
        join: task in Task,
        on: task.id == run.task_id,
        where:
          task.goal_id == ^goal_id and task.goal_revision == ^revision and
            task.work_item_id == ^work_item_id and
            run.state == "failed",
        order_by: [asc: run.inserted_at, asc: run.id],
        select: %{task_id: task.id, run_id: run.id, inserted_at: run.inserted_at}
      )
    )
    |> Enum.map(fn run ->
      %{
        "task_id" => run.task_id,
        "run_id" => run.run_id,
        "reason" => "run_failed",
        "observed_at" => DateTime.to_iso8601(run.inserted_at)
      }
    end)
  end

  @doc false

  def canonical_authority_policy(policy) do
    policy
    |> normalize_map()
    |> Map.put_new("operator_required_for_scope_change", true)
    |> Map.put_new("operator_required_for_completion", true)
    |> Map.put_new("publication_allowed", false)
    |> Map.put_new("allowed_actions", [])
  end

  @doc false

  def canonical_context_manifest(manifest) do
    manifest
    |> normalize_map()
    |> Map.put_new("byte_budget", 32_768)
    |> Map.put_new("required_source_kinds", ["repository"])
    |> Map.put_new("include_advisory_recall", false)
  end
end
