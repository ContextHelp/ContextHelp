package cmd

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/dpkmstest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"hop.top/kit/go/console/output"
)

func seedReprocessObject(t *testing.T, db *testDB) {
	t.Helper()
	seedObj(t, db, &storage.KnowledgeObject{ID: "obj_r", RawContent: "a body worth enriching"})
}

// reprocessJob returns the job the instance holds for id, failing when
// it is not a queued reprocess of step on obj_r.
func reprocessJob(t *testing.T, db *testDB, id, step string) {
	t.Helper()
	job, err := db.Driver.Jobs().Get(context.Background(), id)
	if err != nil {
		t.Fatalf("job %q: %v", id, err)
	}
	var p struct {
		ObjectID string `json:"object_id"`
		Step     string `json:"step"`
	}
	if err := json.Unmarshal([]byte(job.Payload), &p); err != nil {
		t.Fatal(err)
	}
	if job.Type != "object:reprocess" || job.Status != storage.JobPending || p.ObjectID != "obj_r" || p.Step != step {
		t.Errorf("job = %+v payload = %+v", job, p)
	}
}

// TestReprocessQueuesJobOverAPI: reprocess hands the step to the
// instance as a job and reports its ID; it runs nothing locally.
func TestReprocessQueuesJobOverAPI(t *testing.T) {
	db := setupTestDB(t)
	seedReprocessObject(t, db)
	local := pointStorageElsewhere(t, db)

	out, err := db.exec("reprocess", "obj_r", "--step", "tagger")
	if err != nil {
		t.Fatalf("reprocess: %v", err)
	}
	const marker = "job "
	i := strings.LastIndex(out, marker)
	if i < 0 {
		t.Fatalf("output names no job: %q", out)
	}
	reprocessJob(t, db, strings.TrimSpace(out[i+len(marker):]), "tagger")
	assertNoLocalStore(t, local)

	// The object is untouched until the instance's worker runs the job.
	if obj := storedObj(t, db, "obj_r"); len(obj.Tags) != 0 {
		t.Errorf("reprocess changed the object locally: %+v", obj.Tags)
	}
}

func TestReprocessJSON(t *testing.T) {
	db := setupTestDB(t)
	seedReprocessObject(t, db)
	out, err := db.exec("reprocess", "obj_r", "--format", "json")
	if err != nil {
		t.Fatalf("reprocess: %v", err)
	}
	var got struct {
		ID     string `json:"id"`
		Step   string `json:"step"`
		JobID  string `json:"job_id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode: %v\n%s", err, out)
	}
	if got.ID != "obj_r" || got.Step != "structured_metadata" || got.Status != "queued" || got.JobID == "" {
		t.Errorf("json = %+v", got)
	}
	reprocessJob(t, db, got.JobID, "structured_metadata")
}

func TestReprocessRolesAndErrors(t *testing.T) {
	db := setupTestDB(t, dpkmstest.WithStaticTokens())
	seedReprocessObject(t, db)

	db.useRole(t, dpkmstest.RoleWriter)
	if _, err := db.exec("reprocess", "obj_r", "--step", "entity_extractor"); err != nil {
		t.Fatalf("writer: %v", err)
	}
	cases := []struct {
		name string
		role string
		args []string
		exit int
	}{
		{"reader", dpkmstest.RoleReader, []string{"reprocess", "obj_r"}, output.ExitUnauthorized},
		{"no token", "none", []string{"reprocess", "obj_r"}, output.ExitUnauthorized},
		{"unknown step", dpkmstest.RoleWriter, []string{"reprocess", "obj_r", "--step", "summarizer"}, output.ExitUsage},
		{"missing object", dpkmstest.RoleWriter, []string{"reprocess", "obj_missing"}, output.ExitNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db.useRole(t, tc.role)
			_, err := db.exec(tc.args...)
			if exitOf(err) != tc.exit {
				t.Errorf("exit %d (%v), want %d", exitOf(err), err, tc.exit)
			}
		})
	}
}
