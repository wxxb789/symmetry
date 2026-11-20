defmodule SymmetryControlWeb.PortalBundleTest do
  use ExUnit.Case, async: true

  alias SymmetryControlWeb.PortalBundle

  test "extracts hashed asset references, tolerating a query string" do
    index = """
    <script type="module" crossorigin src="/portal/assets/index-a1b2c3.js"></script>
    <link rel="stylesheet" href="/portal/assets/index-d4e5f6.css?vsn=1">
    """

    assert PortalBundle.referenced_assets(index) == [
             "/portal/assets/index-a1b2c3.js",
             "/portal/assets/index-d4e5f6.css"
           ]
  end

  test "does not match data-src or data-href attribute values" do
    index =
      ~s(<img data-src="/portal/assets/decoy-000000.js" data-href="/portal/assets/decoy-000001.css" src="/portal/assets/real-111111.js">)

    assert PortalBundle.referenced_assets(index) == ["/portal/assets/real-111111.js"]
  end
end
