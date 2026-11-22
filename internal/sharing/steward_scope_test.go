package sharing_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// A frozen scope is a list of policy specs plus include_classes,
// decoded strictly: a misspelled key is refused rather than ignored (it
// would otherwise leave a limit unset in a frozen, approved grant). A
// spec may carry the redact as_audience froze from a policy that masks,
// validated as a policy's.
func TestParseStewardScope(t *testing.T) {
	good := `{"policies":[{"data":"type==contact","exclude_classes":["financial"],"fields":{"contact":["title"]},"capabilities":{"search":["fts"],"entities":true}},{"data":"tag==work"}],"include_classes":["personal"]}`
	s, err := sharing.ParseStewardScope(json.RawMessage(good))
	require.NoError(t, err)
	require.Len(t, s.Specs, 2)
	assert.Equal(t, "type==contact", s.Specs[0].Data)
	assert.Equal(t, []string{"financial"}, s.Specs[0].ExcludeClasses)
	assert.True(t, s.Specs[0].Capabilities.Entities)
	assert.Equal(t, "tag==work", s.Specs[1].Data)
	assert.Equal(t, []string{"personal"}, s.IncludeClasses)
	assert.False(t, s.Redacts())

	spec := func(body string) string { return `{"policies":[` + body + `]}` }
	masked, err := sharing.ParseStewardScope(json.RawMessage(spec(`{"data":"type==contact"},{"data":"tag==work","redact":{"mask":["email","phone"],"style":"category"}}`)))
	require.NoError(t, err, "the stored form carries the redact as_audience froze")
	assert.Empty(t, masked.Specs[0].Redact)
	assert.Equal(t, sharing.Redact{Mask: []string{"email", "phone"}, Style: sharing.RedactCategory}, masked.Specs[1].Redact)
	assert.True(t, masked.Redacts())

	for name, raw := range map[string]string{
		"unknown key":                 spec(`{"data":"type==contact","exclude_clases":["financial"]}`),
		"unknown top-level key":       `{"policies":[{"data":"type==contact"}],"extra":1}`,
		"a name":                      spec(`{"name":"x","data":"type==contact"}`),
		"no data":                     spec(`{"fields":{"contact":["title"]}}`),
		"no data, second spec":        spec(`{"data":"type==contact"},{"fields":{"contact":["title"]}}`),
		"unparseable data":            spec(`{"data":"type=="}`),
		"a field policies refuse":     spec(`{"data":"mention==x"}`),
		"bad exclude class":           spec(`{"data":"type==contact","exclude_classes":["Not A Class"]}`),
		"bad include class":           `{"policies":[{"data":"type==contact"}],"include_classes":["Not A Class"]}`,
		"class both ways":             `{"policies":[{"data":"type==contact","exclude_classes":["personal"]}],"include_classes":["personal"]}`,
		"class both ways, later spec": `{"policies":[{"data":"type==contact"},{"data":"tag==work","exclude_classes":["personal"]}],"include_classes":["personal"]}`,
		"empty field list":            spec(`{"data":"type==contact","fields":{"contact":[]}}`),
		"unknown search mode":         spec(`{"data":"type==contact","capabilities":{"search":["telepathy"]}}`),
		"wildcard beside a field":     spec(`{"data":"type==contact","fields":{"contact":["*","title"]}}`),
		"unknown redact key":          spec(`{"data":"type==contact","redact":{"masks":["email"]}}`),
		"bad redact style":            spec(`{"data":"type==contact","redact":{"style":"blur"}}`),
		"bad mask category":           spec(`{"data":"type==contact"},{"data":"tag==work","redact":{"mask":["E-mail"]}}`),
		"redact not an object":        spec(`{"data":"type==contact","redact":["email"]}`),
		"no spec":                     `{"policies":[]}`,
		"no policies key":             `{}`,
		"policies not a list":         `{"policies":{"data":"type==contact"}}`,
		"the request form":            `{"data":"type==contact"}`,
		"not an object":               `["type==contact"]`,
		"trailing data":               spec(`{"data":"type==contact"}`) + ` {}`,
		"empty":                       ``,
		"null":                        `null`,
		"include_classes not list":    `{"policies":[{"data":"type==contact"}],"include_classes":"personal"}`,
	} {
		_, err := sharing.ParseStewardScope(json.RawMessage(raw))
		assert.Error(t, err, name)
	}
}

