# LocDist Runtime V1

## Overview

LocDist Runtime is the Python component that runs inside the user's training process.

Its purpose is to synchronize gradients across multiple worker machines while keeping the training loop almost identical to normal PyTorch.

Example:

```python
import locdist

for batch in dataloader:

    optimizer.zero_grad()

    outputs = model(batch)

    loss = outputs.loss

    loss.backward()

    locdist.sync_gradients(model)

    optimizer.step()
```

The user does not need to understand:

* Distributed Systems
* Networking
* Gradient Aggregation
* Barrier Synchronization
* Worker Coordination

The Runtime handles all gradient synchronization automatically.

---

# Locdist LDGCC V1 Architecture

This runtime is one component of the larger LDGCC system. The overall LDGCC architecture follows a coordinator-based design where the Brain Laptop owns orchestration and workers never communicate directly.

```text
┌──────────────────────────────────────────────────────────────┐
│                    USER (Brain Laptop)                       │
└──────────────────────────────────────────────────────────────┘

Project/
│
├── train.py
├── model.py
├── requirements.txt
├── dataset/
└── ldgcc.yaml

                    │
                    ▼

┌──────────────────────────────────────────────────────────────┐
│                    LocDist Studio                            │
│                  (VS Code Extension)                         │
└──────────────────────────────────────────────────────────────┘

Responsibilities

• Discover Workers
• Start Training
• Show Logs
• Show Cluster Status
• Download Outputs

                    │
                    ▼

┌──────────────────────────────────────────────────────────────┐
│                    LDGCC Master                              │
│                    (Brain Laptop)                            │
└──────────────────────────────────────────────────────────────┘

Components

master/
│
├── discovery/
├── scheduler/
├── sharder/
├── aggregator/
├── orchestrator/
├── worker-manager/
└── storage/

Flow

1. Discover Workers
2. Accept Workers
3. Read Dataset
4. Create Dataset Shards
5. Package Project
6. Generate Worker Configs
7. Send Workspaces
8. Start Training
9. Aggregate Gradients
10. Collect Outputs

                    │
                    ▼

┌──────────────────────────────────────────────────────────────┐
│                    WORKER LAPTOPS                            │
└──────────────────────────────────────────────────────────────┘

worker/
│
├── tray-app/
├── executor/
├── environment-manager/
└── workspace-manager/

                    │
                    ▼

┌──────────────────────────────────────────────────────────────┐
│                    PYTHON TRAINING                           │
└──────────────────────────────────────────────────────────────┘

train.py

loss.backward()

locdist.sync_gradients(model)

optimizer.step()
```

---

# Runtime Architecture

Runtime lives inside the worker training process.

```text
Brain Laptop

┌──────────────────────────────────┐
│             Master               │
├──────────────────────────────────┤
│ Discovery                        │
│ Scheduler                        │
│ Worker Manager                   │
│ Orchestrator                     │
│ Aggregator                       │
│ Storage                          │
└──────────────────────────────────┘

                ▲
                │ Persistent gRPC
                │

┌──────────────────────────────────┐
│       Worker Service (Go)        │
└──────────────────────────────────┘

                ▲
                │ Local gRPC
                │

┌──────────────────────────────────┐
│        Python Runtime            │
└──────────────────────────────────┘

                ▲

             train.py
```

---

# Runtime Execution Flow

```text
loss.backward()
      ↓

Extract Gradient Chunks
      ↓

Build GradientPackage
      ↓

Convert To Proto
      ↓

Send To Worker Service
      ↓

WAIT
      ↓

Worker Service
      ↓

Master Server
      ↓

Aggregator Component
      ↓

Barrier Synchronization
      ↓

Gradient Aggregation
      ↓

Master Server
      ↓

Worker Service
      ↓

Aggregated Response
      ↓

Convert From Proto
      ↓

Apply Aggregated Gradients
      ↓

Return
      ↓

optimizer.step()
```

---

# Runtime V1 Production Decisions

## Architecture

Runtime communicates only with the local Worker Service.

```text
Runtime
    ↓
Worker Service
```

