defmodule SymmetryControlWeb.PortalHTML do
  @moduledoc false

  require EEx

  @login_template Path.join(__DIR__, "portal_html/login.html.eex")
  @index_template Path.join(__DIR__, "portal_html/index.html.eex")
  @external_resource @login_template
  @external_resource @index_template

  EEx.function_from_file(:def, :login, @login_template, [:assigns], trim: true)
  EEx.function_from_file(:def, :index, @index_template, [:assigns], trim: true)
end
