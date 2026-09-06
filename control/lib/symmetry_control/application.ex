defmodule SymmetryControl.Application do
  # See https://elixir.hexdocs.pm/Application.html
  # for more information on OTP Applications
  @moduledoc false

  use Application

  @impl true
  def start(_type, _args) do
    children =
      [
        SymmetryControlWeb.Telemetry,
        SymmetryControl.Repo,
        {DNSCluster,
         query: Application.get_env(:symmetry_control, :dns_cluster_query) || :ignore},
        {Phoenix.PubSub, name: SymmetryControl.PubSub},
        SymmetryControl.Integrations.ProviderAccess,
        SymmetryControl.Orchestration.Scheduler,
        SymmetryControl.Orchestration.Reconciler
      ] ++ integration_children() ++ [SymmetryControlWeb.Endpoint]

    # See https://elixir.hexdocs.pm/Supervisor.html
    # for other strategies and supported options
    opts = [strategy: :one_for_one, name: SymmetryControl.Supervisor]
    Supervisor.start_link(children, opts)
  end

  defp integration_children do
    if Application.get_env(:symmetry_control, :integrations, [])[:syncer_enabled],
      do: [SymmetryControl.Integrations.Syncer],
      else: []
  end

  # Tell Phoenix to update the endpoint configuration
  # whenever the application is updated.
  @impl true
  def config_change(changed, _new, removed) do
    SymmetryControlWeb.Endpoint.config_change(changed, removed)
    :ok
  end
end
