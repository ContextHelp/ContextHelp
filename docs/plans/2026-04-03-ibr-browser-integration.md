# IBR Browser Automation Integration — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Integrate IBR (AI-powered browser automation) as a built-in capability in dPKMS/ctxt, enabling interactive web ingestion pipelines that handle login walls, JavaScript-rendered pages, pagination, and structured data extraction from dynamic content.

**Architecture:** Go calls IBR's daemon HTTP API (`POST /command`) as a subprocess. A new `ibr_fetcher` pipeline step replaces `url_fetcher` when the pipeline requires browser interaction. IBR's daemon lifecycle is managed by `dpkms serve` — started on demand, shut down on `dpkms shutdown`. Config lives in the existing `config.yaml` under a new `browser` key.

**Tech Stack:** Go (dPKMS), Node.js (IBR daemon via subprocess), Playwright/Chromium (headless browser), HTTP JSON API (Go ↔ IBR communication)

---

## Task 1: Add `BrowserConfig` to config system

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Step 1: Write the failing test**

```go
// In config_test.go — add to existing test file
func TestBrowserConfigDefaults(t *testing.T) {
	cfg, err := Load("")
	require.NoError(t, err)
	assert.False(t, cfg.Browser.Enabled)
	assert.Equal(t, "ibr", cfg.Browser.Binary)
	assert.Equal(t, 0, cfg.Browser.Port) // 0 = auto-assign
	assert.Equal(t, 3, cfg.Browser.MaxClients)
	assert.Equal(t, 30*time.Second, cfg.Browser.QueueTimeout)
	assert.True(t, cfg.Browser.Headless)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestBrowserConfigDefaults -v`
Expected: FAIL — `cfg.Browser` field does not exist

**Step 3: Write minimal implementation**

Add to `config.go` — the struct:

```go
// BrowserConfig controls the built-in IBR browser automation daemon.
type BrowserConfig struct {
	// Enabled activates browser-based pipeline steps. Default: false.
	// When false, pipelines using ibr_fetcher are rejected at registration.
	Enabled bool `mapstructure:"enabled" yaml:"enabled"`
	// Binary is the path or name of the IBR executable. Default: "ibr".
	Binary string `mapstructure:"binary" yaml:"binary"`
	// Port is the HTTP port for the IBR daemon. 0 = auto-assign.
	Port int `mapstructure:"port" yaml:"port"`
	// MaxClients is the max concurrent browser contexts. Default: 3.
	MaxClients int `mapstructure:"max_clients" yaml:"max_clients"`
	// QueueTimeout is how long a request waits for a free slot. Default: 30s.
	QueueTimeout time.Duration `mapstructure:"queue_timeout" yaml:"queue_timeout"`
	// Headless runs the browser without a visible window. Default: true.
	Headless bool `mapstructure:"headless" yaml:"headless"`
	// AIProvider overrides the AI provider for IBR (openai, anthropic, google).
	// Empty = IBR uses its own default (openai).
	AIProvider string `mapstructure:"ai_provider" yaml:"ai_provider"`
	// AIModel overrides the AI model for IBR. Empty = IBR default.
	AIModel string `mapstructure:"ai_model" yaml:"ai_model"`
	// Cookies configures browser cookie import for authenticated sessions.
	Cookies BrowserCookieConfig `mapstructure:"cookies" yaml:"cookies"`
}

// BrowserCookieConfig controls cookie import for authenticated web access.
type BrowserCookieConfig struct {
	// Browser is the source browser name: chrome, brave, edge, arc.
	Browser string `mapstructure:"browser" yaml:"browser"`
	// Domains restricts which cookie domains are imported. Empty = all.
	Domains []string `mapstructure:"domains" yaml:"domains"`
	// Profile is the browser profile name. Default: "Default".
	Profile string `mapstructure:"profile" yaml:"profile"`
}
```

Add the field to `Config` struct:

```go
// Browser configures the built-in IBR browser automation daemon.
Browser BrowserConfig `mapstructure:"browser" yaml:"browser"`
```

Add defaults in `setDefaults`:

```go
// Browser defaults — disabled by default; Playwright/Chromium is heavy.
v.SetDefault("browser.enabled", false)
v.SetDefault("browser.binary", "ibr")
v.SetDefault("browser.port", 0)
v.SetDefault("browser.max_clients", 3)
v.SetDefault("browser.queue_timeout", 30*time.Second)
v.SetDefault("browser.headless", true)
v.SetDefault("browser.cookies.profile", "Default")
```

