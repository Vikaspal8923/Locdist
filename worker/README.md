# LDGCC Worker

This README is the developer-facing deep dive for the `worker/` component.

It is written for two audiences:

- contributors who want to understand how the Worker service behaves internally
- interview/review readers who want to explain the Worker architecture, control
  flow, and design tradeoffs with confidence

The top-level [README.md](../README.md) is the user-facing overview. This file
describes the current Worker codebase as it exists today.

## Overview

The Worker is the machine-side execution agent in LDGCC.

It runs on a Worker laptop and acts as the local service that connects the
Brain/Master to the machine that will actually execute the job. In practice,
the Worker has to do several jobs at once:

- advertise itself on the LAN
- accept and store pairing with one Master
- register itself with that Master
- keep the Master updated through heartbeat and status signals
- receive workspace packages and prepare them locally
- build or reuse the Python environment needed for the training project
- launch and monitor the training process
- bridge runtime gradient synchronization traffic to the Master
- expose results back to the Master after training finishes

The Worker is not the global coordinator. It does not schedule jobs across the
cluster or decide which machines should train. That is the Master’s job.

Instead, the Worker is the local execution-side controller that turns Master
commands into local machine actions safely and consistently.

## Responsibilities

The Worker owns the following responsibilities in the current codebase.
These responsibilities are split across discovery, trust/pairing, local job
execution, runtime bridging, and result handling.

### Local presence and identity

This is the part of the Worker that makes the machine visible and identifiable
on the LAN before a job even exists.

- advertise Worker presence on the LAN
- expose Worker name, host, port, and pairing status
- provide the local app/API surface for the Worker machine

### Pairing and trust

Pairing is how the Worker decides which Master is allowed to control it.

- accept or reject a pending Master pairing request
- persist pairing credentials locally
- allow only one active Master pairing at a time
- reset a pairing cleanly when the user disconnects

### Master communication

Once paired, the Worker becomes a client of the Master for control and runtime
communication.

- register with the Master
- send heartbeat and status information
- notify the Master when going offline
- forward runtime synchronization requests to the Master

### Workspace and setup

The Worker is responsible for turning a transferred job package into a runnable
local workspace.

- receive workspace data
- unpack it safely
- validate required files
- create Worker-local workspace directories
- build or reuse the Python environment for the job
- install runtime and project dependencies
- validate CUDA GPU availability

### Training execution

The Worker owns the local training process lifecycle.

- arm a job
- launch the training process
- set runtime environment variables
- capture logs
- report status
- stop and clean up local process state

### Runtime bridge and results

The Worker is also the local bridge between Python runtime code and the Master.

- expose synchronization RPCs to the runtime
- forward synchronization calls to the Master
- provide result manifest and file download endpoints
- expose benchmark upload support for network checks

## System Position

The Worker sits between the local machine and the rest of the cluster.

In practical terms:

- the Master decides what should happen
- the Worker makes that happen on one machine
- the runtime uses the Worker as its local network-facing bridge

```text
Brain / Studio
    -> Master
    -> Worker control RPCs

Worker service
    -> pairing / status / setup / training / results / runtime bridge

Python runtime
    -> local Worker gRPC server
    -> Worker runtime bridge
    -> Master synchronizer
```

The important architectural rule is:

```text
The Worker is a local execution agent, not a peer-to-peer coordinator.
It talks upward to the Master, not sideways to other Workers.
```

That gives the Worker three major roles:

- machine-local control service
- runtime communication bridge
- local job execution manager

## Request And Training Flows

The Worker has two big classes of flow:

- control flows driven by the local user and by the Master
- runtime flows driven by the training process

Another simple way to think about the Worker is:

- before pairing, it behaves like a discoverable local service
- after pairing, it behaves like a controlled execution agent
- during training, it behaves like both a process manager and a runtime bridge

### 1. Worker start and advertisement flow

When the Worker starts, it becomes a discoverable machine on the LAN and opens
its local server stack.

The flow is:

1. `service.Agent.Start()` creates the runtime bridge and Worker gRPC server
2. the Worker server starts listening
3. if a saved pairing record exists, the Worker tries to reconnect to that
   Master
