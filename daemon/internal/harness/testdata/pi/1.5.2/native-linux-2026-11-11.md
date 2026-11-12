# pi 1.5.2 Linux native transport receipt

Date: 2026-11-11. Scope: Ubuntu 24.04 on WSL2 x86_64, native process/stdio/RPC
transport only. No prompt, model, repository, retained resume or capability
promotion is proved.

## Exact binary

- Release asset: `pi-linux-x64.tar.gz` from
  `https://github.com/earendil-works/pi/releases/download/v1.5.2/`
- Archive SHA-256:
  `811c6f2c9e7aaee252b5404252e556fb713aa209c1f7f924226aa77d4ef70cb5`
- `pi` SHA-256:
  `6a2ac1c5b0d08b91ce6095591c1960b469d0665c7fc5f32812960a4a17ca0c57`
- `pi --version`: `1.5.2`

## Result

Source revision `a04c4ab02d1a6a35aa682c5b845133c720d3be61` with the local
1.5.2 pin applied.

```text
SYMMETRY_PI_NATIVE_SMOKE=1 \
SYMMETRY_PI_NATIVE_SMOKE_EXECUTABLE=<absolute>/pi \
  go test ./internal/harness/pi -run '^TestNativeRPCOpenAndClose$' \
    -count=1 -v -timeout 90s

native Pi RPC smoke: platform=linux version=1.5.2
--- PASS: TestNativeRPCOpenAndClose (1.52s)
```

This proves launch, version gate, `get_state` correlation, event delivery and
bounded close. Provider-bridge, credentialed repository, cancellation and
retained-resume Linux evidence remain pending under Goal 0006f.