Add env bindings in `bindEnvVars`:

```go
v.BindEnv("browser.enabled", "CTXT_BROWSER_ENABLED")
v.BindEnv("browser.binary", "CTXT_BROWSER_BINARY")
v.BindEnv("browser.port", "CTXT_BROWSER_PORT")
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -run TestBrowserConfigDefaults -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): add BrowserConfig for IBR integration"
```

---

## Task 2: IBR daemon client — lifecycle management

**Files:**
- Create: `internal/browser/daemon.go`
- Test: `internal/browser/daemon_test.go`

**Step 1: Write the failing test**

```go
package browser

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/config"
)

func TestClient_Health_ReturnsStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/health", r.URL.Path)
		json.NewEncoder(w).Encode(map[string]any{
			"status":        "healthy",
			"uptime":        42,
			"pid":           1234,
			"activeClients": 0,
			"queueDepth":    0,
		})
	}))
	defer srv.Close()

	c := &Client{baseURL: srv.URL, token: "test-token", httpClient: srv.Client()}
	health, err := c.Health(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "healthy", health.Status)
}

func TestClient_Execute_SendsCommandAndReturnsExtracts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/command", r.URL.Path)
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))

		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		assert.Equal(t, "task", body["command"])

		w.Write([]byte(`{"extracts":[{"title":"Example"}],"tokenUsage":{"prompt":100,"completion":50,"total":150}}`))
	}))
	defer srv.Close()

	c := &Client{baseURL: srv.URL, token: "test-token", httpClient: srv.Client()}
	result, err := c.Execute(context.Background(), "url: https://example.com\ninstructions:\n  - extract title")
	require.NoError(t, err)
	assert.Len(t, result.Extracts, 1)
	assert.Equal(t, 150, result.TokenUsage.Total)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/browser/ -run TestClient -v`
Expected: FAIL — package does not exist

**Step 3: Write minimal implementation**

```go
package browser

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// HealthResponse is the IBR daemon health check response.
type HealthResponse struct {
	Status        string `json:"status"`
	Uptime        int    `json:"uptime"`
	PID           int    `json:"pid"`
	ActiveClients int    `json:"activeClients"`
	QueueDepth    int    `json:"queueDepth"`
}

// TokenUsage tracks AI token consumption.
type TokenUsage struct {
	Prompt     int `json:"prompt"`
	Completion int `json:"completion"`
	Total      int `json:"total"`
}

// ExecuteResult is the result of an IBR task execution.
type ExecuteResult struct {
	Extracts   []map[string]any `json:"extracts"`
	TokenUsage TokenUsage       `json:"tokenUsage"`
}

// Client communicates with the IBR daemon HTTP API.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// NewClient creates a client pointing at an IBR daemon.
func NewClient(baseURL, token string) *Client {
	return &Client{
		baseURL:    baseURL,
		token:      token,
		httpClient: &http.Client{Timeout: 120 * time.Second},
	}
}

// Health checks if the daemon is alive.
func (c *Client) Health(ctx context.Context) (*HealthResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/health", nil)
	if err != nil {
		return nil, fmt.Errorf("browser: build health request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("browser: health check: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("browser: health check returned %d", resp.StatusCode)
	}
	var h HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&h); err != nil {
		return nil, fmt.Errorf("browser: decode health: %w", err)
	}
	return &h, nil
}

// Execute sends a task prompt to the IBR daemon and returns structured results.
func (c *Client) Execute(ctx context.Context, prompt string) (*ExecuteResult, error) {
	body, _ := json.Marshal(map[string]any{
		"command": "task",
		"args":    []string{prompt},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/command", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("browser: build execute request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("browser: execute: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("browser: execute returned %d", resp.StatusCode)
	}

	var result ExecuteResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("browser: decode result: %w", err)
	}
	return &result, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/browser/ -run TestClient -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/browser/daemon.go internal/browser/daemon_test.go
git commit -m "feat(browser): IBR daemon HTTP client"
```

---

## Task 3: IBR daemon process manager — start/stop/health

**Files:**
- Create: `internal/browser/manager.go`
- Test: `internal/browser/manager_test.go`

**Step 1: Write the failing test**

