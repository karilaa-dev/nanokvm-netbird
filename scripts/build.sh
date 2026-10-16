#!/usr/bin/env bash
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
version=0.81.1
archive="netbird_${version}_linux_armv6.tar.gz"
expected=aecefaeb9fb34038cddd8b547a7403bb154e6a786171eb71ef5f6dd24b30bc83
output=dist/nanokvm-netbird-0.1.0
mkdir -p .cache/downloads "$output"/{scripts,systemd,web,licenses,docs}
if [[ ! -f .cache/downloads/$archive ]]; then
    curl -fL --retry 3 "https://github.com/netbirdio/netbird/releases/download/v${version}/${archive}" -o ".cache/downloads/$archive"
fi
printf '%s  %s\n' "$expected" ".cache/downloads/$archive" | sha256sum --check --status
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -trimpath -ldflags='-s -w' -o "$output/nanokvm-netbird" ./cmd/nanokvm-netbird
tar -xzf ".cache/downloads/$archive" -C "$output" netbird
tar -xzf ".cache/downloads/$archive" -C "$output/licenses" LICENSE LICENSES README.md
cp scripts/{install.sh,uninstall.sh,integrate.py} "$output/scripts/"
cp systemd/*.service "$output/systemd/"
cp web/netbird-provider.js "$output/web/"
cp README.md LICENSE "$output/"
cp docs/*.md "$output/docs/"
chmod 755 "$output/scripts/"*.sh "$output/scripts/integrate.py"
python3 - "$output" <<'PY'
import hashlib, pathlib, sys
root = pathlib.Path(sys.argv[1])
lines = [f'{hashlib.sha256(p.read_bytes()).hexdigest()}  {p.relative_to(root)}\n' for p in sorted(root.rglob('*')) if p.is_file() and p.name != 'SHA256SUMS']
(root / 'SHA256SUMS').write_text(''.join(lines))
PY
tar -czf dist/nanokvm-netbird-0.1.0-armv7.tar.gz -C dist nanokvm-netbird-0.1.0
(cd dist && sha256sum nanokvm-netbird-0.1.0-armv7.tar.gz > SHA256SUMS)
cat dist/SHA256SUMS
