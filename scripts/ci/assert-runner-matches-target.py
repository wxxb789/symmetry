#!/usr/bin/env python3
"""Fail closed unless the runner matches the declared pinned-release target triple.

One implementation of the "this runner can execute this triple" guard used by both
platform steps of `.github/actions/prepare-pi-release`: the step runs it before the
download, so a mis-parameterised call cannot silently prepare a foreign-arch
release. The guard never consults `RUNNER_OS`/`RUNNER_ARCH`; it compares the
triple's own architecture and system fields against the runner's real values
(`uname -s`/`uname -m` on POSIX callers, `platform.system()`/`platform.machine()`
elsewhere, both overridable with `PI_RUNNER_SYSTEM`/`PI_RUNNER_MACHINE` or the
`--system`/`--machine` flags so the derivation is testable).

Statuses: 0 match, 1 mismatch, 2 usage error or malformed triple.
"""

import argparse
import os
import platform
import sys

ARCHITECTURES = {
    "x86_64": "x86_64",
    "amd64": "x86_64",
    "x64": "x86_64",
    "aarch64": "aarch64",
    "arm64": "aarch64",
    "i686": "i686",
    "i386": "i686",
    "x86": "i686",
}

# System tokens that may appear in the middle of a triple, mapped to the value the
# runner reports.
SYSTEMS = {
    "linux": "linux",
    "gnu": None,  # environment suffix, not a system
    "windows": "windows",
    "win32": "windows",
    "darwin": "darwin",
    "macos": "darwin",
    "freebsd": "freebsd",
    "netbsd": "netbsd",
    "openbsd": "openbsd",
    "none": None,  # bare-metal style suffix, not a system
    "unknown": None,
    "pc": None,
    "w64": None,
}

SYSTEM_ALIASES = {
    "linux": "linux",
    "windows": "windows",
    "darwin": "darwin",
    "macos": "darwin",
    "freebsd": "freebsd",
    "netbsd": "netbsd",
    "openbsd": "openbsd",
}


class TargetError(Exception):
    """The triple is not a shape this guard can reason about."""


def normalise_architecture(value: str) -> str | None:
    return ARCHITECTURES.get(value.strip().lower())


def normalise_system(value: str) -> str | None:
    value = value.strip().lower()
    # Git-Bash/MSYS report names such as `mingw64_nt-10.0`.
    if value.startswith(("mingw", "msys", "cygwin")):
        return "windows"
    return SYSTEM_ALIASES.get(value)


def parse_target(triple: str) -> tuple[str, str]:
    """Return (architecture, system) derived from `arch-vendor-system[-env]`."""
    fields = triple.strip().split("-")
    if len(fields) < 3 or not all(fields):
        raise TargetError(f"expected at least three hyphen-separated fields: {triple!r}")
    architecture = normalise_architecture(fields[0])
    if architecture is None:
        raise TargetError(f"unrecognised architecture field {fields[0]!r} in {triple!r}")
    systems = []
    for field in fields[1:]:
        # Vendor and environment tokens (`pc`, `unknown`, `gnu`, `musl`, ...) carry no
        # system information; anything else that is not a known system is ignored as
        # vendor/environment noise, so the guard only fails closed on a real conflict
        # or on a triple with no system field at all.
        mapped = SYSTEMS.get(field.strip().lower())
        if mapped:
            systems.append(mapped)
    if not systems:
        raise TargetError(f"no system field (for example linux or windows) in the middle of {triple!r}")
    if len(set(systems)) > 1:
        raise TargetError(f"conflicting system fields {sorted(set(systems))} in {triple!r}")
    return architecture, systems[0]


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--target", required=True, metavar="TRIPLE")
    parser.add_argument(
        "--system",
        default=os.environ.get("PI_RUNNER_SYSTEM") or platform.system(),
        help="runner system (default: PI_RUNNER_SYSTEM, else platform.system(), i.e. uname -s)",
    )
    parser.add_argument(
        "--machine",
        default=os.environ.get("PI_RUNNER_MACHINE") or platform.machine(),
        help="runner machine (default: PI_RUNNER_MACHINE, else platform.machine(), i.e. uname -m)",
    )
    args = parser.parse_args(argv)

    try:
        target_architecture, target_system = parse_target(args.target)
    except TargetError as error:
        print(f"malformed pinned Pi target: {error}", file=sys.stderr)
        return 2

    runner_system = normalise_system(args.system)
    runner_architecture = normalise_architecture(args.machine)
    mismatch = []
    if runner_system is None:
        mismatch.append(f"system {args.system!r} is not one this guard recognises")
    elif runner_system != target_system:
        mismatch.append(f"system {args.system!r} != {target_system!r}")
    if runner_architecture is None:
        mismatch.append(f"machine {args.machine!r} is not one this guard recognises")
    elif runner_architecture != target_architecture:
        mismatch.append(f"machine {args.machine!r} != {target_architecture!r}")
    if mismatch:
        print(
            f"pinned Pi target {args.target} requires architecture={target_architecture} "
            f"system={target_system}; {', '.join(mismatch)}",
            file=sys.stderr,
        )
        print(
            f"runner reported uname -s={args.system!r} uname -m={args.machine!r} "
            f"(system={runner_system!r}, machine={runner_architecture!r})",
            file=sys.stderr,
        )
        return 1
    print(f"verified runner matches pinned Pi target {args.target}: uname -s={args.system!r} uname -m={args.machine!r}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
