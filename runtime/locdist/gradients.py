from typing import List
import math

import torch

from locdist.models import (
    GradientChunk,
    ParameterMetadata,
)
from locdist.indices import unpack_u32_indices
from locdist.tensor_bytes import tensor_to_bytes


TORCH_DTYPE_MAP = {
    "torch.float16": torch.float16,
    "torch.float32": torch.float32,
    "torch.float64": torch.float64,
    "torch.bfloat16": torch.bfloat16,
}


def _expected_dtype_size(dtype: torch.dtype) -> int:
    return torch.empty((), dtype=dtype).element_size()


def _validate_shape(metadata: ParameterMetadata) -> None:
    expected = math.prod(metadata.shape)
    if expected != metadata.numel:
        raise ValueError(
            f"Gradient metadata shape/numel mismatch for {metadata.name}: "
            f"{metadata.shape} != {metadata.numel}"
        )


def extract_gradient_chunks(
    model: torch.nn.Module,
) -> List[GradientChunk]:
    """
    Extract gradients and serialize them into raw bytes.
    """

    chunks = []

    for name, parameter in model.named_parameters():

        metadata = ParameterMetadata(
            name=name,
            shape=tuple(parameter.shape),
            numel=parameter.numel(),
            dtype=str(parameter.dtype),
        )

        if parameter.grad is None:

            chunks.append(
                GradientChunk(
                    metadata=metadata,
                    has_grad=False,
                    data=None,
                    byte_size=0,
                )
            )

            continue

        gradient_bytes = tensor_to_bytes(parameter.grad.view(-1))

        chunks.append(
            GradientChunk(
                metadata=metadata,
                has_grad=True,
                data=gradient_bytes,
                byte_size=len(gradient_bytes),
            )
        )

    return chunks


def apply_gradient_chunks(
    model: torch.nn.Module,
    chunks: List[GradientChunk],
) -> None:
    """
    Restore gradients back into the model.
    """

    parameters = list(model.named_parameters())

    if len(parameters) != len(chunks):
        raise ValueError(
            f"Parameter count mismatch "
            f"({len(parameters)} vs {len(chunks)})"
        )

    for (name, parameter), chunk in zip(
        parameters,
        chunks,
    ):

        if name != chunk.metadata.name:
            raise ValueError(
                f"Parameter mismatch: "
                f"{name} != {chunk.metadata.name}"
            )
        _validate_shape(chunk.metadata)

        if not chunk.has_grad:
            parameter.grad = None
            continue

        if chunk.encoding == "topk":
            parameter.grad = _sparse_chunk_to_tensor(
                parameter,
                chunk,
            )
            continue

        dtype = TORCH_DTYPE_MAP[
            chunk.data_dtype
            or chunk.metadata.dtype
        ]
        expected_bytes = chunk.metadata.numel * _expected_dtype_size(dtype)
        actual_bytes = len(chunk.data or b"")
        if chunk.byte_size != actual_bytes:
            raise ValueError("Dense gradient byte_size does not match data length")
        if actual_bytes != expected_bytes:
            raise ValueError("Dense gradient data size mismatch")

        tensor = torch.frombuffer(
            bytearray(chunk.data),
            dtype=dtype,
        )

        tensor = tensor.reshape(
            chunk.metadata.shape
        )

        parameter.grad = (
            tensor
            .clone()
            .to(device=parameter.device, dtype=parameter.dtype)
        )


def _sparse_chunk_to_tensor(
    parameter: torch.nn.Parameter,
    chunk: GradientChunk,
) -> torch.Tensor:
    if chunk.data is None:
        raise ValueError("Sparse gradient chunk has no data")
    indices = (
        unpack_u32_indices(chunk.indices_u32)
        if chunk.indices_u32
        else chunk.indices or []
    )
    dtype = TORCH_DTYPE_MAP[
        chunk.data_dtype
        or chunk.metadata.dtype
    ]
    expected_bytes = len(indices) * _expected_dtype_size(dtype)
    actual_bytes = len(chunk.data)
    if chunk.byte_size != actual_bytes:
        raise ValueError("Sparse gradient byte_size does not match data length")
    if actual_bytes != expected_bytes:
        raise ValueError("Sparse gradient data size mismatch")
    if not indices and not chunk.data:
        return _zero_sparse_gradient(parameter, chunk)
    values = torch.frombuffer(
        bytearray(chunk.data),
        dtype=dtype,
    )
    if values.numel() != len(indices):
        raise ValueError(
            "Sparse gradient index/value count mismatch"
        )
    if len(set(indices)) != len(indices):
        raise ValueError("Duplicate sparse gradient index")
    if any(index < 0 or index >= chunk.metadata.numel for index in indices):
        raise ValueError("Sparse gradient index out of bounds")

    gradient = _zero_sparse_gradient(parameter, chunk)

    if indices:
        device_indices = torch.tensor(
            indices,
            dtype=torch.long,
            device=parameter.device,
        )
        device_values = values.to(
            device=parameter.device,
            dtype=parameter.dtype,
        )
        gradient.view(-1).index_copy_(0, device_indices, device_values)

    return gradient


def _zero_sparse_gradient(
    parameter: torch.nn.Parameter,
    chunk: GradientChunk,
) -> torch.Tensor:
    if (
        parameter.grad is not None
        and tuple(parameter.grad.shape) == tuple(chunk.metadata.shape)
        and parameter.grad.device == parameter.device
        and parameter.grad.dtype == parameter.dtype
    ):
        parameter.grad.zero_()
        return parameter.grad

    return torch.zeros(
        chunk.metadata.shape,
        device=parameter.device,
        dtype=parameter.dtype,
    )
