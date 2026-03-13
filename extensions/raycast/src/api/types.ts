// Mirror of internal/storage/types.go

export interface Section {
  title: string;
  content: string;
  order: number;
  metadata?: Record<string, unknown>;
}

export interface Tag {
  label: string;
  weight?: number;
  source?: string;
}

export interface Decision {
  title: string;
  status: string;
  impact: string;
}

export interface Task {
  title: string;
  status: string;
}

export interface KnowledgeObject {
  id: string;
  type: string;
  subtype?: string;
  raw_content: string;
  text_content?: string;
  content_type?: string;
  metadata?: Record<string, unknown>;
  summaries?: string[];
  sections?: Section[];
  tags?: Tag[];
  mentions?: string[];
  decisions?: Decision[];
  tasks?: Task[];
  // embeddings omitted — float32 array not useful in UI
  pipeline?: string;
  source?: string;
  registry_influences?: string[];
  plugins?: Record<string, unknown>;
  content_hash?: string;
  reinforcement_count?: number;
  last_reinforced_at?: string;
  fts_indexed?: boolean;
  vector_indexed?: boolean;
  // inbox fields (Plan 7)
  status?: string;
  inbox_note?: string;
  created_at: string;
  updated_at: string;
}

export type JobStatus = "pending" | "running" | "completed" | "failed";

export interface Job {
  id: string;
  type: string;
  status: JobStatus;
  payload?: string;
  pipeline?: string;
  source?: string;
  result_id?: string;
  error?: string;
  retry_count?: number;
  max_retries?: number;
  created_at: string;
  updated_at: string;
  started_at?: string;
  completed_at?: string;
}

// Request/response types

export interface AnalyzeRequest {
  content: string;
  type?: string;
  pipeline?: string;
  hints?: string[];
  source?: string;
}

export interface AnalyzeResponse {
  job_id: string;
}

export interface ListObjectsResponse {
  data: KnowledgeObject[];
  total: number;
}

export interface ListJobsResponse {
  jobs: Job[];
  total: number;
}

export interface SearchResponse {
  objects: KnowledgeObject[];
}

// Inbox (Plan 7 — included for forward compatibility)
export interface InboxRequest {
  content: string;
  type?: string;
  note?: string;
  source?: string;
  tags?: string[];
}

// Error class for server-offline detection
export class ServerOfflineError extends Error {
  constructor(message = "dpkms server is not running") {
    super(message);
    this.name = "ServerOfflineError";
  }
}
