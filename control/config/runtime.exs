import Config

# config/runtime.exs is executed for all environments, including
# during releases. It is executed after compilation and before the
# system starts, so it is typically used to load production configuration
# and secrets from environment variables or elsewhere. Do not define
# any compile-time configuration in here, as it won't be applied.
# The block below contains prod specific runtime configuration.

# ## Using releases
#
# If you use `mix release`, you need to explicitly enable the server
# by passing the PHX_SERVER=true when you start it:
#
#     PHX_SERVER=true bin/symmetry_control start
#
# Alternatively, you can use `mix phx.gen.release` to generate a `bin/server`
# script that automatically sets the env var above.
if System.get_env("PHX_SERVER") do
  config :symmetry_control, SymmetryControlWeb.Endpoint, server: true
end

config :symmetry_control, SymmetryControlWeb.Endpoint,
  http: [port: String.to_integer(System.get_env("PORT", "4000"))]

if config_env() != :test do
  token = fn variable, development_default ->
    if config_env() == :prod do
      System.fetch_env!(variable)
    else
      System.get_env(variable, development_default)
    end
  end

  enrollment_token = token.("SYMMETRY_ENROLLMENT_TOKEN", "development-enrollment-token")
  operator_token = token.("SYMMETRY_OPERATOR_TOKEN", "development-operator-token")

  integration_sync_interval_ms =
    case Integer.parse(System.get_env("SYMMETRY_INTEGRATION_SYNC_INTERVAL_MS", "300000")) do
      {value, ""} when value >= 30_000 -> value
      _ -> raise "SYMMETRY_INTEGRATION_SYNC_INTERVAL_MS must be at least 30000"
    end

  lease_duration_ms =
    case Integer.parse(System.get_env("SYMMETRY_LEASE_DURATION_MS", "120000")) do
      {value, ""} when value >= 30_000 -> value
      _ -> raise "SYMMETRY_LEASE_DURATION_MS must be at least 30000"
    end

  if enrollment_token == operator_token do
    raise "SYMMETRY_ENROLLMENT_TOKEN and SYMMETRY_OPERATOR_TOKEN must differ"
  end

  config :symmetry_control, :orchestration,
    enrollment_token: enrollment_token,
    operator_token: operator_token,
    heartbeat_interval_ms:
      String.to_integer(System.get_env("SYMMETRY_HEARTBEAT_INTERVAL_MS", "5000")),
    poll_interval_ms: String.to_integer(System.get_env("SYMMETRY_POLL_INTERVAL_MS", "5000")),
    lease_duration_ms: lease_duration_ms,
    assignment_duration_ms:
      String.to_integer(System.get_env("SYMMETRY_ASSIGNMENT_DURATION_MS", "30000")),
    reaper_interval_ms: String.to_integer(System.get_env("SYMMETRY_REAPER_INTERVAL_MS", "5000")),
    portal_session_max_age_seconds:
      String.to_integer(System.get_env("SYMMETRY_PORTAL_SESSION_MAX_AGE_SECONDS", "28800")),
    reaper_enabled: true,
    scheduler_enabled: true

  config :symmetry_control, :integrations,
    syncer_enabled: true,
    sync_interval_ms: integration_sync_interval_ms
end

if config_env() == :prod do
  database_url =
    System.get_env("DATABASE_URL") ||
      raise """
      environment variable DATABASE_URL is missing.
      For example: ecto://USER:PASS@HOST/DATABASE
      """

  maybe_ipv6 = if System.get_env("ECTO_IPV6") in ~w(true 1), do: [:inet6], else: []

  config :symmetry_control, SymmetryControl.Repo,
    # ssl: true,
    url: database_url,
    pool_size: String.to_integer(System.get_env("POOL_SIZE") || "10"),
    # For machines with several cores, consider starting multiple pools of `pool_size`
    # pool_count: 4,
    socket_options: maybe_ipv6

  # The secret key base is used to sign/encrypt cookies and other secrets.
  # A default value is used in config/dev.exs and config/test.exs but you
  # want to use a different value for prod and you most likely don't want
  # to check this value into version control, so we use an environment
  # variable instead.
  secret_key_base =
    System.get_env("SECRET_KEY_BASE") ||
      raise """
      environment variable SECRET_KEY_BASE is missing.
      You can generate one by calling: mix phx.gen.secret
      """

  host = System.fetch_env!("PHX_HOST")

  config :symmetry_control, :dns_cluster_query, System.get_env("DNS_CLUSTER_QUERY")

  config :symmetry_control, SymmetryControlWeb.Endpoint,
    url: [host: host, port: 443, scheme: "https"],
    http: [
      # Enable IPv6 and bind on all interfaces.
      # Set it to  {0, 0, 0, 0, 0, 0, 0, 1} for local network only access.
      # See the documentation on https://bandit.hexdocs.pm/Bandit.html#t:options/0
      # for details about using IPv6 vs IPv4 and loopback vs public addresses.
      ip: {0, 0, 0, 0, 0, 0, 0, 0}
    ],
    secret_key_base: secret_key_base
end
