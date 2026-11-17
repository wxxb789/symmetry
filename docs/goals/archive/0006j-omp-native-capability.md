# 0006j - Evidence-bounded Oh My Pi native capability

Establish the supported capability range for the pinned Oh My Pi (OMP) `18.18.7`
binary, including repository work, cancellation, retained resume, compatible
fresh handoff, provider access where separately verified, and exact
session-directory provenance. Every handoff creates a new native session and
every effect remains bound to its durable control identity.

OMP is a separate harness, not an alias for Pi. Its `omp --mode rpc` transport
is a custom newline-delimited JSON protocol with a `ready` frame, protocol
version negotiation, `prompt_result`, and `session_settled`; it must not inherit
Pi capability claims or decoder assumptions. Owner directive 2026-11-11: until
the first prototype and release, track the latest stable client rather than
freezing an older release.

Completion requires exact-binary Linux and Windows evidence for each promoted
operation, restart and lost-acknowledgement recovery, no duplicate effects,
artifact and Subject integrity, usage classification, and independent review of
the final revision. An unknown or incompatible protocol version remains
fail-closed and cannot advertise the affected operation.

This goal starts only when both Linux and Windows native runners, the pinned
binary, approved credentials, provider identity, repository, and permitted spend
are available; the 12-hour bound starts after all prerequisites are ready. It
depends on 0006a-0006c and produces one independently mergeable PR.

Owner decision 2026-11-14 (resolved prerequisites; these supersede the `18.18.7`
pin above and start the goal's 12-hour bound):

- **The pin advances to OMP `18.20.0`**, the latest stable client per the
  2026-11-11 directive. The exact installed binaries are
  `C:\Users\lhan\AppData\Local\mise\installs\oh-my-pi\18.20.0\omp.exe`
  (`omp/18.20.0`, 251,502,592 bytes, sha256
  `1b6c7bda8241bc7d948495539f90ec47e6f30d1fec72b6ee097a67a1fc8d8e48`) on Windows
  and `/root/.local/bin/omp` inside the Linux surface below (`omp/18.20.0`,
  287,484,080 bytes, sha256
  `c8694be8c6e6a1f45e3636dd8a8e9eaa78878ad629a89000b403ec5e456d3f89`). Every
  observation names that blob's path, size and digest, and states the platform
  that produced it. `18.18.7` and any other version's observations are
  historical only and cannot satisfy this goal.
- **The Linux surface is `wsl.exe -d Ubuntu-24.04`** (Ubuntu 24.04.5 LTS;
  Go 1.27.0, `python3` 3.12, `tmux` 3.4, `git` 2.43, `curl` and a C toolchain
  present). The host's default WSL distribution is an Alpine image without a
  Python toolchain and is not the Linux surface. The Pi/OpenCode Linux v2
  owner-path blocker (installed A/B systemd runtime, delegated cgroup-v2 parent,
  cross-UID `pidfd_getfd`/`fdinfo` inspection) does not govern this goal: the
  owner route is selected by harness kind, and a distinct OMP kind takes the
  ordinary Linux process-launch path. Each Linux receipt states the kernel and
  distribution that produced it; Windows observations come from this host.
- **The credentialed provider is the operator's Tencent LiteLLM relay**, model
  `tencent-litellm/deepseek-v4.1-flash-fast`, with the credential held in the
  environment variable `TENCENT_GLM_API_KEY`. Credential locality is part of
  what this goal proves: the value never appears in the repository, a receipt, a
  log line, a request payload, an environment dump or durable state, and the
  relay base URL is discovered from the harness configuration that already uses
  this key rather than guessed. A dummy key, a loopback gateway, model narration
  or a local journal test does not satisfy this goal; where the relay cannot be
  reached from the transport under test, that class is recorded as
  **unavailable** with its blocker.
- **The only authorized remote target is the private repository
  `wxxb789/symmetry-0006h-provider-fixture`**, reused from 0006h as this goal's
  repository resource; permitted spend is zero and no other repository, ref,
  release or package may be created or written. Where the repository clause needs
  a remote operation the Linux surface cannot perform, that class is recorded as
  blocked with its reason instead of improvising credential plumbing.
