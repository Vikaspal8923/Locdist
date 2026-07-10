import * as vscode from "vscode";
import * as fs from "node:fs/promises";
import * as path from "node:path";
import { MasterClient } from "./masterClient";
import { MasterProcess } from "./masterProcess";
import { StudioViewProvider } from "./studioView";
import { DiscoveredWorker, JobSummary, MasterEvent } from "./types";

let masterProcess: MasterProcess;
let client: MasterClient | undefined;
let studio: StudioViewProvider;

export function activate(context: vscode.ExtensionContext): void {
  masterProcess = new MasterProcess(context);
  studio = new StudioViewProvider(context, handleViewAction);
  context.subscriptions.push(vscode.window.registerWebviewViewProvider("ldgcc.cluster", studio));

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
  await loadState();
  vscode.window.showInformationMessage(`LDGCC Master ready at ${session.address}`);
}

async function stopMaster(): Promise<void> {
  if (client) {
    await client.shutdown().catch(() => undefined);
    client.closeEvents();
    client = undefined;
  }
  await masterProcess.stop();
  studio.setDisconnected();
  vscode.window.showInformationMessage("LDGCC Master stopped");
}

async function refresh(): Promise<void> {
  if (client) {
    await client.shutdown().catch(() => undefined);
    client.closeEvents();
    client = undefined;
  }
  await masterProcess.resetLocalState();
  studio.setDisconnected("Resetting LDGCC Studio...");
  const api = await ensureClient();
  studio.setState(await api.state());
  vscode.window.showInformationMessage("LDGCC Studio reset");
}

async function loadState(): Promise<void> {
  const api = await ensureClient();
  studio.setState(await api.state());
}

async function discoverWorkers(): Promise<void> {
  const api = await ensureClient();
  await api.discoverWorkers();
  await loadState();
  vscode.window.showInformationMessage("LDGCC worker discovery started");
}

async function pairWorker(node?: unknown): Promise<void> {
  const api = await ensureClient();
  const worker = workerFromNode(node) ?? (await pickDiscoveredWorker());
  if (!worker) {
    return;
  }
  await api.pairWorker(worker.id);
  await loadState();
  vscode.window.showInformationMessage(`Pairing request sent to ${worker.instance}`);
}

async function checkNetwork(): Promise<void> {
  const api = await ensureClient();
  await api.checkNetwork();
  await loadState();
  vscode.window.showInformationMessage("LDGCC Worker network check completed");
}

async function prepareJob(): Promise<void> {
  const api = await ensureClient();
  const root = workspaceRoot();
  if (!root) {
    throw new Error("Open the training project folder before preparing an LDGCC job");
  }
  const state = await api.state();
  await validateProjectBeforePrepare(root, state.workers?.filter((worker) => worker.availability === "ONLINE").length ?? 0);
  await api.prepareJob(root);
  await loadState();
  vscode.window.showInformationMessage("LDGCC job preparation started");
}

async function setupWorkers(): Promise<void> {
  const api = await ensureClient();
  await api.setupWorkers();
  await loadState();
  vscode.window.showInformationMessage("Worker setup started");
}

async function retrySetup(): Promise<void> {
  const api = await ensureClient();
  await api.retrySetup();
  await loadState();
  vscode.window.showInformationMessage("Failed worker setup retry started");
}

async function startTraining(): Promise<void> {
  const api = await ensureClient();
  await api.startTraining();
  await loadState();
  vscode.window.showInformationMessage("Training started");
}

async function stopTraining(): Promise<void> {
  const api = await ensureClient();
  await api.stopTraining();
  await loadState();
  vscode.window.showInformationMessage("Training stop requested");
}

async function openResults(node?: unknown): Promise<void> {
  const api = await ensureClient();
  const summary = resultSummaryFromNode(node) ?? (await api.state()).last_summary;
  if (!summary) {
    throw new Error("No result summary is available yet");
  }
  const resultPath = await api.resultPath(summary.job_id);
  const summaryPath = path.join(resultPath, "summary.json");
  if (await exists(summaryPath)) {
    await vscode.window.showTextDocument(vscode.Uri.file(summaryPath), { preview: false });
  }
  await vscode.commands.executeCommand("revealFileInOS", vscode.Uri.file(resultPath));
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
      await loadState().catch(() => undefined);
      if (event.type === "command.rejected" || event.type.endsWith("_failed") || event.type === "job.failed") {
        const message = eventError(event)
          ? `LDGCC: ${event.type} - ${eventError(event)}`
          : `LDGCC: ${event.type}`;
        studio.addError(`LDGCC: ${event.type}`, eventError(event) ?? message);
        studio.setMessage(message);
        vscode.window.showErrorMessage(message);
      }
      if (event.type === "job.completed") {
        const message = "LDGCC job completed";
        studio.markTrainingStopped();
        studio.setMessage(message);
        vscode.window.showInformationMessage(message);
      }
      if (event.type === "job.failed") {
        studio.markTrainingStopped();
      }
    },
    () => undefined,
  );
}

