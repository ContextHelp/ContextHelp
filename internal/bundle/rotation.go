// Package bundle — key rotation support.
package bundle

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	// RotationLogName is the filename for the rotation log.
	RotationLogName = "rotation_log.json"
	// KeyAgeDays is the threshold (days) after which a warning is emitted.
	KeyAgeDays = 365
)

// RotationEntry is a single entry in the rotation log.
// Both old and new key sign a canonical JSON message to prove possession.
type RotationEntry struct {
	OldKey    string            `json:"old_key"`    // fingerprint of old key
	NewKey    string            `json:"new_key"`    // fingerprint of new key
	Timestamp string            `json:"timestamp"`  // RFC 3339
	Sigs      map[string]string `json:"signatures"` // "old" → hex sig, "new" → hex sig
}

// RotationLog is the persistent record of all key rotations.
type RotationLog struct {
	Entries []RotationEntry `json:"entries"`
}

// RotationLogPath returns the path to rotation_log.json in keysDir.
func RotationLogPath(keysDir string) string {
	return filepath.Join(keysDir, RotationLogName)
}

// LoadRotationLog reads the rotation log from disk. Returns an empty log if
// the file does not exist (first rotation).
func LoadRotationLog(keysDir string) (RotationLog, error) {
	path := RotationLogPath(keysDir)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return RotationLog{}, nil
	}
	if err != nil {
		return RotationLog{}, fmt.Errorf("bundle: read rotation log: %w", err)
	}
	var log RotationLog
	if err := json.Unmarshal(data, &log); err != nil {
		return RotationLog{}, fmt.Errorf("bundle: parse rotation log: %w", err)
	}
	return log, nil
}

// saveRotationLog writes the rotation log to disk atomically.
func saveRotationLog(keysDir string, log RotationLog) error {
	if err := os.MkdirAll(keysDir, 0700); err != nil {
		return fmt.Errorf("bundle: mkdir keys dir: %w", err)
	}
	data, err := json.MarshalIndent(log, "", "  ")
	if err != nil {
		return fmt.Errorf("bundle: marshal rotation log: %w", err)
	}
	path := RotationLogPath(keysDir)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return fmt.Errorf("bundle: write rotation log tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("bundle: rename rotation log: %w", err)
	}
	return nil
}

// rotationMessage returns the canonical byte payload both keys sign.
// Canonical form: JSON with sorted keys, no trailing newline.
func rotationMessage(oldFP, newFP, ts string) ([]byte, error) {
	m := map[string]string{
		"new_key":   newFP,
		"old_key":   oldFP,
		"timestamp": ts,
	}
	return json.Marshal(m)
}

// RotateResult summarises a completed key rotation.
type RotateResult struct {
	OldFingerprint string
	NewFingerprint string
	// OldKeyArchive is the path of the archived old private-key backup file.
	OldKeyArchive string
}

