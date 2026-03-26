package bundle_test

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/bundle"
)

func TestGenerateKeyPair(t *testing.T) {
	kp, err := bundle.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	if len(kp.Public) == 0 {
		t.Fatal("empty public key")
	}
	if len(kp.Private) == 0 {
		t.Fatal("empty private key")
	}
	if kp.Fingerprint == "" {
		t.Fatal("empty fingerprint")
	}
}

func TestSignVerify_Valid(t *testing.T) {
	kp, err := bundle.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	msg := []byte("hello signed world")
	sig := bundle.Sign(msg, kp.Private)
	if err := bundle.Verify(msg, sig, kp.Public); err != nil {
		t.Fatalf("Verify: %v", err)
	}
}

func TestVerify_TamperedMessage(t *testing.T) {
	kp, _ := bundle.GenerateKeyPair()
	msg := []byte("original message")
	sig := bundle.Sign(msg, kp.Private)

	tampered := []byte("tampered message!")
	if err := bundle.Verify(tampered, sig, kp.Public); err == nil {
		t.Fatal("expected verification failure on tampered message, got nil")
	}
}

func TestVerify_WrongKey(t *testing.T) {
	kp1, _ := bundle.GenerateKeyPair()
	kp2, _ := bundle.GenerateKeyPair()
	msg := []byte("signed by kp1")
	sig := bundle.Sign(msg, kp1.Private)
	if err := bundle.Verify(msg, sig, kp2.Public); err == nil {
		t.Fatal("expected verification failure with wrong key, got nil")
	}
}

func TestSaveLoadPublicKey(t *testing.T) {
	dir := t.TempDir()
	kp, _ := bundle.GenerateKeyPair()

	if err := bundle.SavePublicKey(dir, kp); err != nil {
		t.Fatalf("SavePublicKey: %v", err)
	}

	path := bundle.PublicKeyPath(dir, kp.Fingerprint)
	loaded, err := bundle.LoadPublicKeyFile(path)
	if err != nil {
		t.Fatalf("LoadPublicKeyFile: %v", err)
	}
	if string(loaded) != string(kp.Public) {
		t.Fatal("loaded public key does not match original")
	}
}

func TestBuildAndVerify(t *testing.T) {
	kp, _ := bundle.GenerateKeyPair()

	configDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte("storage:\n  type: sqlite\n"), 0644); err != nil {
		t.Fatal(err)
	}

	outDir := t.TempDir()
	opts := bundle.BuildOpts{
		ConfigDir:  configDir,
		Files:      []string{"config.yaml"},
		OutputDir:  outDir,
		PrivateKey: kp.Private,
		PublicKey:  kp.Public,
	}

	result, err := bundle.Build(opts)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if _, err := os.Stat(result.ZipPath); err != nil {
		t.Fatalf("zip not created: %v", err)
	}
	if _, err := os.Stat(result.SigPath); err != nil {
		t.Fatalf("sig not created: %v", err)
	}

	// Verify using embedded public key (no PublicKey override).
	vResult, err := bundle.VerifyAndOpen(bundle.VerifyOpts{
		ZipPath: result.ZipPath,
		SigPath: result.SigPath,
	})
	if err != nil {
		t.Fatalf("VerifyAndOpen: %v", err)
	}
	if vResult.Manifest.SchemaVersion != bundle.SchemaVersion {
		t.Fatalf("unexpected schema_version %d", vResult.Manifest.SchemaVersion)
	}
	if vResult.Files["config.yaml"] == nil {
		t.Fatal("config.yaml not found in bundle")
	}
}

func TestVerifyAndOpen_TamperedZip(t *testing.T) {
	kp, _ := bundle.GenerateKeyPair()
	configDir := t.TempDir()
	os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte("key: value\n"), 0644)

	outDir := t.TempDir()
	result, _ := bundle.Build(bundle.BuildOpts{
		ConfigDir:  configDir,
		Files:      []string{"config.yaml"},
		OutputDir:  outDir,
		PrivateKey: kp.Private,
		PublicKey:  kp.Public,
	})

	// Tamper with the zip.
	data, _ := os.ReadFile(result.ZipPath)
	data[len(data)-1] ^= 0xFF
	os.WriteFile(result.ZipPath, data, 0644)

	_, err := bundle.VerifyAndOpen(bundle.VerifyOpts{
		ZipPath: result.ZipPath,
		SigPath: result.SigPath,
	})
	if err == nil {
		t.Fatal("expected verification error on tampered zip, got nil")
	}
}

func TestFingerprintOfHex_RoundTrip(t *testing.T) {
	kp, _ := bundle.GenerateKeyPair()
	pubHex := hex.EncodeToString(kp.Public)
	fp, err := bundle.FingerprintOfHex(pubHex)
	if err != nil {
		t.Fatalf("FingerprintOfHex: %v", err)
	}
	if fp != kp.Fingerprint {
		t.Fatalf("fingerprint mismatch: got %s, want %s", fp, kp.Fingerprint)
	}
}
