# LDGCC Master

This README is the developer-facing deep dive for the `master/` component.

It is written for two audiences:

- contributors who want to understand how the Master works internally
- interview/review readers who want to understand the design choices, control
  flow, and tradeoffs behind the Master service

The top-level [README.md](../README.md) is the user-facing overview. This file
describes the current Master codebase as it exists today.

## Overview

The Master is the control-plane center of LDGCC.

It runs on the Brain laptop and is responsible for coordinating the entire
lifecycle of a distributed training job:

- discovering and pairing Workers
- tracking Worker state and availability
- validating the project spec
- selecting Workers for a job
- sharding the dataset
- preparing per-worker job state
- coordinating workspace delivery, setup, and training
- synchronizing and aggregating gradients
- collecting outputs and final job results

The Master is intentionally centralized. Workers do not communicate directly
with each other, and the Python runtime does not talk directly to the
aggregator. That makes the current LAN-first design easier to reason about,
easier to debug, and easier to expose through the Studio UI.

## Responsibilities

The Master owns the following responsibilities in the current codebase.
These responsibilities are split across control-plane state, job lifecycle
coordination, and data-plane synchronization. Together, they make the Master
the system component that decides what should happen, when it should happen,
and whether the cluster is still in a valid state.

### Control plane

The control plane is the part of the Master that manages identity, state, and
coordination rather than training data itself. If the UI needs to know which
Workers exist, which ones are paired, or whether a job is active, that answer
comes from the Master control plane.

- Worker discovery state
- pairing lifecycle
- Worker registration and heartbeat validation
- online/stale/offline Worker tracking
- active job ownership

### Job preparation

Job preparation is the stage where a user project is turned into a concrete
distributed execution plan. In this phase, the Master stops thinking in terms
of "a folder on disk" and starts thinking in terms of Workers, shards,
assignments, and runtime settings.

- parse and validate `ldgcc.yaml`
- choose the Workers for a job
- create a new job id
- shard the dataset according to dataset type
- record Worker assignments and shard assignments

### Training orchestration

Training orchestration is the phase control logic around setup, launch,
stopping, cleanup, and recovery. The Master does not run the user's Python
code itself, but it decides when Workers are allowed to move from one lifecycle
phase to the next.

- drive prepare -> setup -> arm -> release -> running -> stop/cleanup flows
- verify that all assigned Workers are ready before training starts
- track per-worker setup state and run state

### Data plane coordination

The data plane is the gradient synchronization path used while training is
already running. This is where the Master stops acting like a deployment
orchestrator and starts acting like a synchronization service.

- accept aggregated gradient requests through gRPC
- apply barrier-style aggregation across Workers
- support whole-gradient, chunked, and grouped/batched synchronization paths

### Results and recovery

Results and recovery cover everything that happens after or around execution:
collecting outputs, recording summaries, reacting to failure, and returning the
system to a reusable state for the next job.

- collect result manifests and files
- archive final summaries
- reset job state after completion or abort
- propagate failure and cancellation state across the system

## System Position

The Master sits between the user-facing Studio, Worker services, and the
runtime traffic coming from training processes.

In practical terms, the Master is the point where all system-wide knowledge is
combined. Studio knows what the user wants to do, Workers know what each
machine is doing locally, and runtimes know what gradients are ready right now.
The Master is the only component that sees enough of the whole picture to
coordinate the cluster safely.

```text
Studio / UI
    -> master/app
    -> master/orchestrator
    -> master/jobs + master/workers

Worker service
    -> master/grpc
    -> master/coordinator
    -> master/aggregator + worker state updates

Python runtime
    -> local Worker service
    -> Master gRPC API
    -> aggregator/coordinator
```

The important architectural rule is:

```text
Workers do not coordinate with each other directly.
All shared synchronization state lives in the Master.
```

That gives the Master three roles at once:

- source of truth for cluster state
- orchestrator for training lifecycle
- barrier point for gradient synchronization

## Request And Training Flows

The Master has two big classes of flow:

- control-plane flows triggered by the Studio/app
- data-plane flows triggered by Worker runtime traffic

These flows matter because the Master is not just a passive server. It moves
jobs through stages, validates preconditions at each stage, and also sits in
the hot path for synchronization once training is underway.

Another useful way to think about the Master is this:

- before training starts, it behaves like an orchestrator
- while training is running, it behaves like a synchronization service
- after training ends, it behaves like a result and cleanup controller

### 1. Discovery and pairing flow

Discovery and pairing answer two different questions. Discovery asks "what
Workers are visible on the LAN right now?" Pairing asks "which of those
Workers has this Master been allowed to control?"

1. `discovery/` finds Workers on the LAN.
2. `app.Controller` exposes them to the UI.
3. `pairing.Service` sends/receives pairing requests.
4. `workers.Manager` persists pairing credentials.
5. paired Workers later authenticate registration and heartbeats using the
   stored Master id and pairing token.

Important idea:

- discovery is about visibility
- pairing is about trust and control

