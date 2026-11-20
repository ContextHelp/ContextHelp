package events

import (
	"time"

	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"hop.top/kit/go/runtime/bus"

	"github.com/ideacrafterslabs/ctxt/internal/config"
)

func newTestEvent(topic string, payload ProfilePayload) bus.Event {
	return bus.NewEvent(bus.Topic(topic), "test", payload)
}

func TestOnProfileCreated(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	cfg := &config.Config{}
	sub := NewSubscriber(cfg, cfgPath)

	e := newTestEvent("aps.profile.created", ProfilePayload{
		ID:         "testuser",
		Name:       "Test User",
		Department: "eng",
	})

	if err := sub.onProfileCreated(context.Background(), e); err != nil {
		t.Fatalf("onProfileCreated: %v", err)
	}

	fp, ok := cfg.Profile.Profiles["testuser"]
	if !ok {
		t.Fatal("profile not created")
	}

	// Check tags.
	wantTags := map[string]bool{
		"agent:testuser":   true,
		"dept:eng":         true,
		"pool:company":     true,
		"pool:product-eng": true,
		"pool:incidents":   true,
		"pool:compliance":  true,
	}
	for _, tag := range fp.Tags {
		delete(wantTags, tag)
	}
	if len(wantTags) > 0 {
		t.Errorf("missing tags: %v", wantTags)
	}

	// Check rerank boosts.
	if fp.RerankBoosts["pool:product-eng"] != 0.15 {
		t.Errorf("expected product-eng boost 0.15, got %f", fp.RerankBoosts["pool:product-eng"])
	}

	// Check file written.
	if _, err := os.Stat(cfgPath); err != nil {
		t.Errorf("config file not written: %v", err)
	}
}

func TestOnProfileCreated_Duplicate(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	cfg := &config.Config{
		Profile: config.ProfileConfig{
			Profiles: map[string]config.FocusProfile{
				"existing": {Description: "already here"},
			},
		},
	}
	sub := NewSubscriber(cfg, cfgPath)

	e := newTestEvent("aps.profile.created", ProfilePayload{
		ID:         "existing",
		Name:       "Existing",
		Department: "eng",
	})

	_ = sub.onProfileCreated(context.Background(), e)

	// Should NOT overwrite.
	if cfg.Profile.Profiles["existing"].Description != "already here" {
		t.Error("duplicate profile was overwritten")
	}
}

func TestOnProfileCreated_UnknownDept(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	cfg := &config.Config{}
	sub := NewSubscriber(cfg, cfgPath)

	e := newTestEvent("aps.profile.created", ProfilePayload{
		ID:         "mystery",
		Name:       "Mystery",
		Department: "unknown-dept",
	})

	_ = sub.onProfileCreated(context.Background(), e)

	if _, ok := cfg.Profile.Profiles["mystery"]; ok {
		t.Error("profile created for unknown department")
	}
}

func TestOnProfileUpdated(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	cfg := &config.Config{
		Profile: config.ProfileConfig{
			Profiles: map[string]config.FocusProfile{
				"updatable": {Description: "old desc", Tags: []string{"agent:updatable", "dept:eng"}},
			},
		},
	}
	sub := NewSubscriber(cfg, cfgPath)

	e := newTestEvent("aps.profile.updated", ProfilePayload{
		ID:         "updatable",
		Name:       "Updated User",
		Department: "sales",
	})

	_ = sub.onProfileUpdated(context.Background(), e)

	fp := cfg.Profile.Profiles["updatable"]
	// Dept should change to sales.
	found := false
	for _, tag := range fp.Tags {
		if tag == "dept:sales" {
			found = true
		}
	}
	if !found {
		t.Error("dept tag not updated to sales")
	}
	if fp.RerankBoosts["pool:revenue-ops"] != 0.15 {
		t.Errorf("expected revenue-ops boost 0.15, got %f", fp.RerankBoosts["pool:revenue-ops"])
	}
}

func TestOnProfileDeleted(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	cfg := &config.Config{
		Profile: config.ProfileConfig{
			Profiles: map[string]config.FocusProfile{
				"doomed": {Description: "to be deleted"},
			},
		},
	}
	sub := NewSubscriber(cfg, cfgPath)

	e := newTestEvent("aps.profile.deleted", ProfilePayload{ID: "doomed"})

	_ = sub.onProfileDeleted(context.Background(), e)

	if _, ok := cfg.Profile.Profiles["doomed"]; ok {
		t.Error("profile not deleted")
	}
}

func TestBusIntegration(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	cfg := &config.Config{}
	b := bus.New()
	sub := NewSubscriber(cfg, cfgPath)
	unregister := sub.Register(b)
	defer unregister()

	e := newTestEvent("aps.profile.created", ProfilePayload{
		ID:         "bustest",
		Name:       "Bus Test",
		Department: "legal",
	})

	// The handlers run async: wait for the one to land.
	require.NoError(t, b.Publish(context.Background(), e))
	require.Eventually(t, func() bool {
		sub.mu.Lock()
		defer sub.mu.Unlock()
		_, ok := cfg.Profile.Profiles["bustest"]
		return ok
	}, 2*time.Second, 10*time.Millisecond, "profile not created via bus integration")
}
