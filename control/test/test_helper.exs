# Each concurrent async module holds one sandbox owner connection, so this ceiling is
# coupled to the Repo pool in config/test.exs: max_cases + 2 <= pool_size (the +2 is the
# sync module's connection and the app's own pool user). 28 covers the 25 async modules
# with that headroom; ExUnit's default (schedulers_online() * 2) already exceeds the pool
# arithmetic of the 16-scheduler dev host and of the 4-scheduler CI runner.
ExUnit.start(max_cases: 28)
Ecto.Adapters.SQL.Sandbox.mode(SymmetryControl.Repo, :manual)