4. the Worker starts LAN advertisement through `discovery/advertiser.go`
5. the Worker starts background heartbeat behavior

Important idea:

- startup is not just "open a port"
- it is "become discoverable, restore trust state if possible, and prepare to
  receive commands"

### 2. Discovery and pairing flow

Discovery and pairing are different steps.

- discovery means "the machine can be seen"
- pairing means "this Master is allowed to control this Worker"

The pairing flow is:

1. a Master sends a `PairWorkerRequest`
2. `pairing.Manager.Request(...)` validates that request and stores it as
   pending
3. the local Worker user accepts or rejects it
4. if accepted, the pairing record is saved locally
5. `service.Agent.AcceptPairing()` connects to the Master and registers
6. the Worker becomes `PAIRED_ONLINE` or `PAIRED_OFFLINE` depending on whether
   the Master is reachable immediately

Important idea:

- the Worker owner must approve the pairing
- the Worker will not accept a second Master while already paired

That makes pairing the trust boundary for the whole Worker.

### 3. Registration and heartbeat flow

After pairing, the Worker becomes an active cluster participant by registering
with the Master and then sending heartbeat updates.

The flow is:

1. `service.Agent.connect(...)` builds a `masterclient.Client`
2. the Worker calls `RegisterWorker`
3. the Master accepts the Worker if pairing credentials match
4. the Worker keeps sending heartbeat requests
5. status and liveness are maintained through the Master-facing client path

Important idea:

- pairing says the Worker is allowed
- registration says the Worker is connected
- heartbeat says the Worker is still alive now

### 4. Workspace receive and prepare flow

Before a job can run, the Worker has to receive and unpack a job-specific
workspace.

The flow is:

1. the Master uploads a workspace package
2. `grpc.WorkerBridgeServer` receives either a direct workspace request or a
   streamed upload
3. `workspace.Manager` validates job id and safe relative paths
4. the archive is extracted into a Worker-local workspace directory
5. the Worker checks that required files exist:
   - entrypoint
   - `job_config.json`
   - dataset path
6. `logs/` and `artifacts/` directories are created

Important details:

- only safe paths are allowed
- symlink-like unsafe archive entries are rejected
- extracted size limits are enforced
- old unrelated workspaces are removed so the Worker stays in a simple
  one-workspace-at-a-time model

### 5. Setup flow

Setup turns a received workspace into a runnable training environment.

The flow is:

1. the Master calls `SetupJob`
2. `setup.Manager.Setup(...)` ensures the job is not already setting up
3. the Worker checks for an NVIDIA CUDA GPU using `nvidia-smi`
4. project requirements are filtered so LDGCC-managed runtime packages are not
   accidentally overwritten
5. a dependency fingerprint is computed
6. if a matching cached environment exists, it is reused
7. otherwise the Worker creates a virtual environment
8. CUDA PyTorch, runtime requirements, and project requirements are installed
9. a venv marker file is written into the workspace
10. the Worker returns `READY` or `FAILED`

Important idea:

- setup is not just pip install
- it is the Worker’s local "make this job executable on this machine" phase

This is also where the Worker enforces the current GPU-first design of LDGCC.

### 6. Training launch flow

The Worker uses an arm/release execution model that mirrors the Master’s
coordinated launch design.

The flow is:

1. the Master sends `ArmJob`
2. `training.Manager.Arm(...)` validates readiness and required files
3. the Worker reads `job_config.json`
4. the Worker records the entrypoint, log path, and serialized communication /
   training config
5. the Worker enters `ARMED`
6. the Master later sends `ReleaseJob`
7. `training.Manager.Release(...)` creates the actual process
8. the Worker sets runtime environment variables such as:
   - `LDGCC_JOB_ID`
   - `LDGCC_WORKER_ID`
   - `LDGCC_MASTER_HOST`
   - `LDGCC_MASTER_PORT`
   - `LDGCC_COMMUNICATION`
   - `LDGCC_TRAINING`
9. the Python training process starts and logs are written locally
10. the Worker moves into `RUNNING`

Important idea:

- `ArmJob` prepares local process state
- `ReleaseJob` actually starts execution

That split lets the Master coordinate a cleaner multi-Worker launch.

