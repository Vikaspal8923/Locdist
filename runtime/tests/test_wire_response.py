from locdist.generated import (
    gradient_pb2,
)

from locdist.wire import (
    response_proto_to_package,
)


def main():

    response = (
        gradient_pb2.AggregatedGradientResponse(
            runtime_version=1,
            job_id="job-123",
            participating_workers=8,
            aggregation_round=42,
        )
    )

    package = (
        response_proto_to_package(
            response
        )
    )

    assert package.runtime_version == 1
    assert package.job_id == "job-123"

    assert (
        package.participating_workers
        == 8
    )

    assert (
        package.aggregation_round
        == 42
    )

    print()
    print(
        "✓ Response metadata preserved"
    )


if __name__ == "__main__":
    main()