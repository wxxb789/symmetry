defmodule SymmetryControl.OrchestrationCase do
  @moduledoc """
  Case template for the control suites that build their fixtures through
  `SymmetryControl.TestFixtures`.

  It carries the data-layer surface of `SymmetryControl.DataCase` (the sandbox
  setup, the `Repo` alias and the Ecto imports) and imports the shared
  builders, so a suite declares its fixture surface with one line:

      use SymmetryControl.OrchestrationCase, async: false

  A fresh id, a different clock or a non-default runtime stays explicit at the
  call site; the template only supplies the surface.

  The data-layer surface is repeated rather than obtained by `use`-ing
  `SymmetryControl.DataCase` from here (or `use`-ing both templates in one
  file): every case template injects `use ExUnit.Case`, and the second
  injection runs the sandbox setup twice. Measured on this tree with a
  throwaway probe (`use SymmetryControl.DataCase, async: false` twice): exit 2,
  `Ecto.Adapters.SQL.Sandbox.start_owner!/2` failure, 0/1 passed.
  `SymmetryControlWeb.ConnCase` is the same pattern over DataCase.
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
          task_attrs: 0,
          task_attrs: 1,
          provider_task_attrs: 0,
          provider_task_attrs: 1,
          goal_command: 3,
          goal_command: 4,
          command_current: 4,
          command_current: 5
        ]
    end
  end

  setup tags do
    SymmetryControl.DataCase.setup_sandbox(tags)
    :ok
  end
end
