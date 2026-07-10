# LDGCC Runtime

This README is the developer-facing deep dive for the `runtime/` component.

It is written for two audiences:

- contributors who want to understand how the runtime works inside the Python
  training process
- interview/review readers who want to explain the runtime design, gradient
  flow, compression behavior, and correctness tradeoffs clearly

The top-level [README.md](../README.md) is the user-facing overview. This file
describes the current runtime codebase as it exists today.

## Overview

The runtime is the Python-side part of LDGCC that runs inside the user's
training process.

Its job is to make distributed gradient synchronization feel as close as
possible to normal PyTorch training code while still supporting:

- model gradient extraction
- optional gradient compression
- serialization to the shared protobuf wire format
- synchronization through the Worker/Master path
- reapplication of aggregated gradients back into the local model
- newer prepared overlap paths that move some synchronization work closer to
  backward-hook timing

At the simplest API level, the user sees something like this:

```python
import locdist

for batch in dataloader:
    optimizer.zero_grad()
    loss = model(batch).loss
    loss.backward()
    locdist.sync_gradients(model)
    optimizer.step()
```

In the newer prepared path, the runtime can also wrap the model and optimizer:

```python
model = locdist.prepare(model)
optimizer = locdist.prepare_optimizer(optimizer)
```

That second path matters because the runtime is no longer just a "pull all
gradients at the end and sync them" layer. It now has logic for hook-driven
capture, grouped async sending, and accumulation-aware synchronization.

## Responsibilities

The runtime owns the following responsibilities in the current codebase.
These responsibilities sit at the boundary between training math, gradient
representation, transport, and runtime-side orchestration.

### Runtime API surface

This is the user-facing Python API.

- `locdist.sync_gradients(model)`
- `locdist.prepare(model)`
- `locdist.prepare_optimizer(optimizer)`

These calls are intentionally small, but they trigger a large amount of runtime
behavior underneath.

### Gradient extraction and application

The runtime is responsible for:

- reading model gradients from PyTorch parameters
- converting them into runtime chunk objects
- reapplying aggregated gradients back into the model

This is the part of the system that directly touches gradient tensors and must
therefore be careful about shape, dtype, sparse encoding, and device placement.

### Compression and payload shaping

The runtime owns the logic that reduces or reshapes what gets sent over the
network.

That includes:

- dense fp32/fp16 payload selection
- top-k compression
- per-layer vs global selection mode
- sampled-threshold selection
- error-feedback residual handling
- sparse index packing

This is one of the most important parts of the runtime because it changes the
actual gradient payload that leaves the process.

### Prepared overlap path

The runtime owns the newer prepare-based synchronization path.

That includes:

- registering backward hooks
- tracking ready layers
- grouping ready chunks
- accumulation-aware behavior
- sending grouped chunks in the background
- wrapping `optimizer.step()` so the runtime can wait for and finalize the
  correct round before the local step is applied

### Wire conversion and transport

The runtime also owns:

- conversion between runtime Python models and protobuf messages
- connection setup for Worker/Master communication
- timing and transport metrics collection

## System Position

The runtime sits inside the user’s Python training process and is the closest
LDGCC component to the model itself.

In practical terms:

- the Master is the cluster coordinator
- the Worker is the machine-local execution and networking service
- the runtime is the training-process-side gradient synchronization layer

```text
train.py
    -> runtime API
    -> gradient extraction / prepare hooks / compression
    -> wire conversion
    -> transport client
    -> Worker or Master RPC target
```

The most important architectural rule is:

```text
The runtime owns local gradient semantics.
It does not own cluster scheduling or global orchestration.
```

That gives the runtime three major roles:

- gradient transformation layer
- process-local synchronization coordinator
- transport client for distributed gradient exchange

## Request And Training Flows

The runtime has two broad operating modes:

- fallback or simple sync mode through `sync_gradients(model)`
- prepared overlap mode through `prepare(model)` and `prepare_optimizer(...)`

Another simple way to think about it is:

- before backward finishes, the runtime may be watching gradients become ready
- between backward and step, the runtime may be synchronizing them
- before optimizer step completes, the runtime must ensure the correct round has
  been applied

### 1. Simple sync flow

