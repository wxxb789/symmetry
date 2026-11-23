defmodule SymmetryControl.Orchestration.Registry do
  @moduledoc false

  import Ecto.Query

  import SymmetryControl.Goals.Eligibility,
    only: [
      exact_map_keys?: 2,
      valid_uuid?: 1,
      value: 2,
      value: 3
    ]

  import SymmetryControl.Orchestration.Claims,
    only: [
      insert_ignoring_conflict: 3,
      lock_machine: 1,
      lock_runtime: 1,
      now: 1,
      persist_insert: 1,
      persist_update: 1,
      rollback: 1,
      stamp_insert: 2,
      stamp_update: 2
    ]

  alias SymmetryControl.Goals.ContractValidation
  alias SymmetryControl.Goals.HarnessSession
  alias SymmetryControl.Orchestration.Command
  alias SymmetryControl.Orchestration.Machine
  alias SymmetryControl.Orchestration.Run
  alias SymmetryControl.Orchestration.Runtime
  alias SymmetryControl.Orchestration.Task
  alias SymmetryControl.Repo
  alias SymmetryControl.RequestHash
  alias SymmetryControl.Workspaces.ProjectResource

  @native_harness_kinds ["codex", "claude_code", "pi", "opencode", "omp", "omo"]
  @supervisory_commands ["guidance", "pause", "resume"]
  @terminal_states ["completed", "failed", "cancelled", "expired"]

  @spec native_harness_kind?(term()) :: boolean()
  def native_harness_kind?(kind), do: kind in native_harness_kinds()

  @spec native_harness_kinds() :: [String.t()]
  def native_harness_kinds, do: @native_harness_kinds

  @spec enroll_machine(map(), String.t(), keyword()) ::
          {:ok, %{machine: Machine.t(), token: String.t()}, :created | :replayed}
          | {:error, atom() | Ecto.Changeset.t()}
  def enroll_machine(attrs, idempotency_key, opts)
      when is_map(attrs) and is_binary(idempotency_key) and byte_size(idempotency_key) > 0 and is_list(opts) do
    submitted = Keyword.get(opts, :enrollment_token)
    expected = Keyword.get(opts, :expected_enrollment_token)
    token = value(attrs, :machine_token)
    current = now(opts)

    cond do
      not (is_binary(submitted) and is_binary(expected) and secure_compare(submitted, expected)) ->
        {:error, :unauthenticated}

      not (is_binary(token) and String.trim(token) != "") ->
        {:error, :invalid_request}

      true ->
        request = %{name: value(attrs, :name), machine_token: token}
        {request_hash, request_hash_version} = RequestHash.write(request)

        fn ->
          case Repo.one(
                 from machine in Machine,
                   where: machine.enrollment_idempotency_key == ^idempotency_key,
                   lock: "FOR UPDATE"
               ) do
            nil ->
              changeset =
                %Machine{}
                |> Machine.changeset(%{
                  name: value(attrs, :name),
                  token_digest: digest(token),
                  enrollment_idempotency_key: idempotency_key,
                  enrollment_request_hash: request_hash,
                  enrollment_request_hash_version: request_hash_version
                })
                |> Ecto.Changeset.force_change(
                  :enrollment_request_hash_version,
                  request_hash_version
                )

              case insert_ignoring_conflict(
                     Machine,
                     stamp_insert(changeset, current),
                     nil
                   ) do
                :inserted ->
                  machine =
                    Repo.one!(
                      from machine in Machine,
                        where: machine.enrollment_idempotency_key == ^idempotency_key,
                        lock: "FOR UPDATE"
                    )

                  {machine, :created}

                :conflict ->
                  case Repo.one(
                         from machine in Machine,
                           where: machine.enrollment_idempotency_key == ^idempotency_key,
                           lock: "FOR UPDATE"
                       ) do
                    %Machine{} = machine ->
                      if RequestHash.matches?(
                           machine.enrollment_request_hash,
                           machine.enrollment_request_hash_version,
                           request
                         ),
                         do: {machine, :replayed},
                         else: rollback(:idempotency_conflict)

                    nil ->
                      rollback(:invalid_request)
                  end

                :invalid ->
                  rollback(:invalid_request)
              end

            %Machine{} = machine ->
              if RequestHash.matches?(
                   machine.enrollment_request_hash,
                   machine.enrollment_request_hash_version,
                   request
                 ),
                 do: {machine, :replayed},
                 else: rollback(:idempotency_conflict)
          end
        end
        |> Repo.transaction()
        |> case do
          {:ok, {machine, disposition}} ->
            {:ok, %{machine: machine, token: token}, disposition}

          {:error, reason} ->
            {:error, reason}
        end
    end
  end

  def enroll_machine(_, _, _), do: {:error, :invalid_request}

  @spec authenticate_machine(String.t()) :: {:ok, Machine.t()} | {:error, :unauthenticated}
  def authenticate_machine(token) when is_binary(token) do
    case Repo.get_by(Machine, token_digest: digest(token)) do
      nil -> {:error, :unauthenticated}
      machine -> {:ok, machine}
    end
  end

  def authenticate_machine(_), do: {:error, :unauthenticated}

  @spec register_runtimes(Ecto.UUID.t(), Ecto.UUID.t(), [map()], keyword()) ::
          {:ok, [Runtime.t()]} | {:error, term()}
  def register_runtimes(machine_id, daemon_instance_id, specifications, opts \\ [])

  def register_runtimes(machine_id, daemon_instance_id, specifications, opts)
      when is_list(specifications) and is_list(opts) do
    if valid_uuid?(machine_id) and valid_uuid?(daemon_instance_id) and
         Enum.all?(specifications, &valid_runtime_specification?/1) do
      Repo.transaction(fn ->
        _machine = lock_machine(machine_id)
        now = now(opts)

        Enum.map(specifications, fn specification ->
          runtime_key = value(specification, :runtime_key)
          repository_resource_id = value(specification, :repository_resource_id)
          ensure_runtime_repository_resource!(repository_resource_id)

          existing =
            Repo.one(
              from runtime in Runtime,
                where: runtime.machine_id == ^machine_id and runtime.runtime_key == ^runtime_key,
                lock: "FOR UPDATE"
            )

          ensure_runtime_repository_binding_change_safe!(existing, repository_resource_id)

          attrs = %{
            name: value(specification, :name),
            capacity: value(specification, :capacity),
            agent_profile: value(specification, :agent_profile),
            workspace: value(specification, :workspace),
            repository_resource_id: repository_resource_id,
            capabilities: value(specification, :capabilities, %{}),
            harness_kind: value(specification, :harness_kind),
            harness_version: value(specification, :harness_version),
            adapter_version: value(specification, :adapter_version),
            adapter_protocol_version: value(specification, :adapter_protocol_version),
            status: "online",
            last_heartbeat_at: now,
            heartbeat_interval_ms: value(specification, :heartbeat_interval_ms, 5_000)
          }

          case existing do
            nil ->
              %Runtime{}
              |> Runtime.changeset(
                Map.merge(attrs, %{
                  machine_id: machine_id,
                  runtime_key: runtime_key,
                  daemon_instance_id: daemon_instance_id,
                  connection_epoch: 1
                })
              )
              |> stamp_insert(now)
              |> persist_insert()

            runtime when runtime.daemon_instance_id == daemon_instance_id ->
              runtime |> Runtime.changeset(attrs) |> stamp_update(now) |> persist_update()

            runtime ->
              runtime
              |> Runtime.changeset(
                Map.merge(attrs, %{
                  daemon_instance_id: daemon_instance_id,
                  connection_epoch: runtime.connection_epoch + 1
                })
              )
              |> stamp_update(now)
              |> persist_update()
          end
        end)
      end)
    else
      {:error, :invalid_request}
    end
  end

  def register_runtimes(_, _, _, _), do: {:error, :invalid_request}

  @spec heartbeat(Ecto.UUID.t(), integer(), [map()], keyword()) :: {:ok, map()} | {:error, atom()}
  def heartbeat(runtime_id, runtime_epoch, active_runs, opts \\ [])

  def heartbeat(runtime_id, runtime_epoch, active_runs, opts)
      when is_integer(runtime_epoch) and runtime_epoch > 0 and is_list(active_runs) do
    if valid_uuid?(runtime_id) and Enum.all?(active_runs, &valid_active_run?/1),
      do: heartbeat_runtime(runtime_id, runtime_epoch, opts),
      else: {:error, :invalid_request}
  end

  def heartbeat(_, _, _, _), do: {:error, :invalid_request}

  defp heartbeat_runtime(runtime_id, runtime_epoch, opts) do
    Repo.transaction(fn ->
      runtime = lock_runtime(runtime_id)
      current = now(opts)

      if runtime.connection_epoch != runtime_epoch do
        rollback(:ownership_lost)
      end

      runtime =
        runtime
        |> Runtime.changeset(%{status: "online", last_heartbeat_at: current})
        |> stamp_update(current)
        |> Repo.update!()

      snapshot_for(runtime, current)
    end)
  end

  @spec machine_owns_runtime?(Ecto.UUID.t(), Ecto.UUID.t()) :: boolean()
  def machine_owns_runtime?(machine_id, runtime_id), do: owned?(Runtime, machine_id, runtime_id, :machine_id)

  @spec machine_owns_run?(Ecto.UUID.t(), Ecto.UUID.t()) :: boolean()
  def machine_owns_run?(machine_id, run_id) when is_binary(machine_id) and is_binary(run_id) do
    if valid_uuid?(machine_id) and valid_uuid?(run_id) do
      Repo.exists?(
        from run in Run,
          join: runtime in Runtime,
          on: runtime.id == run.runtime_id,
          where: run.id == ^run_id and runtime.machine_id == ^machine_id
      )
    else
      false
    end
  end

  def machine_owns_run?(_, _), do: false

  @spec machine_owns_command?(Ecto.UUID.t(), Ecto.UUID.t()) :: boolean()
  def machine_owns_command?(machine_id, command_id) when is_binary(machine_id) and is_binary(command_id) do
    if valid_uuid?(machine_id) and valid_uuid?(command_id) do
      Repo.exists?(
        from command in Command,
          join: run in Run,
          on: run.id == command.run_id,
          join: runtime in Runtime,
          on: runtime.id == run.runtime_id,
          where: command.id == ^command_id and runtime.machine_id == ^machine_id
      )
    else
      false
    end
  end

  def machine_owns_command?(_, _), do: false

  @doc false
  def nonempty_text?(text), do: is_binary(text) and String.trim(text) != ""

  @doc false
  def snapshot_for(runtime, current) do
    assignments =
      Repo.all(
        from run in Run,
          join: task in Task,
          on: task.id == run.task_id,
          where:
            run.runtime_id == ^runtime.id and run.state == "assigned" and
              run.assignment_expires_at > ^current and task.current_generation == run.generation,
          order_by: [asc: run.inserted_at],
          select: %{run: run, task: task}
      )

    commands =
      Repo.all(
        from command in Command,
          join: run in Run,
          on: run.id == command.run_id,
          join: task in Task,
          on: task.id == run.task_id,
          where:
            run.runtime_id == ^runtime.id and
              task.current_generation == run.generation and
              ((command.state == "pending" and command.kind == "cancel" and
                  run.state == "cancelling") or
                 (command.state in ["pending", "applied"] and command.kind == "provide_input" and
                    is_nil(command.acknowledged_at) and
                    run.state in ["waiting_for_input", "running"]) or
                 (command.state in ["pending", "applied"] and
                    command.kind in ^@supervisory_commands and
                    run.state in ["running", "paused"])),
          order_by: [asc: command.inserted_at]
      )

    %{assignments: assignments, commands: commands, server_time: current}
  end

  defp valid_runtime_specification?(specification) when is_map(specification) do
    capabilities = value(specification, :capabilities, %{})

    is_map(capabilities) and jsonb_compatible?(capabilities) and
      valid_runtime_capabilities_schema?(specification, capabilities) and
      (value(capabilities, :supervisory_control, false) != true or
         (value(capabilities, :structured_input, false) == true and
            value(capabilities, :interactive, false) == true)) and
      (value(capabilities, :provider_access, false) != true or
         value(capabilities, :structured_input, false) == true) and
      valid_runtime_repository_resource?(specification) and
      valid_runtime_adapter_metadata?(specification, capabilities)
  end

  defp valid_runtime_specification?(_), do: false

  # A pre-Goal release can omit contracts entirely: only old registrations without adapter
  # metadata may use the legacy capability subset; any schema, rollout or adapter stays strict.
  defp valid_runtime_capabilities_schema?(specification, capabilities) do
    schema_root =
      :symmetry_control
      |> Application.fetch_env!(:contracts)
      |> Keyword.fetch!(:directory)
      |> Path.join("v1")

    if File.dir?(schema_root) do
      ContractValidation.validate_adapter_capabilities(capabilities, schema_root: schema_root) ==
        :ok
    else
      legacy_contract_fallback?(specification, capabilities)
    end
  end

  defp legacy_contract_fallback?(specification, capabilities) do
    goals_rollout_disabled?() and not runtime_metadata_present?(specification) and
      Enum.all?(capabilities, fn {key, value} ->
        key in [
          :structured_input,
          :provider_access,
          :interactive,
          :supervisory_control,
          "structured_input",
          "provider_access",
          "interactive",
          "supervisory_control"
        ] and is_boolean(value)
      end)
  end

  defp goals_rollout_disabled? do
    goals = Application.get_env(:symmetry_control, :goals) || []
    Keyword.get(goals, :rollout_enabled, false) != true
  end

  defp valid_runtime_repository_resource?(specification) do
    not map_key?(specification, :repository_resource_id) or
      is_nil(value(specification, :repository_resource_id)) or
      valid_uuid?(value(specification, :repository_resource_id))
  end

  defp ensure_runtime_repository_resource!(nil), do: :ok

  defp ensure_runtime_repository_resource!(resource_id) do
    resource =
      Repo.one(
        from resource in ProjectResource,
          where: resource.id == ^resource_id,
          lock: "FOR SHARE"
      ) || rollback(:invalid_request)

    if resource.kind != "repository", do: rollback(:invalid_request)
  end

  defp ensure_runtime_repository_binding_change_safe!(nil, _repository_resource_id), do: :ok

  defp ensure_runtime_repository_binding_change_safe!(runtime, repository_resource_id)
       when runtime.repository_resource_id == repository_resource_id, do: :ok

  defp ensure_runtime_repository_binding_change_safe!(runtime, _repository_resource_id) do
    active_goal_run? =
      Repo.exists?(
        from run in Run,
          join: task in Task,
          on: task.id == run.task_id,
          where:
            run.runtime_id == ^runtime.id and not is_nil(task.goal_id) and
              run.state not in ^@terminal_states
      )

    retained_harness_session? =
      Repo.exists?(
        from session in HarnessSession,
          where: session.runtime_id == ^runtime.id and session.state != "closed"
      )

    if active_goal_run? or retained_harness_session?, do: rollback(:state_conflict)
  end

  @doc false
  def valid_boolean_capability?(capabilities, key) do
    case value(capabilities, key, :missing) do
      :missing -> true
      value -> is_boolean(value)
    end
  end

  defp valid_runtime_adapter_metadata?(specification, capabilities) do
    adapter_present? = map_key?(capabilities, :adapter)
    adapter = value(capabilities, :adapter)

    if runtime_metadata_present?(specification) do
      valid_runtime_metadata?(specification) and
        valid_registered_adapter?(adapter, adapter_present?, specification, capabilities)
    else
      not adapter_present?
    end
  end

  defp runtime_metadata_present?(specification) do
    Enum.any?(
      [:harness_kind, :harness_version, :adapter_version, :adapter_protocol_version],
      &map_key?(specification, &1)
    )
  end

  @doc false
  def valid_runtime_metadata?(specification) do
    harness_kind = value(specification, :harness_kind)
    harness_version = value(specification, :harness_version)
    adapter_version = value(specification, :adapter_version)
    protocol_version = value(specification, :adapter_protocol_version)

    (harness_kind == "generic" or native_harness_kind?(harness_kind)) and
      nonempty_bounded_text?(harness_version, 240) and
      nonempty_bounded_text?(adapter_version, 240) and
      is_integer(protocol_version) and protocol_version > 0 and protocol_version <= 2_147_483_647
  end

  defp valid_registered_adapter?(adapter, _adapter_present?, specification, capabilities) when is_map(adapter) do
    adapter_matches_runtime_metadata?(adapter, specification) and
      valid_adapter_operations?(value(adapter, :operations)) and
      generic_adapter_operations_allowed?(
        value(specification, :harness_kind),
        value(adapter, :operations)
      ) and
      native_capabilities_match_adapter?(
        value(specification, :harness_kind),
        capabilities,
        value(adapter, :operations)
      )
  end

  defp valid_registered_adapter?(nil, false, _specification, _capabilities), do: true

  defp valid_registered_adapter?(_adapter, _adapter_present?, _specification, _capabilities), do: false

  @doc false
  def adapter_matches_runtime_metadata?(adapter, specification) do
    exact_map_keys?(adapter, [
      "kind",
      "native_version",
      "implementation_version",
      "protocol_version",
      "operations"
    ]) and
      value(adapter, :kind) == value(specification, :harness_kind) and
      value(adapter, :native_version) == value(specification, :harness_version) and
      value(adapter, :implementation_version) == value(specification, :adapter_version) and
      value(adapter, :protocol_version) == value(specification, :adapter_protocol_version)
  end

  @doc false
  def valid_adapter_operations?(operations) when is_map(operations) do
    exact_map_keys?(operations, [
      "start",
      "events",
      "cancel",
      "resume",
      "handoff",
      "guidance",
      "pause",
      "approval_response",
      "usage",
      "hard_cost_limit"
    ]) and
      is_boolean(value(operations, :start)) and
      is_boolean(value(operations, :events)) and
      is_boolean(value(operations, :cancel)) and
      is_boolean(value(operations, :resume)) and
      is_boolean(value(operations, :handoff)) and
      value(operations, :guidance) in ["native_steer", "next_turn", "unsupported"] and
      value(operations, :pause) in ["safe_boundary", "unsupported"] and
      is_boolean(value(operations, :approval_response)) and
      value(operations, :usage) in ["reported", "estimated", "unknown"] and
      is_boolean(value(operations, :hard_cost_limit)) and
      (value(operations, :resume) != true or value(operations, :start) == true) and
      (value(operations, :cancel) != true or
         (value(operations, :start) == true and value(operations, :events) == true)) and
      (value(operations, :handoff) != true or
         (value(operations, :start) == true and value(operations, :events) == true and
            value(operations, :cancel) == true)) and
      (value(operations, :events) != true or value(operations, :start) == true) and
      (value(operations, :approval_response) != true or
         (value(operations, :start) == true and value(operations, :events) == true)) and
      (value(operations, :guidance) != "native_steer" or value(operations, :start) == true) and
      (value(operations, :pause) != "safe_boundary" or value(operations, :resume) == true) and
      (value(operations, :usage) != "reported" or value(operations, :events) == true)
  end

  def valid_adapter_operations?(_operations), do: false

  defp generic_adapter_operations_allowed?("generic", operations) do
    value(operations, :resume) == false and
      value(operations, :handoff) == false and
      value(operations, :guidance) == "unsupported" and
      value(operations, :pause) == "unsupported" and
      value(operations, :approval_response) == false and
      value(operations, :usage) == "unknown" and
      value(operations, :hard_cost_limit) == false
  end

  defp generic_adapter_operations_allowed?(_harness_kind, _operations), do: true

  defp native_capabilities_match_adapter?("generic", _capabilities, _operations), do: true

  defp native_capabilities_match_adapter?(_harness_kind, capabilities, operations) do
    value(capabilities, :supervisory_control, false) != true and
      value(operations, :pause) == "unsupported"
  end

  @doc false
  def legacy_supervisory_runtime?(runtime), do: is_nil(runtime.harness_kind) or runtime.harness_kind == "generic"

  defp nonempty_bounded_text?(value, maximum) when is_binary(value) and byte_size(value) <= maximum,
    do: String.trim(value) != ""

  defp nonempty_bounded_text?(_value, _maximum), do: false

  # PostgreSQL jsonb rejects U+0000. Validate every untrusted JSONB value before a transaction.
  @doc false
  def jsonb_compatible?(value) when is_binary(value), do: not String.contains?(value, <<0>>)

  def jsonb_compatible?(%DateTime{}), do: true
  def jsonb_compatible?(%_{}), do: false

  def jsonb_compatible?(value) when is_list(value), do: Enum.all?(value, &jsonb_compatible?/1)

  def jsonb_compatible?(value) when is_map(value) do
    Enum.all?(value, fn {key, nested_value} ->
      jsonb_key_compatible?(key) and jsonb_compatible?(nested_value)
    end)
  end

  def jsonb_compatible?(value) when is_number(value) or is_boolean(value) or is_nil(value), do: true

  def jsonb_compatible?(_), do: false

  defp jsonb_key_compatible?(key) when is_binary(key), do: jsonb_compatible?(key)

  defp jsonb_key_compatible?(key) when is_atom(key), do: key |> Atom.to_string() |> jsonb_compatible?()

  defp jsonb_key_compatible?(_), do: false

  @doc false
  def map_key?(map, key) when is_map(map), do: Map.has_key?(map, key) or Map.has_key?(map, Atom.to_string(key))

  def map_key?(_map, _key), do: false

  @doc false
  def valid_active_run?(run) when is_map(run) do
    valid_uuid?(value(run, :run_id)) and
      is_integer(value(run, :generation)) and value(run, :generation) > 0 and
      is_integer(value(run, :claimed_runtime_epoch)) and
      value(run, :claimed_runtime_epoch) > 0 and
      valid_uuid?(value(run, :claim_id)) and valid_uuid?(value(run, :lease_token)) and
      value(run, :state) in ["claimed", "running", "paused", "waiting_for_input", "cancelling"]
  end

  def valid_active_run?(_), do: false

  defp digest(value), do: :crypto.hash(:sha256, value)

  defp owned?(schema, machine_id, id, machine_field) when is_binary(machine_id) and is_binary(id) do
    if valid_uuid?(machine_id) and valid_uuid?(id) do
      Repo.exists?(
        from record in schema,
          where: field(record, ^machine_field) == ^machine_id and record.id == ^id
      )
    else
      false
    end
  end

  defp owned?(_, _, _, _), do: false

  defp secure_compare(left, right) when byte_size(left) == byte_size(right), do: Plug.Crypto.secure_compare(left, right)

  defp secure_compare(_, _), do: false
end
