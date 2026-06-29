import math
from dataclasses import dataclass, field

import torch

from locdist.models import (
    CommunicationConfig,
    GradientChunk,
    ParameterMetadata,
)


PRECISION_TO_DTYPE = {
    "fp32": torch.float32,
    "fp16": torch.float16,
}


@dataclass
class CompressionState:
    sync_step: int = 0
    residuals: dict[str, torch.Tensor] = field(default_factory=dict)


def extract_compressed_gradient_chunks(
    model: torch.nn.Module,
    communication: CommunicationConfig,
    state: CompressionState,
) -> list[GradientChunk]:
    state.sync_step += 1

    if (
        communication.compression_type != "topk"
        or state.sync_step <= communication.warmup_steps
    ):
        return _extract_dense(model, communication)

    if communication.compression_mode == "per_layer":
        return _extract_per_layer_topk(model, communication, state)
    return _extract_global_topk(model, communication, state)


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
        data = payload.numpy().tobytes()
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

    for name, parameter in model.named_parameters():
        metadata = _metadata(name, parameter)
        if parameter.grad is None:
            state.residuals.pop(name, None)
            chunks.append(_empty_chunk(metadata))
            continue

        effective = _effective_gradient(name, parameter, state)
        indices = _topk_indices(effective, communication.top_k_percent)
        values = effective.index_select(0, indices)
        residual = effective.clone()
        residual[indices] = 0
        state.residuals[name] = residual

        chunks.append(_sparse_chunk(metadata, values, indices, payload_dtype))
    return chunks


def _extract_global_topk(
    model: torch.nn.Module,
    communication: CommunicationConfig,
    state: CompressionState,
) -> list[GradientChunk]:
    entries: list[tuple[str, ParameterMetadata, torch.Tensor | None]] = []
    effective_values: list[torch.Tensor] = []

    for name, parameter in model.named_parameters():
        metadata = _metadata(name, parameter)
        if parameter.grad is None:
            state.residuals.pop(name, None)
            entries.append((name, metadata, None))
            continue
        effective = _effective_gradient(name, parameter, state)
        entries.append((name, metadata, effective))
        effective_values.append(effective)

    if not effective_values:
        return [_empty_chunk(metadata) for _, metadata, _ in entries]

    global_gradient = torch.cat(effective_values)
    global_indices = _topk_indices(
        global_gradient,
        communication.top_k_percent,
    )
    selected = torch.zeros(global_gradient.numel(), dtype=torch.bool)
    selected[global_indices] = True

    chunks: list[GradientChunk] = []
    payload_dtype = PRECISION_TO_DTYPE[communication.precision]
    offset = 0
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
        chunks.append(_sparse_chunk(metadata, values, local_indices, payload_dtype))
        offset += effective.numel()
    return chunks


def _effective_gradient(
    name: str,
    parameter: torch.nn.Parameter,
    state: CompressionState,
) -> torch.Tensor:
    gradient = (
        parameter.grad.detach().cpu().contiguous().view(-1).to(torch.float32)
    )
    residual = state.residuals.get(name)
    if residual is None or residual.numel() != gradient.numel():
        residual = torch.zeros_like(gradient)
    return gradient + residual


def _topk_indices(values: torch.Tensor, percent: float) -> torch.Tensor:
    if values.numel() == 0:
        return torch.empty(0, dtype=torch.long)
    k = max(1, math.ceil(values.numel() * percent / 100.0))
    k = min(k, values.numel())
    return torch.topk(values.abs(), k, sorted=False).indices.to(torch.long)


def _sparse_chunk(
    metadata: ParameterMetadata,
    values: torch.Tensor,
    indices: torch.Tensor,
    payload_dtype: torch.dtype,
) -> GradientChunk:
    payload = values.to(payload_dtype).contiguous()
    data = payload.numpy().tobytes()
    return GradientChunk(
        metadata=metadata,
        has_grad=True,
        data=data,
        byte_size=len(data),
        data_dtype=str(payload.dtype),
        encoding="topk",
        indices=[int(value) for value in indices.tolist()],
    )


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
    )
