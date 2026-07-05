from locdist.config import load_config
from locdist.exceptions import ConfigError

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
from locdist.prepare import (
    get_prepared_runtime_state,
    prepare_model,
    prepare_wrapped_optimizer,
)


_config = None
_compression_state = CompressionState()
_prepared_model = None


def get_runtime_config():

    global _config

    if _config is None:

        _config = load_config()

    return _config


def sync_gradients(model) -> None:

    config = get_runtime_config()
    prepared_state = get_prepared_runtime_state(model)
    transport = get_transport()
    total_start_ms = now_ms()

    if prepared_state is not None and prepared_state.active_round is not None:
        _compression_state.sync_step = prepared_state.active_round
        prepared_state.complete_active_round(model)
        return

    if prepared_state is not None:
        prepared_state.note_fallback_sync()

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

    if prepared_state is not None:
        prepared_state.mark_layers_returned(
            [
                chunk.metadata.layer_order
                for chunk in aggregated_package.chunks
            ]
        )

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


def prepare(model):
    global _prepared_model
    prepared = prepare_model(model)
    runtime_state = get_prepared_runtime_state(prepared)
    if runtime_state is not None:
        try:
            config = get_runtime_config()
        except ConfigError:
            config = None
        if config is not None:
            runtime_state.communication = config.communication
            runtime_state.gradient_accumulation_steps = (
                config.gradient_accumulation_steps
            )
            runtime_state.configure_runtime(
                runtime_version=config.runtime_version,
                job_id=config.job_id,
                worker_id=config.worker_id,
                rpc_timeout_seconds=config.rpc_timeout_seconds,
            )
    _prepared_model = prepared
    return prepared


def prepare_optimizer(optimizer):
    if _prepared_model is None:
        raise RuntimeError(
            "locdist.prepare(model) must be called before prepare_optimizer(optimizer)"
        )
    return prepare_wrapped_optimizer(
        model=_prepared_model,
        optimizer=optimizer,
    )