### 7. Runtime synchronization flow

Once training is running, the Worker becomes the local network bridge between
the Python runtime and the Master.

The flow is:

1. the runtime sends a synchronization request to the local Worker gRPC server
2. `grpc.WorkerBridgeServer` receives that request
3. the request is passed to `runtimebridge.Service`
4. the runtime bridge calls the current `Synchronizer`
5. once paired, that synchronizer is a `masterclient.Client`
6. the request is forwarded to the Master
7. the aggregated response comes back through the same path

The Worker supports several sync paths through the synchronizer interface:

- `Synchronize(...)`
- `SynchronizeBatch(...)`
- `SynchronizeBatchStream(...)`
- `SynchronizeChunk(...)`

Important idea:

- the runtime does not talk directly to the Master
- it talks to the local Worker bridge, and the Worker bridge talks to the
  Master

This keeps the runtime API simpler and lets the Worker own network-side control
and authentication behavior.

### 8. Status, stop, and finalize flow

The Worker is responsible for keeping the Master informed about local training
state and for shutting down local execution cleanly.

The status and stop path is:

1. the Master asks for status through `GetJobStatus`
2. the Worker returns the current local process state
3. if the Master sends `StopJob`, the Worker interrupts the process
4. if the process does not stop in time, the Worker kills it
5. the Worker marks the job `CANCELLED`, `FAILED`, or `COMPLETED` depending on
   how the process ends
6. `CleanupJob` removes local tracked setup/training state for that job

Important idea:

- the Worker owns the truth about the local process
- the Master owns the truth about the global job

So status polling and stop/cleanup calls are how those two views stay aligned.

### 9. Result collection flow

After training, the Worker exposes local outputs back to the Master.

The flow is:

1. the Master asks for a result manifest
2. the Worker builds the manifest from local output paths
3. the Worker returns:
   - files
   - missing outputs
   - collection errors
4. the Master downloads selected files through `DownloadResult`
5. the Worker streams file chunks back

This makes the Worker the source of truth for the local artifacts produced by
its training run.

## Internal Components

This section maps the current `worker/` folders to responsibilities in the
running system.

The packages under `worker/` are not arbitrary folders. They roughly follow the
boundaries between local service startup, Master communication, workspace and
environment handling, runtime bridging, and training execution.

### `service/`

Owns top-level Worker process orchestration.
This package is the best place to understand how the Worker starts, reconnects,
advertises itself, and wires its subsystems together.

- `agent.go`: top-level Worker agent; owns start/stop, reconnect, heartbeat,
  pairing reset, runtime bridge setup, and advertisement lifecycle
- `network.go`: network-related Worker service helpers
- tests cover the Agent lifecycle and state transitions

### `app/`

Owns the local app/control layer for the Worker machine.
This package is the user-facing side of the Worker process.

- `controller.go`: app-side control logic for local actions
- `server.go`: HTTP/app server wiring
- tests cover local control behavior

### `grpc/`

Owns the Worker gRPC server surface.
This package is the transport edge seen by both the Master and the local
runtime.

- `server.go`: gRPC server wiring
- `handlers.go`: `WorkerBridge` RPC handlers
- `handlers.go` authenticates Master-issued control requests and dispatches to
  workspace, setup, training, results, and runtime bridge managers

### `runtimebridge/`

Owns the local bridge from runtime requests to the current synchronizer.
This package is the narrow Worker-side abstraction that hides whether the
runtime is currently paired to a real Master client or only has an unavailable
fallback.

- `synchronizer.go`: synchronizer interface definition
- `sync.go`: runtime bridge service implementation
- `unavailable.go`: fallback synchronizer used before Master connection exists

### `masterclient/`

Owns outbound communication from the Worker to the Master.
This package is the Worker’s client-side transport adapter.

- `client.go`: Master gRPC client for register, heartbeat, status updates,
  unpair, and all synchronization calls

This package is intentionally low-level. It should send requests, not decide
job policy.

### `pairing/`

Owns Worker-side trust state with the Master.
This package decides which Master, if any, is currently allowed to control the
Worker.

- `manager.go`: pending pairing request handling, accept/reject, record access,
  reset behavior
