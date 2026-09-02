defmodule SymmetryControl.Repo.Migrations.CreateOrchestrationTables do
  use Ecto.Migration

  def change do
    create table(:machines, primary_key: false) do
      add :id, :binary_id, primary_key: true
      add :name, :string, null: false
      add :token_digest, :binary, null: false
      timestamps(type: :utc_datetime_usec)
    end

    create unique_index(:machines, [:token_digest])

    create table(:runtimes, primary_key: false) do
      add :id, :binary_id, primary_key: true

      add :machine_id, references(:machines, type: :binary_id, on_delete: :delete_all),
        null: false

      add :runtime_key, :string, null: false
      add :name, :string, null: false
      add :daemon_instance_id, :binary_id, null: false
      add :connection_epoch, :bigint, null: false, default: 1
      add :capacity, :integer, null: false
      add :agent_profile, :string, null: false
      add :workspace, :string, null: false
      add :capabilities, :map, null: false, default: %{}
      add :status, :string, null: false, default: "offline"
      add :heartbeat_interval_ms, :integer, null: false, default: 5000
      add :last_heartbeat_at, :utc_datetime_usec
      timestamps(type: :utc_datetime_usec)
    end

    create unique_index(:runtimes, [:machine_id, :runtime_key])

    create index(:runtimes, [:agent_profile, :workspace, :inserted_at],
             where: "status = 'online'",
             name: :runtimes_online_profile_workspace_inserted_at
           )

    create constraint(:runtimes, :runtimes_capacity_positive, check: "capacity > 0")
    create constraint(:runtimes, :runtimes_epoch_positive, check: "connection_epoch > 0")

    create constraint(:runtimes, :runtimes_heartbeat_interval_positive,
             check: "heartbeat_interval_ms > 0"
           )

    create constraint(:runtimes, :runtimes_status_check, check: "status IN ('online', 'offline')")

    create table(:tasks, primary_key: false) do
      add :id, :binary_id, primary_key: true
      add :idempotency_key, :string, null: false
      add :request_hash, :binary, null: false
      add :goal, :text, null: false
      add :agent_profile, :string, null: false
      add :workspace, :string, null: false
      add :input, :map, null: false, default: %{}
      add :state, :string, null: false, default: "queued"
      add :current_generation, :integer, null: false, default: 0
      add :result, :map
      add :failure, :map
      timestamps(type: :utc_datetime_usec)
    end

    create unique_index(:tasks, [:idempotency_key])

    create index(:tasks, [:agent_profile, :workspace, :inserted_at],
             where: "state = 'queued'",
             name: :tasks_queued_profile_workspace_inserted_at
           )

    create constraint(:tasks, :tasks_state_check,
             check:
               "state IN ('queued', 'assigned', 'claimed', 'running', 'waiting_for_input', 'cancelling', 'completed', 'failed', 'cancelled')"
           )

    create constraint(:tasks, :tasks_generation_nonnegative, check: "current_generation >= 0")

    create table(:runs, primary_key: false) do
      add :id, :binary_id, primary_key: true
      add :task_id, references(:tasks, type: :binary_id, on_delete: :delete_all), null: false
      add :runtime_id, references(:runtimes, type: :binary_id, on_delete: :restrict), null: false
      add :generation, :integer, null: false
      add :state, :string, null: false, default: "assigned"
      add :claimed_runtime_epoch, :bigint
      add :claim_id, :binary_id
      add :lease_token, :binary_id
      add :assigned_at, :utc_datetime_usec, null: false
      add :assignment_expires_at, :utc_datetime_usec, null: false
      add :claimed_at, :utc_datetime_usec
      add :lease_expires_at, :utc_datetime_usec
      add :result, :map
      add :failure, :map
      timestamps(type: :utc_datetime_usec)
    end

    create unique_index(:runs, [:task_id, :generation])
    create index(:runs, [:runtime_id, :state])
    create index(:runs, [:assignment_expires_at])
    create index(:runs, [:lease_expires_at])

    create unique_index(:runs, [:task_id],
             where:
               "state IN ('assigned', 'claimed', 'running', 'waiting_for_input', 'cancelling')",
             name: :runs_one_capacity_bearing_run_per_task
           )

    create constraint(:runs, :runs_state_check,
             check:
               "state IN ('assigned', 'claimed', 'running', 'waiting_for_input', 'cancelling', 'completed', 'failed', 'cancelled', 'expired')"
           )

    create constraint(:runs, :runs_generation_positive, check: "generation > 0")

    create table(:run_events, primary_key: false) do
      add :id, :binary_id, primary_key: true
      add :run_id, references(:runs, type: :binary_id, on_delete: :delete_all), null: false
      add :event_id, :binary_id, null: false
      add :request_hash, :binary, null: false
      add :sequence, :integer, null: false
      add :kind, :string, null: false
      add :payload, :map, null: false, default: %{}
      add :occurred_at, :utc_datetime_usec, null: false
      timestamps(type: :utc_datetime_usec)
    end

    create unique_index(:run_events, [:run_id, :event_id])

    create table(:run_transitions, primary_key: false) do
      add :id, :binary_id, primary_key: true
      add :run_id, references(:runs, type: :binary_id, on_delete: :delete_all), null: false
      add :transition_id, :binary_id, null: false
      add :request_hash, :binary, null: false
      add :state, :string, null: false
      add :payload, :map, null: false, default: %{}
      timestamps(type: :utc_datetime_usec)
    end

    create unique_index(:run_transitions, [:run_id, :transition_id])

    create table(:commands, primary_key: false) do
      add :id, :binary_id, primary_key: true
      add :run_id, references(:runs, type: :binary_id, on_delete: :delete_all), null: false
      add :generation, :integer, null: false
      add :kind, :string, null: false
      add :payload, :map, null: false, default: %{}
      add :idempotency_key, :string, null: false
      add :request_hash, :binary, null: false
      add :acknowledgement_id, :binary_id
      add :acknowledgement_outcome, :string
      add :acknowledged_at, :utc_datetime_usec
      timestamps(type: :utc_datetime_usec)
    end

    create unique_index(:commands, [:idempotency_key])

    create unique_index(:commands, [:run_id],
             where: "kind = 'cancel'",
             name: :commands_one_cancel_per_run
           )

    create index(:commands, [:run_id, :generation])

    create constraint(:commands, :commands_kind_check,
             check: "kind IN ('cancel', 'provide_input')"
           )

    create constraint(:commands, :commands_generation_positive, check: "generation > 0")

    create constraint(:commands, :commands_acknowledgement_outcome_check,
             check:
               "acknowledgement_outcome IS NULL OR acknowledgement_outcome IN ('applied', 'rejected', 'failed')"
           )
  end
end
