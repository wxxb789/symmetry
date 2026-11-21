defmodule SymmetryControl.Goals.ChangesetValidators do
  @moduledoc false

  import Ecto.Changeset

  def validate_digest(changeset, field) do
    validate_change(changeset, field, fn ^field, value ->
      if is_binary(value) and byte_size(value) == 32,
        do: [],
        else: [{field, "must be a 32-byte digest"}]
    end)
  end

  def validate_map(changeset, field) do
    validate_change(changeset, field, fn ^field, value ->
      if is_map(value), do: [], else: [{field, "must be an object"}]
    end)
  end

  def reject_changes(changeset, label) do
    Enum.reduce(changeset.changes, changeset, fn {field, _value}, acc ->
      add_error(acc, field, "#{label} is immutable")
    end)
  end
end