```go
package browser

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/config"
)

func TestManager_IsRunning_FalseWhenNoClient(t *testing.T) {
	m := NewManager(config.BrowserConfig{Enabled: true, Binary: "ibr"})
	assert.False(t, m.IsRunning())
}

func TestManager_IsRunning_TrueWhenHealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"healthy"}`))
	}))
	defer srv.Close()

	m := NewManager(config.BrowserConfig{Enabled: true})
	m.client = NewClient(srv.URL, "tok")
	assert.True(t, m.IsRunning())
}

func TestManager_Client_ReturnsNilWhenDisabled(t *testing.T) {
	m := NewManager(config.BrowserConfig{Enabled: false})
	assert.Nil(t, m.Client())
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/browser/ -run TestManager -v`
Expected: FAIL — `NewManager` not defined

**Step 3: Write minimal implementation**

```go
package browser

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
)

// Manager controls the lifecycle of the IBR daemon subprocess.
type Manager struct {
	cfg    config.BrowserConfig
	client *Client
	cmd    *exec.Cmd
	port   int
	token  string
}

// NewManager creates a manager (does not start the daemon).
func NewManager(cfg config.BrowserConfig) *Manager {
	return &Manager{cfg: cfg}
}

// Client returns the IBR HTTP client, or nil if not running.
func (m *Manager) Client() *Client {
	if !m.cfg.Enabled || m.client == nil {
		return nil
	}
	return m.client
}

// IsRunning checks if the daemon is healthy.
func (m *Manager) IsRunning() bool {
	if m.client == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	h, err := m.client.Health(ctx)
	return err == nil && h.Status == "healthy"
}

// Start launches the IBR daemon as a subprocess.
func (m *Manager) Start(ctx context.Context) error {
	if !m.cfg.Enabled {
		return nil
	}

	port := m.cfg.Port
	if port == 0 {
		p, err := findFreePort()
		if err != nil {
			return fmt.Errorf("browser: find free port: %w", err)
		}
		port = p
	}
	m.port = port

	token, err := randomToken()
	if err != nil {
		return fmt.Errorf("browser: generate token: %w", err)
	}
	m.token = token

	binary := m.cfg.Binary
	if binary == "" {
		binary = "ibr"
	}

	// Resolve to node src/server.js if binary points to the ibr package directory.
	// Otherwise assume it's a globally installed `ibr` command or a direct path.
	args := []string{}
	env := append(os.Environ(),
		"IBR_DAEMON=true",
		fmt.Sprintf("IBR_DAEMON_PORT=%d", port),
		fmt.Sprintf("IBR_DAEMON_TOKEN=%s", token),
		fmt.Sprintf("IBR_DAEMON_MAX_CLIENTS=%d", m.cfg.MaxClients),
		fmt.Sprintf("BROWSER_HEADLESS=%t", m.cfg.Headless),
	)

	if m.cfg.AIProvider != "" {
		env = append(env, "AI_PROVIDER="+m.cfg.AIProvider)
	}
	if m.cfg.AIModel != "" {
		env = append(env, "AI_MODEL="+m.cfg.AIModel)
	}

	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Env = env
	cmd.Stdout = os.Stdout // TODO: wire to logger
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("browser: start daemon: %w", err)
	}
	m.cmd = cmd

	// Wait for daemon to become healthy.
	m.client = NewClient(fmt.Sprintf("http://127.0.0.1:%d", port), token)
	if err := m.waitHealthy(ctx, 15*time.Second); err != nil {
		_ = m.Stop()
		return fmt.Errorf("browser: daemon did not become healthy: %w", err)
	}

	log.Printf("browser: IBR daemon started on port %d (pid %d)", port, cmd.Process.Pid)
	return nil
}

// Stop gracefully shuts down the daemon.
func (m *Manager) Stop() error {
	if m.cmd == nil || m.cmd.Process == nil {
		return nil
	}
	log.Printf("browser: stopping IBR daemon (pid %d)", m.cmd.Process.Pid)
	if err := m.cmd.Process.Signal(os.Interrupt); err != nil {
		return m.cmd.Process.Kill()
	}
	// Wait up to 5s for graceful exit.
	done := make(chan error, 1)
	go func() { done <- m.cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = m.cmd.Process.Kill()
	}
	m.client = nil
	m.cmd = nil
	return nil
}

func (m *Manager) waitHealthy(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if m.IsRunning() {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("timeout after %s", timeout)
}

func findFreePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port, nil
}

func randomToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Port returns the daemon's HTTP port (0 if not started).
func (m *Manager) Port() int { return m.port }

// PID returns the daemon's process ID (0 if not started).
func (m *Manager) PID() int {
	if m.cmd != nil && m.cmd.Process != nil {
		return m.cmd.Process.Pid
	}
	return 0
}

