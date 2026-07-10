# LocDist LDGCC

LocDist LDGCC is a local distributed training system for running one training
job across multiple laptops on the same LAN.

The goal is practical: keep your PyTorch training code close to normal, while
LDGCC handles the distributed system work around it.

With LDGCC, the user mainly writes the model, dataloaders, optimizer, and
training loop. LDGCC handles the rest:

- Worker discovery
- Worker pairing
- project packaging
- dataset sharding
- Worker environment setup
- gradient synchronization
- output and log collection

This README is the user-facing guide for the repo. It is meant for someone who
opens LocDist for the first time and wants to know:

- what the system does
- what is currently supported
- how to install and run it
- how to structure a training project
- how to write `ldgcc.yaml`
- how to use gradient accumulation
- what constraints exist today
- how to contribute

If you want deeper implementation docs, use these developer READMEs:

- [master/README.md](master/README.md) - Master deep dive
- [worker/README.md](worker/README.md) - Worker deep dive
- [runtime/README.md](runtime/README.md) - Runtime deep dive
- [extension/README.md](extension/README.md)
- [packaging/README.md](packaging/README.md)

## What LDGCC Is

LDGCC turns nearby laptops into a small temporary training cluster for local
distributed deep learning.

```text
Brain Laptop
    owns the project and starts the job

Worker Laptops
    receive the packaged project and dataset shard
    run the user's training code

LDGCC Runtime
    runs inside train.py and synchronizes gradients
```

The current design is centralized:

- Workers do not talk directly to each other
- all coordination goes through the Brain laptop

That makes the system simpler to operate on a local LAN and easier to debug
than a peer-to-peer design.

## What LDGCC Handles For You

When you start a distributed job, LDGCC handles:

- discovering Workers on the LAN
- pairing them to one Brain laptop
- reading `ldgcc.yaml`
- selecting the Workers for the job
- sharding the dataset
- packaging the project
- sending each Worker its workspace
- creating a private Worker Python environment
- installing LDGCC runtime dependencies
- optionally installing your `requirements.txt`
- running your training entrypoint on each Worker
- synchronizing gradients
- collecting outputs and logs after training

Your training code still owns:

- model definition
- dataloaders
- optimizer
- learning-rate schedule
- loss function
- gradient accumulation policy
- where to call LDGCC runtime functions

## Current Support

Current support in the repo:

### Brain laptop

- Linux x64 VS Code extension release with bundled Master

### Worker laptops

- Linux x64 Worker package
- Windows x64 Worker package
- mixed Linux + Windows Workers in one job
- NVIDIA CUDA GPU Workers only

### Dataset types

- `jsonl`
- `image_folder`
- `yolo_split`

### Communication

- `fp32` or `fp16` gradient payload precision
- `none` or `topk` compression
- `global` or `per_layer` top-k mode
- `exact` or `sampled_threshold` selection
- optional warmup before compression begins

### Training flow

- normal `loss.backward() -> sync -> optimizer.step()` style integration
- newer prepared runtime path through `locdist.prepare(model)` and
  `locdist.prepare_optimizer(optimizer)`
- optional gradient accumulation
- output collection

## What You Need

Before starting, make sure the basic requirements are in place.

### Network requirements

- all machines on the same LAN
- Workers reachable from the Brain laptop

### Hardware requirements

- Worker laptops with NVIDIA CUDA GPUs
- `nvidia-smi` available on the Worker machines

### Project requirements

Your training project should have:

- a Python training entrypoint such as `train.py`
- a dataset path that stays stable inside the project
- an `ldgcc.yaml` or `ldgcc.yml` file in the project root
- optionally a `requirements.txt`

In practice, this means your project should already run as a normal single
machine training project before you adapt it to LDGCC.

## Install

LDGCC release files are published here:

```text
https://github.com/Vikaspal8923/Locdist/releases
```

Typical release artifacts:

```text
ldgcc-studio.vsix
ldgcc-worker-app-linux-x64.zip
ldgcc-worker-app-windows-x64.zip
INSTALL.md
checksums.txt
manifest.json
```

