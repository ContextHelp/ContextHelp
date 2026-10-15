package stack

import (
	"context"
	"database/sql"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/steps"
	"github.com/ideacrafterslabs/ctxt/internal/projection"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// contactDraft is a contact as cardamum ingests it, under the personal
// profile and tagged for the configured rule: the card text, the
// details in the metadata, the note as a personal section node.
func contactDraft() *storage.KnowledgeObject {
	card := "Sarah Lee | Head of Ops @ Acme\nLisbon"
	d := &storage.KnowledgeObject{
		ID: "wiring-sarah", Type: projection.TypeContact, RawContent: card, TextContent: card,
		Status: storage.ObjectStatusActive, ProfileID: "personal", Source: "cardamum",
		Metadata: map[string]any{"name": "Sarah Lee", "org": "Acme", "role": "Head of Ops", "city": "Lisbon", "email": "sarah@acme.example"},
		Tags:     []storage.Tag{{Label: "invoices"}},
		Graph: &storage.ObjectGraph{Nodes: []storage.GraphNode{
			{ID: "wiring-sarah/section/0", NodeType: storage.NodeTypeSection, Label: projection.ContactCardLabel, Content: card},
			{ID: "wiring-sarah/section/1", NodeType: storage.NodeTypeSection, Label: projection.ContactNoteLabel, Content: "kids Leo and Mia"},
		}},
		CreatedAt: time.Now().UTC().Truncate(time.Second), UpdatedAt: time.Now().UTC().Truncate(time.Second),
	}
	storage.SetNodeClasses(&d.Graph.Nodes[1], []string{storage.ClassPersonal})
	return d
}

// withClassRules adds one configured rule beside the defaults.
func withClassRules(cfg *config.Config) *config.Config {
	cfg.Sharing.Classes.Rules = []config.ClassRuleConfig{{Class: "financial", Tag: "invoices"}}
	return cfg
}

// textShortSteps returns the text.short pipeline's steps by name.
func textShortSteps(t *testing.T, st *Stack) map[string]pipeline.PipelineStep {
	t.Helper()
	p, err := st.Pipelines.Get("text.short")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]pipeline.PipelineStep{}
	for _, s := range p.Steps {
		out[s.Name()] = s
	}
	return out
}

// maskedVariantOn reads the embedding step's masked-variant switch, the
// one builtins sets from BuildOpts.Access (steps.WithMaskedVariant).
func maskedVariantOn(t *testing.T, step pipeline.PipelineStep) bool {
	t.Helper()
	gen, ok := step.(*steps.EmbeddingGenerator)
	if !ok {
		t.Fatalf("embedding step is %T, want *steps.EmbeddingGenerator", step)
	}
	return reflect.ValueOf(gen).Elem().FieldByName("masked").Bool()
}

// A stack built for a protected instance runs content_classes with the
// configured rules: a contact under the personal profile, tagged
// invoices, is classed personal and financial, its stored row carries
// the classes and the masked body of the searchable minimum, and the
// embedding step writes the masked variant.
func TestBuild_ProtectedInstanceClassesAndMasks(t *testing.T) {
	ctx := context.Background()
	driver := storageutil.NewTestDriver(t)
	ownerKey(t, driver) // a protected instance needs an owner credential
	st := build(t, withClassRules(protectedCfg()), driver)
	byName := textShortSteps(t, st)

	classify, ok := byName["content_classes"]
	if !ok {
		t.Fatalf("protected text.short runs no content_classes step: %v", slices.Sorted(mapsKeys(byName)))
	}
	draft, err := classify.Run(ctx, contactDraft())
	if err != nil {
		t.Fatalf("content_classes: %v", err)
	}
	if !slices.Equal(draft.Classes, []string{"financial", storage.ClassPersonal}) {
		t.Fatalf("classes = %v, want financial, personal (configured rule and the personal-profile default)", draft.Classes)
	}
	if !maskedVariantOn(t, byName["embedding_generator"]) {
		t.Error("embedding step does not write the masked variant on a protected instance")
	}

	if err := driver.Objects().Create(ctx, draft); err != nil {
		t.Fatal(err)
	}
	stored, err := driver.Objects().Get(ctx, draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(stored.Classes, []string{"financial", storage.ClassPersonal}) {
		t.Errorf("stored classes = %v", stored.Classes)
	}
	masked := maskedBody(t, driver, draft.ID)
	if want := "Sarah Lee | Head of Ops @ Acme\nLisbon\nTags: invoices"; masked != want {
		t.Errorf("masked body = %q, want the searchable minimum %q", masked, want)
	}
}

// A private instance shares nothing: no content_classes step, no
// classes on the same contact, no masked variant.
func TestBuild_PrivateInstanceClassesNothing(t *testing.T) {
	ctx := context.Background()
	driver := storageutil.NewTestDriver(t)
	st := build(t, withClassRules(&config.Config{}), driver)
	byName := textShortSteps(t, st)

	if _, ok := byName["content_classes"]; ok {
		t.Fatal("private text.short runs content_classes")
	}
	if maskedVariantOn(t, byName["embedding_generator"]) {
		t.Error("embedding step writes the masked variant on a private instance")
	}
	draft := contactDraft()
	if err := driver.Objects().Create(ctx, draft); err != nil {
		t.Fatal(err)
	}
	stored, err := driver.Objects().Get(ctx, draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Classes != nil {
		t.Errorf("stored classes = %v, want none", stored.Classes)
	}
}

// maskedBody reads the stored masked FTS body of object id.
func maskedBody(t *testing.T, driver storage.StorageDriver, id string) string {
	t.Helper()
	db, ok := driver.(interface{ DB() *sql.DB })
	if !ok {
		t.Fatalf("driver %T exposes no DB()", driver)
	}
	var body string
	if err := db.DB().QueryRow(`SELECT projected_fts_body_masked FROM objects WHERE id = ?`, id).Scan(&body); err != nil {
		t.Fatalf("read masked body: %v", err)
	}
	return body
}

func mapsKeys[V any](m map[string]V) func(func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}
