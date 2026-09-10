defmodule SymmetryControl.Goals.Goal do
  use Ecto.Schema
  import Ecto.Changeset

  alias SymmetryControl.Goals.{GoalDecision, GoalEvent, GoalRevision, WorkDependency, WorkOutcome}
  alias SymmetryControl.Workspaces.{Project, WorkItem}

  @states ["draft", "active", "paused", "achieved", "cancelled"]
  @terminal_states ["achieved", "cancelled"]
  @terminal_authority_fields [:state, :current_revision, :project_id]

  @primary_key {:id, :binary_id, autogenerate: true}
  @foreign_key_type :binary_id
  schema "goals" do
    belongs_to(:project, Project)
    field(:title, :string)
    field(:state, :string, default: "draft")
    field(:current_revision, :integer)
    field(:event_sequence, :integer, default: 0)
    field(:next_wake_at, :utc_datetime_usec)
    field(:lock_version, :integer, default: 1)

    has_many(:revisions, GoalRevision)
    has_many(:work_items, WorkItem)
    has_many(:dependencies, WorkDependency)
    has_many(:outcomes, WorkOutcome)
    has_many(:decisions, GoalDecision)
    has_many(:events, GoalEvent)

    timestamps(type: :utc_datetime_usec)
  end

  def changeset(goal, attrs) do
    goal
    |> cast(attrs, [
      :project_id,
      :title,
      :state,
      :current_revision,
      :event_sequence,
      :next_wake_at
    ])
    |> update_change(:title, &trim/1)
    |> validate_required([:project_id, :title, :state, :current_revision, :event_sequence])
    |> validate_length(:title, min: 1, max: 240)
    |> validate_inclusion(:state, @states)
    |> validate_number(:current_revision, greater_than: 0)
    |> validate_number(:event_sequence, greater_than_or_equal_to: 0)
    |> reject_terminal_authority_mutation()
    |> validate_terminal_next_wake_at()
    |> assoc_constraint(:project)
    |> check_constraint(:state, name: :goals_state_check)
    |> check_constraint(:current_revision, name: :goals_current_revision_positive)
    |> check_constraint(:event_sequence, name: :goals_event_sequence_nonnegative)
  end

  def transition_changeset(goal, attrs) do
    goal
    |> cast(attrs, [:state, :next_wake_at])
    |> validate_required([:state])
    |> validate_inclusion(:state, @states)
    |> reject_terminal_authority_mutation()
    |> validate_terminal_next_wake_at()
    |> check_constraint(:state, name: :goals_state_check)
    |> optimistic_lock(:lock_version)
  end

  defp trim(value) when is_binary(value), do: String.trim(value)
  defp trim(value), do: value

  defp reject_terminal_authority_mutation(changeset) do
    if changeset.data.state in @terminal_states do
      Enum.reduce(@terminal_authority_fields, changeset, fn field, acc ->
        if Map.has_key?(acc.changes, field) do
          add_error(acc, field, "cannot be changed after Goal terminalization")
        else
          acc
        end
      end)
    else
      changeset
    end
  end

  defp validate_terminal_next_wake_at(changeset) do
    if get_field(changeset, :state) in @terminal_states and
         not is_nil(get_field(changeset, :next_wake_at)) do
      add_error(changeset, :next_wake_at, "must be absent for a terminal Goal")
    else
      changeset
    end
  end
end

defmodule SymmetryControl.Goals.JsonDocument do
  @behaviour Ecto.Type

  @impl Ecto.Type
  def type, do: :map

  @impl Ecto.Type
  def cast(value) when is_map(value) or is_list(value), do: {:ok, value}
  def cast(_value), do: :error

  @impl Ecto.Type
  def load(value) when is_map(value) or is_list(value), do: {:ok, value}
  def load(_value), do: :error

  @impl Ecto.Type
  def dump(value) when is_map(value) or is_list(value), do: {:ok, value}
  def dump(_value), do: :error

  @impl Ecto.Type
  def embed_as(_format), do: :self

  @impl Ecto.Type
  def equal?(left, right), do: left == right
end

