defmodule SymmetryControlWeb.PortalAssets do
  @moduledoc """
  Locates the built portal bundle that Phoenix serves as the workspace document.

  The bundle is gitignored build output holding `index.html` and its hashed
  assets; a fresh checkout has none until the frontend build stages it
  (`docs/runbooks/portal.md`).
  """

  @doc """
  Absolute path of the staged SPA entry document that `PortalController.index/2`
  reads and that `PortalControllerTest` rewrites to exercise the unavailable path.
  """
  def index_path, do: Path.join(directory(), "index.html")

  @doc """
  Absolute path of the staged bundle directory.
  """
  def directory, do: Application.app_dir(:symmetry_control, "priv/static/portal")
end
