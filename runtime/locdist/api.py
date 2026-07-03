from locdist.config import load_config

from locdist.models import (
    GradientPackage,
)

from locdist.gradients import (
    apply_gradient_chunks,
)

from locdist.compression import (
    CompressionState,
    extract_compressed_gradient_chunks,
)

from locdist.transport import (
    get_transport,
)

from locdist.metrics import append_jsonl, now_ms


_config = None
_compression_state = CompressionState()


def get_runtime_config():

    global _config

    if _config is None:

        _config = load_config()

    return _config


def sync_gradients(model) -> None:

    config = get_runtime_config()

    total_start_ms = now_ms()
    extract_start_ms = total_start_ms
    chunks = extract_compressed_gradient_chunks(
        model,
        config.communication,
        _compression_state,
    )
    extract_done_ms = now_ms()

    package = GradientPackage(
        runtime_version=(
            config.runtime_version
        ),
        job_id=config.job_id,
        worker_id=config.worker_id,
        chunks=chunks,
    )
    package_done_ms = now_ms()

    transport = get_transport()

    aggregated_package = (
        transport.synchronize(
            package
        )
    )
    transport_done_ms = now_ms()

    apply_gradient_chunks(
        model,
        aggregated_package.chunks,
    )
    apply_done_ms = now_ms()

    append_jsonl(
        "ldgcc_runtime_sync_metrics.jsonl",
        {
            "component": "runtime",
            "job_id": config.job_id,
            "worker_id": config.worker_id,
            "sync_step": _compression_state.sync_step,
            "total_ms": apply_done_ms - total_start_ms,
            "extract_gradients_ms": extract_done_ms - extract_start_ms,
            "package_object_ms": package_done_ms - extract_done_ms,
            "transport_call_ms": transport_done_ms - package_done_ms,
            "apply_gradients_ms": apply_done_ms - transport_done_ms,
            "chunk_count": len(chunks),
            **_compression_state.last_metrics,
            **getattr(transport, "last_metrics", {}),
        },
    )
