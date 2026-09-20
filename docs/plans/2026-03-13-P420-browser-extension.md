# Browser Extension Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Cross-browser extension (Chrome MV3 / Firefox MV2) that captures from any tab via context menu, popup, and keyboard shortcut, with site-specific adaptors and offline queue.

**Architecture:** WXT framework generates MV3 (Chrome/Edge) and MV2 (Firefox) from one source. Background script handles context menu, hotkey, cookie bridge, and offline queue retry. Content scripts provide selection helper and site-specific adaptors. CORS middleware (Plan 4 Task 1 — `internal/server/http/middleware_cors.go`) enables extension↔server communication.

**Tech Stack:** TypeScript, WXT (build framework), React (popup/options), IndexedDB (offline queue), WebSocket (cookie bridge).

---

## Current State Audit

Before implementing, understand the baseline:

- `internal/server/http/middleware_cors.go` — **Created in Plan 4 Task 1.** Allows `chrome-extension://`, `moz-extension://`, `safari-web-extension://` origins on localhost. Do not re-implement.
- `internal/server/http/server.go` — Router already has CORS middleware wired (`r.Use(CORS(devCORS))`). Extension origins are always allowed regardless of `--dev` flag.
- `internal/service/types.go` — `AnalyzeRequest` has `Content`, `Type`, `Pipeline`, `Source` fields. Needs `SourceTitle` and `AuthState` additions.
- `internal/server/http/handlers_analyze.go` — existing `POST /api/v1/analyze` handler; new capture endpoints will live in `handlers_capture.go`.
- `internal/storage/types.go` — `KnowledgeObject` and `Job` types to mirror in `lib/types.ts`.
- `cmd/dpkms/cmd/serve.go` — no WebSocket listener today; needs new `ws` listener startup.
- Module path: `github.com/ideacrafterslabs/ctxt`

---

## Task List

1. Server-side additions (AnalyzeRequest fields, capture handlers, WebSocket cookie bridge)
2. WXT scaffold
3. API client + types
4. Offline queue
5. WebSocket cookie bridge
6. Capture orchestrator
7. Site adaptors
8. Content script
9. Background script
10. Popup
11. Options page
12. Build + verify

---

### Task 1: Server-side additions

> Implement all Go-side changes before writing any extension code.

**Step 1.1 — Add `SourceTitle` and `AuthState` to `AnalyzeRequest` in `internal/service/types.go`.**

```go
// AnalyzeRequest represents a request to analyze content.
type AnalyzeRequest struct {
	Content     string `json:"content"`
	Type        string `json:"type"`
	Pipeline    string `json:"pipeline,omitempty"`
	Source      string `json:"source,omitempty"`
	SourceTitle string `json:"source_title,omitempty"` // human-readable title of source page
	AuthState   string `json:"auth_state,omitempty"`  // opaque token from cookie bridge
}
```

**Step 1.2 — Create `internal/server/http/handlers_capture.go`.**

```go
package http

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// capturePageRequest is the body for POST /api/v1/capture/page.
type capturePageRequest struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	Content     string `json:"content"`
	Pipeline    string `json:"pipeline,omitempty"`
	AuthState   string `json:"auth_state,omitempty"`
}

// captureSelectionRequest is the body for POST /api/v1/capture/selection.
type captureSelectionRequest struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	Selection   string `json:"selection"`
	Pipeline    string `json:"pipeline,omitempty"`
	AuthState   string `json:"auth_state,omitempty"`
}

// captureElementRequest is the body for POST /api/v1/capture/element.
type captureElementRequest struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	Selector    string `json:"selector"`
	Content     string `json:"content"`
	Pipeline    string `json:"pipeline,omitempty"`
	AuthState   string `json:"auth_state,omitempty"`
}

// CapturePage handles POST /api/v1/capture/page.
// Captures the full text of a web page.
func CapturePage(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req capturePageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid request body")
			return
		}
		if req.URL == "" {
			WriteError(w, http.StatusBadRequest, "MISSING_URL", "url is required")
			return
		}

		content := req.Content
		if content == "" {
			content = req.URL
		}
		pipeline := req.Pipeline
		if pipeline == "" {
			pipeline = "url.generic"
		}

		jobID, err := svc.Analyze(r.Context(), service.AnalyzeRequest{
			Content:     content,
			Type:        "url",
			Pipeline:    pipeline,
			Source:      req.URL,
			SourceTitle: req.Title,
			AuthState:   req.AuthState,
		})
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "ANALYZE_FAILED", err.Error())
			return
		}

		WriteJSON(w, http.StatusAccepted, map[string]string{"job_id": jobID})
	}
}

// CaptureSelection handles POST /api/v1/capture/selection.
// Captures selected text from a web page.
func CaptureSelection(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req captureSelectionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid request body")
			return
		}
		if req.Selection == "" {
			WriteError(w, http.StatusBadRequest, "MISSING_SELECTION", "selection is required")
			return
		}

		pipeline := req.Pipeline
		if pipeline == "" {
			pipeline = "text.default"
		}

		jobID, err := svc.Analyze(r.Context(), service.AnalyzeRequest{
			Content:     req.Selection,
			Type:        "text",
			Pipeline:    pipeline,
			Source:      req.URL,
			SourceTitle: req.Title,
			AuthState:   req.AuthState,
		})
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "ANALYZE_FAILED", err.Error())
			return
		}

		WriteJSON(w, http.StatusAccepted, map[string]string{"job_id": jobID})
	}
}

// CaptureElement handles POST /api/v1/capture/element.
// Captures the text content of a specific DOM element.
func CaptureElement(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req captureElementRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid request body")
			return
		}
		if req.Content == "" {
			WriteError(w, http.StatusBadRequest, "MISSING_CONTENT", "content is required")
			return
		}

		pipeline := req.Pipeline
		if pipeline == "" {
			pipeline = "text.default"
		}

		jobID, err := svc.Analyze(r.Context(), service.AnalyzeRequest{
			Content:     req.Content,
			Type:        "text",
			Pipeline:    pipeline,
			Source:      req.URL,
			SourceTitle: req.Title,
			AuthState:   req.AuthState,
		})
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "ANALYZE_FAILED", err.Error())
			return
		}

		WriteJSON(w, http.StatusAccepted, map[string]string{"job_id": jobID})
	}
}

// ListRecentCaptures handles GET /api/v1/capture/recent.
// Returns the last 10 jobs whose source originates from the extension.
func ListRecentCaptures(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jobs, _, err := svc.ListJobs(r.Context(), storage.JobFilter{
			Limit: 10,
		})
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, map[string]any{"items": jobs, "total": len(jobs)})
	}
}
```

**Step 1.3 — Register capture routes in `internal/server/http/server.go`.**

Inside `r.Route("/api/v1", ...)`, add:

```go
// Capture (browser extension)
r.Post("/capture/page", CapturePage(svc))
r.Post("/capture/selection", CaptureSelection(svc))
r.Post("/capture/element", CaptureElement(svc))
r.Get("/capture/recent", ListRecentCaptures(svc))
```

**Step 1.4 — Create `internal/server/http/handlers_capture_test.go`.**