// RotateKeys generates a new Ed25519 keypair, creates a transition record
// signed by both keys, appends it to the rotation log, and archives the old
// private key. The new keypair is NOT persisted to the keychain — the caller
// is responsible for calling StorePrivateKey(newKP.Private).
func RotateKeys(keysDir string, oldPriv ed25519.PrivateKey) (KeyPair, RotateResult, error) {
	oldPub := PublicFromPrivate(oldPriv)
	oldFP := fingerprint(oldPub)

	newKP, err := GenerateKeyPair()
	if err != nil {
		return KeyPair{}, RotateResult{}, fmt.Errorf("bundle: rotate: generate new keypair: %w", err)
	}

	ts := time.Now().UTC().Format(time.RFC3339)
	msg, err := rotationMessage(oldFP, newKP.Fingerprint, ts)
	if err != nil {
		return KeyPair{}, RotateResult{}, fmt.Errorf("bundle: rotate: build message: %w", err)
	}

	sigOld := hex.EncodeToString(Sign(msg, oldPriv))
	sigNew := hex.EncodeToString(Sign(msg, newKP.Private))

	entry := RotationEntry{
		OldKey:    oldFP,
		NewKey:    newKP.Fingerprint,
		Timestamp: ts,
		Sigs: map[string]string{
			"old": sigOld,
			"new": sigNew,
		},
	}

	// Append to rotation log.
	rotLog, err := LoadRotationLog(keysDir)
	if err != nil {
		return KeyPair{}, RotateResult{}, err
	}
	rotLog.Entries = append(rotLog.Entries, entry)
	if err := saveRotationLog(keysDir, rotLog); err != nil {
		return KeyPair{}, RotateResult{}, err
	}

	// Archive old private key as signing.key.bak.<timestamp-safe>.
	archiveName := fmt.Sprintf("signing.key.bak.%s",
		time.Now().UTC().Format("20060102T150405Z"))
	archivePath := filepath.Join(keysDir, archiveName)
	if err := os.WriteFile(archivePath,
		[]byte(hex.EncodeToString(oldPriv)), 0600); err != nil {
		return KeyPair{}, RotateResult{}, fmt.Errorf("bundle: rotate: archive old key: %w", err)
	}

	res := RotateResult{
		OldFingerprint: oldFP,
		NewFingerprint: newKP.Fingerprint,
		OldKeyArchive:  archivePath,
	}
	return newKP, res, nil
}

// SigningKeyAge returns the modification time age of signing.key in keysDir.
// Returns an error if the file does not exist.
func SigningKeyAge(keysDir string) (time.Duration, error) {
	path := filepath.Join(keysDir, "signing.key")
	info, err := os.Stat(path)
	if err != nil {
		return 0, fmt.Errorf("bundle: stat signing.key: %w", err)
	}
	return time.Since(info.ModTime()), nil
}

// IsRotationChainValid verifies every entry in the rotation log:
// both the old-key and new-key signatures over the canonical message must be valid.
func IsRotationChainValid(keysDir string, knownKeys map[string]ed25519.PublicKey) error {
	rotLog, err := LoadRotationLog(keysDir)
	if err != nil {
		return err
	}
	for i, e := range rotLog.Entries {
		msg, err := rotationMessage(e.OldKey, e.NewKey, e.Timestamp)
		if err != nil {
			return fmt.Errorf("bundle: rotation entry %d: build message: %w", i, err)
		}
		for role, fp := range map[string]string{"old": e.OldKey, "new": e.NewKey} {
			pub, ok := knownKeys[fp]
			if !ok {
				// Cannot validate without the key — skip (chain may be partial).
				continue
			}
			sigHex, ok := e.Sigs[role]
			if !ok {
				return fmt.Errorf("bundle: rotation entry %d: missing %s signature", i, role)
			}
			sigBytes, err := hex.DecodeString(sigHex)
			if err != nil {
				return fmt.Errorf("bundle: rotation entry %d: decode %s sig: %w", i, role, err)
			}
			if err := Verify(msg, sigBytes, pub); err != nil {
				return fmt.Errorf("bundle: rotation entry %d: invalid %s signature: %w", i, role, err)
			}
		}
	}
	return nil
}

// FingerprintInChain returns true if fp is part of an unbroken rotation
// chain leading to currentFP (or equals currentFP).
func FingerprintInChain(keysDir, fp, currentFP string) (bool, error) {
	if fp == currentFP {
		return true, nil
	}
	rotLog, err := LoadRotationLog(keysDir)
	if err != nil {
		return false, err
	}
	// Build: old → new adjacency map.
	successor := make(map[string]string, len(rotLog.Entries))
	for _, e := range rotLog.Entries {
		successor[e.OldKey] = e.NewKey
	}
	// Walk the chain from fp until we reach currentFP or hit a dead end.
	cur := fp
	visited := make(map[string]bool)
	// Loop condition is the cycle guard: stop if we revisit a fingerprint.
	for !visited[cur] {
		visited[cur] = true
		next, ok := successor[cur]
		if !ok {
			break
		}
		if next == currentFP {
			return true, nil
		}
		cur = next
	}
	return false, nil
}
