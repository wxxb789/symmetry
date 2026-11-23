defmodule SymmetryControl.MigrationModules do
  @moduledoc """
  The boot-time registry of `priv/repo/migrations` as `{version, module}` pairs.

  Ecto's directory source recompiles every migration file on each `run/4`, which
  redefines the migration modules in the global code server while concurrent
  tests execute them. The suite compiles the directory once at boot
  (`test_helper.exs`) and passes these pairs to `Ecto.Migrator` instead.

  The `test` alias migrates the same directory in this VM before `test_helper.exs` runs, so on a
  fresh database the compile replaces already-loaded modules; the conflict warning is suppressed
  because the compiled file is the same source that was loaded.
  """

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