- `store.go`: persistent record storage
- tests cover pairing behavior

### `discovery/`

Owns Worker advertisement on the LAN.
This package is how the Worker becomes visible to the Master before pairing.

- `advertiser.go`: LAN advertisement behavior and metadata exposure

### `workspace/`

Owns local job workspace extraction and validation.
This package turns uploaded archives into safe local workspace directories.

- `manager.go`: workspace creation, archive extraction, path safety checks,
  required-file validation, removal, and lookup
- tests cover path safety and extraction behavior

### `setup/`

Owns environment preparation for a job.
This package is where "received workspace" becomes "runnable Python job."

- `manager.go`: CUDA check, dependency filtering, environment caching, venv
  creation, pip install, setup state management
- tests cover setup behavior and retry handling

### `training/`

Owns local process execution for training jobs.
This package is where the Worker becomes a real process manager.

- `manager.go`: arm/release/stop/status/cleanup, process state tracking, log
  path handling, runtime env injection, and process wait logic
- tests cover launch, stop, cleanup, and status behavior

### `status/`

Owns Worker-to-Master status reporting.
This package wraps status update behavior and tracks the last local status
value.

- `manager.go`: set current Worker status and report it to the Master

### `results/`

Owns result manifesting and file access.
This package exposes local outputs back to the Master in a controlled way.

- `manager.go`: manifest building, file lookup, and safe result access
- tests cover result handling behavior

### `metrics/`

Owns Worker-side sync metrics helpers.
This package supports observability for runtime synchronization behavior on the
Worker side.

- `sync.go`: metrics support for synchronization reporting

### `tests/`

Higher-level cross-package coverage:

- handler behavior
- heartbeat behavior
- Master client behavior
- runtime bridge behavior
- setup flow
- training flow
- sync flow
- status flow

These tests matter because many important Worker behaviors only show up once
local managers, gRPC handlers, and Master communication are all wired together.

## Folder Structure

Current `worker/` tree at a high level:

```text
worker/
  app/           local Worker control/API layer
  cmd/           executable entrypoints
  discovery/     LAN advertisement
  generated/     generated protobuf bindings
  grpc/          WorkerBridge gRPC server
  internal/      config and internal helper packages
  masterclient/  outbound Master client
  metrics/       sync metrics helpers
  pairing/       pairing request and record management
  results/       result manifest and file access
  runtimebridge/ runtime-to-Master synchronization bridge
  service/       top-level Worker agent
  setup/         environment preparation
  status/        Worker status reporting
  tests/         higher-level integration-style tests
  training/      local process execution
  workspace/     workspace extraction and validation
```

Files worth knowing for a deep dive:

- `service/agent.go`: best first file for understanding the Worker lifecycle
- `grpc/handlers.go`: best file for understanding the Worker RPC surface
- `runtimebridge/sync.go`: best file for understanding how runtime calls are
  forwarded
- `masterclient/client.go`: best file for understanding Worker-to-Master RPCs
- `workspace/manager.go`: best file for understanding local workspace safety
- `setup/manager.go`: best file for understanding environment creation and
  caching
- `training/manager.go`: best file for understanding process launch and stop
- `pairing/manager.go`: best file for understanding Worker-side trust state

If someone asks "where should I start reading the Worker?", those are the right
files.

## State And Algorithms

This section explains the most important state models and operational algorithms
in the current Worker implementation. This is the part that matters most for
technical interviews, because it explains how the Worker behaves under real job
conditions.

### Connection state model

The Worker tracks a high-level connection state through the service Agent.

Current states include:

- `UNPAIRED`
- `PAIRING_PENDING`
- `PAIRED_ONLINE`
- `PAIRED_OFFLINE`

This model is useful because the Worker has to represent both trust state and
network reachability at the same time.

For example:

- paired + reachable Master -> `PAIRED_ONLINE`
- paired + unreachable Master -> `PAIRED_OFFLINE`

That is better than a single boolean like `connected=true/false`.

### Pairing state model

The pairing manager holds:

- one saved pairing record
- at most one pending pairing request

This means:

- the Worker only allows one controlling Master at a time
- the Worker owner can explicitly approve or reject a new Master
- the Worker can persist trust across restarts