// An explicit scope is filed as one spec with include_classes beside
// its keys, and becomes a scope of one spec; it is decoded and validated
// as strictly as the stored form.
func TestParseStewardRequestScope(t *testing.T) {
	s, err := sharing.ParseStewardRequestScope(json.RawMessage(`{"data":"type==contact","exclude_classes":["financial"],"fields":{"contact":["title"]},"capabilities":{"entities":true},"include_classes":["personal"]}`))
	require.NoError(t, err)
	require.Len(t, s.Specs, 1, "an explicit scope is a list of one")
	assert.Equal(t, "type==contact", s.Specs[0].Data)
	assert.Equal(t, []string{"financial"}, s.Specs[0].ExcludeClasses)
	assert.True(t, s.Specs[0].Capabilities.Entities)
	assert.Equal(t, []string{"personal"}, s.IncludeClasses)

	raw, err := s.JSON()
	require.NoError(t, err)
	assert.JSONEq(t, `{"policies":[{"data":"type==contact","exclude_classes":["financial"],"fields":{"contact":["title"]},"capabilities":{"entities":true}}],"include_classes":["personal"]}`, string(raw))
	back, err := sharing.ParseStewardScope(raw)
	require.NoError(t, err)
	assert.Equal(t, s, back, "the stored form reads back as the scope")

	for name, raw := range map[string]string{
		"unknown key":      `{"data":"type==contact","exclude_clases":["x"]}`,
		"no data":          `{"fields":{"contact":["title"]}}`,
		"redact":           `{"data":"type==contact","redact":{"mask":["email"]}}`,
		"class both ways":  `{"data":"type==contact","exclude_classes":["personal"],"include_classes":["personal"]}`,
		"the stored form":  `{"policies":[{"data":"type==contact"}]}`,
		"not an object":    `["type==contact"]`,
		"trailing data":    `{"data":"type==contact"} {}`,
		"empty":            ``,
		"null":             `null`,
		"unparseable data": `{"data":"type=="}`,
	} {
		_, err := sharing.ParseStewardRequestScope(json.RawMessage(raw))
		assert.Error(t, err, name)
	}

	// An explicit scope never carries a redact, whatever its value: an
	// approver must not read as masking what is not applied.
	for _, redact := range []string{`{"mask":["email"]}`, `{}`, `null`} {
		_, err := sharing.ParseStewardRequestScope(json.RawMessage(`{"data":"type==contact","redact":` + redact + `}`))
		assert.ErrorContains(t, err, "masking is not available", redact)
	}
}

// The digest is over the canonical form: key order, whitespace and
// HTML escaping do not change it; any change of content does.
func TestStewardScopeDigest(t *testing.T) {
	a, err := sharing.StewardScopeDigest(json.RawMessage(`{"data":"type==contact","fields":{"contact":["title","org"]}}`))
	require.NoError(t, err)
	assert.Regexp(t, `^v1:sha256:[0-9a-f]{64}$`, a)
	b, err := sharing.StewardScopeDigest(json.RawMessage("{ \"fields\" : {\"contact\":[\"title\",\"org\"]},\n \"data\":\"type==contact\" }"))
	require.NoError(t, err)
	assert.Equal(t, a, b, "key order and whitespace do not matter")

	for name, raw := range map[string]string{
		"a field added":   `{"data":"type==contact","fields":{"contact":["title","org","email"]}}`,
		"a list reorder":  `{"data":"type==contact","fields":{"contact":["org","title"]}}`,
		"a wider filter":  `{"data":"type!=__none__","fields":{"contact":["title","org"]}}`,
		"a key added":     `{"data":"type==contact","fields":{"contact":["title","org"]},"include_classes":[]}`,
		"a number moved":  `{"data":"type==contact","fields":{"contact":["title","org"]},"n":1.0}`,
		"a number moved2": `{"data":"type==contact","fields":{"contact":["title","org"]},"n":1}`,
	} {
		d, err := sharing.StewardScopeDigest(json.RawMessage(raw))
		require.NoError(t, err, name)
		assert.NotEqual(t, a, d, name)
	}
	for _, bad := range []string{``, `{`, `{} {}`} {
		_, err := sharing.StewardScopeDigest(json.RawMessage(bad))
		assert.Error(t, err, bad)
	}

	// The specs of a scope are a list: their order is content.
	ab, err := sharing.StewardScopeDigest(json.RawMessage(`{"policies":[{"data":"type==contact"},{"data":"tag==work"}]}`))
	require.NoError(t, err)
	ba, err := sharing.StewardScopeDigest(json.RawMessage(`{"policies":[{"data":"tag==work"},{"data":"type==contact"}]}`))
	require.NoError(t, err)
	assert.NotEqual(t, ab, ba)
}

