#!/usr/bin/env python3
import argparse
import os
import shutil
import subprocess
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
EXTENSION = ROOT / "extension"


def main() -> int:
    parser = argparse.ArgumentParser(description="Stage the LDGCC VS Code extension with a bundled Master binary.")
    parser.add_argument("--out", default="dist/ldgcc-extension", help="Output staging directory.")
    parser.add_argument(
        "--target",
        action="append",
        choices=["linux-x64", "win32-x64"],
        help="Master platform to bundle. Repeat for multiple targets. Defaults to linux-x64 and win32-x64.",
    )
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

    targets = args.target or ["linux-x64", "win32-x64"]
    for target in targets:
        goos, goarch = go_target(target)
        bin_dir = output / "bin" / target
        bin_dir.mkdir(parents=True)
        master_binary = bin_dir / executable_name("ldgcc-master", target)
        print(f"building bundled Master for {target}: {master_binary}", flush=True)
        go_env = dict(os.environ)
        go_env.setdefault("GOCACHE", "/tmp/locdist-go-cache")
        go_env["GOOS"] = goos
        go_env["GOARCH"] = goarch
        go_env.setdefault("CGO_ENABLED", "0")
        subprocess.run(["go", "build", "-o", str(master_binary), "./cmd/master"], cwd=ROOT / "master", check=True, env=go_env)

    print(f"extension stage: {output}", flush=True)
    return 0


def go_target(target: str) -> tuple[str, str]:
    if target == "linux-x64":
        return "linux", "amd64"
    if target == "win32-x64":
        return "windows", "amd64"
    raise ValueError(f"Unsupported extension target: {target}")


def executable_name(name: str, target: str) -> str:
    return name + ".exe" if target.startswith("win32-") else name


if __name__ == "__main__":
    raise SystemExit(main())
