# LDGCC Worker Service V1

## Overview

LDGCC (LocDist Distributed GPU Compute Cluster) is a local-first distributed training platform that transforms nearby laptops into a temporary machine learning training cluster.

Version 1 (V1) follows a coordinator-based architecture:

* Master coordinates the cluster.
* Workers execute training-related tasks.
* Runtime runs inside the user's Python training process.
* All synchronization flows through centralized infrastructure.

Workers never communicate directly with each other.

---

# High Level LDGCC Architecture

```text
                     Brain Laptop

┌───────────────────────────────────────────────┐
│                    MASTER                     │
│                                               │
│ Discovery                                     │
│ Scheduler                                     │
│ Aggregator                                    │
│ Orchestrator                                  │
└───────────────────────────────────────────────┘
                     ▲
                     │ Persistent gRPC
                     │
                     ▼

┌───────────────────────────────────────────────┐
│               WORKER SERVICE                  │
└───────────────────────────────────────────────┘
                     ▲
                     │ Local gRPC
                     │
                     ▼

┌───────────────────────────────────────────────┐
│              LOCDIST RUNTIME                  │
└───────────────────────────────────────────────┘
                     ▲
                     │
                     ▼

                 train.py
```

---

# Component Responsibilities

## Runtime

Runtime runs inside the user's training process.

Example:

```python
loss.backward()

locdist.sync_gradients(model)

optimizer.step()
```

Runtime owns:

* Gradient extraction
* Gradient reconstruction
* Serialization
* Proto conversion
* Transport client
* Runtime API

Runtime does NOT own:

* Aggregation
* Scheduling
* Discovery
* Cluster coordination

---

## Worker Service

Worker Service acts as the communication layer between Runtime and the rest of LDGCC infrastructure.

Future responsibilities:

* Runtime communication
* Aggregator communication
* Workspace management
* Environment management
* Process execution
* Status reporting

---

## Master

Master owns cluster orchestration.

Responsibilities:

* Discovery
* Scheduling
* Aggregation
* Sharding
* Coordination

---

# Shared Protocol

LDGCC uses a shared protobuf contract.

```text
ldgcc/

├── protocol/
│   └── gradient.proto
│
├── runtime/
├── worker/
└── master/
```

Important:

There must be exactly one source of truth for protobuf definitions.

All components generate code from:

```text
protocol/gradient.proto
```

---

# Worker Service Final Architecture (V1)

The complete Worker Service architecture is:

```text
Worker Service

├── Runtime Bridge
├── Executor Manager
├── Workspace Manager
├── Environment Manager
└── Status Manager
```

Responsibilities:

### Runtime Bridge

Handles communication between Runtime and LDGCC infrastructure.

Future flow:

```text
Runtime
    ↓
Worker Service
    ↓
Aggregator
```

---

### Executor Manager

Responsible for:

```text
Launch train.py
Monitor process
Collect logs
Handle crashes
```

---

### Workspace Manager

Responsible for:

```text
Workspace creation
Project extraction
Dataset storage
Cleanup
```

---

### Environment Manager

Responsible for:

```text
Virtual environment creation
Dependency installation
Python validation
```

---

### Status Manager

Responsible for:

```text
IDLE
PREPARING
RUNNING
COMPLETED
FAILED
```

status reporting.

---

# Worker Service Phase 1

## Purpose

Phase 1 is NOT distributed training.

Phase 1 is a communication and integration milestone.

Goal:

Validate the complete Runtime → Proto → gRPC → Worker → Proto → Runtime path.

---

## What Phase 1 Does NOT Implement

Phase 1 intentionally excludes:

```text
Master Communication

Aggregator Communication

Worker Coordination

Barrier Synchronization

Workspace Manager

Environment Manager

Executor

Status Manager

Scheduling

Discovery

Distributed Aggregation
```

---

## Phase 1 Architecture

```text
Runtime
    ↓
GradientSubmission
    ↓
protobuf
    ↓
gRPC
    ↓
Worker Service
    ↓
Runtime Bridge
    ↓
Identity Aggregation
    ↓
AggregatedGradientResponse
    ↓
gRPC
    ↓
protobuf
    ↓
Runtime
```

---

## Identity Aggregation

Phase 1 uses a special aggregation algorithm:

