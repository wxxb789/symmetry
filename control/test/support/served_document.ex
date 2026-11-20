defmodule SymmetryControlWeb.ServedDocument do
  @moduledoc """
  Validates a real served document against the canonical bundle the manifest
  gate and the daemon use. A member-name disagreement with the serving
  projection fails here, on the server's own output, instead of passing review.
  """
  import ExUnit.Assertions

  alias SymmetryControl.ContractSchema

  @schema_bundle Path.expand("../../../daemon/internal/contracts/schema_bundle.json", __DIR__)

  @doc "Validates `document` against the bundle's `\"\#{name}.schema.json\"`."
  def validate_served_document!(name, document) do
    bundle = @schema_bundle |> File.read!() |> Jason.decode!()

    schema =
      bundle
      |> Map.fetch!("#{name}.schema.json")
      |> ContractSchema.rewrite_pcre2_pattern_escapes()
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
end
