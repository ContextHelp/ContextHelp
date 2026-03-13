# Raycast Extension Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Raycast extension with 5 commands (capture, search, recent, compose, jobs) backed by the dpkms REST API at `http://127.0.0.1:8080`.

**Architecture:** `extensions/raycast/` is a self-contained Raycast extension package. All API calls go through `src/api/endpoints.ts`. Server-offline state is handled at the command level with an EmptyView + "Start Server" action. Compose uses CLI exec (no REST /compose endpoint).

**Tech Stack:** TypeScript, React (Raycast API), @raycast/api, AppleScript (browser URL helper).

---

## Source References

- `internal/storage/types.go` — canonical Go types for `KnowledgeObject`, `Job`, `Tag`, `Decision`, `Task`, `Section`
- `internal/server/http/handlers_objects.go` — `ListObjects` uses query params `type`, `subtype`, `limit`, `offset`; response envelope is `{"data": [...], "total": N}`
- `cmd/ctxt/cmd/make.go` — `ctxt make <type>` with flags `--tag`, `--mention`, `--since`, `--export markdown|json`, `--no-citations`

## Key Notes Before Starting

- `ListObjects` response envelope uses `"data"` (not `"objects"`): `{"data": [...], "total": N}`
- `Job` has fields `payload`, `pipeline`, `result_id`, `retry_count`, `max_retries`, `started_at`, `completed_at` beyond the basics
- `KnowledgeObject` has `raw_content`, `content_type`, `metadata`, `sections`, `tasks`, `embeddings`, `registry_influences`, `plugins`, `reinforcement_count`, `last_reinforced_at`, `fts_indexed`, `vector_indexed` — include all in types.ts
- `ctxt make` flag is `--export json` (not `--output json`); `--output-file` writes to a file
- Deeplinks use `launchContext` object, not query string parsing
- **Shell injection safety:** All subprocess calls use `execFile` (via the `execFileNoThrow` utility created in Task 7a), never `exec()` with string interpolation

---

## Tasks

### Task 1: Extension scaffold

Create directory structure:

```bash
mkdir -p extensions/raycast/src/{api,commands,hooks,utils,components}
touch extensions/raycast/src/api/{types.ts,client.ts,endpoints.ts}
touch extensions/raycast/src/hooks/{useHealth.ts,useJobs.ts}
touch extensions/raycast/src/utils/{browser.ts,execFileNoThrow.ts}
touch extensions/raycast/src/components/ServerOffline.tsx
touch extensions/raycast/src/commands/{capture.tsx,search.tsx,recent.tsx,compose.tsx,jobs.tsx}
```

Create `extensions/raycast/package.json`:

```json
{
  "name": "ctxt",
  "title": "ctxt Knowledge",
  "description": "Capture, search, and compose with ctxt",
  "icon": "extension-icon.png",
  "author": "contexthelp",
  "categories": ["Productivity"],
  "license": "MIT",
  "commands": [
    {
      "name": "capture",
      "title": "Capture to ctxt",
      "description": "Capture content to knowledge store",
      "mode": "view"
    },
    {
      "name": "search",
      "title": "Search Knowledge",
      "description": "Search your knowledge store",
      "mode": "view"
    },
    {
      "name": "recent",
      "title": "Recent Captures",
      "description": "View recent knowledge objects",
      "mode": "view"
    },
    {
      "name": "compose",
      "title": "Compose Brief or Plan",
      "description": "Generate a brief or plan",
      "mode": "view"
    },
    {
      "name": "jobs",
      "title": "Job Queue",
      "description": "Monitor processing jobs",
      "mode": "view"
    }
  ],
  "deeplinks": [
    { "link": "capture", "command": "capture" },
    { "link": "search", "command": "search" }
  ],
  "preferences": [
    {
      "name": "serverUrl",
      "title": "Server URL",
      "type": "textfield",
      "default": "http://127.0.0.1:8080",
      "description": "dpkms server URL",
      "required": false
    },
    {
      "name": "apiToken",
      "title": "API Token",
      "type": "password",
      "default": "",
      "description": "Optional API token (future)",
      "required": false
    },
    {
      "name": "defaultProfile",
      "title": "Default Profile",
      "type": "textfield",
      "default": "",
      "description": "Default focus profile",
      "required": false
    }
  ],
  "dependencies": {
    "@raycast/api": "^1.0.0"
  },
  "devDependencies": {
    "@raycast/eslint-config": "^1.0.0",
    "typescript": "^5.0.0",
    "@types/node": "^20.0.0"
  },
  "scripts": {
    "build": "ray build -e dist",
    "dev": "ray develop",
    "lint": "ray lint"
  }
}
```

Create `extensions/raycast/tsconfig.json`:

```json
{
  "compilerOptions": {
    "target": "ES2022",
    "lib": ["ES2022"],
    "module": "CommonJS",
    "moduleResolution": "node",
    "jsx": "react-jsx",
    "strict": true,
    "esModuleInterop": true,
    "skipLibCheck": true,
    "outDir": "dist"
  },
  "include": ["src/**/*"]
}
```

Run `npm install` inside `extensions/raycast/`. Expected: `node_modules/@raycast/api` installed without errors.

---

### Task 2: API types

