defmodule SymmetryControl.Goals.RunFenceTest do
  use SymmetryControl.DataCase, async: true

  alias SymmetryControl.Goals.RunFence

  test "claim_with_disposition rejects malformed inputs" do
    assert {:error, :invalid_request} = RunFence.claim_with_disposition("not-a-uuid", %{}, [])

    assert {:error, :invalid_request} =
             RunFence.claim_with_disposition(Ecto.UUID.generate(), :map, [])

    assert {:error, :invalid_request} =
             RunFence.claim_with_disposition(Ecto.UUID.generate(), %{}, :opts)
  end

  test "claim_with_disposition reports an unknown run" do
    assert {:error, :not_found} =
             RunFence.claim_with_disposition(Ecto.UUID.generate(), %{
               runtime_id: Ecto.UUID.generate()
             })
  end

  test "fetch_run_context rejects malformed fences" do
    assert {:error, :invalid_request} =
             RunFence.fetch_run_context("not-a-uuid", Ecto.UUID.generate(), %{})

    assert {:error, :invalid_request} =
             RunFence.fetch_run_context(Ecto.UUID.generate(), "not-a-uuid", %{})

    assert {:error, :invalid_request} =
             RunFence.fetch_run_context(Ecto.UUID.generate(), Ecto.UUID.generate(), %{})
  end
end
