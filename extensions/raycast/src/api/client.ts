import { getPreferenceValues } from "@raycast/api";
import { ServerOfflineError } from "./types";

interface Preferences {
  serverUrl: string;
  apiToken: string;
}

export async function apiFetch<T>(path: string, options?: RequestInit): Promise<T> {
  const { serverUrl, apiToken } = getPreferenceValues<Preferences>();
  const base = serverUrl.replace(/\/$/, "");

  const headers: Record<string, string> = {
    "Content-Type": "application/json",
  };
  if (apiToken) {
    headers["Authorization"] = `Bearer ${apiToken}`;
  }

  try {
    const res = await fetch(`${base}${path}`, {
      ...options,
      headers: {
        ...headers,
        ...((options?.headers as Record<string, string>) ?? {}),
      },
    });

    if (!res.ok) {
      const body = await res.text().catch(() => "");
      throw new Error(`HTTP ${res.status}: ${body}`);
    }

    // 204 No Content — retryJob returns void
    if (res.status === 204) {
      return undefined as unknown as T;
    }

    return (await res.json()) as T;
  } catch (e: unknown) {
    const err = e as Error & { code?: string };
    if (
      err.code === "ECONNREFUSED" ||
      err.message?.includes("fetch failed") ||
      err.message?.includes("ECONNREFUSED") ||
      err.message?.includes("network")
    ) {
      throw new ServerOfflineError();
    }
    throw e;
  }
}
