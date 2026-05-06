// Package s3 is the s3 backend of the files protocol slot.
//
// It is a fetch-only sensor adapter that lists keys in an S3-compatible
// bucket and emits one ingest.Object per key. Declared capabilities:
// fetch + emit-events. Lifecycle methods are no-ops — every Fetch
// builds a fresh AWS SDK client.
//
// Object bodies are NOT downloaded in this PR; that lands later. The
// emitted ingest.Object carries the key, size, etag, last-modified,
// and storage class in its Metadata map; Content stays empty.
//
// Credentials resolution is also deferred. Config.CredentialsRef
// stores an opaque ref string (e.g. `op://Personal/aws-bucket`,
// `keychain://aws-research`) that a follow-up PR will resolve via
// kit/storage/secret. The adapter validates non-emptiness only.
package s3

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"hop.top/kit/go/runtime/bus"

	"github.com/ideacrafterslabs/ctxt/internal/adapter"
	"github.com/ideacrafterslabs/ctxt/internal/adapter/files"
	"github.com/ideacrafterslabs/ctxt/internal/ingest"
)

// Config carries the s3 adapter settings. All four required fields
// (BucketName, Region, CredentialsRef, plus a non-AWS Endpoint when
// targeting an S3-compatible service) are validated at Fetch time;
// missing values produce a loud error rather than a silent no-op.
type Config struct {
	// BucketName is the S3 bucket to list. Required.
	BucketName string
	// Region is the AWS region (or the region label of the
	// S3-compatible endpoint). Required.
	Region string
	// Endpoint optionally overrides the default AWS endpoint for
	// S3-compatible services (R2, B2, MinIO, Spaces, Wasabi, Garage).
	// Empty means use AWS.
	Endpoint string
	// CredentialsRef is an opaque reference to credentials stored
	// outside the YAML — `op://Personal/aws-bucket` (1Password CLI),
	// `keychain://aws-research` (macOS keychain), or an env-ref URI.
	// Resolution is deferred to a later PR; this adapter validates
	// non-empty only.
	CredentialsRef string
	// Prefix limits the listing to keys under this prefix. Empty
	// lists every key in the bucket.
	Prefix string
	// HTTPClient overrides the AWS SDK's default HTTP client. Production
	// leaves this nil and gets the SDK's standard transport; tests
	// inject an xrr-wrapped client to record / replay HTTP interactions
	// through cassettes.
	HTTPClient awsHTTPClient
}

// awsHTTPClient mirrors the smithy-go HTTP client interface without
// pulling in the smithy-go module name into this package's surface.
// awss3.Options accepts any value satisfying this shape.
type awsHTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

// New constructs a typed s3 Adapter. The AWS SDK client is built
// lazily on the first Fetch so that operators with multiple buckets
// configured don't pay the construction cost for buckets they never
// poll in a given sweep.
func New(cfg Config) *Adapter {
	return &Adapter{cfg: cfg}
}

// Adapter is the typed s3 sensor adapter.
type Adapter struct {
	cfg Config
}

// Compile-time assertion: Adapter satisfies the typed adapter contract.
var _ adapter.Adapter = (*Adapter)(nil)

// Protocol returns "files" — the slot identity.
func (a *Adapter) Protocol() string { return files.Protocol }

// Backend returns "s3" — the canonical backend identifier. Covers AWS
// S3 and every S3-compatible service via the Endpoint override.
func (a *Adapter) Backend() string { return "s3" }

// Capabilities returns the s3 backend's declared capabilities.
func (a *Adapter) Capabilities() []adapter.Capability {
	return []adapter.Capability{adapter.CapFetch, adapter.CapEmitEvents}
}

// Start is a no-op — the SDK client is built per-Fetch.
func (a *Adapter) Start(_ context.Context, _ bus.Bus) error { return nil }

// Ready returns true once the adapter is constructed.
func (a *Adapter) Ready() bool { return true }

// Drain is a no-op (no in-flight state).
func (a *Adapter) Drain(_ context.Context) error { return nil }

// Stop is a no-op (no resources held).
func (a *Adapter) Stop(_ context.Context) error { return nil }

