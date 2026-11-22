package podhost

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ControlClient talks to ctxd's loopback HTTP control API through an active
// pod forward.
type ControlClient struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// NewControlClient builds a ControlClient targeting info.Control with its
// bearer token and a 1-minute default timeout.
func NewControlClient(info RunInfo, httpClient *http.Client) *ControlClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: time.Minute}
	}
	return &ControlClient{
		BaseURL: "http://" + info.Control,
		Token:   info.Token,
		HTTP:    httpClient,
	}
}

// Status calls GET /v1/status and decodes the JSON response map.
func (c *ControlClient) Status(ctx context.Context) (map[string]any, error) {
	return c.doJSON(ctx, http.MethodGet, "/v1/status")
}

// Stop calls POST /v1/stop to request graceful drain and exit.
func (c *ControlClient) Stop(ctx context.Context) error {
	_, err := c.doJSON(ctx, http.MethodPost, "/v1/stop")
	return err
}

// BufferFlush calls POST /v1/buffer/flush.
func (c *ControlClient) BufferFlush(ctx context.Context) (map[string]any, error) {
	return c.doJSON(ctx, http.MethodPost, "/v1/buffer/flush")
}

// BufferPurge calls POST /v1/buffer/purge.
func (c *ControlClient) BufferPurge(ctx context.Context) (map[string]any, error) {
	return c.doJSON(ctx, http.MethodPost, "/v1/buffer/purge")
}

// Tail streams GET /v1/tail into dst until ctx is cancelled or the server closes.
func (c *ControlClient) Tail(ctx context.Context, dst io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/v1/tail", nil)
	if err != nil {
		return err
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("control /v1/tail returned %d: %s", resp.StatusCode, string(body))
	}
	_, err = io.Copy(dst, resp.Body)
	return err
}

func (c *ControlClient) doJSON(ctx context.Context, method, path string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("control %s returned %d: %s", path, resp.StatusCode, string(body))
	}
	if len(body) == 0 {
		return map[string]any{}, nil
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode %s response: %w", path, err)
	}
	return out, nil
}
