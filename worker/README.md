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
│ Aggregator                                    |
| Storage                                       |
| Worker Manager                                │
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
Master Server
    ↓
Aggregator Component
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

This establishes the Runtime ↔ Worker contract and provides the foundation for Phase 2, where Worker Service will evolve from a local synchronization service into a true Runtime Bridge that communicates with the Master Server and participates in real distributed gradient aggregation through the Aggregator component.


NOTE:

Phase 1 stores worker_config.json in the Worker root directory.

This is a temporary development setup.

A future Worker Infrastructure phase may relocate the file to:

configs/worker_config.json

or

~/.ldgcc/worker_config.json

to support installation-wide configuration and Master-managed deployment.


# Worker Phase 2

## Goal

Worker Phase 2 exists to connect the Worker Service to the Master.

Phase 1 validated:

```text
Runtime
    ↓
Worker
    ↓
Identity Aggregation
    ↓
Runtime
```

Phase 2 replaces local Identity Aggregation with a real Master connection.

Target architecture:

```text
Runtime
    ↓
Worker
    ↓
Master
    ↓
Aggregator
    ↓
Master
    ↓
Worker
    ↓
Runtime
```

---

## Worker Phase 2 Architecture Decisions

### Worker Becomes A Proxy

Worker no longer owns aggregation logic.

Current:

```text
Runtime
    ↓
Worker
    ↓
Identity Aggregation
```

Phase 2:

```text
Runtime
    ↓
Worker
    ↓
Master
```

Aggregation is owned entirely by Master.

---

### Runtime API Remains Unchanged

Runtime continues using:

```python
loss.backward()

locdist.sync_gradients(model)

optimizer.step()
```

No Runtime modifications were required.

---

### Persistent Worker → Master Connection

Worker maintains:

```text
ONE persistent gRPC connection
```

to Master.

Connection creation occurs once during Worker startup.

The connection is reused throughout training.

---

### Configuration Driven

Master location comes from:

```text
worker_config.json
```

Example:

```json
{
  "grpc_port": "50051",

  "master_host": "127.0.0.1",
  "master_port": "60051"
}
```

No Master networking values are hardcoded.

---

### Shared Protocol

Worker and Master continue using:

```text
GradientSubmission

AggregatedGradientResponse
```

No new protobuf definitions were introduced.

---

## Phase 2 Execution Flow

```text
Runtime
    ↓

Worker Handler

    ↓

RuntimeBridge

    ↓

MasterClient

    ↓

Master Server

    ↓

Aggregator

    ↓

Master Server

    ↓

MasterClient

    ↓

RuntimeBridge

    ↓

Worker Handler

    ↓

Runtime
```

---

## New Components

### masterclient/client.go

Introduced in Phase 2.

Responsibilities:

* Create Master connection
* Create WorkerBridge client
* Forward GradientSubmission
* Receive AggregatedGradientResponse
* Manage connection lifecycle

---

### runtimebridge/synchronizer.go

Introduced in Phase 2.

Provides:

```go
type Synchronizer interface
```

Purpose:

* Decouple RuntimeBridge from MasterClient
* Improve testability
* Allow fake implementations during tests

---

## RuntimeBridge Refactor

Phase 1:

```text
Validate
    ↓
Identity Aggregation
    ↓
Return
```

Phase 2:

```text
Validate
    ↓
Synchronizer
    ↓
Master
    ↓
Return
```

RuntimeBridge no longer performs aggregation.

---

## Testing Refactor

Phase 1 tests depended on local Identity Aggregation.

Phase 2 introduced:

```text
FakeSynchronizer
```

for testing.

Production:

```text
Synchronizer
    ↓
MasterClient
```

Tests:

```text
Synchronizer
    ↓
FakeSynchronizer
```

This preserves isolated Worker tests without requiring a running Master.

---

## Live Validation

### Validation 1

Master Running

Runtime integration test:

```text
Before Sync
After Sync
Identical: True
```

Result:

```text
Runtime
    ↓
Worker
    ↓
Master
    ↓
Aggregator
    ↓
Master
    ↓
Worker
    ↓
Runtime
```

validated successfully.

---

### Validation 2

Master Stopped

Runtime integration test produced:

```text
SynchronizationError

connection refused

127.0.0.1:60051
```

