# Goal 0006b local surface QA

Date: 2026-09-29

## Scope and evidence boundary

This is read-only/local surface validation of the Goal 0006b working tree, except for this report and build artifacts in unique temporary directories. I did not run the full Windows tests, production witness tests, or Linux race suite because the parent task was already running them. I did not start, stop, enable, install, clean, or otherwise change a service, journal, installed config, user credential, Docker workload, or Git/index state. No raw installed config or journal was printed or copied.

The inspected guidance was `AGENTS.md`, `daemon/AGENTS.md`, `docs/goals/0006b-crash-safe-process-containment.md`, `docs/evidence/goal-0006b-convergence-goal-2026-09-29.md`, `docs/evidence/goal-0006b-daemon-test-inventory-2026-09-29.md`, and `docs/evidence/goal-0006b-fixture-inventory-2026-09-29.md`.

The subject was branch `codex/goal-0006b-crash-safe-process-containment`, HEAD `d14debbf6ae3ded09e5013ef90952d347c01474d`. Immediately before this report was created, `git status --porcelain=v1` had 100 entries: 64 tracked changes and 36 untracked entries; the index had zero staged paths. The parent task was concurrently active, so the working tree was neither clean nor frozen. These commands therefore validate the observed working-tree source, not HEAD alone and not an acceptance-eligible exact commit. They cannot bind produced bytes or runtime evidence to the named HEAD.

## Build and vet evidence

All successful Go checks used Go 1.27.0 and bounded package parallelism. Outputs were written only below the listed unique temporary directories.

### Windows amd64

Host toolchain: `go version go1.27.0 windows/amd64`.

From `daemon/`:

| Command | Exit | Observation |
| --- | ---: | --- |
| `GOMAXPROCS=2 go vet -p=2 ./...` | 0 | All daemon packages vetted on Windows. |
| `GOMAXPROCS=2 go build -p=2 -o /tmp/symmetry-0006b-win-co2DmmPj/symmetry-daemon.exe ./cmd/symmetry-daemon` | 0 | Produced a 19,577,856-byte executable. |

### Linux amd64 Go surfaces

Distro `symmetry-race` reported kernel `6.18.40.1-microsoft-standard-WSL2`, `x86_64`. The actual versioned tool is `/opt/go/bin/go` and reports `go version go1.27.0 linux/amd64`; `/opt/go/bin/go1.27` does not exist.

From `/mnt/q/repos/symmetry/daemon`:

| Command | Exit | Observation |
| --- | ---: | --- |
| `GOMAXPROCS=2 /opt/go/bin/go vet -p=2 ./...` | 0 | All daemon packages vetted on Linux. |
| `GOMAXPROCS=2 /opt/go/bin/go build -p=2 -o /tmp/symmetry-0006b-linux-XXCicOOn/symmetry-daemon ./cmd/symmetry-daemon` | 0 | Produced a 25,169,457-byte executable. |
| `GOMAXPROCS=2 /opt/go/bin/go build -p=2 -o /tmp/symmetry-0006b-linux-XXCicOOn/symmetry-linux-launcher ./cmd/symmetry-linux-launcher` | 0 | Produced a 5,208,699-byte executable. |
| `GOMAXPROCS=2 /opt/go/bin/go build -p=2 -o /tmp/symmetry-0006b-linux-extra-XXDOhjDd/symmetry-linux-clone-bootstrap ./cmd/symmetry-linux-clone-bootstrap` | 0 | Produced a 5,075,463-byte executable. |

### Linux amd64 C surfaces

Ubuntu-24.04 supplied `/usr/bin/cc`. From `/mnt/q/repos/symmetry`:

| Command | Exit | Observation |
| --- | ---: | --- |
| `cc -O2 -static -fno-pie -no-pie -Wl,-z,relro,-z,now -Wl,-z,noexecstack -o /tmp/symmetry-0006b-c-01edM44n/symmetry-linux-gate daemon/native/linux-gate/c-gate/gate.c` | 0 | Produced an 808,568-byte static gate. |
| `cc -O2 -static -fno-pie -no-pie -Wl,-z,relro,-z,now -Wl,-z,noexecstack -o /tmp/symmetry-0006b-c-01edM44n/symmetry-linux-launcher daemon/cmd/symmetry-linux-launcher/c-wrapper/wrapper.c` | 0 | Produced a 794,968-byte static wrapper. |

