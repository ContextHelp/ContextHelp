package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/dpkmstest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
	"github.com/ideacrafterslabs/ctxt/internal/watcher/watchstate"
)

// watchEnv is one `ctxt watch` user: a watched temp directory and a -c
// config naming it, routed to an in-process dpkms.
type watchEnv struct {
	db      *testDB
	dir     string
	cfgPath string
}

func newWatchEnv(t *testing.T, opts ...dpkmstest.Option) *watchEnv {
	t.Helper()
	db := setupTestDB(t, opts...)
	t.Setenv("XDG_STATE_HOME", filepath.Join(filepath.Dir(db.ConfigPath), "state"))
	dir := t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), "watch.yaml")
	body := fmt.Sprintf("watch:\n  dirs:\n    - %s\n  patterns:\n    - \"**/*.md\"\n  debounce: 50ms\n", dir)
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return &watchEnv{db: db, dir: dir, cfgPath: cfgPath}
}

func (e *watchEnv) write(t *testing.T, rel, body string) string {
	t.Helper()
	p := filepath.Join(e.dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// runningWatch is one in-process `ctxt watch start`.
type runningWatch struct {
	cancel  context.CancelFunc
	done    chan error
	out     string
	scanned chan string
}

// startWatch runs `ctxt watch start` with args until stop. It waits for
// the initial scan of the watched directory to finish.
func (e *watchEnv) startWatch(t *testing.T, args ...string) *runningWatch {
	t.Helper()
	rw := e.launch(t, args...)
	select {
	case <-rw.scanned:
	case err := <-rw.done:
		t.Fatalf("watch start exited early: %v\n%s", err, rw.out)
	case <-time.After(10 * time.Second):
		t.Fatal("initial scan never finished")
	}
	return rw
}

func (e *watchEnv) launch(t *testing.T, args ...string) *runningWatch {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	watchStartCmd.SetContext(ctx)
	rw := &runningWatch{cancel: cancel, done: make(chan error, 1), scanned: make(chan string, 4)}
	prevHook := watchScanHook
	watchScanHook = func(id string) { rw.scanned <- id }
	t.Cleanup(func() {
		cancel()
		watchScanHook = prevHook
		// cobra keeps a subcommand's context across Execute calls.
		watchStartCmd.SetContext(context.Background())
	})
	go func() {
		out, err := executeCommand(append([]string{"--config", e.cfgPath, "watch", "start"}, args...)...)
		rw.out = out // read only after done delivers
		rw.done <- err
	}()
	return rw
}

func (rw *runningWatch) stop(t *testing.T) string {
	t.Helper()
	rw.cancel()
	select {
	case err := <-rw.done:
		if err != nil {
			t.Fatalf("watch start: %v\n%s", err, rw.out)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("watch start did not return after cancel")
	}
	return rw.out
}

// watchJobs lists the jobs a watch enqueued on driver's instance, by
// source.
func watchJobs(t *testing.T, driver storage.StorageDriver) map[string][]*storage.Job {
	t.Helper()
	jobs, _, err := driver.Jobs().List(context.Background(), storage.JobFilter{Limit: 1000})
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]*storage.Job{}
	for _, j := range jobs {
		if strings.HasPrefix(j.Source, "watch:") {
			out[j.Source] = append(out[j.Source], j)
		}
	}
	return out
}

func jobCount(t *testing.T, driver storage.StorageDriver) int {
	n := 0
	for _, js := range watchJobs(t, driver) {
		n += len(js)
	}
	return n
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// assertNoWatchRows fails when anything wrote a watch row into the dpkms
// database: ctxt's watches never live there.
func assertNoWatchRows(t *testing.T, driver storage.StorageDriver) {
	t.Helper()
	for _, status := range []string{"active", "paused"} {
		ws, err := driver.Watches().ListWatches(context.Background(), status)
		if err != nil {
			t.Fatal(err)
		}
		if len(ws) != 0 {
			t.Fatalf("%d %s watch rows in the dpkms database: %+v", len(ws), status, ws[0])
		}
	}
}

// One enqueue per content change against the instance, and nothing in
// its watch tables.
func TestWatchStart_OneEnqueuePerContentChange(t *testing.T) {
	e := newWatchEnv(t)
	e.write(t, "existing.md", "already here")
	rw := e.startWatch(t)

	waitFor(t, "initial scan enqueue", func() bool { return jobCount(t, e.db.Driver) == 1 })

	note := e.write(t, "sub/note.md", "first")
	waitFor(t, "new file enqueue", func() bool { return jobCount(t, e.db.Driver) == 2 })
	e.write(t, "sub/note.md", "first") // same bytes
	e.write(t, "ignored.txt", "not a pattern match")
	time.Sleep(400 * time.Millisecond)
	if n := jobCount(t, e.db.Driver); n != 2 {
		t.Fatalf("unchanged rewrite or non-matching file enqueued: %d jobs", n)
	}
	if err := os.WriteFile(note, []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "changed file enqueue", func() bool { return jobCount(t, e.db.Driver) == 3 })
	time.Sleep(400 * time.Millisecond)
	out := rw.stop(t)

	jobs := watchJobs(t, e.db.Driver)
	if len(jobs["watch:generic:existing.md"]) != 1 || len(jobs["watch:generic:sub/note.md"]) != 2 {
		t.Fatalf("jobs by source = %v\n%s", jobs, out)
	}
	assertNoWatchRows(t, e.db.Driver)
}

// A restart does not re-enqueue unchanged files; a file changed while
// stopped is sent once.
func TestWatchStart_RestartKeepsState(t *testing.T) {
	e := newWatchEnv(t)
	e.write(t, "a.md", "a")
	e.write(t, "b.md", "b")
	e.startWatch(t).stop(t)
	if n := jobCount(t, e.db.Driver); n != 2 {
		t.Fatalf("first run: %d jobs, want 2", n)
	}

	state := watchstate.StatePath(mustStateDir(t), dpkmsKey(e.db.Server.URL))
	snap, err := watchstate.Read(state)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(snap.Dirs[e.dir].Files); n != 2 {
		t.Fatalf("state file %s holds %d records, want 2", state, n)
	}

	e.write(t, "b.md", "b changed while stopped")
	e.startWatch(t).stop(t)
	jobs := watchJobs(t, e.db.Driver)
	if len(jobs["watch:generic:a.md"]) != 1 || len(jobs["watch:generic:b.md"]) != 2 {
		t.Fatalf("after restart, jobs by source = %v", jobs)
	}
	assertNoWatchRows(t, e.db.Driver)
}

func mustStateDir(t *testing.T) string {
	t.Helper()
	d, err := watchstate.Dir()
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// Two instances keep separate state: switching instances re-ingests into
// the new one, and switching back sends nothing again.
func TestWatchStart_InstancesKeepSeparateState(t *testing.T) {
	e := newWatchEnv(t)
	work := dpkmstest.Start(t, storageutil.NewTestDriver(t))
	cfg := fmt.Sprintf("server:\n  urls:\n    - name: home\n      url: %s\n    - name: work\n      url: %s\n", e.db.Server.URL, work.URL)
	if err := os.WriteFile(e.db.ClientConfigPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	e.write(t, "a.md", "a")

	e.startWatch(t, "--instance", "home").stop(t)
	e.startWatch(t, "--instance", "work").stop(t)
	e.startWatch(t, "--instance", "home").stop(t)

	if n := jobCount(t, e.db.Driver); n != 1 {
		t.Errorf("home: %d jobs, want 1", n)
	}
	if n := jobCount(t, work.Driver); n != 1 {
		t.Errorf("work: %d jobs, want 1 (a new instance gets the file)", n)
	}
	dir := mustStateDir(t)
	for _, key := range []string{"home", "work"} {
		snap, err := watchstate.Read(watchstate.StatePath(dir, key))
		if err != nil || snap.Instance != key || len(snap.Dirs[e.dir].Files) != 1 {
			t.Errorf("%s state = %+v, %v", key, snap, err)
		}
	}
	assertNoWatchRows(t, e.db.Driver)
	assertNoWatchRows(t, work.Driver)
}

// Deleting a watched file deletes the object its job produced.
func TestWatchStart_DeletedFileDeletesResultObject(t *testing.T) {
	e := newWatchEnv(t)
	rw := e.startWatch(t)
	note := e.write(t, "note.md", "to be deleted")
	var job *storage.Job
	waitFor(t, "enqueue", func() bool {
		js := watchJobs(t, e.db.Driver)["watch:generic:note.md"]
		if len(js) == 1 {
			job = js[0]
		}
		return job != nil
	})

	ctx := context.Background()
	now := time.Now()
	if err := e.db.Driver.Objects().Create(ctx, &storage.KnowledgeObject{
		ID: "obj_watched", Type: "text", Summaries: []string{"watched"}, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.db.Driver.Jobs().Complete(ctx, job.ID, "obj_watched"); err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(note); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "object deletion", func() bool {
		_, err := e.db.Driver.Objects().Get(ctx, "obj_watched")
		return err != nil
	})
	rw.stop(t)
}

// Nothing answers at the instance: start exits 70 and leaves no run or
// state file behind.
func TestWatchStart_UnreachableExits70(t *testing.T) {
	e := newWatchEnv(t, dpkmstest.Unreachable())
	e.write(t, "a.md", "a")
	rw := e.launch(t)
	var err error
	select {
	case err = <-rw.done:
	case <-time.After(10 * time.Second):
		t.Fatal("watch start against an unreachable instance kept running")
	}
	if code := ExitCodeFor(err); code != 70 {
		t.Fatalf("exit %d (%v); want 70", code, err)
	}
	entries, _ := os.ReadDir(mustStateDir(t))
	for _, en := range entries {
		if !strings.HasSuffix(en.Name(), ".lock") {
			t.Errorf("left %s behind", en.Name())
		}
	}
}

// A second start for the same instance is refused while one runs.
func TestWatchStart_SecondStartConflicts(t *testing.T) {
	e := newWatchEnv(t)
	rw := e.startWatch(t)
	defer rw.stop(t)

	dir := mustStateDir(t)
	key := dpkmsKey(e.db.Server.URL)
	info, alive, err := watchstate.ReadRun(watchstate.RunPath(dir, key))
	if err != nil || !alive || info.PID != os.Getpid() || len(info.Dirs) != 1 || info.Dirs[0] != e.dir {
		t.Fatalf("run file = %+v, alive %v, %v", info, alive, err)
	}

	_, err = runWatchStartOnce(t, e)
	if code := ExitCodeFor(err); code != 4 {
		t.Fatalf("second start: exit %d (%v); want 4", code, err)
	}
}

// runWatchStartOnce runs a second start directly, bypassing the running
// one's context.
func runWatchStartOnce(t *testing.T, e *watchEnv) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	prev := watchStartCmd.Context()
	watchStartCmd.SetContext(ctx)
	defer watchStartCmd.SetContext(prev)
	return executeCommand("--config", e.cfgPath, "watch", "start")
}

func dpkmsKey(url string) string { return strings.TrimRight(url, "/") }

// status reports the local watcher only: liveness, clipboard setting,
// per-directory counts and last error, from the run and state files.
func TestWatchStatus_ReportsLocalState(t *testing.T) {
	e := newWatchEnv(t)
	e.write(t, "a.md", "a")
	e.write(t, "b.md", "b")
	e.startWatch(t).stop(t)

	out, err := executeCommand("--config", e.cfgPath, "watch", "status", "--format", "json")
	if err != nil {
		t.Fatalf("watch status: %v\n%s", err, out)
	}
	var got struct {
		Instance  string `json:"instance"`
		Endpoint  string `json:"endpoint"`
		Running   bool   `json:"running"`
		Clipboard struct {
			Status string `json:"status"`
		} `json:"clipboard"`
		Dirs []struct {
			Path       string `json:"path"`
			Configured bool   `json:"configured"`
			Files      int    `json:"files"`
			LastError  string `json:"last_error"`
		} `json:"dirs"`
	}
	start := strings.Index(out, "{")
	if start < 0 {
		t.Fatalf("no JSON object in %q", out)
	}
	if err := json.Unmarshal([]byte(out[start:]), &got); err != nil {
		t.Fatalf("decode %q: %v", out, err)
	}
	if got.Running || got.Endpoint != e.db.Server.URL || !strings.HasPrefix(got.Clipboard.Status, "disabled") {
		t.Errorf("status = %+v", got)
	}
	if len(got.Dirs) != 1 || got.Dirs[0].Path != e.dir || !got.Dirs[0].Configured || got.Dirs[0].Files != 2 {
		t.Errorf("dirs = %+v", got.Dirs)
	}
}

// stop with nothing running succeeds and says so.
func TestWatchStop_NothingRunning(t *testing.T) {
	e := newWatchEnv(t)
	out, err := executeCommand("--config", e.cfgPath, "watch", "stop")
	if err != nil || !strings.Contains(out, "No watcher running") {
		t.Fatalf("watch stop = %v\n%s", err, out)
	}
}

// Nothing configured to watch is a usage error, before any network.
func TestWatchStart_NothingToWatch(t *testing.T) {
	e := newWatchEnv(t)
	if err := os.WriteFile(e.cfgPath, []byte("watch:\n  dirs: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := runWatchStartOnce(t, e)
	if code := ExitCodeFor(err); code != 2 {
		t.Fatalf("exit %d (%v); want 2", code, err)
	}
}
