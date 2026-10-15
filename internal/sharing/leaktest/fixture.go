package leaktest

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"slices"
	"testing"
	"time"
	"unicode/utf8"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/dpkmstest"
	"github.com/ideacrafterslabs/ctxt/internal/projection"
	grpcserver "github.com/ideacrafterslabs/ctxt/internal/server/grpc"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/sharing/sharingtest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Fixture is an in-process dpkms the cases call: the HTTP API behind
// an httptest server and the gRPC API over an in-memory listener, both
// over one store the caller supplies, guarded by key-backed
// principals. It holds the owner and the restricted principals of
// ADR-080's test plan: a colleague and a partner (the two audiences and
// policy templates), an agent key narrower than the partner, a person
// in both audiences under a personal-class ceiling, a person whose
// grant expired and a revoked person. The corpus is the ADR's contact
// case: contacts with email, phone, address, a personal note and a
// derived summary, and the hidden rows (the Sarah note with a personal
// class, a co-mentioning note, a work-tagged note, a classed contact,
// the same content captured under another profile) with the entities
// and edges that derive from them. Every case is called as every
// principal; the hidden rows and the projected-out values must never
// show.
type Fixture struct {
	// Server is the HTTP instance: its URL, driver and stack.
	Server *dpkmstest.Server
	// Conn reaches the gRPC API.
	Conn *grpc.ClientConn
	// ObjectID is the seeded contact every viewer sees.
	ObjectID string
	// JobID is the seeded job, completed, that produced the contact.
	// Jobs are owner-only.
	JobID string
	// EntitySlug is the seeded entity the contact mentions; every
	// viewer sees it.
	EntitySlug string

	owner      Principal
	restricted []Principal
	// visibleTo, when set, names the principal the fixture was seeded
	// for: only the rows that principal sees exist. "" seeds everything.
	visibleTo string
	corpus    corpus
}

// Option configures Start.
type Option func(*options)

type options struct{ visibleTo string }

// WithoutHiddenFor seeds only what principal (by name: "colleague",
// "partner", ...) sees: no object, entity or edge hidden from it. The
// differential suite compares that principal's answers over the full
// fixture and this one.
func WithoutHiddenFor(principal string) Option {
	return func(o *options) { o.visibleTo = principal }
}

// bufSize is the in-memory gRPC listener's buffer.
const bufSize = 1 << 20

// Fixture principal ids.
const (
	ColleagueID = "prn_leak_colleague"
	PartnerID   = "prn_leak_partner"
	BothID      = "prn_leak_both"
	ExpiredID   = "prn_leak_expired"
	RevokedID   = "prn_leak_revoked"
)

// Fixture principal names, as Principal.Name and the seenBy lists use
// them.
const (
	NameOwner     = "owner"
	NameColleague = "colleague"
	NamePartner   = "partner"
	// NameAgent is a key on the partner principal bound to AgentPolicy:
	// narrower than the partner in rows (work and client tags only),
	// fields (title and org) and capabilities (full-text search, no
	// MCP).
	NameAgent = "agent"
	// NameBoth is a person in the colleagues and partners audiences and
	// a third one whose policy shows personal notes, under a ceiling
	// that denies the personal class.
	NameBoth = "both"
	// NameExpired is a person whose only grant expired.
	NameExpired = "expired"
	// NameRevoked is a person revoked after its key was minted.
	NameRevoked = "revoked"
)

// Policies the fixture adds to the templates.
const (
	// AgentPolicy narrows the agent key.
	AgentPolicy = "agent-narrow"
	// OpenContactsPolicy projects the note of every contact; the ceiling
	// of the both principal is what keeps the note hidden.
	OpenContactsPolicy = "open-contacts"
)

