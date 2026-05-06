// Package gmail is the mxhook+gmail backend of the email protocol slot.
//
// It declares CapFetch + CapSubmit + CapEmitEvents (NOT CapServe —
// Stalwart-style server mode lands in Phase 3 under a sibling
// backend). Lifecycle is REAL (unlike the legacy himalaya wrap):
// Start opens an IMAP connection and starts a polling goroutine,
// Drain stops accepting new outbound, Stop closes the connection and
// cancels the goroutine.
//
// # Credentials
//
// The adapter never stores OAuth2 tokens on its struct beyond live
// connection lifetime. CredentialsRef is a URI ("env:GMAIL",
// "keyring:gmail", "openbao:secret/gmail") resolved at Start through
// kit/storage/secret. The resolved access/refresh-token pair is
// retained only by the IMAP/SMTP client struct that owns the live
// connection. Reconnect re-resolves the ref so credential rotation
// (token refresh) works without restarting the adapter.
//
// Phase 2 ships only the env: scheme via kit/storage/secret/env.
// keyring: and openbao: are TODOs that drop in cleanly behind the
// resolveCredentials seam below.
//
// # Persistence + policy gating (Acceptance Gate 4)
//
// Inbound messages from Fetch (and the polling goroutine) are
// persisted via a domain.Service[messageEntity] constructed at Start
// with the daemon's policy.Bootstrap.Publisher(). This routes every
// store through kit.runtime.entity.pre_persisted, where CEL rules
// veto malformed mail (e.g. missing From). Operators get the same
// policy seam as pipeline mutations. See ADR-065 §Amendment 2026-05-06
// §Acceptance Gate 4.
//
// Submit is an outbound action, not an entity persistence — it does
// NOT route through domain.Service. Outbound policy gating is a
// separate concern (egress filters, DLP). Phase 2 leaves that to a
// follow-up; SubmitVeto is documented but not yet wired.
//
// # IMAP / SMTP backends
//
// IMAP/SMTP are injectable via the IMAPClient and SMTPSender
// interfaces. Production wiring uses go-imap + golang.org/x/oauth2
// (Phase 3); Phase 2 ships these as INTERFACES so unit tests use
// fakes and so the substrate doesn't haul in third-party deps before
// the integration design lands. New() with nil clients returns an
// adapter that errors on Fetch/Submit until the adopter wires real
// clients in.
package gmail

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"hop.top/kit/go/runtime/bus"
	"hop.top/kit/go/runtime/domain"

	"github.com/ideacrafterslabs/ctxt/internal/adapter"
	"github.com/ideacrafterslabs/ctxt/internal/adapter/email"
	"github.com/ideacrafterslabs/ctxt/internal/adapter/email/mxhook"
	"github.com/ideacrafterslabs/ctxt/internal/ingest"
)

// Backend is the canonical backend identifier returned from
// Adapter.Backend(). Used by the substrate Registry, by operator
// surfaces (`dpkms adapter list`), and by the runner when emitting
// dpkms.adapter.lifecycle.* events.
const Backend = "mxhook+gmail"

// defaultFolder is the IMAP folder polled when Config.Folder is empty.
const defaultFolder = "INBOX"

// defaultMaxItems caps the envelope-list size when Config.MaxItems is 0.
const defaultMaxItems = 100

// defaultPollInterval is the polling cadence when
// Config.PollIntervalSeconds is 0. Phase 3 may swap polling for IMAP
// IDLE; this constant lets adopters tune in the meantime.
const defaultPollInterval = 60 * time.Second

// IMAPClient abstracts the IMAP read path. Production wiring uses
// github.com/emersion/go-imap (Phase 3); tests inject fakes.
//
// Implementations MUST be safe for concurrent FetchEnvelopes calls
// against the same client (the polling goroutine and an explicit
// Adapter.Fetch call may both run).
type IMAPClient interface {
	// Connect dials the IMAP server, authenticates with the resolved
	// OAuth2 access token, and prepares the connection for use. Called
	// from Adapter.Start; idempotent (re-Connect closes + redials).
	Connect(ctx context.Context, accessToken string) error

	// FetchEnvelopes returns up to max envelopes from folder. The
	// envelope shape (Object.Metadata) is provider-specific; gmail
	// includes "From", "To", "Subject", "Date", "MessageID".
	FetchEnvelopes(ctx context.Context, folder string, max int) ([]ingest.Object, error)

	// Close terminates the IMAP connection. Called from Adapter.Stop;
	// must be idempotent.
	Close(ctx context.Context) error
}

