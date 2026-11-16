defmodule SymmetryControlWeb.PortalSession do
  @moduledoc false

  @default_max_age_seconds 8 * 60 * 60
  @shell_path ~r{\A/portal(?:/(?:attention|connections|runtimes|projects(?:/[A-Za-z0-9_-]+)?|work/[A-Za-z0-9_-]+))?\z}
  @connection_identifier_param ~r{\AconnectionId=[A-Za-z0-9_-]+\z}
  @classic_identifier_param ~r{\A(?:project_id|run_id)=[A-Za-z0-9_-]+\z}
  # Work-surface search state the shell itself writes into its own URLs: the
  # view, the text filters, and the JSON-encoded blocked flag. Values stay
  # anchored to that encoding, so an unencoded control character or an
  # unescaped path separator is rejected instead of being passed through. The
  # only C0 escape the shell emits for filter text is the tab (%09), which is
  # accepted; every other control-character escape stays rejected.
  @work_view_param ~r{\Aview=(?:list|board)\z}
  @work_text_param ~r{\A(?:q|state|owner)=(?:[A-Za-z0-9*.+_-]|%(?:09|[2-9A-Fa-f][0-9A-Fa-f]))*\z}
  @work_blocked_param ~r{\Ablocked=(?:1|%221%22)\z}
  @work_project_param ~r{\AprojectId=[A-Za-z0-9_-]+\z}
  @work_search_params [@work_view_param, @work_text_param, @work_blocked_param]
  @work_item_search_params @work_search_params ++ [@work_project_param]

  def return_to(path) when is_binary(path) do
    if shell_return_to?(path) or classic_return_to?(path), do: path, else: "/portal"
  end

  def return_to(_path), do: "/portal"

  defp shell_return_to?(path) do
    case URI.parse(path) do
      %URI{scheme: nil, host: nil, path: shell_path, query: query, fragment: nil}
      when is_binary(shell_path) ->
        Regex.match?(@shell_path, shell_path) and shell_query?(shell_path, query)

      _ ->
        false
    end
  end

  defp shell_query?(_path, nil), do: true

  defp shell_query?("/portal", query), do: classic_query?(query)

  defp shell_query?("/portal/attention", query),
    do: Regex.match?(~r{\AprojectId=[A-Za-z0-9_-]+\z}, query)

  defp shell_query?("/portal/connections", query),
    do: Regex.match?(@connection_identifier_param, query)

  defp shell_query?("/portal/projects/" <> _project_id, query),
    do: work_query?(query, @work_search_params)

  defp shell_query?("/portal/work/" <> _work_item_id, query),
    do: work_query?(query, @work_item_search_params)

  defp shell_query?(_path, _query), do: false

  defp work_query?(query, patterns) do
    query
    |> String.split("&")
    |> Enum.all?(fn param -> Enum.any?(patterns, &Regex.match?(&1, param)) end)
  end

  defp classic_return_to?("/portal/classic" <> _rest = path) do
    case URI.parse(path) do
      %URI{scheme: nil, host: nil, path: "/portal/classic", query: query, fragment: fragment} ->
        fragment in [nil, "board", "chat", "activity", "resources", "connections", "health"] and
          classic_query?(query)

      _ ->
        false
    end
  end

  defp classic_return_to?(_path), do: false

  defp classic_query?(nil), do: true

  defp classic_query?(query) do
    query
    |> String.split("&")
    |> Enum.all?(fn param ->
      Regex.match?(@classic_identifier_param, param) or
        param in ["chat_scope=workspace", "chat_scope=project", "chat_scope=run"]
    end)
  end

  def issue(operator_token, opts \\ []) when is_binary(operator_token) do
    %{
      issued_at: Keyword.get(opts, :now, System.system_time(:second)),
      token_fingerprint: fingerprint(operator_token)
    }
  end

  def valid?(session, operator_token, opts \\ [])

  def valid?(session, operator_token, opts) when is_map(session) and is_binary(operator_token) do
    now = Keyword.get(opts, :now, System.system_time(:second))
    max_age_seconds = Keyword.get(opts, :max_age_seconds, configured_max_age_seconds())
    issued_at = value(session, :issued_at)
    stored_fingerprint = value(session, :token_fingerprint)
    expected_fingerprint = fingerprint(operator_token)

    is_integer(issued_at) and is_integer(max_age_seconds) and max_age_seconds > 0 and
      now >= issued_at and now - issued_at < max_age_seconds and
      is_binary(stored_fingerprint) and
      byte_size(stored_fingerprint) == byte_size(expected_fingerprint) and
      Plug.Crypto.secure_compare(stored_fingerprint, expected_fingerprint)
  end

  def valid?(_session, _operator_token, _opts), do: false

  def configured_max_age_seconds do
    :symmetry_control
    |> Application.get_env(:orchestration, [])
    |> Keyword.get(:portal_session_max_age_seconds, @default_max_age_seconds)
  end

  defp fingerprint(token),
    do: token |> then(&:crypto.hash(:sha256, &1)) |> Base.encode16(case: :lower)

  defp value(map, key), do: Map.get(map, key) || Map.get(map, Atom.to_string(key))
end