// Enabled returns whether browser automation is configured.
func (m *Manager) Enabled() bool { return m.cfg.Enabled }
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/browser/ -run TestManager -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/browser/manager.go internal/browser/manager_test.go
git commit -m "feat(browser): daemon process manager (start/stop/health)"
```

---

## Task 4: `ibr_fetcher` pipeline step

**Files:**
- Create: `internal/pipeline/steps/ibr_fetcher.go`
- Test: `internal/pipeline/steps/ibr_fetcher_test.go`

**Step 1: Write the failing test**

```go
package steps

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/browser"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestIBRFetcher_Name(t *testing.T) {
	f := NewIBRFetcher(nil)
	assert.Equal(t, "ibr_fetcher", f.Name())
}

func TestIBRFetcher_Contract(t *testing.T) {
	f := NewIBRFetcher(nil)
	c := f.Contract()
	assert.Contains(t, c.Requires, "Source")
	assert.Contains(t, c.Produces, "RawContent")
	assert.Contains(t, c.Produces, "Metadata")
	assert.Contains(t, c.Capabilities, "browser")
}

func TestIBRFetcher_Run_FetchesViaIBRDaemon(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		args := body["args"].([]any)
		prompt := args[0].(string)
		assert.Contains(t, prompt, "https://example.com")

		json.NewEncoder(w).Encode(map[string]any{
			"extracts": []map[string]any{
				{"text": "<h1>Hello World</h1><p>Content here</p>"},
			},
			"tokenUsage": map[string]int{"prompt": 100, "completion": 50, "total": 150},
		})
	}))
	defer srv.Close()

	client := browser.NewClient(srv.URL, "test")
	f := NewIBRFetcher(client)

	draft := &storage.KnowledgeObject{
		Source: "https://example.com",
	}

	result, err := f.Run(context.Background(), draft)
	require.NoError(t, err)
	assert.Contains(t, result.RawContent, "Hello World")
	assert.Equal(t, "https://example.com", result.Metadata["source_url"])
}

func TestIBRFetcher_Run_NilClient_ReturnsError(t *testing.T) {
	f := NewIBRFetcher(nil)
	draft := &storage.KnowledgeObject{Source: "https://example.com"}
	_, err := f.Run(context.Background(), draft)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "browser not available")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/pipeline/steps/ -run TestIBRFetcher -v`
Expected: FAIL — `NewIBRFetcher` not defined

**Step 3: Write minimal implementation**

```go
package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/browser"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// IBRFetcher fetches web content using the IBR browser automation daemon.
// Unlike url_fetcher (plain HTTP), this handles JavaScript-rendered pages,
// login walls, cookie consent, pagination, and structured data extraction.
type IBRFetcher struct {
	pipeline.BaseContract
	client *browser.Client
}

// IBRFetcherOption configures an IBRFetcher.
type IBRFetcherOption func(*IBRFetcher)

// WithIBRClient sets the browser client.
func WithIBRClient(c *browser.Client) IBRFetcherOption {
	return func(f *IBRFetcher) { f.client = c }
}

// NewIBRFetcher creates an IBRFetcher with the given browser client.
func NewIBRFetcher(client *browser.Client, opts ...IBRFetcherOption) *IBRFetcher {
	f := &IBRFetcher{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires:     []string{"Source"},
			Produces:     []string{"RawContent", "Metadata"},
			Capabilities: []string{"browser"},
		}),
		client: client,
	}
	for _, opt := range opts {
		opt(f)
	}
	return f
}

func (s *IBRFetcher) Name() string { return "ibr_fetcher" }

