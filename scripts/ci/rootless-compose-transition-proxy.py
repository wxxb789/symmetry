#!/usr/bin/env python3
"""Disposable response-holding reverse proxy for the rootless Compose witness."""

from __future__ import annotations

import argparse
import gzip
import hashlib
import http.client
import json
import re
import socket
import threading
import time
import zlib
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, urlsplit


ARM_TTL_SECONDS = 180
HOLD_TTL_SECONDS = 120
STATUS_TTL_SECONDS = 120
RUN_TRANSITION = re.compile(r"^/api/v1/runs/([^/]+)/transitions/([^/]+)$")
SESSION_REGISTRATION = re.compile(r"^/api/v1/machines/[^/]+/sessions/[^/]+$")
HOP_BY_HOP_HEADERS = {
    "connection",
    "keep-alive",
    "proxy-authenticate",
    "proxy-authorization",
    "proxy-connection",
    "te",
    "trailer",
    "transfer-encoding",
    "upgrade",
}


class Hold:
    def __init__(self, runtime_id: str, runtime_epoch: int, nonce: str, now: float):
        self.runtime_id = runtime_id
        self.runtime_epoch = runtime_epoch
        self.nonce = nonce
        self.state = "armed"
        self.run_id = ""
        self.transition_id = ""
        self.transition_sha256 = ""
        self.generation = 0
        self.claim_id = ""
        self.lease_token_sha256 = ""
        self.expires_at = now + ARM_TTL_SECONDS
        self.release = threading.Event()

    def matches_transition(
        self,
        run_id: str,
        transition_id: str,
        fields: dict[str, object],
        token_hash: str,
        body_hash: str,
    ) -> bool:
        return (
            self.run_id == run_id
            and self.transition_id == transition_id
            and self.transition_sha256 == body_hash
            and self.generation == fields.get("generation")
            and self.runtime_id == fields.get("runtime_id")
            and self.runtime_epoch == fields.get("runtime_epoch")
            and self.claim_id == fields.get("claim_id")
            and self.lease_token_sha256 == token_hash
        )

    def status(self) -> dict[str, object]:
        return {
            "state": self.state,
            "run_id": self.run_id,
            "generation": self.generation,
            "runtime_id": self.runtime_id,
            "runtime_epoch": self.runtime_epoch,
            "claim_id": self.claim_id,
        }


