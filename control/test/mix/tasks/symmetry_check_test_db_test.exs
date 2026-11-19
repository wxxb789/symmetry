defmodule Mix.Tasks.Symmetry.CheckTestDbTest do
  use ExUnit.Case, async: true

  alias Mix.Tasks.Symmetry.CheckTestDb

  @acceptance_databases ~w(symmetry_0007c_test symmetry_0007d_test symmetry_0007e_test)

  describe "check/2" do
    test "refuses every browser acceptance database in the test environment" do
      for database <- @acceptance_databases do
        assert {:error, message} = CheckTestDb.check(:test, database)
        assert message =~ database
        assert message =~ "symmetry_control_test"
      end
    end

    test "refuses the effective name a MIX_TEST_PARTITION suffix produces" do
      assert {:error, message} = CheckTestDb.check(:test, "symmetry_0007d_test16")
      assert message =~ ~s("symmetry_0007d_test16")
      assert message =~ ~s("symmetry_0007d_test")
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
      for database <- @acceptance_databases do
        assert :ok = CheckTestDb.check(:dev, database)
        assert :ok = CheckTestDb.check(:prod, database)
      end
    end

    test "allows an unconfigured database so Ecto reports its own error" do
      assert :ok = CheckTestDb.check(:test, nil)
    end
  end

  test "the storage aliases run the guard before any database work" do
    aliases = Mix.Project.config()[:aliases]

    assert hd(aliases[:test]) == "symmetry.check_test_db"

    for task <- ~w(ecto.create ecto.migrate ecto.drop) do
      assert hd(aliases[String.to_atom(task)]) == "symmetry.check_test_db"
      assert task in aliases[String.to_atom(task)]
    end
  end
end
