package providertest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"hop.top/xrr"
	xexec "hop.top/xrr/adapters/exec"

	"github.com/ideacrafterslabs/ctxt/internal/testguard"
)

// ExecRunner replays (or records) CLI tool calls through an xrr exec
// cassette directory. Hand Run to providers.WithCommandRunner and Lookup to
// providers.WithToolLookup:
//
//	r := providertest.ExecCassette(t, "testdata/cassettes/tesseract")
//	ctx := providers.WithCommandRunner(context.Background(), r.Run)
//	ctx = providers.WithToolLookup(ctx, r.Lookup)
//
// Replay never starts a process, so the tool need not be installed.
// Recording (XRR_MODE=record) runs the real tool.
//
// Temp paths are normalised before fingerprinting, see [NormalizeArg].
// Files the tool creates at a temp path named in its arguments (extracted
// audio, frames, converted images) are stored beside the cassette and
// recreated on replay, so a later step that reads them behaves as it did
// when recorded. Files a tool writes anywhere else are not captured.
type ExecRunner struct {
	t       testing.TB
	dir     string
	sess    *xrr.FileSession
	record  bool
	adapter *xexec.Adapter
	roots   []string
	scrubRE []*regexp.Regexp

	mu    sync.Mutex
	calls [][]string
}

// ExecCassette returns an ExecRunner on cassetteDir. The mode follows
// XRR_MODE: "record" records, anything else replays.
func ExecCassette(t testing.TB, cassetteDir string) *ExecRunner {
	t.Helper()
	return ExecCassetteMode(t, cassetteDir, modeFromEnv(t, cassetteDir))
}

// ExecCassetteMode is ExecCassette with an explicit mode, for tests of the
// harness itself. Record needs cassetteDir to exist.
func ExecCassetteMode(t testing.TB, cassetteDir string, mode xrr.Mode) *ExecRunner {
	t.Helper()
	roots := tempRoots(t)
	r := &ExecRunner{
		t:       t,
		dir:     cassetteDir,
		sess:    xrr.NewSession(mode, xrr.NewFileCassette(cassetteDir)),
		record:  mode == xrr.ModeRecord,
		adapter: xexec.NewAdapter(),
		roots:   roots,
	}
	for _, root := range roots {
		r.scrubRE = append(r.scrubRE, regexp.MustCompile(regexp.QuoteMeta(root)+`(/[^\s"'/]+)*/`))
	}
	return r
}

// modeFromEnv returns the xrr mode selected by XRR_MODE (record, or replay
// for anything else) and creates dir when recording.
func modeFromEnv(t testing.TB, dir string) xrr.Mode {
	t.Helper()
	if os.Getenv(xrr.EnvMode) != string(xrr.ModeRecord) {
		return xrr.ModeReplay
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	return xrr.ModeRecord
}

// Calls returns the normalised argv of every call so far, in order.
func (r *ExecRunner) Calls() [][]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([][]string, len(r.calls))
	copy(out, r.calls)
	return out
}

// Lookup matches providers.ToolLookup. Replay reports every tool present
// (the recording decides what runs); recording needs the real tool on PATH.
// It returns the bare name so the path never reaches an argv.
func (r *ExecRunner) Lookup(name string) (string, error) {
	if r.record {
		if _, err := exec.LookPath(name); err != nil {
			return "", err
		}
	}
	return name, nil
}

// notRecorded is the error for a call with no cassette. It matches
// xrr.ErrCassetteMiss and providers.IsCommandNotRecorded.
type notRecorded struct{ argv []string }

func (e *notRecorded) Error() string {
	return fmt.Sprintf("providertest: no recording for %q (re-record with XRR_MODE=record): %v", e.argv, xrr.ErrCassetteMiss)
}
func (e *notRecorded) Is(target error) bool     { return target == xrr.ErrCassetteMiss }
func (e *notRecorded) CommandNotRecorded() bool { return true }

