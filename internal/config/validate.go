package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// ValidationError is a single field-level validation failure.
type ValidationError struct {
	Field   string
	Message string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// Validate is the full config validator: it checks every section and
// returns one field-level error per problem, never stopping at the
// first. `ctxt config validate` and `ctxt config doctor` both report
// its result. Checks that must hold for any command to run (endpoint
// URLs, UI session bounds, allowed hosts) run at load time instead.
func Validate(c *Config) []ValidationError {
	var errs []ValidationError

	switch c.Storage.Type {
	case "", "sqlite", "postgres":
		// valid
	default:
		errs = append(errs, ValidationError{Field: "storage.type", Message: fmt.Sprintf("unknown storage type %q", c.Storage.Type)})
	}

	if c.Server.Port != 0 && (c.Server.Port < 1024 || c.Server.Port > 65535) {
		errs = append(errs, ValidationError{Field: "server.port", Message: fmt.Sprintf("must be 1024–65535, got %d", c.Server.Port)})
	}

	if c.Server.GRPCPort != 0 && (c.Server.GRPCPort < 1024 || c.Server.GRPCPort > 65535) {
		errs = append(errs, ValidationError{Field: "server.grpc_port", Message: fmt.Sprintf("must be 1024–65535, got %d", c.Server.GRPCPort)})
	}

	if c.Server.CookieBridgePort != 0 && (c.Server.CookieBridgePort < 1024 || c.Server.CookieBridgePort > 65535) {
		errs = append(errs, ValidationError{Field: "server.cookie_bridge_port", Message: fmt.Sprintf("must be 1024–65535, got %d", c.Server.CookieBridgePort)})
	}

	errs = append(errs, validateBlob(c.Storage.Blob)...)
	errs = append(errs, validateAccess(c)...)
	errs = append(errs, validateAuthRoles(c)...)
	errs = append(errs, validateAuthTokens(c)...)
	errs = append(errs, validateQuotas(c)...)
	errs = append(errs, validateDuplicates(c.Duplicates)...)
	errs = append(errs, validateFederations(c.Federations)...)
	errs = append(errs, validateEnvRefs(c)...)
	if err := c.Embeddings.Validate(); err != nil {
		var ve ValidationError
		if errors.As(err, &ve) {
			errs = append(errs, ve)
		}
	}
	errs = append(errs, validateRegistryAuth(c)...)

	if c.Jobs.PollInterval != 0 && c.Jobs.PollInterval < 50*time.Millisecond {
		errs = append(errs, ValidationError{Field: "jobs.poll_interval", Message: fmt.Sprintf("must be ≥50ms, got %s", c.Jobs.PollInterval)})
	}

	if c.Profile.Default != "" {
		if _, ok := c.Profile.Profiles[c.Profile.Default]; !ok {
			errs = append(errs, ValidationError{Field: "profile.default", Message: fmt.Sprintf("profile %q not defined in profiles map", c.Profile.Default)})
		}
	}

	nsRe := regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
	for i, ns := range c.Conventions.AllowedMentionNamespaces {
		if !nsRe.MatchString(ns) {
			errs = append(errs, ValidationError{Field: fmt.Sprintf("conventions.allowed_mention_namespaces[%d]", i), Message: fmt.Sprintf("invalid namespace %q (must match [a-z][a-z0-9_-]*)", ns)})
		}
	}

	switch c.Secrets.Backend {
	case "", "env",
		// kit canonical names (preferred)
		"keyring", "agefile", "onepassword", "ghsecrets",
		// deprecated aliases (still accepted; emit deprecation warning at runtime)
		"keychain", "age-file", "1password", "gh-secrets":
		// valid
	default:
		errs = append(errs, ValidationError{Field: "secrets.backend", Message: fmt.Sprintf("unknown backend %q; must be one of env, keyring, agefile, onepassword, ghsecrets", c.Secrets.Backend)})
	}

	return errs
}

// validateAccess enforces the server.access class rules:
//
//   - the class must be one of private | protected | public (empty = unset,
//     resolved to private by EffectiveAccess);
//   - an explicit access: private combined with server.public / --public is
//     a hard error (the two contradict each other);
//   - a non-private instance must have inbound authentication configured —
//     an exposed listener with zero auth is exactly the failure mode the
//     class exists to delete.
func validateAccess(c *Config) []ValidationError {
	var errs []ValidationError

	switch c.Server.Access {
	case "", AccessPrivate, AccessProtected, AccessPublic:
		// valid
	default:
		errs = append(errs, ValidationError{
			Field:   "server.access",
			Message: fmt.Sprintf("unknown access class %q; must be one of %s, %s, %s", c.Server.Access, AccessPrivate, AccessProtected, AccessPublic),
		})
		return errs // effective-access rules below assume a valid class
	}

	if c.Server.Access == AccessPrivate && c.Server.Public {
		errs = append(errs, ValidationError{
			Field:   "server.public",
			Message: "conflicts with server.access: private — drop server.public / --public, or set access: protected|public",
		})
	}

	if eff := c.Server.EffectiveAccess(); eff != AccessPrivate && !c.Server.Auth.HasInboundAuth() {
		errs = append(errs, ValidationError{
			Field:   "server.access",
			Message: fmt.Sprintf("%s instance requires inbound authentication — set server.auth.provider (e.g. \"static\" with at least one token under server.auth.static.tokens)", eff),
		})
	}

	return errs
}

// validateAuthRoles requires every static token to carry at least one
// known role. Roles are fixed scope bundles; a token without one could
// reach no route, and a misspelled role must not silently lock a device
// out.
func validateAuthRoles(c *Config) []ValidationError {
	var errs []ValidationError
	for i, t := range c.Server.Auth.Static.Tokens {
		field := fmt.Sprintf("server.auth.static.tokens[%d].roles", i)
		if len(t.Roles) == 0 {
			errs = append(errs, ValidationError{
				Field:   field,
				Message: fmt.Sprintf("at least one role is required; must be one of %s", strings.Join(AuthRoles, ", ")),
			})
			continue
		}
		for _, r := range t.Roles {
			if !slices.Contains(AuthRoles, r) {
				errs = append(errs, ValidationError{
					Field:   field,
					Message: fmt.Sprintf("unknown role %q; must be one of %s", r, strings.Join(AuthRoles, ", ")),
				})
			}
		}
	}
	return errs
}

// MinStaticTokenLen is the shortest server.auth.static.tokens entry
// accepted: 32 characters, half of what `openssl rand -hex 32` prints.
const MinStaticTokenLen = 32

// staticTokenPlaceholder marks the tokens docker/dpkms-auth.example.yaml
// ships. They are public, so one left in place is no secret at all.
const staticTokenPlaceholder = "REPLACE_ME"

// CheckStaticToken reports why tok cannot guard a non-private instance:
// it still holds the example placeholder, or it is short enough to
// guess. Empty tokens are left to the caller's own required-field check.
// dpkms serve applies it through the static auth provider; Validate
// reports it per entry.
func CheckStaticToken(tok string) error {
	const generate = "generate one with: openssl rand -hex 32"
	if strings.Contains(strings.ToUpper(tok), staticTokenPlaceholder) {
		return fmt.Errorf("token is the example placeholder (contains %s), which anyone can read; %s", staticTokenPlaceholder, generate)
	}
	if n := len(tok); n < MinStaticTokenLen {
		return fmt.Errorf("token is %d characters, must be at least %d; %s", n, MinStaticTokenLen, generate)
	}
	return nil
}

// validateAuthTokens applies CheckStaticToken to every static token.
func validateAuthTokens(c *Config) []ValidationError {
	var errs []ValidationError
	for i, t := range c.Server.Auth.Static.Tokens {
		if t.Token == "" {
			continue
		}
		if err := CheckStaticToken(t.Token); err != nil {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("server.auth.static.tokens[%d].token", i),
				Message: err.Error(),
			})
		}
	}
	return errs
}

