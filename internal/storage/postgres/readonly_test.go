package postgres

import (
	"net/url"
	"strings"
	"testing"
)

func TestReadOnlyConnStr(t *testing.T) {
	t.Run("url", func(t *testing.T) {
		got, err := readOnlyConnStr("postgres://localhost:5432/db?sslmode=disable")
		if err != nil {
			t.Fatal(err)
		}
		u, err := url.Parse(got)
		if err != nil {
			t.Fatal(err)
		}
		if u.Query().Get("options") != readOnlyOption || u.Query().Get("sslmode") != "disable" {
			t.Errorf("read-only URL %q; want options %q and sslmode kept", got, readOnlyOption)
		}
	})
	t.Run("keyvalue", func(t *testing.T) {
		got, err := readOnlyConnStr("host=localhost dbname=db sslmode=disable")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(got, " options='"+readOnlyOption+"'") || !strings.HasPrefix(got, "host=localhost dbname=db") {
			t.Errorf("read-only key/value string %q", got)
		}
	})
	for name, cs := range map[string]string{
		"url":      "postgres://u@h/db?options=-c%20search_path%3Dx",
		"keyvalue": "host=h options='-c search_path=x'",
	} {
		t.Run("refuses existing options/"+name, func(t *testing.T) {
			if _, err := readOnlyConnStr(cs); err == nil {
				t.Errorf("readOnlyConnStr(%q) merged an existing options string", cs)
			}
		})
	}
}
