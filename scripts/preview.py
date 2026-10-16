#!/usr/bin/env python3
"""Read-only local browser relay for inspecting the installed device UI.

Uses a local Mozilla cookie jar from an authorized NanoKVM login. The relay
binds to loopback, never logs cookies, and blocks all remote mutations.
"""
import argparse
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import http.cookiejar
import ssl
import urllib.error
import urllib.request

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--device', required=True, help='NanoKVM HTTPS URL, for example https://nanokvm.local')
parser.add_argument('--cookies', default='.cache/device/cookies')
parser.add_argument('--port', type=int, default=9080)
args = parser.parse_args()


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        jar = http.cookiejar.MozillaCookieJar(args.cookies)
        jar.load(ignore_discard=True, ignore_expires=True)
        opener = urllib.request.build_opener(urllib.request.HTTPSHandler(context=ssl._create_unverified_context()), urllib.request.HTTPCookieProcessor(jar))
        req = urllib.request.Request(args.device + self.path)
        try:
            response = opener.open(req, timeout=15)
        except urllib.error.HTTPError as err:
            response = err
        except (OSError, urllib.error.URLError):
            self.send_error(502)
            return
        with response:
            data = response.read()
            self.send_response(response.status)
            for name, value in response.headers.items():
                if name.lower() not in {'content-length', 'transfer-encoding', 'connection', 'set-cookie', 'strict-transport-security'}:
                    self.send_header(name, value)
            if self.path == '/':
                self.send_header('Set-Cookie', 'nano-kvm-auth=1; Path=/; SameSite=Strict')
            self.send_header('Content-Length', str(len(data)))
            self.end_headers()
            self.wfile.write(data)

    def do_POST(self):
        if self.path == '/api/application/features/sync':
            data = b'{"code":0,"data":{}}'
            self.send_response(200)
            self.send_header('Content-Type', 'application/json')
            self.send_header('Content-Length', str(len(data)))
            self.end_headers()
            self.wfile.write(data)
        else:
            self.send_error(403, 'Read-only verification relay')

    def log_message(self, *unused):
        pass


print(f'Read-only NanoKVM relay on http://127.0.0.1:{args.port}', flush=True)
ThreadingHTTPServer(('127.0.0.1', args.port), Handler).serve_forever()