Create `extensions/raycast/src/api/types.ts` — mirror all Go types from `internal/storage/types.go`:

```typescript
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
```

Verify: `tsc --noEmit` passes on this file.

---

### Task 3: API client

Create `extensions/raycast/src/api/client.ts`:

```typescript
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
```

---

### Task 4: Named endpoint functions

Create `extensions/raycast/src/api/endpoints.ts`:

```typescript
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
```

Note: `listObjects` response envelope uses `"data"` key (matching `handlers_objects.go` line 42: `"data": objs`). Consumers must use `res.data`, not `res.objects`.

---

### Task 5: useHealth hook

Create `extensions/raycast/src/hooks/useHealth.ts`:

```typescript
import { useEffect, useState } from "react";
import { checkHealth } from "../api/endpoints";

export interface UseHealthResult {
  isOnline: boolean;
  isChecking: boolean;
  recheck: () => void;
}

export function useHealth(): UseHealthResult {
  const [isOnline, setIsOnline] = useState(false);
  const [isChecking, setIsChecking] = useState(true);
  const [tick, setTick] = useState(0);

  useEffect(() => {
    let cancelled = false;
    setIsChecking(true);
    checkHealth()
      .then(() => {
        if (!cancelled) setIsOnline(true);
      })
      .catch(() => {
        if (!cancelled) setIsOnline(false);
      })
      .finally(() => {
        if (!cancelled) setIsChecking(false);
      });
    return () => {
      cancelled = true;
    };
  }, [tick]);

  return {
    isOnline,
    isChecking,
    recheck: () => setTick((n) => n + 1),
  };
}
```

---

### Task 6: useJobs hook

Create `extensions/raycast/src/hooks/useJobs.ts`:

```typescript
import { useEffect, useRef, useState } from "react";
import { listJobs } from "../api/endpoints";
import { Job, JobStatus } from "../api/types";

const POLL_INTERVAL_MS = 3000;
const ACTIVE_STATUSES: JobStatus[] = ["pending", "running"];

export interface UseJobsResult {
  jobs: Job[];
  isLoading: boolean;
  error: string | null;
  refresh: () => void;
}

export function useJobs(): UseJobsResult {
  const [jobs, setJobs] = useState<Job[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [tick, setTick] = useState(0);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    let cancelled = false;

    async function fetchJobs() {
      try {
        const res = await listJobs({ limit: "50" });
        if (!cancelled) {
          setJobs(res.jobs ?? []);
          setError(null);

          // Only schedule next poll if active jobs remain
          const hasActive = (res.jobs ?? []).some((j) =>
            ACTIVE_STATUSES.includes(j.status)
          );
          if (hasActive) {
            timerRef.current = setTimeout(() => {
              if (!cancelled) setTick((n) => n + 1);
            }, POLL_INTERVAL_MS);
          }
        }
      } catch (e: unknown) {
        if (!cancelled) {
          setError((e as Error).message ?? "Failed to load jobs");
        }
      } finally {
        if (!cancelled) setIsLoading(false);
      }
    }

    setIsLoading(true);
    fetchJobs();

    return () => {
      cancelled = true;
      if (timerRef.current) clearTimeout(timerRef.current);
    };
  }, [tick]);

  return {
    jobs,
    isLoading,
    error,
    refresh: () => setTick((n) => n + 1),
  };
}
```

Polling stops automatically when all jobs reach `completed` or `failed`. Polling resumes on next `refresh()` call.

---

### Task 7: execFileNoThrow utility

Create `extensions/raycast/src/utils/execFileNoThrow.ts`.

**Purpose:** Safe subprocess helper using `execFile` (not `exec`) to prevent shell injection. All CLI calls in the extension use this instead of `exec()`.

```typescript
import { execFile as nodeExecFile } from "child_process";
import { promisify } from "util";

const execFileAsync = promisify(nodeExecFile);

export interface ExecResult {
  stdout: string;
  stderr: string;
  /** exit code, or -1 if process failed to spawn */
  status: number;
}

/**
 * Run a command with args safely via execFile (no shell interpolation).
 * Never throws — returns structured output including errors.
 *
 * @param cmd  Absolute or PATH-resolved binary name
 * @param args Array of arguments (no shell quoting needed)
 * @param opts Optional: cwd, env, maxBuffer
 */
export async function execFileNoThrow(
  cmd: string,
  args: string[],
  opts?: { cwd?: string; env?: NodeJS.ProcessEnv; maxBuffer?: number }
): Promise<ExecResult> {
  // Resolve PATH to include common install locations for macOS
  const defaultPaths = [
    "/usr/local/bin",
    "/opt/homebrew/bin",
    `${process.env.HOME ?? ""}/.local/bin`,
    `${process.env.GOPATH ?? (process.env.HOME ?? "") + "/go"}/bin`,
    "/usr/bin",
    "/bin",
  ].join(":");

  const env: NodeJS.ProcessEnv = {
    ...process.env,
    PATH: `${defaultPaths}:${process.env.PATH ?? ""}`,
    ...opts?.env,
  };

  try {
    const { stdout, stderr } = await execFileAsync(cmd, args, {
      cwd: opts?.cwd,
      env,
      maxBuffer: opts?.maxBuffer ?? 10 * 1024 * 1024, // 10MB
    });
    return { stdout: stdout ?? "", stderr: stderr ?? "", status: 0 };
  } catch (e: unknown) {
    const err = e as Error & { stdout?: string; stderr?: string; code?: number | string };
    return {
      stdout: err.stdout ?? "",
      stderr: err.stderr ?? err.message ?? "unknown error",
      status: typeof err.code === "number" ? err.code : 1,
    };
  }
}
```

