defmodule SymmetryControl.Orchestration.Machine do
  use Ecto.Schema
  import Ecto.Changeset

  @primary_key {:id, :binary_id, autogenerate: true}
  @foreign_key_type :binary_id
  schema "machines" do
    field :name, :string
    field :token_digest, :binary
    field :enrollment_idempotency_key, :string
    field :enrollment_request_hash, :binary
    field :enrollment_request_hash_version, :integer, default: 1
    timestamps(type: :utc_datetime_usec)
  end

  def changeset(machine, attrs),
    do:
      machine
      |> cast(attrs, [
        :name,
        :token_digest,
        :enrollment_idempotency_key,
        :enrollment_request_hash,
        :enrollment_request_hash_version
      ])
      |> validate_required([:name, :token_digest])
      |> validate_inclusion(:enrollment_request_hash_version, [1, 2])
      |> unique_constraint(:enrollment_idempotency_key)
      |> check_constraint(:enrollment_request_hash_version,
        name: :machines_enrollment_request_hash_version_check
      )
end

defmodule SymmetryControl.Orchestration.Runtime do
  use Ecto.Schema
  import Ecto.Changeset

  @primary_key {:id, :binary_id, autogenerate: true}
  @foreign_key_type :binary_id
  schema "runtimes" do
    belongs_to :machine, SymmetryControl.Orchestration.Machine
    belongs_to :repository_resource, SymmetryControl.Workspaces.ProjectResource
    field :runtime_key, :string
    field :name, :string
    field :daemon_instance_id, Ecto.UUID
    field :connection_epoch, :integer
    field :capacity, :integer
    field :agent_profile, :string
    field :workspace, :string
    field :capabilities, :map, default: %{}
    field :harness_kind, :string
    field :harness_version, :string
    field :adapter_version, :string
    field :adapter_protocol_version, :integer
    field :status, :string
    field :heartbeat_interval_ms, :integer
    field :last_heartbeat_at, :utc_datetime_usec
    timestamps(type: :utc_datetime_usec)
  end

  def changeset(runtime, attrs) do
    runtime
    |> cast(attrs, [
      :machine_id,
      :runtime_key,
      :name,
      :daemon_instance_id,
      :connection_epoch,
      :capacity,
      :agent_profile,
      :workspace,
      :repository_resource_id,
      :capabilities,
      :harness_kind,
      :harness_version,
      :adapter_version,
      :adapter_protocol_version,
      :status,
      :heartbeat_interval_ms,
      :last_heartbeat_at
    ])
    |> validate_required([
      :machine_id,
      :runtime_key,
      :name,
      :daemon_instance_id,
      :connection_epoch,
      :capacity,
      :agent_profile,
      :workspace,
      :status,
      :heartbeat_interval_ms
    ])
    |> validate_number(:capacity, greater_than: 0)
    |> validate_number(:connection_epoch, greater_than: 0)
    |> validate_number(:heartbeat_interval_ms, greater_than: 0)
    |> validate_number(:adapter_protocol_version, greater_than: 0)
    |> validate_adapter_metadata()
    |> validate_inclusion(:status, ["online", "offline"])
    |> assoc_constraint(:repository_resource)
    |> unique_constraint([:machine_id, :runtime_key])
    |> check_constraint(:capacity, name: :runtimes_capacity_positive)
    |> check_constraint(:connection_epoch, name: :runtimes_epoch_positive)
    |> check_constraint(:heartbeat_interval_ms, name: :runtimes_heartbeat_interval_positive)
    |> check_constraint(:adapter_protocol_version,
      name: :runtimes_adapter_protocol_version_positive
    )
    |> check_constraint(:status, name: :runtimes_status_check)
  end

  defp validate_adapter_metadata(changeset) do
    fields = [:harness_kind, :harness_version, :adapter_version, :adapter_protocol_version]
    values = Enum.map(fields, &get_field(changeset, &1))

    changeset =
      if Enum.any?(values, &is_nil/1) and Enum.any?(values, &(not is_nil(&1))) do
        Enum.reduce(fields, changeset, fn field, acc ->
          if is_nil(get_field(acc, field)),
            do: add_error(acc, field, "must be present with adapter metadata"),
            else: acc
        end)
      else
        changeset
      end

    changeset
    |> validate_inclusion(:harness_kind, ["generic", "codex", "claude_code", "pi", "opencode"])
    |> validate_length(:harness_kind, min: 1, max: 120)
    |> validate_length(:harness_version, min: 1, max: 240)
    |> validate_length(:adapter_version, min: 1, max: 240)
    |> check_constraint(:harness_kind, name: :runtimes_harness_kind_check)
  end
