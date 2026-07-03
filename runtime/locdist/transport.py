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

from locdist.metrics import now_ms


_transport = None


class TransportClient:

    def __init__(self):

        self.config = load_config()
        self.last_metrics = {}

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

            start_ms = now_ms()
            request = (
                package_to_submission_proto(
                    package
                )
            )
            proto_build_ms = now_ms()

            response = (
                self.stub.SynchronizeGradients(
                    request,
                    timeout=(
                        self.config
                        .rpc_timeout_seconds
                    ),
                )
            )
            rpc_done_ms = now_ms()

            aggregated = (
                response_proto_to_package(
                    response
                )
            )
            decode_done_ms = now_ms()

            self.last_metrics = {
                "transport_total_ms": decode_done_ms - start_ms,
                "runtime_to_worker_proto_build_ms": proto_build_ms - start_ms,
                "runtime_to_worker_rpc_ms": rpc_done_ms - proto_build_ms,
                "runtime_response_decode_ms": decode_done_ms - rpc_done_ms,
                "runtime_bytes_up": request.ByteSize(),
                "runtime_bytes_down": response.ByteSize(),
            }

            return aggregated

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
