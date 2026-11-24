// Package encrypted provides a storage.BlobStore decorator that encrypts every
// blob payload and metadata record at rest using AES-256-GCM envelope encryption
// backed by a keys.Provider.
package encrypted

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/ideacrafterslabs/ctxt/internal/keys"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

const blobMagic = "CTXTENCB"

// Store wraps an inner storage.BlobStore and encrypts all objects before write
// and decrypts them on read. It fails closed whenever the KEK provider is
// unavailable, wrong, or encounters unencrypted/tampered bytes.
type Store struct {
	inner    storage.BlobStore
	provider keys.Provider
}

// New wraps inner with envelope encryption using provider.
func New(inner storage.BlobStore, provider keys.Provider) (*Store, error) {
	if inner == nil {
		return nil, errors.New("encrypted blob store: inner store is nil")
	}
	if provider == nil {
		return nil, fmt.Errorf("encrypted blob store: %w", keys.ErrUnconfigured)
	}
	return &Store{inner: inner, provider: provider}, nil
}

func aadForKey(key string) []byte { return []byte("blob:" + key) }

func packBlob(meta storage.BlobMeta, data []byte) ([]byte, error) {
	meta.Size = int64(len(data))
	mb, err := json.Marshal(meta)
	if err != nil {
		return nil, fmt.Errorf("marshal blob meta: %w", err)
	}
	if len(mb) > 0xffff {
		return nil, errors.New("blob metadata too large")
	}
	out := make([]byte, len(blobMagic)+2+len(mb)+len(data))
	off := copy(out, blobMagic)
	binary.BigEndian.PutUint16(out[off:], uint16(len(mb))) //nolint:gosec // checked <= 0xffff above
	off += 2
	off += copy(out[off:], mb)
	copy(out[off:], data)
	return out, nil
}

func unpackBlob(raw []byte) (storage.BlobMeta, []byte, error) {
	if len(raw) < len(blobMagic)+2 || string(raw[:len(blobMagic)]) != blobMagic {
		return storage.BlobMeta{}, nil, fmt.Errorf("%w: invalid blob payload header", keys.ErrUnwrap)
	}
	off := len(blobMagic)
	mlen := int(binary.BigEndian.Uint16(raw[off:]))
	off += 2
	if off+mlen > len(raw) {
		return storage.BlobMeta{}, nil, fmt.Errorf("%w: truncated blob metadata", keys.ErrUnwrap)
	}
	var meta storage.BlobMeta
	if err := json.Unmarshal(raw[off:off+mlen], &meta); err != nil {
		return storage.BlobMeta{}, nil, fmt.Errorf("%w: decode blob metadata: %w", keys.ErrUnwrap, err)
	}
	payload := raw[off+mlen:]
	meta.Size = int64(len(payload))
	return meta, payload, nil
}

// Put encrypts data and meta into an AAD-bound envelope and writes it to inner.
func (s *Store) Put(ctx context.Context, key string, data io.Reader, meta storage.BlobMeta) error {
	plain, err := io.ReadAll(data)
	if err != nil {
		return fmt.Errorf("encrypted blob put %s: read: %w", key, err)
	}
	packed, err := packBlob(meta, plain)
	if err != nil {
		return fmt.Errorf("encrypted blob put %s: %w", key, err)
	}
	env, err := keys.SealEnvelope(ctx, s.provider, packed, aadForKey(key))
	if err != nil {
		return fmt.Errorf("encrypted blob put %s: %w", key, err)
	}
	outerMeta := storage.BlobMeta{
		ContentType: "application/octet-stream",
		Size:        int64(len(env)),
	}
	return s.inner.Put(ctx, key, bytes.NewReader(env), outerMeta)
}

