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
    timestamps(type: :utc_datetime_usec)
  end

  def changeset(machine, attrs),
    do:
      machine
      |> cast(attrs, [
        :name,
        :token_digest,
        :enrollment_idempotency_key,
        :enrollment_request_hash
      ])
      |> validate_required([:name, :token_digest])
      |> unique_constraint(:enrollment_idempotency_key)
end

defmodule SymmetryControl.Orchestration.Runtime do
  use Ecto.Schema
  import Ecto.Changeset

  @primary_key {:id, :binary_id, autogenerate: true}
  @foreign_key_type :binary_id
  schema "runtimes" do
    belongs_to :machine, SymmetryControl.Orchestration.Machine
    field :runtime_key, :string
    field :name, :string
    field :daemon_instance_id, Ecto.UUID
    field :connection_epoch, :integer
    field :capacity, :integer
    field :agent_profile, :string
    field :workspace, :string
    field :capabilities, :map, default: %{}
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
      :capabilities,
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
    |> validate_inclusion(:status, ["online", "offline"])
    |> unique_constraint([:machine_id, :runtime_key])
    |> check_constraint(:capacity, name: :runtimes_capacity_positive)
    |> check_constraint(:connection_epoch, name: :runtimes_epoch_positive)
    |> check_constraint(:heartbeat_interval_ms, name: :runtimes_heartbeat_interval_positive)
    |> check_constraint(:status, name: :runtimes_status_check)
  end
end

defmodule SymmetryControl.Orchestration.Task do
  use Ecto.Schema
  import Ecto.Changeset

  @primary_key {:id, :binary_id, autogenerate: true}
  @foreign_key_type :binary_id
  schema "tasks" do
    field :idempotency_key, :string
    field :request_hash, :binary
    field :goal, :string
    field :agent_profile, :string
    field :workspace, :string
    field :input, :map
    field :required_capabilities, :map, default: %{}
    field :state, :string
    field :current_generation, :integer
    field :attempt_generation, :integer, default: 1
    field :waiting_transition_id, Ecto.UUID
    field :result, :map
    field :failure, :map
    timestamps(type: :utc_datetime_usec)
  end

  def changeset(task, attrs) do
    task
    |> cast(attrs, [
      :idempotency_key,
      :request_hash,
      :goal,
      :agent_profile,
      :workspace,
      :input,
      :required_capabilities,
      :state,
      :current_generation,
      :attempt_generation,
      :waiting_transition_id,
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
    |> check_constraint(:state, name: :tasks_state_check)
    |> check_constraint(:current_generation, name: :tasks_generation_nonnegative)
    |> check_constraint(:attempt_generation, name: :tasks_attempt_generation_valid)
    |> check_constraint(:waiting_transition_id, name: :tasks_waiting_transition_matches_state)
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
    field :generation, :integer
    field :state, :string
    field :claimed_runtime_epoch, :integer
    field :claim_id, Ecto.UUID
    field :lease_token, Ecto.UUID
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
      :generation,
      :state,
      :claimed_runtime_epoch,
      :claim_id,
      :lease_token,
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
    field :sequence, :integer
    field :kind, :string
    field :payload, :map, default: %{}
    field :occurred_at, :utc_datetime_usec
    timestamps(type: :utc_datetime_usec)
  end

  def changeset(event, attrs),
    do:
      event
      |> cast(attrs, [:run_id, :event_id, :request_hash, :sequence, :kind, :payload, :occurred_at])
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
    field :state, :string
    field :payload, :map, default: %{}
    timestamps(type: :utc_datetime_usec)
  end

  def changeset(transition, attrs),
    do:
      transition
      |> cast(attrs, [:run_id, :transition_id, :request_hash, :state, :payload])
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
    |> validate_inclusion(:request_hash_version, [1, 2])
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
