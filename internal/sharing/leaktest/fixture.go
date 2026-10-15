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

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/dpkmstest"
	grpcserver "github.com/ideacrafterslabs/ctxt/internal/server/grpc"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/sharing/sharingtest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Fixture is an in-process dpkms the cases call: the HTTP API behind
// an httptest server and the gRPC API over an in-memory listener, both
// over one store the caller supplies, guarded by key-backed
// principals. It holds the owner, a colleague and a partner (ADR-080
// §2's two audiences and policy templates), and the contact corpus of
// the ADR's test plan: contacts the viewers see, and the hidden rows
// (the Sarah note with a personal class, a co-mentioning note, a
// work-tagged note, a classed contact, the same content captured under
// another profile) with the entities and edges that derive from them.
// Every case is called as every principal; the hidden rows must never
// show.
type Fixture struct {
	// Server is the HTTP instance: its URL, driver and stack.
	Server *dpkmstest.Server
	// Conn reaches the gRPC API.
	Conn *grpc.ClientConn
	// ObjectID is the seeded contact every principal sees.
	ObjectID string
	// JobID is the seeded job, completed, that produced the contact.
	// Jobs are owner-only.
	JobID string
	// EntitySlug is the seeded entity the contact mentions; every
	// principal sees it.
	EntitySlug string

	owner, colleague, partner Principal
	// visibleTo, when set, names the principal the fixture was seeded
	// for: only the rows that principal sees exist. "" seeds everything.
	visibleTo string
	corpus    corpus
}

// Option configures Start.
type Option func(*options)

type options struct{ visibleTo string }

// WithoutHiddenFor seeds only what principal (by name: "colleague",
// "partner") sees: no object, entity or edge hidden from it. The
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
		owner:     Principal{Name: "owner", ID: storage.OwnerPrincipal, Token: srv.Token(dpkmstest.RoleAdmin)},
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
func (f *Fixture) Colleague() Principal { return f.colleague }

// Partner is the person in the partners audience, granted the
// partners-network template: every contact without a personal class,
// entities, MCP, full-text, vector and graph search.
func (f *Fixture) Partner() Principal { return f.partner }

// Restricted lists the fixture's non-owner principals.
func (f *Fixture) Restricted() []Principal { return []Principal{f.colleague, f.partner} }

