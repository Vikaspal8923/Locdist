#!/usr/bin/env python3
import argparse
import json
import os
import shutil
import signal
import socket
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]


def main() -> int:
    parser = argparse.ArgumentParser(description="Run a local LDGCC end-to-end smoke validation.")
    parser.add_argument("--keep", action="store_true", help="Keep the temporary validation directory.")
    parser.add_argument("--timeout", type=int, default=180, help="Operation timeout in seconds.")
    args = parser.parse_args()

    temp_root = Path(tempfile.mkdtemp(prefix="ldgcc-e2e-"))
    log_dir = temp_root / "logs"
    log_dir.mkdir()
    processes: list[subprocess.Popen] = []
    try:
        info(f"validation root: {temp_root}")
        master_binary, worker_binary = build_binaries(temp_root)
        processes.append(start_master(temp_root, master_binary, log_dir))
        session = wait_for_master_session(temp_root / "master-data" / "master-session.json", args.timeout)
        processes.append(start_worker(temp_root, worker_binary, log_dir))

        post_json(worker_app_url(temp_root, "/api/start"))
        discovered = wait_for_discovered_worker(session, "Local-E2E-Worker", args.timeout)
        info(f"discovered worker: {discovered['instance']} at {discovered['address']}")

        post_json(master_url(session, f"/workers/{urllib.parse.quote(discovered['instance'])}/pair"), session)
        wait_for_worker_pending_pairing(temp_root, args.timeout)
        post_json(worker_app_url(temp_root, "/api/pairing/accept"))
        registered = wait_for_registered_worker(session, args.timeout)
        worker_id = registered["worker_id"]
        info(f"worker paired and online: {worker_id}")

        project_root = create_project(temp_root)
        post_json(master_url(session, "/jobs/prepare"), session, {"project_root": str(project_root)})
        job = wait_for_job(session, "prepared", args.timeout)
        info(f"prepared job: {job['job_id']}")

        request_setup(session, args.timeout)
        wait_for_setup_ready(session, args.timeout)
        info("worker setup ready")

        post_json(master_url(session, "/jobs/start"), session)
        summary = wait_for_last_summary(session, args.timeout)
        if summary.get("status") != "finished":
            raise RuntimeError(f"job did not finish successfully: {summary}")

        result = get_json(master_url(session, f"/results/{summary['job_id']}"), session)
        result_path = Path(result["path"])
        expected_output = result_path / "workers" / worker_id / "outputs" / "result.txt"
        if not expected_output.exists():
            raise RuntimeError(f"expected collected output is missing: {expected_output}")
        info(f"results collected: {result_path}")
        info("LDGCC local end-to-end validation passed")
        return 0
    finally:
        for process in reversed(processes):
            stop_process(process)
        if args.keep:
            info(f"kept validation root: {temp_root}")
        else:
            shutil.rmtree(temp_root, ignore_errors=True)


def build_binaries(temp_root: Path) -> tuple[Path, Path]:
    bin_dir = temp_root / "bin"
    bin_dir.mkdir(parents=True)
    master_binary = bin_dir / "ldgcc-master"
    worker_binary = bin_dir / "ldgcc-worker-app"
    run(["go", "build", "-o", str(master_binary), "./cmd/master"], ROOT / "master")
    run(["go", "build", "-o", str(worker_binary), "./cmd/worker-app"], ROOT / "worker")
    return master_binary, worker_binary


def start_master(temp_root: Path, binary: Path, log_dir: Path) -> subprocess.Popen:
    master_dir = temp_root / "master-run"
    data_dir = temp_root / "master-data"
    master_dir.mkdir()
    data_dir.mkdir()
    config = {
        "master_id": "master-e2e",
        "master_name": "LDGCC Local E2E",
        "host": "127.0.0.1",
        "grpc_port": str(free_port()),
        "app_host": "127.0.0.1",
        "app_port": str(free_port()),
        "pairing_path": "master_pairings.json",
    }
    write_json(master_dir / "master_config.json", config)
    log = (log_dir / "master.log").open("w", encoding="utf-8")
    return subprocess.Popen(
        [
            str(binary),
            "--config",
            str(master_dir / "master_config.json"),
            "--data-dir",
            str(data_dir),
            "--app-host",
            "127.0.0.1",
            "--app-port",
            config["app_port"],
        ],
        cwd=master_dir,
        stdout=log,
        stderr=subprocess.STDOUT,
    )


def start_worker(temp_root: Path, binary: Path, log_dir: Path) -> subprocess.Popen:
    worker_dir = temp_root / "worker-run"
    worker_dir.mkdir()
    config = {
        "worker_name": "Local-E2E-Worker",
        "grpc_port": str(free_port()),
        "app_port": str(free_port()),
        "pairing_path": "pairing.json",
        "workspace_root": "workspaces",
        "host": "127.0.0.1",
        "master_host": "",
        "master_port": "",
    }
    write_json(worker_dir / "worker_config.json", config)
    write_json(temp_root / "worker-app.json", {"app_port": config["app_port"]})
    log = (log_dir / "worker-app.log").open("w", encoding="utf-8")
    return subprocess.Popen([str(binary)], cwd=worker_dir, stdout=log, stderr=subprocess.STDOUT)


def create_project(temp_root: Path) -> Path:
    project = temp_root / "project"
    dataset = project / "dataset"
    dataset.mkdir(parents=True)
    (project / "train.py").write_text(
        "from pathlib import Path\nPath('result.txt').write_text('ok\\n', encoding='utf-8')\n",
        encoding="utf-8",
    )
    (dataset / "train.jsonl").write_text('{"text":"a"}\n{"text":"b"}\n', encoding="utf-8")
    (project / "ldgcc.yml").write_text(
        "entrypoint: train.py\n"
        "dataset:\n"
        "  train: dataset/train.jsonl\n"
        "  type: jsonl\n"
        "workers:\n"
        "  count: 1\n"
        "outputs:\n"
        "  - result.txt\n",
        encoding="utf-8",
    )
    return project