The Master keeps those as separate phases so that seeing a Worker on the
network does not automatically mean the Master is allowed to run jobs on it.

### 2. Worker registration and heartbeat flow

Registration and heartbeats are the path from "paired Worker" to "trusted,
currently-active Worker." This is where the Master turns pairing credentials
into live runtime state.

1. Worker calls `RegisterWorker`.
2. `grpc.MasterServer` forwards to `coordinator.Coordinator`.
3. `workers.Manager.Register()` validates pairing credentials and records the
   Worker as online.
4. Worker sends `Heartbeat`.
5. `workers.Manager.Heartbeat()` updates status, job id, availability, and
   `LastSeen`.
6. periodic sweeps mark Workers stale or remove them entirely.

Important idea:

- pairing proves that the Worker is allowed
- registration proves that the Worker is present
- heartbeat proves that the Worker is still alive

That three-step model gives the Master a cleaner cluster state than treating
every seen machine as immediately usable.

### 3. Prepare job flow

Prepare is the most important pre-training phase in the control path. It is
the point where the Master freezes the current project configuration, selects
actual machines, shards the dataset, and creates the active job record that all
later phases depend on.

1. Studio triggers prepare through `app.Controller`.
2. `orchestrator.Preparer.Prepare(projectRoot)` starts the flow.
3. `project.LoadSpec()` reads and validates `ldgcc.yaml`.
4. `scheduler.SelectOnline()` deterministically chooses online Workers.
5. `shardDataset(...)` dispatches to the dataset-specific sharder:
   - `jsonl`
   - `image_folder`
   - `yolo_split`
6. `jobs.Manager.PrepareJob()` stores:
   - job metadata
   - Worker assignments
   - shard assignments
   - communication settings
   - training settings

At the end of prepare, the Master has a fully defined job plan.

In practice, prepare turns a user project into a distributed execution record.
After this point, the Master knows:

- which Workers are in the job
- which shard each Worker should receive
- which entrypoint should run
- which outputs should come back
- which communication/training settings belong to this run

This is one of the most important boundaries in the design, because later
stages should not need to rediscover this information from scratch.

### 4. Setup flow

Setup takes the job plan created during prepare and turns it into runnable
Worker state. A Worker is not considered ready just because it was selected;
it must receive the workspace, the shard, and complete environment setup.

The setup path actually has two closely related pieces:

- distribution
- Worker setup

The distribution path:

1. the Master builds a Worker-specific workspace package
2. the package includes project files, shard path information, outputs, and
   runtime config inputs
3. `Distributor` uploads that package to the Worker in streamed chunks
4. the Worker returns a prepared workspace path

The setup path:

1. `SetupCoordinator` contacts each assigned Worker
2. the Master marks that Worker as `setting_up`
3. the Worker runs its setup phase remotely
4. the returned setup status is written back into the active job state
5. failed setups can be retried for all failed Workers or for one specific
   Worker

Training cannot start until `jobs.Manager.AllWorkersReady(...)` returns true.

Important idea:

- distribute answers "did the Worker receive the project and shard?"
- setup answers "is the Worker now ready to run the training process?"

That split is useful because transfer problems and environment problems are not
the same thing, and the Master tracks them separately.

### 5. Training start flow

Training start is intentionally guarded. The Master does not allow a job to
enter `running` state until all assigned Workers are ready, online, and able to
accept the coordinated launch commands.

The current training start path in `orchestrator/training.go` is:

1. load current job
2. verify job is `prepared`
3. verify all assigned Workers are ready
4. verify all assigned Workers are still online
5. send `ArmJob` to all Workers
6. if arm succeeds, send `ReleaseJob` to all Workers
7. set job status to `running`

This split arm/release model gives the Master a lightweight coordinated launch
mechanism instead of letting each Worker begin as soon as it receives a start
request.

The practical meaning of the two commands is:

- `ArmJob`: get ready, but do not start actual execution yet
- `ReleaseJob`: start for real now

This makes the launch more coordinated than a single "start" RPC. If one Worker
cannot even arm successfully, the Master can stop early instead of letting some
Workers begin while others never enter the run.

### 6. One gradient synchronization round

This is the core runtime path once distributed training is active. At this
stage the Master is no longer just coordinating phases; it is enforcing a round
boundary so that all participating Workers see a consistent aggregation step.

At runtime, gradient synchronization flows through gRPC:

1. runtime on Worker A sends gradients
2. Worker service forwards to Master
3. `grpc.MasterServer` forwards to `coordinator.Coordinator`
4. coordinator routes the request to `aggregator.Service`
5. aggregator stores the submission for the current round
6. once the expected Worker count is reached, barrier is satisfied
7. gradients are aggregated
8. responses are sent back to each Worker
9. round state advances

The current code supports multiple synchronization shapes:

- full gradient submissions
- single gradient chunks
- chunk batches
- streamed batch responses
- grouped/per-layer metadata for more structured synchronization

### 7. Stop, cleanup, and results flow