// Check is the filing-time test of a scope against the instance
// settings: no spec opens mcp or events, a spec opens export only where
// steward.allow_export is on, and include_classes names a withheld
// class only where steward.allow_withheld is on.
func TestStewardScopeCheck(t *testing.T) {
	base := func() sharing.StewardScope {
		return sharing.StewardScope{Specs: []sharing.StewardSpec{{Data: "type==contact"}, {Data: "tag==work"}}}
	}
	withheld := sharing.StewardSettings{Enabled: true, WithheldClasses: []string{"personal"}}
	open := sharing.StewardSettings{Enabled: true, WithheldClasses: []string{"personal"}, AllowWithheld: true, AllowExport: true}

	require.NoError(t, base().Check(withheld))

	for _, i := range []int{0, 1} {
		mcp := base()
		mcp.Specs[i].Capabilities.MCP = true
		assert.Error(t, mcp.Check(open), "mcp is never opened, by any spec (%d)", i)
		events := base()
		events.Specs[i].Capabilities.Events = true
		assert.Error(t, events.Check(open), "events is never opened, by any spec (%d)", i)
		export := base()
		export.Specs[i].Capabilities.Export = true
		assert.Error(t, export.Check(withheld), "export needs steward.allow_export (%d)", i)
		assert.NoError(t, export.Check(open))
	}

	include := base()
	include.IncludeClasses = []string{"personal"}
	assert.Error(t, include.Check(withheld), "include_classes needs steward.allow_withheld")
	assert.NoError(t, include.Check(open))
	notWithheld := base()
	notWithheld.IncludeClasses = []string{"financial"}
	assert.Error(t, notWithheld.Check(open), "include_classes names a class that is not withheld")

	assert.Error(t, base().Check(sharing.StewardSettings{}), "steward.enabled is off")
}

// The settings apply to every spec, as to an explicit scope's one: the
// withheld classes join each spec's exclusions (unless the instance
// allows the grant to include them), and mcp, events and export without
// steward.allow_export are off in each.
func TestStewardScopePoliciesApplyTheSettingsToEverySpec(t *testing.T) {
	loud := sharing.Capabilities{Entities: true, Export: true, MCP: true, Events: true}
	scope := sharing.StewardScope{Specs: []sharing.StewardSpec{
		{Data: "type==contact", ExcludeClasses: []string{"financial"}, Capabilities: loud},
		{Data: "tag==work", Capabilities: loud},
	}}
	withheld := sharing.StewardSettings{Enabled: true, WithheldClasses: []string{"personal"}}
	ps := scope.Policies("stg_x", withheld)
	require.Len(t, ps, 2)
	assert.Equal(t, []string{"financial", "personal"}, ps[0].ExcludeClasses)
	assert.Equal(t, []string{"personal"}, ps[1].ExcludeClasses)
	for i, p := range ps {
		assert.Equal(t, 1, p.Version, "spec %d is frozen at version 1", i)
		assert.True(t, p.Capabilities.Entities, "spec %d", i)
		assert.False(t, p.Capabilities.MCP || p.Capabilities.Events || p.Capabilities.Export, "spec %d: forced off", i)
	}

	scope.IncludeClasses = []string{"personal"}
	open := sharing.StewardSettings{Enabled: true, WithheldClasses: []string{"personal"}, AllowWithheld: true, AllowExport: true}
	ps = scope.Policies("stg_x", open)
	assert.Equal(t, []string{"financial"}, ps[0].ExcludeClasses)
	assert.Empty(t, ps[1].ExcludeClasses, "include_classes lifts the withheld class from every spec")
	for i, p := range ps {
		assert.True(t, p.Capabilities.Export, "spec %d: allow_export keeps a scope's export", i)
		assert.False(t, p.Capabilities.MCP || p.Capabilities.Events, "spec %d", i)
	}
}

