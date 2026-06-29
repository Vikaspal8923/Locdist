# LDGCC Studio Extension

## LDGCC Phase 14: VS Code Extension

Phase 14 introduces `extension/`, the VS Code control surface for LDGCC V1.

The extension runs on the Brain Laptop. It starts or reuses the local Master,
reads `master-session.json`, authenticates with the Phase 13 localhost API,
subscribes to live events, and drives the full job flow from the active VS Code
workspace.

## User Flow

```text
Open training project in VS Code
    -> LDGCC: Start Master
    -> LDGCC: Discover Workers
    -> Pair discovered Workers
    -> LDGCC: Prepare Job
    -> LDGCC: Set Up Workers
    -> LDGCC: Start Training
    -> Watch state/events
    -> Open collected results
```

## Commands

* `LDGCC: Start Master`
* `LDGCC: Stop Master`
* `LDGCC: Discover Workers`
* `LDGCC: Pair Worker`
* `LDGCC: Prepare Job`
* `LDGCC: Set Up Workers`
* `LDGCC: Retry Failed Setup`
* `LDGCC: Start Training`
* `LDGCC: Stop Training`
* `LDGCC: Open Results`

## Development Mode

When `ldgcc.master.binaryPath` is empty, the extension launches Master with:

```text
go run ./cmd/master
```

The command runs from the repository's `master/` directory and passes:

```text
--config
--data-dir
--app-host 127.0.0.1
--app-port 0
--session-token
```

Port `0` lets Master choose an available localhost API port. The extension then
waits for `master-session.json`, health-checks it, and subscribes to `/events`.

## Production Mode

For packaged releases, set `ldgcc.master.binaryPath` to the bundled Master
binary. The same session file and API flow is used, so the extension does not
need a separate production control path.

Phase 21 makes production mode independent of the source tree. When a packaged
Master binary is configured, the extension creates and uses
`master_config.json` inside its VS Code global storage directory. The same
directory also owns `master-session.json`, pairing state, job packages, and
collected results.

## LDGCC Phase 22: Extension Production Packaging

Phase 22 lets production builds avoid manual `ldgcc.master.binaryPath`
configuration. The extension now checks for a bundled Master binary at:

```text
bin/<node-platform>-<node-arch>/ldgcc-master
```

For example, Linux x64 uses:

```text
bin/linux-x64/ldgcc-master
```

Startup order:

```text
configured ldgcc.master.binaryPath
    -> bundled Master binary
    -> development fallback: go run ./cmd/master
```

Create a staged production extension folder from the repository root:

```bash
python3 tools/stage_extension.py
```

The staged folder contains compiled extension code, resources, and the bundled
Master binary.

## Views

The `LDGCC` activity bar view shows:

* Master status
* discovered Workers
* registered Workers
* current job state
* last result summary

The view refreshes from `/state` and also reacts to Server-Sent Events from the
Master API.