// Start builds the fixture over driver and serves it until the test
// ends. The test package must run through testguard (dpkmstest
// requires it).
func Start(t testing.TB, driver storage.StorageDriver, opts ...Option) *Fixture {
	t.Helper()
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	srv := dpkmstest.Start(t, driver, dpkmstest.WithKeys())
	f := &Fixture{
		Server:    srv,
		owner:     Principal{Name: NameOwner, ID: storage.OwnerPrincipal, Token: srv.Token(dpkmstest.RoleAdmin)},
		visibleTo: o.visibleTo,
	}
	f.seedPrincipals(t)
	f.seed(t)
	f.serveGRPC(t)
	return f
}

// Owner is the instance owner: the admin principal.
func (f *Fixture) Owner() Principal { return f.owner }

// Colleague is the person in the colleagues audience, granted the
// colleagues-network template: work and client contacts, no personal
// class, entities and MCP, full-text and vector search.
func (f *Fixture) Colleague() Principal { return f.principal(NameColleague) }

// Partner is the person in the partners audience, granted the
// partners-network template: every contact without a personal class,
// entities, MCP, full-text, vector and graph search.
func (f *Fixture) Partner() Principal { return f.principal(NamePartner) }

// Principal returns the fixture principal named name.
func (f *Fixture) Principal(name string) Principal { return f.principal(name) }

func (f *Fixture) principal(name string) Principal {
	if name == NameOwner {
		return f.owner
	}
	for _, p := range f.restricted {
		if p.Name == name {
			return p
		}
	}
	return Principal{}
}

// Restricted lists the fixture's non-owner principals: the viewers
// with a grant in force, then the expired and the revoked one.
func (f *Fixture) Restricted() []Principal { return slices.Clone(f.restricted) }

// Viewers lists the restricted principals with a grant in force:
// those that see rows.
func (f *Fixture) Viewers() []Principal {
	out := make([]Principal, 0, len(f.restricted))
	for _, p := range f.restricted {
		if p.Name != NameExpired && p.Name != NameRevoked {
			out = append(out, p)
		}
	}
	return out
}

// agentPolicy is the policy the agent key is bound to.
func agentPolicy() sharing.Policy {
	return sharing.Policy{
		Name:           AgentPolicy,
		Data:           "type==contact;tag=in=(work,client)",
		ExcludeClasses: []string{storage.ClassPersonal},
		Fields:         map[string][]string{"contact": {"title", "org"}},
		Capabilities:   sharing.Capabilities{Search: []string{sharing.SearchFTS}, Entities: true},
	}
}

// openContactsPolicy projects every contact's title and note.
func openContactsPolicy() sharing.Policy {
	return sharing.Policy{
		Name:         OpenContactsPolicy,
		Data:         "type==contact",
		Fields:       map[string][]string{"contact": {"title", "note"}},
		Capabilities: sharing.Capabilities{Search: []string{sharing.SearchFTS}, Entities: true},
	}
}