Result:

```text
Runtime
    ↓
Worker
    ↓
Master
```

failed as expected.

This proves Worker is forwarding requests to Master and no longer performs local aggregation.

---

## Phase 2 Result

Worker Phase 2 successfully replaces local Identity Aggregation with a real Master connection.

Validated communication path:

```text
Runtime
    ↓
Worker
    ↓
Master
    ↓
Aggregator
    ↓
Master
    ↓
Worker
    ↓
Runtime
```

using the shared LDGCC protocol.

This establishes the first complete end-to-end LDGCC execution path.

---

# Worker Phase 3

Worker Phase 3 adds config-based identity, Master registration, and Worker
status reporting.

## Startup Flow

```text
Worker reads worker_config.json
    ↓
Worker connects to Master
    ↓
RegisterWorker(worker_id, host, grpc_port)
    ↓
UpdateWorkerStatus(IDLE)
    ↓
Worker starts its Runtime-facing gRPC server
```

The Worker does not discover itself or receive an invitation in this phase.
Its stable `worker_id` is supplied in `worker_config.json`. A later discovery
and approval phase may create or update that configuration.

## Configuration

```json
{
  "worker_id": "worker-a",
  "grpc_port": "50051",
  "host": "127.0.0.1",
  "master_host": "127.0.0.1",
  "master_port": "60051"
}
```

## Status Model

The Worker status manager reports and stores:

```text
IDLE
PREPARING
INSTALLING
RUNNING
COMPLETED
FAILED
```

Local status changes are committed only after Master acknowledges the
update.

## File Responsibilities

`internal/config/config.go`

Loads `worker_id`, the advertised Worker host and gRPC port, and the Master
address.

`masterclient/client.go`

Provides registration and status RPCs with bounded control-call timeouts,
alongside gradient synchronization.

`status/manager.go`

Owns the Worker's current status and reports transitions to Master.

`cmd/worker/main.go`

Registers the Worker, reports initial `IDLE`, and then starts the
Runtime-facing service.

`tests/masterclient_test.go`

Tests registration, status, and gradient calls over a real local gRPC
connection.

`tests/status_test.go`

Tests successful status storage and failed-report behavior.

---

# LDGCC Phase 4: Worker App and LAN Discovery

## Goal

Phase 4 gives the Worker laptop owner an explicit Start/Stop control and
makes a running Worker visible to Master on the same LAN.

```text
Open LDGCC Worker App
    ↓
Click Start Worker
    ↓
Start Worker gRPC service
    ↓
Advertise with mDNS/DNS-SD
    ↓
Master discovers Worker
```

Stopping the Worker removes its advertisement and stops its gRPC service.

## Worker Modes

Unpaired:

```text
No worker_id
    ↓
Worker becomes discoverable
    ↓
Gradient synchronization remains unavailable
```

Paired:

```text
worker_id exists
    ↓
Phase 3 registration and IDLE reporting
    ↓
Worker becomes discoverable as paired
```

This allows first-run discovery without weakening the existing registration
path.

## Folder Responsibilities

`app/`

Contains the UI-independent Start/Stop controller and the local clickable
Worker App surface.

`discovery/`

Owns `_ldgcc-worker._tcp.local` advertisement and its DNS-SD metadata.

`service/`

Coordinates Worker gRPC, discovery, Master registration, and shutdown as one
lifecycle.

`cmd/worker-app/`

Starts the Worker App at `http://127.0.0.1:5050`. The app initially shows a
stopped Worker and provides Start Worker and Stop Worker actions.

`cmd/worker/`

Retains headless startup for service installations and automated deployment.

## Configuration

Phase 4 adds:

```json
{
  "worker_name": "Vikas-Laptop",
  "app_port": "5050"
}
```

`worker_name` is the human-readable LAN discovery name. It is not a trusted
or permanent Worker identity.

## Scope Boundary

Included:

* Start Worker
* Stop Worker
* Running and discoverable state
* Paired and unpaired startup modes
* mDNS/DNS-SD advertisement
* Master discovery

Deferred:

* Native tray packaging and OS installer integration
* Pairing request UI
* Accept or reject
* Pairing credentials
* Automatic `worker_config.json` creation
* Scheduling and training execution

---

