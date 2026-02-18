package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type StepDiscovery struct {
	store     storage.StorageDriver
	client    *http.Client
	stepsPath string
}

func NewStepDiscovery(store storage.StorageDriver, stepsPath string) *StepDiscovery {
	return &StepDiscovery{
		store:     store,
		client:    &http.Client{Timeout: 30 * time.Second},
		stepsPath: stepsPath,
	}
}

func (sd *StepDiscovery) DiscoverAll(ctx context.Context) error {
	if err := sd.LoadBuiltinSteps(ctx); err != nil {
		return fmt.Errorf("load builtin steps: %w", err)
	}

	if err := sd.ScanLocalSteps(ctx); err != nil {
		return fmt.Errorf("scan local steps: %w", err)
	}

	if err := sd.CheckRegistryUpdates(ctx); err != nil {
		return fmt.Errorf("check registry updates: %w", err)
	}

	return nil
}

func (sd *StepDiscovery) LoadBuiltinSteps(ctx context.Context) error {
	builtinSteps := map[string]*storage.RegisteredStep{
		"text_summary": {
			Name:   "text_summary",
			Source: "builtin",
			Path:   "",
			Metadata: &storage.StepMetadata{
				Name:        "text_summary",
				Description: "Summarizes text content using LLM",
				License:     "MIT",
				Version:     "1.0.0",
				Author:      "ContextHelp",
			},
			InstalledAt: time.Now(),
			UpdatedAt:   time.Now(),
		},
		"text_analysis": {
			Name:   "text_analysis",
			Source: "builtin",
			Path:   "",
			Metadata: &storage.StepMetadata{
				Name:        "text_analysis",
				Description: "Analyzes text content and extracts entities, decisions, tasks",
				License:     "MIT",
				Version:     "1.0.0",
				Author:      "ContextHelp",
			},
			InstalledAt: time.Now(),
			UpdatedAt:   time.Now(),
		},
		"embed_text": {
			Name:   "embed_text",
			Source: "builtin",
			Path:   "",
			Metadata: &storage.StepMetadata{
				Name:        "embed_text",
				Description: "Generates embeddings for text content",
				License:     "MIT",
				Version:     "1.0.0",
				Author:      "ContextHelp",
			},
			InstalledAt: time.Now(),
			UpdatedAt:   time.Now(),
		},
	}

	for _, step := range builtinSteps {
		if err := sd.store.Steps().Create(ctx, step); err != nil {
			return fmt.Errorf("create builtin step %s: %w", step.Name, err)
		}
	}

	return nil
}

func (sd *StepDiscovery) ScanLocalSteps(ctx context.Context) error {
	if sd.stepsPath == "" {
		sd.stepsPath = filepath.Join(os.Getenv("HOME"), ".config", "contexthelp", "steps")
	}

	entries, err := os.ReadDir(sd.stepsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read steps directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		stepPath := filepath.Join(sd.stepsPath, entry.Name())
		skillPath := filepath.Join(stepPath, "SKILL.md")

		metadata, err := sd.parseSkillFile(skillPath)
		if err != nil {
			continue
		}

		step := &storage.RegisteredStep{
			Name:        metadata.Name,
			Source:      "local:" + stepPath,
			Path:        stepPath,
			Metadata:    metadata,
			InstalledAt: time.Now(),
			UpdatedAt:   time.Now(),
		}

		if err := sd.store.Steps().Create(ctx, step); err != nil {
			return fmt.Errorf("create local step %s: %w", step.Name, err)
		}
	}

	return nil
}

func (sd *StepDiscovery) parseSkillFile(path string) (*storage.StepMetadata, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read skill file: %w", err)
	}

	metadata := &storage.StepMetadata{
		Name:           filepath.Base(filepath.Dir(path)),
		Description:    "",
		License:        "UNKNOWN",
		Version:        "1.0.0",
		Author:         "",
		ConfigSchema:   make(map[string]any),
		SupportedLangs: []string{},
	}

	lines := strings.Split(string(content), "\n")
	inFrontmatter := false
	frontmatterLines := []string{}

	for _, line := range lines {
		if strings.TrimSpace(line) == "---" {
			if !inFrontmatter {
				inFrontmatter = true
				continue
			}
			break
		}

		if inFrontmatter {
			frontmatterLines = append(frontmatterLines, line)
		}
	}

	if len(frontmatterLines) > 0 {
		var frontmatter map[string]any
		if err := yaml.Unmarshal([]byte(strings.Join(frontmatterLines, "\n")), &frontmatter); err == nil {
			if name, ok := frontmatter["name"].(string); ok {
				metadata.Name = name
			}
			if desc, ok := frontmatter["description"].(string); ok {
				metadata.Description = desc
			}
			if license, ok := frontmatter["license"].(string); ok {
				metadata.License = license
			}
			if version, ok := frontmatter["version"].(string); ok {
				metadata.Version = version
			}
			if author, ok := frontmatter["author"].(string); ok {
				metadata.Author = author
			}
			if schema, ok := frontmatter["config_schema"].(map[string]any); ok {
				metadata.ConfigSchema = schema
			}
			if langs, ok := frontmatter["supported_languages"].([]any); ok {
				for _, lang := range langs {
					if langStr, ok := lang.(string); ok {
						metadata.SupportedLangs = append(metadata.SupportedLangs, langStr)
					}
				}
			}
		}
	}

	return metadata, nil
}

