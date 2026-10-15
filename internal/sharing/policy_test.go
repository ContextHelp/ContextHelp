package sharing_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// adrPolicies is the YAML of ADR-080 §2, two documents.
const adrPolicies = `# dpkms share policy, stored in the instance DB, edited with ctxt share policy
name: colleagues-network
version: 3                      # bumped on every edit; caches key on it
data: "type==contact;tag=in=(work,client)"     # RSQL (ADR-050), compiled to SQL
exclude_classes: [personal]                    # objects and parts with these classes are gone
fields:
  contact: [title, org, role, tags, city]      # per-type allowlist of rendered fields
  "*": [title, summary, tags, created_at]      # every other visible type
redact:
  mask: [email, phone, address]                # PII categories masked in visible text
  reveal: []                                   # categories or value patterns shown anyway
  style: category                              # category ("[email]") | partial ("s…@acme.com")
capabilities:
  search: [fts, vector]                        # graph | fts | vector | rsql
  entities: true                               # entities reachable through visible objects
  export: false
  mcp: true
  events: false
---
name: partners-network
version: 1
data: "type==contact"
exclude_classes: [personal]
fields: { contact: [title, org, role, email, phone, tags, city] }
redact: { mask: [], reveal: [] }
capabilities: { search: [fts, vector, graph], entities: true, export: false, mcp: true, events: false }
`

func TestParsePolicies_ADRDocument(t *testing.T) {
	policies, err := sharing.ParsePolicies([]byte(adrPolicies))
	if err != nil {
		t.Fatalf("ParsePolicies: %v", err)
	}
	if len(policies) != 2 || policies[0].Name != "colleagues-network" || policies[1].Name != "partners-network" {
		t.Fatalf("ParsePolicies = %+v, want the two ADR policies", policies)
	}
	c := policies[0]
	if c.Data != "type==contact;tag=in=(work,client)" || c.ExcludeClasses[0] != "personal" {
		t.Errorf("colleagues data/exclusions = %q, %v", c.Data, c.ExcludeClasses)
	}
	if got := c.FieldsFor("contact"); strings.Join(got, ",") != "title,org,role,tags,city" {
		t.Errorf("colleagues contact fields = %v", got)
	}
	if got := c.FieldsFor("note"); strings.Join(got, ",") != "title,summary,tags,created_at" {
		t.Errorf("colleagues * fields = %v", got)
	}
	if c.Redact.Style != sharing.RedactCategory || len(c.Redact.Mask) != 3 {
		t.Errorf("colleagues redact = %+v", c.Redact)
	}
	if !c.Capabilities.Entities || !c.Capabilities.MCP || c.Capabilities.Export || c.Capabilities.Events || len(c.Capabilities.Search) != 2 {
		t.Errorf("colleagues capabilities = %+v", c.Capabilities)
	}
	// The YAML version is informational: the store owns versions.
	if c.Version != 3 {
		t.Errorf("colleagues yaml version = %d, want 3 as written", c.Version)
	}
}

func TestParsePolicies_PartnersDocument(t *testing.T) {
	policies, err := sharing.ParsePolicies([]byte(adrPolicies))
	if err != nil {
		t.Fatalf("ParsePolicies: %v", err)
	}
	p := policies[1]
	if len(p.Redact.Mask) != 0 || len(p.Capabilities.Search) != 3 || p.FieldsFor("note") != nil {
		t.Errorf("partners = %+v", p)
	}
	// ParsePolicy wants exactly one document.
	if _, err := sharing.ParsePolicy([]byte(adrPolicies)); err == nil {
		t.Error("ParsePolicy(two documents) must fail")
	}
	if _, err := sharing.ParsePolicy([]byte("")); err == nil {
		t.Error("ParsePolicy(empty) must fail")
	}
}

