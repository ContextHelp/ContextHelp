package cmd

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"hop.top/kit/go/console/output"

	"github.com/ideacrafterslabs/ctxt/internal/dpkmstest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func seedRawObject(t *testing.T, db *testDB, id, content string) {
	t.Helper()
	now := time.Now().Truncate(time.Second)
	obj := &storage.KnowledgeObject{
		ID:         id,
		Type:       "text",
		RawContent: content,
		Status:     "raw",
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := db.Driver.Objects().Create(context.Background(), obj); err != nil {
		t.Fatalf("seed raw object: %v", err)
	}
}

func seedInboxItem(t *testing.T, db *testDB, id, content string) {
	t.Helper()
	now := time.Now().Truncate(time.Second)
	obj := &storage.KnowledgeObject{
		ID:         id,
		Type:       "text",
		RawContent: content,
		Status:     "inbox",
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := db.Driver.Objects().Create(context.Background(), obj); err != nil {
		t.Fatalf("seed inbox item: %v", err)
	}
}

func TestInboxList(t *testing.T) {
	db := setupTestDB(t)
	seedInboxItem(t, db, "inbox_001", "hello world")
	seedInboxItem(t, db, "inbox_002", "another item")

	out, err := db.exec("inbox", "list")
	if err != nil {
		t.Fatalf("inbox list should succeed: %v", err)
	}
	if !strings.Contains(out, "inbox_001") {
		t.Error("output should contain inbox_001")
	}
	if !strings.Contains(out, "inbox_002") {
		t.Error("output should contain inbox_002")
	}
}

func TestInboxListEmpty(t *testing.T) {
	db := setupTestDB(t)

	out, err := db.exec("inbox", "list")
	if err != nil {
		t.Fatalf("inbox list empty should succeed: %v", err)
	}
	if !strings.Contains(out, "0 total") {
		t.Errorf("output should show 0 total, got: %s", out)
	}
}

func TestInboxDiscard(t *testing.T) {
	db := setupTestDB(t)
	seedInboxItem(t, db, "inbox_dis_01", "to be discarded")

	out, err := db.exec("inbox", "discard",
		"--confirm=yes", "inbox_dis_01")
	if err != nil {
		t.Fatalf("inbox discard should succeed: %v", err)
	}
	if !strings.Contains(out, "inbox_dis_01") {
		t.Errorf("output should mention the discarded ID, got: %s", out)
	}

	// Verify it no longer appears in inbox list
	listOut, err := db.exec("inbox", "list")
	if err != nil {
		t.Fatalf("inbox list should succeed: %v", err)
	}
	if strings.Contains(listOut, "inbox_dis_01") {
		t.Error("discarded item should not appear in inbox list")
	}
}

func TestInboxDiscardNotFound(t *testing.T) {
	db := setupTestDB(t)

	_, err := db.exec("inbox", "discard",
		"--confirm=yes", "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent item")
	}
}

func TestInboxClear(t *testing.T) {
	db := setupTestDB(t)
	seedInboxItem(t, db, "inbox_clr_01", "item one")
	seedInboxItem(t, db, "inbox_clr_02", "item two")

	out, err := db.exec("inbox", "clear",
		"--confirm=yes")
	if err != nil {
		t.Fatalf("inbox clear should succeed: %v", err)
	}
	if !strings.Contains(out, "2") {
		t.Errorf("output should mention 2 cleared items, got: %s", out)
	}

	// Verify inbox is empty
	listOut, err := db.exec("inbox", "list")
	if err != nil {
		t.Fatalf("inbox list should succeed: %v", err)
	}
	if !strings.Contains(listOut, "0 total") {
		t.Errorf("inbox should be empty after clear, got: %s", listOut)
	}
}

func TestInboxHelp(t *testing.T) {
	out, err := executeCommand("inbox", "--help")
	if err != nil {
		t.Fatalf("inbox --help should succeed: %v", err)
	}
	for _, sub := range []string{"list", "triage", "discard", "clear"} {
		if !strings.Contains(out, sub) {
			t.Errorf("inbox help should list subcommand %s", sub)
		}
	}
}

func TestInboxListRawFlag(t *testing.T) {
	db := setupTestDB(t)
	seedRawObject(t, db, "raw_obj_01", "unenriched content")

	out, err := db.exec("inbox", "list", "--raw")
	if err != nil {
		t.Fatalf("inbox list --raw should succeed: %v", err)
	}
	if !strings.Contains(out, "raw_obj_01") {
		t.Errorf("output should contain raw object ID, got: %s", out)
	}
	if !strings.Contains(out, "raw") {
		t.Errorf("output should show raw status, got: %s", out)
	}
}

