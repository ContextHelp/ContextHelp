// Package corpus builds a deterministic knowledge corpus through the
// storage API: contacts whose email and phone sit in their metadata only,
// their text being the card the contact renderer builds, and a third of
// them carrying a personal note as a classed graph node, as contact
// ingest stores them; notes that mention those contacts, with the
// mention edges a pipeline run writes; tags; and a few profiles. The
// auth and sharing work measures against it, the owner's find and list
// first and then the owner against restricted viewers on one corpus, and
// any test that needs a graph larger than a handful of objects can seed
// one with it.
//
// Build writes only through storage.StorageDriver, so the corpus is the
// same on every driver, and one seed always yields the same IDs, text,
// timestamps, metadata and edges: a build on another machine or another
// day is the identical corpus, so its numbers compare.
//
// test/integration's 100k simulation is the scale harness: random text
// and random vectors behind a count check. This corpus is for comparison
// between runs and between principals. It carries no vectors, because a
// vector leg embeds the query with a provider the fixture does not have.
package corpus

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ideacrafterslabs/ctxt/internal/projection"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	uri "hop.top/cite/scheme"
)

const (
	// DefaultObjects is the corpus size when Spec.Objects is zero: small
	// enough for a routine benchmark run.
	DefaultObjects = 1000
	// DefaultContactRatio is the share of objects that are contacts when
	// Spec.ContactRatio is zero; the rest are notes.
	DefaultContactRatio = 0.1
	// DefaultEdgeBatch is how many edges one CreateAll transaction writes
	// when Spec.EdgeBatch is zero.
	DefaultEdgeBatch = 1000

	// Namespace is the entity namespace contacts resolve under: a
	// contact's mention is @person.<slug>.
	Namespace = "person"
	// TypeContact and TypeNote are the two object types the corpus holds.
	TypeContact = "contact"
	TypeNote    = "note"
	// Source is every object's Source: the corpus is its own ingest
	// source.
	Source = "corpus"

	// progressEvery is how many objects Spec.Progress is told about at a
	// time.
	progressEvery = 10_000
	// maxMentions is the most contacts one note mentions.
	maxMentions = 3
	// queryContacts is how many contact names Queries returns.
	queryContacts = 16

	// entityNamespace is the ctxt:// URI namespace a mention names.
	entityNamespace = "entity"
	nodeObject      = "object"
	edgeMentions    = "mentions"
	edgeMentionedIn = "mentioned_in"
	// pcgSalt separates the generator's second seed word from the first.
	pcgSalt = 0x9E3779B97F4A7C15
)

// Spec describes the corpus Build generates.
type Spec struct {
	// Objects is the total object count, contacts and notes together;
	// zero means DefaultObjects.
	Objects int
	// Seed drives every choice the generator makes. Two builds with one
	// seed produce identical corpora; different seeds, different ones.
	Seed uint64
	// ContactRatio is the share of Objects that are contacts, in (0, 1);
	// zero means DefaultContactRatio. At least one object is a contact.
	ContactRatio float64
	// EdgeBatch is how many edges go into one transaction; zero means
	// DefaultEdgeBatch.
	EdgeBatch int
	// Progress, when set, is called after every progressEvery objects
	// and once more at the end.
	Progress func(done, total int)
}

// withDefaults fills the zero fields of s.
func (s Spec) withDefaults() Spec {
	if s.Objects < 1 {
		s.Objects = DefaultObjects
	}
	if s.ContactRatio <= 0 || s.ContactRatio >= 1 {
		s.ContactRatio = DefaultContactRatio
	}
	if s.EdgeBatch < 1 {
		s.EdgeBatch = DefaultEdgeBatch
	}
	return s
}

// Contact is one generated contact: the object that holds the card and
// the entity notes mention.
type Contact struct {
	// ID is the contact object's ID.
	ID string
	// Slug is the entity slug, person/<first>-<last>-<n>. Mention is its
	// @person.<...> form; URI is the canonical ctxt://entity/... string
	// the object's mention and its edges carry.
	Slug, Mention, URI string
	Name, Email, Phone string
	Org, Role, City    string
	// Profile is the object's ProfileID.
	Profile string
	// Note is the personal note the contact carries as a classed graph
	// node; empty for two contacts in three.
	Note string
}

// Corpus is what Build wrote.
type Corpus struct {
	Spec Spec
	// Contacts are the contacts in creation order.
	Contacts []Contact
	// NoteIDs are the note object IDs in creation order.
	NoteIDs []string
	// Edges is how many edges were written: a mentions and a
	// mentioned_in edge per mention, on notes and on contacts, which
	// mention their own entity.
	Edges int
	// BuildTime is how long Build took.
	BuildTime time.Duration
}

