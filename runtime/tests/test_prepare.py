import json
import os
from pathlib import Path
import time
import torch
import torch.nn as nn

import locdist
from locdist.prepare import get_prepared_runtime_state
from locdist.models import AggregatedGradientChunkPackage
import locdist.transport as transport_module


class TinyModel(nn.Module):
    def __init__(self):
        super().__init__()
        self.fc1 = nn.Linear(4, 3)
        self.fc2 = nn.Linear(3, 2)

    def forward(self, x):
        return self.fc2(self.fc1(x))


class FakeTransport:
    def __init__(self):
        self.last_metrics = {}
        self.chunk_calls = []
        self.batch_calls = []

    def synchronize_chunk(self, package):
        time.sleep(0.01)
        self.chunk_calls.append(package.chunk.metadata.layer_order)
        self.last_metrics = {
            "runtime_to_worker_proto_build_ms": 1.0,
            "runtime_to_worker_rpc_ms": 2.0,
            "runtime_response_decode_ms": 0.5,
            "runtime_bytes_up": 128,
            "runtime_bytes_down": 256,
        }
        return AggregatedGradientChunkPackage(
            runtime_version=package.runtime_version,
            job_id=package.job_id,
            participating_workers=1,
            aggregation_round=package.chunk.sync_round,
            chunk=package.chunk,
        )

    def synchronize_chunk_batch(self, package):
        time.sleep(0.01)
        self.batch_calls.append([chunk.metadata.layer_order for chunk in package.chunks])
        self.last_metrics = {
            "runtime_to_worker_proto_build_ms": 1.0,
            "runtime_to_worker_rpc_ms": 2.0,
            "runtime_response_decode_ms": 0.5,
            "runtime_bytes_up": 256,
            "runtime_bytes_down": 512,
        }
        raise AssertionError("legacy batch response path should not be used in async test")

    def synchronize_chunk_batch_stream(self, package):
        time.sleep(0.01)
        self.batch_calls.append([chunk.metadata.layer_order for chunk in package.chunks])
        for index, chunk in enumerate(package.chunks):
            yield AggregatedGradientChunkPackage(
                runtime_version=package.runtime_version,
                job_id=package.job_id,
                participating_workers=1,
                aggregation_round=chunk.sync_round,
                chunk=chunk,
            ), {
                "runtime_to_worker_proto_build_ms": 1.0 if index == 0 else 0.0,
                "runtime_to_worker_rpc_ms": 2.0,
                "runtime_response_decode_ms": 0.5,
                "runtime_bytes_up": 256 if index == 0 else 0,
                "runtime_bytes_down": 128,
                "transport_mode": "chunk_batch_stream",
            }


class FakeBadTransport(FakeTransport):
    def synchronize_chunk(self, package):
        response = super().synchronize_chunk(package)
        response.job_id = "wrong-job"
        return response

    def synchronize_chunk_batch_stream(self, package):
        for response, metrics in super().synchronize_chunk_batch_stream(package):
            response.job_id = "wrong-job"
            yield response, metrics


class FakeHangingTransport(FakeTransport):
    def synchronize_chunk(self, package):
        time.sleep(2.0)
        return super().synchronize_chunk(package)

    def synchronize_chunk_batch_stream(self, package):
        time.sleep(2.0)
        yield from super().synchronize_chunk_batch_stream(package)


