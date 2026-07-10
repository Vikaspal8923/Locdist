import grpc
from locdist.config import load_config

from locdist.exceptions import (
    ConnectionError,
    SynchronizationError,
)

from locdist.models import (
    GradientChunkPackage,
    AggregatedGradientChunkPackage,
    GradientPackage,
    AggregatedGradientPackage,
)

from locdist.generated.gradient_pb2_grpc import (
    WorkerBridgeStub,
)

from locdist.wire import (
    chunk_package_to_submission_proto,
    chunk_response_proto_to_package,
    package_to_submission_proto,
    response_proto_to_package,
)

from locdist.metrics import now_ms
from locdist.metrics import estimate_transfer_ms


_transport = None
GRPC_MAX_MESSAGE_BYTES = 128 * 1024 * 1024


class TransportClient:

    def __init__(self):

        self.config = load_config()
        self.last_metrics = {}

        if (
            self.config.sync_target == "master"
            and self.config.master_host
            and self.config.master_port
        ):
            self.address = (
                f"{self.config.master_host}:"
                f"{self.config.master_port}"
            )
            self.transport_peer = "master"
        else:
            self.address = (
                f"{self.config.worker_host}:"
                f"{self.config.worker_port}"
            )
            self.transport_peer = "worker"

        try:

            self.channel = grpc.insecure_channel(
                self.address,
                options=(
                    (
                        "grpc.max_send_message_length",
                        GRPC_MAX_MESSAGE_BYTES,
                    ),
                    (
                        "grpc.max_receive_message_length",
                        GRPC_MAX_MESSAGE_BYTES,
                    ),
                ),
            )

            self.stub = WorkerBridgeStub(
                self.channel
            )

        except Exception as e:

            raise ConnectionError(
                f"Failed to create transport "
                f"client: {e}"
            ) from e

    def _augment_transfer_metrics(
        self,
        metrics: dict,
    ) -> dict:
        total_bytes = int(metrics.get("runtime_bytes_up", 0)) + int(
            metrics.get("runtime_bytes_down", 0)
        )
        estimated_link_mbps = self.config.communication.estimated_link_mbps
        estimated_pure_transfer_ms = estimate_transfer_ms(
            total_bytes,
            estimated_link_mbps,
        )
        local_known_overhead_ms = float(
            metrics.get("runtime_to_worker_proto_build_ms", 0.0)
        ) + float(metrics.get("runtime_response_decode_ms", 0.0))
        mixed_rpc_ms = float(metrics.get("runtime_to_worker_rpc_ms", 0.0))
        transport_total_ms = float(metrics.get("transport_total_ms", 0.0))
        metrics.update(
            {
                "estimated_link_mbps": estimated_link_mbps or 0.0,
                "estimated_pure_transfer_ms": estimated_pure_transfer_ms,
                "estimated_non_transfer_comm_overhead_ms": max(
                    0.0,
                    transport_total_ms - estimated_pure_transfer_ms,
                ),
                "known_local_comm_overhead_ms": local_known_overhead_ms,
                "mixed_rpc_and_remote_ms": mixed_rpc_ms,
            }
        )
        return metrics

    def _package_round_id(
        self,
        package,
    ) -> int:
        if isinstance(package, GradientChunkPackage):
            return int(package.chunk.sync_round)
        if getattr(package, "groups", None):
            return int(package.groups[0].sync_round)
        if getattr(package, "chunks", None):
            return int(package.chunks[0].sync_round)
        return 0

    def _rpc_timeout_for_round(
        self,
        round_id: int,
    ) -> float | None:
        if round_id <= 1:
            return None
        return self.config.rpc_timeout_seconds

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
                    timeout=self._rpc_timeout_for_round(
                        self._package_round_id(package)
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

            self.last_metrics = self._augment_transfer_metrics({
                "transport_total_ms": decode_done_ms - start_ms,
                "runtime_to_worker_proto_build_ms": proto_build_ms - start_ms,
                "runtime_to_worker_rpc_ms": rpc_done_ms - proto_build_ms,
                "runtime_response_decode_ms": decode_done_ms - rpc_done_ms,
                "runtime_bytes_up": request.ByteSize(),
                "runtime_bytes_down": response.ByteSize(),
                "transport_peer": self.transport_peer,
            })

            return aggregated

        except grpc.RpcError as e:

            raise SynchronizationError(
                f"Gradient synchronization "
                f"failed: {e}"
            ) from e

    def synchronize_chunk(
        self,
        package: GradientChunkPackage,
    ) -> AggregatedGradientChunkPackage:

        try:
            start_ms = now_ms()
            request = chunk_package_to_submission_proto(package)
            proto_build_ms = now_ms()
            response = self.stub.SynchronizeGradientChunk(
                request,
                timeout=self._rpc_timeout_for_round(
                    self._package_round_id(package)
                ),
            )
            rpc_done_ms = now_ms()
            aggregated = chunk_response_proto_to_package(response)
            decode_done_ms = now_ms()

            self.last_metrics = self._augment_transfer_metrics({
                "transport_total_ms": decode_done_ms - start_ms,
                "runtime_to_worker_proto_build_ms": proto_build_ms - start_ms,
                "runtime_to_worker_rpc_ms": rpc_done_ms - proto_build_ms,
                "runtime_response_decode_ms": decode_done_ms - rpc_done_ms,
                "runtime_bytes_up": request.ByteSize(),
                "runtime_bytes_down": response.ByteSize(),
                "transport_mode": "chunk",
                "transport_peer": self.transport_peer,
            })

            return aggregated
        except grpc.RpcError as e:
            raise SynchronizationError(
                f"Gradient chunk synchronization "
                f"failed: {e}"
            ) from e

    def synchronize_chunk_batch(
        self,
        package: GradientPackage,
    ) -> AggregatedGradientPackage:

        try:
            start_ms = now_ms()
            request = package_to_submission_proto(package)
            proto_build_ms = now_ms()
            future = self.stub.SynchronizeGradientBatch.future(
                request,
                timeout=self._rpc_timeout_for_round(
                    self._package_round_id(package)
                ),
            )
            invoke_done_ms = now_ms()
            response = future.result()
            rpc_done_ms = now_ms()
            aggregated = response_proto_to_package(response)
            decode_done_ms = now_ms()

            self.last_metrics = self._augment_transfer_metrics({
                "transport_total_ms": decode_done_ms - start_ms,
                "runtime_to_worker_proto_build_ms": proto_build_ms - start_ms,
                "runtime_rpc_invoke_setup_ms": invoke_done_ms - proto_build_ms,
                "runtime_rpc_wait_ms": rpc_done_ms - invoke_done_ms,
                "runtime_to_worker_rpc_ms": rpc_done_ms - proto_build_ms,
                "runtime_response_decode_ms": decode_done_ms - rpc_done_ms,
                "runtime_bytes_up": request.ByteSize(),
                "runtime_bytes_down": response.ByteSize(),
                "transport_mode": "chunk_batch",
                "transport_peer": self.transport_peer,
            })

            return aggregated
        except grpc.RpcError as e:
            raise SynchronizationError(
                f"Gradient chunk batch synchronization "
                f"failed: {e}"
            ) from e

    def synchronize_chunk_batch_stream(
        self,
        package: GradientPackage,
    ):

        request = package_to_submission_proto(package)
        start_ms = now_ms()
        proto_build_ms = now_ms()

        try:
            stream = self.stub.SynchronizeGradientBatchStream(
                request,
                timeout=self._rpc_timeout_for_round(
                    self._package_round_id(package)
                ),
            )
            response_count = 0
            previous_ms = proto_build_ms
            total_bytes_down = 0

            for response in stream:
                received_ms = now_ms()
                aggregated = chunk_response_proto_to_package(response)
                decoded_ms = now_ms()
                response_count += 1
                total_bytes_down += response.ByteSize()

                yield aggregated, self._augment_transfer_metrics({
                    "transport_total_ms": decoded_ms - (
                        start_ms if response_count == 1 else previous_ms
                    ),
                    "runtime_to_worker_proto_build_ms": (
                        proto_build_ms - start_ms
                    ) if response_count == 1 else 0.0,
                    "runtime_to_worker_rpc_ms": received_ms - previous_ms,
                    "runtime_response_decode_ms": decoded_ms - received_ms,
                    "runtime_bytes_up": request.ByteSize() if response_count == 1 else 0,
                    "runtime_bytes_down": response.ByteSize(),
                    "transport_mode": "chunk_batch_stream",
                    "transport_peer": self.transport_peer,
                })
                previous_ms = decoded_ms

            self.last_metrics = self._augment_transfer_metrics({
                "transport_total_ms": previous_ms - start_ms,
                "runtime_to_worker_proto_build_ms": proto_build_ms - start_ms,
                "runtime_to_worker_rpc_ms": previous_ms - proto_build_ms,
                "runtime_response_decode_ms": 0.0,
                "runtime_bytes_up": request.ByteSize(),
                "runtime_bytes_down": total_bytes_down,
                "transport_mode": "chunk_batch_stream",
                "transport_peer": self.transport_peer,
            })
        except grpc.RpcError as e:
            raise SynchronizationError(
                f"Gradient chunk batch stream synchronization "
                f"failed: {e}"
            ) from e

    def close(self) -> None:

        self.channel.close()


def get_transport() -> TransportClient:

    global _transport

    if _transport is None:

        _transport = TransportClient()

    return _transport
