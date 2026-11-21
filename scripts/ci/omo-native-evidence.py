#!/usr/bin/env python3
"""Characterize the pinned OmO executable directly; never promote adapter support.

Run only with an authorized local fixture and relay credentials supplied by the
operator's environment. Evidence roots are deliberately retained, including on
failure. The pinned executable's host path comes from SYMMETRY_OMO_BINARY; the
committed size and digest remain the authority. This is native-process evidence,
not daemon or Control evidence.
"""

import argparse
import configparser
import contextlib
import ctypes
import hashlib
import json
import os
import pathlib
import platform
import queue
import re
import shutil
import signal
import subprocess
import sys
import tempfile
import threading
import time
import urllib.parse
import uuid

# The executable's location is host-specific, so no path is committed: the operator
# names it in BINARY_ENVIRONMENT, and the exact size and digest below stay the
# authority. An unset variable, a missing file, or a different blob refuses the run.
BINARY_ENVIRONMENT = "SYMMETRY_OMO_BINARY"
PINS = {
    "Windows": {
        "size": 202352128,
        "sha256": "04338dd63b24c53156492e1a0777d996034416c4131a26e1ba48eb07b5429464",
    },
    "Linux": {
        "size": 211998896,
        "sha256": "3a74aba613cf02e740073b302171c0fb9d9155f5ff6069f91bea23972ffa1444",
    },
}
ORIGIN = "https://github.com/wxxb789/symmetry-0006h-provider-fixture.git"
SCENARIOS = ("metadata", "fresh_identity", "durable_replay", "process_restart", "fresh_handoff", "cancellation")
MAX_LINE = 1024 * 1024
MAX_RECORDS = 30000
MAX_BYTES = 48 * 1024 * 1024
TURN_TIMEOUT = 180
NAMESPACE = uuid.UUID("3c89db31-7c6f-4d86-bb97-6f3433859c26")
ARTIFACTS = {
    "original": ("omo-native-original.txt", b"OmO native durable evidence\n"),
    "resume": ("omo-native-resume.txt", b"OmO native retained-session evidence\n"),
    "handoff": ("omo-native-handoff.txt", b"OmO native fresh-handoff evidence\n"),
}


class WitnessError(Exception):
    """Only constant, nonsecret diagnostic messages belong in this exception."""


def require(condition, message):
    if not condition:
        raise WitnessError(message)


def strict_json(text):
    def object_pairs(pairs):
        value = {}
        for key, item in pairs:
            require(key not in value, "duplicate key in native JSON")
            value[key] = item
        return value

    def invalid_constant(_):
        raise WitnessError("non-JSON numeric constant in native JSON")

    return json.loads(text, object_pairs_hook=object_pairs, parse_constant=invalid_constant)


def digest(path):
    value = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            value.update(chunk)
    return value.hexdigest()


def bounded_read(path, limit=MAX_LINE):
    with path.open("rb") as stream:
        value = stream.read(limit + 1)
    require(len(value) <= limit, "local evidence file exceeds bound")
    return value


def same_path(left, right):
    return os.path.normcase(os.path.realpath(left)) == os.path.normcase(os.path.realpath(right))


def contained(path, directory):
    try:
        pathlib.Path(path).resolve().relative_to(pathlib.Path(directory).resolve())
        return True
    except (ValueError, OSError):
        return False


def private_root(root):
    if os.name != "nt":
        root.chmod(0o700)
        return "owner-only POSIX directory mode 0700"
    from ctypes import wintypes

    security = ctypes.WinDLL("advapi32", use_last_error=True)
    kernel = ctypes.WinDLL("kernel32", use_last_error=True)
    security.ConvertStringSecurityDescriptorToSecurityDescriptorW.argtypes = [
        wintypes.LPCWSTR,
        wintypes.DWORD,
        ctypes.POINTER(ctypes.c_void_p),
        ctypes.c_void_p,
    ]
    security.SetFileSecurityW.argtypes = [wintypes.LPCWSTR, wintypes.DWORD, ctypes.c_void_p]
    kernel.LocalFree.argtypes = [ctypes.c_void_p]
    descriptor = ctypes.c_void_p()
    require(
        bool(
            security.ConvertStringSecurityDescriptorToSecurityDescriptorW(
                "D:P(A;OICI;FA;;;OW)(A;OICI;FA;;;SY)", 1, ctypes.byref(descriptor), None
            )
        ),
        "private Windows root security descriptor creation failed",
    )
    try:
        require(
            bool(security.SetFileSecurityW(str(root), 0x80000004, descriptor)),
            "private Windows root ACL application failed",
        )
    finally:
        kernel.LocalFree(descriptor)
    return "protected inheritable Windows owner-rights and SYSTEM DACL"


# A URL inside captured output can carry a foreign credential. The scan is
# deliberately narrow: it only redacts the authority, and it keeps the rest of
# the URL so the observation stays readable.
URL_TOKEN = re.compile(r"https?://[^\s\"'<>\\]+")


def redact_url_userinfo(text):
    """Replace the userinfo of every URL in `text` with a fixed marker.

    `urllib.parse.urlsplit` splits the authority at its *last* `@`, so a
    password that itself contains `@` is still recognised as userinfo and the
    rebuilt authority keeps only the host and port. The former
    `(https?://)[^\\s/@\"<>]+:[^\\s/@\"<>]+@` pattern stopped at the first `@`,
    which turned `https://alice:p@s:w0rd@control.internal/x` into
    `https://[REDACTED]@s:w0rd@control.internal/x` and leaked the password tail.
    """

    def redact(match):
        token = match.group(0)
        try:
            parsed = urllib.parse.urlsplit(token)
        except ValueError:
            # An authority the standard parser rejects is evidence of nothing.
            return "[REDACTED]"
        if not parsed.username and not parsed.password:
            return token
        authority = parsed.netloc.rpartition("@")[2]
        return urllib.parse.urlunsplit(
            (parsed.scheme, "[REDACTED]@" + authority, parsed.path, parsed.query, parsed.fragment)
        )

    return URL_TOKEN.sub(redact, text)


