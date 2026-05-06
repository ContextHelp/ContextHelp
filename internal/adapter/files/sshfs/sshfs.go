// Package sshfs is the sshfs backend of the files protocol slot.
//
// It is a fetch-only sensor adapter that walks a remote file tree
// over SSH (using SFTP) and emits one ingest.Object per file. Declared
// capabilities: fetch + emit-events. File metadata (size, mode, mtime)
// is included in the Object's Metadata map; bodies are NOT downloaded
// in this PR — that lands later.
//
// Live integration testing against a real SSH server is intentionally
// out of scope for this PR. Unit tests cover identity, capability
// honesty, undeclared-capability rejection, config validation, and
// the ignore-pattern helper. The Fetch path itself is exercised by a
// follow-up integration suite that spins up a fixture SSH server.
//
// TODO: integration test against test SSH server (sshd-in-container
// or x/crypto/ssh.NewServerConn fixture).
package sshfs

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"hop.top/kit/go/runtime/bus"

	"github.com/ideacrafterslabs/ctxt/internal/adapter"
	"github.com/ideacrafterslabs/ctxt/internal/adapter/files"
	"github.com/ideacrafterslabs/ctxt/internal/ingest"
)

// Config carries the sshfs adapter settings.
type Config struct {
	// Host is the remote hostname (with optional :port suffix).
	// Required.
	Host string
	// User is the SSH login user. Required.
	User string
	// RemotePath is the directory on the remote whose tree is walked.
	// Required.
	RemotePath string
	// IdentityFile is the SSH private key path. Defaults to
	// ~/.ssh/id_ed25519 (the modern key choice in 2026).
	IdentityFile string
	// Ignore is a list of glob patterns; matching basenames are
	// skipped during the walk. Common values: ".git", "node_modules",
	// "*.tmp".
	Ignore []string
}

// New constructs a typed sshfs Adapter and applies the IdentityFile
// default. Required-field validation runs at Fetch time so unit tests
// can construct an Adapter and operator config errors surface during
// the actual sweep where the failure has context.
func New(cfg Config) *Adapter {
	if cfg.IdentityFile == "" {
		cfg.IdentityFile = "~/.ssh/id_ed25519"
	}
	return &Adapter{cfg: cfg}
}

// Adapter is the typed sshfs sensor adapter.
type Adapter struct {
	cfg Config
}

// Compile-time assertion: Adapter satisfies the typed adapter contract.
var _ adapter.Adapter = (*Adapter)(nil)

// Protocol returns "files" — the slot identity.
func (a *Adapter) Protocol() string { return files.Protocol }

// Backend returns "sshfs" — the canonical backend identifier.
func (a *Adapter) Backend() string { return "sshfs" }

// Capabilities returns the sshfs backend's declared capabilities.
func (a *Adapter) Capabilities() []adapter.Capability {
	return []adapter.Capability{adapter.CapFetch, adapter.CapEmitEvents}
}

// Start is a no-op — the SSH connection is built per-Fetch.
func (a *Adapter) Start(_ context.Context, _ bus.Bus) error { return nil }

// Ready returns true once the adapter is constructed.
func (a *Adapter) Ready() bool { return true }

// Drain is a no-op (no in-flight state).
func (a *Adapter) Drain(_ context.Context) error { return nil }

// Stop is a no-op (no resources held).
func (a *Adapter) Stop(_ context.Context) error { return nil }

// Fetch dials the remote, walks Config.RemotePath, and returns one
// ingest.Object per file (skipping directories and ignored names).
// Object bodies are NOT downloaded — only file metadata.
func (a *Adapter) Fetch(ctx context.Context) ([]ingest.Object, error) {
	if err := a.validate(); err != nil {
		return nil, err
	}
	client, closer, err := a.dial(ctx)
	if err != nil {
		return nil, err
	}
	defer closer()
	return a.walk(client)
}

// Submit returns ErrCapabilityNotDeclared — sshfs is fetch-only.
func (a *Adapter) Submit(_ context.Context, _ ingest.Object) error {
	return adapter.ErrCapabilityNotDeclared
}

// Serve returns ErrCapabilityNotDeclared — sshfs is fetch-only.
func (a *Adapter) Serve(_ context.Context, _ net.Listener) error {
	return adapter.ErrCapabilityNotDeclared
}

