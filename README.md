# LocDist LDGCC V1

LocDist LDGCC is a local distributed training system for running one training
job across multiple laptops on the same LAN.

The goal is simple: keep the user's PyTorch training code mostly normal, while
LDGCC handles worker discovery, pairing, project packaging, dataset sharding,
environment setup, gradient synchronization, and result collection.

This top-level README is written for normal users who want to understand what
LDGCC is, what it supports, and how to run it.

If you want implementation details or want to work on the codebase itself, use
the component developer docs instead:

- [master/README.md](master/README.md)
- [worker/README.md](worker/README.md)
- [runtime/README.md](runtime/README.md)
- [extension/README.md](extension/README.md)
- [packaging/README.md](packaging/README.md)

In short:

- `README.md`: user-facing overview and usage
- component READMEs: developer-facing architecture and implementation notes

## What LDGCC Does

LDGCC turns nearby laptops into a small local training cluster:

```text
Brain Laptop
    owns the training project and controls the job

Worker Laptops
    provide compute and run assigned dataset shards

LDGCC Runtime
    syncs gradients from inside the user's Python training loop
```

The workers do not talk to each other. All coordination goes through the Brain
laptop.

## User-Facing Components

Users only need to understand these pieces:

```text
LDGCC Studio
    VS Code extension installed on the Brain laptop.
    Starts Master internally and provides the training controls.

LDGCC Worker
    App installed on each Worker laptop.
    Makes the laptop discoverable, handles pairing, setup, and training.

LDGCC Runtime
    Python package imported by train.py.
    Synchronizes gradients with locdist.sync_gradients(model).

ldgcc.yml
    Project config file.
    Tells LDGCC the entrypoint, dataset path, worker count, outputs, and
    communication settings.
```

## Current V1 Support

Supported now:

```text
Brain laptop:
    Linux x64 VS Code extension release with bundled Master

Worker laptops:
    Linux x64 Worker package
    Windows x64 Worker package
    Mixed Linux + Windows Workers in the same job
    NVIDIA CUDA GPU Workers only

Dataset types:
    JSONL line-based datasets
    ImageFolder-style image datasets

Training:
    PyTorch-style training loop
    averaged gradient synchronization
    fp32 or fp16 gradient communication
    optional top-k gradient compression
    setup with private Worker .venv
    automatic LDGCC runtime dependency install
    optional user requirements.txt install
    declared output collection
```

Not yet V1 scope:

```text
Native .deb/.msi installers
macOS packages
Windows Brain laptop bundled Master release
multi-Master per Worker
object-detection dataset formats such as COCO/YOLO
automatic accuracy guarantees for compressed training
```

## Download

LDGCC release files are published through GitHub Releases:

```text
https://github.com/Vikaspal8923/Locdist/releases
```

Each release provides:

```text
ldgcc-studio.vsix
ldgcc-worker-app-linux-x64.zip
ldgcc-worker-app-windows-x64.zip
INSTALL.md
checksums.txt
manifest.json
```

Normal users usually need only:

```text
ldgcc-studio.vsix
one Worker App zip matching each Worker laptop OS
INSTALL.md
```

`checksums.txt` and `manifest.json` are release verification/support files.

## Install

### Brain Laptop

The Brain laptop is where the training project is opened in VS Code.

Download:

```text
ldgcc-studio.vsix
```

Install:

```text
VS Code
    -> Extensions
    -> Install from VSIX
    -> select ldgcc-studio.vsix
```

Then open the training project folder and use the LDGCC view.

### Linux Worker Laptop

Download:

```text
ldgcc-worker-app-linux-x64.zip
```

Install:

```bash
unzip ldgcc-worker-app-linux-x64.zip
cd ldgcc-worker-app
./install-worker-app.sh
```

Open `LDGCC Worker` from the app menu.

### Windows Worker Laptop

Download:

```text
ldgcc-worker-app-windows-x64.zip
```

Install:

```text
Extract ldgcc-worker-app-windows-x64.zip
Double click ldgcc-worker-app\install-worker-app.bat
```

Open `LDGCC Worker` from the Desktop or Start Menu.

## Training Flow

1. Open the training project on the Brain laptop in VS Code.
2. Open the LDGCC view.
3. Start Master.
4. On each Worker laptop, open `LDGCC Worker` and click `Start Worker`.
5. In VS Code, discover Workers.
6. Pair the Workers.
7. Prepare the job.
8. Set up Workers.
9. Start training.
10. Open collected results after the job finishes.

Under the hood, LDGCC:

```text
packages the project
    -> shards the dataset
    -> sends each Worker its workspace
    -> creates a private .venv on each Worker
    -> runs train.py on each Worker
    -> synchronizes gradients through Master
    -> collects declared outputs and logs
```

## Training Project Layout

Example JSONL project:

```text
movie-review/
    train.py
    requirements.txt
    ldgcc.yml
    dataset/
        train.jsonl
```

Example image project:

```text
dental-classifier/
    train.py
    requirements.txt
    ldgcc.yml
    dataset/
        train/
            caries/
            calculus/
            gingivitis/
```