// seedPrincipals creates the restricted principals, each with one key,
// in their audiences under their policies, entitled to every namespace
// so the entity gate never hides what the leak checks are about.
func (f *Fixture) seedPrincipals(t testing.TB) {
	t.Helper()
	ctx := context.Background()
	driver := f.Server.Driver
	keys := authn.NewKeysOver(driver.Auth(), nil)
	colleagues, _ := sharing.Template(sharing.TemplateColleagues)
	partners, _ := sharing.Template(sharing.TemplatePartners)

	person := func(id string) {
		sharingtest.EnsurePrincipal(t, driver, id, storage.PrincipalKindPerson, authn.RoleReader)
		if err := driver.Entitlements().Upsert(ctx, &storage.RegistryEntitlement{
			RegistryName: id, Plan: "inbound", Namespaces: []string{"*"}, FetchedAt: time.Now(),
		}); err != nil {
			t.Fatalf("leaktest: entitle %s: %v", id, err)
		}
	}
	mint := func(name, id string) (string, string) {
		secret, issued, err := keys.Mint(ctx, authn.MintKey{PrincipalID: id, Name: "leaktest " + name})
		if err != nil {
			t.Fatalf("leaktest: mint %s key: %v", name, err)
		}
		return secret, issued.IdentityID
	}
	compile := func(name, id, identity string) sharing.Viewer {
		v, err := sharing.CompileFor(ctx, driver.Sharing(), id, identity, time.Now())
		if err != nil {
			t.Fatalf("leaktest: compile %s viewer: %v", name, err)
		}
		return v
	}

	person(ColleagueID)
	sharingtest.Grant(t, driver, colleagues, ColleagueID)
	token, _ := mint(NameColleague, ColleagueID)
	f.restricted = append(f.restricted, Principal{Name: NameColleague, ID: ColleagueID, Token: token, viewer: compile(NameColleague, ColleagueID, "")})

	person(PartnerID)
	sharingtest.Grant(t, driver, partners, PartnerID)
	token, _ = mint(NamePartner, PartnerID)
	f.restricted = append(f.restricted, Principal{Name: NamePartner, ID: PartnerID, Token: token, viewer: compile(NamePartner, PartnerID, "")})

	// The agent: a second key on the partner, bound to the narrower
	// policy (ADR-080 §2: keys narrow, never widen).
	agentToken, agentIdentity := mint(NameAgent, PartnerID)
	spec, err := agentPolicy().Spec()
	if err != nil {
		t.Fatalf("leaktest: agent policy: %v", err)
	}
	agentRow := &storage.SharePolicy{Name: AgentPolicy, Spec: spec}
	if err := driver.Sharing().Policies().Create(ctx, agentRow); err != nil {
		t.Fatalf("leaktest: create agent policy: %v", err)
	}
	if err := driver.Sharing().BindIdentityPolicy(ctx, agentIdentity, agentRow.ID); err != nil {
		t.Fatalf("leaktest: bind agent key: %v", err)
	}
	f.restricted = append(f.restricted, Principal{Name: NameAgent, ID: PartnerID, Token: agentToken, viewer: compile(NameAgent, PartnerID, agentIdentity)})

	// Both audiences plus an open grant, under the ceiling.
	person(BothID)
	sharingtest.Grant(t, driver, colleagues, BothID)
	sharingtest.Grant(t, driver, partners, BothID)
	sharingtest.Grant(t, driver, openContactsPolicy(), BothID)
	if err := driver.Sharing().SetDenyClasses(ctx, BothID, []string{storage.ClassPersonal}); err != nil {
		t.Fatalf("leaktest: ceiling: %v", err)
	}
	token, _ = mint(NameBoth, BothID)
	f.restricted = append(f.restricted, Principal{Name: NameBoth, ID: BothID, Token: token, viewer: compile(NameBoth, BothID, "")})

	// An expired grant: its own audience, granted the partners template
	// until an hour ago.
	person(ExpiredID)
	st := driver.Sharing()
	partnersRow, err := st.Policies().GetByName(ctx, sharing.TemplatePartners)
	if err != nil {
		t.Fatalf("leaktest: partners policy: %v", err)
	}
	expiredAud := &storage.Audience{Name: "expired-partners"}
	if err := st.Audiences().Create(ctx, expiredAud); err != nil {
		t.Fatalf("leaktest: expired audience: %v", err)
	}
	past := time.Now().Add(-time.Hour)
	if err := st.Grants().Create(ctx, &storage.ShareGrant{AudienceID: expiredAud.ID, PolicyID: partnersRow.ID, CreatedBy: storage.OwnerPrincipal, ExpiresAt: &past}); err != nil {
		t.Fatalf("leaktest: expired grant: %v", err)
	}
	if err := st.Audiences().AddMember(ctx, &storage.AudienceMember{AudienceID: expiredAud.ID, PrincipalID: ExpiredID, AddedBy: storage.OwnerPrincipal}); err != nil {
		t.Fatalf("leaktest: expired member: %v", err)
	}
	token, _ = mint(NameExpired, ExpiredID)
	f.restricted = append(f.restricted, Principal{Name: NameExpired, ID: ExpiredID, Token: token, viewer: compile(NameExpired, ExpiredID, "")})

	// A revoked person: granted like the colleague, key minted, then
	// revoked.
	person(RevokedID)
	sharingtest.Grant(t, driver, colleagues, RevokedID)
	token, _ = mint(NameRevoked, RevokedID)
	if err := driver.Auth().Principals().Revoke(ctx, RevokedID, time.Now()); err != nil {
		t.Fatalf("leaktest: revoke: %v", err)
	}
	f.restricted = append(f.restricted, Principal{Name: NameRevoked, ID: RevokedID, Token: token, Revoked: true})
}

