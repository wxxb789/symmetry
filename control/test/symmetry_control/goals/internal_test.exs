defmodule SymmetryControl.Goals.InternalTest do
  use ExUnit.Case, async: true

  alias SymmetryControl.Goals.Internal

  test "has_key?/2 resolves an atom key against a string-keyed map (red mutant: swap with map_has_key?/2)" do
    assert Internal.has_key?(%{"subject" => 1}, :subject)
    refute Internal.has_key?(%{subject: 1}, "subject")
  end

  test "map_has_key?/2 resolves a string key against an atom-keyed map (red mutant: swap with has_key?/2)" do
    assert Internal.map_has_key?(%{subject: 1}, "subject")
    refute Internal.map_has_key?(%{"subject" => 1}, :subject)
  end
end
