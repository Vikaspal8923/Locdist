import * as vscode from "vscode";
import { MasterState } from "./types";

type ActionHandler = (action: string, payload?: unknown) => Promise<void>;

export class StudioViewProvider implements vscode.WebviewViewProvider {
  private view?: vscode.WebviewView;
  private connected = false;
  private state?: MasterState;
  private busy = false;
  private message = "Master stopped";

  constructor(private readonly context: vscode.ExtensionContext, private readonly onAction: ActionHandler) {}

  resolveWebviewView(view: vscode.WebviewView): void {
    this.view = view;
    view.webview.options = { enableScripts: true };
    view.webview.onDidReceiveMessage((message: { action: string; payload?: unknown }) => {
      void this.run(message.action, message.payload);
    });
    this.render();
  }

  setDisconnected(message = "Master stopped"): void {
    this.connected = false;
    this.state = undefined;
    this.message = message;
    this.render();
  }

  setState(state: MasterState): void {
    this.connected = true;
    this.state = state;
    this.message = "Master ready";
    this.render();
  }

  setMessage(message: string): void {
    this.message = message;
    this.render();
  }

  private async run(action: string, payload?: unknown): Promise<void> {
    this.busy = true;
    this.message = labelForAction(action);
    this.render();
    try {
      await this.onAction(action, payload);
    } catch (error) {
      this.message = error instanceof Error ? error.message : String(error);
      vscode.window.showErrorMessage(this.message);
      this.render();
    } finally {
      this.busy = false;
      this.render();
    }
  }

  private render(): void {
    if (!this.view) {
      return;
    }
    this.view.webview.html = this.html(this.view.webview);
  }