interface ParsedProjectSpec {
  entrypoint?: string;
  datasetTrain?: string;
  datasetType?: string;
  workersCount?: number;
  outputs: string[];
  precision?: string;
  compressionType?: string;
  compressionMode?: string;
  topK?: string;
  errorFeedback?: boolean;
  warmupSteps?: number;
}

async function validateProjectBeforePrepare(projectRoot: string, onlineWorkers: number): Promise<void> {
  const specPath = await findSpecPath(projectRoot);
  const spec = parseProjectSpec(await fs.readFile(specPath, "utf8"));
  const errors: string[] = [];
  if (!spec.entrypoint) {
    errors.push("entrypoint is required");
  }
  if (!spec.datasetTrain) {
    errors.push("dataset.train is required");
  }
  if (!spec.workersCount || spec.workersCount <= 0) {
    errors.push("workers.count must be greater than zero");
  }
  const datasetType = spec.datasetType || "jsonl";
  if (datasetType !== "jsonl" && datasetType !== "image_folder" && datasetType !== "yolo_split") {
    errors.push("dataset.type must be jsonl, image_folder, or yolo_split");
  }
  if (spec.precision && spec.precision !== "fp32" && spec.precision !== "fp16") {
    errors.push("communication.precision must be fp32 or fp16");
  }
  if (spec.compressionType && spec.compressionType !== "none" && spec.compressionType !== "topk") {
    errors.push("communication.compression.type must be none or topk");
  }
  if (spec.compressionType === "topk") {
    if (spec.compressionMode && spec.compressionMode !== "global" && spec.compressionMode !== "per_layer") {
      errors.push("communication.compression.mode must be global or per_layer");
    }
    if (spec.topK && !isValidPercent(spec.topK)) {
      errors.push("communication.compression.top_k must be a percent string > 0% and <= 100%");
    }
    if (spec.errorFeedback !== true) {
      errors.push("communication.compression.error_feedback must be true for topk");
    }
    if (spec.warmupSteps !== undefined && spec.warmupSteps < 0) {
      errors.push("communication.compression.warmup_steps must be non-negative");
    }
  }
  for (const [label, value] of [
    ["entrypoint", spec.entrypoint],
    ["dataset.train", spec.datasetTrain],
  ] as const) {
    if (value && !isSafeRelativePath(value)) {
      errors.push(`${label} must be a relative path inside the project`);
    }
  }
  for (const output of spec.outputs) {
    if (!isSafeRelativePath(output)) {
      errors.push(`output path "${output}" must be relative and stay inside the project`);
    }
  }
  if (spec.entrypoint && isSafeRelativePath(spec.entrypoint) && !(await exists(path.join(projectRoot, spec.entrypoint)))) {
    errors.push(`entrypoint is not readable: ${spec.entrypoint}`);
  }
  if (spec.datasetTrain && isSafeRelativePath(spec.datasetTrain) && !(await exists(path.join(projectRoot, spec.datasetTrain)))) {
    errors.push(`dataset.train is not readable: ${spec.datasetTrain}`);
  }
  if (spec.workersCount && onlineWorkers < spec.workersCount) {
    errors.push(`workers.count is ${spec.workersCount}, but only ${onlineWorkers} paired Worker(s) are online`);
  }
  if (errors.length > 0) {
    const detail = [`${path.basename(specPath)} validation failed:`, ...errors.map((error) => `- ${error}`)].join("\n");
    studio.addError("LDGCC: Project validation failed", detail);
    throw new Error(detail);
  }
}

async function findSpecPath(projectRoot: string): Promise<string> {
  const yml = path.join(projectRoot, "ldgcc.yml");
  if (await exists(yml)) {
    return yml;
  }
  const yaml = path.join(projectRoot, "ldgcc.yaml");
  if (await exists(yaml)) {
    return yaml;
  }
  throw new Error("ldgcc.yml or ldgcc.yaml is required in the training project folder");
}

