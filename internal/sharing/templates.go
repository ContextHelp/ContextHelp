package sharing

// Template names.
const (
	TemplateColleagues = "colleagues-network"
	TemplatePartners   = "partners-network"
)

// Templates returns the built-in share policies of ADR-080 §2: the
// contact network for colleagues, without contact details, and for
// partners, with them; neither sees personal notes. `ctxt share policy`
// installs them.
func Templates() []Policy {
	return []Policy{
		{
			Name:           TemplateColleagues,
			Data:           "type==contact;tag=in=(work,client)",
			ExcludeClasses: []string{"personal"},
			Fields: map[string][]string{
				"contact": {"title", "org", "role", "tags", "city"},
				"*":       {"title", "summary", "tags", "created_at"},
			},
			Redact: Redact{Mask: []string{"email", "phone", "address"}, Style: RedactCategory},
			Capabilities: Capabilities{
				Search:   []string{SearchFTS, SearchVector},
				Entities: true,
				MCP:      true,
			},
		},
		{
			Name:           TemplatePartners,
			Data:           "type==contact",
			ExcludeClasses: []string{"personal"},
			Fields: map[string][]string{
				"contact": {"title", "org", "role", "email", "phone", "tags", "city"},
			},
			Capabilities: Capabilities{
				Search:   []string{SearchFTS, SearchVector, SearchGraph},
				Entities: true,
				MCP:      true,
			},
		},
	}
}

// Template returns the built-in policy named name and whether one
// exists.
func Template(name string) (Policy, bool) {
	for _, p := range Templates() {
		if p.Name == name {
			return p, true
		}
	}
	return Policy{}, false
}