```text
Identity Aggregation
```

Mathematically:

```text
G = G
```

Meaning:

```text
Aggregated Gradient
=
Original Gradient
```

No:

* averaging
* reduction
* scaling
* compression
* optimization

The response gradient must be identical to the request gradient.

---

## Phase 1 Success Criterion

The most important requirement is:

```python
loss.backward()

locdist.sync_gradients(model)

optimizer.step()
```

must behave identically to:

```python
loss.backward()

optimizer.step()
```

for a single worker.

Meaning:

```text
Gradient Before Sync
=
Gradient After Sync
```

---

# Phase 1 Repository Structure

```text
worker/

├── cmd/
│   └── worker/
│       └── main.go

├── grpc/
│   ├── handlers.go
│   └── server.go

├── runtimebridge/
│   └── sync.go

├── internal/
│   ├── config/
│   │   └── config.go
│   └── errors/
│       └── errors.go

├── generated/
│   └── gradient/
│       ├── gradient.pb.go
│       └── gradient_grpc.pb.go

├── tests/
│   ├── runtimebridge_test.go
│   ├── handler_test.go
│   └── sync_test.go

├── go.mod
├── go.sum
└── README.md
```

---
# Phase 1 File Responsibilities

## internal/errors/errors.go

Purpose:

Centralized location for all Runtime → Worker request validation errors.

This file prevents validation logic from scattering across multiple packages and provides a single source of truth for request validation failures.

Current Phase 1 Errors:

```text
ErrInvalidRuntimeVersion

ErrMissingJobID

ErrMissingWorkerID

ErrMissingChunks
```

These errors are consumed by:

```text
runtimebridge/sync.go
```

during request validation.

Flow:

```text
GradientSubmission
        ↓
Runtime Bridge Validation
        ↓
errors.go
        ↓
Error Returned
```

No business logic exists here.

Only error definitions.

---

## internal/config/config.go

Purpose:

Contains Worker Service configuration.

Current Phase 1 Configuration:

```text
Port
```

Example:

```text
50051
```

This configuration is used when creating the gRPC server.

Flow:

```text
main.go
      ↓
Load Config
      ↓
grpc/server.go
      ↓
Start Listener
```

Future versions may include:

```text
Aggregator Address

Workspace Path

Environment Path

Heartbeat Interval

Logging Configuration
```

but those are intentionally excluded from Phase 1.

---

## runtimebridge/sync.go

Purpose:

Core business logic of Worker Service Phase 1.

This is the most important file in the entire Phase 1 implementation.

It implements:

```text
Identity Aggregation
```

which acts as a simulated one-worker aggregation step.

Phase 1 Flow:

```text
GradientSubmission
        ↓
Validate Request
        ↓
Identity Aggregation
        ↓
AggregatedGradientResponse
```

### Validation Logic

Allowed validations:

```text
runtime_version > 0

job_id != ""

worker_id != ""

chunks != nil
```

Worker Service intentionally does NOT validate:

```text
Tensor Shapes

Parameter Ordering

Model Architecture

Gradient Values

Dtypes

Tensor Sizes
```

These responsibilities belong to Runtime.

Worker trusts Runtime.

### Identity Aggregation

Mathematically:

```text
G = G
```

Implementation:

```text
response.chunks
        =
request.chunks
```

No averaging.

No reduction.

No scaling.

No compression.

No optimization.

### Response Construction

Input:

```text
GradientSubmission
```

Output:

```text
AggregatedGradientResponse
```

Important:

```text
response != request
```

The protobuf message changes.

Only the gradient payload is preserved.

Response fields:

```text
runtime_version
    = request.runtime_version

job_id
    = request.job_id

participating_workers
    = 1

aggregation_round
    = 1

chunks
    = request.chunks
```

This mirrors what a real Aggregator will eventually return in future phases.

---

## grpc/handlers.go

Purpose:

Expose Runtime Bridge functionality through gRPC.

This file contains the implementation of:

```text
SynchronizeGradients()
```

Responsibilities:

```text
Receive RPC Request

Call Runtime Bridge

Return RPC Response
```

Flow:

```text
Runtime
      ↓
gRPC
      ↓
SynchronizeGradients()
      ↓
Runtime Bridge
      ↓
AggregatedGradientResponse
      ↓
gRPC
      ↓
Runtime
```

