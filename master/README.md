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
✗ Orchestration

✗ Workspace Distribution

✗ Worker Environment Setup

✗ Worker Execution

✗ Artifact Collection

✗ VS Code Extension UI
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

# LDGCC Phase 7: Job Spec and Dataset Sharding Foundation

## Goal

Phase 7 teaches the Master how to prepare a user project for a distributed
training job before any Worker execution exists.

```text
VS Code project folder
    ↓
ldgcc.yaml
    ↓
Master selects ONLINE Workers
    ↓
Master validates dataset/train.jsonl
    ↓
Master creates one shard per selected Worker
    ↓
Prepared job metadata is stored in memory
```

## Frozen V1 Project Spec

`ldgcc.yaml`

```yaml
job:
  name: movie-review-training

entrypoint: train.py

dataset:
  train: dataset/train.jsonl

workers:
  count: 3

outputs:
  - model/model.pt
  - results/
```

`job.name` is optional. `entrypoint`, `dataset.train`, and
`workers.count` are required. `outputs` is optional and accepts relative files
or directories. Both `ldgcc.yaml` and `ldgcc.yml` are supported; `.yml` is
preferred when both exist.

## Dataset Rule

V1 supports line-based JSONL sharding only.

```text
dataset/train.jsonl
```

Each non-empty line must be one JSON object. The Master splits lines across
the selected Workers and writes each shard back under the same relative
path.

```text
master/jobs/<job_id>/shards/
    worker-a/dataset/train.jsonl
    worker-b/dataset/train.jsonl
    worker-c/dataset/train.jsonl
```

Worker code will still read:

```text
dataset/train.jsonl
```

but each Worker will receive different file contents in a later workspace
distribution phase.

## Phase 7 Components

`project/`

Loads and validates `ldgcc.yaml`.

`scheduler/`

Selects exactly `workers.count` Workers from registered Workers with
`ONLINE` availability.

`sharder/`

Validates JSONL, computes even shard ranges, and writes per-Worker shard
files.

`orchestrator/`

Combines project spec loading, Worker selection, dataset sharding, and job
metadata preparation.

`jobs/`

Stores prepared job metadata, selected Workers, and shard assignments.

## Not In Phase 7

```text
Project zip packaging
Workspace upload to Worker
Dependency installation
Training process execution
VS Code extension UI
Output collection
```

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

## Master Phase 7

Status:

```text
COMPLETE
```

Completed Goal:

* `ldgcc.yaml` Project Spec
* Required Worker Count
* ONLINE Worker Selection
* JSONL Dataset Validation
* Per-Worker Dataset Shards
* Prepared Job Metadata

---

# LDGCC Phase 8: Project Packaging and Worker Workspace Delivery

Phase 8 turns the Phase 7 job plan into real, Worker-specific project
workspaces. The Master packages the user's full project once per selected
Worker, replaces `dataset/train.jsonl` with that Worker's shard at the same
relative path, writes `job_config.json`, and sends the ZIP over authenticated
gRPC.

```text
Prepare project and shards (Phase 7)
    -> Build one project ZIP per Worker
    -> Replace original dataset with assigned shard
    -> Authenticate with saved pairing credentials
    -> PrepareWorkspace gRPC
    -> Worker validates and extracts workspaces/<job_id>
```

## Phase 8 Components

* `packager/package.go`: copies project files, applies exclusions, substitutes
  the shard, emits `job_config.json`, skips symlinks, and enforces a 64 MiB ZIP
  limit.
* `orchestrator/distribute.go`: maps shards to Workers, builds each package,
  authenticates with the pairing token, and delivers it to the Worker.
* `PrepareAndDistribute`: joins Phase 7 preparation and Phase 8 delivery for
  the future VS Code extension command.
* `protocol/gradient.proto`: defines the `PrepareWorkspace` request/response.

Excluded local state includes `.git`, virtual environments, Python caches,
`.ldgcc`, and `ldgcc_jobs`. Phase 8 prepares files only; dependency setup and
training process execution belong to the next phase.

---

# LDGCC Phase 9: Worker Setup and Readiness

Phase 9 prepares every selected Worker for training without starting the user
entrypoint. It is the backend for the future VS Code extension's **Set Up
Workers** action.

```text
User chooses Set Up Workers
    -> Master sends SetupJob to all assigned Workers concurrently
    -> Worker creates a private .venv
    -> Worker installs requirements.txt when present
    -> Worker returns READY or SETUP_FAILED
    -> Master enables Start Training only when every Worker is READY
```

## Phase 9 Components