// SMTPSender abstracts the SMTP write path. Production wiring uses
// net/smtp + OAuth2 XOAUTH2 SASL (Phase 3); tests inject fakes.
type SMTPSender interface {
	// Send transmits a single outbound message. Adapter.Submit
	// validates required headers (From/To/Subject/Body) before
	// invoking Send; senders MAY assume the input is well-formed.
	Send(ctx context.Context, accessToken string, msg OutboundMessage) error
}

// OutboundMessage is the parsed Submit input. Adapter.Submit
// constructs it from ingest.Object.Metadata + ingest.Object.Content
// and rejects with ErrMissingHeader when required fields are absent.
type OutboundMessage struct {
	From    string
	To      string
	Subject string
	Body    string
}

// CredentialResolver resolves an OAuthCredentialsRef into an OAuth2
// credential at Start time. Production wiring uses kit/storage/secret;
// tests inject a static resolver. Adapter.Start fails loud on resolve
// errors so misconfig surfaces immediately.
type CredentialResolver interface {
	Resolve(ctx context.Context, ref mxhook.OAuthCredentialsRef) (Credentials, error)
}

// Credentials carries the resolved OAuth2 tokens. The adapter holds
// these only for the duration of a Connect/Send call; nothing is
// persisted on the adapter struct.
type Credentials struct {
	AccessToken  string
	RefreshToken string
	ClientID     string
	ClientSecret string
}

// MessageRepository is the persistence sink for inbound mail. The
// adapter wraps it in a domain.Service[messageEntity] at Start so
// every Create routes through the policy pre_persisted gate (Gate 4).
type MessageRepository = domain.Repository[messageEntity]

// ErrMissingHeader is returned from Submit when an outbound
// ingest.Object lacks From, To, Subject, or Body. Substrate code can
// branch on it via errors.Is so adopters distinguish caller error
// from transport failure.
var ErrMissingHeader = errors.New("gmail: outbound message missing required header")

// ErrClientNotWired is returned from Fetch/Submit when the adapter
// was constructed without an IMAPClient/SMTPSender. Phase 2 ships
// the adapter wired with real clients only when adopters opt in.
var ErrClientNotWired = errors.New("gmail: client not wired (Phase 2 stub)")

// Config is the gmail backend configuration. Embeds mxhook.Config so
// gmail-specific knobs sit alongside the shared OAuth/folder bits.
type Config struct {
	mxhook.Config

	// IMAP injects the IMAP read path. nil means Fetch errors with
	// ErrClientNotWired. Phase 3 supplies a real go-imap-backed client.
	IMAP IMAPClient

	// SMTP injects the SMTP write path. nil means Submit errors with
	// ErrClientNotWired. Phase 3 supplies a real net/smtp-backed client.
	SMTP SMTPSender

	// Resolver injects credential resolution. nil falls back to the
	// internal env-prefix resolver for the env: URI scheme.
	Resolver CredentialResolver

	// Repo is the persistence sink for inbound mail. nil means Fetch
	// returns envelopes but doesn't persist (legacy ingest mode).
	// Production wiring passes a real Repository so Gate 4 applies.
	Repo MessageRepository

	// Publisher is the policy event publisher. nil bypasses the
	// pre_persisted gate (test-only). Production callers pass
	// pol.Publisher() from the policy.Bootstrap.
	Publisher domain.EventPublisher
}

// Adapter is the mxhook+gmail typed adapter.
type Adapter struct {
	cfg Config

	mu         sync.Mutex
	state      adapter.LifecycleState
	creds      Credentials
	svc        *domain.Service[messageEntity]
	cancelPoll context.CancelFunc
	pollDone   chan struct{}
	drained    bool
}

// Compile-time assertion: Adapter satisfies the typed adapter contract.
var _ adapter.Adapter = (*Adapter)(nil)

