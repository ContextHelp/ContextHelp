package keys

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"

	"hop.top/kit/go/storage/secret"
)

type ring struct {
	name    string
	mu      sync.RWMutex
	keys    map[string][]byte
	current string
	down    bool
}

func newRing(name string) *ring {
	return &ring{name: name, keys: map[string][]byte{}}
}

func (r *ring) add(id string, key []byte, makeCurrent bool) error {
	if !ValidID(id) {
		return fmt.Errorf("key id %q: invalid format", id)
	}
	if len(key) != MaterialSize {
		return fmt.Errorf("key %q: must be %d bytes", id, MaterialSize)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.keys[id]; dup {
		return fmt.Errorf("key id %q: duplicate", id)
	}
	r.keys[id] = append([]byte(nil), key...)
	if makeCurrent || r.current == "" {
		r.current = id
	}
	return nil
}

func (r *ring) Name() string { return r.name }

func (r *ring) check() error {
	if r.down {
		return fail(ErrUnavailable, "%s provider is down", r.name)
	}
	if r.current == "" {
		return ErrUnconfigured
	}
	return nil
}

func (r *ring) Current(context.Context) (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if err := r.check(); err != nil {
		return "", err
	}
	return r.current, nil
}

func (r *ring) Wrap(_ context.Context, keyID string, material, aad []byte) ([]byte, string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if err := r.check(); err != nil {
		return nil, "", err
	}
	if keyID == "" {
		keyID = r.current
	}
	k, ok := r.keys[keyID]
	if !ok {
		return nil, "", ErrUnknownKey
	}
	w, err := sealAEAD(k, material, aad)
	return w, keyID, err
}

func (r *ring) Unwrap(_ context.Context, keyID string, wrapped, aad []byte) ([]byte, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if err := r.check(); err != nil {
		return nil, err
	}
	k, ok := r.keys[keyID]
	if !ok {
		return nil, ErrUnknownKey
	}
	return openAEAD(k, wrapped, aad)
}

func (r *ring) Rewrap(ctx context.Context, keyID string, wrapped, aad []byte, toID string) ([]byte, string, error) {
	mat, err := r.Unwrap(ctx, keyID, wrapped, aad)
	if err != nil {
		return nil, "", err
	}
	defer zeroBytes(mat)
	return r.Wrap(ctx, toID, mat, aad)
}

func (r *ring) Health(ctx context.Context) error {
	probe := make([]byte, MaterialSize)
	if _, err := rand.Read(probe); err != nil {
		return fail(ErrUnavailable, "random source")
	}
	aad := []byte("ctxt.keys.health")
	w, id, err := r.Wrap(ctx, "", probe, aad)
	if err != nil {
		return err
	}
	back, err := r.Unwrap(ctx, id, w, aad)
	if err != nil {
		return err
	}
	if string(back) != string(probe) {
		return fail(ErrUnavailable, "health round-trip mismatch")
	}
	return nil
}

// MemoryProvider is an in-memory Provider for tests supporting rotation,
// retirement, and simulated downtime.
type MemoryProvider struct{ *ring }

// NewMemory creates an in-memory Provider seeded with one random key.
func NewMemory(initialID string) (*MemoryProvider, error) {
	if initialID == "" {
		initialID = "k1"
	}
	m := &MemoryProvider{ring: newRing("memory")}
	if _, err := m.Rotate(initialID); err != nil {
		return nil, err
	}
	return m, nil
}

// Rotate generates a fresh 32-byte key under id and makes it current.
func (m *MemoryProvider) Rotate(id string) ([]byte, error) {
	raw := make([]byte, MaterialSize)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	if err := m.add(id, raw, true); err != nil {
		return nil, err
	}
	return raw, nil
}

// Retire removes id from the keyring (refuses retiring the active key).
func (m *MemoryProvider) Retire(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if id == m.current {
		return errors.New("keys: cannot retire active key")
	}
	delete(m.keys, id)
	return nil
}

// SetDown toggles simulated unavailability.
func (m *MemoryProvider) SetDown(v bool) {
	m.mu.Lock()
	m.down = v
	m.mu.Unlock()
}

// ParseKeys parses "id=base64" entries separated by newlines or commas.
func ParseKeys(src string) ([]string, map[string][]byte, error) {
	var ids []string
	out := map[string][]byte{}
	sc := bufio.NewScanner(strings.NewReader(src))
	for sc.Scan() {
		for _, part := range strings.Split(sc.Text(), ",") {
			part = strings.TrimSpace(part)
			if part == "" || strings.HasPrefix(part, "#") {
				continue
			}
			id, enc, ok := strings.Cut(part, "=")
			id, enc = strings.TrimSpace(id), strings.TrimSpace(enc)
			if !ok || id == "" || enc == "" {
				return nil, nil, errors.New("keys: entry must be id=base64")
			}
			if !ValidID(id) {
				return nil, nil, fmt.Errorf("keys: invalid key id %q", id)
			}
			raw, err := base64.StdEncoding.DecodeString(enc)
			if err != nil || len(raw) != MaterialSize {
				return nil, nil, fmt.Errorf("keys: key %q must be %d bytes standard base64", id, MaterialSize)
			}
			if _, dup := out[id]; dup {
				return nil, nil, fmt.Errorf("keys: duplicate key id %q", id)
			}
			ids = append(ids, id)
			out[id] = raw
		}
	}
	if len(ids) == 0 {
		return nil, nil, ErrUnconfigured
	}
	return ids, out, nil
}

func newRingFromSpec(name, spec, currentOverride string) (Provider, error) {
	ids, keyMap, err := ParseKeys(spec)
	if err != nil {
		return nil, err
	}
	r := newRing(name)
	for _, id := range ids {
		if err := r.add(id, keyMap[id], true); err != nil {
			return nil, err
		}
	}
	if currentOverride != "" {
		if _, ok := r.keys[currentOverride]; !ok {
			return nil, fmt.Errorf("%w: current key %q not in keyring", ErrUnknownKey, currentOverride)
		}
		r.current = currentOverride
	}
	return r, nil
}

// NewLocalFromEnv loads a local ring from an "id=base64[,...]" string (e.g. env var).
func NewLocalFromEnv(raw, currentOverride string) (Provider, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, ErrUnconfigured
	}
	return newRingFromSpec("local", raw, currentOverride)
}