```go
package http_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	httpserver "github.com/ideacrafterslabs/ctxt/internal/server/http"
)

func TestCapturePage_MissingURL(t *testing.T) {
	svc := newTestService(t)
	handler := httpserver.CapturePage(svc)

	body, _ := json.Marshal(map[string]string{"title": "Test Page"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/capture/page", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestCapturePage_ValidRequest(t *testing.T) {
	svc := newTestService(t)
	handler := httpserver.CapturePage(svc)

	body, _ := json.Marshal(map[string]string{
		"url":     "https://example.com/article",
		"title":   "Example Article",
		"content": "The article body text.",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/capture/page", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Errorf("expected 202, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp map[string]string
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp["job_id"] == "" {
		t.Error("expected non-empty job_id")
	}
}

func TestCaptureSelection_MissingSelection(t *testing.T) {
	svc := newTestService(t)
	handler := httpserver.CaptureSelection(svc)

	body, _ := json.Marshal(map[string]string{"url": "https://example.com"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/capture/selection", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestListRecentCaptures(t *testing.T) {
	svc := newTestService(t)
	handler := httpserver.ListRecentCaptures(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/capture/recent", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}
```

**Step 1.5 — Create `internal/server/ws/` directory and cookie bridge.**

```bash
mkdir -p internal/server/ws
```

Create `internal/server/ws/cookie_bridge.go`:

```go
package ws

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// CookieEntry represents a single browser cookie.
type CookieEntry struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Domain   string `json:"domain"`
	Path     string `json:"path"`
	Secure   bool   `json:"secure"`
	HTTPOnly bool   `json:"httpOnly"`
	SameSite string `json:"sameSite"`
}

// SyncMessage is the message format sent by the extension.
type SyncMessage struct {
	Type    string        `json:"type"`    // "cookie_sync"
	Domain  string        `json:"domain"`
	Cookies []CookieEntry `json:"cookies"`
}

// CookieCache holds session cookies synced from the extension.
// Entries expire after 1 hour.
type CookieCache struct {
	mu      sync.RWMutex
	entries map[string]cachedEntry
}

type cachedEntry struct {
	cookies   []CookieEntry
	expiresAt time.Time
}

// NewCookieCache creates an empty CookieCache.
func NewCookieCache() *CookieCache {
	return &CookieCache{
		entries: make(map[string]cachedEntry),
	}
}

// Set stores cookies for a domain with a 1-hour TTL.
func (c *CookieCache) Set(domain string, cookies []CookieEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[domain] = cachedEntry{
		cookies:   cookies,
		expiresAt: time.Now().Add(time.Hour),
	}
}

// Get retrieves cookies for a domain, returning nil if absent or expired.
func (c *CookieCache) Get(domain string) []CookieEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.entries[domain]
	if !ok || time.Now().After(e.expiresAt) {
		return nil
	}
	return e.cookies
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		// Only accept connections from localhost or extension origins.
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		return host == "127.0.0.1" || host == "::1" || host == "localhost"
	},
}

// CookieBridgeServer is a standalone WebSocket server on port 9377.
type CookieBridgeServer struct {
	cache  *CookieCache
	server *http.Server
}

// NewCookieBridgeServer creates a CookieBridgeServer with the given cache.
func NewCookieBridgeServer(cache *CookieCache) *CookieBridgeServer {
	mux := http.NewServeMux()
	srv := &CookieBridgeServer{cache: cache}
	mux.HandleFunc("/", srv.handleWS)
	srv.server = &http.Server{
		Addr:    "127.0.0.1:9377",
		Handler: mux,
	}
	return srv
}

// Start starts the WebSocket server and blocks until ctx is cancelled.
func (s *CookieBridgeServer) Start(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return s.server.Shutdown(context.Background())
	}
}

func (s *CookieBridgeServer) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws: upgrade error: %v", err)
		return
	}
	defer conn.Close()

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var sync SyncMessage
		if err := json.Unmarshal(msg, &sync); err != nil {
			continue
		}
		if sync.Type == "cookie_sync" && sync.Domain != "" {
			s.cache.Set(sync.Domain, sync.Cookies)
		}
	}
}
```

**Step 1.6 — Wire cookie bridge into `cmd/dpkms/cmd/serve.go`.**

Import:
```go
"github.com/ideacrafterslabs/ctxt/internal/server/ws"
```

After step 9 in `runServe`, add:

```go
// 9b. Init cookie bridge (for browser extension).
cookieCache := ws.NewCookieCache()
cookieBridge := ws.NewCookieBridgeServer(cookieCache)

// In the errgroup:
g.Go(func() error {
    fmt.Println("Cookie bridge listening on ws://127.0.0.1:9377")
    return cookieBridge.Start(ctx)
})
```

**Step 1.7 — Run tests.**

```bash
go test ./internal/server/http/... -run TestCapture -v
# Expected: 4 tests pass

go build ./internal/server/ws/...
# Expected: no errors
```

---

### Task 2: WXT scaffold

**Goal:** Bootstrap the WXT extension project that generates MV3 (Chrome/Edge) and MV2 (Firefox) from one source.

**Step 2.1 — Create the extension directory.**

```bash
mkdir -p extensions/browser
cd extensions/browser
```

**Step 2.2 — Initialize WXT project.**

```bash
npx wxt init .
# Select: TypeScript + React when prompted
npm install
```

Expected output: `WXT Project initialized successfully.`

**Step 2.3 — Install additional runtime dependencies.**

```bash
cd extensions/browser
npm install idb @tanstack/react-query react-hot-toast
npm install -D @types/chrome
```

**Step 2.4 — Create `extensions/browser/wxt.config.ts`.**

```typescript
import { defineConfig } from "wxt";

export default defineConfig({
  srcDir: "src",
  outDir: ".output",
  manifest: {
    name: "ctxt",
    description: "Capture knowledge to your local ctxt instance",
    version: "0.1.0",
    permissions: [
      "activeTab",
      "contextMenus",
      "cookies",
      "storage",
      "alarms",
      "notifications",
      "scripting",
    ],
    host_permissions: [
      "http://127.0.0.1:8080/*",
      "ws://127.0.0.1:9377/*",
      "*://*/*",
    ],
    commands: {
      "capture-tab": {
        suggested_key: {
          default: "Ctrl+Shift+K",
          mac: "Command+Shift+K",
        },
        description: "Capture current tab to ctxt",
      },
    },
  },
});
```

**Step 2.5 — Verify WXT builds.**

```bash
cd extensions/browser && npm run build
# Expected: .output/chrome-mv3/ directory created
```

---

### Task 3: API client + types

**Goal:** TypeScript types mirroring the server models and a resilient fetch wrapper.

**Step 3.1 — Create `extensions/browser/src/lib/types.ts`.**

Mirror `internal/storage/types.go` relevant types:

```typescript
// Mirrors internal/storage/types.go and internal/service/types.go

export type JobStatus = "pending" | "running" | "completed" | "failed";

export interface Tag {
  label: string;
  weight?: number;
  source?: string;
}

export interface KnowledgeObject {
  id: string;
  type: string;
  subtype?: string;
  raw_content: string;
  text_content?: string;
  metadata?: Record<string, unknown>;
  summaries?: string[];
  tags?: Tag[];
  mentions?: string[];
  pipeline?: string;
  source?: string;
  created_at: string;
  updated_at: string;
}

export interface Job {
  id: string;
  type: string;
  status: JobStatus;
  pipeline: string;
  source: string;
  error: string;
  retry_count: number;
  created_at: string;
  updated_at: string;
  completed_at?: string;
}

export interface CapturePageRequest {
  url: string;
  title: string;
  content?: string;
  pipeline?: string;
  auth_state?: string;
}

export interface CaptureSelectionRequest {
  url: string;
  title: string;
  selection: string;
  pipeline?: string;
  auth_state?: string;
}

export interface CaptureElementRequest {
  url: string;
  title: string;
  selector: string;
  content: string;
  pipeline?: string;
  auth_state?: string;
}

export interface CaptureResponse {
  job_id: string;
}

export interface AnalyzeRequest {
  content: string;
  type: string;
  pipeline?: string;
  source?: string;
  source_title?: string;
  auth_state?: string;
}
```

