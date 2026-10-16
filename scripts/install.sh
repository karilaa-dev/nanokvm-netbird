#!/usr/bin/env bash
set -euo pipefail
package_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
target=/opt/nanokvm-netbird

[[ $(id -u) == 0 ]] || { echo 'Run as root.' >&2; exit 1; }
[[ $(uname -m) == armv7l ]] || { echo 'This package requires ARMv7 NanoKVM Go.' >&2; exit 1; }
[[ $(dpkg-query -W -f='${Version}' nanokvm-go) == 0.2.4 ]] || { echo 'NanoKVM Go 0.2.4 is required.' >&2; exit 1; }
[[ -c /dev/net/tun ]] || { echo '/dev/net/tun is unavailable.' >&2; exit 1; }
for tool in python3 systemctl systemd-run curl sha256sum ss iptables; do
    command -v "$tool" >/dev/null || { echo "Missing prerequisite: $tool" >&2; exit 1; }
done
(cd "$package_dir" && sha256sum --check --quiet SHA256SUMS)
state=$(python3 "$package_dir/scripts/integrate.py" check)
"$package_dir/nanokvm-netbird" --check --netbird "$package_dir/netbird"
if [[ $state != installed ]]; then
    if ss -H -lnt '( sport = :8080 or sport = :8443 )' | read -r _; then
        echo 'Port 8080 or 8443 is already occupied.' >&2; exit 1
    fi
fi
if [[ ${1:-} == --check ]]; then
    echo "Compatibility: $state. No services or files changed."
    exit 0
fi
[[ $# == 0 ]] || { echo 'Usage: install.sh [--check]' >&2; exit 1; }
if [[ $state == installed ]]; then
    systemctl is-active --quiet nanokvm-netbird.service
    curl -kfsS --max-time 5 https://127.0.0.1/api/extensions/netbird/healthz
    echo 'This version is already installed.'
    exit 0
fi

install -d -m 755 "$target" "$target/scripts" "$target/web" "$target/systemd" "$target/licenses" "$target/docs"
install -m 755 "$package_dir/nanokvm-netbird" "$package_dir/netbird" "$target/"
install -m 755 "$package_dir/scripts/"{install.sh,uninstall.sh,integrate.py} "$target/scripts/"
install -m 644 "$package_dir/web/netbird-provider.js" "$target/web/"
install -m 644 "$package_dir/systemd/"*.service "$target/systemd/"
cp -R "$package_dir/licenses/." "$target/licenses/"
cp -R "$package_dir/docs/." "$target/docs/"
install -m 644 "$package_dir/README.md" "$package_dir/LICENSE" "$target/"
install -m 644 "$package_dir/systemd/"*.service /etc/systemd/system/
systemctl daemon-reload

rollback() {
    trap - ERR INT TERM
    echo 'Installation failed; restoring the original NanoKVM web service.' >&2
    "$target/scripts/uninstall.sh" || true
    exit 1
}
trap rollback ERR INT TERM
systemd-run --unit=nanokvm-netbird-rollback --on-active=3m "$target/scripts/uninstall.sh"
python3 "$target/scripts/integrate.py" apply
systemctl restart nanokvm-go.service
for ((i=0; i<30; i++)); do
    if curl -kfsS --max-time 2 https://127.0.0.1:8443/ -o /dev/null; then break; fi
    sleep 1
done
curl -kfsS --max-time 5 https://127.0.0.1:8443/ -o /dev/null
systemctl enable --now nanokvm-netbird.service
curl -kfsS --retry 5 --retry-connrefused --retry-delay 1 --max-time 5 https://127.0.0.1/api/extensions/netbird/healthz
curl -kfsS --max-time 5 https://127.0.0.1/ -o /dev/null
systemctl stop nanokvm-netbird-rollback.timer
trap - ERR INT TERM
echo 'Installed. Refresh NanoKVM, then open Settings > Remote Network > NetBird.'
echo 'The tunnel is not started until you connect from the provider panel.'
