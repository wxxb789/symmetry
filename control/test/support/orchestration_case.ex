defmodule SymmetryControl.OrchestrationCase do
  @moduledoc """
  The data-layer surface is repeated, not `use`-d from `SymmetryControl.DataCase`: a second
  `use ExUnit.Case` injection re-runs the sandbox setup and fails `:already_shared`.
  """

  use ExUnit.CaseTemplate

  using do
    quote do
      alias SymmetryControl.Repo

      import Ecto
      import Ecto.Changeset
      import Ecto.Query
      import SymmetryControl.DataCase

      import SymmetryControl.TestFixtures,
        only: [
          register_runtime: 2,
          register_runtime: 3,
          claim: 3,
          claim: 4,
          goal_claim: 3,
          task_attrs: 0,
          task_attrs: 1,
          provider_task_attrs: 1,
          goal_command: 3,
          goal_command: 4
        ]
    end
  end

  setup tags do
    SymmetryControl.DataCase.setup_sandbox(tags)
    :ok
  end
end