Keep dataset paths stable. If `train.py` reads `dataset/train.jsonl`, LDGCC
will make sure each Worker receives its shard at the same relative path.

## Python Runtime Usage

User training code should call `locdist.sync_gradients(model)` after
`loss.backward()` and before `optimizer.step()`.

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

Gradient clipping, optimizer choice, batch size, and gradient accumulation
remain user training-code responsibilities.

## `ldgcc.yml`

LDGCC reads `ldgcc.yml` or `ldgcc.yaml` from the training project root.

Full V1 example:

```yaml
job:
  name: movie-review-test

entrypoint: train.py

dataset:
  train: dataset/train.jsonl
  type: jsonl

workers:
  count: 2

outputs:
  - result.txt
  - checkpoints/

communication:
  precision: fp16
  compression:
    type: topk
    mode: global
    top_k: 5%
    error_feedback: true
    warmup_steps: 0
```

### Required Fields

`entrypoint`

Relative path to the training file LDGCC runs on each Worker.

```yaml
entrypoint: train.py
```

`dataset.train`

Relative path to the training dataset.

```yaml
dataset:
  train: dataset/train.jsonl
```

`workers.count`

Exact number of Workers required for the job. LDGCC selects this many paired
online Workers before preparing the job.

```yaml
workers:
  count: 2
```

### Optional Fields

`job.name`

Human-readable job name.

Default: empty.

```yaml
job:
  name: experiment-a
```

`dataset.type`

Supported values:

```text
jsonl
image_folder
yolo_split
```

Default: `jsonl`.

JSONL sharding is line-based. Image folder sharding expects class folders under
the dataset path, similar to PyTorch `ImageFolder`. `yolo_split` supports a
single YOLO train split directory containing paired `images/` and `labels/`
subdirectories.

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

Expected `yolo_split` structure:

```text
dataset/train/
  images/
    a.jpg
    nested/b.jpg
  labels/
    a.txt
    nested/b.txt
```

`outputs`

Relative files or directories to collect after training. When omitted, LDGCC
still collects setup/training logs.

```yaml
outputs:
  - result.txt
  - checkpoints/
```

`communication.precision`

Supported values:

```text
fp32
fp16
```

Default: `fp32`.

`communication.compression`

Supported values:

```text
none
topk
```

Default: `none`.

Top-k defaults:

```text
mode: global
top_k: 5%
error_feedback: true
warmup_steps: 0
```

Top-k modes:

```text
global
    choose the strongest gradients across the whole model

per_layer
    choose the strongest gradients separately inside each parameter tensor
```

Example without compression:

```yaml
communication:
  precision: fp32
  compression:
    type: none
```

Example with top-k:

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

In V1, `error_feedback` must be `true` when `compression.type` is `topk`.

## Things To Keep In Mind

All machines must be on the same LAN.

Each Worker pairs with one Master at a time. Use `Reset Previous Connection` in
the Worker App before pairing with a different Brain laptop.

Worker setup creates a private `.venv` for each job. LDGCC installs its runtime
dependencies automatically:

```text
torch
grpcio
protobuf
numpy
```

LDGCC V1 requires an NVIDIA CUDA Worker. If a Worker has no CUDA GPU or
`nvidia-smi` is not available, setup fails clearly before training starts.

If the project contains `requirements.txt`, Workers also install it for the
user's training code. LDGCC filters its own packages from `requirements.txt` so
the user file does not overwrite the LDGCC CUDA PyTorch runtime.

Every new job sends a fresh project package and dataset shard. This matters when
the user changes code or data between runs.

If any required Worker disconnects during training, the job should be treated as
failed and rerun from discovery/setup.

For successful jobs, declared `outputs` must exist. Missing declared outputs
cause result collection to fail.

## Build A Release

Maintainers can build release artifacts locally:

```bash
python3 tools/package_release.py
```

This creates:

```text
dist/release/
    ldgcc-studio.vsix
    ldgcc-worker-app-linux-x64.zip
    ldgcc-worker-app-windows-x64.zip
    INSTALL.md
    manifest.json
    checksums.txt
```

Publish to GitHub Releases:

```bash
gh auth login
python3 tools/publish_github_release.py v0.1.0 --draft
```

Use `--skip-build` if `dist/release/` is already built.

## Contributing

LDGCC is open for contributions around:

```text
dataset format support
runtime performance
training examples
Worker App UX
VS Code extension UX
release packaging
cross-platform validation
documentation
tests
```

Before opening a PR, run the relevant checks:

```bash
cd master && env GOCACHE=/tmp/locdist-go-cache go test ./...
cd worker && env GOCACHE=/tmp/locdist-go-cache go test ./...
cd extension && npm run compile
python3 tools/e2e_local_validation.py --timeout 180
```

For release packaging changes:

```bash
python3 tools/package_release.py --out /tmp/ldgcc-release-check
```

## Component Docs

Implementation notes live in component docs:

```text
master/README.md
worker/README.md
runtime/README.md
extension/README.md
packaging/README.md
```
