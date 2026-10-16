# NanoKVM NetBird

Add **NetBird** to **Settings → Remote Network** on **NanoKVM Go 0.2.4**. Connect through NetBird Cloud or a self-hosted server, view connection status, and reconnect automatically after reboot.

This community plugin supports the verified **0.2.4 ARMv7 Debian firmware**. It bundles NetBird **0.81.1**. Installation briefly restarts the NanoKVM web service; SSH remains available. Uninstall the plugin before upgrading NanoKVM firmware.

## Install

Open an SSH session to your NanoKVM. Run these commands as root:

```sh
cd /var/tmp
curl -fLO https://github.com/karilaa-dev/nanokvm-netbird/releases/download/v0.1.0/nanokvm-netbird-0.1.0-armv7.tar.gz
curl -fLO https://github.com/karilaa-dev/nanokvm-netbird/releases/download/v0.1.0/SHA256SUMS
sha256sum -c SHA256SUMS
tar -xzf nanokvm-netbird-0.1.0-armv7.tar.gz
bash nanokvm-netbird-0.1.0/scripts/install.sh --check
bash nanokvm-netbird-0.1.0/scripts/install.sh
```

Continue only if the checksum and compatibility checks pass. The installer checks the exact frontend version and restores the original web service if installation fails.

## Use

1. Hard-refresh the NanoKVM web page and sign in.
2. Open **Settings → Remote Network → NetBird**.
3. Keep the NetBird Cloud URL or enter your self-hosted server's HTTPS URL.
4. Paste a NetBird setup key and click **Connect**.
5. Connect your computer or phone to the same NetBird network. Allow access in your NetBird policy, then open `https://NETBIRD_IP` using the IP shown in the panel.

The NanoKVM login stays the same. The plugin does not save your setup key. Leave the key blank when reconnecting an enrolled device.

**Reconnect after reboot** enables automatic startup. NetBird DNS and remote subnet routes are optional and disabled by default. **Disconnect** stops the tunnel and automatic startup but keeps the enrollment. NanoKVM's existing Tailscale two-factor policy also protects NetBird actions.

## Build

On your development machine, install Git, Go **1.24+**, Python **3.8+**, curl, and tar. Then run:

```sh
git clone https://github.com/karilaa-dev/nanokvm-netbird.git
cd nanokvm-netbird
bash scripts/build.sh
```

The build cross-compiles the plugin for ARMv7 and downloads a checksum-verified NetBird binary. It produces:

- `dist/nanokvm-netbird-0.1.0-armv7.tar.gz`
- `dist/SHA256SUMS`

Copy both files to `/var/tmp` on the NanoKVM, then follow the checksum, extraction, and installation steps above. See [verification](docs/verification.md) for tests and device checks.

## Uninstall

Run as root on the NanoKVM:

```sh
/opt/nanokvm-netbird/scripts/uninstall.sh
```

This restores the original web service and removes the NetBird provider. Enrollment data remains available for a later installation.

See [the integration design](docs/design.md) for ports, authentication, and storage. Plugin source is [MIT licensed](LICENSE); bundled NetBird files retain their upstream licenses.