* `orchestrator/setup.go`: runs setup concurrently, stores each response, checks
  the all-ready barrier, retries one failed Worker, or retries all failures.
* `jobs/manager.go`: owns job-specific setup states and the `AllWorkersReady`
  gate.
* `protocol/gradient.proto`: defines authenticated `SetupJob` messages and the
  `WORKSPACE_RECEIVED`, `SETTING_UP`, `READY`, and `FAILED` states.

Setup may take several minutes, so each Worker receives an independent
15-minute request timeout. A failure does not start training. Retrying rebuilds
only that Worker's environment; Workers already marked READY are not repeated.

Phase 9 intentionally does not launch `train.py`. The user-controlled,
synchronized start belongs to Phase 10.

---

# LDGCC Phase 10: Synchronized Training Start

Phase 10 implements the backend for the user-controlled **Start Training**
action. Master starts only a prepared job whose assigned Workers are all READY,
online, and not already training.

```text
User selects Start Training
    -> Master sends ArmJob concurrently
    -> Every Worker validates and returns ARMED
    -> Master sends ReleaseJob concurrently
    -> Workers launch their training entrypoints
    -> Runtime -> local Worker -> Master aggregation begins
```

`orchestrator/training.go` owns the all-Worker arm barrier. Any arm or release
failure triggers authenticated `StopJob` rollback and marks the complete job
FAILED. The explicit stop path stops all assigned Workers and marks the job
CANCELLED.

`jobs/manager.go` tracks each Worker's ARMED, RUNNING, FAILED, or CANCELLED
result. Continuous completion polling and disconnect recovery remain Phase 11.

---

# LDGCC Phase 11: Lifecycle Monitoring and Job Reset

Phase 11 monitors every required training process after synchronized start. A
failed, cancelled, offline, or unreachable Worker fails the complete job.

```text
Poll authenticated Worker status
    -> any Worker fails/disconnects
    -> abort the aggregation barrier
    -> stop surviving processes
    -> capture exit codes and bounded log tails
    -> remove Master and Worker job data
    -> archive a compact final summary
    -> clear the active job
```

Pairing and online Worker registration are preserved. The user returns to the
connected-Workers state and must run **Prepare Job**, **Set Up Workers**, and
**Start Training** again. New preparation reads the project again and removes
old Master job data. A Worker that missed cleanup while offline removes stale
workspaces when it accepts the next job.

`orchestrator/lifecycle.go` also supports job timeout, explicit cancellation,
and successful completion when every Worker reports COMPLETED.

---

# LDGCC Phase 12: Configurable Results and Logs

Phase 12 collects only outputs declared by the user in `ldgcc.yml`, plus setup
and training logs. When `outputs` is omitted, LDGCC collects logs and the final
summary without guessing which project files are models or metrics.

```yaml
outputs:
  - model/model.pt
  - results/metrics.json
  - checkpoints/
```

Workers return an authenticated manifest containing relative paths, sizes, and
SHA-256 checksums. Master downloads files through bounded gRPC streams, verifies
every size and checksum, and atomically publishes:

```text
ldgcc_results/<job_id>/
  summary.json
  logs/<worker_id>/
  workers/<worker_id>/outputs/<declared paths>
```

Successful jobs require every configured output. Failed jobs preserve whatever
safe logs and outputs are available. Absolute paths, traversal, symlinks,
undeclared downloads, files over 64 MiB, and Worker result sets over 256 MiB are
rejected. Worker workspaces are cleaned only after collection.

---

# LDGCC Phase 13: Unified Local Application API

Phase 13 composes discovery, pairing, preparation, setup, training, lifecycle,
and results into one localhost API for the VS Code extension.

```text
GET  /health
GET  /state
GET  /events
POST /discovery/start
GET  /workers/discovered
POST /workers/{instance}/pair
POST /jobs/prepare
POST /jobs/setup
POST /jobs/setup/retry
POST /jobs/start
POST /jobs/stop
GET  /jobs/current
GET  /jobs/last-summary
GET  /results/{job_id}
POST /shutdown
```

The API accepts only loopback hosts and requires the extension's bearer session
token. `/events` uses Server-Sent Events for Worker, setup, training, failure,
completion, and result notifications. `/state` returns one secret-free snapshot
with readable state names.

Production process options:

```text
--config
--data-dir
--app-host
--app-port
--session-token
```

Port `0` selects an available port. Master writes a mode-0600,
`master-session.json` atomically with PID, host, port, version, address, and
session token. The extension can health-check/reuse that process and request
graceful shutdown. Pairings, jobs, results, and session metadata live under the
provided data directory.