// FreezeAudienceScope freezes each policy currently granted to an
// audience as a spec of its own: the rows the scope selects are the
// rows the audience's members see, never more, and the capabilities a
// steward scope never opens are dropped and reported.
func TestFreezeAudienceScopeSelectsWhatTheAudienceSees(t *testing.T) {
	drv := storageutil.NewTestDriver(t)
	seedCorpus(t, drv)
	col, par := colleagues(), partners()
	set := sharing.StewardSettings{Enabled: true}
	for name, c := range map[string]struct {
		policies []sharing.Policy
		want     []string
	}{
		"one policy":       {[]sharing.Policy{col}, want("c1", "c4")},
		"another":          {[]sharing.Policy{par}, want("c1", "c2", "c4")},
		"two, in a union":  {[]sharing.Policy{col, par}, want("c1", "c2", "c4")},
		"two, other order": {[]sharing.Policy{par, col}, want("c1", "c2", "c4")},
	} {
		scope, dropped, err := sharing.FreezeAudienceScope(c.policies, set)
		require.NoError(t, err, name)
		assert.Len(t, scope.Specs, len(c.policies), name+": one spec per policy")
		assert.Equal(t, []string{"mcp"}, dropped, name+": both templates open mcp, which a steward scope does not")
		raw, err := scope.JSON()
		require.NoError(t, err, name)
		_, err = sharing.ParseStewardScope(raw)
		require.NoError(t, err, name+": the frozen scope parses as a scope")
		require.NoError(t, scope.Check(set), name+": the frozen scope passes the filing check")

		v := elevation(t, scope, set)
		assert.Equal(t, c.want, visible(t, drv, v), name)
		assert.Equal(t, visible(t, drv, memberViewer(t, "p", c.policies)), visible(t, drv, v), name+": what a member sees")
	}
	_, _, err := sharing.FreezeAudienceScope(nil, set)
	assert.Error(t, err, "an audience that grants nothing freezes to a scope that opens nothing: refused")
}

// elevation compiles scope as the resolver does for steward grant
// stg_x: one grant per spec.
func elevation(t *testing.T, scope sharing.StewardScope, set sharing.StewardSettings) sharing.Viewer {
	t.Helper()
	in := sharing.Input{PrincipalID: "p", StewardGrant: "stg_x"}
	for _, p := range scope.Policies("stg_x", set) {
		in.Grants = append(in.Grants, sharing.Grant{ID: "stg_x", Audience: "steward:stg_x", Policy: p})
	}
	v, err := sharing.Compile(in)
	require.NoError(t, err)
	return v
}

// The capabilities a steward scope never opens are dropped from every
// spec and reported once each, sorted; export stays where the instance
// allows it.
func TestFreezeAudienceScopeDropsWhatAStewardNeverOpens(t *testing.T) {
	quiet := sharing.Policy{Name: "a-quiet", Data: "type==note"}
	loud := sharing.Policy{Name: "b-loud", Data: "type==contact", Capabilities: sharing.Capabilities{Entities: true, Export: true, MCP: true, Events: true}}
	scope, dropped, err := sharing.FreezeAudienceScope([]sharing.Policy{loud, quiet}, sharing.StewardSettings{Enabled: true})
	require.NoError(t, err)
	assert.Equal(t, []string{"events", "export", "mcp"}, dropped)
	for _, sp := range scope.Specs {
		assert.False(t, sp.Capabilities.MCP || sp.Capabilities.Events || sp.Capabilities.Export, sp.Data)
	}
	assert.True(t, scope.Specs[1].Capabilities.Entities, "what a steward may open is kept")

	scope, dropped, err = sharing.FreezeAudienceScope([]sharing.Policy{loud, quiet}, sharing.StewardSettings{Enabled: true, AllowExport: true})
	require.NoError(t, err)
	assert.Equal(t, []string{"events", "mcp"}, dropped)
	assert.True(t, scope.Specs[1].Capabilities.Export, "export stays where steward.allow_export is on")

	_, dropped, err = sharing.FreezeAudienceScope([]sharing.Policy{quiet}, sharing.StewardSettings{Enabled: true})
	require.NoError(t, err)
	assert.Empty(t, dropped)
}

