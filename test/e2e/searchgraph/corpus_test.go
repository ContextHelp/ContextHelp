//go:build e2e && unix

package searchgraph_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/embeddings"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob/local"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// fixtureModel is the default embedding model the corpus vectors live
// under. Its registry entry fixes backend and model; the endpoint comes
// from each test's config (a closed port, or the fake Ollama).
var fixtureModel = registry.Model{
	ModelID:    "fixture-e2e-3",
	Provider:   "ollama",
	Dimension:  3,
	ConfigJSON: `{"backend":"ollama","model":"fixture-embed"}`,
}

// fixtureObject is one knowledge object of the fixture corpus.
type fixtureObject struct {
	alias    string // stable name; replaces the random object id in goldens
	content  string
	mentions string // --mention value; "" for none
	// embedding is stored under fixtureModel so the hybrid scenarios have
	// a vector leg; nil stores none.
	embedding []float32
}

// fixtureLink is one typed link, as `ctxt link create` asks dpkms to
// store it (POST /api/v1/edges).
type fixtureLink struct{ from, typ, to string }

// The corpus is shaped so one query exercises every graph feature:
//
//   - "deployment" matches five objects through FTS; budget only matches
//     through the vector leg (hybrid scenarios).
//   - Every linked object gets the same backlink boost; runbook,
//     postmortem and rollback also mention entities, so their mention
//     boost clears --min-score 0.1 while freeze and glossary mention
//     nothing and fall below it (cut_threshold). With --limit 2 one of
//     the three boosted objects is cut by the limit (cut_limit).
//   - runbook and rollback share project/atlas, runbook and postmortem
//     share person/alice-chen: two co_mention edges.
//   - rollback extends runbook: the stored forward/inverse pair collapses
//     to one extends edge. glossary superseded-by freeze is entered with
//     the inverse name and collapses to freeze supersedes glossary.
//     postmortem related-to runbook is symmetric (undirected).
//     postmortem supports budget: budget is no FTS candidate, so the
//     link is absent from FTS-only graphs (depth 1).
var fixtureObjects = []fixtureObject{
	{
		alias:     "runbook",
		content:   "Deployment runbook for the atlas cluster: deployment order, health checks and rollback triggers",
		mentions:  "@project.atlas @person.alice-chen",
		embedding: []float32{1, 0, 0},
	},
	{
		alias:     "rollback",
		content:   "Rollback checklist for a failed atlas deployment",
		mentions:  "@project.atlas",
		embedding: []float32{0.8, 0.6, 0},
	},
	{
		alias:     "postmortem",
		content:   "Postmortem of the billing service deployment outage",
		mentions:  "@person.alice-chen @team.sre",
		embedding: []float32{0.6, 0.8, 0},
	},
	{
		alias:     "freeze",
		content:   "Deployment freeze calendar",
		embedding: []float32{0, 1, 0},
	},
	{
		alias:   "glossary",
		content: "Glossary of platform terms used in every deployment review and every deployment retrospective",
	},
	{
		alias:     "budget",
		content:   "Quarterly budget review for the platform team",
		mentions:  "@team.sre",
		embedding: []float32{0.9, 0, 0.436},
	},
}

var fixtureLinks = []fixtureLink{
	{"rollback", "extends", "runbook"},
	{"glossary", "superseded-by", "freeze"},
	{"postmortem", "related-to", "runbook"},
	{"postmortem", "supports", "budget"},
}

// corpus is the seeded fixture: a database file every test copies, and
// the id assigned to each alias.
type corpus struct {
	db  string
	ids map[string]string // alias -> object id
}

