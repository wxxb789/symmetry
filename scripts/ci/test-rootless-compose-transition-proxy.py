#!/usr/bin/env python3
"""Verify compressed session registration admits the exact proxy hold binding."""

import gzip
import http.client
import http.server
import importlib.util
import json
import pathlib
import threading
import unittest
import urllib.parse


SCRIPT = pathlib.Path(__file__).with_name("rootless-compose-transition-proxy.py")


class TransitionProxyRegistrationTest(unittest.TestCase):
    def test_gzip_registration_arms_observed_runtime_epoch(self):
        spec = importlib.util.spec_from_file_location("transition_proxy_gzip_test", SCRIPT)
        proxy = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(proxy)
        runtime_id = "c3b94e16-f802-489e-8653-6c0427551e37"
        nonce = "gzip-registration-fixture"
        registration_path = "/api/v1/machines/fixture-machine/sessions/fixture-session"
        registration_response = {
            "runtimes": [{
                "runtime_key": "fixture-runtime",
                "runtime_id": runtime_id,
                "runtime_epoch": 7,
            }],
        }
        compressed_body = gzip.compress(json.dumps(registration_response).encode("utf-8"), mtime=0)

        class RegistrationHandler(http.server.BaseHTTPRequestHandler):
            def log_message(self, *_arguments):
                pass

            def do_PUT(self):
                self.rfile.read(int(self.headers["Content-Length"]))
                if self.path != registration_path:
                    self.send_response_only(404)
                    self.send_header("Content-Length", "0")
                    self.end_headers()
                    return
                self.send_response_only(200)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Encoding", "gzip")
                self.send_header("Content-Length", str(len(compressed_body)))
                self.end_headers()
                self.wfile.write(compressed_body)

        upstream = http.server.ThreadingHTTPServer(("127.0.0.1", 0), RegistrationHandler)
        server = proxy.TransitionProxyServer(("127.0.0.1", 0), upstream.server_address)
        threads = [threading.Thread(target=service.serve_forever) for service in (upstream, server)]
        for thread in threads:
            thread.start()
        client = http.client.HTTPConnection(*server.server_address, timeout=10)
        try:
            client.request("PUT", registration_path, b"{}", headers={
                "Content-Type": "application/json",
                "Accept-Encoding": "gzip",
            })
            response = client.getresponse()
            response.read()
            self.assertEqual(response.status, 200)

            client.request("POST", "/__test/arm", json.dumps({
                "runtime_id": runtime_id,
                "nonce": nonce,
            }), headers={"Content-Type": "application/json"})
            response = client.getresponse()
            response.read()
            self.assertEqual(response.status, 204)

            query = urllib.parse.urlencode({"runtime_id": runtime_id, "nonce": nonce})
            client.request("GET", "/__test/status?" + query)
            response = client.getresponse()
            status = json.loads(response.read())
            self.assertEqual(response.status, 200)
            self.assertEqual(status["state"], "armed")
            self.assertEqual(status["runtime_id"], runtime_id)
            self.assertEqual(status["runtime_epoch"], 7)
        finally:
            client.close()
            for service, thread in zip((upstream, server), threads):
                service.shutdown()
                service.server_close()
                thread.join()


if __name__ == "__main__":
    unittest.main()