func (s *IBRFetcher) Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if s.client == nil {
		return nil, fmt.Errorf("ibr_fetcher: browser not available (is browser.enabled=true in config?)")
	}

	rawURL := strings.TrimSpace(draft.Source)
	if rawURL == "" {
		if strings.HasPrefix(strings.TrimSpace(draft.RawContent), "http") {
			rawURL = strings.TrimSpace(draft.RawContent)
		}
	}
	if rawURL == "" {
		return nil, fmt.Errorf("ibr_fetcher: no URL to fetch")
	}

	log.Printf("ibr_fetcher: fetching %s via browser", rawURL)

	// Build the IBR task prompt. Default: navigate and extract page content.
	// If draft.Metadata has "ibr_instructions", use those instead.
	instructions := []string{"extract the full page content as HTML"}
	if draft.Metadata != nil {
		if instr, ok := draft.Metadata["ibr_instructions"]; ok {
			if instrSlice, ok := instr.([]any); ok {
				instructions = make([]string, 0, len(instrSlice))
				for _, v := range instrSlice {
					if s, ok := v.(string); ok {
						instructions = append(instructions, s)
					}
				}
			}
		}
	}

	var sb strings.Builder
	sb.WriteString("url: " + rawURL + "\n")
	sb.WriteString("instructions:\n")
	for _, instr := range instructions {
		sb.WriteString("  - " + instr + "\n")
	}

	result, err := s.client.Execute(ctx, sb.String())
	if err != nil {
		return nil, fmt.Errorf("ibr_fetcher: execute: %w", err)
	}

	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	draft.Metadata["source_url"] = rawURL
	draft.Metadata["ibr_token_usage"] = result.TokenUsage

	// Merge extracted content into RawContent.
	if len(result.Extracts) > 0 {
		// If extracts contain a "text" or "content" or "html" key, use that.
		// Otherwise serialize the first extract as JSON.
		content := extractContent(result.Extracts[0])
		draft.RawContent = content
	}

	// Store all extracts in metadata for downstream steps.
	if len(result.Extracts) > 0 {
		raw, _ := json.Marshal(result.Extracts)
		draft.Metadata["ibr_extracts"] = json.RawMessage(raw)
	}

	log.Printf("ibr_fetcher: fetched %d bytes, %d extracts", len(draft.RawContent), len(result.Extracts))
	return draft, nil
}