class Receipt:
    def __init__(self, secret):
        self.secret_forms = (
            tuple({secret, json.dumps(secret)[1:-1], urllib.parse.quote(secret, safe="")}) if secret else ()
        )
        self.lock = threading.Lock()
        self.failure = threading.Event()
        self.failure_reason = None
        self.size = 0
        self.value = {
            "schemaVersion": 1,
            "evidenceScope": "direct native RPC with a local authorized fixture",
            "testedProfile": {
                "tools": ["read", "write"],
                "permissionPreset": "workspace",
                "cancellation": "model streaming only; no child-tool cancellation",
            },
            "notProven": [
                "Symmetry daemon restart",
                "Control integration",
                "adapter capabilities",
                "bash/terminal child-tool cancellation",
                "arbitrary detached-tool containment",
            ],
            "capabilityPromotion": "none; operator acceptance required",
            "startedAt": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
            "classification": "unavailable",
            "activeScenario": "metadata",
            "scenarios": {name: {"classification": "unavailable", "reason": "not reached"} for name in SCENARIOS},
            "observations": [],
        }

    def sanitize(self, text):
        for secret in self.secret_forms:
            if secret:
                text = text.replace(secret, "[REDACTED]")
        # A foreign credential-bearing URL is never useful as evidence.
        return redact_url_userinfo(text)

    def check_raw(self, text):
        return not any(secret and secret in text for secret in self.secret_forms)

    def fail_capture(self, reason):
        with self.lock:
            if self.failure_reason is None:
                self.failure_reason = reason
        self.failure.set()

    def record(self, operation, classification="passed", **data):
        # Check before keeping ANY raw native output or exception in evidence.
        encoded = json.dumps(data, ensure_ascii=True)
        clean = self.check_raw(encoded)
        sanitized = self.sanitize(encoded)
        with self.lock:
            if len(self.value["observations"]) >= MAX_RECORDS or self.size + len(sanitized) > MAX_BYTES:
                self.failure_reason = self.failure_reason or "evidence record bound exceeded"
                self.failure.set()
                return None
            observation = {
                "index": len(self.value["observations"]),
                "elapsedMonotonic": time.monotonic(),
                "operation": operation,
                "classification": classification,
                **json.loads(sanitized),
            }
            self.value["observations"].append(observation)
            self.size += len(sanitized)
        if not clean:
            self.fail_capture("credential detected in captured output; redacted before persistence")
        return observation

    def check(self):
        if self.failure.is_set():
            raise WitnessError(self.failure_reason or "native capture failed")

    def save(self, out):
        encoded = json.dumps(self.value, indent=2, ensure_ascii=True)
        require(self.check_raw(encoded), "credential found in receipt serialization")
        encoded = self.sanitize(encoded)
        require(self.check_raw(encoded), "credential redaction failed")
        out.parent.mkdir(parents=True, exist_ok=True)
        with out.open("w", encoding="utf-8", newline="\n") as stream:
            stream.write(encoded + "\n")
        if os.name != "nt":
            out.chmod(0o600)


class WindowsJob:
    """Own only this suspended child and its descendants, including pipe reapers."""

    def __init__(self):
        from ctypes import wintypes

        class BasicLimit(ctypes.Structure):
            _fields_ = [
                ("PerProcessUserTimeLimit", ctypes.c_longlong),
                ("PerJobUserTimeLimit", ctypes.c_longlong),
                ("LimitFlags", wintypes.DWORD),
                ("MinimumWorkingSetSize", ctypes.c_size_t),
                ("MaximumWorkingSetSize", ctypes.c_size_t),
                ("ActiveProcessLimit", wintypes.DWORD),
                ("Affinity", ctypes.c_size_t),
                ("PriorityClass", wintypes.DWORD),
                ("SchedulingClass", wintypes.DWORD),
            ]

        class IoCounters(ctypes.Structure):
            _fields_ = [
                (name, ctypes.c_ulonglong)
                for name in (
                    "ReadOperationCount",
                    "WriteOperationCount",
                    "OtherOperationCount",
                    "ReadTransferCount",
                    "WriteTransferCount",
                    "OtherTransferCount",
                )
            ]

        class ExtendedLimit(ctypes.Structure):
            _fields_ = [
                ("BasicLimitInformation", BasicLimit),
                ("IoInfo", IoCounters),
                ("ProcessMemoryLimit", ctypes.c_size_t),
                ("JobMemoryLimit", ctypes.c_size_t),
                ("PeakProcessMemoryUsed", ctypes.c_size_t),
                ("PeakJobMemoryUsed", ctypes.c_size_t),
            ]

        self.kernel = ctypes.WinDLL("kernel32", use_last_error=True)
        self.kernel.CreateJobObjectW.argtypes = [ctypes.c_void_p, wintypes.LPCWSTR]
        self.kernel.CreateJobObjectW.restype = wintypes.HANDLE
        self.kernel.SetInformationJobObject.argtypes = [wintypes.HANDLE, ctypes.c_int, ctypes.c_void_p, wintypes.DWORD]
        self.kernel.AssignProcessToJobObject.argtypes = [wintypes.HANDLE, wintypes.HANDLE]
        self.kernel.TerminateJobObject.argtypes = [wintypes.HANDLE, wintypes.UINT]
        self.kernel.CloseHandle.argtypes = [wintypes.HANDLE]
        self.handle = self.kernel.CreateJobObjectW(None, None)
        require(bool(self.handle), "Windows job creation failed")
        limits = ExtendedLimit()
        limits.BasicLimitInformation.LimitFlags = 0x2000  # JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
        if not self.kernel.SetInformationJobObject(self.handle, 9, ctypes.byref(limits), ctypes.sizeof(limits)):
            self.close()
            raise WitnessError("Windows descendant cleanup configuration failed")

    def attach_and_resume(self, process):
        from ctypes import wintypes

        require(
            bool(self.kernel.AssignProcessToJobObject(self.handle, int(process._handle))),
            "Windows child job assignment failed",
        )

        class ThreadEntry(ctypes.Structure):
            _fields_ = [
                ("dwSize", wintypes.DWORD),
                ("cntUsage", wintypes.DWORD),
                ("th32ThreadID", wintypes.DWORD),
                ("th32OwnerProcessID", wintypes.DWORD),
                ("tpBasePri", wintypes.LONG),
                ("tpDeltaPri", wintypes.LONG),
                ("dwFlags", wintypes.DWORD),
            ]

        self.kernel.CreateToolhelp32Snapshot.argtypes = [wintypes.DWORD, wintypes.DWORD]
        self.kernel.CreateToolhelp32Snapshot.restype = wintypes.HANDLE
        self.kernel.Thread32First.argtypes = [wintypes.HANDLE, ctypes.POINTER(ThreadEntry)]
        self.kernel.Thread32Next.argtypes = [wintypes.HANDLE, ctypes.POINTER(ThreadEntry)]
        self.kernel.OpenThread.argtypes = [wintypes.DWORD, wintypes.BOOL, wintypes.DWORD]
        self.kernel.OpenThread.restype = wintypes.HANDLE
        self.kernel.ResumeThread.argtypes = [wintypes.HANDLE]
        self.kernel.ResumeThread.restype = wintypes.DWORD
        snapshot = self.kernel.CreateToolhelp32Snapshot(4, 0)  # TH32CS_SNAPTHREAD
        require(snapshot not in (None, ctypes.c_void_p(-1).value), "Windows thread snapshot failed")
        resumed = []
        try:
            entry = ThreadEntry()
            entry.dwSize = ctypes.sizeof(entry)
            present = self.kernel.Thread32First(snapshot, ctypes.byref(entry))
            while present:
                if entry.th32OwnerProcessID == process.pid:
                    thread = self.kernel.OpenThread(2, False, entry.th32ThreadID)
                    require(bool(thread), "Windows owned thread open failed")
                    try:
                        previous_count = self.kernel.ResumeThread(thread)
                        require(previous_count != 0xFFFFFFFF, "Windows owned thread resume failed")
                        resumed.append({"threadId": entry.th32ThreadID, "previousSuspendCount": previous_count})
                    finally:
                        self.kernel.CloseHandle(thread)
                present = self.kernel.Thread32Next(snapshot, ctypes.byref(entry))
        finally:
            self.kernel.CloseHandle(snapshot)
        require(
            any(item["previousSuspendCount"] == 1 for item in resumed),
            "Windows suspended child primary thread was not resumed",
        )
        return resumed

    def terminate(self):
        require(bool(self.kernel.TerminateJobObject(self.handle, 1)), "Windows job termination failed")

    def close(self):
        if self.handle:
            self.kernel.CloseHandle(self.handle)
            self.handle = None


