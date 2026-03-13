import { apiFetch } from "./client";
import {
  AnalyzeRequest,
  AnalyzeResponse,
  InboxRequest,
  KnowledgeObject,
  ListJobsResponse,
  ListObjectsResponse,
  SearchResponse,
} from "./types";

// Health

export function checkHealth(): Promise<{ status: string }> {
  return apiFetch("/health");
}

// Objects

export function listObjects(params?: Record<string, string>): Promise<ListObjectsResponse> {
  const qs = params ? `?${new URLSearchParams(params).toString()}` : "";
  return apiFetch(`/api/v1/objects${qs}`);
}

export function getObject(id: string): Promise<KnowledgeObject> {
  return apiFetch(`/api/v1/objects/${encodeURIComponent(id)}`);
}

// Search

export function searchObjects(q: string): Promise<SearchResponse> {
  return apiFetch(`/api/v1/search?q=${encodeURIComponent(q)}`);
}

// Analyze / Ingest

export function analyzeContent(req: AnalyzeRequest): Promise<AnalyzeResponse> {
  return apiFetch("/api/v1/analyze", {
    method: "POST",
    body: JSON.stringify(req),
  });
}

// Jobs

export function listJobs(params?: Record<string, string>): Promise<ListJobsResponse> {
  const qs = params ? `?${new URLSearchParams(params).toString()}` : "";
  return apiFetch(`/api/v1/jobs${qs}`);
}

export function retryJob(id: string): Promise<void> {
  return apiFetch(`/api/v1/jobs/${encodeURIComponent(id)}/retry`, {
    method: "POST",
  });
}

// Inbox (Plan 7 — forward compatible)

export function captureToInbox(req: InboxRequest): Promise<KnowledgeObject> {
  return apiFetch("/api/v1/inbox", {
    method: "POST",
    body: JSON.stringify(req),
  });
}

export function listInbox(params?: Record<string, string>): Promise<ListObjectsResponse> {
  const qs = params ? `?${new URLSearchParams(params).toString()}` : "";
  return apiFetch(`/api/v1/inbox${qs}`);
}
