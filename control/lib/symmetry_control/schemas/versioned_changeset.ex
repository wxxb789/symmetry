defmodule SymmetryControl.Schemas.VersionedChangeset do
  @moduledoc false

  # Optimistic-locking contract: positive-integer expected version; `:stale` on mismatch
  # or a missing locked re-read; attrs helpers normalize the JSON atom/string spellings.

  alias SymmetryControl.Repo

  @spec expected_version(map()) :: {:ok, pos_integer()} | {:error, :invalid_request}
  def expected_version(attrs) do
    case Map.get(attrs, :version) || Map.get(attrs, "version") do
      version when is_integer(version) and version > 0 -> {:ok, version}
      _ -> {:error, :invalid_request}
    end
  end

  @spec matches_version(term(), term()) :: :ok | {:error, :stale | :invalid_request}
  def matches_version(%{lock_version: version}, version), do: :ok

  # Only a malformed expected version is `:invalid_request`: `nil` from a locked re-read
  # (row deleted mid-transaction) keeps the base `:stale` verdict.
  def matches_version(_record, expected) when is_integer(expected), do: {:error, :stale}

  def matches_version(_record, _expected), do: {:error, :invalid_request}

  @spec update_with_stale_error(Ecto.Changeset.t()) ::
          {:ok, struct()} | {:error, :stale | :state_conflict | Ecto.Changeset.t()}
  def update_with_stale_error(changeset) do
    changeset
    |> Repo.update(stale_error_field: :lock_version, stale_error_message: "is stale")
    |> normalize_stale_result()
  end

  @spec delete_with_stale_error(Ecto.Changeset.t()) ::
          {:ok, struct()} | {:error, :stale | :state_conflict | Ecto.Changeset.t()}
  def delete_with_stale_error(changeset) do
    changeset
    |> Repo.delete(stale_error_field: :lock_version, stale_error_message: "is stale")
    |> normalize_stale_result()
  end

  defp normalize_stale_result({:error, changeset} = error) do
    cond do
      Keyword.has_key?(changeset.errors, :lock_version) -> {:error, :stale}
      Keyword.has_key?(changeset.errors, :provider_action_intents) -> {:error, :state_conflict}
      true -> error
    end
  end

  defp normalize_stale_result(result), do: result

  @spec put_attr(map(), atom(), term()) :: map()
  def put_attr(attrs, key, value) do
    if Enum.any?(Map.keys(attrs), &is_binary/1),
      do: Map.put(attrs, Atom.to_string(key), value),
      else: Map.put(attrs, key, value)
  end

  @spec drop_attr(map(), atom()) :: map()
  def drop_attr(attrs, key), do: Map.drop(attrs, [key, Atom.to_string(key)])

  @spec drop_attrs(map(), [atom()]) :: map()
  def drop_attrs(attrs, keys) do
    Enum.reduce(keys, attrs, fn key, acc -> drop_attr(acc, key) end)
  end
end
