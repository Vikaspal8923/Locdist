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

✓ Real Gradient Aggregation

✓ Barrier Synchronization

✓ Aggregation Rounds
```

Not Implemented:

```text
✗ Discovery

✗ Scheduler

✗ Sharder

✗ Worker Manager

✗ Worker Registration

✗ Heartbeats

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
│   ├── aggregate.go
│   ├── service.go
│   └── state.go
│
├── coordinator/
│   └── coordinator.go
│
├── jobs/
│   ├── manager.go
│   └── state.go
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

# Master Phase 2

## Goal

Master Phase 2 upgrades the Master Aggregator from Identity Aggregation to real distributed gradient synchronization.

Phase 2 is intentionally narrow.

It implements:

* Gradient storage for the current aggregation round
* Barrier synchronization
* Multi-worker gradient collection
* Duplicate worker submission replacement
* Gradient averaging
* Aggregation round advancement
* Round reset after all waiting workers receive the result

It does not implement:

* Worker discovery
* Worker registration
* Scheduling
* Dataset sharding
* Workspace distribution
* Training orchestration
* Fault tolerance
* Retry logic
* Timeouts
* Checkpointing

---

## Phase 2 Architecture

Phase 2 follows the frozen Master responsibility split:

```text
Worker
    ↓
Master gRPC Handler
    ↓
Coordinator
    ↓
Job Manager
    ↓
Aggregator
```

The responsibilities are separated as follows.

### Job Manager

Owns job metadata only.

Responsibilities:

* Current job
* Expected worker count
* Job status

The Job Manager does not:

* Store gradients
* Track aggregation rounds
* Average tensors
* Synchronize workers
* Manage barriers

---

### Coordinator

Owns request workflow only.

For gradient synchronization:

```text
Receive request
    ↓
CurrentJob()
    ↓
Aggregator.Aggregate(request, expectedWorkers)
```

The Coordinator does not:

* Store gradients
* Average gradients
* Track per-round state
* Implement barrier logic

---

### Aggregator

Owns all distributed synchronization state.

Responsibilities:

* Current aggregation round
* Per-round gradient storage
* Barrier synchronization
* Duplicate worker replacement
* Gradient averaging
* Shared response construction
* Round reset

The Aggregator does not:

* Create jobs
* Own job metadata
* Register workers
* Schedule workers
* Start or stop training

---

## Phase 2 Gradient Flow

```text
Runtime
    ↓
Worker Service
    ↓
Master gRPC Handler
    ↓
Coordinator
    ↓
Job Manager
    ↓
Aggregator
    ↓
Barrier Wait
    ↓
Average Gradients
    ↓
Return AggregatedGradientResponse
    ↓
Worker Service
    ↓
Runtime
```

During aggregation, every worker waits until all expected workers submit for the current round.

Example with two workers:

```text
worker-a submits gradients
    ↓
waits

worker-b submits gradients
    ↓
barrier opens
    ↓
average gradients
    ↓
worker-a and worker-b receive identical result
```

---

## Duplicate Worker Submission

If the same worker submits more than once in the same round:

```text
currentRound.Gradients[worker_id] = latestSubmission
```

The latest submission replaces the older one.

The worker count does not increase.

---

## Gradient Averaging

For every matching gradient chunk:

```text
Average = (G1 + G2 + ... + Gn) / N
```

where:

```text
N = expected worker count
```

All waiting workers receive the exact same averaged gradient chunks.

Supported runtime dtypes:

* torch.float16
* torch.float32
* torch.float64
* torch.bfloat16

---

## Aggregation Rounds

The Aggregator starts at round 1.

After a round completes:

```text
round 1
    ↓
all workers receive response
    ↓
clear round state
    ↓
round 2
```

The completed round number is returned in:

```text
AggregatedGradientResponse.aggregation_round
```

---

## Phase 2 File Responsibilities

### aggregator/state.go

Defines per-round Aggregator state.

Contains:

* Round number
* Worker gradient submissions
* Completed response
* Aggregation error
* Waiting receiver count

---

### aggregator/service.go

Defines the Aggregator service and synchronization primitives.

Responsibilities:

* Create Aggregator service
* Own mutex and condition variable
* Store gradients by worker ID
* Count received workers
* Check barrier status
* Reset completed rounds

---

### aggregator/aggregate.go

Core Phase 2 aggregation logic.

Responsibilities:

* Validate incoming gradient submissions
* Store current-round gradients
* Block workers at the barrier
* Replace duplicate worker submissions
* Average gradient bytes by dtype
* Return identical aggregated responses
* Advance/reset aggregation rounds

---

### coordinator/coordinator.go

Workflow layer between gRPC and internal Master components.

Responsibilities:

* Start the current job through Job Manager
* Read current job metadata
* Pass expected worker count into Aggregator

No aggregation logic belongs here.

---

### jobs/manager.go

Owns job metadata lifecycle.

Responsibilities:

* Create a current job
* Store expected worker count
* Return current job metadata

Does not store gradients or aggregation rounds.

---

### jobs/state.go

Defines job metadata.

Contains:

* Job ID
* Expected worker count
* Job status

---

### grpc/handlers.go

Receives external WorkerBridge gRPC requests.

Responsibilities:

* Accept SynchronizeGradients requests
* Call Coordinator
* Return AggregatedGradientResponse

Contains no aggregation logic.

---

### grpc/server.go

Creates and registers the Master gRPC server.

Responsibilities:

* Open listener
* Create gRPC server
* Register WorkerBridge service
* Route requests through MasterServer

---

### cmd/master/main.go

Master process entry point.

Responsibilities:

* Load configuration
* Create Aggregator
* Create Job Manager
* Create Coordinator
* Create gRPC server
* Start service
* Graceful shutdown

---

## Phase 2 Tests

### aggregator_test.go

Validates:

* Single-worker aggregation
* Barrier blocking
* Multi-worker averaging
* Duplicate worker replacement
* Round advancement
* Request validation

---

### handler_test.go

Validates:

```text
Master gRPC Handler
    ↓
Coordinator
    ↓
Job Manager
    ↓
Aggregator
```

without opening a network listener.

---

### integration_test.go

Validates:

```text
gRPC Client
    ↓
Master gRPC Server
    ↓
Coordinator
    ↓
Job Manager
    ↓
Aggregator
    ↓
gRPC Response
```

using a real local gRPC server.

---

## Phase 2 Validation

Validated using:

```bash
env GOCACHE=/tmp/locdist-go-cache go test ./...
```

Expected result:

```text
ok github.com/Vikaspal8923/Locdist/master/tests
```

---

## Phase 2 Result

Master Phase 2 successfully validates:

```text
Worker
    ↓
Master Server
    ↓
Coordinator
    ↓
Job Manager
    ↓
Aggregator
    ↓
Barrier Synchronization
    ↓
Gradient Averaging
    ↓
Master Server
    ↓
Worker
```

This completes real distributed gradient synchronization inside the Master Aggregator.

---

# Master Phase 3

## Goal

Master Phase 3 adds the first Worker lifecycle foundation:

```text
Worker starts
    ↓
RegisterWorker
    ↓
Master stores Worker metadata
    ↓
UpdateWorkerStatus(IDLE)
    ↓
Master stores current status
```

Worker discovery, approval UI, heartbeats, scheduling, persistence, and
training launch remain outside this phase.

## Architecture

```text
Worker Service
    ↓ RegisterWorker / UpdateWorkerStatus
Master gRPC Handler
    ↓
Coordinator
    ↓
Worker Manager
    ↓
In-memory Worker State
```

The Worker Manager owns a concurrency-safe map keyed by `worker_id`.
Registering an existing ID refreshes its host and gRPC port instead of
creating a duplicate. Status updates are accepted only for registered
workers.

Supported statuses:

```text
IDLE
PREPARING
INSTALLING
RUNNING
COMPLETED
FAILED
```

`UNKNOWN` is reserved as the protobuf zero value and is not accepted as a
reported lifecycle status.

## File Responsibilities

`protocol/gradient.proto`

Defines Worker status values, registration messages, status messages, and
the two control RPCs.

`workers/state.go`

Defines the metadata and lifecycle state stored for one Worker.

`workers/manager.go`

Validates registrations and status updates, refreshes duplicate
registrations, and provides concurrency-safe Worker lookup.

`coordinator/coordinator.go`

Coordinates registration and status requests between gRPC handlers and the
Worker Manager.

`grpc/handlers.go`

Exposes `RegisterWorker` and `UpdateWorkerStatus`.

`cmd/master/main.go`

Creates the Worker Manager and injects it into the Coordinator.

`tests/workers_test.go`

Covers registration, status changes, duplicate replacement, and invalid
status updates.

---

# LDGCC Phase 4: LAN Discovery

## Goal

Phase 4 lets Master find running LDGCC Worker Apps on the same LAN without
knowing their addresses in advance.

```text
Worker owner clicks Start Worker
    ↓
Worker advertises _ldgcc-worker._tcp.local
    ↓
Master scans with mDNS/DNS-SD
    ↓
Master stores a temporary discovery record
```

Discovery records are deliberately separate from registered Worker state.
An mDNS result provides a network location, not a trusted identity.

## Advertised Metadata

```text
worker name
host/address
Worker gRPC port
protocol version
pairing status
```

## Master Components

`discovery/browser.go`

Scans `_ldgcc-worker._tcp.local` and converts DNS-SD records into LDGCC
discovery records.

`discovery/registry.go`

Maintains a concurrency-safe temporary registry, refreshes repeated
sightings, and expires Workers that stop advertising.

`discovery/service.go`

Runs periodic scans and records Worker arrival and disappearance.

`discovery/state.go`

Defines temporary discovery metadata.

`cmd/master/main.go`

Starts and stops discovery with the Master process.

## Scope Boundary

Included:

* Same-LAN Worker discovery
* Temporary presence tracking
* Protocol and pairing-state metadata
* Discovery expiry

Deferred:

* Pairing request
* Accept or reject
* Authentication
* Permanent identity assignment
* Automatic Worker configuration
* Heartbeats and scheduling

