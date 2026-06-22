import torch
import torch.nn as nn

from locdist.models import (
    GradientPackage,
)

from locdist.gradients import (
    extract_gradient_chunks,
    apply_gradient_chunks,
)


class SimpleModel(nn.Module):

    def __init__(self):
        super().__init__()

        self.fc1 = nn.Linear(4, 3)
        self.fc2 = nn.Linear(3, 2)

    def forward(self, x):

        return self.fc2(
            self.fc1(x)
        )


def main():

    model = SimpleModel()

    x = torch.randn(8, 4)

    output = model(x)

    loss = output.sum()

    loss.backward()

    # ----------------------------------
    # Extract
    # ----------------------------------

    chunks = extract_gradient_chunks(
        model
    )

    package = GradientPackage(
        runtime_version=1,
        job_id="job-123",
        worker_id="worker-a",
        chunks=chunks,
    )

    # ----------------------------------
    # Simulate Aggregator
    # ----------------------------------

    apply_gradient_chunks(
        model,
        package.chunks,
    )

    # ----------------------------------
    # Verify gradients still exist
    # ----------------------------------

    for parameter in model.parameters():

        assert parameter.grad is not None

    print(
        "✓ Runtime flow successful"
    )


if __name__ == "__main__":
    main()