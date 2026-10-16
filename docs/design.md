# How NetBird integrates with NanoKVM Go

The installed 0.2.4 browser bundle contains a Remote Network provider registry. The patch adds one provider through that registry and uses the app's existing React runtime. It preserves the Tailscale provider. The installer checks the original bundle's SHA-256 before it modifies the file.

Sipeed's [NanoKVM Go repository](https://github.com/sipeed/NanoKVM-Go) contains no application source at the time of this implementation. The plugin therefore uses an external Go service and a version-specific browser patch. It does not replace the NanoKVM executable.

The original app sends a `connect-src 'self'` Content Security Policy. A gateway on the existing HTTPS address keeps the plugin API on that origin and preserves the policy. The original server moves from ports 80 and 443 to 8080 and 8443. The gateway takes ports 80 and 443, redirects HTTP to HTTPS, and proxies all existing routes to the original HTTPS server. Streaming responses and WebSocket upgrades pass through the proxy.

Only `/api/extensions/netbird/` belongs to the plugin. Each authenticated request asks the original `/api/auth/account` endpoint to validate its existing session cookie. Connection changes require same-origin JSON requests and follow the native Tailscale two-factor policy. When that policy applies, the original sensitive-action verifier must accept a new code before the plugin runs a command. The plugin does not read NanoKVM's JWT signing secret.

The gateway trusts the exact certificate in `/etc/kvm/server.crt` when it contacts the original server. It also serves the existing certificate pair. Certificate regeneration requires a gateway restart to load the new certificate pin.

NetBird runs in a separate systemd service with private Unix control sockets. Status reads use NetBird's HTTP/JSON socket directly, so polling does not launch another NetBird process. Enrollment uses the CLI. The plugin passes setup keys through temporary files with mode `0600`, removes them after the command, and omits CLI stderr from API responses. Preferences contain no setup key. NetBird retains its enrollment identity under `/var/lib/nanokvm-netbird`.

The provider card and detail page share an in-memory status result and any request already in progress. A result younger than five seconds avoids another read on navigation; a result younger than fifteen seconds can appear immediately while an older result is refreshed. Visible views poll every five seconds. Manual refresh bypasses the freshness check and displays progress without clearing the connection details. Connection actions wait for an outstanding read, pause polling, and invalidate the cached result before checking the new state. No status or credentials are saved in browser storage.

The package pins [NetBird v0.81.1](https://github.com/netbirdio/netbird/releases/tag/v0.81.1), verifies the upstream ARMv6 archive checksum, and includes its licenses. That static ARM executable runs on the device's ARMv7 processor. The plugin uses the documented [NetBird CLI](https://docs.netbird.io/get-started/cli). The Go garbage-collector memory targets reduce pressure on the device's 119 MB of RAM; they are soft targets, not hard process limits.

The backend listeners remain protected by NanoKVM's original authentication. This package does not add firewall rules or change the native server's bind address. Uninstall restores its original listening ports and removes the provider module. It preserves enrollment data so that reinstalling does not create a new peer identity.