// Run matches providers.CommandRunner.
func (r *ExecRunner) Run(ctx context.Context, name string, args ...string) (string, string, int, error) {
	argv, outIdx := r.normalise(name, args)
	r.mu.Lock()
	r.calls = append(r.calls, argv)
	r.mu.Unlock()
	req := &xexec.Request{Argv: argv}

	resp, err := r.sess.Record(ctx, r.adapter, req, func() (xrr.Response, error) {
		return r.runReal(ctx, name, args, outIdx)
	})
	if errors.Is(err, xrr.ErrCassetteMiss) {
		return "", "", -1, &notRecorded{argv: argv}
	}

	switch v := resp.(type) {
	case *outputResponse:
		return v.Stdout, v.Stderr, v.ExitCode, err
	case *xrr.RawResponse:
		stdout, _ := v.Payload["stdout"].(string)
		stderr, _ := v.Payload["stderr"].(string)
		code := 0
		switch n := v.Payload["exit_code"].(type) {
		case int:
			code = n
		case float64:
			code = int(n)
		}
		if rerr := r.restoreOutputs(v.Payload["outputs"], args, outIdx); rerr != nil {
			return "", "", -1, rerr
		}
		if err != nil && strings.Contains(err.Error(), "executable file not found") {
			err = fmt.Errorf("%s: %w", err.Error(), exec.ErrNotFound)
			code = -1
		}
		return stdout, stderr, code, err
	}
	if err == nil {
		err = errors.New("providertest: unexpected xrr response")
	}
	return "", "", -1, err
}

// normalise returns the fingerprinted argv and the indexes (into args) of
// arguments that name a temp path which does not exist yet: candidate
// outputs. While recording it fails the test on an absolute path outside
// the temp roots, which would tie the cassette to one machine.
func (r *ExecRunner) normalise(name string, args []string) (argv []string, outIdx []int) {
	argv = make([]string, 0, len(args)+1)
	argv = append(argv, name)
	for i, a := range args {
		n := NormalizeArg(a, r.roots...)
		if strings.HasPrefix(n, "<tmp>/") {
			outIdx = append(outIdx, i)
		} else if r.record && filepath.IsAbs(n) { // checked before the home scrub: a path under home is still a machine path
			r.t.Fatalf("providertest: refusing to record absolute path argument %q outside the temp dirs; cassettes must not carry machine paths", a)
		}
		argv = append(argv, testguard.ScrubHome(n))
	}
	return argv, outIdx
}

func (r *ExecRunner) runReal(ctx context.Context, name string, args []string, outIdx []int) (xrr.Response, error) {
	cmd := exec.CommandContext(ctx, name, args...) // #nosec G204 -- record/passthrough only, in tests
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	runErr := cmd.Run()
	resp := &outputResponse{Response: xexec.Response{
		Stdout:   r.scrub(so.String()),
		Stderr:   r.scrub(se.String()),
		ExitCode: xexec.ExitCodeFromError(runErr),
	}}
	for _, i := range outIdx {
		b, err := os.ReadFile(args[i]) // #nosec G304 -- temp path named in argv
		if err != nil {
			continue // the tool did not create it
		}
		sum := sha256.Sum256(b)
		key := hex.EncodeToString(sum[:8])
		if err := os.WriteFile(filepath.Join(r.dir, "blob-"+key+".bin"), b, 0o644); err != nil { // #nosec G306 G703 -- cassette data, name is a content hash
			return nil, fmt.Errorf("providertest: write blob: %w", err)
		}
		resp.Outputs = append(resp.Outputs, output{ArgIndex: i, Blob: key})
	}
	return resp, runErr
}

// blobKey is the shape of a blob name; anything else is a tampered cassette.
var blobKey = regexp.MustCompile(`^[0-9a-f]{16}$`)

