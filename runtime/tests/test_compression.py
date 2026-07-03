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


class SmallLayerModel(nn.Module):
    def __init__(self):
        super().__init__()
        self.a = nn.Parameter(torch.zeros(1))
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


def test_global_topk_can_emit_empty_local_chunks():
    model = TinyModel()
    model.a.grad = torch.tensor([100.0, 90.0, 80.0, 70.0])
    model.b.grad = torch.tensor([0.5, -0.1, 0.7, 0.2])
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

    assert unpack_u32_indices(chunks[0].indices_u32) == [0, 1]
    assert unpack_u32_indices(chunks[1].indices_u32) == []
    assert chunks[1].data == b""
    assert chunks[1].byte_size == 0


def test_per_layer_topk_keeps_one_entry_for_small_layers():
    model = SmallLayerModel()
    model.a.grad = torch.tensor([1.0])
    model.b.grad = torch.tensor([0.0, 0.0, 0.0, 0.0])
    config = parse_communication_config(
        {
            "compression": {
                "type": "topk",
                "mode": "per_layer",
                "top_k": "0.5%",
                "error_feedback": True,
            },
        }
    )

    chunks = extract_compressed_gradient_chunks(model, config, CompressionState())

    assert unpack_u32_indices(chunks[0].indices_u32) == [0]
    assert len(unpack_u32_indices(chunks[1].indices_u32)) == 1


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

    assert config.compression_mode == "per_layer"
    assert config.top_k_percent == 5.0
    assert config.selection == "exact"
    assert config.sample_rate_percent == 1.0
    assert config.max_payload_factor == 1.5
    assert config.device == "auto"

    try:
        parse_communication_config(
            {"compression": {"type": "topk", "error_feedback": False}}
        )
    except ConfigError:
        return
    raise AssertionError("expected disabled error feedback to be rejected")


def test_sampled_threshold_config_and_exact_fallback():
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
                "selection": "sampled_threshold",
                "sample_rate": "100%",
                "max_payload_factor": 1.5,
                "device": "auto",
                "error_feedback": True,
            },
        }
    )
    state = CompressionState()

    chunks = extract_compressed_gradient_chunks(model, config, state)

    assert config.selection == "sampled_threshold"
    assert config.sample_rate_percent == 100.0
    assert unpack_u32_indices(chunks[0].indices_u32) == [1]
    assert unpack_u32_indices(chunks[1].indices_u32) == [2]
    assert state.last_metrics["compression_path"] == "per_layer_topk"
    assert state.last_metrics["selection_fallback_count"] == 2
    assert state.last_metrics["selected_value_count"] == 2


def test_sampled_threshold_keeps_error_feedback():
    model = TinyModel()
    state = CompressionState()
    config = parse_communication_config(
        {
            "compression": {
                "type": "topk",
                "mode": "per_layer",
                "top_k": "25%",
                "selection": "sampled_threshold",
                "sample_rate": "100%",
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


def test_sparse_response_allows_empty_topk_chunk():
    model = TinyModel()
    chunk_a = GradientChunk(
        metadata=ParameterMetadata(
            name="a",
            shape=(4,),
            numel=4,
            dtype="torch.float32",
        ),
        has_grad=True,
        data=b"",
        byte_size=0,
        data_dtype="torch.float16",
        encoding="topk",
        indices=[],
        indices_u32=b"",
    )
    chunk_b = GradientChunk(
        metadata=ParameterMetadata(
            name="b",
            shape=(4,),
            numel=4,
            dtype="torch.float32",
        ),
        has_grad=True,
        data=tensor_to_bytes(torch.tensor([3.0], dtype=torch.float16)),
        byte_size=2,
        data_dtype="torch.float16",
        encoding="topk",
        indices=[2],
    )

    apply_gradient_chunks(model, [chunk_a, chunk_b])

    assert torch.equal(model.a.grad, torch.zeros(4))
    assert torch.equal(
        model.b.grad,
        torch.tensor([0.0, 0.0, 3.0, 0.0]),
    )


def test_sparse_response_uses_packed_u32_indices():
    model = TinyModel()
    chunk = GradientChunk(
        metadata=ParameterMetadata(
            name="a",
            shape=(4,),
            numel=4,
            dtype="torch.float32",
        ),
        has_grad=True,
        data=tensor_to_bytes(torch.tensor([5.0], dtype=torch.float16)),
        byte_size=2,
        data_dtype="torch.float16",
        encoding="topk",
        indices=[],
        indices_u32=bytes([2, 0, 0, 0]),
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
        torch.tensor([0.0, 0.0, 5.0, 0.0]),
    )


def test_sparse_response_rejects_data_index_mismatch():
    model = TinyModel()
    chunk = GradientChunk(
        metadata=ParameterMetadata(
            name="a",
            shape=(4,),
            numel=4,
            dtype="torch.float32",
        ),
        has_grad=True,
        data=tensor_to_bytes(torch.tensor([1.0], dtype=torch.float16)),
        byte_size=2,
        data_dtype="torch.float16",
        encoding="topk",
        indices=[0, 1],
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

    try:
        apply_gradient_chunks(model, [chunk, empty])
    except ValueError as error:
        assert "Sparse gradient data size mismatch" in str(error)
        return
    raise AssertionError("expected sparse mismatch to be rejected")


def test_dense_response_rejects_data_size_mismatch():
    model = TinyModel()
    chunk = GradientChunk(
        metadata=ParameterMetadata(
            name="a",
            shape=(4,),
            numel=4,
            dtype="torch.float32",
        ),
        has_grad=True,
        data=tensor_to_bytes(torch.tensor([1.0], dtype=torch.float32)),
        byte_size=4,
        data_dtype="torch.float32",
        encoding="dense",
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

    try:
        apply_gradient_chunks(model, [chunk, empty])
    except ValueError as error:
        assert "Dense gradient data size mismatch" in str(error)
        return
    raise AssertionError("expected dense mismatch to be rejected")


def test_response_rejects_shape_numel_mismatch():
    model = TinyModel()
    chunk = GradientChunk(
        metadata=ParameterMetadata(
            name="a",
            shape=(2, 3),
            numel=4,
            dtype="torch.float32",
        ),
        has_grad=True,
        data=tensor_to_bytes(torch.ones(4, dtype=torch.float32)),
        byte_size=16,
        data_dtype="torch.float32",
        encoding="dense",
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

    try:
        apply_gradient_chunks(model, [chunk, empty])
    except ValueError as error:
        assert "shape/numel mismatch" in str(error)
        return
    raise AssertionError("expected metadata mismatch to be rejected")


def main():
    test_per_layer_topk_keeps_each_parameter()
    test_global_topk_selects_across_all_parameters()
    test_global_topk_can_emit_empty_local_chunks()
    test_per_layer_topk_keeps_one_entry_for_small_layers()
    test_error_feedback_reuses_dropped_values()
    test_warmup_sends_dense_before_topk()
    test_topk_defaults_and_rejects_disabled_error_feedback()
    test_sampled_threshold_config_and_exact_fallback()
    test_sampled_threshold_keeps_error_feedback()
    test_sparse_response_applies_only_returned_indices()
    test_sparse_response_allows_empty_topk_chunk()
    test_sparse_response_uses_packed_u32_indices()
    test_sparse_response_rejects_data_index_mismatch()
    test_dense_response_rejects_data_size_mismatch()
    test_response_rejects_shape_numel_mismatch()
    print("✓ Compression tests passed")


if __name__ == "__main__":
    main()
