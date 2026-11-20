defmodule Mix.Tasks.Symmetry.CheckTestDbTest do
  use ExUnit.Case, async: false

  alias Mix.Tasks.Symmetry.CheckTestDb

  @acceptance_database_prefixes ~w(symmetry_0007c_ symmetry_0007d_ symmetry_0007e_)
  @acceptance_databases ~w(symmetry_dev)

  # Every script that configures a database, and the value it must keep
  # configuring: a renamed, moved or rewritten script fails here instead of
  # silently dropping out of the cross-check below.
  @database_owning_scripts %{
    "run-compose-daemon-tests.ps1" => "symmetry_dev",
    "run-connection-fixture-tests.ps1" => "symmetry_0007e_test_$Port",
    "run-planning-tests.ps1" => "symmetry_0007c_test"
  }

  # The database values a harness script assigns: a literal
  # `POSTGRES_DB = "…"`/`'…'`, the literal default of a `-DbName` parameter in
  # either quote style, and the `-d` argument of the psql invocation the compose
  # harness fingerprints rows with, quoted or not. A bare mention (for example a
  # log-file name) is not an assignment.
  @assigned_database_patterns [
    ~r/POSTGRES_DB\s*=\s*["']([^"']+)["']/,
    ~r/\$DbName\s*=\s*["']([^"']+)["']/,
    ~r/-d\s+"?(symmetry\w+)"?/
  ]

  describe "check/2" do
    test "refuses every browser acceptance database family in the test environment" do
      # The planning harness takes its database as a `-DbName` parameter, so a
      # recorded run's `symmetry_0007c_shell` must be refused like any suffix.
      for prefix <- @acceptance_database_prefixes,
          database <- [prefix <> "test", prefix <> "shell"] do
        assert {:error, message} = CheckTestDb.check(:test, database)
        assert message =~ ~s("#{database}")
        assert message =~ "symmetry_control_test"
        assert message =~ "any suffix"
      end
    end

    test "refuses the compose acceptance database in the test environment" do
      for database <- @acceptance_databases do
        assert {:error, message} = CheckTestDb.check(:test, database)
        assert message =~ ~s("#{database}")
      end
    end

    test "refuses every database value the browser harness scripts assign" do
      scripts = read_scripts()

      for {script, expected} <- @database_owning_scripts do
        assigned = scripts |> Map.get(script, "") |> assigned_databases()

        assert expected in assigned,
               "#{script} no longer assigns #{expected}; update the guard list and this pin together"
      end

      for {script, contents} <- scripts, database <- assigned_databases(contents) do
        case CheckTestDb.check(:test, database) do
          {:error, message} ->
            assert message =~ database

          :ok ->
            flunk("#{script} assigns #{inspect(database)} but the guard does not refuse it")
        end
      end
    end

    test "extracts an assigned database value in either quote style" do
      contents = """
      $env:POSTGRES_DB = 'symmetry_0008e_test'
      $env:POSTGRES_DB = "symmetry_0008g_test"
      [string]$DbName = 'symmetry_0008h_test'
      docker compose exec -T postgres psql -U symmetry -d "symmetry_0008f_test" -At -c $Sql
      tee symmetry_0008i-run.log
      """

      assert contents |> assigned_databases() |> Enum.sort() ==
               ~w(symmetry_0008e_test symmetry_0008f_test
                  symmetry_0008g_test symmetry_0008h_test)
    end

    test "refuses the effective name a MIX_TEST_PARTITION suffix produces" do
      assert {:error, message} = CheckTestDb.check(:test, "symmetry_0007d_test16")
      assert message =~ ~s("symmetry_0007d_test16")
      assert message =~ "symmetry_0007d_"
    end

    test "allows the repository default with and without a partition" do
      assert :ok = CheckTestDb.check(:test, "symmetry_control_test")
      assert :ok = CheckTestDb.check(:test, "symmetry_control_test16")
    end

    test "allows the disposable goal receipt e2e database" do
      database = "symmetry_receipt_e2e_0123456789abcdef0123456789abcdef"

      assert :ok = CheckTestDb.check(:test, database)
    end

    test "does not restrict the harnesses' own development environment" do
      databases =
        Enum.map(@acceptance_database_prefixes, &(&1 <> "test")) ++ @acceptance_databases

      for database <- databases do
        assert :ok = CheckTestDb.check(:dev, database)
        assert :ok = CheckTestDb.check(:prod, database)
      end
    end

    test "allows an unconfigured database so Ecto reports its own error" do
      assert :ok = CheckTestDb.check(:test, nil)
    end
  end

  test "run/1 refuses the database the repository configures" do
    repo_config = Application.get_env(:symmetry_control, SymmetryControl.Repo) || []

    Application.put_env(
      :symmetry_control,
      SymmetryControl.Repo,
      Keyword.put(repo_config, :database, "symmetry_0007e_shell16")
    )

    on_exit(fn -> Application.put_env(:symmetry_control, SymmetryControl.Repo, repo_config) end)

    assert_raise Mix.Error,
                 ~r/refusing to use the browser acceptance database "symmetry_0007e_shell16"/,
                 fn -> CheckTestDb.run([]) end
  end

  test "the storage aliases run the guard before any database work" do
    aliases = Mix.Project.config()[:aliases]

    assert hd(aliases[:test]) == "symmetry.check_test_db"

    # The rest of the documented `test` alias body: quality.md's recovery recipe
    # relies on the create/migrate steps that heal the database `mix test` resolves.
    assert "ecto.create --quiet" in aliases[:test]
    assert "ecto.migrate --quiet" in aliases[:test]

    for task <- ~w(ecto.create ecto.migrate ecto.migrations ecto.drop ecto.rollback ecto.load) do
      assert hd(aliases[String.to_atom(task)]) == "symmetry.check_test_db"
      assert task in aliases[String.to_atom(task)]
    end
  end

  defp read_scripts do
    "../../../../browser/scripts/*.ps1"
    |> Path.expand(__DIR__)
    |> Path.wildcard()
    |> Map.new(&{Path.basename(&1), File.read!(&1)})
  end

  defp assigned_databases(contents) do
    @assigned_database_patterns
    |> Enum.flat_map(&Regex.scan(&1, contents, capture: :all_but_first))
    |> Enum.concat()
    |> Enum.uniq()
  end
end
