import * as vscode from "vscode";
import { ClusterNode, ClusterTreeDataProvider } from "./clusterView";
import { MasterClient } from "./masterClient";
import { MasterProcess } from "./masterProcess";
import { DiscoveredWorker, JobSummary } from "./types";

let masterProcess: MasterProcess;
let client: MasterClient | undefined;
let tree: ClusterTreeDataProvider;

export function activate(context: vscode.ExtensionContext): void {
  masterProcess = new MasterProcess(context);
  tree = new ClusterTreeDataProvider();
  context.subscriptions.push(vscode.window.registerTreeDataProvider("ldgcc.cluster", tree));

  register(context, "ldgcc.startMaster", startMaster);
  register(context, "ldgcc.stopMaster", stopMaster);
  register(context, "ldgcc.refresh", refresh);
  register(context, "ldgcc.discoverWorkers", discoverWorkers);
  register(context, "ldgcc.pairWorker", pairWorker);
  register(context, "ldgcc.prepareJob", prepareJob);
  register(context, "ldgcc.setupWorkers", setupWorkers);
  register(context, "ldgcc.retrySetup", retrySetup);
  register(context, "ldgcc.startTraining", startTraining);
  register(context, "ldgcc.stopTraining", stopTraining);
  register(context, "ldgcc.openResults", openResults);
}

export function deactivate(): void {
  client?.closeEvents();
}

function register(context: vscode.ExtensionContext, command: string, handler: (...args: unknown[]) => Promise<void>): void {
  context.subscriptions.push(vscode.commands.registerCommand(command, (...args) => runCommand(handler, ...args)));
}

async function runCommand(handler: (...args: unknown[]) => Promise<void>, ...args: unknown[]): Promise<void> {
  try {
    await handler(...args);
  } catch (error) {
    vscode.window.showErrorMessage(error instanceof Error ? error.message : String(error));
  }
}

async function startMaster(): Promise<void> {
  const session = await masterProcess.ensureStarted();
  client = new MasterClient(session);
  subscribeToEvents();
  await refresh();
  vscode.window.showInformationMessage(`LDGCC Master ready at ${session.address}`);
}

async function stopMaster(): Promise<void> {
  if (client) {
    await client.shutdown().catch(() => undefined);
    client.closeEvents();
    client = undefined;
  }
  await masterProcess.stop();
  tree.setConnected(false);
  vscode.window.showInformationMessage("LDGCC Master stopped");
}

async function refresh(): Promise<void> {
  const api = await ensureClient();
  tree.setState(await api.state());
}

async function discoverWorkers(): Promise<void> {
  const api = await ensureClient();
  await api.discoverWorkers();
  await refresh();
  vscode.window.showInformationMessage("LDGCC worker discovery started");
}

async function pairWorker(node?: unknown): Promise<void> {
  const api = await ensureClient();
  const worker = workerFromNode(node) ?? (await pickDiscoveredWorker());
  if (!worker) {
    return;
  }
  await api.pairWorker(worker.instance);
  await refresh();
  vscode.window.showInformationMessage(`Pairing request sent to ${worker.instance}`);
}

async function prepareJob(): Promise<void> {
  const api = await ensureClient();
  const root = workspaceRoot();
  if (!root) {
    throw new Error("Open the training project folder before preparing an LDGCC job");
  }
  await api.prepareJob(root);
  await refresh();
  vscode.window.showInformationMessage("LDGCC job preparation started");
}

async function setupWorkers(): Promise<void> {
  const api = await ensureClient();
  await api.setupWorkers();
  await refresh();
  vscode.window.showInformationMessage("Worker setup started");
}

async function retrySetup(): Promise<void> {
  const api = await ensureClient();
  await api.retrySetup();
  await refresh();
  vscode.window.showInformationMessage("Failed worker setup retry started");
}

async function startTraining(): Promise<void> {
  const api = await ensureClient();
  await api.startTraining();
  await refresh();
  vscode.window.showInformationMessage("Training started");
}

async function stopTraining(): Promise<void> {
  const api = await ensureClient();
  await api.stopTraining();
  await refresh();
  vscode.window.showInformationMessage("Training stop requested");
}

async function openResults(node?: unknown): Promise<void> {
  const api = await ensureClient();
  const summary = resultSummaryFromNode(node) ?? (await api.state()).last_summary;
  if (!summary) {
    throw new Error("No result summary is available yet");
  }
  const path = await api.resultPath(summary.job_id);
  await vscode.commands.executeCommand("vscode.openFolder", vscode.Uri.file(path), { forceNewWindow: false });
}

async function ensureClient(): Promise<MasterClient> {
  if (!client) {
    const session = await masterProcess.ensureStarted();
    client = new MasterClient(session);
    subscribeToEvents();
  }
  return client;
}

function subscribeToEvents(): void {
  if (!client) {
    return;
  }
  client.subscribe(
    async (event) => {
      await refresh().catch(() => undefined);
      if (event.type === "command.rejected" || event.type.endsWith("_failed") || event.type === "job.failed") {
        vscode.window.showWarningMessage(`LDGCC: ${event.type}`);
      }
      if (event.type === "job.completed") {
        vscode.window.showInformationMessage("LDGCC job completed");
      }
    },
    () => undefined,
  );
}

function workspaceRoot(): string | undefined {
  return vscode.workspace.workspaceFolders?.[0]?.uri.fsPath;
}

async function pickDiscoveredWorker(): Promise<DiscoveredWorker | undefined> {
  const api = await ensureClient();
  const state = await api.state();
  const picked = await vscode.window.showQuickPick(
    state.discovered_workers.map((worker) => ({
      label: worker.instance,
      description: worker.pairing_status,
      worker,
    })),
    { placeHolder: "Select an LDGCC Worker to pair" },
  );
  return picked?.worker;
}

function workerFromNode(value: unknown): DiscoveredWorker | undefined {
  if (value instanceof ClusterNode) {
    const candidate = value.value as Partial<DiscoveredWorker> | undefined;
    if (candidate?.instance) {
      return candidate as DiscoveredWorker;
    }
  }
  return undefined;
}

function resultSummaryFromNode(value: unknown): JobSummary | undefined {
  if (value instanceof ClusterNode) {
    const candidate = value.value as Partial<JobSummary> | undefined;
    if (candidate?.job_id) {
      return candidate as JobSummary;
    }
  }
  return undefined;
}
