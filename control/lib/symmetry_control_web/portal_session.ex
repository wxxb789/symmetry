defmodule SymmetryControlWeb.PortalSession do
  @moduledoc false

  @default_max_age_seconds 8 * 60 * 60
  @shell_path ~r{\A/portal(?:/(?:attention|chat|connections|goals/[A-Za-z0-9_-]+|projects(?:/[A-Za-z0-9_-]+)?|runtimes|work/[A-Za-z0-9_-]+))?\z}
  @connection_identifier_param ~r{\AconnectionId=[A-Za-z0-9_-]+\z}
  @legacy_identifier_param ~r{\A(?:project_id|run_id)=[A-Za-z0-9_-]+\z}
  # Conversation-surface search state the shell itself writes into its own
  # URLs: the scope selection and the selected project or run.
  @chat_scope_param ~r{\Ascope=(?:workspace|project|run)\z}
  @chat_identifier_param ~r{\A(?:projectId|runId)=[A-Za-z0-9_-]+\z}
  # The retired classic carrier's scope value, kept only so the one-time
  # `/portal` hash redirect can run after login.
  @legacy_chat_scope_param ~r{\Achat_scope=(?:workspace|project|run)\z}
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
  @connections_query_params [@connection_identifier_param, @work_project_param]

  def return_to(path) when is_binary(path) do
    if shell_return_to?(path), do: path, else: "/portal"
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

  # `/portal` is the one-time legacy hash redirect's carrier: the browser's hash
  # never reaches the server, so the retired classic search parameters
  # (`project_id`, `run_id`, `chat_scope`) must survive login for the redirect to
  # run once the SPA document loads at `/portal`.
  defp shell_query?("/portal", query), do: legacy_hash_query?(query)

  # `/portal/chat` is the migrated conversation surface; its URL state is the
  # scope selection, never a destination of its own.
  defp shell_query?("/portal/chat", query), do: chat_query?(query)

  defp shell_query?("/portal/attention", query), do: Regex.match?(@work_project_param, query)

  # The connections surface carries the selected connection and the project
  # whose resource surface it scopes; both are shell-written view state. Without
  # the project clause a session expiry collapses a project-scoped connections
  # page to /portal.
  defp shell_query?("/portal/connections", query), do: all_params?(query, @connections_query_params)

  # The shell's runtimes navigation carries the same project scope as the work
  # surface; without this clause a session expiry collapses a project-scoped
  # runtimes page to /portal.
  defp shell_query?("/portal/runtimes", query), do: Regex.match?(@work_project_param, query)

  defp shell_query?("/portal/projects/" <> _project_id, query), do: all_params?(query, @work_search_params)

  defp shell_query?("/portal/work/" <> _work_item_id, query), do: all_params?(query, @work_item_search_params)

  defp shell_query?(_path, _query), do: false

  defp all_params?(query, patterns) do
    query
    |> String.split("&")
    |> Enum.all?(fn param -> Enum.any?(patterns, &Regex.match?(&1, param)) end)
  end

  defp chat_query?(query), do: all_params?(query, [@chat_scope_param, @chat_identifier_param])

  defp legacy_hash_query?(query), do: all_params?(query, [@legacy_identifier_param, @legacy_chat_scope_param])

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

  defp fingerprint(token), do: token |> then(&:crypto.hash(:sha256, &1)) |> Base.encode16(case: :lower)

  defp value(map, key), do: Map.get(map, key) || Map.get(map, Atom.to_string(key))
end
