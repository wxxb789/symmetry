# 0006k - Evidence-bounded OmO native capability

Establish the supported capability range for the pinned OmO
(`oh-my-openagent`) `5.1.68` native binary, including repository work,
cancellation, retained resume, compatible fresh handoff, provider access where
separately verified, and exact session-directory provenance. Every handoff
creates a new native session and every effect remains bound to its durable
control identity.

OmO Native runs on the senpi engine, a Pi-derived fork. Its `omo --mode rpc`
surface is the primary integration target; the `omo app-server` Codex protocol
and the OpenCode-plugin edition are not assumed to be equivalent. The adapter
must probe the exact installed binary and protocol rather than inheriting Pi
capability claims. Owner directive 2026-11-11: until the first prototype and
release, track the latest stable client rather than freezing an older release.

Completion requires exact-binary Linux and Windows evidence for each promoted
operation, restart and lost-acknowledgement recovery, no duplicate effects,
artifact and Subject integrity, usage classification, and independent review of
the final revision. An unknown or incompatible protocol version remains
fail-closed and cannot advertise the affected operation.

This goal starts only when both Linux and Windows native runners, the pinned
binary, approved credentials, provider identity, repository, and permitted spend
are available; the 12-hour bound starts after all prerequisites are ready. It
depends on 0006a-0006c and produces one independently mergeable PR.

Owner decision 2026-11-14 (resolved prerequisites; these supersede the `5.1.68`
pin above and start the goal's 12-hour bound):

- **The pin advances to OmO `5.1.69`**, the latest stable client per the
  2026-11-11 directive, taken from the upstream GitHub release `v5.1.69` of
  `code-yeongyu/oh-my-openagent` (the `omo update` path returns 403 on this
  network, so the release asset is the channel). The exact installed binaries
  are `C:\Users\lhan\.local\bin\omo.exe` (`omo 5.1.69 (engine: senpi
  2026.11.10-2)`, 202,352,128 bytes, sha256
  `04338dd63b24c53156492e1a0777d996034416c4131a26e1ba48eb07b5429464`) on Windows
  and `/root/.local/bin/omo` inside the Linux surface below (`omo 5.1.69 (engine:
  senpi 2026.11.10-2; scheme nodef)`, 211,998,896 bytes, sha256
  `3a74aba613cf02e740073b302171c0fb9d9155f5ff6069f91bea23972ffa1444`). Every
  observation names that blob's path, size and digest, and states the platform
  that produced it. `5.1.68` and any other version's observations are historical
  only and cannot satisfy this goal.
- **The Linux surface is `wsl.exe -d Ubuntu-24.04`** (Ubuntu 24.04.5 LTS;
  Go 1.27.0, `python3` 3.12, `tmux` 3.4, `git` 2.43, `curl` and a C toolchain
  present). The host's default WSL distribution is an Alpine image without a
  Python toolchain and is not the Linux surface. The Pi/OpenCode Linux v2
  owner-path blocker (installed A/B systemd runtime, delegated cgroup-v2 parent,
  cross-UID `pidfd_getfd`/`fdinfo` inspection) does not govern this goal: the
  owner route is selected by harness kind, and a distinct OmO kind takes the
  ordinary Linux process-launch path. Each Linux receipt states the kernel and
  distribution that produced it; Windows observations come from this host.
- **The credentialed provider is the operator's Tencent LiteLLM relay**, model
  `tencent-litellm/deepseek-v4.1-flash-fast`, base URL
  `https://litellm-proxy-hk.graydune-4a6be1e2.eastasia.azurecontainerapps.io/v1`
  (verified reachable from this host and from the Linux surface). The working
  credential is the one carried by the harness's own configuration
  (`~/.omp/agent/models.yml`, provider `tencent-litellm`); this host's
  `TENCENT_GLM_API_KEY` value is a different 64-character key that the relay
  rejects by format (verified 2026-11-14: `401 … expected to start with 'sk-'`),
  so it must not be sent. Credential locality is part of what this goal proves:
  the value never appears in the repository, a receipt, a log line, a request
  payload, an environment dump or durable state, and the relay base URL is
  discovered from the harness configuration that already uses this key rather
  than guessed. A dummy key, a loopback gateway, model narration or a local
  journal test does not satisfy this goal; where the relay cannot be reached from
  the transport under test, that class is recorded as **unavailable** with its
  blocker.
- **The only authorized remote target is the private repository
  `wxxb789/symmetry-0006h-provider-fixture`**, reused from 0006h as this goal's
  repository resource; permitted spend is zero and no other repository, ref,
  release or package may be created or written. Where the repository clause needs
  a remote operation the Linux surface cannot perform, that class is recorded as
  blocked with its reason instead of improvising credential plumbing.