class HoldRegistry:
    def __init__(self):
        self.lock = threading.Lock()
        self.changed = threading.Condition(self.lock)
        self.holds: dict[str, Hold] = {}
        self.runtime_epochs: dict[str, int] = {}

    def expire(self) -> None:
        now = time.monotonic()
        with self.lock:
            for runtime_id, hold in tuple(self.holds.items()):
                if hold.expires_at > now:
                    continue
                if hold.state == "held":
                    hold.state = "released"
                    hold.expires_at = now + STATUS_TTL_SECONDS
                    hold.release.set()
                elif hold.state == "released":
                    del self.holds[runtime_id]
                else:
                    del self.holds[runtime_id]

    def observe_registration(self, path: str, response_body: bytes) -> None:
        if SESSION_REGISTRATION.fullmatch(path) is None:
            return
        try:
            value = json.loads(response_body)
        except (UnicodeDecodeError, json.JSONDecodeError):
            return
        if not isinstance(value, dict) or not isinstance(value.get("runtimes"), list):
            return
        observed = []
        for runtime in value["runtimes"]:
            if not isinstance(runtime, dict):
                return
            runtime_id = runtime.get("runtime_id")
            runtime_epoch = runtime.get("runtime_epoch")
            if (
                not isinstance(runtime_id, str)
                or not runtime_id
                or not isinstance(runtime_epoch, int)
                or isinstance(runtime_epoch, bool)
                or runtime_epoch <= 0
            ):
                return
            observed.append((runtime_id, runtime_epoch))
        with self.changed:
            for runtime_id, runtime_epoch in observed:
                self.runtime_epochs[runtime_id] = runtime_epoch
            if observed:
                self.changed.notify_all()

    def arm(self, runtime_id: str, nonce: str) -> bool:
        self.expire()
        deadline = time.monotonic() + 5
        with self.changed:
            runtime_epoch = self.runtime_epochs.get(runtime_id)
            while runtime_epoch is None:
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    return False
                self.changed.wait(remaining)
                runtime_epoch = self.runtime_epochs.get(runtime_id)
            if runtime_epoch is None or runtime_epoch <= 0:
                return False
            now = time.monotonic()
            current = self.holds.get(runtime_id)
            if current is not None and current.state != "released":
                return False
            self.holds[runtime_id] = Hold(runtime_id, runtime_epoch, nonce, now)
            return True

    def status(self, runtime_id: str, nonce: str) -> dict[str, object] | None:
        self.expire()
        with self.lock:
            hold = self.holds.get(runtime_id)
            if hold is None or hold.nonce != nonce:
                return None
            return hold.status()

    def release(self, value: dict[str, object]) -> bool:
        self.expire()
        runtime_id = value.get("runtime_id")
        if not isinstance(runtime_id, str):
            return False
        with self.lock:
            hold = self.holds.get(runtime_id)
            if hold is None or hold.state != "held" or hold.nonce != value.get("nonce"):
                return False
            if (
                hold.run_id != value.get("run_id")
                or hold.generation != value.get("generation")
                or hold.runtime_epoch != value.get("runtime_epoch")
                or hold.claim_id != value.get("claim_id")
                or hold.lease_token_sha256 != value.get("lease_token_sha256")
            ):
                return False
            hold.state = "released"
            hold.expires_at = time.monotonic() + STATUS_TTL_SECONDS
            hold.release.set()
            return True

    def hold_successful_transition(
        self, run_id: str, transition_id: str, body: bytes
    ) -> tuple[Hold, bool] | None:
        try:
            value = json.loads(body)
        except (UnicodeDecodeError, json.JSONDecodeError):
            return None
        if not isinstance(value, dict) or value.get("state") != "failed":
            return None
        runtime_id = value.get("runtime_id")
        lease_token = value.get("lease_token")
        generation = value.get("generation")
        runtime_epoch = value.get("runtime_epoch")
        claim_id = value.get("claim_id")
        if (
            not isinstance(runtime_id, str)
            or not runtime_id
            or not isinstance(lease_token, str)
            or not lease_token
            or not isinstance(generation, int)
            or isinstance(generation, bool)
            or generation <= 0
            or not isinstance(runtime_epoch, int)
            or isinstance(runtime_epoch, bool)
            or runtime_epoch <= 0
            or not isinstance(claim_id, str)
            or not claim_id
        ):
            return None
        token_hash = hashlib.sha256(lease_token.encode("utf-8")).hexdigest()
        body_hash = hashlib.sha256(body).hexdigest()
        self.expire()
        with self.lock:
            hold = self.holds.get(runtime_id)
            if hold is None:
                return None
            if hold.runtime_epoch != runtime_epoch:
                return None
            if hold.state == "armed":
                hold.run_id = run_id
                hold.transition_id = transition_id
                hold.transition_sha256 = body_hash
                hold.generation = generation
                hold.runtime_epoch = runtime_epoch
                hold.claim_id = claim_id
                hold.lease_token_sha256 = token_hash
                hold.state = "held"
                hold.expires_at = time.monotonic() + HOLD_TTL_SECONDS
                return hold, True
            if hold.state == "held" and hold.matches_transition(
                run_id, transition_id, value, token_hash, body_hash
            ):
                return hold, False
            return None


REGISTRY = HoldRegistry()


def background_expiration() -> None:
    while True:
        time.sleep(1)
        REGISTRY.expire()


