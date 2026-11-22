defmodule SymmetryControl.Goals.Handoff do
  @moduledoc false

  import Ecto.Query
  import SymmetryControl.Goals.ContractValidation, only: [validate_contract: 3]

  import SymmetryControl.Goals.Internal,
    only: [
      lock_run!: 1,
      lock_task!: 1,
      optional_uuid!: 2,
      parse_subject!: 1,
      required_uuid!: 2,
      rollback: 1,
      settlement_mutation_id: 3,
      valid_uuid?: 1,
      value: 2,
      value: 3
    ]

  alias SymmetryControl.Goals.ContextAssembly
  alias SymmetryControl.Goals.GoalEvent
  alias SymmetryControl.Goals.HarnessSession
  alias SymmetryControl.Orchestration.Run
  alias SymmetryControl.Orchestration.Runtime
  alias SymmetryControl.Orchestration.Task
  alias SymmetryControl.Repo
  alias SymmetryControl.RequestHash

  @doc false

  # A handoff is a new admission that receives only a source Run identity. The
  # caller never chooses its Subject or native session handle.
  def handoff_source!(
        _goal,
        _target_work_item_id,
        _repository_resource_id,
        _payload,
        "fresh",
        _opts
      ),
      do: nil

  def handoff_source!(
        _goal,
        _target_work_item_id,
        _repository_resource_id,
        _payload,
        "resume",
        _opts
      ),
      do: nil

  def handoff_source!(
        goal,
        target_work_item_id,
        repository_resource_id,
        payload,
        "handoff",
        opts
      ) do
    source_run_id = required_uuid!(payload, :handoff_source_run_id)

    source_task_id =
      Repo.one(from(source in Run, where: source.id == ^source_run_id, select: source.task_id)) ||
        rollback(:handoff_source_not_found)

    source_task = lock_task!(source_task_id)
    source_run = lock_run!(source_run_id)

    if Repo.one(
         from(target in Task,
           where: target.handoff_source_run_id == ^source_run_id,
           select: target.id,
           lock: "FOR UPDATE"
         )
       ),
       do: rollback(:handoff_source_consumed)

    unless source_run.task_id == source_task.id and source_task.goal_id == goal.id and
             source_task.goal_revision == goal.current_revision and
             source_task.work_item_id == target_work_item_id and
             not is_nil(target_work_item_id) and source_task.purpose != "plan" and
             source_task.state == "completed" and
             source_task.current_generation == source_run.generation and
             source_run.state == "completed" do
      rollback(:handoff_source_ineligible)
    end

    ensure_handoff_source_is_current_tip!(goal, source_task)

    task_result = value(source_run.result || %{}, "task_result")

    unless source_task.result == source_run.result and is_map(task_result) and
             validate_contract(:task_result, task_result, opts) == :ok and
             value(task_result, "kind") in ["progress", "candidate_completion"],
           do: rollback(:handoff_source_ineligible)

    subject = parse_subject!(value(task_result, "subject"))
    subject_hash = RequestHash.canonical(subject)

    unless subject["resource_id"] == repository_resource_id and
             value(task_result, "subject_hash") == ContextAssembly.context_digest(subject_hash),
           do: rollback(:handoff_source_subject_mismatch)

    evidence_refs = handoff_evidence_refs!(value(task_result, "evidence_refs"))

    settlement =
      handoff_source_settlement!(
        goal,
        source_task,
        source_run,
        task_result,
        subject_hash,
        evidence_refs
      )

    unless {value(task_result, "kind"), settlement} in [
             {"progress", "progress"},
             {"candidate_completion", "awaiting_validation"}
           ],
           do: rollback(:handoff_source_ineligible)

    %{
      run_id: source_run.id,
      task_id: source_task.id,
      result_id: required_uuid!(task_result, :result_id),
      result_kind: value(task_result, "kind"),
      subject: subject,
      subject_hash: subject_hash,
      evidence_ids: evidence_refs
    }
  end

  @doc false

  def handoff_admission_subject!(_goal, _item, "validate", payload, source, _opts) do
    producer_id = optional_uuid!(payload, :validation_of_task_id) || rollback(:invalid_validation)

    unless producer_id == source.task_id and source.result_kind == "candidate_completion",
      do: rollback(:handoff_source_ineligible)

    source.subject
  end

  def handoff_admission_subject!(_goal, _item, _purpose, _payload, source, _opts),
    do: source.subject

  defp ensure_handoff_source_is_current_tip!(goal, source_task) do
    tip =
      Repo.one(
        from(task in Task,
          where:
            task.goal_id == ^goal.id and task.goal_revision == ^goal.current_revision and
              task.work_item_id == ^source_task.work_item_id and task.state == "completed" and
              task.current_generation > 0,
          order_by: [desc: task.inserted_at, desc: task.id],
          limit: 1,
          lock: "FOR UPDATE"
        )
      )

    unless tip && tip.id == source_task.id, do: rollback(:handoff_source_stale)
  end

  defp handoff_evidence_refs!(evidence_refs) when is_list(evidence_refs) do
    if Enum.all?(evidence_refs, &valid_uuid?/1) and
         length(evidence_refs) == length(Enum.uniq(evidence_refs)) do
      Enum.sort(evidence_refs)
    else
      rollback(:handoff_source_ineligible)
    end
  end

  defp handoff_evidence_refs!(_evidence_refs), do: rollback(:handoff_source_ineligible)

  defp handoff_source_settlement!(
         goal,
         source_task,
         source_run,
         task_result,
         subject_hash,
         evidence_refs
       ) do
    mutation_id = settlement_mutation_id(source_task.id, source_run.id, source_run.generation)

    event =
      Repo.one(
        from(event in GoalEvent,
          where: event.goal_id == ^goal.id and event.id == ^mutation_id,
          lock: "FOR UPDATE"
        )
      ) || rollback(:handoff_source_ineligible)

    expected_body = %{
      "task_id" => source_task.id,
      "run_id" => source_run.id,
      "generation" => source_run.generation,
      "state" => source_run.state,
      "goal_revision" => source_task.goal_revision
    }

    response = event.response || %{}

    unless event.kind == "task_settled" and event.revision == source_task.goal_revision and
             event.actor_ref == "system:settlement" and event.payload == expected_body and
             value(response, "goal_id") == goal.id and
             value(response, "goal_revision") == source_task.goal_revision and
             value(response, "mutation_id") == mutation_id and
             value(response, "task_id") == source_task.id and
             value(response, "run_id") == source_run.id and
             value(response, "generation") == source_run.generation and
             value(response, "result_id") == value(task_result, "result_id") and
             value(response, "result_kind") == value(task_result, "kind") and
             value(response, "subject_hash") == ContextAssembly.context_digest(subject_hash) and
             handoff_evidence_refs!(value(response, "evidence_refs")) == evidence_refs,
           do: rollback(:handoff_source_ineligible)

    value(response, "settlement")
  end

  @doc false

  def admission_session_mode!(payload, requested_session_id) do
    mode =
      value(payload, :session_mode, if(is_nil(requested_session_id), do: "fresh", else: "resume"))

    unless mode in ["fresh", "resume", "handoff"], do: rollback(:invalid_request)
    if mode == "fresh" and not is_nil(requested_session_id), do: rollback(:invalid_request)

    if mode == "resume" and is_nil(requested_session_id),
      do: rollback(:requested_session_required)

    if mode == "handoff" and not is_nil(requested_session_id), do: rollback(:invalid_request)

    mode
  end

  @doc false

  def ensure_requested_session_available!(_item, nil, "fresh"), do: :ok
  def ensure_requested_session_available!(_item, nil, "handoff"), do: :ok

  def ensure_requested_session_available!(item, requested_session_id, session_mode)
      when session_mode == "resume" do
    untrusted_session =
      Repo.get(HarnessSession, requested_session_id) || rollback(:requested_session_not_found)

    runtime =
      Repo.one(
        from(runtime in Runtime,
          where: runtime.id == ^untrusted_session.runtime_id,
          lock: "FOR UPDATE"
        )
      ) || rollback(:requested_session_unavailable)

    session =
      Repo.one(
        from(session in HarnessSession,
          where: session.id == ^requested_session_id,
          lock: "FOR UPDATE"
        )
      ) || rollback(:requested_session_not_found)

    unless session.state == "available" and session.binding_verified == true and
             is_nil(session.active_run_id) and
             session.repository_resource_id == item.repository_resource_id and
             session.runtime_id == runtime.id and
             runtime.repository_resource_id == item.repository_resource_id and
             session.harness_kind == runtime.harness_kind and
             session.harness_version == runtime.harness_version and
             session.adapter_version == runtime.adapter_version and
             native_resume_capable?(runtime.capabilities) do
      rollback(:requested_session_unavailable)
    end
  end

  @doc false

  def ensure_plan_requested_session_available!(_resource, nil, "fresh"), do: :ok
  def ensure_plan_requested_session_available!(_resource, nil, "handoff"), do: :ok

  def ensure_plan_requested_session_available!(resource, requested_session_id, "resume") do
    untrusted_session =
      Repo.get(HarnessSession, requested_session_id) || rollback(:requested_session_not_found)

    runtime =
      Repo.one(
        from(runtime in Runtime,
          where: runtime.id == ^untrusted_session.runtime_id,
          lock: "FOR UPDATE"
        )
      ) || rollback(:requested_session_unavailable)

    session =
      Repo.one(
        from(session in HarnessSession,
          where: session.id == ^requested_session_id,
          lock: "FOR UPDATE"
        )
      ) || rollback(:requested_session_not_found)

    unless session.state == "available" and session.binding_verified == true and
             is_nil(session.active_run_id) and
             session.repository_resource_id == resource.id and session.runtime_id == runtime.id and
             runtime.repository_resource_id == resource.id and
             session.harness_kind == runtime.harness_kind and
             session.harness_version == runtime.harness_version and
             session.adapter_version == runtime.adapter_version and
             native_resume_capable?(runtime.capabilities) do
      rollback(:requested_session_unavailable)
    end
  end

  defp native_resume_capable?(capabilities) do
    value(capabilities, "adapter", %{})
    |> value("operations", %{})
    |> value("resume") == true
  end
end