defmodule SymmetryControl.Goals.GoalRevision do
  use Ecto.Schema
  import Ecto.Changeset

  alias SymmetryControl.Goals.{Goal, JsonDocument}

  @execution_policy_keys [
    "automatic_execution",
    "max_parallel_tasks",
    "max_task_admissions",
    "max_run_attempts_per_task",
    "budget_limit_microusd",
    "per_run_cost_limit_microusd",
    "budget_mode",
    "hard_cost_limit_required",
    "allowed_runtime_ids",
    "allowed_model_profiles",
    "final_acceptance",
    "allowed_actions",
    "allowed_resource_ids"
  ]
  @positive_integer_keys [
    "max_parallel_tasks",
    "max_task_admissions",
    "max_run_attempts_per_task"
  ]
  @max_safe_integer 9_007_199_254_740_991
  @max_microusd 9_223_372_036_854_775_807

  @primary_key false
  @foreign_key_type :binary_id
  schema "goal_revisions" do
    belongs_to(:goal, Goal, primary_key: true)
    field(:revision, :integer, primary_key: true)
    field(:objective, :string)
    field(:non_goals, JsonDocument, default: [])
    field(:acceptance_contract, :map)
    field(:authority_policy, :map)
    field(:execution_policy, :map)
    field(:context_manifest, :map)
    field(:reason, :string)
    field(:actor_ref, :string)

    timestamps(type: :utc_datetime_usec, updated_at: false)
  end

  def changeset(revision, attrs) do
    revision
    |> cast(attrs, [
      :goal_id,
      :revision,
      :objective,
      :non_goals,
      :acceptance_contract,
      :authority_policy,
      :execution_policy,
      :context_manifest,
      :reason,
      :actor_ref
    ])
    |> update_change(:objective, &trim/1)
    |> validate_required([
      :goal_id,
      :revision,
      :objective,
      :non_goals,
      :acceptance_contract,
      :authority_policy,
      :execution_policy,
      :context_manifest,
      :reason,
      :actor_ref
    ])
    |> validate_number(:revision, greater_than: 0)
    |> validate_length(:objective, min: 1, max: 20_000)
    |> validate_length(:reason, min: 1, max: 4_000)
    |> validate_length(:actor_ref, max: 500)
    |> validate_non_goals()
    |> validate_map(:acceptance_contract)
    |> validate_map(:authority_policy)
    |> validate_map(:execution_policy)
    |> validate_execution_policy()
    |> validate_map(:context_manifest)
    |> assoc_constraint(:goal)
    |> check_constraint(:revision, name: :goal_revisions_revision_positive)
    |> check_constraint(:objective, name: :goal_revisions_objective_present)
    |> check_constraint(:non_goals, name: :goal_revisions_non_goals_array)
    |> check_constraint(:acceptance_contract, name: :goal_revisions_contracts_are_objects)
    |> check_constraint(:execution_policy, name: :goal_revisions_execution_policy_v1)
  end

  def immutable_changeset(revision, attrs) do
    revision
    |> change()
    |> cast(attrs, [
      :goal_id,
      :revision,
      :objective,
      :non_goals,
      :acceptance_contract,
      :authority_policy,
      :execution_policy,
      :context_manifest,
      :reason,
      :actor_ref
    ])
    |> reject_changes(:goal_revision)
  end

  defp validate_non_goals(changeset) do
    validate_change(changeset, :non_goals, fn :non_goals, non_goals ->
      if Enum.all?(non_goals, &(is_binary(&1) and String.trim(&1) != "")),
        do: [],
        else: [non_goals: "must contain non-blank strings"]
    end)
  end

  defp validate_map(changeset, field) do
    validate_change(changeset, field, fn ^field, value ->
      if is_map(value), do: [], else: [{field, "must be an object"}]
    end)
  end

  defp validate_execution_policy(changeset) do
    validate_change(changeset, :execution_policy, fn :execution_policy, policy ->
      execution_policy_errors(policy)
    end)
  end

  defp execution_policy_errors(policy) when is_map(policy) do
    errors = []

    errors =
      if MapSet.new(Map.keys(policy)) == MapSet.new(@execution_policy_keys) do
        errors
      else
        [{:execution_policy, "must contain exactly the v1 execution policy keys"} | errors]
      end

    errors =
      if Enum.all?(@positive_integer_keys, &positive_safe_integer?(Map.get(policy, &1))) do
        errors
      else
        [{:execution_policy, "must use positive safe integers for execution limits"} | errors]
      end

    errors =
      if nullable_microusd?(Map.get(policy, "budget_limit_microusd")) and
           nullable_microusd?(Map.get(policy, "per_run_cost_limit_microusd")) do
        errors
      else
        [{:execution_policy, "must use nullable non-negative signed 64-bit cost limits"} | errors]
      end

    errors =
      if is_boolean(Map.get(policy, "automatic_execution")) and
           is_boolean(Map.get(policy, "hard_cost_limit_required")) do
        errors
      else
        [{:execution_policy, "must use booleans for execution flags"} | errors]
      end

    errors =
      if Map.get(policy, "budget_mode") in ["soft", "strict"] and
           Map.get(policy, "final_acceptance") in ["operator", "deterministic"] do
        errors
      else
        [{:execution_policy, "contains an unsupported execution mode"} | errors]
      end

    errors =
      if uuid_array?(Map.get(policy, "allowed_runtime_ids")) and
           uuid_array?(Map.get(policy, "allowed_resource_ids")) and
           nonblank_string_array?(Map.get(policy, "allowed_model_profiles")) and
           nonblank_string_array?(Map.get(policy, "allowed_actions")) do
        errors
      else
        [{:execution_policy, "contains invalid allowed-value arrays"} | errors]
      end

    errors =
      if Map.get(policy, "automatic_execution") == true and
           is_nil(Map.get(policy, "budget_limit_microusd")) do
        [{:execution_policy, "automatic execution requires a total budget"} | errors]
      else
        errors
      end

    if Map.get(policy, "budget_mode") == "strict" and
         (is_nil(Map.get(policy, "per_run_cost_limit_microusd")) or
            Map.get(policy, "hard_cost_limit_required") != true) do
      [{:execution_policy, "strict execution requires a per-run ceiling and hard limit"} | errors]
    else
      errors
    end
  end

  defp execution_policy_errors(_policy),
    do: [execution_policy: "must be an object"]

  defp positive_safe_integer?(value),
    do: is_integer(value) and value >= 1 and value <= @max_safe_integer

  defp nullable_microusd?(nil), do: true
  defp nullable_microusd?(value), do: is_integer(value) and value >= 0 and value <= @max_microusd

  defp uuid_array?(values) when is_list(values), do: Enum.all?(values, &valid_uuid?/1)
  defp uuid_array?(_values), do: false

  defp nonblank_string_array?(values) when is_list(values),
    do: Enum.all?(values, &nonblank_string?/1)

  defp nonblank_string_array?(_values), do: false

  defp valid_uuid?(value) when is_binary(value), do: match?({:ok, _}, Ecto.UUID.cast(value))
  defp valid_uuid?(_value), do: false

  defp nonblank_string?(value) when is_binary(value), do: String.trim(value) != ""
  defp nonblank_string?(_value), do: false

  defp reject_changes(changeset, label) do
    Enum.reduce(changeset.changes, changeset, fn {field, _}, acc ->
      add_error(acc, field, "#{label} is immutable")
    end)
  end

  defp trim(value) when is_binary(value), do: String.trim(value)
  defp trim(value), do: value
end

