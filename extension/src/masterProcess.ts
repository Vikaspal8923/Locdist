import { ChildProcessWithoutNullStreams, spawn } from "node:child_process";
import { randomBytes } from "node:crypto";
import { promises as fs } from "node:fs";
import { join, resolve } from "node:path";
import * as vscode from "vscode";
import { MasterSession } from "./types";

export class MasterProcess {
  private child?: ChildProcessWithoutNullStreams;
  private session?: MasterSession;

  constructor(private readonly context: vscode.ExtensionContext) {}

  async ensureStarted(): Promise<MasterSession> {
    const existing = await this.readSession();
    if (existing && (await this.isHealthy(existing))) {
      this.session = existing;
      return existing;
    }

    await fs.mkdir(this.dataDir(), { recursive: true });
    const token = randomBytes(32).toString("hex");
    const configuredBinary = this.config("master.binaryPath");
    const bundledBinary = configuredBinary ? undefined : await this.bundledMasterBinary();
    const binary = configuredBinary || bundledBinary;
    const repoRoot = this.repoRoot();
    const configPath = binary ? await this.ensureMasterConfig() : join(repoRoot, "master", "master_config.json");
    const args = [
      "--config",
      configPath,
      "--data-dir",
      this.dataDir(),
      "--app-host",
      "127.0.0.1",
      "--app-port",
      "0",
      "--session-token",
      token,
    ];

    if (binary) {
      this.child = spawn(resolve(binary), args, { cwd: this.dataDir() });
    } else {
      this.child = spawn("go", ["run", "./cmd/master", ...args], { cwd: join(repoRoot, "master") });
    }

    this.child.on("exit", () => {
      this.child = undefined;
      this.session = undefined;
    });

    const session = await this.waitForSession();
    this.session = session;
    return session;
  }

  async stop(): Promise<void> {
    this.child?.kill();
    this.child = undefined;
    this.session = undefined;
  }

  currentSession(): MasterSession | undefined {
    return this.session;
  }

  sessionPath(): string {
    return join(this.dataDir(), "master-session.json");
  }

  dataDir(): string {
    const configured = this.config("master.dataDir");
    return configured ? resolve(configured) : this.context.globalStorageUri.fsPath;
  }

  repoRoot(): string {
    const configured = this.config("master.sourceRoot");
    if (configured) {
      return resolve(configured);
    }
    return resolve(this.context.extensionPath, "..");
  }

  private async waitForSession(): Promise<MasterSession> {
    const deadline = Date.now() + 20_000;
    let lastError: Error | undefined;
    while (Date.now() < deadline) {
      try {
        const session = await this.readSession();
        if (session && (await this.isHealthy(session))) {
          return session;
        }
      } catch (error) {
        lastError = error instanceof Error ? error : new Error(String(error));
      }
      await new Promise((resolveWait) => setTimeout(resolveWait, 250));
    }
    throw lastError ?? new Error("Master did not write a usable session file");
  }

  private async readSession(): Promise<MasterSession | undefined> {
    try {
      const raw = await fs.readFile(this.sessionPath(), "utf8");
      return JSON.parse(raw) as MasterSession;
    } catch (error) {
      const code = typeof error === "object" && error && "code" in error ? String(error.code) : "";
      if (code === "ENOENT") {
        return undefined;
      }
      throw error;
    }
  }

  private async ensureMasterConfig(): Promise<string> {
    const path = join(this.dataDir(), "master_config.json");
    try {
      await fs.access(path);
      return path;
    } catch (error) {
      const code = typeof error === "object" && error && "code" in error ? String(error.code) : "";
      if (code !== "ENOENT") {
        throw error;
      }
    }
    await fs.writeFile(
      path,
      JSON.stringify(
        {
          master_id: "master-local",
          master_name: "LDGCC Master",
          host: "127.0.0.1",
          grpc_port: "60051",
          app_host: "127.0.0.1",
          app_port: "0",
          pairing_path: "master_pairings.json",
        },
        null,
        2,
      ) + "\n",
      { mode: 0o600 },
    );
    return path;
  }

  private async bundledMasterBinary(): Promise<string | undefined> {
    const candidate = join(this.context.extensionPath, "bin", `${process.platform}-${process.arch}`, executableName("ldgcc-master"));
    try {
      await fs.access(candidate);
      return candidate;
    } catch (error) {
      const code = typeof error === "object" && error && "code" in error ? String(error.code) : "";
      if (code === "ENOENT") {
        return undefined;
      }
      throw error;
    }
  }

  private async isHealthy(session: MasterSession): Promise<boolean> {
    try {
      const { MasterClient } = await import("./masterClient");
      await new MasterClient(session).health();
      return true;
    } catch {
      return false;
    }
  }

  private config(key: string): string {
    return vscode.workspace.getConfiguration("ldgcc").get<string>(key, "").trim();
  }
}

function executableName(name: string): string {
  return process.platform === "win32" ? `${name}.exe` : name;
}
