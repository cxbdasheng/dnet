#!/usr/bin/env python3
"""Exercise a locally built image; never push images or use cloud credentials."""
import http.cookiejar
import json
from pathlib import Path
import socket
import subprocess
import sys
import tempfile
import time
import urllib.request
import uuid


def docker(*args, check=True):
    return subprocess.run(
        ["docker", *args], check=check, capture_output=True, text=True, timeout=90
    ).stdout.strip()


def wait_for(check, description):
    deadline = time.monotonic() + 30
    last_error = None
    while time.monotonic() < deadline:
        try:
            return check()
        except (OSError, ValueError, AssertionError) as error:
            last_error = error
            time.sleep(0.25)
    raise RuntimeError(f"Timed out waiting for {description}: {last_error}")


def main():
    if len(sys.argv) != 3:
        raise SystemExit("Usage: docker-smoke.py IMAGE LINUX_ECHO_BACKEND")
    image, binary = sys.argv[1], Path(sys.argv[2]).resolve(strict=True)
    prefix = "dnet-smoke-" + uuid.uuid4().hex[:12]
    network, backend, app = prefix, prefix + "-echo", prefix + "-app"
    password = uuid.uuid4().hex
    opener = urllib.request.build_opener(
        urllib.request.ProxyHandler({}),
        urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()),
    )
    web_port = 0

    def api(path, payload=None):
        data = json.dumps(payload).encode() if payload is not None else None
        request = urllib.request.Request(
            f"http://127.0.0.1:{web_port}{path}", data=data,
            headers={"Content-Type": "application/json"},
        )
        with opener.open(request, timeout=3) as response:
            result = json.load(response)
        if result.get("status") is not True:
            raise AssertionError(f"{path}: {result.get('msg')}")
        return result["data"]

    def published(port):
        mappings = json.loads(docker("inspect", app))[0]["NetworkSettings"]["Ports"]
        return int(mappings[port][0]["HostPort"])

    def exchanges():
        payload = b"dnet-tcp-smoke\x00" * 128
        with socket.create_connection(("127.0.0.1", published("19000/tcp")), 3) as conn:
            conn.settimeout(3)
            conn.sendall(payload)
            received = bytearray()
            while len(received) < len(payload):
                chunk = conn.recv(len(payload) - len(received))
                if not chunk:
                    raise AssertionError("TCP target closed before echoing all bytes")
                received.extend(chunk)
            assert received == payload, "TCP payload mismatch"
        # Two clients and differently sized datagrams verify reply isolation/boundaries.
        with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as a, socket.socket(
            socket.AF_INET, socket.SOCK_DGRAM
        ) as b:
            port = published("19000/udp")
            for conn in (a, b):
                conn.settimeout(3)
                conn.connect(("127.0.0.1", port))
            for payload_a, payload_b in ((b"client-a", b"client-b"), (b"a" * 1024, b"b" * 31)):
                a.send(payload_a)
                b.send(payload_b)
                assert a.recv(2048) == payload_a, "UDP client A echo mismatch"
                assert b.recv(2048) == payload_b, "UDP client B echo mismatch"

    try:
        with tempfile.TemporaryDirectory(prefix=prefix) as root:
            docker("network", "create", network)
            docker("run", "-d", "--name", backend, "--network", network,
                   "--network-alias", "echo-backend", "--mount",
                   f"type=bind,src={binary},dst=/smoke-backend,readonly",
                   "--entrypoint", "/smoke-backend", image)
            docker("run", "-d", "--name", app, "--network", network,
                   "-p", "127.0.0.1::9877/tcp", "-p", "127.0.0.1::19000/tcp",
                   "-p", "127.0.0.1::19000/udp", "--mount",
                   f"type=bind,src={root},dst=/root", image)
            web_port = published("9877/tcp")

            def web_ready():
                with opener.open(f"http://127.0.0.1:{web_port}/login", timeout=3) as response:
                    assert response.status == 200
                    assert b"<html" in response.read().lower()

            wait_for(web_ready, "Web login page")
            credentials = {"username": "ci-smoke", "password": password}
            api("/login", credentials)
            rules = [
                {"id": protocol, "name": protocol, "custom_name": True, "enabled": True,
                 "network": protocol + "4", "listen_address": "0.0.0.0",
                 "listen_port": 19000, "target_host": "echo-backend", "target_port": 19001,
                 "dial_timeout_sec": 3, "idle_timeout_sec": 0,
                 "max_connections": 10, "allow_cidrs": []}
                for protocol in ("tcp", "udp")
            ]
            api("/api/forward", {"enabled": True, "rules": rules})
            wait_for(exchanges, "TCP and UDP replies")
            config = Path(root, ".dnet_config.yaml")
            assert config.is_file(), "Default config was not persisted to the mounted directory"
            # On Linux the bind-mounted file belongs to container root (0600),
            # so the host runner cannot read it. Check inside the container,
            # without exposing credentials or weakening file permissions.
            docker("exec", app, "grep", "-qF", "echo-backend", "/root/.dnet_config.yaml")
            assert docker("exec", app, "stat", "-c", "%a", "/root/.dnet_config.yaml") == "600", \
                "Config permissions must remain 0600"
            assert api("/api/forward/probe", rules[0])["supported"] is True
            assert api("/api/forward/probe", rules[1])["supported"] is False
            docker("restart", "--time", "5", app)
            web_port = published("9877/tcp")
            wait_for(web_ready, "Web after restart")
            api("/login", credentials)
            restored = api("/api/forward")
            assert restored["enabled"] is True
            assert restored["rules"] == rules, "Saved rules changed after restart"
            wait_for(exchanges, "persisted TCP and UDP rules")
            health = docker("inspect", "--format", "{{.Config.Healthcheck.Test}}", app)
            assert "curl" in health, "Image healthcheck is missing"
            docker("exec", app, "curl", "-fsS", "http://localhost:9877/")
            print("PASS: Web, mounted config, restart, TCP, UDP and probe semantics")
            # Remove containers before deleting their bind-mounted directory.
            docker("rm", "-f", app, backend)
    except Exception:
        for container in (app, backend):
            print(f"--- {container} logs ---", file=sys.stderr)
            print(docker("logs", "--tail", "100", container, check=False), file=sys.stderr)
        raise
    finally:
        docker("rm", "-f", app, backend, check=False)
        docker("network", "rm", network, check=False)


if __name__ == "__main__":
    main()