---

### Task 8: Browser URL helper

Create `extensions/raycast/src/utils/browser.ts`.

Uses AppleScript via `runAppleScript` (Raycast built-in — no subprocess injection risk since no user input reaches the script).

```typescript
import { runAppleScript } from "@raycast/api";

export interface BrowserTab {
  url: string;
  title: string;
}

/**
 * Get the URL and title of the frontmost browser tab.
 * Supports Safari, Chrome, Arc, and Brave. Returns null for unsupported
 * browsers or if AppleScript access is denied.
 */
export async function getFrontmostTabUrl(): Promise<BrowserTab | null> {
  // Tab delimiter avoids issues with commas in titles
  const script = `
    tell application "System Events"
      set frontApp to name of first application process whose frontmost is true
    end tell
    set tabURL to ""
    set tabTitle to ""
    if frontApp is "Safari" then
      tell application "Safari"
        set tabURL to URL of current tab of window 1
        set tabTitle to name of current tab of window 1
      end tell
    else if frontApp is "Google Chrome" then
      tell application "Google Chrome"
        set tabURL to URL of active tab of window 1
        set tabTitle to title of active tab of window 1
      end tell
    else if frontApp is "Arc" then
      tell application "Arc"
        set tabURL to URL of active tab of window 1
        set tabTitle to title of active tab of window 1
      end tell
    else if frontApp is "Brave Browser" then
      tell application "Brave Browser"
        set tabURL to URL of active tab of window 1
        set tabTitle to title of active tab of window 1
      end tell
    end if
    return tabURL & "\t" & tabTitle
  `;

  try {
    const result = await runAppleScript(script);
    if (!result || !result.trim()) return null;
    const parts = result.split("\t");
    const url = parts[0]?.trim() ?? "";
    const title = parts[1]?.trim() ?? "";
    if (!url || !url.startsWith("http")) return null;
    return { url, title };
  } catch {
    // AppleScript permission denied or unsupported browser
    return null;
  }
}

export function isUrl(text: string): boolean {
  try {
    const u = new URL(text.trim());
    return u.protocol === "http:" || u.protocol === "https:";
  } catch {
    return false;
  }
}
```

---

### Task 9: ServerOffline component

Create `extensions/raycast/src/components/ServerOffline.tsx`.

Uses `execFileNoThrow` to start the server — no shell injection risk since no user input is passed.

```typescript
import { Action, ActionPanel, Detail } from "@raycast/api";
import { execFileNoThrow } from "../utils/execFileNoThrow";

interface Props {
  onRetry?: () => void;
}

const MARKDOWN = `## ctxt server is offline

The dpkms server is not running at the configured URL.

**To start the server:**
\`\`\`
dpkms serve
\`\`\`

Or click **Start Server** below to launch it in the background.
`;

export function ServerOffline({ onRetry }: Props) {
  async function handleStart() {
    // Use execFileNoThrow to avoid shell injection; dpkms is a trusted binary
    // We fire-and-forget (no await) so the command runs in background
    execFileNoThrow("dpkms", ["serve"]).then(() => {
      // dpkms serve blocks; this resolves only if it exits immediately
    });

    // Give server 1.5s to bind, then re-check health
    setTimeout(() => {
      onRetry?.();
    }, 1500);
  }

  return (
    <Detail
      markdown={MARKDOWN}
      actions={
        <ActionPanel>
          <Action title="Start Server" onAction={handleStart} />
          {onRetry && (
            <Action title="Retry Connection" onAction={onRetry} />
          )}
        </ActionPanel>
      }
    />
  );
}
```

---

### Task 10: capture.tsx command

Create `extensions/raycast/src/commands/capture.tsx`.

Responsibilities:
- Check clipboard for URL or text; check frontmost browser tab
- Pre-fill form fields based on content type
- Submit to `POST /api/v1/analyze`; show toast with job ID
- Handle `ServerOfflineError` → render `<ServerOffline />`
- Accept deeplink `launchContext.url` and `launchContext.text`

```typescript
import {
  Action,
  ActionPanel,
  Clipboard,
  Form,
  LaunchProps,
  Toast,
  showToast,
} from "@raycast/api";
import { useEffect, useState } from "react";
import { analyzeContent } from "../api/endpoints";
import { AnalyzeRequest, ServerOfflineError } from "../api/types";
import { ServerOffline } from "../components/ServerOffline";
import { useHealth } from "../hooks/useHealth";
import { getFrontmostTabUrl, isUrl } from "../utils/browser";

interface LaunchContext {
  url?: string;
  text?: string;
}

interface FormValues {
  content: string;
  type: string;
  pipeline: string;
  hints: string;
  source: string;
}

const TYPE_OPTIONS = ["auto", "url", "text", "note", "document", "image", "audio", "video"];