func TestTemplatesAreValidAndMatchTheADR(t *testing.T) {
	templates := sharing.Templates()
	if len(templates) != 2 {
		t.Fatalf("Templates = %d policies, want 2", len(templates))
	}
	parsed, err := sharing.ParsePolicies([]byte(adrPolicies))
	if err != nil {
		t.Fatal(err)
	}
	for i, tpl := range templates {
		if err := tpl.Validate(); err != nil {
			t.Errorf("template %s: %v", tpl.Name, err)
		}
		want := parsed[i]
		want.Version = 0
		wantJSON, _ := json.Marshal(want)
		gotJSON, _ := json.Marshal(tpl)
		if string(wantJSON) != string(gotJSON) {
			t.Errorf("template %s = %s, want the ADR document %s", tpl.Name, gotJSON, wantJSON)
		}
	}
	if _, ok := sharing.Template(sharing.TemplatePartners); !ok {
		t.Error("Template(partners-network) missing")
	}
	if _, ok := sharing.Template("nobody"); ok {
		t.Error("Template(nobody) must not exist")
	}
}

// Every refused form of the spec, by name.
func TestPolicyValidate_Refusals(t *testing.T) {
	good := func() sharing.Policy {
		p, _ := sharing.Template(sharing.TemplateColleagues)
		return p
	}
	for _, tc := range []struct {
		name  string
		mut   func(*sharing.Policy)
		wants string
	}{
		{"empty name", func(p *sharing.Policy) { p.Name = "" }, "policy name"},
		{"bad name", func(p *sharing.Policy) { p.Name = "Bad Name" }, "policy name"},
		{"empty data", func(p *sharing.Policy) { p.Data = " " }, "needs a data filter"},
		{"bad RSQL", func(p *sharing.Policy) { p.Data = "type==" }, "data:"},
		{"related refused", func(p *sharing.Policy) { p.Data = "related==person/sarah" }, "reaches across objects"},
		{"mention refused", func(p *sharing.Policy) { p.Data = "type==contact;mention==person/sarah" }, "reaches across objects"},
		{"mention inside an or", func(p *sharing.Policy) { p.Data = "type==contact,mention==person/sarah" }, "reaches across objects"},
		{"similar refused", func(p *sharing.Policy) { p.Data = "similar==sarah" }, "not allowed in a policy"},
		{"unknown field", func(p *sharing.Policy) { p.Data = "topic==ops" }, "not allowed in a policy"},
		{"class with a range operator", func(p *sharing.Policy) { p.Data = "class>personal" }, "unsupported operator"},
		{"bad metadata key", func(p *sharing.Policy) { p.Data = "metadata.a'b==x" }, "data:"},
		{"bad class name", func(p *sharing.Policy) { p.ExcludeClasses = []string{"Personal Notes"} }, "exclude_classes"},
		{"bad field type", func(p *sharing.Policy) { p.Fields = map[string][]string{"Contact": {"title"}} }, "fields: type"},
		{"empty field list", func(p *sharing.Policy) { p.Fields = map[string][]string{"contact": {}} }, "at least one field"},
		{"bad field name", func(p *sharing.Policy) { p.Fields = map[string][]string{"contact": {"E-Mail"}} }, "field \"E-Mail\""},
		{"bad redact style", func(p *sharing.Policy) { p.Redact.Style = "blur" }, "redact.style"},
		{"bad mask category", func(p *sharing.Policy) { p.Redact.Mask = []string{"E-mail"} }, "redact.mask"},
		{"bad reveal category", func(p *sharing.Policy) { p.Redact.Reveal = []string{"-x"} }, "redact.reveal"},
		{"unknown capability", func(p *sharing.Policy) { p.Capabilities.Search = []string{"sql"} }, "capabilities.search"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := good()
			tc.mut(&p)
			err := p.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.wants) {
				t.Fatalf("Validate = %v, want an error naming %q", err, tc.wants)
			}
			if _, err := p.Spec(); err == nil {
				t.Fatal("Spec of an invalid policy must fail")
			}
		})
	}
}

