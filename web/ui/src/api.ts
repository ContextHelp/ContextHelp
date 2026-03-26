// Thin API client — wraps fetch with base path and error handling.

const BASE = '/api/v1';

async function req<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    headers: { 'Content-Type': 'application/json', ...init?.headers },
    ...init,
  });
  if (!res.ok) {
    const body = await res.json().catch(() => ({}));
    const msg = (body as { message?: string }).message ?? res.statusText;
    throw new Error(`${res.status}: ${msg}`);
  }
  // 204 No Content
  if (res.status === 204) return undefined as unknown as T;
  return res.json() as Promise<T>;
}

// ── Objects ───────────────────────────────────────────────────────────────────
import type { KnowledgeObject, Job, Entity, PagedResponse } from './types';

export const objects = {
  list: (params?: { type?: string; limit?: number; offset?: number }) =>
    req<PagedResponse<KnowledgeObject>>(
      `/objects?${new URLSearchParams(
        Object.fromEntries(
          Object.entries(params ?? {}).filter(([, v]) => v !== undefined).map(([k, v]) => [k, String(v)])
        )
      )}`
    ),
  get: (id: string) => req<KnowledgeObject>(`/objects/${id}`),
  delete: (id: string) => req<void>(`/objects/${id}`, { method: 'DELETE' }),
};

// ── Jobs ──────────────────────────────────────────────────────────────────────
export const jobs = {
  list: (params?: { status?: string; limit?: number; offset?: number }) =>
    req<PagedResponse<Job>>(
      `/jobs?${new URLSearchParams(
        Object.fromEntries(
          Object.entries(params ?? {}).filter(([, v]) => v !== undefined).map(([k, v]) => [k, String(v)])
        )
      )}`
    ),
  get: (id: string) => req<Job>(`/jobs/${id}`),
  retry: (id: string) => req<Job>(`/jobs/${id}/retry`, { method: 'POST' }),
};

// ── Search ────────────────────────────────────────────────────────────────────
export const search = {
  query: (q: string, params?: { limit?: number; offset?: number }) =>
    req<PagedResponse<KnowledgeObject>>(
      `/search?q=${encodeURIComponent(q)}&${new URLSearchParams(
        Object.fromEntries(
          Object.entries(params ?? {}).filter(([, v]) => v !== undefined).map(([k, v]) => [k, String(v)])
        )
      )}`
    ),
};

// ── Entities ──────────────────────────────────────────────────────────────────
export const entities = {
  list: (params?: { namespace?: string; limit?: number; offset?: number }) =>
    req<PagedResponse<Entity>>(
      `/entities?${new URLSearchParams(
        Object.fromEntries(
          Object.entries(params ?? {}).filter(([, v]) => v !== undefined).map(([k, v]) => [k, String(v)])
        )
      )}`
    ),
  get: (slug: string) => req<Entity>(`/entities/${slug}`),
};

// ── Registries ────────────────────────────────────────────────────────────────
export const registries = {
  list: () =>
    req<{ registries: unknown[]; total: number }>(`/steps/registries`),
};

// ── System ────────────────────────────────────────────────────────────────────
export const system = {
  health: () => req<{ status: string }>(`/../../health`),
};
