//go:build integration

package postgres_test

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	pgdrv "github.com/ideacrafterslabs/ctxt/internal/storage/postgres"
	"github.com/ideacrafterslabs/ctxt/internal/projection"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

// integrationDSN returns the Postgres DSN to use for integration tests.
//
// Resolution order:
//  1. POSTGRES_DSN — full DSN takes precedence (legacy/local override).
//  2. POSTGRES_HOST + POSTGRES_PORT + POSTGRES_USER + POSTGRES_PASSWORD +
//     POSTGRES_DB — the env block exported by .github/workflows/ci.yml and
//     standard for the postgres:16-alpine service container.
//
// Returns "" if neither is set, signalling the test should be skipped.
//
// Defaulting POSTGRES_USER (or any field) to the current OS user / "root"
// must be avoided: libpq's fallback to $USER causes "FATAL: role \"root\"
// does not exist" against the contexthelp role provisioned by CI.
func integrationDSN() string {
	if dsn := os.Getenv("POSTGRES_DSN"); dsn != "" {
		return dsn
	}
	host := os.Getenv("POSTGRES_HOST")
	user := os.Getenv("POSTGRES_USER")
	if host == "" || user == "" {
		return ""
	}
	port := os.Getenv("POSTGRES_PORT")
	if port == "" {
		port = "5432"
	}
	db := os.Getenv("POSTGRES_DB")
	if db == "" {
		db = user
	}
	u := &url.URL{
		Scheme: "postgres",
		Host:   fmt.Sprintf("%s:%s", host, port),
		Path:   "/" + db,
	}
	if pw := os.Getenv("POSTGRES_PASSWORD"); pw != "" {
		u.User = url.UserPassword(user, pw)
	} else {
		u.User = url.User(user)
	}
	q := u.Query()
	q.Set("sslmode", "disable")
	u.RawQuery = q.Encode()
	return u.String()
}

// newIntegrationDriver creates a Postgres driver for integration tests.
// Accepts either POSTGRES_DSN or the POSTGRES_HOST/PORT/USER/PASSWORD/DB
// env block (see integrationDSN).
func newIntegrationDriver(t *testing.T) *pgdrv.Driver {
	t.Helper()
	dsn := integrationDSN()
	if dsn == "" {
		t.Skip("POSTGRES_DSN (or POSTGRES_HOST + POSTGRES_USER) not set; skipping Postgres integration test")
	}
	drv, err := pgdrv.New(dsn)
	if err != nil {
		t.Fatalf("postgres.New: %v", err)
	}
	ctx := context.Background()
	if err := drv.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(func() { drv.Close(context.Background()) })
	return drv
}

func TestPostgres_GraphRoundtrip_Create(t *testing.T) {
	drv := newIntegrationDriver(t)
	ctx := context.Background()

	id := "pg-rw-1"
	ko := &pluginapi.KnowledgeObject{
		ID:        id,
		Type:      "note",
		Status:    "active",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
		Graph: &pluginapi.ObjectGraph{
			Nodes: []pluginapi.GraphNode{
				{
					ID:       pluginapi.NewNodeID(id, pluginapi.NodeTypeSection, 0),
					NodeType: pluginapi.NodeTypeSection,
					Label:    "Intro",
					Content:  "hello postgres",
					Order:    0,
				},
			},
		},
	}

	if err := drv.Objects().Create(ctx, ko); err != nil {
		t.Fatal(err)
	}
	got, err := drv.Objects().Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Graph == nil {
		t.Fatal("Graph not persisted after Create")
	}
	if len(got.Graph.Nodes) != 1 {
		t.Fatalf("want 1 node, got %d", len(got.Graph.Nodes))
	}
	if got.Graph.Nodes[0].Content != "hello postgres" {
		t.Errorf("node content mismatch: %q", got.Graph.Nodes[0].Content)
	}
	if got.Graph.Nodes[0].Label != "Intro" {
		t.Errorf("node label mismatch: %q", got.Graph.Nodes[0].Label)
	}

	// Verify ProjectDocument/ProjectIndex work on the returned object.
	doc := projection.ProjectDocument(got)
	if len(doc.Sections) == 0 {
		t.Error("ProjectDocument returned no sections from graph")
	}
	idx := projection.ProjectIndex(got)
	if idx.FTSBody == "" {
		t.Error("ProjectIndex returned empty FTSBody from graph")
	}
}