// NewLocalFromFile loads a local ring from path, refusing non-regular files or
// files accessible by group/others on Unix (must be 0600 or 0400).
func NewLocalFromFile(path, currentOverride string) (Provider, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, ErrUnconfigured
	}
	fi, err := os.Stat(path)
	if err != nil {
		return nil, fail(ErrUnavailable, "stat key file: %v", err)
	}
	if !fi.Mode().IsRegular() {
		return nil, fail(ErrUnavailable, "key file must be a regular file")
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm()&0o077 != 0 {
		return nil, fail(ErrUnavailable, "key file permissions %04o too open (require 0600 or 0400)", fi.Mode().Perm())
	}
	raw, err := os.ReadFile(path) // #nosec G304 -- operator-configured key file path
	if err != nil {
		return nil, fail(ErrUnavailable, "read key file: %v", err)
	}
	return newRingFromSpec("local", string(raw), currentOverride)
}

// NewFromSecretStore loads the keyring string from an existing kit secret.Store
// (keyring, agefile, onepassword, env) under secretName.
func NewFromSecretStore(ctx context.Context, store secret.Store, secretName, currentOverride string) (Provider, error) {
	if store == nil || strings.TrimSpace(secretName) == "" {
		return nil, ErrUnconfigured
	}
	sec, err := store.Get(ctx, secretName)
	if errors.Is(err, secret.ErrNotFound) {
		return nil, fail(ErrUnavailable, "secret %q not found", secretName)
	}
	if err != nil {
		return nil, fail(ErrUnavailable, "read secret %q: %v", secretName, err)
	}
	return newRingFromSpec("secret", string(sec.Value), currentOverride)
}
