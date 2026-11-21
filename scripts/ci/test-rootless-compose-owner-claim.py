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
            # The decoys are hostile input: the real token has to win, and a
            # first-match scrape would submit `decoy-in-js` and be refused.
            self.send_page(
                200,
                b"""<script>var decoy = '<input name="_csrf_token" value="decoy-in-js">';</script>
<!-- <input name="_csrf_token" value="decoy-in-comment"> -->
<form method="post"><input type="hidden" name="_csrf_token" value="fixture-csrf"></form>""",
                cookie=True,
            )
        elif self.path == "/portal" and "fixture-session=csrf" in self.headers.get("Cookie", ""):
            self.send_page(
                200,
                b"""<script>var decoy = '<meta name="csrf-token" content="decoy-in-js">';</script>
<meta name="csrf-token" content="authenticated-csrf">""",
            )
        else:
            self.send_page(403, b"Secure session missing")

    def do_POST(self):
        form = urllib.parse.parse_qs(self.rfile.read(int(self.headers["Content-Length"])).decode())
        if (
            self.path != "/portal/login"
            or "fixture-session=csrf" not in self.headers.get("Cookie", "")
            or form.get("_csrf_token") != ["fixture-csrf"]
            or form.get("operator_token") != ["fixture-operator"]
        ):
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
            subprocess.run(
                [
                    "openssl",
                    "req",
                    "-x509",
                    "-newkey",
                    "rsa:2048",
                    "-sha256",
                    "-nodes",
                    "-days",
                    "1",
                    "-subj",
                    "/CN=localhost",
                    "-addext",
                    "subjectAltName=DNS:localhost",
                    "-keyout",
                    str(key),
                    "-out",
                    str(certificate),
                ],
                capture_output=True,
                check=True,
            )
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


class CsrfTokenParserTest(unittest.TestCase):
    """Hostile markup must not decide which CSRF token the fixture submits."""

    def login_token(self, page):
        return claim.single_token(claim.csrf_tokens(page).input_tokens, "login page")

    def test_decoy_in_a_script_body_does_not_win(self):
        page = (
            '<script>var decoy = \'<input name="_csrf_token" value="decoy-in-js">\';</script>'
            '<form method="post"><input type="hidden" name="_csrf_token" value="real-token"></form>'
        )
        self.assertEqual(self.login_token(page), "real-token")

    def test_decoy_in_a_comment_does_not_win(self):
        page = (
            '<!-- <input name="_csrf_token" value="decoy-in-comment"> --><input name="_csrf_token" value="real-token">'
        )
        self.assertEqual(self.login_token(page), "real-token")

    def test_attribute_order_does_not_matter(self):
        self.assertEqual(self.login_token('<input value="real-token" name="_csrf_token">'), "real-token")

    def test_entities_in_the_token_attribute_are_decoded(self):
        self.assertEqual(self.login_token('<input name="_csrf_token" value="a&amp;b">'), "a&b")

    def test_decoy_meta_inside_a_script_body_does_not_win(self):
        page = (
            '<script>var decoy = \'<meta name="csrf-token" content="decoy-in-js">\';</script>'
            '<meta name="csrf-token" content="authenticated-csrf">'
        )
        self.assertEqual(
            claim.single_token(claim.csrf_tokens(page).meta_tokens, "authenticated page"),
            "authenticated-csrf",
        )

    def test_two_distinct_real_tokens_are_refused(self):
        page = '<input name="_csrf_token" value="one"><input name="_csrf_token" value="two">'
        with self.assertRaisesRegex(SystemExit, "carried 2 distinct CSRF tokens"):
            self.login_token(page)

    def test_repeated_identical_token_is_one_token(self):
        page = '<input name="_csrf_token" value="same"><input name="_csrf_token" value="same">'
        self.assertEqual(self.login_token(page), "same")

    def test_page_without_a_token_is_refused(self):
        with self.assertRaisesRegex(SystemExit, "omitted CSRF token"):
            self.login_token("<html><body>no token here</body></html>")


if __name__ == "__main__":
    unittest.main()
