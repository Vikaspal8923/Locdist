import torch
import torch.nn as nn

from locdist.compression import (
    CompressionState,
    extract_compressed_gradient_chunks,
)
from locdist.config import parse_communication_config
from locdist.exceptions import ConfigError
from locdist.gradients import apply_gradient_chunks
from locdist.indices import unpack_u32_indices
from locdist.models import GradientChunk, ParameterMetadata
from locdist.tensor_bytes import tensor_to_bytes


class TinyModel(nn.Module):
    def __init__(self):
        super().__init__()
        self.a = nn.Parameter(torch.zeros(4))
        self.b = nn.Parameter(torch.zeros(4))


def test_per_layer_topk_keeps_each_parameter():
    model = TinyModel()
    model.a.grad = torch.tensor([1.0, -4.0, 2.0, 3.0])
    model.b.grad = torch.tensor([0.5, -0.1, 7.0, 0.2])
    config = parse_communication_config(
        {
            "precision": "fp16",
            "compression": {
                "type": "topk",
                "mode": "per_layer",
                "top_k": "25%",
                "error_feedback": True,
            },
        }
    )

    chunks = extract_compressed_gradient_chunks(model, config, CompressionState())

    assert chunks[0].encoding == "topk"
    assert unpack_u32_indices(chunks[0].indices_u32) == [1]
    assert chunks[0].data_dtype == "torch.float16"
    assert unpack_u32_indices(chunks[1].indices_u32) == [2]


def test_global_topk_selects_across_all_parameters():
    model = TinyModel()
    model.a.grad = torch.tensor([1.0, -4.0, 2.0, 3.0])
    model.b.grad = torch.tensor([0.5, -0.1, 7.0, 0.2])
    config = parse_communication_config(
        {
            "compression": {
                "type": "topk",
                "mode": "global",
                "top_k": "25%",
                "error_feedback": True,
            },
        }
    )

    chunks = extract_compressed_gradient_chunks(model, config, CompressionState())
    selected = {
        chunk.metadata.name: unpack_u32_indices(chunk.indices_u32)
        for chunk in chunks
    }

    assert selected == {"a": [1], "b": [2]}


def test_error_feedback_reuses_dropped_values():
    model = TinyModel()
    state = CompressionState()
    config = parse_communication_config(
        {
            "compression": {
                "type": "topk",
                "mode": "per_layer",
                "top_k": "25%",
                "error_feedback": True,
            },
        }
    )

    model.a.grad = torch.tensor([1.0, 4.0, 2.0, 3.0])
    model.b.grad = torch.zeros(4)
    extract_compressed_gradient_chunks(model, config, state)

    model.a.grad = torch.zeros(4)
    model.b.grad = torch.zeros(4)
    chunks = extract_compressed_gradient_chunks(model, config, state)

    assert unpack_u32_indices(chunks[0].indices_u32) == [3]


def test_warmup_sends_dense_before_topk():
    model = TinyModel()
    model.a.grad = torch.ones(4)
    model.b.grad = torch.ones(4)
    config = parse_communication_config(
        {
            "compression": {
                "type": "topk",
                "mode": "per_layer",
                "top_k": "25%",
                "error_feedback": True,
                "warmup_steps": 1,
            },
        }
    )
    state = CompressionState()

    first = extract_compressed_gradient_chunks(model, config, state)
    second = extract_compressed_gradient_chunks(model, config, state)

    assert first[0].encoding == "dense"
    assert second[0].encoding == "topk"


def test_topk_defaults_and_rejects_disabled_error_feedback():
    config = parse_communication_config(
        {"compression": {"type": "topk", "error_feedback": True}}
    )

    assert config.compression_mode == "global"
    assert config.top_k_percent == 5.0

    try:
        parse_communication_config(
            {"compression": {"type": "topk", "error_feedback": False}}
        )
    except ConfigError:
        return
    raise AssertionError("expected disabled error feedback to be rejected")


def test_sparse_response_applies_only_returned_indices():
    model = TinyModel()
    chunk = GradientChunk(
        metadata=ParameterMetadata(
            name="a",
            shape=(4,),
            numel=4,
            dtype="torch.float32",
        ),
        has_grad=True,
        data=tensor_to_bytes(torch.tensor([2.0, 8.0], dtype=torch.float32)),
        byte_size=8,
        data_dtype="torch.float32",
        encoding="topk",
        indices=[1, 3],
    )
    empty = GradientChunk(
        metadata=ParameterMetadata(
            name="b",
            shape=(4,),
            numel=4,
            dtype="torch.float32",
        ),
        has_grad=False,
        data=None,
        byte_size=0,
    )

    apply_gradient_chunks(model, [chunk, empty])

    assert torch.equal(
        model.a.grad,
        torch.tensor([0.0, 2.0, 0.0, 8.0]),
    )
    assert model.b.grad is None


def main():
    test_per_layer_topk_keeps_each_parameter()
    test_global_topk_selects_across_all_parameters()
    test_error_feedback_reuses_dropped_values()
    test_warmup_sends_dense_before_topk()
    test_topk_defaults_and_rejects_disabled_error_feedback()
    test_sparse_response_applies_only_returned_indices()
    print("✓ Compression tests passed")


if __name__ == "__main__":
    main()