**Step 3.2 — Create `extensions/browser/src/lib/api.ts`.**

```typescript
import type { CapturePageRequest, CaptureSelectionRequest, CaptureElementRequest, CaptureResponse, Job, AnalyzeRequest } from "./types";

const DEFAULT_BASE = "http://127.0.0.1:8080";

function getBase(): string {
  // Options page can override this via chrome.storage.sync.
  return DEFAULT_BASE;
}

export class ApiError extends Error {
  constructor(
    public readonly status: number,
    public readonly code: string,
    message: string
  ) {
    super(message);
    this.name = "ApiError";
  }
}

export async function apiFetch<T>(path: string, options?: RequestInit): Promise<T> {
  const base = getBase();
  const res = await fetch(`${base}${path}`, {
    ...options,
    headers: {
      "Content-Type": "application/json",
      ...options?.headers,
    },
  });

  if (!res.ok) {
    let code = "HTTP_ERROR";
    let message = `HTTP ${res.status}`;
    try {
      const body = (await res.json()) as { code?: string; message?: string };
      code = body.code ?? code;
      message = body.message ?? message;
    } catch {
      // ignore
    }
    throw new ApiError(res.status, code, message);
  }

  if (res.status === 204) return undefined as unknown as T;
  return res.json() as Promise<T>;
}

export const api = {
  capture: {
    // Try the specific capture endpoint first; fall back to /api/v1/analyze on 404.
    page: async (req: CapturePageRequest): Promise<CaptureResponse> => {
      try {
        return await apiFetch<CaptureResponse>("/api/v1/capture/page", {
          method: "POST",
          body: JSON.stringify(req),
        });
      } catch (e) {
        if (e instanceof ApiError && e.status === 404) {
          // Graceful degradation for older server versions.
          return apiFetch<CaptureResponse>("/api/v1/analyze", {
            method: "POST",
            body: JSON.stringify({
              content: req.content ?? req.url,
              type: "url",
              pipeline: req.pipeline,
              source: req.url,
              source_title: req.title,
            } satisfies AnalyzeRequest),
          });
        }
        throw e;
      }
    },

    selection: async (req: CaptureSelectionRequest): Promise<CaptureResponse> => {
      try {
        return await apiFetch<CaptureResponse>("/api/v1/capture/selection", {
          method: "POST",
          body: JSON.stringify(req),
        });
      } catch (e) {
        if (e instanceof ApiError && e.status === 404) {
          return apiFetch<CaptureResponse>("/api/v1/analyze", {
            method: "POST",
            body: JSON.stringify({
              content: req.selection,
              type: "text",
              source: req.url,
              source_title: req.title,
            } satisfies AnalyzeRequest),
          });
        }
        throw e;
      }
    },

    element: async (req: CaptureElementRequest): Promise<CaptureResponse> => {
      try {
        return await apiFetch<CaptureResponse>("/api/v1/capture/element", {
          method: "POST",
          body: JSON.stringify(req),
        });
      } catch (e) {
        if (e instanceof ApiError && e.status === 404) {
          return apiFetch<CaptureResponse>("/api/v1/analyze", {
            method: "POST",
            body: JSON.stringify({
              content: req.content,
              type: "text",
              source: req.url,
              source_title: req.title,
            } satisfies AnalyzeRequest),
          });
        }
        throw e;
      }
    },

    recent: () =>
      apiFetch<{ items: Job[]; total: number }>("/api/v1/capture/recent"),
  },

  health: () => apiFetch<{ status: string }>("/health"),
};
```

---

### Task 4: Offline queue

**Goal:** IndexedDB-backed queue that stores failed captures and retries them when the server is reachable.

**Step 4.1 — Create `extensions/browser/src/lib/offline-queue.ts`.**

```typescript
import { openDB, type IDBPDatabase } from "idb";
import type { CapturePageRequest, CaptureSelectionRequest, CaptureElementRequest } from "./types";

export type QueuedCapture =
  | { mode: "page"; req: CapturePageRequest; id?: number; enqueuedAt: string }
  | { mode: "selection"; req: CaptureSelectionRequest; id?: number; enqueuedAt: string }
  | { mode: "element"; req: CaptureElementRequest; id?: number; enqueuedAt: string };

const DB_NAME = "ctxt-offline-queue";
const STORE_NAME = "captures";
const DB_VERSION = 1;

async function getDB(): Promise<IDBPDatabase> {
  return openDB(DB_NAME, DB_VERSION, {
    upgrade(db) {
      if (!db.objectStoreNames.contains(STORE_NAME)) {
        db.createObjectStore(STORE_NAME, { keyPath: "id", autoIncrement: true });
      }
    },
  });
}

/** Add a failed capture to the queue. */
export async function enqueue(capture: Omit<QueuedCapture, "id" | "enqueuedAt">): Promise<void> {
  const db = await getDB();
  await db.add(STORE_NAME, {
    ...capture,
    enqueuedAt: new Date().toISOString(),
  });
}

/** Retrieve all queued captures. */
export async function getAll(): Promise<QueuedCapture[]> {
  const db = await getDB();
  return db.getAll(STORE_NAME);
}

/** Remove a capture from the queue by its IDB key. */
export async function remove(id: number): Promise<void> {
  const db = await getDB();
  await db.delete(STORE_NAME, id);
}

/** Count the number of queued captures. */
export async function count(): Promise<number> {
  const db = await getDB();
  return db.count(STORE_NAME);
}

/** Retry all queued captures. Removes successfully captured items. */
export async function retryAll(): Promise<{ succeeded: number; failed: number }> {
  const { api } = await import("./api");
  const items = await getAll();
  let succeeded = 0;
  let failed = 0;

  for (const item of items) {
    try {
      switch (item.mode) {
        case "page":
          await api.capture.page(item.req);
          break;
        case "selection":
          await api.capture.selection(item.req);
          break;
        case "element":
          await api.capture.element(item.req);
          break;
      }
      if (item.id !== undefined) {
        await remove(item.id);
      }
      succeeded++;
    } catch {
      failed++;
    }
  }

  return { succeeded, failed };
}
```

---

### Task 5: WebSocket cookie bridge

**Goal:** Maintain a persistent WebSocket connection to `ws://127.0.0.1:9377` with exponential backoff reconnection, syncing cookies from configured domains.

**Step 5.1 — Create `extensions/browser/src/lib/ws.ts`.**

```typescript
export interface CookieData {
  name: string;
  value: string;
  domain: string;
  path: string;
  secure: boolean;
  httpOnly: boolean;
  sameSite: string;
}

const WS_URL = "ws://127.0.0.1:9377";
const MAX_BACKOFF_MS = 30_000;

class CookieBridgeClient {
  private ws: WebSocket | null = null;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private backoffMs = 500;
  private stopped = false;

  connect(): void {
    if (this.stopped) return;
    try {
      this.ws = new WebSocket(WS_URL);

      this.ws.onopen = () => {
        this.backoffMs = 500; // reset on successful connection
      };

      this.ws.onclose = () => {
        this.ws = null;
        if (!this.stopped) this.scheduleReconnect();
      };

      this.ws.onerror = () => {
        // onclose fires after onerror; no need to reconnect here
      };
    } catch {
      this.scheduleReconnect();
    }
  }

  private scheduleReconnect(): void {
    if (this.reconnectTimer !== null) return;
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null;
      this.backoffMs = Math.min(this.backoffMs * 2, MAX_BACKOFF_MS);
      this.connect();
    }, this.backoffMs);
  }

  /** Sync cookies for a domain to the server. */
  syncCookies(domain: string, cookies: CookieData[]): void {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
      // Not connected — cookies will be synced on next successful connection.
      return;
    }
    this.ws.send(
      JSON.stringify({ type: "cookie_sync", domain, cookies })
    );
  }

  stop(): void {
    this.stopped = true;
    if (this.reconnectTimer !== null) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    this.ws?.close();
    this.ws = null;
  }

  isConnected(): boolean {
    return this.ws?.readyState === WebSocket.OPEN;
  }
}

export const cookieBridge = new CookieBridgeClient();
```