// validateQuotas enforces server.quotas entry rules: a named principal,
// a known metered event type, and coherent thresholds. Event names
// mirror the metering vocabulary (entity_resolve | content_pull |
// taxonomy_sync) without importing the storage layer.
func validateQuotas(c *Config) []ValidationError {
	var errs []ValidationError
	for i, q := range c.Server.Quotas {
		if q.Principal == "" {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("server.quotas[%d].principal", i),
				Message: "principal is required",
			})
		}
		switch q.Event {
		case "entity_resolve", "content_pull", "taxonomy_sync":
			// valid
		default:
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("server.quotas[%d].event", i),
				Message: fmt.Sprintf("unknown event %q; must be one of entity_resolve, content_pull, taxonomy_sync", q.Event),
			})
		}
		if q.Limit < 0 {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("server.quotas[%d].limit", i),
				Message: fmt.Sprintf("must be ≥0 (0 = unlimited), got %d", q.Limit),
			})
		}
		if q.WarnAt < 0 {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("server.quotas[%d].warn_at", i),
				Message: fmt.Sprintf("must be ≥0, got %d", q.WarnAt),
			})
		} else if q.Limit > 0 && q.WarnAt > q.Limit {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("server.quotas[%d].warn_at", i),
				Message: fmt.Sprintf("must not exceed limit %d, got %d", q.Limit, q.WarnAt),
			})
		}
	}
	return errs
}