// New constructs a gmail Adapter with cfg. Validation deferred to
// Start so misconfig surfaces with the operator-facing identity (
// adapter %s/%s wraps the cause).
func New(cfg Config) *Adapter {
	if cfg.Folder == "" {
		cfg.Folder = defaultFolder
	}
	if cfg.MaxItems == 0 {
		cfg.MaxItems = defaultMaxItems
	}
	if cfg.PollIntervalSeconds == 0 {
		cfg.PollIntervalSeconds = int(defaultPollInterval.Seconds())
	}
	if cfg.Resolver == nil {
		cfg.Resolver = envResolver{}
	}
	return &Adapter{
		cfg:   cfg,
		state: adapter.StateStopped,
	}
}

// Protocol returns "email" — the slot identity defined in
// internal/adapter/email/slot.go.
func (a *Adapter) Protocol() string { return email.Protocol }

// Backend returns "mxhook+gmail" — the canonical backend identifier.
func (a *Adapter) Backend() string { return Backend }

// Capabilities returns the gmail backend's declared capabilities:
// fetch (IMAP envelope list), submit (SMTP send), emit-events
// (lifecycle topics). Serve (IMAP/SMTP server) lands in Phase 3
// under a Stalwart-backed sibling; gmail itself is a client only.
func (a *Adapter) Capabilities() []adapter.Capability {
	return []adapter.Capability{
		adapter.CapFetch,
		adapter.CapSubmit,
		adapter.CapEmitEvents,
	}
}

// Start resolves the OAuth2 credentials, opens the IMAP connection,
// constructs the policy-gated domain.Service for inbound persistence,
// and starts the polling goroutine.
func (a *Adapter) Start(ctx context.Context, _ bus.Bus) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.state == adapter.StateReady {
		return nil // idempotent
	}
	a.state = adapter.StateStarting

	creds, err := a.cfg.Resolver.Resolve(ctx, a.cfg.CredentialsRef)
	if err != nil {
		a.state = adapter.StateStopped
		return fmt.Errorf("resolve credentials: %w", err)
	}
	a.creds = creds

	if a.cfg.IMAP != nil {
		if err := a.cfg.IMAP.Connect(ctx, creds.AccessToken); err != nil {
			a.state = adapter.StateStopped
			return fmt.Errorf("imap connect: %w", err)
		}
	}

	if a.cfg.Repo != nil {
		opts := []domain.Option[messageEntity]{}
		if a.cfg.Publisher != nil {
			opts = append(opts, domain.WithPublisher[messageEntity](a.cfg.Publisher))
		}
		a.svc = domain.NewService[messageEntity](a.cfg.Repo, opts...)
	}

	pollCtx, cancel := context.WithCancel(context.Background())
	a.cancelPoll = cancel
	a.pollDone = make(chan struct{})
	a.drained = false
	go a.pollLoop(pollCtx)

	a.state = adapter.StateReady
	return nil
}

// Ready reports whether Start has completed successfully.
func (a *Adapter) Ready() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.state == adapter.StateReady
}

// Drain stops accepting new outbound and lets in-flight sends settle.
// The polling goroutine continues until Stop — Drain is the
// "graceful" half of shutdown.
func (a *Adapter) Drain(_ context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.drained = true
	a.state = adapter.StateDraining
	return nil
}

// Stop cancels the polling goroutine and closes the IMAP connection.
// Idempotent across repeated calls; resource release runs even when
// Drain wasn't called.
func (a *Adapter) Stop(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancelPoll != nil {
		a.cancelPoll()
		a.cancelPoll = nil
	}
	if a.pollDone != nil {
		// Wait for goroutine teardown but don't block forever.
		select {
		case <-a.pollDone:
		case <-time.After(2 * time.Second):
		}
		a.pollDone = nil
	}
	var imapErr error
	if a.cfg.IMAP != nil {
		imapErr = a.cfg.IMAP.Close(ctx)
	}
	a.state = adapter.StateStopped
	a.creds = Credentials{}
	if imapErr != nil {
		return fmt.Errorf("imap close: %w", imapErr)
	}
	return nil
}

