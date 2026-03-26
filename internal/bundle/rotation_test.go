package bundle_test

import (
	"crypto/ed25519"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/bundle"
)

func TestRotateKeys_ProducesNewKeypair(t *testing.T) {
	keysDir := t.TempDir()

	oldKP, err := bundle.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}

	newKP, res, err := bundle.RotateKeys(keysDir, oldKP.Private)
	if err != nil {
		t.Fatalf("RotateKeys: %v", err)
	}

	if newKP.Fingerprint == oldKP.Fingerprint {
		t.Fatal("new fingerprint must differ from old")
	}
	if res.OldFingerprint != oldKP.Fingerprint {
		t.Fatalf("OldFingerprint mismatch: got %s want %s", res.OldFingerprint, oldKP.Fingerprint)
	}
	if res.NewFingerprint != newKP.Fingerprint {
		t.Fatalf("NewFingerprint mismatch: got %s want %s", res.NewFingerprint, newKP.Fingerprint)
	}
}

func TestRotateKeys_WritesRotationLog(t *testing.T) {
	keysDir := t.TempDir()

	kp, _ := bundle.GenerateKeyPair()
	_, _, err := bundle.RotateKeys(keysDir, kp.Private)
	if err != nil {
		t.Fatalf("RotateKeys: %v", err)
	}

	logPath := bundle.RotationLogPath(keysDir)
	if _, err := os.Stat(logPath); err != nil {
		t.Fatalf("rotation_log.json not created: %v", err)
	}

	rotLog, err := bundle.LoadRotationLog(keysDir)
	if err != nil {
		t.Fatalf("LoadRotationLog: %v", err)
	}
	if len(rotLog.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(rotLog.Entries))
	}
}

func TestRotateKeys_ArchivesOldKey(t *testing.T) {
	keysDir := t.TempDir()

	kp, _ := bundle.GenerateKeyPair()
	_, res, err := bundle.RotateKeys(keysDir, kp.Private)
	if err != nil {
		t.Fatalf("RotateKeys: %v", err)
	}

	if _, err := os.Stat(res.OldKeyArchive); err != nil {
		t.Fatalf("archive file not created: %v", err)
	}
	// Archive must be inside keysDir.
	if filepath.Dir(res.OldKeyArchive) != keysDir {
		t.Fatalf("archive not in keysDir: %s", res.OldKeyArchive)
	}
}

func TestRotateKeys_MultipleRotations(t *testing.T) {
	keysDir := t.TempDir()

	kp, _ := bundle.GenerateKeyPair()
	for i := 0; i < 3; i++ {
		newKP, _, err := bundle.RotateKeys(keysDir, kp.Private)
		if err != nil {
			t.Fatalf("rotation %d: %v", i, err)
		}
		kp = newKP
	}

	rotLog, err := bundle.LoadRotationLog(keysDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(rotLog.Entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(rotLog.Entries))
	}
}

func TestRotationEntrySignatures_Valid(t *testing.T) {
	keysDir := t.TempDir()

	oldKP, _ := bundle.GenerateKeyPair()
	newKP, _, err := bundle.RotateKeys(keysDir, oldKP.Private)
	if err != nil {
		t.Fatal(err)
	}

	knownKeys := map[string]ed25519.PublicKey{
		oldKP.Fingerprint: oldKP.Public,
		newKP.Fingerprint: newKP.Public,
	}

	if err := bundle.IsRotationChainValid(keysDir, knownKeys); err != nil {
		t.Fatalf("IsRotationChainValid: %v", err)
	}
}

func TestFingerprintInChain_DirectMatch(t *testing.T) {
	keysDir := t.TempDir()
	ok, err := bundle.FingerprintInChain(keysDir, "abc", "abc")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("fingerprint should match itself")
	}
}

func TestFingerprintInChain_ChainedKey(t *testing.T) {
	keysDir := t.TempDir()

	kp0, _ := bundle.GenerateKeyPair()
	kp1, _, _ := bundle.RotateKeys(keysDir, kp0.Private)
	kp2, _, _ := bundle.RotateKeys(keysDir, kp1.Private)

	// kp0 → kp1 → kp2; kp0 should be in chain for current=kp2.
	ok, err := bundle.FingerprintInChain(keysDir, kp0.Fingerprint, kp2.Fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("kp0 should be found in chain leading to kp2")
	}
}