// validate checks the required fields. Empty host/user/remote path
// fail at Fetch rather than New so unit tests can construct an
// Adapter without supplying every field.
func (a *Adapter) validate() error {
	var errs []error
	if a.cfg.Host == "" {
		errs = append(errs, errors.New("sshfs adapter: Host is required"))
	}
	if a.cfg.User == "" {
		errs = append(errs, errors.New("sshfs adapter: User is required"))
	}
	if a.cfg.RemotePath == "" {
		errs = append(errs, errors.New("sshfs adapter: RemotePath is required"))
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// dial opens an SSH connection and wraps it as an SFTP client. The
// returned closer tears both layers down in reverse order.
//
// HostKeyCallback uses ssh.InsecureIgnoreHostKey for now — known-hosts
// validation is a follow-up; without it sshfs is unsafe for production
// hosts. Operators should rely on policy/ambient.yaml gating until then.
func (a *Adapter) dial(_ context.Context) (*sftp.Client, func(), error) {
	keyPath, err := expandTilde(a.cfg.IdentityFile)
	if err != nil {
		return nil, nil, fmt.Errorf("expand identity file: %w", err)
	}
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, nil, fmt.Errorf("read identity file %s: %w", keyPath, err)
	}
	signer, err := ssh.ParsePrivateKey(keyData)
	if err != nil {
		return nil, nil, fmt.Errorf("parse identity file %s: %w", keyPath, err)
	}
	host := a.cfg.Host
	if !strings.Contains(host, ":") {
		host = host + ":22"
	}
	cfg := &ssh.ClientConfig{
		User:            a.cfg.User,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // TODO: known-hosts validation
	}
	conn, err := ssh.Dial("tcp", host, cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("ssh dial %s: %w", host, err)
	}
	client, err := sftp.NewClient(conn)
	if err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("sftp open: %w", err)
	}
	closer := func() {
		_ = client.Close()
		_ = conn.Close()
	}
	return client, closer, nil
}

// walk traverses a.cfg.RemotePath via sftp.Walker and returns one
// ingest.Object per regular file. Directories are descended into;
// matches against a.cfg.Ignore (basename-globbed) are skipped.
func (a *Adapter) walk(client *sftp.Client) ([]ingest.Object, error) {
	var objs []ingest.Object
	walker := client.Walk(a.cfg.RemotePath)
	for walker.Step() {
		if err := walker.Err(); err != nil {
			// One subtree failure should not abort the whole walk;
			// record-and-continue would require errors.Join across the
			// loop. For Phase 2 we surface the first error so operators
			// see config / permission issues immediately. Subsequent
			// PRs may switch to errors.Join when integration tests
			// validate the partial-success path.
			return objs, fmt.Errorf("walk %s: %w", walker.Path(), err)
		}
		info := walker.Stat()
		base := path.Base(walker.Path())
		if shouldIgnore(base, a.cfg.Ignore) {
			if info.IsDir() {
				walker.SkipDir()
			}
			continue
		}
		if info.IsDir() {
			continue
		}
		objs = append(objs, ingest.Object{
			ID:   fmt.Sprintf("sshfs://%s@%s%s", a.cfg.User, a.cfg.Host, walker.Path()),
			Type: "sshfs-file",
			Metadata: map[string]any{
				"host":        a.cfg.Host,
				"user":        a.cfg.User,
				"remote_path": walker.Path(),
				"size":        info.Size(),
				"mode":        info.Mode().String(),
				"mod_time":    info.ModTime().Format("2006-01-02T15:04:05Z07:00"),
			},
		})
	}
	return objs, nil
}

// shouldIgnore reports whether basename matches any of the glob
// patterns in ignores. The match uses filepath.Match semantics
// (`*`, `?`, `[]`) on the basename only — full-path globs are out
// of scope for Phase 2.
func shouldIgnore(basename string, ignores []string) bool {
	for _, p := range ignores {
		if ok, _ := filepath.Match(p, basename); ok {
			return true
		}
	}
	return false
}

// expandTilde resolves a leading `~/` against the user's home
// directory. Other shell expansions (`$HOME`, `~user`) are out of scope.
func expandTilde(p string) (string, error) {
	if !strings.HasPrefix(p, "~/") {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, p[2:]), nil
}
