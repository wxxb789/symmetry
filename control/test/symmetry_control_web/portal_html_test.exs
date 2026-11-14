defmodule SymmetryControlWeb.PortalHTMLTest do
  use ExUnit.Case, async: true

  alias SymmetryControlWeb.PortalHTML

  test "escapes dynamic login output" do
    html = PortalHTML.login(%{csrf_token: "csrf", error: "<b>x</b>", return_to: "/portal"})

    assert html =~ "&lt;b&gt;x&lt;/b&gt;"
    refute html =~ "<b>x</b>"
  end

  test "escapes CSRF and return paths in login inputs and the built shell placeholder" do
    csrf_token = "\"csrf\"&<"
    escaped = "&quot;csrf&quot;&amp;&lt;"

    login = PortalHTML.login(%{csrf_token: csrf_token, error: nil, return_to: "\"<&"})

    shell =
      PortalHTML.shell(
        ~s(<meta name="csrf-token" content="__SYMMETRY_CSRF__">),
        csrf_token
      )

    assert login =~ ~s(value="#{escaped}")
    assert login =~ ~s(name="return_to" value="&quot;&lt;&amp;")
    assert shell == ~s(<meta name="csrf-token" content="#{escaped}">)
    refute shell =~ "__SYMMETRY_CSRF__"
  end

  test "escapes csrf tokens in every classic workspace interpolation" do
    csrf_token = "\"csrf\"&<"
    escaped = "&quot;csrf&quot;&amp;&lt;"
    index = PortalHTML.index(%{csrf_token: csrf_token})

    assert length(String.split(index, escaped)) == 3
  end
end