// corpus names what seed wrote.
type corpus struct {
	// objects by name, with the names of the principals that see each.
	objects map[string]corpusObject
	// entities by slug, with the names of the principals that see each.
	entities map[string][]string
	// edges by id, with the names of the principals that see each.
	edges map[string][]string
	// canaries are content strings of hidden objects that must never
	// appear in a restricted principal's answer.
	canaries []string
}

type corpusObject struct {
	id      string
	seenBy  []string
	hidden  bool
	content string
	// fields holds a contact's field values by policy field name
	// (title, org, role, city, email, phone, address, note, tags); the
	// values a viewer's policy does not project are canaries.
	fields map[string]string
	// derived holds the values of the fields derived from the whole
	// card (the summary): never shown to a restricted viewer.
	derived []string
}

// Corpus object ids and the two canaries.
const (
	ContactSarah       = "leak-contact-sarah"
	ContactClient      = "leak-contact-client"
	ContactPersonalTag = "leak-contact-personal-tag"
	ContactClassed     = "leak-contact-classed"
	ContactSarahDup    = "leak-contact-sarah-dup"
	NoteSarah          = "leak-note-sarah"
	NoteCoMention      = "leak-note-comention"
	NoteWork           = "leak-note-work"
	EntitySarah        = "person.sarah-lee"
	EntitySecret       = "person.secret-contact"
	EntityAcme         = "org.acme-registry"
	CanaryNote         = "SARAH-PRIVATE-NOTE-CANARY"
	CanaryCoMention    = "SARAH-COMENTION-CANARY"
	SharedContentHash  = "sha256:leak-shared-contact-content"
)

// Contact field values of the seeded contacts, for tests that assert
// on one of them.
const (
	SarahEmail   = "sarah.lee@acme.example"
	SarahPhone   = "+1 555 0100 2001"
	SarahAddress = "12 Harbor Street, Boston"
	SarahNote    = "NOTE-SARAH-CANARY prefers mornings"
	// ClientCity is a projected-out value under CanaryMinRunes for the
	// agent: it must not blank other text.
	ClientCity = "Lee"
)

// everyone names the principals that see the unclassed work contacts:
// every viewer with a grant in force.
var everyone = []string{NameOwner, NameColleague, NamePartner, NameAgent, NameBoth}

// seeds reports whether a row seen by seenBy is part of this fixture.
func (f *Fixture) seeds(seenBy []string) bool {
	return f.visibleTo == "" || slices.Contains(seenBy, f.visibleTo)
}

// contactSpec is one seeded contact.
type contactSpec struct {
	id, name, role, org, city, email, phone, address, note string
	tags, classes                                          []string
	profile, hash                                          string
}

