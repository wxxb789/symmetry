defmodule SymmetryControl.Integrations.Provider do
  @moduledoc false

  @type operation :: String.t()

  @callback authenticate(map()) :: {:ok, term()} | {:error, term()}
  @callback check(map(), term()) :: {:ok, map()} | {:error, term()}
  @callback validate_resource_reference(map(), String.t(), String.t() | nil) ::
              :ok | {:error, term()}
  @callback sync_resource(map(), map(), term()) :: {:ok, map()} | {:error, term()}
  @callback sync_delivery(map(), map(), map(), term()) ::
              {:ok, map() | nil} | {:error, term()}
  @callback sync_ci(map(), map(), map(), term()) :: {:ok, map()} | {:error, term()}
  @callback execute(map(), map(), map(), operation(), map(), term()) ::
              {:ok, map()} | {:error, term()}
end