class NativeProcess:
    def __init__(self, receipt, name, argv, cwd, env, rpc=True):
        self.receipt, self.name, self.rpc = receipt, name, rpc
        self.queue = queue.Queue(maxsize=MAX_RECORDS)
        self.frames = []
        self.stdout = []
        self.discarded = set()
        self.process = None
        self.job = None
        self.threads = []
        self.closed = False
        receipt.record(
            "launch",
            process=name,
            argv=argv,
            cwd=str(cwd),
            environmentKeys=sorted(env),
            shell=False,
            cleanup="Windows owned job" if os.name == "nt" else "owned process group",
        )
        try:
            self.job = WindowsJob() if os.name == "nt" else None
            self.process = subprocess.Popen(
                argv,
                cwd=cwd,
                env=env,
                stdin=subprocess.PIPE,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                shell=False,
                creationflags=0x4 if os.name == "nt" else 0,  # CREATE_SUSPENDED
                start_new_session=os.name != "nt",
            )
            if self.job:
                resumed_threads = self.job.attach_and_resume(self.process)
                receipt.record(
                    "windows_owned_threads_resumed", process=name, pid=self.process.pid, threads=resumed_threads
                )
            receipt.record("process_identity", process=name, pid=self.process.pid)
            for label, stream in (("stdout", self.process.stdout), ("stderr", self.process.stderr)):
                thread = threading.Thread(target=self.consume, args=(label, stream), daemon=True)
                thread.start()
                self.threads.append(thread)
        except Exception:
            if self.process:
                self.process.kill()
                self.process.wait(timeout=10)
                for stream in (self.process.stdin, self.process.stdout, self.process.stderr):
                    stream.close()
            if self.job:
                self.job.close()
            raise

    def consume(self, label, stream):
        try:
            while True:
                line = stream.readline(MAX_LINE + 1)
                if not line:
                    return
                if len(line) > MAX_LINE:
                    self.receipt.fail_capture("native output line exceeds bound")
                    continue
                try:
                    text = line.decode("utf-8").rstrip("\r\n")
                except UnicodeDecodeError:
                    self.receipt.fail_capture("native " + label + " output is not valid UTF-8")
                    self.receipt.record(
                        "native_output", "failed", process=self.name, stream=label, error="invalid UTF-8"
                    )
                    continue
                if not self.receipt.check_raw(text):
                    self.receipt.fail_capture("credential detected in native output; redacted before persistence")
                text = self.receipt.sanitize(text)
                frame = None
                if label == "stdout" and self.rpc:
                    try:
                        frame = strict_json(text)
                        require(
                            isinstance(frame, dict) and isinstance(frame.get("type"), str),
                            "malformed native frame identity",
                        )
                    except (ValueError, WitnessError):
                        self.receipt.record("native_output", "failed", process=self.name, stream=label, raw=text)
                        self.receipt.fail_capture("invalid native JSON frame")
                        continue
                observation = self.receipt.record(
                    "native_output", process=self.name, stream=label, raw=text, frame=frame
                )
                if observation is None:
                    continue
                if label == "stdout":
                    if not self.rpc:
                        self.stdout.append(text)
                    else:
                        try:
                            self.queue.put_nowait((observation["index"], frame))
                        except queue.Full:
                            self.receipt.fail_capture("native reader queue exceeds bound")
        except Exception:
            self.receipt.fail_capture("native output reader failed")

    def send(self, command, **fields):
        self.receipt.check()
        value = {"id": str(uuid.uuid4()), "type": command, **fields}
        encoded = (json.dumps(value, ensure_ascii=True) + "\n").encode("utf-8")
        require(len(encoded) <= 4096, "native request exceeds single-pipe-buffer bound")
        self.receipt.record("request", process=self.name, argvCommand=command, request=value)
        require(self.process.poll() is None, "native process exited before request")
        # Commands are small and sent serially while the independent readers drain.
        sent = self.process.stdin.write(encoded)
        require(sent == len(encoded), "incomplete native request write")
        self.process.stdin.flush()
        return value["id"]

    def next(self, deadline):
        while time.monotonic() < deadline:
            self.receipt.check()
            try:
                index, frame = self.queue.get(timeout=min(0.2, max(0.001, deadline - time.monotonic())))
            except queue.Empty:
                require(self.process.poll() is None, "native process exited while awaiting frame")
                continue
            self.frames.append((index, frame))
            if frame["type"] == "extension_ui_request":
                require(
                    frame.get("method") in ("setWidget", "setStatus", "notify"),
                    "actionable native UI request refused unattended evidence",
                )
            if frame["type"] == "response":
                require(
                    isinstance(frame.get("id"), str)
                    and isinstance(frame.get("command"), str)
                    and isinstance(frame.get("success"), bool),
                    "malformed native response identity",
                )
                if frame["id"] in self.discarded:
                    self.receipt.record(
                        "discard_prompt_ack",
                        process=self.name,
                        transportId=frame["id"],
                        nativeObservation=index,
                        ownership="not delivered to witness request owner",
                    )
                    self.discarded.remove(frame["id"])
                    continue
            if frame["type"] == "tool_execution_end":
                require(frame.get("isError") is not True, "native tool effect failed")
            if frame["type"] == "message_end" and frame.get("message", {}).get("role") == "assistant":
                require(frame["message"].get("stopReason") != "error", "native provider turn failed")
            return index, frame
        raise WitnessError("native observation deadline exceeded")

    def response(self, request_id, command, timeout=60, expected_success=True):
        deadline = time.monotonic() + timeout
        while True:
            _, frame = self.next(deadline)
            if frame["type"] == "response":
                require(
                    frame["id"] == request_id and frame["command"] == command, "unexpected native response identity"
                )
                require(frame["success"] is expected_success, "native command refused or unexpectedly accepted")
                return frame

    def request(self, command, timeout=60, expected_success=True, **fields):
        return self.response(self.send(command, **fields), command, timeout, expected_success).get("data", {})

    def idle_event(self, start, timeout=TURN_TIMEOUT):
        deadline = time.monotonic() + timeout
        cursor, idle = start, False
        while not idle:
            while cursor < len(self.frames) and not idle:
                idle = self.frames[cursor][1]["type"] == "agent_idle"
                cursor += 1
            if not idle:
                self.next(deadline)
        kinds = [frame["type"] for _, frame in self.frames[start:]]
        require("agent_start" in kinds, "no native agent_start observed")
        require("agent_end" in kinds and "agent_settled" in kinds, "native settlement events missing")
        require(
            kinds.index("agent_start")
            < kinds.index("agent_end")
            < kinds.index("agent_settled")
            < kinds.index("agent_idle"),
            "native settlement order invalid",
        )

    def finish_cli(self):
        try:
            self.process.wait(timeout=120)
        except subprocess.TimeoutExpired:
            raise WitnessError("native CLI metadata deadline exceeded") from None
        self.stop()
        self.receipt.check()
        require(self.process.returncode == 0, "native CLI metadata command failed")
        return "\n".join(self.stdout)

    def stop(self):
        if self.closed:
            return
        self.closed = True
        error = None
        try:
            if self.job:
                self.job.terminate()
            else:
                with contextlib.suppress(ProcessLookupError):
                    os.killpg(self.process.pid, signal.SIGTERM)
            try:
                self.process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                if self.job:
                    self.job.terminate()
                else:
                    os.killpg(self.process.pid, signal.SIGKILL)
                self.process.wait(timeout=10)
            if not self.job:
                with contextlib.suppress(ProcessLookupError):
                    os.killpg(self.process.pid, signal.SIGKILL)
        except Exception:
            error = "native process cleanup failed"
        finally:
            if self.job:
                self.job.close()
            if self.process.poll() is None:
                try:
                    self.process.kill()
                    self.process.wait(timeout=10)
                except Exception:
                    error = "owned native process did not exit after cleanup"
            if self.process.stdin:
                self.process.stdin.close()
            for thread in self.threads:
                thread.join(timeout=10)
            if any(thread.is_alive() for thread in self.threads):
                error = "native pipe reader did not drain after owned descendant termination"
            else:
                self.process.stdout.close()
                self.process.stderr.close()
            self.receipt.record(
                "process_stop",
                "failed" if error else "passed",
                process=self.name,
                pid=self.process.pid,
                exitCode=self.process.poll(),
                readersDrained=not any(thread.is_alive() for thread in self.threads),
            )
        if error:
            raise WitnessError(error)