export default function CaptureCommand(props: LaunchProps<{ launchContext: LaunchContext }>) {
  const { isOnline, isChecking, recheck } = useHealth();
  const [isSubmitting, setIsSubmitting] = useState(false);

  // Pre-fill state
  const [defaultContent, setDefaultContent] = useState("");
  const [defaultType, setDefaultType] = useState("auto");
  const [defaultSource, setDefaultSource] = useState("");

  useEffect(() => {
    async function prefill() {
      // 1. Deeplink args take highest priority
      if (props.launchContext?.url) {
        setDefaultContent(props.launchContext.url);
        setDefaultType("url");
        setDefaultSource(props.launchContext.url);
        return;
      }
      if (props.launchContext?.text) {
        setDefaultContent(props.launchContext.text);
        setDefaultType("text");
        return;
      }

      // 2. Check frontmost browser tab
      const tab = await getFrontmostTabUrl();
      if (tab) {
        setDefaultContent(tab.url);
        setDefaultType("url");
        setDefaultSource(tab.url);
        return;
      }

      // 3. Fall back to clipboard
      const clip = await Clipboard.readText();
      if (clip) {
        if (isUrl(clip)) {
          setDefaultContent(clip);
          setDefaultType("url");
          setDefaultSource(clip);
        } else {
          setDefaultContent(clip);
          setDefaultType("text");
        }
      }
    }

    prefill();
  }, []);

  if (isChecking) {
    return <Form isLoading={true} />;
  }

  if (!isOnline) {
    return <ServerOffline onRetry={recheck} />;
  }

  async function handleSubmit(values: FormValues) {
    setIsSubmitting(true);

    const req: AnalyzeRequest = {
      content: values.content,
      type: values.type === "auto" ? undefined : values.type,
      pipeline: values.pipeline || undefined,
      hints: values.hints
        ? values.hints.split(",").map((h) => h.trim()).filter(Boolean)
        : undefined,
      source: values.source || undefined,
    };

    try {
      const res = await analyzeContent(req);
      await showToast({
        style: Toast.Style.Success,
        title: "Captured",
        message: `Job ${res.job_id} queued`,
      });
    } catch (e: unknown) {
      if (e instanceof ServerOfflineError) {
        await showToast({ style: Toast.Style.Failure, title: "Server offline" });
      } else {
        await showToast({
          style: Toast.Style.Failure,
          title: "Capture failed",
          message: (e as Error).message,
        });
      }
    } finally {
      setIsSubmitting(false);
    }
  }

  return (
    <Form
      isLoading={isSubmitting}
      actions={
        <ActionPanel>
          <Action.SubmitForm title="Capture" onSubmit={handleSubmit} />
        </ActionPanel>
      }
    >
      <Form.TextArea
        id="content"
        title="Content"
        placeholder="Paste URL, text, or content to capture…"
        defaultValue={defaultContent}
      />
      <Form.Dropdown id="type" title="Type" defaultValue={defaultType}>
        {TYPE_OPTIONS.map((t) => (
          <Form.Dropdown.Item key={t} value={t} title={t} />
        ))}
      </Form.Dropdown>
      <Form.TextField
        id="pipeline"
        title="Pipeline"
        placeholder="e.g. url.generic (optional)"
      />
      <Form.TextField
        id="hints"
        title="Hints"
        placeholder="Comma-separated hints (optional)"
      />
      <Form.TextField
        id="source"
        title="Source URL"
        placeholder="Override source URL (optional)"
        defaultValue={defaultSource}
      />
    </Form>
  );
}
```

---

### Task 11: search.tsx command

Create `extensions/raycast/src/commands/search.tsx`.

Responsibilities:
- Detect RSQL query (contains `==`) → use `GET /api/v1/search?q=`; plain text → also `GET /api/v1/search?q=` (single endpoint handles both)
- List results with truncated title, type badge, tag accessories
- Push Detail view on select
- Accept deeplink `launchContext.q` to pre-fill query

```typescript
import {
  Action,
  ActionPanel,
  Color,
  Detail,
  Icon,
  List,
  LaunchProps,
  Toast,
  showToast,
} from "@raycast/api";
import { useEffect, useState } from "react";
import { searchObjects } from "../api/endpoints";
import { KnowledgeObject, ServerOfflineError } from "../api/types";
import { ServerOffline } from "../components/ServerOffline";
import { useHealth } from "../hooks/useHealth";

interface LaunchContext {
  q?: string;
}

function truncate(s: string, n: number): string {
  return s.length > n ? s.slice(0, n - 1) + "…" : s;
}