// Fetch pulls a fresh envelope list from IMAP. When Repo is wired,
// each envelope is persisted through domain.Service.Create — which
// fires kit.runtime.entity.pre_persisted, where the policy engine can
// veto. Vetoed envelopes are dropped from the returned slice; the
// error from the policy engine is logged via the bus, not returned,
// so a single vetoed message doesn't fail the whole batch.
func (a *Adapter) Fetch(ctx context.Context) ([]ingest.Object, error) {
	a.mu.Lock()
	imapClient := a.cfg.IMAP
	folder := a.cfg.Folder
	max := a.cfg.MaxItems
	creds := a.creds
	svc := a.svc
	a.mu.Unlock()

	if imapClient == nil {
		return nil, ErrClientNotWired
	}
	envelopes, err := imapClient.FetchEnvelopes(ctx, folder, max)
	if err != nil {
		return nil, fmt.Errorf("fetch envelopes: %w", err)
	}
	if svc == nil {
		// Legacy ingest mode: return envelopes without policy gating.
		// The substrate's existing pipeline-side gate still applies
		// downstream of Fetch.
		return envelopes, nil
	}
	out := make([]ingest.Object, 0, len(envelopes))
	for _, env := range envelopes {
		ent := fromObject(env)
		if err := svc.Create(ctx, &ent); err != nil {
			// Policy veto or repo failure — drop this message from
			// the batch but keep going. Adopters who need fail-fast
			// semantics handle the bus event instead.
			_ = err
			continue
		}
		out = append(out, env)
	}
	_ = creds // reserved for refresh-on-demand in Phase 3
	return out, nil
}

// Submit sends an outbound message via SMTP. The input must carry
// From, To, Subject, and Body in Metadata or (for Body) Content.
// Missing headers return ErrMissingHeader so callers see the cause
// without parsing strings.
//
// Submit does NOT route through domain.Service — Submit is an
// outbound action, not an entity persistence. Outbound policy gating
// (egress filters, DLP) is a Phase 3 follow-up.
func (a *Adapter) Submit(ctx context.Context, obj ingest.Object) error {
	a.mu.Lock()
	smtp := a.cfg.SMTP
	creds := a.creds
	drained := a.drained
	a.mu.Unlock()

	if drained {
		return errors.New("gmail: drained, refusing new outbound")
	}
	if smtp == nil {
		return ErrClientNotWired
	}
	msg, err := outboundFromObject(obj)
	if err != nil {
		return err
	}
	return smtp.Send(ctx, creds.AccessToken, msg)
}

// Serve returns ErrCapabilityNotDeclared — gmail is a client-side
// adapter; protocol-server semantics live in a Stalwart-backed
// sibling under email/stalwart in Phase 3.
func (a *Adapter) Serve(_ context.Context, _ net.Listener) error {
	return adapter.ErrCapabilityNotDeclared
}

// pollLoop is the long-running fetcher. It calls Fetch on the
// configured cadence and emits results into the bus pipeline via
// the domain.Service Create path. Cancellation is via Stop; the
// goroutine exits when ctx is done.
func (a *Adapter) pollLoop(ctx context.Context) {
	defer close(a.pollDone)
	interval := time.Duration(a.cfg.PollIntervalSeconds) * time.Second
	if interval <= 0 {
		interval = defaultPollInterval
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			// Best-effort poll. Errors are intentionally swallowed —
			// the next tick retries. Adopters who need observability
			// subscribe to bus events emitted by domain.Service.
			_, _ = a.Fetch(ctx)
		}
	}
}

// outboundFromObject extracts the required headers from an
// ingest.Object. Body falls back to Object.Content when the metadata
// lookup misses, which lets adopters carry the body in either field.
func outboundFromObject(obj ingest.Object) (OutboundMessage, error) {
	from, _ := obj.Metadata["From"].(string)
	to, _ := obj.Metadata["To"].(string)
	subject, _ := obj.Metadata["Subject"].(string)
	body, _ := obj.Metadata["Body"].(string)
	if body == "" {
		body = obj.Content
	}
	if from == "" {
		return OutboundMessage{}, fmt.Errorf("%w: From", ErrMissingHeader)
	}
	if to == "" {
		return OutboundMessage{}, fmt.Errorf("%w: To", ErrMissingHeader)
	}
	if subject == "" {
		return OutboundMessage{}, fmt.Errorf("%w: Subject", ErrMissingHeader)
	}
	if body == "" {
		return OutboundMessage{}, fmt.Errorf("%w: Body", ErrMissingHeader)
	}
	return OutboundMessage{From: from, To: to, Subject: subject, Body: body}, nil
}
