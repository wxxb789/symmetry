defmodule SymmetryControlWeb.PortalResourceIdentityTest do
  use SymmetryControlWeb.ConnCase, async: false

  import SymmetryControlWeb.PortalFixtures, only: [create_connection: 4, create_project: 2]
  import SymmetryControlWeb.TestConn, only: [api_conn: 0]

  alias SymmetryControl.TestEnv

  setup do
    TestEnv.put!(:integration_providers,
      github: SymmetryControl.Integrations.ProviderStub,
      azure_devops: SymmetryControl.Integrations.ProviderStub
    )

    :ok
  end

  test "GitHub connected resource identity is case-insensitive on create" do
    project = create_project("GHID")

    connection =
      create_connection("github", "GitHub identity", "acme", ["repositories", "work_items"])

    assert %{"resource" => _resource} =
             create_resource(project, %{
               "connection_id" => connection["id"],
               "kind" => "repository",
               "name" => "Primary repository",
               "external_ref" => "Acme/Symmetry"
             })
             |> json_response(201)

    assert %{
             "error" => %{
               "code" => "validation_failed",
               "fields" => %{
                 "external_ref" => ["has already been connected for this resource kind"]
               }
             }
           } =
             create_resource(project, %{
               "connection_id" => connection["id"],
               "kind" => "repository",
               "name" => "Duplicate repository",
               "external_ref" => "acme/symmetry"
             })
             |> json_response(422)

    assert %{"resource" => _tracker} =
             create_resource(project, %{
               "connection_id" => connection["id"],
               "kind" => "work_tracking",
               "name" => "GitHub Issues",
               "external_ref" => "acme/symmetry"
             })
             |> json_response(201)

    other_connection =
      create_connection("github", "GitHub identity two", "acme", ["repositories"])

    assert %{"resource" => _other_connection_resource} =
             create_resource(project, %{
               "connection_id" => other_connection["id"],
               "kind" => "repository",
               "name" => "Other connection repository",
               "external_ref" => "acme/symmetry"
             })
             |> json_response(201)

    assert %{"resource" => _first_manual} =
             create_resource(project, %{
               "kind" => "repository",
               "name" => "Manual repository one",
               "external_ref" => "acme/symmetry"
             })
             |> json_response(201)

    assert %{"resource" => _second_manual} =
             create_resource(project, %{
               "kind" => "repository",
               "name" => "Manual repository two",
               "external_ref" => "ACME/SYMMETRY"
             })
             |> json_response(201)
  end

  test "Azure DevOps connected resource identity is case-insensitive on update" do
    project = create_project("AZID")

    connection =
      create_connection("azure_devops", "Azure identity", "acme", ["repositories"])

    assert %{"resource" => _primary} =
             create_resource(project, %{
               "connection_id" => connection["id"],
               "kind" => "repository",
               "name" => "Primary Azure repository",
               "external_ref" => "Platform/Symmetry"
             })
             |> json_response(201)

    assert %{"resource" => candidate} =
             create_resource(project, %{
               "connection_id" => connection["id"],
               "kind" => "repository",
               "name" => "Candidate Azure repository",
               "external_ref" => "Platform/Other"
             })
             |> json_response(201)

    assert %{
             "error" => %{
               "code" => "validation_failed",
               "fields" => %{
                 "external_ref" => ["has already been connected for this resource kind"]
               }
             }
           } =
             api_conn()
             |> patch("/portal/api/resources/#{candidate["id"]}", %{
               "version" => candidate["version"],
               "external_ref" => "platform/symmetry"
             })
             |> json_response(422)
  end

  defp create_project(key), do: create_project(key, "Connected identity #{key}")

  defp create_resource(project, attrs) do
    api_conn()
    |> post("/portal/api/projects/#{project["id"]}/resources", attrs)
  end
end
