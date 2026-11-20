defmodule SymmetryControl.Chat do
  @moduledoc """
  Durable human conversations over the existing work and execution domain.

  Only the explicit intent can create work or control a worker. A replayable
  action, its messages, and its domain mutation commit together before delivery.
  """

  import Ecto.Query

  alias SymmetryControl.Chat.{Action, Message, ReadModel}
  alias SymmetryControl.Orchestration
  alias SymmetryControl.Orchestration.{Command, Notifier, Run, Runtime, Scheduler, Task}
  alias SymmetryControl.Portal.Invalidations
  alias SymmetryControl.Repo
  alias SymmetryControl.RequestHash
  alias SymmetryControl.Workspaces
  alias SymmetryControl.Workspaces.{Project, WorkItem}
  alias SymmetryControlWeb.Protocol

  @intents ~w(discuss status start_work guidance decision pause resume cancel)
  @page_size 50
  @cursor_salt "chat-messages-v1"

  def conversation(params) when is_map(params) do
    params = Protocol.normalize_map(params)

    with {:ok, scope} <- resolve_scope(params),
         {:ok, before} <- cursor(params["before"], scope.key) do
      messages =
        from(message in Message,
          where: message.scope_key == ^scope.key,
          order_by: [desc: message.inserted_at, desc: message.id],
          limit: ^(@page_size + 1),
          preload: [:command]
        )
        |> before_message(before)
        |> Repo.all()

      page = Enum.take(messages, @page_size)

      next_before =
        if length(messages) > @page_size do
          last = List.last(page)

          Phoenix.Token.sign(
            SymmetryControlWeb.Endpoint,
            @cursor_salt,
            {scope.key, last.inserted_at, last.id}
          )
        end

      {:ok,
       %{
         scope: scope.kind,
         project_id: scope.project_id,
         run_id: scope.run_id,
         messages: page |> Enum.reverse() |> Enum.map(&message_json/1),
         runs: ReadModel.runs(scope),
         next_before: next_before
       }}
    end
  end

  def conversation(_params), do: {:error, :invalid_request}

  def post_message(params) when is_map(params) do
    params = Protocol.normalize_map(params)

    with {:ok, params} <- validate_message(params) do
      action_id = params["action_id"]

      request = Map.delete(params, "action_id")
      {request_hash, request_hash_version} = RequestHash.write(request)

      result =
        Repo.transaction(fn ->
          # Serializes retries without publishing a partially populated action.
          Repo.query!("SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", [
            "chat:" <> action_id
          ])

          case Repo.get_by(Action, action_id: action_id) do
            %Action{} = action ->
              if RequestHash.matches?(action.request_hash, action.request_hash_version, request),
                do: {response(action), :replayed, nil},
                else: Repo.rollback(:idempotency_conflict)

            nil ->
              scope = resolve_scope(params) |> unwrap!()
              {work_item, command, reply, metadata} = execute(scope, params)

              attrs = %{
                scope_key: scope.key,
                project_id: scope.project_id,
                run_id: scope.run_id,
                work_item_id: work_item && work_item.id,
                command_id: command && command.id,
                intent: params["intent"],
                metadata: metadata
              }

              message = insert_message(attrs, "human", params["content"])
              reply = insert_message(attrs, "assistant", reply)

              action =
                %Action{}
                |> Action.changeset(%{
                  action_id: action_id,
                  request_hash: request_hash,
                  request_hash_version: request_hash_version,
                  message_id: message.id,
                  reply_id: reply.id
                })
                |> Repo.insert!()

              {response(action), :created, command}
          end
        end)

      case result do
        {:ok, {reply, disposition, command}} ->
          if disposition == :created do
            if command, do: Notifier.command_available(command)
            if params["intent"] not in ["discuss", "status"], do: Scheduler.wake()
            announce_changes(params["intent"], command, reply)
          end

          {:ok, reply, disposition}

        {:error, reason} ->
          {:error, reason}
      end
    end
  end

  def post_message(_params), do: {:error, :invalid_request}

  # A created chat write announces the domain resources it changed, not only
  # kind "chat": a start_work creates its work item, and a durable control
  # command changes its run. The transaction has already committed here.
  # discuss/status and a replayed request changed nothing new and announce
  # nothing.
  defp announce_changes("start_work", _command, reply) do
    Invalidations.publish(%{kind: "work_item", id: reply.work_item_id})
  end

  defp announce_changes(_intent, %Command{} = command, reply) do
    if command.run_id, do: Invalidations.publish(%{kind: "run", id: command.run_id})
    if reply.work_item_id, do: Invalidations.publish(%{kind: "work_item", id: reply.work_item_id})
  end

  defp announce_changes(_intent, _command, _reply), do: :ok

  defp execute(scope, %{"intent" => intent} = params) when intent in ["discuss", "status"] do
    work_item = optional_target(scope, params)
    runs = ReadModel.runs(scope, work_item && work_item.id)

    {work_item, nil, ReadModel.context_reply(runs, intent, params["content"]),
     %{"source" => "durable_work_context", "work_item_ids" => Enum.map(runs, & &1.work_item.id)}}
  end

  defp execute(scope, %{"intent" => "start_work"} = params) do
    if scope.kind == "run", do: Repo.rollback(:state_conflict)

    project_id =
      if scope.kind == "project", do: scope.project_id, else: params["target_project_id"]

    if not valid_uuid?(project_id), do: Repo.rollback(:invalid_request)

    work_item =
      case params["work_item_id"] do
        nil ->
          attrs = params["work"] || %{}

          attrs =
            Map.take(
              attrs,
              ~w(title agent_profile workspace repository_resource_id ci_resource_id)
            )
            |> Map.put_new(
              "title",
              params["content"] |> String.split("\n", parts: 2) |> hd() |> String.slice(0, 160)
            )
            |> Map.merge(%{
              "description" => params["content"],
              "status" => "ready",
              "assignee_type" => "agent"
            })

          Workspaces.create_work_item(project_id, attrs) |> unwrap!()

        id ->
          item = fetch(WorkItem, id) |> unwrap!()
          if item.project_id != project_id, do: Repo.rollback(:not_found)
          if params["generation"] != 0, do: Repo.rollback(:state_conflict)
          if item.orchestration_task_id, do: Repo.rollback(:state_conflict)
          item
      end

    case Workspaces.launch_work_item(work_item.id, params["action_id"],
           required_capabilities: &start_admission_requirement/1,
           notify: false
         ) do
      {:ok, snapshot, _disposition} ->
        {snapshot.work_item, nil,
         "Started #{snapshot.work_item.title}. The worker will continue autonomously and request a decision only when human judgment is needed.",
         %{"source" => "work_started", "task_id" => snapshot.task.task.id}}

      {:error, reason} ->
        Repo.rollback(reason)
    end
  end

  defp execute(scope, params) do
    work_item = required_target(scope, params)
    generation = params["generation"]
    if not is_integer(generation) or generation < 0, do: Repo.rollback(:invalid_request)
    task_id = work_item.orchestration_task_id || Repo.rollback(:state_conflict)
    task = Repo.one!(from task in Task, where: task.id == ^task_id, lock: "FOR UPDATE")

    if task.attempt_generation != generation, do: Repo.rollback(:state_conflict)

    if scope.run_id &&
         (scope.run.generation != generation or task.attempt_generation != scope.run.generation),
       do: Repo.rollback(:state_conflict)

    kind = params["intent"]
    key = "chat:" <> params["action_id"]

    result =
      if kind == "decision" do
        transition_id = params["waiting_transition_id"]
        if not valid_uuid?(transition_id), do: Repo.rollback(:invalid_request)
        payload = %{"answer" => params["content"]}

        payload =
          if params["option_id"],
            do: Map.put(payload, "option_id", params["option_id"]),
            else: payload

        Orchestration.provide_input(task_id, payload, key,
          expected_generation: generation,
          expected_waiting_transition_id: transition_id
        )
      else
        payload = if kind == "guidance", do: %{"message" => params["content"]}, else: %{}
        Orchestration.create_command(task_id, kind, payload, key, expected_generation: generation)
      end

    case result do
      {:ok, command, _disposition} ->
        {work_item, command, control_reply(kind),
         %{"source" => "durable_command", "generation" => generation}}

      {:error, reason} ->
        Repo.rollback(reason)
    end
  end

  # A Chat start launches a goal-less task, and the daemon refuses supervisory
  # commands on a native runtime (`@supervisory_commands` in
  # `create_new_command!` requires a legacy supervisory runtime), so a native
  # runtime can never satisfy the legacy requirement. When every runtime
  # registered for the work item's execution binding runs a native harness —
  # the vocabulary the native Goal admission selection query and this
  # derivation share through `Orchestration.native_harness_kind?/1` rather than
  # restating; the Runtime and HarnessSession schemas and the database check
  # constraints restate it and are held to it by
  # `SymmetryControl.NativeHarnessKindBindingTest` — admit the start
  # through the operations a native admission needs: start one turn, stream the
  # events that carry its terminal result, and remain cancellable. Every other
  # binding (no registered runtime, or any runtime without harness metadata)
  # keeps the legacy supervisory requirement. A native kind that cannot claim a
  # goal-less task is unaffected: `next_assignable_legacy_task_and_runtime/1`
  # excludes `omp`/`omo` from such a claim and `commit_new_claim!` rolls back,
  # so the exclusion — not the requirement — decides there, while a mixed
  # binding keeps the requirement its claimable sibling runtime can satisfy.
  #
  # Called by `Workspaces.launch_work_item/3` inside its transaction, after the
  # work item's row lock, so the derived requirement reads exactly the row the
  # launch commits against: a concurrent profile or registry change between an
  # earlier read and the lock cannot desynchronise the two.
  defp start_admission_requirement(work_item) do
    work_item = Repo.preload(work_item, :project)
    project = work_item.project
    agent_profile = work_item.agent_profile || project.default_agent_profile
    workspace = work_item.workspace || project.default_workspace

    harness_kinds =
      Repo.all(
        from runtime in Runtime,
          where: runtime.agent_profile == ^agent_profile and runtime.workspace == ^workspace,
          select: runtime.harness_kind
      )

    if harness_kinds != [] and Enum.all?(harness_kinds, &Orchestration.native_harness_kind?/1) do
      %{
        "adapter" => %{
          "operations" => %{
            "start" => true,
            "events" => true,
            "cancel" => true,
            "pause" => "unsupported"
          }
        }
      }
    else
      %{"supervisory_control" => true}
    end
  end

  defp control_reply("guidance"),
    do:
      "Guidance saved for the worker's next safe execution boundary. Application is pending acknowledgement."

  defp control_reply("pause"),
    do:
      "Pause requested. The worker will pause at a safe boundary; check the command acknowledgement for confirmation."

  defp control_reply("resume"),
    do: "Resume requested. Autonomous work continues after the worker acknowledges this command."

  defp control_reply("cancel"),
    do: "Cancellation requested. Execution history and useful workspace artifacts are retained."

  defp control_reply("decision"),
    do:
      "Decision saved for this waiting request. The worker will continue after it consumes the response."

  defp optional_target(scope, %{"work_item_id" => _} = params), do: required_target(scope, params)
  defp optional_target(%{kind: "run", work_item: item}, _params), do: item
  defp optional_target(_scope, _params), do: nil

  defp required_target(scope, params) do
    item = fetch(WorkItem, params["work_item_id"]) |> unwrap!()

    cond do
      scope.kind == "project" and item.project_id != scope.project_id -> Repo.rollback(:not_found)
      scope.kind == "run" and item.id != scope.work_item.id -> Repo.rollback(:not_found)
      true -> item
    end
  end

  defp resolve_scope(%{"scope" => "workspace"} = params) do
    if params["project_id"] || params["run_id"],
      do: {:error, :invalid_request},
      else: {:ok, %{kind: "workspace", key: "workspace", project_id: nil, run_id: nil}}
  end

  defp resolve_scope(%{"scope" => "project", "project_id" => id} = params) do
    with {:ok, project} <- fetch(Project, id),
         true <- is_nil(params["run_id"]) do
      {:ok,
       %{kind: "project", key: "project:" <> project.id, project_id: project.id, run_id: nil}}
    else
      false -> {:error, :invalid_request}
      error -> error
    end
  end

  defp resolve_scope(%{"scope" => "run", "run_id" => id} = params) do
    with {:ok, run} <- fetch(Run, id),
         %WorkItem{} = item <- Repo.get_by(WorkItem, orchestration_task_id: run.task_id),
         true <- is_nil(params["project_id"]) or params["project_id"] == item.project_id do
      {:ok,
       %{
         kind: "run",
         key: "run:" <> run.id,
         project_id: item.project_id,
         run_id: run.id,
         run: run,
         work_item: item
       }}
    else
      nil -> {:error, :not_found}
      false -> {:error, :not_found}
      error -> error
    end
  end

  defp resolve_scope(_params), do: {:error, :invalid_request}

  defp validate_message(params) do
    intent = params["intent"]
    content = params["content"]

    action_id =
      params["action_id"] || if(intent in ["discuss", "status"], do: Ecto.UUID.generate())

    if intent in @intents and is_binary(content) and byte_size(content) in 1..100_000 and
         String.trim(content) != "" and not String.contains?(content, <<0>>) and
         is_binary(action_id) and byte_size(action_id) in 1..128 and
         not String.contains?(action_id, <<0>>) and
         (is_nil(params["work"]) or is_map(params["work"])) do
      {:ok, Map.put(params, "action_id", action_id)}
    else
      {:error, :invalid_request}
    end
  end

  defp insert_message(attrs, role, content) do
    %Message{}
    |> Message.changeset(Map.merge(attrs, %{role: role, content: content}))
    |> Repo.insert()
    |> unwrap!()
  end

  defp response(action) do
    message = Repo.get!(Message, action.message_id) |> Repo.preload(:command)
    reply = Repo.get!(Message, action.reply_id) |> Repo.preload(:command)

    %{
      message: message_json(message),
      reply: message_json(reply),
      work_item_id: message.work_item_id,
      command: message.command && Protocol.command(message.command)
    }
  end

  defp message_json(message) do
    %{
      id: message.id,
      role: message.role,
      intent: message.intent,
      content: message.content,
      inserted_at: DateTime.to_iso8601(message.inserted_at),
      work_item_id: message.work_item_id,
      command: message.command && Protocol.command(message.command),
      metadata: message.metadata
    }
  end

  defp cursor(nil, _scope_key), do: {:ok, nil}

  defp cursor(token, scope_key) when is_binary(token) do
    case Phoenix.Token.verify(SymmetryControlWeb.Endpoint, @cursor_salt, token, max_age: 86_400) do
      {:ok, {^scope_key, %DateTime{} = date, id}} ->
        if valid_uuid?(id), do: {:ok, {date, id}}, else: {:error, :invalid_request}

      _ ->
        {:error, :invalid_request}
    end
  end

  defp cursor(_token, _scope_key), do: {:error, :invalid_request}

  defp before_message(query, nil), do: query

  defp before_message(query, {date, id}) do
    where(
      query,
      [message],
      message.inserted_at < ^date or
        (message.inserted_at == ^date and message.id < ^id)
    )
  end

  defp fetch(schema, id) do
    if valid_uuid?(id) do
      case Repo.get(schema, id) do
        nil -> {:error, :not_found}
        record -> {:ok, record}
      end
    else
      {:error, :not_found}
    end
  end

  defp valid_uuid?(value), do: match?({:ok, _}, Ecto.UUID.cast(value))
  defp unwrap!({:ok, value}), do: value
  defp unwrap!({:error, reason}), do: Repo.rollback(reason)
end