// object builds the contact as cardamum ingests it (ADR-080 plan
// Phase 2a): details in metadata, never in the text; the card text
// from the card fields; the card as a section node; the note as a
// section node classed personal; and a summary a step would derive
// from the whole card, email included.
func (c contactSpec) object(at time.Time) (*storage.KnowledgeObject, corpusObject) {
	meta := map[string]any{"addressbook": "default"}
	for k, v := range map[string]string{"name": c.name, "org": c.org, "role": c.role, "city": c.city, "email": c.email, "phone": c.phone, "address": c.address} {
		if v != "" {
			meta[k] = v
		}
	}
	o := &storage.KnowledgeObject{
		ID: c.id, Type: projection.TypeContact, Status: storage.ObjectStatusActive,
		ContentHash: c.hash, ProfileID: c.profile, Classes: c.classes, CreatedAt: at, UpdatedAt: at, Metadata: meta,
	}
	for _, tag := range c.tags {
		o.Tags = append(o.Tags, storage.Tag{Label: tag})
	}
	o.RawContent = projection.RenderContact(o, projection.ContactCardFields)
	summary := c.name + ", " + c.role + " at " + c.org + ", reachable at " + c.email
	o.Summaries = []string{summary}
	o.Graph = &storage.ObjectGraph{Nodes: []storage.GraphNode{{
		ID: storage.NewNodeID(c.id, string(storage.NodeTypeSection), 0), NodeType: storage.NodeTypeSection,
		Label: projection.ContactCardLabel, Content: o.RawContent,
	}}}
	if c.note != "" {
		n := storage.GraphNode{
			ID: storage.NewNodeID(c.id, string(storage.NodeTypeSection), 1), NodeType: storage.NodeTypeSection,
			Label: projection.ContactNoteLabel, Content: c.note, Order: 1,
		}
		storage.SetNodeClasses(&n, []string{storage.ClassPersonal})
		o.Graph.Nodes = append(o.Graph.Nodes, n)
	}
	co := corpusObject{
		id: c.id, content: o.RawContent, derived: []string{summary},
		fields: map[string]string{
			projection.ContactTitle: c.name, projection.ContactOrg: c.org, projection.ContactRole: c.role,
			projection.ContactCity: c.city, projection.ContactEmail: c.email, projection.ContactPhone: c.phone,
			projection.ContactAddress: c.address, projection.ContactNote: c.note,
		},
	}
	return o, co
}

