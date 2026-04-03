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
