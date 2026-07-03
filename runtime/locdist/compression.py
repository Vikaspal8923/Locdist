import math
import time
from dataclasses import dataclass, field

import torch

from locdist.models import (
    CommunicationConfig,
    GradientChunk,
    ParameterMetadata,
)
from locdist.indices import pack_u32_indices
from locdist.tensor_bytes import tensor_to_bytes


PRECISION_TO_DTYPE = {
    "fp32": torch.float32,
    "fp16": torch.float16,
}


@dataclass
class CompressionState:
    sync_step: int = 0
    residuals: dict[str, torch.Tensor] = field(default_factory=dict)
    last_metrics: dict[str, float | int | str] = field(default_factory=dict)


def extract_compressed_gradient_chunks(
    model: torch.nn.Module,
    communication: CommunicationConfig,
    state: CompressionState,
) -> list[GradientChunk]:
    state.sync_step += 1
    state.last_metrics = {
        "compression_mode": communication.compression_mode,
        "compression_selection": communication.selection,
        "compression_device": communication.device,
        "sample_rate_percent": communication.sample_rate_percent,
        "max_payload_factor": communication.max_payload_factor,
    }

    if (
        communication.compression_type != "topk"
        or state.sync_step <= communication.warmup_steps
    ):
        start = _now_ms()
        chunks = _extract_dense(model, communication)
        state.last_metrics.update(
            {
                "compression_path": "dense",
                "compression_total_ms": _elapsed_ms(start),
            }
        )
        return chunks

    start = _now_ms()
    if communication.compression_mode == "per_layer":
        chunks = _extract_per_layer_topk(model, communication, state)
    else:
        chunks = _extract_global_topk(model, communication, state)
    state.last_metrics["compression_total_ms"] = _elapsed_ms(start)
    return chunks


def _extract_dense(
    model: torch.nn.Module,
    communication: CommunicationConfig,
) -> list[GradientChunk]:
    chunks: list[GradientChunk] = []
    payload_dtype = PRECISION_TO_DTYPE[communication.precision]

    for name, parameter in model.named_parameters():
        metadata = _metadata(name, parameter)
        if parameter.grad is None:
            chunks.append(_empty_chunk(metadata))
            continue

        flat = parameter.grad.detach().cpu().contiguous().view(-1)
        payload = flat.to(payload_dtype)
        data = tensor_to_bytes(payload)
        chunks.append(
            GradientChunk(
                metadata=metadata,
                has_grad=True,
                data=data,
                byte_size=len(data),
                data_dtype=str(payload.dtype),
                encoding="dense",
                indices=[],
            )
        )
    return chunks


def _extract_per_layer_topk(
    model: torch.nn.Module,
    communication: CommunicationConfig,
    state: CompressionState,
) -> list[GradientChunk]:
    chunks: list[GradientChunk] = []
    payload_dtype = PRECISION_TO_DTYPE[communication.precision]
    selected_total = 0
    target_total = 0
    fallback_total = 0

    for name, parameter in model.named_parameters():
        metadata = _metadata(name, parameter)
        if parameter.grad is None:
            state.residuals.pop(name, None)
            chunks.append(_empty_chunk(metadata))
            continue

        effective = _effective_gradient(name, parameter, communication, state)
        selection = _select_indices(effective, communication)
        indices = selection.indices
        values = effective.index_select(0, indices)
        residual = effective.clone()
        if indices.numel() > 0:
            residual[indices] = 0
        state.residuals[name] = residual
        selected_total += int(indices.numel())
        target_total += selection.target_count
        fallback_total += int(selection.used_fallback)

        chunks.append(_sparse_chunk(metadata, values, indices, payload_dtype))
    state.last_metrics.update(
        {
            "compression_path": "per_layer_topk",
            "selected_value_count": selected_total,
            "target_value_count": target_total,
            "selection_fallback_count": fallback_total,
        }
    )
    return chunks


def _extract_global_topk(
    model: torch.nn.Module,
    communication: CommunicationConfig,
    state: CompressionState,
) -> list[GradientChunk]:
    entries: list[tuple[str, ParameterMetadata, torch.Tensor | None]] = []
    effective_values: list[torch.Tensor] = []
    devices: set[torch.device] = set()

    for name, parameter in model.named_parameters():
        metadata = _metadata(name, parameter)
        if parameter.grad is None:
            state.residuals.pop(name, None)
            entries.append((name, metadata, None))
            continue
        effective = _effective_gradient(name, parameter, communication, state)
        entries.append((name, metadata, effective))
        effective_values.append(effective)
        devices.add(effective.device)

    if not effective_values:
        return [_empty_chunk(metadata) for _, metadata, _ in entries]

    if len(devices) > 1:
        effective_values = [value.cpu() for value in effective_values]
        entries = [
            (name, metadata, None if effective is None else effective.cpu())
            for name, metadata, effective in entries
        ]
        state.last_metrics["mixed_device_global_fallback"] = 1

    global_gradient = torch.cat(effective_values)
    selection = _select_indices(global_gradient, communication)
    global_indices = selection.indices
    selected = torch.zeros(global_gradient.numel(), dtype=torch.bool)
    selected = selected.to(device=global_gradient.device)
    selected[global_indices] = True

    chunks: list[GradientChunk] = []
    payload_dtype = PRECISION_TO_DTYPE[communication.precision]
    offset = 0
    selected_total = 0
    for name, metadata, effective in entries:
        if effective is None:
            chunks.append(_empty_chunk(metadata))
            continue

        local_mask = selected[offset : offset + effective.numel()]
        local_indices = local_mask.nonzero(as_tuple=False).view(-1)
        values = effective.index_select(0, local_indices)
        residual = effective.clone()
        if local_indices.numel() > 0:
            residual[local_indices] = 0
        state.residuals[name] = residual
        selected_total += int(local_indices.numel())
        chunks.append(_sparse_chunk(metadata, values, local_indices, payload_dtype))
        offset += effective.numel()
    state.last_metrics.update(
        {
            "compression_path": "global_topk",
            "selected_value_count": selected_total,
            "target_value_count": selection.target_count,
            "selection_fallback_count": int(selection.used_fallback),
        }
    )
    return chunks