def isolated_environment(root):
    # Never inherit provider credentials, proxy settings, git settings, NODE_OPTIONS,
    # certificate overrides, or the real user's agent/config directories.
    allowed = {"SYSTEMROOT", "WINDIR", "COMSPEC", "PATHEXT"} if os.name == "nt" else set()
    env = {key: value for key, value in os.environ.items() if key.upper() in allowed}
    home, temp = root / "home", root / "temp"
    env.update(
        HOME=str(home),
        USERPROFILE=str(home),
        APPDATA=str(home / "AppData/Roaming"),
        LOCALAPPDATA=str(home / "AppData/Local"),
        XDG_CONFIG_HOME=str(home / ".config"),
        XDG_CACHE_HOME=str(home / ".cache"),
        XDG_DATA_HOME=str(home / ".local/share"),
        TEMP=str(temp),
        TMP=str(temp),
        TMPDIR=str(temp),
        OMO_CODING_AGENT_DIR=str(root / "agent"),
        DO_NOT_TRACK="1",
        OMO_DISABLE_POSTHOG="1",
        LANG="C.UTF-8",
        LC_ALL="C.UTF-8",
    )
    if os.name == "nt":
        env["PATH"] = str(pathlib.Path(env.get("SystemRoot", env.get("SYSTEMROOT", "C:/Windows"))) / "System32")
    else:
        env["PATH"] = "/usr/bin:/bin"
    for path in (
        home,
        temp,
        root / "agent",
        root / "sessions",
        home / "AppData/Roaming",
        home / "AppData/Local",
        home / ".config",
        home / ".cache",
        home / ".local/share",
    ):
        path.mkdir(mode=0o700, parents=True, exist_ok=True)
    return env


def copy_fixture(repository, workspace, receipt):
    require(repository.is_dir() and not repository.is_symlink(), "fixture directory invalid")
    git_dir = repository / ".git"
    require(git_dir.is_dir() and not git_dir.is_symlink(), "fixture must be an ordinary local clone")
    config = configparser.ConfigParser(interpolation=None, strict=True)
    config.read_string(bounded_read(git_dir / "config", 65536).decode("utf-8"))
    remotes = [section for section in config.sections() if section.startswith('remote "')]
    require(remotes == ['remote "origin"'], "fixture has unauthorized remote configuration")
    origin = config.get('remote "origin"', "url", fallback="")
    require(
        origin
        in (
            ORIGIN,
            ORIGIN[:-4],
            "git@github.com:wxxb789/symmetry-0006h-provider-fixture.git",
            "ssh://git@github.com/wxxb789/symmetry-0006h-provider-fixture.git",
        ),
        "fixture origin does not match the authorized target",
    )
    total, count = 0, 0
    for directory, dirs, files in os.walk(repository, followlinks=False):
        dirs[:] = [name for name in dirs if name != ".git"]
        for name in dirs + files:
            child = pathlib.Path(directory) / name
            require(
                not child.is_symlink() and not getattr(child, "is_junction", lambda: False)(),
                "fixture symlinks and junctions are not allowed",
            )
        for name in files:
            path = pathlib.Path(directory) / name
            require(path.is_file(), "fixture special file is not allowed")
            total += path.stat().st_size
            count += 1
            require(count <= 10000 and total <= 64 * 1024 * 1024, "fixture copy exceeds bound")
    # Copy the repository's working files, not its hooks/config/auth-bearing .git.
    # No git process, remote access, .git mutation, or commit is needed for evidence.
    shutil.copytree(repository, workspace, ignore=shutil.ignore_patterns(".git"))
    receipt.record(
        "copy_authorized_fixture",
        repository=str(repository),
        workspace=str(workspace),
        authorizedOrigin=ORIGIN,
        workingFiles=count,
        workingBytes=total,
        gitMetadataCopied=False,
        sourceMutated=False,
        remoteOperations=[],
    )