// Build writes the corpus spec describes into store and returns what it
// wrote. Objects go in one at a time, as the object store writes them;
// edges go in transactions of spec.EdgeBatch.
func Build(ctx context.Context, store storage.StorageDriver, spec Spec) (*Corpus, error) {
	spec = spec.withDefaults()
	contacts := int(math.Round(float64(spec.Objects) * spec.ContactRatio))
	contacts = max(1, min(contacts, spec.Objects))
	notes := spec.Objects - contacts

	start := time.Now()
	g := newGenerator(spec.Seed)
	w := &edgeWriter{store: store, ns: g.ns, batch: spec.EdgeBatch}
	c := &Corpus{Spec: spec, Contacts: make([]Contact, 0, contacts), NoteIDs: make([]string, 0, notes)}
	done := 0
	progress := func() {
		if spec.Progress != nil {
			spec.Progress(done, spec.Objects)
		}
	}

	for i := range contacts {
		ct, obj := g.contact(i)
		if err := store.Objects().Create(ctx, obj); err != nil {
			return nil, fmt.Errorf("corpus: contact %d: %w", i, err)
		}
		if err := store.Entities().UpsertThin(ctx, entity(ct, obj.CreatedAt)); err != nil {
			return nil, fmt.Errorf("corpus: contact %d entity: %w", i, err)
		}
		if err := w.mention(ctx, obj, ct.URI); err != nil {
			return nil, fmt.Errorf("corpus: contact %d: %w", i, err)
		}
		c.Contacts = append(c.Contacts, ct)
		if done++; done%progressEvery == 0 {
			progress()
		}
	}
	for i := range notes {
		obj, mentioned := g.note(i, c.Contacts)
		if err := store.Objects().Create(ctx, obj); err != nil {
			return nil, fmt.Errorf("corpus: note %d: %w", i, err)
		}
		for _, ct := range mentioned {
			if err := w.mention(ctx, obj, ct.URI); err != nil {
				return nil, fmt.Errorf("corpus: note %d: %w", i, err)
			}
		}
		c.NoteIDs = append(c.NoteIDs, obj.ID)
		if done++; done%progressEvery == 0 {
			progress()
		}
	}
	if err := w.flush(ctx); err != nil {
		return nil, fmt.Errorf("corpus: %w", err)
	}
	if done%progressEvery != 0 {
		progress()
	}
	c.Edges = w.written
	c.BuildTime = time.Since(start)
	return c, nil
}

// Queries returns full-text queries every build of a spec yields alike:
// the first contacts' names, which their cards and the notes mentioning
// them match, interleaved with the topics, which many notes discuss.
// Every query has hits, so a benchmark cycling through them measures
// ranking, not misses.
func (c *Corpus) Queries() []string {
	names := min(len(c.Contacts), queryContacts)
	out := make([]string, 0, names+len(topics))
	for i := 0; i < max(names, len(topics)); i++ {
		if i < names {
			out = append(out, c.Contacts[i].Name)
		}
		if i < len(topics) {
			out = append(out, topics[i])
		}
	}
	return out
}

// generator makes every choice from one seeded source, in a fixed order,
// so a seed is a corpus.
type generator struct {
	rng *rand.Rand
	// ns is the UUID namespace object and edge IDs derive from.
	ns uuid.UUID
	// epoch is the newest time an object is created at; objects spread
	// over the three years before it.
	epoch time.Time
}