def main():
    fake_transport = FakeTransport()
    original_get_transport = transport_module.get_transport
    state = None
    original_metrics_path = os.environ.get("LDGCC_SYNC_METRICS_PATH")
    metrics_file = Path("/tmp/ldgcc-test-prepare-metrics.jsonl")
    if metrics_file.exists():
        metrics_file.unlink()
    os.environ["LDGCC_SYNC_METRICS_PATH"] = str(metrics_file)
    transport_module.get_transport = lambda: fake_transport
    try:
        model = TinyModel()
        model.fc1.weight.requires_grad = False
        model = locdist.prepare(model)
        state = get_prepared_runtime_state(model)
        assert state is not None
        assert len(state.expected_layers) == len(list(model.named_parameters()))
        state.configure_runtime(
            runtime_version=1,
            job_id="job-test",
            worker_id="worker-test",
            rpc_timeout_seconds=5,
        )

        optimizer = torch.optim.SGD(model.parameters(), lr=0.1)
        optimizer = locdist.prepare_optimizer(optimizer)

        x = torch.randn(8, 4)
        loss = model(x).sum()
        loss.backward()

        assert state.active_round == 1
        assert len(state.ready_layers) == len(state.expected_layers) - 1
        assert len(state.queued_chunks[1]) == len(state.expected_layers) - 1
        assert state.emitted_chunk_count == len(state.expected_layers) - 1

        round_id, finalized_chunks = state.finalize_round_chunks()
        assert round_id == 1
        assert len(finalized_chunks) == len(state.expected_layers)

        queued_chunk = state.queued_chunks[1][1]
        assert queued_chunk.sync_round == 1
        assert queued_chunk.metadata.layer_order == 1

        frozen_chunk = state.queued_chunks[1][0]
        assert frozen_chunk.metadata.layer_order == 0
        assert frozen_chunk.has_grad is False

        time.sleep(0.05)
        assert fake_transport.chunk_calls or fake_transport.batch_calls, "sender thread did not begin chunk transport before optimizer.step()"

        optimizer.step()

        assert state.active_round is None
        assert state.ready_layers == {}
        assert state.returned_layers == set()
        assert state.completed_rounds == [1]
        assert state.step_count == 1
        sent_layers = set(fake_transport.chunk_calls)
        for batch in fake_transport.batch_calls:
            sent_layers.update(batch)
        assert sent_layers == {0, 1, 2, 3}
        assert metrics_file.exists()

        metric = json.loads(metrics_file.read_text(encoding="utf-8").strip().splitlines()[-1])
        assert metric["sync_mode"] == "chunk_async"
        assert metric["chunk_count"] == 4
        assert metric["chunk_send_count"] == 4
        assert metric["chunk_response_count"] == 4
        assert metric["batch_send_count"] >= 1
        assert metric["max_chunks_per_batch"] >= 1
        assert metric["max_queue_depth"] >= 1
        assert metric["max_inflight_chunks"] >= 1
        assert metric["step_wait_ms"] >= 0.0
        assert metric["first_layer_ready_ms"] > 0.0
        assert metric["first_chunk_sent_ms"] > 0.0
        assert metric["last_chunk_returned_ms"] >= metric["first_chunk_returned_ms"]

        print("✓ prepare(model) registered runtime hooks")
        print("✓ prepare_optimizer(optimizer) wrapped optimizer step")
        print("✓ async chunk sender started before optimizer.step()")
        print("✓ async round metrics captured overlap and queue state")
    finally:
        if state is not None:
            state.shutdown()
        transport_module.get_transport = original_get_transport
        if original_metrics_path is None:
            os.environ.pop("LDGCC_SYNC_METRICS_PATH", None)
        else:
            os.environ["LDGCC_SYNC_METRICS_PATH"] = original_metrics_path
        if metrics_file.exists():
            metrics_file.unlink()


def test_invalid_chunk_response_is_rejected():
    bad_transport = FakeBadTransport()
    original_get_transport = transport_module.get_transport
    state = None
    transport_module.get_transport = lambda: bad_transport
    try:
        model = TinyModel()
        model = locdist.prepare(model)
        state = get_prepared_runtime_state(model)
        assert state is not None
        state.configure_runtime(
            runtime_version=1,
            job_id="job-test",
            worker_id="worker-test",
            rpc_timeout_seconds=1,
        )
        optimizer = locdist.prepare_optimizer(torch.optim.SGD(model.parameters(), lr=0.1))
        loss = model(torch.randn(4, 4)).sum()
        loss.backward()
        try:
            optimizer.step()
        except Exception as exc:
            assert "Prepared runtime round 1 failed" in str(exc)
            assert "job_id mismatch" in str(exc)
        else:
            raise AssertionError("expected invalid returned chunk response to fail")
        print("✓ invalid async chunk response is rejected")
    finally:
        if state is not None:
            state.shutdown()
        transport_module.get_transport = original_get_transport


