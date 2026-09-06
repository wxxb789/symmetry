defmodule SymmetryControl.Repo.Migrations.EnforceConnectedResourceIdentity do
  use Ecto.Migration

  @index :project_resources_connected_identity

  def up do
    # GitHub repository coordinates and Azure DevOps project/repository names are
    # case-insensitive. Azure CI references end in a numeric definition ID.
    execute("""
    CREATE UNIQUE INDEX #{@index}
    ON project_resources (
      project_id,
      connection_id,
      kind,
      lower(btrim(external_ref))
    )
    WHERE connection_id IS NOT NULL
    """)
  end

  def down do
    execute("DROP INDEX #{@index}")
  end
end
