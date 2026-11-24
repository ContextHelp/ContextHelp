package providertest

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"hop.top/xrr"

	"github.com/ideacrafterslabs/ctxt/internal/testguard"
)

func TestMain(m *testing.M) {
	os.Exit(testguard.Main(m, nil))
}

func TestNormalizeArg(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Dir(dir)
	existing := filepath.Join(dir, "a.png")
	if err := os.WriteFile(existing, []byte("same bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(t.TempDir(), "renamed.png")
	if err := os.WriteFile(other, []byte("same bytes"), 0o600); err != nil {
		t.Fatal(err)
	}

	got := NormalizeArg(existing, root)
	if !strings.HasPrefix(got, "<file:") || !strings.HasSuffix(got, ".png>") {
		t.Fatalf("existing file = %q", got)
	}
	if NormalizeArg(other, root) != got {
		t.Errorf("same bytes in another temp dir must normalise alike: %q vs %q", NormalizeArg(other, root), got)
	}
	if g := NormalizeArg(filepath.Join(dir, "out.wav"), root); g != "<tmp>/out.wav" {
		t.Errorf("missing file = %q, want <tmp>/out.wav", g)
	}
	// ffmpeg's "file:" input guard is not part of the argument's identity.
	if g := NormalizeArg("file:"+existing, root); g != got {
		t.Errorf("file: prefix on an existing file = %q, want %q", g, got)
	}
	if g := NormalizeArg("file:"+filepath.Join(dir, "out.wav"), root); g != "<tmp>/out.wav" {
		t.Errorf("file: prefix on a missing file = %q", g)
	}
	if g := NormalizeArg("file:/usr/bin/x", root); g != "file:/usr/bin/x" {
		t.Errorf("file: path outside the roots = %q, want unchanged", g)
	}
	for _, plain := range []string{"-i", "stdout", "16000", "/usr/bin/x"} {
		if g := NormalizeArg(plain, root); g != plain {
			t.Errorf("NormalizeArg(%q) = %q, want unchanged", plain, g)
		}
	}
}

// Outside-root, traversal and symlink arguments are never outputs.
func TestNormalizeArgHardening(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Dir(dir)
	target := filepath.Join(t.TempDir(), "real.txt")
	if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if got := NormalizeArg(link, root); got != link {
		t.Errorf("symlink = %q, want unchanged", got)
	}
	dotdot := dir + "/../../elsewhere.txt" // not Join: it would clean the path first
	if got := NormalizeArg(dotdot, root); got != dotdot {
		t.Errorf("traversal = %q, want unchanged", got)
	}
	if got := NormalizeArg(dir, root); !strings.HasPrefix(got, "<dir:") {
		t.Errorf("directory = %q, want <dir:...>", got)
	}
}

func TestScrubRemovesTemp(t *testing.T) {
	r := ExecCassetteMode(t, t.TempDir(), xrr.ModeReplay)
	tmp := filepath.Join(r.roots[len(r.roots)-1], "TestA123", "001", "clip.mp4")
	in := `{"filename": "` + tmp + `"}`
	got := r.scrub(in)
	if strings.Contains(got, tmp) {
		t.Errorf("scrub left a machine path: %s", got)
	}
	if !strings.Contains(got, "<tmp>/clip.mp4") {
		t.Errorf("scrub = %s", got)
	}
}

func TestScrubHomeRespectsPathBoundaries(t *testing.T) {
	u, err := user.Current()
	if err != nil || u.HomeDir == "" || u.HomeDir == os.Getenv("HOME") {
		t.Skip("real home not distinguishable from the isolated one")
	}
	h := u.HomeDir
	if got := testguard.ScrubHome(h + "/x/y"); got != "<home>/x/y" {
		t.Errorf("under home = %q", got)
	}
	if got := testguard.ScrubHome("at " + h + ": ok"); got != "at <home>: ok" {
		t.Errorf("home then punctuation = %q", got)
	}
	if got := testguard.ScrubHome(h + "2/x"); got != h+"2/x" {
		t.Errorf("sibling dir with the home as prefix was rewritten: %q", got)
	}
}

// writeCassette records one exec call with outputs, then returns the dir and
// the resp file path so a test can tamper with it.
func recordWithOutput(t *testing.T) (dir string, respPath string) {
	t.Helper()
	dir = t.TempDir()
	rec := ExecCassetteMode(t, dir, xrr.ModeRecord)
	out := filepath.Join(t.TempDir(), "out.txt")
	if _, _, _, err := rec.Run(context.Background(), "sh", "-c", `printf data > "$1"`, "sh", out); err != nil {
		t.Fatal(err)
	}
	m, _ := filepath.Glob(filepath.Join(dir, "exec-*.resp.yaml"))
	if len(m) != 1 {
		t.Fatalf("resp files = %v", m)
	}
	return dir, m[0]
}

// Replay must write only to output candidates (temp paths that did not
// exist), only from blobs inside the cassette dir.
func TestExecCassette_TamperedCassetteIsRefused(t *testing.T) {
	cases := []struct {
		name string
		edit func(s string) string
	}{
		{"arg_index points at an input", func(s string) string { return strings.Replace(s, "arg_index: 3", "arg_index: 2", 1) }},
		{"missing arg_index", func(s string) string { return strings.Replace(s, "arg_index: 3\n", "", 1) }},
		{"blob path traversal", func(s string) string {
			i := strings.Index(s, "blob: ")
			j := strings.Index(s[i:], "\n")
			return s[:i] + "blob: x/../../escape" + s[i+j:]
		}},
		{"arg_index out of range", func(s string) string { return strings.Replace(s, "arg_index: 3", "arg_index: 99", 1) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir, resp := recordWithOutput(t)
			b, err := os.ReadFile(resp)
			if err != nil {
				t.Fatal(err)
			}
			edited := c.edit(string(b))
			if edited == string(b) {
				t.Fatalf("edit did not change the cassette:\n%s", b)
			}
			if err := os.WriteFile(resp, []byte(edited), 0o600); err != nil {
				t.Fatal(err)
			}

			// The recorded argv is ["-c", script, "sh", out]. A tampered
			// arg_index 2 would write to the relative path "sh" in the cwd.
			cwd := t.TempDir()
			t.Chdir(cwd)
			victim := filepath.Join(cwd, "sh")
			if err := os.WriteFile(victim, []byte("keep"), 0o600); err != nil {
				t.Fatal(err)
			}
			// A file one level above the cassette dir, reachable by traversal.
			if err := os.WriteFile(filepath.Join(filepath.Dir(dir), "escape.bin"), []byte("escaped"), 0o600); err != nil {
				t.Fatal(err)
			}
			out := filepath.Join(t.TempDir(), "out.txt")
			rep := ExecCassetteMode(t, dir, xrr.ModeReplay)
			_, _, _, err = rep.Run(context.Background(), "sh", "-c", `printf data > "$1"`, "sh", out)
			if err == nil {
				t.Errorf("replay of a tampered cassette must fail")
			}
			if _, err := os.Stat(out); err == nil {
				t.Errorf("a tampered cassette still produced the output file")
			}
			if b, _ := os.ReadFile(victim); string(b) != "keep" {
				t.Errorf("file outside the outputs was overwritten: %q", b)
			}
		})
	}
}

// The output is argument 0, so a dropped arg_index falls back to 0 and the
// candidate check passes: only the presence check can reject it.
func TestExecCassette_MissingArgIndexRefusedEvenAtIndexZero(t *testing.T) {
	if _, err := exec.LookPath("tee"); err != nil {
		t.Skip("tee not on PATH")
	}
	dir := t.TempDir()
	out := filepath.Join(t.TempDir(), "out.txt")
	rec := ExecCassetteMode(t, dir, xrr.ModeRecord)
	if _, _, _, err := rec.Run(context.Background(), "tee", out); err != nil {
		t.Fatal(err)
	}
	m, _ := filepath.Glob(filepath.Join(dir, "exec-*.resp.yaml"))
	b, err := os.ReadFile(m[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "arg_index: 0\n") {
		t.Fatalf("expected an output at argument 0:\n%s", b)
	}
	if err := os.WriteFile(m[0], []byte(strings.Replace(string(b), "arg_index: 0\n", "", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	out2 := filepath.Join(t.TempDir(), "out.txt")
	rep := ExecCassetteMode(t, dir, xrr.ModeReplay)
	if _, _, _, err := rep.Run(context.Background(), "tee", out2); err == nil {
		t.Error("replay must refuse an output entry without arg_index")
	}
	if _, err := os.Stat(out2); err == nil {
		t.Error("output was written despite the missing arg_index")
	}
}

func TestExecCassette_RecordRefusesPathUnderHome(t *testing.T) {
	u, err := user.Current()
	if err != nil || u.HomeDir == "" || u.HomeDir == os.Getenv("HOME") {
		t.Skip("real home not distinguishable from the isolated one")
	}
	fr := &fatalRecorder{TB: t}
	r := ExecCassetteMode(fr, t.TempDir(), xrr.ModeRecord)
	defer func() {
		if recover() == nil || !strings.Contains(fr.msg, "absolute path") {
			t.Errorf("recording a path under home must fail; msg = %q", fr.msg)
		}
	}()
	_, _, _, _ = r.Run(context.Background(), "cat", u.HomeDir+"/.config/x")
}

func TestMediaFixturesPresent(t *testing.T) {
	for _, n := range []string{MediaImage, MediaAudio, MediaVideo, MediaSubSRT, MediaSubVTT} {
		if len(MediaBytes(t, n)) == 0 {
			t.Errorf("fixture %s empty", n)
		}
	}
	if !strings.HasPrefix(string(MediaBytes(t, MediaImage)), "\x89PNG") {
		t.Error("image fixture is not a PNG")
	}
	if !strings.HasPrefix(string(MediaBytes(t, MediaAudio)), "RIFF") {
		t.Error("audio fixture is not a WAV")
	}
	if !strings.Contains(string(MediaBytes(t, MediaSubSRT)), SpeechTurns[0].Text) {
		t.Error("srt lacks first turn")
	}
	if _, err := mediaFS.ReadFile("testdata/media/README.md"); err == nil {
		t.Error("README.md must not be embedded")
	}
}

// Record two chained steps with real (POSIX) tools, then replay them with
// the files gone: step one's output must be recreated for step two to hit
// its cassette, which is keyed on that file's content.
func TestExecCassette_RecordThenReplayChainedSteps(t *testing.T) {
	for _, tool := range []string{"sh", "cat"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not on PATH", tool)
		}
	}
	cassettes := t.TempDir()
	steps := func(r *ExecRunner, out string) (string, string) {
		ctx := context.Background()
		if _, _, code, err := r.Run(ctx, "sh", "-c", `printf 'made by tool' > "$1"`, "sh", out); err != nil || code != 0 {
			t.Fatalf("step 1: code %d err %v", code, err)
		}
		stdout, _, code, err := r.Run(ctx, "cat", out)
		if err != nil || code != 0 {
			t.Fatalf("step 2: code %d err %v", code, err)
		}
		return stdout, out
	}

	rec := ExecCassetteMode(t, cassettes, xrr.ModeRecord)
	got, _ := steps(rec, filepath.Join(t.TempDir(), "out.txt"))
	if got != "made by tool" {
		t.Fatalf("recorded step 2 stdout = %q", got)
	}

	// New temp dir, no output file, replay mode: nothing may execute.
	rep := ExecCassetteMode(t, cassettes, xrr.ModeReplay)
	out := filepath.Join(t.TempDir(), "out.txt")
	got, _ = steps(rep, out)
	if got != "made by tool" {
		t.Errorf("replayed step 2 stdout = %q", got)
	}
	if b, err := os.ReadFile(out); err != nil || string(b) != "made by tool" {
		t.Errorf("output not recreated on replay: %q, %v", b, err)
	}
	if len(rep.Calls()) != 2 {
		t.Errorf("calls = %v", rep.Calls())
	}
}

func TestExecCassette_ReplayedNotFoundMatchesErrNotFound(t *testing.T) {
	cassettes := t.TempDir()
	rec := ExecCassetteMode(t, cassettes, xrr.ModeRecord)
	_, _, code, err := rec.Run(context.Background(), "definitely-not-a-real-binary-xyz", "--version")
	if err == nil || code != -1 {
		t.Fatalf("record: code %d err %v, want start failure", code, err)
	}
	rep := ExecCassetteMode(t, cassettes, xrr.ModeReplay)
	_, _, code, err = rep.Run(context.Background(), "definitely-not-a-real-binary-xyz", "--version")
	if !errors.Is(err, exec.ErrNotFound) || code != -1 {
		t.Errorf("replay: code %d err %v, want errors.Is exec.ErrNotFound", code, err)
	}
}

// fatalRecorder turns Fatalf into a panic so a test can observe it.
type fatalRecorder struct {
	testing.TB
	msg string
}

func (f *fatalRecorder) Fatalf(format string, args ...any) {
	f.msg = format
	panic(f)
}

func TestExecCassette_RecordRefusesMachinePathArgs(t *testing.T) {
	fr := &fatalRecorder{TB: t}
	r := ExecCassetteMode(fr, t.TempDir(), xrr.ModeRecord)
	defer func() {
		if recover() == nil || !strings.Contains(fr.msg, "absolute path") {
			t.Errorf("recording an absolute path outside the temp dirs must fail; msg = %q", fr.msg)
		}
	}()
	_, _, _, _ = r.Run(context.Background(), "cat", "/etc/hosts")
}

func TestExecRunner_CallsAreRaceFree(t *testing.T) {
	r := ExecCassetteMode(t, t.TempDir(), xrr.ModeReplay)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, _, _ = r.Run(context.Background(), "x", "arg")
			_ = r.Calls()
		}()
	}
	wg.Wait()
	if len(r.Calls()) != 8 {
		t.Errorf("calls = %d, want 8", len(r.Calls()))
	}
}

// realHomeForTest returns the pre-isolation home, or skips when it cannot be
// told apart from the isolated one.
func realHomeForTest(t *testing.T) string {
	t.Helper()
	u, err := user.Current()
	if err != nil || u.HomeDir == "" || u.HomeDir == os.Getenv("HOME") {
		t.Skip("real home not distinguishable from the isolated one")
	}
	return u.HomeDir
}