func TestInboxListPendingFlag(t *testing.T) {
	db := setupTestDB(t)
	seedJob(t, db, "job_pend_01", "ingest:text", "text.short", storage.JobPending)

	out, err := db.exec("inbox", "list", "--pending")
	if err != nil {
		t.Fatalf("inbox list --pending should succeed: %v", err)
	}
	if !strings.Contains(out, "job_pend_01") {
		t.Errorf("output should contain pending job ID, got: %s", out)
	}
	if !strings.Contains(out, "pending") {
		t.Errorf("output should show pending status, got: %s", out)
	}
}

func TestInboxListFailedFlag(t *testing.T) {
	db := setupTestDB(t)
	seedJob(t, db, "job_fail_01", "ingest:url", "url.generic", storage.JobFailed)

	out, err := db.exec("inbox", "list", "--failed")
	if err != nil {
		t.Fatalf("inbox list --failed should succeed: %v", err)
	}
	if !strings.Contains(out, "job_fail_01") {
		t.Errorf("output should contain failed job ID, got: %s", out)
	}
}

func TestInboxListQueueEmpty(t *testing.T) {
	db := setupTestDB(t)

	out, err := db.exec("inbox", "list", "--raw")
	if err != nil {
		t.Fatalf("inbox list --raw on empty db should succeed: %v", err)
	}
	if !strings.Contains(out, "0 total") && !strings.Contains(out, "No results") {
		t.Errorf("output should indicate empty result, got: %s", out)
	}
}

func TestInboxListQueueHeader(t *testing.T) {
	db := setupTestDB(t)
	seedRawObject(t, db, "raw_hdr_01", "header test")

	out, err := db.exec("inbox", "list", "--raw")
	if err != nil {
		t.Fatalf("inbox list --raw should succeed: %v", err)
	}
	if !strings.Contains(out, "Inbox queue") {
		t.Errorf("output should contain Inbox queue header, got: %s", out)
	}
}

func inboxStatus(t *testing.T, db *testDB, id string) string {
	t.Helper()
	obj, err := db.Driver.Objects().Get(context.Background(), id)
	if err != nil {
		t.Fatalf("get %s: %v", id, err)
	}
	return obj.Status
}

// A reader lists the inbox and its queue; writer and reader tokens are
// refused triage, discard and clear with exit 5 and change nothing; an
// admin token does all of it.
func TestInboxOverAPIRoles(t *testing.T) {
	db := setupTestDB(t, dpkmstest.WithStaticTokens())
	seedInboxItem(t, db, "inbox_role_01", "first")
	seedInboxItem(t, db, "inbox_role_02", "second")
	seedRawObject(t, db, "raw_role_01", "raw")

	db.useRole(t, dpkmstest.RoleReader)
	out, err := db.exec("inbox", "list")
	if err != nil || !strings.Contains(out, "inbox_role_01") || !strings.Contains(out, "2 total") {
		t.Fatalf("reader inbox list: %v\n%s", err, out)
	}
	out, err = db.exec("inbox", "list", "--raw")
	if err != nil || !strings.Contains(out, "raw_role_01") {
		t.Fatalf("reader inbox list --raw: %v\n%s", err, out)
	}

	for _, role := range []string{dpkmstest.RoleWriter, dpkmstest.RoleReader} {
		db.useRole(t, role)
		for _, args := range [][]string{
			{"inbox", "triage", "inbox_role_01"},
			{"inbox", "discard", "--confirm=yes", "inbox_role_01"},
			{"inbox", "clear", "--confirm=yes"},
		} {
			out, err := db.exec(args...)
			if got := ExitCodeFor(err); got != output.ExitUnauthorized {
				t.Errorf("%s %v: exit %d (%v); want %d\n%s", role, args, got, err, output.ExitUnauthorized, out)
			}
		}
	}
	for _, id := range []string{"inbox_role_01", "inbox_role_02"} {
		if s := inboxStatus(t, db, id); s != "inbox" {
			t.Fatalf("a refused command changed %s: %q", id, s)
		}
	}

	db.useRole(t, dpkmstest.RoleAdmin)
	out, err = db.exec("inbox", "triage", "inbox_role_01", "--pipeline", "text.short")
	if err != nil || !strings.Contains(out, "inbox_role_01") {
		t.Fatalf("admin triage: %v\n%s", err, out)
	}
	if s := inboxStatus(t, db, "inbox_role_01"); s != "active" {
		t.Fatalf("triaged item status %q", s)
	}
	out, err = db.exec("inbox", "clear", "--confirm=yes")
	if err != nil || !strings.Contains(out, "Cleared 1 inbox item(s)") {
		t.Fatalf("admin clear: %v\n%s", err, out)
	}
	if s := inboxStatus(t, db, "inbox_role_02"); s != "discarded" {
		t.Fatalf("cleared item status %q", s)
	}
	if s := inboxStatus(t, db, "raw_role_01"); s != "raw" {
		t.Fatalf("clear touched a raw object: %q", s)
	}
}

