import { request } from "node:http";
import { URL } from "node:url";
import { MasterEvent, MasterSession, MasterState } from "./types";

export class MasterClient {
  private eventRequest?: ReturnType<typeof request>;

  constructor(private readonly session: MasterSession) {}

  async health(): Promise<{ status: string; version: string }> {
    return this.get("/health");
  }

  async state(): Promise<MasterState> {
    return this.get("/state");
  }

  async discoverWorkers(): Promise<void> {
    await this.post("/discovery/start");
  }

  async pairWorker(id: string): Promise<void> {
    await this.post(`/workers/${encodeURIComponent(id)}/pair`);
  }

  async prepareJob(projectRoot: string): Promise<void> {
    await this.post("/jobs/prepare", { project_root: projectRoot });
  }

  async setupWorkers(): Promise<void> {
    await this.post("/jobs/setup");
  }

  async retrySetup(): Promise<void> {
    await this.post("/jobs/setup/retry");
  }

  async startTraining(): Promise<void> {
    await this.post("/jobs/start");
  }

  async stopTraining(): Promise<void> {
    await this.post("/jobs/stop");
  }

  async shutdown(): Promise<void> {
    await this.post("/shutdown");
  }

  async resultPath(jobID: string): Promise<string> {
    const response = await this.get<{ path: string }>(`/results/${encodeURIComponent(jobID)}`);
    return response.path;
  }

  subscribe(onEvent: (event: MasterEvent) => void, onError: (error: Error) => void): void {
    this.closeEvents();
    const url = this.url("/events");
    const req = request(
      url,
      {
        method: "GET",
        headers: this.headers(),
      },
      (res) => {
        if (res.statusCode !== 200) {
          onError(new Error(`events stream failed with HTTP ${res.statusCode}`));
          res.resume();
          return;
        }
        res.setEncoding("utf8");
        let buffer = "";
        res.on("data", (chunk) => {
          buffer += chunk;
          let boundary = buffer.indexOf("\n\n");
          while (boundary >= 0) {
            const frame = buffer.slice(0, boundary);
            buffer = buffer.slice(boundary + 2);
            const data = frame
              .split("\n")
              .filter((line) => line.startsWith("data:"))
              .map((line) => line.slice(5).trimStart())
              .join("\n");
            if (data) {
              try {
                onEvent(JSON.parse(data) as MasterEvent);
              } catch (error) {
                onError(error instanceof Error ? error : new Error(String(error)));
              }
            }
            boundary = buffer.indexOf("\n\n");
          }
        });
      },
    );
    req.on("error", onError);
    req.end();
    this.eventRequest = req;
  }

  closeEvents(): void {
    this.eventRequest?.destroy();
    this.eventRequest = undefined;
  }

  private get<T>(path: string): Promise<T> {
    return this.fetchJSON<T>("GET", path);
  }

  private post<T = unknown>(path: string, body?: unknown): Promise<T> {
    return this.fetchJSON<T>("POST", path, body);
  }

  private fetchJSON<T>(method: string, path: string, body?: unknown): Promise<T> {
    const payload = body === undefined ? undefined : Buffer.from(JSON.stringify(body));
    return new Promise<T>((resolve, reject) => {
      const req = request(
        this.url(path),
        {
          method,
          headers: {
            ...this.headers(),
            ...(payload ? { "Content-Type": "application/json", "Content-Length": String(payload.length) } : {}),
          },
        },
        (res) => {
          const chunks: Buffer[] = [];
          res.on("data", (chunk) => chunks.push(Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk)));
          res.on("end", () => {
            const text = Buffer.concat(chunks).toString("utf8");
            const value = text ? JSON.parse(text) : {};
            if (res.statusCode && res.statusCode >= 400) {
              const message = typeof value?.error === "string" ? value.error : `HTTP ${res.statusCode}`;
              reject(new Error(message));
              return;
            }
            resolve(value as T);
          });
        },
      );
      req.on("error", reject);
      if (payload) {
        req.write(payload);
      }
      req.end();
    });
  }

  private headers(): Record<string, string> {
    return {
      Authorization: `Bearer ${this.session.session_token}`,
    };
  }

  private url(path: string): URL {
    return new URL(path, this.session.address.endsWith("/") ? this.session.address : `${this.session.address}/`);
  }
}
