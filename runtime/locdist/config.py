import json
import os
from pathlib import Path

from locdist.models import RuntimeConfig, CommunicationConfig
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
        "LDGCC_MASTER_HOST": "master_host",
        "LDGCC_MASTER_PORT": "master_port",
        "LDGCC_SYNC_TARGET": "sync_target",
    }
    for environment_name, field_name in environment_fields.items():
        value = os.environ.get(environment_name)
        if value is not None:
            data[field_name] = value

    communication_env = os.environ.get("LDGCC_COMMUNICATION")
    if communication_env:
        try:
            data["communication"] = json.loads(communication_env)
        except json.JSONDecodeError as e:
            raise ConfigError("LDGCC_COMMUNICATION must be JSON") from e

    training_env = os.environ.get("LDGCC_TRAINING")
    if training_env:
        try:
            data["training"] = json.loads(training_env)
        except json.JSONDecodeError as e:
            raise ConfigError("LDGCC_TRAINING must be JSON") from e

    if "worker_port" in data and isinstance(data["worker_port"], str):
        try:
            data["worker_port"] = int(data["worker_port"])
        except ValueError as e:
            raise ConfigError("worker_port must be an integer") from e

    if "master_port" in data and isinstance(data["master_port"], str):
        try:
            data["master_port"] = int(data["master_port"])
        except ValueError as e:
            raise ConfigError("master_port must be an integer") from e

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

    optional_fields = {
        "communication",
        "training",
        "master_host",
        "master_port",
        "sync_target",
    }

    unknown_fields = (
        actual_fields - required_fields - optional_fields
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

    if "master_host" in data and not isinstance(
        data["master_host"],
        str,
    ):
        raise ConfigError(
            "master_host must be a string"
        )

    if "master_port" in data and not isinstance(
        data["master_port"],
        int,
    ):
        raise ConfigError(
            "master_port must be an integer"
        )

    if "sync_target" in data and not isinstance(
        data["sync_target"],
        str,
    ):
        raise ConfigError(
            "sync_target must be a string"
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

    if "master_host" in data and not data["master_host"].strip():
        raise ConfigError(
            "master_host cannot be empty"
        )

    if "master_port" in data and not (
        1 <= data["master_port"] <= 65535
    ):
        raise ConfigError(
            "master_port must be between 1 and 65535"
        )

    if "sync_target" in data and data["sync_target"] not in {
        "worker",
        "master",
    }:
        raise ConfigError(
            "sync_target must be either 'worker' or 'master'"
        )

    # --------------------------------------------------
    # Build RuntimeConfig
    # --------------------------------------------------

    communication = parse_communication_config(
        data.get("communication", {})
    )
    gradient_accumulation_steps = parse_training_config(
        data.get("training", {})
    )

    return RuntimeConfig(
        runtime_version=data["runtime_version"],
        job_id=data["job_id"],
        worker_id=data["worker_id"],
        worker_host=data["worker_host"],
        worker_port=data["worker_port"],
        master_host=data.get("master_host"),
        master_port=data.get("master_port"),
        sync_target=data.get("sync_target", "worker"),
        rpc_timeout_seconds=data[
            "rpc_timeout_seconds"
        ],
        communication=communication,
        gradient_accumulation_steps=gradient_accumulation_steps,
    )


def parse_communication_config(value) -> CommunicationConfig:
    if value is None:
        value = {}
    if not isinstance(value, dict):
        raise ConfigError("communication must be an object")

    precision = value.get("precision", "fp32")
    if precision not in {"fp32", "fp16"}:
        raise ConfigError("communication.precision must be fp32 or fp16")

    compression = value.get("compression", "none")
    if isinstance(compression, str):
        compression_data = {"type": compression}
    elif isinstance(compression, dict):
        compression_data = dict(compression)
    else:
        raise ConfigError("communication.compression must be a string or object")

    compression_type = compression_data.get("type", "none")
    if compression_type not in {"none", "topk"}:
        raise ConfigError("communication.compression.type must be none or topk")

    mode = compression_data.get("mode", "per_layer")
    if "per_layer_top_k" in compression_data:
        mode = "per_layer"
        top_k = compression_data["per_layer_top_k"]
    elif "global_top_k" in compression_data:
        mode = "global"
        top_k = compression_data["global_top_k"]
    else:
        top_k = compression_data.get("top_k", "5%")

    if mode not in {"global", "per_layer"}:
        raise ConfigError("communication.compression.mode must be global or per_layer")

    top_k_percent = parse_percent(top_k)

    selection = compression_data.get("selection", "exact")
    if selection not in {"exact", "sampled_threshold"}:
        raise ConfigError(
            "communication.compression.selection must be exact or sampled_threshold"
        )

    sample_rate_percent = parse_percent(
        compression_data.get("sample_rate", "1%"),
        field_name="communication.compression.sample_rate",
    )

    max_payload_factor = compression_data.get("max_payload_factor", 1.5)
    if not isinstance(max_payload_factor, (int, float)):
        raise ConfigError("communication.compression.max_payload_factor must be numeric")
    max_payload_factor = float(max_payload_factor)
    if max_payload_factor < 1.0:
        raise ConfigError("communication.compression.max_payload_factor must be >= 1.0")

    device = compression_data.get("device", "auto")
    if device not in {"auto", "cpu", "gpu"}:
        raise ConfigError("communication.compression.device must be auto, cpu, or gpu")

    error_feedback = compression_data.get("error_feedback", True)
    if not isinstance(error_feedback, bool):
        raise ConfigError("communication.compression.error_feedback must be a boolean")
    if compression_type == "topk" and not error_feedback:
        raise ConfigError("communication.compression.error_feedback must be true for topk")

    warmup_steps = compression_data.get("warmup_steps", 0)
    if not isinstance(warmup_steps, int) or warmup_steps < 0:
        raise ConfigError("communication.compression.warmup_steps must be a non-negative integer")

    return CommunicationConfig(
        precision=precision,
        compression_type=compression_type,
        compression_mode=mode,
        top_k_percent=top_k_percent,
        selection=selection,
        sample_rate_percent=sample_rate_percent,
        max_payload_factor=max_payload_factor,
        device=device,
        error_feedback=error_feedback,
        warmup_steps=warmup_steps,
    )


def parse_training_config(value) -> int:
    if value is None:
        value = {}
    if not isinstance(value, dict):
        raise ConfigError("training must be an object")

    unknown_fields = set(value.keys()) - {"gradient_accumulation_steps"}
    if unknown_fields:
        raise ConfigError(
            "Unknown training fields: " + ", ".join(sorted(unknown_fields))
        )

    gradient_accumulation_steps = value.get("gradient_accumulation_steps", 1)
    if not isinstance(gradient_accumulation_steps, int):
        raise ConfigError("training.gradient_accumulation_steps must be an integer")
    if gradient_accumulation_steps <= 0:
        raise ConfigError("training.gradient_accumulation_steps must be positive")
    return gradient_accumulation_steps


def parse_percent(value, field_name: str = "communication.compression.top_k") -> float:
    if isinstance(value, str):
        text = value.strip()
        if not text.endswith("%"):
            raise ConfigError(f"{field_name} must be a percent string")
        text = text[:-1].strip()
        try:
            percent = float(text)
        except ValueError as e:
            raise ConfigError(f"{field_name} must be numeric") from e
    elif isinstance(value, (int, float)):
        percent = float(value)
    else:
        raise ConfigError(f"{field_name} must be a percent")

    if percent <= 0 or percent > 100:
        raise ConfigError(f"{field_name} must be > 0% and <= 100%")
    return percent