// Without a token a protected instance answers 401: exit 5.
func TestInboxNoTokenIsUnauthorized(t *testing.T) {
	db := setupTestDB(t, dpkmstest.WithStaticTokens())
	db.useRole(t, "none")
	for _, args := range [][]string{{"inbox", "list"}, {"inbox", "list", "--pending"}} {
		out, err := db.exec(args...)
		if got := ExitCodeFor(err); got != output.ExitUnauthorized {
			t.Errorf("%v: exit %d (%v); want %d\n%s", args, got, err, output.ExitUnauthorized, out)
		}
	}
}

// Nothing answering is exit 70: no command reads or writes the local
// store instead.
func TestInboxUnreachableHasNoLocalFallback(t *testing.T) {
	db := setupTestDB(t, dpkmstest.Unreachable())
	seedInboxItem(t, db, "inbox_down_01", "local only")
	for _, args := range [][]string{
		{"inbox", "list"},
		{"inbox", "list", "--failed"},
		{"inbox", "triage", "inbox_down_01"},
		{"inbox", "discard", "--confirm=yes", "inbox_down_01"},
		{"inbox", "clear", "--confirm=yes"},
	} {
		out, err := db.exec(args...)
		if got := ExitCodeFor(err); got != output.ExitPrerequisite {
			t.Errorf("%v: exit %d (%v); want %d\n%s", args, got, err, output.ExitPrerequisite, out)
		}
		if strings.Contains(out, "inbox_down_01") {
			t.Errorf("%v read the local store:\n%s", args, out)
		}
	}
	if s := inboxStatus(t, db, "inbox_down_01"); s != "inbox" {
		t.Fatalf("an unreachable instance still changed the local store: %q", s)
	}
}

// --before and --after reach the server; a malformed value is a usage
// error before any request.
func TestInboxListTimeFilters(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	for id, created := range map[string]string{"inbox_t_old": "2026-04-10T09:00:00Z", "inbox_t_new": "2026-04-20T09:00:00Z"} {
		at, _ := time.Parse(time.RFC3339, created)
		if err := db.Driver.Objects().Create(ctx, &storage.KnowledgeObject{
			ID: id, Type: "text", Status: "inbox", CreatedAt: at, UpdatedAt: at,
		}); err != nil {
			t.Fatal(err)
		}
	}
	out, err := db.exec("inbox", "list", "--after", "2026-04-15T00:00:00Z")
	if err != nil || !strings.Contains(out, "inbox_t_new") || strings.Contains(out, "inbox_t_old") {
		t.Fatalf("--after: %v\n%s", err, out)
	}
	out, err = db.exec("inbox", "list", "--before", "2026-04-15T00:00:00Z")
	if err != nil || !strings.Contains(out, "inbox_t_old") || strings.Contains(out, "inbox_t_new") {
		t.Fatalf("--before: %v\n%s", err, out)
	}
	out, err = db.exec("inbox", "list", "--before", "yesterday")
	if got := ExitCodeFor(err); got != output.ExitUsage {
		t.Fatalf("--before yesterday: exit %d (%v); want %d\n%s", got, err, output.ExitUsage, out)
	}
}

// A missing item is exit 3.
func TestInboxMissingItemIsNotFound(t *testing.T) {
	db := setupTestDB(t)
	for _, args := range [][]string{
		{"inbox", "triage", "nonexistent"},
		{"inbox", "discard", "--confirm=yes", "nonexistent"},
	} {
		out, err := db.exec(args...)
		if got := ExitCodeFor(err); got != output.ExitNotFound {
			t.Errorf("%v: exit %d (%v); want %d\n%s", args, got, err, output.ExitNotFound, out)
		}
	}
}

func TestInboxListJSON(t *testing.T) {
	db := setupTestDB(t)
	seedJob(t, db, "job_json_01", "ingest:text", "text.short", storage.JobFailed)
	out, err := db.exec("inbox", "list", "--failed", "--format", "json")
	if err != nil {
		t.Fatalf("inbox list --failed --format json: %v\n%s", err, out)
	}
	var got struct {
		Items []map[string]any `json:"items"`
		Total int              `json:"total"`
	}
	start := strings.Index(out, "{")
	if start < 0 {
		t.Fatalf("no JSON in output:\n%s", out)
	}
	if err := json.Unmarshal([]byte(out[start:]), &got); err != nil {
		t.Fatalf("decode: %v\n%s", err, out)
	}
	if got.Total != 1 || len(got.Items) != 1 || got.Items[0]["id"] != "job_json_01" || got.Items[0]["kind"] != "job" {
		t.Fatalf("json: %+v", got)
	}
}
