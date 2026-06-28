import json
import os
from pathlib import Path

from locdist.models import RuntimeConfig
from locdist.exceptions import ConfigError


DEFAULT_CONFIG_FILE = "locdist_config.json"


def load_config(
    config_path: str = DEFAULT_CONFIG_FILE,
) -> RuntimeConfig:
    """
    Load and validate LDGCC Runtime V1 configuration.
    """

    path = Path(config_path)

    if path.exists() and not path.is_file():
        raise ConfigError(
            f"Configuration path is not a file: {path}"
        )

    # --------------------------------------------------
    # Parse JSON
    # --------------------------------------------------

    data = {}
    if path.exists():
        try:
            with open(path, "r", encoding="utf-8") as f:
                data = json.load(f)
        except json.JSONDecodeError as e:
            raise ConfigError(f"Invalid JSON: {e}") from e

    # Worker-injected values override the development JSON file. Defaults
    # allow production jobs to run without copying locdist_config.json.
    if not path.exists():
        data.update({"runtime_version": 1, "rpc_timeout_seconds": 120})

    environment_fields = {
        "LDGCC_JOB_ID": "job_id",
        "LDGCC_WORKER_ID": "worker_id",
        "LDGCC_WORKER_HOST": "worker_host",
        "LDGCC_WORKER_PORT": "worker_port",
    }
    for environment_name, field_name in environment_fields.items():
        value = os.environ.get(environment_name)
        if value is not None:
            data[field_name] = value

    if "worker_port" in data and isinstance(data["worker_port"], str):
        try:
            data["worker_port"] = int(data["worker_port"])
        except ValueError as e:
            raise ConfigError("worker_port must be an integer") from e

    # --------------------------------------------------
    # Required schema
    # --------------------------------------------------

    required_fields = {
        "runtime_version",
        "job_id",
        "worker_id",
        "worker_host",
        "worker_port",
        "rpc_timeout_seconds",
    }

    actual_fields = set(data.keys())

    missing_fields = (
        required_fields - actual_fields
    )

    if missing_fields:

        raise ConfigError(
            "Missing required configuration fields: "
            + ", ".join(
                sorted(missing_fields)
            )
        )

    unknown_fields = (
        actual_fields - required_fields
    )

    if unknown_fields:

        raise ConfigError(
            "Unknown configuration fields: "
            + ", ".join(
                sorted(unknown_fields)
            )
        )

    # --------------------------------------------------
    # Type validation
    # --------------------------------------------------

    if not isinstance(
        data["runtime_version"],
        int,
    ):
        raise ConfigError(
            "runtime_version must be an integer"
        )

    if not isinstance(
        data["job_id"],
        str,
    ):
        raise ConfigError(
            "job_id must be a string"
        )

    if not isinstance(
        data["worker_id"],
        str,
    ):
        raise ConfigError(
            "worker_id must be a string"
        )

    if not isinstance(
        data["worker_host"],
        str,
    ):
        raise ConfigError(
            "worker_host must be a string"
        )

    if not isinstance(
        data["worker_port"],
        int,
    ):
        raise ConfigError(
            "worker_port must be an integer"
        )

    if not isinstance(
        data["rpc_timeout_seconds"],
        int,
    ):
        raise ConfigError(
            "rpc_timeout_seconds must be an integer"
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

    if not data["worker_host"].strip():

        raise ConfigError(
            "worker_host cannot be empty"
        )

    if not (
        1 <= data["worker_port"] <= 65535
    ):

        raise ConfigError(
            "worker_port must be between 1 and 65535"
        )

    if data["rpc_timeout_seconds"] <= 0:

        raise ConfigError(
            "rpc_timeout_seconds must be positive"
        )

    # --------------------------------------------------
    # Build RuntimeConfig
    # --------------------------------------------------

    return RuntimeConfig(
        runtime_version=data["runtime_version"],
        job_id=data["job_id"],
        worker_id=data["worker_id"],
        worker_host=data["worker_host"],
        worker_port=data["worker_port"],
        rpc_timeout_seconds=data[
            "rpc_timeout_seconds"
        ],
    )
