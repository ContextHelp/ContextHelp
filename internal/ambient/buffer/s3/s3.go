// Package s3 is the S3-compatible ambient.Buffer backend (per ADR-066
// §Decision item 5 + T-0510).
//
// Backends: AWS S3, Cloudflare R2, Backblaze B2, MinIO — anything that
// implements the S3 API. Authentication via standard env vars
// (AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, AWS_REGION) or static
// credentials in Config.
//
// This backend exists for cross-machine / compliance retention scenarios
// (multi-laptop households, ephemeral worker hosts, hosted-ctxd
// deployments). For the typical solo-laptop case, the local-FS backend
// (T-0506) is the right default.
//
// Storage layout under <Bucket>/<Prefix>/:
//
//	events/<source>/<yyyy>/<mm>/<dd>/<event-id>.json
//	media/<session-id>/<filename>.{mov,mp4,m4a,...}
//
// Retention: lifecycle policies on the provider side (S3 lifecycle rules,
// R2/B2/MinIO equivalents). The Buffer.Stats() reports object counts and
// total bytes; the daemon doesn't enforce retention itself for this backend.
package s3

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/ideacrafterslabs/ctxt/internal/ambient"
)

// Config configures an S3 ambient buffer.
type Config struct {
	// Bucket is required.
	Bucket string
	// Prefix is prepended to every object key. Optional.
	Prefix string
	// Endpoint overrides the default AWS S3 endpoint. Set to your R2 /
	// B2 / MinIO URL for non-AWS providers. Optional.
	Endpoint string
	// Region. Optional; falls back to AWS_REGION env or "auto" for
	// providers that don't care.
	Region string
	// AccessKey / SecretKey. Optional; falls back to default credential
	// chain (env, instance profile, etc.).
	AccessKey string
	SecretKey string
	// UsePathStyle forces path-style addressing (vs. virtual-host). Set
	// true for MinIO and some R2 configurations.
	UsePathStyle bool
}

// Buffer is the S3-backed ambient.Buffer.
//
// Goroutine-safe; concurrent calls to Append/Pop/Range are serialised
// at the S3 client level (AWS SDK is thread-safe). Counters use
// sync/atomic.
type Buffer struct {
	client *s3.Client
	bucket string
	prefix string

	appendedTotal atomic.Uint64
	poppedTotal   atomic.Uint64
}

// New constructs an S3-backed Buffer.
func New(ctx context.Context, cfg Config) (*Buffer, error) {
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("ambient/buffer/s3: bucket is required")
	}

	var loadOpts []func(*awsconfig.LoadOptions) error
	if cfg.Region != "" {
		loadOpts = append(loadOpts, awsconfig.WithRegion(cfg.Region))
	}
	if cfg.AccessKey != "" && cfg.SecretKey != "" {
		loadOpts = append(loadOpts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
		))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return nil, fmt.Errorf("ambient/buffer/s3: load config: %w", err)
	}

	var clientOpts []func(*s3.Options)
	if cfg.Endpoint != "" {
		endpoint := cfg.Endpoint
		usePathStyle := cfg.UsePathStyle
		clientOpts = append(clientOpts, func(o *s3.Options) {
			o.BaseEndpoint = &endpoint
			o.UsePathStyle = usePathStyle
		})
	}

	return &Buffer{
		client: s3.NewFromConfig(awsCfg, clientOpts...),
		bucket: cfg.Bucket,
		prefix: strings.TrimSuffix(cfg.Prefix, "/"),
	}, nil
}

// keyForEvent returns the S3 object key for a RawEvent. Layout:
// <prefix>/events/<source>/<yyyy>/<mm>/<dd>/<fingerprint-or-timestamp>.json
func (b *Buffer) keyForEvent(ev ambient.RawEvent) string {
	when := ev.OccurredAt
	if when.IsZero() {
		when = time.Now()
	}
	id := ev.Fingerprint
	if id == "" {
		// Fall back to nanosecond timestamp so distinct events with empty
		// fingerprints don't collide.
		id = fmt.Sprintf("%d", when.UnixNano())
	}
	parts := []string{
		"events",
		ev.Source,
		fmt.Sprintf("%04d", when.Year()),
		fmt.Sprintf("%02d", when.Month()),
		fmt.Sprintf("%02d", when.Day()),
		id + ".json",
	}
	full := path.Join(parts...)
	if b.prefix != "" {
		full = path.Join(b.prefix, full)
	}
	return full
}

// eventsPrefix returns the prefix under which all events live. Used by
// Pop/Range/Stats to list and consume.
func (b *Buffer) eventsPrefix() string {
	if b.prefix == "" {
		return "events/"
	}
	return path.Join(b.prefix, "events") + "/"
}

