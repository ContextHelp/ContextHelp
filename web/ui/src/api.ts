// Thin API client — wraps fetch with base path and error handling.

const BASE = '/api/v1';

/** A non-2xx answer; `code` and the message come from dpkms's error envelope. */
export class ApiError extends Error {
  readonly status: number;
  readonly code: string | undefined;

  constructor(status: number, code: string | undefined, message: string) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.code = code;
  }
}

export const AUTH_REQUIRED_MESSAGE =
  'Authentication required: this dpkms instance only answers authenticated clients, ' +
  'and the web UI cannot sign in yet. Use the ctxt CLI (with server.token configured) ' +
  'or open the UI of a private instance.';

/** The instance answered 401: it requires credentials the web UI does not send. */
export class AuthRequiredError extends ApiError {
  constructor(code: string | undefined) {
    super(401, code, AUTH_REQUIRED_MESSAGE);
    this.name = 'AuthRequiredError';
  }
}

export function isAuthRequired(err: unknown): err is AuthRequiredError {
  return err instanceof AuthRequiredError;
}

type ErrorEnvelope = { error?: { code?: string; message?: string } };

async function req<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    ...init,
    headers: { 'Content-Type': 'application/json', ...init?.headers },
  });
  if (!res.ok) {
    const body = (await res.json().catch(() => ({}))) as ErrorEnvelope;
    if (res.status === 401) throw new AuthRequiredError(body.error?.code);
    const msg = body.error?.message ?? res.statusText;
    throw new ApiError(res.status, body.error?.code, `${res.status}: ${msg}`);
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
