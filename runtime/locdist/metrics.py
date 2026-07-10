import json
import os
import time
from pathlib import Path
from typing import Any


def now_ms() -> float:
    return time.perf_counter() * 1000.0


def estimate_transfer_ms(
    total_bytes: int,
    link_mbps: float | None,
) -> float:
    if link_mbps is None or link_mbps <= 0 or total_bytes <= 0:
        return 0.0
    bits = float(total_bytes) * 8.0
    seconds = bits / (link_mbps * 1_000_000.0)
    return seconds * 1000.0


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
