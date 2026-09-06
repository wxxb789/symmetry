defmodule SymmetryControl.Integrations.ChangeActionTest do
  use ExUnit.Case, async: true

  alias SymmetryControl.Integrations.ChangeAction

  test "normalizes the broker caller input to title and optional body" do
    assert {:ok, %{"title" => "Create PR"}} =
             ChangeAction.normalize_caller_input("change.upsert", %{title: "  Create PR  "})

    assert {:ok, %{"title" => "Update PR", "body" => ""}} =
             ChangeAction.normalize_caller_input("change.update", %{
               "title" => "Update PR",
               "body" => ""
             })

    assert {:error, :invalid_request} =
             ChangeAction.normalize_caller_input("change.upsert", %{
               "title" => "Create PR",
               "body" => nil
             })

    assert {:error, :invalid_request} =
             ChangeAction.normalize_caller_input("change.upsert", %{
               "title" => "Create PR",
               "source_branch" => "caller-controlled"
             })
  end

  test "rejects nested ref components forbidden by git check-ref-format" do
    for branch <- [
          "HEAD",
          "refs/heads/HEAD",
          "feature/.hidden",
          "feature/release.lock",
          "feature/release.LOCK"
        ] do
      assert {:error, :invalid_request} = ChangeAction.normalize_branch(branch)
    end

    assert {:ok, "feature/release"} =
             ChangeAction.normalize_branch("refs/heads/feature/release")
  end
end
