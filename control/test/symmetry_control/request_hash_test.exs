defmodule SymmetryControl.RequestHashTest do
  use ExUnit.Case, async: true

  alias SymmetryControl.RequestHash

  test "canonical hash ignores map key representation and insertion order" do
    atom_keys = %{alpha: 1, nested: %{beta: true}, values: ["one", nil]}
    string_keys = %{"values" => ["one", nil], "nested" => %{"beta" => true}, "alpha" => 1}

    assert RequestHash.canonical(atom_keys) == RequestHash.canonical(string_keys)
  end

  test "canonical hash normalizes DateTime precision" do
    first = DateTime.from_unix!(1_700_000_000_000_000, :microsecond)
    same_instant = %{first | microsecond: {0, 3}}
    later = DateTime.add(first, 1, :microsecond)

    assert RequestHash.canonical(%{occurred_at: first}) ==
             RequestHash.canonical(%{occurred_at: same_instant})

    refute RequestHash.canonical(%{occurred_at: first}) ==
             RequestHash.canonical(%{occurred_at: later})
  end

  test "canonical and legacy hashes remain version-addressable" do
    body = %{kind: "completed", payload: %{"summary" => "done"}}

    refute RequestHash.canonical(body) == RequestHash.legacy(body)
    assert RequestHash.matches?(RequestHash.legacy(body), 1, body)
    assert RequestHash.matches?(RequestHash.canonical(body), 2, body)
    assert RequestHash.matches?(RequestHash.canonical(body), 3, body)
  end

  test "write modes keep legacy-compatible rows until canonical cutover" do
    body = %{kind: "completed", payload: %{"summary" => "done"}}

    assert {legacy, 1} = RequestHash.write(body, :standard, :legacy)
    assert legacy == RequestHash.legacy(body)
    assert {canonical, 2} = RequestHash.write(body, :standard, :canonical)
    assert canonical == RequestHash.canonical(body)

    assert {command_legacy, 2} = RequestHash.write(body, :command, :legacy)
    assert command_legacy == RequestHash.legacy(body)
    assert {command_canonical, 3} = RequestHash.write(body, :command, :canonical)
    assert command_canonical == RequestHash.canonical(body)
  end

  test "canonical hash keeps JSON literals distinct from strings" do
    refute RequestHash.canonical(%{value: true}) == RequestHash.canonical(%{value: "true"})
    refute RequestHash.canonical(%{value: nil}) == RequestHash.canonical(%{value: "nil"})
  end

  test "unsupported structs and map keys raise ArgumentError" do
    assert_raise ArgumentError, fn -> RequestHash.canonical(%{date: ~D[2026-09-07]}) end
    assert_raise ArgumentError, fn -> RequestHash.canonical(%{1 => "unsupported"}) end
  end
end
