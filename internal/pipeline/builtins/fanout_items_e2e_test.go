package builtins

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

// fanOutJob is a container job as its entry point enqueues it, and the
// distinctive word each item it splits into carries.
type fanOutJob struct {
	payload string
	source  string
	words   []string
}

// Each fan-out gets a container whose items mention one animal apiece, so
// every item is findable on its own.
func fanOutJobs(t *testing.T) map[string]fanOutJob {
	t.Helper()
	dir := t.TempDir()

	slackDir := filepath.Join(dir, "slack")
	writeFile(t, filepath.Join(slackDir, "channels.json"), `[{"id":"C01","name":"wildlife"}]`)
	writeFile(t, filepath.Join(slackDir, "wildlife", "2024-07-01.json"), `[
  {"type":"message","user":"U01","username":"alice","text":"Spotted a bilby near the camp tonight.","ts":"1719830400.000100"},
  {"type":"message","user":"U02","username":"bob","text":"A wombat dug under the fence again.","ts":"1719830460.000200"},
  {"type":"message","user":"U01","username":"alice","text":"The echidna was back at the termite mound.","ts":"1719830520.000300"}
]`)

	discordFile := filepath.Join(dir, "discord.json")
	writeFile(t, discordFile, `{
  "guild": {"id": "1", "name": "Rangers"},
  "channel": {"id": "2", "name": "sightings", "type": "GuildTextChat"},
  "messages": [
    {"id": "31", "type": "Default", "timestamp": "2024-07-01T08:00:00.000+00:00",
     "content": "A platypus surfaced in the creek at dawn.",
     "author": {"id": "41", "name": "alice", "isBot": false},
     "attachments": [], "embeds": [], "reactions": [], "reference": {}},
    {"id": "32", "type": "Default", "timestamp": "2024-07-01T08:01:00.000+00:00",
     "content": "Two dingoes crossed the firebreak.",
     "author": {"id": "42", "name": "bob", "isBot": false},
     "attachments": [], "embeds": [], "reactions": [], "reference": {}}
  ]
}`)

	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(`<?xml version="1.0"?><rss version="2.0"><channel><title>Field notes</title>
<item><title>Cassowary</title><link>https://field.example/cassowary</link><guid>cassowary</guid><description>A cassowary crossed the rainforest track.</description></item>
<item><title>Bandicoot</title><link>https://field.example/bandicoot</link><guid>bandicoot</guid><description>A bandicoot foraged under the lantana.</description></item>
</channel></rss>`))
	}))
	t.Cleanup(feed.Close)

	mbox := "From alice@example.com Mon Jan 01 10:00:00 2024\n" +
		"From: alice@example.com\nTo: bob@example.com\nSubject: Survey one\n" +
		"Message-Id: <m1@example.com>\nDate: Mon, 01 Jan 2024 10:00:00 +0000\nContent-Type: text/plain\n\n" +
		"The dunnart count doubled along the ridge.\n\n" +
		"From carol@example.com Tue Jan 02 11:00:00 2024\n" +
		"From: carol@example.com\nTo: bob@example.com\nSubject: Survey two\n" +
		"Message-Id: <m2@example.com>\nDate: Tue, 02 Jan 2024 11:00:00 +0000\nContent-Type: text/plain\n\n" +
		"A quoll raided the bait station.\n"
	newsletter := "From: digest@wildlife.example\nTo: bob@example.com\nSubject: Weekly digest\n" +
		"List-Id: <digest.wildlife.example>\nMessage-Id: <n1@example.com>\n" +
		"Date: Wed, 03 Jan 2024 09:00:00 +0000\nContent-Type: text/plain\n\n" +
		"This week the numbat colony grew by four joeys.\n"
	billing := "From: billing@stripe.com\nTo: bob@example.com\nSubject: Your invoice for January\n" +
		"Message-Id: <b1@example.com>\nDate: Thu, 04 Jan 2024 09:00:00 +0000\nContent-Type: text/plain\n\n" +
		"Invoice for the potoroo tracking collars.\n"

	return map[string]fanOutJob{
		"import.slack": {
			payload: mustJSON(t, map[string]any{"slack_export_dir": slackDir}),
			source:  "import:slack",
			words:   []string{"bilby", "wombat", "echidna"},
		},
		"import.discord": {
			payload: mustJSON(t, map[string]any{"discord_export_file": discordFile}),
			source:  "import:discord",
			words:   []string{"platypus", "dingoes"},
		},
		"import.twitter": {
			payload: `window.YTD.tweets.part0 = [
  {"tweet": {"id_str": "101", "full_text": "Saw a kookaburra at the trailhead", "lang": "en", "created_at": "Mon Jan 02 15:04:05 +0000 2023"}},
  {"tweet": {"id_str": "102", "full_text": "A goanna sunning on the rocks", "lang": "en", "created_at": "Tue Jan 03 15:04:05 +0000 2023"}}
]`,
			source: "tweets.js",
			words:  []string{"kookaburra", "goanna"},
		},
		"import.linkedin.posts": {
			payload: "Date,ShareCommentary,ShareMediaCategory,SharedUrl\n" +
				"2023-03-15 10:30:00 UTC,Our team released a numbat recovery report.,ARTICLE,https://example.com/numbat\n" +
				"2023-03-16 10:30:00 UTC,Volunteers counted every quokka on the island.,NONE,\n",
			source: "Posts.csv",
			words:  []string{"numbat", "quokka"},
		},
		"import.linkedin.articles": {
			payload: "Title,Url,Description,PublishedAt\n" +
				"Saving the bilby,https://linkedin.com/pulse/bilby,How fences brought the bilby back.,2023-05-01\n" +
				"Wombat burrows,https://linkedin.com/pulse/wombat,What a wombat burrow shelters in a fire.,2023-05-02\n",
			source: "Articles.csv",
			words:  []string{"bilby", "wombat"},
		},
		"batch.jsonl": {
			payload: `{"content":"A sugar glider nested in the box.","source":"survey:1"}` + "\n" +
				`{"content":"The wallaby joey left the pouch.","source":"survey:2"}` + "\n",
			source: "records.jsonl",
			words:  []string{"glider", "wallaby"},
		},
		"batch.csv": {
			payload: "content,source\nA pademelon grazed at dusk.,survey:3\nA bettong dug for truffles.,survey:4\n",
			source:  "records.csv",
			words:   []string{"pademelon", "bettong"},
		},
		"batch.tsv": {
			payload: "content\tsource\nA possum raided the orchard.\tsurvey:5\nA koala slept in the gum tree.\tsurvey:6\n",
			source:  "records.tsv",
			words:   []string{"possum", "koala"},
		},
		"email.general":    {payload: mbox, source: "survey.mbox", words: []string{"dunnart", "quoll"}},
		"email.newsletter": {payload: newsletter, source: "digest.eml", words: []string{"numbat"}},
		"email.billing":    {payload: billing, source: "invoice.eml", words: []string{"potoroo"}},
		"feed.sync":        {payload: feed.URL, source: feed.URL, words: []string{"cassowary", "bandicoot"}},
	}
}

