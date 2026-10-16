#!/usr/bin/env python3
"""Apply only the NanoKVM Go 0.2.4 bundle whose provider registry was verified."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import tempfile

ASSET = 'desktop-fdMOBWG4.js'
ORIGINAL_SHA = 'b8037dee1c69466950c1392374d3f9392be6fa6f9b4d077064578f621e01bf4c'
MODULE = 'netbird-provider-0.1.0.js'
ANCHOR = b'cI(lI);const fI='
IMPORT = b'import{createNetbirdProvider as __nkNetBird}from"./netbird-provider-0.1.0.js";\n'


def digest(data):
    return hashlib.sha256(data).hexdigest()


def atomic_write(path, data, mode):
    with tempfile.NamedTemporaryFile(dir=path.parent, delete=False) as f:
        temp = Path(f.name)
        try:
            os.fchmod(f.fileno(), mode)
            f.write(data)
            f.flush()
            os.fsync(f.fileno())
            os.replace(temp, path)
        finally:
            temp.unlink(missing_ok=True)


def change_ports(data, old, new):
    text = data.decode()
    match = re.search(r'^port:\s*\n((?:[ \t]+[^\n]*\n)+)', text, re.M)
    if not match:
        raise ValueError('Cannot locate the NanoKVM port configuration')
    block = match.group(0)
    for proto, before, after in zip(('http', 'https'), old, new):
        pattern = rf'(^[ \t]+{proto}:\s*){before}(\s*(?:#.*)?$)'
        block, count = re.subn(pattern, lambda m: m[1] + str(after) + m[2], block, flags=re.M)
        if count != 1:
            raise ValueError(f'Expected {proto} port {before}; configuration was not changed')
    return (text[:match.start()] + block + text[match.end():]).encode()


def paths(root):
    web = root / 'kvmapp/server/web/assets'
    backup = root / 'var/lib/nanokvm-netbird-install'
    return web, backup, root / 'etc/kvm/server.yaml'


def check(root, module):
    web, backup, config = paths(root)
    original = (web / ASSET).read_bytes()
    if (backup / 'receipt.json').exists():
        receipt = json.loads((backup / 'receipt.json').read_text())
        if digest(original) != receipt['patchedSHA'] or digest((web / MODULE).read_bytes()) != digest(module.read_bytes()):
            raise ValueError('Installed plugin files changed; inspect them before reinstalling')
        change_ports(config.read_bytes(), (8080, 8443), (80, 443))
        return 'installed'
    if digest(original) != ORIGINAL_SHA or original.count(ANCHOR) != 1:
        raise ValueError('Unsupported NanoKVM frontend. Expected the verified 0.2.4 bundle')
    if (web / MODULE).exists():
        raise ValueError('Provider module already exists without an installation receipt')
    change_ports(config.read_bytes(), (80, 443), (8080, 8443))
    return 'ready'


def apply(root, module):
    if check(root, module) == 'installed':
        return
    web, backup, config = paths(root)
    backup.mkdir(mode=0o700, parents=True, exist_ok=True)
    original = (web / ASSET).read_bytes()
    patched = IMPORT + original.replace(ANCHOR, b'cI(lI);cI(__nkNetBird(d));const fI=')
    configuration = config.read_bytes()
    # The receipt and originals are durable before any live file changes.
    atomic_write(backup / ASSET, original, 0o600)
    atomic_write(backup / 'server.yaml', configuration, 0o600)
    atomic_write(backup / 'receipt.json', json.dumps({'originalSHA': ORIGINAL_SHA, 'patchedSHA': digest(patched), 'moduleSHA': digest(module.read_bytes())}).encode(), 0o600)
    atomic_write(web / MODULE, module.read_bytes(), 0o644)
    atomic_write(web / ASSET, patched, 0o644)
    atomic_write(config, change_ports(configuration, (80, 443), (8080, 8443)), config.stat().st_mode & 0o777)


def restore(root):
    web, backup, config = paths(root)
    receipt_path = backup / 'receipt.json'
    if not receipt_path.exists():
        return
    receipt = json.loads(receipt_path.read_text())
    asset = web / ASSET
    if asset.exists():
        current = digest(asset.read_bytes())
        if current == receipt['patchedSHA']:
            original = (backup / ASSET).read_bytes()
            if digest(original) != receipt['originalSHA']:
                raise ValueError('Original frontend backup checksum mismatch')
            atomic_write(asset, original, 0o644)
        elif current != receipt['originalSHA']:
            print('Frontend changed since installation; preserving that newer file')
    provider = web / MODULE
    if provider.exists() and digest(provider.read_bytes()) == receipt['moduleSHA']:
        provider.unlink()
    data = config.read_bytes()
    try:
        restored = change_ports(data, (8080, 8443), (80, 443))
    except ValueError:
        # An interrupted apply may have left the original ports in place.
        change_ports(data, (80, 443), (8080, 8443))
    else:
        atomic_write(config, restored, config.stat().st_mode & 0o777)
    receipt_path.rename(backup / 'last-receipt.json')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=['check', 'apply', 'restore'])
    parser.add_argument('--root', type=Path, default=Path('/'))
    parser.add_argument('--module', type=Path, default=Path(__file__).resolve().parent.parent / 'web/netbird-provider.js')
    args = parser.parse_args()
    try:
        if args.action == 'check':
            print(check(args.root, args.module))
        elif args.action == 'apply':
            apply(args.root, args.module)
        else:
            restore(args.root)
    except (OSError, ValueError, KeyError) as err:
        parser.exit(1, f'{err}\n')
