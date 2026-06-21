# runtime/locdist/config.py

import json
from pathlib import Path

from locdist.models import RuntimeConfig


DEFAULT_CONFIG_FILE = "locdist_config.json"


class ConfigError(Exception):
    """Raised when runtime configuration is invalid."""


def load_config(
    config_path: str = DEFAULT_CONFIG_FILE
) -> RuntimeConfig:
    """
    Load and validate LDGCC Runtime V1 configuration.
    """

    path = Path(config_path)

    # --------------------------------------------------
    # File checks
    # --------------------------------------------------

    if not path.exists():
        raise ConfigError(
            f"Configuration file does not exist: {path}"
        )

    if not path.is_file():
        raise ConfigError(
            f"Configuration path is not a file: {path}"
        )

    # --------------------------------------------------
    # Parse JSON
    # --------------------------------------------------

    try:
        with open(path, "r", encoding="utf-8") as f:
            data = json.load(f)

    except json.JSONDecodeError as e:
        raise ConfigError(
            f"Invalid JSON: {e}"
        ) from e

    # --------------------------------------------------
    # Required schema
    # --------------------------------------------------

    required_fields = {
        "runtime_version",
        "job_id",
        "worker_id",
        "master_host",
        "master_port",
    }

    actual_fields = set(data.keys())

    # Missing fields

    missing_fields = required_fields - actual_fields

    if missing_fields:
        raise ConfigError(
            "Missing required configuration fields: "
            + ", ".join(sorted(missing_fields))
        )

    # Unknown fields

    unknown_fields = actual_fields - required_fields

    if unknown_fields:
        raise ConfigError(
            "Unknown configuration fields: "
            + ", ".join(sorted(unknown_fields))
        )

    # --------------------------------------------------
    # Type validation
    # --------------------------------------------------

    if not isinstance(data["runtime_version"], int):
        raise ConfigError(
            "runtime_version must be an integer"
        )

    if not isinstance(data["job_id"], str):
        raise ConfigError(
            "job_id must be a string"
        )

    if not isinstance(data["worker_id"], str):
        raise ConfigError(
            "worker_id must be a string"
        )

    if not isinstance(data["master_host"], str):
        raise ConfigError(
            "master_host must be a string"
        )

    if not isinstance(data["master_port"], int):
        raise ConfigError(
            "master_port must be an integer"
        )

    # --------------------------------------------------
    # Value validation
    # --------------------------------------------------

    if data["runtime_version"] != 1:
        raise ConfigError(
            f"Unsupported runtime_version: "
            f"{data['runtime_version']}"
        )

    if not data["job_id"].strip():
        raise ConfigError(
            "job_id cannot be empty"
        )

    if not data["worker_id"].strip():
        raise ConfigError(
            "worker_id cannot be empty"
        )

    if not data["master_host"].strip():
        raise ConfigError(
            "master_host cannot be empty"
        )

    if not (1 <= data["master_port"] <= 65535):
        raise ConfigError(
            "master_port must be between 1 and 65535"
        )

    # --------------------------------------------------
    # Build RuntimeConfig
    # --------------------------------------------------

    return RuntimeConfig(
        runtime_version=data["runtime_version"],
        job_id=data["job_id"],
        worker_id=data["worker_id"],
        master_host=data["master_host"],
        master_port=data["master_port"],
    )