// Every fan-out pipeline, run through the real worker on a real SQLite
// store, turns a container of N items into N item objects of their own:
// each embedded by every populating model, found by full-text search, and
// named by a contains edge on its container's graph.
func TestFanOutItemsBecomeEmbeddedObjects(t *testing.T) {
	cases := fanOutJobs(t)
	for name := range fanOutPipelines {
		if name == "dropbox.sync" {
			continue // needs an authenticated download per file
		}
		if _, ok := cases[name]; !ok {
			t.Errorf("%s: no fan-out case", name)
		}
	}
	names := make([]string, 0, len(cases))
	for name := range cases {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		tc := cases[name]
		t.Run(name, func(t *testing.T) { requireFanOut(t, name, tc) })
	}
}

func requireFanOut(t *testing.T, name string, tc fanOutJob) {
	driver := storageutil.NewTestDriver(t)
	done := runContainer(t, driver, name, tc)
	parent, items := splitObjects(t, driver, done.ResultID)
	if len(items) != len(tc.words) {
		t.Fatalf("%d item objects, want %d (one per item)", len(items), len(tc.words))
	}
	requireVectors1024(t, driver, items)
	requireSearchable(t, driver, done.ResultID, tc.words)
	requireContainsItems(t, parent, items)
}

// runContainer registers the embedding models, enqueues the container job
// and drains the queue through a real worker; every job must complete.
func runContainer(t *testing.T, driver storage.StorageDriver, name string, tc fanOutJob) *storage.Job {
	t.Helper()
	ctx := context.Background()
	reg, err := registry.ForDriver(driver)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range wiringModels() {
		if err := reg.Register(ctx, m, m.IsDefault); err != nil {
			t.Fatalf("register %s: %v", m.ModelID, err)
		}
		if err := driver.Embeddings().EnsureIndex(ctx, embeddings.SpecFor(m)); err != nil {
			t.Fatalf("EnsureIndex(%s): %v", m.ModelID, err)
		}
	}
	pipes := ConfiguredRegistryWithOpts(BuildOpts{
		Models:     reg,
		Resolver:   wiringResolver(t),
		Embeddings: driver.Embeddings(),
	})
	q := jobs.NewQueue(driver.Jobs())
	now := time.Now().Truncate(time.Second)
	container := &storage.Job{
		ID: "container", Type: "ingest:" + name, Status: storage.JobPending,
		Payload: tc.payload, Pipeline: name, Source: tc.source,
		MaxRetries: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := q.Enqueue(ctx, container); err != nil {
		t.Fatal(err)
	}
	drain(t, jobs.NewWorkerPool(q, pipes, driver, 1, nil, config.JobsConfig{
		PollInterval: 20 * time.Millisecond, StaleTimeout: time.Minute, MaxRetries: 1, MaxHops: 5,
	}), driver.Jobs())

	done, err := q.Get(ctx, container.ID)
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != storage.JobCompleted {
		t.Fatalf("container job %s (err=%q)", done.Status, done.Error)
	}
	all, _, err := driver.Jobs().List(ctx, storage.JobFilter{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	for _, j := range all {
		if j.Status != storage.JobCompleted {
			t.Errorf("job %s on %s: %s (err=%q)", j.ID, j.Pipeline, j.Status, j.Error)
		}
	}
	return done
}

// splitObjects returns the stored container and every other object.
func splitObjects(t *testing.T, driver storage.StorageDriver, containerID string) (*storage.KnowledgeObject, []*storage.KnowledgeObject) {
	t.Helper()
	objs, _, err := driver.Objects().List(context.Background(), storage.ObjectFilter{Status: "all", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	var parent *storage.KnowledgeObject
	var items []*storage.KnowledgeObject
	for _, o := range objs {
		if o.ID == containerID {
			parent = o
			continue
		}
		items = append(items, o)
	}
	return parent, items
}

func requireVectors1024(t *testing.T, driver storage.StorageDriver, items []*storage.KnowledgeObject) {
	t.Helper()
	for _, it := range items {
		for _, m := range wiringModels() {
			rows, err := driver.Embeddings().Get(context.Background(), it.ID, m.ModelID)
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) == 0 || len(rows[0].Vector) != 1024 {
				t.Errorf("item %s (%s): no 1024-dim %s vector", it.ID, it.Pipeline, m.ModelID)
			}
		}
	}
}

// requireSearchable checks full-text search finds an item, not only the
// container, for each word.
func requireSearchable(t *testing.T, driver storage.StorageDriver, containerID string, words []string) {
	t.Helper()
	for _, w := range words {
		hits, err := driver.Objects().FTSSearch(context.Background(), w, storage.ObjectFilter{Status: "all", Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, h := range hits {
			found = found || h.ID != containerID
		}
		if !found {
			t.Errorf("no item object found searching %q", w)
		}
	}
}

// requireContainsItems checks the container's graph names each item by
// source through a contains edge.
func requireContainsItems(t *testing.T, parent *storage.KnowledgeObject, items []*storage.KnowledgeObject) {
	t.Helper()
	if parent == nil || parent.Graph == nil {
		t.Fatal("container object has no graph")
	}
	contained := map[string]bool{}
	for _, e := range parent.Graph.Edges {
		if e.EdgeType == pluginapi.EdgeTypeContains && e.FromID == parent.ID {
			contained[e.ToID] = true
		}
	}
	var got []string
	for _, n := range parent.Graph.Nodes {
		if n.NodeType == pluginapi.NodeTypeArtifact && contained[n.ID] {
			src, _ := n.Metadata["source"].(string)
			got = append(got, src)
		}
	}
	want := make([]string, 0, len(items))
	for _, it := range items {
		want = append(want, it.Source)
	}
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("container contains %q, want the item sources %q", got, want)
	}
}

// drain runs the pool until no job is pending or running. Start returns
// only after in-flight jobs finish, fan-out enqueues included, so a round
// that ends with nothing pending leaves the queue settled.
func drain(t *testing.T, pool *jobs.WorkerPool, store storage.JobStore) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			defer cancel()
			for ctx.Err() == nil {
				if !busy(store) {
					return
				}
				time.Sleep(20 * time.Millisecond)
			}
		}()
		_ = pool.Start(ctx)
		cancel()
		if !busy(store) {
			return
		}
	}
	t.Fatal("jobs still pending after 60s")
}

func busy(store storage.JobStore) bool {
	for _, s := range []storage.JobStatus{storage.JobPending, storage.JobRunning} {
		_, n, err := store.List(context.Background(), storage.JobFilter{Status: s, Limit: 1})
		if err != nil || n > 0 {
			return true
		}
	}
	return false
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