Normal users usually need:

- `ldgcc-studio.vsix`
- one Worker zip for each Worker laptop OS
- `INSTALL.md`

### Brain laptop install

The Brain laptop is where you open the training project in VS Code.

Install the Studio extension:

```text
VS Code
    -> Extensions
    -> Install from VSIX
    -> choose ldgcc-studio.vsix
```

Then open your training project in VS Code and use the LDGCC view.

### Linux Worker install

```bash
unzip ldgcc-worker-app-linux-x64.zip
cd ldgcc-worker-app
./install-worker-app.sh
```

Then open `LDGCC Worker`.

### Windows Worker install

```text
Extract ldgcc-worker-app-windows-x64.zip
Double click ldgcc-worker-app\install-worker-app.bat
```

Then open `LDGCC Worker`.

## Full User Flow

The normal user flow is:

1. open the training project on the Brain laptop in VS Code
2. open the LDGCC view
3. start Master
4. open `LDGCC Worker` on each Worker laptop
5. click `Start Worker`
6. discover Workers from the Brain laptop
7. pair the Workers
8. prepare the job
9. set up the Workers
10. start training
11. inspect logs and results after the run finishes

Under the hood, the system is doing:

```text
project package
    -> dataset shard creation
    -> workspace transfer
    -> Worker setup
    -> train.py launch
    -> gradient synchronization
    -> result collection
```

## Full Project Setup

For a normal user project, the setup usually looks like this:

1. create or prepare a training project folder
2. put the dataset inside the project, or make sure the dataset path exists at
   the relative location your code expects
3. add `ldgcc.yaml`
4. add `requirements.txt` if your training code needs extra Python packages
5. update `train.py` to call the LDGCC runtime
6. install the Studio extension on the Brain laptop
7. install the Worker app on each Worker machine
8. discover, pair, prepare, set up, and start the job

If the project already trains on one machine, the usual code change is mainly
around gradient synchronization.

## Training Project Layout

### Example JSONL project

```text
movie-review/
    train.py
    requirements.txt
    ldgcc.yaml
    dataset/
        train.jsonl
```

### Example image-folder project

```text
dental-classifier/
    train.py
    requirements.txt
    ldgcc.yaml
    dataset/
        train/
            class_a/
            class_b/
            class_c/
```

### Example YOLO split project

```text
detector/
    train.py
    requirements.txt
    ldgcc.yaml
    dataset/
        train/
            images/
            labels/
```

Keep dataset paths stable inside the project. If your code expects
`dataset/train.jsonl` or `dataset/train/images`, LDGCC will keep that relative
path structure on the Worker side.

## `requirements.txt`

`requirements.txt` is optional, but in most real projects you should include
it.

Use it when your training code depends on packages beyond the LDGCC runtime
stack.

Typical examples:

- `transformers`
- `timm`
- `Pillow`
- `opencv-python`
- `ultralytics`
- dataset-specific utilities

LDGCC already manages its own core runtime packages on Workers, including
PyTorch and the communication/runtime dependencies. Your project
`requirements.txt` should focus on the packages your training code needs.

A simple example:

```text
timm==1.0.9
Pillow==10.4.0
scikit-learn==1.5.1
```

## Training Code Integration

There are two main runtime integration styles.

### 1. Simple sync style

This is the easiest model to understand.

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

This keeps your training loop close to normal PyTorch code.

### 2. Prepared runtime style

This is the newer path used when you want the runtime to prepare the model and
optimizer for the more advanced synchronization path.

```python
import locdist

model = locdist.prepare(model)
optimizer = locdist.prepare_optimizer(optimizer)

for batch in dataloader:
    optimizer.zero_grad()
    outputs = model(batch)
    loss = outputs.loss
    loss.backward()
    optimizer.step()
```

Use the prepared path when your project is written for the newer overlap-aware
runtime path.

## Gradient Accumulation

Gradient accumulation is still the training code's responsibility.

LDGCC does not decide your accumulation schedule for you. You decide it in your
training loop and, if you want the runtime to stay aligned with that behavior,
you also declare it in `ldgcc.yaml`.