def native_identity(process, sessions, workspace, expected=None, persisted=False):
    state = process.request("get_state")
    require(isinstance(state, dict), "native state is not an object")
    identity, filename = state.get("sessionId"), state.get("sessionFile")
    require(isinstance(identity, str) and identity.strip(), "native session id missing")
    require(isinstance(filename, str) and pathlib.Path(filename).is_absolute(), "native session filename invalid")
    require(
        contained(filename, sessions) and pathlib.Path(filename).suffix == ".jsonl",
        "native session filename escapes pinned directory",
    )
    require(same_path(state.get("cwd", ""), workspace), "native state cwd differs from fixture workspace")
    if expected:
        require(identity == expected[0] and same_path(filename, expected[1]), "native retained identity changed")
    require(
        state.get("isStreaming") is False and state.get("isCompacting") is False, "native session not explicitly idle"
    )
    require(
        state.get("steering") == []
        and state.get("followUp") == []
        and state.get("ordered") == []
        and state.get("pendingMessageCount") == 0,
        "native idle state retains queued work",
    )
    require(
        state.get("pendingQuestions") == [] and state.get("pendingDialogs") == [],
        "native session needs actionable input",
    )
    require(
        state.get("effectivePermissionPreset") == "workspace" and state.get("permissionPresetStatus") == "applied",
        "native workspace permissions not applied",
    )
    if persisted:
        require(pathlib.Path(filename).is_file(), "native retained session file missing")
        with pathlib.Path(filename).open("rb") as stream:
            line = stream.readline(MAX_LINE + 1)
        require(len(line) <= MAX_LINE, "native session header exceeds bound")
        require(process.receipt.check_raw(line.decode("utf-8")), "credential found in native session header")
        header = strict_json(line)
        require(
            isinstance(header, dict)
            and header.get("type") == "session"
            and header.get("id") == identity
            and same_path(header.get("cwd", ""), workspace),
            "native first header does not bind id and fixture workspace",
        )
        process.receipt.record(
            "session_header",
            process=process.name,
            filename=filename,
            header=header,
            provenance="first line of exact get_state.sessionFile only",
        )
    stats = process.request("get_session_stats")
    require(
        stats.get("sessionId") == identity and same_path(stats.get("sessionFile", ""), filename),
        "native stats identity differs from state",
    )
    require(
        isinstance(stats.get("toolCalls"), int) and not isinstance(stats["toolCalls"], bool),
        "native tool call statistics missing",
    )
    process.receipt.record(
        "identity_readback",
        process=process.name,
        state=state,
        stats=stats,
        nativeHandle=identity,
        nativeSessionFile=filename,
        pinnedSessionDirectory=str(sessions),
        sessionRealpath=os.path.realpath(filename),
        sessionDirectoryRealpath=os.path.realpath(sessions),
    )
    return (identity, filename), stats


def protocol_gate(process, agent_dir):
    data = process.request("get_protocol_info")
    require(
        data.get("protocolVersion") == 1
        and data.get("mode") == "classic"
        and data.get("engineVersion") == "5.1.69"
        and data.get("serverVersion") == "5.1.69",
        "native protocol or pinned engine version mismatch",
    )
    require(
        {"durable_client_message_id", "effect_ledger", "session_lifecycle"}.issubset(data.get("capabilities", [])),
        "required native durable/effect capabilities absent",
    )
    require(same_path(data.get("agentDir", ""), agent_dir), "native agent directory is not isolated")


def action(name, message):
    return {
        "message": message,
        "clientMessageId": str(uuid.uuid5(NAMESPACE, name + ":message")),
        "clientTurnId": str(uuid.uuid5(NAMESPACE, name + ":turn")),
    }


def write_prompt(kind, context=""):
    filename, content = ARTIFACTS[kind]
    final = {"ok": True, "artifact": filename, "bytes": len(content), "sha256": hashlib.sha256(content).hexdigest()}
    prompt = (
        "Perform this bounded repository evidence task. "
        + context
        + "Use only the read and write tools. Do not delegate, run commands, access credentials, "
        "make network calls, or change any other file. Write exactly these UTF-8 bytes to "
        + filename
        + ": "
        + json.dumps(content.decode("utf-8"))
        + ". The JSON string specifies the literal file content, including its final newline. "
        "After the tool succeeds, your final assistant response must be only this strict JSON "
        "object, without markdown, extra keys, or protocol/event frames: " + json.dumps(final)
    )
    return action(kind, prompt), final


def verify_artifact(workspace, kind, receipt):
    filename, expected = ARTIFACTS[kind]
    path = workspace / filename
    require(not path.is_symlink() and contained(path, workspace), "artifact escapes workspace")
    actual = bounded_read(path, 4096)
    receipt.record(
        "artifact_readback",
        path=str(path),
        byteLength=len(actual),
        sha256=hashlib.sha256(actual).hexdigest(),
        expectedBytes=expected.decode("utf-8"),
        exactBytes=actual == expected,
    )
    require(actual == expected, "native artifact bytes do not match exact expected content")


def verify_turn(process, start, expected_final, read_original=False):
    process.idle_event(start)
    frames = [frame for _, frame in process.frames[start:]]
    messages = [
        frame["message"]
        for frame in frames
        if frame["type"] == "message_end" and frame.get("message", {}).get("role") == "assistant"
    ]
    require(messages, "native assistant completion missing")
    final_message = messages[-1]
    require(final_message.get("stopReason") == "stop", "native final response did not complete successfully")
    content = final_message.get("content", [])
    require(all(block.get("type") == "text" for block in content), "native final response is not plain text")
    final_text = "".join(block.get("text", "") for block in content)
    require(len(final_text) <= 1024, "native final JSON exceeds bound")
    try:
        parsed = strict_json(final_text)
    except ValueError:
        raise WitnessError("native final response is not strict JSON") from None
    require(
        json.dumps(parsed, sort_keys=True) == json.dumps(expected_final, sort_keys=True),
        "native final JSON does not match bounded task",
    )
    tools = [frame for frame in frames if frame["type"] == "tool_execution_start"]
    require(
        tools and all(frame.get("toolName") in ("read", "write") for frame in tools),
        "native tools absent or outside allowlist",
    )
    require(
        sum(frame.get("toolName") == "write" for frame in tools) == 1, "native turn did not perform exactly one write"
    )
    if read_original:
        original = ARTIFACTS["original"][0]
        reads = [
            index
            for index, frame in enumerate(tools)
            if frame.get("toolName") == "read" and pathlib.Path(frame.get("args", {}).get("path", "")).name == original
        ]
        writes = [index for index, frame in enumerate(tools) if frame.get("toolName") == "write"]
        require(reads and min(reads) < min(writes), "native turn did not read original before writing")
    usage = [message["usage"] for message in messages if isinstance(message.get("usage"), dict)]
    require(usage, "native assistant usage missing")
    unknown = any(value.get("costUnknown") is True or value.get("cost", {}).get("unknown") is True for value in usage)
    process.receipt.record(
        "turn_readback",
        process=process.name,
        final=parsed,
        usage=usage,
        costClassification="unknown" if unknown else "reported",
        costNote="zero is not free when costUnknown is true",
    )