// Several problems are reported together, each named.
func TestPolicyValidate_ReportsEveryProblem(t *testing.T) {
	p := sharing.Policy{Name: "", Data: "topic==x", Redact: sharing.Redact{Style: "blur"}}
	err := p.Validate()
	if err == nil {
		t.Fatal("Validate must fail")
	}
	for _, want := range []string{"policy name", "not allowed in a policy", "redact.style"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Validate = %v, want it to name %q", err, want)
		}
	}
}

// The accepted forms: every policy field, metadata keys, dates, and the
// inert redact block.
func TestPolicyValidate_Accepted(t *testing.T) {
	for _, data := range []string{
		"type==contact",
		"subtype==vcard;source==cardamum",
		"tag=in=(work,client),tag==vip",
		"profile==work;class!=personal",
		"class=out=(personal,financial)",
		"created_at>2026-01-01;updated_at<2027-01-01",
		"metadata.city==Lisbon",
		"metadata.enrichment.structured_metadata.type=in=(person_note,idea)",
		"(type==contact,type==note);profile=in=(work,client)",
	} {
		p := sharing.Policy{Name: "accepted", Data: data, Redact: sharing.Redact{Mask: []string{"email"}, Reveal: []string{"phone"}, Style: sharing.RedactPartial}}
		if err := p.Validate(); err != nil {
			t.Errorf("Validate(%q) = %v, want accepted", data, err)
		}
	}
}

// Unknown YAML keys are refused; the YAML version key is accepted.
func TestParsePolicy_UnknownKey(t *testing.T) {
	_, err := sharing.ParsePolicy([]byte("name: x\ndata: type==contact\nredaction: {}\n"))
	if err == nil || !strings.Contains(err.Error(), "redaction") {
		t.Fatalf("ParsePolicy(unknown key) = %v, want it refused by name", err)
	}
	p, err := sharing.ParsePolicy([]byte("name: x\nversion: 4\ndata: type==contact\n"))
	if err != nil || p.Version != 4 {
		t.Fatalf("ParsePolicy(version) = %+v, %v", p, err)
	}
}

// Spec and PolicyFromSpec round-trip through the stored JSON; the row's
// name and version win; unknown JSON keys and invalid specs are refused.
func TestPolicySpecRoundTrip(t *testing.T) {
	tpl, _ := sharing.Template(sharing.TemplateColleagues)
	spec, err := tpl.Spec()
	if err != nil {
		t.Fatalf("Spec: %v", err)
	}
	if strings.Contains(string(spec), `"version"`) {
		t.Errorf("Spec carries a version: %s", spec)
	}
	if !strings.Contains(string(spec), `"mask":["email","phone","address"]`) {
		t.Errorf("Spec dropped the inert redact block: %s", spec)
	}
	row := &storage.SharePolicy{ID: "pol_x", Name: "renamed", Version: 7, Spec: spec}
	got, err := sharing.PolicyFromSpec(row)
	if err != nil {
		t.Fatalf("PolicyFromSpec: %v", err)
	}
	if got.Name != "renamed" || got.Version != 7 || got.Data != tpl.Data {
		t.Errorf("PolicyFromSpec = %+v, want the row's name and version over the spec's", got)
	}
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(sharing.Policy{Name: "renamed", Data: tpl.Data, ExcludeClasses: tpl.ExcludeClasses, Fields: tpl.Fields, Redact: tpl.Redact, Capabilities: tpl.Capabilities})
	if string(gotJSON) != string(wantJSON) {
		t.Errorf("round trip = %s, want %s", gotJSON, wantJSON)
	}
	if _, err := sharing.PolicyFromSpec(&storage.SharePolicy{Name: "x", Spec: json.RawMessage(`{"data":"type==contact","bogus":1}`)}); err == nil {
		t.Error("PolicyFromSpec(unknown key) must fail")
	}
	if _, err := sharing.PolicyFromSpec(&storage.SharePolicy{Name: "x", Spec: json.RawMessage(`{"data":"related==x"}`)}); err == nil {
		t.Error("PolicyFromSpec(invalid data) must fail")
	}
	if _, err := sharing.PolicyFromSpec(nil); err == nil {
		t.Error("PolicyFromSpec(nil) must fail")
	}
}