class TransitionProxyHandler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, _format: str, *_args: object) -> None:
        # Requests carry machine credentials and transition lease tokens.
        return

    def do_GET(self) -> None:
        parsed = urlsplit(self.path)
        if self._is_websocket_upgrade():
            self._forward_websocket()
            return
        if parsed.path == "/healthz":
            self.send_response_only(204)
            self.end_headers()
            return
        if parsed.path == "/__test/status":
            try:
                query = parse_qs(parsed.query, strict_parsing=True)
            except ValueError:
                self._send_empty(400)
                return
            runtime_ids = query.get("runtime_id", [])
            nonces = query.get("nonce", [])
            if len(runtime_ids) != 1 or len(nonces) != 1:
                self._send_empty(400)
                return
            status = REGISTRY.status(runtime_ids[0], nonces[0])
            if status is None:
                self._send_empty(404)
                return
            self._send_json(200, status)
            return
        if parsed.path.startswith("/__test/"):
            self._send_empty(404)
            return
        self._forward()

    def do_POST(self) -> None:
        parsed = urlsplit(self.path)
        if parsed.path == "/__test/arm":
            value = self._read_json()
            if value is None or set(value) != {"runtime_id", "nonce"}:
                self._send_empty(400)
                return
            runtime_id = value.get("runtime_id")
            nonce = value.get("nonce")
            if not isinstance(runtime_id, str) or not runtime_id or not isinstance(nonce, str) or not nonce:
                self._send_empty(400)
                return
            if not REGISTRY.arm(runtime_id, nonce):
                self._send_empty(409)
                return
            self._send_empty(204)
            return
        if parsed.path == "/__test/release":
            value = self._read_json()
            required = {
                "run_id",
                "generation",
                "runtime_id",
                "runtime_epoch",
                "claim_id",
                "lease_token_sha256",
                "nonce",
            }
            if value is None or set(value) != required:
                self._send_empty(400)
                return
            if (
                not isinstance(value.get("run_id"), str)
                or not value["run_id"]
                or not isinstance(value.get("generation"), int)
                or isinstance(value.get("generation"), bool)
                or value["generation"] <= 0
                or not isinstance(value.get("runtime_id"), str)
                or not value["runtime_id"]
                or not isinstance(value.get("runtime_epoch"), int)
                or isinstance(value.get("runtime_epoch"), bool)
                or value["runtime_epoch"] <= 0
                or not isinstance(value.get("claim_id"), str)
                or not value["claim_id"]
                or not isinstance(value.get("lease_token_sha256"), str)
                or re.fullmatch(r"[0-9a-f]{64}", value["lease_token_sha256"]) is None
                or not isinstance(value.get("nonce"), str)
                or not value["nonce"]
            ):
                self._send_empty(400)
                return
            if not REGISTRY.release(value):
                self._send_empty(409)
                return
            self._send_empty(204)
            return
        if parsed.path.startswith("/__test/"):
            self._send_empty(404)
            return
        self._forward()

    def do_PUT(self) -> None:
        self._forward()

    def do_PATCH(self) -> None:
        self._forward()

    def do_DELETE(self) -> None:
        self._forward()

    def do_HEAD(self) -> None:
        self._forward()

    def _read_body(self, limit: int | None = None) -> bytes | None:
        if self.headers.get("Transfer-Encoding"):
            return None
        content_length = self.headers.get("Content-Length")
        if content_length is None:
            return b""
        try:
            length = int(content_length, 10)
        except ValueError:
            return None
        if length < 0 or (limit is not None and length > limit):
            return None
        body = self.rfile.read(length)
        if len(body) != length:
            return None
        return body

    def _read_json(self) -> dict[str, object] | None:
        body = self._read_body(limit=4096)
        if body is None:
            return None
        try:
            value = json.loads(body)
        except (UnicodeDecodeError, json.JSONDecodeError):
            return None
        return value if isinstance(value, dict) else None

    def _is_websocket_upgrade(self) -> bool:
        upgrades = {value.strip().lower() for value in self.headers.get("Upgrade", "").split(",")}
        connections = {value.strip().lower() for value in self.headers.get("Connection", "").split(",")}
        return "websocket" in upgrades and "upgrade" in connections

    def _forward_websocket(self) -> None:
        parsed = urlsplit(self.path)
        if parsed.scheme or parsed.netloc or not self.path.startswith("/"):
            self._send_empty(400)
            return
        body = self._read_body()
        if body is None:
            self._send_empty(400)
            return

        target = self.server.target  # type: ignore[attr-defined]
        connection = http.client.HTTPConnection(target[0], target[1], timeout=30)
        try:
            connection.putrequest(self.command, self.path, skip_host=True, skip_accept_encoding=True)
            for name, value in self.headers.raw_items():
                connection.putheader(name, value)
            connection.endheaders(body)
            upstream_socket = connection.sock
            if upstream_socket is None:
                self._send_empty(502)
                return
            response = http.client.HTTPResponse(upstream_socket, method=self.command)
            response.begin()
        except (OSError, http.client.HTTPException):
            connection.close()
            self._send_empty(502)
            return

        if response.status != 101:
            response_body = response.read()
            response_headers = response.getheaders()
            connection.close()
            self.send_response_only(response.status, response.reason)
            forwarded_length = False
            for name, value in response_headers:
                if name.lower() in HOP_BY_HOP_HEADERS:
                    continue
                if name.lower() == "content-length":
                    forwarded_length = True
                self.send_header(name, value)
            if not forwarded_length:
                self.send_header("Content-Length", str(len(response_body)))
            self.end_headers()
            if self.command != "HEAD" and response_body:
                self.wfile.write(response_body)
            return

        self.close_connection = True
        self.send_response_only(response.status, response.reason)
        for name, value in response.getheaders():
            self.send_header(name, value)
        self.end_headers()
        self.wfile.flush()
        upstream_socket.settimeout(None)
        self.connection.settimeout(None)

        def relay(source, destination: socket.socket) -> None:
            try:
                while True:
                    data = source.read1(65536)
                    if not data:
                        break
                    destination.sendall(data)
            except (OSError, ValueError):
                pass
            finally:
                try:
                    destination.shutdown(socket.SHUT_WR)
                except OSError:
                    pass

        client_to_control = threading.Thread(target=relay, args=(self.rfile, upstream_socket), daemon=True)
        control_to_client = threading.Thread(target=relay, args=(response.fp, self.connection), daemon=True)
        client_to_control.start()
        control_to_client.start()
        client_to_control.join()
        control_to_client.join()
        connection.close()

    def _send_empty(self, status: int) -> None:
        self.send_response_only(status)
        self.send_header("Content-Length", "0")
        self.end_headers()

    def _send_json(self, status: int, value: dict[str, object]) -> None:
        body = json.dumps(value, separators=(",", ":")).encode("utf-8")
        self.send_response_only(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def _forward(self) -> None:
        parsed = urlsplit(self.path)
        if parsed.scheme or parsed.netloc or not self.path.startswith("/"):
            self._send_empty(400)
            return
        body = self._read_body()
        if body is None:
            self._send_empty(400)
            return

        target = self.server.target  # type: ignore[attr-defined]
        connection = http.client.HTTPConnection(target[0], target[1], timeout=30)
        try:
            connection.putrequest(self.command, self.path, skip_host=True, skip_accept_encoding=True)
            for name, value in self.headers.raw_items():
                if name.lower() in HOP_BY_HOP_HEADERS:
                    continue
                connection.putheader(name, value)
            connection.endheaders(body)
            response = connection.getresponse()
            response_body = response.read()
            response_status = response.status
            response_reason = response.reason
            response_headers = response.getheaders()
        except (OSError, http.client.HTTPException):
            self._send_empty(502)
            return
        finally:
            connection.close()

        transition = None
        if (
            self.command == "PUT"
            and 200 <= response_status < 300
            and SESSION_REGISTRATION.fullmatch(parsed.path) is not None
        ):
            observed_body = response_body
            content_encoding = ",".join(
                value.strip().lower()
                for name, value in response_headers
                if name.lower() == "content-encoding"
            )
            if content_encoding == "gzip":
                try:
                    observed_body = gzip.decompress(response_body)
                except (gzip.BadGzipFile, EOFError, zlib.error):
                    observed_body = b""
            elif content_encoding not in ("", "identity"):
                observed_body = b""
            REGISTRY.observe_registration(parsed.path, observed_body)
        if self.command == "PUT":
            match = RUN_TRANSITION.fullmatch(parsed.path)
            if match is not None and 200 <= response_status < 300:
                transition = REGISTRY.hold_successful_transition(match.group(1), match.group(2), body)
        if transition is not None:
            hold, _first = transition
            if not hold.release.wait(HOLD_TTL_SECONDS):
                REGISTRY.expire()
                hold.release.set()

        self.send_response_only(response_status, response_reason)
        forwarded_length = False
        for name, value in response_headers:
            lower = name.lower()
            if lower in HOP_BY_HOP_HEADERS:
                continue
            if lower == "content-length":
                forwarded_length = True
            self.send_header(name, value)
        if not forwarded_length:
            self.send_header("Content-Length", str(len(response_body)))
        self.end_headers()
        if self.command != "HEAD" and response_body:
            self.wfile.write(response_body)


class TransitionProxyServer(ThreadingHTTPServer):
    daemon_threads = True
    allow_reuse_address = True

    def __init__(self, address: tuple[str, int], target: tuple[str, int]):
        super().__init__(address, TransitionProxyHandler)
        self.target = target


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--listen", default="0.0.0.0:4000")
    parser.add_argument("--upstream", default="http://control:4000")
    arguments = parser.parse_args()
    listen_host, separator, listen_port = arguments.listen.rpartition(":")
    if not separator or not listen_host:
        parser.error("--listen must be HOST:PORT")
    try:
        port = int(listen_port, 10)
        upstream = urlsplit(arguments.upstream)
        upstream_port = upstream.port or (80 if upstream.scheme == "http" else 443)
    except ValueError:
        parser.error("invalid listen or upstream port")
    if not 0 < port <= 65535:
        parser.error("listen port must be in 1..65535")
    if upstream.scheme != "http" or not upstream.hostname or upstream.path not in ("", "/") or upstream.query or upstream.fragment:
        parser.error("upstream must be an HTTP origin without path, query, or fragment")
    if upstream.username or upstream.password:
        parser.error("upstream credentials are not accepted")

    target = (upstream.hostname, upstream_port)
    REGISTRY.expire()
    threading.Thread(target=background_expiration, daemon=True).start()
    server = TransitionProxyServer((listen_host, port), target)
    server.serve_forever(poll_interval=0.5)


if __name__ == "__main__":
    main()