Stopping and cleanup are not just "nice to have" control actions. In a
distributed system they are part of correctness, because a partially-running
cluster can leave stale job state, stale outputs, or blocked synchronization
waiters behind.

There are really three related flows here:

- stop/cancel
- monitor/finalize
- result collection

The stop/cancel flow:

1. the Master sends `StopJob` to Workers when the user cancels or when cleanup
   is needed after launch failure
2. the job status is moved toward `cancelled` or `failed`

The monitoring/finalize flow:

1. `LifecycleCoordinator.Monitor(...)` polls Worker job status
2. if a Worker disconnects, fails, times out, or returns a bad final state, the
   Master finalizes the job as failed
3. if all Workers complete successfully, the Master finalizes the job as
   finished
4. during finalize, the Master may abort aggregator waiters, stop online
   Workers, collect results, run cleanup commands, and archive the final
   summary

The result collection flow:

1. `ResultCollector` asks each Worker for a result manifest
2. the Master validates file paths and size limits
3. files are downloaded and checksum-verified
4. Master-side sync metrics are copied into the result bundle when present
5. `summary.json` is written for the finished job

There is also an important retry-aware detail:

- for some failed runs, the lifecycle logic can return the job from `running`
  back to `prepared` while keeping a summary, so retry/setup flows still make
  sense without rebuilding everything from zero

If a job fails partway through, the lifecycle code tries to return the system
to a known state as cleanly as it can rather than just dropping the active job
record and hoping the next run starts cleanly.

## Internal Components

This section maps the current `master/` folders to responsibilities in the
running system.

The packages under `master/` are not arbitrary folders. They roughly follow
the system boundaries between transport adapters, control-plane state,
orchestration logic, and synchronization logic.

### `app/`

Owns the app-facing control layer used by Studio.
This is the user-control entrypoint for the Master side of the system.

- `controller.go`: top-level UI/backend controller; coordinates actions like
  pairing, network checks, and job operations
- `controller.go` is also where several cross-subsystem flows are stitched
  together from the Studio side, so it is a good file for understanding the
  operational "verbs" the Master supports
- `server.go`: HTTP/API server wiring for the app layer
- `server.go` defines the loopback-only app server, REST-style endpoints, SSE
  event streaming, authentication checks, and the mapping from UI commands to
  controller actions
- `events.go`: event hub for streaming state changes to the UI
- `events.go` is what allows Studio to react to state transitions without
  polling every subsystem independently
- `api_test.go`: tests for app-facing behavior

`app/` is the control entrypoint for user actions. It should stay thin and
delegate real state transitions to lower-level managers/coordinators.

### `grpc/`

Owns the gRPC surface used by Workers.
This package is the Worker-facing network edge of the Master.

- `server.go`: gRPC server wiring
- `handlers.go`: `WorkerBridge` RPC handlers that forward into the coordinator
- `handlers.go` is intentionally simple: it validates the transport boundary
  shape and hands requests off instead of embedding orchestration logic in RPC
  methods

`grpc/` should not contain business logic. It is the network adapter layer.

### `coordinator/`

Owns cross-component request routing for Worker-originated RPCs.
This package is the central handoff layer between transport and business logic.

- `coordinator.go`: central service that wires together aggregator, worker
  management, job management, and orchestration-facing calls
- `coordinator.go` is especially important for understanding the runtime-facing
  sync path because it validates active job and Worker assignment before
  delegating to the aggregator
- the coordinator is also where Worker lifecycle RPCs like register, heartbeat,
  and unpair are normalized into calls on the underlying managers

This is the bridge between transport handlers and core services.

### `aggregator/`

Owns synchronized gradient aggregation state.
This package is the distributed synchronization core of the Master.

- `service.go`: owns round lifecycle, current round state, barrier checks,
  reset behavior, abort behavior, chunk/group round bookkeeping, and condition
  variable signaling for blocked waiters
- `aggregate.go`: owns the actual aggregation logic once the required Worker
  inputs have been collected
- `chunk.go`: extends synchronization beyond full-gradient submissions into
  chunked and grouped synchronization flows
- `state.go`: defines the state structures used to represent rounds, chunk
  rounds, grouped rounds, and pending/completed synchronization bookkeeping
- `service_test.go`: behavior tests

This is one of the most important subsystems in the Master because it is where
distributed correctness and runtime synchronization pressure meet.

### `discovery/`

Owns Worker discovery state and browsing.
This package answers the question: "what Worker instances can the Brain laptop
currently see?"

- `browser.go`: LAN discovery browser
- `browser.go` is where mDNS/network browsing behavior is implemented
- `registry.go`: in-memory discovered Worker registry
- `registry.go` answers "what has been seen recently?" without yet implying
  authenticated participation
- `service.go`: discovery service orchestration
- `state.go`: discovery state model
- tests validate presence handling and registry updates

### `pairing/`

Owns user-approved Worker pairing.
This package answers the question: "which discovered Workers are allowed to be
controlled by this Master?"

