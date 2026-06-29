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
