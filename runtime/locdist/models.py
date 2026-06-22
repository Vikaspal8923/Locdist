from dataclasses import dataclass
from typing import List


@dataclass
class RuntimeConfig:
    runtime_version: int
    job_id: str
    worker_id: str
    master_host: str
    master_port: int


@dataclass
class ParameterMetadata:
    """
    Static parameter information.
    """

    name: str
    shape: tuple
    numel: int
    dtype: str


@dataclass
class GradientChunk:
    """
    Runtime gradient information.
    """

    metadata: ParameterMetadata

    has_grad: bool

    data: bytes | None

    byte_size: int


@dataclass
class GradientPackage:
    runtime_version: int

    job_id: str

    worker_id: str

    chunks: List[GradientChunk]


@dataclass
class AggregatedGradientPackage:

    runtime_version: int

    job_id: str

    participating_workers: int

    aggregation_round: int

    chunks: list[GradientChunk]