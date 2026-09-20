package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// HeaderCtxtNote is the request header CLIs use to plumb their --note|-n
// flag through to the policy engine on the daemon. Mirrors
// internal/server/http.HeaderCtxtNote — duplicated here to avoid
// pulling the server package into the CLI.
const HeaderCtxtNote = "X-Ctxt-Note"

// ErrPolicyDenied is the sentinel returned when the daemon refuses a
// state-changing call because of a policy veto. CLI handlers map it
// to exit code 4 (CONFLICT).
var ErrPolicyDenied = errors.New("policy denied")

type APIClient struct {
	baseURL string
	client  *http.Client
}

func NewAPIClient(baseURL string) *APIClient {
	return &APIClient{
		baseURL: baseURL,
		client:  &http.Client{},
	}
}

// withNoteHeader sets the X-Ctxt-Note header on req when note is
// non-empty. The daemon's HTTP layer copies the value into ctx via
// policy.ContextAttrsKey before invoking the service.
func withNoteHeader(req *http.Request, note string) {
	if note != "" {
		req.Header.Set(HeaderCtxtNote, note)
	}
}

func (c *APIClient) CreatePipeline(req service.CreatePipelineRequest, note string) (string, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}

	httpReq, err := http.NewRequest(http.MethodPost, c.baseURL+"/api/v1/pipelines", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	withNoteHeader(httpReq, note)

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return "", c.parseError(resp)
	}

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	return result["id"], nil
}

func (c *APIClient) DeletePipeline(name, note string) error {
	req, _ := http.NewRequest(http.MethodDelete, c.baseURL+"/api/v1/pipelines/"+name, nil)
	withNoteHeader(req, note)
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		return c.parseError(resp)
	}

	return nil
}

func (c *APIClient) ListPipelines(filter storage.PipelineFilter) ([]*storage.Pipeline, int, error) {
	req, _ := http.NewRequest("GET", c.baseURL+"/api/v1/pipelines?name="+filter.Name, nil)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, 0, c.parseError(resp)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, 0, err
	}

	pipelinesJSON, _ := json.Marshal(result["pipelines"])
	var pipelines []*storage.Pipeline
	json.Unmarshal(pipelinesJSON, &pipelines)

	total := int(result["total"].(float64))

	return pipelines, total, nil
}

func (c *APIClient) GetPipeline(name string) (*storage.Pipeline, error) {
	resp, err := c.client.Get(c.baseURL + "/api/v1/pipelines/" + name)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseError(resp)
	}

	var pipeline storage.Pipeline
	if err := json.NewDecoder(resp.Body).Decode(&pipeline); err != nil {
		return nil, err
	}

	return &pipeline, nil
}

func (c *APIClient) ArchivePipeline(name, note string) error {
	req, _ := http.NewRequest(http.MethodPost, c.baseURL+"/api/v1/pipelines/"+name+"/archive", nil)
	withNoteHeader(req, note)
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.parseError(resp)
	}

	return nil
}

func (c *APIClient) UnarchivePipeline(name, note string) error {
	req, _ := http.NewRequest(http.MethodPost, c.baseURL+"/api/v1/pipelines/"+name+"/unarchive", nil)
	withNoteHeader(req, note)
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.parseError(resp)
	}

	return nil
}

func (c *APIClient) Enqueue(req service.AnalyzeRequest) (string, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}

	resp, err := c.client.Post(c.baseURL+"/api/v1/pipelines/enqueue", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		return "", c.parseError(resp)
	}

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	return result["job_id"], nil
}

func (c *APIClient) ListSteps(source string) ([]*storage.RegisteredStep, int, error) {
	url := c.baseURL + "/api/v1/steps"
	if source != "" {
		url += "?source=" + source
	}

	resp, err := c.client.Get(url)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, 0, c.parseError(resp)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, 0, err
	}

	stepsJSON, _ := json.Marshal(result["steps"])
	var steps []*storage.RegisteredStep
	json.Unmarshal(stepsJSON, &steps)

	total := int(result["total"].(float64))

	return steps, total, nil
}

func (c *APIClient) GetStep(name string) (*storage.RegisteredStep, error) {
	resp, err := c.client.Get(c.baseURL + "/api/v1/steps/" + name)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseError(resp)
	}

	var step storage.RegisteredStep
	if err := json.NewDecoder(resp.Body).Decode(&step); err != nil {
		return nil, err
	}

	return &step, nil
}

func (c *APIClient) InstallStep(name, fromRegistry string) error {
	reqBody := map[string]string{
		"name":          name,
		"from_registry": fromRegistry,
	}
	body, _ := json.Marshal(reqBody)

	resp, err := c.client.Post(c.baseURL+"/api/v1/steps/install", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.parseError(resp)
	}

	return nil
}