def test_accumulation_boundary_starts_round_at_optimizer_step():
    fake_transport = FakeTransport()
    original_get_transport = transport_module.get_transport
    state = None
    transport_module.get_transport = lambda: fake_transport
    try:
        model = TinyModel()
        model = locdist.prepare(model)
        state = get_prepared_runtime_state(model)
        assert state is not None
        state.gradient_accumulation_steps = 10
        state.configure_runtime(
            runtime_version=1,
            job_id="job-accum",
            worker_id="worker-accum",
            rpc_timeout_seconds=5,
        )
        optimizer = locdist.prepare_optimizer(torch.optim.SGD(model.parameters(), lr=0.1))
        loss = model(torch.randn(4, 4)).sum()
        loss.backward()

        time.sleep(0.05)
        assert state.active_round is None
        assert fake_transport.chunk_calls == []
        assert fake_transport.batch_calls == []

        optimizer.step()

        assert state.completed_rounds == [1]
        sent_layers = set(fake_transport.chunk_calls)
        for batch in fake_transport.batch_calls:
            sent_layers.update(batch)
        assert sent_layers == {0, 1, 2, 3}
        print("✓ accumulation-aware V2 waits until optimizer.step() before sending")
    finally:
        if state is not None:
            state.shutdown()
        transport_module.get_transport = original_get_transport


def test_round_timeout_is_cleaned_up():
    hanging_transport = FakeHangingTransport()
    original_get_transport = transport_module.get_transport
    original_metrics_path = os.environ.get("LDGCC_SYNC_METRICS_PATH")
    metrics_file = Path("/tmp/ldgcc-test-prepare-timeout-metrics.jsonl")
    if metrics_file.exists():
        metrics_file.unlink()
    os.environ["LDGCC_SYNC_METRICS_PATH"] = str(metrics_file)
    state = None
    transport_module.get_transport = lambda: hanging_transport
    try:
        model = TinyModel()
        model = locdist.prepare(model)
        state = get_prepared_runtime_state(model)
        assert state is not None
        state.configure_runtime(
            runtime_version=1,
            job_id="job-timeout",
            worker_id="worker-timeout",
            rpc_timeout_seconds=1,
        )
        optimizer = locdist.prepare_optimizer(torch.optim.SGD(model.parameters(), lr=0.1))
        loss = model(torch.randn(4, 4)).sum()
        loss.backward()
        try:
            optimizer.step()
        except Exception as exc:
            assert "Timed out waiting for aggregated chunks" in str(exc)
        else:
            raise AssertionError("expected timeout cleanup failure")
        assert state.active_round is None
        assert len(state.outgoing_order) == 0
        assert len(state.inflight_layers) == 0
        assert metrics_file.exists()
        metric = json.loads(metrics_file.read_text(encoding="utf-8").strip().splitlines()[-1])
        assert metric["round_status"] == "timeout"
        assert "failure_reason" in metric
        print("✓ timeout round cleanup emits terminal metric and clears async state")
    finally:
        if state is not None:
            state.shutdown()
        transport_module.get_transport = original_get_transport
        if original_metrics_path is None:
            os.environ.pop("LDGCC_SYNC_METRICS_PATH", None)
        else:
            os.environ["LDGCC_SYNC_METRICS_PATH"] = original_metrics_path
        if metrics_file.exists():
            metrics_file.unlink()


if __name__ == "__main__":
    main()
    test_invalid_chunk_response_is_rejected()
    test_accumulation_boundary_starts_round_at_optimizer_step()
    test_round_timeout_is_cleaned_up()
