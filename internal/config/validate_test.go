package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestValidate(t *testing.T) {
	cases := []struct {
		name      string
		mutate    func(*Config)
		wantField string
	}{
		{
			name:      "invalid storage type",
			mutate:    func(c *Config) { c.Storage.Type = "badtype" },
			wantField: "storage.type",
		},
		{
			name:      "port below range",
			mutate:    func(c *Config) { c.Server.Port = 80 },
			wantField: "server.port",
		},
		{
			name:      "grpc port above range",
			mutate:    func(c *Config) { c.Server.GRPCPort = 99999 },
			wantField: "server.grpc_port",
		},
		{
			name:      "poll interval too short",
			mutate:    func(c *Config) { c.Jobs.PollInterval = 10 * time.Millisecond },
			wantField: "jobs.poll_interval",
		},
		{
			name: "default profile not in profiles map",
			mutate: func(c *Config) {
				c.Profile.Default = "missing"
				c.Profile.Profiles = map[string]FocusProfile{"other": {}}
			},
			wantField: "profile.default",
		},
		{
			name: "invalid namespace format",
			mutate: func(c *Config) {
				c.Conventions.AllowedMentionNamespaces = []string{"BadNS"}
			},
			wantField: "conventions.allowed_mention_namespaces[0]",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{}
			tc.mutate(cfg)
			errs := Validate(cfg)
			assert.NotEmpty(t, errs)
			assert.Equal(t, tc.wantField, errs[0].Field)
		})
	}
}