Runtime never communicates directly with the Aggregator.

```text
Runtime
    ✗
    Aggregator
```

---

## Communication

Runtime ↔ Worker Service

```text
Local gRPC
```

Worker Service ↔ Master

```text
Persistent gRPC
```

---

## Aggregation Ownership

Aggregator owns:

* Barrier Synchronization
* Aggregation Logic
* Worker Coordination

Runtime owns:

* Gradient Extraction
* Serialization
* Communication
* Reconstruction

Aggregator exists as an internal Master component.

Runtime is unaware of:

* Master Internals
* Aggregator Location
* Scheduler
* Worker Manager
* Orchestrator

---

## Synchronization Model

Runtime performs:

```text
Send
↓
Wait
↓
Receive
```

Runtime never knows:

* Worker Count
* Cluster Size
* Aggregation Strategy

---

## Gradient Strategy

Runtime V1 uses:

```text
GradientChunk Per Parameter
```

Not:

```text
One Giant Flattened Tensor
```

Reason:

* Mixed dtype safe
* Easier reconstruction
* Lower memory risk
* Simpler debugging

---

## Dtype Preservation

Runtime preserves:

```text
torch.float16
torch.float32
torch.bfloat16
```

No dtype conversion is performed.

---

## Configuration Driven

Runtime behavior comes entirely from:

```text
locdist_config.json
```

No hardcoded networking values.

No environment variables.

Runtime only knows how to reach the local Worker Service.

Example:

{
  "worker_host": "127.0.0.1",
  "worker_port": 50051
}

Runtime never knows:

• Master Address
• Aggregator Address
• Cluster Topology

---

## Timeout Handling

Runtime uses:

```text
rpc_timeout_seconds
```

from configuration.

Purpose:

```text
Protection against synchronization hangs.
```

Examples:

* Aggregator crash
* Worker Service crash
* Barrier never completes

---

## Persistent Connections

TransportClient creates:

```text
gRPC Channel
WorkerBridge Stub
```

once.

The connection is reused throughout training.

---

## Singleton Design

Runtime uses:

```text
get_transport()
```

and

```text
get_runtime_config()
```

to avoid repeatedly creating expensive objects.

---

# Runtime Folder Structure

```text
runtime/
│
├── README.md
├── pyproject.toml
│
├── locdist/
│   │
│   ├── __init__.py
│   ├── api.py
│   ├── config.py
│   ├── exceptions.py
│   ├── gradients.py
│   ├── metadata.py
│   ├── models.py
│   ├── transport.py
│   ├── wire.py
│   │
│   └── generated/
│
└── tests/
```

---

# File Responsibilities

## **init**.py

Public Runtime API.

Exports:

```python
sync_gradients()
```

---

## api.py

Runtime orchestration layer.

Responsibilities:

* Extract gradients
* Build packages
* Invoke transport
* Apply aggregated gradients

---

## config.py

Loads and validates:

```text
locdist_config.json
```

Provides:

```python
RuntimeConfig
```

---

## exceptions.py

Runtime exception hierarchy.

Provides:

* ConfigError
* GradientError
* SerializationError
* TransportError
* ConnectionError
* SynchronizationError

---

## models.py

Core Runtime dataclasses.

Provides:

* RuntimeConfig
* ParameterMetadata
* GradientChunk
* GradientPackage
* AggregatedGradientPackage

---

## metadata.py

Extracts static parameter metadata.

Provides:

* Parameter names
* Shapes
* Dtypes
* Tensor sizes

---

## gradients.py

Gradient processing engine.

Responsibilities:

* Extract gradients
* Serialize tensors
* Reconstruct tensors
* Apply gradients

---

## wire.py

Runtime ↔ Proto conversion layer.

Responsibilities:

```text
Python Objects
      ↔
Protocol Buffers
```

---

## transport.py

Worker Service client.

Responsibilities:

* Create gRPC channel
* Create WorkerBridge stub
* Send requests
* Receive responses

---

## generated/

Generated protobuf files.

Produced by:

