package keys

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultTransitMount   = "transit"
	defaultTransitTimeout = 15 * time.Second
	maxTransitRespBytes   = 4 << 20
)

// TransitConfig configures an OpenBao/Vault Transit engine Provider.
type TransitConfig struct {
	// Addr is the OpenBao/Vault base URL (https required unless loopback).
	Addr string
	// Mount is the transit mount path (default "transit").
	Mount string
	// KeyName is the transit key name (must have derived=true on the server).
	KeyName string
	// Token is the Vault/OpenBao token, supplied from environment only.
	Token string
	// HTTP overrides the HTTP client (redirects are always disabled).
	HTTP *http.Client
}

type transitProvider struct {
	addr    string
	mount   string
	keyName string
	token   string
	hc      *http.Client
}

// NewTransit returns a Provider backed by an OpenBao/Vault-compatible Transit API.
func NewTransit(cfg TransitConfig) (Provider, error) {
	addr, err := validateTransitAddr(cfg.Addr)
	if err != nil {
		return nil, err
	}
	keyName := strings.TrimSpace(cfg.KeyName)
	if !ValidID(keyName) {
		return nil, fail(ErrUnavailable, "invalid transit key name")
	}
	if strings.TrimSpace(cfg.Token) == "" {
		return nil, fail(ErrUnavailable, "transit token is required")
	}
	mount := strings.Trim(strings.TrimSpace(cfg.Mount), "/")
	if mount == "" {
		mount = defaultTransitMount
	}
	var hc http.Client
	if cfg.HTTP != nil {
		hc = *cfg.HTTP
	} else {
		hc.Timeout = defaultTransitTimeout
	}
	hc.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &transitProvider{
		addr:    addr,
		mount:   mount,
		keyName: keyName,
		token:   strings.TrimSpace(cfg.Token),
		hc:      &hc,
	}, nil
}

func validateTransitAddr(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ErrUnconfigured
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", fail(ErrUnavailable, "invalid transit address")
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
	case "http":
		h := u.Hostname()
		ip := net.ParseIP(h)
		if !strings.EqualFold(h, "localhost") && (ip == nil || !ip.IsLoopback()) {
			return "", fail(ErrUnavailable, "transit plain http allowed only on loopback")
		}
	default:
		return "", fail(ErrUnavailable, "unsupported transit scheme")
	}
	return strings.TrimRight(u.String(), "/"), nil
}

func (t *transitProvider) Name() string { return "transit" }

