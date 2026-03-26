// Package bundle implements Ed25519 signed config bundle export and import.
//
// Flow:
//
//	ctxt key init       → generate Ed25519 keypair; private key → OS keychain;
//	                      public key → ~/.config/contexthelp/keys/<fp>.pub
//	ctxt config backup  → zip config files; sign zip bytes; write .zip + .sig sidecar;
//	                      embed public key hex in manifest.json inside zip
//	ctxt config restore --verify → read .zip + .sig; verify before restore
package bundle

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

const (
	// KeychainService is the OS keychain service name for the signing private key.
	KeychainService = "ctxt-signing"
	// KeychainAccount is the keychain account name for the private key.
	KeychainAccount = "ed25519-private-key"
	// ManifestName is the entry name inside the bundle zip.
	ManifestName = "manifest.json"
	// SchemaVersion identifies the bundle format.
	SchemaVersion = 1
)

// Manifest is embedded in every signed bundle zip as manifest.json.
type Manifest struct {
	SchemaVersion int       `json:"schema_version"`
	CreatedAt     time.Time `json:"created_at"`
	// PublicKeyHex is the hex-encoded Ed25519 public key used to sign this bundle.
	// Enables self-contained verification without a separate key lookup.
	PublicKeyHex string `json:"public_key_hex"`
	// Fingerprint is the SHA-256 fingerprint of the public key (first 8 bytes, hex).
	Fingerprint string `json:"fingerprint"`
	// Files lists the non-manifest entries included in the bundle.
	Files []string `json:"files"`
}

// KeyPair holds an Ed25519 keypair and its derived fingerprint.
type KeyPair struct {
	Public      ed25519.PublicKey
	Private     ed25519.PrivateKey
	Fingerprint string // hex of SHA-256[:8] of public key bytes
}

// GenerateKeyPair creates a new Ed25519 keypair.
func GenerateKeyPair() (KeyPair, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return KeyPair{}, fmt.Errorf("bundle: generate key: %w", err)
	}
	return KeyPair{
		Public:      pub,
		Private:     priv,
		Fingerprint: fingerprint(pub),
	}, nil
}

// fingerprint returns the first 8 bytes of SHA-256(pubKey) as hex.
func fingerprint(pub ed25519.PublicKey) string {
	h := sha256.Sum256(pub)
	return hex.EncodeToString(h[:8])
}

// FingerprintOfHex derives the fingerprint from a hex-encoded public key.
func FingerprintOfHex(pubHex string) (string, error) {
	b, err := hex.DecodeString(pubHex)
	if err != nil {
		return "", fmt.Errorf("bundle: decode pubkey hex: %w", err)
	}
	return fingerprint(ed25519.PublicKey(b)), nil
}

// PublicKeyDir returns the directory where public keys are stored.
func PublicKeyDir(configDir string) string {
	return filepath.Join(configDir, "keys")
}

// PublicKeyPath returns the path for storing a public key file.
func PublicKeyPath(configDir, fp string) string {
	return filepath.Join(PublicKeyDir(configDir), fp+".pub")
}

// SavePublicKey writes the hex-encoded public key to <configDir>/keys/<fp>.pub.
func SavePublicKey(configDir string, kp KeyPair) error {
	dir := PublicKeyDir(configDir)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("bundle: mkdir keys dir: %w", err)
	}
	path := PublicKeyPath(configDir, kp.Fingerprint)
	content := hex.EncodeToString(kp.Public)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("bundle: write pubkey: %w", err)
	}
	return nil
}

// LoadPublicKeyFile reads a hex-encoded public key from a .pub file.
func LoadPublicKeyFile(path string) (ed25519.PublicKey, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("bundle: read pubkey file: %w", err)
	}
	decoded, err := hex.DecodeString(string(bytes.TrimSpace(b)))
	if err != nil {
		return nil, fmt.Errorf("bundle: decode pubkey file: %w", err)
	}
	if len(decoded) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("bundle: invalid pubkey length %d", len(decoded))
	}
	return ed25519.PublicKey(decoded), nil
}

// Sign returns an Ed25519 signature over message.
func Sign(message []byte, priv ed25519.PrivateKey) []byte {
	return ed25519.Sign(priv, message)
}

