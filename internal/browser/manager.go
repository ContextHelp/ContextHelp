package browser

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
)

// Manager controls the lifecycle of the IBR daemon subprocess.
type Manager struct {
	cfg    config.BrowserConfig
	client *Client
	cmd    *exec.Cmd
	port   int
	token  string
}

// NewManager creates a manager (does not start the daemon).
func NewManager(cfg config.BrowserConfig) *Manager {
	return &Manager{cfg: cfg}
}

// Client returns the IBR HTTP client, or nil if not running.
func (m *Manager) Client() *Client {
	if !m.cfg.Enabled || m.client == nil {
		return nil
	}
	return m.client
}

// IsRunning checks if the daemon is healthy.
func (m *Manager) IsRunning() bool {
	if m.client == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	h, err := m.client.Health(ctx)
	return err == nil && h.Status == "healthy"
}

// Start launches the IBR daemon as a subprocess.
func (m *Manager) Start(ctx context.Context) error {
	if !m.cfg.Enabled {
		return nil
	}

	port := m.cfg.Port
	if port == 0 {
		p, err := findFreePort()
		if err != nil {
			return fmt.Errorf("browser: find free port: %w", err)
		}
		port = p
	}
	m.port = port

	token, err := randomToken()
	if err != nil {
		return fmt.Errorf("browser: generate token: %w", err)
	}
	m.token = token

	binary := m.cfg.Binary
	if binary == "" {
		binary = "ibr"
	}

	env := append(os.Environ(),
		"IBR_DAEMON=true",
		fmt.Sprintf("IBR_DAEMON_PORT=%d", port),
		fmt.Sprintf("IBR_DAEMON_TOKEN=%s", token),
		fmt.Sprintf("IBR_DAEMON_MAX_CLIENTS=%d", m.cfg.MaxClients),
		fmt.Sprintf("BROWSER_HEADLESS=%t", m.cfg.Headless),
	)

	if m.cfg.AIProvider != "" {
		env = append(env, "AI_PROVIDER="+m.cfg.AIProvider)
	}
	if m.cfg.AIModel != "" {
		env = append(env, "AI_MODEL="+m.cfg.AIModel)
	}

	cmd := exec.CommandContext(ctx, binary)
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("browser: start daemon: %w", err)
	}
	m.cmd = cmd

	m.client = NewClient(fmt.Sprintf("http://127.0.0.1:%d", port), token)
	if err := m.waitHealthy(ctx, 15*time.Second); err != nil {
		_ = m.Stop()
		return fmt.Errorf("browser: daemon did not become healthy: %w", err)
	}

	log.Printf("browser: IBR daemon started on port %d (pid %d)", port, cmd.Process.Pid)
	return nil
}

// Stop gracefully shuts down the daemon.
func (m *Manager) Stop() error {
	if m.cmd == nil || m.cmd.Process == nil {
		return nil
	}
	log.Printf("browser: stopping IBR daemon (pid %d)", m.cmd.Process.Pid)
	if err := m.cmd.Process.Signal(os.Interrupt); err != nil {
		return m.cmd.Process.Kill()
	}
	done := make(chan error, 1)
	go func() { done <- m.cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = m.cmd.Process.Kill()
	}
	m.client = nil
	m.cmd = nil
	return nil
}

func (m *Manager) waitHealthy(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if m.IsRunning() {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("timeout after %s", timeout)
}

func findFreePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port, nil
}

func randomToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Port returns the daemon's HTTP port (0 if not started).
func (m *Manager) Port() int { return m.port }

// PID returns the daemon's process ID (0 if not started).
func (m *Manager) PID() int {
	if m.cmd != nil && m.cmd.Process != nil {
		return m.cmd.Process.Pid
	}
	return 0
}

// Enabled returns whether browser automation is configured.
func (m *Manager) Enabled() bool { return m.cfg.Enabled }
