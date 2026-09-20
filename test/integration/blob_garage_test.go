//go:build integration

// Garage S3-compatibility integration test.
//
// Verifies that ctxt's blob.Store works against a real Garage instance.
// Garage is API-compatible with S3 over SigV4 + path-style addressing; the
// "garage" backend preset in internal/storage/blob/factory.go applies those
// defaults automatically. This test exercises the full BlobStore interface
// (Put → Exists → Get → List → URL → Delete) end-to-end.
//
// How to run:
//
//  1. Bring your own Garage:
//     export CTXT_GARAGE_ENDPOINT=http://localhost:3900
//     export CTXT_GARAGE_ACCESS_KEY=GK...
//     export CTXT_GARAGE_SECRET_KEY=...
//     export CTXT_GARAGE_BUCKET=ctxt-test
//     make test-integration
//
//  2. Auto-launch via docker (skipped if docker is unavailable):
//     export CTXT_GARAGE_AUTO=1
//     make test-integration
//
// Skipped silently when no env is set and CTXT_GARAGE_AUTO is unset.
package integration

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob"
)

const (
	garageImage      = "dxflrs/garage:v1.0.1"
	garageContainer  = "ctxt-garage-itest"
	garageHTTPPort   = "3900"
	garageAdminPort  = "3903"
	garageReadyDelay = 10 * time.Second
)

func TestGarageBlobRoundTrip(t *testing.T) {
	endpoint, access, secret, bucket, cleanup := garageSetup(t)
	defer cleanup()

	cfg := config.BlobConfig{
		Backend: "garage",
		S3: config.BlobS3Config{
			Endpoint:  endpoint,
			Bucket:    bucket,
			AccessKey: access,
			SecretKey: secret,
		},
	}

	store, err := blob.New(cfg)
	if err != nil {
		t.Fatalf("blob.New: %v", err)
	}

	ctx := context.Background()
	key := randomHex(t, 16)
	payload := []byte("ctxt → garage round-trip\nline 2\n")
	meta := storage.BlobMeta{
		ContentType: "text/plain",
		Size:        int64(len(payload)),
		ContentHash: key,
	}

	if err := store.Put(ctx, key, bytes.NewReader(payload), meta); err != nil {
		t.Fatalf("Put: %v", err)
	}

	exists, err := store.Exists(ctx, key)
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !exists {
		t.Fatal("Exists: expected true after Put")
	}

	rc, gotMeta, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	got, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("Get body mismatch:\n got %q\nwant %q", got, payload)
	}
	if gotMeta.ContentType != meta.ContentType {
		t.Errorf("Get meta.ContentType: got %q want %q", gotMeta.ContentType, meta.ContentType)
	}
	if gotMeta.Size != meta.Size {
		t.Errorf("Get meta.Size: got %d want %d", gotMeta.Size, meta.Size)
	}

	infos, err := store.List(ctx, "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(infos) == 0 {
		t.Fatal("List returned no items after Put")
	}

	url, err := store.URL(ctx, key)
	if err != nil {
		t.Fatalf("URL: %v", err)
	}
	if !strings.Contains(url, bucket) {
		t.Errorf("presigned URL missing bucket %q: %s", bucket, url)
	}
	resp, err := http.Get(url) //nolint:gosec // presigned URL by design
	if err != nil {
		t.Fatalf("GET presigned: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("presigned URL fetch: got status %d, want 200", resp.StatusCode)
	}

	if err := store.Delete(ctx, key); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	exists, err = store.Exists(ctx, key)
	if err != nil {
		t.Fatalf("Exists after Delete: %v", err)
	}
	if exists {
		t.Error("Exists: expected false after Delete")
	}
}

// garageSetup returns endpoint, access key, secret key, bucket, and a
// teardown function. It either uses env vars (BYO Garage), auto-launches a
// docker container, or skips the test.
func garageSetup(t *testing.T) (endpoint, access, secret, bucket string, cleanup func()) {
	t.Helper()

	if ep := os.Getenv("CTXT_GARAGE_ENDPOINT"); ep != "" {
		ak := os.Getenv("CTXT_GARAGE_ACCESS_KEY")
		sk := os.Getenv("CTXT_GARAGE_SECRET_KEY")
		bk := os.Getenv("CTXT_GARAGE_BUCKET")
		if ak == "" || sk == "" || bk == "" {
			t.Skipf("CTXT_GARAGE_ENDPOINT set but ACCESS_KEY/SECRET_KEY/BUCKET missing")
		}
		return ep, ak, sk, bk, func() {}
	}

	if os.Getenv("CTXT_GARAGE_AUTO") == "" {
		t.Skip("garage integration: set CTXT_GARAGE_ENDPOINT or CTXT_GARAGE_AUTO=1")
	}

	if _, err := exec.LookPath("docker"); err != nil {
		t.Skipf("garage auto-mode requires docker: %v", err)
	}
	if out, err := exec.Command("docker", "info", "--format", "ok").CombinedOutput(); err != nil {
		t.Skipf("docker daemon not reachable: %v\n%s", err, out)
	}

	return launchGarage(t)
}

