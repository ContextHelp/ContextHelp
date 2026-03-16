package secrets

import (
	"fmt"
	"io"
	"os"
	"strings"

	"filippo.io/age"
	"gopkg.in/yaml.v3"
)

// AgeFileResolver reads secrets from an age-encrypted YAML file.
// The decrypted YAML must be a flat map[string]string.
type AgeFileResolver struct {
	agefile      string
	identityFile string
}

// NewAgeFileResolver creates an AgeFileResolver.
func NewAgeFileResolver(agefile, identityFile string) *AgeFileResolver {
	return &AgeFileResolver{agefile: agefile, identityFile: identityFile}
}

func (r *AgeFileResolver) decryptAll() (map[string]string, error) {
	identBytes, err := os.ReadFile(r.identityFile)
	if err != nil {
		return nil, fmt.Errorf("secrets: age identity: %w", err)
	}

	identities, err := age.ParseIdentities(strings.NewReader(string(identBytes)))
	if err != nil {
		return nil, fmt.Errorf("secrets: parse age identity: %w", err)
	}

	f, err := os.Open(r.agefile)
	if err != nil {
		return nil, fmt.Errorf("secrets: open age file: %w", err)
	}
	defer f.Close()

	dec, err := age.Decrypt(f, identities...)
	if err != nil {
		return nil, fmt.Errorf("secrets: decrypt: %w", err)
	}

	plain, err := io.ReadAll(dec)
	if err != nil {
		return nil, fmt.Errorf("secrets: read decrypted: %w", err)
	}

	var kv map[string]string
	if err := yaml.Unmarshal(plain, &kv); err != nil {
		return nil, fmt.Errorf("secrets: parse yaml: %w", err)
	}
	return kv, nil
}

func (r *AgeFileResolver) Get(key string) (string, error) {
	kv, err := r.decryptAll()
	if err != nil {
		return "", err
	}
	v, ok := kv[key]
	if !ok {
		return "", ErrNotFound{Key: key}
	}
	return v, nil
}

func (r *AgeFileResolver) Set(key, value string) error {
	return fmt.Errorf("secrets: AgeFileResolver.Set() not implemented; edit the age file manually")
}

// Keys decrypts the age file and returns the names of all stored secrets.
// Implements Lister.
func (r *AgeFileResolver) Keys() ([]string, error) {
	kv, err := r.decryptAll()
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(kv))
	for k := range kv {
		keys = append(keys, k)
	}
	return keys, nil
}
