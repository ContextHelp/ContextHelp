package bundle_test

import (
	"archive/zip"
	"bytes"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/bundle"
)

// writeSignedZip writes a validly signed bundle whose one file entry
// is name (with mode), so only the entry rules can refuse it.
func writeSignedZip(t *testing.T, name string, mode fs.FileMode) (zipPath, sigPath string) {
	t.Helper()
	kp, err := bundle.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	fh := &zip.FileHeader{Name: name, Method: zip.Deflate}
	fh.SetMode(mode)
	w, err := zw.CreateHeader(fh)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("payload")); err != nil {
		t.Fatal(err)
	}
	mf, _ := json.Marshal(bundle.Manifest{
		SchemaVersion: bundle.SchemaVersion,
		PublicKeyHex:  hex.EncodeToString(kp.Public),
		Files:         []string{name},
	})
	mw, err := zw.Create(bundle.ManifestName)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mw.Write(mf); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	zipPath = filepath.Join(dir, "b.zip")
	sigPath = zipPath + ".sig"
	if err := os.WriteFile(zipPath, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sigPath, bundle.Sign(buf.Bytes(), kp.Private), 0o600); err != nil {
		t.Fatal(err)
	}
	return zipPath, sigPath
}

func TestBundleRefusesUnsafeEntries(t *testing.T) {
	for _, tc := range []struct {
		name  string
		entry string
		mode  fs.FileMode
	}{
		{"dotdot", "../evil", 0o600},
		{"nested dotdot", "keys/../../evil", 0o600},
		{"absolute", "/tmp/evil", 0o600},
		{"backslash", `..\evil`, 0o600},
		{"symlink", "keys", fs.ModeSymlink | 0o777},
		{"device", "dev", fs.ModeDevice | 0o600},
	} {
		t.Run(tc.name, func(t *testing.T) {
			zipPath, sigPath := writeSignedZip(t, tc.entry, tc.mode)
			if _, err := bundle.ExtractOnly(zipPath); err == nil {
				t.Error("ExtractOnly: want error")
			}
			if _, err := bundle.VerifyAndOpen(bundle.VerifyOpts{ZipPath: zipPath, SigPath: sigPath}); err == nil {
				t.Error("VerifyAndOpen: want error")
			}
		})
	}
}

func TestBundleAcceptsNestedEntry(t *testing.T) {
	zipPath, sigPath := writeSignedZip(t, "keys/abcd.pub", 0o600)
	files, err := bundle.ExtractOnly(zipPath)
	if err != nil {
		t.Fatalf("ExtractOnly: %v", err)
	}
	if string(files["keys/abcd.pub"]) != "payload" {
		t.Errorf("files = %v", files)
	}
	if _, err := bundle.VerifyAndOpen(bundle.VerifyOpts{ZipPath: zipPath, SigPath: sigPath}); err != nil {
		t.Errorf("VerifyAndOpen: %v", err)
	}
}
