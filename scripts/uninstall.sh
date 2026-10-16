#!/usr/bin/env bash
set -euo pipefail
target=/opt/nanokvm-netbird
[[ $(id -u) == 0 ]] || { echo 'Run as root.' >&2; exit 1; }
systemctl disable --now nanokvm-netbird.service nanokvm-netbird-daemon.service 2>/dev/null || true
python3 "$target/scripts/integrate.py" restore
rm -f /etc/systemd/system/nanokvm-netbird.service /etc/systemd/system/nanokvm-netbird-daemon.service
systemctl daemon-reload
systemctl restart nanokvm-go.service
systemctl stop nanokvm-netbird-rollback.timer 2>/dev/null || true
echo 'Original NanoKVM web interface restored. NetBird identity and preferences are retained in /var/lib/nanokvm-netbird.'
