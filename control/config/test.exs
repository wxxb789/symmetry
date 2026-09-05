import Config

# Configure your database
#
# The MIX_TEST_PARTITION environment variable can be used
# to provide built-in test partitioning in CI environment.
# Run `mix help test` for more information.
config :symmetry_control, SymmetryControl.Repo,
  username: System.get_env("POSTGRES_USER", "postgres"),
  password: System.get_env("POSTGRES_PASSWORD", "postgres"),
  hostname: System.get_env("POSTGRES_HOST", "localhost"),
  port: String.to_integer(System.get_env("POSTGRES_PORT", "5432")),
  database:
    System.get_env("POSTGRES_DB", "symmetry_control_test") <>
      System.get_env("MIX_TEST_PARTITION", ""),
  pool: Ecto.Adapters.SQL.Sandbox,
  pool_size: System.schedulers_online() * 2

# We don't run a server during test. If one is required,
# you can enable the server option below.
config :symmetry_control, SymmetryControlWeb.Endpoint,
  http: [ip: {127, 0, 0, 1}, port: 4002],
  secret_key_base: "yG7VzZSqQAx23FDg5ioDlYmYOZxehP5ykyftVrBbrHzYEVd7P7RC4t+33pfd5Fsf",
  server: false

# Print only warnings and errors during test
config :logger, level: :warning

config :symmetry_control, :orchestration,
  enrollment_token: "test-enrollment-token",
  operator_token: "test-operator-token",
  heartbeat_interval_ms: 5_000,
  poll_interval_ms: 5_000,
  lease_duration_ms: 30_000,
  assignment_duration_ms: 30_000,
  reaper_interval_ms: 60_000,
  portal_session_max_age_seconds: 28_800,
  reaper_enabled: false,
  scheduler_enabled: false

# Initialize plugs at runtime for faster test compilation
config :phoenix, :plug_init_mode, :runtime

# Sort query params output of verified routes for robust url comparisons
config :phoenix,
  sort_verified_routes_query_params: true