This is a deliberately simple model, but it keeps the trust boundary easy to
understand.

### Workspace preparation algorithm

The workspace manager follows a safety-first extraction path.

At a high level:

1. validate job id and relative paths
2. validate archive size
3. clear unrelated previous workspaces
4. extract into a temporary directory
5. reject unsafe archive entries
6. enforce extracted size limits
7. verify required files exist
8. create standard output directories
9. atomically move the temporary workspace into place

Important details:

- job ids must be safe
- entrypoint and dataset paths must be safe relative paths
- symlink-style unsafe archive entries are rejected
- archive extraction is bounded
- required files are checked before the workspace is accepted

This is important because the Worker is the place where transferred packages
become real local files on disk.

### Setup algorithm

The setup manager turns a workspace into a runnable Python environment.

At a high level:

1. check whether setup is already running or already ready
2. locate the workspace
3. create `logs/setup.log`
4. verify CUDA GPU availability with `nvidia-smi`
5. read and filter user `requirements.txt`
6. build a dependency fingerprint
7. reuse a cached environment if one matches
8. otherwise create a new venv
9. install CUDA PyTorch
10. install LDGCC runtime requirements
11. install filtered project requirements
12. write the venv marker file and mark the cache ready

Important details:

- setup is cache-aware, not always rebuild-from-zero
- user requirements are filtered so they do not overwrite LDGCC’s runtime
  package decisions
- setup is GPU-gated in the current design
- retry behavior is explicit

This makes setup one of the most operationally important Worker subsystems.

### Training execution algorithm

The training manager uses a two-step launch model:

1. arm
2. release

The arm path:

- checks readiness
- verifies required files
- reads `job_config.json`
- records local process metadata

The release path:

- resolves the Python path for the job
- opens the training log
- injects LDGCC runtime environment variables
- launches the Python entrypoint
- tracks the process until completion

Important details:

- one process record is tracked per job id
- stop first tries interrupt, then force kill after timeout
- final status is inferred from process exit behavior
- log output is kept locally for later result collection

This package is where the Worker really becomes an execution agent.

### Runtime bridge algorithm

The runtime bridge is intentionally narrow.

The current model is:

1. runtime submits a sync request locally
2. the bridge forwards that request to the current synchronizer
3. once the Worker is paired and connected, that synchronizer is the Master
   client
4. if no Master connection exists yet, an unavailable synchronizer protects the
   path from pretending sync is possible

This design keeps the runtime path simple while still letting the Worker swap
between disconnected and connected states cleanly.

### Status reporting algorithm

The Worker keeps a local status manager that:

1. sends a status update request to the Master
2. only updates its own local cached state after the remote update succeeds

That is a small but good design choice because it reduces the chance that the
Worker and Master drift apart on what status was actually accepted.

## Failure Handling

Failure handling is a first-class concern in the Worker because it touches the
real machine: local files, local environments, local processes, and network
reachability.

The Worker contains explicit guardrails around several common failure modes.

### Pairing failure

- incomplete pairing requests are rejected
- a second pending pairing request is rejected
- a second Master cannot pair while the Worker is already paired
- reset is blocked while a pairing request is still pending

### Master connectivity failure

- reconnect can fail while the Worker still remains paired locally
- this is represented as `PAIRED_OFFLINE`
- the runtime bridge can fall back to an unavailable synchronizer until the
  Master client is restored

### Unsafe workspace input

- unsafe job ids are rejected
- unsafe relative paths are rejected
- invalid archives are rejected
- oversized archives are rejected
- missing required files are rejected

### Setup failure

- no CUDA GPU -> setup fails
- invalid or missing Python environment creation -> setup fails
- dependency install failures -> setup fails
- failed setup state is tracked and can be retried

### Training failure

- releasing a job that is not armed fails
- starting a job with missing required files fails
- process launch failures are returned immediately
- runtime process failures are captured and converted to final job status
- stop escalation goes from interrupt to kill if needed

### Result handling failure

- result access is gated through the result manager
- manifest generation can return missing outputs and collection errors
- file streaming is explicit instead of exposing arbitrary local file access

