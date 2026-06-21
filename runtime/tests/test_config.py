from locdist.config import load_config


def main():

    config = load_config()

    print(config)

    assert config.runtime_version == 1
    assert config.job_id == "job-123"
    assert config.worker_id == "worker-2"
    assert config.master_host == "192.168.1.10"
    assert config.master_port == 50051

    print("✓ Config test passed")


if __name__ == "__main__":
    main()