A preliminary Linux invocation followed the task's literal `/opt/go/bin/go1.27` path. `version`, `vet`, and both attempted builds exited 127, so their dependent CLI attempts also exited 127 and produced no artifacts. Inspection then established that `/opt/go/bin/go` is the installed Go 1.27.0 binary, and the successful commands above used it. During the corrected smoke script, an accidental suppressed `go run /mnt/q/repos/symmetry/.omo/nonexistent.go` exited nonzero before the temporary fixture copy; it produced no source or requested build artifact and was not used as evidence.

## Fresh-binary CLI smoke

These were real executions of the freshly built binaries. They establish parser/configuration rejection and unsupported-host behavior only. They are not synthetic positive owner witnesses and do not establish A/B containment, native harness support, a Control claim, or Goal completion.

### Windows executable

| Command | Exit | Observable result |
| --- | ---: | --- |
| `symmetry-daemon.exe` | 2 | Stderr: `-config is required`. |
| `symmetry-daemon.exe -print-state-dir -config ../docker/daemon-config.json` | 1 | Zero stdout; rejected because StateDir printing is Linux-only. |
| `env -u SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WITNESS symmetry-daemon.exe -linux-v2-native-owner-witness` | 1 | Zero stdout; rejected because explicit production witness opt-in is required. |

### Linux executable

The valid configuration was the existing credential-free `docker/daemon-config.json`. The unsafe variant was copied to the unique temporary output directory and changed only there to use `state_dir: ../escape`.

| Command | Exit | Observable result |
| --- | ---: | --- |
| `symmetry-daemon -print-state-dir -config /mnt/q/repos/symmetry/docker/daemon-config.json` | 0 | Printed only `/var/lib/symmetry`. |
| `if [ -e /var/lib/symmetry ]; then stat ...; else printf 'STATE_DIR_AFTER=absent'; fi` | 0 | Reported `STATE_DIR_AFTER=absent`, confirming the print command did not create the StateDir. |
| `symmetry-daemon -print-state-dir -config /tmp/symmetry-0006b-linux-XXCicOOn/unsafe.json` | 1 | Zero stdout; rejected non-clean dot/parent components. |
| `env -u SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WITNESS symmetry-daemon -linux-v2-native-owner-witness` | 1 | Zero stdout; rejected absent production witness opt-in. |
| `SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WITNESS=1 symmetry-daemon -linux-v2-native-owner-witness -config /nonexistent -request /nonexistent unexpected` | 1 | Zero stdout; strict argument rejection occurred before fixture or host access. |

No owner command, recovery command, native action, Control action, provider action, or service action was invoked.

## Docker and Compose prerequisites

All Docker checks were read-only; no image build, pull, container, network, volume, or Compose lifecycle command ran.

| Command | Exit | Observation |
| --- | ---: | --- |
| `docker version --format 'client={{.Client.Version}} server={{if .Server}}{{.Server.Version}}{{else}}unavailable{{end}}'` | 1 | Client 29.8.0 is installed; the Docker Desktop Linux engine named pipe was absent. |
| `docker compose version` | 0 | Docker Compose v5.5.1 is installed. |
| `docker compose -f compose.yaml config --quiet` | 0 | Development Compose interpolation/schema parsed without contacting the engine. |
| `env -u DATABASE_URL -u PHX_HOST -u SECRET_KEY_BASE -u SYMMETRY_ENROLLMENT_TOKEN -u SYMMETRY_OPERATOR_TOKEN -u SYMMETRY_TLS_CERT_FILE -u SYMMETRY_TLS_KEY_FILE docker compose -f compose.production.yaml config --quiet` | 1 | Correctly rejected missing required production database, host, secret/token, and TLS inputs. Values were not inspected. |

The referenced `docker/control.Dockerfile`, `docker/daemon.Dockerfile`, `docker/edge-nginx.conf`, and `docker/daemon-config.json` files were present. Rootless Compose refusal could not be exercised because no Docker engine was available. Even if runnable, the documented rootless surface is rejection-only E1 evidence, not qualified Linux v2 positive acceptance.

## WSL prerequisite observations

Only metadata, kernel knobs, filesystem type, executable presence, and `systemctl` state were queried. Installed config contents and journals were not read.

### `symmetry-race`

- Kernel: `6.18.40.1-microsoft-standard-WSL2`, amd64.
- PID 1 identified as WSL init, not systemd; `systemctl` was absent.
- `/sys/fs/cgroup/cgroup.kill` existed. The filesystem probe returned `UNKNOWN` in this minimal distro, so this observation alone does not establish a usable delegated cgroup-v2 owner hierarchy.
- `ptrace_scope` was `1`; the unprivileged-user-namespace sysctl path was unavailable.
- `/etc/symmetry/linux-owner.conf` and `/etc/symmetry/daemon.json` were absent.
- `cc`, `git`, and `unshare` were present; Python 3 and `findmnt` were absent.
- Read-only `systemctl is-active` attempts exited 127 because systemctl is absent. No installed-owner witness can run here.