```bash
grpc_tools.protoc
```

---

# Testing

Runtime V1 includes:

```text
test_models.py
test_config.py
test_gradients.py
test_wire.py
test_transport.py
test_runtime_flow.py
test_runtime_worker_integration.py
```

Coverage includes:

* Configuration loading
* Metadata extraction
* Gradient extraction
* Gradient reconstruction
* Serialization roundtrip
* Transport singleton behavior
* Runtime flow validation
 Runtime
    ↓
Worker Service
    ↓
Runtime

---

# Runtime Responsibilities

Runtime owns:

* Gradient extraction
* Metadata generation
* Serialization
* Worker Service communication
* Gradient reconstruction
* Gradient replacement

Runtime does NOT own:

* Dataset sharding
* Worker discovery
* Scheduling
* Aggregation
* Cluster management
* Job orchestration
* Master Communication
* Aggregator Communication
* Worker Registration
* Heartbeats
* Log Streaming

---

# Runtime V1 Success Criteria

Runtime V1 is complete when:

```python
loss.backward()

locdist.sync_gradients(model)

optimizer.step()
```

works correctly while preserving:

* Gradient correctness
* Shape correctness
* Parameter ordering
* Mixed dtype support

without requiring users to write distributed-training-specific code.

---

# Phase 10: Automatic Worker Connection

When the Worker launches a training entrypoint, Runtime configuration is
injected through `LDGCC_JOB_ID`, `LDGCC_WORKER_ID`, `LDGCC_WORKER_HOST`, and
`LDGCC_WORKER_PORT`. These values override `locdist_config.json`.

Production training therefore connects to the local Worker automatically and
does not require a copied Runtime config file. `locdist_config.json` remains a
fallback for manual development and testing. Runtime never receives or connects
to the Master address directly.

---

# Phase 16: Communication Compression

Runtime supports LDGCC V1 communication compression from `ldgcc.yml` through the
Worker-injected `LDGCC_COMMUNICATION` environment variable.

```yaml
communication:
  precision: fp16
  compression:
    type: topk
    mode: per_layer
    top_k: 5%
    error_feedback: true
    warmup_steps: 500
```

Defaults:

```text
precision: fp32
compression.type: none

When compression.type = topk:
  mode: global
  top_k: 5%
  error_feedback: true
  warmup_steps: 0
```

Top-k mode:

* `global`: choose the strongest gradients across all parameters.
* `per_layer`: choose the strongest gradients separately for each parameter.
* If both legacy global/per-layer values appear, per-layer wins.

Runtime owns FP16/fp32 payload encoding, global and per-layer top-k selection,
residual/error-feedback buffers, warmup dense syncs, and restoring aggregated
gradients into the model.

Master owns sparse-aware averaging and returns dense averaged chunks. Worker
only transports `LDGCC_COMMUNICATION` and gradient messages.

For top-k, `error_feedback` must be true in V1. Gradient clipping and optimizer
momentum correction remain user training-loop responsibilities; user code must
call `locdist.sync_gradients(model)` before `optimizer.step()`.

---

# Phase 17: Sparse Aggregated Response

Runtime can now receive sparse top-k responses from Master. For sparse response
chunks, Runtime creates a zero gradient tensor, fills the returned indices, and
then writes that gradient back into the model parameter.

This keeps both directions compressed:

```text
Runtime upload: sparse top-k
Runtime download: sparse top-k union from Master
```

Dense responses are still supported for no-compression and warmup syncs.
Runtime validates sparse response size, duplicate indices, and index bounds
before applying gradients.

---

# Phase 18: Packed Sparse Indices

Runtime now sends top-k sparse indices as packed little-endian uint32 bytes in
`indices_u32`. The older repeated `indices` field is still readable for
compatibility, but new Runtime payloads use the compact format.

```text
fp16 value + int64 index  = 10 bytes
fp16 value + uint32 index =  6 bytes
```

Runtime rejects indices outside the uint32 range before sending. Sparse
responses from Master are decoded from `indices_u32` and then applied through
the same sparse gradient validation path.