// Fetch lists every key under Config.Prefix in Config.BucketName and
// returns one ingest.Object per key. Object bodies are NOT downloaded
// in this PR; only key + metadata.
func (a *Adapter) Fetch(ctx context.Context) ([]ingest.Object, error) {
	if err := a.validate(); err != nil {
		return nil, err
	}
	client := a.newClient()
	out, err := client.ListObjectsV2(ctx, &awss3.ListObjectsV2Input{
		Bucket: aws.String(a.cfg.BucketName),
		Prefix: aws.String(a.cfg.Prefix),
	})
	if err != nil {
		return nil, fmt.Errorf("s3 list %s: %w", a.cfg.BucketName, err)
	}
	objs := make([]ingest.Object, 0, len(out.Contents))
	for _, item := range out.Contents {
		objs = append(objs, a.objectFor(item))
	}
	return objs, nil
}

// Submit returns ErrCapabilityNotDeclared — s3 is fetch-only here.
func (a *Adapter) Submit(_ context.Context, _ ingest.Object) error {
	return adapter.ErrCapabilityNotDeclared
}

// Serve returns ErrCapabilityNotDeclared — s3 is fetch-only here.
func (a *Adapter) Serve(_ context.Context, _ net.Listener) error {
	return adapter.ErrCapabilityNotDeclared
}

// validate checks the required fields. Empty bucket/region/credentials
// fail at Fetch rather than New so unit tests can construct an Adapter
// without supplying every field, and so operator config errors surface
// during the actual ambient sweep where the failure is contextual.
func (a *Adapter) validate() error {
	var errs []error
	if a.cfg.BucketName == "" {
		errs = append(errs, errors.New("s3 adapter: BucketName is required"))
	}
	if a.cfg.Region == "" {
		errs = append(errs, errors.New("s3 adapter: Region is required"))
	}
	if a.cfg.CredentialsRef == "" {
		errs = append(errs, errors.New("s3 adapter: CredentialsRef is required"))
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// newClient builds an SDK S3 client. Credentials are stubbed via
// credentials.NewStaticCredentialsProvider — the brief defers real
// credential resolution to a follow-up PR. The Endpoint override (when
// set) routes traffic to S3-compatible services and to the httptest
// server in unit tests.
func (a *Adapter) newClient() *awss3.Client {
	awsCfg := aws.Config{
		Region:      a.cfg.Region,
		Credentials: credentials.NewStaticCredentialsProvider("stub-access", "stub-secret", ""),
	}
	opts := []func(*awss3.Options){}
	if a.cfg.Endpoint != "" {
		opts = append(opts, func(o *awss3.Options) {
			o.BaseEndpoint = aws.String(a.cfg.Endpoint)
			// Path-style is required for httptest + most
			// S3-compatible services that don't speak
			// virtual-host bucket addressing.
			o.UsePathStyle = true
		})
	}
	if a.cfg.HTTPClient != nil {
		opts = append(opts, func(o *awss3.Options) {
			o.HTTPClient = a.cfg.HTTPClient
		})
	}
	return awss3.NewFromConfig(awsCfg, opts...)
}

// objectFor maps an S3 ListObjectsV2 entry to an ingest.Object. The
// object body stays empty — bodies land in a follow-up PR — and the
// stable ID is endpoint+bucket+key so the same key reappearing across
// sweeps dedups at storage, and so two buckets with the same name
// across providers (AWS + R2) don't collide.
func (a *Adapter) objectFor(item s3types.Object) ingest.Object {
	key := aws.ToString(item.Key)
	id := fmt.Sprintf("s3://%s/%s/%s", a.cfg.Endpoint, a.cfg.BucketName, key)
	meta := map[string]any{
		"bucket":   a.cfg.BucketName,
		"region":   a.cfg.Region,
		"endpoint": a.cfg.Endpoint,
		"key":      key,
		"etag":     aws.ToString(item.ETag),
		"size":     aws.ToInt64(item.Size),
	}
	if item.LastModified != nil {
		meta["last_modified"] = item.LastModified.Format("2006-01-02T15:04:05Z07:00")
	}
	if item.StorageClass != "" {
		meta["storage_class"] = string(item.StorageClass)
	}
	return ingest.Object{
		ID:       id,
		Type:     "s3-object",
		Metadata: meta,
	}
}
