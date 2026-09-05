defmodule SymmetryControl.Orchestration.Notifier do
  @moduledoc false

  alias SymmetryControl.Orchestration

  def command_available(%{id: command_id, state: "pending", run_id: run_id})
      when is_binary(run_id) do
    with {:ok, %{machine_id: machine_id, runtime_id: runtime_id}} <-
           Orchestration.assignment_target(run_id) do
      Phoenix.PubSub.broadcast(
        SymmetryControl.PubSub,
        "daemon:" <> machine_id,
        {:command_available, %{runtime_id: runtime_id, command_id: command_id}}
      )
    end
  end

  def command_available(_command), do: :ok
end
