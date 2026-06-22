from locdist.models import (
    GradientChunk,
    GradientPackage,
    ParameterMetadata,
)

from locdist.wire import (
    package_to_submission_proto,
    submission_proto_to_package,
)


def main():

    metadata = ParameterMetadata(
        name="fc1.weight",
        shape=(3, 4),
        numel=12,
        dtype="torch.float32",
    )

    chunk = GradientChunk(
        metadata=metadata,
        has_grad=True,
        data=b"hello-world",
        byte_size=len(b"hello-world"),
    )

    original_package = GradientPackage(
        runtime_version=1,
        job_id="job-123",
        worker_id="worker-1",
        chunks=[chunk],
    )

    proto = package_to_submission_proto(
        original_package
    )

    restored_package = (
        submission_proto_to_package(
            proto
        )
    )

    assert (
        restored_package.runtime_version
        == 1
    )

    assert (
        restored_package.job_id
        == "job-123"
    )

    assert (
        restored_package.worker_id
        == "worker-1"
    )

    assert (
        len(restored_package.chunks)
        == 1
    )

    restored_chunk = (
        restored_package.chunks[0]
    )

    assert (
        restored_chunk.metadata.name
        == "fc1.weight"
    )

    assert (
        restored_chunk.metadata.shape
        == (3, 4)
    )

    assert (
        restored_chunk.metadata.numel
        == 12
    )

    assert (
        restored_chunk.metadata.dtype
        == "torch.float32"
    )

    assert (
        restored_chunk.has_grad
        is True
    )

    assert (
        restored_chunk.data
        == b"hello-world"
    )

    print()
    print(
        "✓ Package -> Proto -> Package"
    )
    print(
        "✓ Serialization roundtrip successful"
    )


if __name__ == "__main__":
    main()