This file contains no aggregation logic.

This file contains no validation logic.

All business decisions are delegated to Runtime Bridge.

Responsibilities are intentionally kept small.

---

## grpc/server.go

Purpose:

Create and manage the Worker Service gRPC server.

Responsibilities:

```text
Create TCP Listener

Create gRPC Server

Register WorkerBridge Service

Start Serving Requests

Graceful Shutdown
```

Initialization Flow:

```text
main.go
      ↓
NewServer()
      ↓
net.Listen()
      ↓
grpc.NewServer()
      ↓
RegisterWorkerBridge()
      ↓
Serve()
```

This file is infrastructure only.

No business logic exists here.

---

## cmd/worker/main.go

Purpose:

Application entry point.

This file bootstraps the entire Worker Service.

Startup Flow:

```text
Start Process
      ↓
Load Config
      ↓
Create Runtime Bridge
      ↓
Create gRPC Server
      ↓
Register Services
      ↓
Start Listening
      ↓
Wait For Shutdown Signal
```

Shutdown Flow:

```text
SIGINT / SIGTERM
      ↓
GracefulStop()
      ↓
Worker Service Exit
```

Responsibilities:

```text
Configuration Initialization

Dependency Wiring

Server Startup

Graceful Shutdown
```

No business logic exists here.

Its only responsibility is application lifecycle management.

---

# Testing

Phase 1 uses a layered testing strategy.

Each test validates a different architectural layer.

---

## runtimebridge_test.go

Purpose:

Validate Runtime Bridge independently of gRPC.

Tests:

```text
Valid Request

Invalid Runtime Version

Missing Job ID

Missing Worker ID

Missing Chunks

Identity Aggregation

Response Construction
```

Architecture:

```text
Test
      ↓
Runtime Bridge
```

No networking.

No protobuf transport.

No gRPC.

This is pure business logic testing.

---

## handler_test.go

Purpose:

Validate the gRPC handler layer.

Tests:

```text
Request Routing

Runtime Bridge Invocation

Response Return
```

Architecture:

```text
Test
      ↓
Handler
      ↓
Runtime Bridge
```

No network communication.

The handler is tested in isolation.

This verifies that RPC requests are correctly forwarded to Runtime Bridge.

---

## sync_test.go

Purpose:

Validate the complete Runtime ↔ Worker communication stack.

This is the most important test in Phase 1.

Architecture:

```text
Client
      ↓
protobuf
      ↓
gRPC
      ↓
Worker Service
      ↓
Runtime Bridge
      ↓
Identity Aggregation
      ↓
AggregatedGradientResponse
      ↓
gRPC
      ↓
protobuf
      ↓
Client
```

What it validates:

```text
Proto Serialization

Proto Deserialization

gRPC Transport

Handler Routing

Runtime Bridge

Identity Aggregation

Response Construction
```

This test proves that the complete communication stack works correctly.

---

# Current Status

```text
Shared Protocol
    ✓ COMPLETE

Generated Protobuf Code
    ✓ COMPLETE

Configuration Layer
    ✓ COMPLETE

Validation Layer
    ✓ COMPLETE

Runtime Bridge
    ✓ COMPLETE

Identity Aggregation
    ✓ COMPLETE

gRPC Handler
    ✓ COMPLETE

gRPC Server
    ✓ COMPLETE

Worker Bootstrap
    ✓ COMPLETE

Unit Tests
    ✓ COMPLETE

Integration Tests
    ✓ COMPLETE

go build ./...
    ✓ PASS

go test ./...
    ✓ PASS
```

---

# Phase 1 Result

Worker Service Phase 1 successfully validates the complete Runtime → Proto → gRPC → Worker → Proto → Runtime communication stack.

The implementation proves that:

```python
loss.backward()

locdist.sync_gradients(model)

optimizer.step()
```

can safely cross the Worker Service boundary while preserving gradient correctness.

Identity Aggregation guarantees:

```text
Gradient Before Sync
        =
Gradient After Sync
```

for a single worker.

This establishes the Runtime ↔ Worker contract and provides the foundation for Phase 2, where Worker Service will evolve from a local synchronization service into a true Runtime Bridge that communicates with a real Aggregator.
