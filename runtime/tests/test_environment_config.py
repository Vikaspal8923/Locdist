import json
import os
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from locdist.config import load_config


ENVIRONMENT = {
    "LDGCC_JOB_ID": "job-production",
    "LDGCC_WORKER_ID": "worker-production",
    "LDGCC_WORKER_HOST": "127.0.0.1",
    "LDGCC_WORKER_PORT": "51000",
}


class EnvironmentConfigTests(unittest.TestCase):
    def test_environment_configuration_without_file(self):
        with tempfile.TemporaryDirectory() as directory:
            with patch.dict(os.environ, ENVIRONMENT, clear=False):
                config = load_config(str(Path(directory) / "missing.json"))

        self.assertEqual(config.job_id, "job-production")
        self.assertEqual(config.worker_id, "worker-production")
        self.assertEqual(config.worker_port, 51000)
        self.assertEqual(config.gradient_accumulation_steps, 1)

    def test_environment_overrides_json(self):
        with tempfile.TemporaryDirectory() as directory:
            config_path = Path(directory) / "locdist_config.json"
            config_path.write_text(
                json.dumps(
                    {
                        "runtime_version": 1,
                        "job_id": "old",
                        "worker_id": "old",
                        "worker_host": "old",
                        "worker_port": 1,
                        "rpc_timeout_seconds": 120,
                    }
                ),
                encoding="utf-8",
            )
            with patch.dict(os.environ, ENVIRONMENT, clear=False):
                config = load_config(str(config_path))

        self.assertEqual(config.job_id, "job-production")
        self.assertEqual(config.worker_id, "worker-production")

    def test_training_gradient_accumulation_is_loaded(self):
        with tempfile.TemporaryDirectory() as directory:
            config_path = Path(directory) / "locdist_config.json"
            config_path.write_text(
                json.dumps(
                    {
                        "runtime_version": 1,
                        "job_id": "job-production",
                        "worker_id": "worker-production",
                        "worker_host": "127.0.0.1",
                        "worker_port": 51000,
                        "rpc_timeout_seconds": 120,
                        "training": {
                            "gradient_accumulation_steps": 10,
                        },
                    }
                ),
                encoding="utf-8",
            )
            config = load_config(str(config_path))

        self.assertEqual(config.gradient_accumulation_steps, 10)

    def test_environment_training_overrides_json(self):
        with tempfile.TemporaryDirectory() as directory:
            config_path = Path(directory) / "locdist_config.json"
            config_path.write_text(
                json.dumps(
                    {
                        "runtime_version": 1,
                        "job_id": "job-production",
                        "worker_id": "worker-production",
                        "worker_host": "127.0.0.1",
                        "worker_port": 51000,
                        "rpc_timeout_seconds": 120,
                        "training": {
                            "gradient_accumulation_steps": 2,
                        },
                    }
                ),
                encoding="utf-8",
            )
            with patch.dict(
                os.environ,
                {"LDGCC_TRAINING": json.dumps({"gradient_accumulation_steps": 10})},
                clear=False,
            ):
                config = load_config(str(config_path))

        self.assertEqual(config.gradient_accumulation_steps, 10)


if __name__ == "__main__":
    unittest.main()
