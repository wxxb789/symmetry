defmodule SymmetryControlWeb.ServedDocument do
  @moduledoc """
  Validates a real served document against the canonical bundle the manifest
  gate and the daemon use. A member-name disagreement with the serving
  projection fails here, on the server's own output, instead of passing review.
  """
  import ExUnit.Assertions

  @schema_bundle Path.expand("../../../daemon/internal/contracts/schema_bundle.json", __DIR__)

  @doc "Validates `document` against the bundle's `\"\#{name}.schema.json\"`."
  def validate_served_document!(name, document) do
    bundle = @schema_bundle |> File.read!() |> Jason.decode!()

    schema =
      bundle
      |> Map.fetch!("#{name}.schema.json")
      |> rewrite_pcre2_pattern_escapes()
      |> ExJsonSchema.Schema.resolve()

    case ExJsonSchema.Validator.validate(schema, document) do
      :ok ->
        :ok

      {:error, errors} ->
        flunk(
          "#{name} real served document failed the contract bundle: #{inspect(errors, pretty: true)}"
        )
    end
  end

  # Match ContractValidation's ECMA-262-to-PCRE2 adaptation, exactly as the
  # manifest gate does, so the bundle resolves in ExJsonSchema.
  defp rewrite_pcre2_pattern_escapes(value) when is_map(value) do
    Map.new(value, fn
      {"pattern", pattern} when is_binary(pattern) ->
        {"pattern", String.replace(pattern, "\\u0000", "\\x{0}")}

      {key, child} ->
        {key, rewrite_pcre2_pattern_escapes(child)}
    end)
  end

  defp rewrite_pcre2_pattern_escapes(value) when is_list(value),
    do: Enum.map(value, &rewrite_pcre2_pattern_escapes/1)

  defp rewrite_pcre2_pattern_escapes(value), do: value
end
