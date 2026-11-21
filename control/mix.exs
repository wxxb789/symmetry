defmodule SymmetryControl.MixProject do
  use Mix.Project

  def project do
    [
      app: :symmetry_control,
      version: "0.1.0",
      elixir: "~> 1.17",
      elixirc_paths: elixirc_paths(Mix.env()),
      start_permanent: Mix.env() == :prod,
      aliases: aliases(),
      deps: deps(),
      # The advisory `mix test --cover` step of the coverage plane (refactor
      # slice W2-17, `.github/workflows/ci.yml`) must not trip the coverage
      # task's default 90% summary threshold: coverage floors become blocking at
      # W6-10/W6-12, not here, so the report is the artifact and the suite's own
      # status is the step's status.
      test_coverage: [summary: [threshold: 0]],
      listeners: [Phoenix.CodeReloader]
    ]
  end

  # Configuration for the OTP application.
  #
  # Type `mix help compile.app` for more information.
  def application do
    [
      mod: {SymmetryControl.Application, []},
      extra_applications: [:logger, :runtime_tools, :inets, :ssl]
    ]
  end

  def cli do
    [
      preferred_envs: [precommit: :test]
    ]
  end

  # Specifies which paths to compile per environment.
  defp elixirc_paths(:test), do: ["lib", "test/support"]
  defp elixirc_paths(_), do: ["lib"]

  # Specifies your project dependencies.
  #
  # Type `mix help deps` for examples and options.
  defp deps do
    [
      {:phoenix, "~> 1.8.13"},
      {:phoenix_ecto, "~> 4.5"},
      {:ecto_sql, "~> 3.13"},
      {:postgrex, ">= 0.0.0"},
      {:jason, "~> 1.2"},
      {:dns_cluster, "~> 0.3.1"},
      {:bandit, "~> 1.5"},
      # Oban 2.24 is the current stable line and supports this project's
      # Elixir 1.17, Ecto SQL 3.14, Postgrex 0.22, and Telemetry 1.4 versions.
      {:oban, "~> 2.24"},
      # ExJsonSchema 0.11.5 is a stable release compatible with the existing
      # Decimal 3.x dependency and all supported Elixir versions in this project.
      {:ex_json_schema, "~> 0.11.5"},
      {:credo, "~> 1.7", only: [:dev, :test], runtime: false}
    ]
  end

  # Aliases are shortcuts or tasks specific to the current project.
  # For example, to install project dependencies and perform other setup tasks, run:
  #
  #     $ mix setup
  #
  # See the documentation for `Mix` for more info on aliases.
  defp aliases do
    [
      setup: ["deps.get", "ecto.setup"],
      "ecto.setup": ["ecto.create", "ecto.migrate", "run priv/repo/seeds.exs"],
      "ecto.reset": ["ecto.drop", "ecto.setup"],
      # The guard task refuses to touch a browser acceptance database from the
      # test environment before any Ecto storage task runs; see its moduledoc.
      "ecto.create": ["symmetry.check_test_db", "ecto.create"],
      "ecto.migrate": ["symmetry.check_test_db", "ecto.migrate"],
      "ecto.migrations": ["symmetry.check_test_db", "ecto.migrations"],
      "ecto.drop": ["symmetry.check_test_db", "ecto.drop"],
      "ecto.rollback": ["symmetry.check_test_db", "ecto.rollback"],
      "ecto.load": ["symmetry.check_test_db", "ecto.load"],
      test: ["symmetry.check_test_db", "ecto.create --quiet", "ecto.migrate --quiet", "test"],
      precommit: ["compile --warnings-as-errors", "deps.unlock --unused", "format", "test"]
    ]
  end
end
