#!/usr/bin/env python3
"""Append usage beacon markdown to repo docs (idempotent)."""
from __future__ import annotations

import os

BEACON = "\n\n<!-- quantmesh usage beacon -->\n![](https://um.facev.app/p/IiDQJEIGM)\n"
MARKER = "um.facev.app/p/IiDQJEIGM"

SKIP_DIR_NAMES = {".git", "node_modules", "dist", ".venv", "__pycache__"}


def should_skip_dir(path: str) -> bool:
    p = path.replace("\\", "/")
    if "/reports/" in p + "/":
        return True
    return False


def process_file(path: str) -> bool:
    with open(path, encoding="utf-8") as f:
        content = f.read()
    if MARKER in content:
        return False
    with open(path, "w", encoding="utf-8") as f:
        f.write(content.rstrip() + BEACON)
    return True


def main() -> None:
    root = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
    os.chdir(root)
    changed = 0

    for rel in ("README.md", "CONTRIBUTING.md"):
        p = os.path.join(root, rel)
        if os.path.isfile(p) and process_file(p):
            changed += 1

    for top in ("docs", "rdocs", "scripts", "plugin", "monitoring", "backtest", "webui"):
        base = os.path.join(root, top)
        if not os.path.isdir(base):
            continue
        for dirpath, dirnames, filenames in os.walk(base):
            dirnames[:] = [d for d in dirnames if d not in SKIP_DIR_NAMES and not d.startswith(".")]
            if should_skip_dir(dirpath):
                continue
            for name in filenames:
                if not name.endswith(".md"):
                    continue
                path = os.path.join(dirpath, name)
                if should_skip_dir(path):
                    continue
                if process_file(path):
                    changed += 1

    print(f"usage beacon: updated {changed} markdown file(s)")


if __name__ == "__main__":
    main()
