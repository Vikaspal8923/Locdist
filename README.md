## LDGCC Phase 14: VS Code Extension

Phase 14 introduces `extension/`, the VS Code control surface for LDGCC V1.
The extension starts or reuses the local Master, reads `master-session.json`,
authenticates with the Phase 13 localhost API, subscribes to live events, and
exposes the main production workflow from the active training project folder:

```text
Start Master
    -> Discover Workers
    -> Pair Workers
    -> Prepare Job
    -> Set Up Workers
    -> Start Training
    -> Stop Training / Open Results
```

Development mode launches Master with `go run ./cmd/master`. Production mode
can point `ldgcc.master.binaryPath` at a bundled Master binary while keeping the
same API and session-file contract.