### `Ubuntu-24.04`

- Kernel: `6.18.40.1-microsoft-standard-WSL2`, amd64; PID 1 was systemd.
- `systemctl is-system-running` reported `degraded` and exit 1.
- `/sys/fs/cgroup` reported `cgroup2fs`, and root `cgroup.kill` existed. These are necessary but not sufficient: no delegated A owner unit or exact A/B peer capability was available.
- `ptrace_scope` was `1`; the unprivileged-user-namespace sysctl path was unavailable.
- `/etc/symmetry/linux-owner.conf` existed as a root-owned regular file, mode 0644. Its contents were not read. `/etc/symmetry/daemon.json` was absent.
- `systemctl is-active symmetry-owner.service` and `symmetry-daemon.service` both reported `inactive` with exit 4; `is-enabled` reported `not-found` with exit 4. `systemctl show` likewise reported the owner unit `LoadState=not-found`. No service action was taken.
- Python 3, `cc`, `git`, `unshare`, and `findmnt` were present; Go was absent from `PATH`.

The observed WSL host was not provisioned as the required installed distinct-UID A/B environment. Presence of a cgroup-v2 filesystem, `cgroup.kill`, or an owner config file does not prove systemd delegation, exact service topology, B credentials, pidfd/fdinfo/socket duplication across the credential drop, App-owned RunJournal behavior, or physical-stop receipt issuance. The exact cross-UID peer probe and production denial witness were not attempted without the operator-provisioned fixture and authentic request.

## Credentials, claims, and acceptance matrix

The following variable names were checked for presence only; values were never read: `GITHUB_TOKEN`, `GH_TOKEN`, `OPENAI_API_KEY`, `SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WITNESS`, `SYMMETRY_LINUX_V2_OWNER_FIXTURE`, and `SYMMETRY_LINUX_V2_OWNER_REQUEST`. All were absent. No authentic Control-issued claim-scoped token, isolated Integration, approved A/B host, B HOME/credentials, or explicit paid-provider test environment was supplied.

| Surface | Local result | Qualification |
| --- | --- | --- |
| Windows Go vet/build | Pass | Working-tree compilation evidence only. |
| Windows CLI configuration/unsupported gates | Pass | Real local rejection evidence only. |
| Linux Go vet and production executable builds | Pass | Working-tree compilation evidence only. |
| Linux static C gate/wrapper builds | Pass | Working-tree compilation evidence only. |
| Linux StateDir and owner-witness CLI gates | Pass | Real local parser/config rejection evidence only. |
| Docker Compose static development config | Pass | Prerequisite syntax only. |
| Docker engine/rootless Compose refusal witness | Unavailable | Engine absent; no runtime evidence. |
| WSL2 unprovisioned/unsupported condition | Observed | Prerequisite gap, not the authentic denial witness. |
| Qualified Linux amd64 systemd A/B Pi positive | Unavailable | No installed units/A-B fixture, authentic claim token, Integration, or provider credentials. |
| Qualified Linux amd64 systemd A/B OpenCode positive | Unavailable | No installed units/A-B fixture, authentic claim, or approved native environment. |
| Qualified crash/replay/receipt/release/clear matrix | Not run here | Requires the protected installed fixture; parent-owned test activity was not duplicated. |
| Exact-commit acceptance package | Blocked | Dirty, concurrently changing tree cannot bind evidence to HEAD. |

## Conclusion

The observed working-tree source vets and builds on Windows and Linux with Go 1.27.0, all in bounded parallelism, and the Linux C launch surfaces compile statically. Fresh executables enforce the tested configuration, platform, witness opt-in, and strict-argument rejection gates. Docker/Compose and WSL observations identify real local prerequisite gaps without mutating installed state.

This is not Goal 0006b acceptance evidence. The dirty/unfrozen source, absent Docker engine, uninstalled/inactive WSL owner topology, missing distinct A/B fixture, and absent authentic Control/provider inputs leave qualified Linux Pi/OpenCode positive, WSL authentic denial, crash/replay, and exact-subject evidence unavailable. No local refusal or successful build is promoted to a native capability, Control, provider-action, or Goal-completion claim.
