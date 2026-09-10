#!/usr/bin/env python3
"""Check tracked Go sources without changing the checkout."""
import subprocess
import sys

files = subprocess.check_output(["git", "ls-files", "-z", "--", "*.go"]).decode().split("\0")
failed = False
for name in filter(None, files):
    result = subprocess.run(["gofmt", "-l", name], capture_output=True, text=True, check=True)
    if result.stdout:
        print(result.stdout, end="")
        failed = True
if failed:
    print("Run gofmt on the files above and commit the changes.", file=sys.stderr)
    sys.exit(1)
