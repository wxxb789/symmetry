defmodule SymmetryControlWeb.PortalAssets do
  @moduledoc """
  Locates the built portal bundle that Phoenix serves as the workspace document.

  The bundle is frontend build output, not source: `pnpm --filter
  @symmetry/frontend build` runs Vite and then `frontend/scripts/stage-assets.mjs`,
  which replaces `priv/static/portal/` with the new `index.html` and hashed
  assets. That directory is gitignored, so a fresh checkout has no bundle until
  the build runs. CI stages it in the `control` job from the `contracts` job's
  `portal-assets` artifact (`.github/workflows/ci.yml`) before `mix test`.
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