- `service.go`: pairing workflow
- `service.go` owns the approval handshake that turns a discovered Worker into
  a machine this Master is allowed to control
- `network.go`: network-related pairing helpers

Pairing is distinct from discovery: discovery says "this Worker exists";
pairing says "this Master is allowed to control it."

### `workers/`

Owns authenticated Worker state.
This package is the live registry of Workers that have moved past discovery and
pairing into actual runtime participation.

- `manager.go`: register, heartbeat, sweep stale Workers, update status,
  reserve/revoke pairing, and enforce pairing-token authentication
- `state.go`: Worker state model, including availability and current runtime
  status fields
- `store.go`: persistent pairing store used so the Master can remember
  pairings across restarts
- tests cover store and manager behavior

This package is the source of truth for which Workers are currently known,
paired, online, stale, or removed.

### `jobs/`

Owns active job state and archived summaries.
This package is the source of truth for the current distributed job.

- `manager.go`: one-active-job manager; owns prepare/start/update/archive/reset
  behavior and tracks per-worker setup/run state maps
- `state.go`: defines `JobState`, `Summary`, `WorkerAssignment`,
  `ShardAssignment`, `WorkerSetup`, and `WorkerRun`, which together form the
  Master-side distributed job model
- `state.go` also customizes JSON serialization so protobuf enum-backed setup
  and run states are readable in app/API outputs

The design is intentionally simple: one active job at a time, with the last
summary retained after reset.

### `orchestrator/`

Owns job lifecycle coordination.
This package contains most of the system's operational workflow logic.

- `prepare.go`: spec load, Worker selection, shard creation, and initial
  `JobState` construction
- `distribute.go`: workspace/package delivery flow from Brain to Workers
- `setup.go`: Worker-side environment and workspace setup coordination
- `training.go`: arm/release/stop/status/cleanup command fanout across assigned
  Workers
- `lifecycle.go`: lifecycle recovery and transition logic when a job ends,
  fails, or needs cleanup
- `results.go`: manifest retrieval, file download, checksum verification,
  result directory construction, and summary writing
- tests validate lifecycle behavior and failure handling

`orchestrator/` is where most end-to-end operational logic lives.

### `scheduler/`

Owns Worker selection.
This package decides which available Workers become part of a specific job.

- `scheduler.go`: deterministic selection of online Workers
- the selection algorithm is deliberately simple enough to audit mentally, which
  matters for debugging and repeatability

The current algorithm is intentionally conservative: select online Workers,
sort by Worker id, take the first `N`.

### `sharder/`

Owns dataset splitting.
This package is where dataset semantics become distributed-execution semantics.

- `jsonl.go`: line-based sharding for text-style datasets
- `image_folder.go`: class-folder-aware sharding for `ImageFolder`-style image
  datasets
- `yolo_split.go`: image/label paired sharding for YOLO split datasets

Each sharder converts the user dataset into per-worker assignments while
preserving the expected runtime path semantics.

### `packager/`

Owns project packaging before workspace distribution.
This package is responsible for turning a user project into something that can
be shipped to Workers reproducibly.

- `package.go`: package build logic
- `package.go` is responsible for building the transferable project payload
  that Workers receive before setup/training
- tests validate packaging behavior

### `project/`

Owns `ldgcc.yaml` parsing and validation.
This package defines the contract between the training project and the Master.

- `spec.go`: config schema, validation, supported dataset and communication
  fields
- `spec.go` is one of the highest-leverage files in the Master because invalid
  project assumptions are stopped here before they become broken distributed
  jobs
- tests cover supported config shapes

This package is important because it defines the contract between user projects
and the Master.

### `metrics/`

Owns sync-related metrics helpers.
This package supports observability around synchronization behavior.

- `sync.go`: metrics support for synchronization reporting
- this package is small, but it matters because synchronization overhead is one
  of the key things users and developers need to inspect in distributed runs

### `tests/`

Higher-level cross-package integration coverage:
These tests matter because many important Master behaviors only show up when
multiple packages are wired together.

- aggregator behavior
- handlers
- worker paths
- integration scenarios
- these tests act like the closest thing to a system contract for the Master,
  because they verify that the packages behave correctly when composed together

## Folder Structure

Current `master/` tree at a high level:

```text
master/
  aggregator/     gradient round state and aggregation logic
  app/            Studio-facing controller and API/event layer
  cmd/master/     executable entrypoint
  coordinator/    bridges RPC handlers to core services
  discovery/      LAN Worker discovery
  generated/      generated protobuf bindings
  grpc/           gRPC server and WorkerBridge handlers
  internal/       config and internal helper packages
  jobs/           active job state and archived summaries
  metrics/        sync metrics helpers
  orchestrator/   prepare/setup/train/results lifecycle
  packager/       project packaging
  pairing/        pairing workflow
  project/        ldgcc.yaml parsing/validation
  scheduler/      Worker selection
  sharder/        dataset splitting implementations
  tests/          higher-level integration tests
  workers/        authenticated Worker state and pairing store
```