## Design Decisions And Tradeoffs

This section explains why the Worker looks the way it does today. The current
design favors local correctness, debuggability, and controlled machine access
over aggressive complexity.

### Worker as a service, not just a training script launcher

Why:

- the machine needs a long-lived identity
- pairing and trust need to survive beyond one job
- runtime sync requires a local RPC bridge
- setup, training, and results are separate phases

Tradeoff:

- more moving parts than a one-shot remote command runner

### One paired Master at a time

Why:

- clearer ownership model
- simpler trust and reset behavior
- less chance of conflicting cluster control

Tradeoff:

- not yet designed for shared multi-Master control

### Local runtime bridge instead of direct runtime-to-Master calls

Why:

- keeps runtime API simpler
- lets the Worker own connection and authentication behavior
- keeps the machine-local control plane in one place

Tradeoff:

- one extra local hop in the sync path

### Cached environment setup

Why:

- repeated installs are expensive
- many benchmark or repeated training runs share the same dependency shape

Tradeoff:

- caching logic is more complex than always building from scratch

### GPU-only setup gate

Why:

- current LDGCC target is NVIDIA CUDA training
- failing early is better than silently running on an unsupported setup

Tradeoff:

- less flexible than a broader CPU/GPU compatibility story

## Transition: From Early V1 To Current Worker

This section explains how the Worker evolved over time. It is useful both for
understanding the current codebase and for explaining the project in an
interview without pretending the Worker started as a fully formed execution
agent.

### Early V1 shape

The early Worker was much closer to a narrow bridge and connectivity milestone:

- runtime bridge experiments
- basic Worker communication
- early discovery and pairing ideas
- simpler execution expectations

At that stage, the Worker was more about proving the communication path than
about owning a full local job lifecycle.

### What was added after the early foundation

As the system matured, the Worker grew through these major additions:

- stronger pairing and persistence logic:
  the Worker gained a clearer trust model and local storage of pairing records

- better Worker app and control surface:
  the machine-side service became easier to operate and integrate with UI flows

- workspace delivery and extraction:
  the Worker moved from only being a bridge to becoming the place where job
  packages are received and materialized

- setup as a real lifecycle phase:
  environment creation, dependency install, and readiness became explicit

- training execution lifecycle:
  arm, release, stop, status, and cleanup became structured local operations

- result collection:
  the Worker gained manifest and file-download support for returning outputs

- image-folder and later YOLO-oriented dataset handling on the broader system
  side:
  this made the Worker’s workspace/setup/training responsibilities more
  important because the local file layouts became richer

- compression-aware runtime synchronization:
  the Worker had to support more advanced sync paths than a single dense RPC

- chunk, group, and batch sync support:
  the runtime bridge and Master client path expanded to support newer
  synchronization shapes

- metrics and timing support:
  the Worker gained more observability for sync behavior and local execution

- cached environments and GPU enforcement:
  setup became more practical for repeated runs and more explicit about what
  hardware is required

### What the current Worker is now

Today, the Worker should be thought of as a machine-local execution service,
not just a runtime proxy.

It now owns:

- local trust state
- Master connectivity
- workspace materialization
- environment setup
- process execution
- runtime bridging
- result exposure

That is the right way to talk about the transition in an interview:

```text
The Worker started as a communication-side component and evolved into a full
local execution agent that manages the machine-side lifecycle of a distributed
training job.
```

### What is still intentionally simple

Even after this evolution, some choices remain conservative on purpose:

- one paired Master at a time
- one local execution service per machine
- simple cached environment model
- Master-driven control flow instead of Worker-to-Worker coordination

Those choices keep the Worker understandable and operationally manageable for a
local distributed training project.

## Current Limits And Future Work

The current Worker works well for the local-first LDGCC model, but there are
clear next steps if the design grows further:

- richer local observability for setup and training phases
- stronger recovery across Worker restarts
- broader hardware/runtime support
- more advanced environment reuse and cleanup policies
- tighter sync-path optimization
- clearer separation between app-facing Worker UX and machine-facing execution
  logic

In other words, the Worker is already a real local execution agent, but the
next phase would make it more robust, more flexible, and easier to operate at
scale.
