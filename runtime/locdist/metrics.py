import json
import os
import time
from pathlib import Path
from typing import Any


def now_ms() -> float:
    return time.perf_counter() * 1000.0


def metrics_path(filename: str) -> Path:
    configured = os.environ.get("LDGCC_SYNC_METRICS_PATH")
    if configured:
        return Path(configured)
    return Path.cwd() / "logs" / filename


def append_jsonl(filename: str, event: dict[str, Any]) -> None:
    path = metrics_path(filename)
    try:
        path.parent.mkdir(parents=True, exist_ok=True)
        with path.open("a", encoding="utf-8") as file:
            file.write(json.dumps(event, separators=(",", ":"), sort_keys=True) + "\n")
    except OSError:
        pass