// headerNameRe matches an RFC 9110 field name (a token).
var headerNameRe = regexp.MustCompile("^[!#$%&'*+.^_`|~0-9A-Za-z-]+$")

// validateRegistryAuth requires every registries[].auth.type to be a
// supported mechanism and header_name to be a valid header field name.
// An unknown type would otherwise surface only when a request fails.
func validateRegistryAuth(c *Config) []ValidationError {
	var errs []ValidationError
	for i, r := range c.Registries {
		if r.Auth.Type != "" && !r.Auth.Type.Known() {
			errs = append(errs, ValidationError{
				Field: fmt.Sprintf("registries[%d].auth.type", i),
				Message: fmt.Sprintf("unknown auth type %q; must be one of %s, %s, %s",
					r.Auth.Type, RegistryAuthBearer, RegistryAuthAPIKey, RegistryAuthOAuth2),
			})
		}
		if r.Auth.HeaderName != "" && !headerNameRe.MatchString(r.Auth.HeaderName) {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("registries[%d].auth.header_name", i),
				Message: fmt.Sprintf("invalid HTTP header name %q", r.Auth.HeaderName),
			})
		}
	}
	return errs
}

// ValidateAccess returns the access-class misconfiguration for c, if
// any, as a single error. dpkms serve calls it (with the --public flag
// folded into Server.Public) so a protected/public instance with no
// credentials refuses to start instead of silently exposing 0.0.0.0
// with zero auth.
func (c *Config) ValidateAccess() error {
	errs := validateAccess(c)
	if len(errs) == 0 {
		return nil
	}
	msgs := make([]string, len(errs))
	for i, e := range errs {
		msgs[i] = e.Error()
	}
	return fmt.Errorf("config: %s", strings.Join(msgs, "; "))
}

// envRefMessage is the error for a config value written as ${VAR}.
const envRefMessage = "config does not expand ${VAR}; set the value literally, use the bound env var, or a second --config secrets file"

// validateEnvRefs rejects every string value written as an env-var
// reference. Neither this loader nor kit's expands one, so "${TOKEN}"
// would be used as the literal token.
func validateEnvRefs(c *Config) []ValidationError {
	var errs []ValidationError
	walkStrings(reflect.ValueOf(c), "", func(path, val string) {
		if isEnvRef(val) {
			errs = append(errs, ValidationError{Field: path, Message: envRefMessage})
		}
	})
	return errs
}

// blobBackends are the storage.blob.backend values the blob factory
// builds; empty selects local.
var blobBackends = []string{"local", "s3", "garage"}

// Validate checks storage.blob: a known backend, and the S3 fields the
// s3 and garage backends cannot start without. The blob factory calls
// it before building the store, so `config validate` and startup agree.
func (b BlobConfig) Validate() error {
	errs := validateBlob(b)
	if len(errs) == 0 {
		return nil
	}
	joined := make([]error, len(errs))
	for i, e := range errs {
		joined[i] = e
	}
	return errors.Join(joined...)
}

