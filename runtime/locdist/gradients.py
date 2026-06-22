from typing import List

import torch

from locdist.models import (
    GradientChunk,
    ParameterMetadata,
)


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

        gradient_bytes = (
            parameter.grad
            .detach()
            .cpu()
            .contiguous()
            .view(-1)
            .numpy()
            .tobytes()
        )

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

        tensor = torch.frombuffer(
            bytearray(chunk.data),
            dtype=TORCH_DTYPE_MAP[
                chunk.metadata.dtype
            ],
        )

        tensor = tensor.reshape(
            chunk.metadata.shape
        )

        parameter.grad = (
            tensor
            .clone()
            .to(parameter.device)
        )