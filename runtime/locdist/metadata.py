from typing import List

from locdist.models import ParameterMetadata


def extract_metadata(model) -> List[ParameterMetadata]:
    """
    Extract static parameter metadata.
    """

    metadata = []

    for name, parameter in model.named_parameters():

        metadata.append(
            ParameterMetadata(
                name=name,
                shape=tuple(parameter.shape),
                numel=parameter.numel(),
                dtype=str(parameter.dtype),
            )
        )

    return metadata