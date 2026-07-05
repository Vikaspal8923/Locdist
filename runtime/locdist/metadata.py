from typing import List

from locdist.models import ParameterMetadata


def extract_metadata(model) -> List[ParameterMetadata]:
    """
    Extract static parameter metadata.
    """

    metadata = []

    for layer_order, (name, parameter) in enumerate(model.named_parameters()):

        metadata.append(
            ParameterMetadata(
                name=name,
                shape=tuple(parameter.shape),
                numel=parameter.numel(),
                dtype=str(parameter.dtype),
                layer_order=layer_order,
            )
        )

    return metadata