function parseProjectSpec(text: string): ParsedProjectSpec {
  const spec: ParsedProjectSpec = { outputs: [] };
  let section = "";
  for (const rawLine of text.split(/\r?\n/)) {
    const line = stripComment(rawLine);
    if (!line.trim()) {
      continue;
    }
    const indent = line.length - line.trimStart().length;
    const trimmed = line.trim();
    if (section === "outputs" && trimmed.startsWith("- ")) {
      const value = unquote(trimmed.slice(2).trim());
      if (value) {
        spec.outputs.push(value);
      }
      continue;
    }
    const separator = trimmed.indexOf(":");
    if (separator < 0) {
      throw new Error(`invalid ldgcc.yml line: ${rawLine}`);
    }
    const key = trimmed.slice(0, separator).trim();
    const value = unquote(trimmed.slice(separator + 1).trim());
    if (indent === 0 && !value) {
      section = key;
      continue;
    }
    if (indent === 2 && section === "communication" && key === "compression" && !value) {
      section = "communication.compression";
      continue;
    }
    if (indent === 0) {
      section = "";
    }
    switch (true) {
      case section === "" && key === "entrypoint":
        spec.entrypoint = value;
        break;
      case section === "dataset" && key === "train":
        spec.datasetTrain = value;
        break;
      case section === "dataset" && key === "type":
        spec.datasetType = value;
        break;
      case section === "workers" && key === "count": {
        const count = Number(value);
        if (!Number.isInteger(count)) {
          throw new Error("workers.count must be a number");
        }
        spec.workersCount = count;
        break;
      }
      case section === "communication" && key === "precision":
        spec.precision = value;
        break;
      case section === "communication" && key === "compression":
        spec.compressionType = value;
        spec.errorFeedback = true;
        break;
      case section === "communication.compression" && key === "type":
        spec.compressionType = value;
        spec.errorFeedback = true;
        break;
      case section === "communication.compression" && key === "mode":
        spec.compressionMode = value;
        break;
      case section === "communication.compression" && key === "top_k":
        spec.topK = value;
        break;
      case section === "communication.compression" && key === "error_feedback":
        spec.errorFeedback = value === "true";
        break;
      case section === "communication.compression" && key === "warmup_steps": {
        const steps = Number(value);
        if (!Number.isInteger(steps)) {
          throw new Error("communication.compression.warmup_steps must be a number");
        }
        spec.warmupSteps = steps;
        break;
      }
      default:
        break;
    }
  }
  return spec;
}

function stripComment(line: string): string {
  const index = line.indexOf("#");
  return index >= 0 ? line.slice(0, index) : line;
}

function unquote(value: string): string {
  return value.replace(/^["']|["']$/g, "").trim();
}

function isSafeRelativePath(value: string): boolean {
  if (!value || path.isAbsolute(value)) {
    return false;
  }
  const normalized = path.normalize(value);
  return normalized !== "." && !normalized.startsWith("..") && !path.isAbsolute(normalized);
}

function isValidPercent(value: string): boolean {
  if (!value.endsWith("%")) {
    return false;
  }
  const percent = Number(value.slice(0, -1));
  return Number.isFinite(percent) && percent > 0 && percent <= 100;
}

async function exists(filePath: string): Promise<boolean> {
  try {
    await fs.access(filePath);
    return true;
  } catch {
    return false;
  }
}

function eventError(event: MasterEvent): string | undefined {
  if (!event.data || typeof event.data !== "object") {
    return undefined;
  }
  const data = event.data as Record<string, unknown>;
  if (typeof data.error === "string") {
    return data.error;
  }
  if (typeof data.reason === "string") {
    return data.reason;
  }
  return undefined;
}

async function handleViewAction(action: string, payload?: unknown): Promise<void> {
  switch (action) {
    case "startMaster":
      return startMaster();
    case "stopMaster":
      return stopMaster();
    case "refresh":
      return refresh();
    case "discoverWorkers":
      return discoverWorkers();
    case "pairWorker":
      return pairWorker(workerPayloadToDiscovered(payload));
    case "checkNetwork":
      return checkNetwork();
    case "prepareJob":
      return prepareJob();
    case "setupWorkers":
      return setupWorkers();
    case "retrySetup":
      return retrySetup();
    case "startTraining":
      return startTraining();
    case "stopTraining":
      return stopTraining();
    case "openResults":
      return openResults();
    case "clearErrors":
      studio.clearErrors();
      return;
    default:
      throw new Error(`Unsupported LDGCC action: ${action}`);
  }
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
      description: worker.address,
      detail: worker.request_status ?? worker.pairing_status,
      worker,
    })),
    { placeHolder: "Select an LDGCC Worker to pair" },
  );
  return picked?.worker;
}

function workerFromNode(value: unknown): DiscoveredWorker | undefined {
  const candidate = value as Partial<DiscoveredWorker> | undefined;
  if (candidate?.id) {
    return candidate as DiscoveredWorker;
  }
  return undefined;
}

function resultSummaryFromNode(value: unknown): JobSummary | undefined {
  const candidate = value as Partial<JobSummary> | undefined;
  if (candidate?.job_id) {
    return candidate as JobSummary;
  }
  return undefined;
}

function workerPayloadToDiscovered(payload: unknown): DiscoveredWorker | undefined {
  const candidate = payload as { id?: unknown } | undefined;
  if (typeof candidate?.id === "string") {
    return {
      id: candidate.id,
      instance: candidate.id,
      address: "",
      pairing_status: "",
    };
  }
  return undefined;
}