// PublicFromPrivate extracts the Ed25519 public key from a private key.
// Ed25519 private keys embed the public key in their last 32 bytes.
func PublicFromPrivate(priv ed25519.PrivateKey) ed25519.PublicKey {
	return priv.Public().(ed25519.PublicKey)
}

// Verify returns nil iff the signature is valid for message under pubKey.
func Verify(message, sig []byte, pub ed25519.PublicKey) error {
	if !ed25519.Verify(pub, message, sig) {
		return errors.New("bundle: signature verification failed")
	}
	return nil
}

// BuildOpts configures a bundle build operation.
type BuildOpts struct {
	// ConfigDir is the directory containing config files to bundle.
	ConfigDir string
	// Files are the filenames (within ConfigDir) to include. e.g. ["config.yaml"]
	Files []string
	// OutputDir is where the .zip and .sig are written.
	OutputDir string
	// PrivateKey used to sign the bundle bytes.
	PrivateKey ed25519.PrivateKey
	// PublicKey embedded in manifest for self-contained verification.
	PublicKey ed25519.PublicKey
}

// BuildResult summarises a completed bundle.
type BuildResult struct {
	ZipPath string
	SigPath string
	// Fingerprint of the signing key.
	Fingerprint string
}

// Build creates a signed bundle zip + .sig sidecar.
//
// The zip contains:
//   - manifest.json  (public key + file list)
//   - <files...>     (config files)
//
// The .sig sidecar contains the raw Ed25519 signature over the zip bytes.
func Build(opts BuildOpts) (BuildResult, error) {
	ts := time.Now().UTC().Format("2006-01-02T15-04-05Z")
	baseName := fmt.Sprintf("ctxt-config-bundle-%s", ts)
	zipPath := filepath.Join(opts.OutputDir, baseName+".zip")
	sigPath := zipPath + ".sig"

	fp := fingerprint(opts.PublicKey)

	// Build zip in memory first so we sign the exact bytes.
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	// Write config files into zip.
	var fileNames []string
	for _, fname := range opts.Files {
		srcPath := filepath.Join(opts.ConfigDir, fname)
		data, err := os.ReadFile(srcPath)
		if err != nil {
			return BuildResult{}, fmt.Errorf("bundle: read %s: %w", fname, err)
		}
		w, err := zw.Create(fname)
		if err != nil {
			return BuildResult{}, fmt.Errorf("bundle: zip create %s: %w", fname, err)
		}
		if _, err := w.Write(data); err != nil {
			return BuildResult{}, fmt.Errorf("bundle: zip write %s: %w", fname, err)
		}
		fileNames = append(fileNames, fname)
	}

	// Write manifest.json (with public key embedded).
	manifest := Manifest{
		SchemaVersion: SchemaVersion,
		CreatedAt:     time.Now().UTC(),
		PublicKeyHex:  hex.EncodeToString(opts.PublicKey),
		Fingerprint:   fp,
		Files:         fileNames,
	}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return BuildResult{}, fmt.Errorf("bundle: marshal manifest: %w", err)
	}
	mw, err := zw.Create(ManifestName)
	if err != nil {
		return BuildResult{}, fmt.Errorf("bundle: zip create manifest: %w", err)
	}
	if _, err := mw.Write(manifestBytes); err != nil {
		return BuildResult{}, fmt.Errorf("bundle: zip write manifest: %w", err)
	}

	if err := zw.Close(); err != nil {
		return BuildResult{}, fmt.Errorf("bundle: close zip: %w", err)
	}

	zipBytes := buf.Bytes()
	sig := Sign(zipBytes, opts.PrivateKey)

	// Write zip and sig files.
	if err := os.WriteFile(zipPath, zipBytes, 0644); err != nil {
		return BuildResult{}, fmt.Errorf("bundle: write zip: %w", err)
	}
	if err := os.WriteFile(sigPath, sig, 0644); err != nil {
		os.Remove(zipPath)
		return BuildResult{}, fmt.Errorf("bundle: write sig: %w", err)
	}

	return BuildResult{ZipPath: zipPath, SigPath: sigPath, Fingerprint: fp}, nil
}

