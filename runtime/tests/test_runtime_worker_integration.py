import torch
import torch.nn as nn

import locdist


model = nn.Linear(4, 2)

x = torch.randn(3, 4)
y = torch.randn(3, 2)

criterion = nn.MSELoss()

output = model(x)

loss = criterion(output, y)

loss.backward()

before = model.weight.grad.clone()

print("Before Sync:")
print(before)

locdist.sync_gradients(model)

after = model.weight.grad.clone()

print("\nAfter Sync:")
print(after)

print(
    "\nIdentical:",
    torch.equal(before, after),
)