defmodule SymmetryControlWeb.DaemonChannelTest do
  use SymmetryControl.DataCase, async: false
  import Phoenix.ChannelTest

  alias SymmetryControl.Orchestration
  @endpoint SymmetryControlWeb.Endpoint

  test "only the authenticated machine can join its hint topic and receives work hints" do
    %{machine: machine, token: token} =
      assert_enrolled("socket-builder")

    assert {:ok, socket} =
             connect(SymmetryControlWeb.UserSocket, %{},
               connect_info: %{"x_headers" => [{"x-symmetry-token", token}]}
             )

    assert {:ok, _, socket} = subscribe_and_join(socket, "daemon:#{machine.id}")

    assert {:error, %{reason: "forbidden"}} =
             subscribe_and_join(socket, "daemon:00000000-0000-0000-0000-000000000999")

    Phoenix.PubSub.broadcast(
      SymmetryControl.PubSub,
      "daemon:#{machine.id}",
      {:work_available, %{runtime_id: "runtime-id"}}
    )

    assert_push "work_available", %{type: "work_available", runtime_id: "runtime-id"}
  end

  test "does not authenticate a socket from Authorization" do
    %{token: token} = assert_enrolled("socket-no-authorization")

    assert :error =
             connect(SymmetryControlWeb.UserSocket, %{},
               connect_info: %{"x_headers" => [{"authorization", "Bearer " <> token}]}
             )
  end

  defp assert_enrolled(name) do
    assert {:ok, enrolled} =
             Orchestration.enroll_machine(%{name: name},
               enrollment_token: "test-enrollment-token",
               expected_enrollment_token: "test-enrollment-token"
             )

    enrolled
  end
end
