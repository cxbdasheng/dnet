---
name: verify
description: Build and drive D-NET through its CLI and HTTP surface.
---

# D-NET runtime verification

1. Build an isolated binary: `go build -o /tmp/dnet-verify/dnet .`.
2. Always pass `-c` with a temporary YAML file and `-l 127.0.0.1:<free-port>`; never use the user's default config or port.
3. Start the binary in the background, capture stdout/stderr to a temp log, and poll `/login` to verify Web readiness.
4. For outbound HTTP behavior, configure a DDNS `callback` provider with a static A record and point `accesskey` at a local HTTP/TLS server.
5. To exercise WaitInternet deterministically, use an unused local TCP DNS address such as `-dns tcp://127.0.0.1:<free-port>`; Web should respond immediately and synchronization should continue after the 60-second timeout.
6. Do not run `-u` against the real network or install/uninstall services. If updater TLS must be checked, use a local CONNECT proxy with a self-signed certificate and a synthetic high version so replacement is never reached.
7. Stop all spawned processes and confirm temporary listener ports are clean.
