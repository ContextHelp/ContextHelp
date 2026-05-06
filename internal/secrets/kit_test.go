package secrets

import "testing"

func TestCanonicalBackendName(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		// canonical names pass through
		{"", ""},
		{"env", "env"},
		{"keyring", "keyring"},
		{"agefile", "agefile"},
		{"onepassword", "onepassword"},
		{"ghsecrets", "ghsecrets"},
		// deprecated dpkms aliases map to canonical kit names
		{"keychain", "keyring"},
		{"age-file", "agefile"},
		{"1password", "onepassword"},
		{"gh-secrets", "ghsecrets"},
		// unknown values pass through (caller validates)
		{"bogus", "bogus"},
	}
	for _, c := range cases {
		if got := CanonicalBackendName(c.in); got != c.want {
			t.Errorf("CanonicalBackendName(%q) = %q; want %q", c.in, got, c.want)
		}
	}
}

func TestIsDeprecatedBackendAlias(t *testing.T) {
	deprecated := []string{"keychain", "age-file", "1password", "gh-secrets"}
	for _, d := range deprecated {
		if !IsDeprecatedBackendAlias(d) {
			t.Errorf("expected %q to be a deprecated alias", d)
		}
	}
	canonical := []string{"", "env", "keyring", "agefile", "onepassword", "ghsecrets"}
	for _, c := range canonical {
		if IsDeprecatedBackendAlias(c) {
			t.Errorf("did not expect %q to be a deprecated alias", c)
		}
	}
}
