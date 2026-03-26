// API types matching Go storage/types

export type JobStatus = 'pending' | 'running' | 'completed' | 'failed';

export interface KnowledgeObject {
  id: string;
  type: string;
  subtype?: string;
  source: string;
  raw_content?: string;
  text_content?: string;
  pipeline?: string;
  profile_id?: string;
  created_at: string;
  updated_at: string;
}

export interface Job {
  id: string;
  type: string;
  status: JobStatus;
  payload?: string;
  pipeline?: string;
  source?: string;
  result_id?: string;
  error?: string;
  retry_count: number;
  max_retries: number;
  created_at: string;
  updated_at: string;
  started_at?: string;
  completed_at?: string;
}

export interface Entity {
  slug: string;
  title: string;
  description?: string;
  namespace?: string;
  aliases?: string[];
  content_status?: string;
  version_hash?: string;
  registry_url?: string;
  created_at: string;
  updated_at: string;
}

export interface Registry {
  url: string;
  name?: string;
  description?: string;
  entity_count?: number;
  last_synced_at?: string;
  created_at: string;
  updated_at: string;
}

export interface PagedResponse<T> {
  data: T[];
  total: number;
}