defmodule SymmetryControl.Goals.WorkDependency do
  use Ecto.Schema
  import Ecto.Changeset

  alias SymmetryControl.Goals.Goal
  alias SymmetryControl.Workspaces.WorkItem

  @primary_key false
  @foreign_key_type :binary_id
  schema "work_dependencies" do
    belongs_to(:goal, Goal)
    belongs_to(:work_item, WorkItem, primary_key: true)
    belongs_to(:depends_on, WorkItem, primary_key: true)

    timestamps(type: :utc_datetime_usec, updated_at: false)
  end

  def changeset(dependency, attrs) do
    dependency
    |> cast(attrs, [:goal_id, :work_item_id, :depends_on_id])
    |> validate_required([:goal_id, :work_item_id, :depends_on_id])
    |> validate_distinct_items()
    |> assoc_constraint(:goal)
    |> foreign_key_constraint(:work_item_id, name: :work_dependencies_work_item_ownership_fkey)
    |> foreign_key_constraint(:depends_on_id, name: :work_dependencies_depends_on_ownership_fkey)
    |> check_constraint(:depends_on_id, name: :work_dependencies_distinct_items)
  end

  def immutable_changeset(dependency, attrs) do
    dependency
    |> change()
    |> cast(attrs, [:goal_id, :work_item_id, :depends_on_id])
    |> reject_changes()
  end

  defp validate_distinct_items(changeset) do
    if get_field(changeset, :work_item_id) == get_field(changeset, :depends_on_id) do
      add_error(changeset, :depends_on_id, "must differ from work item")
    else
      changeset
    end
  end

  defp reject_changes(changeset) do
    Enum.reduce(changeset.changes, changeset, fn {field, _}, acc ->
      add_error(acc, field, "work dependency is immutable")
    end)
  end
end

defmodule SymmetryControl.Goals.ContextSnapshot do
  use Ecto.Schema
  import Ecto.Changeset

  alias SymmetryControl.Goals.Goal
  alias SymmetryControl.Workspaces.WorkItem

  @primary_key {:id, :binary_id, autogenerate: true}
  @foreign_key_type :binary_id
  schema "context_snapshots" do
    belongs_to(:goal, Goal)
    belongs_to(:work_item, WorkItem)
    field(:goal_revision, :integer)
    field(:schema_version, :integer)
    field(:content_hash, :binary)
    field(:payload, :map)

    timestamps(type: :utc_datetime_usec, updated_at: false)
  end

  def changeset(snapshot, attrs) do
    snapshot
    |> cast(attrs, [
      :goal_id,
      :goal_revision,
      :work_item_id,
      :schema_version,
      :content_hash,
      :payload
    ])
    |> validate_required([
      :goal_id,
      :goal_revision,
      :schema_version,
      :content_hash,
      :payload
    ])
    |> validate_number(:goal_revision, greater_than: 0)
    |> validate_number(:schema_version, greater_than: 0)
    |> validate_digest(:content_hash)
    |> validate_map(:payload)
    |> foreign_key_constraint(:goal_id, name: :context_snapshots_goal_revision_fkey)
    |> foreign_key_constraint(:work_item_id, name: :context_snapshots_work_item_ownership_fkey)
    |> check_constraint(:schema_version, name: :context_snapshots_schema_version_positive)
    |> check_constraint(:content_hash, name: :context_snapshots_content_hash_size)
    |> check_constraint(:payload, name: :context_snapshots_payload_object)
  end

  def immutable_changeset(snapshot, attrs) do
    snapshot
    |> change()
    |> cast(attrs, [
      :goal_id,
      :goal_revision,
      :work_item_id,
      :schema_version,
      :content_hash,
      :payload
    ])
    |> reject_changes()
  end

  defp validate_digest(changeset, field) do
    validate_change(changeset, field, fn ^field, value ->
      if is_binary(value) and byte_size(value) == 32,
        do: [],
        else: [{field, "must be a 32-byte digest"}]
    end)
  end

  defp validate_map(changeset, field) do
    validate_change(changeset, field, fn ^field, value ->
      if is_map(value), do: [], else: [{field, "must be an object"}]
    end)
  end

  defp reject_changes(changeset) do
    Enum.reduce(changeset.changes, changeset, fn {field, _}, acc ->
      add_error(acc, field, "context snapshot is immutable")
    end)
  end
end

defmodule SymmetryControl.Goals.HarnessSession do
  use Ecto.Schema
  import Ecto.Changeset

  alias SymmetryControl.Orchestration.{Machine, Run, Runtime}
  alias SymmetryControl.Workspaces.ProjectResource

  @states ["available", "busy", "unavailable", "closed"]

  @primary_key {:id, :binary_id, autogenerate: true}
  @foreign_key_type :binary_id
  schema "harness_sessions" do
    belongs_to(:machine, Machine)
    belongs_to(:runtime, Runtime)
    belongs_to(:repository_resource, ProjectResource)
    belongs_to(:active_run, Run)
    field(:harness_kind, :string)
    field(:harness_version, :string)
    field(:adapter_version, :string)
    field(:local_handle_id, Ecto.UUID)
    field(:workspace_fingerprint, :string)
    field(:state, :string, default: "available")
    field(:lock_version, :integer, default: 1)

    timestamps(type: :utc_datetime_usec)
  end

  def changeset(session, attrs) do
    session
    |> cast(attrs, [
      :machine_id,
      :runtime_id,
      :repository_resource_id,
      :harness_kind,
      :harness_version,
      :adapter_version,
      :local_handle_id,
      :workspace_fingerprint,
      :state,
      :active_run_id
    ])
    |> validate_required([
      :machine_id,
      :runtime_id,
      :repository_resource_id,
      :harness_kind,
      :harness_version,
      :adapter_version,
      :local_handle_id,
      :workspace_fingerprint,
      :state
    ])
    |> validate_length(:harness_kind, min: 1, max: 120)
    |> validate_length(:harness_version, min: 1, max: 240)
    |> validate_length(:adapter_version, min: 1, max: 240)
    |> validate_length(:workspace_fingerprint, min: 1, max: 1_000)
    |> validate_inclusion(:harness_kind, ["codex", "claude_code", "pi", "opencode"])
    |> validate_inclusion(:state, @states)
    |> validate_active_run()
    |> foreign_key_constraint(:runtime_id, name: :harness_sessions_runtime_machine_ownership_fkey)
    |> assoc_constraint(:repository_resource)
    |> assoc_constraint(:active_run)
    |> unique_constraint([:machine_id, :local_handle_id],
      name: :harness_sessions_machine_id_local_handle_id_key
    )
    |> unique_constraint(:active_run_id, name: :harness_sessions_active_run_id_key)
    |> check_constraint(:state, name: :harness_sessions_state_check)
    |> check_constraint(:harness_kind, name: :harness_sessions_harness_kind_check)
    |> check_constraint(:active_run_id, name: :harness_sessions_busy_active_run_check)
  end

  def update_changeset(session, attrs) do
    session
    |> cast(attrs, [:state, :active_run_id])
    |> validate_required([:state])
    |> validate_inclusion(:state, @states)
    |> validate_active_run()
    |> assoc_constraint(:active_run)
    |> check_constraint(:state, name: :harness_sessions_state_check)
    |> check_constraint(:active_run_id, name: :harness_sessions_busy_active_run_check)
    |> optimistic_lock(:lock_version)
  end

  defp validate_active_run(changeset) do
    busy? = get_field(changeset, :state) == "busy"
    active_run_id = get_field(changeset, :active_run_id)

    if busy? == is_nil(active_run_id) do
      add_error(changeset, :active_run_id, "must be present exactly when session is busy")
    else
      changeset
    end
  end
