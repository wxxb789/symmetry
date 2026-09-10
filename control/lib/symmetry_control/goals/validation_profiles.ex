defmodule SymmetryControl.Goals.ValidationProfiles do
  @moduledoc """
  Resolves operator-configured validation profiles into wire-safe bindings.

  Profiles deliberately contain only authorization metadata. Daemon-reported
  capabilities, executable commands, arguments, and credentials cannot create
  or alter a binding.
  """

  @profile_fields MapSet.new(["kind", "profile_digest", "enabled", "allowed_runtime_ids"])
  @profile_kinds %{"check" => "check", "review" => "review", check: "check", review: "review"}
  @short_identifier ~r/\A[A-Za-z0-9][A-Za-z0-9._:-]{0,127}\z/
  @sha256 ~r/\Asha256:[0-9a-f]{64}\z/

  @type kind :: :check | :review | String.t()

  @type binding :: %{String.t() => String.t() | [String.t()]}

  @doc """
  Resolves one operator profile for the requested validation kind.

  Pass `profiles: ...` to inject an operator registry in tests. Without that
  option, the registry comes from `:symmetry_control, :goals` configuration.
  """
  @spec resolve(String.t(), kind(), keyword()) :: {:ok, binding()} | {:error, term()}
  def resolve(profile_name, expected_kind, opts \\ []) do
    with {:ok, profile_name} <- profile_name(profile_name),
         {:ok, expected_kind} <- normalize_kind(expected_kind),
         {:ok, profiles} <- configured_profiles(opts) do
      resolve_snapshot(profile_name, expected_kind, profiles)
    end
  end

  @doc """
  Derives the unique check/review bindings required by an acceptance contract.

  Artifact and operator-acceptance predicates require no validation profile.
  Profile availability remains fail-closed: every referenced check or review
  profile must be present, enabled, and of the matching operator-configured
  kind.
  """
  @spec bindings_for_acceptance(map(), keyword()) :: {:ok, [binding()]} | {:error, term()}
  def bindings_for_acceptance(acceptance_contract, opts \\ [])

  def bindings_for_acceptance(acceptance_contract, opts) when is_map(acceptance_contract) do
    with {:ok, references} <- profile_references(acceptance_contract),
         {:ok, profiles} <- configured_profiles(opts) do
      references
      |> Enum.reduce_while({:ok, []}, fn {profile_name, kind}, {:ok, bindings} ->
        case resolve_snapshot(profile_name, kind, profiles) do
          {:ok, binding} -> {:cont, {:ok, [binding | bindings]}}
          {:error, reason} -> {:halt, {:error, reason}}
        end
      end)
      |> case do
        {:ok, bindings} -> {:ok, Enum.reverse(bindings)}
        {:error, reason} -> {:error, reason}
      end
    end
  end

  def bindings_for_acceptance(_, _), do: {:error, :invalid_acceptance_contract}

  defp configured_profiles(opts) when is_list(opts) do
    if Keyword.keyword?(opts) do
      case Keyword.fetch(opts, :profiles) do
        {:ok, profiles} ->
          parse_profiles(profiles)

        :error ->
          configured_application_profiles()
      end
    else
      {:error, :invalid_validation_profile_registry}
    end
  end

  defp configured_profiles(_), do: {:error, :invalid_validation_profile_registry}

  defp resolve_snapshot(profile_name, expected_kind, profiles) do
    with {:ok, expected_kind} <- normalize_kind(expected_kind),
         {:ok, profile} <- fetch_profile(profiles, profile_name),
         :ok <- ensure_enabled(profile, profile_name),
         :ok <- ensure_kind(profile, profile_name, expected_kind) do
      {:ok,
       %{
         "profile_name" => profile_name,
         "kind" => expected_kind,
         "profile_digest" => profile.profile_digest,
         "allowed_runtime_ids" => profile.allowed_runtime_ids
       }}
    end
  end

  defp configured_application_profiles do
    case Application.get_env(:symmetry_control, :goals, []) do
      goals when is_list(goals) ->
        if Keyword.keyword?(goals) do
          goals
          |> Keyword.get(:validation_profiles, [])
          |> parse_profiles()
        else
          {:error, :invalid_validation_profile_registry}
        end

      _ ->
        {:error, :invalid_validation_profile_registry}
    end
  end

  defp parse_profiles(profiles) when is_map(profiles),
    do: profiles |> Map.to_list() |> parse_profiles()

  defp parse_profiles(profiles) when is_function(profiles, 0) do
    try do
      profiles.() |> parse_profiles()
    rescue
      _ -> {:error, :invalid_validation_profile_registry}
    end
  end

  defp parse_profiles(profiles) when is_list(profiles) do
    profiles
    |> Enum.reduce_while({:ok, %{}}, fn
      {name, fields}, {:ok, parsed} ->
        with {:ok, name} <- profile_name(name),
             false <- Map.has_key?(parsed, name),
             {:ok, profile} <- parse_profile(name, fields) do
          {:cont, {:ok, Map.put(parsed, name, profile)}}
        else
          true -> {:halt, {:error, {:duplicate_validation_profile, to_string(name)}}}
          {:error, reason} -> {:halt, {:error, reason}}
        end

      _, _ ->
        {:halt, {:error, :invalid_validation_profile_registry}}
    end)
  end

  defp parse_profiles(_), do: {:error, :invalid_validation_profile_registry}

  defp parse_profile(name, fields) do
    with {:ok, fields} <- normalize_fields(fields, name),
         true <- MapSet.equal?(MapSet.new(Map.keys(fields)), @profile_fields),
         {:ok, kind} <- normalize_kind(Map.get(fields, "kind")),
         {:ok, digest} <- profile_digest(Map.get(fields, "profile_digest")),
         {:ok, enabled} <- enabled(Map.get(fields, "enabled")),
         {:ok, runtime_ids} <- runtime_ids(Map.get(fields, "allowed_runtime_ids")) do
      {:ok,
       %{kind: kind, profile_digest: digest, enabled: enabled, allowed_runtime_ids: runtime_ids}}
    else
      false -> {:error, {:invalid_validation_profile, name}}
      {:error, {:invalid_validation_profile, _} = reason} -> {:error, reason}
      {:error, _} -> {:error, {:invalid_validation_profile, name}}
    end
  end

  defp normalize_fields(fields, name) when is_map(fields),
    do: fields |> Map.to_list() |> normalize_fields(name)

  defp normalize_fields(fields, name) when is_list(fields) do
    fields
    |> Enum.reduce_while({:ok, %{}}, fn
      {key, value}, {:ok, normalized} ->
        with {:ok, key} <- profile_field(key),
             false <- Map.has_key?(normalized, key) do
          {:cont, {:ok, Map.put(normalized, key, value)}}
        else
          true -> {:halt, {:error, {:invalid_validation_profile, name}}}
          {:error, _} -> {:halt, {:error, {:invalid_validation_profile, name}}}
        end

      _, _ ->
        {:halt, {:error, {:invalid_validation_profile, name}}}
    end)
  end

  defp normalize_fields(_, name), do: {:error, {:invalid_validation_profile, name}}

  defp fetch_profile(profiles, name) do
    case Map.fetch(profiles, name) do
      {:ok, profile} -> {:ok, profile}
      :error -> {:error, {:validation_profile_not_found, name}}
    end
  end

  defp ensure_enabled(%{enabled: true}, _name), do: :ok
  defp ensure_enabled(_, name), do: {:error, {:validation_profile_disabled, name}}

  defp ensure_kind(%{kind: kind}, _name, kind), do: :ok

  defp ensure_kind(_, name, _expected_kind),
    do: {:error, {:validation_profile_kind_mismatch, name}}

  defp profile_references(contract) do
    case value(contract, :predicates) do
      predicates when is_list(predicates) ->
        predicates
        |> Enum.reduce_while({:ok, []}, fn predicate, {:ok, references} ->
          case profile_reference(predicate) do
            :ignore -> {:cont, {:ok, references}}
            {:ok, reference} -> {:cont, {:ok, [reference | references]}}
            {:error, reason} -> {:halt, {:error, reason}}
          end
        end)
        |> case do
          {:ok, references} -> {:ok, references |> Enum.reverse() |> Enum.uniq()}
          {:error, reason} -> {:error, reason}
        end

      _ ->
        {:error, :invalid_acceptance_contract}
    end
  end

  defp profile_reference(predicate) when is_map(predicate) do
    case value(predicate, :kind) do
      "check" -> required_profile_reference(predicate, :validator_profile, :check)
      "review" -> required_profile_reference(predicate, :reviewer_profile, :review)
      "artifact" -> :ignore
      "operator_acceptance" -> :ignore
      _ -> {:error, :invalid_acceptance_contract}
    end
  end

  defp profile_reference(_), do: {:error, :invalid_acceptance_contract}

  defp required_profile_reference(predicate, key, kind) do
    case profile_name(value(predicate, key)) do
      {:ok, name} -> {:ok, {name, kind}}
      {:error, _} -> {:error, :invalid_acceptance_contract}
    end
  end

  defp profile_name(name) when name in [nil, true, false], do: {:error, :invalid_profile_name}
  defp profile_name(name) when is_atom(name), do: name |> Atom.to_string() |> profile_name()

  defp profile_name(name) when is_binary(name) do
    name = String.trim(name)

    if Regex.match?(@short_identifier, name),
      do: {:ok, name},
      else: {:error, :invalid_profile_name}
  end

  defp profile_name(_), do: {:error, :invalid_profile_name}

  defp profile_field(field) when is_atom(field), do: field |> Atom.to_string() |> profile_field()

  defp profile_field(field) when is_binary(field) do
    if MapSet.member?(@profile_fields, field),
      do: {:ok, field},
      else: {:error, :invalid_profile_field}
  end

  defp profile_field(_), do: {:error, :invalid_profile_field}

  defp normalize_kind(kind) when kind in [:check, :review, "check", "review"],
    do: {:ok, Map.fetch!(@profile_kinds, kind)}

  defp normalize_kind(_), do: {:error, :invalid_profile_kind}

  defp profile_digest(digest) when is_binary(digest) do
    if Regex.match?(@sha256, digest), do: {:ok, digest}, else: {:error, :invalid_profile_digest}
  end

  defp profile_digest(_), do: {:error, :invalid_profile_digest}

  defp enabled(enabled) when is_boolean(enabled), do: {:ok, enabled}
  defp enabled(_), do: {:error, :invalid_profile_enabled}

  defp runtime_ids(runtime_ids) when is_list(runtime_ids) and runtime_ids != [] do
    runtime_ids
    |> Enum.reduce_while({:ok, []}, fn runtime_id, {:ok, parsed} ->
      case canonical_uuid(runtime_id) do
        {:ok, runtime_id} -> {:cont, {:ok, [runtime_id | parsed]}}
        {:error, _} -> {:halt, {:error, :invalid_runtime_id}}
      end
    end)
    |> case do
      {:ok, parsed} ->
        runtime_ids = Enum.reverse(parsed)

        if length(runtime_ids) == MapSet.size(MapSet.new(runtime_ids)),
          do: {:ok, runtime_ids},
          else: {:error, :duplicate_runtime_id}

      {:error, reason} ->
        {:error, reason}
    end
  end

  defp runtime_ids(_), do: {:error, :invalid_runtime_id}

  defp canonical_uuid(value) when is_binary(value) do
    case Ecto.UUID.cast(value) do
      {:ok, uuid} -> {:ok, uuid}
      :error -> {:error, :invalid_runtime_id}
    end
  end

  defp canonical_uuid(_), do: {:error, :invalid_runtime_id}

  defp value(map, key), do: Map.get(map, key, Map.get(map, to_string(key)))
end