Files worth knowing for a deep dive:

If you only have a short amount of time to study the Master, these files give
the best return because they show the primary state transitions and service
boundaries.

- `app/controller.go`: best first file for understanding control flow
- `app/server.go`: best file for understanding how Studio/API commands are
  exposed and authenticated
- `orchestrator/prepare.go`: where job creation really starts
- `orchestrator/training.go`: how training is coordinated
- `orchestrator/results.go`: where output collection, verification, and summary
  materialization happen
- `jobs/manager.go`: active job state transitions
- `jobs/state.go`: the shape of the persisted/in-memory job and summary model
- `workers/manager.go`: Worker registration/heartbeat logic
- `coordinator/coordinator.go`: runtime-facing validation and aggregator routing
- `aggregator/service.go`: round lifecycle and barrier state
- `grpc/handlers.go`: Worker-facing RPC entrypoints
- `project/spec.go`: project contract and validation rules

If someone asks you "where should I start reading?", those are the right files.

## State And Algorithms

This section explains the most important state machines and algorithms in the
current Master implementation.
This is the section that matters most for technical interviews, because it
explains not only what the Master does, but how it decides and coordinates
state across multiple machines.

### Job state model

The job state model is the Master-side representation of a distributed training
run. It captures not only metadata like job id and entrypoint, but also the
per-Worker progress needed to safely move a job through setup, execution, and
cleanup.

`jobs.Manager` keeps:

- at most one active `currentJob`
- one archived `lastSummary`

The active job contains:

- job id and metadata
- Worker assignments
- shard assignments
- communication settings
- training settings
- per-worker setup state
- per-worker run state

This is simple by design. The Master is currently optimized around one active
job at a time rather than full multi-job scheduling.

### Worker state model

The Worker state model is the Master-side view of the cluster membership. It
separates discovered presence from authenticated participation and tracks which
Workers are healthy enough to be assigned work.

`workers.Manager` keeps:

- `workers`: current authenticated Worker runtime state
- `pairings`: persisted pairing credentials

Worker state is updated by:

- registration
- heartbeat
- explicit status updates
- offline notification
- stale/offline sweeps

This model separates "discovered Worker exists" from "paired/authenticated
Worker is allowed to participate in jobs."

### Worker selection algorithm

The current Worker selection algorithm is intentionally simple and
deterministic. It favors predictability and easy reasoning over dynamic
optimization.

Current selection in `scheduler.SelectOnline(...)`:

1. filter Workers by `AvailabilityOnline`
2. sort by `WorkerID`
3. take the first `required` Workers

Tradeoff:

- good: deterministic and easy to reason about
- weak: not yet network-aware or load-aware

### Dataset sharding strategy

Dataset sharding is format-specific because each dataset type has different
correctness requirements. The Master uses a dispatch layer so that each format
can have its own sharding logic while the overall prepare flow stays the same.

In simple terms, the Master first checks `dataset.type`, then sends the work to
the matching sharder. This keeps the orchestration code clean while still
letting each dataset type use the right algorithm.

The `Preparer` does not implement sharding itself. It delegates to the dataset-
specific sharder through `shardDataset(...)`.

#### `jsonl` sharding

For `jsonl`, the system treats each valid non-empty JSON line as one sample.

The algorithm is:

1. open the dataset file
2. read it line by line
3. skip empty lines
4. validate that every kept line is valid JSON
5. build a clean in-memory sample list
6. split the sample count evenly across selected Workers
7. give one extra sample to the first few Workers if there is a remainder
8. write one shard file per Worker

Important details:

- each Worker gets a contiguous range of samples
- the split is balanced by sample count
- shard metadata records `start`, `end`, and `count`
- prepare fails early if:
  - the dataset is empty
  - a line is invalid JSON
  - there are fewer samples than selected Workers

This is the simplest sharding path because the dataset already has a natural
record boundary: one line equals one sample.

#### `image_folder` sharding

For `image_folder`, the system treats each image file inside a class directory
as one sample.

The algorithm is:

1. scan the dataset root for class directories
2. walk each class directory recursively
3. keep only supported image files
4. group discovered samples by class name
5. sort class names and sample paths so the result is deterministic
6. compute an equal target image count per Worker
7. assign images across Workers in a rotating pattern while respecting the
   target count
8. copy the selected image files into each Worker's shard directory while
   keeping the relative class-folder structure

Important details:

- the shard output is a directory, not a single file
- class folders are preserved, so training code still sees an
  `ImageFolder`-style layout
- balancing is done by total image count
- the rotating assignment pattern helps avoid dumping a whole class onto just
  one Worker by default
- the current implementation expects exact balancing and returns an error if it
  cannot hit the target cleanly

This path is more physical than JSONL sharding because it must copy real image
files and preserve the folder layout the user code expects.

#### `yolo_split` sharding

For `yolo_split`, the system treats one image and its matching label file as
one logical sample.