### Example training-loop accumulation

```python
import locdist

accum_steps = 4

for step, batch in enumerate(dataloader, start=1):
    outputs = model(batch)
    loss = outputs.loss / accum_steps
    loss.backward()

    if step % accum_steps == 0:
        locdist.sync_gradients(model)
        optimizer.step()
        optimizer.zero_grad()
```

### Matching config

```yaml
training:
  gradient_accumulation_steps: 4
```

Why this matters:

- your code decides **when** optimizer steps happen
- `training.gradient_accumulation_steps` tells LDGCC runtime what accumulation
  structure to expect

If you use accumulation in code, keep the YAML value aligned with it.

If you do not want gradient accumulation, keep your training loop normal and
set the YAML value to `1`.

## `ldgcc.yaml`

LDGCC reads `ldgcc.yaml` or `ldgcc.yml` from the project root.

### Full example

```yaml
job:
  name: experiment-a

entrypoint: train.py

dataset:
  train: dataset/train.jsonl
  type: jsonl

workers:
  count: 2

outputs:
  - outputs/
  - checkpoints/

training:
  gradient_accumulation_steps: 4

communication:
  precision: fp16
  estimated_link_mbps: 300
  compression:
    type: topk
    mode: per_layer
    selection: sampled_threshold
    sample_rate: 1%
    max_payload_factor: 1.5
    top_k: 5%
    device: auto
    error_feedback: true
    warmup_steps: 100
```

## Required Fields

### `entrypoint`

The training file LDGCC runs on each Worker.

```yaml
entrypoint: train.py
```

Rules:

- must be a relative path
- must stay inside the project
- must exist and be readable

### `dataset.train`

The dataset path used for training.

```yaml
dataset:
  train: dataset/train.jsonl
```

Rules:

- must be a relative path
- must stay inside the project
- must exist and be readable

### `workers.count`

The exact number of Workers required for the job.

```yaml
workers:
  count: 2
```

LDGCC will only prepare the job if that many online Workers are available.

## Optional Fields

### `job.name`

Human-readable name for the job.

```yaml
job:
  name: food101-test
```

### `dataset.type`

Supported values:

- `jsonl`
- `image_folder`
- `yolo_split`

Examples:

```yaml
dataset:
  train: dataset/train.jsonl
  type: jsonl
```

```yaml
dataset:
  train: dataset/train
  type: image_folder
```

```yaml
dataset:
  train: dataset/train
  type: yolo_split
```

YOLO split layout should look like:

```text
dataset/train/
  images/
  labels/
```

### `outputs`

Relative files or directories to collect after the job finishes.

```yaml
outputs:
  - outputs/
  - logs/
  - result.json
```

If omitted, LDGCC still collects internal logs, but declared outputs are the
normal way to ask for project artifacts back.

### `training.gradient_accumulation_steps`

Tell LDGCC runtime how many accumulation microsteps belong to one optimizer
step.

```yaml
training:
  gradient_accumulation_steps: 4
```

Rules:

- should be a positive integer
- keep it aligned with your training code
- use `1` when you are not doing accumulation

### `communication.precision`

Supported values:

- `fp32`
- `fp16`

```yaml
communication:
  precision: fp16
```

This changes the transmitted gradient payload precision, not your full training
code automatically.

### `communication.estimated_link_mbps`

Optional estimate of link speed used for metric interpretation.

```yaml
communication:
  estimated_link_mbps: 300
```

### `communication.compression.type`

Supported values:

- `none`
- `topk`

```yaml
communication:
  compression:
    type: none
```

or

```yaml
communication:
  compression:
    type: topk
```

### `communication.compression.mode`

Only used when `type: topk`.

Supported values:

- `global`
- `per_layer`

```yaml
communication:
  compression:
    type: topk
    mode: per_layer
```

### `communication.compression.selection`

Only used when `type: topk`.

Supported values:

- `exact`
- `sampled_threshold`

```yaml
communication:
  compression:
    type: topk
    selection: sampled_threshold
```

