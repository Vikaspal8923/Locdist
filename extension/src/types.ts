export interface MasterSession {
  pid: number;
  host: string;
  port: string;
  address: string;
  version: string;
  session_token: string;
  started_at: string;
}

export interface DiscoveredWorker {
  instance: string;
  address: string;
  pairing_status: string;
  request_status?: string;
  error?: string;
}

export interface RegisteredWorker {
  worker_id: string;
  host: string;
  grpc_port: string;
  status: string;
  job_id?: string;
  availability: string;
  last_seen: string;
}

export interface JobWorkerState {
  status: string;
  error_message?: string;
  log_path?: string;
}

export interface JobState {
  job_id: string;
  expected_workers: number;
  name: string;
  project_root: string;
  entrypoint: string;
  dataset_path: string;
  outputs?: string[];
  workers: Array<{ worker_id: string; host: string; grpc_port: string }>;
  setup?: Record<string, JobWorkerState>;
  run?: Record<string, JobWorkerState>;
  status: string;
}

export interface JobSummary {
  job_id: string;
  status: string;
  reason?: string;
  finished_at: string;
}

export interface MasterState {
  master: {
    status: string;
    version: string;
  };
  discovered_workers: DiscoveredWorker[];
  workers?: RegisteredWorker[];
  job?: JobState;
  last_summary?: JobSummary;
}

export interface MasterEvent {
  type: string;
  time: string;
  data?: unknown;
}
