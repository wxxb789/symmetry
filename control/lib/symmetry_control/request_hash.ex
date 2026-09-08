defmodule SymmetryControl.RequestHash do
  @moduledoc false

  @spec canonical(term()) :: <<_::256>>
  def canonical(value), do: value |> canonical_json() |> Jason.encode!() |> digest()

  @spec legacy(term()) :: <<_::256>>
  def legacy(value), do: value |> :erlang.term_to_binary() |> digest()

  @spec write(term(), :standard | :command) :: {<<_::256>>, 1 | 2 | 3}
  def write(value, family \\ :standard), do: write(value, family, write_mode())

  @doc false
  @spec write(term(), :standard | :command, :legacy | :canonical) :: {<<_::256>>, 1 | 2 | 3}
  def write(value, :standard, :legacy), do: {legacy(value), 1}
  def write(value, :standard, :canonical), do: {canonical(value), 2}
  def write(value, :command, :legacy), do: {legacy(value), 2}
  def write(value, :command, :canonical), do: {canonical(value), 3}

  @spec matches?(binary(), pos_integer(), term()) :: boolean()
  def matches?(stored, 1, body), do: stored == legacy(body)
  def matches?(stored, 2, body), do: stored == canonical(body)
  def matches?(stored, 3, body), do: stored == canonical(body)
  def matches?(_stored, _version, _body), do: false

  defp canonical_json(%DateTime{} = value), do: DateTime.to_unix(value, :microsecond)

  defp canonical_json(%_{} = value),
    do: raise(ArgumentError, "unsupported request hash value: #{inspect(value)}")

  defp canonical_json(value) when is_map(value) do
    value
    |> Enum.map(fn {key, nested_value} -> {canonical_key(key), canonical_json(nested_value)} end)
    |> Enum.sort_by(&elem(&1, 0))
    |> Jason.OrderedObject.new()
  end

  defp canonical_json(value) when is_list(value), do: Enum.map(value, &canonical_json/1)
  defp canonical_json(value) when value in [true, false, nil], do: value
  defp canonical_json(value) when is_atom(value), do: Atom.to_string(value)

  defp canonical_json(value) when is_integer(value) or is_float(value) or is_binary(value),
    do: value

  defp canonical_json(value),
    do: raise(ArgumentError, "unsupported request hash value: #{inspect(value)}")

  defp canonical_key(key) when is_atom(key), do: Atom.to_string(key)
  defp canonical_key(key) when is_binary(key), do: key

  defp canonical_key(key),
    do: raise(ArgumentError, "unsupported request hash key: #{inspect(key)}")

  defp write_mode do
    :symmetry_control
    |> Application.get_env(:orchestration, [])
    |> Keyword.get(:request_hash_write_mode, :legacy)
  end

  defp digest(value), do: :crypto.hash(:sha256, value)
end