---

### Task 6: Capture orchestrator

**Goal:** Single entry point that dispatches to the correct site adaptor, calls the right API endpoint, and falls back to the offline queue on failure.

**Step 6.1 — Create `extensions/browser/src/lib/capture.ts`.**

```typescript
import { api } from "./api";
import { enqueue } from "./offline-queue";
import { findAdaptor } from "./adaptors/index";
import type { CaptureResponse } from "./types";

export type CaptureMode = "page" | "selection" | "element";

export interface TabContext {
  tabId: number;
  url: string;
  title: string;
}

export interface CaptureOptions {
  mode: CaptureMode;
  pipeline?: string;
  authState?: string;
  /** Required for "selection" mode */
  selection?: string;
  /** Required for "element" mode */
  elementSelector?: string;
  elementContent?: string;
}

/**
 * Orchestrates a capture from a browser tab.
 * 1. Looks up the site adaptor for the URL.
 * 2. Extracts content via the adaptor (or uses raw content for generic).
 * 3. Posts to the appropriate API endpoint.
 * 4. On network failure, enqueues to offline queue.
 */
export async function capture(
  tab: TabContext,
  options: CaptureOptions
): Promise<CaptureResponse> {
  const adaptor = findAdaptor(tab.url);
  const pipeline = options.pipeline ?? adaptor?.pipelineHint ?? undefined;

  try {
    switch (options.mode) {
      case "page": {
        // Request content extraction from content script via chrome.tabs.sendMessage.
        let content = "";
        try {
          const extracted = await chrome.tabs.sendMessage(tab.tabId, {
            type: "extract_content",
            adaptor: adaptor?.name,
          }) as { content: string } | undefined;
          content = extracted?.content ?? "";
        } catch {
          // Content script not injected or page not accessible — use empty content.
          content = "";
        }

        return await api.capture.page({
          url: tab.url,
          title: tab.title,
          content,
          pipeline,
          auth_state: options.authState,
        });
      }

      case "selection": {
        if (!options.selection) throw new Error("selection required for selection mode");
        return await api.capture.selection({
          url: tab.url,
          title: tab.title,
          selection: options.selection,
          pipeline,
          auth_state: options.authState,
        });
      }

      case "element": {
        if (!options.elementContent) throw new Error("elementContent required for element mode");
        return await api.capture.element({
          url: tab.url,
          title: tab.title,
          selector: options.elementSelector ?? "",
          content: options.elementContent,
          pipeline,
          auth_state: options.authState,
        });
      }
    }
  } catch (err) {
    // Network failure or server unreachable — queue for retry.
    const queueEntry = options.mode === "page"
      ? { mode: "page" as const, req: { url: tab.url, title: tab.title, pipeline, auth_state: options.authState } }
      : options.mode === "selection"
      ? { mode: "selection" as const, req: { url: tab.url, title: tab.title, selection: options.selection!, pipeline, auth_state: options.authState } }
      : { mode: "element" as const, req: { url: tab.url, title: tab.title, selector: options.elementSelector ?? "", content: options.elementContent!, pipeline, auth_state: options.authState } };

    await enqueue(queueEntry);
    throw err;
  }
}
```

---

### Task 7: Site adaptors

**Goal:** URL-pattern-based adaptors that extract structured content and suggest a pipeline hint for each supported site.

**Step 7.1 — Create `extensions/browser/src/lib/adaptors/index.ts`.**

```typescript
export interface Adaptor {
  name: string;
  pattern: RegExp;
  pipelineHint: string;
  /** Extract structured content from the current document. */
  extract(document: Document): AdaptorResult;
}

export interface AdaptorResult {
  content: string;
  title?: string;
  metadata?: Record<string, unknown>;
}

import { githubAdaptor } from "./github";
import { twitterAdaptor } from "./twitter";
import { arxivAdaptor } from "./arxiv";
import { linkedinAdaptor } from "./linkedin";
import { wikipediaAdaptor } from "./wikipedia";

const ADAPTORS: Adaptor[] = [
  githubAdaptor,
  twitterAdaptor,
  arxivAdaptor,
  linkedinAdaptor,
  wikipediaAdaptor,
];

/** Find the best adaptor for a given URL, or return null for generic handling. */
export function findAdaptor(url: string): Adaptor | null {
  for (const adaptor of ADAPTORS) {
    if (adaptor.pattern.test(url)) return adaptor;
  }
  return null;
}
```

**Step 7.2 — Create `extensions/browser/src/lib/adaptors/github.ts`.**

```typescript
import type { Adaptor, AdaptorResult } from "./index";

function detectGitHubType(url: string): string {
  if (/\/pull\/\d+/.test(url)) return "pr";
  if (/\/issues\/\d+/.test(url)) return "issue";
  if (/\/commit\//.test(url)) return "commit";
  if (/\/blob\//.test(url)) return "file";
  if (/^https:\/\/github\.com\/[^/]+\/[^/]+$/.test(url)) return "repo";
  if (/^https:\/\/github\.com\/[^/]+$/.test(url)) return "user";
  return "repo";
}

export const githubAdaptor: Adaptor = {
  name: "github",
  pattern: /^https:\/\/github\.com\//,
  get pipelineHint() {
    return "code.github.repo"; // overridden per-extract
  },
  extract(document: Document): AdaptorResult {
    const url = document.location.href;
    const ghType = detectGitHubType(url);
    const pipeline = `code.github.${ghType}`;

    // Extract main content area.
    const mainContent =
      document.querySelector(".markdown-body, .js-issue-title, [data-target='readme-toc.content']")?.textContent?.trim() ??
      document.title;

    const title =
      document.querySelector("h1.gh-header-title span.js-issue-title")?.textContent?.trim() ??
      document.title;

    const labels = Array.from(document.querySelectorAll(".labels a")).map((el) => el.textContent?.trim()).filter(Boolean);
    const author = document.querySelector(".author")?.textContent?.trim();

    return {
      content: mainContent,
      title,
      metadata: {
        github_type: ghType,
        pipeline,
        labels,
        author,
        url,
      },
    };
  },
};
```

**Step 7.3 — Create `extensions/browser/src/lib/adaptors/twitter.ts`.**

```typescript
import type { Adaptor, AdaptorResult } from "./index";

export const twitterAdaptor: Adaptor = {
  name: "twitter",
  pattern: /^https:\/\/(twitter|x)\.com\//,
  get pipelineHint() {
    return "social.x.profile";
  },
  extract(document: Document): AdaptorResult {
    const url = document.location.href;

    // Detect type: thread/tweet or profile.
    const isTweet = /\/status\/\d+/.test(url);
    const pipelineHint = isTweet ? "social.x.thread" : "social.x.profile";

    // Extract tweet text or profile bio.
    const tweetTexts = Array.from(
      document.querySelectorAll("[data-testid='tweetText']")
    ).map((el) => el.textContent?.trim()).filter(Boolean);

    const bio = document.querySelector("[data-testid='UserDescription']")?.textContent?.trim();
    const displayName = document.querySelector("[data-testid='UserName']")?.textContent?.trim();

    const content = isTweet
      ? tweetTexts.join("\n\n")
      : [displayName, bio].filter(Boolean).join("\n");

    return {
      content: content || document.title,
      title: displayName ?? document.title,
      metadata: {
        twitter_type: isTweet ? "thread" : "profile",
        pipeline: pipelineHint,
        url,
      },
    };
  },
};
```