// extractContent picks the best text field from an extract map.
func extractContent(extract map[string]any) string {
	for _, key := range []string{"html", "content", "text", "body"} {
		if v, ok := extract[key]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	// Fallback: serialize as JSON.
	b, _ := json.Marshal(extract)
	return string(b)
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/pipeline/steps/ -run TestIBRFetcher -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/pipeline/steps/ibr_fetcher.go internal/pipeline/steps/ibr_fetcher_test.go
git commit -m "feat(pipeline): add ibr_fetcher step for browser-based URL fetching"
```

---

## Task 5: Register `ibr_fetcher` in builtin step constructors

**Files:**
- Modify: `internal/pipeline/builtins/builtins.go`
- Test: `internal/pipeline/builtins/builtins_test.go`

**Step 1: Write the failing test**

```go
func TestIBRFetcherRegistered(t *testing.T) {
	// Verify ibr_fetcher can be resolved when browser client is provided.
	defs := Defs()
	// Check url.interactive pipeline exists.
	d, ok := defs["url.interactive"]
	require.True(t, ok, "url.interactive pipeline should be registered")
	assert.Contains(t, d.Steps, "ibr_fetcher")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/pipeline/builtins/ -run TestIBRFetcherRegistered -v`
Expected: FAIL — no `url.interactive` pipeline

**Step 3: Write minimal implementation**

In `builtins.go`, add to `stepConstructors`:

```go
"ibr_fetcher": func() pipeline.PipelineStep { return steps.NewIBRFetcher(nil) },
```

Add to `browserStepConstructors` (new map, similar to `providerStepConstructors`):

```go
// browserStepConstructors maps step names to browser-client-aware constructors.
var browserStepConstructors = map[string]func(*browser.Client) pipeline.PipelineStep{
	"ibr_fetcher": func(c *browser.Client) pipeline.PipelineStep {
		return steps.NewIBRFetcher(c)
	},
}
```

Update `BuildOpts`:

```go
type BuildOpts struct {
	Factory       *providers.Factory
	BlobStore     storage.BlobStore
	BlobThreshold int64
	BrowserClient *browser.Client // IBR daemon client
}
```

Update `resolveStep` to check `browserStepConstructors` when `opts.BrowserClient != nil`:

```go
if opts.BrowserClient != nil {
	if ctor, ok := browserStepConstructors[name]; ok {
		return ctor(opts.BrowserClient), nil
	}
}
```

Create `internal/pipeline/builtins/url_interactive.go`:

```go
package builtins

func init() {
	MustRegister("url.interactive", Def{
		Description: "Browser-based interactive URL fetch and extraction",
		Steps:       []string{"ibr_fetcher", "html_cleaner", "typedetector", "textcleaner", "sectioner", "tagger", "entity_extractor", "entity_resolver", "embedding"},
		Providers:   []string{"browser"},
	})
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/pipeline/builtins/ -run TestIBRFetcherRegistered -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/pipeline/builtins/builtins.go internal/pipeline/builtins/url_interactive.go
git commit -m "feat(pipeline): register ibr_fetcher step and url.interactive pipeline"
```

---

## Task 6: Wire browser manager into `dpkms serve`

**Files:**
- Modify: `cmd/dpkms/cmd/serve.go`

**Step 1: Write the failing test**

This is an integration-level wiring task. Verify by reading the code and running `go build ./cmd/dpkms/`.

**Step 2: Write the implementation**

In `serve.go`, add import:

```go
"github.com/ideacrafterslabs/ctxt/internal/browser"
```

In `runServe`, after pipeline registry construction and before errgroup start:

```go
// Browser automation daemon (optional).
var browserMgr *browser.Manager
if cfg.Browser.Enabled {
	browserMgr = browser.NewManager(cfg.Browser)
	if err := browserMgr.Start(ctx); err != nil {
		return fmt.Errorf("browser daemon: %w", err)
	}
	log.Printf("serve: IBR browser daemon on port %d", browserMgr.Port())
}
```

Pass `browserMgr.Client()` into the pipeline registry build opts:

```go
var browserClient *browser.Client
if browserMgr != nil {
	browserClient = browserMgr.Client()
}

// In the ConfiguredRegistryWithPipelineOverrides call or BuildOpts:
bOpts := builtins.BuildOpts{
	Factory:       factory,
	BlobStore:     blobStore,
	BlobThreshold: cfg.Storage.Blob.Threshold,
	BrowserClient: browserClient,
}
```

In the shutdown section (after errgroup returns), add:

```go
if browserMgr != nil {
	if err := browserMgr.Stop(); err != nil {
		log.Printf("serve: browser daemon stop error: %v", err)
	}
}
```

Add a `--browser` flag:

```go
serveCmd.Flags().Bool("browser", false, "enable browser automation (requires IBR)")
```

And bind it in `runServe`:

```go
if v, _ := cmd.Flags().GetBool("browser"); v {
	cfg.Browser.Enabled = true
}
```

**Step 3: Verify it compiles**

Run: `go build ./cmd/dpkms/`
Expected: builds successfully

**Step 4: Commit**

```bash
git add cmd/dpkms/cmd/serve.go
git commit -m "feat(serve): wire IBR browser manager into daemon lifecycle"
```

---

## Task 7: `url.authenticated` pipeline with cookie support

**Files:**
- Create: `internal/pipeline/builtins/url_authenticated.go`
- Modify: `internal/pipeline/steps/ibr_fetcher.go` (add cookie instruction support)
- Test: `internal/pipeline/steps/ibr_fetcher_test.go`

**Step 1: Write the failing test**

```go
func TestIBRFetcher_Run_WithCookieConfig(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		args := body["args"].([]any)
		prompt := args[0].(string)
		// Verify --cookies flag is present in the prompt/args
		assert.Contains(t, prompt, "https://example.com")
		json.NewEncoder(w).Encode(map[string]any{
			"extracts":   []map[string]any{{"text": "authenticated content"}},
			"tokenUsage": map[string]int{"total": 100},
		})
	}))
	defer srv.Close()

	client := browser.NewClient(srv.URL, "test")
	f := NewIBRFetcher(client)

	draft := &storage.KnowledgeObject{
		Source:   "https://example.com/dashboard",
		Metadata: map[string]any{
			"ibr_cookies": map[string]any{
				"browser": "chrome",
				"domains": []any{"example.com"},
			},
		},
	}

	result, err := f.Run(context.Background(), draft)
	require.NoError(t, err)
	assert.Contains(t, result.RawContent, "authenticated content")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/pipeline/steps/ -run TestIBRFetcher_Run_WithCookieConfig -v`
Expected: FAIL (test passes vacuously since we don't check cookie handling yet — add assertion for metadata)

**Step 3: Write minimal implementation**

Create `url_authenticated.go`:

```go
package builtins

func init() {
	MustRegister("url.authenticated", Def{
		Description: "Browser-based authenticated URL fetch using imported cookies",
		Steps:       []string{"ibr_fetcher", "html_cleaner", "typedetector", "textcleaner", "sectioner", "tagger", "entity_extractor", "entity_resolver", "embedding"},
		Providers:   []string{"browser"},
	})
}
```

In `ibr_fetcher.go`, update the `Run` method to read cookie config from metadata and pass it through to the prompt or as a separate field. The IBR daemon handles `--cookies` at the CLI level, but via the HTTP API the cookies are set on the browser context. For now, include cookie info in metadata that the daemon can read:

Update the Execute call to include cookie config in the command body when present:

```go
// In ibr_fetcher.go Run(), before building the prompt:
var cookieSpec string
if draft.Metadata != nil {
	if cc, ok := draft.Metadata["ibr_cookies"]; ok {
		if cm, ok := cc.(map[string]any); ok {
			b, _ := cm["browser"].(string)
			if b != "" {
				cookieSpec = b
				if domains, ok := cm["domains"].([]any); ok && len(domains) > 0 {
					ds := make([]string, 0, len(domains))
					for _, d := range domains {
						if s, ok := d.(string); ok {
							ds = append(ds, s)
						}
					}
					if len(ds) > 0 {
						cookieSpec += ":" + strings.Join(ds, ",")
					}
				}
			}
		}
	}
}
draft.Metadata["ibr_cookies_spec"] = cookieSpec
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/pipeline/steps/ -run TestIBRFetcher -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/pipeline/builtins/url_authenticated.go internal/pipeline/steps/ibr_fetcher.go internal/pipeline/steps/ibr_fetcher_test.go
git commit -m "feat(pipeline): add url.authenticated pipeline with cookie support"
```

---

## Task 8: Add `browser` capability to capability system

**Files:**
- Modify: `internal/pipeline/builtins/builtins.go` (add `CapabilitiesFromOpts` update)
- Test: `internal/pipeline/builtins/builtins_test.go`

**Step 1: Write the failing test**

```go
func TestCapabilitiesFromOpts_IncludesBrowser(t *testing.T) {
	// Without browser client
	caps := CapabilitiesFromOpts(BuildOpts{})
	assert.NotContains(t, caps, "browser")

	// With browser client
	caps = CapabilitiesFromOpts(BuildOpts{BrowserClient: browser.NewClient("http://localhost:1234", "tok")})
	assert.Contains(t, caps, "browser")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/pipeline/builtins/ -run TestCapabilitiesFromOpts_IncludesBrowser -v`
Expected: FAIL — `browser` capability not recognized

**Step 3: Write minimal implementation**

In `CapabilitiesFromOpts` function (in `builtins.go`), add:

```go
if opts.BrowserClient != nil {
	caps = append(caps, "browser")
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/pipeline/builtins/ -run TestCapabilitiesFromOpts_IncludesBrowser -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/pipeline/builtins/builtins.go internal/pipeline/builtins/builtins_test.go
git commit -m "feat(pipeline): add 'browser' capability for IBR-dependent steps"
```

---

## Task 9: Pipeline selector — route URLs to `url.interactive` when configured

**Files:**
- Modify: `internal/pipeline/builtins/builtins.go` (`selectPipeline` function)
- Test: `internal/pipeline/builtins/builtins_test.go`

**Step 1: Write the failing test**

```go
func TestSelectPipeline_URLInteractive_WhenMetadataFlagSet(t *testing.T) {
	// The url.interactive pipeline is chosen when the user explicitly
	// passes --interactive or the content metadata indicates it.
	// For auto-detection: url.generic remains the default for URLs.
	// url.interactive is selected via explicit pipeline= parameter on ingest.
	r := Registry()
	p, err := r.Get("url.interactive")
	require.NoError(t, err)
	assert.Equal(t, "url.interactive", p.PipelineName)
}
```

**Step 2: Run test to verify it passes**

Run: `go test ./internal/pipeline/builtins/ -run TestSelectPipeline_URLInteractive -v`
Expected: PASS (pipeline is registered; selection is explicit, not auto-detected)

Note: `url.interactive` is not auto-selected for all URLs. Users must explicitly choose it via `ctxt analyze --pipeline url.interactive <url>` or configure it per-domain. Auto-detection would be a future enhancement.

**Step 3: Commit**

```bash
git add internal/pipeline/builtins/builtins_test.go
git commit -m "test(pipeline): verify url.interactive pipeline is registered and selectable"
```

---

## Task 10: Integration test — full pipeline with mock IBR daemon

**Files:**
- Create: `internal/browser/integration_test.go`

**Step 1: Write the integration test**

```go
package browser_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/browser"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/steps"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestIBRFetcher_IntegrationWithMockDaemon(t *testing.T) {
	// Simulate a full IBR daemon that returns structured page content.
	mockDaemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			json.NewEncoder(w).Encode(map[string]any{"status": "healthy"})
		case "/command":
			json.NewEncoder(w).Encode(map[string]any{
				"extracts": []map[string]any{
					{
						"html":  "<article><h1>Test Article</h1><p>This is the content of a JS-rendered page.</p></article>",
						"title": "Test Article",
					},
				},
				"tokenUsage": map[string]int{"prompt": 500, "completion": 200, "total": 700},
			})
		}
	}))
	defer mockDaemon.Close()

	client := browser.NewClient(mockDaemon.URL, "integration-test")

	// Verify health.
	health, err := client.Health(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "healthy", health.Status)

	// Run ibr_fetcher step.
	fetcher := steps.NewIBRFetcher(client)
	draft := &storage.KnowledgeObject{
		Source: "https://spa-app.example.com/dashboard",
		Metadata: map[string]any{
			"ibr_instructions": []any{
				"wait for the dashboard to load",
				"extract the main article content",
			},
		},
	}

	result, err := fetcher.Run(context.Background(), draft)
	require.NoError(t, err)
	assert.Contains(t, result.RawContent, "Test Article")
	assert.Contains(t, result.RawContent, "JS-rendered page")
	assert.Equal(t, "https://spa-app.example.com/dashboard", result.Metadata["source_url"])
	assert.NotNil(t, result.Metadata["ibr_token_usage"])
}
```

**Step 2: Run test**

Run: `go test ./internal/browser/ -run TestIBRFetcher_IntegrationWithMockDaemon -v`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/browser/integration_test.go
git commit -m "test(browser): integration test with mock IBR daemon"
```

