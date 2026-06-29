#!/usr/bin/env python3
import argparse
import os
import platform
import shutil
import subprocess
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
EXTENSION = ROOT / "extension"


def main() -> int:
    parser = argparse.ArgumentParser(description="Stage the LDGCC VS Code extension with a bundled Master binary.")
    parser.add_argument("--out", default="dist/ldgcc-extension", help="Output staging directory.")
    args = parser.parse_args()

    output = (ROOT / args.out).resolve()
    if output.exists():
        shutil.rmtree(output)
    output.mkdir(parents=True)

    print("compiling extension", flush=True)
    shutil.rmtree(EXTENSION / "out", ignore_errors=True)
    subprocess.run(["npm", "run", "compile"], cwd=EXTENSION, check=True)

    for name in ["package.json", "package-lock.json", "README.md", "tsconfig.json"]:
        shutil.copy2(EXTENSION / name, output / name)
    for directory in ["out", "resources"]:
        shutil.copytree(EXTENSION / directory, output / directory)

    bin_dir = output / "bin" / platform_key()
    bin_dir.mkdir(parents=True)
    master_binary = bin_dir / executable_name("ldgcc-master")
    print(f"building bundled Master: {master_binary}", flush=True)
    go_env = dict(os.environ)
    go_env.setdefault("GOCACHE", "/tmp/locdist-go-cache")
    subprocess.run(["go", "build", "-o", str(master_binary), "./cmd/master"], cwd=ROOT / "master", check=True, env=go_env)

    print(f"extension stage: {output}", flush=True)
    return 0


def platform_key() -> str:
    return f"{node_platform()}-{node_arch()}"


def node_platform() -> str:
    system = platform.system().lower()
    if system == "darwin":
        return "darwin"
    if system.startswith("win"):
        return "win32"
    return "linux"


def node_arch() -> str:
    machine = platform.machine().lower()
    if machine in {"x86_64", "amd64"}:
        return "x64"
    if machine in {"aarch64", "arm64"}:
        return "arm64"
    return machine


def executable_name(name: str) -> str:
    return name + ".exe" if node_platform() == "win32" else name


if __name__ == "__main__":
    raise SystemExit(main())