func newGenerator(seed uint64) *generator {
	return &generator{
		rng:   rand.New(rand.NewPCG(seed, seed^pcgSalt)), // #nosec G404 -- a repeatable corpus is the point
		ns:    uuid.NewSHA1(uuid.NameSpaceOID, fmt.Appendf(nil, "ctxt corpus %d", seed)),
		epoch: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

// id is the UUID of the i-th object of a kind under this seed.
func (g *generator) id(kind string, i int) string {
	return uuid.NewSHA1(g.ns, fmt.Appendf(nil, "%s:%d", kind, i)).String()
}

func (g *generator) pick(words []string) string {
	return words[g.rng.IntN(len(words))]
}

// sample returns k distinct indices below n, k clipped to n.
func (g *generator) sample(n, k int) []int {
	k = min(k, n)
	out := make([]int, 0, k)
	for len(out) < k {
		if x := g.rng.IntN(n); !slices.Contains(out, x) {
			out = append(out, x)
		}
	}
	return out
}

// between returns an integer in [lo, hi].
func (g *generator) between(lo, hi int) int {
	return lo + g.rng.IntN(hi-lo+1)
}

// createdAt is a time in the three years before the epoch, to the second.
func (g *generator) createdAt() time.Time {
	return g.epoch.Add(-time.Duration(g.rng.IntN(3*365*24*60)) * time.Minute)
}

// profile is personal more often than work, and work more often than
// the client profile.
func (g *generator) profile() string {
	switch p := g.rng.IntN(100); {
	case p < 55:
		return Profiles[0]
	case p < 90:
		return Profiles[1]
	default:
		return Profiles[2]
	}
}

// tags are one to most distinct labels from vocab.
func (g *generator) tags(vocab []string, most int) []storage.Tag {
	idx := g.sample(len(vocab), g.between(1, most))
	tags := make([]storage.Tag, len(idx))
	for i, j := range idx {
		tags[i] = storage.Tag{Label: vocab[j], Source: Source, Weight: 1}
	}
	return tags
}

// contact is the i-th contact and the object that holds its card. The
// email and phone sit in the metadata only; the text is the card the
// contact renderer builds from the identity fields; every third contact
// carries a personal note as a classed section node, as contact ingest
// stores them; and the contact mentions its own entity.
func (g *generator) contact(i int) (Contact, *storage.KnowledgeObject) {
	first, last := g.pick(firstNames), g.pick(lastNames)
	org := g.pick(orgs)
	ct := Contact{
		ID:      g.id(TypeContact, i),
		Name:    first + " " + last,
		Org:     org,
		Role:    g.pick(roles),
		City:    g.pick(cities),
		Profile: g.profile(),
	}
	ct.Slug = fmt.Sprintf("%s/%s-%s-%04d", Namespace, strings.ToLower(first), strings.ToLower(last), i)
	ct.Mention = "@" + strings.ReplaceAll(ct.Slug, "/", ".")
	u := uri.URI{Scheme: "ctxt", Namespace: entityNamespace, ID: ct.Slug}
	ct.URI = u.String()
	ct.Email = fmt.Sprintf("%s.%s%d@%s.example", strings.ToLower(first), strings.ToLower(last), i,
		strings.ToLower(strings.ReplaceAll(org, " ", "")))
	ct.Phone = fmt.Sprintf("+1-555-%03d-%04d", g.rng.IntN(1000), i%10_000)
	if i%3 == 0 {
		ct.Note = fmt.Sprintf("%s: %s", first, g.pick(contactNotes))
	}

	at := g.createdAt()
	obj := &storage.KnowledgeObject{
		ID:        ct.ID,
		Type:      TypeContact,
		Summaries: []string{ct.Name},
		Metadata: map[string]any{
			"type": TypeContact, "name": ct.Name, "email": ct.Email, "phone": ct.Phone,
			"org": ct.Org, "role": ct.Role, "city": ct.City, "source_type": TypeContact,
		},
		Tags:      g.tags(contactTags, 2),
		Mentions:  []uri.URI{u},
		Source:    Source,
		ProfileID: ct.Profile,
		Status:    storage.ObjectStatusActive,
		CreatedAt: at,
		UpdatedAt: at,
	}
	obj.RawContent = projection.RenderContact(obj, projection.ContactCardFields)
	obj.TextContent = obj.RawContent
	obj.Graph = &storage.ObjectGraph{Nodes: []storage.GraphNode{{
		ID: storage.NewNodeID(obj.ID, storage.NodeTypeSection, 0), NodeType: storage.NodeTypeSection,
		Label: projection.ContactCardLabel, Content: obj.RawContent, Order: 0,
	}}}
	if ct.Note != "" {
		note := storage.GraphNode{
			ID: storage.NewNodeID(obj.ID, storage.NodeTypeSection, 1), NodeType: storage.NodeTypeSection,
			Label: projection.ContactNoteLabel, Content: ct.Note, Order: 1,
		}
		storage.SetNodeClasses(&note, []string{storage.ClassPersonal})
		obj.Graph.Nodes = append(obj.Graph.Nodes, note)
	}
	obj.ContentHash = contentHash(obj)
	return ct, obj
}

// note is the i-th note: two to four sentences about one to three of
// the contacts, every one of them named in the text, mentioned, listed
// under metadata.people and carried as a mention URI.
func (g *generator) note(i int, contacts []Contact) (*storage.KnowledgeObject, []Contact) {
	idx := g.sample(len(contacts), g.between(1, maxMentions))
	people := make([]Contact, len(idx))
	mentioned := make([]uri.URI, len(idx))
	names := make([]string, len(idx))
	handles := make([]string, len(idx))
	for j, k := range idx {
		people[j] = contacts[k]
		mentioned[j] = uri.URI{Scheme: "ctxt", Namespace: entityNamespace, ID: people[j].Slug}
		names[j] = fmt.Sprintf("%s (%s)", people[j].Name, people[j].Org)
		handles[j] = people[j].Mention
	}
	kind := g.pick(noteKinds)
	ts := make([]string, 0, maxMentions)
	for _, k := range g.sample(len(topics), g.between(1, 3)) {
		ts = append(ts, topics[k])
	}
	at := g.createdAt()

	obj := &storage.KnowledgeObject{
		ID:         g.id(TypeNote, i),
		Type:       TypeNote,
		RawContent: g.body(people, ts) + " Present: " + strings.Join(names, ", ") + ".",
		Summaries:  []string{strings.ToUpper(kind[:1]) + kind[1:] + " with " + people[0].Name + " about " + ts[0]},
		Metadata: map[string]any{
			"type": kind, "topics": ts, "people": handles, "source_type": g.pick(sourceTypes),
			"dates_mentioned": []string{at.Format("2006-01-02")},
		},
		Tags:      g.tags(noteTags, 3),
		Mentions:  mentioned,
		Source:    Source,
		ProfileID: g.profile(),
		Status:    storage.ObjectStatusActive,
		CreatedAt: at,
		UpdatedAt: at,
	}
	obj.ContentHash = contentHash(obj)
	return obj, people
}

// body is two to four distinct sentences filled in for people and ts.
func (g *generator) body(people []Contact, ts []string) string {
	name2, topic2 := "the team", ts[0]
	if len(people) > 1 {
		name2 = people[1].Name
	}
	if len(ts) > 1 {
		topic2 = ts[1]
	}
	fill := strings.NewReplacer(
		"{name}", people[0].Name, "{name2}", name2, "{org}", people[0].Org,
		"{role}", people[0].Role, "{city}", people[0].City, "{topic}", ts[0], "{topic2}", topic2,
	)
	idx := g.sample(len(sentences), g.between(2, 4))
	out := make([]string, len(idx))
	for i, k := range idx {
		out[i] = fill.Replace(sentences[k])
	}
	return strings.Join(out, " ")
}

// entity is the thin entity row a contact's mention resolves to.
func entity(ct Contact, at time.Time) *storage.Entity {
	return &storage.Entity{
		Slug: ct.Slug, Title: ct.Name, Namespace: Namespace,
		ContentStatus: storage.ContentStatusThin, CreatedAt: at, UpdatedAt: at,
	}
}

// contentHash is the object's content hash: hex SHA-256, as the ingest
// path stores it, over the text and the ID. The ID keeps every hash
// distinct, as the objects table requires, where two notes read alike.
func contentHash(obj *storage.KnowledgeObject) string {
	h := sha256.Sum256([]byte(obj.RawContent + "\x00" + obj.ID))
	return hex.EncodeToString(h[:])
}

// edgeWriter collects mention edges and writes them in batches.
type edgeWriter struct {
	store   storage.StorageDriver
	ns      uuid.UUID
	batch   int
	pending []*storage.Edge
	written int
}

// mention queues the mentions edge from obj to the entity at target and
// the mentioned_in edge back, as the ingest worker writes them, and
// flushes a full batch.
func (w *edgeWriter) mention(ctx context.Context, obj *storage.KnowledgeObject, target string) error {
	w.pending = append(w.pending,
		w.edge(nodeObject, obj.ID, storage.EdgeEndpointEntity, target, edgeMentions, obj.CreatedAt),
		w.edge(storage.EdgeEndpointEntity, target, nodeObject, obj.ID, edgeMentionedIn, obj.CreatedAt),
	)
	if len(w.pending) >= w.batch {
		return w.flush(ctx)
	}
	return nil
}

func (w *edgeWriter) edge(fromType, fromID, toType, toID, edgeType string, at time.Time) *storage.Edge {
	key := fmt.Appendf(nil, "edge:%s/%s>%s/%s:%s", fromType, fromID, toType, toID, edgeType)
	return &storage.Edge{
		ID: uuid.NewSHA1(w.ns, key).String(), FromType: fromType, FromID: fromID, ToType: toType, ToID: toID,
		EdgeType: edgeType, Weight: 1, CreatedAt: at,
	}
}

// flush writes the pending edges in one transaction.
func (w *edgeWriter) flush(ctx context.Context) error {
	if len(w.pending) == 0 {
		return nil
	}
	if err := w.store.Edges().CreateAll(ctx, w.pending); err != nil {
		return fmt.Errorf("edges: %w", err)
	}
	w.written += len(w.pending)
	w.pending = w.pending[:0]
	return nil
}