def wait_for_master_session(path: Path, timeout: int) -> dict:
    return wait_for(lambda: read_json(path) if path.exists() else None, timeout, "Master session")


def wait_for_discovered_worker(session: dict, instance: str, timeout: int) -> dict:
    def check():
        state = get_json(master_url(session, "/state"), session)
        for worker in state.get("discovered_workers", []):
            if worker.get("instance") == instance:
                return worker
        return None

    return wait_for(check, timeout, "discovered Worker")


def wait_for_worker_pending_pairing(temp_root: Path, timeout: int) -> dict:
    return wait_for(lambda: get_json(worker_app_url(temp_root, "/api/state")).get("pending_pairing"), timeout, "Worker pending pairing")


def wait_for_registered_worker(session: dict, timeout: int) -> dict:
    def check():
        state = get_json(master_url(session, "/state"), session)
        for worker in state.get("workers") or []:
            if worker.get("availability") == "ONLINE":
                return worker
        return None

    return wait_for(check, timeout, "registered online Worker")


def wait_for_job(session: dict, status: str, timeout: int) -> dict:
    def check():
        state = get_json(master_url(session, "/state"), session)
        job = state.get("job")
        return job if job and job.get("status") == status else None

    return wait_for(check, timeout, f"job status {status}")


def wait_for_setup_ready(session: dict, timeout: int) -> dict:
    def check():
        state = get_json(master_url(session, "/state"), session)
        job = state.get("job") or {}
        setup = job.get("setup") or {}
        return job if setup and all(value.get("status") == "JOB_SETUP_STATUS_READY" for value in setup.values()) else None

    return wait_for(check, timeout, "Worker setup ready")


def request_setup(session: dict, timeout: int) -> None:
    deadline = time.time() + timeout
    attempt = 1
    while time.time() < deadline:
        info(f"requesting worker setup (attempt {attempt})")
        post_json(master_url(session, "/jobs/setup"), session)
        try:
            wait_for(lambda: setup_has_started(session), min(5, max(1, int(deadline - time.time()))), "Worker setup start")
            return
        except RuntimeError:
            attempt += 1
    raise RuntimeError("timed out requesting Worker setup")


def setup_has_started(session: dict) -> bool:
    state = get_json(master_url(session, "/state"), session)
    job = state.get("job") or {}
    setup = job.get("setup") or {}
    if not setup:
        return False
    statuses = [value.get("status") for value in setup.values()]
    return any(status != "JOB_SETUP_STATUS_WORKSPACE_RECEIVED" for status in statuses)


def wait_for_last_summary(session: dict, timeout: int) -> dict:
    return wait_for(lambda: get_json(master_url(session, "/state"), session).get("last_summary"), timeout, "last job summary")


def wait_for(callback, timeout: int, label: str):
    deadline = time.time() + timeout
    last_error = None
    while time.time() < deadline:
        try:
            result = callback()
            if result:
                return result
        except Exception as error:
            last_error = error
        time.sleep(0.5)
    if last_error:
        raise RuntimeError(f"timed out waiting for {label}: {last_error}") from last_error
    raise RuntimeError(f"timed out waiting for {label}")


def master_url(session: dict, path: str) -> str:
    return session["address"].rstrip("/") + path


def worker_app_url(temp_root: Path, path: str) -> str:
    app = read_json(temp_root / "worker-app.json")
    return f"http://127.0.0.1:{app['app_port']}{path}"


def get_json(url: str, session: dict | None = None) -> dict:
    request = urllib.request.Request(url, headers=headers(session))
    with urllib.request.urlopen(request, timeout=5) as response:
        return json.loads(response.read().decode("utf-8"))


def post_json(url: str, session: dict | None = None, body: dict | None = None) -> dict:
    payload = json.dumps(body).encode("utf-8") if body is not None else b""
    request = urllib.request.Request(url, data=payload, method="POST", headers=headers(session, body is not None))
    try:
        with urllib.request.urlopen(request, timeout=10) as response:
            return json.loads(response.read().decode("utf-8"))
    except urllib.error.HTTPError as error:
        detail = error.read().decode("utf-8", errors="replace")
        raise RuntimeError(f"POST {url} failed: HTTP {error.code}: {detail}") from error


def headers(session: dict | None, has_body: bool = False) -> dict:
    result = {}
    if session:
        result["Authorization"] = "Bearer " + session["session_token"]
    if has_body:
        result["Content-Type"] = "application/json"
    return result


def free_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


def write_json(path: Path, value: dict) -> None:
    path.write_text(json.dumps(value, indent=2) + "\n", encoding="utf-8")


def read_json(path: Path) -> dict:
    return json.loads(path.read_text(encoding="utf-8"))


def run(command: list[str], cwd: Path) -> None:
    info("+ " + " ".join(command))
    env = dict(os.environ)
    env.setdefault("GOCACHE", "/tmp/locdist-go-cache")
    subprocess.run(command, cwd=cwd, check=True, env=env)


def info(message: str) -> None:
    print(message, flush=True)


def stop_process(process: subprocess.Popen) -> None:
    if process.poll() is not None:
        return
    process.send_signal(signal.SIGTERM)
    try:
        process.wait(timeout=10)
    except subprocess.TimeoutExpired:
        process.kill()
        process.wait(timeout=5)


if __name__ == "__main__":
    sys.exit(main())
