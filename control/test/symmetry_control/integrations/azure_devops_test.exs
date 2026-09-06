defmodule SymmetryControl.Integrations.AzureDevOpsTest do
  use ExUnit.Case, async: false

  alias SymmetryControl.Integrations.HTTPStub
  alias SymmetryControl.Integrations.Providers.AzureDevOps

  setup do
    previous = Application.get_env(:symmetry_control, :integration_http_client)
    previous_auth = Application.get_env(:symmetry_control, :integration_auth_provider)
    Application.put_env(:symmetry_control, :integration_http_client, HTTPStub)

    Application.put_env(
      :symmetry_control,
      :integration_auth_provider,
      SymmetryControl.Integrations.AuthStub
    )

    on_exit(fn ->
      HTTPStub.expect([])

      if previous do
        Application.put_env(:symmetry_control, :integration_http_client, previous)
      else
        Application.delete_env(:symmetry_control, :integration_http_client)
      end

      if previous_auth do
        Application.put_env(:symmetry_control, :integration_auth_provider, previous_auth)
      else
        Application.delete_env(:symmetry_control, :integration_auth_provider)
      end
    end)

    :ok
  end

  test "checks an organization and normalizes Azure Boards work items" do
    auth = "Bearer azure-token"

    HTTPStub.expect([
      %{
        method: :get,
        url_contains: "https://dev.azure.com/acme/_apis/projects?",
        headers: %{"authorization" => auth},
        response:
          {:ok, 200, %{}, %{"count" => 1, "value" => [%{"id" => "p1", "name" => "Platform"}]}}
      },
      %{
        method: :post,
        url_contains: "/acme/Platform/_apis/wit/wiql?",
        url_excludes: ["%24top", "$top"],
        body_assertion: fn body ->
          unless body["query"] =~ "System.TeamProject", do: raise("missing project query")
        end,
        response: {:ok, 200, %{}, %{"workItems" => [%{"id" => 77}]}}
      },
      %{
        method: :post,
        url_contains: "/acme/Platform/_apis/wit/workitemsbatch?",
        body_assertion: fn body ->
          unless body["ids"] == [77], do: raise("unexpected work item ids")
        end,
        response:
          {:ok, 200, %{},
           %{
             "value" => [
               %{
                 "id" => 77,
                 "url" => "https://dev.azure.com/acme/_apis/wit/workItems/77",
                 "fields" => %{
                   "System.Title" => "Connect Azure Boards",
                   "System.Description" => "Keep Boards authoritative.",
                   "System.State" => "Active",
                   "System.Tags" => "agent-ready; priority:urgent",
                   "System.AssignedTo" => %{"displayName" => "Grace Hopper"},
                   "System.ChangedDate" => "2026-09-05T10:00:00Z"
                 },
                 "_links" => %{
                   "html" => %{"href" => "https://dev.azure.com/acme/Platform/_workitems/edit/77"}
                 }
               }
             ]
           }}
      }
    ])

    connection = %{account_ref: "acme", auth_type: "entra_id", capabilities: ["work_items"]}
    resource = %{kind: "work_tracking", external_ref: "Platform"}

    assert {:ok, metadata} = AzureDevOps.check(connection)
    assert metadata["project_count"] == 1

    assert {:ok, %{work_items: [item]}} =
             AzureDevOps.sync_resource(connection, resource)

    assert item.external_id == "77"
    assert item.title == "Connect Azure Boards"
    assert item.external_state == "Active"
    assert item.status == "ready"
    assert item.priority == "urgent"
    assert item.labels == ["agent-ready", "priority:urgent"]
    assert item.external_assignee_name == "Grace Hopper"
    HTTPStub.verify!()
  end

  test "does not invent Azure work item timestamps when ChangedDate is missing or malformed" do
    fields = %{
      "System.Title" => "Connected work",
      "System.State" => "Active"
    }

    HTTPStub.expect([
      %{
        method: :post,
        url_contains: "/acme/Platform/_apis/wit/wiql?",
        response: {:ok, 200, %{}, %{"workItems" => [%{"id" => 77}, %{"id" => 78}]}}
      },
      %{
        method: :post,
        url_contains: "/acme/Platform/_apis/wit/workitemsbatch?",
        response:
          {:ok, 200, %{},
           %{
             "value" => [
               %{
                 "id" => 77,
                 "url" => "https://dev.azure.com/acme/_apis/wit/workItems/77",
                 "fields" => fields
               },
               %{
                 "id" => 78,
                 "url" => "https://dev.azure.com/acme/_apis/wit/workItems/78",
                 "fields" => Map.put(fields, "System.ChangedDate", "not-a-timestamp")
               }
             ]
           }}
      }
    ])

    assert {:ok, %{work_items: work_items}} =
             AzureDevOps.sync_resource(
               %{account_ref: "acme", auth_type: "entra_id", capabilities: ["work_items"]},
               %{kind: "work_tracking", external_ref: "Platform"}
             )

    assert Enum.map(work_items, & &1.external_updated_at) == [nil, nil]
    HTTPStub.verify!()
  end

  test "normalizes Azure Repos pull request, reviewer votes, and Pipelines status" do
    HTTPStub.expect([
      %{
        method: :get,
        url_contains: "/acme/Platform/_apis/git/repositories/symmetry/pullrequests/84?",
        response:
          {:ok, 200, %{},
           %{
             "pullRequestId" => 84,
             "status" => "active",
             "sourceRefName" => "refs/heads/codex/external-77",
             "lastMergeSourceCommit" => %{"commitId" => "def456"},
             "lastMergeCommit" => %{"commitId" => "merge789"},
             "reviewers" => [
               %{"displayName" => "Reviewer", "vote" => 5}
             ]
           }}
      },
      %{
        method: :get,
        url_contains: "repositoryId=repository-id",
        response:
          {:ok, 200, %{},
           %{
             "value" => [
               %{
                 "definition" => %{"id" => 1},
                 "status" => "completed",
                 "result" => "succeeded",
                 "sourceVersion" => "merge789",
                 "_links" => %{
                   "web" => %{
                     "href" => "https://dev.azure.com/acme/Platform/_build/results?buildId=5"
                   }
                 }
               },
               %{
                 "definition" => %{"id" => 1},
                 "status" => "completed",
                 "result" => "failed",
                 "sourceVersion" => "older-commit"
               }
             ]
           }}
      }
    ])

    connection = %{
      account_ref: "acme",
      auth_type: "entra_id",
      capabilities: ["changes", "ci"]
    }

    resource = %{
      kind: "repository",
      external_ref: "Platform/symmetry",
      metadata: %{"repository_id" => "repository-id"}
    }

    item = %{
      pull_request_url: "https://dev.azure.com/acme/Platform/_git/symmetry/pullrequest/84",
      branch: "codex/external-77"
    }

    assert {:ok, delivery} =
             AzureDevOps.sync_delivery(connection, resource, item)

    assert delivery.pull_request_state == "open"
    assert delivery.review_status == "approved"
    assert delivery.ci_status == "passed"
    assert delivery.provider_data["commit_id"] == "def456"
    HTTPStub.verify!()
  end

  test "synchronizes a separately bound Azure Pipelines resource" do
    HTTPStub.expect([
      %{
        method: :get,
        url_contains: "definitions=42",
        url_excludes: ["repositoryId", "repositoryType", "sourceVersion"],
        response:
          {:ok, 200, %{},
           %{
             "value" => [
               %{
                 "status" => "completed",
                 "result" => "succeeded",
                 "sourceVersion" => "abc123",
                 "_links" => %{"web" => %{"href" => "https://dev.azure.com/acme/build/9"}}
               }
             ]
           }}
      }
    ])

    connection = %{account_ref: "acme", auth_type: "entra_id", capabilities: ["ci"]}
    resource = %{kind: "ci", external_ref: "Platform/pipeline/42"}
    item = %{external_change_data: %{"head_sha" => "abc123"}}

    assert {:ok, %{ci_status: "passed"}} =
             AzureDevOps.sync_ci(connection, resource, item, [
               {"authorization", "Bearer azure-token"}
             ])

    HTTPStub.verify!()
  end

  test "synchronizes a GitHub-backed Pipeline by Azure definition id" do
    HTTPStub.expect([
      %{
        method: :get,
        url_contains: "definitions=42",
        url_excludes: ["repositoryId", "repositoryType", "sourceVersion"],
        response:
          {:ok, 200, %{},
           %{
             "value" => [
               %{
                 "definition" => %{"id" => 42},
                 "status" => "completed",
                 "result" => "succeeded",
                 "sourceVersion" => "different-commit"
               },
               %{
                 "definition" => %{"id" => 42},
                 "status" => "completed",
                 "result" => "failed",
                 "sourceVersion" => "github-merge-commit",
                 "triggerInfo" => %{"pr.sourceSha" => "abc123"},
                 "_links" => %{"web" => %{"href" => "https://dev.azure.com/acme/build/42"}}
               }
             ]
           }}
      }
    ])

    connection = %{account_ref: "acme", auth_type: "entra_id", capabilities: ["ci"]}
    resource = %{kind: "ci", external_ref: "Platform/pipeline/42"}
    item = %{external_change_data: %{"head_sha" => "abc123"}}

    assert {:ok, result} =
             AzureDevOps.sync_ci(connection, resource, item, [
               {"authorization", "Bearer azure-token"}
             ])

    assert result.ci_status == "failed"
    assert result.provider_data["ci_url"] == "https://dev.azure.com/acme/build/42"
    HTTPStub.verify!()
  end

  test "paginates Pipeline builds until the target commit is found" do
    HTTPStub.expect([
      %{
        method: :get,
        url_contains: "definitions=42",
        url_excludes: ["continuationToken"],
        response:
          {:ok, 200, %{"x-ms-continuationtoken" => "next-page"},
           %{
             "value" => [
               %{
                 "definition" => %{"id" => 42},
                 "status" => "completed",
                 "result" => "succeeded",
                 "sourceVersion" => "newer-commit"
               }
             ]
           }}
      },
      %{
        method: :get,
        url_contains: "continuationToken=next-page",
        response:
          {:ok, 200, %{},
           %{
             "value" => [
               %{
                 "definition" => %{"id" => 42},
                 "status" => "completed",
                 "result" => "succeeded",
                 "sourceVersion" => "abc123"
               }
             ]
           }}
      }
    ])

    connection = %{account_ref: "acme", auth_type: "entra_id", capabilities: ["ci"]}
    resource = %{kind: "ci", external_ref: "Platform/pipeline/42"}
    item = %{external_change_data: %{"head_sha" => "abc123"}}

    assert {:ok, %{ci_status: "passed"}} =
             AzureDevOps.sync_ci(connection, resource, item, [
               {"authorization", "Bearer azure-token"}
             ])

    HTTPStub.verify!()
  end

  test "bounds Pipeline history searches when the target commit is absent" do
    expectations =
      Enum.map(1..10, fn page ->
        %{
          method: :get,
          url_contains:
            if(page == 1,
              do: "definitions=42",
              else: "continuationToken=page-#{page}"
            ),
          url_excludes: if(page == 1, do: ["continuationToken"], else: []),
          response:
            {:ok, 200, %{"x-ms-continuationtoken" => "page-#{page + 1}"},
             %{
               "value" => [
                 %{
                   "definition" => %{"id" => 42},
                   "status" => "completed",
                   "result" => "succeeded",
                   "sourceVersion" => "unrelated-#{page}"
                 }
               ]
             }}
        }
      end)

    HTTPStub.expect(expectations)

    connection = %{account_ref: "acme", auth_type: "entra_id", capabilities: ["ci"]}
    resource = %{kind: "ci", external_ref: "Platform/pipeline/42"}
    item = %{external_change_data: %{"head_sha" => "missing-commit"}}

    assert {:error, :ci_history_limit} =
             AzureDevOps.sync_ci(connection, resource, item, [
               {"authorization", "Bearer azure-token"}
             ])

    HTTPStub.verify!()
  end

  test "does not treat absent or unrecognized completed Pipeline results as passed" do
    HTTPStub.expect([
      %{
        method: :get,
        url_contains: "definitions=42",
        url_excludes: ["repositoryId", "repositoryType", "sourceVersion"],
        response:
          {:ok, 200, %{},
           %{
             "value" => [
               %{
                 "definition" => %{"id" => 1},
                 "status" => "completed",
                 "result" => "none",
                 "sourceVersion" => "abc123"
               },
               %{
                 "definition" => %{"id" => 2},
                 "status" => "completed",
                 "result" => "future_result",
                 "sourceVersion" => "abc123"
               }
             ]
           }}
      }
    ])

    connection = %{account_ref: "acme", auth_type: "entra_id", capabilities: ["ci"]}
    resource = %{kind: "ci", external_ref: "Platform/pipeline/42"}
    item = %{external_change_data: %{"head_sha" => "abc123"}}

    assert {:ok, %{ci_status: "unknown"}} =
             AzureDevOps.sync_ci(connection, resource, item, [
               {"authorization", "Bearer azure-token"}
             ])

    HTTPStub.verify!()
  end

  test "accepts URL-encoded Azure project and repository names in pull request URLs" do
    HTTPStub.expect([
      %{
        method: :get,
        url_contains:
          "/acme/Platform%20Team/_apis/git/repositories/symmetry%20repo/pullrequests/84?",
        response:
          {:ok, 200, %{},
           %{
             "status" => "active",
             "lastMergeSourceCommit" => %{"commitId" => "def456"},
             "reviewers" => []
           }}
      },
      %{
        method: :get,
        url_contains: "/acme/Platform%20Team/_apis/build/builds?",
        response: {:ok, 200, %{}, %{"value" => []}}
      }
    ])

    connection = %{
      account_ref: "acme",
      auth_type: "entra_id",
      capabilities: ["changes", "ci"]
    }

    resource = %{
      kind: "repository",
      external_ref: "Platform Team/symmetry repo",
      metadata: %{"repository_id" => "repository-id"}
    }

    item = %{
      pull_request_url:
        "https://dev.azure.com/acme/Platform%20Team/_git/symmetry%20repo/pullrequest/84"
    }

    assert {:ok, delivery} = AzureDevOps.sync_delivery(connection, resource, item)
    assert delivery.pull_request_state == "open"
    HTTPStub.verify!()
  end

  test "syncs Azure Pipelines with CI-only capability" do
    HTTPStub.expect([
      %{
        method: :get,
        url_contains: "/acme/Platform/_apis/build/definitions/42?",
        response:
          {:ok, 200, %{},
           %{
             "id" => 42,
             "name" => "Cross-provider CI",
             "repository" => %{"type" => "GitHub", "name" => "acme/symmetry"}
           }}
      },
      %{
        method: :get,
        url_contains: "definitions=42",
        url_excludes: ["repositoryId", "repositoryType"],
        response:
          {:ok, 200, %{},
           %{
             "value" => [
               %{
                 "id" => 9,
                 "status" => "completed",
                 "result" => "succeeded",
                 "sourceVersion" => "abc123"
               }
             ]
           }}
      }
    ])

    connection = %{account_ref: "acme", auth_type: "entra_id", capabilities: ["ci"]}
    resource = %{kind: "ci", external_ref: "Platform/pipeline/42"}

    assert {:ok, %{resource: %{metadata: metadata}}} =
             AzureDevOps.sync_resource(connection, resource)

    assert metadata["latest"]["result"] == "succeeded"
    assert metadata["definition_id"] == 42
    assert metadata["definition_name"] == "Cross-provider CI"
    assert metadata["repository_type"] == "GitHub"
    HTTPStub.verify!()
  end

  test "rejects a missing Azure Pipeline definition during resource health check" do
    HTTPStub.expect([
      %{
        method: :get,
        url_contains: "/acme/Platform/_apis/build/definitions/999999?",
        response: {:ok, 404, %{}, %{"message" => "Definition not found"}}
      }
    ])

    connection = %{account_ref: "acme", auth_type: "entra_id", capabilities: ["ci"]}
    resource = %{kind: "ci", external_ref: "Platform/pipeline/999999"}

    assert {:error, {:http, 404, _body}} = AzureDevOps.sync_resource(connection, resource)
    HTTPStub.verify!()
  end

  test "validates Azure Pipeline definition references" do
    connection = %{account_ref: "acme"}

    assert :ok =
             AzureDevOps.validate_resource_reference(
               connection,
               "ci",
               "Platform/pipeline/42"
             )

    assert {:error, :invalid_request} =
             AzureDevOps.validate_resource_reference(
               connection,
               "ci",
               "Platform/pipeline/not-an-id"
             )

    assert {:error, :invalid_request} =
             AzureDevOps.validate_resource_reference(connection, "ci", "Platform/symmetry")
  end

  test "creates an Azure Repos pull request through the typed change action" do
    headers = [{"authorization", "Bearer azure-token"}]

    HTTPStub.expect([
      %{
        method: :get,
        url_contains: "searchCriteria.sourceRefName=refs%2Fheads%2Fcodex%2Fgoal-3",
        headers: %{"authorization" => "Bearer azure-token"},
        response: {:ok, 200, %{}, %{"value" => []}}
      },
      %{
        method: :post,
        url_contains: "/acme/Platform/_apis/git/repositories/symmetry/pullrequests?",
        body_assertion: fn body ->
          unless body == %{
                   "sourceRefName" => "refs/heads/codex/goal-3",
                   "targetRefName" => "refs/heads/main",
                   "title" => "Connect providers",
                   "description" => "Typed action"
                 },
                 do: raise("unexpected Azure pull request body")
        end,
        response:
          {:ok, 201, %{},
           %{
             "pullRequestId" => 84,
             "status" => "active",
             "lastMergeSourceCommit" => %{"commitId" => "abc123"},
             "_links" => %{
               "web" => %{
                 "href" => "https://dev.azure.com/acme/Platform/_git/symmetry/pullrequest/84"
               }
             }
           }}
      }
    ])

    assert {:ok, delivery} =
             AzureDevOps.execute(
               %{account_ref: "acme", capabilities: ["changes"]},
               %{kind: "repository", external_ref: "Platform/symmetry"},
               %{},
               "change.upsert",
               %{
                 "source_branch" => "codex/goal-3",
                 "target_branch" => "main",
                 "title" => "Connect providers",
                 "body" => "Typed action"
               },
               headers
             )

    assert delivery.pull_request_url ==
             "https://dev.azure.com/acme/Platform/_git/symmetry/pullrequest/84"

    assert delivery.pull_request_state == "open"
    assert delivery.provider_data["commit_id"] == "abc123"
    HTTPStub.verify!()
  end

  test "replays an existing active Azure Repos pull request instead of creating another" do
    HTTPStub.expect([
      %{
        method: :get,
        url_contains: "searchCriteria.sourceRefName=refs%2Fheads%2Fcodex%2Fgoal-3",
        response:
          {:ok, 200, %{},
           %{
             "value" => [
               %{
                 "pullRequestId" => 84,
                 "status" => "active",
                 "_links" => %{
                   "web" => %{
                     "href" => "https://dev.azure.com/acme/Platform/_git/symmetry/pullrequest/84"
                   }
                 }
               }
             ]
           }}
      },
      %{
        method: :patch,
        url_contains: "/acme/Platform/_apis/git/repositories/symmetry/pullrequests/84?",
        body_assertion: fn body ->
          unless body == %{
                   "title" => "Connect providers",
                   "description" => "Reconciled body"
                 },
                 do: raise("existing Azure pull request was not reconciled")
        end,
        response:
          {:ok, 200, %{},
           %{
             "pullRequestId" => 84,
             "status" => "active",
             "_links" => %{
               "web" => %{
                 "href" => "https://dev.azure.com/acme/Platform/_git/symmetry/pullrequest/84"
               }
             }
           }}
      }
    ])

    assert {:ok,
            %{
              pull_request_url: "https://dev.azure.com/acme/Platform/_git/symmetry/pullrequest/84"
            }} =
             AzureDevOps.execute(
               %{account_ref: "acme", capabilities: ["changes"]},
               %{kind: "repository", external_ref: "Platform/symmetry"},
               %{},
               "change.upsert",
               %{
                 "source_branch" => "refs/heads/codex/goal-3",
                 "target_branch" => "refs/heads/main",
                 "title" => "Connect providers",
                 "body" => "Reconciled body"
               },
               []
             )

    HTTPStub.verify!()
  end

  test "replays the winning pull request when Azure create races with another request" do
    existing = %{
      "pullRequestId" => 84,
      "status" => "active",
      "_links" => %{
        "web" => %{
          "href" => "https://dev.azure.com/acme/Platform/_git/symmetry/pullrequest/84"
        }
      }
    }

    HTTPStub.expect([
      %{
        method: :get,
        url_contains: "searchCriteria.sourceRefName=refs%2Fheads%2Fcodex%2Fgoal-3",
        response: {:ok, 200, %{}, %{"value" => []}}
      },
      %{
        method: :post,
        url_contains: "/acme/Platform/_apis/git/repositories/symmetry/pullrequests?",
        response: {:ok, 409, %{}, %{"message" => "Pull request already exists"}}
      },
      %{
        method: :get,
        url_contains: "searchCriteria.sourceRefName=refs%2Fheads%2Fcodex%2Fgoal-3",
        response: {:ok, 200, %{}, %{"value" => [existing]}}
      },
      %{
        method: :patch,
        url_contains: "/acme/Platform/_apis/git/repositories/symmetry/pullrequests/84?",
        body_assertion: fn body ->
          unless body == %{
                   "title" => "Connect providers",
                   "description" => "Reconciled body"
                 },
                 do: raise("raced Azure pull request was not reconciled")
        end,
        response: {:ok, 200, %{}, existing}
      }
    ])

    assert {:ok,
            %{
              pull_request_url: "https://dev.azure.com/acme/Platform/_git/symmetry/pullrequest/84"
            }} =
             AzureDevOps.execute(
               %{account_ref: "acme", capabilities: ["changes"]},
               %{kind: "repository", external_ref: "Platform/symmetry"},
               %{},
               "change.upsert",
               %{
                 "source_branch" => "codex/goal-3",
                 "target_branch" => "main",
                 "title" => "Connect providers",
                 "body" => "Reconciled body"
               },
               []
             )

    HTTPStub.verify!()
  end

  test "preserves an Azure 400 create error when conflict lookup finds no pull request" do
    error_body = %{"message" => "Invalid pull request"}

    HTTPStub.expect([
      %{
        method: :get,
        url_contains: "searchCriteria.sourceRefName=refs%2Fheads%2Fcodex%2Fgoal-3",
        response: {:ok, 200, %{}, %{"value" => []}}
      },
      %{
        method: :post,
        url_contains: "/acme/Platform/_apis/git/repositories/symmetry/pullrequests?",
        response: {:ok, 400, %{}, error_body}
      },
      %{
        method: :get,
        url_contains: "searchCriteria.sourceRefName=refs%2Fheads%2Fcodex%2Fgoal-3",
        response: {:ok, 200, %{}, %{"value" => []}}
      }
    ])

    assert {:error, {:http, 400, ^error_body}} =
             AzureDevOps.execute(
               %{account_ref: "acme", capabilities: ["changes"]},
               %{kind: "repository", external_ref: "Platform/symmetry"},
               %{},
               "change.upsert",
               %{
                 "source_branch" => "codex/goal-3",
                 "target_branch" => "main",
                 "title" => "Connect providers"
               },
               []
             )

    HTTPStub.verify!()
  end

  test "updates only Azure Repos pull request title and description" do
    HTTPStub.expect([
      %{
        method: :patch,
        url_contains: "/acme/Platform/_apis/git/repositories/symmetry/pullrequests/84?",
        body_assertion: fn body ->
          unless body == %{"title" => "Updated title", "description" => "Updated body"},
            do: raise("unexpected Azure pull request update")
        end,
        response:
          {:ok, 200, %{},
           %{
             "pullRequestId" => 84,
             "status" => "active",
             "_links" => %{
               "web" => %{
                 "href" => "https://dev.azure.com/acme/Platform/_git/symmetry/pullrequest/84"
               }
             }
           }}
      }
    ])

    assert {:ok, %{pull_request_state: "open"}} =
             AzureDevOps.execute(
               %{account_ref: "acme", capabilities: ["changes"]},
               %{kind: "repository", external_ref: "Platform/symmetry"},
               %{
                 external_pull_request_url:
                   "https://dev.azure.com/acme/Platform/_git/symmetry/pullrequest/84"
               },
               "change.update",
               %{"title" => "Updated title", "body" => "Updated body"},
               []
             )

    HTTPStub.verify!()
  end

  test "rejects invalid Azure change input and pull request URLs without making requests" do
    connection = %{account_ref: "acme", capabilities: ["changes"]}
    resource = %{kind: "repository", external_ref: "Platform/symmetry"}

    assert {:error, :invalid_request} =
             AzureDevOps.execute(
               connection,
               resource,
               %{},
               "change.upsert",
               %{
                 "source_branch" => "../unsafe",
                 "target_branch" => "main",
                 "title" => "Unsafe",
                 "url" => "https://example.test"
               },
               []
             )

    assert {:error, :invalid_pull_request_url} =
             AzureDevOps.execute(
               connection,
               resource,
               %{
                 external_pull_request_url:
                   "https://dev.azure.com/acme/Other/_git/repo/pullrequest/84"
               },
               "change.update",
               %{"title" => "Cannot escape binding"},
               []
             )

    assert {:error, :invalid_request} =
             AzureDevOps.execute(
               connection,
               resource,
               %{
                 external_pull_request_url:
                   "https://dev.azure.com/acme/Platform/_git/symmetry/pullrequest/84"
               },
               "change.update",
               %{"title" => "No branch mutation", "target_branch" => "other"},
               []
             )

    assert {:error, :invalid_pull_request_url} =
             AzureDevOps.execute(
               connection,
               resource,
               %{
                 external_pull_request_url:
                   "https://dev.azure.com/acme/Platform/_git/symmetry/pullrequest/84"
               },
               "change.update",
               %{
                 "title" => "Cannot change another PR",
                 "pull_request_url" =>
                   "https://dev.azure.com/acme/Platform/_git/symmetry/pullrequest/85"
               },
               []
             )

    HTTPStub.verify!()
  end
end