// seedPrincipals creates the colleague and the partner, each with one
// key, in its audience under its template, entitled to every
// namespace so the entity gate never hides what the leak checks are
// about.
func (f *Fixture) seedPrincipals(t testing.TB) {
	t.Helper()
	ctx := context.Background()
	driver := f.Server.Driver
	keys := authn.NewKeysOver(driver.Auth(), nil)
	mint := func(name, id, template string) Principal {
		sharingtest.EnsurePrincipal(t, driver, id, storage.PrincipalKindPerson, authn.RoleReader)
		tpl, ok := sharing.Template(template)
		if !ok {
			t.Fatalf("leaktest: no template %s", template)
		}
		sharingtest.Grant(t, driver, tpl, id)
		if err := driver.Entitlements().Upsert(ctx, &storage.RegistryEntitlement{
			RegistryName: id, Plan: "inbound", Namespaces: []string{"*"}, FetchedAt: time.Now(),
		}); err != nil {
			t.Fatalf("leaktest: entitle %s: %v", id, err)
		}
		secret, _, err := keys.Mint(ctx, authn.MintKey{PrincipalID: id, Name: "leaktest " + name})
		if err != nil {
			t.Fatalf("leaktest: mint %s key: %v", name, err)
		}
		v, err := sharing.CompileFor(ctx, driver.Sharing(), id, "", time.Now())
		if err != nil {
			t.Fatalf("leaktest: compile %s viewer: %v", name, err)
		}
		return Principal{Name: name, ID: id, Token: secret, viewer: v}
	}
	f.colleague = mint("colleague", ColleagueID, sharing.TemplateColleagues)
	f.partner = mint("partner", PartnerID, sharing.TemplatePartners)
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

var everyone = []string{"owner", "colleague", "partner"}

// seeds reports whether a row seen by seenBy is part of this fixture.
func (f *Fixture) seeds(seenBy []string) bool {
	return f.visibleTo == "" || slices.Contains(seenBy, f.visibleTo)
}

// seed stores the corpus: the contacts every principal sees first, then
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
	contact := func(i int, id, content string, tags []string, classes []string, profile, hash string) *storage.KnowledgeObject {
		o := &storage.KnowledgeObject{
			ID: id, Type: "contact", Status: storage.ObjectStatusActive, RawContent: content,
			ContentHash: hash, ProfileID: profile, Classes: classes, CreatedAt: at(i), UpdatedAt: at(i),
			Metadata: map[string]any{"org": "Acme", "role": "Head of Ops"},
		}
		for _, tag := range tags {
			o.Tags = append(o.Tags, storage.Tag{Label: tag})
		}
		return o
	}
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
	// Visible to both: a work contact and a client contact.
	visible := []*storage.KnowledgeObject{
		contact(1, ContactSarah, "Sarah Lee | Head of Ops @ Acme", []string{"work"}, nil, "work", SharedContentHash),
		contact(2, ContactClient, "Chris Day | Buyer @ Globex", []string{"client"}, nil, "work", "sha256:"+ContactClient),
	}
	seen := map[string][]string{
		ContactSarah: everyone, ContactClient: everyone,
		// A contact tagged personal only: the partner's filter (every
		// contact) takes it, the colleague's (work or client) does not.
		ContactPersonalTag: {"owner", "partner"},
		// The same content as the work contact, captured under the
		// personal profile with its own tags: its own object, seen by
		// the partner, not the colleague.
		ContactSarahDup: {"owner", "partner"},
		// A contact with the personal class: both templates exclude it.
		ContactClassed: {"owner"},
		// Notes: neither template's data filter reaches a note.
		NoteSarah: {"owner"}, NoteCoMention: {"owner"}, NoteWork: {"owner"},
	}
	hidden := []*storage.KnowledgeObject{
		contact(3, ContactPersonalTag, "Pat Kim | Friend", []string{"personal"}, nil, "personal", "sha256:"+ContactPersonalTag),
		contact(4, ContactSarahDup, "Sarah Lee | Head of Ops @ Acme", []string{"personal"}, nil, "personal", SharedContentHash),
		contact(5, ContactClassed, "Alex Roe | Therapist", []string{"work"}, []string{"personal"}, "work", "sha256:"+ContactClassed),
		note(6, NoteSarah, "private note about Sarah "+CanaryNote, nil, []string{"personal"}),
		note(7, NoteCoMention, "Sarah and the secret contact met "+CanaryCoMention, []string{"work"}, nil),
		note(8, NoteWork, "quarterly planning note", []string{"work"}, nil),
	}
	for _, o := range visible {
		f.create(t, o)
		c.objects[o.ID] = corpusObject{id: o.ID, seenBy: seen[o.ID], content: o.RawContent}
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
	entity(EntitySecret, "Secret Contact", "", []string{"owner"})
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

// VisibleObjects lists the ids of the objects p sees, created order.
func (f *Fixture) VisibleObjects(p Principal) []string {
	var out []string
	for _, id := range []string{ContactSarah, ContactClient, ContactPersonalTag, ContactSarahDup, ContactClassed, NoteSarah, NoteCoMention, NoteWork} {
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
// the job (owner-only) and the canary strings.
func (f *Fixture) Hidden(p Principal) []string {
	var out []string
	if p.Name == "owner" {
		return nil
	}
	for id, o := range f.corpus.objects {
		if !slices.Contains(o.seenBy, p.Name) {
			out = append(out, id)
		}
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
	slices.Sort(out)
	return out
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

// RPCContext returns ctx carrying p's bearer token as gRPC metadata.
func (f *Fixture) RPCContext(ctx context.Context, p Principal) context.Context {
	if p.Token == "" {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+p.Token)
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