// seed stores the corpus: the contacts every viewer sees first, then
// the rows some principal does not see, each when the fixture carries
// it, and the edges whose both ends it carries.
func (f *Fixture) seed(t testing.TB) {
	t.Helper()
	ctx := context.Background()
	driver := f.Server.Driver
	base := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	c := corpus{objects: map[string]corpusObject{}, entities: map[string][]string{}, edges: map[string][]string{}}
	f.corpus = c

	at := func(i int) time.Time { return base.Add(-time.Duration(i) * time.Minute) }
	note := func(i int, id, content string, tags []string, classes []string) *storage.KnowledgeObject {
		o := &storage.KnowledgeObject{
			ID: id, Type: "note", Status: storage.ObjectStatusActive, RawContent: content,
			ContentHash: "sha256:" + id, Classes: classes, CreatedAt: at(i), UpdatedAt: at(i),
		}
		for _, tag := range tags {
			o.Tags = append(o.Tags, storage.Tag{Label: tag})
		}
		return o
	}
	seen := map[string][]string{
		ContactSarah: everyone, ContactClient: everyone,
		// A contact tagged personal only: the partner's filter (every
		// contact) takes it, the colleague's (work or client) and the
		// agent's (work or client) do not.
		ContactPersonalTag: {NameOwner, NamePartner, NameBoth},
		// The same content as the work contact, captured under the
		// personal profile with its own tags: its own object, seen by
		// the partner, not the colleague or the agent.
		ContactSarahDup: {NameOwner, NamePartner, NameBoth},
		// A contact with the personal class: every template excludes
		// it, and the ceiling keeps it from the both principal whose
		// open grant would show it.
		ContactClassed: {NameOwner},
		// Notes: neither template's data filter reaches a note.
		NoteSarah: {NameOwner}, NoteCoMention: {NameOwner}, NoteWork: {NameOwner},
	}
	contacts := []contactSpec{
		{id: ContactSarah, name: "Sarah Lee", role: "Head of Ops", org: "Acme", city: "Boston", email: SarahEmail, phone: SarahPhone, address: SarahAddress, note: SarahNote, tags: []string{"work"}, profile: "work", hash: SharedContentHash},
		{id: ContactClient, name: "Chris Day", role: "Buyer", org: "Globex", city: ClientCity, email: "chris.day@globex.example", phone: "+1 555 0100 2002", address: "7 Market Row, Austin", note: "NOTE-CLIENT-CANARY golf on fridays", tags: []string{"client"}, profile: "work", hash: "sha256:" + ContactClient},
		{id: ContactPersonalTag, name: "Pat Kim", role: "Friend", org: "", city: "Denver", email: "pat.kim@home.example", phone: "+1 555 0100 2003", address: "3 Pine Lane, Denver", note: "NOTE-PAT-CANARY birthday in june", tags: []string{"personal"}, profile: "personal", hash: "sha256:" + ContactPersonalTag},
		{id: ContactSarahDup, name: "Sarah Lee", role: "Head of Ops", org: "Acme", city: "Boston", email: SarahEmail, phone: SarahPhone, address: SarahAddress, note: "NOTE-SARAH-DUP-CANARY home address shared", tags: []string{"personal"}, profile: "personal", hash: SharedContentHash},
		{id: ContactClassed, name: "Alex Roe", role: "Therapist", org: "", city: "Zurich", email: "alex.roe@clinic.example", phone: "+1 555 0100 2004", address: "9 Quiet Close, Zurich", note: "NOTE-ALEX-CANARY thursday sessions", tags: []string{"work"}, classes: []string{storage.ClassPersonal}, profile: "work", hash: "sha256:" + ContactClassed},
	}
	for i, spec := range contacts {
		o, co := spec.object(at(i + 1))
		co.seenBy = seen[spec.id]
		co.hidden = i >= 2
		c.objects[spec.id] = co
		if f.seeds(co.seenBy) {
			f.create(t, o)
		}
	}
	hidden := []*storage.KnowledgeObject{
		note(6, NoteSarah, "private note about Sarah "+CanaryNote, nil, []string{storage.ClassPersonal}),
		note(7, NoteCoMention, "Sarah and the secret contact met "+CanaryCoMention, []string{"work"}, nil),
		note(8, NoteWork, "quarterly planning note", []string{"work"}, nil),
	}
	for _, o := range hidden {
		so := corpusObject{id: o.ID, seenBy: seen[o.ID], content: o.RawContent, hidden: true}
		c.objects[o.ID] = so
		if f.seeds(so.seenBy) {
			f.create(t, o)
		}
	}
	c.canaries = []string{CanaryNote, CanaryCoMention}

	// Entities: a placeholder the visible contact mentions, a placeholder
	// only hidden notes mention, and a registry-served one.
	entity := func(slug, title, registry string, seenBy []string) {
		e := &storage.Entity{Slug: slug, Title: title, Namespace: "person", RegistryURL: registry, CreatedAt: base, UpdatedAt: base}
		if slug == EntityAcme {
			e.Namespace = "org"
		}
		c.entities[slug] = seenBy
		if !f.seeds(seenBy) {
			return
		}
		if err := driver.Entities().Upsert(ctx, e); err != nil {
			t.Fatalf("leaktest: seed entity %s: %v", slug, err)
		}
	}
	entity(EntitySarah, "Sarah Lee", "", everyone)
	entity(EntitySecret, "Secret Contact", "", []string{NameOwner})
	entity(EntityAcme, "Acme", "https://registry.test", everyone)

	// Edges: mentions from each object to its entities, the reverse
	// mentioned_in, and object links.
	target := func(slug string) string { return "ctxt://entity/" + slug }
	edge := func(id, fromType, fromID, toType, toID, kind string) {
		seenBy := intersect(c.objects, c.entities, fromType, fromID, toType, toID)
		c.edges[id] = seenBy
		if !f.seeds(seersOf(c, fromType, fromID)) || !f.seeds(seersOf(c, toType, toID)) {
			return
		}
		e := &storage.Edge{ID: id, FromType: fromType, FromID: fromID, ToType: toType, ToID: toID, EdgeType: kind, Weight: 1, CreatedAt: base}
		if err := driver.Edges().Create(ctx, e); err != nil {
			t.Fatalf("leaktest: seed edge %s: %v", id, err)
		}
	}
	mention := func(objectID, slug string) {
		edge("leak-mention-"+objectID+"-"+slug, "object", objectID, "entity", target(slug), storage.EdgeTypeMentions)
		edge("leak-mentioned-in-"+objectID+"-"+slug, "entity", target(slug), "object", objectID, storage.EdgeTypeMentionedIn)
	}
	mention(ContactSarah, EntitySarah)
	mention(ContactSarahDup, EntitySarah)
	mention(ContactClassed, EntitySarah)
	mention(NoteSarah, EntitySarah)
	mention(NoteSarah, EntitySecret)
	mention(NoteCoMention, EntitySarah)
	mention(NoteCoMention, EntitySecret)
	edge("leak-link-sarah-note", "object", ContactSarah, "object", NoteSarah, "references")
	edge("leak-link-sarah-client", "object", ContactSarah, "object", ContactClient, "references")
	edge("leak-link-personal-sarah", "object", ContactPersonalTag, "object", ContactSarah, "references")

	job := &storage.Job{
		ID: "leak-owner-job", Type: "analyze", Status: storage.JobCompleted, Pipeline: "text.short",
		ResultID: ContactSarah, Payload: "job payload " + CanaryNote, CreatedAt: base, UpdatedAt: base, CompletedAt: &base,
	}
	if err := driver.Jobs().Create(ctx, job); err != nil {
		t.Fatalf("leaktest: seed job: %v", err)
	}
	f.corpus = c
	f.ObjectID, f.JobID, f.EntitySlug = ContactSarah, job.ID, EntitySarah
}

