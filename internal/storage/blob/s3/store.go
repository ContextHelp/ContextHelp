// Package s3 implements storage.BlobStore on an S3-compatible backend.
package s3

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	cfgpkg "github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Store implements storage.BlobStore using an S3-compatible backend.
type Store struct {
	client        *s3.Client
	bucket        string
	prefix        string
	presignExpiry time.Duration
}

func New(cfg cfgpkg.BlobS3Config) (*Store, error) {
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("blob s3: bucket is required")
	}

	ctx := context.Background()
	var opts []func(*awsconfig.LoadOptions) error

	if cfg.Region != "" {
		opts = append(opts, awsconfig.WithRegion(cfg.Region))
	}
	if cfg.AccessKey != "" && cfg.SecretKey != "" {
		opts = append(opts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
		))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("blob s3: load config: %w", err)
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

	presign := cfg.PresignExpiry
	if presign == 0 {
		presign = time.Hour
	}

	return &Store{
		client:        s3.NewFromConfig(awsCfg, clientOpts...),
		bucket:        cfg.Bucket,
		prefix:        cfg.Prefix,
		presignExpiry: presign,
	}, nil
}

func (s *Store) keyPath(key string) string {
	var sharded string
	if len(key) >= 4 {
		sharded = path.Join(key[:2], key[2:4], key)
	} else {
		sharded = key
	}
	if s.prefix != "" {
		return path.Join(s.prefix, sharded)
	}
	return sharded
}

func (s *Store) metaKey(key string) string {
	return s.keyPath(key) + ".meta.json"
}

func (s *Store) Put(ctx context.Context, key string, data io.Reader, meta storage.BlobMeta) error {
	objKey := s.keyPath(key)
	contentType := meta.ContentType
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      &s.bucket,
		Key:         &objKey,
		Body:        data,
		ContentType: &contentType,
	})
	if err != nil {
		return fmt.Errorf("blob s3 put: %w", err)
	}

	metaBytes, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("blob s3 put meta: marshal: %w", err)
	}
	metaKey := s.metaKey(key)
	ct := "application/json"
	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        &s.bucket,
		Key:           &metaKey,
		Body:          bytes.NewReader(metaBytes),
		ContentType:   &ct,
		ContentLength: func() *int64 { v := int64(len(metaBytes)); return &v }(),
	})
	if err != nil {
		return fmt.Errorf("blob s3 put meta: %w", err)
	}

	return nil
}

func (s *Store) Get(ctx context.Context, key string) (io.ReadCloser, storage.BlobMeta, error) {
	objKey := s.keyPath(key)
	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &s.bucket,
		Key:    &objKey,
	})
	if isNotFound(err) {
		return nil, storage.BlobMeta{}, fmt.Errorf("blob %q: %w: %w", key, storage.ErrBlobNotFound, err)
	}
	if err != nil {
		return nil, storage.BlobMeta{}, fmt.Errorf("blob s3 get %q: %w", key, err)
	}

	var meta storage.BlobMeta
	metaKey := s.metaKey(key)
	metaResult, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &s.bucket,
		Key:    &metaKey,
	})
	if err == nil {
		defer metaResult.Body.Close()
		json.NewDecoder(metaResult.Body).Decode(&meta) //nolint:errcheck
	}

	return result.Body, meta, nil
}

func (s *Store) Delete(ctx context.Context, key string) error {
	metaKey := s.metaKey(key)
	_, _ = s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: &s.bucket,
		Key:    &metaKey,
	})

	objKey := s.keyPath(key)
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: &s.bucket,
		Key:    &objKey,
	})
	if err != nil {
		return fmt.Errorf("blob s3 delete: %w", err)
	}
	return nil
}

func (s *Store) Exists(ctx context.Context, key string) (bool, error) {
	objKey := s.keyPath(key)
	_, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: &s.bucket,
		Key:    &objKey,
	})
	if err != nil {
		// Only a definitive "no such key" means the blob is absent. Every
		// other failure (auth, network, throttling) leaves existence
		// unknown, and reporting it as absent would let callers overwrite
		// or re-upload live data. Mirrors the local store, which returns
		// the error for any stat failure other than ErrNotExist.
		if isNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("blob s3 exists: %w", err)
	}
	return true, nil
}

// List returns the blobs whose key starts with prefix, every blob for
// an empty one. Keys are stored sharded (keyPath), so a prefix of four
// or more characters lists its shard alone; a shorter one lists the
// whole store and filters on the key. Like the local store, it matches
// the key itself, never the shard directories it sits under.
func (s *Store) List(ctx context.Context, prefix string) ([]storage.BlobInfo, error) {
	root := ""
	if s.prefix != "" {
		root = strings.TrimSuffix(s.prefix, "/") + "/"
	}
	listPrefix := root
	if len(prefix) >= 4 {
		listPrefix = s.keyPath(prefix)
	}

	// A page holds at most 1000 keys: follow the continuation token to
	// the end, so a caller (the orphan sweep) sees every blob.
	var items []storage.BlobInfo
	pages := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: &s.bucket,
		Prefix: &listPrefix,
	})
	for pages.HasMorePages() {
		result, err := pages.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("blob s3 list: %w", err)
		}
		for _, obj := range result.Contents {
			key := path.Base(*obj.Key)
			if strings.HasSuffix(key, ".meta.json") || !strings.HasPrefix(key, prefix) {
				continue
			}
			size := int64(0)
			if obj.Size != nil {
				size = *obj.Size
			}
			updatedAt := time.Time{}
			if obj.LastModified != nil {
				updatedAt = *obj.LastModified
			}
			items = append(items, storage.BlobInfo{
				Key:       key,
				Size:      size,
				UpdatedAt: updatedAt,
			})
		}
	}
	return items, nil
}

func (s *Store) URL(ctx context.Context, key string) (string, error) {
	objKey := s.keyPath(key)
	presigner := s3.NewPresignClient(s.client)
	result, err := presigner.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: &s.bucket,
		Key:    &objKey,
	}, s3.WithPresignExpires(s.presignExpiry))
	if err != nil {
		return "", fmt.Errorf("blob s3 url: %w", err)
	}
	return result.URL, nil
}

// isNotFound reports whether err is S3's definitive "no such key": a
// NoSuchKey or NotFound error (HeadObject has no body to name it). Any
// other failure (auth, network, throttling, 5xx) leaves the object's
// existence unknown.
func isNotFound(err error) bool {
	var notFound *types.NotFound
	var noSuchKey *types.NoSuchKey
	return errors.As(err, &notFound) || errors.As(err, &noSuchKey)
}