// seedCorpus builds the fixture database. Objects go in through the
// service's raw analyze path, what `ctxt analyze --raw --mention` asks
// dpkms to run (ctxt itself never writes a store), so each object keeps
// its entity mentions. Each typed link then goes in with its inverse
// through the service's link path, what `ctxt link create` asks dpkms to
// run. The default model and its vectors go in through the storage
// driver: registering through the CLI probes a live provider, and
// vectors are written by the dpkms migration job.
func seedCorpus(dir string) (*corpus, error) {
	h, err := newHome(dir, closedURL(), closedURL())
	if err != nil {
		return nil, err
	}

	c := &corpus{db: h.db, ids: map[string]string{}}
	if err := storeObjects(c); err != nil {
		return nil, err
	}
	if err := storeEmbeddings(c); err != nil {
		return nil, err
	}
	return c, nil
}

// corpusBlobs is the corpus's blob store, beside its database.
func corpusBlobs(c *corpus) storage.BlobStore {
	bs, err := local.New(filepath.Join(filepath.Dir(c.db), "blobs"))
	if err != nil {
		panic(err)
	}
	return bs
}

// storeObjects stores each fixture object through the service's raw
// analyze path as `ctxt analyze --raw` stored it (source "argument"),
// with its --mention value split as the CLI splits it, then each fixture
// link through the service's link path.
func storeObjects(c *corpus) error {
	ctx := context.Background()
	if err := os.MkdirAll(filepath.Dir(c.db), 0o700); err != nil {
		return err
	}
	d, err := storageutil.NewDriver("sqlite", c.db, corpusBlobs(c))
	if err != nil {
		return err
	}
	if err := d.Init(ctx); err != nil {
		return err
	}
	defer d.Close(ctx)
	svc := service.NewWithOptions(d, jobs.NewQueue(d.Jobs()), pipeline.DefaultRegistry(), search.NewEngine(d), "", nil, nil)
	for _, o := range fixtureObjects {
		id, err := svc.Analyze(ctx, service.AnalyzeRequest{
			Content: o.content, Type: "text", Source: "argument", Raw: true, NoFanout: true,
			Mentions: strings.Fields(o.mentions),
		})
		if err != nil {
			return fmt.Errorf("store %s: %w", o.alias, err)
		}
		c.ids[o.alias] = id
	}
	for _, l := range fixtureLinks {
		if _, err := svc.CreateLink(ctx, service.LinkRequest{Source: c.ids[l.from], Target: c.ids[l.to], Type: l.typ}); err != nil {
			return fmt.Errorf("link %s -[%s]-> %s: %w", l.from, l.typ, l.to, err)
		}
	}
	return nil
}

// storeEmbeddings registers fixtureModel as the default, builds its index
// and writes each fixture embedding as its object's single chunk.
func storeEmbeddings(c *corpus) error {
	ctx := context.Background()
	d, err := storageutil.NewDriver("sqlite", c.db, corpusBlobs(c))
	if err != nil {
		return err
	}
	if err := d.Init(ctx); err != nil {
		return err
	}
	defer d.Close(ctx)
	reg, err := registry.ForDriver(d)
	if err != nil {
		return err
	}
	if err := reg.Register(ctx, fixtureModel, true); err != nil {
		return fmt.Errorf("register %s: %w", fixtureModel.ModelID, err)
	}
	if err := d.Embeddings().EnsureIndex(ctx, embeddings.SpecFor(fixtureModel)); err != nil {
		return fmt.Errorf("index %s: %w", fixtureModel.ModelID, err)
	}
	for _, o := range fixtureObjects {
		if o.embedding == nil {
			continue
		}
		v := []storage.ObjectVector{{ModelID: fixtureModel.ModelID, Vector: o.embedding}}
		if err := d.Embeddings().Put(ctx, c.ids[o.alias], v); err != nil {
			return fmt.Errorf("store embedding for %s: %w", o.alias, err)
		}
	}
	return nil
}

// copyDB copies the seeded database to dst.
func (c *corpus) copyDB(dst string) error {
	b, err := os.ReadFile(c.db)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o600)
}

// alias rewrites every object id in s to its fixture alias.
func (c *corpus) alias(s string) string {
	for a, id := range c.ids {
		s = strings.ReplaceAll(s, id, a)
	}
	return s
}
