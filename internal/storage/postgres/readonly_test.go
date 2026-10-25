package postgres

import (
	"testing"
)

// The read-only open parses the connection string with the driver's
// own parser (URL or key/value) and adds the read-only session
// parameter, keeping everything the string set, options included.
func TestReadOnlyConfig(t *testing.T) {
	for name, cs := range map[string]string{
		"url":              "postgres://localhost:5432/db?sslmode=disable",
		"keyvalue":         "host=localhost port=5432 dbname=db sslmode=disable",
		"url options":      "postgres://localhost:5432/db?sslmode=disable&options=-c%20search_path%3Dx",
		"keyvalue options": "host=localhost dbname=db sslmode=disable options='-c search_path=x'",
		// A value that only looks like an options key is not one.
		"options lookalike": "host=localhost dbname=db sslmode=disable application_name='options=foo'",
	} {
		t.Run(name, func(t *testing.T) {
			cfg, err := readOnlyConfig(cs)
			if err != nil {
				t.Fatalf("readOnlyConfig(%q): %v", cs, err)
			}
			if cfg.Runtime[readOnlyParam] != "on" {
				t.Errorf("runtime %v; want %s=on", cfg.Runtime, readOnlyParam)
			}
			if cfg.Host != "localhost" || cfg.Database != "db" {
				t.Errorf("parsed host %q db %q; want localhost, db", cfg.Host, cfg.Database)
			}
			if name == "url options" || name == "keyvalue options" {
				if cfg.Options != "-c search_path=x" {
					t.Errorf("options %q; want kept", cfg.Options)
				}
			}
		})
	}
	if _, err := readOnlyConfig("host=localhost dbname='unterminated"); err == nil {
		t.Error("readOnlyConfig accepted a malformed connection string")
	}
}