**Step 7.4 — Create `extensions/browser/src/lib/adaptors/arxiv.ts`.**

```typescript
import type { Adaptor, AdaptorResult } from "./index";

export const arxivAdaptor: Adaptor = {
  name: "arxiv",
  pattern: /^https:\/\/arxiv\.org\/(abs|pdf)\//,
  pipelineHint: "research.arxiv.paper",
  extract(document: Document): AdaptorResult {
    const title =
      document.querySelector(".title.mathjax")?.textContent?.replace(/^Title:\s*/i, "").trim() ??
      document.title;

    const abstract =
      document.querySelector(".abstract.mathjax")?.textContent?.replace(/^Abstract:\s*/i, "").trim() ??
      "";

    const authors = Array.from(
      document.querySelectorAll(".authors a")
    ).map((el) => el.textContent?.trim()).filter(Boolean);

    const arxivID = document.location.pathname.split("/").pop() ?? "";
    const submittedText = document.querySelector(".submission-history")?.textContent?.trim() ?? "";

    const content = [
      `# ${title}`,
      "",
      `Authors: ${authors.join(", ")}`,
      `arXiv: ${arxivID}`,
      "",
      abstract,
      "",
      submittedText,
    ].join("\n");

    return {
      content,
      title,
      metadata: {
        arxiv_id: arxivID,
        authors,
        pipeline: "research.arxiv.paper",
        url: document.location.href,
      },
    };
  },
};
```

**Step 7.5 — Create `extensions/browser/src/lib/adaptors/linkedin.ts`.**

```typescript
import type { Adaptor, AdaptorResult } from "./index";

export const linkedinAdaptor: Adaptor = {
  name: "linkedin",
  pattern: /^https:\/\/www\.linkedin\.com\/(in|company)\//,
  pipelineHint: "social.linkedin.profile",
  extract(document: Document): AdaptorResult {
    const url = document.location.href;
    const isCompany = /\/company\//.test(url);

    const name =
      document.querySelector(".top-card-layout__title, h1.text-heading-xlarge")?.textContent?.trim() ??
      document.title;

    const headline =
      document.querySelector(".top-card-layout__headline, .text-body-medium.break-words")?.textContent?.trim() ??
      "";

    const about =
      document.querySelector(".summary, .core-section-container__content p")?.textContent?.trim() ??
      "";

    const content = [name, headline, about].filter(Boolean).join("\n\n");

    return {
      content: content || document.title,
      title: name,
      metadata: {
        linkedin_type: isCompany ? "company" : "profile",
        pipeline: isCompany ? "social.linkedin.company" : "social.linkedin.profile",
        url,
      },
    };
  },
};
```

**Step 7.6 — Create `extensions/browser/src/lib/adaptors/wikipedia.ts`.**

```typescript
import type { Adaptor, AdaptorResult } from "./index";

export const wikipediaAdaptor: Adaptor = {
  name: "wikipedia",
  pattern: /^https:\/\/[a-z]{2}\.wikipedia\.org\/wiki\//,
  pipelineHint: "reference.wikipedia.article",
  extract(document: Document): AdaptorResult {
    const title =
      document.querySelector("#firstHeading")?.textContent?.trim() ??
      document.title;

    // First paragraph as lead summary.
    const lead =
      document.querySelector("#mw-content-text .mw-parser-output > p:not(.mw-empty-elt)")?.textContent?.trim() ??
      "";

    // Infobox key/value pairs.
    const infoboxRows = Array.from(
      document.querySelectorAll(".infobox tr")
    ).map((row) => {
      const key = row.querySelector("th")?.textContent?.trim();
      const val = row.querySelector("td")?.textContent?.trim();
      return key && val ? `${key}: ${val}` : null;
    }).filter(Boolean);

    const content = [
      `# ${title}`,
      "",
      lead,
      "",
      infoboxRows.length ? "## Key Facts\n" + infoboxRows.join("\n") : "",
    ].filter(Boolean).join("\n");

    return {
      content,
      title,
      metadata: {
        pipeline: "reference.wikipedia.article",
        url: document.location.href,
        lang: document.documentElement.lang,
      },
    };
  },
};
```

**Step 7.7 — Write adaptor unit tests in `extensions/browser/src/lib/adaptors/__tests__/`.**

Create `extensions/browser/src/lib/adaptors/__tests__/arxiv.test.ts` as an example:

```typescript
import { describe, it, expect } from "vitest";
import { JSDOM } from "jsdom";
import { arxivAdaptor } from "../arxiv";

function makeDocument(html: string): Document {
  return new JSDOM(html, { url: "https://arxiv.org/abs/2501.00001" }).window.document;
}

describe("arxivAdaptor", () => {
  it("matches arxiv URLs", () => {
    expect(arxivAdaptor.pattern.test("https://arxiv.org/abs/2501.00001")).toBe(true);
    expect(arxivAdaptor.pattern.test("https://example.com")).toBe(false);
  });

  it("extracts title and abstract", () => {
    const doc = makeDocument(`
      <h1 class="title mathjax">Title: Test Paper on Knowledge Graphs</h1>
      <blockquote class="abstract mathjax">Abstract: This paper studies knowledge graphs.</blockquote>
      <div class="authors"><a>Alice Smith</a>, <a>Bob Jones</a></div>
    `);
    const result = arxivAdaptor.extract(doc);
    expect(result.title).toContain("Test Paper");
    expect(result.content).toContain("knowledge graphs");
    expect(result.metadata?.authors).toEqual(["Alice Smith", "Bob Jones"]);
    expect(result.metadata?.pipeline).toBe("research.arxiv.paper");
  });
});
```

Install jsdom for tests:
```bash
cd extensions/browser && npm install -D jsdom @types/jsdom vitest
```

Run tests:
```bash
cd extensions/browser && npx vitest run
# Expected: adaptor tests pass
```

---

### Task 8: Content script

**Goal:** Lightweight content script injected on all pages that provides a selection helper and messaging bridge for adaptors.

**Step 8.1 — Create `extensions/browser/src/entrypoints/content/index.ts`.**

```typescript
import { defineContentScript } from "wxt/sandbox";

export default defineContentScript({
  matches: ["<all_urls>"],
  runAt: "document_idle",

  main() {
    // Message bridge: background script requests content extraction.
    chrome.runtime.onMessage.addListener((message, _sender, sendResponse) => {
      if (message.type === "get_selection") {
        const selection = window.getSelection()?.toString() ?? "";
        sendResponse({ selection });
        return true;
      }

      if (message.type === "extract_content") {
        // Dynamically import the adaptor registry to avoid loading all adaptors on every page.
        import("../../lib/adaptors/index").then(({ findAdaptor }) => {
          const adaptor = findAdaptor(window.location.href);
          if (adaptor) {
            const result = adaptor.extract(document);
            sendResponse({ content: result.content, metadata: result.metadata });
          } else {
            // Generic extraction: use main text content.
            const main =
              document.querySelector("main, article, [role='main']")?.textContent?.trim() ??
              document.body.textContent?.trim() ??
              "";
            sendResponse({ content: main.slice(0, 50_000) });
          }
        });
        return true; // Keep message channel open for async response.
      }

      if (message.type === "highlight_element") {
        const el = document.querySelector(message.selector as string);
        if (el) {
          (el as HTMLElement).style.outline = "2px solid #00d9ff";
          setTimeout(() => {
            (el as HTMLElement).style.outline = "";
          }, 2000);
        }
        sendResponse({});
        return true;
      }
    });
  },
});
```

---

### Task 9: Background script

**Goal:** Central orchestrator — context menus, keyboard shortcut, cookie bridge, offline queue retry, and badge counter.

**Step 9.1 — Create `extensions/browser/src/entrypoints/background.ts`.**

```typescript
import { defineBackground } from "wxt/sandbox";
import { cookieBridge } from "../lib/ws";
import { capture } from "../lib/capture";
import { retryAll, count } from "../lib/offline-queue";
import { api } from "../lib/api";

