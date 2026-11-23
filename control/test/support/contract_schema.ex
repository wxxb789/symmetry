defmodule SymmetryControl.ContractSchema do
  @moduledoc """
  The contract-bundle schema adaptation every bundle-resolving test shares.

  Tests resolve `daemon/internal/contracts/schema_bundle.json` — the one
  canonical bundle — with `ExJsonSchema`, while the production validator in
  `SymmetryControl.Goals.ContractValidation` adapts the bundle's ECMA-262
  patterns to PCRE2 before resolving them. This module holds that adaptation
  once instead of a private copy per test module: it matches
  `ContractValidation`'s rewrite without changing either the canonical bundle or
  the production validator's supported reads.
  """

  @doc """
  Rewrites the bundle's ECMA-262 `\\u0000` pattern escapes to the PCRE2
  `\\x{0}` form `ExJsonSchema` compiles; every other member is returned as is.
  """
  def rewrite_pcre2_pattern_escapes(value) when is_map(value) do
    Map.new(value, fn
      {"pattern", pattern} when is_binary(pattern) ->
        {"pattern", String.replace(pattern, "\\u0000", "\\x{0}")}

      {key, child} ->
        {key, rewrite_pcre2_pattern_escapes(child)}
    end)
  end

  def rewrite_pcre2_pattern_escapes(value) when is_list(value), do: Enum.map(value, &rewrite_pcre2_pattern_escapes/1)

  def rewrite_pcre2_pattern_escapes(value), do: value
end
