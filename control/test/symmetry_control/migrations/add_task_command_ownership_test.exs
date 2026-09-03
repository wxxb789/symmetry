defmodule SymmetryControl.Migrations.AddTaskCommandOwnershipTest do
  use ExUnit.Case, async: false

  alias Ecto.Adapters.SQL.Sandbox
  alias SymmetryControl.Repo
  alias SymmetryControl.Repo.Migrations.AddTaskCommandOwnership
  alias SymmetryControl.Repo.Migrations.CreateOrchestrationTables

  test "backfills command ownership and enforces nullable command bindings" do
    load_migration_modules!()

    schema = "u1_command_ownership_#{System.unique_integer([:positive])}"
    create_schema!(schema)

    repo = start_schema_repo(schema)
    previous_dynamic_repo = Repo.put_dynamic_repo(repo)

    try do
      Ecto.Migrator.run(Repo, [{20_260_902_000_000, CreateOrchestrationTables}], :up,
        all: true,
        log: false,
        migration_lock: false,
        dynamic_repo: repo
      )

      historical = insert_historical_rows!()

      Ecto.Migrator.run(Repo, [{20_260_903_000_000, AddTaskCommandOwnership}], :up,
        all: true,
        log: false,
        migration_lock: false,
        dynamic_repo: repo
      )

      assert %{rows: [[input, nil, "YES"]]} =
               Repo.query!(
                 """
                 SELECT tasks.input, columns.column_default, columns.is_nullable
                 FROM tasks
                 JOIN information_schema.columns AS columns
                   ON columns.table_schema = current_schema()
                  AND columns.table_name = 'tasks'
                  AND columns.column_name = 'input'
                 WHERE tasks.id = $1
                 """,
                 [historical.first_task_id]
               )

      assert input == %{}

      assert %{rows: [[pending_task_id, "pending"]]} =
               Repo.query!(
                 "SELECT task_id, state FROM commands WHERE id = $1",
                 [historical.pending_command_id]
               )

      assert pending_task_id == historical.first_task_id

      assert %{rows: [[acknowledged_task_id, "acknowledged"]]} =
               Repo.query!(
                 "SELECT task_id, state FROM commands WHERE id = $1",
                 [historical.acknowledged_command_id]
               )

      assert acknowledged_task_id == historical.second_task_id

      Repo.query!(
        """
        INSERT INTO commands (
          id, task_id, run_id, generation, kind, payload, idempotency_key, request_hash, state,
          inserted_at, updated_at
        )
        VALUES ($1, $2, NULL, NULL, 'cancel', '{}'::jsonb, $3, $4, 'applied', now(), now())
        """,
        [uuid(), historical.second_task_id, "historical-pending", <<3>>]
      )

      assert_raise Postgrex.Error, ~r/task_id.*not-null|null value/i, fn ->
        Repo.query!(
          """
          INSERT INTO commands (
            id, task_id, run_id, generation, kind, payload, idempotency_key, request_hash, state,
            inserted_at, updated_at
          )
          VALUES ($1, NULL, NULL, NULL, 'cancel', '{}'::jsonb, $2, $3, 'applied', now(), now())
          """,
          [uuid(), "missing-task", <<4>>]
        )
      end

      assert_raise Postgrex.Error, ~r/commands_run_generation_pair/i, fn ->
        Repo.query!(
          """
          INSERT INTO commands (
            id, task_id, run_id, generation, kind, payload, idempotency_key, request_hash, state,
            inserted_at, updated_at
          )
          VALUES ($1, $2, $3, NULL, 'cancel', '{}'::jsonb, $4, $5, 'pending', now(), now())
          """,
          [
            uuid(),
            historical.second_task_id,
            historical.second_run_id,
            "missing-generation",
            <<5>>
          ]
        )
      end

      assert_raise Postgrex.Error, ~r/commands_run_generation_pair/i, fn ->
        Repo.query!(
          """
          INSERT INTO commands (
            id, task_id, run_id, generation, kind, payload, idempotency_key, request_hash, state,
            inserted_at, updated_at
          )
          VALUES ($1, $2, NULL, 1, 'cancel', '{}'::jsonb, $3, $4, 'pending', now(), now())
          """,
          [uuid(), historical.second_task_id, "missing-run", <<6>>]
        )
      end
    after
      Repo.put_dynamic_repo(previous_dynamic_repo)
      GenServer.stop(repo)
      drop_schema!(schema)
    end
  end

  defp insert_historical_rows! do
    machine_id = uuid()
    runtime_id = uuid()
    first_task_id = uuid()
    second_task_id = uuid()
    first_run_id = uuid()
    second_run_id = uuid()
    pending_command_id = uuid()
    acknowledged_command_id = uuid()

    Repo.query!(
      "INSERT INTO machines (id, name, token_digest, inserted_at, updated_at) VALUES ($1, 'machine', $2, now(), now())",
      [
        machine_id,
        <<1>>
      ]
    )

    Repo.query!(
      """
      INSERT INTO runtimes (
        id, machine_id, runtime_key, name, daemon_instance_id, connection_epoch, capacity,
        agent_profile, workspace, capabilities, status, heartbeat_interval_ms, last_heartbeat_at,
        inserted_at, updated_at
      )
      VALUES ($1, $2, 'runtime', 'Runtime', $3, 1, 2, 'codex', 'primary', '{}'::jsonb,
              'online', 5000, now(), now(), now())
      """,
      [runtime_id, machine_id, uuid()]
    )

    for {task_id, idempotency_key} <- [
          {first_task_id, "historical-first-task"},
          {second_task_id, "historical-second-task"}
        ] do
      Repo.query!(
        """
        INSERT INTO tasks (
          id, idempotency_key, request_hash, goal, agent_profile, workspace, input, state,
          current_generation, inserted_at, updated_at
        )
        VALUES ($1, $2, $3, 'Run tests', 'codex', 'primary', '{}'::jsonb, 'assigned', 1, now(), now())
        """,
        [task_id, idempotency_key, <<2>>]
      )
    end

    for {run_id, task_id} <- [{first_run_id, first_task_id}, {second_run_id, second_task_id}] do
      Repo.query!(
        """
        INSERT INTO runs (
          id, task_id, runtime_id, generation, state, assigned_at, assignment_expires_at,
          inserted_at, updated_at
        )
        VALUES ($1, $2, $3, 1, 'assigned', now(), now() + interval '1 minute', now(), now())
        """,
        [run_id, task_id, runtime_id]
      )
    end

    Repo.query!(
      """
      INSERT INTO commands (
        id, run_id, generation, kind, payload, idempotency_key, request_hash, inserted_at, updated_at
      )
      VALUES ($1, $2, 1, 'cancel', '{}'::jsonb, 'historical-pending', $3, now(), now())
      """,
      [pending_command_id, first_run_id, <<7>>]
    )

    Repo.query!(
      """
      INSERT INTO commands (
        id, run_id, generation, kind, payload, idempotency_key, request_hash, acknowledgement_id,
        acknowledgement_outcome, acknowledged_at, inserted_at, updated_at
      )
      VALUES ($1, $2, 1, 'provide_input', '{}'::jsonb, 'historical-acknowledged', $3, $4,
              'applied', now(), now(), now())
      """,
      [acknowledged_command_id, second_run_id, <<8>>, uuid()]
    )

    %{
      first_task_id: first_task_id,
      second_task_id: second_task_id,
      second_run_id: second_run_id,
      pending_command_id: pending_command_id,
      acknowledged_command_id: acknowledged_command_id
    }
  end

  defp start_schema_repo(schema) do
    config =
      Application.fetch_env!(:symmetry_control, Repo)
      |> Keyword.merge(name: nil, pool: DBConnection.ConnectionPool, pool_size: 1)
      |> Keyword.put(:parameters, search_path: schema)

    {:ok, repo} = Repo.start_link(config)
    repo
  end

  defp load_migration_modules! do
    migrations = [
      {CreateOrchestrationTables, "20260902000000_create_orchestration_tables.exs"},
      {AddTaskCommandOwnership, "20260903000000_add_task_command_ownership.exs"}
    ]

    Enum.each(migrations, fn {module, filename} ->
      unless function_exported?(module, :__migration__, 0) do
        filename
        |> then(&Path.expand("../../../priv/repo/migrations/#{&1}", __DIR__))
        |> Code.compile_file()
      end
    end)
  end

  defp create_schema!(schema) do
    Sandbox.unboxed_run(Repo, fn ->
      Repo.query!("CREATE SCHEMA #{schema}")
    end)
  end

  defp drop_schema!(schema) do
    Sandbox.unboxed_run(Repo, fn ->
      Repo.query!("DROP SCHEMA IF EXISTS #{schema} CASCADE")
    end)
  end

  defp uuid, do: Ecto.UUID.bingenerate()
end
