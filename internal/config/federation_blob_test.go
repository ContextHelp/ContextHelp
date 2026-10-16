package config

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const federationBlobYAML = `
federations:
  - name: replica
    url: file:///mnt/replica/db.sqlite
    sync_mode: async
    interval: 5m
    blob:
      backend: local
      threshold: 4096
      local:
        path: /mnt/replica-blobs
  - name: plain
    url: /mnt/plain/db.sqlite
    sync_mode: inline
`

// A file target's blob setting loads with storage.blob's shape; an
// entry without one leaves it unset.
func TestFederationBlobLoadsFromYAML(t *testing.T) {
	cfg, err := loadFromYAML(t, federationBlobYAML)
	require.NoError(t, err)
	require.Len(t, cfg.Federations, 2)
	require.NotNil(t, cfg.Federations[0].Blob)
	assert.Equal(t, BlobConfig{Backend: "local", Threshold: 4096, Local: BlobLocalConfig{Path: "/mnt/replica-blobs"}},
		*cfg.Federations[0].Blob)
	assert.Nil(t, cfg.Federations[1].Blob)
}

// Strict decode (`config validate`'s unknown-key warnings) accepts it.
func TestFederationBlobIsAKnownKey(t *testing.T) {
	got, err := UnknownKeys(layerFile(t, t.TempDir(), "ctxt.yaml", federationBlobYAML))
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestValidateFederationBlob(t *testing.T) {
	entry := func(url string, b *BlobConfig) []FederationEntry {
		return []FederationEntry{{Name: "f", URL: url, SyncMode: "inline", Blob: b}}
	}
	cases := []struct {
		name  string
		feds  []FederationEntry
		field string // "" = valid
	}{
		{"unset", entry("/data/db.sqlite", nil), ""},
		{"local path", entry("/data/db.sqlite", &BlobConfig{Local: BlobLocalConfig{Path: "/blobs"}}), ""},
		{"unknown backend", entry("/data/db.sqlite", &BlobConfig{Backend: "stub"}), "federations[0].blob.backend"},
		{"s3 without bucket", entry("file:///data/db.sqlite", &BlobConfig{Backend: "s3"}), "federations[0].blob.s3.bucket"},
		{"http target", entry("https://peer:8080", &BlobConfig{}), "federations[0].blob"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fields := fieldsOf(Validate(&Config{Federations: tc.feds}))
			if tc.field == "" {
				assert.Empty(t, fields)
				return
			}
			assert.Equal(t, []string{tc.field}, fields)
		})
	}
}

func TestFileTargetBlob(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "db.sqlite")
	beside := filepath.Join(dir, BlobDirName)
	assert.Equal(t, BlobConfig{Local: BlobLocalConfig{Path: beside}}, FileTargetBlob(db, nil), "unset")
	assert.Equal(t, BlobConfig{Backend: "local", Threshold: 9, Local: BlobLocalConfig{Path: beside}},
		FileTargetBlob(db, &BlobConfig{Backend: "local", Threshold: 9}), "local without a path")
	custom := &BlobConfig{Local: BlobLocalConfig{Path: "/elsewhere"}}
	assert.Equal(t, *custom, FileTargetBlob(db, custom), "local path")
	s3 := &BlobConfig{Backend: "s3", S3: BlobS3Config{Bucket: "b"}}
	assert.Equal(t, *s3, FileTargetBlob(db, s3), "s3")
}
