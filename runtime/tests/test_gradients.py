import torch
import torch.nn as nn

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


model = SimpleModel()

x = torch.randn(8, 4)

output = model(x)

loss = output.sum()

loss.backward()

chunks = extract_gradient_chunks(model)

print()
print("Extracted Chunks")

for chunk in chunks:

    print(
        chunk.metadata.name,
        chunk.metadata.dtype,
        chunk.byte_size,
    )

apply_gradient_chunks(
    model,
    chunks,
)

print()
print("✓ Gradient round-trip successful")