func (t *transitProvider) keyInfo(ctx context.Context) (latestVersion int, err error) {
	status, body, err := t.do(ctx, http.MethodGet, "/v1/"+t.mount+"/keys/"+url.PathEscape(t.keyName), nil)
	if err != nil {
		return 0, err
	}
	if status != http.StatusOK {
		return 0, fail(ErrUnavailable, "transit key lookup HTTP %d", status)
	}
	var resp struct {
		Data struct {
			LatestVersion int  `json:"latest_version"`
			Derived       bool `json:"derived"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return 0, fail(ErrUnavailable, "transit key lookup malformed")
	}
	if !resp.Data.Derived {
		return 0, fail(ErrUnavailable, "transit key must have derived=true for AAD binding")
	}
	if resp.Data.LatestVersion <= 0 {
		return 0, fail(ErrUnavailable, "transit key has no active version")
	}
	return resp.Data.LatestVersion, nil
}

func (t *transitProvider) Current(ctx context.Context) (string, error) {
	ver, err := t.keyInfo(ctx)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s:v%d", t.keyName, ver), nil
}

func (t *transitProvider) Wrap(ctx context.Context, _ string, material, aad []byte) ([]byte, string, error) {
	if _, err := t.keyInfo(ctx); err != nil {
		return nil, "", err
	}
	if len(aad) == 0 {
		return nil, "", fail(ErrUnavailable, "transit wrap requires non-empty aad")
	}
	payload := map[string]string{
		"plaintext": base64.StdEncoding.EncodeToString(material),
		"context":   base64.StdEncoding.EncodeToString(aad),
	}
	status, body, err := t.do(ctx, http.MethodPost, "/v1/"+t.mount+"/encrypt/"+url.PathEscape(t.keyName), payload)
	if err != nil {
		return nil, "", err
	}
	if status != http.StatusOK {
		return nil, "", fail(ErrUnavailable, "transit encrypt HTTP %d", status)
	}
	var resp struct {
		Data struct {
			Ciphertext string `json:"ciphertext"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil || resp.Data.Ciphertext == "" {
		return nil, "", fail(ErrUnavailable, "transit encrypt malformed response")
	}
	ver, err := parseVaultVersion(resp.Data.Ciphertext)
	if err != nil {
		return nil, "", err
	}
	return []byte(resp.Data.Ciphertext), fmt.Sprintf("%s:v%d", t.keyName, ver), nil
}

func (t *transitProvider) Unwrap(ctx context.Context, keyID string, wrapped, aad []byte) ([]byte, error) {
	if _, err := t.keyInfo(ctx); err != nil {
		return nil, err
	}
	if len(aad) == 0 {
		return nil, ErrUnwrap
	}
	ct := string(wrapped)
	if ver, err := parseVaultVersion(ct); err != nil || fmt.Sprintf("%s:v%d", t.keyName, ver) != keyID {
		return nil, ErrUnwrap
	}
	payload := map[string]string{
		"ciphertext": ct,
		"context":    base64.StdEncoding.EncodeToString(aad),
	}
	status, body, err := t.do(ctx, http.MethodPost, "/v1/"+t.mount+"/decrypt/"+url.PathEscape(t.keyName), payload)
	if err != nil {
		return nil, err
	}
	if status == http.StatusBadRequest {
		return nil, ErrUnwrap
	}
	if status != http.StatusOK {
		return nil, fail(ErrUnavailable, "transit decrypt HTTP %d", status)
	}
	var resp struct {
		Data struct {
			Plaintext string `json:"plaintext"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fail(ErrUnavailable, "transit decrypt malformed response")
	}
	pt, err := base64.StdEncoding.DecodeString(resp.Data.Plaintext)
	if err != nil {
		return nil, fail(ErrUnavailable, "transit decrypt invalid base64")
	}
	return pt, nil
}

func (t *transitProvider) Rewrap(ctx context.Context, keyID string, wrapped, aad []byte, _ string) ([]byte, string, error) {
	if _, err := t.keyInfo(ctx); err != nil {
		return nil, "", err
	}
	ct := string(wrapped)
	if ver, err := parseVaultVersion(ct); err != nil || fmt.Sprintf("%s:v%d", t.keyName, ver) != keyID {
		return nil, "", ErrUnwrap
	}
	payload := map[string]string{
		"ciphertext": ct,
		"context":    base64.StdEncoding.EncodeToString(aad),
	}
	status, body, err := t.do(ctx, http.MethodPost, "/v1/"+t.mount+"/rewrap/"+url.PathEscape(t.keyName), payload)
	if err != nil {
		return nil, "", err
	}
	if status == http.StatusBadRequest {
		return nil, "", ErrUnwrap
	}
	if status != http.StatusOK {
		return nil, "", fail(ErrUnavailable, "transit rewrap HTTP %d", status)
	}
	var resp struct {
		Data struct {
			Ciphertext string `json:"ciphertext"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil || resp.Data.Ciphertext == "" {
		return nil, "", fail(ErrUnavailable, "transit rewrap malformed response")
	}
	ver, err := parseVaultVersion(resp.Data.Ciphertext)
	if err != nil {
		return nil, "", err
	}
	return []byte(resp.Data.Ciphertext), fmt.Sprintf("%s:v%d", t.keyName, ver), nil
}

func (t *transitProvider) Health(ctx context.Context) error {
	_, err := t.keyInfo(ctx)
	return err
}

func (t *transitProvider) do(ctx context.Context, method, path string, payload any) (int, []byte, error) {
	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return 0, nil, fail(ErrUnavailable, "marshal transit request")
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, t.addr+path, body)
	if err != nil {
		return 0, nil, fail(ErrUnavailable, "build transit request")
	}
	req.Header.Set("X-Vault-Token", t.token)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := t.hc.Do(req)
	if err != nil {
		return 0, nil, fail(ErrUnavailable, "transit unreachable")
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return 0, nil, fail(ErrUnavailable, "transit redirect refused")
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxTransitRespBytes))
	if err != nil {
		return 0, nil, fail(ErrUnavailable, "read transit response")
	}
	return resp.StatusCode, raw, nil
}

func parseVaultVersion(ct string) (int, error) {
	parts := strings.SplitN(ct, ":", 3)
	if len(parts) != 3 || parts[0] != "vault" || !strings.HasPrefix(parts[1], "v") {
		return 0, errors.New("keys: invalid transit ciphertext prefix")
	}
	v, err := strconv.Atoi(strings.TrimPrefix(parts[1], "v"))
	if err != nil || v <= 0 {
		return 0, errors.New("keys: invalid transit ciphertext version")
	}
	return v, nil
}