// Append implements ambient.Buffer. Marshals ev to JSON and uploads.
func (b *Buffer) Append(ctx context.Context, ev ambient.RawEvent) error {
	body, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	key := b.keyForEvent(ev)
	_, err = b.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      &b.bucket,
		Key:         &key,
		Body:        bytes.NewReader(body),
		ContentType: stringPtr("application/json"),
	})
	if err != nil {
		return fmt.Errorf("put object %s: %w", key, err)
	}
	b.appendedTotal.Add(1)
	return nil
}

// Pop implements ambient.Buffer. Lists, picks the oldest, GETs, and
// DELETEs atomically (best-effort — S3 doesn't support true atomic
// take-and-delete, but the substrate's at-least-once semantics tolerate
// rare double-pop).
func (b *Buffer) Pop(ctx context.Context) (ambient.RawEvent, error) {
	keys, err := b.listAllKeys(ctx)
	if err != nil {
		return ambient.RawEvent{}, err
	}
	if len(keys) == 0 {
		return ambient.RawEvent{}, ambient.ErrBufferEmpty
	}
	// Keys are sorted lexicographically by listAllKeys; the date-tree
	// layout makes lex-order roughly chronological. Take the first.
	key := keys[0]
	resp, err := b.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &b.bucket,
		Key:    &key,
	})
	if err != nil {
		return ambient.RawEvent{}, fmt.Errorf("get object %s: %w", key, err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return ambient.RawEvent{}, fmt.Errorf("read object %s: %w", key, err)
	}
	var ev ambient.RawEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		return ambient.RawEvent{}, fmt.Errorf("unmarshal %s: %w", key, err)
	}
	// Best-effort delete; substrate dedups on fingerprint so re-pop is
	// not catastrophic.
	_, _ = b.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: &b.bucket,
		Key:    &key,
	})
	b.poppedTotal.Add(1)
	return ev, nil
}

// Range implements ambient.Buffer. Iterates all events oldest-first
// without removing them.
func (b *Buffer) Range(ctx context.Context, fn func(ambient.RawEvent) bool) error {
	keys, err := b.listAllKeys(ctx)
	if err != nil {
		return err
	}
	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			return err
		}
		resp, err := b.client.GetObject(ctx, &s3.GetObjectInput{
			Bucket: &b.bucket,
			Key:    &key,
		})
		if err != nil {
			continue
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			continue
		}
		var ev ambient.RawEvent
		if json.Unmarshal(body, &ev) != nil {
			continue
		}
		if !fn(ev) {
			return nil
		}
	}
	return nil
}

// Len implements ambient.Buffer. Counts objects under the events prefix.
// Note: this issues an S3 ListObjects round-trip; callers should not call
// Len in hot paths. For monitoring / health endpoints, prefer Stats().
func (b *Buffer) Len() int {
	keys, err := b.listAllKeys(context.Background())
	if err != nil {
		return 0
	}
	return len(keys)
}

// Stats implements ambient.Buffer.
func (b *Buffer) Stats() ambient.BufferStats {
	stats := ambient.BufferStats{
		Backend:       "s3",
		AppendedTotal: b.appendedTotal.Load(),
		PoppedTotal:   b.poppedTotal.Load(),
		Extra: map[string]any{
			"bucket": b.bucket,
			"prefix": b.prefix,
		},
	}
	return stats
}

// listAllKeys lists all keys under the events prefix, sorted ascending.
// Paginates via ContinuationToken.
func (b *Buffer) listAllKeys(ctx context.Context) ([]string, error) {
	var keys []string
	prefix := b.eventsPrefix()
	var continuation *string
	for {
		resp, err := b.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            &b.bucket,
			Prefix:            &prefix,
			ContinuationToken: continuation,
		})
		if err != nil {
			return nil, fmt.Errorf("list objects: %w", err)
		}
		for _, obj := range resp.Contents {
			if obj.Key != nil {
				keys = append(keys, *obj.Key)
			}
		}
		if resp.IsTruncated == nil || !*resp.IsTruncated {
			break
		}
		continuation = resp.NextContinuationToken
	}
	sort.Strings(keys)
	return keys, nil
}

func stringPtr(s string) *string { return &s }

// Compile-time assertion: *Buffer satisfies ambient.Buffer.
var _ ambient.Buffer = (*Buffer)(nil)

// avoid unused-import warning when no s3types are referenced; keep the
// import handy for future presigned-URL helpers.
var _ s3types.Object
