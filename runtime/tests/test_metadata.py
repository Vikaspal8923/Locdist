import torch.nn as nn

from locdist.metadata import extract_metadata


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

    metadata = extract_metadata(model)

    print()
    print("Extracted Metadata")

    for item in metadata:

        print(
            item.name,
            item.shape,
            item.numel,
            item.dtype,
        )

    assert len(metadata) == 4

    assert metadata[0].name == "fc1.weight"
    assert metadata[1].name == "fc1.bias"
    assert metadata[2].name == "fc2.weight"
    assert metadata[3].name == "fc2.bias"

    print()
    print("✓ Metadata extraction successful")


if __name__ == "__main__":
    main()