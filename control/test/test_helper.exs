# Coupled to pool_size in config/test.exs: max_cases + 2 <= pool_size.
ExUnit.start(max_cases: 28)
Ecto.Adapters.SQL.Sandbox.mode(SymmetryControl.Repo, :manual)
