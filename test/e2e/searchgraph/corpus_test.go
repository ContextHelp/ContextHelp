//go:build e2e && unix

package searchgraph_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/embeddings"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
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

// fixtureLink is one `ctxt link create` call.
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

var jobIDLine = regexp.MustCompile(`(?m)^Job ID: ([0-9a-f-]{36})$`)

// seedCorpus builds the fixture database through the binary itself:
// `analyze --raw --mention` stores each object with its entity mentions
// (the dpkms server is unreachable, so the CLI takes its local direct
// path) and `link create` stores each typed link with its inverse. Only
// the default model and its vectors go in through the storage driver:
// registering through the CLI probes a live provider, and vectors are
// written by the dpkms migration job.
func seedCorpus(bin, dir string) (*corpus, error) {
	h, err := newHome(dir, closedURL(), closedURL())
	if err != nil {
		return nil, err
	}
	run := func(args ...string) (string, error) {
		cmd := exec.Command(bin, args...)
		cmd.Env = h.environ()
		var out, errb bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errb
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("ctxt %s: %w\nstdout: %s\nstderr: %s", strings.Join(args, " "), err, out.String(), errb.String())
		}
		return out.String(), nil
	}

	c := &corpus{db: h.db, ids: map[string]string{}}
	for _, o := range fixtureObjects {
		args := []string{"analyze", "--raw"}
		if o.mentions != "" {
			args = append(args, "--mention", o.mentions)
		}
		out, err := run(append(args, o.content)...)
		if err != nil {
			return nil, err
		}
		m := jobIDLine.FindStringSubmatch(out)
		if m == nil {
			return nil, fmt.Errorf("analyze %s: no object id in output %q", o.alias, out)
		}
		c.ids[o.alias] = m[1]
	}
	for _, l := range fixtureLinks {
		if _, err := run("link", "create", c.ids[l.from], c.ids[l.to], "--type", l.typ); err != nil {
			return nil, err
		}
	}
	if err := storeEmbeddings(c); err != nil {
		return nil, err
	}
	return c, nil
}

// storeEmbeddings registers fixtureModel as the default, builds its index
// and writes each fixture embedding as its object's single chunk.
func storeEmbeddings(c *corpus) error {
	ctx := context.Background()
	d, err := storageutil.NewDriver("sqlite", c.db)
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
