# pi 1.5.2 Windows native transport receipt

Date: 2026-11-11. Scope: Windows x64, native process/stdio/RPC transport and
the generated provider-bridge extension. This does not establish a prompt,
model call, repository mutation, retained resume or capability promotion.

## Exact binary

- Release asset: `pi-windows-x64.zip` from
  `https://github.com/earendil-works/pi/releases/download/v1.5.2/`
- Archive SHA-256:
  `e6adb8cb8c84eca14391ad24b363de9d2be98a89ca3ba3fdee1fa4de32b7f2e8`
- `pi.exe` SHA-256:
  `40a062bedd75767e2c3c889265de7c83d64a0ba55c74b60243c2453d97cffd85`
- `pi --version`: `1.5.2`

## Results

Source revision `a04c4ab02d1a6a35aa682c5b845133c720d3be61` with the local
1.5.2 pin change applied.

```text
SYMMETRY_PI_NATIVE_SMOKE=1 \
SYMMETRY_PI_NATIVE_SMOKE_EXECUTABLE=<absolute>/pi.exe \
  go test ./internal/harness/pi -run '^TestNativeRPCOpenAndClose$' -count=1 -v -timeout 60s

--- PASS: TestNativeRPCOpenAndClose (1.03s)
```

```text
SYMMETRY_PI_PROVIDER_EXTENSION_SMOKE=1 \
SYMMETRY_PI_NATIVE_SMOKE_EXECUTABLE=<absolute>/pi.exe \
  go test ./internal/harness/pi -run '^TestNativePiProviderBridgeLifecycle$' -count=1 -v -timeout 90s

--- PASS: TestNativePiProviderBridgeLifecycle (0.78s)
```

The first check proves launch, version gate, `get_state` correlation, event
delivery and bounded close. The second proves the daemon-owned extension loads
and the bridge binds without executing a provider action. Neither check sends a
prompt or proves Linux behavior.
