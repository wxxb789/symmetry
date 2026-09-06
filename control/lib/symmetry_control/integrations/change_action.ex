defmodule SymmetryControl.Integrations.ChangeAction do
  @moduledoc false

  @upsert_keys ["source_branch", "target_branch", "title", "body"]
  @update_keys ["title", "body", "pull_request_url"]
  @caller_keys ["title", "body"]

  def validate_keys(operation, input) when is_map(input) do
    allowed =
      case operation do
        "change.upsert" -> @upsert_keys
        "change.update" -> @update_keys
      end

    validate_allowed_keys(input, allowed)
  end

  def branch(input, key) do
    case value(input, key) do
      branch when is_binary(branch) -> normalize_branch(branch)
      _value -> {:error, :invalid_request}
    end
  end

  def normalize_caller_input(operation, input)
      when operation in ["change.upsert", "change.update"] and is_map(input) do
    with :ok <- validate_allowed_keys(input, @caller_keys),
         {:ok, title} <- title(input),
         {:ok, body} <- body(input) do
      {:ok, put_body(%{"title" => title}, "body", body)}
    end
  end

  def normalize_caller_input(_operation, _input), do: {:error, :invalid_request}

  def normalize_branch(value) when is_binary(value) do
    branch = value |> String.trim() |> String.replace_prefix("refs/heads/", "")

    invalid? =
      branch == "" or byte_size(branch) > 255 or branch in ["@", "HEAD"] or
        String.starts_with?(branch, ["/", ".", "-"]) or
        String.ends_with?(branch, ["/", ".", ".lock"]) or
        String.contains?(branch, ["..", "//", "@{", "\\", "~", "^", ":", "?", "*", "["]) or
        Regex.match?(~r/[[:space:][:cntrl:]]/u, branch) or invalid_component?(branch)

    if invalid?, do: {:error, :invalid_request}, else: {:ok, branch}
  end

  def normalize_branch(_value), do: {:error, :invalid_request}

  def title(input) do
    case value(input, "title") do
      title when is_binary(title) ->
        title = String.trim(title)

        if title != "" and String.length(title) <= 255,
          do: {:ok, title},
          else: {:error, :invalid_request}

      _value ->
        {:error, :invalid_request}
    end
  end

  def body(input) do
    case value(input, "body") do
      :missing -> {:ok, :missing}
      body when is_binary(body) and byte_size(body) <= 1_048_576 -> {:ok, body}
      _value -> {:error, :invalid_request}
    end
  end

  def put_body(payload, _key, :missing), do: payload
  def put_body(payload, key, body), do: Map.put(payload, key, body)

  def value(input, key) do
    atom_key = atom_key(key)

    cond do
      Map.has_key?(input, key) -> Map.get(input, key)
      Map.has_key?(input, atom_key) -> Map.get(input, atom_key)
      true -> :missing
    end
  end

  def linked_pull_request_url(work_item) do
    Map.get(work_item, :external_pull_request_url) || Map.get(work_item, :pull_request_url) ||
      Map.get(work_item, "external_pull_request_url") || Map.get(work_item, "pull_request_url")
  end

  defp validate_allowed_keys(input, allowed) do
    if Enum.all?(Map.keys(input), &allowed_key?(&1, allowed)),
      do: :ok,
      else: {:error, :invalid_request}
  end

  defp invalid_component?(branch) do
    Enum.any?(String.split(branch, "/"), fn component ->
      String.starts_with?(component, ".") or
        String.ends_with?(String.downcase(component), ".lock")
    end)
  end

  defp allowed_key?(key, allowed) when is_binary(key), do: key in allowed
  defp allowed_key?(key, allowed) when is_atom(key), do: Atom.to_string(key) in allowed
  defp allowed_key?(_key, _allowed), do: false

  defp atom_key("source_branch"), do: :source_branch
  defp atom_key("target_branch"), do: :target_branch
  defp atom_key("title"), do: :title
  defp atom_key("body"), do: :body
  defp atom_key("pull_request_url"), do: :pull_request_url
end