// A policy that redacts freezes with its redact unchanged, as its
// members have it: masking is inert for members and steward alike, and
// once it lands it applies to the steward as to the members. The scope
// says it redacts, so the approvers can be told the masking is asked
// for but not applied.
func TestFreezeAudienceScopeKeepsEachPolicysRedact(t *testing.T) {
	col, par := colleagues(), partners()
	set := sharing.StewardSettings{Enabled: true}
	scope, _, err := sharing.FreezeAudienceScope([]sharing.Policy{par, col}, set)
	require.NoError(t, err, "the colleagues template masks contact details, and freezes")
	require.Len(t, scope.Specs, 2)
	assert.Equal(t, sharing.Redact{Mask: []string{"email", "phone", "address"}, Style: sharing.RedactCategory}, scope.Specs[0].Redact, "colleagues-network keeps its redact unchanged")
	assert.Empty(t, scope.Specs[1].Redact, "partners-network masks nothing")
	assert.True(t, scope.Redacts())

	raw, err := scope.JSON()
	require.NoError(t, err)
	back, err := sharing.ParseStewardScope(raw)
	require.NoError(t, err)
	assert.Equal(t, scope, back, "the stored form keeps the redact")
	ps := back.Policies("stg_x", set)
	assert.Equal(t, col.Redact, ps[0].Redact, "the redact reaches the policy the grant compiles from")
	assert.Empty(t, ps[1].Redact)

	plain, _, err := sharing.FreezeAudienceScope([]sharing.Policy{par}, set)
	require.NoError(t, err)
	assert.False(t, plain.Redacts())
	plainRaw, err := plain.JSON()
	require.NoError(t, err)
	assert.NotContains(t, string(plainRaw), "redact", "a spec that masks nothing stores no redact")
}

// The exclusions of every policy stay with that policy's spec: a row
// one policy excludes is not shown through it, while another policy
// covering it still shows it with its own fields, as it does to the
// audience's members. Each policy here excludes a class, so a freeze
// that kept only one policy's exclusions shows a row (or a field) the
// members do not see.
func TestFreezeAudienceScopeKeepsEveryPolicysExclusions(t *testing.T) {
	drv := storageutil.NewTestDriver(t)
	seedCorpus(t, drv)
	// p1 shows contact titles, not personal ones (c3); p2 shows every
	// field of work-tagged objects, not financial ones (c4).
	p1 := sharing.Policy{Name: "p1", Version: 1, Data: "type==contact", ExcludeClasses: []string{"personal"}, Fields: map[string][]string{"contact": {sharing.FieldTitle}}}
	p2 := sharing.Policy{Name: "p2", Version: 1, Data: "tag==work", ExcludeClasses: []string{"financial"}, Fields: map[string][]string{"*": {sharing.FieldAll}}}
	set := sharing.StewardSettings{Enabled: true}
	scope, _, err := sharing.FreezeAudienceScope([]sharing.Policy{p2, p1}, set)
	require.NoError(t, err)
	require.Len(t, scope.Specs, 2)
	assert.Equal(t, []string{"personal"}, scope.Specs[0].ExcludeClasses, "p1's exclusions stay with p1")
	assert.Equal(t, []string{"financial"}, scope.Specs[1].ExcludeClasses, "p2's exclusions stay with p2, the last")

	steward, members := elevation(t, scope, set), memberViewer(t, "p", []sharing.Policy{p1, p2})
	assert.Equal(t, want("c1", "c2", "c4", "n1"), visible(t, drv, members))
	assert.Equal(t, visible(t, drv, members), visible(t, drv, steward), "the rows the members see")
	for _, name := range []string{"c1", "c2", "c3", "c4", "n1"} {
		id := "obj-" + name
		assert.Equal(t, rendered(t, drv, members, id), rendered(t, drv, steward, id), "%s renders as it does for a member", name)
	}
}

// The shorthand's inputs fold deterministically: policy order and
// repeats do not change the frozen scope, so equal audiences freeze to
// equal digests.
func TestFreezeAudienceScopeIsDeterministic(t *testing.T) {
	col, par := colleagues(), partners()
	a, _, err := sharing.FreezeAudienceScope([]sharing.Policy{col, par}, sharing.StewardSettings{})
	require.NoError(t, err)
	b, _, err := sharing.FreezeAudienceScope([]sharing.Policy{par, col, par}, sharing.StewardSettings{})
	require.NoError(t, err)
	ja, _ := a.JSON()
	jb, _ := b.JSON()
	assert.JSONEq(t, string(ja), string(jb))
	da, _ := sharing.StewardScopeDigest(ja)
	db, _ := sharing.StewardScopeDigest(jb)
	assert.Equal(t, da, db)
}

