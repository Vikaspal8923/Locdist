import grpc

from locdist.config import load_config

from locdist.exceptions import (
    ConnectionError,
    SynchronizationError,
)

from locdist.models import (
    GradientPackage,
    AggregatedGradientPackage,
)

from locdist.generated.gradient_pb2_grpc import (
    WorkerBridgeStub,
)

from locdist.wire import (
    package_to_submission_proto,
    response_proto_to_package,
)


_transport = None


class TransportClient:

    def __init__(self):

        self.config = load_config()

        self.address = (
            f"{self.config.worker_host}:"
            f"{self.config.worker_port}"
        )

        try:

            self.channel = grpc.insecure_channel(
                self.address
            )

            self.stub = WorkerBridgeStub(
                self.channel
            )

        except Exception as e:

            raise ConnectionError(
                f"Failed to create transport "
                f"client: {e}"
            ) from e

    def synchronize(
        self,
        package: GradientPackage,
    ) -> AggregatedGradientPackage:

        try:

            request = (
                package_to_submission_proto(
                    package
                )
            )

            response = (
                self.stub.SynchronizeGradients(
                    request,
                    timeout=(
                        self.config
                        .rpc_timeout_seconds
                    ),
                )
            )

            return (
                response_proto_to_package(
                    response
                )
            )

        except grpc.RpcError as e:

            raise SynchronizationError(
                f"Gradient synchronization "
                f"failed: {e}"
            ) from e

    def close(self) -> None:

        self.channel.close()


def get_transport() -> TransportClient:

    global _transport

    if _transport is None:

        _transport = TransportClient()

    return _transport