#!/usr/bin/env python3
"""Verify the installed provider using a local authenticated Mozilla cookie jar."""
import argparse
import base64
import hashlib
import http.cookiejar
import json
import os
from pathlib import Path
import socket
import ssl
import urllib.error
import urllib.parse
import urllib.request

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--device', required=True, help='NanoKVM HTTPS URL, for example https://nanokvm.local')
parser.add_argument('--cookies', default='.cache/device/cookies')
args = parser.parse_args()
jar = http.cookiejar.MozillaCookieJar(args.cookies)
jar.load(ignore_discard=True, ignore_expires=True)
context = ssl._create_unverified_context()
opener = urllib.request.build_opener(urllib.request.HTTPSHandler(context=context), urllib.request.HTTPCookieProcessor(jar))
anon = urllib.request.build_opener(urllib.request.HTTPSHandler(context=context))


def request(path, body=None, origin=None, authenticated=True):
    headers = {}
    if origin:
        headers['Origin'] = origin
    if body is not None:
        headers.update({'Content-Type': 'application/json', 'X-NanoKVM-NetBird': '1'})
    req = urllib.request.Request(args.device + path, data=None if body is None else json.dumps(body).encode(), headers=headers)
    try:
        response = (opener if authenticated else anon).open(req, timeout=20)
    except urllib.error.HTTPError as err:
        response = err
    with response:
        return response.status, response.read()


code, body = request('/api/application/version')
assert code == 200 and json.loads(body)['data']['current'] == '0.2.4', 'Native version/login failed'
print('PASS native NanoKVM 0.2.4 API and login')
code, body = request('/api/extensions/netbird/api/status')
assert code == 200, (code, body)
state = json.loads(body)
assert state['state'] in ('connected', 'disconnected', 'busy'), state
print('PASS provider status:', state['state'])
code, _ = request('/api/extensions/netbird/api/status', authenticated=False)
assert code == 401, code
print('PASS unauthenticated access rejected')
code, _ = request('/api/extensions/netbird/api/disconnect', {}, 'https://attacker.invalid')
assert code == 403, code
print('PASS cross-origin mutation rejected')
code, _ = request('/api/extensions/netbird/api/connect', {'managementUrl': 'http://invalid.example'}, args.device)
assert code == 400, code
print('PASS invalid connection settings rejected')
code, body = request('/assets/netbird-provider-0.1.0.js')
expected = (Path(__file__).resolve().parent.parent / 'web/netbird-provider.js').read_bytes()
assert code == 200 and hashlib.sha256(body).digest() == hashlib.sha256(expected).digest(), 'Provider asset differs'
print('PASS installed frontend matches source')
code, body = request('/api/extensions/tailscale/status')
assert code == 200 and json.loads(body)['code'] == 0, 'Tailscale API failed'
print('PASS original Tailscale API')

url = urllib.parse.urlparse(args.device)
req = urllib.request.Request(args.device + '/api/ws')
jar.add_cookie_header(req)
key = base64.b64encode(os.urandom(16)).decode()
headers = '\r\n'.join([
    'GET /api/ws HTTP/1.1', f'Host: {url.netloc}', 'Upgrade: websocket',
    'Connection: Upgrade', f'Sec-WebSocket-Key: {key}', 'Sec-WebSocket-Version: 13',
    f'Origin: {args.device}', f'Cookie: {req.get_header("Cookie")}', '', '',
])
with socket.create_connection((url.hostname, url.port or 443), timeout=10) as raw:
    with context.wrap_socket(raw, server_hostname=url.hostname) as connection:
        connection.sendall(headers.encode())
        response = connection.recv(4096)
        assert response.startswith(b'HTTP/1.1 101 '), response.split(b'\r\n')[0]
print('PASS native control WebSocket upgrade through gateway (no input sent)')