// An elevation's policy key names its steward grant, so an ordinary
// viewer and an elevated one never share a compiled-viewer cache entry,
// even over grants that are otherwise the same.
func TestElevationPolicyKeyNamesTheStewardGrant(t *testing.T) {
	g := sharing.Grant{ID: "stg_1", Audience: "steward:stg_1", Policy: sharing.Policy{Name: "stg_1", Version: 1, Data: "tag==family"}}
	ordinary := sharing.Input{PrincipalID: "p", Grants: []sharing.Grant{g}}
	elevated := ordinary
	elevated.StewardGrant = "stg_1"
	assert.NotEqual(t, ordinary.Key(), elevated.Key())
	assert.Contains(t, elevated.Key(), ";steward=stg_1")
	assert.NotContains(t, ordinary.Key(), ";steward=")
	v, err := sharing.Compile(elevated)
	require.NoError(t, err)
	assert.Equal(t, elevated.Key(), v.PolicyKey(), "the viewer's key is the input's")
}

// An elevation compiles to its steward grant alone: one Grant per spec,
// each the steward grant's. Compile refuses an input that names a
// steward grant and carries another grant beside it (an ordinary one
// merged in by a careless caller), or only another, or none.
func TestCompileElevationHoldsOnlyItsStewardGrant(t *testing.T) {
	specs := sharing.StewardScope{Specs: []sharing.StewardSpec{{Data: "tag==family"}, {Data: "type==note"}}}.Policies("stg_1", sharing.StewardSettings{})
	steward := sharing.Grant{ID: "stg_1", Audience: "steward:stg_1", Policy: specs[0]}
	second := sharing.Grant{ID: "stg_1", Audience: "steward:stg_1", Policy: specs[1]}
	ordinary := sharing.Grant{ID: "grt_1", Audience: "colleagues", Policy: colleagues()}
	assert.Equal(t, []string{"stg_1/0", "stg_1/1"}, []string{specs[0].Name, specs[1].Name}, "each spec is named for the grant and its index")

	v, err := sharing.Compile(sharing.Input{PrincipalID: "p", StewardGrant: "stg_1", Grants: []sharing.Grant{steward}})
	require.NoError(t, err)
	assert.Equal(t, "stg_1", v.StewardGrantID())
	assert.True(t, v.StrictBackstop())
	both, err := sharing.Compile(sharing.Input{PrincipalID: "p", StewardGrant: "stg_1", Grants: []sharing.Grant{steward, second}})
	require.NoError(t, err, "the specs of one steward grant")
	require.Len(t, both.Plan().Grants, 2)
	assert.Contains(t, both.PolicyKey(), "stg_1/0")
	assert.Contains(t, both.PolicyKey(), "stg_1/1")

	_, err = sharing.Compile(sharing.Input{PrincipalID: "p", StewardGrant: "stg_1", Grants: []sharing.Grant{steward, ordinary}})
	assert.Error(t, err, "an ordinary grant beside the steward grant")
	_, err = sharing.Compile(sharing.Input{PrincipalID: "p", StewardGrant: "stg_1", Grants: []sharing.Grant{ordinary, steward, second}})
	assert.Error(t, err, "an ordinary grant first")
	_, err = sharing.Compile(sharing.Input{PrincipalID: "p", StewardGrant: "stg_1", Grants: []sharing.Grant{ordinary}})
	assert.Error(t, err, "not the named grant")
	_, err = sharing.Compile(sharing.Input{PrincipalID: "p", StewardGrant: "stg_1"})
	assert.Error(t, err, "no grant at all")

	plain, err := sharing.Compile(sharing.Input{PrincipalID: "p", Grants: []sharing.Grant{steward, ordinary}})
	require.NoError(t, err, "without the name, Compile is the ordinary compile")
	assert.Empty(t, plain.StewardGrantID())
	assert.False(t, plain.StrictBackstop())
}