# LDGCC Phase 5: Pairing and Connection Management

## Goal

Phase 5 removes the need to manually add `worker_id` or Master connection
values to `worker_config.json`.

```text
Worker advertises as unpaired
    ↓
Master sends PairWorker
    ↓
Worker App shows Master identity
    ↓
Owner selects Accept or Reject
```

On acceptance:

```text
Save pairing.json
    ↓
Connect to saved Master
    ↓
Register with pairing credential
    ↓
Report IDLE
    ↓
PAIRED_ONLINE
```

## Connection States

Connection state is separate from training state:

```text
UNPAIRED
PAIRING_PENDING
PAIRED_ONLINE
PAIRED_OFFLINE
```

The Worker App can start when its saved Master is offline. It remains
`PAIRED_OFFLINE`; it does not silently delete or replace the pairing.

## One-Master Rule

LDGCC V1 supports exactly one saved Master per Worker. A second Master is
rejected until the owner uses Reset Previous Connection.

## Reset Previous Connection

Reset:

* Revokes the credential on an online Master
* Closes the old Master client
* Deletes the local pairing file
* Returns Worker to `UNPAIRED`
* Refreshes its LAN advertisement

Training-state enforcement will block reset during active execution once
the Executor phase introduces running jobs.

## Folder Responsibilities

`pairing/manager.go`

Owns pending requests, Accept/Reject decisions, one-Master enforcement, and
the current pairing record.

`pairing/store.go`

Atomically writes and deletes `pairing.json` with `0600` permissions.

`service/agent.go`

Coordinates connection states, approval, authenticated registration,
offline startup, advertisement refresh, and reset.

`app/`

Displays pending Master identity, Accept/Reject actions, current connection
state, and Reset Previous Connection.

## Configuration Ownership

`worker_config.json` contains installation settings:

```text
worker_name
Worker gRPC port
Worker App port
host
pairing file location
```

Accepted pairing creates `pairing.json` containing:

```text
worker_id
master_id and name
Master host and gRPC port
pairing credential
```

The Master supplies these values; Worker writes them locally.

## Security Boundary

Pairing credentials are randomly generated and registration/reset are
credential-authenticated. Credential files are atomic and owner-readable
only. TLS, certificate pinning, and OS credential-vault integration remain
production hardening work.

---

## Current Status

```text
Worker Phase 1
    ✓ COMPLETE

Worker Phase 2
    ✓ COMPLETE

Worker Phase 3
    ✓ COMPLETE

Worker Registration
    ✓ COMPLETE

Worker Status Foundation
    ✓ COMPLETE

LDGCC Phase 4
    ✓ COMPLETE

Worker App Start/Stop
    ✓ COMPLETE

LAN Discovery Advertisement
    ✓ COMPLETE

LDGCC Phase 5
    ✓ COMPLETE

Pairing Accept/Reject
    ✓ COMPLETE

One Master per Worker
    ✓ ENFORCED

Reset Previous Connection
    ✓ COMPLETE

MasterClient
    ✓ COMPLETE

Synchronizer Interface
    ✓ COMPLETE

RuntimeBridge Refactor
    ✓ COMPLETE

Unit Tests
    ✓ PASS

Integration Tests
    ✓ PASS

Runtime ↔ Worker ↔ Master
    ✓ LIVE VALIDATED

go build ./...
    ✓ PASS

go test ./...
    ✓ PASS
```

---

## Future TODOs

### Master Phase 2

Current:

```text
G = G
```

Future:

```text
G = Average(All Worker Gradients)
```

---

### Barrier Synchronization

Aggregator will eventually:

* Wait for all workers
* Coordinate aggregation rounds
* Release workers simultaneously

---

### Multi-Worker Training

Current:

```text
Single Worker Validation
```

Future:

```text
Multiple Workers
    ↓
Master
    ↓
Aggregation
```

---

## Worker Phase 2 Success Criteria

Worker Phase 2 is considered complete because:

```text
go build ./...
    ✓ PASS

go test ./...
    ✓ PASS
```

and:

```text
Runtime
    ↓
Worker
    ↓
Master
    ↓
Aggregator
    ↓
Master
    ↓
Worker
    ↓
Runtime
```

has been successfully validated using a real Runtime process, Worker Service, and Master Service.