end

defmodule SymmetryControl.Goals.RunEvidence do
  use Ecto.Schema
  import Ecto.Changeset

  alias SymmetryControl.Orchestration.Run

  @kinds ["check", "artifact", "review", "observation"]
  @verdicts ["passed", "failed", "unknown", "not_applicable"]

  @primary_key {:id, :binary_id, autogenerate: true}
  @foreign_key_type :binary_id
  schema "run_evidence" do
    belongs_to(:run, Run)
    field(:evidence_key, :string)
    field(:kind, :string)
    field(:subject_hash, :binary)
    field(:source_ref, :map)
    field(:source_revision, :string)
    field(:validator_profile, :string)
    field(:verdict, :string)
    field(:payload, :map)

    timestamps(type: :utc_datetime_usec, updated_at: false)
  end

  def changeset(evidence, attrs) do
    evidence
    |> cast(attrs, [
      :run_id,
      :evidence_key,
      :kind,
      :subject_hash,
      :source_ref,
      :source_revision,
      :validator_profile,
      :verdict,
      :payload
    ])
    |> validate_required([
      :run_id,
      :evidence_key,
      :kind,
      :subject_hash,
      :source_ref,
      :source_revision,
      :verdict,
      :payload
    ])
    |> validate_length(:evidence_key, min: 1, max: 240)
    |> validate_length(:source_revision, min: 1, max: 1_000)
    |> validate_length(:validator_profile, max: 240)
    |> validate_inclusion(:kind, @kinds)
    |> validate_inclusion(:verdict, @verdicts)
    |> validate_digest(:subject_hash)
    |> validate_map(:source_ref)
    |> validate_map(:payload)
    |> assoc_constraint(:run)
    |> unique_constraint([:run_id, :evidence_key])
    |> check_constraint(:kind, name: :run_evidence_kind_check)
    |> check_constraint(:verdict, name: :run_evidence_verdict_check)
    |> check_constraint(:subject_hash, name: :run_evidence_subject_hash_size)
    |> check_constraint(:source_ref, name: :run_evidence_payloads_are_objects)
  end

  def immutable_changeset(evidence, attrs) do
    evidence
    |> change()
    |> cast(attrs, [
      :run_id,
      :evidence_key,
      :kind,
      :subject_hash,
      :source_ref,
      :source_revision,
      :validator_profile,
      :verdict,
      :payload
    ])
    |> reject_changes()
  end

  defp validate_digest(changeset, field) do
    validate_change(changeset, field, fn ^field, value ->
      if is_binary(value) and byte_size(value) == 32,
        do: [],
        else: [{field, "must be a 32-byte digest"}]
    end)
  end

  defp validate_map(changeset, field) do
    validate_change(changeset, field, fn ^field, value ->
      if is_map(value), do: [], else: [{field, "must be an object"}]
    end)
  end

  defp reject_changes(changeset) do
    Enum.reduce(changeset.changes, changeset, fn {field, _}, acc ->
      add_error(acc, field, "run evidence is immutable")
    end)
  end
end

defmodule SymmetryControl.Goals.WorkOutcome do
  use Ecto.Schema
  import Ecto.Changeset

  alias SymmetryControl.Goals.{Goal, JsonDocument}
  alias SymmetryControl.Orchestration.{Run, Task}
  alias SymmetryControl.Workspaces.WorkItem

  @dispositions ["accepted", "rejected"]

  @primary_key {:id, :binary_id, autogenerate: true}
  @foreign_key_type :binary_id
  schema "work_outcomes" do
    belongs_to(:goal, Goal)
    belongs_to(:work_item, WorkItem)
    belongs_to(:producing_task, Task)
    belongs_to(:producing_run, Run)
    belongs_to(:validation_task, Task)
    field(:goal_revision, :integer)
    field(:candidate_subject, JsonDocument)
    field(:subject_hash, :binary)
    field(:producing_result_id, Ecto.UUID)
    field(:evidence_ids, JsonDocument, default: [])
    field(:disposition, :string)
    field(:reason, :string)
    field(:decision_id, Ecto.UUID)

    timestamps(type: :utc_datetime_usec, updated_at: false)
  end

  def changeset(outcome, attrs) do
    outcome
    |> cast(attrs, [
      :goal_id,
      :goal_revision,
      :work_item_id,
      :producing_task_id,
      :producing_run_id,
      :validation_task_id,
      :candidate_subject,
      :subject_hash,
      :producing_result_id,
      :evidence_ids,
      :disposition,
      :reason,
      :decision_id
    ])
    |> validate_required([
      :goal_id,
      :goal_revision,
      :work_item_id,
      :producing_task_id,
      :producing_run_id,
      :candidate_subject,
      :subject_hash,
      :producing_result_id,
      :evidence_ids,
      :disposition,
      :reason
    ])
    |> validate_number(:goal_revision, greater_than: 0)
    |> validate_length(:reason, min: 1, max: 4_000)
    |> validate_inclusion(:disposition, @dispositions)
    |> validate_change(:candidate_subject, fn :candidate_subject, value ->
      if is_map(value), do: [], else: [candidate_subject: "must be an object"]
    end)
    |> validate_digest(:subject_hash)
    |> validate_evidence_ids()
    |> foreign_key_constraint(:goal_id, name: :work_outcomes_goal_revision_fkey)
    |> foreign_key_constraint(:work_item_id, name: :work_outcomes_work_item_ownership_fkey)
    |> foreign_key_constraint(:producing_task_id,
      name: :work_outcomes_producing_task_identity_fkey
    )
    |> foreign_key_constraint(:producing_run_id, name: :work_outcomes_producing_run_task_fkey)
    |> foreign_key_constraint(:validation_task_id,
      name: :work_outcomes_validation_task_identity_fkey
    )
    |> foreign_key_constraint(:decision_id, name: :work_outcomes_decision_identity_fkey)
    |> unique_constraint([:work_item_id, :goal_revision],
      name: :work_outcomes_one_accepted_per_revision
    )
    |> check_constraint(:disposition, name: :work_outcomes_disposition_check)
    |> check_constraint(:subject_hash, name: :work_outcomes_subject_hash_size)
    |> check_constraint(:evidence_ids, name: :work_outcomes_evidence_ids_array)
    |> check_constraint(:reason, name: :work_outcomes_reason_present)
  end

  def immutable_changeset(outcome, attrs) do
    outcome
    |> change()
    |> cast(attrs, [
      :goal_id,
      :goal_revision,
      :work_item_id,
      :producing_task_id,
      :producing_run_id,
      :validation_task_id,
      :candidate_subject,
      :subject_hash,
      :producing_result_id,
      :evidence_ids,
      :disposition,
      :reason,
      :decision_id
    ])
    |> reject_changes()
  end

  defp validate_digest(changeset, field) do
    validate_change(changeset, field, fn ^field, value ->
      if is_binary(value) and byte_size(value) == 32,
        do: [],
        else: [{field, "must be a 32-byte digest"}]
    end)
  end

  defp validate_evidence_ids(changeset) do
    validate_change(changeset, :evidence_ids, fn :evidence_ids, evidence_ids ->
      if is_list(evidence_ids) and Enum.all?(evidence_ids, &valid_uuid?/1),
        do: [],
        else: [evidence_ids: "must be an array of UUIDs"]
    end)
  end

  defp valid_uuid?(value) when is_binary(value), do: match?({:ok, _}, Ecto.UUID.cast(value))
  defp valid_uuid?(_value), do: false

  defp reject_changes(changeset) do
    Enum.reduce(changeset.changes, changeset, fn {field, _}, acc ->
      add_error(acc, field, "work outcome is immutable")
    end)
  end