---

# LDGCC Phase 14: VS Code Extension Control Surface

Phase 14 adds `extension/`, the first user-facing Brain Laptop control surface
for LDGCC V1. It uses the Phase 13 localhost API instead of talking directly to
Master internals.

```text
VS Code extension
    -> start or reuse local Master
    -> read master-session.json
    -> authenticate with bearer token
    -> subscribe to /events
    -> drive discovery, pairing, prepare, setup, training, and results
```

Development mode starts Master with `go run ./cmd/master`. Production mode can
set `ldgcc.master.binaryPath` to the bundled Master binary while keeping the
same session-file and API contract.

The extension contributes:

* `LDGCC` activity bar view
* cluster tree for Master, discovered Workers, registered Workers, job state,
  and last results
* commands for Start/Stop Master, Discover Workers, Pair Worker, Prepare Job,
  Set Up Workers, Retry Setup, Start/Stop Training, and Open Results
* Server-Sent Events subscription for live state refreshes

---

# LDGCC Phase 16: Communication Compression

Phase 16 adds the first performance-oriented gradient communication path.
`ldgcc.yml` can now carry a `communication` block:

```yaml
communication:
  precision: fp16
  compression:
    type: topk
    mode: global
    top_k: 5%
    error_feedback: true
    warmup_steps: 0
```

Master validates and packages this config into each Worker's `job_config.json`.
Worker injects it into the training process as `LDGCC_COMMUNICATION`. Runtime
uses it to choose dense, global top-k, or per-layer top-k communication.

The protocol now marks every gradient chunk with payload dtype, encoding mode,
and sparse indices for top-k chunks. Master aggregation reconstructs sparse
chunks safely, checks index bounds and duplicate indices, averages in float32,
and returns dense averaged gradients.

Defaults:

```text
precision: fp32
compression.type: none
top-k mode default: global
top-k default: 5%
error feedback: required for top-k
warmup_steps: 0
```

---

# LDGCC Phase 17: Sparse Aggregated Response

Phase 17 completes the top-k communication path by compressing the return trip
from Master back to Runtime.

```text
Runtime -> Worker -> Master
    sparse top-k upload

Master -> Worker -> Runtime
    sparse union response
```

When Workers submit top-k chunks, each Worker may send different indices. Master
averages over the union of submitted indices and returns only that sparse union.
Missing indices from a Worker count as zero for that round, matching the Phase
16 dense reconstruction semantics.

Dense warmup and `compression.type: none` still return dense gradients.

For a 50M parameter model with `top_k: 5%` and fp16 values, the rough shape is:

```text
Before Phase 17:
  upload   ~25 MB+
  download ~100 MB

After Phase 17:
  upload   ~25 MB+
  download ~25 MB+
```

Runtime applies sparse responses by creating a zero gradient tensor and filling
only returned indices. Dropped local values remain protected by residual/error
feedback.

---

# LDGCC Phase 18: Packed Sparse Indices

Phase 18 reduces top-k payload size by replacing protobuf `repeated int64`
sparse indices with packed little-endian uint32 bytes.

```text
Before:
  fp16 value = 2 bytes
  int64 index = 8 bytes
  total      = 10 bytes per selected gradient

After:
  fp16 value  = 2 bytes
  uint32 index = 4 bytes
  total       = 6 bytes per selected gradient
```

The protocol keeps the old `indices` field for compatibility and adds
`indices_u32` for the compact form. Runtime writes `indices_u32`; Master reads
either format and returns `indices_u32` for sparse responses.

For a 50M parameter model with `top_k: 5%`, one sparse direction drops from
roughly:

```text
2.5M selected gradients * 10 bytes = ~25 MB+
2.5M selected gradients *  6 bytes = ~15 MB+
```

If a sparse index exceeds uint32 capacity, aggregation fails clearly instead of
silently changing payload format.

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

LDGCC Phase 7
    ✓ COMPLETE

LDGCC Phase 8
    ✓ COMPLETE

LDGCC Phase 9
    ✓ COMPLETE

LDGCC Phase 10
    ✓ COMPLETE

LDGCC Phase 11
    ✓ COMPLETE

LDGCC Phase 12
    ✓ COMPLETE

LDGCC Phase 13
    ✓ COMPLETE

LDGCC Phase 14
    ✓ COMPLETE

LDGCC Phase 16
    ✓ COMPLETE

LDGCC Phase 17
    ✓ COMPLETE

LDGCC Phase 18
    ✓ COMPLETE

Job Spec Foundation
    ✓ COMPLETE

Dataset Sharding Foundation
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
