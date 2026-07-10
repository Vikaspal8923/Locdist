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
    GradientChunkGroup,
    GradientPackage,
    ParameterMetadata,
)

ASYNC_MAX_GROUPS_PER_BATCH = 256
# Keep async groups tensor-granular so the final accumulation backward pass
# can enqueue work as soon as each parameter becomes ready. The sender still
# coalesces many of these groups into one bulk RPC.
ASYNC_MAX_TENSORS_PER_GROUP = 1
ASYNC_MAX_GROUP_BYTES = 256 * 1024
ASYNC_MAX_GROUP_BYTES_PER_BATCH = 16 * 1024 * 1024
ASYNC_BATCH_FLUSH_MS = 1.0


@dataclass(frozen=True)
class LayerIdentity:
    name: str
    layer_order: int
    shape: tuple[int, ...]
    numel: int
    dtype: str


@dataclass(frozen=True)
class GroupIdentity:
    group_id: int
    layer_orders: tuple[int, ...]
    estimated_dense_bytes: int


@dataclass
class PreparedRuntimeState:
    expected_layers: tuple[LayerIdentity, ...]
    expected_groups: tuple[GroupIdentity, ...] = ()
    layer_to_group: dict[int, int] = field(default_factory=dict)
    hook_handles: list[Any] = field(default_factory=list)
    communication: CommunicationConfig = field(default_factory=CommunicationConfig)
    gradient_accumulation_steps: int = 1
    runtime_version: int = 1
    job_id: str = ""
    worker_id: str = ""
    rpc_timeout_seconds: int = 120
    next_round: int = 1
    active_round: int | None = None
    ready_layers: dict[int, LayerIdentity] = field(default_factory=dict)
    returned_layers: set[int] = field(default_factory=set)
    returned_groups: dict[int, set[int]] = field(default_factory=dict)
    queued_chunks: dict[int, dict[int, GradientChunk]] = field(default_factory=dict)
    outgoing_order: deque[tuple[int, int]] = field(default_factory=deque)
    outgoing_enqueued: set[tuple[int, int]] = field(default_factory=set)
    submitted_groups: dict[int, set[int]] = field(default_factory=dict)
    inflight_groups: dict[int, set[int]] = field(default_factory=dict)
    round_send_started_ms: dict[int, float] = field(default_factory=dict)
    round_finalized_ms: dict[int, float] = field(default_factory=dict)
    round_backward_complete_ms: dict[int, float] = field(default_factory=dict)
    round_capture_mode: dict[int, str] = field(default_factory=dict)
    round_step_sync_started_ms: dict[int, float] = field(default_factory=dict)
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
    completed_accumulation_microsteps: int = 0
    backward_pass_active: bool = False
    backward_pass_final_microstep: bool = False
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
            self._begin_backward_pass_locked()

            if (
                self.gradient_accumulation_steps > 1
                and not self.backward_pass_final_microstep
            ):
                return

            if self.active_round is None:
                self.active_round = self.next_round
                self.round_metrics.setdefault(
                    self.active_round,
                    {
                        "compression_ms": 0.0,
                        "runtime_to_worker_proto_build_ms": 0.0,
                        "runtime_to_worker_rpc_ms": 0.0,
                        "runtime_response_decode_ms": 0.0,
                        "estimated_pure_transfer_ms": 0.0,
                        "estimated_non_transfer_comm_overhead_ms": 0.0,
                        "known_local_comm_overhead_ms": 0.0,
                        "mixed_rpc_and_remote_ms": 0.0,
                        "transport_total_ms": 0.0,
                        "estimated_link_mbps": 0.0,
                        "runtime_bytes_up": 0,
                        "runtime_bytes_down": 0,
                        "chunk_send_count": 0,
                        "chunk_response_count": 0,
                        "group_send_count": 0,
                        "group_response_count": 0,
                        "batch_send_count": 0,
                        "max_queue_depth": 0,
                        "max_inflight_groups": 0,
                        "max_groups_per_batch": 0,
                        "batch_bytes_up": 0,
                        "first_layer_ready_ms": 0.0,
                        "last_layer_ready_ms": 0.0,
                        "first_chunk_sent_ms": 0.0,
                        "last_chunk_sent_ms": 0.0,
                        "first_chunk_returned_ms": 0.0,
                        "last_chunk_returned_ms": 0.0,
                        "finalize_round_ms": 0.0,
                        "optimizer_step_capture_ms": 0.0,
                        "step_wait_ms": 0.0,
                    },
                )
                if self.gradient_accumulation_steps > 1:
                    self.round_capture_mode[self.active_round] = (
                        "accumulation_final_backward_hook"
                    )
                else:
                    self.round_capture_mode[self.active_round] = "backward_hook"

            active_round = self.active_round

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
            self._enqueue_group_if_ready_locked(
                active_round,
                self.layer_to_group[layer_order],
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

    def capture_accumulated_gradients(
        self,
        model: torch.nn.Module,
    ) -> bool:
        named_parameters = dict(model.named_parameters())
        backward_complete_ms = now_ms()

        with self.condition:
            if self.active_round is not None:
                return False

            self.active_round = self.next_round
            round_id = self.active_round
            self.round_metrics.setdefault(
                round_id,
                {
                    "compression_ms": 0.0,
                    "runtime_to_worker_proto_build_ms": 0.0,
                    "runtime_to_worker_rpc_ms": 0.0,
                    "runtime_response_decode_ms": 0.0,
                    "estimated_pure_transfer_ms": 0.0,
                    "estimated_non_transfer_comm_overhead_ms": 0.0,
                    "known_local_comm_overhead_ms": 0.0,
                    "mixed_rpc_and_remote_ms": 0.0,
                    "transport_total_ms": 0.0,
                    "estimated_link_mbps": 0.0,
                    "runtime_bytes_up": 0,
                    "runtime_bytes_down": 0,
                    "chunk_send_count": 0,
                    "chunk_response_count": 0,
                    "group_send_count": 0,
                    "group_response_count": 0,
                    "batch_send_count": 0,
                    "max_queue_depth": 0,
                    "max_inflight_groups": 0,
                    "max_groups_per_batch": 0,
                    "batch_bytes_up": 0,
                    "first_layer_ready_ms": 0.0,
                    "last_layer_ready_ms": 0.0,
                    "first_chunk_sent_ms": 0.0,
                    "last_chunk_sent_ms": 0.0,
                    "first_chunk_returned_ms": 0.0,
                    "last_chunk_returned_ms": 0.0,
                    "finalize_round_ms": 0.0,
                    "optimizer_step_capture_ms": 0.0,
                    "step_wait_ms": 0.0,
                },
            )
            self.round_capture_mode[round_id] = "optimizer_step_capture"
            self.round_backward_complete_ms.setdefault(
                round_id,
                backward_complete_ms,
            )

        for layer in self.expected_layers:
            parameter = named_parameters.get(layer.name)
            if parameter is None or parameter.grad is None:
                continue

            chunk_start_ms = now_ms()
            chunk = compress_ready_gradient(
                name=layer.name,
                layer_order=layer.layer_order,
                gradient=parameter.grad,
                parameter_dtype=layer.dtype,
                communication=self.communication,
                residuals=self.preview_residuals,
                sync_round=round_id,
            )

            with self.condition:
                self.ready_layers.setdefault(layer.layer_order, layer)
                self.queued_chunks.setdefault(
                    round_id,
                    {},
                )[layer.layer_order] = chunk
                self._enqueue_group_if_ready_locked(
                    round_id,
                    self.layer_to_group[layer.layer_order],
                )
                metrics = self.round_metrics.setdefault(
                    round_id,
                    {},
                )
                ready_ts = now_ms()
                if not metrics.get("first_layer_ready_ms"):
                    metrics["first_layer_ready_ms"] = ready_ts
                metrics["last_layer_ready_ms"] = ready_ts
                metrics["compression_ms"] = float(
                    metrics.get("compression_ms", 0.0)
                ) + (ready_ts - chunk_start_ms)
                metrics["optimizer_step_capture_ms"] = float(
                    metrics.get("optimizer_step_capture_ms", 0.0)
                ) + (ready_ts - chunk_start_ms)
                self.emitted_chunk_count += 1
                self._ensure_sender_started_locked()
                self.condition.notify_all()

        return True

    def _begin_backward_pass_locked(self) -> None:
        if self.backward_pass_active:
            return
        self.backward_pass_active = True
        next_microstep = self.completed_accumulation_microsteps + 1
        accumulation_steps = max(1, self.gradient_accumulation_steps)
        if next_microstep > accumulation_steps:
            next_microstep = 1
            self.completed_accumulation_microsteps = 0
        self.backward_pass_final_microstep = next_microstep >= accumulation_steps
        torch.autograd.Variable._execution_engine.queue_callback(  # type: ignore[attr-defined]
            self._finish_backward_pass
        )

    def _finish_backward_pass(self) -> None:
        backward_complete_ms = now_ms()
        with self.condition:
            if not self.backward_pass_active:
                return
            accumulation_steps = max(1, self.gradient_accumulation_steps)
            if self.backward_pass_final_microstep:
                self.completed_accumulation_microsteps = accumulation_steps
                if self.active_round is not None:
                    self.round_backward_complete_ms.setdefault(
                        self.active_round,
                        backward_complete_ms,
                    )
            else:
                self.completed_accumulation_microsteps = min(
                    accumulation_steps,
                    self.completed_accumulation_microsteps + 1,
                )
            self.backward_pass_active = False
            self.backward_pass_final_microstep = False
            self.condition.notify_all()

    def note_optimizer_step_sync_start(self) -> None:
        with self.condition:
            round_id = self.active_round or self.next_round
            self.round_step_sync_started_ms.setdefault(
                round_id,
                now_ms(),
            )

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
            for group in self.expected_groups:
                self._enqueue_group_if_ready_locked(
                    round_id,
                    group.group_id,
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

    def _group_identity(
        self,
        group_id: int,
    ) -> GroupIdentity:
        return self.expected_groups[group_id]

    def _group_is_ready_locked(
        self,
        round_id: int,
        group_id: int,
    ) -> bool:
        round_chunks = self.queued_chunks.get(round_id, {})
        group = self._group_identity(group_id)
        return all(
            layer_order in round_chunks
            for layer_order in group.layer_orders
        )

    def _enqueue_group_if_ready_locked(
        self,
        round_id: int,
        group_id: int,
    ) -> None:
        if not self._group_is_ready_locked(round_id, group_id):
            return
        key = (round_id, group_id)
        if key in self.outgoing_enqueued:
            return
        if group_id in self.submitted_groups.get(round_id, set()):
            return
        if group_id in self.inflight_groups.get(round_id, set()):
            return
        if group_id in self.returned_groups.get(round_id, set()):
            return
        self.outgoing_order.append(key)
        self.outgoing_enqueued.add(key)
        self.round_send_started_ms.setdefault(round_id, now_ms())
        metrics = self.round_metrics.setdefault(round_id, {})
        metrics["max_queue_depth"] = max(
            int(metrics.get("max_queue_depth", 0)),
            len(self.outgoing_order),
        )

    def _build_group_payload_locked(
        self,
        round_id: int,
        group_id: int,
    ) -> GradientChunkGroup | None:
        if not self._group_is_ready_locked(round_id, group_id):
            return None
        round_chunks = self.queued_chunks.get(round_id, {})
        group = self._group_identity(group_id)
        chunks = [
            round_chunks[layer_order]
            for layer_order in group.layer_orders
        ]
        return GradientChunkGroup(
            group_id=group_id,
            sync_round=round_id,
            chunks=chunks,
            byte_size=sum(chunk.byte_size for chunk in chunks),
        )

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
            finalize_start_ms = now_ms()
            round_id, _ = self.finalize_round_chunks()
            finalize_done_ms = now_ms()
            wait_start_ms = finalize_done_ms
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
            self.returned_groups[round_id] = {
                group.group_id for group in self.expected_groups
            }
            self.round_applied.add(round_id)
            metrics = self.round_metrics.setdefault(round_id, {})
            metrics["finalize_round_ms"] = finalize_done_ms - finalize_start_ms
            metrics["step_wait_ms"] = wait_done_ms - wait_start_ms
            metrics["apply_gradients_ms"] = apply_done_ms - apply_start_ms
            self.round_terminal_status[round_id] = "completed"
            self._emit_round_metric_locked(
                round_id,
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
                self.submitted_groups.pop(round_id, None)
                self.inflight_groups.pop(round_id, None)
                self.returned_groups.pop(round_id, None)
                self.round_metrics.pop(round_id, None)
                self.round_errors.pop(round_id, None)
                self.round_applied.discard(round_id)
                self.round_terminal_status.pop(round_id, None)
                self.round_metric_emitted.discard(round_id)
                self.round_finalized_ms.pop(round_id, None)
                self.round_backward_complete_ms.pop(round_id, None)
                self.round_send_started_ms.pop(round_id, None)
                self.round_capture_mode.pop(round_id, None)
                self.round_step_sync_started_ms.pop(round_id, None)
                self._purge_round_queue_locked(round_id)
            self.active_round = None
            self.ready_layers.clear()
            self.returned_layers.clear()
            self.completed_accumulation_microsteps = 0
            self.backward_pass_active = False
            self.backward_pass_final_microstep = False
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

                round_id, group_id = self.outgoing_order.popleft()
                self.outgoing_enqueued.discard((round_id, group_id))
                if round_id in self.round_errors:
                    continue

                batch_entries: list[tuple[int, int, GradientChunkGroup]] = []
                first_group = self._build_group_payload_locked(round_id, group_id)
                if first_group is not None:
                    self.submitted_groups.setdefault(round_id, set()).add(group_id)
                    self.inflight_groups.setdefault(round_id, set()).add(group_id)
                    batch_entries.append((round_id, group_id, first_group))

                flush_deadline = now_ms() + ASYNC_BATCH_FLUSH_MS
                batch_bytes = sum(group.byte_size for _, _, group in batch_entries)
                while len(batch_entries) < ASYNC_MAX_GROUPS_PER_BATCH:
                    while not self.outgoing_order:
                        remaining_ms = flush_deadline - now_ms()
                        if remaining_ms <= 0:
                            break
                        self.condition.wait(timeout=remaining_ms / 1000.0)
                        if self.shutdown_sender:
                            return
                    if not self.outgoing_order:
                        break

                    candidate_round, candidate_group_id = self.outgoing_order[0]
                    if candidate_round != round_id:
                        break
                    candidate_group = self._build_group_payload_locked(
                        candidate_round,
                        candidate_group_id,
                    )
                    if candidate_group is None:
                        break
                    if batch_entries and (
                        batch_bytes + candidate_group.byte_size
                        > ASYNC_MAX_GROUP_BYTES_PER_BATCH
                    ):
                        break
                    self.outgoing_order.popleft()
                    self.outgoing_enqueued.discard((candidate_round, candidate_group_id))
                    if candidate_round in self.round_errors:
                        continue
                    self.submitted_groups.setdefault(candidate_round, set()).add(candidate_group_id)
                    self.inflight_groups.setdefault(candidate_round, set()).add(candidate_group_id)
                    batch_entries.append((candidate_round, candidate_group_id, candidate_group))
                    batch_bytes += candidate_group.byte_size

                if not batch_entries:
                    continue

                send_started_ms = now_ms()
                metrics = self.round_metrics.setdefault(round_id, {})
                if not metrics.get("first_chunk_sent_ms"):
                    metrics["first_chunk_sent_ms"] = send_started_ms
                metrics["last_chunk_sent_ms"] = send_started_ms
                metrics["chunk_send_count"] = int(
                    metrics.get("chunk_send_count", 0)
                ) + sum(len(group.chunks) for _, _, group in batch_entries)
                metrics["group_send_count"] = int(
                    metrics.get("group_send_count", 0)
                ) + len(batch_entries)
                metrics["batch_send_count"] = int(
                    metrics.get("batch_send_count", 0)
                ) + 1
                metrics["max_groups_per_batch"] = max(
                    int(metrics.get("max_groups_per_batch", 0)),
                    len(batch_entries),
                )
                metrics["max_inflight_groups"] = max(
                    int(metrics.get("max_inflight_groups", 0)),
                    len(self.inflight_groups.get(round_id, set())),
                )
                metrics["batch_bytes_up"] = int(
                    metrics.get("batch_bytes_up", 0)
                ) + batch_bytes

            try:
                expected_group_ids = {
                    candidate_group_id
                    for _, candidate_group_id, _ in batch_entries
                }
                aggregated = transport.synchronize_chunk_batch(
                    GradientPackage(
                        runtime_version=self.runtime_version,
                        job_id=self.job_id,
                        worker_id=self.worker_id,
                        chunks=[],
                        groups=[group for _, _, group in batch_entries],
                    )
                )
                returned_groups = aggregated.groups or []
                returned_group_ids = {
                    group.group_id for group in returned_groups
                }
                unexpected_group_ids = sorted(
                    returned_group_ids - expected_group_ids
                )
                if unexpected_group_ids:
                    raise SynchronizationError(
                        "Returned batch unexpected group ids: "
                        f"{unexpected_group_ids}"
                    )
                missing_group_ids = sorted(
                    expected_group_ids - returned_group_ids
                )
                if missing_group_ids:
                    raise SynchronizationError(
                        "Returned batch missing groups: "
                        f"{missing_group_ids}"
                    )
                response_metrics = dict(getattr(transport, "last_metrics", {}) or {})
                response_by_group = {
                    group.group_id: group
                    for group in returned_groups
                }
                for index, (_, returned_group_id, _) in enumerate(batch_entries):
                    group = response_by_group.get(returned_group_id)
                    if group is None:
                        raise SynchronizationError(
                            f"Returned batch missing expected group_id {returned_group_id}"
                        )
                    self._store_group_response(
                        round_id=round_id,
                        expected_group_id=returned_group_id,
                        response=AggregatedGradientChunkPackage(
                            runtime_version=aggregated.runtime_version,
                            job_id=aggregated.job_id,
                            participating_workers=aggregated.participating_workers,
                            aggregation_round=aggregated.aggregation_round,
                            group=group,
                        ),
                        metrics=response_metrics if index == 0 else {},
                    )
            except Exception as exc:
                with self.condition:
                    for failed_round_id, failed_group_id, _ in batch_entries:
                        self.inflight_groups.get(failed_round_id, set()).discard(failed_group_id)
                    self._fail_round_locked(
                        round_id,
                        exc,
                        status="failed",
                    )
                    self.condition.notify_all()

    def _store_group_response(
        self,
        *,
        round_id: int,
        expected_group_id: int,
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
        if response.group is None:
            raise SynchronizationError("Returned group response missing group payload")
        group = response.group
        if group.group_id != expected_group_id:
            raise SynchronizationError(
                f"Returned group mismatch ({group.group_id} != {expected_group_id})"
            )
        if group.sync_round != round_id:
            raise SynchronizationError(
                f"Returned group round mismatch ({group.sync_round} != {round_id})"
            )
        if int(response.aggregation_round) != round_id:
            raise SynchronizationError(
                "Returned aggregation round mismatch "
                f"({response.aggregation_round} != {round_id})"
            )
        expected_group = self._group_identity(expected_group_id)
        if len(group.chunks) != len(expected_group.layer_orders):
            raise SynchronizationError(
                "Returned group member count mismatch "
                f"({len(group.chunks)} != {len(expected_group.layer_orders)})"
            )

        with self.condition:
            if round_id < self.next_round:
                raise SynchronizationError(
                    f"Returned stale group for completed round {round_id}"
                )
            self.inflight_groups.get(round_id, set()).discard(expected_group_id)
            round_chunks = self.queued_chunks.setdefault(round_id, {})
            for expected_layer_order, chunk in zip(expected_group.layer_orders, group.chunks):
                if chunk.metadata is None:
                    raise SynchronizationError(
                        "Returned group member missing metadata"
                    )
                if chunk.metadata.layer_order != expected_layer_order:
                    raise SynchronizationError(
                        "Returned group member layer_order mismatch "
                        f"({chunk.metadata.layer_order} != {expected_layer_order})"
                    )
                expected_layer = self.expected_layers[expected_layer_order]
                if chunk.metadata.name != expected_layer.name:
                    raise SynchronizationError(
                        "Returned group member parameter mismatch "
                        f"({chunk.metadata.name!r} != {expected_layer.name!r})"
                    )
                if chunk.sync_round != round_id:
                    raise SynchronizationError(
                        f"Returned group member round mismatch ({chunk.sync_round} != {round_id})"
                    )
                round_chunks[expected_layer_order] = chunk
                self.returned_layers.add(expected_layer_order)
            self.returned_groups.setdefault(round_id, set()).add(expected_group_id)
            round_metrics = self.round_metrics.setdefault(round_id, {})
            returned_ms = now_ms()
            if not round_metrics.get("first_chunk_returned_ms"):
                round_metrics["first_chunk_returned_ms"] = returned_ms
            round_metrics["last_chunk_returned_ms"] = returned_ms
            round_metrics["chunk_response_count"] = int(
                round_metrics.get("chunk_response_count", 0)
            ) + len(group.chunks)
            round_metrics["group_response_count"] = int(
                round_metrics.get("group_response_count", 0)
            ) + 1
            for key in (
                "runtime_to_worker_proto_build_ms",
                "runtime_to_worker_rpc_ms",
                "runtime_response_decode_ms",
                "estimated_pure_transfer_ms",
                "estimated_non_transfer_comm_overhead_ms",
                "known_local_comm_overhead_ms",
                "mixed_rpc_and_remote_ms",
                "transport_total_ms",
            ):
                round_metrics[key] = float(round_metrics.get(key, 0.0)) + float(
                    metrics.get(key, 0.0)
                )
            if metrics.get("estimated_link_mbps"):
                round_metrics["estimated_link_mbps"] = float(
                    metrics["estimated_link_mbps"]
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
        deadline = None
        if round_id > 1:
            deadline = time.monotonic() + max(1, self.rpc_timeout_seconds)
        expected_groups = {
            group.group_id
            for group in self.expected_groups
        }
        with self.condition:
            while True:
                if round_id in self.round_errors:
                    err = self.round_errors[round_id]
                    raise SynchronizationError(
                        f"Prepared runtime round {round_id} failed: {err}"
                    ) from err

                returned_groups = self.returned_groups.get(round_id, set())
                if returned_groups == expected_groups:
                    return

                if deadline is None:
                    self.condition.wait(timeout=0.1)
                    continue

                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    missing = sorted(expected_groups - returned_groups)
                    err = SynchronizationError(
                        "Timed out waiting for aggregated groups "
                        f"for round {round_id}; missing groups: {missing}"
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
        self.submitted_groups.pop(round_id, None)
        self.inflight_groups.pop(round_id, None)
        self._purge_round_queue_locked(round_id)
        self._emit_round_metric_locked(
            round_id,
            end_ms=now_ms(),
            failure_reason=str(err),
        )

    def _emit_round_metric_locked(
        self,
        round_id: int,
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
        metrics["estimated_non_transfer_comm_overhead_ms"] = max(
            0.0,
            float(metrics.get("transport_call_ms", 0.0))
            - float(metrics.get("estimated_pure_transfer_ms", 0.0)),
        )
        metrics["total_ms"] = end_ms - send_start_ms
        metrics["chunk_count"] = len(self.expected_layers)
        metrics["group_count"] = len(self.expected_groups)
        metrics["sync_mode"] = "group_async"
        metrics["startup_grace_round"] = round_id == 1
        metrics["round_trigger"] = self.round_capture_mode.get(round_id, "unknown")
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
        step_sync_started_ms = self.round_step_sync_started_ms.get(round_id)
        if step_sync_started_ms is not None:
            metrics["optimizer_step_blocking_ms"] = max(
                0.0,
                end_ms - step_sync_started_ms,
            )
        else:
            metrics["optimizer_step_blocking_ms"] = 0.0
        metrics["non_overlap_overhead_ms"] = metrics["optimizer_step_blocking_ms"]
        metrics["blocking_capture_and_finalize_ms"] = max(
            0.0,
            float(metrics.get("optimizer_step_blocking_ms", 0.0))
            - float(metrics.get("step_wait_ms", 0.0))
            - float(metrics.get("apply_gradients_ms", 0.0)),
        )
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
                self._ldgcc_runtime_state.note_optimizer_step_sync_start()
                if self._ldgcc_runtime_state.active_round is None:
                    self._ldgcc_runtime_state.capture_accumulated_gradients(
                        self._model
                    )
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


def _build_fused_groups(
    expected_layers: list[LayerIdentity],
) -> tuple[tuple[GroupIdentity, ...], dict[int, int]]:
    groups: list[GroupIdentity] = []
    layer_to_group: dict[int, int] = {}
    current_orders: list[int] = []
    current_bytes = 0

    def flush() -> None:
        nonlocal current_orders, current_bytes
        if not current_orders:
            return
        group_id = len(groups)
        ordered = tuple(sorted(current_orders))
        groups.append(
            GroupIdentity(
                group_id=group_id,
                layer_orders=ordered,
                estimated_dense_bytes=current_bytes,
            )
        )
        for layer_order in ordered:
            layer_to_group[layer_order] = group_id
        current_orders = []
        current_bytes = 0

    for layer in reversed(expected_layers):
        estimated_bytes = max(1, layer.numel) * 4
        if current_orders and (
            len(current_orders) >= ASYNC_MAX_TENSORS_PER_GROUP
            or current_bytes+estimated_bytes > ASYNC_MAX_GROUP_BYTES
        ):
            flush()
        current_orders.append(layer.layer_order)
        current_bytes += estimated_bytes

    flush()
    return tuple(groups), layer_to_group


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

    expected_groups, layer_to_group = _build_fused_groups(expected_layers)
    state.expected_layers = tuple(expected_layers)
    state.expected_groups = expected_groups
    state.layer_to_group = layer_to_group
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
