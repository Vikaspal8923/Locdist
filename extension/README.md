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

## Views

The `LDGCC` activity bar view shows:

* Master status
* discovered Workers
* registered Workers
* current job state
* last result summary

The view refreshes from `/state` and also reacts to Server-Sent Events from the
Master API.