def completed_effect(process, stable_action, identity, kind="input"):
    effect_id = stable_action["clientMessageId"] if kind == "input" else stable_action["effectId"]
    data = process.request("get_effect_status", effectId=effect_id)
    effect = data.get("effect", {})
    require(
        data.get("found") is True
        and effect.get("effectId") == effect_id
        and effect.get("durableSessionId") == identity[0]
        and effect.get("kind") == kind
        and effect.get("state") == "done",
        "native completed effect not found or identity mismatched",
    )
    if kind == "input":
        outcome = effect.get("outcome")
        require(
            outcome == "completed" or (isinstance(outcome, dict) and outcome.get("kind") == "completed"),
            "native input effect did not complete successfully",
        )
    return effect


def replay_without_work(
    process, stable_action, identity, sessions, workspace, stats, command="prompt", expected_admission=None
):
    start = len(process.frames)
    response = process.response(process.send(command, **stable_action), command)
    data = response.get("data", {})
    if command == "prompt":
        admission = data.get("admission", {})
        require(
            admission.get("state") == "completed"
            and admission.get("durableSessionId") == identity[0]
            and admission.get("clientMessageId") == stable_action["clientMessageId"]
            and admission.get("clientTurnId") == stable_action["clientTurnId"],
            "native replay did not return completed durable admission",
        )
        require(
            data.get("clientMessageId") == stable_action["clientMessageId"]
            and data.get("clientTurnId") == stable_action["clientTurnId"]
            and data.get("disposition") == admission.get("disposition") == "started"
            and isinstance(admission.get("turnId"), str)
            and bool(admission["turnId"])
            and data.get("turnId") == admission["turnId"]
            and admission.get("outcome", {}).get("kind") == "completed",
            "native replay completed outcome or original turn identity mismatched",
        )
        if expected_admission is not None:
            require(admission == expected_admission, "native retained replay changed the completed original admission")
    else:
        require(
            data.get("replayed") is True and data.get("effect", {}).get("state") == "done",
            "native abort replay did not return recorded effect",
        )
    _, after = native_identity(process, sessions, workspace, identity, persisted=True)
    require(after["toolCalls"] == stats["toolCalls"], "native replay changed tool call statistics")
    require(
        after.get("userMessages") == stats.get("userMessages")
        and after.get("assistantMessages") == stats.get("assistantMessages"),
        "native replay appended conversation messages",
    )
    require(
        not any(
            frame["type"] in ("agent_start", "tool_execution_start", "tool_execution_end")
            for _, frame in process.frames[start:]
        ),
        "native replay started new work",
    )
    process.receipt.record(
        "replay_no_work",
        process=process.name,
        command=command,
        durableId=stable_action.get("clientMessageId", stable_action.get("effectId")),
        beforeStats=stats,
        afterStats=after,
    )
    return data.get("admission")


def pinned_binary(system, binary):
    """Authorize the operator-named executable against the committed platform pin."""
    pin = PINS.get(system)
    require(pin is not None, "unsupported operating system")
    configured = os.environ.get(BINARY_ENVIRONMENT, "")
    require(bool(configured), BINARY_ENVIRONMENT + " must name the pinned executable")
    declared = pathlib.Path(configured).resolve()
    require(declared.is_file(), BINARY_ENVIRONMENT + " must name an existing file")
    require(same_path(binary, declared), "--binary must name the file declared by " + BINARY_ENVIRONMENT)
    size = binary.stat().st_size
    observed = digest(binary)
    require(size == pin["size"] and observed == pin["sha256"], "exact platform binary pin mismatch")
    return {"path": str(binary), "size": size, "sha256": observed, "independentPlatformPin": pin}


