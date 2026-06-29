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
    const pairedOnline = workers.filter((worker) => worker.availability === "ONLINE").length;
    const jobStatus = job?.status ?? "No job";
    const workerRows = workers.length
      ? workers
          .map(
            (worker) => `
              <div class="row worker-row">
                <div>
                  <strong>${escapeHtml(worker.worker_id)}</strong>
                  <span>${escapeHtml(worker.host)}:${escapeHtml(worker.grpc_port)} · ${escapeHtml(worker.status)}</span>
                </div>
                <b class="badge ${worker.availability === "ONLINE" ? "good" : "attention"}">${escapeHtml(worker.availability)}</b>
              </div>`,
          )
          .join("")
      : `<div class="empty"><strong>No paired Workers yet</strong><span>Start Worker App on another laptop, discover it, then pair.</span></div>`;
    const discoveredRows = discovered.length
      ? discovered
          .map(
            (worker) => {
              const status = worker.request_status ? `${worker.pairing_status} · ${worker.request_status}` : worker.pairing_status;
              const error = worker.error ? `<span class="error">${escapeHtml(worker.error)}</span>` : "";
              return `
              <div class="row worker-row">
                <div>
                  <strong>${escapeHtml(worker.instance)}</strong>
                  <span>${escapeHtml(worker.address)} · ${escapeHtml(status)}</span>
                  ${error}
                </div>
                <button class="compact" data-action="pairWorker" data-instance="${escapeAttribute(worker.instance)}" ${this.busy ? "disabled" : ""}>Pair</button>
              </div>`;
            },
          )
          .join("")
      : `<div class="empty"><strong>No Workers discovered</strong><span>Open LDGCC Worker on a worker laptop and click Discover.</span></div>`;
    const setupRows = job?.setup
      ? Object.entries(job.setup)
          .map(([workerID, setup]) => `<div class="mini"><span>${escapeHtml(workerID)}</span><b class="badge subtle">${escapeHtml(cleanStatus(setup.status))}</b></div>`)
          .join("")
      : "";
    const runRows = job?.run
      ? Object.entries(job.run)
          .map(([workerID, run]) => `<div class="mini"><span>${escapeHtml(workerID)}</span><b class="badge subtle">${escapeHtml(cleanStatus(run.status))}</b></div>`)
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
      --accent: var(--vscode-textLink-foreground);
      --card: color-mix(in srgb, var(--input) 58%, transparent);
      --card-strong: color-mix(in srgb, var(--button) 14%, var(--input));
    }
    * { box-sizing: border-box; }
    body { margin: 0; padding: 14px; background: var(--bg); color: var(--fg); font-family: var(--vscode-font-family); font-size: var(--vscode-font-size); }
    h1 { margin: 0 0 6px; font-size: 20px; font-weight: 760; letter-spacing: 0; }
    h2 { margin: 0; font-size: 11px; font-weight: 760; text-transform: uppercase; color: var(--muted); letter-spacing: .06em; }
    .shell { display: grid; gap: 12px; max-width: 820px; }
    .hero, .panel { border: 1px solid var(--border); border-radius: 8px; background: var(--card); padding: 13px; }
    .hero { position: relative; overflow: hidden; background: linear-gradient(135deg, var(--card-strong), var(--card)); }
    .hero-main { display: grid; grid-template-columns: 1fr auto; gap: 12px; align-items: start; }
    .status { display: inline-flex; align-items: center; gap: 7px; color: var(--muted); }
    .dot { width: 9px; height: 9px; border-radius: 50%; background: ${this.connected ? "#2ea043" : "#d29922"}; box-shadow: 0 0 0 3px color-mix(in srgb, ${this.connected ? "#2ea043" : "#d29922"} 18%, transparent); }
    .message { margin-top: 10px; color: var(--muted); line-height: 1.45; }
    .metrics { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 8px; margin-top: 14px; }
    .metric { border: 1px solid var(--border); border-radius: 7px; padding: 9px; background: color-mix(in srgb, var(--bg) 28%, transparent); min-height: 58px; }
    .metric span { display: block; color: var(--muted); font-size: 11px; margin-bottom: 5px; }
    .metric strong { display: block; font-size: 16px; line-height: 1.1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .panel-head { display: flex; justify-content: space-between; gap: 10px; align-items: center; margin-bottom: 10px; }
    .actions { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 8px; }
    .workflow { display: grid; gap: 8px; }
    .step { display: grid; grid-template-columns: 26px minmax(0, 1fr) auto; gap: 9px; align-items: center; width: 100%; min-height: 44px; text-align: left; }
    .step-number { display: grid; place-items: center; width: 24px; height: 24px; border-radius: 50%; background: color-mix(in srgb, var(--button) 18%, var(--bg)); color: var(--fg); font-weight: 760; }
    .step strong, .step span { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .step span { color: color-mix(in srgb, var(--button-fg) 70%, transparent); font-size: 11px; margin-top: 2px; }
    button { min-height: 34px; border: 1px solid transparent; border-radius: 6px; padding: 7px 11px; color: var(--button-fg); background: var(--button); font: inherit; font-weight: 650; cursor: pointer; }
    button:hover { background: var(--button-hover); }
    button.secondary { color: var(--secondary-fg); background: var(--secondary); border-color: var(--border); }
    button.ghost { color: var(--fg); background: transparent; border-color: var(--border); }
    button.compact { min-height: 30px; padding: 5px 10px; }
    button:focus-visible { outline: 1px solid var(--focus); outline-offset: 2px; }
    button:disabled { opacity: .48; cursor: not-allowed; }
    .row { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 9px; align-items: center; padding: 10px 0; border-top: 1px solid var(--border); }
    .row:first-of-type { border-top: 0; padding-top: 0; }
    .row strong, .row span { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .row span, .empty, .meta { color: var(--muted); }
    .error { color: #f85149; white-space: normal; line-height: 1.35; margin-top: 4px; }
    .worker-row strong { margin-bottom: 3px; }
    .empty { display: grid; gap: 4px; padding: 12px; line-height: 1.45; border: 1px dashed var(--border); border-radius: 7px; background: color-mix(in srgb, var(--bg) 20%, transparent); }
    .empty strong { color: var(--fg); }
    .empty span { color: var(--muted); }
    .badge { display: inline-flex; align-items: center; justify-content: center; min-height: 22px; border: 1px solid var(--border); border-radius: 999px; padding: 2px 8px; color: var(--muted); font-size: 11px; font-weight: 760; white-space: nowrap; }
    .badge.good { color: #2ea043; border-color: color-mix(in srgb, #2ea043 44%, var(--border)); background: color-mix(in srgb, #2ea043 9%, transparent); }
    .badge.attention { color: #d29922; border-color: color-mix(in srgb, #d29922 44%, var(--border)); background: color-mix(in srgb, #d29922 9%, transparent); }
    .badge.subtle { background: color-mix(in srgb, var(--bg) 30%, transparent); }
    .job-title { display: flex; justify-content: space-between; gap: 8px; align-items: center; margin-bottom: 8px; }
    .mini { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 8px; padding: 5px 0; color: var(--muted); }
    .mini span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .result { display: grid; gap: 8px; }
    @media (min-width: 560px) {
      .actions { grid-template-columns: repeat(4, minmax(0, 1fr)); }
      .workflow { grid-template-columns: repeat(2, minmax(0, 1fr)); }
    }
  </style>
</head>
<body>
  <main class="shell">
    <section class="hero">
      <div class="hero-main">
        <div>
          <h1>LDGCC Studio</h1>
          <div class="status"><span class="dot"></span><span>Master ${escapeHtml(masterStatus)}</span></div>
          <div class="message">${escapeHtml(this.message)}</div>
        </div>
        <button data-action="refresh" class="ghost compact" ${this.busy ? "disabled" : ""}>Refresh</button>
      </div>
      <div class="metrics">
        <div class="metric"><span>Online Workers</span><strong>${pairedOnline}/${workers.length}</strong></div>
        <div class="metric"><span>Discovered</span><strong>${discovered.length}</strong></div>
        <div class="metric"><span>Job</span><strong>${escapeHtml(jobStatus)}</strong></div>
      </div>
    </section>

    <section class="panel">
      <div class="panel-head">
        <h2>Master Controls</h2>
        <span class="badge ${this.connected ? "good" : "attention"}">${this.connected ? "Ready" : "Stopped"}</span>
      </div>
      <div class="actions">
        <button data-action="startMaster" ${this.busy || this.connected ? "disabled" : ""}>Start</button>
        <button data-action="stopMaster" class="secondary" ${this.busy || !this.connected ? "disabled" : ""}>Stop</button>
        <button data-action="discoverWorkers" ${!canRunJob ? "disabled" : ""}>Discover</button>
        <button data-action="refresh" class="ghost" ${this.busy ? "disabled" : ""}>Refresh</button>
      </div>
    </section>

    <section class="panel">
      <div class="panel-head">
        <h2>Job Workflow</h2>
        <span class="badge subtle">Active folder</span>
      </div>
      <div class="workflow">
        <button class="step" data-action="prepareJob" ${!canRunJob ? "disabled" : ""}>
          <span class="step-number">1</span><span><strong>Prepare</strong><span>Package project and shard dataset</span></span><span>Start</span>
        </button>
        <button class="step" data-action="setupWorkers" ${!canRunJob ? "disabled" : ""}>
          <span class="step-number">2</span><span><strong>Set Up</strong><span>Create venv and install requirements</span></span><span>Run</span>
        </button>
        <button class="step" data-action="startTraining" ${!canRunJob ? "disabled" : ""}>
          <span class="step-number">3</span><span><strong>Train</strong><span>Launch workers and sync gradients</span></span><span>Go</span>
        </button>
        <button class="step secondary" data-action="stopTraining" ${!canRunJob ? "disabled" : ""}>
          <span class="step-number">4</span><span><strong>Stop</strong><span>Request training stop</span></span><span>Stop</span>
        </button>
      </div>
    </section>

    <section class="panel">
      <div class="panel-head"><h2>Discovered Workers</h2><span class="badge subtle">${discovered.length}</span></div>
      ${discoveredRows}
    </section>

    <section class="panel">
      <div class="panel-head"><h2>Paired Workers</h2><span class="badge subtle">${workers.length}</span></div>
      ${workerRows}
    </section>

    <section class="panel">
      <div class="panel-head"><h2>Current Job</h2>${job ? `<span class="badge subtle">${escapeHtml(job.status)}</span>` : ""}</div>
      ${
        job
          ? `<div class="job-title"><strong>${escapeHtml(job.job_id)}</strong></div>
             <div class="meta">${escapeHtml(job.entrypoint)} · ${escapeHtml(job.dataset_path)} · ${job.expected_workers} worker(s)</div>
             ${setupRows ? `<h2 style="margin-top:12px">Setup</h2>${setupRows}` : ""}
             ${runRows ? `<h2 style="margin-top:12px">Run</h2>${runRows}` : ""}`
          : `<div class="empty">No active job. Prepare a project from the open VS Code folder.</div>`
      }
    </section>

    <section class="panel result">
      <div class="panel-head"><h2>Results</h2>${summary ? `<span class="badge good">${escapeHtml(summary.status)}</span>` : ""}</div>
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

function cleanStatus(value: unknown): string {
  return String(value ?? "")
    .replace(/^JOB_SETUP_STATUS_/, "")
    .replace(/^JOB_RUN_STATUS_/, "")
    .replace(/^WORKER_STATUS_/, "")
    .replace(/_/g, " ")
    .toLowerCase()
    .replace(/\b\w/g, (letter) => letter.toUpperCase());
}