end

defmodule SymmetryControl.Orchestration.Task do
  use Ecto.Schema
  import Ecto.Changeset

  @primary_key {:id, :binary_id, autogenerate: true}
  @foreign_key_type :binary_id
  schema "tasks" do
    belongs_to :work_item, SymmetryControl.Workspaces.WorkItem
    belongs_to :goal_record, SymmetryControl.Goals.Goal, foreign_key: :goal_id
    belongs_to :context_snapshot, SymmetryControl.Goals.ContextSnapshot
    belongs_to :validation_of_task, __MODULE__
    belongs_to :requested_session, SymmetryControl.Goals.HarnessSession
    belongs_to :handoff_source_run, SymmetryControl.Orchestration.Run
    field :idempotency_key, :string
    field :request_hash, :binary
    field :request_hash_version, :integer, default: 1
    field :goal, :string
    field :agent_profile, :string
    field :workspace, :string
    field :input, :map
    field :required_capabilities, :map, default: %{}
    field :state, :string
    field :current_generation, :integer
    field :attempt_generation, :integer, default: 1
    field :waiting_transition_id, Ecto.UUID
    field :goal_revision, :integer
    field :purpose, :string, default: "implement"
    field :admission_key, Ecto.UUID
    field :max_run_attempts, :integer
    field :result, :map
    field :failure, :map
    timestamps(type: :utc_datetime_usec)
  end

  def changeset(task, attrs) do
    task
    |> cast(attrs, [
      :idempotency_key,
      :request_hash,
      :request_hash_version,
      :work_item_id,
      :goal_id,
      :goal_revision,
      :context_snapshot_id,
      :goal,
      :agent_profile,
      :workspace,
      :input,
      :required_capabilities,
      :state,
      :current_generation,
      :attempt_generation,
      :waiting_transition_id,
      :purpose,
      :validation_of_task_id,
      :admission_key,
      :max_run_attempts,
      :requested_session_id,
      :handoff_source_run_id,
      :result,
      :failure
    ])
    |> validate_required([
      :idempotency_key,
      :request_hash,
      :goal,
      :agent_profile,
      :workspace,
      :required_capabilities,
      :state,
      :current_generation,
      :attempt_generation
    ])
    |> validate_number(:current_generation, greater_than_or_equal_to: 0)
    |> validate_number(:attempt_generation, greater_than: 0)
    |> validate_number(:goal_revision, greater_than: 0)
    |> validate_number(:max_run_attempts, greater_than: 0)
    |> validate_inclusion(:request_hash_version, [1, 2])
    |> validate_inclusion(:purpose, ["implement", "validate", "plan", "observe", "chat"])
    |> validate_goal_task_fields()
    |> validate_validation_task()
    |> validate_handoff_lineage()
    |> validate_inclusion(:state, [
      "queued",
      "assigned",
      "claimed",
      "running",
      "paused",
      "waiting_for_input",
      "cancelling",
      "completed",
      "failed",
      "cancelled"
    ])
    |> unique_constraint(:idempotency_key)
    |> check_constraint(:request_hash_version, name: :tasks_request_hash_version_check)
    |> check_constraint(:state, name: :tasks_state_check)
    |> check_constraint(:current_generation, name: :tasks_generation_nonnegative)
    |> check_constraint(:attempt_generation, name: :tasks_attempt_generation_valid)
    |> check_constraint(:waiting_transition_id, name: :tasks_waiting_transition_matches_state)
    |> check_constraint(:goal_id, name: :tasks_goal_fields_all_or_none)
    |> check_constraint(:purpose, name: :tasks_purpose_check)
    |> check_constraint(:max_run_attempts, name: :tasks_max_run_attempts_positive)
    |> foreign_key_constraint(:work_item_id, name: :tasks_goal_work_item_membership_fkey)
    |> foreign_key_constraint(:goal_id, name: :tasks_goal_revision_fkey)
    |> foreign_key_constraint(:context_snapshot_id,
      name: :tasks_context_snapshot_goal_revision_fkey
    )
    |> foreign_key_constraint(:validation_of_task_id, name: :tasks_validation_task_identity_fkey)
    |> assoc_constraint(:requested_session)
    |> assoc_constraint(:handoff_source_run)
  end

  defp validate_goal_task_fields(changeset) do
    goal_id = get_field(changeset, :goal_id)

    if is_nil(goal_id) do
      changeset =
        Enum.reduce(
          [
            :goal_revision,
            :context_snapshot_id,
            :admission_key,
            :max_run_attempts,
            :handoff_source_run_id
          ],
          changeset,
          fn field, acc ->
            if is_nil(get_field(acc, field)),
              do: acc,
              else: add_error(acc, field, "must be absent without Goal membership")
          end
        )

      if get_field(changeset, :purpose) == "plan",
        do: add_error(changeset, :purpose, "requires Goal membership"),
        else: changeset
    else
      fields = [:goal_id, :goal_revision, :context_snapshot_id, :admission_key]

      changeset =
        Enum.reduce(fields, changeset, fn field, acc ->
          if is_nil(get_field(acc, field)),
            do: add_error(acc, field, "must be present for a Goal task"),
            else: acc
        end)

      changeset =
        if is_nil(get_field(changeset, :max_run_attempts)),
          do: add_error(changeset, :max_run_attempts, "must be present for a Goal task"),
          else: changeset

      validate_goal_task_purpose_fields(changeset)
    end
  end

  defp validate_goal_task_purpose_fields(changeset) do
    case get_field(changeset, :purpose) do
      "plan" ->
        if is_nil(get_field(changeset, :work_item_id)),
          do: changeset,
          else: add_error(changeset, :work_item_id, "must be absent for a planning task")

      _purpose ->
        if is_nil(get_field(changeset, :work_item_id)),
          do: add_error(changeset, :work_item_id, "must be present for a Goal task"),
          else: changeset
    end
  end

  defp validate_validation_task(changeset) do
    purpose = get_field(changeset, :purpose)
    validation_of_task_id = get_field(changeset, :validation_of_task_id)

    cond do
      purpose == "validate" and is_nil(validation_of_task_id) ->
        add_error(changeset, :validation_of_task_id, "must be present for a validation task")

      purpose != "validate" and not is_nil(validation_of_task_id) ->
        add_error(changeset, :validation_of_task_id, "is only valid for a validation task")

      true ->
        changeset
    end
  end

  defp validate_handoff_lineage(changeset) do
    input = get_field(changeset, :input, %{}) || %{}
    session_mode = Map.get(input, "session_mode", "fresh")
    source_run_id = get_field(changeset, :handoff_source_run_id)

    cond do
      session_mode == "handoff" and is_nil(source_run_id) ->
        add_error(changeset, :handoff_source_run_id, "must be present for a handoff task")

      session_mode != "handoff" and not is_nil(source_run_id) ->
        add_error(changeset, :handoff_source_run_id, "is only valid for a handoff task")

      true ->
        changeset
    end
  end
