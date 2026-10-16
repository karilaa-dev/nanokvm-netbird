# Verification of the 0.2.4 integration

Run the local checks from the project root:

```sh
go test -race ./...
go vet ./...
shellcheck scripts/*.sh
node --check web/netbird-provider.js
python3 scripts/test_integration.py -v
bash scripts/build.sh
```

The integration tests require the original `desktop-fdMOBWG4.js` from NanoKVM Go 0.2.4 in `.cache/device/assets/`. This firmware file is not redistributed. Its expected SHA-256 is recorded in `scripts/integrate.py`.

The tests exercise session rejection, origin checks, two-factor verification, malformed settings, concurrent operations, status parsing, disconnect behavior, and setup-key cleanup after success and failure. Status tests reject any attempt to launch a command. Integration tests apply the patch to the real captured bundle, repeat the installation, reject unknown firmware, and restore the original files while preserving unrelated configuration changes.

For an installed device, provide a Mozilla cookie jar from an authorized NanoKVM web login:

```sh
python3 scripts/verify_device.py --device https://DEVICE_IP --cookies /private/path/cookies
```

This checks the native version and login, provider status, unauthenticated rejection, cross-origin rejection, settings validation, frontend integrity, the Tailscale API, and the native control WebSocket upgrade. It sends no keyboard or mouse input and does not enroll or disconnect a peer.

## Device verification on 2026-10-16

The target was an ARMv7 NanoKVM Go running Debian 13 with application packages `nanokvm-go 0.2.4` and `kvmcomm 0.2.4`. Its API also reported version 0.2.4.

The package passed preflight and installed successfully. The gateway and original NanoKVM service ran together. HTTP redirected to the existing HTTPS address. Authenticated API requests and the control WebSocket upgrade passed through the gateway. Unauthenticated and cross-origin provider requests were rejected.

NetBird 0.81.1 ran on the device. Its local HTTP/JSON socket returned `NeedsLogin`. The provider read that response as disconnected and reported the daemon version. The authenticated Disconnect action then stopped and disabled the tunnel service. The final installation leaves the tunnel inactive pending enrollment.

The installed provider appeared beside Tailscale in the real NanoKVM Remote Network registry. Browser inspection used a temporary, read-only loopback relay because the automation browser rejected the device's self-signed certificate. The relay forwarded real status and static files. Its video WebSocket was not used for the UI check; the separate device check verified a direct WebSocket upgrade through the gateway.

At the initial installation, no NetBird setup key had been supplied, so enrollment, peer connectivity, remote video, and resource use during an active tunnel were unverified. Two-factor behavior was tested with native API fixtures; this device had two-factor authentication disabled. Rollback was exercised locally against the captured firmware files, not by uninstalling the live device.

## Status UI follow-up on 2026-10-16

The user subsequently enrolled the device and confirmed it worked. The live provider API reported Connected, an assigned NetBird IP, connected management and signaling, and one connected peer out of six. NetBird was enabled for startup. This follow-up did not reenroll or disconnect it.

Browser inspection reproduced the status page clearing its previous result and making another request when opened from the provider card. Five repeated navigations all initially showed a loading state. API reads varied with load on the device, which was using swap. The UI now shares pending reads and recent results instead of waiting for another device read on every navigation.

Verification against the installed UI, through the read-only relay:

- Five repeated navigations displayed the connected IP on the first observation and made no additional status requests while the result was fresh.
- Manual Refresh immediately showed a disabled Refreshing button. Navigating back and reopening during that refresh retained the connected details and made only one status request.
- A browser-only disconnect simulation intercepted the mutation before it reached the device. It verified that the action waited for an outstanding read, requested fresh status afterward, and updated the provider card from the new result. Real status was restored afterward.
- The device verification script passed authentication, origin rejection, settings validation, installed frontend integrity, the original Tailscale API, and the native control WebSocket upgrade.
- NetBird's process ID and start time were unchanged after deployment. The native server, gateway, and tunnel services all remained active.

The frontend syntax check and all three integration/rollback fixture tests passed. The update changes only browser behavior; underlying device API latency and remote video over NetBird were not optimized or benchmarked in this follow-up.
