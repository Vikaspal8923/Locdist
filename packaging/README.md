# LDGCC Phase 21: Production Packaging Foundation

Phase 21 defines the first production ownership model for LDGCC V1 binaries,
configs, and local data.

## Build Output

From the repository root:

```bash
python3 tools/build_release.py
```

This creates:

```text
dist/ldgcc-local/
    bin/
        ldgcc-master
        ldgcc-worker-app
        ldgcc-worker
    manifest.json
```

The script builds the local platform binaries only. Cross-platform release
bundles can build on top of the same layout later.

## Master Ownership

The VS Code extension owns the local Master process on the Brain Laptop.

In development mode, the extension runs:

```text
go run ./cmd/master
```

In production mode, `ldgcc.master.binaryPath` points to the packaged
`ldgcc-master` binary. The extension stores Master state in its VS Code global
storage directory:

```text
master_config.json
master-session.json
master_pairings.json
ldgcc_jobs/
ldgcc_results/
```

The Master binary creates a default `master_config.json` when the configured
file does not exist.

## Worker Ownership

The worker laptop runs `ldgcc-worker-app`. The Worker App owns the local Worker
service lifecycle, pairing state, and workspace storage.

Production flags:

```bash
ldgcc-worker-app --config /path/to/worker_config.json --data-dir /path/to/worker-data
```

The Worker binaries create a default `worker_config.json` when the configured
file does not exist. Relative `pairing_path` and `workspace_root` values are
resolved under the Worker data directory.

## Validation

The Phase 20 local validation remains the main smoke test:

```bash
python3 tools/e2e_local_validation.py
```

---

# LDGCC Phase 22: VS Code Extension Production Packaging

Phase 22 stages the VS Code extension with a bundled Master binary, so a
production extension can start Master without requiring the user to configure
`ldgcc.master.binaryPath`.

From the repository root:

```bash
python3 tools/stage_extension.py
```

This creates:

```text
dist/ldgcc-extension/
    package.json
    README.md
    out/
    resources/
    bin/
        linux-x64/
            ldgcc-master
```

The platform folder uses Node's `process.platform` and `process.arch` naming.
The extension checks the bundled binary first after any explicitly configured
binary path.

Production startup order:

```text
configured Master binary
    -> bundled Master binary
    -> development go run fallback
```

The next release step is turning this staged folder into a `.vsix` with VS Code
packaging tooling.

---

# LDGCC Phase 23: Worker App Production Packaging

Phase 23 stages the Worker App as a worker-laptop package. The package contains
the Worker App binary, headless Worker binary, launcher script, manifest, and a
worker-facing README.

From the repository root:

```bash
python3 tools/stage_worker_app.py
```

This creates:

```text
dist/ldgcc-worker-app/
    README.md
    manifest.json
    run-worker-app.sh
    bin/
        linux-x64/
            ldgcc-worker-app
            ldgcc-worker
```

Worker laptop flow:

```text
run-worker-app.sh
    -> starts local Worker App
    -> opens/prints http://127.0.0.1:5050
    -> user clicks Start Worker
    -> user accepts Master pairing request
    -> Worker receives setup/training commands
```

The launcher stores local Worker state under:

```text
~/.ldgcc/worker
```

Override it with:

```bash
LDGCC_WORKER_DATA_DIR=/custom/path ./run-worker-app.sh
```

This is still a staged package, not a native OS installer. A future release step
can wrap this folder into platform-specific installers.

---

# LDGCC Phase 24: Release Bundle and Install Artifacts

Phase 24 creates one release folder for local distribution.

From the repository root:

```bash
python3 tools/package_release.py
```

This creates:

```text
dist/release/
    ldgcc-studio.vsix
    ldgcc-worker-app-linux-x64.zip
    INSTALL.md
    manifest.json
    checksums.txt
```

User install flow:

```text
Brain Laptop
    -> install ldgcc-studio.vsix in VS Code
    -> open training project
    -> use LDGCC view

Worker Laptop
    -> extract ldgcc-worker-app-linux-x64.zip
    -> run ./run-worker-app.sh
    -> open http://127.0.0.1:5050
    -> click Start Worker
    -> accept pairing request
```

`manifest.json` records release artifact names and SHA-256 values.
`checksums.txt` provides copy-paste checksum verification.

This phase produces local release artifacts. It does not yet produce native OS
installers such as `.deb`, `.dmg`, `.msi`, or AppImage.

---

# LDGCC Phase 25: Cross-Platform Worker Packages

Phase 25 supports mixed Worker operating systems in one LDGCC cluster.

From the repository root:

```bash
python3 tools/package_release.py
```

The release now includes Linux and Windows Worker packages by default:

```text
dist/release/
    ldgcc-studio.vsix
    ldgcc-worker-app-linux-x64.zip
    ldgcc-worker-app-windows-x64.zip
    INSTALL.md
    manifest.json
    checksums.txt
```

Worker install flow:

```text
Linux worker laptop
    -> extract ldgcc-worker-app-linux-x64.zip
    -> run ./run-worker-app.sh

Windows worker laptop
    -> extract ldgcc-worker-app-windows-x64.zip
    -> run run-worker-app.bat
```

Both Worker packages speak the same LDGCC gRPC protocol, so a single Master can
train with Linux and Windows Workers in the same job.

Build only one Worker target when needed:

```bash
python3 tools/package_release.py --worker-target linux-x64
python3 tools/package_release.py --worker-target windows-x64
```

Stage one Worker package directly:

```bash
python3 tools/stage_worker_app.py --target linux-x64
python3 tools/stage_worker_app.py --target windows-x64
```