func validateBlob(b BlobConfig) []ValidationError {
	backend := b.Backend
	if backend == "" {
		backend = "local"
	}
	if !slices.Contains(blobBackends, backend) {
		return []ValidationError{{
			Field:   "storage.blob.backend",
			Message: fmt.Sprintf("unknown backend %q; must be one of %s", b.Backend, strings.Join(blobBackends, ", ")),
		}}
	}
	var errs []ValidationError
	if backend == "s3" || backend == "garage" {
		if b.S3.Bucket == "" {
			errs = append(errs, ValidationError{Field: "storage.blob.s3.bucket", Message: fmt.Sprintf("required for the %s backend", backend)})
		}
	}
	if backend == "garage" && b.S3.Endpoint == "" {
		errs = append(errs, ValidationError{Field: "storage.blob.s3.endpoint", Message: "required for the garage backend (e.g. http://localhost:3900)"})
	}
	return errs
}

// validateFederations checks federation entries for valid sync modes, required
// intervals, and duplicate names (which would cause ambiguous routing).
// Full topology-level cycle detection (A→B→A across remote configs) is deferred
// to Phase 2 when remote config discovery is available.
func validateFederations(feds []FederationEntry) []ValidationError {
	var errs []ValidationError
	seen := make(map[string]struct{}, len(feds))
	for i, f := range feds {
		field := fmt.Sprintf("federations[%d]", i)
		if f.Name == "" {
			errs = append(errs, ValidationError{Field: field + ".name", Message: "name must not be empty"})
		} else if _, dup := seen[f.Name]; dup {
			errs = append(errs, ValidationError{Field: field + ".name", Message: fmt.Sprintf("duplicate name %q (names must be unique)", f.Name)})
		} else {
			seen[f.Name] = struct{}{}
		}

		switch f.SyncMode {
		case "async", "inline":
			// valid
		case "bidirectional":
			errs = append(errs, ValidationError{Field: field + ".sync_mode", Message: "bidirectional sync mode is not yet implemented (Phase 3)"})
		default:
			errs = append(errs, ValidationError{Field: field + ".sync_mode", Message: fmt.Sprintf("must be \"async\" or \"inline\"; got %q", f.SyncMode)})
		}

		if f.SyncMode == "async" && f.Interval <= 0 {
			errs = append(errs, ValidationError{Field: field + ".interval", Message: "interval is required and must be >0 for async sync_mode"})
		}
	}
	return errs
}

// validateServerEndpoints checks the client-routing endpoints. Runs at load
// (not just on `config validate`): a malformed URL must fail loudly, never
// degrade into a silently unreachable instance.
func (c *Config) validateServerEndpoints() error {
	if c.Server.URL != "" {
		if err := validateEndpointURL(c.Server.URL); err != nil {
			return fmt.Errorf("config: server.url: %w", err)
		}
	}
	seen := make(map[string]int, len(c.Server.URLs))
	for i, ep := range c.Server.URLs {
		if err := validateEndpointURL(ep.URL); err != nil {
			return fmt.Errorf("config: server.urls[%d]: %w", i, err)
		}
		if ep.Name == "" {
			continue
		}
		if err := validateEndpointName(ep.Name); err != nil {
			return fmt.Errorf("config: server.urls[%d]: %w", i, err)
		}
		if j, dup := seen[ep.Name]; dup {
			return fmt.Errorf("config: server.urls[%d]: duplicate name %q (also server.urls[%d])", i, ep.Name, j)
		}
		seen[ep.Name] = i
	}
	return nil
}

// endpointName is the shape of a server.urls entry name: the URI-safe
// slug dpkms instance names use.
var endpointName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// validateEndpointName requires a lowercase slug that is not all digits:
// --instance reads a bare number as a local instance's port.
func validateEndpointName(name string) error {
	if !endpointName.MatchString(name) {
		return fmt.Errorf("invalid name %q: use lowercase letters, digits and hyphens", name)
	}
	if strings.Trim(name, "0123456789") == "" {
		return fmt.Errorf("invalid name %q: an all-digit name reads as a port", name)
	}
	return nil
}

