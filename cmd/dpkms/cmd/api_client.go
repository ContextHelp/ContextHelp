package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

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

func (c *APIClient) CreatePipeline(req service.CreatePipelineRequest) (string, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}

	resp, err := c.client.Post(c.baseURL+"/api/v1/pipelines", "application/json", bytes.NewReader(body))
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

func (c *APIClient) DeletePipeline(name string) error {
	req, _ := http.NewRequest("DELETE", c.baseURL+"/api/v1/pipelines/"+name, nil)
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

func (c *APIClient) ArchivePipeline(name string) error {
	req, _ := http.NewRequest("POST", c.baseURL+"/api/v1/pipelines/"+name+"/archive", nil)
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

func (c *APIClient) UnarchivePipeline(name string) error {
	req, _ := http.NewRequest("POST", c.baseURL+"/api/v1/pipelines/"+name+"/unarchive", nil)
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

func (c *APIClient) parseError(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)
	var errResp map[string]any
	json.Unmarshal(body, &errResp)

	if errMsg, ok := errResp["error"].(map[string]any); ok {
		return fmt.Errorf("%s: %s", errMsg["code"], errMsg["message"])
	}

	return fmt.Errorf("unexpected error: %s", string(body))
}