// ExtractOnly opens a bundle zip and returns its file contents without verifying the signature.
// Suitable for restore without --verify. The caller is responsible for trust decisions.
func ExtractOnly(zipPath string) (map[string][]byte, error) {
	zipBytes, err := os.ReadFile(zipPath)
	if err != nil {
		return nil, fmt.Errorf("bundle: read zip: %w", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return nil, fmt.Errorf("bundle: open zip: %w", err)
	}
	files := make(map[string][]byte)
	for _, f := range zr.File {
		if f.Name == ManifestName {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("bundle: open entry %s: %w", f.Name, err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, fmt.Errorf("bundle: read entry %s: %w", f.Name, err)
		}
		files[f.Name] = data
	}
	return files, nil
}

// VerifyOpts configures a bundle verification + open operation.
type VerifyOpts struct {
	ZipPath string
	SigPath string
	// PublicKey overrides the key embedded in the manifest (optional).
	// When nil, the key embedded in manifest.json is used.
	PublicKey ed25519.PublicKey
	// KeysDir enables rotation-chain verification when set.
	// If the bundle's signing key is not the current key, the rotation log is
	// consulted to confirm the fingerprint is part of a valid chain.
	KeysDir string
	// CurrentFingerprint is the fingerprint of the current active key.
	// Used only when KeysDir is set.
	CurrentFingerprint string
}

// VerifyResult holds the opened bundle after successful verification.
type VerifyResult struct {
	Manifest Manifest
	// Files maps filename → content bytes for all non-manifest entries.
	Files map[string][]byte
}

// VerifyAndOpen reads a signed bundle, verifies the signature, and returns its contents.
// Uses the public key embedded in the bundle manifest unless opts.PublicKey is set.
func VerifyAndOpen(opts VerifyOpts) (VerifyResult, error) {
	zipBytes, err := os.ReadFile(opts.ZipPath)
	if err != nil {
		return VerifyResult{}, fmt.Errorf("bundle: read zip: %w", err)
	}
	sigBytes, err := os.ReadFile(opts.SigPath)
	if err != nil {
		return VerifyResult{}, fmt.Errorf("bundle: read sig: %w", err)
	}

	// Parse zip to extract manifest (needed for embedded public key).
	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return VerifyResult{}, fmt.Errorf("bundle: open zip: %w", err)
	}

	var manifest Manifest
	files := make(map[string][]byte)

	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			return VerifyResult{}, fmt.Errorf("bundle: open zip entry %s: %w", f.Name, err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return VerifyResult{}, fmt.Errorf("bundle: read zip entry %s: %w", f.Name, err)
		}
		if f.Name == ManifestName {
			if err := json.Unmarshal(data, &manifest); err != nil {
				return VerifyResult{}, fmt.Errorf("bundle: parse manifest: %w", err)
			}
			continue
		}
		files[f.Name] = data
	}

	// Resolve public key: caller-supplied > manifest-embedded.
	pubKey := opts.PublicKey
	if pubKey == nil {
		if manifest.PublicKeyHex == "" {
			return VerifyResult{}, errors.New("bundle: no public key in manifest; cannot verify")
		}
		decoded, err := hex.DecodeString(manifest.PublicKeyHex)
		if err != nil {
			return VerifyResult{}, fmt.Errorf("bundle: decode manifest pubkey: %w", err)
		}
		if len(decoded) != ed25519.PublicKeySize {
			return VerifyResult{}, fmt.Errorf("bundle: manifest pubkey wrong size %d", len(decoded))
		}
		pubKey = ed25519.PublicKey(decoded)
	}

	if err := Verify(zipBytes, sigBytes, pubKey); err != nil {
		return VerifyResult{}, err
	}

	// Rotation-chain check: if a keysDir is provided and the bundle was signed
	// by an older key, verify the chain still leads to the current key.
	if opts.KeysDir != "" && opts.CurrentFingerprint != "" &&
		manifest.Fingerprint != opts.CurrentFingerprint {
		ok, err := FingerprintInChain(opts.KeysDir, manifest.Fingerprint, opts.CurrentFingerprint)
		if err != nil {
			return VerifyResult{}, fmt.Errorf("bundle: rotation chain lookup: %w", err)
		}
		if !ok {
			return VerifyResult{}, fmt.Errorf(
				"bundle: signing key %s is not in the rotation chain for current key %s",
				manifest.Fingerprint, opts.CurrentFingerprint,
			)
		}
	}

	return VerifyResult{Manifest: manifest, Files: files}, nil
}
