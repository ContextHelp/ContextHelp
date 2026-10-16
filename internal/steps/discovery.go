package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/registry"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type StepDiscovery struct {
	store  storage.StorageDriver
	client *http.Client
	creds  *registry.Credentials
}

func NewStepDiscovery(store storage.StorageDriver) *StepDiscovery {
	return &StepDiscovery{
		store:  store,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// WithCredentials authenticates manifest fetches, update checks and step
// downloads with the stored token of the registry each request targets.
// A 401/403 from a registry becomes a registry.ErrRegistryAuth naming the
// login command.
func (sd *StepDiscovery) WithCredentials(c *registry.Credentials) *StepDiscovery {
	sd.creds = c
	sd.client = c.Client(sd.client)
	return sd
}

// FetchRemoteManifest fetches a registry manifest from url without caching it.
// Used by dry-run to diff remote vs local state without writing to storage.
func (sd *StepDiscovery) FetchRemoteManifest(ctx context.Context, url string) (*storage.RegistryManifest, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := sd.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch manifest: %w", config.RedactURLError(err))
	}
	defer resp.Body.Close()

	if err := sd.creds.CheckResponse(resp); err != nil {
		return nil, fmt.Errorf("fetch manifest: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch manifest: status %d", resp.StatusCode)
	}

	var manifest storage.RegistryManifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return nil, fmt.Errorf("decode manifest: %w", err)
	}

	return &manifest, nil
}

func (sd *StepDiscovery) FetchRegistryManifest(ctx context.Context, url string) (*storage.RegistryManifest, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := sd.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch manifest: %w", config.RedactURLError(err))
	}
	defer resp.Body.Close()

	if err := sd.creds.CheckResponse(resp); err != nil {
		return nil, fmt.Errorf("fetch manifest: %w", err)
	}
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

// DownloadArchive fetches a step archive the registry at registryURL
// lists at rel, a path relative to the registry URL (an absolute URL is
// refused, so the registry's token never travels elsewhere), with the
// registry's credentials. It reads at most MaxArchiveBytes.
func (sd *StepDiscovery) DownloadArchive(ctx context.Context, registryURL, rel string) ([]byte, error) {
	clean, err := cleanRelPath(rel)
	if err != nil || strings.Contains(rel, "://") || strings.ContainsAny(rel, "?#") {
		return nil, invalid("registry archive path %q: want a path relative to the registry URL", rel)
	}
	archiveURL := strings.TrimSuffix(registryURL, "/") + "/" + clean
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, archiveURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create download request: %w", err)
	}
	resp, err := sd.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download step: %w", config.RedactURLError(err))
	}
	defer resp.Body.Close()
	if err := sd.creds.CheckResponse(resp); err != nil {
		return nil, fmt.Errorf("download step: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download step: status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxArchiveBytes+1))
	if err != nil {
		return nil, fmt.Errorf("download step: %w", err)
	}
	if len(body) > MaxArchiveBytes {
		return nil, invalid("step archive is larger than %d bytes", MaxArchiveBytes)
	}
	return body, nil
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