func TestFingerprintInChain_UnrelatedKey(t *testing.T) {
	keysDir := t.TempDir()

	kp, _ := bundle.GenerateKeyPair()
	other, _ := bundle.GenerateKeyPair()

	ok, err := bundle.FingerprintInChain(keysDir, other.Fingerprint, kp.Fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("unrelated key should not be in chain")
	}
}

func TestSigningKeyAge_ReturnsAge(t *testing.T) {
	keysDir := t.TempDir()
	keyPath := filepath.Join(keysDir, "signing.key")
	// Write a dummy key file.
	if err := os.WriteFile(keyPath, []byte("dummy"), 0600); err != nil {
		t.Fatal(err)
	}
	age, err := bundle.SigningKeyAge(keysDir)
	if err != nil {
		t.Fatalf("SigningKeyAge: %v", err)
	}
	if age < 0 || age > 5*time.Second {
		t.Fatalf("unexpected age: %v", age)
	}
}

func TestSigningKeyAge_MissingFile(t *testing.T) {
	keysDir := t.TempDir()
	_, err := bundle.SigningKeyAge(keysDir)
	if err == nil {
		t.Fatal("expected error for missing signing.key")
	}
}

func TestRotateKeys_ArchiveContainsOldPrivKey(t *testing.T) {
	keysDir := t.TempDir()

	oldKP, _ := bundle.GenerateKeyPair()
	_, res, err := bundle.RotateKeys(keysDir, oldKP.Private)
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(res.OldKeyArchive)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	decoded, err := hex.DecodeString(string(data))
	if err != nil {
		t.Fatalf("decode archive hex: %v", err)
	}
	if string(decoded) != string(oldKP.Private) {
		t.Fatal("archive does not match old private key")
	}
}

func TestVerifyAndOpen_RotationChain(t *testing.T) {
	keysDir := t.TempDir()
	configDir := t.TempDir()

	// Initial keypair signs a bundle.
	kp0, _ := bundle.GenerateKeyPair()

	cfgFile := filepath.Join(configDir, "config.yaml")
	os.WriteFile(cfgFile, []byte("storage:\n  type: sqlite\n"), 0644)

	outDir := t.TempDir()
	result, err := bundle.Build(bundle.BuildOpts{
		ConfigDir:  configDir,
		Files:      []string{"config.yaml"},
		OutputDir:  outDir,
		PrivateKey: kp0.Private,
		PublicKey:  kp0.Public,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Rotate once — kp0 → kp1.
	kp1, _, err := bundle.RotateKeys(keysDir, kp0.Private)
	if err != nil {
		t.Fatal(err)
	}

	// Bundle was signed by kp0; current key is kp1.
	// Verification with rotation chain should succeed.
	_, err = bundle.VerifyAndOpen(bundle.VerifyOpts{
		ZipPath:            result.ZipPath,
		SigPath:            result.SigPath,
		KeysDir:            keysDir,
		CurrentFingerprint: kp1.Fingerprint,
	})
	if err != nil {
		t.Fatalf("VerifyAndOpen with rotation chain: %v", err)
	}
}

func TestVerifyAndOpen_RejectsUnrelatedOldKey(t *testing.T) {
	keysDir := t.TempDir()
	configDir := t.TempDir()

	// Sign with an unrelated key, then set a different "current" key.
	kpUnrelated, _ := bundle.GenerateKeyPair()
	kpCurrent, _ := bundle.GenerateKeyPair()

	cfgFile := filepath.Join(configDir, "config.yaml")
	os.WriteFile(cfgFile, []byte("x: 1\n"), 0644)

	outDir := t.TempDir()
	result, err := bundle.Build(bundle.BuildOpts{
		ConfigDir:  configDir,
		Files:      []string{"config.yaml"},
		OutputDir:  outDir,
		PrivateKey: kpUnrelated.Private,
		PublicKey:  kpUnrelated.Public,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// No rotation entries in keysDir — kpUnrelated is not in kpCurrent's chain.
	_, err = bundle.VerifyAndOpen(bundle.VerifyOpts{
		ZipPath:            result.ZipPath,
		SigPath:            result.SigPath,
		KeysDir:            keysDir,
		CurrentFingerprint: kpCurrent.Fingerprint,
	})
	if err == nil {
		t.Fatal("expected error: unrelated old key not in rotation chain")
	}
}
