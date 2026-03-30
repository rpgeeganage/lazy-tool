#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
from pathlib import Path

def load(path: Path) -> dict:
    return json.loads(path.read_text(encoding="utf-8"))

def hit_count(obj: dict) -> int:
    return len(obj.get("results", []))

def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--echo", required=True)
    ap.add_argument("--prompt", required=True)
    ap.add_argument("--resource", required=True)
    args = ap.parse_args()

    checks = {
        "echo": load(Path(args.echo)),
        "prompt": load(Path(args.prompt)),
        "resource": load(Path(args.resource)),
    }

    failed = []
    for name, obj in checks.items():
        count = hit_count(obj)
        if count <= 0:
            failed.append(f"{name}: 0 results")

    if failed:
        raise SystemExit("Preflight failed: " + "; ".join(failed))

    print("Preflight passed: echo/prompt/resource searches returned hits.")

if __name__ == "__main__":
    main()