The expected layout is:

```text
dataset/train/
  images/
  labels/
```

The algorithm is:

1. scan `images/` recursively for image files
2. compute the matching label path under `labels/` using the same relative
   name and a `.txt` extension
3. build a sorted list of image/label pairs
4. split that list evenly across Workers using the same balanced range logic
   used by JSONL sharding
5. copy images and labels into each Worker's shard directory
6. if a label file is missing, create an empty label file in the shard output

Important details:

- the shard output is a directory
- relative image paths are preserved
- relative label paths are preserved
- image-label pairing is based on matching relative names
- prepare fails if:
  - `images/` is missing
  - `labels/` is missing
  - there are no image files
  - there are fewer samples than selected Workers

This matters because object-detection datasets are not just loose files. The
image path and label path must stay aligned for training to work correctly.

#### Why the Master keeps separate sharding logic

The Master does not try to force every dataset through one generic sharder.
That would make the code look simpler, but it would make correctness weaker.

Instead, the current design is:

- one shared prepare flow
- one dataset-specific sharder per supported format

That is a good tradeoff here because it keeps the orchestration path readable
without breaking dataset-specific assumptions.

### Training launch algorithm

The launch algorithm is a coordinated release pattern rather than a single
"start now" command. This reduces the chance that one Worker begins training
while others are still not ready to enter the same phase.

Current launch is two-phase:

1. `ArmJob` on all assigned Workers
2. `ReleaseJob` on all assigned Workers

If arm fails on some Workers:

- already-armed Workers are stopped
- job becomes failed

If release fails:

- all assigned Workers are stopped
- job becomes failed

This is a simple coordination pattern that avoids partial early starts.

### Aggregation algorithm

The aggregation algorithm is barrier-oriented. That means the Master does not
finish a synchronization round just because one Worker has sent gradients. It
waits until the expected set of Workers has submitted data for the same round,
then computes one shared aggregated result and releases that result back to all
participants.

At a high level, the dense whole-gradient path works like this:

1. a Worker submission arrives
2. the Master validates basic request fields
3. the submission is stored in the current round state
4. the Master checks whether the barrier has been reached
5. if not, the request waits
6. if yes, the Master aggregates all submissions for that round
7. the aggregated response is shared with all waiting Workers
8. once all waiting receivers have collected the response, the round state is
   reset and the next round can begin

This is simple to reason about and gives clear round boundaries, which is why
it is a good starting point for correctness.

#### Round state in the Master

The aggregator keeps explicit round state instead of trying to compute
everything from transient RPC calls.

The main state structures are:

- `RoundState`:
  used for whole-gradient submissions; stores the current round number, the
  submissions received so far, the final aggregated response, any error, and
  how many receivers are still waiting to read the result

- `ChunkRoundState`:
  used when a single parameter chunk is synchronized as its own round unit;
  stores the chunk-round key, layer key, per-worker submissions, response, and
  waiter count

- `GroupRoundState`:
  used for grouped chunk synchronization, where several chunks are treated as a
  higher-level grouped payload

This explicit state is important because synchronization is not just "receive
and reply." The Master may need to hold submissions from some Workers while it
waits for others, and it must do that without mixing rounds or returning
partial results.

#### Whole-gradient averaging path

For the whole-gradient path, the Master uses one `GradientSubmission` per
Worker.

The aggregation logic:

1. chooses a reference submission
2. checks that every Worker submitted the same number of chunks
3. checks that the metadata for matching chunks is the same across Workers
4. gathers corresponding chunks from all Workers
5. averages those chunks
6. builds an `AggregatedGradientResponse` for the whole round

Important details:

- the Master sorts Worker ids before using them, which keeps processing
  deterministic
- metadata mismatch is treated as an error because it means Workers are no
  longer synchronizing the same model structure
- no response is returned until the barrier condition is satisfied
- after the last waiting receiver reads the result, the round state is reset

#### Chunk-based synchronization path

The current Master does more than whole-gradient sync. It also supports a
chunk-based path where one logical layer chunk can move through the system as
its own unit.

This path exists because later runtime and compression work needed finer
control over synchronization than "send the entire model gradient every time."

The chunk flow is:

1. each chunk submission is keyed by job id, sync round, layer order, and layer
   name
2. the Master stores per-worker submissions for that exact chunk-round key
3. once submissions from all expected Workers arrive, that chunk is averaged
4. the response is returned as an `AggregatedGradientChunkResponse`
5. completed chunk rounds are tracked so stale chunk submissions can be
   rejected later

Important details:

- duplicate chunk submissions from the same Worker are rejected
- conflicting duplicates are also rejected
- stale chunk rounds are rejected using the `completedChunks` tracking map
- the Master waits separately on each chunk round, not just one global round

This makes the aggregator more flexible, but it also makes state management
more complex than the original dense path.

#### Group and batch synchronization path

The Master also supports grouped chunk batches. In this path, several chunks
can be wrapped into a higher-level group so synchronization can happen in more
structured batches.