func (f *Fixture) create(t testing.TB, o *storage.KnowledgeObject) {
	t.Helper()
	if err := f.Server.Driver.Objects().Create(context.Background(), o); err != nil {
		t.Fatalf("leaktest: seed object %s: %v", o.ID, err)
	}
}

// seersOf returns the principals that see an edge end: an object end
// by its object, an entity end by its entity.
func seersOf(c corpus, typ, id string) []string {
	if typ == "object" {
		return c.objects[id].seenBy
	}
	for slug, by := range c.entities {
		if "ctxt://entity/"+slug == id {
			return by
		}
	}
	return nil
}

// intersect returns the principals that see both ends of an edge.
func intersect(objects map[string]corpusObject, entities map[string][]string, fromType, fromID, toType, toID string) []string {
	c := corpus{objects: objects, entities: entities}
	var out []string
	for _, p := range seersOf(c, fromType, fromID) {
		if slices.Contains(seersOf(c, toType, toID), p) {
			out = append(out, p)
		}
	}
	return out
}

// corpusOrder is the seeded objects in created order.
var corpusOrder = []string{ContactSarah, ContactClient, ContactPersonalTag, ContactSarahDup, ContactClassed, NoteSarah, NoteCoMention, NoteWork}

// VisibleObjects lists the ids of the objects p sees, created order.
func (f *Fixture) VisibleObjects(p Principal) []string {
	var out []string
	for _, id := range corpusOrder {
		o := f.corpus.objects[id]
		if !f.seeds(o.seenBy) {
			continue
		}
		if slices.Contains(o.seenBy, p.Name) {
			out = append(out, id)
		}
	}
	return out
}

