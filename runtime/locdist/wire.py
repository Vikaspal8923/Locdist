from typing import List

from locdist.models import (
    AggregatedGradientChunkPackage,
    AggregatedGradientPackage,
    GradientChunk,
    GradientChunkPackage,
    GradientPackage,
    ParameterMetadata,
)

from locdist.generated import gradient_pb2
from locdist.indices import unpack_u32_indices


def package_to_submission_proto(
    package: GradientPackage,
) -> gradient_pb2.GradientSubmission:

    submission = gradient_pb2.GradientSubmission(
        runtime_version=package.runtime_version,
        job_id=package.job_id,
        worker_id=package.worker_id,
    )

    for chunk in package.chunks:

        proto_chunk = submission.chunks.add()

        proto_chunk.metadata.name = (
            chunk.metadata.name
        )

        proto_chunk.metadata.shape.extend(
            chunk.metadata.shape
        )

        proto_chunk.metadata.numel = (
            chunk.metadata.numel
        )

        proto_chunk.metadata.dtype = (
            chunk.metadata.dtype
        )

        proto_chunk.metadata.layer_order = (
            chunk.metadata.layer_order
        )

        proto_chunk.has_grad = (
            chunk.has_grad
        )

        proto_chunk.byte_size = (
            chunk.byte_size
        )

        proto_chunk.data_dtype = (
            chunk.data_dtype or ""
        )

        proto_chunk.encoding = (
            chunk.encoding
        )

        proto_chunk.sync_round = (
            chunk.sync_round
        )

        proto_chunk.indices.extend(
            chunk.indices or []
        )

        if chunk.indices_u32:
            proto_chunk.indices_u32 = (
                chunk.indices_u32
            )

        if chunk.data is not None:
            proto_chunk.data = chunk.data

    return submission


def chunk_package_to_submission_proto(
    package: GradientChunkPackage,
) -> gradient_pb2.GradientChunkSubmission:

    submission = gradient_pb2.GradientChunkSubmission(
        runtime_version=package.runtime_version,
        job_id=package.job_id,
        worker_id=package.worker_id,
    )
    _populate_proto_chunk(submission.chunk, package.chunk)
    return submission


def submission_proto_to_package(
    proto: gradient_pb2.GradientSubmission,
) -> GradientPackage:

    chunks: List[GradientChunk] = []

    for proto_chunk in proto.chunks:

        metadata = ParameterMetadata(
            name=proto_chunk.metadata.name,
            shape=tuple(
                proto_chunk.metadata.shape
            ),
            numel=proto_chunk.metadata.numel,
            dtype=proto_chunk.metadata.dtype,
            layer_order=proto_chunk.metadata.layer_order,
        )

        chunk = GradientChunk(
            metadata=metadata,
            has_grad=proto_chunk.has_grad,
            data=(
                proto_chunk.data
                if proto_chunk.has_grad
                else None
            ),
            byte_size=proto_chunk.byte_size,
            data_dtype=(
                proto_chunk.data_dtype
                or None
            ),
            encoding=(
                proto_chunk.encoding
                or "dense"
            ),
            indices=indices_from_proto(proto_chunk),
            indices_u32=(
                proto_chunk.indices_u32
                or None
            ),
            sync_round=proto_chunk.sync_round,
        )

        chunks.append(chunk)

    return GradientPackage(
        runtime_version=proto.runtime_version,
        job_id=proto.job_id,
        worker_id=proto.worker_id,
        chunks=chunks,
    )


def response_proto_to_package(
    proto: gradient_pb2.AggregatedGradientResponse,
) -> AggregatedGradientPackage:

    chunks: List[GradientChunk] = []

    for proto_chunk in proto.chunks:

        metadata = ParameterMetadata(
            name=proto_chunk.metadata.name,
            shape=tuple(
                proto_chunk.metadata.shape
            ),
            numel=proto_chunk.metadata.numel,
            dtype=proto_chunk.metadata.dtype,
            layer_order=proto_chunk.metadata.layer_order,
        )

        chunk = GradientChunk(
            metadata=metadata,
            has_grad=proto_chunk.has_grad,
            data=(
                proto_chunk.data
                if proto_chunk.has_grad
                else None
            ),
            byte_size=proto_chunk.byte_size,
            data_dtype=(
                proto_chunk.data_dtype
                or None
            ),
            encoding=(
                proto_chunk.encoding
                or "dense"
            ),
            indices=indices_from_proto(proto_chunk),
            indices_u32=(
                proto_chunk.indices_u32
                or None
            ),
            sync_round=proto_chunk.sync_round,
        )

        chunks.append(chunk)

    return AggregatedGradientPackage(
        runtime_version=proto.runtime_version,
        job_id=proto.job_id,
        participating_workers=(
            proto.participating_workers
        ),
        aggregation_round=(
            proto.aggregation_round
        ),
        chunks=chunks,
    )


