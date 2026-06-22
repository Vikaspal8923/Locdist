from locdist.config import load_config

from locdist.models import (
    GradientPackage,
)

from locdist.gradients import (
    extract_gradient_chunks,
    apply_gradient_chunks,
)

from locdist.transport import (
    get_transport,
)


_config = None


def get_runtime_config():

    global _config

    if _config is None:

        _config = load_config()

    return _config


def sync_gradients(model) -> None:

    config = get_runtime_config()

    chunks = extract_gradient_chunks(
        model
    )

    package = GradientPackage(
        runtime_version=(
            config.runtime_version
        ),
        job_id=config.job_id,
        worker_id=config.worker_id,
        chunks=chunks,
    )

    transport = get_transport()

    aggregated_package = (
        transport.synchronize(
            package
        )
    )

    apply_gradient_chunks(
        model,
        aggregated_package.chunks,
    )