---

# LDGCC Phase 5: Worker Pairing and Connection Management

## Goal

Phase 5 connects temporary Phase 4 discovery to trusted Phase 3
registration:

```text
Master discovers unpaired Worker
    ↓
Master owner clicks Connect
    ↓
Worker owner accepts or rejects
    ↓
Master reserves worker_id and credential
    ↓
Worker stores one pairing record
    ↓
Worker performs authenticated registration
    ↓
Worker reports IDLE
```

## V1 Topology Rule

```text
One Master → multiple Workers
One Worker → one Master at a time
```

A paired Worker rejects requests from every other Master. Changing Master
requires Reset Previous Connection on the Worker.

## Master Components

`app/`

Provides the local Master control surface at `127.0.0.1:6060`, lists
discovered Workers, and starts pairing requests.

`pairing/service.go`

Generates random request IDs, Worker IDs, and 256-bit pairing credentials;
reserves credentials before contacting Worker; and waits for the owner's
decision.

`workers/store.go`

Atomically persists Master-side pairing credentials with owner-only file
permissions so Workers can reconnect after a Master restart.

`workers/manager.go`

Authenticates registration and unpair requests against the saved pairing
credential.

## Connection Reset

When an online Worker resets its previous connection:

```text
Worker sends authenticated UnpairWorker
    ↓
Master revokes credential and registered state
    ↓
Worker deletes its local pairing
```

If the old Master is offline, Worker still removes its local pairing and can
join another Master. The deleted credential is no longer available to the
Worker.

## Security Boundary

Phase 5 provides random pairing credentials, credential-authenticated
registration/unpairing, atomic files, and `0600` credential-file
permissions. TLS, certificate pinning, OS credential-vault storage, and
signed installers remain production hardening outside this phase.

---

# LDGCC Phase 6: Worker Heartbeats and Availability

## Goal

Phase 6 makes paired Workers continuously prove they are still reachable.

```text
Worker registers
    ↓
Worker sends authenticated Heartbeat
    ↓
Master updates last_seen and availability
    ↓
Master sweeper marks stale/offline when heartbeats stop
```

## Availability States

```text
ONLINE
STALE
OFFLINE
```

`ONLINE` means the Master recently received a valid registration or
heartbeat. `STALE` means heartbeats are late. `OFFLINE` means the Worker
explicitly stopped or missed the offline timeout.

## Master Components

`workers/state.go`

Adds Worker availability and `last_seen` tracking.

`workers/manager.go`

Authenticates `Heartbeat` and `GoingOffline`, updates Worker status/job
metadata, and runs the availability sweep.

`coordinator/coordinator.go`

Exposes heartbeat and offline actions to the gRPC layer.

`grpc/handlers.go`

Implements the new protocol RPC handlers.

`cmd/master/main.go`

Runs the periodic availability sweeper.

---

# Master Phase Roadmap

## Master Phase 2

Status:

```text
COMPLETE
```

Completed Goal:

```text
Worker
    ↓
Master
    ↑
Worker
```

Real Master-side gradient synchronization.

---

## Master Phase 3

Status:

```text
COMPLETE
```

Completed Goal:

* Worker Registration
* Worker Status Foundation
* Duplicate Registration Refresh
* In-Memory Worker Registry

Heartbeats and failure detection are deferred to a later phase.

---

## Master Phase 4

Status:

```text
COMPLETE
```

Completed Goal:

* mDNS/DNS-SD Worker Discovery
* Temporary Discovered-Worker Registry
* Worker Presence Expiry

---

## Master Phase 5

Status:

```text
COMPLETE
```

Completed Goal:

* Pairing and Approval
* Permanent Worker Identity
* Automatic Worker Configuration
* One-Master Enforcement
* Pairing Persistence and Revocation

---

## Master Phase 6

Status:

```text
COMPLETE
```

Completed Goal:

* Authenticated Worker Heartbeats
* Worker Availability Tracking
* Stale and Offline Detection
* Explicit GoingOffline Handling

---

# Current Status

```text
Shared Protocol
    ✓ COMPLETE

Master V1 Architecture
    ✓ FROZEN

Master Phase 1
    ✓ COMPLETE

Master Phase 2
    ✓ COMPLETE

Master Phase 3
    ✓ COMPLETE

Worker Registration
    ✓ COMPLETE

Worker Status Foundation
    ✓ COMPLETE

LDGCC Phase 4
    ✓ COMPLETE

LAN Worker Discovery
    ✓ COMPLETE

LDGCC Phase 5
    ✓ COMPLETE

LDGCC Phase 6
    ✓ COMPLETE

Worker Heartbeats
    ✓ COMPLETE

Availability Detection
    ✓ COMPLETE

Worker Pairing and Approval
    ✓ COMPLETE

Authenticated Registration
    ✓ COMPLETE

Identity Aggregation
    ✓ COMPLETE

Real Gradient Aggregation
    ✓ COMPLETE

Barrier Synchronization
    ✓ COMPLETE

Aggregation Rounds
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