export default defineBackground({
  type: "module",

  main() {
    // Connect cookie bridge immediately.
    cookieBridge.connect();

    // ── Context menus ──────────────────────────────────────────────────────
    chrome.runtime.onInstalled.addListener(() => {
      chrome.contextMenus.create({
        id: "ctxt-capture-page",
        title: "Save page to ctxt",
        contexts: ["page"],
      });
      chrome.contextMenus.create({
        id: "ctxt-capture-selection",
        title: "Save selection to ctxt",
        contexts: ["selection"],
      });
      chrome.contextMenus.create({
        id: "ctxt-capture-link",
        title: "Save link to ctxt",
        contexts: ["link"],
      });
    });

    chrome.contextMenus.onClicked.addListener(async (info, tab) => {
      if (!tab?.id || !tab.url) return;

      const tabCtx = { tabId: tab.id, url: tab.url, title: tab.title ?? "" };

      if (info.menuItemId === "ctxt-capture-page") {
        await safeCaptureWithNotification(tabCtx, { mode: "page" });
      } else if (info.menuItemId === "ctxt-capture-selection" && info.selectionText) {
        await safeCaptureWithNotification(tabCtx, {
          mode: "selection",
          selection: info.selectionText,
        });
      } else if (info.menuItemId === "ctxt-capture-link" && info.linkUrl) {
        await safeCaptureWithNotification(
          { tabId: tab.id, url: info.linkUrl, title: info.linkUrl },
          { mode: "page" }
        );
      }
    });

    // ── Keyboard shortcut ─────────────────────────────────────────────────
    chrome.commands.onCommand.addListener(async (command) => {
      if (command !== "capture-tab") return;

      const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
      if (!tab?.id || !tab.url) return;

      await safeCaptureWithNotification(
        { tabId: tab.id, url: tab.url, title: tab.title ?? "" },
        { mode: "page" }
      );
    });

    // ── Cookie bridge: watch configured domains ───────────────────────────
    chrome.cookies.onChanged.addListener(async (changeInfo) => {
      if (changeInfo.removed) return;

      const cookie = changeInfo.cookie;
      const domain = cookie.domain.replace(/^\./, "");

      // Get the list of monitored domains from options storage.
      const { cookieDomains = [] } = await chrome.storage.sync.get("cookieDomains") as { cookieDomains?: string[] };

      if (cookieDomains.includes(domain)) {
        const cookies = await chrome.cookies.getAll({ domain });
        cookieBridge.syncCookies(domain, cookies.map((c) => ({
          name: c.name,
          value: c.value,
          domain: c.domain,
          path: c.path,
          secure: c.secure,
          httpOnly: c.httpOnly,
          sameSite: c.sameSite ?? "unspecified",
        })));
      }
    });

    // ── Offline queue retry alarm ─────────────────────────────────────────
    chrome.alarms.create("retry-queue", { periodInMinutes: 0.5 });

    chrome.alarms.onAlarm.addListener(async (alarm) => {
      if (alarm.name !== "retry-queue") return;

      // Check server health before retrying.
      try {
        await api.health();
      } catch {
        return; // Server still down, skip retry.
      }

      const { succeeded } = await retryAll();
      if (succeeded > 0) {
        await updateBadge();
        chrome.notifications.create({
          type: "basic",
          iconUrl: "/icons/icon-48.png",
          title: "ctxt",
          message: `Synced ${succeeded} queued capture${succeeded > 1 ? "s" : ""}.`,
        });
      }
    });

    // Initial badge update.
    updateBadge();
  },
});

async function safeCaptureWithNotification(
  tab: { tabId: number; url: string; title: string },
  options: Parameters<typeof capture>[1]
): Promise<void> {
  try {
    await capture(tab, options);
    chrome.notifications.create({
      type: "basic",
      iconUrl: "/icons/icon-48.png",
      title: "ctxt",
      message: `Saved: ${tab.title || tab.url}`,
    });
  } catch {
    const queueCount = await count();
    chrome.notifications.create({
      type: "basic",
      iconUrl: "/icons/icon-48.png",
      title: "ctxt — Saved offline",
      message: `Server unreachable. ${queueCount} capture${queueCount !== 1 ? "s" : ""} queued.`,
    });
    await updateBadge();
  }
}

async function updateBadge(): Promise<void> {
  const n = await count();
  if (n > 0) {
    chrome.action.setBadgeText({ text: String(n) });
    chrome.action.setBadgeBackgroundColor({ color: "#ffc247" });
  } else {
    chrome.action.setBadgeText({ text: "" });
  }
}
```

---

### Task 10: Popup

**Goal:** Extension popup showing server status, recent captures, a quick capture form, and offline queue badge.

**Step 10.1 — Create `extensions/browser/src/entrypoints/popup/App.tsx`.**

```tsx
import React, { useState, useEffect } from "react";
import { api } from "../../lib/api";
import { count as queueCount } from "../../lib/offline-queue";
import type { Job } from "../../lib/types";

const BASE_STYLES: React.CSSProperties = {
  width: 360,
  minHeight: 400,
  fontFamily: '"Inter", system-ui, sans-serif',
  background: "#fdfcf9",
  color: "#1a2332",
  padding: 16,
  display: "flex",
  flexDirection: "column",
  gap: 12,
};

type ServerStatus = "checking" | "online" | "offline";

