from __future__ import annotations

from collections import deque
from dataclasses import dataclass, field
import threading
import time
from typing import Any

import torch

from locdist.compression import compress_ready_gradient
from locdist.exceptions import SynchronizationError
from locdist.gradients import apply_gradient_chunks
from locdist.metrics import append_jsonl, now_ms
from locdist.models import (
    AggregatedGradientChunkPackage,
    CommunicationConfig,
    GradientChunk,
    GradientChunkPackage,
    GradientPackage,
    ParameterMetadata,
)

ASYNC_BATCH_SIZE = 4


@dataclass(frozen=True)
class LayerIdentity:
    name: str
    layer_order: int
    shape: tuple[int, ...]
    numel: int
    dtype: str


@dataclass
class PreparedRuntimeState:
    expected_layers: tuple[LayerIdentity, ...]
    hook_handles: list[Any] = field(default_factory=list)
    communication: CommunicationConfig = field(default_factory=CommunicationConfig)
    runtime_version: int = 1
    job_id: str = ""
    worker_id: str = ""
    rpc_timeout_seconds: int = 120
    next_round: int = 1
    active_round: int | None = None
    ready_layers: dict[int, LayerIdentity] = field(default_factory=dict)
    returned_layers: set[int] = field(default_factory=set)
    queued_chunks: dict[int, dict[int, GradientChunk]] = field(default_factory=dict)
    outgoing_order: deque[tuple[int, int]] = field(default_factory=deque)
    outgoing_enqueued: set[tuple[int, int]] = field(default_factory=set)
    inflight_layers: dict[int, set[int]] = field(default_factory=dict)
    round_send_started_ms: dict[int, float] = field(default_factory=dict)
    round_finalized_ms: dict[int, float] = field(default_factory=dict)
    round_backward_complete_ms: dict[int, float] = field(default_factory=dict)
    round_metrics: dict[int, dict[str, float | int | str]] = field(default_factory=dict)
    round_errors: dict[int, Exception] = field(default_factory=dict)
    round_applied: set[int] = field(default_factory=set)
    round_terminal_status: dict[int, str] = field(default_factory=dict)
    round_metric_emitted: set[int] = field(default_factory=set)
    preview_residuals: dict[str, torch.Tensor] = field(default_factory=dict)
    completed_rounds: list[int] = field(default_factory=list)
    fallback_sync_rounds: list[int] = field(default_factory=list)
    emitted_chunk_count: int = 0
    step_count: int = 0
    sender_started: bool = False
    shutdown_sender: bool = False
    sender_thread: threading.Thread | None = None
    lock: threading.RLock = field(default_factory=threading.RLock)
    condition: threading.Condition = field(init=False)

    def __post_init__(self) -> None:
        self.condition = threading.Condition(self.lock)

    def configure_runtime(
        self,
        *,
        runtime_version: int,
        job_id: str,
        worker_id: str,
        rpc_timeout_seconds: int,
    ) -> None:
        with self.condition:
            self.runtime_version = runtime_version
            self.job_id = job_id
            self.worker_id = worker_id
            self.rpc_timeout_seconds = rpc_timeout_seconds
            self.condition.notify_all()

    def record_ready_layer(
        self,
        name: str,
        layer_order: int,
        shape: tuple[int, ...],
        gradient: torch.Tensor,
        parameter_dtype: str,
    ) -> None:
        start_ms = now_ms()
        with self.condition:
            if self.active_round is None:
                self.active_round = self.next_round
                self.round_metrics.setdefault(
                    self.active_round,
                    {
                        "compression_ms": 0.0,
                        "runtime_to_worker_proto_build_ms": 0.0,
                        "runtime_to_worker_rpc_ms": 0.0,
                        "runtime_response_decode_ms": 0.0,
                        "runtime_bytes_up": 0,
                        "runtime_bytes_down": 0,
                        "chunk_send_count": 0,
                        "chunk_response_count": 0,
                        "batch_send_count": 0,
                        "max_queue_depth": 0,
                        "max_inflight_chunks": 0,
                        "max_chunks_per_batch": 0,
                        "first_layer_ready_ms": 0.0,
                        "last_layer_ready_ms": 0.0,
                        "first_chunk_sent_ms": 0.0,
                        "last_chunk_sent_ms": 0.0,
                        "first_chunk_returned_ms": 0.0,
                        "last_chunk_returned_ms": 0.0,
                        "step_wait_ms": 0.0,
                    },
                )

            active_round = self.active_round
            self.ready_layers.setdefault(
                layer_order,
                LayerIdentity(
                    name=name,
                    layer_order=layer_order,
                    shape=shape,
                    numel=gradient.numel(),
                    dtype=parameter_dtype,
                ),
            )

        chunk = compress_ready_gradient(
            name=name,
            layer_order=layer_order,
            gradient=gradient,
            parameter_dtype=parameter_dtype,
            communication=self.communication,
            residuals=self.preview_residuals,
            sync_round=active_round,
        )

        with self.condition:
            self.queued_chunks.setdefault(
                active_round,
                {},
            )[layer_order] = chunk
            self._enqueue_chunk_locked(
                active_round,
                layer_order,
            )
            metrics = self.round_metrics.setdefault(
                active_round,
                {},
            )
            ready_ts = now_ms()
            if not metrics.get("first_layer_ready_ms"):
                metrics["first_layer_ready_ms"] = ready_ts
            metrics["last_layer_ready_ms"] = ready_ts
            metrics["compression_ms"] = float(
                metrics.get("compression_ms", 0.0)
            ) + (ready_ts - start_ms)
            self.emitted_chunk_count += 1
            self._ensure_sender_started_locked()
            self.condition.notify_all()

    def finalize_round_chunks(
        self,
    ) -> tuple[int, list[GradientChunk]]:
        with self.condition:
            if self.active_round is None:
                raise RuntimeError("No active prepared runtime round to finalize")

            round_id = self.active_round
            round_chunks = self.queued_chunks.setdefault(round_id, {})

            for layer in self.expected_layers:
                round_chunks.setdefault(
                    layer.layer_order,
                    GradientChunk(
                        metadata=ParameterMetadata(
                            name=layer.name,
                            shape=layer.shape,
                            numel=layer.numel,
                            dtype=layer.dtype,
                            layer_order=layer.layer_order,
                        ),
                        has_grad=False,
                        data=None,
                        byte_size=0,
                        data_dtype=None,
                        encoding="dense",
                        indices=[],
                        indices_u32=None,
                        sync_round=round_id,
                    ),
                )
                self._enqueue_chunk_locked(
                    round_id,
                    layer.layer_order,
                )

            self.round_finalized_ms.setdefault(
                round_id,
                now_ms(),
            )
            self.round_backward_complete_ms.setdefault(
                round_id,
                self.round_finalized_ms[round_id],
            )
            self._ensure_sender_started_locked()
            self.condition.notify_all()

            return (
                round_id,
                [
                    round_chunks[layer.layer_order]
                    for layer in self.expected_layers
                ],
            )

    def mark_layers_returned(
        self,
        layer_orders: list[int],
    ) -> None:
        with self.condition:
            self.returned_layers.update(layer_orders)
            self.condition.notify_all()

    def note_fallback_sync(self) -> None:
        with self.condition:
            if self.active_round is None:
                self.active_round = self.next_round
            self.fallback_sync_rounds.append(self.active_round)

    def complete_active_round(
        self,
        model: torch.nn.Module,
    ) -> bool:
        with self.condition:
            if self.active_round is None:
                return False
            round_id = self.active_round
            if round_id in self.round_applied:
                return True

        try:
            round_id, _ = self.finalize_round_chunks()
            wait_start_ms = now_ms()
            self._wait_for_round_completion(round_id)
            wait_done_ms = now_ms()
            ordered_chunks = self._ordered_returned_chunks(round_id)
            apply_start_ms = now_ms()
            apply_gradient_chunks(
                model,
                ordered_chunks,
            )
            apply_done_ms = now_ms()
        except Exception as exc:
            with self.condition:
                if round_id is not None:
                    status = "failed"
                    if isinstance(exc, SynchronizationError) and "Timed out waiting" in str(exc):
                        status = "timeout"
                    self._fail_round_locked(
                        round_id,
                        exc,
                        status=status,
                    )
            raise

        with self.condition:
            self.returned_layers.update(
                chunk.metadata.layer_order
                for chunk in ordered_chunks
            )
            self.round_applied.add(round_id)
            metrics = self.round_metrics.setdefault(round_id, {})
            metrics["step_wait_ms"] = wait_done_ms - wait_start_ms
            metrics["apply_gradients_ms"] = apply_done_ms - apply_start_ms
            self.round_terminal_status[round_id] = "completed"
            self._emit_round_metric_locked(
                round_id,
                len(ordered_chunks),
                end_ms=apply_done_ms,
            )
            self.condition.notify_all()
        return True

    def finish_step(self) -> None:
        with self.condition:
            if self.active_round is not None:
                self.completed_rounds.append(self.active_round)
                self.next_round = self.active_round + 1
                round_id = self.active_round
                self.queued_chunks.pop(round_id, None)
                self.inflight_layers.pop(round_id, None)
                self.round_metrics.pop(round_id, None)
                self.round_errors.pop(round_id, None)
                self.round_applied.discard(round_id)
                self.round_terminal_status.pop(round_id, None)
                self.round_metric_emitted.discard(round_id)
                self.round_finalized_ms.pop(round_id, None)
                self.round_backward_complete_ms.pop(round_id, None)
                self.round_send_started_ms.pop(round_id, None)
                self._purge_round_queue_locked(round_id)
            self.active_round = None
            self.ready_layers.clear()
            self.returned_layers.clear()
            self.step_count += 1
            self.condition.notify_all()

    def shutdown(self) -> None:
        with self.condition:
            self.shutdown_sender = True
            if self.active_round is not None:
                self._fail_round_locked(
                    self.active_round,
                    SynchronizationError("runtime shutdown"),
                    status="cancelled",
                )
            self.condition.notify_all()
        if self.sender_thread is not None:
            self.sender_thread.join(timeout=1.0)

    def _ensure_sender_started_locked(self) -> None:
        if self.sender_started:
            return
        self.sender_started = True
        self.sender_thread = threading.Thread(
            target=self._sender_loop,
            name="ldgcc-runtime-sender",
            daemon=True,
        )
        self.sender_thread.start()

    def _enqueue_chunk_locked(
        self,
        round_id: int,
        layer_order: int,
    ) -> None:
        key = (round_id, layer_order)
        if key in self.outgoing_enqueued:
            return
        if layer_order in self.inflight_layers.get(round_id, set()):
            return
        if layer_order in self.returned_layers:
            return
        self.outgoing_order.append(key)
        self.outgoing_enqueued.add(key)
        self.round_send_started_ms.setdefault(round_id, now_ms())
        metrics = self.round_metrics.setdefault(round_id, {})
        metrics["max_queue_depth"] = max(
            int(metrics.get("max_queue_depth", 0)),
            len(self.outgoing_order),
        )

    def _sender_loop(self) -> None:
        from locdist.transport import get_transport

        transport = get_transport()
        while True:
            with self.condition:
                while not self.shutdown_sender and (
                    not self.outgoing_order
                    or not self.job_id
                    or not self.worker_id
                ):
                    self.condition.wait()
                if self.shutdown_sender:
                    return

                round_id, layer_order = self.outgoing_order.popleft()
                self.outgoing_enqueued.discard((round_id, layer_order))
                if round_id in self.round_errors:
                    continue

                batch_entries: list[tuple[int, int, GradientChunk]] = []
                first_chunk = self.queued_chunks.get(round_id, {}).get(layer_order)
                if first_chunk is not None:
                    batch_entries.append((round_id, layer_order, first_chunk))

                while len(batch_entries) < ASYNC_BATCH_SIZE and self.outgoing_order:
                    candidate_round, candidate_layer = self.outgoing_order[0]
                    if candidate_round != round_id:
                        break
                    self.outgoing_order.popleft()
                    self.outgoing_enqueued.discard((candidate_round, candidate_layer))
                    if candidate_round in self.round_errors:
                        continue
                    candidate_chunk = self.queued_chunks.get(candidate_round, {}).get(candidate_layer)
                    if candidate_chunk is None:
                        continue
                    batch_entries.append((candidate_round, candidate_layer, candidate_chunk))

                if not batch_entries:
                    continue

                for _, queued_layer_order, _ in batch_entries:
                    self.inflight_layers.setdefault(round_id, set()).add(queued_layer_order)

                send_started_ms = now_ms()
                metrics = self.round_metrics.setdefault(round_id, {})
                if not metrics.get("first_chunk_sent_ms"):
                    metrics["first_chunk_sent_ms"] = send_started_ms
                metrics["last_chunk_sent_ms"] = send_started_ms
                metrics["chunk_send_count"] = int(
                    metrics.get("chunk_send_count", 0)
                ) + len(batch_entries)
                metrics["batch_send_count"] = int(
                    metrics.get("batch_send_count", 0)
                ) + 1
                metrics["max_chunks_per_batch"] = max(
                    int(metrics.get("max_chunks_per_batch", 0)),
                    len(batch_entries),
                )
                metrics["max_inflight_chunks"] = max(
                    int(metrics.get("max_inflight_chunks", 0)),
                    len(self.inflight_layers.get(round_id, set())),
                )

            try:
                if len(batch_entries) == 1:
                    _, single_layer_order, single_chunk = batch_entries[0]
                    aggregated = transport.synchronize_chunk(
                        GradientChunkPackage(
                            runtime_version=self.runtime_version,
                            job_id=self.job_id,
                            worker_id=self.worker_id,
                            chunk=single_chunk,
                        )
                    )
                    self._store_chunk_response(
                        round_id=round_id,
                        expected_layer_order=single_layer_order,
                        response=aggregated,
                        metrics=getattr(transport, "last_metrics", {}),
                    )
                else:
                    expected_layer_orders = {
                        layer_order
                        for _, layer_order, _ in batch_entries
                    }
                    received_layer_orders: set[int] = set()
                    for aggregated, response_metrics in transport.synchronize_chunk_batch_stream(
                        GradientPackage(
                            runtime_version=self.runtime_version,
                            job_id=self.job_id,
                            worker_id=self.worker_id,
                            chunks=[chunk for _, _, chunk in batch_entries],
                        )
                    ):
                        if aggregated.chunk is None or aggregated.chunk.metadata is None:
                            raise SynchronizationError(
                                "Returned streamed batch chunk missing metadata"
                            )
                        layer_order = aggregated.chunk.metadata.layer_order
                        if layer_order not in expected_layer_orders:
                            raise SynchronizationError(
                                f"Returned streamed batch unexpected layer_order {layer_order}"
                            )
                        if layer_order in received_layer_orders:
                            raise SynchronizationError(
                                f"Returned streamed batch duplicate layer_order {layer_order}"
                            )
                        received_layer_orders.add(layer_order)
                        self._store_chunk_response(
                            round_id=round_id,
                            expected_layer_order=layer_order,
                            response=aggregated,
                            metrics=response_metrics,
                        )
                    missing_layer_orders = sorted(
                        expected_layer_orders - received_layer_orders
                    )
                    if missing_layer_orders:
                        raise SynchronizationError(
                            "Returned streamed batch ended before all chunks arrived; "
                            f"missing layers: {missing_layer_orders}"
                        )
            except Exception as exc:
                with self.condition:
                    for failed_round_id, failed_layer_order, _ in batch_entries:
                        self.inflight_layers.get(failed_round_id, set()).discard(failed_layer_order)
                    self._fail_round_locked(
                        round_id,
                        exc,
                        status="failed",
                    )
                    self.condition.notify_all()

    def _store_chunk_response(
        self,
        *,
        round_id: int,
        expected_layer_order: int,
        response: AggregatedGradientChunkPackage,
        metrics: dict[str, float | int | str],
    ) -> None:
        if response.runtime_version != self.runtime_version:
            raise SynchronizationError(
                f"Returned chunk runtime_version mismatch ({response.runtime_version} != {self.runtime_version})"
            )
        if response.job_id != self.job_id:
            raise SynchronizationError(
                f"Returned chunk job_id mismatch ({response.job_id!r} != {self.job_id!r})"
            )
        if response.chunk is None:
            raise SynchronizationError("Returned chunk response missing chunk payload")
        chunk = response.chunk
        if chunk.metadata is None:
            raise SynchronizationError("Returned chunk response missing chunk metadata")
        if chunk.metadata.layer_order != expected_layer_order:
            raise SynchronizationError(
                "Returned chunk layer_order mismatch "
                f"({chunk.metadata.layer_order} != {expected_layer_order})"
            )
        expected_layer = self.expected_layers[expected_layer_order]
        if chunk.metadata.name != expected_layer.name:
            raise SynchronizationError(
                "Returned chunk parameter mismatch "
                f"({chunk.metadata.name!r} != {expected_layer.name!r})"
            )
        if chunk.sync_round != round_id:
            raise SynchronizationError(
                f"Returned chunk round mismatch ({chunk.sync_round} != {round_id})"
            )
        if int(response.aggregation_round) != round_id:
            raise SynchronizationError(
                "Returned aggregation round mismatch "
                f"({response.aggregation_round} != {round_id})"
            )

        with self.condition:
            if round_id < self.next_round:
                raise SynchronizationError(
                    f"Returned stale chunk for completed round {round_id}"
                )
            self.inflight_layers.get(round_id, set()).discard(expected_layer_order)
            self.queued_chunks.setdefault(round_id, {})[expected_layer_order] = chunk
            self.returned_layers.add(expected_layer_order)
            round_metrics = self.round_metrics.setdefault(round_id, {})
            returned_ms = now_ms()
            if not round_metrics.get("first_chunk_returned_ms"):
                round_metrics["first_chunk_returned_ms"] = returned_ms
            round_metrics["last_chunk_returned_ms"] = returned_ms
            round_metrics["chunk_response_count"] = int(
                round_metrics.get("chunk_response_count", 0)
            ) + 1
            for key in (
                "runtime_to_worker_proto_build_ms",
                "runtime_to_worker_rpc_ms",
                "runtime_response_decode_ms",
            ):
                round_metrics[key] = float(round_metrics.get(key, 0.0)) + float(
                    metrics.get(key, 0.0)
                )
            for key in ("runtime_bytes_up", "runtime_bytes_down"):
                round_metrics[key] = int(round_metrics.get(key, 0)) + int(
                    metrics.get(key, 0)
                )
            self.condition.notify_all()

    def _wait_for_round_completion(
        self,
        round_id: int,
    ) -> None:
        deadline = time.monotonic() + max(1, self.rpc_timeout_seconds)
        expected_orders = {
            layer.layer_order
            for layer in self.expected_layers
        }
        with self.condition:
            while True:
                if round_id in self.round_errors:
                    err = self.round_errors[round_id]
                    raise SynchronizationError(
                        f"Prepared runtime round {round_id} failed: {err}"
                    ) from err

                returned_orders = {
                    layer_order
                    for layer_order in expected_orders
                    if layer_order in self.returned_layers
                }
                if returned_orders == expected_orders:
                    return

                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    missing = sorted(expected_orders - returned_orders)
                    err = SynchronizationError(
                        "Timed out waiting for aggregated chunks "
                        f"for round {round_id}; missing layers: {missing}"
                    )
                    self._fail_round_locked(
                        round_id,
                        err,
                        status="timeout",
                    )
                    raise err
                self.condition.wait(timeout=min(0.1, remaining))

    def _fail_round_locked(
        self,
        round_id: int,
        err: Exception,
        *,
        status: str,
    ) -> None:
        self.round_errors.setdefault(round_id, err)
        self.round_terminal_status[round_id] = status
        self.inflight_layers.pop(round_id, None)
        self._purge_round_queue_locked(round_id)
        self._emit_round_metric_locked(
            round_id,
            len(self.queued_chunks.get(round_id, {})),
            end_ms=now_ms(),
            failure_reason=str(err),
        )

    def _emit_round_metric_locked(
        self,
        round_id: int,
        chunk_count: int,
        *,
        end_ms: float,
        failure_reason: str | None = None,
    ) -> None:
        if round_id in self.round_metric_emitted:
            return
        metrics = self.round_metrics.setdefault(round_id, {})
        send_start_ms = self.round_send_started_ms.get(round_id, end_ms)
        finalized_ms = self.round_finalized_ms.get(round_id, send_start_ms)
        backward_complete_ms = self.round_backward_complete_ms.get(
            round_id,
            finalized_ms,
        )
        metrics["extract_gradients_ms"] = max(
            0.0,
            finalized_ms - send_start_ms,
        )
        metrics["transport_call_ms"] = float(
            metrics.get("runtime_to_worker_proto_build_ms", 0.0)
        ) + float(
            metrics.get("runtime_to_worker_rpc_ms", 0.0)
        ) + float(
            metrics.get("runtime_response_decode_ms", 0.0)
        )
        metrics["total_ms"] = end_ms - send_start_ms
        metrics["chunk_count"] = chunk_count
        metrics["sync_mode"] = "chunk_async"
        metrics["round_status"] = self.round_terminal_status.get(round_id, "unknown")
        if failure_reason is not None:
            metrics["failure_reason"] = failure_reason
        metrics["overlap_send_before_backward_complete_ms"] = max(
            0.0,
            backward_complete_ms - float(metrics.get("first_chunk_sent_ms", backward_complete_ms)),
        ) if metrics.get("first_chunk_sent_ms") else 0.0
        metrics["overlap_response_before_backward_complete_ms"] = max(
            0.0,
            backward_complete_ms - float(metrics.get("first_chunk_returned_ms", backward_complete_ms)),
        ) if metrics.get("first_chunk_returned_ms") else 0.0
        metrics["post_backward_transport_tail_ms"] = max(
            0.0,
            float(metrics.get("last_chunk_returned_ms", backward_complete_ms)) - backward_complete_ms,
        )
        metrics["backward_to_first_send_ms"] = max(
            0.0,
            float(metrics.get("first_chunk_sent_ms", backward_complete_ms)) - float(metrics.get("first_layer_ready_ms", backward_complete_ms)),
        ) if metrics.get("first_layer_ready_ms") else 0.0
        append_jsonl(
            "ldgcc_runtime_sync_metrics.jsonl",
            {
                "component": "runtime",
                "job_id": self.job_id,
                "worker_id": self.worker_id,
                "sync_step": round_id,
                **metrics,
            },
        )
        self.round_metric_emitted.add(round_id)

    def _ordered_returned_chunks(
        self,
        round_id: int,
    ) -> list[GradientChunk]:
        with self.condition:
            round_chunks = self.queued_chunks.get(round_id, {})
            return [
                round_chunks[layer.layer_order]
                for layer in self.expected_layers
            ]

    def _purge_round_queue_locked(
        self,
        round_id: int,
    ) -> None:
        if not self.outgoing_order:
            return
        keep = deque()
        while self.outgoing_order:
            key = self.outgoing_order.popleft()
            if key[0] != round_id:
                keep.append(key)
            else:
                self.outgoing_enqueued.discard(key)
        self.outgoing_order = keep