end

defmodule SymmetryControl.Goals.GoalDecision do
  use Ecto.Schema
  import Ecto.Changeset

  alias SymmetryControl.Goals.{Goal, JsonDocument}
  alias SymmetryControl.Workspaces.WorkItem

  @kinds ["plan", "scope", "review", "budget", "external_action", "completion"]
  @states ["open", "resolved", "superseded"]
  @terminal_states ["resolved", "superseded"]

  @terminal_authority_fields [
    :goal_id,
    :goal_revision,
    :work_item_id,
    :kind,
    :action_hash,
    :subject_hash,
    :state,
    :question,
    :options,
    :resolution,
    :actor_ref,
    :expires_at
  ]

  @primary_key {:id, :binary_id, autogenerate: true}
  @foreign_key_type :binary_id
  schema "goal_decisions" do
    belongs_to(:goal, Goal)
    belongs_to(:work_item, WorkItem)
    field(:goal_revision, :integer)
    field(:kind, :string)
    field(:action_hash, :binary)
    field(:subject_hash, :binary)
    field(:state, :string, default: "open")
    field(:question, :string)
    field(:options, JsonDocument, default: [])
    field(:resolution, :map)
    field(:actor_ref, :string)
    field(:expires_at, :utc_datetime_usec)
    field(:lock_version, :integer, default: 1)

    timestamps(type: :utc_datetime_usec)
  end

  def changeset(decision, attrs) do
    decision
    |> cast(attrs, [
      :goal_id,
      :goal_revision,
      :work_item_id,
      :kind,
      :action_hash,
      :subject_hash,
      :state,
      :question,
      :options,
      :resolution,
      :actor_ref,
      :expires_at
    ])
    |> validate_required([
      :goal_id,
      :goal_revision,
      :kind,
      :action_hash,
      :state,
      :question,
      :options
    ])
    |> validate_number(:goal_revision, greater_than: 0)
    |> validate_length(:question, min: 1, max: 8_000)
    |> validate_length(:actor_ref, max: 500)
    |> validate_inclusion(:kind, @kinds)
    |> validate_inclusion(:state, @states)
    |> validate_digest(:action_hash)
    |> validate_optional_digest(:subject_hash)
    |> validate_options()
    |> validate_resolution_state()
    |> require_open_decision_creation()
    |> reject_terminal_authority_mutation()
    |> foreign_key_constraint(:goal_id, name: :goal_decisions_goal_revision_fkey)
    |> foreign_key_constraint(:work_item_id, name: :goal_decisions_work_item_ownership_fkey)
    |> unique_constraint([:goal_id, :goal_revision, :action_hash])
    |> check_constraint(:kind, name: :goal_decisions_kind_check)
    |> check_constraint(:state, name: :goal_decisions_state_resolution_check)
    |> check_constraint(:action_hash, name: :goal_decisions_action_hash_size)
    |> check_constraint(:subject_hash, name: :goal_decisions_subject_hash_size)
    |> check_constraint(:question, name: :goal_decisions_question_present)
    |> check_constraint(:options, name: :goal_decisions_options_array)
  end

  def resolve_changeset(decision, attrs) do
    decision
    |> cast(attrs, [:state, :resolution])
    |> reject_resolution_change()
    |> require_open_decision("resolved")
    |> validate_required([:state, :resolution])
    |> validate_inclusion(:state, ["resolved"])
    |> validate_change(:resolution, fn :resolution, resolution ->
      if is_map(resolution), do: [], else: [resolution: "must be an object"]
    end)
    |> optimistic_lock(:lock_version)
  end

  def supersede_changeset(decision) do
    decision
    |> change(state: "superseded")
    |> require_open_decision("superseded")
    |> validate_inclusion(:state, ["superseded"])
    |> optimistic_lock(:lock_version)
  end

  defp validate_digest(changeset, field) do
    validate_change(changeset, field, fn ^field, value ->
      if is_binary(value) and byte_size(value) == 32,
        do: [],
        else: [{field, "must be a 32-byte digest"}]
    end)
  end

  defp validate_optional_digest(changeset, field) do
    validate_change(changeset, field, fn ^field, value ->
      if is_nil(value) or (is_binary(value) and byte_size(value) == 32),
        do: [],
        else: [{field, "must be a 32-byte digest"}]
    end)
  end

  defp validate_options(changeset) do
    validate_change(changeset, :options, fn :options, options ->
      valid? =
        is_list(options) and options != [] and
          Enum.all?(options, fn option ->
            is_map(option) and
              Enum.all?(["id", "label", "consequence"], fn key ->
                is_binary(Map.get(option, key)) and String.trim(Map.get(option, key)) != ""
              end)
          end) and unique_option_ids?(options)

      if valid?, do: [], else: [options: "must contain id, label, and consequence records"]
    end)
  end

  defp unique_option_ids?(options) do
    option_ids = Enum.map(options, &Map.get(&1, "id"))
    length(option_ids) == MapSet.size(MapSet.new(option_ids))
  end

  defp validate_resolution_state(changeset) do
    state = get_field(changeset, :state)
    resolution = get_field(changeset, :resolution)

    cond do
      state == "open" and not is_nil(resolution) ->
        add_error(changeset, :resolution, "must be absent while decision is open")

      state == "resolved" and not is_map(resolution) ->
        add_error(changeset, :resolution, "must be present when decision is resolved")

      true ->
        changeset
    end
  end

  defp reject_resolution_change(changeset) do
    if not is_nil(changeset.data.resolution) and Map.has_key?(changeset.changes, :resolution) do
      add_error(changeset, :resolution, "cannot be changed once resolved")
    else
      changeset
    end
  end

  defp require_open_decision(changeset, target_state) do
    if changeset.data.state == "open" do
      changeset
    else
      add_error(changeset, :state, "only open decisions can be marked #{target_state}")
    end
  end

  defp require_open_decision_creation(changeset) do
    if changeset.data.__meta__.state == :built and get_field(changeset, :state) != "open" do
      add_error(changeset, :state, "new decisions must start open")
    else
      changeset
    end
  end

  defp reject_terminal_authority_mutation(changeset) do
    if changeset.data.state in @terminal_states do
      Enum.reduce(@terminal_authority_fields, changeset, fn field, acc ->
        if Map.has_key?(acc.changes, field) do
          add_error(acc, field, "cannot be changed after decision terminalization")
        else
          acc
        end
      end)
    else
      changeset
    end
  end