// restoreOutputs recreates recorded output files. It writes only to the
// candidate indexes (temp paths that did not exist before the call) and
// reads only blobs inside the cassette dir, so a tampered cassette cannot
// make replay overwrite an input or a file elsewhere.
func (r *ExecRunner) restoreOutputs(raw any, args []string, candidates []int) error {
	list, _ := raw.([]any)
	for _, item := range list {
		m, _ := item.(map[string]any)
		idx, ok := m["arg_index"].(int)
		blob, _ := m["blob"].(string)
		if !ok || !blobKey.MatchString(blob) {
			return fmt.Errorf("providertest: malformed output entry %v in cassette %s", item, r.dir)
		}
		if !slices.Contains(candidates, idx) {
			return fmt.Errorf("providertest: cassette %s names argument %d as an output, but this call's output paths are %v", r.dir, idx, candidates)
		}
		b, err := os.ReadFile(filepath.Join(r.dir, "blob-"+blob+".bin")) // #nosec G304 -- name validated against blobKey
		if err != nil {
			return fmt.Errorf("providertest: cassette output blob: %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(args[idx]), 0o750); err != nil {
			return err
		}
		if err := os.WriteFile(args[idx], b, 0o600); err != nil { // #nosec G703 -- idx is one of the call's own temp output paths
			return fmt.Errorf("providertest: recreate output: %w", err)
		}
	}
	return nil
}

// scrub removes temp-dir prefixes and the developer's real home directory
// from recorded output so cassettes carry no machine-specific paths.
func (r *ExecRunner) scrub(s string) string {
	for _, re := range r.scrubRE {
		s = re.ReplaceAllString(s, "<tmp>/")
	}
	return testguard.ScrubHome(s)
}

// output is a file the tool created at an argument path, stored as a blob
// next to the cassette.
type output struct {
	ArgIndex int    `yaml:"arg_index"`
	Blob     string `yaml:"blob"`
}

// outputResponse is the exec response plus created files. xrr's exec
// adapter records stdout, stderr and the exit code only; extending the
// response keeps the cassette shape otherwise unchanged.
type outputResponse struct {
	xexec.Response `yaml:",inline"`
	Outputs        []output `yaml:"outputs,omitempty"`
}

// NormalizeArg maps one argv element to a value that is stable across
// runs. An argument under one of roots (temp dirs by default) becomes:
//
//   - "<file:HASH.ext>" when it is an existing file: HASH is the first 8
//     hex digits of the SHA-256 of its bytes, so the same fixture matches
//     wherever a test copies it;
//   - "<tmp>/NAME" when it does not exist yet (an output path); only the
//     base name is kept because the dirs between the root and the file
//     are per-test and random;
//   - "<dir:NAME>" for a directory.
//
// The path is cleaned first, and a symlink or special file is returned
// unchanged, so "$TMP/../x" and links never count as outputs. Anything else
// is returned unchanged too.
//
// A "file:" prefix on an absolute path (ffmpeg and ffprobe inputs carry it
// so a ':' in the name is not read as a protocol) is a guard, not part of
// the argument's identity: it is dropped when the path normalises.
func NormalizeArg(arg string, roots ...string) string {
	if rest, ok := strings.CutPrefix(arg, "file:"); ok && filepath.IsAbs(rest) {
		if n := NormalizeArg(rest, roots...); n != rest {
			return n
		}
		return arg
	}
	clean := filepath.Clean(arg)
	for _, root := range roots {
		if !strings.HasPrefix(clean, root+string(filepath.Separator)) {
			continue
		}
		st, err := os.Lstat(clean)
		switch {
		case err == nil && st.Mode().IsRegular():
			if b, err := os.ReadFile(clean); err == nil { // #nosec G304 -- test temp path
				sum := sha256.Sum256(b)
				return fmt.Sprintf("<file:%s%s>", hex.EncodeToString(sum[:4]), filepath.Ext(clean))
			}
			return arg
		case err == nil && st.IsDir():
			return "<dir:" + filepath.Base(clean) + ">"
		case err == nil:
			return arg // symlink or special file: never an output, never hashed
		case errors.Is(err, os.ErrNotExist):
			return "<tmp>/" + filepath.Base(clean)
		default:
			return arg
		}
	}
	return arg
}

// tempRoots lists the spellings of the temp directory (raw and
// symlink-resolved: /var vs /private/var on macOS) plus the test's own.
func tempRoots(t testing.TB) []string {
	t.Helper()
	seen := map[string]bool{}
	var roots []string
	add := func(p string) {
		p = filepath.Clean(p)
		if p != "" && !seen[p] {
			seen[p] = true
			roots = append(roots, p)
		}
	}
	for _, base := range []string{os.TempDir(), t.TempDir()} {
		add(base)
		if r, err := filepath.EvalSymlinks(base); err == nil {
			add(r)
		}
	}
	// Longest first so a test's own dir wins over its parent.
	for i := range roots {
		for j := i + 1; j < len(roots); j++ {
			if len(roots[j]) > len(roots[i]) {
				roots[i], roots[j] = roots[j], roots[i]
			}
		}
	}
	return roots
}