// launchGarage runs a single-node Garage container, creates a bucket and a
// key pair, and returns connection details. cleanup stops + removes the
// container.
func launchGarage(t *testing.T) (endpoint, access, secret, bucket string, cleanup func()) {
	t.Helper()

	rmIfExists(garageContainer)

	cfgPath := writeGarageConfig(t)
	args := []string{
		"run", "-d", "--rm",
		"--name", garageContainer,
		"-p", garageHTTPPort + ":3900",
		"-p", garageAdminPort + ":3903",
		"-v", cfgPath + ":/etc/garage.toml:ro",
		garageImage,
	}
	if out, err := exec.Command("docker", args...).CombinedOutput(); err != nil {
		t.Fatalf("docker run garage: %v\n%s", err, out)
	}
	cleanup = func() { _ = exec.Command("docker", "stop", garageContainer).Run() }

	endpoint = "http://127.0.0.1:" + garageHTTPPort
	adminURL := "http://127.0.0.1:" + garageAdminPort + "/health"
	if err := waitForGarage(adminURL, garageReadyDelay); err != nil {
		cleanup()
		t.Fatalf("garage not ready: %v", err)
	}

	if err := garageInitLayout(); err != nil {
		cleanup()
		t.Fatalf("layout: %v", err)
	}

	bucket = "ctxt-itest"
	if err := garageCmd("bucket", "create", bucket); err != nil {
		cleanup()
		t.Fatalf("bucket create: %v", err)
	}

	access, secret, err := garageNewKey("ctxt-itest")
	if err != nil {
		cleanup()
		t.Fatalf("key create: %v", err)
	}
	if err := garageCmd("bucket", "allow", "--read", "--write", "--owner", bucket, "--key", access); err != nil {
		cleanup()
		t.Fatalf("bucket allow: %v", err)
	}

	return endpoint, access, secret, bucket, cleanup
}

// garageInitLayout assigns a role to the single node and commits the layout.
// Required before bucket/key operations succeed on a fresh Garage cluster.
func garageInitLayout() error {
	out, err := exec.Command("docker", "exec", garageContainer, "/garage", "status").CombinedOutput()
	if err != nil {
		return fmt.Errorf("status: %w\n%s", err, out)
	}
	var nodeID string
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && len(fields[0]) == 16 && isHex(fields[0]) {
			nodeID = fields[0]
			break
		}
	}
	if nodeID == "" {
		return fmt.Errorf("could not parse node id from status output:\n%s", out)
	}
	if err := garageCmd("layout", "assign", "-z", "dc1", "-c", "1G", nodeID); err != nil {
		return err
	}
	return garageCmd("layout", "apply", "--version", "1")
}

func isHex(s string) bool {
	_, err := hex.DecodeString(s)
	return err == nil
}

// writeGarageConfig writes a minimal single-node Garage configuration to a
// temp file and returns its absolute path.
func writeGarageConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := dir + "/garage.toml"
	body := `metadata_dir = "/var/lib/garage/meta"
data_dir = "/var/lib/garage/data"
db_engine = "sqlite"
replication_factor = 1
rpc_bind_addr = "[::]:3901"
rpc_public_addr = "127.0.0.1:3901"
rpc_secret = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

[s3_api]
s3_region = "garage"
api_bind_addr = "[::]:3900"
root_domain = ".s3.garage.localhost"

[s3_web]
bind_addr = "[::]:3902"
root_domain = ".web.garage.localhost"
index = "index.html"

[admin]
api_bind_addr = "[::]:3903"
admin_token = "ctxt-admin-token"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write garage config: %v", err)
	}
	return path
}

func waitForGarage(healthURL string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(healthURL) //nolint:gosec // local
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("garage health at %s did not return 200 within %s", healthURL, timeout)
}

func garageCmd(args ...string) error {
	full := append([]string{"exec", garageContainer, "/garage"}, args...)
	out, err := exec.Command("docker", full...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("garage %v: %w\n%s", args, err, out)
	}
	return nil
}

// garageNewKey creates a new Garage access key and parses the output.
func garageNewKey(name string) (access, secret string, err error) {
	out, err := exec.Command("docker", "exec", garageContainer, "/garage", "key", "create", name).CombinedOutput()
	if err != nil {
		return "", "", fmt.Errorf("key create: %w\n%s", err, out)
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "Key ID:"):
			access = strings.TrimSpace(strings.TrimPrefix(line, "Key ID:"))
		case strings.HasPrefix(line, "Secret key:"):
			secret = strings.TrimSpace(strings.TrimPrefix(line, "Secret key:"))
		}
	}
	if access == "" || secret == "" {
		return "", "", fmt.Errorf("could not parse key output:\n%s", out)
	}
	return access, secret, nil
}

func rmIfExists(name string) {
	_ = exec.Command("docker", "rm", "-f", name).Run()
}

func randomHex(t *testing.T, n int) string {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return hex.EncodeToString(b)
}