end

defmodule SymmetryControl.Goals.GoalEvent do
  use Ecto.Schema
  import Ecto.Changeset

  alias SymmetryControl.Goals.Goal

  @primary_key {:id, :binary_id, autogenerate: false}
  @foreign_key_type :binary_id
  schema "goal_events" do
    belongs_to(:goal, Goal)
    field(:sequence, :integer)
    field(:kind, :string)
    field(:actor_ref, :string)
    field(:request_hash, :binary)
    field(:request_hash_version, :integer, default: 1)
    field(:revision, :integer)
    field(:payload, :map)
    field(:response, :map)

    timestamps(type: :utc_datetime_usec, updated_at: false)
  end

  def changeset(event, attrs) do
    event
    |> cast(attrs, [
      :id,
      :goal_id,
      :sequence,
      :kind,
      :actor_ref,
      :request_hash,
      :request_hash_version,
      :revision,
      :payload,
      :response
    ])
    |> validate_required([
      :id,
      :goal_id,
      :sequence,
      :kind,
      :actor_ref,
      :request_hash,
      :revision,
      :payload,
      :response
    ])
    |> validate_number(:sequence, greater_than: 0)
    |> validate_number(:revision, greater_than: 0)
    |> validate_length(:kind, min: 1, max: 120)
    |> validate_length(:actor_ref, max: 500)
    |> validate_number(:request_hash_version, greater_than: 0)
    |> validate_digest(:request_hash)
    |> validate_map(:payload)
    |> validate_map(:response)
    |> foreign_key_constraint(:goal_id, name: :goal_events_goal_revision_fkey)
    |> unique_constraint([:goal_id, :sequence])
    |> check_constraint(:sequence, name: :goal_events_sequence_positive)
    |> check_constraint(:revision, name: :goal_events_revision_positive)
    |> check_constraint(:request_hash, name: :goal_events_request_hash_size)
    |> check_constraint(:request_hash_version, name: :goal_events_request_hash_version_positive)
    |> check_constraint(:kind, name: :goal_events_kind_present)
    |> check_constraint(:payload, name: :goal_events_payloads_are_objects)
  end

  def immutable_changeset(event, attrs) do
    event
    |> change()
    |> cast(attrs, [
      :goal_id,
      :sequence,
      :kind,
      :actor_ref,
      :request_hash,
      :request_hash_version,
      :revision,
      :payload,
      :response
    ])
    |> reject_changes()
  end

  defp validate_digest(changeset, field) do
    validate_change(changeset, field, fn ^field, value ->
      if is_binary(value) and byte_size(value) == 32,
        do: [],
        else: [{field, "must be a 32-byte digest"}]
    end)
  end

  defp validate_map(changeset, field) do
    validate_change(changeset, field, fn ^field, value ->
      if is_map(value), do: [], else: [{field, "must be an object"}]
    end)
  end

  defp reject_changes(changeset) do
    Enum.reduce(changeset.changes, changeset, fn {field, _}, acc ->
      add_error(acc, field, "goal event is immutable")
    end)
  end
end

