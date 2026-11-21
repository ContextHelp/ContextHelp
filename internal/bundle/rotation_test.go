package bundle_test

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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

// keyMaterial holds every encoding of a private key we refuse to find on disk.
type keyMaterial struct {
	name  string
	forms [][]byte
}

func encodingsOf(name string, priv ed25519.PrivateKey) keyMaterial {
	var forms [][]byte
	for _, raw := range [][]byte{priv, priv.Seed()} {
		h := hex.EncodeToString(raw)
		forms = append(forms,
			raw,
			[]byte(h),
			[]byte(strings.ToUpper(h)),
			[]byte(base64.StdEncoding.EncodeToString(raw)),
			[]byte(base64.URLEncoding.EncodeToString(raw)),
			[]byte(base64.RawStdEncoding.EncodeToString(raw)),
			[]byte(base64.RawURLEncoding.EncodeToString(raw)),
		)
	}
	return keyMaterial{name: name, forms: forms}
}

var tokenRE = regexp.MustCompile(`[A-Za-z0-9+/_=-]{40,}`)

// looksLikePrivateKey reports whether tok decodes (hex/base64) to a 64-byte
// Ed25519 private key (seed || matching public key).
func looksLikePrivateKey(tok string) bool {
	var decoded [][]byte
	if b, err := hex.DecodeString(tok); err == nil {
		decoded = append(decoded, b)
	}
	for _, enc := range []*base64.Encoding{
		base64.StdEncoding, base64.URLEncoding,
		base64.RawStdEncoding, base64.RawURLEncoding,
	} {
		if b, err := enc.DecodeString(tok); err == nil {
			decoded = append(decoded, b)
		}
	}
	for _, b := range decoded {
		if len(b) == ed25519.PrivateKeySize {
			pk := ed25519.NewKeyFromSeed(b[:ed25519.SeedSize])
			if bytes.Equal(pk, b) {
				return true
			}
		}
	}
	return false
}

// scanForPrivateKeys walks root; returns findings for any file holding the
// given keys in any common encoding, or any token that decodes to an Ed25519
// private key. Scans content of every file, not just by name.
func scanForPrivateKeys(t *testing.T, root string, keys ...keyMaterial) []string {
	t.Helper()
	var found []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, k := range keys {
			for _, f := range k.forms {
				if bytes.Contains(data, f) {
					found = append(found, path+": contains "+k.name+" private key material")
				}
			}
		}
		for _, tok := range tokenRE.FindAllString(string(data), -1) {
			if looksLikePrivateKey(tok) {
				found = append(found, path+": token decodes to an Ed25519 private key")
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return found
}

func assertNoPrivateKeyOnDisk(t *testing.T, root string, keys ...keyMaterial) {
	t.Helper()
	for _, f := range scanForPrivateKeys(t, root, keys...) {
		t.Error(f)
	}
}

func TestAssertNoPrivateKeyOnDisk_DetectsPlaintext(t *testing.T) {
	// Guard the scanner itself: each encoding must be caught.
	kp, _ := bundle.GenerateKeyPair()
	for name, content := range map[string]string{
		"hex":    hex.EncodeToString(kp.Private),
		"seed":   hex.EncodeToString(kp.Private.Seed()),
		"b64":    base64.StdEncoding.EncodeToString(kp.Private),
		"b64url": base64.RawURLEncoding.EncodeToString(kp.Private.Seed()),
		"json":   `{"k":"` + hex.EncodeToString(kp.Private) + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			os.WriteFile(filepath.Join(dir, "innocuous.txt"), []byte(content), 0600)
			if len(scanForPrivateKeys(t, dir, encodingsOf("k", kp.Private))) == 0 {
				t.Fatalf("scanner missed %s encoding", name)
			}
		})
	}
}

func TestRotateKeys_NoPrivateKeyMaterialOnDisk(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	keysDir := filepath.Join(home, ".config", "contexthelp", "keys")

	kp0, _ := bundle.GenerateKeyPair()
	mats := []keyMaterial{encodingsOf("kp0", kp0.Private)}
	kp := kp0
	for i := 1; i <= 3; i++ {
		next, _, err := bundle.RotateKeys(keysDir, kp.Private)
		if err != nil {
			t.Fatalf("rotation %d: %v", i, err)
		}
		mats = append(mats,
			encodingsOf("new key "+next.Fingerprint, next.Private))
		kp = next
	}
	assertNoPrivateKeyOnDisk(t, home, mats...)
}

func TestRotateKeys_OldBundlesVerifyAfterMultipleRotations(t *testing.T) {
	keysDir := t.TempDir()
	configDir := t.TempDir()

	kp0, _ := bundle.GenerateKeyPair()
	os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte("x: 1\n"), 0644)
	res, err := bundle.Build(bundle.BuildOpts{
		ConfigDir:  configDir,
		Files:      []string{"config.yaml"},
		OutputDir:  t.TempDir(),
		PrivateKey: kp0.Private,
		PublicKey:  kp0.Public,
	})
	if err != nil {
		t.Fatal(err)
	}

	kp := kp0
	known := map[string]ed25519.PublicKey{kp0.Fingerprint: kp0.Public}
	for i := 0; i < 3; i++ {
		next, _, err := bundle.RotateKeys(keysDir, kp.Private)
		if err != nil {
			t.Fatal(err)
		}
		known[next.Fingerprint] = next.Public
		kp = next
	}

	if err := bundle.IsRotationChainValid(keysDir, known); err != nil {
		t.Fatalf("chain invalid: %v", err)
	}
	if _, err := bundle.VerifyAndOpen(bundle.VerifyOpts{
		ZipPath:            res.ZipPath,
		SigPath:            res.SigPath,
		KeysDir:            keysDir,
		CurrentFingerprint: kp.Fingerprint,
	}); err != nil {
		t.Fatalf("old bundle must verify via chain after 3 rotations: %v", err)
	}
}
