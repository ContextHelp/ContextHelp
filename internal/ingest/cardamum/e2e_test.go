package cardamum

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/ingest"
)

// TestE2ECardamumIngestAndSearch exercises the full flow:
// write test vCards -> adapter Fetch -> runner dedup + store -> verify.
//
// Requires cardamum binary. Skipped when not available.
func TestE2ECardamumIngestAndSearch(t *testing.T) {
	if _, err := exec.LookPath("cardamum"); err != nil {
		t.Skip("cardamum binary not in PATH")
	}

	// Create temp vdir with test vCards.
	tmpDir := t.TempDir()
	vdir := filepath.Join(tmpDir, "contacts")
	if err := os.MkdirAll(vdir, 0o755); err != nil {
		t.Fatal(err)
	}

	vcards := map[string]string{
		"alice.vcf": "BEGIN:VCARD\r\nVERSION:3.0\r\n" +
			"FN:Alice Test\r\nORG:TestCorp\r\nTITLE:Engineer\r\n" +
			"EMAIL:alice@test.com\r\n" +
			"UID:e2e-alice-001\r\nEND:VCARD\r\n",
		"bob.vcf": "BEGIN:VCARD\r\nVERSION:3.0\r\n" +
			"FN:Bob Builder\r\nORG:BuildInc\r\n" +
			"UID:e2e-bob-002\r\nEND:VCARD\r\n",
	}

	for name, content := range vcards {
		if err := os.WriteFile(
			filepath.Join(vdir, name), []byte(content), 0o644,
		); err != nil {
			t.Fatal(err)
		}
	}

	// Write cardamum config pointing at temp vdir.
	cfgPath := filepath.Join(tmpDir, "cardamum.toml")
	cfgContent := `[test-e2e]
backend = "vdir"
vdir.path = "` + vdir + `"
`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Verify cardamum can list from this config.
	ctx := context.Background()
	cmd := exec.CommandContext(ctx, "cardamum",
		"-c", cfgPath, "-a", "test-e2e",
		"addressbooks", "list", "--json")
	out, err := cmd.Output()
	if err != nil {
		t.Skipf("cardamum config setup failed: %v", err)
	}

	var books []struct{ ID string }
	if err := json.Unmarshal(out, &books); err != nil || len(books) == 0 {
		t.Skipf("no addressbooks found: %v", err)
	}

	abID := books[0].ID

	// Create adapter with custom binary path + config.
	adapter := New(abID,
		WithBinary("cardamum"),
		WithAccount("test-e2e"),
	)
	// Override binary args to include config path.
	adapter.binary = "cardamum"

	// Use a custom adapter wrapper that injects -c flag.
	wrapper := &configAdapter{
		inner:   adapter,
		cfgPath: cfgPath,
	}

	// Run through runner with in-memory store.
	store := newTestStore()
	runner := ingest.NewRunner(store)

	res, err := runner.Run(ctx, wrapper)
	if err != nil {
		t.Fatalf("ingest run: %v", err)
	}

	if res.Total < 2 {
		t.Errorf("expected >= 2 objects, got %d", res.Total)
	}
	if res.Created < 2 {
		t.Errorf("expected >= 2 created, got %d", res.Created)
	}

	// Re-run: dedup should prevent new objects.
	beforeCount := len(store.objects)
	res2, err := runner.Run(ctx, wrapper)
	if err != nil {
		t.Fatalf("second ingest run: %v", err)
	}
	if len(store.objects) != beforeCount {
		t.Errorf("dedup failed: had %d objects, now %d",
			beforeCount, len(store.objects))
	}
	_ = res2

	// Verify stored objects have correct fields.
	for _, obj := range store.objects {
		if obj.Type != "contact" {
			t.Errorf("expected type contact, got %q", obj.Type)
		}
		if obj.Source != "cardamum" {
			t.Errorf("expected source cardamum, got %q", obj.Source)
		}
		if obj.SourceKey == "" {
			t.Error("expected non-empty source_key")
		}
	}
}

// configAdapter wraps the real adapter to inject -c flag.
type configAdapter struct {
	inner   *Adapter
	cfgPath string
}

func (c *configAdapter) Name() string { return c.inner.Name() }

func (c *configAdapter) Fetch(ctx context.Context) ([]ingest.Object, error) {
	args := []string{"-c", c.cfgPath, "-a", c.inner.account,
		"cards", "list", "--json", c.inner.addressbook}
	cmd := exec.CommandContext(ctx, c.inner.binary, args...)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var cards []card
	if err := json.Unmarshal(out, &cards); err != nil {
		return nil, err
	}
	return TransformCards(cards), nil
}
