defmodule SymmetryControl.Integrations.HTTPStub do
  @moduledoc false

  @shared_state __MODULE__.SharedState

  def expect(expectations), do: Process.put({__MODULE__, :expectations}, expectations)

  def expect_shared(expectations) do
    case Process.whereis(@shared_state) do
      nil ->
        {:ok, _pid} = Agent.start(fn -> expectations end, name: @shared_state)

      _pid ->
        Agent.update(@shared_state, fn _current -> expectations end)
    end

    :ok
  end

  def verify! do
    case Process.get({__MODULE__, :expectations}, []) do
      [] -> :ok
      remaining -> raise "unconsumed HTTP expectations: #{inspect(remaining)}"
    end
  end

  def verify_shared! do
    case Process.whereis(@shared_state) do
      nil ->
        :ok

      _pid ->
        case Agent.get(@shared_state, & &1) do
          [] -> :ok
          remaining -> raise "unconsumed shared HTTP expectations: #{inspect(remaining)}"
        end
    end
  end

  def stop_shared do
    case Process.whereis(@shared_state) do
      nil -> :ok
      pid -> Agent.stop(pid)
    end
  end

  def request(method, url, headers, body \\ nil) do
    case next_expectation() do
      [expectation | rest] ->
        Process.put({__MODULE__, :expectations}, rest)
        assert_request!(expectation, method, url, headers, body)
        expectation.response

      {:shared, expectation} ->
        assert_request!(expectation, method, url, headers, body)
        expectation.response

      [] ->
        raise "unexpected HTTP request: #{inspect({method, url, headers, body})}"
    end
  end

  defp next_expectation do
    case Process.get({__MODULE__, :expectations}, :missing) do
      :missing -> pop_shared_expectation()
      expectations -> expectations
    end
  end

  defp pop_shared_expectation do
    case Process.whereis(@shared_state) do
      nil ->
        []

      _pid ->
        Agent.get_and_update(@shared_state, fn
          [expectation | rest] -> {{:shared, expectation}, rest}
          [] -> {[], []}
        end)
    end
  end

  defp assert_request!(expectation, method, url, headers, body) do
    unless expectation.method == method,
      do: raise("expected #{expectation.method}, got #{method}")

    unless String.contains?(url, expectation.url_contains),
      do: raise("expected URL containing #{expectation.url_contains}, got #{url}")

    Enum.each(Map.get(expectation, :url_excludes, []), fn excluded ->
      if String.contains?(url, excluded),
        do: raise("expected URL not to contain #{excluded}, got #{url}")
    end)

    Enum.each(Map.get(expectation, :headers, %{}), fn {name, value} ->
      actual =
        Enum.find_value(headers, fn {header_name, header_value} ->
          if String.downcase(to_string(header_name)) == String.downcase(name),
            do: to_string(header_value)
        end)

      unless actual == value,
        do: raise("expected header #{name}=#{value}, got #{inspect(actual)}")
    end)

    if assertion = Map.get(expectation, :body_assertion), do: assertion.(body)
  end
end