function buildObjectMarkdown(obj: KnowledgeObject): string {
  const lines: string[] = [];
  lines.push(`# ${truncate(obj.text_content ?? obj.raw_content, 80)}`);
  lines.push(`\n**Type:** \`${obj.type}\`${obj.subtype ? ` / \`${obj.subtype}\`` : ""}`);
  if (obj.pipeline) lines.push(`**Pipeline:** \`${obj.pipeline}\``);
  if (obj.source) lines.push(`**Source:** ${obj.source}`);
  lines.push(`**Created:** ${new Date(obj.created_at).toLocaleString()}`);

  if (obj.summaries?.length) {
    lines.push("\n## Summary");
    obj.summaries.forEach((s) => lines.push(s));
  }
  if (obj.tags?.length) {
    lines.push("\n## Tags");
    lines.push(obj.tags.map((t) => `\`${t.label}\``).join(" "));
  }
  if (obj.mentions?.length) {
    lines.push("\n## Mentions");
    lines.push(obj.mentions.join(", "));
  }
  if (obj.decisions?.length) {
    lines.push("\n## Decisions");
    obj.decisions.forEach((d) =>
      lines.push(`- **${d.title}** — ${d.status} / ${d.impact}`)
    );
  }
  if (obj.tasks?.length) {
    lines.push("\n## Tasks");
    obj.tasks.forEach((t) =>
      lines.push(`- [${t.status === "done" ? "x" : " "}] ${t.title}`)
    );
  }
  return lines.join("\n");
}

function ObjectDetail({ obj }: { obj: KnowledgeObject }) {
  return (
    <Detail
      markdown={buildObjectMarkdown(obj)}
      actions={
        <ActionPanel>
          <Action.CopyToClipboard title="Copy ID" content={obj.id} />
          {obj.source && (
            <Action.OpenInBrowser title="Open Source" url={obj.source} />
          )}
        </ActionPanel>
      }
    />
  );
}

export default function SearchCommand(props: LaunchProps<{ launchContext: LaunchContext }>) {
  const { isOnline, isChecking, recheck } = useHealth();
  const [query, setQuery] = useState(props.launchContext?.q ?? "");
  const [results, setResults] = useState<KnowledgeObject[]>([]);
  const [isLoading, setIsLoading] = useState(false);

  useEffect(() => {
    if (!isOnline || !query.trim()) {
      setResults([]);
      return;
    }

    let cancelled = false;
    setIsLoading(true);

    searchObjects(query.trim())
      .then((res) => {
        if (!cancelled) setResults(res.objects ?? []);
      })
      .catch(async (e: unknown) => {
        if (!cancelled && !(e instanceof ServerOfflineError)) {
          await showToast({
            style: Toast.Style.Failure,
            title: "Search failed",
            message: (e as Error).message,
          });
        }
      })
      .finally(() => {
        if (!cancelled) setIsLoading(false);
      });

    return () => {
      cancelled = true;
    };
  }, [query, isOnline]);

  if (isChecking) return <List isLoading={true} />;
  if (!isOnline) return <ServerOffline onRetry={recheck} />;

  return (
    <List
      isLoading={isLoading}
      searchText={query}
      onSearchTextChange={setQuery}
      searchBarPlaceholder="Search knowledge (RSQL or text)…"
      throttle
    >
      {results.map((obj) => (
        <List.Item
          key={obj.id}
          title={truncate(obj.text_content ?? obj.raw_content, 60)}
          subtitle={`${obj.type}${obj.pipeline ? ` · ${obj.pipeline}` : ""}`}
          accessories={[
            ...(obj.tags
              ?.slice(0, 3)
              .map((t) => ({ tag: { value: t.label, color: Color.Blue } })) ?? []),
          ]}
          actions={
            <ActionPanel>
              <Action.Push
                title="View Details"
                target={<ObjectDetail obj={obj} />}
              />
              <Action.CopyToClipboard title="Copy ID" content={obj.id} />
              {obj.source && (
                <Action.OpenInBrowser title="Open Source" url={obj.source} />
              )}
            </ActionPanel>
          }
        />
      ))}
    </List>
  );
}
```

---

### Task 12: recent.tsx command

Create `extensions/raycast/src/commands/recent.tsx`.

Loads `GET /api/v1/objects?sort=updated_at&dir=desc&limit=30` on mount. Shares `ObjectDetail` / `buildObjectMarkdown` pattern with search (extract to shared module if desired as a follow-up refactor).

```typescript
import {
  Action,
  ActionPanel,
  Color,
  Detail,
  List,
  Toast,
  showToast,
} from "@raycast/api";
import { useEffect, useState } from "react";
import { listObjects } from "../api/endpoints";
import { KnowledgeObject, ServerOfflineError } from "../api/types";
import { ServerOffline } from "../components/ServerOffline";
import { useHealth } from "../hooks/useHealth";

function truncate(s: string, n: number): string {
  return s.length > n ? s.slice(0, n - 1) + "…" : s;
}

function buildObjectMarkdown(obj: KnowledgeObject): string {
  const lines: string[] = [];
  lines.push(`# ${truncate(obj.text_content ?? obj.raw_content, 80)}`);
  lines.push(`\n**Type:** \`${obj.type}\`${obj.subtype ? ` / \`${obj.subtype}\`` : ""}`);
  if (obj.pipeline) lines.push(`**Pipeline:** \`${obj.pipeline}\``);
  if (obj.source) lines.push(`**Source:** ${obj.source}`);
  lines.push(`**Created:** ${new Date(obj.created_at).toLocaleString()}`);
  if (obj.summaries?.length) {
    lines.push("\n## Summary");
    obj.summaries.forEach((s) => lines.push(s));
  }
  if (obj.tags?.length) {
    lines.push("\n## Tags");
    lines.push(obj.tags.map((t) => `\`${t.label}\``).join(" "));
  }
  if (obj.mentions?.length) {
    lines.push("\n## Mentions");
    lines.push(obj.mentions.join(", "));
  }
  if (obj.decisions?.length) {
    lines.push("\n## Decisions");
    obj.decisions.forEach((d) =>
      lines.push(`- **${d.title}** — ${d.status} / ${d.impact}`)
    );
  }
  return lines.join("\n");
}

