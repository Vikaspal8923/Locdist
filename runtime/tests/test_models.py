from locdist.models import (
    CommunicationConfig,
    RuntimeConfig,
    ParameterMetadata,
    GradientChunk,
    GradientPackage,
)


def main():

    config = RuntimeConfig(
        runtime_version=1,
        job_id="job-123",
        worker_id="worker-1",
        worker_host="127.0.0.1",
        worker_port=7000,
        rpc_timeout_seconds=120,
        communication=CommunicationConfig(),
        gradient_accumulation_steps=10,
    )

    metadata = ParameterMetadata(
        name="fc1.weight",
        shape=(3, 4),
        numel=12,
        dtype="torch.float32",
        layer_order=5,
    )

    chunk = GradientChunk(
        metadata=metadata,
        has_grad=True,
        data=b"hello",
        byte_size=5,
        sync_round=9,
    )

    package = GradientPackage(
        runtime_version=1,
        job_id="job-1",
        worker_id="worker-1",
        chunks=[chunk],
    )

    assert config.runtime_version == 1
    assert config.gradient_accumulation_steps == 10

    assert metadata.name == "fc1.weight"
    assert metadata.numel == 12
    assert metadata.layer_order == 5

    assert chunk.has_grad is True
    assert chunk.byte_size == 5
    assert chunk.sync_round == 9

    assert len(package.chunks) == 1
    assert package.runtime_version == 1

    print("✓ RuntimeConfig OK")
    print("✓ ParameterMetadata OK")
    print("✓ GradientChunk OK")
    print("✓ GradientPackage OK")


if __name__ == "__main__":
    main()
