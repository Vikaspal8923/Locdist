"""
LocDist Prototype 2 — PyTorch Gradient Extraction & Replacement
Proves that the Runtime layer can intercept gradients between
loss.backward() and optimizer.step().
"""

import torch
import torch.nn as nn


# ──────────────────────────────────────────────
# SETUP
# ──────────────────────────────────────────────

def separator(title):
    print(f"\n{'='*50}")
    print(f"  {title}")
    print(f"{'='*50}")


# Smallest possible model
model     = nn.Linear(10, 1)
criterion = nn.MSELoss()
optimizer = torch.optim.SGD(model.parameters(), lr=0.01)

# Synthetic data — no real dataset needed
x = torch.randn(32, 10)
y = torch.randn(32, 1)


# ──────────────────────────────────────────────
# STEP 1 — Does loss.backward() create gradients?
# ──────────────────────────────────────────────

separator("STEP 1: Verify Gradient Extraction")

outputs = model(x)
loss    = criterion(outputs, y)
loss.backward()                      # PyTorch computes gradients here

print("\nGradients after loss.backward():\n")
for name, p in model.named_parameters():
    print(f"  {name}:")
    print(f"    grad exists : {p.grad is not None}")
    print(f"    grad shape  : {p.grad.shape}")
    print(f"    grad values : {p.grad}")

# Proof check
for p in model.parameters():
    assert p.grad is not None, "FAIL — gradient is None"

print("\n✓ STEP 1 PASSED — gradients exist and are accessible")


# ──────────────────────────────────────────────
# STEP 2 — Can we replace gradients before optimizer.step()?
# ──────────────────────────────────────────────

separator("STEP 2: Verify Gradient Replacement")

# Capture original gradients
original_grads = {
    name: p.grad.clone()
    for name, p in model.named_parameters()
}

print("\nOriginal gradients:")
for name, g in original_grads.items():
    print(f"  {name}: {g}")

# Replace every gradient with ones (simulates receiving aggregated gradient)
for p in model.parameters():
    p.grad = torch.ones_like(p.grad)

print("\nModified gradients (replaced with ones):")
for name, p in model.named_parameters():
    print(f"  {name}: {p.grad}")

# Proof check — must differ from originals
for name, p in model.named_parameters():
    assert not torch.equal(p.grad, original_grads[name]), \
        f"FAIL — {name} gradient was not replaced"

print("\n✓ STEP 2 PASSED — gradients were successfully replaced")


# ──────────────────────────────────────────────
# STEP 3 — Does optimizer.step() use replaced gradients?
# ──────────────────────────────────────────────

separator("STEP 3: Verify Optimizer Uses Replaced Gradient")

# Weights before update
before_weight = model.weight.clone()
before_bias   = model.bias.clone()

print(f"\nWeights BEFORE optimizer.step():")
print(f"  weight : {before_weight}")
print(f"  bias   : {before_bias}")

optimizer.step()

# Weights after update
after_weight = model.weight.clone()
after_bias   = model.bias.clone()

print(f"\nWeights AFTER optimizer.step():")
print(f"  weight : {after_weight}")
print(f"  bias   : {after_bias}")

# SGD update rule: new_w = old_w - lr * grad
# grad = ones, lr = 0.01 → delta should be 0.01
expected_delta = 0.01   # lr * 1.0

print(f"\nExpected weight change (lr × grad = 0.01 × 1.0 = {expected_delta}):")
actual_delta_w = (before_weight - after_weight).abs().mean().item()
actual_delta_b = (before_bias   - after_bias  ).abs().mean().item()
print(f"  Actual weight delta : {actual_delta_w:.6f}")
print(f"  Actual bias delta   : {actual_delta_b:.6f}")

assert not torch.equal(before_weight, after_weight), "FAIL — weights did not change"
assert abs(actual_delta_w - expected_delta) < 1e-5,  "FAIL — wrong update magnitude"

print("\n✓ STEP 3 PASSED — optimizer used the replaced gradient exactly")


# ──────────────────────────────────────────────
# STEP 4 — Simulate LocDist Runtime API
# ──────────────────────────────────────────────

separator("STEP 4: Simulate sync_gradients() Runtime API")

def sync_gradients(model):
    """
    Simulates what the real LocDist runtime will do:
      1. Extract gradients from model
      2. (In real system: send to Master, wait for aggregation)
      3. Replace gradients with aggregated result
      4. Return — optimizer.step() runs next

    In this prototype we simulate aggregation by scaling by 0.5.
    """
    print("\n  [sync_gradients] Extracting gradients…")
    local_grads = [p.grad.clone() for p in model.parameters()]
    for i, g in enumerate(local_grads):
        print(f"    param[{i}] extracted: {g}")

    print("  [sync_gradients] Simulating aggregation (×0.5)…")
    aggregated = [g * 0.5 for g in local_grads]

    print("  [sync_gradients] Replacing gradients with aggregated result…")
    for p, agg in zip(model.parameters(), aggregated):
        p.grad = agg
        print(f"    replaced with: {p.grad}")

    print("  [sync_gradients] Done — optimizer.step() may now proceed")


# Fresh model and optimizer for a clean run
model     = nn.Linear(10, 1)
optimizer = torch.optim.SGD(model.parameters(), lr=0.01)

print("\nRunning one full training step with sync_gradients():\n")
print("  outputs = model(x)")
outputs = model(x)

print("  loss = criterion(outputs, y)")
loss = criterion(outputs, y)

print("  loss.backward()")
loss.backward()

print("  sync_gradients(model)   ← LocDist intercept point")
sync_gradients(model)

print("  optimizer.step()")
before = model.weight.clone()
optimizer.step()
after  = model.weight.clone()

assert not torch.equal(before, after), "FAIL — weights did not update"
print("\n✓ STEP 4 PASSED — sync_gradients() successfully intercepted the training loop")


# ──────────────────────────────────────────────
# FINAL SUMMARY
# ──────────────────────────────────────────────

separator("PROTOTYPE 2 COMPLETE")
print("""
  ✓  loss.backward() creates gradients
  ✓  Gradients can be extracted
  ✓  Gradients can be replaced
  ✓  optimizer.step() uses the replaced gradients
  ✓  sync_gradients(model) API works as the intercept point

  The LocDist Runtime layer assumption is PROVEN.

  Training loop:
      loss.backward()
      locdist.sync_gradients(model)   ← intercept here
      optimizer.step()

  Next: Prototype 3 — connect Prototype 1 (Go aggregator)
        with Prototype 2 (Python gradient hook) over gRPC.
""")