function ObjectDetail({ obj }: { obj: KnowledgeObject }) {
  return (
    <Detail
      markdown={buildObjectMarkdown(obj)}
      actions={
        <ActionPanel>
          <Action.CopyToClipboard title="Copy ID" content={obj.id} />
          {obj.source && (
            <Action.OpenInBrowser title="Open Source" url={obj.source} />
          )}
        </ActionPanel>
      }
    />
  );
}

export default function RecentCommand() {
  const { isOnline, isChecking, recheck } = useHealth();
  const [objects, setObjects] = useState<KnowledgeObject[]>([]);
  const [isLoading, setIsLoading] = useState(false);

  useEffect(() => {
    if (!isOnline) return;
    let cancelled = false;
    setIsLoading(true);

    listObjects({ sort: "updated_at", dir: "desc", limit: "30" })
      .then((res) => {
        if (!cancelled) setObjects(res.data ?? []);
      })
      .catch(async (e: unknown) => {
        if (!cancelled && !(e instanceof ServerOfflineError)) {
          await showToast({
            style: Toast.Style.Failure,
            title: "Failed to load recent",
            message: (e as Error).message,
          });
        }
      })
      .finally(() => {
        if (!cancelled) setIsLoading(false);
      });

    return () => { cancelled = true; };
  }, [isOnline]);

  if (isChecking) return <List isLoading={true} />;
  if (!isOnline) return <ServerOffline onRetry={recheck} />;

  return (
    <List isLoading={isLoading} searchBarPlaceholder="Filter recent…">
      {objects.map((obj) => (
        <List.Item
          key={obj.id}
          title={truncate(obj.text_content ?? obj.raw_content, 60)}
          subtitle={`${obj.type}${obj.pipeline ? ` · ${obj.pipeline}` : ""}`}
          accessories={[
            { text: new Date(obj.updated_at).toLocaleDateString() },
            ...(obj.tags
              ?.slice(0, 2)
              .map((t) => ({ tag: { value: t.label, color: Color.Blue } })) ?? []),
          ]}
          actions={
            <ActionPanel>
              <Action.Push title="View Details" target={<ObjectDetail obj={obj} />} />
              <Action.CopyToClipboard title="Copy ID" content={obj.id} />
              {obj.source && (
                <Action.OpenInBrowser title="Open Source" url={obj.source} />
              )}
            </ActionPanel>
          }
        />
      ))}
    </List>
  );
}
```

Note: `res.data` (not `res.objects`) because `ListObjects` handler returns `{"data": [...], "total": N}`.

---

### Task 13: compose.tsx command

Create `extensions/raycast/src/commands/compose.tsx`.

Uses `execFileNoThrow("ctxt", args)` — no shell injection risk since arguments are passed as an array. The `--export json` flag (not `--output json`) matches `cmd/ctxt/cmd/make.go` line 44.

```typescript
import {
  Action,
  ActionPanel,
  Clipboard,
  Detail,
  Form,
  Toast,
  showToast,
} from "@raycast/api";
import { useState } from "react";
import { ServerOffline } from "../components/ServerOffline";
import { useHealth } from "../hooks/useHealth";
import { execFileNoThrow } from "../utils/execFileNoThrow";

type ComposeType = "brief" | "plan" | "summary" | "draft";

interface ComposeResult {
  // ComposeWithCitations JSON output from svc.ComposeWithCitations
  content: string;
  citations?: Array<{ id: string; ref: string }>;
}

interface FormValues {
  type: ComposeType;
  tag: string;
  mention: string;
  since: string;
  noCitations: boolean;
}

function buildArgs(values: FormValues): string[] {
  const args: string[] = ["make", values.type];
  if (values.tag) {
    args.push("--tag", values.tag);
  }
  if (values.mention) {
    args.push("--mention", values.mention);
  }
  if (values.since) {
    args.push("--since", values.since);
  }
  if (values.noCitations) {
    args.push("--no-citations");
  } else {
    // Default: export as JSON so we can extract citations and .content
    args.push("--export", "json");
  }
  return args;
}

