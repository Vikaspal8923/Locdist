import * as vscode from "vscode";
import { MasterState } from "./types";

type NodeKind = "status" | "discovered" | "worker" | "job" | "results" | "empty";

export class ClusterNode extends vscode.TreeItem {
  constructor(
    readonly kind: NodeKind,
    label: string,
    description?: string,
    collapsibleState = vscode.TreeItemCollapsibleState.None,
    readonly value?: unknown,
  ) {
    super(label, collapsibleState);
    this.description = description;
    this.contextValue = kind === "discovered" ? "ldgcc.discoveredWorker" : kind === "results" ? "ldgcc.results" : kind;
  }
}

export class ClusterTreeDataProvider implements vscode.TreeDataProvider<ClusterNode> {
  private readonly changed = new vscode.EventEmitter<ClusterNode | undefined>();
  readonly onDidChangeTreeData = this.changed.event;
  private state?: MasterState;
  private connected = false;

  setConnected(connected: boolean): void {
    this.connected = connected;
    this.changed.fire(undefined);
  }

  setState(state: MasterState): void {
    this.state = state;
    this.connected = true;
    this.changed.fire(undefined);
  }

  refresh(): void {
    this.changed.fire(undefined);
  }

  getTreeItem(element: ClusterNode): vscode.TreeItem {
    return element;
  }

  getChildren(element?: ClusterNode): ClusterNode[] {
    if (element) {
      return this.childrenFor(element);
    }
    if (!this.connected) {
      return [new ClusterNode("status", "Master stopped", "run LDGCC: Start Master")];
    }
    const master = this.state?.master;
    return [
      new ClusterNode("status", "Master", master ? `${master.status} ${master.version}` : "connecting"),
      new ClusterNode("discovered", "Discovered Workers", `${this.state?.discovered_workers?.length ?? 0}`, vscode.TreeItemCollapsibleState.Expanded),
      new ClusterNode("worker", "Registered Workers", `${this.state?.workers?.length ?? 0}`, vscode.TreeItemCollapsibleState.Expanded),
      new ClusterNode("job", "Current Job", this.state?.job?.status ?? "none", vscode.TreeItemCollapsibleState.Expanded),
      new ClusterNode("results", "Last Results", this.state?.last_summary?.job_id ?? "none", vscode.TreeItemCollapsibleState.None, this.state?.last_summary),
    ];
  }

  private childrenFor(element: ClusterNode): ClusterNode[] {
    if (element.label === "Discovered Workers") {
      const workers = this.state?.discovered_workers ?? [];
      return workers.length
        ? workers.map((worker) => new ClusterNode("discovered", worker.instance, `${worker.pairing_status}${worker.request_status ? ` / ${worker.request_status}` : ""}`, undefined, worker))
        : [new ClusterNode("empty", "No discovered workers")];
    }
    if (element.label === "Registered Workers") {
      const workers = this.state?.workers ?? [];
      return workers.length
        ? workers.map((worker) => new ClusterNode("worker", worker.worker_id, `${worker.availability} / ${worker.status}`, undefined, worker))
        : [new ClusterNode("empty", "No registered workers")];
    }
    if (element.label === "Current Job") {
      const job = this.state?.job;
      if (!job) {
        return [new ClusterNode("empty", "No active job")];
      }
      const children = [
        new ClusterNode("job", "Job ID", job.job_id),
        new ClusterNode("job", "Project", job.name),
        new ClusterNode("job", "Entrypoint", job.entrypoint),
        new ClusterNode("job", "Dataset", job.dataset_path),
      ];
      for (const [workerID, setup] of Object.entries(job.setup ?? {})) {
        children.push(new ClusterNode("job", `Setup ${workerID}`, setup.status));
      }
      for (const [workerID, run] of Object.entries(job.run ?? {})) {
        children.push(new ClusterNode("job", `Run ${workerID}`, run.status));
      }
      return children;
    }
    return [];
  }
}