end

defmodule SymmetryControl.Orchestration.Run do
  use Ecto.Schema
  import Ecto.Changeset

  @primary_key {:id, :binary_id, autogenerate: true}
  @foreign_key_type :binary_id
  schema "runs" do
    belongs_to :task, SymmetryControl.Orchestration.Task
    belongs_to :runtime, SymmetryControl.Orchestration.Runtime
    belongs_to :harness_session, SymmetryControl.Goals.HarnessSession
    field :generation, :integer
    field :state, :string
    field :claimed_runtime_epoch, :integer
    field :claim_id, Ecto.UUID
    field :lease_token, Ecto.UUID
    field :provider_access_snapshot, :map
    field :assigned_at, :utc_datetime_usec
    field :assignment_expires_at, :utc_datetime_usec
    field :claimed_at, :utc_datetime_usec
    field :lease_expires_at, :utc_datetime_usec
    field :result, :map
    field :failure, :map
    timestamps(type: :utc_datetime_usec)
  end

  def changeset(run, attrs) do
    run
    |> cast(attrs, [
      :task_id,
      :runtime_id,
      :harness_session_id,
      :generation,
      :state,
      :claimed_runtime_epoch,
      :claim_id,
      :lease_token,
      :provider_access_snapshot,
      :assigned_at,
      :assignment_expires_at,
      :claimed_at,
      :lease_expires_at,
      :result,
      :failure
    ])
    |> validate_required([
      :task_id,
      :runtime_id,
      :generation,
      :state,
      :assigned_at,
      :assignment_expires_at
    ])
    |> validate_number(:generation, greater_than: 0)
    |> validate_inclusion(:state, [
      "assigned",
      "claimed",
      "running",
      "paused",
      "waiting_for_input",
      "cancelling",
      "completed",
      "failed",
      "cancelled",
      "expired"
    ])
    |> unique_constraint([:task_id, :generation])
    |> check_constraint(:state, name: :runs_state_check)
    |> check_constraint(:generation, name: :runs_generation_positive)
    |> assoc_constraint(:harness_session)
  end
end