func TestPostgres_GraphRoundtrip_Update(t *testing.T) {
	drv := newIntegrationDriver(t)
	ctx := context.Background()

	id := "pg-rw-2"
	ko := &pluginapi.KnowledgeObject{
		ID: id, Type: "note", Status: "active",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := drv.Objects().Create(ctx, ko); err != nil {
		t.Fatal(err)
	}

	ko.Graph = &pluginapi.ObjectGraph{
		Nodes: []pluginapi.GraphNode{
			{
				ID:       pluginapi.NewNodeID(id, pluginapi.NodeTypeTag, 0),
				NodeType: pluginapi.NodeTypeTag,
				Label:    "go",
				Content:  "go",
			},
		},
	}
	ko.UpdatedAt = time.Now().UTC()
	if err := drv.Objects().Update(ctx, ko); err != nil {
		t.Fatal(err)
	}
	got, err := drv.Objects().Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Graph == nil || len(got.Graph.Nodes) == 0 {
		t.Fatal("updated graph not persisted")
	}
	if got.Graph.Nodes[0].Label != "go" {
		t.Errorf("updated node label: %q", got.Graph.Nodes[0].Label)
	}
}

func TestPostgres_GraphRoundtrip_NilGraph(t *testing.T) {
	drv := newIntegrationDriver(t)
	ctx := context.Background()

	id := "pg-nil-g"
	ko := &pluginapi.KnowledgeObject{
		ID: id, Type: "note", Status: "active",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		// Graph is nil
	}
	if err := drv.Objects().Create(ctx, ko); err != nil {
		t.Fatal(err)
	}
	got, err := drv.Objects().Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	// nil graph → stored as NULL or '{}' in DB; round-trips as nil or empty.
	if got.Graph != nil && len(got.Graph.Nodes) != 0 {
		t.Errorf("expected nil/empty Graph for object without graph, got %+v", got.Graph)
	}
}

func TestPostgres_ObjectNodes_UpsertOnCreate(t *testing.T) {
	drv := newIntegrationDriver(t)
	ctx := context.Background()

	id := "pg-rw-3"
	ko := &pluginapi.KnowledgeObject{
		ID: id, Type: "note", Status: "active",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		Graph: &pluginapi.ObjectGraph{
			Nodes: []pluginapi.GraphNode{
				{ID: pluginapi.NewNodeID(id, pluginapi.NodeTypeTag, 0),
					NodeType: pluginapi.NodeTypeTag, Label: "go", Content: "go"},
				{ID: pluginapi.NewNodeID(id, pluginapi.NodeTypeTag, 1),
					NodeType: pluginapi.NodeTypeTag, Label: "postgres", Content: "postgres"},
			},
		},
	}
	if err := drv.Objects().Create(ctx, ko); err != nil {
		t.Fatal(err)
	}

	rows, err := drv.DB().QueryContext(ctx,
		`SELECT node_type FROM object_nodes WHERE object_id = $1 ORDER BY ordinal`, id)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var types []string
	for rows.Next() {
		var nt string
		if err := rows.Scan(&nt); err != nil {
			t.Fatal(err)
		}
		types = append(types, nt)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(types) != 2 {
		t.Fatalf("want 2 object_nodes rows, got %d", len(types))
	}
}

func TestPostgres_MigrationAppliesCleanly(t *testing.T) {
	drv := newIntegrationDriver(t)
	ctx := context.Background()

	// Verify graph_json column exists.
	var colExists bool
	err := drv.DB().QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_name = 'objects' AND column_name = 'graph_json'
		)
	`).Scan(&colExists)
	if err != nil {
		t.Fatalf("check graph_json column: %v", err)
	}
	if !colExists {
		t.Error("graph_json column not found after migration")
	}

	// Verify object_nodes table exists.
	var tableExists bool
	err = drv.DB().QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_name = 'object_nodes'
		)
	`).Scan(&tableExists)
	if err != nil {
		t.Fatalf("check object_nodes table: %v", err)
	}
	if !tableExists {
		t.Error("object_nodes table not found after migration")
	}
}
