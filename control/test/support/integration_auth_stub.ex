defmodule SymmetryControl.Integrations.AuthStub do
  @moduledoc false

  def github_headers(_connection) do
    {:ok,
     [
       {"accept", "application/vnd.github+json"},
       {"authorization", "Bearer gho_github-token"},
       {"user-agent", "symmetry-control"},
       {"x-github-api-version", "2026-03-10"}
     ]}
  end

  def azure_devops_headers(_connection) do
    {:ok, [{"accept", "application/json"}, {"authorization", "Bearer azure-token"}]}
  end
end

defmodule SymmetryControl.Integrations.CommandStub do
  @moduledoc false

  def expect(expectations), do: Process.put({__MODULE__, :expectations}, expectations)

  def verify! do
    case Process.get({__MODULE__, :expectations}, []) do
      [] -> :ok
      remaining -> raise "unconsumed command expectations: #{inspect(remaining)}"
    end
  end

  def run(executable, arguments, options \\ []) do
    case Process.get({__MODULE__, :expectations}, []) do
      [
        %{executable: ^executable, arguments: ^arguments, options: ^options, response: response}
        | rest
      ] ->
        Process.put({__MODULE__, :expectations}, rest)
        response

      [expectation | _rest] ->
        raise "unexpected command: #{inspect({executable, arguments, options})}; expected #{inspect(expectation)}"

      [] ->
        raise "unexpected command: #{inspect({executable, arguments, options})}"
    end
  end
end