func (c *APIClient) UninstallStep(name string) error {
	req, _ := http.NewRequest("DELETE", c.baseURL+"/api/v1/steps/"+name, nil)
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		return c.parseError(resp)
	}

	return nil
}

func (c *APIClient) FetchRegistry(url string) error {
	reqBody := map[string]string{"url": url}
	body, _ := json.Marshal(reqBody)

	resp, err := c.client.Post(c.baseURL+"/api/v1/steps/registries/fetch", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.parseError(resp)
	}

	return nil
}

func (c *APIClient) UpdateRegistry(url string) error {
	resp, err := c.client.Post(c.baseURL+"/api/v1/steps/registries/"+url+"/update", "application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.parseError(resp)
	}

	return nil
}

func (c *APIClient) ConfigureAutoUpdate(url string, enabled bool) error {
	return fmt.Errorf("ConfigureAutoUpdate not yet implemented")
}

func (c *APIClient) ListRegistries() ([]*storage.RegistryCache, int, error) {
	resp, err := c.client.Get(c.baseURL + "/api/v1/steps/registries")
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, 0, c.parseError(resp)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, 0, err
	}

	registriesJSON, _ := json.Marshal(result["registries"])
	var registries []*storage.RegistryCache
	json.Unmarshal(registriesJSON, &registries)

	total := int(result["total"].(float64))

	return registries, total, nil
}

func (c *APIClient) GetJob(id string) (*storage.Job, error) {
	resp, err := c.client.Get(c.baseURL + "/api/v1/jobs/" + id)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseError(resp)
	}

	var job storage.Job
	if err := json.NewDecoder(resp.Body).Decode(&job); err != nil {
		return nil, err
	}

	return &job, nil
}

// HealthzEnvelope is the wire shape returned by GET /healthz on dpkms.
// Mirrors internal/server/http.HealthzEnvelope; duplicated here so the
// CLI doesn't import the server package transitively.
//
// New top-level fields (e.g. T-0580's `Upgrade`) are tolerated by Go's
// JSON decoder; consumers looking for them must either extend this
// struct or read the raw map via Healthz().
type HealthzEnvelope struct {
	Health        string         `json:"health"`
	Version       string         `json:"version"`
	UptimeSeconds int64          `json:"uptime_seconds"`
	Checks        HealthzChecks  `json:"checks"`
	Upgrade       map[string]any `json:"upgrade,omitempty"`
}

// HealthzChecks groups per-subsystem signals.
type HealthzChecks struct {
	Process  string            `json:"process"`
	RESTAPI  string            `json:"rest_api"`
	GRPCAPI  string            `json:"grpc_api"`
	DB       HealthzDBCheck    `json:"db"`
	Queue    HealthzQueueCheck `json:"queue"`
	Watchers []HealthzWatcher  `json:"watchers"`
}

// HealthzDBCheck reports DB reachability.
type HealthzDBCheck struct {
	Status    string `json:"status"`
	LastWrite string `json:"last_write,omitempty"`
}

// HealthzQueueCheck reports queue depths.
type HealthzQueueCheck struct {
	Pending int `json:"pending"`
	Running int `json:"running"`
	Failed  int `json:"failed"`
}

// HealthzWatcher reports the state of one registered watcher.
type HealthzWatcher struct {
	Name          string `json:"name"`
	Subscriptions int    `json:"subscriptions"`
	LastEvent     string `json:"last_event,omitempty"`
}

// Healthz fetches GET /healthz. The HTTP status code is returned so
// callers can distinguish 200 healthy/degraded from 503 failed without
// re-parsing the body.
func (c *APIClient) Healthz() (HealthzEnvelope, int, error) {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+"/healthz", nil)
	if err != nil {
		return HealthzEnvelope{}, 0, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return HealthzEnvelope{}, 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return HealthzEnvelope{}, resp.StatusCode, err
	}

	var env HealthzEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return HealthzEnvelope{}, resp.StatusCode, fmt.Errorf("decode healthz: %w", err)
	}
	return env, resp.StatusCode, nil
}

func (c *APIClient) parseError(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)
	var errResp map[string]any
	json.Unmarshal(body, &errResp)

	if errMsg, ok := errResp["error"].(map[string]any); ok {
		code, _ := errMsg["code"].(string)
		msg, _ := errMsg["message"].(string)
		formatted := fmt.Errorf("%s: %s", code, msg)
		// 409 + POLICY_DENIED is the daemon's way of surfacing a
		// policy.PolicyDeniedError. Wrap with ErrPolicyDenied so the
		// CLI runner can map to exit code 4 without re-parsing the
		// error string.
		if resp.StatusCode == http.StatusConflict && code == "POLICY_DENIED" {
			return fmt.Errorf("%w: %w", ErrPolicyDenied, formatted)
		}
		return formatted
	}

	return fmt.Errorf("unexpected error: %s", string(body))
}