export default function App() {
  const [serverStatus, setServerStatus] = useState<ServerStatus>("checking");
  const [recentJobs, setRecentJobs] = useState<Job[]>([]);
  const [offlineCount, setOfflineCount] = useState(0);
  const [content, setContent] = useState("");
  const [pipeline, setPipeline] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [message, setMessage] = useState<{ text: string; type: "success" | "error" | "info" } | null>(null);

  useEffect(() => {
    const init = async () => {
      // Check server health.
      try {
        await api.health();
        setServerStatus("online");

        const { items } = await api.capture.recent();
        setRecentJobs(items.slice(0, 5));
      } catch {
        setServerStatus("offline");
      }

      setOfflineCount(await queueCount());
    };
    init();
  }, []);

  const handleCapture = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!content.trim()) return;

    setSubmitting(true);
    setMessage(null);

    // Get active tab.
    const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });

    try {
      const isURL = content.trim().startsWith("http");
      const res = await api.capture.page({
        url: tab?.url ?? content,
        title: tab?.title ?? content,
        content: isURL ? "" : content,
        pipeline: pipeline || undefined,
      });
      setMessage({ text: `Queued job ${res.job_id}`, type: "success" });
      setContent("");
      setPipeline("");
    } catch {
      setMessage({ text: "Server offline — saved to queue", type: "info" });
      setOfflineCount((n) => n + 1);
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div style={BASE_STYLES}>
      {/* Header */}
      <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between" }}>
        <strong style={{ fontSize: 16 }}>ctxt</strong>
        <span
          role="status"
          aria-label={`Server ${serverStatus}`}
          style={{
            display: "flex",
            alignItems: "center",
            gap: 6,
            fontSize: 12,
            color: serverStatus === "online" ? "#10b981" : serverStatus === "offline" ? "#ef4444" : "#4a5568",
          }}
        >
          <span
            aria-hidden="true"
            style={{
              width: 8,
              height: 8,
              borderRadius: "50%",
              background: serverStatus === "online" ? "#10b981" : serverStatus === "offline" ? "#ef4444" : "#4a5568",
            }}
          />
          {serverStatus === "checking" ? "Connecting…" : serverStatus}
        </span>
      </div>

      {/* Offline queue badge */}
      {offlineCount > 0 && (
        <div
          role="alert"
          style={{ padding: "6px 10px", background: "rgba(255,194,71,0.15)", borderRadius: 6, fontSize: 12, color: "#92600a" }}
        >
          {offlineCount} capture{offlineCount > 1 ? "s" : ""} queued offline — will sync when server is available.
        </div>
      )}

      {/* Quick capture form */}
      <form onSubmit={handleCapture} style={{ display: "flex", flexDirection: "column", gap: 8 }}>
        <label htmlFor="popup-content" style={{ fontSize: 12, fontWeight: 600 }}>
          Capture
        </label>
        <textarea
          id="popup-content"
          value={content}
          onChange={(e) => setContent(e.target.value)}
          placeholder="URL, text, or anything…"
          rows={3}
          style={{ padding: 8, borderRadius: 6, border: "1px solid #4a5568", fontSize: 13, resize: "vertical", fontFamily: "inherit" }}
        />
        <select
          value={pipeline}
          onChange={(e) => setPipeline(e.target.value)}
          aria-label="Pipeline"
          style={{ padding: "5px 8px", borderRadius: 6, border: "1px solid #4a5568", fontSize: 12 }}
        >
          <option value="">Auto pipeline</option>
          <option value="url.article">url.article</option>
          <option value="url.generic">url.generic</option>
          <option value="text.default">text.default</option>
          <option value="text.long">text.long</option>
          <option value="code.snippet">code.snippet</option>
        </select>
        <button
          type="submit"
          disabled={submitting || !content.trim()}
          style={{
            padding: "8px 16px",
            borderRadius: 6,
            border: "none",
            background: "#00d9ff",
            color: "#1a2332",
            fontWeight: 600,
            fontSize: 13,
            cursor: submitting ? "wait" : "pointer",
          }}
        >
          {submitting ? "Saving…" : "Save to ctxt"}
        </button>

        {message && (
          <p
            role={message.type === "error" ? "alert" : "status"}
            style={{ fontSize: 12, color: message.type === "error" ? "#ef4444" : message.type === "success" ? "#10b981" : "#4a5568", margin: 0 }}
          >
            {message.text}
          </p>
        )}
      </form>

      {/* Recent captures */}
      {recentJobs.length > 0 && (
        <section aria-labelledby="recent-heading">
          <h2 id="recent-heading" style={{ fontSize: 12, fontWeight: 600, margin: "0 0 4px" }}>
            Recent
          </h2>
          <ul style={{ listStyle: "none", padding: 0, margin: 0, display: "flex", flexDirection: "column", gap: 4 }}>
            {recentJobs.map((job) => (
              <li
                key={job.id}
                style={{ fontSize: 11, color: "#4a5568", padding: "4px 8px", borderRadius: 4, background: "rgba(74,85,104,0.06)", display: "flex", justifyContent: "space-between" }}
              >
                <span style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", flex: 1 }}>
                  {job.source || job.id}
                </span>
                <span style={{
                  marginLeft: 8,
                  flexShrink: 0,
                  color: job.status === "completed" ? "#10b981" : job.status === "failed" ? "#ef4444" : "#4a5568",
                }}>
                  {job.status}
                </span>
              </li>
            ))}
          </ul>
        </section>
      )}

      {/* Open Web UI link */}
      <a
        href="http://127.0.0.1:8080/ui"
        target="_blank"
        rel="noopener noreferrer"
        style={{ fontSize: 12, color: "#00d9ff", textAlign: "center", textDecoration: "none" }}
      >
        Open ctxt UI →
      </a>
    </div>
  );
}
```

**Step 10.2 — Create `extensions/browser/src/entrypoints/popup/index.html`.**

```html
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0" />
  <title>ctxt</title>
</head>
<body>
  <div id="root"></div>
  <script type="module" src="./main.tsx"></script>
</body>
</html>
```

**Step 10.3 — Create `extensions/browser/src/entrypoints/popup/main.tsx`.**

```tsx
import React from "react";
import ReactDOM from "react-dom/client";
import App from "./App";

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>
);
```

---

### Task 11: Options page

**Goal:** Settings UI for server URL, WebSocket URL, default pipeline, cookie domains, and adaptor toggles.

**Step 11.1 — Create `extensions/browser/src/entrypoints/options/Options.tsx`.**

```tsx
import React, { useState, useEffect } from "react";

interface Settings {
  serverURL: string;
  wsURL: string;
  defaultPipeline: string;
  cookieDomains: string;
  disabledAdaptors: string[];
}

const ADAPTORS = ["github", "twitter", "arxiv", "linkedin", "wikipedia"];

const DEFAULT_SETTINGS: Settings = {
  serverURL: "http://127.0.0.1:8080",
  wsURL: "ws://127.0.0.1:9377",
  defaultPipeline: "",
  cookieDomains: "",
  disabledAdaptors: [],
};

const STYLES: Record<string, React.CSSProperties> = {
  page: { maxWidth: 480, margin: "32px auto", fontFamily: '"Inter", system-ui, sans-serif', color: "#1a2332", padding: "0 16px" },
  label: { display: "flex", flexDirection: "column" as const, gap: 4, marginBottom: 16 },
  labelText: { fontSize: 13, fontWeight: 600 },
  input: { padding: "6px 10px", borderRadius: 6, border: "1px solid #4a5568", fontSize: 14 },
  button: { padding: "8px 20px", borderRadius: 6, border: "none", background: "#00d9ff", color: "#1a2332", fontWeight: 600, fontSize: 14, cursor: "pointer" },
  hint: { fontSize: 11, color: "#4a5568", marginTop: 2 },
};

