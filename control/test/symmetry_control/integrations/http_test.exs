defmodule SymmetryControl.Integrations.HTTPTest do
  use ExUnit.Case, async: true

  alias SymmetryControl.Integrations.HTTP

  # Port 1 is reserved and unbound, so a socket attempt fails immediately with econnrefused.
  @unreachable_url "http://127.0.0.1:1/x"

  describe "request/4 request construction" do
    test "an un-encodable body value raises instead of mapping to a transport error" do
      outcome = capture(fn -> HTTP.request(:post, @unreachable_url, [], %{pid: self()}) end)

      refute match?({:returned, {:error, {:transport, _}}}, outcome),
             "un-encodable body classified as a transport failure: #{inspect(outcome)}"

      assert {:raised, %Protocol.UndefinedError{}} = outcome
    end

    test "an invalid UTF-8 body raises instead of mapping to a transport error" do
      outcome = capture(fn -> HTTP.request(:post, @unreachable_url, [], <<0xFF>>) end)

      refute match?({:returned, {:error, {:transport, _}}}, outcome),
             "invalid UTF-8 body classified as a transport failure: #{inspect(outcome)}"

      assert {:raised, %Jason.EncodeError{}} = outcome
    end

    test "an un-encodable header value raises instead of mapping to a transport error" do
      outcome = capture(fn -> HTTP.request(:get, @unreachable_url, [{"x-test", self()}]) end)

      refute match?({:returned, {:error, {:transport, _}}}, outcome),
             "un-encodable header value classified as a transport failure: #{inspect(outcome)}"

      assert {:raised, %Protocol.UndefinedError{}} = outcome
    end
  end

  describe "request/4 transport" do
    test "a socket failure still maps to a transport error" do
      assert {:error, {:transport, {:failed_connect, _details}}} =
               HTTP.request(:get, @unreachable_url, [])
    end
  end

  defp capture(fun) do
    {:returned, fun.()}
  rescue
    error -> {:raised, error}
  end
end
