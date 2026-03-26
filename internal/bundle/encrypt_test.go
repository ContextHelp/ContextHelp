package bundle_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/bundle"
)

func TestEncryptDecrypt_Roundtrip(t *testing.T) {
	plaintext := []byte("this is a config backup zip payload")
	passphrase := "correct-horse-battery-staple"

	blob, err := bundle.EncryptBundle(plaintext, passphrase)
	if err != nil {
		t.Fatalf("EncryptBundle: %v", err)
	}
	if len(blob) == 0 {
		t.Fatal("EncryptBundle: returned empty blob")
	}

	got, err := bundle.DecryptBundle(blob, passphrase)
	if err != nil {
		t.Fatalf("DecryptBundle: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("DecryptBundle: got %q, want %q", got, plaintext)
	}
}

func TestEncryptBundle_EmptyPassphrase(t *testing.T) {
	_, err := bundle.EncryptBundle([]byte("data"), "")
	if err == nil {
		t.Fatal("expected error for empty passphrase, got nil")
	}
}

func TestDecryptBundle_EmptyPassphrase(t *testing.T) {
	_, err := bundle.DecryptBundle([]byte("some data longer than header size and more bytes here"), "")
	if err == nil {
		t.Fatal("expected error for empty passphrase, got nil")
	}
}

func TestDecryptBundle_WrongPassphrase(t *testing.T) {
	plaintext := []byte("secret config")
	blob, err := bundle.EncryptBundle(plaintext, "right-passphrase")
	if err != nil {
		t.Fatalf("EncryptBundle: %v", err)
	}

	_, err = bundle.DecryptBundle(blob, "wrong-passphrase")
	if err == nil {
		t.Fatal("expected error for wrong passphrase, got nil")
	}
}

func TestIsEncryptedBundle(t *testing.T) {
	t.Run("encrypted", func(t *testing.T) {
		blob, err := bundle.EncryptBundle([]byte("payload"), "pass")
		if err != nil {
			t.Fatal(err)
		}
		if !bundle.IsEncryptedBundle(blob) {
			t.Fatal("IsEncryptedBundle: expected true for encrypted blob")
		}
	})

	t.Run("plain zip", func(t *testing.T) {
		// zip magic bytes: PK\x03\x04
		plainZip := []byte("PK\x03\x04some zip bytes here padded to length")
		if bundle.IsEncryptedBundle(plainZip) {
			t.Fatal("IsEncryptedBundle: expected false for plain zip")
		}
	})

	t.Run("empty", func(t *testing.T) {
		if bundle.IsEncryptedBundle(nil) {
			t.Fatal("IsEncryptedBundle: expected false for nil")
		}
		if bundle.IsEncryptedBundle([]byte{}) {
			t.Fatal("IsEncryptedBundle: expected false for empty")
		}
	})
}

func TestParseEncryptedHeader(t *testing.T) {
	plaintext := []byte("payload bytes")
	passphrase := "my-passphrase"

	blob, err := bundle.EncryptBundle(plaintext, passphrase)
	if err != nil {
		t.Fatalf("EncryptBundle: %v", err)
	}

	h, err := bundle.ParseEncryptedHeader(blob)
	if err != nil {
		t.Fatalf("ParseEncryptedHeader: %v", err)
	}
	if h.Version != bundle.EncryptedMagicVersion {
		t.Fatalf("unexpected version: got 0x%02x", h.Version)
	}
	if h.Time == 0 {
		t.Fatal("argon2 time must not be zero")
	}
	if h.Memory == 0 {
		t.Fatal("argon2 memory must not be zero")
	}
	if h.Threads == 0 {
		t.Fatal("argon2 threads must not be zero")
	}
}

func TestEncryptDecrypt_DifferentSaltEachTime(t *testing.T) {
	plaintext := []byte("same plaintext")
	passphrase := "same-pass"

	blob1, err := bundle.EncryptBundle(plaintext, passphrase)
	if err != nil {
		t.Fatal(err)
	}
	blob2, err := bundle.EncryptBundle(plaintext, passphrase)
	if err != nil {
		t.Fatal(err)
	}

	if bytes.Equal(blob1, blob2) {
		t.Fatal("two encryptions of same plaintext should differ (random salt/nonce)")
	}

	// Both must decrypt correctly.
	p1, err := bundle.DecryptBundle(blob1, passphrase)
	if err != nil {
		t.Fatalf("DecryptBundle blob1: %v", err)
	}
	p2, err := bundle.DecryptBundle(blob2, passphrase)
	if err != nil {
		t.Fatalf("DecryptBundle blob2: %v", err)
	}
	if !bytes.Equal(p1, plaintext) || !bytes.Equal(p2, plaintext) {
		t.Fatal("decrypted blobs do not match original plaintext")
	}
}

func TestEncryptBundleFile_RoundTrip(t *testing.T) {
	plaintext := []byte("PK\x03\x04fake zip content")
	passphrase := "file-level-pass"

	// Write plaintext to a temp file.
	tmp, err := os.CreateTemp(t.TempDir(), "bundle-*.zip")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tmp.Write(plaintext); err != nil {
		t.Fatal(err)
	}
	tmp.Close()
	path := tmp.Name()

	if err := bundle.EncryptBundleFile(path, passphrase); err != nil {
		t.Fatalf("EncryptBundleFile: %v", err)
	}

	// Verify the file is now encrypted.
	blob, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bundle.IsEncryptedBundle(blob) {
		t.Fatal("file should be detected as encrypted after EncryptBundleFile")
	}

	// Decrypt and compare.
	got, err := bundle.DecryptBundle(blob, passphrase)
	if err != nil {
		t.Fatalf("DecryptBundle after EncryptBundleFile: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("decrypted content mismatch: got %q, want %q", got, plaintext)
	}
}

func TestDecryptBundle_TamperedCiphertext(t *testing.T) {
	blob, err := bundle.EncryptBundle([]byte("important data"), "pass")
	if err != nil {
		t.Fatal(err)
	}
	// flip the last byte of ciphertext
	blob[len(blob)-1] ^= 0xFF

	_, err = bundle.DecryptBundle(blob, "pass")
	if err == nil {
		t.Fatal("expected error for tampered ciphertext, got nil")
	}
}
