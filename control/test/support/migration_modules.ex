defmodule SymmetryControl.MigrationModules do
  @moduledoc """
  Ecto's directory source recompiles every migration file into the global code server on each
  `run/4`, redefining modules concurrent tests may execute; the suite compiles them once at boot.
  """

  alias SymmetryControl.Repo.Migrations.AddEngineeringConnections
  alias SymmetryControl.Repo.Migrations.AddHistoryLookupIndexes
  alias SymmetryControl.Repo.Migrations.AddMachineEnrollmentReplay
  alias SymmetryControl.Repo.Migrations.AddProviderActionDispatchOwnership
  alias SymmetryControl.Repo.Migrations.AddRetryCommands
  alias SymmetryControl.Repo.Migrations.AddSupervisoryControls
  alias SymmetryControl.Repo.Migrations.AddTaskAttemptIdentity
  alias SymmetryControl.Repo.Migrations.AddTaskCommandOwnership
  alias SymmetryControl.Repo.Migrations.AddTaskRequiredCapabilities
  alias SymmetryControl.Repo.Migrations.BindWorkItemsToResources
  alias SymmetryControl.Repo.Migrations.CompleteEngineeringWorkspace
  alias SymmetryControl.Repo.Migrations.CreateChat
  alias SymmetryControl.Repo.Migrations.CreateEngineeringWorkspace
  alias SymmetryControl.Repo.Migrations.CreateOrchestrationTables
  alias SymmetryControl.Repo.Migrations.CreateProviderActionIntents
  alias SymmetryControl.Repo.Migrations.EnforceConnectedResourceIdentity
  alias SymmetryControl.Repo.Migrations.EnforceWorkItemCoherence
  alias SymmetryControl.Repo.Migrations.SnapshotProviderActionTargets
  alias SymmetryControl.Repo.Migrations.VersionCommandRequestHashes
  alias SymmetryControl.Repo.Migrations.VersionRequestHashes

  @migrations_dir Path.expand("../../priv/repo/migrations", __DIR__)
  @pairs_key {__MODULE__, :pairs}

  @doc "Compiles every migration file once and stores the resulting pairs."
  def compile_all! do
    files =
      @migrations_dir
      |> Path.join("**/*.exs")
      |> Path.wildcard()
      |> Enum.sort()

    if files == [], do: raise(ArgumentError, "no migration files found in #{@migrations_dir}")

    previous_conflict = Code.get_compiler_option(:ignore_module_conflict)
    Code.put_compiler_option(:ignore_module_conflict, true)

    try do
      pairs = Enum.map(files, &compile!/1)
      :persistent_term.put(@pairs_key, pairs)
      pairs
    after
      Code.put_compiler_option(:ignore_module_conflict, previous_conflict)
    end
  end

  @doc "The `{version, module}` pairs compiled by `compile_all!/0`."
  def migrations do
    case :persistent_term.get(@pairs_key, :missing) do
      :missing -> raise ArgumentError, "compile_all!/0 has not run in this VM"
      pairs -> pairs
    end
  end

  @doc """
  The 20 migration pairs every goal-0006-era test builds on, one definition so the files that
  extend them cannot drift apart silently.
  """
  def base_migrations do
    verified_pairs!([
      {20_260_902_000_000, CreateOrchestrationTables},
      {20_260_903_000_000, AddTaskCommandOwnership},
      {20_260_903_010_000, AddMachineEnrollmentReplay},
      {20_260_903_020_000, AddHistoryLookupIndexes},
      {20_260_904_000_000, CreateEngineeringWorkspace},
      {20_260_904_010_000, CompleteEngineeringWorkspace},
      {20_260_904_020_000, BindWorkItemsToResources},
      {20_260_904_030_000, AddRetryCommands},
      {20_260_904_040_000, AddTaskAttemptIdentity},
      {20_260_904_050_000, EnforceWorkItemCoherence},
      {20_260_905_000_000, AddEngineeringConnections},
      {20_260_906_000_000, AddTaskRequiredCapabilities},
      {20_260_906_010_000, CreateProviderActionIntents},
      {20_260_906_020_000, AddProviderActionDispatchOwnership},
      {20_260_906_030_000, EnforceConnectedResourceIdentity},
      {20_260_906_040_000, SnapshotProviderActionTargets},
      {20_260_906_050_000, AddSupervisoryControls},
      {20_260_906_060_000, CreateChat},
      {20_260_906_070_000, VersionCommandRequestHashes},
      {20_260_907_000_000, VersionRequestHashes}
    ])
  end

  @doc """
  Returns `pairs` after checking each against the boot registry; raises when the version differs or
  the module has no migration file, so a test cannot record a version no file carries.
  """
  def verified_pairs!(pairs) do
    registry = Map.new(migrations(), fn {version, module} -> {module, version} end)

    Enum.each(pairs, fn {version, module} ->
      case Map.fetch(registry, module) do
        {:ok, ^version} ->
          :ok

        {:ok, file_version} ->
          raise ArgumentError,
                "#{inspect(module)} is recorded at version #{version} but its migration file " <>
                  "carries #{file_version}"

        :error ->
          raise ArgumentError, "#{inspect(module)} has no migration file in the boot registry"
      end
    end)

    pairs
  end

  defp compile!(file), do: {version!(file), migration_module!(file)}

  defp version!(file) do
    case Integer.parse(Path.rootname(Path.basename(file))) do
      {version, "_" <> _name} -> version
      _ -> raise ArgumentError, "migration file #{file} does not start with a version"
    end
  end

  defp migration_module!(file) do
    module =
      file
      |> Code.compile_file()
      |> Enum.find_value(fn {module, _binary} ->
        function_exported?(module, :__migration__, 0) && module
      end)

    module || raise ArgumentError, "migration file #{file} defines no Ecto migration"
  end
end