### `communication.compression.sample_rate`

Only used with `selection: sampled_threshold`.

```yaml
communication:
  compression:
    type: topk
    selection: sampled_threshold
    sample_rate: 1%
```

### `communication.compression.max_payload_factor`

Only used with sampled-threshold selection.

```yaml
communication:
  compression:
    type: topk
    selection: sampled_threshold
    max_payload_factor: 1.5
```

This lets the selection path overshoot the target payload in a controlled way
before fallback logic is used.

### `communication.compression.top_k`

Top-k percentage.

```yaml
communication:
  compression:
    type: topk
    top_k: 5%
```

### `communication.compression.device`

Supported values:

- `auto`
- `cpu`
- `gpu`

```yaml
communication:
  compression:
    type: topk
    device: auto
```

### `communication.compression.error_feedback`

For top-k in the current design, this must be `true`.

```yaml
communication:
  compression:
    type: topk
    error_feedback: true
```

### `communication.compression.warmup_steps`

Number of sync steps to run before compression starts.

```yaml
communication:
  compression:
    type: topk
    warmup_steps: 100
```

## Project Requirements

Your project should follow these rules:

- `train.py` must exist
- `ldgcc.yaml` must exist
- dataset path in YAML must exist
- output paths must be relative and stay inside the project
- entrypoint and dataset paths must be relative

If you include `requirements.txt`:

- Workers will install it
- LDGCC still manages its own core runtime stack
- the Worker setup process filters user requirements so the LDGCC runtime
  environment is not accidentally overwritten

## Worker Setup Behavior

When a Worker sets up a job, it will:

1. verify that an NVIDIA CUDA GPU exists
2. unpack the workspace
3. create or reuse a Python environment
4. install CUDA PyTorch
5. install LDGCC runtime requirements
6. install your project requirements if present

This means:

- you do not need to manually create `.venv` on every Worker for each run
- repeated runs may reuse cached environments when dependency shape matches

## Notes And Limitations

Important current limitations:

- Brain laptop support is currently Linux-focused
- Worker laptops must have NVIDIA CUDA GPUs
- one Worker pairs with one Master at a time
- one active job model is the normal path
- LDGCC is designed for local LAN distributed training, not cloud-scale
  orchestration

## Contributor Guide

There are two ways to read this repo, depending on what you want.

### If you want to use LDGCC

Start with this top-level README. It is written from the user perspective and
covers installation, project setup, training integration, and config fields.

### If you want to build or extend LDGCC

Then move into the component READMEs:

- [master/README.md](master/README.md)
- [worker/README.md](worker/README.md)
- [runtime/README.md](runtime/README.md)
- [extension/README.md](extension/README.md)
- [packaging/README.md](packaging/README.md)

Those documents explain the internal design, request flows, algorithms,
component responsibilities, and tradeoffs.

## Developer Docs

If you want to understand the implementation:

- [master/README.md](master/README.md)
- [worker/README.md](worker/README.md)
- [runtime/README.md](runtime/README.md)

These are the main technical deep dives for the distributed training path.

## Contributing

Contributions are welcome, especially around:

- runtime performance
- synchronization and compression behavior
- dataset support
- Worker app UX
- Studio UX
- packaging and install flow
- cross-platform validation
- documentation
- tests

Before opening a PR, run the relevant checks.

### Master

```bash
cd master
env GOCACHE=/tmp/locdist-go-cache go test ./...
```

### Worker

```bash
cd worker
env GOCACHE=/tmp/locdist-go-cache go test ./...
```

### Extension

```bash
cd extension
npm run compile
```

### Runtime / end-to-end checks

```bash
python3 tools/e2e_local_validation.py --timeout 180
```

### Release packaging check

```bash
python3 tools/package_release.py --out /tmp/ldgcc-release-check
```

## Repo Guide

If you are new to the repo:

- start here for user setup and usage
- read `master/README.md`, `worker/README.md`, and `runtime/README.md` for the
  main technical design
- then read `extension/README.md` and `packaging/README.md` for the supporting
  parts around UI and release flow
