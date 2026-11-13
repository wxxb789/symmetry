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
  @callback execute_bound(map(), map(), map(), operation(), map(), map(), term()) ::
              {:ok, map()} | {:error, term()}
  @callback readback_action(map(), map(), operation(), map(), term()) ::
              {:ok, %{status: :confirmed, delivery: map()} | %{status: :unconfirmed}}
              | {:error, term()}
  @optional_callbacks execute_bound: 7, readback_action: 5
end