This is the classic runtime path and still exists as a valid path in the
current codebase.

The flow is:

1. `sync_gradients(model)` is called
2. runtime config is loaded
3. the current transport client is resolved
4. gradients are extracted and compressed into runtime chunks
5. a `GradientPackage` is built
6. the package is converted to protobuf
7. the transport client sends it over RPC
8. the aggregated response is converted back into runtime objects
9. aggregated chunks are applied back to the model
10. timing and compression metrics are recorded

This path is conceptually simple and easy to explain, but it does more work at
one explicit sync point.

### 2. Prepared model flow

The prepared path is the newer runtime mode and is the one tied to overlap and
accumulation-aware behavior.

The flow is:

1. `locdist.prepare(model)` is called
2. the runtime walks `model.named_parameters()`
3. each parameter is assigned layer metadata
4. backward hooks are registered on trainable parameters
5. expected layer identities and expected group identities are built
6. a `PreparedRuntimeState` is attached to the model
7. runtime communication and training settings are copied into that state when
   available

After this, the runtime no longer has to wait for a later one-shot extraction
path for everything. It can react as gradients become ready.

### 3. Prepared optimizer flow

The optimizer wrapper is the second half of the prepared path.

The flow is:

1. `locdist.prepare_optimizer(optimizer)` is called
2. the runtime verifies that `prepare(model)` already happened
3. the optimizer is wrapped in a `PreparedOptimizer`
4. the wrapper controls what happens when `optimizer.step()` is called

Important idea:

- the model wrapper captures gradient readiness
- the optimizer wrapper decides when synchronization must be finalized before
  the local step is allowed to complete

### 4. Backward-hook readiness flow

In the prepared path, each trainable parameter hook can call
`record_ready_layer(...)`.

That flow is:

1. a parameter gradient becomes ready during backward
2. the runtime records the layer identity
3. the runtime determines whether this backward pass is the final accumulation
   microstep or not
4. if this is not the final accumulation microstep, the hook can return without
   emitting a sync chunk yet
5. otherwise the gradient is compressed immediately into a runtime chunk
6. the chunk is added to the current round
7. if the corresponding group is now ready, that group is queued for sending
8. the background sender thread is started if needed

This is where the runtime moves from "sync after backward" toward "begin
coordinating communication as gradients become available."

### 5. Accumulation-aware capture flow

If gradient accumulation is configured, the runtime tracks microsteps.

The important rule is:

- do not treat every backward hook as a full synchronization round
- only treat the final accumulation microstep as the one that should trigger
  send/finalize behavior

If hook-driven capture does not already own the active round at step time, the
runtime also has a fallback path through `capture_accumulated_gradients(...)`.
That path gathers parameter grads directly at optimizer-step time and builds
the round there.

### 6. Sender loop and grouped batch flow

In the prepared path, a background sender thread can batch ready groups and
send them through `synchronize_chunk_batch(...)`.

The flow is:

1. ready groups are added to an outgoing queue
2. the sender thread wakes when work is available
3. it builds a batch of groups for the same round
4. batch size is limited by:
   - group count
   - bytes per batch
   - a small flush deadline
5. the batch is sent through the transport client
6. returned groups are stored back into runtime round state
7. the runtime marks returned layers/groups as available for final application

This is a more advanced path than the original dense sync flow, but it still
keeps the same correctness goal: the runtime must not locally step the model
until the round it depends on has been completed and applied.

### 7. Finalize-before-step flow

When the wrapped optimizer step runs, the runtime must ensure the active round
has been completed before the underlying optimizer performs the local update.

At a high level:

1. note that step-side synchronization is starting
2. if needed, finalize queued chunks for the active round
3. wait for all required returned chunks/groups for that round
4. apply aggregated gradients back into the model
5. emit round metrics
6. allow the wrapped optimizer to do the actual local parameter step
7. clean up round-local state for the next step

This is one of the key correctness boundaries in the runtime.

## Internal Components

This section maps the current `runtime/` files and packages to responsibilities
in the running system.

### `locdist/api.py`

Owns the runtime API surface.
This is the main entrypoint for users and for code readers trying to understand
what the runtime exposes.

