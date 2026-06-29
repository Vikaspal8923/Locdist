# LDGCC V1

LDGCC runs local distributed training across one Brain laptop and one or more
Worker laptops on the same LAN.

## Download

LDGCC releases are distributed through GitHub Releases.

Each release provides:

```text
ldgcc-studio.vsix
ldgcc-worker-app-linux-x64.zip
ldgcc-worker-app-windows-x64.zip
INSTALL.md
checksums.txt
manifest.json
```

## Brain Laptop

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

Then open the training project and use the LDGCC view:

```text
Start Master
    -> Discover Workers
    -> Pair Workers
    -> Prepare Job
    -> Set Up Workers
    -> Start Training
    -> Open Results
```

## Worker Laptop

The Worker laptop provides compute. Linux and Windows Workers can join the same
training job.

Linux Worker download:

```text
ldgcc-worker-app-linux-x64.zip
```

Linux install:

```bash
unzip ldgcc-worker-app-linux-x64.zip
cd ldgcc-worker-app
./install-worker-app.sh
```

Windows Worker download:

```text
ldgcc-worker-app-windows-x64.zip
```

Windows install:

```text
Extract ldgcc-worker-app-windows-x64.zip
Double click ldgcc-worker-app\install-worker-app.bat
```

After install, open `LDGCC Worker`, click `Start Worker`, and accept the pairing
request from the Brain laptop.

## Training Project

A training project contains user code, an `ldgcc.yml` file, and the dataset.

Example:

```text
movie-review/
    train.py
    ldgcc.yml
    dataset/
        train.jsonl
```

Minimal `ldgcc.yml`:

```yaml
entrypoint: train.py
dataset:
  train: dataset/train.jsonl
  type: jsonl
workers:
  count: 2
outputs:
  - result.txt
```

## Build A Release

For maintainers:

```bash
python3 tools/package_release.py
```

This creates:

```text
dist/release/
```

Upload the files in that folder to a GitHub Release.

## More Docs

Implementation notes live in component docs:

```text
master/README.md
worker/README.md
runtime/README.md
extension/README.md
packaging/README.md
```