// Get reads and decrypts key from inner. Unencrypted or tampered blobs fail
// closed with keys.ErrUnwrap.
func (s *Store) Get(ctx context.Context, key string) (io.ReadCloser, storage.BlobMeta, error) {
	rc, _, err := s.inner.Get(ctx, key)
	if err != nil {
		return nil, storage.BlobMeta{}, err
	}
	env, rerr := io.ReadAll(rc)
	_ = rc.Close()
	if rerr != nil {
		return nil, storage.BlobMeta{}, fmt.Errorf("encrypted blob get %s: read: %w", key, rerr)
	}
	packed, err := keys.OpenEnvelope(ctx, s.provider, env, aadForKey(key))
	if err != nil {
		return nil, storage.BlobMeta{}, fmt.Errorf("encrypted blob get %s: %w", key, err)
	}
	meta, plain, err := unpackBlob(packed)
	if err != nil {
		return nil, storage.BlobMeta{}, fmt.Errorf("encrypted blob get %s: %w", key, err)
	}
	return io.NopCloser(bytes.NewReader(plain)), meta, nil
}

// Delete delegates to the underlying store.
func (s *Store) Delete(ctx context.Context, key string) error { return s.inner.Delete(ctx, key) }

// Exists delegates to the underlying store.
func (s *Store) Exists(ctx context.Context, key string) (bool, error) {
	return s.inner.Exists(ctx, key)
}

// List delegates to the underlying store.
func (s *Store) List(ctx context.Context, prefix string) ([]storage.BlobInfo, error) {
	return s.inner.List(ctx, prefix)
}

// URL refuses direct unauthenticated/presigned URLs for encrypted blobs.
func (s *Store) URL(_ context.Context, _ string) (string, error) {
	return "", errors.New("encrypted blob store: direct URLs are not supported")
}

// MigratePlaintext encrypts all plaintext blobs currently in inner under
// provider, skipping keys that are already valid envelopes for their key AAD.
// Returns the number of migrated blobs.
func MigratePlaintext(ctx context.Context, inner storage.BlobStore, provider keys.Provider) (int, error) {
	enc, err := New(inner, provider)
	if err != nil {
		return 0, err
	}
	items, err := inner.List(ctx, "")
	if err != nil {
		return 0, fmt.Errorf("migrate blobs: list: %w", err)
	}
	migrated := 0
	for _, item := range items {
		rc, meta, err := inner.Get(ctx, item.Key)
		if err != nil {
			return migrated, fmt.Errorf("migrate blob %s: read: %w", item.Key, err)
		}
		raw, rerr := io.ReadAll(rc)
		_ = rc.Close()
		if rerr != nil {
			return migrated, fmt.Errorf("migrate blob %s: read body: %w", item.Key, rerr)
		}
		if keys.IsEnvelope(raw) {
			if _, oerr := keys.OpenEnvelope(ctx, provider, raw, aadForKey(item.Key)); oerr == nil {
				continue
			}
		}
		if err := enc.Put(ctx, item.Key, bytes.NewReader(raw), meta); err != nil {
			return migrated, fmt.Errorf("migrate blob %s: write encrypted: %w", item.Key, err)
		}
		migrated++
	}
	return migrated, nil
}

// RewrapAll rewraps every encrypted blob in inner to toKeyID (or current key
// when empty) without decrypting the payload.
func RewrapAll(ctx context.Context, inner storage.BlobStore, provider keys.Provider, toKeyID string) (int, error) {
	items, err := inner.List(ctx, "")
	if err != nil {
		return 0, fmt.Errorf("rewrap blobs: list: %w", err)
	}
	rewrapped := 0
	for _, item := range items {
		rc, meta, err := inner.Get(ctx, item.Key)
		if err != nil {
			return rewrapped, fmt.Errorf("rewrap blob %s: read: %w", item.Key, err)
		}
		raw, rerr := io.ReadAll(rc)
		_ = rc.Close()
		if rerr != nil {
			return rewrapped, fmt.Errorf("rewrap blob %s: read body: %w", item.Key, rerr)
		}
		next, _, err := keys.RewrapEnvelope(ctx, provider, raw, aadForKey(item.Key), toKeyID)
		if err != nil {
			return rewrapped, fmt.Errorf("rewrap blob %s: %w", item.Key, err)
		}
		meta.Size = int64(len(next))
		if err := inner.Put(ctx, item.Key, bytes.NewReader(next), meta); err != nil {
			return rewrapped, fmt.Errorf("rewrap blob %s: write: %w", item.Key, err)
		}
		rewrapped++
	}
	return rewrapped, nil
}