export default function ComposeCommand() {
  const { isOnline, isChecking, recheck } = useHealth();
  const [isRunning, setIsRunning] = useState(false);
  const [result, setResult] = useState<string | null>(null);

  if (isChecking) return <Form isLoading={true} />;
  if (!isOnline) return <ServerOffline onRetry={recheck} />;

  if (result !== null) {
    return (
      <Detail
        markdown={result}
        actions={
          <ActionPanel>
            <Action
              title="Copy to Clipboard"
              onAction={() => Clipboard.copy(result!)}
            />
            <Action title="New Composition" onAction={() => setResult(null)} />
          </ActionPanel>
        }
      />
    );
  }

  async function handleSubmit(values: FormValues) {
    setIsRunning(true);

    const args = buildArgs(values);
    const res = await execFileNoThrow("ctxt", args);

    if (res.status !== 0) {
      await showToast({
        style: Toast.Style.Failure,
        title: "Compose failed",
        message: res.stderr || `exit code ${res.status}`,
      });
      setIsRunning(false);
      return;
    }

    let markdown: string;
    if (!values.noCitations) {
      // --export json: parse ComposeResult and use .content field
      try {
        const parsed = JSON.parse(res.stdout) as ComposeResult;
        markdown = parsed.content;
      } catch {
        // Fallback if output is not valid JSON
        markdown = res.stdout;
      }
    } else {
      // --no-citations: output is plain markdown
      markdown = res.stdout;
    }

    setResult(markdown);
    setIsRunning(false);
  }

  return (
    <Form
      isLoading={isRunning}
      actions={
        <ActionPanel>
          <Action.SubmitForm title="Compose" onSubmit={handleSubmit} />
        </ActionPanel>
      }
    >
      <Form.Dropdown id="type" title="Type" defaultValue="brief">
        {(["brief", "plan", "summary", "draft"] as ComposeType[]).map((t) => (
          <Form.Dropdown.Item key={t} value={t} title={t} />
        ))}
      </Form.Dropdown>
      <Form.TextField
        id="tag"
        title="Tags"
        placeholder="Comma-separated tags (e.g. ux,onboarding)"
      />
      <Form.TextField
        id="mention"
        title="Mention"
        placeholder="@namespace.slug"
      />
      <Form.TextField
        id="since"
        title="Since Date"
        placeholder="YYYY-MM-DD"
      />
      <Form.Checkbox
        id="noCitations"
        label="No Citations"
        defaultValue={false}
      />
    </Form>
  );
}
```

---

### Task 14: jobs.tsx command

Create `extensions/raycast/src/commands/jobs.tsx`:

```typescript
import {
  Action,
  ActionPanel,
  Color,
  Icon,
  List,
  Toast,
  showToast,
} from "@raycast/api";
import { retryJob } from "../api/endpoints";
import { Job } from "../api/types";
import { ServerOffline } from "../components/ServerOffline";
import { useHealth } from "../hooks/useHealth";
import { useJobs } from "../hooks/useJobs";

function statusColor(status: Job["status"]): Color {
  switch (status) {
    case "completed": return Color.Green;
    case "failed":    return Color.Red;
    case "running":   return Color.Yellow;
    case "pending":   return Color.SecondaryText;
  }
}

function statusIcon(status: Job["status"]): Icon {
  switch (status) {
    case "completed": return Icon.Checkmark;
    case "failed":    return Icon.XMarkCircle;
    case "running":   return Icon.CircleProgress;
    case "pending":   return Icon.Clock;
  }
}

function statusLabel(job: Job): string {
  if (job.status === "running") return "Processing…";
  if (job.status === "failed" && job.error) return job.error.slice(0, 40);
  return job.status;
}

export default function JobsCommand() {
  const { isOnline, isChecking, recheck: recheckHealth } = useHealth();
  const { jobs, isLoading, error, refresh } = useJobs();

  if (isChecking) return <List isLoading={true} />;
  if (!isOnline) return <ServerOffline onRetry={recheckHealth} />;

  return (
    <List
      isLoading={isLoading}
      searchBarPlaceholder="Filter jobs…"
    >
      <List.EmptyView
        title={error ? "Failed to load jobs" : "No jobs"}
        description={error ?? "Capture content to create jobs."}
      />
      {jobs.map((job) => (
        <List.Item
          key={job.id}
          icon={{ source: statusIcon(job.status), tintColor: statusColor(job.status) }}
          title={job.source ?? job.type}
          subtitle={job.pipeline ?? job.type}
          accessories={[
            { tag: { value: statusLabel(job), color: statusColor(job.status) } },
            { text: new Date(job.created_at).toLocaleTimeString() },
          ]}
          actions={
            <ActionPanel>
              <Action.CopyToClipboard title="Copy Job ID" content={job.id} />
              {job.status === "failed" && (
                <Action
                  title="Retry Job"
                  icon={Icon.ArrowClockwise}
                  onAction={async () => {
                    try {
                      await retryJob(job.id);
                      await showToast({
                        style: Toast.Style.Success,
                        title: "Retry queued",
                      });
                      refresh();
                    } catch (e: unknown) {
                      await showToast({
                        style: Toast.Style.Failure,
                        title: "Retry failed",
                        message: (e as Error).message,
                      });
                    }
                  }}
                />
              )}
              <Action title="Refresh" onAction={refresh} />
            </ActionPanel>
          }
        />
      ))}
    </List>
  );
}
```

---

### Task 15: Build and lint

```bash
cd extensions/raycast
npm run build
```

Expected: `dist/` directory created with compiled JS for each command. No TypeScript errors.

```bash
npm run lint
```

Expected: No lint errors (or only warnings about unused imports — clean up before merging).

---

### Task 16: Manual verification checklist

Start server before testing: `dpkms serve`

1. **Capture — clipboard URL pre-fill**
   - Copy `https://example.com` to clipboard
   - Open "Capture to ctxt" in Raycast
   - Expect: content field = URL, type = `url`