// validateUISession checks the web UI session bounds: no negative
// duration, and an idle timeout no longer than the absolute lifetime
// when both are set.
func (c *Config) validateUISession() error {
	s := c.Server.UI.Session
	if s.IdleTTL < 0 {
		return fmt.Errorf("config: server.ui.session.idle_ttl must not be negative; got %s", s.IdleTTL)
	}
	if s.MaxTTL < 0 {
		return fmt.Errorf("config: server.ui.session.max_ttl must not be negative; got %s", s.MaxTTL)
	}
	if s.IdleTTL > 0 && s.MaxTTL > 0 && s.IdleTTL > s.MaxTTL {
		return fmt.Errorf("config: server.ui.session.idle_ttl (%s) must not exceed max_ttl (%s)", s.IdleTTL, s.MaxTTL)
	}
	return nil
}

// validateAllowedHosts checks every server.allowed_hosts entry parses.
func (c *Config) validateAllowedHosts() error {
	for i, entry := range c.Server.AllowedHosts {
		if _, _, err := ParseAllowedHost(entry); err != nil {
			return fmt.Errorf("config: server.allowed_hosts[%d]: %w", i, err)
		}
	}
	return nil
}

// ParseAllowedHost splits a server.allowed_hosts entry into a lower-cased
// host and an optional port. A bare host name or IP ("dpkms.lan",
// "192.168.1.20", "::1", "[::1]") matches that host on any port;
// host:port ("dpkms.lan:8443", "[::1]:8080") matches only that port.
// Entries are compared with the request's Host header, so schemes,
// paths, wildcards and whitespace are rejected.
func ParseAllowedHost(entry string) (host, port string, err error) {
	if entry == "" {
		return "", "", fmt.Errorf("empty entry (want a host such as dpkms.lan or dpkms.lan:8443)")
	}
	if strings.ContainsAny(entry, " \t\r\n/\\*@?#,") {
		return "", "", fmt.Errorf("invalid entry %q: want a host or host:port, not a URL or pattern", entry)
	}
	switch {
	case strings.HasPrefix(entry, "[") && strings.HasSuffix(entry, "]"):
		host = entry[1 : len(entry)-1]
	case strings.HasPrefix(entry, "["), strings.Count(entry, ":") == 1:
		host, port, err = net.SplitHostPort(entry)
		if err != nil {
			return "", "", fmt.Errorf("invalid entry %q: %w", entry, err)
		}
	default:
		host = entry
	}
	if host == "" {
		return "", "", fmt.Errorf("invalid entry %q: missing host", entry)
	}
	if strings.Contains(host, ":") && net.ParseIP(host) == nil {
		return "", "", fmt.Errorf("invalid entry %q: not an IPv6 address", entry)
	}
	if port != "" {
		n, perr := strconv.Atoi(port)
		if perr != nil || n < 1 || n > 65535 {
			return "", "", fmt.Errorf("invalid entry %q: port must be 1-65535", entry)
		}
	}
	return strings.ToLower(host), port, nil
}

// validateEndpointURL requires an absolute http/https URL with a host — the
// only shape the client bridge can actually dial.
func validateEndpointURL(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return fmt.Errorf("empty URL (each entry needs a base URL like http://127.0.0.1:8080)")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid URL %q: %w", raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("invalid URL %q: scheme must be http or https", raw)
	}
	if u.Host == "" {
		return fmt.Errorf("invalid URL %q: missing host", raw)
	}
	return nil
}

func validateDuplicates(d DuplicatesConfig) []ValidationError {
	var errs []ValidationError
	switch d.Policy {
	case "", "warn", "drop", "keep":
		// valid; empty is the default, warn
	default:
		errs = append(errs, ValidationError{Field: "duplicates.policy", Message: fmt.Sprintf("must be one of \"warn\", \"drop\", \"keep\"; got %q", d.Policy)})
	}
	if d.SimilarityThreshold < 0.0 || d.SimilarityThreshold > 1.0 {
		errs = append(errs, ValidationError{Field: "duplicates.similarity_threshold", Message: fmt.Sprintf("must be 0.0–1.0; got %f", d.SimilarityThreshold)})
	}
	return errs
}