def chunk_response_proto_to_package(
    proto: gradient_pb2.AggregatedGradientChunkResponse,
) -> AggregatedGradientChunkPackage:
    return AggregatedGradientChunkPackage(
        runtime_version=proto.runtime_version,
        job_id=proto.job_id,
        participating_workers=proto.participating_workers,
        aggregation_round=proto.aggregation_round,
        chunk=_proto_chunk_to_runtime_chunk(proto.chunk),
    )


def package_to_response_proto(
    package: GradientPackage,
    participating_workers: int,
    aggregation_round: int,
) -> gradient_pb2.AggregatedGradientResponse:

    response = (
        gradient_pb2.AggregatedGradientResponse(
            runtime_version=package.runtime_version,
            job_id=package.job_id,
            participating_workers=(
                participating_workers
            ),
            aggregation_round=(
                aggregation_round
            ),
        )
    )

    for chunk in package.chunks:
        _populate_proto_chunk(
            response.chunks.add(),
            chunk,
        )

    return response


def chunk_package_to_response_proto(
    package: GradientChunkPackage,
    participating_workers: int,
    aggregation_round: int,
) -> gradient_pb2.AggregatedGradientChunkResponse:
    response = gradient_pb2.AggregatedGradientChunkResponse(
        runtime_version=package.runtime_version,
        job_id=package.job_id,
        participating_workers=participating_workers,
        aggregation_round=aggregation_round,
    )
    _populate_proto_chunk(response.chunk, package.chunk)
    return response


def indices_from_proto(proto_chunk) -> list[int]:
    if proto_chunk.indices_u32:
        return unpack_u32_indices(
            proto_chunk.indices_u32
        )
    return list(proto_chunk.indices)


def _populate_proto_chunk(
    proto_chunk,
    chunk: GradientChunk,
) -> None:
    proto_chunk.metadata.name = (
        chunk.metadata.name
    )

    proto_chunk.metadata.shape.extend(
        chunk.metadata.shape
    )

    proto_chunk.metadata.numel = (
        chunk.metadata.numel
    )

    proto_chunk.metadata.dtype = (
        chunk.metadata.dtype
    )

    proto_chunk.metadata.layer_order = (
        chunk.metadata.layer_order
    )

    proto_chunk.has_grad = (
        chunk.has_grad
    )

    proto_chunk.byte_size = (
        chunk.byte_size
    )

    proto_chunk.data_dtype = (
        chunk.data_dtype or ""
    )

    proto_chunk.encoding = (
        chunk.encoding
    )

    proto_chunk.sync_round = (
        chunk.sync_round
    )

    proto_chunk.indices.extend(
        chunk.indices or []
    )

    if chunk.indices_u32:
        proto_chunk.indices_u32 = (
            chunk.indices_u32
        )

    if chunk.data is not None:
        proto_chunk.data = chunk.data


def _proto_chunk_to_runtime_chunk(
    proto_chunk,
) -> GradientChunk:
    metadata = ParameterMetadata(
        name=proto_chunk.metadata.name,
        shape=tuple(
            proto_chunk.metadata.shape
        ),
        numel=proto_chunk.metadata.numel,
        dtype=proto_chunk.metadata.dtype,
        layer_order=proto_chunk.metadata.layer_order,
    )

    return GradientChunk(
        metadata=metadata,
        has_grad=proto_chunk.has_grad,
        data=(
            proto_chunk.data
            if proto_chunk.has_grad
            else None
        ),
        byte_size=proto_chunk.byte_size,
        data_dtype=(
            proto_chunk.data_dtype
            or None
        ),
        encoding=(
            proto_chunk.encoding
            or "dense"
        ),
        indices=indices_from_proto(proto_chunk),
        indices_u32=(
            proto_chunk.indices_u32
            or None
        ),
        sync_round=proto_chunk.sync_round,
    )
