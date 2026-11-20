defmodule SymmetryControl.Repo.Migrations.AddGoalOperatorVersion do
  use Ecto.Migration

  # The operator-authority version a Goal command carries as `expected_version`: it
  # advances by exactly one per accepted operator command and never for internal
  # reconciliation, settlement, automatic admission or a non-command row write.
  # `lock_version` stays the row-level optimistic lock every Goal writer takes. The
  # backfill starts every existing Goal at the last version any client could have
  # read, so a read taken before this migration keeps matching and no served value
  # moves backwards.
  def up do
    alter table(:goals) do
      add :operator_version, :bigint, null: false, default: 1
    end

    execute("UPDATE goals SET operator_version = lock_version")

    create constraint(:goals, :goals_operator_version_positive, check: "operator_version > 0")
  end

  def down do
    drop constraint(:goals, :goals_operator_version_positive)

    alter table(:goals) do
      remove :operator_version
    end
  end
end