defmodule SymmetryControl.Orchestration.RunEvent do
  use Ecto.Schema
  import Ecto.Changeset

  @primary_key {:id, :binary_id, autogenerate: true}
  @foreign_key_type :binary_id
  schema "run_events" do
    belongs_to :run, SymmetryControl.Orchestration.Run
    field :event_id, Ecto.UUID
    field :request_hash, :binary
    field :request_hash_version, :integer, default: 1
    field :sequence, :integer
    field :kind, :string
    field :payload, :map, default: %{}
    field :occurred_at, :utc_datetime_usec
    timestamps(type: :utc_datetime_usec)
  end

  def changeset(event, attrs),
    do:
      event
      |> cast(attrs, [
        :run_id,
        :event_id,
        :request_hash,
        :request_hash_version,
        :sequence,
        :kind,
        :payload,
        :occurred_at
      ])
      |> validate_required([
        :run_id,
        :event_id,
        :request_hash,
        :sequence,
        :kind,
        :payload,
        :occurred_at
      ])
      |> validate_number(:sequence, greater_than_or_equal_to: 0)
      |> unique_constraint([:run_id, :event_id])
      |> validate_inclusion(:request_hash_version, [1, 2])
      |> check_constraint(:request_hash_version, name: :run_events_request_hash_version_check)
end

defmodule SymmetryControl.Orchestration.RunTransition do
  use Ecto.Schema
  import Ecto.Changeset

  @primary_key {:id, :binary_id, autogenerate: true}
  @foreign_key_type :binary_id
  schema "run_transitions" do
    belongs_to :run, SymmetryControl.Orchestration.Run
    field :transition_id, Ecto.UUID
    field :request_hash, :binary
    field :request_hash_version, :integer, default: 1
    field :state, :string
    field :payload, :map, default: %{}
    timestamps(type: :utc_datetime_usec)
  end

  def changeset(transition, attrs),
    do:
      transition
      |> cast(attrs, [
        :run_id,
        :transition_id,
        :request_hash,
        :request_hash_version,
        :state,
        :payload
      ])
      |> validate_required([:run_id, :transition_id, :request_hash, :state, :payload])
      |> validate_inclusion(:state, [
        "running",
        "paused",
        "waiting_for_input",
        "completed",
        "failed",
        "cancelled"
      ])
      |> unique_constraint([:run_id, :transition_id])
      |> validate_inclusion(:request_hash_version, [1, 2])
      |> check_constraint(:request_hash_version,
        name: :run_transitions_request_hash_version_check
      )
end

defmodule SymmetryControl.Orchestration.Command do
  use Ecto.Schema
  import Ecto.Changeset

  @primary_key {:id, :binary_id, autogenerate: true}
  @foreign_key_type :binary_id
  schema "commands" do
    belongs_to :task, SymmetryControl.Orchestration.Task
    belongs_to :run, SymmetryControl.Orchestration.Run
    field :generation, :integer
    field :kind, :string
    field :payload, :map, default: %{}
    field :idempotency_key, :string
    field :request_hash, :binary
    field :request_hash_version, :integer, default: 2
    field :state, :string
    field :applied_at, :utc_datetime_usec
    field :acknowledgement_id, Ecto.UUID
    field :acknowledgement_outcome, :string
    field :acknowledged_at, :utc_datetime_usec
    timestamps(type: :utc_datetime_usec)
  end

  def changeset(command, attrs) do
    command
    |> cast(attrs, [
      :task_id,
      :run_id,
      :generation,
      :kind,
      :payload,
      :idempotency_key,
      :request_hash,
      :request_hash_version,
      :state,
      :applied_at,
      :acknowledgement_id,
      :acknowledgement_outcome,
      :acknowledged_at
    ])
    |> validate_required([:task_id, :kind, :payload, :idempotency_key, :request_hash, :state])
    |> validate_number(:generation, greater_than: 0)
    |> validate_inclusion(:request_hash_version, [1, 2, 3])
    |> check_constraint(:request_hash_version, name: :commands_request_hash_version_check)
    |> validate_inclusion(:kind, [
      "cancel",
      "provide_input",
      "retry",
      "guidance",
      "pause",
      "resume"
    ])
    |> validate_inclusion(:state, ["pending", "applied", "acknowledged"])
    |> validate_inclusion(:acknowledgement_outcome, ["applied", "rejected", "failed"])
    |> unique_constraint([:task_id, :idempotency_key])
    |> check_constraint(:kind, name: :commands_kind_check)
    |> check_constraint(:run_id, name: :commands_run_generation_pair)
    |> check_constraint(:state, name: :commands_state_check)
    |> check_constraint(:acknowledgement_outcome, name: :commands_acknowledgement_outcome_check)
  end
end
