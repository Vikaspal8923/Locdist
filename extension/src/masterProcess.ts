import { ChildProcessWithoutNullStreams, spawn } from "node:child_process";
import { randomBytes } from "node:crypto";
import { promises as fs } from "node:fs";
import { createServer } from "node:net";
import { join, resolve } from "node:path";
import * as vscode from "vscode";
import { MasterSession } from "./types";

export class MasterProcess {
  private child?: ChildProcessWithoutNullStreams;
  private session?: MasterSession;
  private startupOutput = "";
  private startupFailure?: Error;
  private exitWaiter?: Promise<void>;

  constructor(private readonly context: vscode.ExtensionContext) {}

  async ensureStarted(): Promise<MasterSession> {
    this.startupOutput = "";
    this.startupFailure = undefined;
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

    this.child.stdout.on("data", (chunk) => {
      this.appendStartupOutput(chunk);
    });
    this.child.stderr.on("data", (chunk) => {
      this.appendStartupOutput(chunk);
    });
    this.child.on("error", (error) => {
      this.startupFailure = error instanceof Error ? error : new Error(String(error));
    });

    this.child.on("exit", (code, signal) => {
      if (!this.session) {
        this.startupFailure = new Error(this.describeExit(code, signal));
      }
      this.child = undefined;
      this.session = undefined;
    });
    this.exitWaiter = new Promise((resolveExit) => {
      this.child?.once("exit", () => resolveExit());
    });

    const session = await this.waitForSession();
    this.session = session;
    return session;
  }

  async stop(): Promise<void> {
    const child = this.child;
    const exitWaiter = this.exitWaiter;
    if (child && child.exitCode === null && !child.killed) {
      child.kill();
      if (exitWaiter) {
        await Promise.race([
          exitWaiter,
          new Promise((resolveWait) => setTimeout(resolveWait, 5_000)),
        ]);
      }
    }
    this.child = undefined;
    this.exitWaiter = undefined;
    this.session = undefined;
  }

  async resetLocalState(): Promise<void> {
    await this.stop();
    await fs.rm(this.dataDir(), { recursive: true, force: true });
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
    throw this.buildStartupError(lastError);
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
    const grpcPort = await this.reserveLoopbackPort();
    let current: Record<string, unknown> = {};
    try {
      current = JSON.parse(await fs.readFile(path, "utf8")) as Record<string, unknown>;
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
          ...current,
          master_id: typeof current.master_id === "string" && current.master_id ? current.master_id : "master-local",
          master_name: typeof current.master_name === "string" && current.master_name ? current.master_name : "LDGCC Master",
          host: "127.0.0.1",
          grpc_port: grpcPort,
          app_host: "127.0.0.1",
          app_port: "0",
          pairing_path: typeof current.pairing_path === "string" && current.pairing_path ? current.pairing_path : "master_pairings.json",
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

  private reserveLoopbackPort(): Promise<string> {
    return new Promise((resolvePort, reject) => {
      const server = createServer();
      server.once("error", reject);
      server.listen(0, "127.0.0.1", () => {
        const address = server.address();
        if (!address || typeof address === "string") {
          server.close(() => reject(new Error("Failed to reserve a loopback port for Master gRPC")));
          return;
        }
        const { port } = address;
        server.close((error) => {
          if (error) {
            reject(error);
            return;
          }
          resolvePort(String(port));
        });
      });
    });
  }

  private appendStartupOutput(chunk: string | Buffer): void {
    const text = Buffer.isBuffer(chunk) ? chunk.toString("utf8") : chunk;
    this.startupOutput = `${this.startupOutput}${text}`.slice(-8000);
  }

  private describeExit(code: number | null, signal: NodeJS.Signals | null): string {
    if (signal) {
      return `Master process exited from signal ${signal}`;
    }
    if (code !== null) {
      return `Master process exited with code ${code}`;
    }
    return "Master process exited before becoming ready";
  }

  private buildStartupError(lastError?: Error): Error {
    const parts = [
      this.startupFailure?.message,
      lastError?.message,
      "Master did not write a usable session file",
    ].filter((value, index, values): value is string => Boolean(value) && values.indexOf(value) === index);
    const output = this.startupOutput.trim();
    if (output) {
      parts.push(`Recent master output:\n${output}`);
    }
    return new Error(parts.join("\n\n"));
  }
}

function executableName(name: string): string {
  return process.platform === "win32" ? `${name}.exe` : name;
}
