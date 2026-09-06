defmodule SymmetryControl.Config.RuntimeTest do
  use ExUnit.Case, async: false

  @base_config Path.expand("../../../config/config.exs", __DIR__)
  @runtime_config Path.expand("../../../config/runtime.exs", __DIR__)
  @test_config Path.expand("../../../config/test.exs", __DIR__)

  @variables [
    "SYMMETRY_ENROLLMENT_TOKEN",
    "SYMMETRY_OPERATOR_TOKEN",
    "SYMMETRY_LEASE_DURATION_MS",
    "SYMMETRY_INTEGRATION_SYNC_INTERVAL_MS"
  ]

  setup do
    previous = Map.new(@variables, &{&1, System.get_env(&1)})

    on_exit(fn ->
      Enum.each(previous, fn
        {variable, nil} -> System.delete_env(variable)
        {variable, value} -> System.put_env(variable, value)
      end)
    end)
  end

  test "development runtime config rejects identical enrollment and operator tokens" do
    System.put_env("SYMMETRY_ENROLLMENT_TOKEN", "same-token")
    System.put_env("SYMMETRY_OPERATOR_TOKEN", "same-token")

    assert_raise RuntimeError,
                 "SYMMETRY_ENROLLMENT_TOKEN and SYMMETRY_OPERATOR_TOKEN must differ",
                 fn ->
                   Config.Reader.read!(@runtime_config, env: :dev)
                 end
  end

  test "base configuration defaults leases to two minutes" do
    config = Config.Reader.read!(@base_config, env: :dev)

    assert 120_000 == config[:symmetry_control][:orchestration][:lease_duration_ms]
  end

  test "base configuration filters provider credential parameters" do
    config = Config.Reader.read!(@base_config, env: :dev)

    assert Enum.all?(
             ["password", "credential", "token", "pat", "secret", "authorization"],
             &(&1 in config[:phoenix][:filter_parameters])
           )
  end

  test "development runtime configuration defaults and overrides lease duration" do
    System.delete_env("SYMMETRY_LEASE_DURATION_MS")

    default_config = Config.Reader.read!(@runtime_config, env: :dev)

    assert 120_000 == default_config[:symmetry_control][:orchestration][:lease_duration_ms]

    System.put_env("SYMMETRY_LEASE_DURATION_MS", "45000")

    override_config = Config.Reader.read!(@runtime_config, env: :dev)

    assert 45_000 == override_config[:symmetry_control][:orchestration][:lease_duration_ms]
  end

  test "runtime configuration rejects lease durations below thirty seconds" do
    for value <- ["0", "-1", "29999"] do
      System.put_env("SYMMETRY_LEASE_DURATION_MS", value)

      assert_raise RuntimeError, ~r/SYMMETRY_LEASE_DURATION_MS must be at least 30000/, fn ->
        Config.Reader.read!(@runtime_config, env: :dev)
      end
    end
  end

  test "runtime configuration rejects malformed lease durations" do
    System.put_env("SYMMETRY_LEASE_DURATION_MS", "abc")

    assert_raise RuntimeError, ~r/SYMMETRY_LEASE_DURATION_MS must be at least 30000/, fn ->
      Config.Reader.read!(@runtime_config, env: :dev)
    end
  end

  test "test configuration preserves its explicit short lease override" do
    config = Config.Reader.read!(@test_config, env: :test)

    assert 30_000 == config[:symmetry_control][:orchestration][:lease_duration_ms]
  end

  test "development integration configuration supports sync overrides" do
    System.put_env("SYMMETRY_INTEGRATION_SYNC_INTERVAL_MS", "60000")

    config = Config.Reader.read!(@runtime_config, env: :dev)

    assert 60_000 == config[:symmetry_control][:integrations][:sync_interval_ms]
    assert config[:symmetry_control][:integrations][:syncer_enabled]
  end

  test "runtime configuration rejects invalid sync intervals" do
    for value <- ["0", "29999", "invalid"] do
      System.put_env("SYMMETRY_INTEGRATION_SYNC_INTERVAL_MS", value)

      assert_raise RuntimeError,
                   ~r/SYMMETRY_INTEGRATION_SYNC_INTERVAL_MS must be at least 30000/,
                   fn -> Config.Reader.read!(@runtime_config, env: :dev) end
    end
  end
end