def _effective_gradient(
    name: str,
    parameter: torch.nn.Parameter,
    communication: CommunicationConfig,
    state: CompressionState,
) -> torch.Tensor:
    gradient = parameter.grad.detach().contiguous().view(-1).to(torch.float32)
    if communication.device == "cpu":
        gradient = gradient.cpu()
    elif communication.device == "gpu" and not gradient.is_cuda:
        gradient = gradient.cpu()
    residual = state.residuals.get(name)
    if (
        residual is None
        or residual.numel() != gradient.numel()
        or residual.device != gradient.device
    ):
        residual = torch.zeros_like(gradient)
    return gradient + residual


@dataclass
class _SelectionResult:
    indices: torch.Tensor
    target_count: int
    used_fallback: bool = False


def _select_indices(
    values: torch.Tensor,
    communication: CommunicationConfig,
) -> _SelectionResult:
    if communication.selection == "sampled_threshold":
        return _sampled_threshold_indices(values, communication)
    indices = _topk_indices(values, communication.top_k_percent)
    return _SelectionResult(
        indices=indices,
        target_count=_topk_count(values.numel(), communication.top_k_percent),
    )


def _topk_count(numel: int, percent: float) -> int:
    if numel <= 0:
        return 0
    return min(numel, max(1, math.ceil(numel * percent / 100.0)))


def _topk_indices(values: torch.Tensor, percent: float) -> torch.Tensor:
    if values.numel() == 0:
        return torch.empty(0, dtype=torch.long, device=values.device)
    k = _topk_count(values.numel(), percent)
    return torch.topk(values.abs(), k, sorted=False).indices.to(torch.long)


def _sampled_threshold_indices(
    values: torch.Tensor,
    communication: CommunicationConfig,
) -> _SelectionResult:
    numel = values.numel()
    if numel == 0:
        return _SelectionResult(
            indices=torch.empty(0, dtype=torch.long, device=values.device),
            target_count=0,
        )

    target_count = _topk_count(numel, communication.top_k_percent)
    sample_count = _topk_count(numel, communication.sample_rate_percent)
    if sample_count >= numel:
        return _SelectionResult(
            indices=_topk_indices(values, communication.top_k_percent),
            target_count=target_count,
            used_fallback=True,
        )

    abs_values = values.abs()
    sample_indices = torch.randint(
        low=0,
        high=numel,
        size=(sample_count,),
        device=values.device,
    )
    sample = abs_values.index_select(0, sample_indices)
    sample_target_count = _topk_count(sample_count, communication.top_k_percent)
    threshold = torch.topk(sample, sample_target_count, sorted=False).values.min()
    selected_mask = abs_values >= threshold
    selected_count = int(selected_mask.sum().item())

    max_count = max(
        target_count,
        min(numel, math.ceil(target_count * communication.max_payload_factor)),
    )
    min_count = max(1, target_count // 2)

    if selected_count == 0 or selected_count < min_count:
        return _SelectionResult(
            indices=_topk_indices(values, communication.top_k_percent),
            target_count=target_count,
            used_fallback=True,
        )

    selected_indices = selected_mask.nonzero(as_tuple=False).view(-1)
    if selected_count > max_count:
        candidate_values = abs_values.index_select(0, selected_indices)
        candidate_topk = torch.topk(
            candidate_values,
            target_count,
            sorted=False,
        ).indices
        selected_indices = selected_indices.index_select(0, candidate_topk)
        return _SelectionResult(
            indices=selected_indices.to(torch.long),
            target_count=target_count,
            used_fallback=True,
        )

    return _SelectionResult(
        indices=selected_indices.to(torch.long),
        target_count=target_count,
    )


def _sparse_chunk(
    metadata: ParameterMetadata,
    values: torch.Tensor,
    indices: torch.Tensor,
    payload_dtype: torch.dtype,
) -> GradientChunk:
    payload = values.to(payload_dtype).contiguous()
    data = tensor_to_bytes(payload)
    packed_indices = [int(value) for value in indices.detach().cpu().tolist()]
    return GradientChunk(
        metadata=metadata,
        has_grad=True,
        data=data,
        byte_size=len(data),
        data_dtype=str(payload.dtype),
        encoding="topk",
        indices=[],
        indices_u32=pack_u32_indices(packed_indices),
    )


def _now_ms() -> float:
    return time.perf_counter() * 1000.0


def _elapsed_ms(start_ms: float) -> float:
    return _now_ms() - start_ms


def _metadata(name: str, parameter: torch.nn.Parameter) -> ParameterMetadata:
    return ParameterMetadata(
        name=name,
        shape=tuple(parameter.shape),
        numel=parameter.numel(),
        dtype=str(parameter.dtype),
    )


def _empty_chunk(metadata: ParameterMetadata) -> GradientChunk:
    return GradientChunk(
        metadata=metadata,
        has_grad=False,
        data=None,
        byte_size=0,
        data_dtype=None,
        encoding="dense",
        indices=[],
        indices_u32=None,
    )
