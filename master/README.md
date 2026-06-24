# LDGCC Master

## Overview

The Master is the central coordination component of LDGCC.

It runs on the Brain Laptop and owns all cluster orchestration responsibilities.

Master is the single source of truth for:

* Worker Discovery
* Worker Registration
* Scheduling
* Dataset Sharding
* Workspace Distribution
* Job Orchestration
* Gradient Aggregation
* Output Collection

Workers never communicate directly with each other.

All cluster communication flows through the Master.

---

# Master V1 Architecture

LDGCC V1 follows a coordinator-based architecture.

```text
Worker A
    │
    ▼

Worker B
    │
    ▼

Worker C
    │
    ▼

      Master

    ▲   ▲   ▲

No Worker ↔ Worker Communication
```

All synchronization occurs through the Master.

---

# Future Master Components

The following components represent the long-term Master architecture.

Some are not implemented yet.

```text
master/

├── discovery/
├── scheduler/
├── sharder/
├── worker-manager/
├── orchestrator/
├── aggregator/
└── storage/
```

---

## Discovery

Responsibilities:

* Discover available workers
* Detect worker availability
* Maintain worker presence information

---

## Scheduler

Responsibilities:

* Select workers for jobs
* Allocate dataset shards
* Determine training assignments

---

## Sharder

Responsibilities:

* Read datasets
* Count samples
* Create dataset shards
* Generate shard assignments

---

## Worker Manager

Responsibilities:

* Register workers
* Track worker state
* Maintain worker connections
* Handle heartbeats

---

## Orchestrator

Responsibilities:

* Start jobs
* Coordinate phases
* Manage training lifecycle
* Handle job completion

---

## Aggregator

Responsibilities:

* Receive gradients
* Perform barrier synchronization
* Aggregate gradients
* Return aggregated results

---

## Storage

Responsibilities:

* Job metadata
* Worker metadata
* Checkpoints
* Artifacts
* Logs

---

# Master Architecture Decisions

## Decision 1

Master exposes:

```text
ONE external gRPC server
```

---

## Decision 2

Workers connect to:

```text
Master
```

NOT:

```text
Aggregator
Scheduler
Worker Manager
Orchestrator
```

---

## Decision 3

Worker maintains:

```text
ONE persistent connection
```

to Master.

Worker does not maintain separate connections for:

* Aggregation
* Registration
* Status
* Logs
* Heartbeats

---

## Decision 4

Aggregator is an internal Master component.

Correct architecture:

```text
Worker
    ↓
Master Server
    ↓
Aggregator Component
```

NOT:

```text
Worker
    ↓
Standalone Aggregator
```

---

# Training Lifecycle

## Before Training

Master performs:

```text
Discover Workers
    ↓

Accept Workers
    ↓

Read Dataset
    ↓

Create Dataset Shards
    ↓

Package Project
    ↓

Generate Configurations
    ↓

Send Workspaces
    ↓

Start Training
```

Aggregator is inactive.

---

## During Training

```text
loss.backward()
      ↓

Runtime
      ↓

Worker Service
      ↓

Master Server
      ↓

Aggregator Component
      ↓

Master Server
      ↓

Worker Service
      ↓

Runtime
      ↓

optimizer.step()
```

Aggregator is active only during synchronization.

---

## After Training

Master performs:

```text
Collect Outputs
    ↓

Collect Logs
    ↓

Collect Artifacts
    ↓

Mark Job Completed
```

Aggregator is inactive.

---

# Master Communication Model

```text
Runtime
    ↓

Worker Service
    ↓

Master Server
    ↓

Internal Components

    ├── Aggregator
    ├── Worker Manager
    ├── Scheduler
    ├── Orchestrator
    └── Storage
```

Workers never communicate directly with internal Master components.

---

# Current Implementation Status

Implemented:

```text
✓ Shared Protocol Integration

✓ Identity Aggregation

✓ gRPC Server

✓ Configuration Loading

✓ Validation Layer

✓ Unit Tests

✓ Integration Tests
```

Not Implemented:

```text
✗ Discovery

✗ Scheduler

✗ Sharder

✗ Worker Manager

✗ Worker Registration

✗ Heartbeats

✗ Real Gradient Aggregation

✗ Barrier Synchronization

✗ Orchestration

✗ Artifact Collection
```

---

# Master Phase 1

## Goal

Master Phase 1 exists to validate:

```text
Worker
    ↓
Master
    ↓
Aggregator
    ↓
Master
    ↓
Worker
```

communication using Identity Aggregation.

No distributed-training logic exists yet.

---

## Identity Aggregation

Phase 1 uses:

```text
G = G
```

where:

```text
Input Gradient
=
Output Gradient
```

No mathematical aggregation occurs.

Purpose:

* Validate communication
* Validate protobuf contracts
* Validate gRPC flow
* Validate response handling

