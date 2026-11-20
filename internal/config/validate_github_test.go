package config

import (
	"strings"
	"testing"
	"time"
)

func TestValidateGitHub(t *testing.T) {
	tests := []struct {
		name    string
		cfg     GitHubConfig
		wantErr string // substring of the single expected message; "" = valid
	}{
		{"zero value", GitHubConfig{}, ""},
		{"full", GitHubConfig{APIURL: "https://ghe.example/api/v3", MaxWait: time.Minute,
			Accounts: []GitHubAccount{{Name: "a", TokenSecret: "T", Owners: []string{"x"}}}}, ""},
		{"bad api url", GitHubConfig{APIURL: "ftp://x"}, "github.api_url"},
		{"negative wait", GitHubConfig{MaxWait: -1}, "github.max_wait"},
		{"account no secret", GitHubConfig{Accounts: []GitHubAccount{{Owners: []string{"x"}}}}, "token_secret"},
		{"account no owners", GitHubConfig{Accounts: []GitHubAccount{{TokenSecret: "T"}}}, "owners"},
		{"owner claimed twice", GitHubConfig{Accounts: []GitHubAccount{
			{Name: "a", TokenSecret: "T", Owners: []string{"Acme"}},
			{Name: "b", TokenSecret: "U", Owners: []string{"acme"}}}}, "already served"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := validateGitHub(tt.cfg)
			if tt.wantErr == "" {
				if len(errs) != 0 {
					t.Fatalf("errs = %v", errs)
				}
				return
			}
			if len(errs) != 1 || !strings.Contains(errs[0].Field+errs[0].Message, tt.wantErr) {
				t.Fatalf("errs = %v, want one containing %q", errs, tt.wantErr)
			}
		})
	}
}
