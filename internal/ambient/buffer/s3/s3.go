// Package s3 is the S3-compatible ambient.Buffer backend.
//
// Per the storage-lives-in-kit principle, this package is a THIN SHIM
// over kit/go/storage/blob/s3. The Buffer interface adds RawEvent JSON
// encoding + chronological-ordering semantics on top of the blob.Store
// key/value primitive; the underlying SDK setup, endpoint resolution,
// path-style toggles, and CRUD all live in kit and are reused by
// internal/storage/blob/s3 and any downstream tools that want
// S3-compatible storage.
//
// Backends: AWS S3, Cloudflare R2, Backblaze B2, MinIO — anything that
// implements the S3 API.
//
// Layout under <Bucket>/<Prefix>/:
//
//	events/<source>/<yyyy>/<mm>/<dd>/<event-id>.json
//
// Lex-ordering of keys via the date tree gives chronological ordering
// for Pop. Retention lives at the provider side (lifecycle policies).
package s3

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"sync/atomic"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"

	kitblob "hop.top/kit/go/storage/blob"
	kits3 "hop.top/kit/go/storage/blob/s3"

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
	// Region. Optional; falls back to AWS_REGION env or "auto".
	Region string
	// AccessKey / SecretKey. Optional; falls back to default cred chain.
	AccessKey string
	SecretKey string
	// UsePathStyle forces path-style addressing. Set true for MinIO.
	UsePathStyle bool
}

// Buffer is the S3-backed ambient.Buffer. Wraps kit/go/storage/blob/s3.
type Buffer struct {
	store  kitblob.Store
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
		return nil, fmt.Errorf("ambient/buffer/s3: load AWS config: %w", err)
	}

	var clientOpts []func(*awss3.Options)
	if cfg.Endpoint != "" {
		endpoint := cfg.Endpoint
		usePathStyle := cfg.UsePathStyle
		clientOpts = append(clientOpts, func(o *awss3.Options) {
			o.BaseEndpoint = &endpoint
			o.UsePathStyle = usePathStyle
		})
	}
	client := awss3.NewFromConfig(awsCfg, clientOpts...)
	prefix := cfg.Prefix
	if prefix != "" && prefix[len(prefix)-1] != '/' {
		prefix += "/"
	}
	return &Buffer{
		store:  kits3.New(client, cfg.Bucket, prefix),
		bucket: cfg.Bucket,
		prefix: cfg.Prefix,
	}, nil
}

func keyForEvent(ev ambient.RawEvent) string {
	when := ev.OccurredAt
	if when.IsZero() {
		when = time.Now()
	}
	id := ev.Fingerprint
	if id == "" {
		id = fmt.Sprintf("%d", when.UnixNano())
	}
	return fmt.Sprintf("events/%s/%04d/%02d/%02d/%s.json",
		ev.Source, when.Year(), int(when.Month()), when.Day(), id)
}

// Append implements ambient.Buffer.
func (b *Buffer) Append(ctx context.Context, ev ambient.RawEvent) error {
	body, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	key := keyForEvent(ev)
	if err := b.store.Put(ctx, key, bytes.NewReader(body), "application/json"); err != nil {
		return fmt.Errorf("put %s: %w", key, err)
	}
	b.appendedTotal.Add(1)
	return nil
}

// Pop implements ambient.Buffer.
func (b *Buffer) Pop(ctx context.Context) (ambient.RawEvent, error) {
	objs, err := b.store.List(ctx, "events/")
	if err != nil {
		return ambient.RawEvent{}, fmt.Errorf("list events: %w", err)
	}
	if len(objs) == 0 {
		return ambient.RawEvent{}, ambient.ErrBufferEmpty
	}
	sort.Slice(objs, func(i, j int) bool { return objs[i].Key < objs[j].Key })
	oldest := objs[0]

	rc, err := b.store.Get(ctx, oldest.Key)
	if err != nil {
		return ambient.RawEvent{}, fmt.Errorf("get %s: %w", oldest.Key, err)
	}
	body, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		return ambient.RawEvent{}, fmt.Errorf("read %s: %w", oldest.Key, err)
	}
	var ev ambient.RawEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		return ambient.RawEvent{}, fmt.Errorf("unmarshal %s: %w", oldest.Key, err)
	}
	// Best-effort delete; substrate dedups on fingerprint, so a rare
	// double-pop after a mid-flight failure is recoverable.
	_ = b.store.Delete(ctx, oldest.Key)
	b.poppedTotal.Add(1)
	return ev, nil
}

// Range implements ambient.Buffer.
func (b *Buffer) Range(ctx context.Context, fn func(ambient.RawEvent) bool) error {
	objs, err := b.store.List(ctx, "events/")
	if err != nil {
		return fmt.Errorf("list events: %w", err)
	}
	sort.Slice(objs, func(i, j int) bool { return objs[i].Key < objs[j].Key })
	for _, obj := range objs {
		if err := ctx.Err(); err != nil {
			return err
		}
		rc, err := b.store.Get(ctx, obj.Key)
		if err != nil {
			continue
		}
		body, err := io.ReadAll(rc)
		rc.Close()
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

// Len implements ambient.Buffer. Issues an S3 ListObjects round-trip;
// callers should not call Len in hot paths.
func (b *Buffer) Len() int {
	objs, err := b.store.List(context.Background(), "events/")
	if err != nil {
		return 0
	}
	return len(objs)
}

// Stats implements ambient.Buffer.
func (b *Buffer) Stats() ambient.BufferStats {
	return ambient.BufferStats{
		Backend:       "s3",
		AppendedTotal: b.appendedTotal.Load(),
		PoppedTotal:   b.poppedTotal.Load(),
		Extra: map[string]any{
			"bucket": b.bucket,
			"prefix": b.prefix,
		},
	}
}

// Compile-time assertion.
var _ ambient.Buffer = (*Buffer)(nil)
