defmodule SymmetryControl.Repo do
  use Ecto.Repo,
    otp_app: :symmetry_control,
    adapter: Ecto.Adapters.Postgres
end