- lazy config loading
- `sync_gradients(model)`
- `prepare(model)`
- `prepare_optimizer(optimizer)`
- global compression/runtime state

### `locdist/prepare.py`

Owns the prepared overlap path.
This is one of the most important runtime files because it contains the logic
for hooks, active rounds, grouped sending, accumulation-aware behavior, sender
thread coordination, step-time waiting, and round metric emission.

Key responsibilities include:

- model hook registration
- `PreparedRuntimeState`
- `PreparedOptimizer`
- ready-layer tracking
- grouped batch queueing
- sender thread lifecycle
- fallback sync handling
- finalize/apply/wait behavior

### `locdist/compression.py`

Owns payload shaping and compression behavior.
This file is where the runtime decides whether to send dense gradients or
compressed sparse gradients and how to build those sparse payloads.

Key responsibilities include:

- dense extraction
- top-k extraction
- per-layer and global compression modes
- sampled-threshold selection
- residual / error-feedback handling
- sparse chunk construction

### `locdist/gradients.py`

Owns gradient extraction and gradient application.
This is where runtime chunk objects are translated to and from actual model
gradient tensors.

Key responsibilities include:

- dense extraction from model parameters
- sparse chunk application
- shape and size validation
- dtype restoration
- safe zero-gradient reconstruction

### `locdist/transport.py`

Owns outbound RPC transport.
This file turns runtime packages into network calls and records timing and
payload metrics around those calls.

Key responsibilities include:

- choose Worker-target or Master-target sync address
- gRPC channel creation
- dense, chunk, batch, and stream synchronization calls
- transport timing metrics
- transfer-size metrics

### `locdist/wire.py`

Owns conversion between runtime Python objects and protobuf objects.
This file matters because the runtime and the Go services do not share Python
objects; they only share protobuf messages.

Key responsibilities include:

- runtime package -> protobuf submission
- protobuf response -> runtime package
- grouped payload conversion
- sparse index unpack/pack bridging

### `locdist/config.py`

Owns runtime config loading and validation.
This file defines how the runtime receives worker-injected values and validates
communication/training settings.

### `locdist/models.py`

Owns runtime-side data structures.
This file defines:

- `RuntimeConfig`
- `CommunicationConfig`
- metadata and chunk objects
- package and aggregated-package objects

### Supporting runtime files

- `metadata.py`: parameter metadata helpers
- `metrics.py`: metric emission and timing helpers
- `tensor_bytes.py`: tensor serialization helpers
- `indices.py`: packed sparse-index helpers
- `exceptions.py`: runtime error types

### `tests/`

The tests are especially important in the runtime because this component mixes
math semantics, wire semantics, and synchronization semantics.

There are dedicated tests for:

- compression
- config
- gradient application
- metadata
- prepare path behavior
- transport behavior
- wire conversion
- runtime/Worker integration

## Folder Structure

Current `runtime/` tree at a high level:

```text
runtime/
  locdist/        runtime package
  tests/          behavior and integration tests
  pyproject.toml  packaging metadata
```

Important files for a deep dive:

- `locdist/api.py`: best first file for the public runtime surface
- `locdist/prepare.py`: best file for the overlap/prepared path
- `locdist/compression.py`: best file for payload reduction logic
- `locdist/gradients.py`: best file for tensor extraction/application semantics
- `locdist/transport.py`: best file for network-side runtime behavior
- `locdist/wire.py`: best file for runtime <-> protobuf conversion
- `locdist/models.py`: best file for the runtime’s core data model

If someone asks "where should I start reading the runtime?", those are the
right files.

## State And Algorithms

This section explains the most important state models and algorithms in the
runtime. This is the part that matters most for technical interviews, because
the runtime is where synchronization semantics meet actual gradient math.

### Runtime config model

The runtime config is the set of values the training process needs in order to
behave as one Worker in a distributed job.

That includes:

- job id
- worker id
- worker host/port
- optional Master host/port
- sync target
- communication settings
- gradient accumulation steps

Important idea:

- the runtime does not discover these values itself
- they are injected into the environment or provided through config

### Communication config model

The communication config tells the runtime how gradients should be represented
when they leave the local process.

The main fields are:

- `precision`
- `compression_type`
- `compression_mode`
- `top_k_percent`
- `selection`
- `sample_rate_percent`
- `max_payload_factor`
- `device`
- `error_feedback`
- `warmup_steps`
- `estimated_link_mbps`

This config does not directly change the optimizer math. It changes the
representation and synchronization behavior of the gradient payload.

### Compression state model

The runtime keeps a `CompressionState` that holds:

- current sync step
- residual tensors for error feedback
- metrics from the last compression path

The residuals matter because when top-k compression sends only part of a
gradient, the unsent part is not thrown away. It is stored and added into later
effective gradients.

That is an important approximation-quality detail. It is not the same as
simply dropping the omitted values forever.

More carefully stated:

- the runtime does not guarantee that one compressed step is identical to one
  dense synchronization step
- it does preserve unsent residual information across later steps through the
  residual buffer
- that makes the compression path a structured approximation, not an arbitrary
  truncation

### Dense extraction algorithm

If compression is disabled, or if the current step is still inside warmup, the
runtime follows a dense path.

That means:

1. flatten the parameter gradient
2. cast it to the chosen payload precision
3. serialize it into bytes
4. package it with metadata

This path is simple and preserves all gradient values, but it also sends the
largest payload.

To be precise:

- all local gradient values are represented in the transmitted payload
- the payload precision may still be `fp16` instead of `fp32`, so "dense" here
  means dense representation, not necessarily original full-precision bytes

### Top-k compression algorithm

If top-k compression is enabled and warmup is finished, the runtime follows a
sparse path.

At a high level:

1. build an effective gradient
   - current gradient
   - plus residual from prior unsent values if present
2. choose indices using either:
   - exact top-k
   - sampled-threshold selection
3. extract the values at those indices
4. zero those selected positions out of a copy to produce the next residual
5. store that residual for the next sync step
6. build a sparse gradient chunk with values, indices, encoding, and metadata

Important idea:

- the runtime is not changing the local gradient tensor in-place to the sparse
  form for the optimizer
- it is changing the transmitted representation of the synchronization payload

That distinction matters when explaining the math.

More carefully stated:

- top-k here is a communication-side sparsification method
- the runtime chooses which gradient coordinates to transmit for synchronization
- the unsent coordinates are kept in residual form for later effective-gradient
  construction
- the runtime later reconstructs a sparse aggregated gradient tensor locally
  from the returned payload

So the approximation is in what is transmitted during synchronization, not in a
direct permanent overwrite of the local gradient object before sync logic runs.

### Per-layer vs global top-k

The runtime supports two compression scopes.

#### Per-layer top-k

Each parameter tensor chooses its own strongest values independently.

This means:

- every tensor gets its own top-k selection
- selection is easier to reason about per tensor
- payload is spread across layers more evenly

In more exact terms:

- if `top_k_percent = p`, each tensor aims for roughly `p%` of its own elements
- small tensors still get at least one selected element because the code uses a
  ceil-and-minimum rule for target count
- this keeps every layer participating, but it can also reserve payload for
  layers whose gradients are weaker globally

#### Global top-k

All gradients are conceptually flattened into one larger vector for selection.

This means:

- the strongest values can come disproportionately from some layers
- the payload budget is used more globally
- the runtime has to map selected global positions back to per-parameter chunks

This is one place where the runtime behavior is more mathematically involved,
so the code must be described carefully and only in terms it actually
implements.

In more exact terms:

- the runtime concatenates effective gradients across parameters
- selection happens on that concatenated vector
- selected positions are then mapped back into per-parameter sparse chunks
- if tensors are on mixed devices, the runtime falls back to a CPU-side path
  for that global selection logic

### Sampled-threshold selection

The runtime also supports `selection="sampled_threshold"`.

This path is used to reduce the cost of exact selection.

At a high level:

1. sample a subset of values
2. estimate a threshold from that sample
3. select values above that threshold
4. allow some controlled overshoot through `max_payload_factor`
5. fall back when needed

This is not identical to exact top-k. It is a speed-oriented approximation
strategy whose behavior is bounded by the config fields and fallback logic in
the code.

More carefully stated:

- the runtime samples absolute gradient values
- it estimates a threshold from that sample
- it then selects all full-gradient coordinates whose absolute value meets that
  threshold