def run(args, receipt, active):
    binary = pathlib.Path(args.binary).resolve()
    repository = pathlib.Path(args.repository).resolve()
    out = pathlib.Path(args.out).resolve()
    root = (
        pathlib.Path(args.root).absolute()
        if args.root
        else pathlib.Path(tempfile.mkdtemp(prefix="symmetry-omo-native-"))
    )
    receipt.value.update(root=str(root), output=str(out))
    require(not contained(root, repository) and not contained(repository, root), "evidence root overlaps fixture")
    if args.root:
        root.mkdir(mode=0o700, parents=True, exist_ok=False)
    require(not root.is_symlink(), "private evidence root must not be a symlink")
    receipt.record("private_root", path=str(root), accessControl=private_root(root))
    system = platform.system()
    receipt.value["platform"] = {
        "system": system,
        "platform": platform.platform(),
        "kernel": platform.release(),
        "kernelVersion": platform.version(),
        "machine": platform.machine(),
        "distribution": platform.win32_ver()
        if os.name == "nt"
        else bounded_read(pathlib.Path("/etc/os-release"), 65536).decode("utf-8"),
        "wslDistribution": os.environ.get("WSL_DISTRO_NAME") if system == "Linux" else None,
    }
    receipt.value["binary"] = pinned_binary(system, binary)
    if system == "Linux":
        distribution = receipt.value["platform"]["distribution"]
        require(
            "ID=ubuntu" in distribution
            and 'VERSION_ID="24.04"' in distribution
            and receipt.value["platform"]["wslDistribution"] == "Ubuntu-24.04",
            "Linux witness requires the exact Ubuntu-24.04 WSL distribution",
        )
    env = isolated_environment(root)
    for command in ("--version", "--help"):
        cli = NativeProcess(receipt, command, [str(binary), command], root, env, rpc=False)
        active.append(cli)
        text = cli.finish_cli()
        receipt.record("cli_metadata", argv=[str(binary), command], output=text)
        if command == "--version":
            require(text.startswith("omo 5.1.69 "), "native CLI version differs from pin")
        else:
            require("--mode" in text and "--session" in text, "native CLI help lacks required launch flags")
    workspace, sessions = root / "workspace", root / "sessions"
    copy_fixture(repository, workspace, receipt)
    require(
        all(not (workspace / name).exists() for name, _ in ARTIFACTS.values()),
        "fixture already contains a witness artifact",
    )
    receipt.value["scenarios"]["metadata"] = {"classification": "passed"}
    receipt.value["activeScenario"] = "fresh_identity"

    key = os.environ.get("SYMMETRY_OMO_PROBE_KEY", "")
    url = os.environ.get("SYMMETRY_OMO_PROBE_URL", "")
    require(bool(key) and bool(url), "approved relay environment is missing")
    endpoint = urllib.parse.urlsplit(url)
    require(
        endpoint.scheme == "https"
        and endpoint.hostname
        and endpoint.username is None
        and endpoint.password is None
        and not endpoint.query
        and not endpoint.fragment,
        "relay URL must be HTTPS without credentials, query, or fragment",
    )
    env["SYMMETRY_OMO_PROBE_KEY"] = key
    config = {
        "providers": {
            "tencent-litellm": {
                "api": "openai-completions",
                "baseUrl": url,
                "apiKey": "$SYMMETRY_OMO_PROBE_KEY",
                "models": [
                    {"id": "deepseek-v4.1-flash-fast", "reasoning": False, "contextWindow": 1000000, "maxTokens": 4096}
                ],
            }
        }
    }
    encoded = json.dumps(config, indent=2)
    require(receipt.check_raw(encoded), "model configuration unexpectedly contains credential value")
    config_path = root / "agent" / "models.json"
    config_path.write_text(encoded + "\n", encoding="utf-8")
    receipt.record(
        "model_configuration",
        path=str(config_path),
        config=config,
        credentialSource="environment only",
        ambientCredentialInheritance=False,
    )
    base = [
        str(binary),
        "--mode",
        "rpc",
        "--no-extensions",
        "--no-skills",
        "--no-context-files",
        "--no-prompt-templates",
        "--no-themes",
        "--permission-preset",
        "workspace",
        "--tools",
        "read,write",
        "--session-dir",
        str(sessions),
        "--provider",
        "tencent-litellm",
        "--model",
        "deepseek-v4.1-flash-fast",
        "--thinking",
        "off",
    ]

    def launch(name, retained=None):
        argv = base + (["--session", retained] if retained else [])
        process = NativeProcess(receipt, name, argv, workspace, env)
        active.append(process)
        protocol_gate(process, root / "agent")
        return process

    process = launch("fresh")
    identity, _ = native_identity(process, sessions, workspace)
    sentinel = sessions / "unrelated-witness-sentinel.jsonl"
    sentinel_bytes = b'{"unrelated":"preserve byte-for-byte; never open as a native session"}\n'
    sentinel.write_bytes(sentinel_bytes)
    receipt.value["sentinel"] = {"path": str(sentinel), "expectedSha256": hashlib.sha256(sentinel_bytes).hexdigest()}
    receipt.record("sentinel_created", path=str(sentinel), sha256=digest(sentinel))
    receipt.value["scenarios"]["fresh_identity"] = {
        "classification": "unavailable",
        "identity": identity,
        "reason": "awaiting first persisted native header",
    }

    receipt.value["activeScenario"] = "durable_replay"
    original, expected = write_prompt("original")
    receipt.record(
        "stable_action",
        name="original",
        action=original,
        ackLoss="native prompt acknowledgement deliberately discarded by witness owner",
    )
    start = len(process.frames)
    transport = process.send("prompt", **original)
    process.discarded.add(transport)
    verify_turn(process, start, expected)
    require(not process.discarded, "discarded prompt ACK never arrived")
    identity, stats = native_identity(process, sessions, workspace, identity, persisted=True)
    receipt.value["scenarios"]["fresh_identity"] = {
        "classification": "passed",
        "identity": identity,
        "headerVerification": "first persisted line verified",
    }
    verify_artifact(workspace, "original", receipt)
    completed_effect(process, original, identity)
    original_admission = replay_without_work(process, original, identity, sessions, workspace, stats)
    conflict_start = len(process.frames)
    changed = {**original, "message": original["message"] + " Different payload under the same id."}
    conflict = process.response(process.send("prompt", **changed), "prompt", expected_success=False)
    require(conflict.get("errorCode") == "client_message_id_conflict", "native changed-payload conflict not rejected")
    _, after_conflict = native_identity(process, sessions, workspace, identity, persisted=True)
    require(
        after_conflict["toolCalls"] == stats["toolCalls"]
        and not any(
            frame["type"] in ("agent_start", "tool_execution_start") for _, frame in process.frames[conflict_start:]
        ),
        "native conflicting input started work",
    )
    receipt.value["scenarios"]["durable_replay"] = {"classification": "passed"}

    receipt.value["activeScenario"] = "process_restart"
    process.stop()
    process = launch("retained_restart", identity[1])
    _, restored_stats = native_identity(process, sessions, workspace, identity, persisted=True)
    require(restored_stats["toolCalls"] == stats["toolCalls"], "native restart changed historical tool calls")
    completed_effect(process, original, identity)
    replay_without_work(
        process, original, identity, sessions, workspace, restored_stats, expected_admission=original_admission
    )
    context = "Read " + ARTIFACTS["original"][0] + " using the read tool before writing the new file. "
    resumed, expected = write_prompt("resume", context)
    receipt.record("stable_action", name="resume", action=resumed)
    start = len(process.frames)
    admitted = process.request("prompt", **resumed)
    require(
        admitted.get("admission", {}).get("durableSessionId") == identity[0]
        and admitted.get("disposition") == "started"
        and isinstance(admitted.get("turnId"), str),
        "native retained input was not admitted to the original session",
    )
    verify_turn(process, start, expected, read_original=True)
    native_identity(process, sessions, workspace, identity, persisted=True)
    verify_artifact(workspace, "original", receipt)
    verify_artifact(workspace, "resume", receipt)
    completed_effect(process, resumed, identity)
    receipt.value["scenarios"]["process_restart"] = {
        "classification": "passed",
        "kind": "whole native process stop and exact --session reopen",
        "daemonRestart": "not tested",
    }

    receipt.value["activeScenario"] = "fresh_handoff"
    process.stop()
    process = launch("fresh_handoff")
    successor, _ = native_identity(process, sessions, workspace)
    require(
        successor[0] != identity[0] and not same_path(successor[1], identity[1]),
        "fresh handoff reused the predecessor native session",
    )
    original_bytes = ARTIFACTS["original"][1]
    handoff_context = (
        "This is a NEW native session after the predecessor process was closed. "
        "Advisory context only, not execution truth: the predecessor artifact "
        + ARTIFACTS["original"][0]
        + " had UTF-8 content "
        + json.dumps(original_bytes.decode("utf-8"))
        + " and SHA256 "
        + hashlib.sha256(original_bytes).hexdigest()
        + ". "
        + context
    )
    handed, expected = write_prompt("handoff", handoff_context)
    receipt.record(
        "stable_action",
        name="handoff",
        action=handed,
        predecessor=identity,
        successor=successor,
        nativeSessionArgument=False,
    )
    start = len(process.frames)
    process.request("prompt", **handed)
    verify_turn(process, start, expected, read_original=True)
    successor, _ = native_identity(process, sessions, workspace, successor, persisted=True)
    verify_artifact(workspace, "original", receipt)
    verify_artifact(workspace, "handoff", receipt)
    completed_effect(process, handed, successor)
    receipt.value["scenarios"]["fresh_handoff"] = {
        "classification": "passed",
        "predecessor": identity,
        "successor": successor,
        "advisoryContextOnly": True,
    }

    receipt.value["activeScenario"] = "cancellation"
    cancellation = action(
        "cancellation",
        "Do not read, write, or mutate any file, and do not call any tools. "
        "Produce a long plain-text stream of the integers 1 through 20000, one per line. "
        "Continue until explicitly stopped; do not emit protocol frames or event JSON.",
    )
    receipt.record("stable_action", name="cancellation", action=cancellation)
    start = len(process.frames)
    process.request("prompt", **cancellation)
    deadline = time.monotonic() + 60
    while not any(frame["type"] == "agent_start" for _, frame in process.frames[start:]):
        process.next(deadline)
    streaming = process.request("get_state")
    if streaming.get("isStreaming") is False:
        process.idle_event(start)
        receipt.value["scenarios"]["cancellation"] = {
            "classification": "unavailable",
            "reason": "native turn already idle before abort; not tested",
        }
    else:
        require(
            streaming.get("isStreaming") is True
            and streaming.get("sessionId") == successor[0]
            and same_path(streaming.get("sessionFile", ""), successor[1]),
            "native cancellation streaming identity invalid",
        )
        abort = {
            "effectId": str(uuid.uuid5(NAMESPACE, "cancellation:abort")),
            "awaitIdle": True,
            "clearQueue": True,
            "graceMs": 5000,
        }
        receipt.record("stable_action", name="abort", action=abort, streamingReadback=streaming)
        stopped = process.request("abort", timeout=30, **abort)
        require(
            stopped.get("stopped") is True and stopped.get("idle") is True,
            "native cancellation did not quiesce within grace",
        )
        process.idle_event(start, timeout=30)
        _, stopped_stats = native_identity(process, sessions, workspace, successor, persisted=True)
        effect = completed_effect(process, abort, successor, "abort")
        replay_without_work(process, abort, successor, sessions, workspace, stopped_stats, command="abort")
        ended = [frame for _, frame in process.frames[start:] if frame["type"] == "agent_end"]
        if effect.get("disposition") == "no_active_turn":
            receipt.value["scenarios"]["cancellation"] = {
                "classification": "unavailable",
                "reason": "native turn became idle before abort dispatch; not tested",
            }
        else:
            require(
                any(
                    frame.get("aborted") is True and frame.get("abortedBy", {}).get("effectId") == abort["effectId"]
                    for frame in ended
                ),
                "native abort did not identify this command as the run stop owner",
            )
            receipt.value["scenarios"]["cancellation"] = {"classification": "passed"}
    require(
        not any(frame["type"].startswith("tool_execution_") for _, frame in process.frames[start:]),
        "native cancellation prompt unexpectedly performed a tool effect",
    )
    process.stop()
    receipt.check()
    receipt.value["activeScenario"] = None


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", required=True, help="Must be the file named by SYMMETRY_OMO_BINARY")
    parser.add_argument("--repository", required=True, help="Authorized local fixture clone; never modified")
    parser.add_argument("--out", required=True, help="Sanitized JSON receipt destination")
    parser.add_argument("--root", help="New private evidence directory; retained on failure and success")
    args = parser.parse_args()
    receipt = Receipt(os.environ.get("SYMMETRY_OMO_PROBE_KEY", ""))
    receipt.value["invocation"] = [
        sys.executable,
        str(pathlib.Path(__file__).resolve()),
        "--binary",
        args.binary,
        "--repository",
        args.repository,
        "--out",
        args.out,
    ] + (["--root", args.root] if args.root else [])
    active = []
    exit_code = 1
    try:
        run(args, receipt, active)
        unavailable = any(item["classification"] == "unavailable" for item in receipt.value["scenarios"].values())
        receipt.value["classification"] = "unavailable" if unavailable else "passed"
        exit_code = 2 if unavailable else 0
    except BaseException as error:
        # Never stringify arbitrary native/OS exceptions: they may embed credentials.
        reason = str(error) if isinstance(error, WitnessError) else "witness operation failed: " + type(error).__name__
        receipt.value["classification"] = "failed"
        receipt.value["failure"] = receipt.sanitize(reason)
        scenario = receipt.value.get("activeScenario")
        if scenario:
            receipt.value["scenarios"][scenario] = {"classification": "failed", "reason": receipt.sanitize(reason)}
    finally:
        for process in reversed(active):
            try:
                process.stop()
            except Exception:
                receipt.value["classification"] = "failed"
                receipt.value["cleanupFailure"] = "owned native process or readers did not terminate cleanly"
                exit_code = 1
        if receipt.failure.is_set():
            receipt.value["classification"] = "failed"
            receipt.value["captureFailure"] = receipt.failure_reason
            exit_code = 1
        sentinel = receipt.value.get("sentinel")
        if sentinel:
            try:
                actual = hashlib.sha256(bounded_read(pathlib.Path(sentinel["path"]), 4096)).hexdigest()
                preserved = actual == sentinel["expectedSha256"]
                receipt.record(
                    "sentinel_preserved",
                    "passed" if preserved else "failed",
                    path=sentinel["path"],
                    sha256=actual,
                    exactBytes=preserved,
                    nativeDirectoriesSearched=False,
                )
                require(preserved, "unrelated session sentinel changed")
            except Exception:
                receipt.value["classification"] = "failed"
                receipt.value["sentinelFailure"] = "unrelated session sentinel missing, unreadable, or changed"
                exit_code = 1
        receipt.value["finishedAt"] = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
        receipt.value["exitCode"] = exit_code
        try:
            receipt.save(pathlib.Path(args.out).resolve())
        except Exception:
            print('{"classification":"failed","reason":"sanitized receipt could not be saved"}')
            return 1
    print(
        json.dumps(
            {
                "classification": receipt.value["classification"],
                "out": receipt.sanitize(str(pathlib.Path(args.out).resolve())),
                "root": receipt.sanitize(receipt.value.get("root", "")),
                "exitCode": exit_code,
            }
        )
    )
    return exit_code


if __name__ == "__main__":
    sys.exit(main())