class PreparedOptimizer:
    # Delegate everything except the future sync gate around step().
    def __init__(
        self,
        model: torch.nn.Module,
        optimizer: Any,
        state: PreparedRuntimeState | None,
    ) -> None:
        self._model = model
        self._optimizer = optimizer
        self._ldgcc_runtime_state = state

    def step(self, *args, **kwargs):
        try:
            if self._ldgcc_runtime_state is not None:
                self._ldgcc_runtime_state.complete_active_round(self._model)
            return self._optimizer.step(*args, **kwargs)
        finally:
            if self._ldgcc_runtime_state is not None:
                self._ldgcc_runtime_state.finish_step()

    def zero_grad(self, *args, **kwargs):
        return self._optimizer.zero_grad(*args, **kwargs)

    def state_dict(self):
        return self._optimizer.state_dict()

    def load_state_dict(self, state_dict):
        return self._optimizer.load_state_dict(state_dict)

    def __getattr__(self, name: str):
        return getattr(self._optimizer, name)


def get_prepared_runtime_state(
    model: torch.nn.Module,
) -> PreparedRuntimeState | None:
    return getattr(model, "_ldgcc_runtime_state", None)


def prepare_model(
    model: torch.nn.Module,
    communication: CommunicationConfig | None = None,
) -> torch.nn.Module:
    existing = get_prepared_runtime_state(model)
    if existing is not None:
        return model

    expected_layers: list[LayerIdentity] = []
    state = PreparedRuntimeState(
        expected_layers=(),
        communication=communication or CommunicationConfig(),
    )

    for layer_order, (name, parameter) in enumerate(model.named_parameters()):
        identity = LayerIdentity(
            name=name,
            layer_order=layer_order,
            shape=tuple(parameter.shape),
            numel=parameter.numel(),
            dtype=str(parameter.dtype),
        )
        expected_layers.append(identity)

        if not parameter.requires_grad:
            continue

        def _hook(
            grad: torch.Tensor,
            *,
            layer_name: str = name,
            order: int = layer_order,
            parameter_dtype: str = str(parameter.dtype),
        ) -> torch.Tensor:
            state.record_ready_layer(
                name=layer_name,
                layer_order=order,
                shape=tuple(grad.shape),
                gradient=grad,
                parameter_dtype=parameter_dtype,
            )
            return grad

        state.hook_handles.append(parameter.register_hook(_hook))

    state.expected_layers = tuple(expected_layers)
    setattr(model, "_ldgcc_runtime_state", state)
    return model


def prepare_wrapped_optimizer(
    model: torch.nn.Module,
    optimizer: Any,
):
    state = get_prepared_runtime_state(model)
    if isinstance(optimizer, PreparedOptimizer):
        return optimizer
    return PreparedOptimizer(
        model=model,
        optimizer=optimizer,
        state=state,
    )
