defmodule SymmetryControl.Integrations.GitHubTest do
  use ExUnit.Case, async: false

  alias SymmetryControl.Integrations.HTTPStub
  alias SymmetryControl.Integrations.Providers.GitHub

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

  test "validates a pull request URL against the exact repository without network access" do
    resource = %{kind: "repository", external_ref: "acme/symmetry"}

    assert :ok ==
             GitHub.validate_pull_request_url(
               nil,
               resource,
               "https://github.com/acme/symmetry/pull/42"
             )

    assert {:error, :invalid_pull_request_url} =
             GitHub.validate_pull_request_url(
               nil,
               resource,
               "https://github.com/acme/other/pull/42"
             )

    assert {:error, :invalid_pull_request_url} =
             GitHub.validate_pull_request_url(
               nil,
               resource,
               "https://github.com/acme/symmetry/pull/0"
             )
  end

  test "checks identity and normalizes GitHub Issues without importing pull requests" do
    HTTPStub.expect([
      %{
        method: :get,
        url_contains: "https://api.github.com/user",
        headers: %{
          "authorization" => "Bearer gho_github-token",
          "x-github-api-version" => "2026-03-10"
        },
        response:
          {:ok, 200, %{}, %{"login" => "octocat", "html_url" => "https://github.com/octocat"}}
      },
      %{
        method: :get,
        url_contains: "https://api.github.com/user/memberships/orgs/acme",
        headers: %{"authorization" => "Bearer gho_github-token"},
        response:
          {:ok, 200, %{},
           %{
             "state" => "active",
             "organization" => %{
               "login" => "acme",
               "html_url" => "https://github.com/acme"
             }
           }}
      },
      %{
        method: :get,
        url_contains: "/repos/acme/symmetry/issues?",
        headers: %{"authorization" => "Bearer gho_github-token"},
        response:
          {:ok, 200, %{},
           [
             %{
               "number" => 12,
               "title" => "Ship connected work",
               "body" => "Keep GitHub authoritative.",
               "state" => "open",
               "html_url" => "https://github.com/acme/symmetry/issues/12",
               "updated_at" => "2026-09-05T09:30:00Z",
               "labels" => [%{"name" => "priority:high"}],
               "assignee" => %{"login" => "octocat"}
             },
             %{
               "number" => 13,
               "title" => "A pull request is not an issue",
               "pull_request" => %{"url" => "https://api.github.com/repos/acme/symmetry/pulls/13"}
             }
           ]}
      }
    ])

    connection = %{account_ref: "acme", auth_type: "gh_cli", capabilities: ["work_items"]}
    resource = %{kind: "work_tracking", external_ref: "acme/symmetry"}

    assert {:ok, metadata} = GitHub.check(connection)
    assert metadata["actor"] == "octocat"
    assert metadata["account"] == "acme"

    assert {:ok, %{work_items: [item]}} =
             GitHub.sync_resource(connection, resource)

    assert item.external_id == "12"
    assert item.title == "Ship connected work"
    assert item.status == "ready"
    assert item.priority == "high"
    assert item.labels == ["priority:high"]
    assert item.external_assignee_name == "octocat"
    HTTPStub.verify!()
  end

  test "does not invent GitHub issue timestamps when updated_at is missing or malformed" do
    issues = [
      issue_response(1) |> Map.delete("updated_at"),
      issue_response(2) |> Map.put("updated_at", "not-a-timestamp")
    ]

    HTTPStub.expect([
      %{
        method: :get,
        url_contains: "/repos/acme/symmetry/issues?",
        response: {:ok, 200, %{}, issues}
      }
    ])

    assert {:ok, %{work_items: work_items}} =
             GitHub.sync_resource(
               %{account_ref: "acme", auth_type: "gh_cli", capabilities: ["work_items"]},
               %{kind: "work_tracking", external_ref: "acme/symmetry"}
             )

    assert Enum.map(work_items, & &1.external_updated_at) == [nil, nil]
    HTTPStub.verify!()
  end

  test "normalizes pull request, review, and GitHub Actions state" do
    HTTPStub.expect([
      %{
        method: :get,
        url_contains: "/repos/acme/symmetry/pulls/42",
        response:
          {:ok, 200, %{},
           %{
             "html_url" => "https://github.com/acme/symmetry/pull/42",
             "state" => "open",
             "merged_at" => nil,
             "draft" => false,
             "head" => %{"sha" => "abc123"}
           }}
      },
      %{
        method: :get,
        url_contains: "/repos/acme/symmetry/pulls/42/reviews",
        response:
          {:ok, 200, %{},
           [
             %{"user" => %{"login" => "reviewer"}, "state" => "APPROVED"},
             %{"user" => %{"login" => "reviewer"}, "state" => "COMMENTED"},
             %{"user" => %{"login" => "reviewer"}, "state" => "PENDING"},
             %{"user" => %{"login" => "dismissed"}, "state" => "CHANGES_REQUESTED"},
             %{"user" => %{"login" => "dismissed"}, "state" => "DISMISSED"}
           ]}
      },
      %{
        method: :get,
        url_contains: "/repos/acme/symmetry/actions/runs?",
        response:
          {:ok, 200, %{},
           %{
             "workflow_runs" => [
               %{
                 "workflow_id" => 1,
                 "status" => "completed",
                 "conclusion" => "success",
                 "html_url" => "https://github.com/acme/symmetry/actions/runs/9"
               },
               %{
                 "workflow_id" => 1,
                 "status" => "completed",
                 "conclusion" => "failure",
                 "html_url" => "https://github.com/acme/symmetry/actions/runs/8"
               }
             ]
           }}
      }
    ])

    connection = %{
      account_ref: "acme",
      auth_type: "gh_cli",
      capabilities: ["changes", "ci"]
    }

    resource = %{kind: "repository", external_ref: "acme/symmetry"}

    item = %{
      pull_request_url: "https://github.com/acme/symmetry/pull/42",
      branch: "codex/external-12"
    }

    assert {:ok, delivery} = GitHub.sync_delivery(connection, resource, item)
    assert delivery.pull_request_state == "open"
    assert delivery.review_status == "approved"
    assert delivery.ci_status == "passed"
    assert delivery.provider_data["head_sha"] == "abc123"
    HTTPStub.verify!()
  end

  test "changes-only connections synchronize pull requests without querying Actions" do
    HTTPStub.expect([
      %{
        method: :get,
        url_contains: "/repos/acme/symmetry/pulls/42",
        response:
          {:ok, 200, %{},
           %{
             "html_url" => "https://github.com/acme/symmetry/pull/42",
             "state" => "open",
             "merged_at" => nil,
             "head" => %{"sha" => "abc123"}
           }}
      },
      %{
        method: :get,
        url_contains: "/repos/acme/symmetry/pulls/42/reviews",
        response:
          {:ok, 200, %{},
           [
             %{"user" => %{"login" => "reviewer"}, "state" => "APPROVED"},
             %{"user" => %{"login" => "reviewer"}, "state" => "DISMISSED"}
           ]}
      }
    ])

    connection = %{account_ref: "acme", auth_type: "gh_cli", capabilities: ["changes"]}
    resource = %{kind: "repository", external_ref: "acme/symmetry"}
    item = %{pull_request_url: "https://github.com/acme/symmetry/pull/42"}

    assert {:ok, delivery} = GitHub.sync_delivery(connection, resource, item)
    assert delivery.pull_request_state == "open"
    assert delivery.review_status == "required"
    assert Map.get(delivery, :ci_status) == nil
    HTTPStub.verify!()
  end

  test "paginates pull request reviews before folding the latest decisive state" do
    first_page =
      [%{"user" => %{"login" => "reviewer"}, "state" => "APPROVED"}] ++
        List.duplicate(%{"user" => %{"login" => "observer"}, "state" => "COMMENTED"}, 99)

    HTTPStub.expect([
      %{
        method: :get,
        url_contains: "/repos/acme/symmetry/pulls/42",
        url_excludes: ["reviews"],
        response:
          {:ok, 200, %{},
           %{
             "html_url" => "https://github.com/acme/symmetry/pull/42",
             "state" => "open",
             "merged_at" => nil,
             "head" => %{"sha" => "abc123"}
           }}
      },
      %{
        method: :get,
        url_contains: "/repos/acme/symmetry/pulls/42/reviews?",
        url_excludes: ["page=2"],
        response: {:ok, 200, %{}, first_page}
      },
      %{
        method: :get,
        url_contains: "page=2",
        response:
          {:ok, 200, %{}, [%{"user" => %{"login" => "reviewer"}, "state" => "CHANGES_REQUESTED"}]}
      }
    ])

    assert {:ok, %{review_status: "changes_requested"}} =
             GitHub.sync_delivery(
               %{account_ref: "acme", auth_type: "gh_cli", capabilities: ["changes"]},
               %{kind: "repository", external_ref: "acme/symmetry"},
               %{pull_request_url: "https://github.com/acme/symmetry/pull/42"}
             )

    HTTPStub.verify!()
  end

  test "synchronizes a separately bound GitHub Actions resource" do
    HTTPStub.expect([
      %{
        method: :get,
        url_contains: "head_sha=abc123",
        response:
          {:ok, 200, %{},
           %{
             "workflow_runs" => [
               %{
                 "status" => "completed",
                 "conclusion" => "success",
                 "html_url" => "https://github.com/acme/symmetry/actions/runs/9"
               }
             ]
           }}
      }
    ])

    connection = %{account_ref: "acme", auth_type: "gh_cli", capabilities: ["ci"]}
    resource = %{kind: "ci", external_ref: "acme/symmetry"}

    item = %{
      branch: "codex/external-12",
      external_change_data: %{"head_sha" => "abc123"}
    }

    assert {:ok, %{ci_status: "passed"}} =
             GitHub.sync_ci(connection, resource, item, [{"authorization", "Bearer gho_test"}])

    HTTPStub.verify!()
  end

  test "does not treat stale or unrecognized completed Actions conclusions as passed" do
    HTTPStub.expect([
      %{
        method: :get,
        url_contains: "head_sha=abc123",
        response:
          {:ok, 200, %{},
           %{
             "workflow_runs" => [
               %{
                 "workflow_id" => 1,
                 "status" => "completed",
                 "conclusion" => "stale"
               },
               %{
                 "workflow_id" => 2,
                 "status" => "completed",
                 "conclusion" => "future_conclusion"
               }
             ]
           }}
      }
    ])

    connection = %{account_ref: "acme", auth_type: "gh_cli", capabilities: ["ci"]}
    resource = %{kind: "ci", external_ref: "acme/symmetry"}
    item = %{external_change_data: %{"head_sha" => "abc123"}}

    assert {:ok, %{ci_status: "unknown"}} =
             GitHub.sync_ci(connection, resource, item, [{"authorization", "Bearer gho_test"}])

    HTTPStub.verify!()
  end

  test "rejects repositories outside the connection account" do
    connection = %{account_ref: "acme", auth_type: "gh_cli", capabilities: ["work_items"]}
    resource = %{kind: "work_tracking", external_ref: "other/symmetry"}

    assert {:error, :forbidden} = GitHub.sync_resource(connection, resource)
    HTTPStub.verify!()
  end

  test "validates GitHub owner and repository reference segments" do
    connection = %{account_ref: "acme"}

    for reference <- [
          "",
          "/repository",
          "acme/",
          "acme/symmetry/extra",
          " acme/repository",
          "acme/repository ",
          "acme/ repo",
          "./repository",
          "../repository",
          "acme/.",
          "acme/..",
          "-acme/repository",
          "acme-/repository",
          "acme--team/repository",
          "acme/repo@name",
          "#{String.duplicate("a", 40)}/repository",
          "acme/#{String.duplicate("r", 101)}"
        ] do
      assert {:error, :invalid_repository_reference} =
               GitHub.validate_resource_reference(connection, "repository", reference)
    end

    for reference <- [
          "acme/repo-name",
          "acme/repo.name",
          "acme/repo_name",
          "acme/.github"
        ] do
      assert :ok = GitHub.validate_resource_reference(connection, "repository", reference)
    end

    assert :ok =
             GitHub.validate_resource_reference(
               %{account_ref: "acme-inc"},
               "repository",
               "acme-inc/repo.name_with-parts"
             )
  end

  test "paginates GitHub issues, filters pull requests, and preserves issue order" do
    first_page =
      1..99
      |> Enum.map(&issue_response/1)
      |> List.insert_at(49, %{
        "number" => 500,
        "title" => "Pull request",
        "pull_request" => %{"url" => "https://api.github.com/repos/acme/symmetry/pulls/500"}
      })

    HTTPStub.expect([
      %{
        method: :get,
        url_contains: "/repos/acme/symmetry/issues?",
        url_excludes: ["page=2"],
        response: {:ok, 200, %{}, first_page}
      },
      %{
        method: :get,
        url_contains: "page=2",
        response: {:ok, 200, %{}, [issue_response(100)]}
      }
    ])

    assert {:ok, %{work_items: work_items}} =
             GitHub.sync_resource(
               %{account_ref: "acme", auth_type: "gh_cli", capabilities: ["work_items"]},
               %{kind: "work_tracking", external_ref: "acme/symmetry"}
             )

    assert Enum.map(work_items, & &1.external_id) == Enum.map(1..100, &Integer.to_string/1)
    HTTPStub.verify!()
  end

  test "creates a pull request through the typed change action" do
    headers = [{"authorization", "Bearer gho_test"}]

    HTTPStub.expect([
      %{
        method: :get,
        url_contains: "head=acme%3Acodex%2Fgoal-3",
        headers: %{"authorization" => "Bearer gho_test"},
        response: {:ok, 200, %{}, []}
      },
      %{
        method: :post,
        url_contains: "/repos/acme/symmetry/pulls",
        body_assertion: fn body ->
          unless body == %{
                   "head" => "codex/goal-3",
                   "base" => "main",
                   "title" => "Connect providers",
                   "body" => "Typed action"
                 },
                 do: raise("unexpected pull request body")
        end,
        response:
          {:ok, 201, %{},
           %{
             "number" => 18,
             "html_url" => "https://github.com/acme/symmetry/pull/18",
             "state" => "open",
             "head" => %{"sha" => "abc123"}
           }}
      }
    ])

    assert {:ok, delivery} =
             GitHub.execute(
               %{account_ref: "acme", capabilities: ["changes"]},
               %{kind: "repository", external_ref: "acme/symmetry"},
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

    assert delivery.pull_request_url == "https://github.com/acme/symmetry/pull/18"
    assert delivery.pull_request_state == "open"
    assert delivery.provider_data["head_sha"] == "abc123"
    HTTPStub.verify!()
  end

  test "replays an existing open pull request instead of creating another" do
    HTTPStub.expect([
      %{
        method: :get,
        url_contains: "head=acme%3Acodex%2Fgoal-3",
        response:
          {:ok, 200, %{},
           [
             %{
               "number" => 18,
               "html_url" => "https://github.com/acme/symmetry/pull/18",
               "state" => "open"
             }
           ]}
      },
      %{
        method: :patch,
        url_contains: "/repos/acme/symmetry/pulls/18",
        body_assertion: fn body ->
          unless body == %{"title" => "Connect providers", "body" => "Reconciled body"},
            do: raise("existing pull request was not reconciled")
        end,
        response:
          {:ok, 200, %{},
           %{
             "number" => 18,
             "html_url" => "https://github.com/acme/symmetry/pull/18",
             "state" => "open"
           }}
      }
    ])

    assert {:ok, %{pull_request_url: "https://github.com/acme/symmetry/pull/18"}} =
             GitHub.execute(
               %{account_ref: "acme", capabilities: ["changes"]},
               %{kind: "repository", external_ref: "acme/symmetry"},
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

  test "replays the winning pull request when GitHub create races with another request" do
    existing = %{
      "number" => 18,
      "html_url" => "https://github.com/acme/symmetry/pull/18",
      "state" => "open"
    }

    HTTPStub.expect([
      %{
        method: :get,
        url_contains: "head=acme%3Acodex%2Fgoal-3",
        response: {:ok, 200, %{}, []}
      },
      %{
        method: :post,
        url_contains: "/repos/acme/symmetry/pulls",
        response: {:ok, 422, %{}, %{"message" => "A pull request already exists"}}
      },
      %{
        method: :get,
        url_contains: "head=acme%3Acodex%2Fgoal-3",
        response: {:ok, 200, %{}, [existing]}
      },
      %{
        method: :patch,
        url_contains: "/repos/acme/symmetry/pulls/18",
        body_assertion: fn body ->
          unless body == %{"title" => "Connect providers", "body" => "Reconciled body"},
            do: raise("raced pull request was not reconciled")
        end,
        response: {:ok, 200, %{}, existing}
      }
    ])

    assert {:ok, %{pull_request_url: "https://github.com/acme/symmetry/pull/18"}} =
             GitHub.execute(
               %{account_ref: "acme", capabilities: ["changes"]},
               %{kind: "repository", external_ref: "acme/symmetry"},
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

  test "preserves the GitHub create error when conflict lookup finds no pull request" do
    error_body = %{"message" => "Validation failed"}

    HTTPStub.expect([
      %{
        method: :get,
        url_contains: "head=acme%3Acodex%2Fgoal-3",
        response: {:ok, 200, %{}, []}
      },
      %{
        method: :post,
        url_contains: "/repos/acme/symmetry/pulls",
        response: {:ok, 422, %{}, error_body}
      },
      %{
        method: :get,
        url_contains: "head=acme%3Acodex%2Fgoal-3",
        response: {:ok, 200, %{}, []}
      }
    ])

    assert {:error, {:http, 422, ^error_body}} =
             GitHub.execute(
               %{account_ref: "acme", capabilities: ["changes"]},
               %{kind: "repository", external_ref: "acme/symmetry"},
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

  test "updates only pull request title and body within the bound repository" do
    HTTPStub.expect([
      %{
        method: :patch,
        url_contains: "/repos/acme/symmetry/pulls/18",
        body_assertion: fn body ->
          unless body == %{"title" => "Updated title", "body" => "Updated body"},
            do: raise("unexpected pull request update")
        end,
        response:
          {:ok, 200, %{},
           %{
             "number" => 18,
             "html_url" => "https://github.com/acme/symmetry/pull/18",
             "state" => "open"
           }}
      }
    ])

    assert {:ok, %{pull_request_state: "open"}} =
             GitHub.execute(
               %{account_ref: "acme", capabilities: ["changes"]},
               %{kind: "repository", external_ref: "acme/symmetry"},
               %{external_pull_request_url: "https://GITHUB.com/ACME/SYMMETRY/pull/18"},
               "change.update",
               %{"title" => "Updated title", "body" => "Updated body"},
               []
             )

    HTTPStub.verify!()
  end

  test "rejects invalid change input and pull request URLs without making requests" do
    connection = %{account_ref: "acme", capabilities: ["changes"]}
    resource = %{kind: "repository", external_ref: "acme/symmetry"}

    assert {:error, :invalid_request} =
             GitHub.execute(
               connection,
               resource,
               %{},
               "change.upsert",
               %{
                 "source_branch" => "../unsafe",
                 "target_branch" => "main",
                 "title" => "Unsafe",
                 "method" => "DELETE"
               },
               []
             )

    assert {:error, :invalid_pull_request_url} =
             GitHub.execute(
               connection,
               resource,
               %{external_pull_request_url: "https://github.com/other/repo/pull/18"},
               "change.update",
               %{"title" => "Cannot escape binding"},
               []
             )

    assert {:error, :invalid_request} =
             GitHub.execute(
               connection,
               resource,
               %{external_pull_request_url: "https://github.com/acme/symmetry/pull/18"},
               "change.update",
               %{"title" => "No branch mutation", "source_branch" => "other"},
               []
             )

    assert {:error, :invalid_pull_request_url} =
             GitHub.execute(
               connection,
               resource,
               %{external_pull_request_url: "https://github.com/acme/symmetry/pull/18"},
               "change.update",
               %{
                 "title" => "Cannot change another PR",
                 "pull_request_url" => "https://github.com/acme/symmetry/pull/19"
               },
               []
             )

    for invalid_url <- [
          "http://github.com/acme/symmetry/pull/18",
          "https://github.com.evil.test/acme/symmetry/pull/18",
          "https://github.com/acme/symmetry/pull/not-a-number"
        ] do
      assert {:error, :invalid_pull_request_url} =
               GitHub.execute(
                 connection,
                 resource,
                 %{external_pull_request_url: invalid_url},
                 "change.update",
                 %{"title" => "Reject malformed URL"},
                 []
               )
    end

    HTTPStub.verify!()
  end

  test "exact readback confirms the dispatched action without another mutation" do
    {resource, binding} = bound_action()
    body = dispatched_unknown_body(resource, binding)
    pull_request = bound_pull_request(binding, body)

    HTTPStub.expect(bound_reads(binding, [pull_request]))

    assert {:ok, %{status: :confirmed, delivery: delivery}} =
             GitHub.readback_action(bound_connection(), resource, "change.upsert", binding, [])

    assert delivery.pull_request_url == "https://github.com/acme/symmetry/pull/18"
    assert delivery.provider_data["head_sha"] == binding["subject"]["commit"]
    HTTPStub.verify!()
  end

  test "bound mutation accepts only a success carrying the exact persisted Subject and content" do
    {resource, binding} = bound_action()
    digest = binding |> SymmetryControl.RequestHash.canonical() |> Base.encode16(case: :lower)

    body =
      "Typed action\n\n<!-- symmetry-provider-action:v1:#{binding["action_id"]}:#{digest} -->"

    pull_request = bound_pull_request(binding, body)

    HTTPStub.expect(
      bound_reads(binding, []) ++
        [
          %{
            method: :post,
            url_contains: "/pulls",
            body_assertion: fn payload -> assert payload["body"] == body end,
            response: {:ok, 201, %{}, pull_request}
          }
        ]
    )

    assert {:ok, %{pull_request_url: "https://github.com/acme/symmetry/pull/18"}} =
             GitHub.execute_bound(
               bound_connection(),
               resource,
               %{},
               "change.upsert",
               binding["input"],
               binding,
               []
             )

    HTTPStub.verify!()

    HTTPStub.expect(
      bound_reads(binding, []) ++
        [
          %{
            method: :post,
            url_contains: "/pulls",
            response:
              {:ok, 201, %{}, put_in(pull_request, ["head", "sha"], String.duplicate("b", 40))}
          }
        ]
    )

    assert {:error, :invalid_provider_response} =
             GitHub.execute_bound(
               bound_connection(),
               resource,
               %{},
               "change.upsert",
               binding["input"],
               binding,
               []
             )

    HTTPStub.verify!()
  end

  test "bound updates and existing-PR upserts reject a success for another pull request" do
    {resource, upsert_binding} = bound_action()

    update_input = %{
      "title" => "Update exact PR",
      "body" => "Explicit body",
      "pull_request_url" => "https://github.com/acme/symmetry/pull/18"
    }

    assert {:ok, update_binding} =
             GitHub.action_binding(
               resource,
               Ecto.UUID.generate(),
               upsert_binding["subject"],
               "change.update",
               update_input,
               %{
                 "kind" => "pull_request",
                 "pull_request_url" => update_input["pull_request_url"]
               },
               upsert_binding
             )

    for binding <- [upsert_binding, update_binding] do
      original = bound_pull_request(binding, "Original body")
      digest = binding |> SymmetryControl.RequestHash.canonical() |> Base.encode16(case: :lower)

      body =
        binding["body"] <>
          "\n\n<!-- symmetry-provider-action:v1:#{binding["action_id"]}:#{digest} -->"

      wrong_pull_request =
        bound_pull_request(binding, body)
        |> Map.merge(%{
          "number" => 19,
          "html_url" => "https://github.com/acme/symmetry/pull/19"
        })

      reads =
        if binding["operation"] == "change.update" do
          [
            hd(bound_reads(binding, [])),
            %{method: :get, url_contains: "/pulls/18", response: {:ok, 200, %{}, original}}
          ]
        else
          bound_reads(binding, [original])
        end

      HTTPStub.expect(
        reads ++
          [
            %{
              method: :patch,
              url_contains: "/pulls/18",
              body_assertion: fn payload ->
                assert payload == %{"title" => binding["title"], "body" => body}
              end,
              response: {:ok, 200, %{}, wrong_pull_request}
            }
          ]
      )

      assert {:error, :invalid_provider_response} =
               GitHub.execute_bound(
                 bound_connection(),
                 resource,
                 %{},
                 binding["operation"],
                 binding["input"],
                 binding,
                 []
               )

      HTTPStub.verify!()
    end
  end

  test "readback binds the complete Subject tree digest rather than just its commit" do
    {resource, binding} = bound_action()
    body = dispatched_unknown_body(resource, binding)

    altered_subject =
      put_in(binding, ["subject", "tree_digest"], "sha256:" <> String.duplicate("5", 64))

    HTTPStub.expect(bound_reads(altered_subject, [bound_pull_request(binding, body)]))

    assert {:ok, %{status: :unconfirmed}} =
             GitHub.readback_action(
               bound_connection(),
               resource,
               "change.upsert",
               altered_subject,
               []
             )

    HTTPStub.verify!()
  end

  test "exact readback refuses missing markers, edited content, changed Subject head and selectors" do
    {resource, binding} = bound_action()
    body = dispatched_unknown_body(resource, binding)
    original = bound_pull_request(binding, body)

    mismatches = [
      Map.put(original, "body", "Typed action"),
      Map.put(original, "body", body <> " edited"),
      Map.put(original, "body", String.replace(body, binding["action_id"], Ecto.UUID.generate())),
      Map.put(original, "title", "Edited title"),
      put_in(original, ["head", "sha"], String.duplicate("b", 40)),
      put_in(original, ["head", "ref"], "another-branch"),
      put_in(original, ["base", "ref"], "another-base"),
      put_in(original, ["head", "repo", "id"], 999),
      put_in(original, ["base", "repo", "node_id"], "another-node")
    ]

    Enum.each(mismatches, fn pull_request ->
      HTTPStub.expect(bound_reads(binding, [pull_request]))

      assert {:ok, %{status: :unconfirmed}} =
               GitHub.readback_action(bound_connection(), resource, "change.upsert", binding, [])

      HTTPStub.verify!()
    end)
  end

  test "exact readback refuses absent or ambiguous pull requests" do
    {resource, binding} = bound_action()
    body = dispatched_unknown_body(resource, binding)
    pull_request = bound_pull_request(binding, body)

    Enum.each([[], [pull_request, Map.put(pull_request, "number", 19)]], fn entries ->
      HTTPStub.expect(bound_reads(binding, entries))

      assert {:ok, %{status: :unconfirmed}} =
               GitHub.readback_action(bound_connection(), resource, "change.upsert", binding, [])

      HTTPStub.verify!()
    end)
  end

  test "stable repository identity and branch head are checked before initial mutation" do
    {resource, binding} = bound_action()

    Enum.each(
      [%{"id" => 999, "node_id" => "R_exact"}, %{"id" => 123, "node_id" => "R_replaced"}],
      fn repository ->
        HTTPStub.expect([
          %{
            method: :get,
            url_contains: "/repos/acme/symmetry",
            response: {:ok, 200, %{}, repository}
          }
        ])

        assert {:error, :forbidden} =
                 GitHub.execute_bound(
                   bound_connection(),
                   resource,
                   %{},
                   "change.upsert",
                   binding["input"],
                   binding,
                   []
                 )

        HTTPStub.verify!()
      end
    )

    HTTPStub.expect([
      hd(bound_reads(binding, [])),
      %{
        method: :get,
        url_contains: "/git/ref/heads/",
        response: {:ok, 200, %{}, %{"object" => %{"sha" => String.duplicate("b", 40)}}}
      }
    ])

    assert {:error, :forbidden} =
             GitHub.execute_bound(
               bound_connection(),
               resource,
               %{},
               "change.upsert",
               binding["input"],
               binding,
               []
             )

    HTTPStub.verify!()
  end

  test "exact readback cannot promote a replaced repository or moved source ref" do
    {resource, binding} = bound_action()

    HTTPStub.expect([
      %{
        method: :get,
        url_contains: "/repos/acme/symmetry",
        response: {:ok, 200, %{}, %{"id" => 999, "node_id" => "R_exact"}}
      }
    ])

    assert {:ok, %{status: :unconfirmed}} =
             GitHub.readback_action(bound_connection(), resource, "change.upsert", binding, [])

    HTTPStub.verify!()

    HTTPStub.expect([
      hd(bound_reads(binding, [])),
      %{
        method: :get,
        url_contains: "/git/ref/heads/",
        response: {:ok, 200, %{}, %{"object" => %{"sha" => String.duplicate("b", 40)}}}
      }
    ])

    assert {:ok, %{status: :unconfirmed}} =
             GitHub.readback_action(bound_connection(), resource, "change.upsert", binding, [])

    HTTPStub.verify!()
  end

  test "exact original pull request updates read back only that immutable selector" do
    {resource, upsert_binding} = bound_action()

    input = %{
      "title" => "Update exact PR",
      "body" => "Explicit body",
      "pull_request_url" => "https://github.com/acme/symmetry/pull/18"
    }

    assert {:ok, binding} =
             GitHub.action_binding(
               resource,
               Ecto.UUID.generate(),
               upsert_binding["subject"],
               "change.update",
               input,
               %{"kind" => "pull_request", "pull_request_url" => input["pull_request_url"]},
               upsert_binding
             )

    original = bound_pull_request(upsert_binding, "Original body")

    HTTPStub.expect([
      hd(bound_reads(binding, [])),
      %{method: :get, url_contains: "/pulls/18", response: {:ok, 200, %{}, original}},
      %{
        method: :patch,
        url_contains: "/pulls/18",
        body_assertion: fn payload -> Process.put(:bound_update_body, payload["body"]) end,
        response: {:error, {:transport, :timeout}}
      }
    ])

    assert {:error, {:transport, :timeout}} =
             GitHub.execute_bound(
               bound_connection(),
               resource,
               %{},
               "change.update",
               input,
               binding,
               []
             )

    HTTPStub.verify!()
    exact = %{original | "title" => input["title"], "body" => Process.get(:bound_update_body)}

    HTTPStub.expect([
      hd(bound_reads(binding, [])),
      %{method: :get, url_contains: "/pulls/18", response: {:ok, 200, %{}, exact}}
    ])

    assert {:ok, %{status: :confirmed}} =
             GitHub.readback_action(bound_connection(), resource, "change.update", binding, [])

    HTTPStub.verify!()
  end

  defp bound_connection, do: %{account_ref: "acme", capabilities: ["repositories", "changes"]}

  defp bound_action do
    resource = %{
      id: Ecto.UUID.generate(),
      kind: "repository",
      external_ref: "acme/symmetry",
      metadata: %{"repository_id" => 123, "repository_node_id" => "R_exact"}
    }

    subject = %{
      "resource_id" => resource.id,
      "commit" => String.duplicate("a", 40),
      "tree_digest" => "sha256:" <> String.duplicate("4", 64)
    }

    input = %{
      "source_branch" => "codex/goal-3",
      "target_branch" => "main",
      "title" => "Connect providers",
      "body" => "Typed action"
    }

    target = %{
      "kind" => "branches",
      "source_branch" => input["source_branch"],
      "target_branch" => input["target_branch"]
    }

    assert {:ok, binding} =
             GitHub.action_binding(
               resource,
               Ecto.UUID.generate(),
               subject,
               "change.upsert",
               input,
               target,
               %{"run_id" => Ecto.UUID.generate(), "task_id" => Ecto.UUID.generate()}
             )

    {resource, binding}
  end

  defp dispatched_unknown_body(resource, binding) do
    HTTPStub.expect(
      bound_reads(binding, []) ++
        [
          %{
            method: :post,
            url_contains: "/pulls",
            body_assertion: fn payload ->
              assert payload["title"] == "Connect providers"

              assert String.starts_with?(
                       payload["body"],
                       "Typed action\n\n<!-- symmetry-provider-action:v1:" <>
                         binding["action_id"] <> ":"
                     )

              Process.put(:bound_action_body, payload["body"])
            end,
            response: {:error, {:transport, :timeout}}
          }
        ]
    )

    assert {:error, {:transport, :timeout}} =
             GitHub.execute_bound(
               bound_connection(),
               resource,
               %{},
               "change.upsert",
               binding["input"],
               binding,
               []
             )

    HTTPStub.verify!()
    Process.get(:bound_action_body)
  end

  defp bound_reads(binding, entries) do
    [
      %{
        method: :get,
        url_contains: "/repos/acme/symmetry",
        url_excludes: ["/pulls", "/git/"],
        response: {:ok, 200, %{}, binding["repository"]}
      },
      %{
        method: :get,
        url_contains: "/git/ref/heads/codex%2Fgoal-3",
        response: {:ok, 200, %{}, %{"object" => %{"sha" => binding["subject"]["commit"]}}}
      },
      %{method: :get, url_contains: "/pulls?", response: {:ok, 200, %{}, entries}}
    ]
  end

  defp bound_pull_request(binding, body) do
    %{
      "number" => 18,
      "html_url" => "https://github.com/acme/symmetry/pull/18",
      "state" => "open",
      "title" => binding["title"],
      "body" => body,
      "head" => %{
        "ref" => "codex/goal-3",
        "sha" => binding["subject"]["commit"],
        "repo" => binding["repository"]
      },
      "base" => %{"ref" => "main", "repo" => binding["repository"]}
    }
  end

  defp issue_response(number) do
    %{
      "number" => number,
      "title" => "Issue #{number}",
      "body" => nil,
      "state" => "open",
      "html_url" => "https://github.com/acme/symmetry/issues/#{number}",
      "updated_at" => "2026-09-05T09:30:00Z",
      "labels" => [],
      "assignee" => nil
    }
  end
end
