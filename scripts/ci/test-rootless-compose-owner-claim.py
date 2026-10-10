#!/usr/bin/env python3
"""Exercise the Portal fixture login with a trusted TLS Secure-cookie session."""

import http.server
import importlib.util
import pathlib
import ssl
import subprocess
import tempfile
import threading
import unittest
import urllib.parse


SCRIPT = pathlib.Path(__file__).with_name("prepare-rootless-compose-owner-claim.py")
spec = importlib.util.spec_from_file_location("rootless_claim", SCRIPT)
claim = importlib.util.module_from_spec(spec)
spec.loader.exec_module(claim)


class PortalHandler(http.server.BaseHTTPRequestHandler):
    def log_message(self, *_arguments):
        pass

    def send_page(self, status, body, cookie=False):
        self.send_response(status)
        if cookie:
            self.send_header("Set-Cookie", "fixture-session=csrf; Path=/; Secure; HttpOnly")
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path == "/portal/login":
            self.send_page(200, b'<input name="_csrf_token" value="fixture-csrf">', cookie=True)
        elif self.path == "/portal" and "fixture-session=csrf" in self.headers.get("Cookie", ""):
            self.send_page(200, b'<meta name="csrf-token" content="authenticated-csrf">')
        else:
            self.send_page(403, b"Secure session missing")

    def do_POST(self):
        form = urllib.parse.parse_qs(self.rfile.read(int(self.headers["Content-Length"])).decode())
        if (self.path != "/portal/login" or "fixture-session=csrf" not in self.headers.get("Cookie", "")
                or form.get("_csrf_token") != ["fixture-csrf"] or form.get("operator_token") != ["fixture-operator"]):
            self.send_page(403, b"Invalid Secure session login")
            return
        self.send_response(302)
        self.send_header("Location", "/portal")
        self.end_headers()


class PortalTLSClientTest(unittest.TestCase):
    def test_trusted_tls_preserves_secure_csrf_session_and_rejects_untrusted_certificate(self):
        with tempfile.TemporaryDirectory() as directory:
            certificate = pathlib.Path(directory) / "tls.crt"
            key = pathlib.Path(directory) / "tls.key"
            subprocess.run([
                "openssl", "req", "-x509", "-newkey", "rsa:2048", "-sha256", "-nodes", "-days", "1",
                "-subj", "/CN=localhost", "-addext", "subjectAltName=DNS:localhost",
                "-keyout", str(key), "-out", str(certificate),
            ], capture_output=True, check=True)
            server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), PortalHandler)
            context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
            context.load_cert_chain(certificate, key)
            server.socket = context.wrap_socket(server.socket, server_side=True)
            thread = threading.Thread(target=server.serve_forever)
            thread.start()
            try:
                origin = f"https://localhost:{server.server_port}"
                with self.assertRaisesRegex(SystemExit, "login page was unavailable"):
                    claim.portal_client(origin, "fixture-operator", None)
                client, csrf = claim.portal_client(origin, "fixture-operator", str(certificate))
                self.assertEqual(csrf, "authenticated-csrf")
                with client.open(origin + "/portal", timeout=5) as response:
                    self.assertEqual(response.status, 200)
            finally:
                server.shutdown()
                thread.join()
                server.server_close()


if __name__ == "__main__":
    unittest.main()
