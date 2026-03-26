package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	gohttp "net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// codeFixture represents a small source file from a codebase fixture.
type codeFixture struct {
	name    string
	content string
	srcType string
}

// codebaseFixtures is a minimal multi-file codebase for analysis tests.
var codebaseFixtures = []codeFixture{
	{
		name:    "main.go",
		content: `package main

import "fmt"

// App is the main application entry.
type App struct{ Name string }

func main() {
	a := App{Name: "ctxt"}
	fmt.Println(a.Name)
}
`,
		srcType: "text",
	},
	{
		name: "README.md",
		content: `# ctxt

ctxt is a local-first knowledge management system.

## Features
- Capture text, URLs, images
- Enrich via pipeline steps
- Search with RSQL + NLQ
`,
		srcType: "text",
	},
	{
		name: "config.yaml",
		content: `storage:
  backend: sqlite
  path: ~/.local/share/ctxt

pipelines:
  default: text.short
`,
		srcType: "text",
	},
}

func TestUS0113_AnalyzeAPI_IngestsCodeFile(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	fix := codebaseFixtures[0]
	body, err := json.Marshal(map[string]string{
		"content": fix.content,
		"type":    fix.srcType,
		"source":  "fixture:" + fix.name,
	})
	require.NoError(t, err)

	resp, err := gohttp.Post(env.URL+"/api/v1/analyze", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, gohttp.StatusAccepted, resp.StatusCode, "analyze must return 202 Accepted")

	var result map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	jobID := result["job_id"]
	require.NotEmpty(t, jobID, "job_id must be returned")

	// Wait for job to complete → object must exist.
	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID, "completed job must have a result_id")

	// Verify object is retrievable and carries correct source.
	objResp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
	require.NoError(t, err)
	defer objResp.Body.Close()
	require.Equal(t, gohttp.StatusOK, objResp.StatusCode)

	var obj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(objResp.Body).Decode(&obj))
	assert.Equal(t, job.ResultID, obj.ID)
	assert.Equal(t, "fixture:"+fix.name, obj.Source)
}

func TestUS0113_AnalyzeAPI_IngestsMultipleFiles(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	resultIDs := make([]string, 0, len(codebaseFixtures))

	for _, fix := range codebaseFixtures {
		body, err := json.Marshal(map[string]string{
			"content": fix.content,
			"type":    fix.srcType,
			"source":  "fixture:" + fix.name,
		})
		require.NoError(t, err)

		resp, err := gohttp.Post(env.URL+"/api/v1/analyze", "application/json", bytes.NewReader(body))
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, gohttp.StatusAccepted, resp.StatusCode)

		var result map[string]string
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
		jobID := result["job_id"]
		require.NotEmpty(t, jobID)

		job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
		require.NotEmpty(t, job.ResultID)
		resultIDs = append(resultIDs, job.ResultID)
	}

	// All files produce distinct objects.
	unique := make(map[string]struct{}, len(resultIDs))
	for _, id := range resultIDs {
		unique[id] = struct{}{}
	}
	assert.Len(t, unique, len(codebaseFixtures), "each file should produce a distinct object")

	// All objects appear in the object list.
	listResp, err := gohttp.Get(env.URL + "/api/v1/objects?limit=100")
	require.NoError(t, err)
	defer listResp.Body.Close()
	require.Equal(t, gohttp.StatusOK, listResp.StatusCode)

	var listBody struct {
		Data  []*storage.KnowledgeObject `json:"data"`
		Total int                        `json:"total"`
	}
	require.NoError(t, json.NewDecoder(listResp.Body).Decode(&listBody))

	listedIDs := make(map[string]struct{}, len(listBody.Data))
	for _, obj := range listBody.Data {
		listedIDs[obj.ID] = struct{}{}
	}
	for _, id := range resultIDs {
		assert.Contains(t, listedIDs, id, "object %s must appear in /api/v1/objects", id)
	}
}

func TestUS0113_AnalyzeAPI_ObjectMetadata(t *testing.T) {
	// Verify that ingested file object carries the correct source and type metadata.
	env := startTestEnv(t)
	defer env.stop(t)

	fix := codebaseFixtures[1] // README.md
	body, err := json.Marshal(map[string]string{
		"content": fix.content,
		"type":    fix.srcType,
		"source":  "fixture:" + fix.name,
	})
	require.NoError(t, err)

	resp, err := gohttp.Post(env.URL+"/api/v1/analyze", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	var result map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	job := waitForJob(t, env.URL, result["job_id"], storage.JobCompleted)

	objResp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
	require.NoError(t, err)
	defer objResp.Body.Close()

	var obj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(objResp.Body).Decode(&obj))
	assert.Equal(t, "fixture:"+fix.name, obj.Source, "object source must match ingested file source")
	assert.NotEmpty(t, obj.Pipeline, "pipeline field must be set after processing")
}

func TestUS0113_AnalyzeAPI_MissingContent(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	body, _ := json.Marshal(map[string]string{
		"type":   "text",
		"source": "fixture:empty.go",
		// content intentionally omitted
	})
	resp, err := gohttp.Post(env.URL+"/api/v1/analyze", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusBadRequest, resp.StatusCode, "missing content must return 400")
}

func TestUS0113_AnalyzeAPI_SearchAfterIngest(t *testing.T) {
	// Analyze a code file → search for it by source type.
	env := startTestEnv(t)
	defer env.stop(t)

	fix := codebaseFixtures[2] // config.yaml
	body, _ := json.Marshal(map[string]string{
		"content": fix.content,
		"type":    fix.srcType,
		"source":  "fixture:" + fix.name,
	})
	resp, err := gohttp.Post(env.URL+"/api/v1/analyze", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	job := waitForJob(t, env.URL, result["job_id"], storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	// Search for the object by type==text.
	searchResp, err := gohttp.Get(env.URL + "/api/v1/search?q=type==text")
	require.NoError(t, err)
	defer searchResp.Body.Close()
	require.Equal(t, gohttp.StatusOK, searchResp.StatusCode)

	var searchBody struct {
		Data  []storage.KnowledgeObject `json:"data"`
		Total int                       `json:"total"`
	}
	require.NoError(t, json.NewDecoder(searchResp.Body).Decode(&searchBody))
	assert.GreaterOrEqual(t, searchBody.Total, 1, "at least the ingested object must appear in search results")

	found := false
	for _, obj := range searchBody.Data {
		if obj.ID == job.ResultID {
			found = true
			break
		}
	}
	assert.True(t, found, "ingested code file object must be found via search")
}
