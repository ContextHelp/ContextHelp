package gmail

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/adapter/email/mxhook"
)

// envResolver is the default CredentialResolver. It reads
// CLIENT_ID / CLIENT_SECRET / REFRESH_TOKEN / ACCESS_TOKEN from the
// environment under a prefix derived from the URI's path component.
//
// URI shape: env:<PREFIX>. Example: env:GMAIL → reads GMAIL_CLIENT_ID,
// GMAIL_CLIENT_SECRET, GMAIL_REFRESH_TOKEN, GMAIL_ACCESS_TOKEN.
//
// Empty access token is allowed (the adopter's IMAP client may
// refresh on Connect using the refresh token). Empty refresh token is
// rejected — the adapter cannot survive token expiry without it.
//
// TODO(phase 3): integrate kit/storage/secret directly so keyring:
// and openbao: schemes work without per-adapter glue. The shape below
// already mirrors kit/storage/secret/env.Store; lift it when the
// secret.Open(secret.Config{...}) surface stabilizes for the gmail
// shape.
type envResolver struct{}

// Resolve implements CredentialResolver. Returns a clear error when
// the prefix is empty or required env vars are missing so misconfig
// surfaces at Start, not at first Fetch.
func (envResolver) Resolve(_ context.Context, ref mxhook.OAuthCredentialsRef) (Credentials, error) {
	s := string(ref)
	if s == "" {
		return Credentials{}, fmt.Errorf("envResolver: empty CredentialsRef (set Config.CredentialsRef to env:<PREFIX>)")
	}
	if !strings.HasPrefix(s, "env:") {
		return Credentials{}, fmt.Errorf("envResolver: only env: scheme supported in Phase 2, got %q", s)
	}
	prefix := strings.TrimPrefix(s, "env:")
	if prefix == "" {
		return Credentials{}, fmt.Errorf("envResolver: empty prefix in %q", s)
	}
	prefix = strings.ToUpper(prefix) + "_"

	creds := Credentials{
		ClientID:     os.Getenv(prefix + "CLIENT_ID"),
		ClientSecret: os.Getenv(prefix + "CLIENT_SECRET"),
		RefreshToken: os.Getenv(prefix + "REFRESH_TOKEN"),
		AccessToken:  os.Getenv(prefix + "ACCESS_TOKEN"),
	}
	if creds.ClientID == "" {
		return Credentials{}, fmt.Errorf("envResolver: missing %sCLIENT_ID", prefix)
	}
	if creds.ClientSecret == "" {
		return Credentials{}, fmt.Errorf("envResolver: missing %sCLIENT_SECRET", prefix)
	}
	if creds.RefreshToken == "" {
		return Credentials{}, fmt.Errorf("envResolver: missing %sREFRESH_TOKEN", prefix)
	}
	return creds, nil
}

// StaticResolver returns a CredentialResolver that always yields the
// same Credentials. Tests use it to bypass env lookup; production
// callers SHOULD use envResolver (or, in Phase 3, a kit/storage/secret-
// backed resolver).
func StaticResolver(c Credentials) CredentialResolver {
	return staticResolver{creds: c}
}

type staticResolver struct{ creds Credentials }

func (r staticResolver) Resolve(_ context.Context, _ mxhook.OAuthCredentialsRef) (Credentials, error) {
	return r.creds, nil
}
