package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ErrEntitlementRequired is returned when a namespace is restricted and the
// caller lacks a valid entitlement for the registry.
type ErrEntitlementRequired struct {
	RegistryName string
	Namespace    string
	UpgradeURL   string
}

func (e ErrEntitlementRequired) Error() string {
	msg := fmt.Sprintf("entitlement required: namespace %q is restricted on registry %q",
		e.Namespace, e.RegistryName)
	if e.UpgradeURL != "" {
		msg += " — upgrade at: " + e.UpgradeURL
	}
	return msg
}

// entitlementResponse is the JSON shape returned by a registry /entitlements endpoint.
type entitlementResponse struct {
	Plan       string    `json:"plan"`
	Namespaces []string  `json:"namespaces"`
	ExpiresAt  time.Time `json:"expires_at"`
}

// EntitlementChecker fetches and stores registry entitlement records.
type EntitlementChecker struct {
	store  storage.EntitlementStore
	client *http.Client
}

// NewEntitlementChecker creates an EntitlementChecker backed by the given store.
func NewEntitlementChecker(store storage.EntitlementStore) *EntitlementChecker {
	return &EntitlementChecker{
		store:  store,
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

// FetchAndStore calls entitlementURL, parses the response, persists it under
// registryName, and returns the stored record. The caller supplies the auth
// token (may be empty for open registries that still publish entitlements).
func (c *EntitlementChecker) FetchAndStore(
	ctx context.Context,
	registryName string,
	entitlementURL string,
	authToken string,
) (*storage.RegistryEntitlement, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, entitlementURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build entitlement request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if authToken != "" {
		req.Header.Set("Authorization", "Bearer "+authToken)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch entitlements from %s: %w", entitlementURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, ErrEntitlementRequired{
			RegistryName: registryName,
			UpgradeURL:   entitlementURL,
		}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("entitlements endpoint returned HTTP %d", resp.StatusCode)
	}

	var payload entitlementResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode entitlement response: %w", err)
	}

	ent := &storage.RegistryEntitlement{
		RegistryName: registryName,
		Plan:         payload.Plan,
		Namespaces:   payload.Namespaces,
		ExpiresAt:    payload.ExpiresAt,
		FetchedAt:    time.Now().UTC(),
	}

	if err := c.store.Upsert(ctx, ent); err != nil {
		return nil, fmt.Errorf("store entitlement: %w", err)
	}
	return ent, nil
}

// CheckNamespace verifies that registryName's stored entitlement grants access
// to namespace. Returns ErrEntitlementRequired if not covered.
// When no entitlement record exists, access is assumed unrestricted (caller
// should call FetchAndStore first if the registry declares an entitlement URL).
func (c *EntitlementChecker) CheckNamespace(
	ctx context.Context,
	registryName string,
	namespace string,
	upgradeURL string,
) error {
	ent, err := c.store.Get(ctx, registryName)
	if err != nil {
		// Documented fail-open (see the doc comment): with no stored
		// entitlement the registry is treated as unrestricted.
		//
		// Caveat worth fixing separately: EntitlementStore exposes no
		// not-found sentinel — the postgres implementation wraps
		// sql.ErrNoRows as an opaque "scan entitlement: %w" — so a genuine
		// store outage is indistinguishable here from "no record" and also
		// fails open. Narrowing this to a not-found sentinel requires a
		// change to the EntitlementStore contract and its implementations.
		return nil //nolint:nilerr // documented fail-open; store exposes no not-found sentinel to narrow on
	}

	// Expired entitlement — treat as restricted.
	if !ent.ExpiresAt.IsZero() && time.Now().After(ent.ExpiresAt) {
		return ErrEntitlementRequired{
			RegistryName: registryName,
			Namespace:    namespace,
			UpgradeURL:   upgradeURL,
		}
	}

	for _, pattern := range ent.Namespaces {
		if matchNamespace(pattern, namespace) {
			return nil
		}
	}
	return ErrEntitlementRequired{
		RegistryName: registryName,
		Namespace:    namespace,
		UpgradeURL:   upgradeURL,
	}
}

// matchNamespace matches namespace against a glob pattern using path.Match
// semantics (e.g. "ai.*" matches "ai.bert" but NOT "ai").
func matchNamespace(pattern, namespace string) bool {
	// Exact match.
	if pattern == namespace {
		return true
	}
	// Bare wildcard: "*" matches anything.
	if pattern == "*" {
		return true
	}
	// Wildcard suffix: "ai.*" → match any "ai.<something>" (dot required).
	if strings.HasSuffix(pattern, ".*") {
		prefix := strings.TrimSuffix(pattern, ".*")
		if strings.HasPrefix(namespace, prefix+".") {
			return true
		}
		return false
	}
	// Full glob via path.Match as fallback.
	matched, err := path.Match(pattern, namespace)
	return err == nil && matched
}

// IsEntitlementRequired reports whether err is (or wraps) ErrEntitlementRequired.
func IsEntitlementRequired(err error) bool {
	var e ErrEntitlementRequired
	return errors.As(err, &e)
}