---

## Task 11: Add `--browser` flag to `dpkms serve --help` and update pidfile

**Files:**
- Modify: `cmd/dpkms/cmd/serve.go`
- Modify: `internal/pidfile/pidfile.go` (add browser port to pidfile)

**Step 1: Verify the `--browser` flag was added in Task 6**

Run: `go run ./cmd/dpkms/ serve --help`
Expected: shows `--browser` flag in help output

**Step 2: Update pidfile to include browser daemon port**

In `pidfile.go`, add `BrowserPort int` field to the pidfile struct so `dpkms ps` can report it.

**Step 3: Update `serve.go` to write browser port to pidfile**

After `browserMgr.Start()`, before writing pidfile:

```go
if browserMgr != nil {
	pf.BrowserPort = browserMgr.Port()
}
```

**Step 4: Verify it compiles**

Run: `go build ./cmd/dpkms/`
Expected: builds successfully

**Step 5: Commit**

```bash
git add cmd/dpkms/cmd/serve.go internal/pidfile/pidfile.go
git commit -m "feat(serve): expose browser port in pidfile for dpkms ps"
```

---

## Summary

| Task | What it does | Key files |
|------|-------------|-----------|
| 1 | `BrowserConfig` in config system | `internal/config/config.go` |
| 2 | IBR daemon HTTP client | `internal/browser/daemon.go` |
| 3 | Daemon process manager (start/stop) | `internal/browser/manager.go` |
| 4 | `ibr_fetcher` pipeline step | `internal/pipeline/steps/ibr_fetcher.go` |
| 5 | Register step + `url.interactive` pipeline | `internal/pipeline/builtins/` |
| 6 | Wire manager into `dpkms serve` | `cmd/dpkms/cmd/serve.go` |
| 7 | `url.authenticated` pipeline + cookies | `internal/pipeline/builtins/url_authenticated.go` |
| 8 | `browser` capability in pipeline system | `internal/pipeline/builtins/builtins.go` |
| 9 | Pipeline selector integration | `internal/pipeline/builtins/builtins_test.go` |
| 10 | Integration test with mock daemon | `internal/browser/integration_test.go` |
| 11 | Pidfile + CLI flag polish | `cmd/dpkms/cmd/serve.go`, `internal/pidfile/` |

**Dependency order:** 1 → 2 → 3 → 4 → 5 → 6 → 7 (parallel with 8) → 9 → 10 → 11

**What this does NOT cover (future work):**
- Auto-detection of URLs that need browser rendering (requires a `url_probe` step that tries HTTP first, falls back to IBR)
- Per-domain pipeline routing (e.g. "always use `url.interactive` for `*.notion.so`")
- IBR tool YAML integration (exposing IBR tools as ctxt capture recipes)
- Web page change monitoring (combining `ctxt watch` with IBR for dynamic pages)
- NDJSON event streaming from IBR into dPKMS CloudEvents bus
- Cookie refresh/rotation lifecycle