func (sd *StepDiscovery) FetchRegistryManifest(ctx context.Context, url string) (*storage.RegistryManifest, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := sd.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch manifest: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch manifest: status %d", resp.StatusCode)
	}

	var manifest storage.RegistryManifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return nil, fmt.Errorf("decode manifest: %w", err)
	}

	cache := &storage.RegistryCache{
		RegistryURL: url,
		Manifest:    &manifest,
		LastFetched: time.Now(),
		ETag:        resp.Header.Get("ETag"),
	}

	if err := sd.store.Registries().CacheManifest(ctx, cache); err != nil {
		return nil, fmt.Errorf("cache manifest: %w", err)
	}

	return &manifest, nil
}

func (sd *StepDiscovery) CheckRegistryUpdates(ctx context.Context) error {
	registries, _, err := sd.store.Registries().List(ctx)
	if err != nil {
		return fmt.Errorf("list registries: %w", err)
	}

	for _, registry := range registries {
		req, err := http.NewRequestWithContext(ctx, "GET", registry.RegistryURL, nil)
		if err != nil {
			continue
		}

		if registry.ETag != "" {
			req.Header.Set("If-None-Match", registry.ETag)
		}

		resp, err := sd.client.Do(req)
		if err != nil {
			continue
		}
		resp.Body.Close()

		if resp.StatusCode == http.StatusNotModified {
			continue
		}

		if resp.StatusCode == http.StatusOK {
			if err := sd.NotifyUpdateAvailable(ctx, registry.RegistryURL, resp.Header.Get("ETag")); err != nil {
				return fmt.Errorf("notify update available: %w", err)
			}

			if registry.AutoUpdate {
				if _, err := sd.FetchRegistryManifest(ctx, registry.RegistryURL); err != nil {
					return fmt.Errorf("fetch updated manifest: %w", err)
				}
			}
		}
	}

	return nil
}

func (sd *StepDiscovery) DownloadStep(ctx context.Context, name, fromRegistry string) error {
	manifest, err := sd.FetchRegistryManifest(ctx, fromRegistry)
	if err != nil {
		return fmt.Errorf("fetch manifest: %w", err)
	}

	var manifestStep *storage.ManifestStep
	for _, step := range manifest.Steps {
		if step.Name == name {
			manifestStep = &step
			break
		}
	}

	if manifestStep == nil {
		return fmt.Errorf("step %s not found in registry", name)
	}

	stepPath := filepath.Join(sd.stepsPath, name)
	if err := os.MkdirAll(stepPath, 0755); err != nil {
		return fmt.Errorf("create step directory: %w", err)
	}

	stepURL := fromRegistry + "/" + manifestStep.Path
	req, err := http.NewRequestWithContext(ctx, "GET", stepURL, nil)
	if err != nil {
		return fmt.Errorf("create download request: %w", err)
	}

	resp, err := sd.client.Do(req)
	if err != nil {
		return fmt.Errorf("download step: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download step: status %d", resp.StatusCode)
	}

	skillPath := filepath.Join(stepPath, "SKILL.md")
	out, err := os.Create(skillPath)
	if err != nil {
		return fmt.Errorf("create skill file: %w", err)
	}
	defer out.Close()

	if _, err := out.ReadFrom(resp.Body); err != nil {
		return fmt.Errorf("write skill file: %w", err)
	}

	metadata, err := sd.parseSkillFile(skillPath)
	if err != nil {
		return fmt.Errorf("parse skill file: %w", err)
	}

	step := &storage.RegisteredStep{
		Name:        name,
		Source:      "registry:" + fromRegistry,
		Path:        stepPath,
		Metadata:    metadata,
		InstalledAt: time.Now(),
		UpdatedAt:   time.Now(),
	}

	if err := sd.store.Steps().Create(ctx, step); err != nil {
		return fmt.Errorf("register step: %w", err)
	}

	return nil
}

func (sd *StepDiscovery) InstallStep(ctx context.Context, name, fromRegistry string) error {
	return sd.DownloadStep(ctx, name, fromRegistry)
}

func (sd *StepDiscovery) NotifyUpdateAvailable(ctx context.Context, url, etag string) error {
	reminder := &storage.SystemReminder{
		ID:        generateID(),
		Type:      "update",
		Title:     "Registry Update Available",
		Message:   fmt.Sprintf("Registry %s has updates available", url),
		Source:    "registry",
		Dismissed: false,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := sd.store.Reminders().Create(ctx, reminder); err != nil {
		return fmt.Errorf("create reminder: %w", err)
	}

	if err := sd.store.Registries().UpdateETag(ctx, url, etag); err != nil {
		return fmt.Errorf("update etag: %w", err)
	}

	return nil
}

func generateID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}
