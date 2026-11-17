defmodule SymmetryControlWeb.PortalHTML do
  @moduledoc false

  require EEx

  @login_template Path.join(__DIR__, "portal_html/login.html.eex")
  @external_resource @login_template

  EEx.function_from_file(:def, :login, @login_template, [:assigns], trim: true)

  def shell(index_html, csrf_token) do
    String.replace(index_html, "__SYMMETRY_CSRF__", Plug.HTML.html_escape(csrf_token))
  end
end