defmodule SymmetryControl.Goals.GoalBudgetReservation do
  use Ecto.Schema
  import Ecto.Changeset

  alias SymmetryControl.Goals.Goal
  alias SymmetryControl.Orchestration.Task

  @states ["held", "settled", "released", "unknown"]

  @primary_key {:id, :binary_id, autogenerate: true}
  @foreign_key_type :binary_id
  schema "goal_budget_reservations" do
    belongs_to(:goal, Goal)
    belongs_to(:task, Task)
    field(:goal_revision, :integer)
    field(:admission_key, Ecto.UUID)
    field(:reserved_microusd, :integer)
    field(:state, :string, default: "held")
    field(:lock_version, :integer, default: 1)

    timestamps(type: :utc_datetime_usec)
  end

  def changeset(reservation, attrs) do
    reservation
    |> cast(attrs, [
      :goal_id,
      :goal_revision,
      :task_id,
      :admission_key,
      :reserved_microusd,
      :state
    ])
    |> validate_required([:goal_id, :goal_revision, :task_id, :admission_key, :state])
    |> validate_number(:goal_revision, greater_than: 0)
    |> validate_number(:reserved_microusd, greater_than_or_equal_to: 0)
    |> validate_inclusion(:state, @states)
    |> foreign_key_constraint(:goal_id, name: :goal_budget_reservations_goal_revision_fkey)
    |> foreign_key_constraint(:task_id, name: :goal_budget_reservations_task_identity_fkey)
    |> unique_constraint(:task_id)
    |> unique_constraint([:goal_id, :admission_key])
    |> check_constraint(:reserved_microusd, name: :goal_budget_reservations_amount_nonnegative)
    |> check_constraint(:state, name: :goal_budget_reservations_state_check)
  end

  def settle_changeset(reservation, state) when state in @states do
    reservation
    |> change(state: state)
    |> validate_inclusion(:state, @states)
    |> optimistic_lock(:lock_version)
  end
end

defmodule SymmetryControl.Goals.RunUsage do
  use Ecto.Schema
  import Ecto.Changeset

  alias SymmetryControl.Orchestration.Run

  @cost_bases ["reported", "estimated", "unknown"]

  @primary_key {:id, :binary_id, autogenerate: true}
  @foreign_key_type :binary_id
  schema "run_usage" do
    belongs_to(:run, Run)
    belongs_to(:supersedes, __MODULE__)
    field(:usage_key, :string)
    field(:provider, :string)
    field(:model, :string)
    field(:input_tokens, :integer)
    field(:output_tokens, :integer)
    field(:cached_input_tokens, :integer)
    field(:cost_microusd, :integer)
    field(:cost_basis, :string)
    field(:price_version, :string)

    timestamps(type: :utc_datetime_usec, updated_at: false)
  end

  def changeset(usage, attrs) do
    usage
    |> cast(attrs, [
      :run_id,
      :usage_key,
      :provider,
      :model,
      :input_tokens,
      :output_tokens,
      :cached_input_tokens,
      :cost_microusd,
      :cost_basis,
      :price_version,
      :supersedes_id
    ])
    |> validate_required([:run_id, :usage_key, :provider, :model, :cost_basis])
    |> validate_length(:usage_key, min: 1, max: 240)
    |> validate_length(:provider, min: 1, max: 240)
    |> validate_length(:model, min: 1, max: 500)
    |> validate_length(:price_version, max: 240)
    |> validate_nonnegative([:input_tokens, :output_tokens, :cached_input_tokens, :cost_microusd])
    |> validate_inclusion(:cost_basis, @cost_bases)
    |> assoc_constraint(:run)
    |> assoc_constraint(:supersedes)
    |> foreign_key_constraint(:supersedes_id, name: :run_usage_supersedes_same_run_fkey)
    |> unique_constraint([:run_id, :usage_key])
    |> unique_constraint(:supersedes_id, name: :run_usage_supersedes_id_key)
    |> check_constraint(:supersedes_id, name: :run_usage_supersedes_not_self)
    |> check_constraint(:input_tokens, name: :run_usage_count_nonnegative)
    |> check_constraint(:cost_basis, name: :run_usage_cost_basis_check)
    |> check_constraint(:usage_key, name: :run_usage_key_present)
    |> validate_not_self_supersede()
  end

  def immutable_changeset(usage, attrs) do
    usage
    |> change()
    |> cast(attrs, [
      :run_id,
      :usage_key,
      :provider,
      :model,
      :input_tokens,
      :output_tokens,
      :cached_input_tokens,
      :cost_microusd,
      :cost_basis,
      :price_version,
      :supersedes_id
    ])
    |> reject_changes()
  end

  defp validate_nonnegative(changeset, fields) do
    Enum.reduce(fields, changeset, fn field, acc ->
      validate_number(acc, field, greater_than_or_equal_to: 0)
    end)
  end

  defp validate_not_self_supersede(changeset) do
    if same_uuid?(get_field(changeset, :id), get_field(changeset, :supersedes_id)) do
      add_error(changeset, :supersedes_id, "cannot supersede itself")
    else
      changeset
    end
  end

  defp same_uuid?(_left, nil), do: false

  defp same_uuid?(left, right) when is_binary(left) and is_binary(right) do
    case {Ecto.UUID.dump(left), Ecto.UUID.dump(right)} do
      {{:ok, left}, {:ok, right}} -> left == right
      _ -> false
    end
  end

  defp same_uuid?(_left, _right), do: false

  defp reject_changes(changeset) do
    Enum.reduce(changeset.changes, changeset, fn {field, _}, acc ->
      add_error(acc, field, "run usage is immutable")
    end)
  end
end

