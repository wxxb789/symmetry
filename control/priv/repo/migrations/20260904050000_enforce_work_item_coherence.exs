defmodule SymmetryControl.Repo.Migrations.EnforceWorkItemCoherence do
  use Ecto.Migration

  def up do
    execute("""
    UPDATE work_items
    SET assignee_name = NULL, agent_profile = NULL, workspace = NULL
    WHERE assignee_type = 'unassigned'
    """)

    execute("""
    UPDATE work_items
    SET agent_profile = NULL, workspace = NULL
    WHERE assignee_type = 'human'
    """)

    execute("UPDATE work_items SET blocker = NULL WHERE blocked = FALSE")

    drop constraint(:work_items, :work_items_blocker_check)

    create constraint(:work_items, :work_items_blocker_check,
             check:
               "(blocked = TRUE AND NULLIF(BTRIM(blocker), '') IS NOT NULL) OR " <>
                 "(blocked = FALSE AND blocker IS NULL)"
           )

    create constraint(:work_items, :work_items_owner_fields_check,
             check:
               "(assignee_type = 'unassigned' AND assignee_name IS NULL AND agent_profile IS NULL AND workspace IS NULL) OR " <>
                 "(assignee_type = 'human' AND NULLIF(BTRIM(assignee_name), '') IS NOT NULL AND agent_profile IS NULL AND workspace IS NULL) OR " <>
                 "(assignee_type = 'agent' AND NULLIF(BTRIM(assignee_name), '') IS NOT NULL AND NULLIF(BTRIM(agent_profile), '') IS NOT NULL AND NULLIF(BTRIM(workspace), '') IS NOT NULL)"
           )
  end

  def down do
    drop constraint(:work_items, :work_items_owner_fields_check)
    drop constraint(:work_items, :work_items_blocker_check)

    create constraint(:work_items, :work_items_blocker_check,
             check: "blocked = FALSE OR NULLIF(BTRIM(blocker), '') IS NOT NULL"
           )
  end
end
