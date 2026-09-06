defmodule SymmetryControl.Repo.Migrations.AddTaskRequiredCapabilities do
  use Ecto.Migration

  def change do
    alter table(:tasks) do
      add :required_capabilities, :map, null: false, default: %{}
    end
  end
end