At a practical level, this means:

- the request may contain `groups`
- the Master registers those grouped submissions separately from plain chunk
  submissions
- each group round is averaged when all expected Workers have submitted it
- the result can be returned either as a batched response or as a stream of
  chunk/group responses

This matters because later performance-oriented runtime work pushed the Master
beyond one flat synchronization path. Grouping and batching make it possible to
coordinate richer runtime behavior without discarding the barrier model.

#### Error handling and abort behavior in aggregation

Aggregation code has to handle failure carefully because synchronization bugs
often show up as hangs rather than clean crashes.

The current aggregator has explicit behavior for:

- invalid runtime version
- missing job id
- missing worker id
- missing chunks
- metadata mismatch
- duplicate chunk submissions
- stale chunk submissions
- job abort/reset while waiters still exist

The `AbortJob(...)` path is especially important. If a job is aborted while
Workers are blocked waiting on round completion, the aggregator sets round
errors, wakes the waiters, and resets state safely instead of leaving threads
stuck forever.

That is one of the places where the current Master is clearly stronger than an
earlier minimal design.

#### Metrics and timing in aggregation

The dense aggregation path also records timing and size metrics, including:

- total request time
- lock wait time
- previous-round wait time
- submission store time
- barrier wait time
- aggregation time
- response clone time
- bytes received and returned

These metrics are useful because distributed training often feels "slow" for
many possible reasons. Without timing breakdowns, it is hard to tell whether
the problem is network cost, barrier wait, aggregation work, or lock
contention.

#### Why this section matters for the architecture story

The aggregator is one of the clearest examples of how the Master evolved over
time.

Early on, the basic idea was simple:

- receive gradients
- wait for all Workers
- average
- reply

The current design still keeps that simple barrier model, but it now supports:

- dense full-gradient rounds
- chunk rounds
- grouped/batched rounds
- stale-round rejection
- safer abort/reset behavior
- timing instrumentation

So the algorithm did not change from "barrier" to something completely
different. Instead, the same basic coordination idea was extended into a more
capable and more defensive synchronization service.

## Failure Handling

Failure handling is a first-class concern in the Master because this component
owns shared cluster state. If a failure is handled poorly here, the entire
system can become inconsistent even if the underlying Worker or runtime code is
correct.

The Master contains explicit guardrails around several common failure modes.

### Invalid project spec

`project/spec.go` rejects invalid:

- dataset types
- communication config
- compression config
- path escapes
- output duplication
- missing required fields

### No available Workers

`scheduler.SelectOnline(...)` returns an error if the required Worker count is
not available online.

### Duplicate active jobs

Both `jobs.Manager` and `orchestrator.Preparer` reject starting a new active job
while another job is already active.

### Worker authentication failure

Registration, heartbeats, and offline/unpair operations all validate pairing
credentials before mutating state.

### Worker drops during execution

Training start checks that every assigned Worker is still online before launch.
Lifecycle and result logic then handle the consequences of later failures.

### Partial arm/release failures

If some Workers succeed and others fail:

- successful Workers are explicitly stopped
- job status is moved to failed/cancelled as appropriate

### Aggregation abort/reset

`aggregator.Service.AbortJob(...)` propagates an abort error to waiting rounds
and resets round state safely, including chunk/group waiters.

This is important because synchronization bugs in distributed training often
turn into deadlocks if abort paths are not explicit.

## Design Decisions And Tradeoffs

This section explains why the Master looks the way it does today. The current
design is not an accident; it reflects deliberate tradeoffs in favor of
correctness, debuggability, and local-cluster usability.

### Centralized coordination

Why:

- simpler correctness model
- easier debugging
- easier UI/state reporting
- easier pairing/security model

Tradeoff:

- Master can become a bottleneck
- all aggregation traffic funnels through one node

### One active job at a time

Why:

- simpler lifecycle management
- clearer failure handling
- less state complexity in early versions

Tradeoff:

- limited scheduling sophistication
- not yet a general multi-tenant cluster manager

### Deterministic Worker selection

Why:

- easy to reason about
- repeatable
- low implementation complexity

Tradeoff:

- not yet optimized for network quality, hardware class, or observed throughput

### Barrier-based synchronization

Why:

- straightforward distributed correctness
- all Workers see the same aggregation round boundary

Tradeoff:

- slowest Worker influences overall step completion
- synchronization overhead can dominate on small jobs

### Distinct packages for spec, sharding, orchestration, and aggregation

Why:

- keeps responsibilities separate
- makes testing easier
- allows the architecture to evolve subsystem-by-subsystem

Tradeoff:

- more moving pieces
- more wiring code in coordinator/app layers

## Transition: From Early V1 To Current Master

This section explains how the Master evolved over time. It is useful both for
understanding the current codebase and for explaining the project in an
interview without pretending the architecture was perfect from day one.

The current Master did not appear all at once. It evolved in layers.

### Early V1 shape