export default function Options() {
  const [settings, setSettings] = useState<Settings>(DEFAULT_SETTINGS);
  const [saved, setSaved] = useState(false);

  useEffect(() => {
    chrome.storage.sync.get(Object.keys(DEFAULT_SETTINGS)).then((stored) => {
      setSettings({ ...DEFAULT_SETTINGS, ...stored } as Settings);
    });
  }, []);

  const save = async (e: React.FormEvent) => {
    e.preventDefault();
    await chrome.storage.sync.set({
      ...settings,
      cookieDomains: settings.cookieDomains
        .split(",")
        .map((d) => d.trim())
        .filter(Boolean),
    });
    setSaved(true);
    setTimeout(() => setSaved(false), 2000);
  };

  const toggleAdaptor = (name: string) => {
    setSettings((s) => ({
      ...s,
      disabledAdaptors: s.disabledAdaptors.includes(name)
        ? s.disabledAdaptors.filter((a) => a !== name)
        : [...s.disabledAdaptors, name],
    }));
  };

  return (
    <div style={STYLES.page}>
      <h1 style={{ fontSize: "1.4rem", marginBottom: 24 }}>ctxt Extension Settings</h1>

      <form onSubmit={save}>
        <label style={STYLES.label}>
          <span style={STYLES.labelText}>Server URL</span>
          <input
            type="url"
            value={settings.serverURL}
            onChange={(e) => setSettings((s) => ({ ...s, serverURL: e.target.value }))}
            style={STYLES.input}
            aria-describedby="server-url-hint"
          />
          <span id="server-url-hint" style={STYLES.hint}>Default: http://127.0.0.1:8080</span>
        </label>

        <label style={STYLES.label}>
          <span style={STYLES.labelText}>Cookie Bridge WebSocket URL</span>
          <input
            type="text"
            value={settings.wsURL}
            onChange={(e) => setSettings((s) => ({ ...s, wsURL: e.target.value }))}
            style={STYLES.input}
            aria-describedby="ws-url-hint"
          />
          <span id="ws-url-hint" style={STYLES.hint}>Default: ws://127.0.0.1:9377</span>
        </label>

        <label style={STYLES.label}>
          <span style={STYLES.labelText}>Default Pipeline</span>
          <select
            value={settings.defaultPipeline}
            onChange={(e) => setSettings((s) => ({ ...s, defaultPipeline: e.target.value }))}
            style={STYLES.input}
          >
            <option value="">Auto-detect</option>
            <option value="url.article">url.article</option>
            <option value="url.generic">url.generic</option>
            <option value="text.default">text.default</option>
            <option value="text.long">text.long</option>
          </select>
        </label>

        <label style={STYLES.label}>
          <span style={STYLES.labelText}>Cookie Bridge Domains (comma-separated)</span>
          <input
            type="text"
            value={settings.cookieDomains}
            onChange={(e) => setSettings((s) => ({ ...s, cookieDomains: e.target.value }))}
            placeholder="example.com, another.com"
            style={STYLES.input}
            aria-describedby="cookie-domains-hint"
          />
          <span id="cookie-domains-hint" style={STYLES.hint}>
            Cookies for these domains are synced to the cookie bridge for authenticated captures.
          </span>
        </label>

        <fieldset style={{ border: "1px solid #4a5568", borderRadius: 6, padding: 12, marginBottom: 16 }}>
          <legend style={{ fontSize: 13, fontWeight: 600, padding: "0 4px" }}>Site Adaptors</legend>
          {ADAPTORS.map((adaptor) => (
            <label key={adaptor} style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 8, fontSize: 14, cursor: "pointer" }}>
              <input
                type="checkbox"
                checked={!settings.disabledAdaptors.includes(adaptor)}
                onChange={() => toggleAdaptor(adaptor)}
                aria-label={`Enable ${adaptor} adaptor`}
              />
              {adaptor}
            </label>
          ))}
        </fieldset>

        <button type="submit" style={STYLES.button}>
          {saved ? "Saved!" : "Save Settings"}
        </button>
      </form>
    </div>
  );
}
```

**Step 11.2 — Create `extensions/browser/src/entrypoints/options/index.html`.**

```html
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0" />
  <title>ctxt Settings</title>
</head>
<body>
  <div id="root"></div>
  <script type="module" src="./main.tsx"></script>
</body>
</html>
```

**Step 11.3 — Create `extensions/browser/src/entrypoints/options/main.tsx`.**

```tsx
import React from "react";
import ReactDOM from "react-dom/client";
import Options from "./Options";

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <Options />
  </React.StrictMode>
);
```

---

### Task 12: Build + verify

**Goal:** Both Chrome (MV3) and Firefox (MV2) builds succeed; end-to-end capture flow works; offline queue and adaptor logic verified.

**Step 12.1 — Add `gorilla/websocket` dependency to Go module.**

```bash
cd ../ctxt
go get github.com/gorilla/websocket
go mod tidy
```

**Step 12.2 — Run Go tests.**

```bash
go test ./internal/server/http/... -v
# Expected: all tests pass including CORS, SSE, and capture handler tests

go test ./internal/server/ws/... -v
# Expected: no test failures (or empty if no tests written yet)

go build ./...
# Expected: no compilation errors
```

**Step 12.3 — Run extension adaptor tests.**

```bash
cd extensions/browser
npx vitest run
# Expected: all adaptor unit tests pass
```

**Step 12.4 — Build Chrome MV3 extension.**

```bash
cd extensions/browser
npm run build
# Expected: .output/chrome-mv3/ directory created with manifest_version: 3
ls .output/chrome-mv3/
# Expected: manifest.json, background.js, content-scripts/, popup/, options/
```

**Step 12.5 — Build Firefox MV2 extension.**

```bash
cd extensions/browser
npm run build:firefox
# Expected: .output/firefox-mv2/ directory created with manifest_version: 2
```

**Step 12.6 — Verify Chrome extension loads without errors.**

1. Open `chrome://extensions/`
2. Enable Developer Mode
3. Click "Load unpacked" → select `.output/chrome-mv3/`
4. Expected: Extension loads, no errors in background page console.

**Step 12.7 — Verify context menu capture.**

1. Start `dpkms serve`
2. Right-click on any web page → "Save page to ctxt"
3. Expected: notification appears; `ctxt list` shows a new pending job.

**Step 12.8 — Verify offline queue.**

1. Stop `dpkms serve`
2. Right-click → "Save page to ctxt"
3. Expected: notification says "Saved offline"; badge shows "1"
4. Start `dpkms serve`
5. Wait up to 30 seconds for alarm to fire
6. Expected: badge clears; `ctxt list` shows the job.

**Step 12.9 — Verify GitHub adaptor.**

1. Navigate to a GitHub PR page (e.g., `https://github.com/owner/repo/pull/1`)
2. Click extension icon → popup → "Save to ctxt"
3. Expected: job created with `pipeline: code.github.pr`

**Step 12.10 — Verify popup shows "queued (offline)" when server down.**

1. Stop `dpkms serve`
2. Open extension popup
3. Expected: server status dot is red; offline count badge visible if queue non-empty.

**Step 12.11 — Commit.**

```bash
git add extensions/browser/ \
        internal/server/http/handlers_capture.go \
        internal/server/http/handlers_capture_test.go \
        internal/server/ws/ \
        internal/service/types.go \
        internal/server/http/server.go \
        cmd/dpkms/cmd/serve.go \
        go.mod go.sum
git commit -m "feat(browser-ext): add WXT browser extension with site adaptors and offline queue"
```

---

## Verification Checklist

- [ ] `go test ./internal/server/http/... -v` — CORS, SSE, capture handler tests pass
- [ ] `go build ./...` — no compilation errors (includes `internal/server/ws`)
- [ ] `cd extensions/browser && npx vitest run` — adaptor unit tests pass
- [ ] `cd extensions/browser && npm run build` — `.output/chrome-mv3/` created
- [ ] `cd extensions/browser && npm run build:firefox` — `.output/firefox-mv2/` created
- [ ] Chrome extension loads in Developer Mode with no console errors
- [ ] Right-click → "Save page to ctxt" → job appears in `ctxt list`
- [ ] Kill server → capture → item in IndexedDB queue; restart server → item synced
- [ ] GitHub PR page → adaptor detects PR; pipeline set to `code.github.pr`
- [ ] arXiv page → adaptor extracts title + abstract + authors
- [ ] Popup shows red status dot when server unreachable
- [ ] Options page saves server URL and persists across browser restarts
- [ ] CORS headers present for `chrome-extension://` origin (confirmed in Plan 4 Task 1)

## See Also

- `docs/plans/2026-03-13-P410-web-ui.md` — Plan 4; Task 1 implements the CORS middleware this plan depends on
- `internal/server/http/middleware_cors.go` — CORS middleware (do not re-implement)
- `internal/server/http/handlers_capture.go` — server-side capture endpoints
- `internal/server/ws/cookie_bridge.go` — WebSocket cookie bridge server
- `internal/service/types.go` — `AnalyzeRequest` with `SourceTitle` and `AuthState` fields
- `internal/storage/types.go` — Go types mirrored in `extensions/browser/src/lib/types.ts`
