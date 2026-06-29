from dataclasses import dataclass
from typing import List


@dataclass
class RuntimeConfig:
    runtime_version: int

    job_id: str

    worker_id: str

    worker_host: str

    worker_port: int

    rpc_timeout_seconds: int

    communication: "CommunicationConfig"


@dataclass
class CommunicationConfig:
    precision: str = "fp32"

    compression_type: str = "none"

    compression_mode: str = "global"

    top_k_percent: float = 5.0

    error_feedback: bool = True

    warmup_steps: int = 0

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

    data_dtype: str | None = None

    encoding: str = "dense"

    indices: list[int] | None = None

    indices_u32: bytes | None = None


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
