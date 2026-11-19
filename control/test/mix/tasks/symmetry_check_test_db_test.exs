defmodule Mix.Tasks.Symmetry.CheckTestDbTest do
  use ExUnit.Case, async: false

  alias Mix.Tasks.Symmetry.CheckTestDb

  @acceptance_database_prefixes ~w(symmetry_0007c_ symmetry_0007d_ symmetry_0007e_)

  describe "check/2" do
    test "refuses every browser acceptance database family in the test environment" do
      for prefix <- @acceptance_database_prefixes,
          database <- [prefix <> "test", prefix <> "shell"] do
        assert {:error, message} = CheckTestDb.check(:test, database)
        assert message =~ ~s("#{database}")
        assert message =~ "symmetry_control_test"
        assert message =~ "any suffix"
      end
    end

    test "refuses a harness database named outside its default, because -DbName is a parameter" do
      assert {:error, _message} = CheckTestDb.check(:test, "symmetry_0007c_shell")
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
      for prefix <- @acceptance_database_prefixes do
        assert :ok = CheckTestDb.check(:dev, prefix <> "test")
        assert :ok = CheckTestDb.check(:prod, prefix <> "test")
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

    for task <- ~w(ecto.create ecto.migrate ecto.drop ecto.rollback ecto.load) do
      assert hd(aliases[String.to_atom(task)]) == "symmetry.check_test_db"
      assert task in aliases[String.to_atom(task)]
    end
  end
end
