# This file is responsible for configuring your application
# and its dependencies with the aid of the Config module.
#
# This configuration file is loaded before any dependency and
# is restricted to this project.

# General application configuration
import Config

config :symmetry_control,
  ecto_repos: [SymmetryControl.Repo],
  generators: [timestamp_type: :utc_datetime, binary_id: true],
  orchestration: [
    enrollment_token: "development-enrollment-token",
    operator_token: "development-operator-token",
    heartbeat_interval_ms: 5_000,
    poll_interval_ms: 5_000,
    lease_duration_ms: 120_000,
    assignment_duration_ms: 30_000,
    reaper_interval_ms: 5_000,
    portal_session_max_age_seconds: 28_800,
    reaper_enabled: true,
    scheduler_enabled: true
  ]

config :symmetry_control, :portal_session_secure, false

config :symmetry_control, SymmetryControl.Repo, migration_lock: :pg_advisory_lock

# Configure the endpoint
config :symmetry_control, SymmetryControlWeb.Endpoint,
  url: [host: "localhost"],
  adapter: Bandit.PhoenixAdapter,
  render_errors: [
    formats: [json: SymmetryControlWeb.ErrorJSON],
    layout: false
  ],
  pubsub_server: SymmetryControl.PubSub,
  live_view: [signing_salt: "cLAPZIpY"]

# Configure Elixir's Logger
config :logger, :default_formatter,
  format: "$time $metadata[$level] $message\n",
  metadata: [:request_id]

# Use Jason for JSON parsing in Phoenix
config :phoenix, :json_library, Jason

config :phoenix, :filter_parameters, [
  "password",
  "credential",
  "token",
  "pat",
  "secret",
  "authorization"
]

# Import environment specific config. This must remain at the bottom
# of this file so it overrides the configuration defined above.
import_config "#{config_env()}.exs"