---

## Phase 1 Execution Flow

```text
Worker
    ↓

GradientSubmission

    ↓

Master Server

    ↓

Aggregator Service

    ↓

Identity Aggregation

    ↓

AggregatedGradientResponse

    ↓

Worker
```

---

## Folder Structure

```text
master/

├── README.md
│
├── go.mod
├── master_config.json
│
├── cmd/
│   └── master/
│       └── main.go
│
├── grpc/
│   ├── handlers.go
│   └── server.go
│
├── aggregator/
│   └── aggregate.go
│
├── internal/
│   ├── config/
│   │   └── config.go
│   │
│   └── errors/
│       └── errors.go
│
├── generated/
│
└── tests/
```

---

## File Responsibilities

### internal/errors/errors.go

Contains request validation errors.

Examples:

* Invalid runtime version
* Missing job ID
* Missing worker ID
* Missing chunks

---

### internal/config/config.go

Loads:

```text
master_config.json
```

Provides:

```go
Config
```

Current configuration:

```json
{
  "grpc_port": "60051"
}
```

---

### aggregator/aggregate.go

Core business logic.

Responsibilities:

* Request validation
* Identity aggregation
* Response construction

Validation rules:

```text
runtime_version > 0

job_id != ""

worker_id != ""

chunks != nil
```

---

### grpc/handlers.go

Implements:

```text
SynchronizeGradients()
```

Responsibilities:

* Receive request
* Call Aggregator
* Return response

Contains no business logic.

---

### grpc/server.go

Responsibilities:

* Create listener
* Create gRPC server
* Register WorkerBridge service
* Start serving

---

### cmd/master/main.go

Responsibilities:

* Load configuration
* Create Aggregator
* Create gRPC server
* Start service
* Graceful shutdown

---

### generated/

Generated protobuf code.

Produced from:

```text
protocol/gradient.proto
```

---

# Testing

Master Phase 1 includes three validation levels.

---

## aggregator_test.go

Validates:

* Request validation
* Identity aggregation
* Response construction

without gRPC.

---

## handler_test.go

Validates:

```text
Handler
    ↓
Aggregator
```

without networking.

---

## integration_test.go

Validates:

```text
Client
    ↓
gRPC
    ↓
Master
    ↓
Aggregator
    ↓
gRPC
    ↓
Client
```

using a real gRPC server.

---

# Startup Validation

Validated using:

```bash
go run cmd/master/main.go
```

Expected:

```text
master service listening on port 60051
```

This validates:

* Config loading
* Aggregator creation
* Server registration
* Port binding
* Startup path

---

# Phase 1 Result

Master Phase 1 successfully validates:

```text
Worker
    ↓
Master Server
    ↓
Aggregator Service
    ↓
Master Server
    ↓
Worker
```

using Identity Aggregation.

This establishes the Worker ↔ Master contract.

---

# Phase 1 Future TODOs


## Real Aggregation

Current:

```text
G = G
```

Future:

```text
G = Average(All Worker Gradients)
```

---

## Barrier Synchronization

Future Aggregator responsibilities:

* Wait for all workers
* Coordinate aggregation rounds
* Release workers simultaneously

---

# Future Master Phases

## Master Phase 2

Status:

```text
NOT STARTED
```

Expected Goal:

```text
Worker
    ↓
Master
    ↑
Worker
```

Real Worker ↔ Master integration.

---

## Master Phase 3

Status:

```text
NOT STARTED
```

Expected Goal:

* Worker Registration
* Heartbeats
* Worker Lifecycle Management

---

## Master Phase 4

Status:

```text
NOT STARTED
```

Expected Goal:

* Scheduling
* Dataset Sharding
* Training Planning

---

## Master Phase 5

Status:

```text
NOT STARTED
```

Expected Goal:

* Workspace Distribution
* Artifact Collection
* Full Training Orchestration

---

# Current Status

```text
Shared Protocol
    ✓ COMPLETE

Master V1 Architecture
    ✓ FROZEN

Master Phase 1
    ✓ COMPLETE

Identity Aggregation
    ✓ COMPLETE

gRPC Server
    ✓ COMPLETE

Unit Tests
    ✓ COMPLETE

Integration Tests
    ✓ COMPLETE

go build ./...
    ✓ PASS

go test ./...
    ✓ PASS

go run cmd/master/main.go
    ✓ PASS
```

---

# Master Phase 1 Success Criteria

Master Phase 1 is considered complete because:

```text
go build ./...
    ✓ PASS

go test ./...
    ✓ PASS

go run cmd/master/main.go
    ✓ PASS
```

and:

```text
Worker
    ↓
Master Server
    ↓
Aggregator Service
    ↓
Master Server
    ↓
Worker
```

works correctly using the shared LDGCC protocol.