- if the selection is clearly too small, empty, or too large, the code falls
  back to a safer exact-style top-k path

So this mode is an approximation of exact selection, but not an unbounded one.
The runtime has explicit lower/upper guardrails through:

- `top_k_percent`
- `sample_rate_percent`
- `max_payload_factor`
- fallback conditions in the selector logic

### Sparse gradient application algorithm

When the runtime receives sparse aggregated chunks back, it must rebuild the
local gradient tensors safely.

The sparse apply flow is:

1. validate metadata
2. validate byte length and expected dtype size
3. unpack sparse indices
4. rebuild a zero tensor on the local parameter device/dtype
5. place returned values at the returned sparse indices
6. assign that reconstructed tensor as `parameter.grad`

Important details:

- duplicate indices are rejected
- out-of-bounds indices are rejected
- size mismatches are rejected
- dense and sparse payloads are handled through different validation paths

More carefully stated:

- the runtime does not "recover" the original dense pre-compression gradient
  from one sparse payload
- it reconstructs the aggregated sparse payload that the synchronization path
  returned for that round
- this reconstructed tensor is then used as the gradient that the local
  optimizer sees for that synchronized step

### Prepared runtime state algorithm

`PreparedRuntimeState` is the main state machine for the newer overlap path.

It keeps:

- expected layers
- expected groups
- communication config
- gradient accumulation settings
- round ids
- ready layers
- returned layers/groups
- queued outgoing chunks
- in-flight group tracking
- sender thread state
- round-local metrics
- fallback sync tracking

This is the heart of the prepared path. It is what lets the runtime coordinate
multiple phases of one training step without losing track of what is ready,
what has been sent, what has returned, and what has already been applied.

### Prepared optimizer algorithm

The prepared optimizer wrapper is important because it turns synchronization
from "just an explicit API call" into "a condition of completing the local
optimizer step."

At a high level:

1. when `step()` is called, note that optimizer-side sync is starting
2. if needed, capture accumulated gradients into a round
3. complete the active round
4. only then call the wrapped optimizer’s real `step()`
5. after step, clean up round-local state for the next iteration

That design is what lets the runtime combine hook-driven readiness with a clear
"do not locally step before distributed sync is done" rule.

## Failure Handling

Failure handling is especially important in the runtime because small mistakes
here can corrupt synchronization semantics silently.

The runtime contains explicit guardrails around several common failure modes.

### Invalid config

- missing required fields
- invalid ports
- invalid communication config values
- invalid selection mode
- invalid payload factor

These are rejected early through config validation.

### Gradient metadata mismatch

During apply, the runtime checks:

- parameter count
- parameter name
- metadata shape
- metadata numel

This matters because applying the wrong aggregated gradient to the wrong local
parameter would silently corrupt training.

### Dense payload mismatch

The runtime rejects dense payloads when:

- `byte_size` does not match actual data length
- data length does not match expected dtype size times element count

### Sparse payload mismatch

The runtime rejects sparse payloads when:

- there is no sparse data when data is required
- index/value counts do not match
- indices are duplicated
- indices are out of bounds
- declared byte size is inconsistent

### Synchronization and transport failure

- gRPC errors are wrapped as synchronization errors
- connection setup errors are surfaced clearly
- prepared rounds can be marked failed or timeout
- shutdown can abort an active prepared round

### Stale or inconsistent prepared state

The prepared path includes explicit round state cleanup and error recording so
that broken or timed-out rounds do not continue pretending to be valid.

## Design Decisions And Tradeoffs

This section explains why the runtime looks the way it does today. The current
design tries to balance correctness, usability, payload reduction, and overlap
opportunity without hiding too much complexity under unsafe assumptions.

### Keep the user API small

Why:

- users should not have to learn a huge distributed API
- most of the complexity should stay inside the runtime

Tradeoff:

- the internals become much more sophisticated than the public surface suggests

### Separate simple sync and prepared sync paths

Why:

- simple sync is easier to reason about
- prepared sync enables overlap and finer control

Tradeoff:

- two conceptual runtime modes must now remain consistent with each other

### Represent payload math explicitly in runtime objects

Why:

- compression, sparse encoding, and metadata need explicit structures
- wire conversion should not depend on raw tensors alone

Tradeoff:

- more bookkeeping than "just send tensors"

### Error feedback for compressed gradients

Why:

- top-k compression should not permanently drop omitted gradient mass
- residual accumulation gives a more stable approximation path

Tradeoff:

- extra state must be carried across sync steps

### Worker-target vs Master-target transport

Why:

- the runtime originally talks through the local Worker path
- newer paths can target the Master directly when configured

Tradeoff:

- transport logic becomes more flexible, but also a bit more complex

## Transition: From Early V1 To Current Runtime

This section explains how the runtime evolved over time. It is useful both for
understanding the current codebase and for explaining the project in an
interview without oversimplifying the math and synchronization story.

### Early V1 shape

The early runtime was closer to:

- extract gradients
- serialize them
- send them
- wait for aggregated response
- apply them

That was enough to prove the basic distributed synchronization loop, but it was
structurally simpler and less performance-aware.

### What was added after the early foundation

As the runtime matured, it grew through these major additions:

- protobuf and wire conversion layers:
  the runtime gained a proper shared contract with the Go services

- stronger config loading and validation:
  runtime behavior became driven by explicit communication settings

- compression-aware gradient extraction:
  the runtime moved beyond dense-only exchange and gained fp16/fp32 and top-k
  payload shaping

- sparse index packing and sparse response support:
  sparse gradient payloads became first-class runtime objects instead of an
  ad hoc idea

- image and richer training examples on the broader system side:
  this increased the practical importance of runtime performance and correctness

- prepare/model hook pipeline:
  the runtime gained a newer path for tracking gradients as they become ready
  rather than waiting for only one late extraction point

- grouped async sending:
  the runtime gained background grouped batch sending behavior

- deadlock-safe chunk batch synchronization:
  the runtime and the surrounding system became safer around partial or blocked
  synchronization states

- per-layer chunk metadata:
  the runtime gained a richer notion of layer identity and round-structured
  chunk flow

- timing instrumentation:
  the runtime gained detailed timing metrics for extraction, transport, apply,
  and prepared-round behavior

- accumulation-aware synchronization behavior:
  the runtime gained better control over how gradient accumulation interacts
  with synchronization and prepared-step logic

### What the current runtime is now

Today, the runtime should be thought of as a local synchronization engine, not
just a serializer.

It now owns:

- gradient extraction
- gradient compression
- sparse/dense payload construction
- hook-driven prepared-state tracking
- grouped async sending
- transport calls
- aggregated gradient application
- timing and compression metrics

That is the right way to describe the transition in an interview:

```text
The runtime started as a straightforward gradient exchange layer and evolved
into a more capable synchronization engine with compression, prepared overlap
hooks, and accumulation-aware step coordination.
```

### What is still intentionally conservative

Even after these improvements, some choices remain intentionally conservative:

- the runtime still keeps an explicit barrier-based sync model
- the user API is still small
- correctness checks are favored over silent best-effort behavior
- the runtime still relies on the larger Worker/Master architecture for cluster
  control

Those are good tradeoffs for a local distributed training system where
debuggability matters.

## Current Limits And Future Work

The current runtime is already capable, but there are clear next steps if the
design continues to evolve:

- more overlap without weakening correctness guarantees
- tighter compression-performance tuning
- stronger analysis of quality tradeoffs under compression
- clearer user-facing diagnostics for prepared sync behavior
- broader optimization of apply and reconstruction costs
- more observability around round-level overlap and wait behavior

In other words, the runtime is already much more than a V1 prototype, but the
next phase would make it faster, more observable, and easier to tune safely.

## Interview Questions This README Should Help You Answer

After reading this file, you should be able to answer:

- What exactly does `locdist.sync_gradients(model)` do?
- Why is the prepared path different from the simple sync path?
- How does top-k compression work at a high level in this runtime?
- What is the difference between per-layer and global top-k?
- What is error feedback and why is it used here?
- How does the runtime make sure the correct aggregated gradient is applied
  back to the right parameter?
- Why does the runtime wrap the optimizer in the prepared path?
- How did the runtime evolve from a simple sync layer into a more capable
  synchronization engine?
