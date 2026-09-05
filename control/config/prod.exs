import Config

# Do not print debug messages in production
config :logger, level: :info
config :symmetry_control, :portal_session_secure, true

# Runtime production configuration, including reading
# of environment variables, is done on config/runtime.exs.
