#!/usr/bin/env python3
import argparse
import json
import os
import platform
import shutil
import subprocess
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]


def main() -> int:
    parser = argparse.ArgumentParser(description="Build local LDGCC production binaries.")
    parser.add_argument("--out", default="dist/ldgcc-local", help="Output directory.")
    args = parser.parse_args()

    output = (ROOT / args.out).resolve()
    if output.exists():
        shutil.rmtree(output)
    bin_dir = output / "bin"
    bin_dir.mkdir(parents=True)

    go_env = dict(os.environ)
    go_env.setdefault("GOCACHE", "/tmp/locdist-go-cache")

    builds = [
        ("master", ROOT / "master", "./cmd/master", executable_name("ldgcc-master")),
        ("worker-app", ROOT / "worker", "./cmd/worker-app", executable_name("ldgcc-worker-app")),
        ("worker", ROOT / "worker", "./cmd/worker", executable_name("ldgcc-worker")),
    ]
    for name, cwd, package, binary in builds:
        target = bin_dir / binary
        print(f"building {name}: {target}", flush=True)
        subprocess.run(["go", "build", "-o", str(target), package], cwd=cwd, check=True, env=go_env)

    manifest = {
        "name": "ldgcc-local",
        "platform": platform.system().lower(),
        "machine": platform.machine(),
        "binaries": {
            "master": str((bin_dir / executable_name("ldgcc-master")).relative_to(output)),
            "worker_app": str((bin_dir / executable_name("ldgcc-worker-app")).relative_to(output)),
            "worker": str((bin_dir / executable_name("ldgcc-worker")).relative_to(output)),
        },
    }
    (output / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
    print(f"release output: {output}", flush=True)
    return 0


def executable_name(name: str) -> str:
    if platform.system().lower().startswith("win"):
        return name + ".exe"
    return name


if __name__ == "__main__":
    raise SystemExit(main())
