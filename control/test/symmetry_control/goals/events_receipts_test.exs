defmodule SymmetryControl.Goals.EventsReceiptsTest do
  use SymmetryControl.DataCase, async: true

  alias SymmetryControl.Goals.EventsReceipts

  test "replay_command reports a missing mutation" do
    assert :missing =
             EventsReceipts.replay_command(Ecto.UUID.generate(), Ecto.UUID.generate(), %{})
  end

  test "nullable_datetime renders absent and present timestamps" do
    assert nil == EventsReceipts.nullable_datetime(nil)

    datetime = ~U[2026-01-02 03:04:05.000000Z]
    assert "2026-01-02T03:04:05.000000Z" == EventsReceipts.nullable_datetime(datetime)
  end

  test "valid_receipt_utc? accepts only Z-suffixed UTC instants" do
    assert EventsReceipts.valid_receipt_utc?("2026-01-02T03:04:05Z")
    refute EventsReceipts.valid_receipt_utc?("2026-01-02T03:04:05+00:00")
    refute EventsReceipts.valid_receipt_utc?("2026-01-02T03:04:05")
    refute EventsReceipts.valid_receipt_utc?(:not_a_string)
  end
end
