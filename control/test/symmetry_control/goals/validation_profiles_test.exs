defmodule SymmetryControl.Goals.ValidationProfilesTest do
  use ExUnit.Case, async: true

  alias SymmetryControl.Goals.ValidationProfiles

  @runtime_one "00000000-0000-0000-0000-000000000001"
  @runtime_two "00000000-0000-0000-0000-000000000002"
  @digest "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

  test "derives unique wire-safe bindings from check and review predicates" do
    acceptance = %{
      "predicates" => [
        %{"id" => "unit", "kind" => "check", "validator_profile" => "unit-check"},
        %{"id" => "review", "kind" => "review", "reviewer_profile" => "peer-review"},
        %{
          "id" => "artifact",
          "kind" => "artifact",
          "resource_id" => @runtime_one,
          "path" => "README.md"
        },
        %{"id" => "accept", "kind" => "operator_acceptance"},
        %{"id" => "unit-again", "kind" => "check", "validator_profile" => "unit-check"}
      ]
    }

    assert {:ok,
            [
              %{
                "profile_name" => "unit-check",
                "kind" => "check",
                "profile_digest" => @digest,
                "allowed_runtime_ids" => [@runtime_one]
              },
              %{
                "profile_name" => "peer-review",
                "kind" => "review",
                "profile_digest" => @digest,
                "allowed_runtime_ids" => [@runtime_two]
              }
            ]} = ValidationProfiles.bindings_for_acceptance(acceptance, profiles: profiles())
  end

  test "captures the injected registry once before resolving every predicate" do
    acceptance = %{
      "predicates" => [
        %{"id" => "unit", "kind" => "check", "validator_profile" => "unit-check"},
        %{"id" => "review", "kind" => "review", "reviewer_profile" => "peer-review"}
      ]
    }

    registry = fn ->
      send(self(), :validation_profile_registry_read)
      profiles()
    end

    assert {:ok, [_check, _review]} =
             ValidationProfiles.bindings_for_acceptance(acceptance, profiles: registry)

    assert_receive :validation_profile_registry_read
    refute_receive :validation_profile_registry_read
  end

  test "default empty registry fails closed for a check predicate" do
    assert {:error, {:validation_profile_not_found, "unit-check"}} =
             ValidationProfiles.bindings_for_acceptance(check_acceptance())
  end

  test "rejects missing, disabled, and kind-mismatched profiles" do
    assert {:error, {:validation_profile_not_found, "missing"}} =
             ValidationProfiles.resolve("missing", :check, profiles: [])

    assert {:error, {:validation_profile_disabled, "unit-check"}} =
             ValidationProfiles.resolve(
               "unit-check",
               :check,
               profiles: [
                 "unit-check": [
                   kind: :check,
                   profile_digest: @digest,
                   enabled: false,
                   allowed_runtime_ids: [@runtime_one]
                 ]
               ]
             )

    assert {:error, {:validation_profile_kind_mismatch, "unit-check"}} =
             ValidationProfiles.resolve("unit-check", :review, profiles: profiles())
  end

  test "rejects incomplete or unsafe operator profile configuration" do
    for profiles <- [
          ["unit-check": [kind: :check, profile_digest: @digest, enabled: true]],
          [
            "unit-check": [
              kind: :check,
              profile_digest: @digest,
              enabled: true,
              allowed_runtime_ids: [@runtime_one],
              argv: ["mix", "test"]
            ]
          ],
          [
            "unit-check": [
              kind: :check,
              profile_digest: "sha256:" <> String.duplicate("A", 64),
              enabled: true,
              allowed_runtime_ids: [@runtime_one]
            ]
          ],
          [
            "unit-check": [
              kind: :check,
              profile_digest: @digest,
              enabled: true,
              allowed_runtime_ids: []
            ]
          ],
          [
            "unit-check": [
              kind: :check,
              profile_digest: @digest,
              enabled: true,
              allowed_runtime_ids: ["not-a-uuid"]
            ]
          ],
          [
            "unit-check": [
              kind: :check,
              profile_digest: @digest,
              enabled: true,
              allowed_runtime_ids: [@runtime_one, @runtime_one]
            ]
          ]
        ] do
      assert {:error, {:invalid_validation_profile, "unit-check"}} =
               ValidationProfiles.resolve("unit-check", :check, profiles: profiles)
    end
  end

  test "rejects duplicate profile entries and malformed acceptance references" do
    duplicate_profiles = [
      {"unit-check",
       [kind: :check, profile_digest: @digest, enabled: true, allowed_runtime_ids: [@runtime_one]]},
      {"unit-check",
       [kind: :check, profile_digest: @digest, enabled: true, allowed_runtime_ids: [@runtime_one]]}
    ]

    assert {:error, {:duplicate_validation_profile, "unit-check"}} =
             ValidationProfiles.resolve("unit-check", :check, profiles: duplicate_profiles)

    assert {:error, :invalid_acceptance_contract} =
             ValidationProfiles.bindings_for_acceptance(%{"predicates" => [%{"kind" => "check"}]},
               profiles: profiles()
             )
  end

  defp check_acceptance do
    %{"predicates" => [%{"id" => "unit", "kind" => "check", "validator_profile" => "unit-check"}]}
  end

  defp profiles do
    [
      "unit-check": [
        kind: :check,
        profile_digest: @digest,
        enabled: true,
        allowed_runtime_ids: [@runtime_one]
      ],
      "peer-review": [
        kind: :review,
        profile_digest: @digest,
        enabled: true,
        allowed_runtime_ids: [@runtime_two]
      ]
    ]
  end
end