defmodule SymmetryControl.Goals.GoalExternalWait do
  use Ecto.Schema
  import Ecto.Changeset

  alias SymmetryControl.Goals.{Goal, GoalEvent, JsonDocument}
  alias SymmetryControl.Orchestration.{Run, Task}
  alias SymmetryControl.Workspaces.{ProjectResource, WorkItem}

  @states ["satisfied", "failed", "cancelled", "unsupported"]
  @active_states []
  @terminal_states ["satisfied", "failed", "cancelled", "unsupported"]

  @identity_fields [
    :goal_id,
    :goal_revision,
    :work_item_id,
    :task_id,
    :run_id,
    :run_generation,
    :source_ref,
    :result_id,
    :result,
    :resource_id,
    :external_ref,
    :subject,
    :subject_hash
  ]

  @primary_key {:id, :binary_id, autogenerate: true}
  @foreign_key_type :binary_id
  schema "goal_external_waits" do
    belongs_to(:goal, Goal)
    belongs_to(:work_item, WorkItem)
    belongs_to(:task, Task)
    belongs_to(:run, Run)
    belongs_to(:resource, ProjectResource, foreign_key: :resource_id)
    belongs_to(:receipt_event, GoalEvent, foreign_key: :receipt_event_id)

    field(:goal_revision, :integer)
    field(:run_generation, :integer)
    field(:source_ref, :map)
    field(:result_id, Ecto.UUID)
    field(:result, :map)
    field(:external_ref, :string)
    field(:subject, :map)
    field(:subject_hash, :binary)
    field(:next_check_at, :utc_datetime_usec)
    field(:state, :string, default: "unsupported")
    field(:check_seq, :integer, default: 0)

    timestamps(type: :utc_datetime_usec)
  end

  @doc "All durable states accepted by the external-wait record."
  def states, do: @states

  @doc "States that represent a current wait and should be scanned for reconciliation."
  def active_states, do: @active_states

  @doc "Terminal states cannot be changed by a reconciliation update."
  def terminal_states, do: @terminal_states

  @doc "Build the immutable source and target snapshot for a new external wait."
  def changeset(wait, attrs) do
    wait
    |> cast(attrs, @identity_fields ++ [:next_check_at, :state])
    |> validate_required(@identity_fields ++ [:state])
    |> validate_number(:goal_revision, greater_than: 0)
    |> validate_number(:run_generation, greater_than: 0)
    |> validate_number(:check_seq, greater_than_or_equal_to: 0)
    |> validate_length(:external_ref, min: 1, max: 1_000)
    |> validate_inclusion(:state, @states)
    |> validate_source_ref()
    |> validate_result()
    |> validate_subject()
    |> validate_digest(:subject_hash)
    |> validate_schedule()
    |> assoc_constraint(:goal)
    |> assoc_constraint(:work_item)
    |> assoc_constraint(:task)
    |> assoc_constraint(:run)
    |> assoc_constraint(:resource)
    |> foreign_key_constraint(:goal_id, name: :goal_external_waits_goal_revision_fkey)
    |> foreign_key_constraint(:work_item_id, name: :goal_external_waits_work_item_identity_fkey)
    |> foreign_key_constraint(:task_id, name: :goal_external_waits_task_identity_fkey)
    |> foreign_key_constraint(:run_id, name: :goal_external_waits_run_identity_fkey)
    |> foreign_key_constraint(:receipt_event_id,
      name: :goal_external_waits_receipt_event_identity_fkey
    )
    |> unique_constraint([:goal_id, :goal_revision, :task_id, :run_id, :result_id],
      name: :goal_external_waits_source_identity_key
    )
    |> unique_constraint([:goal_id, :goal_revision, :work_item_id],
      name: :goal_external_waits_current_work_item_key
    )
    |> check_constraint(:goal_revision, name: :goal_external_waits_goal_revision_positive)
    |> check_constraint(:run_generation, name: :goal_external_waits_run_generation_positive)
    |> check_constraint(:state, name: :goal_external_waits_state_check)
    |> check_constraint(:next_check_at, name: :goal_external_waits_next_check_state_check)
    |> check_constraint(:source_ref, name: :goal_external_waits_source_ref_object)
    |> check_constraint(:result, name: :goal_external_waits_result_object)
    |> check_constraint(:subject, name: :goal_external_waits_subject_object)
    |> check_constraint(:subject_hash, name: :goal_external_waits_subject_hash_size)
    |> check_constraint(:external_ref, name: :goal_external_waits_external_ref_present)
    |> check_constraint(:result_id, name: :goal_external_waits_result_identity_check)
  end

  @doc "Apply one Goal-first reconciliation transition using `check_seq` as the CAS version."
  def reconcile_changeset(%__MODULE__{} = wait, attrs) do
    wait
    |> cast(attrs, [:next_check_at, :state, :receipt_event_id])
    |> validate_required([:state])
    |> validate_inclusion(:state, @states)
    |> validate_schedule()
    |> reject_terminal_transition()
    |> assoc_constraint(:receipt_event)
    |> foreign_key_constraint(:receipt_event_id,
      name: :goal_external_waits_receipt_event_identity_fkey
    )
    |> check_constraint(:state, name: :goal_external_waits_state_check)
    |> check_constraint(:next_check_at, name: :goal_external_waits_next_check_state_check)
    |> optimistic_lock(:check_seq)
  end

  @doc "Reject program-level changes to an external wait history row."
  def immutable_changeset(wait, attrs) do
    wait
    |> change()
    |> cast(attrs, @identity_fields ++ [:next_check_at, :state, :receipt_event_id, :check_seq])
    |> reject_changes()
  end

  defp validate_source_ref(changeset) do
    validate_change(changeset, :source_ref, fn :source_ref, value ->
      if is_map(value), do: [], else: [source_ref: "must be an object"]
    end)
  end

  defp validate_result(changeset) do
    validate_change(changeset, :result, fn :result, value ->
      if is_map(value), do: [], else: [result: "must be an object"]
    end)
  end

  defp validate_subject(changeset) do
    validate_change(changeset, :subject, fn :subject, value ->
      valid? =
        is_map(value) and
          Map.keys(value) |> Enum.sort() == ["commit", "resource_id", "tree_digest"] and
          Enum.all?(~w(commit resource_id tree_digest), fn key ->
            is_binary(Map.get(value, key)) and String.trim(Map.get(value, key)) != ""
          end) and
          Regex.match?(~r/^(?:[0-9a-f]{40}|[0-9a-f]{64})$/, value["commit"]) and
          Regex.match?(~r/^sha256:[0-9a-f]{64}$/, value["tree_digest"]) and
          match?({:ok, _}, Ecto.UUID.cast(value["resource_id"]))

      if valid?, do: [], else: [subject: "must be a canonical Subject object"]
    end)
  end

  defp validate_digest(changeset, field) do
    validate_change(changeset, field, fn ^field, value ->
      if is_binary(value) and byte_size(value) == 32,
        do: [],
        else: [{field, "must be a 32-byte digest"}]
    end)
  end

  defp validate_schedule(changeset) do
    next_check_at = get_field(changeset, :next_check_at)

    if is_nil(next_check_at),
      do: changeset,
      else: add_error(changeset, :next_check_at, "must be absent after the wait is terminal")
  end

  defp reject_terminal_transition(changeset) do
    current_state = changeset.data.state
    requested_state = get_field(changeset, :state)

    if current_state in @terminal_states and requested_state != current_state do
      add_error(changeset, :state, "cannot transition a terminal external wait")
    else
      changeset
    end
  end

  defp reject_changes(changeset) do
    Enum.reduce(changeset.changes, changeset, fn {field, _value}, acc ->
      add_error(acc, field, "external wait history is immutable")
    end)
  end
end