2. **Capture — browser tab pre-fill**
   - Open Safari/Chrome/Arc/Brave with any HTTP page
   - Open "Capture to ctxt" in Raycast
   - Expect: content = tab URL, type = `url`, source = tab URL

3. **Capture — submit**
   - Fill form and press Enter
   - Expect: success toast "Job `<uuid>` queued"

4. **Search — plain text**
   - Type "machine learning" in search bar
   - Expect: results list updates (throttled)

5. **Search — RSQL**
   - Type `type==url;tag=in=(ux)` in search bar
   - Expect: filtered results matching RSQL expression

6. **Search — Detail view**
   - Select a result → push Detail
   - Expect: summary, tags, decisions rendered in markdown

7. **Recent — loads list**
   - Open "Recent Captures"
   - Expect: up to 30 objects sorted newest first

8. **Compose — generates brief**
   - Open "Compose", select type=brief, add a tag, submit
   - Expect: markdown brief rendered in Detail view, copy action available

9. **Jobs — live poll**
   - Capture something, open "Job Queue"
   - Expect: job appears, status updates every 3s while running

10. **Jobs — retry**
    - If a job is `failed`, press Retry
    - Expect: success toast, list refreshes

11. **Offline — server down**
    - Kill dpkms, open any command
    - Expect: "ctxt server is offline" with "Start Server" and "Retry Connection"

12. **Deeplink — capture**
    ```bash
    open 'raycast://extensions/contexthelp/ctxt/capture?context=%7B%22url%22%3A%22https%3A%2F%2Fexample.com%22%7D'
    ```
    - Expect: capture form opens with `https://example.com` pre-filled

13. **Deeplink — search**
    ```bash
    open 'raycast://extensions/contexthelp/ctxt/search?context=%7B%22q%22%3A%22machine+learning%22%7D'
    ```
    - Expect: search opens with "machine learning" pre-filled

---

### Task 17: Commit

```bash
git add extensions/raycast/
git commit -m "feat(raycast): add raycast extension with capture/search/recent/compose/jobs"
```

Expected:
```
[main <hash>] feat(raycast): add raycast extension with capture/search/recent/compose/jobs
 N files changed, M insertions(+)
 create mode 100644 extensions/raycast/package.json
 create mode 100644 extensions/raycast/tsconfig.json
 create mode 100644 extensions/raycast/src/api/types.ts
 create mode 100644 extensions/raycast/src/api/client.ts
 create mode 100644 extensions/raycast/src/api/endpoints.ts
 create mode 100644 extensions/raycast/src/hooks/useHealth.ts
 create mode 100644 extensions/raycast/src/hooks/useJobs.ts
 create mode 100644 extensions/raycast/src/utils/execFileNoThrow.ts
 create mode 100644 extensions/raycast/src/utils/browser.ts
 create mode 100644 extensions/raycast/src/components/ServerOffline.tsx
 create mode 100644 extensions/raycast/src/commands/capture.tsx
 create mode 100644 extensions/raycast/src/commands/search.tsx
 create mode 100644 extensions/raycast/src/commands/recent.tsx
 create mode 100644 extensions/raycast/src/commands/compose.tsx
 create mode 100644 extensions/raycast/src/commands/jobs.tsx
```

---

## Architecture Invariants

| Invariant | Where enforced |
|-----------|---------------|
| All API calls go through `endpoints.ts` | Client code only calls named functions, never `apiFetch` directly |
| Server URL comes from preferences | `apiFetch` reads `serverUrl` via `getPreferenceValues` |
| Offline state handled per-command | Each command root checks `useHealth` before rendering content |
| Compose is CLI-only | `compose.tsx` uses `execFileNoThrow("ctxt", args)`, no REST call |
| No shell injection | All subprocesses use `execFileNoThrow` with arg arrays, never `exec()` with string interpolation |
| `listObjects` response uses `"data"` key | All consumers use `res.data`, not `res.objects` |
| Polling stops when no active jobs | `useJobs` only schedules next tick if `pending` or `running` jobs exist |

## File Map

```
extensions/raycast/
├── package.json
├── tsconfig.json
└── src/
    ├── api/
    │   ├── types.ts              # Task 2 — Go type mirrors + ServerOfflineError
    │   ├── client.ts             # Task 3 — apiFetch + offline detection
    │   └── endpoints.ts          # Task 4 — all named API functions
    ├── hooks/
    │   ├── useHealth.ts          # Task 5 — server health check with recheck
    │   └── useJobs.ts            # Task 6 — auto-polling hook
    ├── utils/
    │   ├── execFileNoThrow.ts    # Task 7 — safe subprocess helper (no shell injection)
    │   └── browser.ts            # Task 8 — AppleScript tab URL helper
    ├── components/
    │   └── ServerOffline.tsx     # Task 9 — offline state UI
    └── commands/
        ├── capture.tsx           # Task 10 — form with clipboard/tab/deeplink pre-fill
        ├── search.tsx            # Task 11 — RSQL + text search with Detail view
        ├── recent.tsx            # Task 12 — recent objects list
        ├── compose.tsx           # Task 13 — CLI-driven composition via execFileNoThrow
        └── jobs.tsx              # Task 14 — live job queue with retry
```
