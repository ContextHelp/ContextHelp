package pipeline

import "testing"

func TestParseVersionedName(t *testing.T) {
	cases := []struct {
		in       string
		wantName string
		wantVer  int
		wantOK   bool
	}{
		// Valid versioned.
		{"text.short@v1", "text.short", 1, true},
		{"text.short@v0", "text.short", 0, true},
		{"text.short@v42", "text.short", 42, true},
		{"doc.code@v3", "doc.code", 3, true},
		// Bare → implicit v0.
		{"text.short", "text.short", 0, true},
		{"", "", 0, true},
		{"url.generic", "url.generic", 0, true},
		// Malformed.
		{"text.short@vfoo", "text.short@vfoo", 0, false},
		{"text.short@v", "text.short@v", 0, false},
		{"text.short@1", "text.short@1", 0, false},
		{"text.short@v-1", "text.short@v-1", 0, false},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			gotName, gotVer, gotOK := ParseVersionedName(c.in)
			if gotName != c.wantName || gotVer != c.wantVer || gotOK != c.wantOK {
				t.Errorf("ParseVersionedName(%q) = (%q,%d,%v); want (%q,%d,%v)",
					c.in, gotName, gotVer, gotOK, c.wantName, c.wantVer, c.wantOK)
			}
		})
	}
}

func TestFormatVersionedName(t *testing.T) {
	if got := FormatVersionedName("text.short", 1); got != "text.short@v1" {
		t.Errorf("FormatVersionedName: got %q, want text.short@v1", got)
	}
	if got := FormatVersionedName("doc.pdf", 0); got != "doc.pdf@v0" {
		t.Errorf("FormatVersionedName: got %q, want doc.pdf@v0", got)
	}
}

func TestRegistryGet_BareNameResolvesToV0(t *testing.T) {
	r := NewRegistry()
	p := &Pipeline{PipelineName: "text.short"}
	if err := r.Register("text.short", p); err != nil {
		t.Fatalf("register: %v", err)
	}

	// Bare lookup hits direct entry.
	got, err := r.Get("text.short")
	if err != nil {
		t.Fatalf("Get bare: %v", err)
	}
	if got != p {
		t.Errorf("bare lookup returned different pipeline")
	}

	// "@v0" lookup falls back to the bare registration.
	got, err = r.Get("text.short@v0")
	if err != nil {
		t.Fatalf("Get @v0: %v", err)
	}
	if got != p {
		t.Errorf("@v0 lookup did not resolve to bare-registered pipeline")
	}
}

func TestRegistryGet_VersionedRegistration(t *testing.T) {
	r := NewRegistry()
	v0 := &Pipeline{PipelineName: "text.short@v0"}
	v1 := &Pipeline{PipelineName: "text.short@v1"}
	if err := r.Register("text.short@v0", v0); err != nil {
		t.Fatalf("register v0: %v", err)
	}
	if err := r.Register("text.short@v1", v1); err != nil {
		t.Fatalf("register v1: %v", err)
	}

	got, err := r.Get("text.short@v1")
	if err != nil {
		t.Fatalf("Get @v1: %v", err)
	}
	if got != v1 {
		t.Errorf("Get @v1 returned wrong pipeline")
	}

	// Bare lookup must fall back to "@v0" alias when only versioned entries
	// are registered.
	got, err = r.Get("text.short")
	if err != nil {
		t.Fatalf("Get bare: %v", err)
	}
	if got != v0 {
		t.Errorf("bare Get did not resolve via @v0 alias")
	}
}

func TestRegistryGet_UnknownVersionMisses(t *testing.T) {
	r := NewRegistry()
	if err := r.Register("text.short", &Pipeline{}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, err := r.Get("text.short@v9"); err == nil {
		t.Errorf("Get @v9 should fail when only bare is registered")
	}
	if _, err := r.Get("text.short@vfoo"); err == nil {
		t.Errorf("Get malformed should fail")
	}
}
