#!/usr/bin/env python3
import argparse
import json
import os
import platform
import shutil
import subprocess
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
WORKER = ROOT / "worker"


def main() -> int:
    parser = argparse.ArgumentParser(description="Stage the LDGCC Worker App package.")
    parser.add_argument("--out", default="dist/ldgcc-worker-app", help="Output staging directory.")
    parser.add_argument("--target", default=platform_key(), help="Target platform, for example linux-x64 or windows-x64.")
    args = parser.parse_args()

    output = (ROOT / args.out).resolve()
    if output.exists():
        shutil.rmtree(output)
    output.mkdir(parents=True)

    target = parse_target(args.target)
    platform_dir = target["platform_key"]
    bin_dir = output / "bin" / platform_dir
    bin_dir.mkdir(parents=True)

    go_env = dict(os.environ)
    go_env.setdefault("GOCACHE", "/tmp/locdist-go-cache")
    go_env["GOOS"] = target["goos"]
    go_env["GOARCH"] = target["goarch"]

    worker_app = bin_dir / executable_name("ldgcc-worker-app", target)
    worker_service = bin_dir / executable_name("ldgcc-worker", target)
    print(f"building Worker App: {worker_app}", flush=True)
    subprocess.run(["go", "build", "-o", str(worker_app), "./cmd/worker-app"], cwd=WORKER, check=True, env=go_env)
    print(f"building Worker service: {worker_service}", flush=True)
    subprocess.run(["go", "build", "-o", str(worker_service), "./cmd/worker"], cwd=WORKER, check=True, env=go_env)

    write_readme(output, target)
    write_launcher(output, target)
    write_manifest(output, target)

    print(f"Worker App stage: {output}", flush=True)
    return 0


def write_readme(output: Path, target: dict) -> None:
    launcher = "run-worker-app.bat" if target["node_platform"] == "win32" else "run-worker-app.sh"
    run_block = f"{launcher}" if target["node_platform"] == "win32" else f"./{launcher}"
    default_data_dir = "%USERPROFILE%\\.ldgcc\\worker" if target["node_platform"] == "win32" else "~/.ldgcc/worker"
    override_block = (
        "set LDGCC_WORKER_DATA_DIR=C:\\\\ldgcc-worker\r\nrun-worker-app.bat"
        if target["node_platform"] == "win32"
        else "LDGCC_WORKER_DATA_DIR=/custom/path ./run-worker-app.sh"
    )
    (output / "README.md").write_text(
        "# LDGCC Worker App Package\n\n"
        "This package is for the worker laptop. It starts the local LDGCC Worker App,\n"
        "which lets the user make the machine discoverable, accept pairing, reset a\n"
        "previous Master connection, and view Worker settings.\n\n"
        "## Run\n\n"
        "```bash\n"
        f"{run_block}\n"
        "```\n\n"
        "Then open the URL printed by the app, usually:\n\n"
        "```text\n"
        "http://127.0.0.1:5050\n"
        "```\n\n"
        "## Worker User Flow\n\n"
        "```text\n"
        "Run Worker App\n"
        "    -> open local Worker page\n"
        "    -> click Start Worker\n"
        "    -> accept pairing request from Master\n"
        "    -> wait while setup/training runs\n"
        "```\n\n"
        "## Local Data\n\n"
        "By default, the launcher stores Worker state under:\n\n"
        "```text\n"
        f"{default_data_dir}\n"
        "```\n\n"
        "Override with:\n\n"
        "```bash\n"
        f"{override_block}\n"
        "```\n\n"
        "## Binaries\n\n"
        "```text\n"
        f"bin/{target['platform_key']}/{executable_name('ldgcc-worker-app', target)}\n"
        f"bin/{target['platform_key']}/{executable_name('ldgcc-worker', target)}\n"
        "```\n",
        encoding="utf-8",
    )


def write_launcher(output: Path, target: dict) -> None:
    platform_dir = target["platform_key"]
    if target["node_platform"] == "win32":
        launcher = output / "run-worker-app.bat"
        launcher.write_text(
            "@echo off\r\n"
            "setlocal\r\n"
            "set APP_DIR=%~dp0\r\n"
            "if \"%LDGCC_WORKER_DATA_DIR%\"==\"\" set LDGCC_WORKER_DATA_DIR=%USERPROFILE%\\.ldgcc\\worker\r\n"
            "if \"%LDGCC_WORKER_CONFIG%\"==\"\" set LDGCC_WORKER_CONFIG=%LDGCC_WORKER_DATA_DIR%\\worker_config.json\r\n"
            "if not exist \"%LDGCC_WORKER_DATA_DIR%\" mkdir \"%LDGCC_WORKER_DATA_DIR%\"\r\n"
            f"\"%APP_DIR%bin\\{platform_dir}\\{executable_name('ldgcc-worker-app', target)}\" --config \"%LDGCC_WORKER_CONFIG%\" --data-dir \"%LDGCC_WORKER_DATA_DIR%\"\r\n",
            encoding="utf-8",
        )
        return

    launcher = output / "run-worker-app.sh"
    launcher.write_text(
        "#!/usr/bin/env sh\n"
        "set -eu\n"
        "APP_DIR=$(CDPATH= cd -- \"$(dirname -- \"$0\")\" && pwd)\n"
        "DATA_DIR=${LDGCC_WORKER_DATA_DIR:-$HOME/.ldgcc/worker}\n"
        "CONFIG_PATH=${LDGCC_WORKER_CONFIG:-$DATA_DIR/worker_config.json}\n"
        "mkdir -p \"$DATA_DIR\"\n"
        f"exec \"$APP_DIR/bin/{platform_dir}/{executable_name('ldgcc-worker-app', target)}\" --config \"$CONFIG_PATH\" --data-dir \"$DATA_DIR\"\n",
        encoding="utf-8",
    )
    launcher.chmod(0o755)


def write_manifest(output: Path, target: dict) -> None:
    platform_dir = target["platform_key"]
    default_data_dir = "%USERPROFILE%\\.ldgcc\\worker" if target["node_platform"] == "win32" else "~/.ldgcc/worker"
    manifest = {
        "name": "ldgcc-worker-app",
        "platform": platform_dir,
        "binaries": {
            "worker_app": f"bin/{platform_dir}/{executable_name('ldgcc-worker-app', target)}",
            "worker": f"bin/{platform_dir}/{executable_name('ldgcc-worker', target)}",
        },
        "launcher": "run-worker-app.bat" if target["node_platform"] == "win32" else "run-worker-app.sh",
        "default_data_dir": default_data_dir,
    }
    (output / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")


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


def parse_target(value: str) -> dict:
    targets = {
        "linux-x64": {"platform_key": "linux-x64", "node_platform": "linux", "node_arch": "x64", "goos": "linux", "goarch": "amd64"},
        "linux-arm64": {"platform_key": "linux-arm64", "node_platform": "linux", "node_arch": "arm64", "goos": "linux", "goarch": "arm64"},
        "windows-x64": {"platform_key": "win32-x64", "node_platform": "win32", "node_arch": "x64", "goos": "windows", "goarch": "amd64"},
        "win32-x64": {"platform_key": "win32-x64", "node_platform": "win32", "node_arch": "x64", "goos": "windows", "goarch": "amd64"},
    }
    if value not in targets:
        raise SystemExit(f"unsupported target {value!r}; choose one of: {', '.join(sorted(targets))}")
    return targets[value]


def executable_name(name: str, target: dict) -> str:
    return name + ".exe" if target["node_platform"] == "win32" else name


if __name__ == "__main__":
    raise SystemExit(main())
