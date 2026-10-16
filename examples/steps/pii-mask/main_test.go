package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMask(t *testing.T) {
	m := masker{cfg: config{EmailMask: "[email]", PhoneMask: "[phone]"}}
	cases := map[string]string{
		"mail jane.doe+x@example.co.uk now":    "mail [email] now",
		"call +1 (415) 555-0123 today":         "call [phone] today",
		"or 020 7946 0958.":                    "or [phone].",
		"meeting on 2026-10-14 at 10:30":       "meeting on 2026-10-14 at 10:30",
		"order 12345 shipped":                  "order 12345 shipped",
		"two: a@b.io and +33 6 12 34 56 78 ok": "two: [email] and [phone] ok",
	}
	for in, want := range cases {
		if got := m.mask(in); got != want {
			t.Errorf("mask(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRunWritesOnlyTextFields(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(cfgPath, []byte(`{"email_mask":"<e>","phone_mask":"<p>"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DPKMS_STEP_CONFIG_PATH", cfgPath)

	in := `{"id":"ko-1","raw_content":"write to bob@example.com or 415-555-0123","source":"cli","summaries":["bob@example.com"]}`
	var out bytes.Buffer
	if code := run(strings.NewReader(in), &out); code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}
	var r map[string]map[string]any
	if err := json.Unmarshal(out.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	obj := r["object"]
	if obj["raw_content"] != "write to <e> or <p>" {
		t.Errorf("raw_content = %v", obj["raw_content"])
	}
	if _, ok := obj["id"]; ok {
		t.Error("output repeats id; it should carry only the fields it rewrites")
	}
	if _, ok := obj["source"]; ok {
		t.Error("output carries source")
	}
}

func TestRunReportsBadInput(t *testing.T) {
	t.Setenv("DPKMS_STEP_CONFIG_PATH", "")
	var out bytes.Buffer
	if code := run(strings.NewReader("not json"), &out); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if !strings.Contains(out.String(), `"code":"BAD_INPUT"`) {
		t.Errorf("output %s", out.String())
	}
}
