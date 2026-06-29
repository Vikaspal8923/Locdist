from typing import List

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

        if not chunk.has_grad:
            parameter.grad = None
            continue

        if chunk.encoding == "topk":
            parameter.grad = _sparse_chunk_to_tensor(
                parameter,
                chunk,
            )
            continue

        tensor = torch.frombuffer(
            bytearray(chunk.data),
            dtype=TORCH_DTYPE_MAP[
                chunk.data_dtype
                or chunk.metadata.dtype
            ],
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
    flat = torch.zeros(
        chunk.metadata.numel,
        dtype=values.dtype,
    )
    if indices:
        flat[
            torch.tensor(indices, dtype=torch.long)
        ] = values
    return (
        flat
        .reshape(chunk.metadata.shape)
        .to(device=parameter.device, dtype=parameter.dtype)
    )
