defmodule SymmetryControlWeb.ErrorJSON do
  @moduledoc """
  This module is invoked by your endpoint in case of errors on JSON requests.

  See config/config.exs.
  """

  def render("404.json", _assigns), do: error("not_found", "resource was not found")

  def render(<<"5", _status::binary-size(2), ".json">>, _assigns),
    do: error("internal_error", "internal server error")

  def render(_template, _assigns), do: error("invalid_request", "request is invalid")

  defp error(code, message), do: %{error: %{code: code, message: message}}
end
