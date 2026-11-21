defmodule SymmetryControl.Integrations.Providers.Support do
  @moduledoc false

  # Shared plumbing for the HTTP-backed provider implementations. Every function
  # below was AST-identical across GitHub and AzureDevOps before this module
  # existed (decoded_path_segments/1 differed by whitespace only); the three
  # authenticate-then-delegate functions take the provider module as their first
  # argument. Provider-specific URL/body mapping, pagination and error atoms stay
  # in the provider modules.

  @doc "Authenticates the connection, then delegates to the provider's `check/2`."
  def check(provider, connection) do
    with {:ok, headers} <- provider.authenticate(connection),
         do: provider.check(connection, headers)
  end

  @doc "Authenticates the connection, then delegates to the provider's `sync_resource/3`."
  def sync_resource(provider, connection, resource) do
    with {:ok, headers} <- provider.authenticate(connection),
         do: provider.sync_resource(connection, resource, headers)
  end

  @doc """
  Short-circuits a work item without a pull request, otherwise authenticates the
  connection and delegates to the provider's `sync_delivery/4`.
  """
  def sync_delivery(_provider, _connection, _resource, %{pull_request_url: nil}), do: {:ok, nil}

  def sync_delivery(provider, connection, resource, work_item) do
    with {:ok, headers} <- provider.authenticate(connection),
         do: provider.sync_delivery(connection, resource, work_item, headers)
  end

  def http do
    Application.get_env(
      :symmetry_control,
      :integration_http_client,
      SymmetryControl.Integrations.HTTP
    )
  end

  def auth do
    Application.get_env(
      :symmetry_control,
      :integration_auth_provider,
      SymmetryControl.Integrations.Auth
    )
  end

  def segment(value), do: URI.encode(value, &URI.char_unreserved?/1)

  def now, do: DateTime.utc_now() |> DateTime.truncate(:microsecond)

  def required_datetime(value) when is_binary(value) do
    case DateTime.from_iso8601(value) do
      {:ok, parsed, _offset} -> DateTime.truncate(parsed, :microsecond)
      _ -> nil
    end
  end

  def required_datetime(_value), do: nil

  def latest_by(entries, key_fun) do
    entries
    |> Enum.with_index()
    |> Enum.uniq_by(fn {entry, index} -> key_fun.(entry) || {:entry, index} end)
    |> Enum.map(&elem(&1, 0))
  end

  def priority(labels) do
    Enum.find_value(labels, "no_priority", fn label ->
      case String.downcase(label) do
        "priority:urgent" -> "urgent"
        "priority:high" -> "high"
        "priority:medium" -> "medium"
        "priority:low" -> "low"
        _ -> nil
      end
    end)
  end

  def same_reference?(left, right),
    do: String.downcase(left) == String.downcase(right)

  def decoded_path_segments(path) when is_binary(path) do
    {:ok, path |> String.split("/", trim: true) |> Enum.map(&URI.decode/1)}
  rescue
    ArgumentError -> {:error, :invalid_pull_request_url}
  end

  def decoded_path_segments(_path), do: {:error, :invalid_pull_request_url}
end
