#!/usr/bin/env python3
"""Verify the updater-facing GoReleaser archive contract."""

from __future__ import annotations

import hashlib
import re
import sys
from pathlib import Path

EXPECTED_SUFFIXES = (
    "android_arm64.tar.gz",
    "darwin_x86_64.tar.gz",
    "darwin_arm64.tar.gz",
    "freebsd_i386.tar.gz",
    "freebsd_x86_64.tar.gz",
    "freebsd_armv5.tar.gz",
    "freebsd_armv6.tar.gz",
    "freebsd_armv7.tar.gz",
    "freebsd_arm64.tar.gz",
    "linux_i386.tar.gz",
    "linux_x86_64.tar.gz",
    "linux_armv5.tar.gz",
    "linux_armv6.tar.gz",
    "linux_armv7.tar.gz",
    "linux_arm64.tar.gz",
    "linux_mips_hardfloat.tar.gz",
    "linux_mips_softfloat.tar.gz",
    "linux_mipsle_hardfloat.tar.gz",
    "linux_mipsle_softfloat.tar.gz",
    "linux_mips64_hardfloat.tar.gz",
    "linux_mips64_softfloat.tar.gz",
    "linux_mips64le_hardfloat.tar.gz",
    "linux_mips64le_softfloat.tar.gz",
    "linux_riscv64.tar.gz",
    "windows_i386.zip",
    "windows_x86_64.zip",
    "windows_arm64.zip",
)
CHECKSUM_LINE = re.compile(r"^([0-9a-fA-F]{64})  ([^/\\\r\n]+)$")


def fail(message: str) -> None:
    raise SystemExit(f"release artifact verification failed: {message}")


def parse_checksums(path: Path) -> dict[str, str]:
    raw = path.read_bytes()
    text = raw.replace(b"\r\n", b"\n")
    if b"\r" in text:
        fail("checksums.txt contains a lone carriage return")
    lines = text.decode("utf-8").splitlines()
    if not lines:
        fail("checksums.txt is empty")

    checksums: dict[str, str] = {}
    for number, line in enumerate(lines, 1):
        match = CHECKSUM_LINE.fullmatch(line)
        if match is None:
            fail(f"checksums.txt line {number} has invalid format")
        digest, name = match.groups()
        if name in checksums:
            fail(f"checksums.txt contains duplicate entry {name!r}")
        checksums[name] = digest.lower()
    return checksums


def main() -> None:
    dist = Path(sys.argv[1] if len(sys.argv) > 1 else "dist")
    checksum_path = dist / "checksums.txt"
    if not checksum_path.is_file():
        fail(f"missing {checksum_path}")

    archives = sorted(
        path.name
        for path in dist.iterdir()
        if path.is_file() and (path.name.endswith(".tar.gz") or path.name.endswith(".zip"))
    )
    if len(archives) != len(EXPECTED_SUFFIXES):
        fail(f"found {len(archives)} archives, expected {len(EXPECTED_SUFFIXES)}")

    anchor_suffix = "_android_arm64.tar.gz"
    anchors = [name for name in archives if name.endswith(anchor_suffix)]
    if len(anchors) != 1 or not anchors[0].startswith("dnet_"):
        fail("cannot determine one release version from the Android arm64 archive")
    version = anchors[0][len("dnet_") : -len(anchor_suffix)]
    if not version:
        fail("archive version is empty")

    expected = sorted(f"dnet_{version}_{suffix}" for suffix in EXPECTED_SUFFIXES)
    if archives != expected:
        missing = sorted(set(expected) - set(archives))
        unexpected = sorted(set(archives) - set(expected))
        fail(f"archive contract mismatch; missing={missing}, unexpected={unexpected}")

    checksums = parse_checksums(checksum_path)
    if sorted(checksums) != archives:
        missing = sorted(set(archives) - set(checksums))
        unexpected = sorted(set(checksums) - set(archives))
        fail(f"checksum entries mismatch; missing={missing}, unexpected={unexpected}")

    for name in archives:
        digest = hashlib.sha256()
        with (dist / name).open("rb") as archive:
            for chunk in iter(lambda: archive.read(1024 * 1024), b""):
                digest.update(chunk)
        if digest.hexdigest() != checksums[name]:
            fail(f"SHA-256 mismatch for {name!r}")

    print(f"verified {len(archives)} release archives and checksums for version {version}")


if __name__ == "__main__":
    main()
