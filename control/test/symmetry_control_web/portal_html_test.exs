defmodule SymmetryControlWeb.PortalHTMLTest do
  use ExUnit.Case, async: true

  alias SymmetryControlWeb.PortalHTML

  test "escapes dynamic login output" do
    html = PortalHTML.login(%{csrf_token: "csrf", error: "<b>x</b>"})

    assert html =~ "&lt;b&gt;x&lt;/b&gt;"
    refute html =~ "<b>x</b>"
  end

  test "escapes csrf tokens in every portal template interpolation" do
    csrf_token = "\"csrf\"&<"
    escaped = "&quot;csrf&quot;&amp;&lt;"

    login = PortalHTML.login(%{csrf_token: csrf_token, error: nil})
    index = PortalHTML.index(%{csrf_token: csrf_token})

    assert login =~ ~s(value="#{escaped}")
    assert length(String.split(index, escaped)) == 3
  end
end
