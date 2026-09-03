defmodule SymmetryControl.Repo.Migrations.AddMachineEnrollmentReplay do
  use Ecto.Migration

  def change do
    alter table(:machines) do
      add :enrollment_idempotency_key, :string
      add :enrollment_request_hash, :binary
    end

    create unique_index(:machines, [:enrollment_idempotency_key])
  end
end
