package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliformat"
)

// redactFixture sets every credential the config carries. Each secret
// is unique, at least 16 characters, and ends in a distinct suffix so a
// leak is attributable to its field.
const redactFixture = `storage:
  type: postgres
  path: postgres://dbuser:PG-TEST-PASSWORD-0001@db.internal:5432/ctxt?sslmode=disable
  blob:
    backend: s3
    s3:
      bucket: b
      access_key: S3-ACCESS-KEY-SECRET-0002
      secret_key: S3-SECRET-KEY-SECRET-0003
server:
  access: protected
  token: SERVER-CLIENT-TOKEN-SECRET-0004
  urls:
    - name: home
      url: https://primary.example.net:7700
      token: SERVER-URLS-TOKEN-SECRET-0005
  auth:
    provider: static
    static:
      tokens:
        - token: STATIC-AUTH-TOKEN-0006-abcdefghij
          principal: alice
          roles: [admin]
federation:
  token: FEDERATION-INBOUND-TOKEN-0007
federations:
  - name: peer
    url: https://peer.example.net:8080
    sync_mode: async
    interval: 1h
    token: FEDERATION-PEER-TOKEN-SECRET-0008
audit:
  webhook:
    enabled: true
    url: https://hooks.example.net/services/WEBHOOK-CAPABILITY-0011
security:
  alerts:
    webhook_url: https://hooks.example.net/services/ALERT-CAPABILITY-0012
`

// redactFixtureSecrets are the raw credential values in redactFixture.
var redactFixtureSecrets = []string{
	"PG-TEST-PASSWORD-0001",
	"S3-ACCESS-KEY-SECRET-0002",
	"S3-SECRET-KEY-SECRET-0003",
	"SERVER-CLIENT-TOKEN-SECRET-0004",
	"SERVER-URLS-TOKEN-SECRET-0005",
	"STATIC-AUTH-TOKEN-0006-abcdefghij",
	"FEDERATION-INBOUND-TOKEN-0007",
	"FEDERATION-PEER-TOKEN-SECRET-0008",
	"WEBHOOK-CAPABILITY-0011",
	"ALERT-CAPABILITY-0012",
}

func writeRedactFixture(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(redactFixture), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return p
}

// assertNoSecret fails when s shows any part of a fixture secret other
// than the last 4 characters redaction keeps. Every 4-character window
// of the rest counts, so a partial hint (a prefix) is a leak too.
func assertNoSecret(t *testing.T, label, s string) {
	t.Helper()
	assertNoSecretOf(t, redactFixtureSecrets, label, s)
}

// assertNoSecretOf is assertNoSecret over the given secrets.
func assertNoSecretOf(t *testing.T, secrets []string, label, s string) {
	t.Helper()
	for _, sec := range secrets {
		hidden := sec[:len(sec)-4]
		for i := 0; i+4 <= len(hidden); i++ {
			if strings.Contains(s, hidden[i:i+4]) {
				t.Errorf("%s leaks %q (shows %q):\n%s", label, sec, hidden[i:i+4], s)
				break
			}
		}
	}
}

// config show must never print a credential, in any --format.
func TestConfigShowRedactsSecretsInEveryFormat(t *testing.T) {
	cfgPath := writeRedactFixture(t)
	formats := append([]string{""}, cliformat.Valid()...)
	for _, f := range formats {
		name := f
		if name == "" {
			name = "default"
		}
		t.Run(name, func(t *testing.T) {
			args := []string{"--config", cfgPath}
			if f != "" {
				args = append(args, "--format", f)
			}
			args = append(args, "config", "show")
			out, err := executeCommand(args...)
			if err != nil {
				assertNoSecret(t, "error", err.Error())
			}
			assertNoSecret(t, "output", out)
		})
	}
}

// The structured formats still show that a credential is set: its
// field carries the mask, not an empty value.
func TestConfigShowStructuredKeepsMask(t *testing.T) {
	cfgPath := writeRedactFixture(t)
	for _, f := range []string{"json", "yaml"} {
		t.Run(f, func(t *testing.T) {
			out, err := executeCommand("--config", cfgPath, "--format", f, "config", "show")
			if err != nil {
				t.Fatalf("config show --format %s: %v\n%s", f, err, out)
			}
			// SERVER-CLIENT-TOKEN-SECRET-0004 is >= 16 chars: mask + last 4.
			if !strings.Contains(out, "****0004") {
				t.Errorf("want masked server token ****0004 in output:\n%s", out)
			}
			// Non-secret fields stay intact.
			for _, keep := range []string{"db.internal:5432", "https://peer.example.net:8080", "alice"} {
				if !strings.Contains(out, keep) {
					t.Errorf("non-secret %q missing from output:\n%s", keep, out)
				}
			}
		})
	}
}

// config validate and config doctor report suspected plaintext secrets;
// the report must identify the field without echoing the value.
func TestConfigValidateAndDoctorRedactHints(t *testing.T) {
	cfgPath := writeRedactFixture(t)
	for _, sub := range []string{"validate", "doctor"} {
		t.Run(sub, func(t *testing.T) {
			out, err := executeCommand("--config", cfgPath, "config", sub)
			if err != nil {
				assertNoSecret(t, "error", err.Error())
			}
			assertNoSecret(t, "output", out)
		})
	}
}
