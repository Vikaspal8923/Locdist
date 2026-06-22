from locdist.config import load_config


def main():

    config = load_config()

    print(config)

    assert config.runtime_version == 1

    assert config.job_id == "job-123"

    assert config.worker_id == "worker-a"

    assert config.worker_host == "127.0.0.1"

    assert config.worker_port == 7000

    assert config.rpc_timeout_seconds == 120

    print("✓ Config test passed")


if __name__ == "__main__":
    main()