  private html(webview: vscode.Webview): string {
    const nonce = randomNonce();
    const state = this.state;
    const discovered = state?.discovered_workers ?? [];
    const workers = state?.workers ?? [];
    const job = state?.job;
    const summary = state?.last_summary;
    const masterStatus = this.connected ? `${state?.master.status ?? "ready"} ${state?.master.version ?? ""}`.trim() : "stopped";
    const canRunJob = this.connected && !this.busy;
    const workerRows = workers.length
      ? workers
          .map(
            (worker) => `
              <div class="row">
                <div>
                  <strong>${escapeHtml(worker.worker_id)}</strong>
                  <span>${escapeHtml(worker.host)}:${escapeHtml(worker.grpc_port)}</span>
                </div>
                <b class="${worker.availability === "ONLINE" ? "ok" : "warn"}">${escapeHtml(worker.availability)}</b>
              </div>`,
          )
          .join("")
      : `<div class="empty">No paired Workers yet.</div>`;
    const discoveredRows = discovered.length
      ? discovered
          .map(
            (worker) => `
              <div class="row">
                <div>
                  <strong>${escapeHtml(worker.instance)}</strong>
                  <span>${escapeHtml(worker.address)} · ${escapeHtml(worker.pairing_status)}</span>
                </div>
                <button data-action="pairWorker" data-instance="${escapeAttribute(worker.instance)}" ${this.busy ? "disabled" : ""}>Pair</button>
              </div>`,
          )
          .join("")
      : `<div class="empty">No Workers discovered. Start Worker App on another laptop, then discover.</div>`;
    const setupRows = job?.setup
      ? Object.entries(job.setup)
          .map(([workerID, setup]) => `<div class="mini"><span>${escapeHtml(workerID)}</span><b>${escapeHtml(setup.status)}</b></div>`)
          .join("")
      : "";
    const runRows = job?.run
      ? Object.entries(job.run)
          .map(([workerID, run]) => `<div class="mini"><span>${escapeHtml(workerID)}</span><b>${escapeHtml(run.status)}</b></div>`)
          .join("")
      : "";

    return `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src ${webview.cspSource} 'unsafe-inline'; script-src 'nonce-${nonce}';">
  <style>
    :root {
      color-scheme: dark light;
      --bg: var(--vscode-sideBar-background);
      --fg: var(--vscode-sideBar-foreground);
      --muted: var(--vscode-descriptionForeground);
      --border: var(--vscode-panel-border);
      --button: var(--vscode-button-background);
      --button-fg: var(--vscode-button-foreground);
      --button-hover: var(--vscode-button-hoverBackground);
      --secondary: var(--vscode-button-secondaryBackground);
      --secondary-fg: var(--vscode-button-secondaryForeground);
      --input: var(--vscode-input-background);
      --focus: var(--vscode-focusBorder);
    }
    * { box-sizing: border-box; }
    body { margin: 0; padding: 14px; background: var(--bg); color: var(--fg); font-family: var(--vscode-font-family); font-size: var(--vscode-font-size); }
    h1 { margin: 0 0 4px; font-size: 18px; font-weight: 700; letter-spacing: 0; }
    h2 { margin: 0 0 10px; font-size: 12px; font-weight: 700; text-transform: uppercase; color: var(--muted); letter-spacing: .04em; }
    .shell { display: grid; gap: 12px; max-width: 760px; }
    .hero, .panel { border: 1px solid var(--border); border-radius: 8px; background: color-mix(in srgb, var(--input) 45%, transparent); padding: 12px; }
    .hero { display: grid; grid-template-columns: 1fr auto; gap: 12px; align-items: start; }
    .status { display: inline-flex; align-items: center; gap: 7px; color: var(--muted); }
    .dot { width: 9px; height: 9px; border-radius: 50%; background: ${this.connected ? "#2ea043" : "#d29922"}; }
    .message { margin-top: 10px; color: var(--muted); line-height: 1.45; }
    .actions { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 8px; }
    .workflow { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 8px; }
    button { min-height: 32px; border: 0; border-radius: 5px; padding: 6px 10px; color: var(--button-fg); background: var(--button); font: inherit; cursor: pointer; }
    button:hover { background: var(--button-hover); }
    button.secondary { color: var(--secondary-fg); background: var(--secondary); }
    button:disabled { opacity: .55; cursor: not-allowed; }
    .row { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 8px; align-items: center; padding: 8px 0; border-top: 1px solid var(--border); }
    .row:first-of-type { border-top: 0; padding-top: 0; }
    .row strong, .row span { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .row span, .empty, .meta { color: var(--muted); }
    .empty { padding: 8px 0; line-height: 1.45; }
    .ok { color: #2ea043; }
    .warn { color: #d29922; }
    .job-title { display: flex; justify-content: space-between; gap: 8px; align-items: center; margin-bottom: 8px; }
    .pill { border: 1px solid var(--border); border-radius: 999px; padding: 2px 8px; color: var(--muted); white-space: nowrap; }
    .mini { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 8px; padding: 5px 0; color: var(--muted); }
    .mini span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .result { display: grid; gap: 8px; }
    @media (min-width: 560px) {
      .actions, .workflow { grid-template-columns: repeat(4, minmax(0, 1fr)); }
    }
  </style>
</head>
<body>
  <main class="shell">
    <section class="hero">
      <div>
        <h1>LDGCC Studio</h1>
        <div class="status"><span class="dot"></span><span>Master ${escapeHtml(masterStatus)}</span></div>
        <div class="message">${escapeHtml(this.message)}</div>
      </div>
      <button data-action="refresh" class="secondary" ${this.busy ? "disabled" : ""}>Refresh</button>
    </section>

    <section class="panel">
      <h2>Master</h2>
      <div class="actions">
        <button data-action="startMaster" ${this.busy || this.connected ? "disabled" : ""}>Start</button>
        <button data-action="stopMaster" class="secondary" ${this.busy || !this.connected ? "disabled" : ""}>Stop</button>
        <button data-action="discoverWorkers" ${!canRunJob ? "disabled" : ""}>Discover</button>
        <button data-action="refresh" class="secondary" ${this.busy ? "disabled" : ""}>Refresh</button>
      </div>
    </section>

    <section class="panel">
      <h2>Workflow</h2>
      <div class="workflow">
        <button data-action="prepareJob" ${!canRunJob ? "disabled" : ""}>Prepare</button>
        <button data-action="setupWorkers" ${!canRunJob ? "disabled" : ""}>Set Up</button>
        <button data-action="startTraining" ${!canRunJob ? "disabled" : ""}>Train</button>
        <button data-action="stopTraining" class="secondary" ${!canRunJob ? "disabled" : ""}>Stop</button>
      </div>
    </section>

    <section class="panel">
      <h2>Discovered Workers</h2>
      ${discoveredRows}
    </section>

    <section class="panel">
      <h2>Paired Workers</h2>
      ${workerRows}
    </section>

    <section class="panel">
      <h2>Current Job</h2>
      ${
        job
          ? `<div class="job-title"><strong>${escapeHtml(job.job_id)}</strong><span class="pill">${escapeHtml(job.status)}</span></div>
             <div class="meta">${escapeHtml(job.entrypoint)} · ${escapeHtml(job.dataset_path)} · ${job.expected_workers} worker(s)</div>
             ${setupRows ? `<h2 style="margin-top:12px">Setup</h2>${setupRows}` : ""}
             ${runRows ? `<h2 style="margin-top:12px">Run</h2>${runRows}` : ""}`
          : `<div class="empty">No active job. Prepare a project from the open VS Code folder.</div>`
      }
    </section>

    <section class="panel result">
      <h2>Results</h2>
      ${
        summary
          ? `<div><strong>${escapeHtml(summary.job_id)}</strong><div class="meta">${escapeHtml(summary.status)} · ${escapeHtml(summary.finished_at)}</div></div>
             <button data-action="openResults" ${this.busy ? "disabled" : ""}>Open Results</button>`
          : `<div class="empty">No collected results yet.</div>`
      }
    </section>
  </main>
  <script nonce="${nonce}">
    const vscode = acquireVsCodeApi();
    document.addEventListener("click", (event) => {
      const button = event.target.closest("button[data-action]");
      if (!button) return;
      const payload = button.dataset.instance ? { instance: button.dataset.instance } : undefined;
      vscode.postMessage({ action: button.dataset.action, payload });
    });
  </script>
</body>
</html>`;
  }
}

function labelForAction(action: string): string {
  const labels: Record<string, string> = {
    startMaster: "Starting Master...",
    stopMaster: "Stopping Master...",
    refresh: "Refreshing cluster state...",
    discoverWorkers: "Searching for Workers on the LAN...",
    pairWorker: "Sending pairing request...",
    prepareJob: "Preparing project and dataset shards...",
    setupWorkers: "Setting up Worker environments...",
    retrySetup: "Retrying failed Worker setup...",
    startTraining: "Starting training...",
    stopTraining: "Stopping training...",
    openResults: "Opening collected results...",
  };
  return labels[action] ?? "Working...";
}

function randomNonce(): string {
  const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789";
  let value = "";
  for (let index = 0; index < 32; index += 1) {
    value += alphabet[Math.floor(Math.random() * alphabet.length)];
  }
  return value;
}

function escapeHtml(value: unknown): string {
  return String(value ?? "")
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}

function escapeAttribute(value: unknown): string {
  return escapeHtml(value).replace(/`/g, "&#96;");
}
