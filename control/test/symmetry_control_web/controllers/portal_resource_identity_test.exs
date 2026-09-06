defmodule SymmetryControlWeb.PortalResourceIdentityTest do
  use SymmetryControlWeb.ConnCase, async: false

  alias SymmetryControlWeb.PortalSession

  @operator_token "test-operator-token"

  setup do
    previous = Application.get_env(:symmetry_control, :integration_providers)

    Application.put_env(:symmetry_control, :integration_providers,
      github: SymmetryControl.Integrations.ProviderStub,
      azure_devops: SymmetryControl.Integrations.ProviderStub
    )

    on_exit(fn ->
      if previous do
        Application.put_env(:symmetry_control, :integration_providers, previous)
      else
        Application.delete_env(:symmetry_control, :integration_providers)
      end
    end)

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

  defp create_project(key) do
    %{"project" => project} =
      api_conn()
      |> post("/portal/api/projects", %{
        "name" => "Connected identity #{key}",
        "key" => key,
        "default_agent_profile" => "codex",
        "default_workspace" => "primary"
      })
      |> json_response(201)

    project
  end

  defp create_connection(provider, name, account_ref, capabilities) do
    %{"connection" => connection} =
      api_conn()
      |> post("/portal/api/connections", %{
        "provider" => provider,
        "name" => name,
        "account_ref" => account_ref,
        "capabilities" => capabilities
      })
      |> json_response(201)

    connection
  end

  defp create_resource(project, attrs) do
    api_conn()
    |> post("/portal/api/projects/#{project["id"]}/resources", attrs)
  end

  defp api_conn do
    Phoenix.ConnTest.build_conn()
    |> init_test_session(%{portal_operator: PortalSession.issue(@operator_token)})
    |> put_private(:plug_skip_csrf_protection, true)
    |> put_req_header("accept", "application/json")
  end
end