// Hidden lists what p must never be answered: the ids of objects, the
// slugs and node ids of entities and the ids of edges it does not see,
// the job (owner-only), the canary strings, and the contact values its
// policies do not project (ADR-080 §5): every field value and derived
// value of a contact it does not see, the values of the fields its
// plan leaves out of a contact it does see, the note whenever its plan
// excludes the personal class, and every summary, which derives from
// the whole card. Values under sharing.CanaryMinRunes are left out, as
// the backstop leaves them alone.
func (f *Fixture) Hidden(p Principal) []string {
	if p.Name == NameOwner {
		return nil
	}
	out := make([]string, 0, 64)
	plan := p.viewer.Plan()
	projects := func(field string) bool {
		fields := plan.FieldsFor(projection.TypeContact)
		return slices.Contains(fields, field) || slices.Contains(fields, sharing.FieldAll)
	}
	// What p legitimately sees of its visible contacts: a hidden row's
	// value that repeats one of these (the same content captured under
	// another profile, a shared city) is no leak when it shows.
	shown := map[string]bool{}
	for _, o := range f.corpus.objects {
		if !slices.Contains(o.seenBy, p.Name) {
			continue
		}
		for field, v := range o.fields {
			visible := projects(field)
			if class, ok := projection.ContactFieldClasses[field]; ok && plan.ExcludesAny([]string{class}) {
				visible = false
			}
			if visible {
				shown[v] = true
			} else {
				out = append(out, v)
			}
		}
		out = append(out, o.derived...)
	}
	for id, o := range f.corpus.objects {
		if slices.Contains(o.seenBy, p.Name) {
			continue
		}
		out = append(out, id)
		for _, v := range o.fields {
			if !shown[v] {
				out = append(out, v)
			}
		}
		out = append(out, o.derived...)
	}
	for slug, by := range f.corpus.entities {
		if !slices.Contains(by, p.Name) {
			out = append(out, slug, "ctxt://entity/"+slug)
		}
	}
	for id, by := range f.corpus.edges {
		if !slices.Contains(by, p.Name) {
			out = append(out, id)
		}
	}
	out = append(out, f.JobID)
	out = append(out, f.corpus.canaries...)
	out = slices.DeleteFunc(out, func(s string) bool { return utf8.RuneCountInString(s) < sharing.CanaryMinRunes })
	slices.Sort(out)
	return slices.Compact(out)
}

// serveGRPC serves the stack's service over gRPC on an in-memory
// listener, behind the same provider, emitter and entitlement gate as
// the HTTP router, reflection on.
func (f *Fixture) serveGRPC(t testing.TB) {
	t.Helper()
	st := f.Server.Stack
	ln := bufconn.Listen(bufSize)
	gs := grpcserver.New(ln.Addr().String(), st.Service,
		grpcserver.WithAuth(st.RouteAuth),
		grpcserver.WithSecurity(st.Security),
		grpcserver.WithEntitlements(st.Entitlements),
		grpcserver.WithReflection(true),
		grpcserver.WithStrictSharing(st.StrictSharing),
		grpcserver.WithAudit(st.Audit),
	)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := gs.Serve(ctx, ln); err != nil {
			t.Logf("leaktest: grpc serve: %v", err)
		}
	}()
	t.Cleanup(func() { cancel(); <-done })

	conn, err := grpc.NewClient("passthrough:///"+ln.Addr().String(),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return ln.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("leaktest: grpc client: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	f.Conn = conn
}

// HTTP calls method path (relative to /api/v1) as p, body JSON-encoded
// when non-nil, and returns what the route answered. A stream is read
// until ctx ends; the frames received by then are the body.
func (f *Fixture) HTTP(ctx context.Context, p Principal, method, path string, body any) (Result, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return Result{}, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, f.Server.URL+"/api/v1"+path, rd)
	if err != nil {
		return Result{}, err
	}
	if p.Token != "" {
		req.Header.Set("Authorization", "Bearer "+p.Token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, vs := range p.Header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	// A body cut by ctx is a stream's answer so far, not a failure.
	if err != nil && ctx.Err() == nil {
		return Result{}, err
	}
	return Result{Status: resp.StatusCode, Header: resp.Header, Body: out}, nil
}

// RPCContext returns ctx carrying p's bearer token, and its extra
// headers, as gRPC metadata.
func (f *Fixture) RPCContext(ctx context.Context, p Principal) context.Context {
	if p.Token != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+p.Token)
	}
	for k, vs := range p.Header {
		for _, v := range vs {
			ctx = metadata.AppendToOutgoingContext(ctx, k, v)
		}
	}
	return ctx
}

// RPCResult folds an RPC's response messages and error into a Result.
func RPCResult(msgs []proto.Message, err error) Result {
	var body bytes.Buffer
	for _, m := range msgs {
		b, merr := protojson.Marshal(m)
		if merr != nil {
			b = []byte("<" + merr.Error() + ">")
		}
		body.Write(b)
		body.WriteByte('\n')
	}
	st, _ := status.FromError(err)
	if err != nil {
		body.WriteString(st.Message())
	}
	return Result{Code: st.Code(), Body: body.Bytes()}
}
