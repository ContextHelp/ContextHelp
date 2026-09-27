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
  'Sign-in required: this dpkms instance only answers signed-in clients. ' +
  'Run `ctxt ui open` on a machine whose ctxt has a token for this instance; ' +
  'it opens a single-use sign-in link.';

/**
 * Sent with every request. A browser only adds a custom header to a
 * cross-origin request after a CORS preflight, which dpkms never
 * approves, so the server takes it as proof a write comes from its own
 * pages. Cookie-authenticated writes without it are refused.
 */
export const CSRF_HEADER = 'X-Ctxt-CSRF';

/** The instance answered 401: no session cookie, or it ended. */
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
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', [CSRF_HEADER]: '1', ...init?.headers },
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

// ── Browser session ───────────────────────────────────────────────────────────

/**
 * Who the caller is (GET /api/v1/whoami): principal, roles and the
 * effective scopes. A browser session holds its token's scopes narrowed
 * to the web UI's set.
 */
export interface WhoAmI {
  principal: string;
  name?: string;
  provider: string;
  roles: string[];
  scopes: string[];
  /** token, session (this browser's sign-in) or none (private instance: nothing to sign in to). */
  via: 'token' | 'session' | 'none';
  session?: { id: string; kind: string; created_at: string; idle_expires_at: string; expires_at: string };
}

/** The code exchange answers the new session's whoami, plus a warning. */
export interface SignedIn extends WhoAmI {
  /** A condition that will break the session, e.g. plain HTTP. */
  warning?: string;
}

/** Where `ctxt ui open` links point; the code rides in the fragment. */
export const LOGIN_PATH = '/ui/auth';

export const session = {
  whoami: () => req<WhoAmI>('/whoami'),
  signOut: () => req<void>('/ui/session', { method: 'DELETE' }),
  /** Trades a login code for the session cookie (outside /api/v1). */
  exchange: async (code: string): Promise<SignedIn> => {
    const res = await fetch('/ui/auth/session', {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json', [CSRF_HEADER]: '1' },
      body: JSON.stringify({ code }),
    });
    const body = (await res.json().catch(() => ({}))) as SignedIn & ErrorEnvelope;
    if (!res.ok) {
      throw new ApiError(res.status, body.error?.code, body.error?.message ?? res.statusText);
    }
    return body;
  },
};