The early V1 Master was focused on making the core loop work end to end:
discover machines, assign them work, launch training, and coordinate basic
aggregation. It was functional, but structurally simpler.

The early Master implementation focused on the minimum usable cluster loop:

- basic Master service
- Worker discovery
- Worker registration
- heartbeats
- one-job orchestration model
- initial job spec and dataset sharding

At that stage, the system was much closer to:

```text
discover Workers
assign work
start training
aggregate gradients
```

with fewer dataset types, fewer lifecycle guarantees, and simpler runtime
synchronization assumptions.

### What was added after the initial V1 foundation

As the system matured, the Master expanded from a basic coordinator into a
clearer control plane with stronger spec handling, more dataset support,
improved orchestration phases, richer synchronization paths, and better
observability.

From the current code and the phase-style history in the repo, the Master grew
through these major capability additions:

- explicit job spec parsing and validation:
  the system gained a proper project contract through `ldgcc.yaml`, so invalid
  inputs could fail early instead of turning into confusing runtime problems

- dataset sharding as a first-class subsystem:
  sharding became its own piece of the architecture instead of being hidden
  inside prepare logic, which made dataset behavior easier to extend and test

- workspace delivery support:
  the Master moved beyond just selecting Workers and began coordinating how
  code and data are actually delivered to them

- Worker environment setup coordination:
  setup became a real tracked phase, so a Worker is only treated as ready after
  it has received the workspace and prepared its runtime environment

- training execution lifecycle:
  launch, stop, cleanup, and status checking became explicit coordinated
  operations instead of looser remote actions

- recovery and lifecycle cleanup:
  the Master gained better ways to return the system to a usable state after
  failure, cancellation, or partial job completion

- result collection:
  the system gained structured manifest handling, file download, checksum
  verification, and summary writing so finished jobs produce inspectable
  outputs

- application API and Studio integration:
  the Master gained a clearer app-facing control surface with controller logic,
  HTTP endpoints, and event streaming for the Studio experience

- image-folder dataset support:
  the project expanded from text-style datasets into image classification
  folder layouts

- YOLO split dataset support:
  the Master added sharding for paired image/label directory layouts, which
  moved the system closer to object-detection style workloads

- compression-aware communication config:
  the project spec and runtime contract expanded to include communication
  precision, compression type, selection method, sample rate, payload factor,
  and related sync controls

- sparse index and chunk metadata handling:
  the gradient protocol became richer, which made it possible to support more
  structured and compressed synchronization behavior

- chunk batch synchronization:
  the Master moved beyond only whole-gradient sync paths and added chunked and
  batched synchronization flows

- deadlock-safe synchronization fixes:
  later improvements made abort and reset behavior safer, which matters because
  blocked synchronization rounds can otherwise leave the system hanging

- sync metrics and timing instrumentation:
  the system gained metrics files and timing signals so developers can inspect
  overhead, blocking time, and runtime sync behavior

- gradient accumulation alignment and runtime-related control refinements:
  newer updates improved how the Master fits with prepared runtime paths,
  accumulation-aware execution, and later overlap-related behavior

### What the current Master is now

The current Master should be thought of as a LAN-first distributed training
orchestrator, not just a gradient relay. It now owns explicit state and
workflow boundaries for most of the lifecycle around a distributed job.

Today, the Master is more than a "launch jobs and average gradients" service.
It is a structured control plane with:

- explicit Worker authentication and pairing state
- explicit active job state
- multiple dataset sharding implementations
- explicit setup and training orchestration stages
- richer synchronization modes in the aggregator
- result collection and summary archiving
- Studio-facing control and event surfaces

That is the right way to talk about the transition in an interview:

```text
The system started as a minimal local distributed training coordinator.
It evolved into a more complete control plane with stronger orchestration,
dataset handling, runtime synchronization support, and observability.
```

### What is still intentionally simple

Even after these improvements, some design choices remain conservative on
purpose. Those choices keep the system understandable and workable for a local
distributed training project, even if they leave performance and scale on the
table.

Even after this evolution, some parts are still deliberately conservative:

- one active job at a time
- deterministic, not adaptive, Worker selection
- centralized aggregation
- LAN-first topology assumptions

So the current Master is more mature than the earliest V1 phases, but it is
still optimized for a local-first, understandable architecture rather than a
large-scale distributed scheduler.

## Current Limits And Future Work

This section is not a list of weaknesses so much as the most obvious next
design moves. The current Master works, but there are clear places where a
future version could become smarter, more durable, or more scalable.

If the next evolution of the Master continues, the most natural improvements
are:

- smarter Worker selection using network and hardware signals
- stronger persistence of job state across Master restarts
- richer recovery semantics for interrupted jobs
- broader task/dataset support
- lower-overhead aggregation paths
- better observability for prepare/setup/train/result phases
- clearer separation between control-plane API and data-plane sync concerns
- eventual support for more than one active job

In other words, the Master is already a real orchestrator, but the next phase
would make it more adaptive, more observable, and less dependent on one
centralized happy path.
