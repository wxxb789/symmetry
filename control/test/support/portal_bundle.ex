defmodule SymmetryControlWeb.PortalBundle do
  @moduledoc """
  Turns a missing or incomplete staged portal bundle into an explicit prerequisite.

  Four `:portal_assets` cases in `PortalControllerTest` assert against the real
  frontend build in `priv/static/portal/`: the SPA entry document and the hashed
  assets it references. That directory is gitignored build output, so a fresh
  checkout has it only after the pinned
  `mise exec pnpm@12.17.0 -- pnpm --filter @symmetry/frontend build`
  (`frontend/scripts/stage-assets.mjs`), and CI stages the same directory in the
  `control` job from the `contracts` job's `portal-assets` artifact before
  `mix test` (`.github/workflows/ci.yml`).

  `ConnCase` calls `require_staged!/1` for every web test, so tagging a case
  `:portal_assets` is how it states that prerequisite. Without a usable staging
  the tagged cases fail here, with the staging command and CI's equivalent
  staging, instead of a raw `File.Error` or `File.RenameError` on the staged path.
  No case is skipped silently: a host without Node/pnpm excludes the tag
  explicitly (`mix test --exclude portal_assets`).
  """

  alias SymmetryControlWeb.PortalAssets

  @build_command "mise exec pnpm@12.17.0 -- pnpm --filter @symmetry/frontend build"

  # `src`/`href` need an attribute boundary so `data-src="…"` does not match,
  # and a query suffix (Vite's `?vsn=…`) is consumed but not captured: the
  # reference is the bare asset path the staged directory holds.
  @asset_ref ~r/(?<![-\w])(?:src|href)="(\/portal\/assets\/[^"?]+\.(?:js|css))(?:\?[^"]*)?"/

  @doc """
  Checks the staged bundle when `tags` marks the case as bundle-dependent.

  Untagged cases pass through: only `:portal_assets` makes the bundle a
  prerequisite of the case.
  """
  def require_staged!(tags) do
    if tags[:portal_assets], do: assert_staged!()
    :ok
  end

  @doc """
  The hashed `/portal/assets/...` references of a staged entry document.

  A reference with a query suffix (for example `/portal/assets/b.css?vsn=1`)
  yields the bare asset path; `data-src`-style attributes are not references
  and are ignored.
  """
  def referenced_assets(index) do
    @asset_ref
    |> Regex.scan(index, capture: :all_but_first)
    |> List.flatten()
  end

  defp assert_staged! do
    index_path = PortalAssets.index_path()

    case File.read(index_path) do
      {:error, :enoent} ->
        fail_staging!(
          index_path,
          "the staged entry document is missing" <> leftover_rename(index_path)
        )

      {:error, reason} ->
        fail_staging!(index_path, "the staged entry document is unreadable (#{inspect(reason)})")

      {:ok, ""} ->
        fail_staging!(index_path, "the staged entry document is empty")

      {:ok, index} ->
        assert_assets_staged!(index_path, index)
    end
  end

  # A build stages `index.html` and its hashed assets together
  # (`frontend/scripts/stage-assets.mjs`); an entry document naming absent files is
  # either an interrupted staging or a build older than the current one.
  defp assert_assets_staged!(index_path, index) do
    assets = referenced_assets(index)
    unstaged = Enum.reject(assets, &File.regular?(staged_asset_path(&1)))

    cond do
      not Enum.any?(assets, &String.ends_with?(&1, ".js")) ->
        fail_staging!(
          index_path,
          "no `/portal/assets/*.js` reference found in the entry document"
        )

      not Enum.any?(assets, &String.ends_with?(&1, ".css")) ->
        fail_staging!(
          index_path,
          "no `/portal/assets/*.css` reference found in the entry document"
        )

      unstaged != [] ->
        fail_staging!(
          index_path,
          "the staged entry document references assets that are not staged: " <>
            Enum.join(unstaged, ", ")
        )

      true ->
        :ok
    end
  end

  # `PortalControllerTest`'s 503 case renames the entry document to
  # `index.html.unavailable-*` and restores it in `after`; a killed run can leave
  # the rename behind, and then the bundle looks absent rather than renamed.
  defp leftover_rename(index_path) do
    case Path.wildcard(index_path <> ".unavailable-*") do
      [] ->
        ""

      leftovers ->
        "; a leftover rename from the 503 case exists at " <>
          Enum.join(leftovers, ", ") <> " (restore it, or delete it and restage)"
    end
  end

  defp fail_staging!(index_path, problem) do
    ExUnit.Assertions.flunk("""
    the portal bundle that `priv/static/portal/` must hold is not usable: #{problem}
    (#{index_path}, the compiled app's view of the source tree's `control/priv/static/portal/`)

    The `:portal_assets` cases assert against the real frontend build staged in
    `control/priv/static/portal/`, so staging it is a Control-suite prerequisite,
    not a test fixture. The bundle is gitignored build output: a fresh checkout
    has it only after

        #{@build_command}

    and CI stages the same directory in the `control` job from the `contracts`
    job's `portal-assets` artifact before `mix test` (.github/workflows/ci.yml).
    Without Node/pnpm on this host, exclude those cases explicitly
    (`mix test --exclude portal_assets`) rather than removing their assertions.
    """)
  end

  defp staged_asset_path(asset), do: Path.join(PortalAssets.directory(), String.replace_prefix(asset, "/portal/", ""))
end
