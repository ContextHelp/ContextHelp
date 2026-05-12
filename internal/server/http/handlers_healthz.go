// Package http exposes dPKMS over HTTP.
//
// The /healthz endpoint returns an envelope of the form:
//
//	{
//	  "health": "healthy" | "degraded" | "failed" | "upgrading",
//	  "version": "...",
//	  "uptime_seconds": <number>,
//	  "checks": {
//	    "process":   "ok",
//	    "rest_api":  "ok",
//	    "grpc_api":  "ok" | "unknown",
//	    "db":        {"status": "ok"|"failed", "last_write": "..."},
//	    "queue":     {"pending": <n>, "running": <n>, "failed": <n>},
//	    "watchers":  [{"name": "...", "subscriptions": <n>, "last_event": "..."}]
//	  }
//	}
//
// The envelope is INTENTIONALLY EXTENSIBLE. T-0580 (ADR-070, Phase 2)
// adds an `upgrade` top-level field with the upgrade-state envelope:
//
//	{
//	  ...
//	  "upgrade": {"state":"in_progress","bucket":"reingest_selective",
//	              "progress":0.62,"eta_seconds":47,"started_at":"..."}
//	}
//
// When an upgrade is in progress, the top-level `health` value flips
// to "upgrading". Callers must treat unknown top-level keys as forward-
// compatible additions and must not error on them.
//
// Status codes:
//   - 200 + envelope when health is "healthy" or "degraded".
//   - 503 + envelope when health is "failed". A failed envelope still
//     conforms to the schema; consumers can read the `checks` map to
//     diagnose without parsing free-form error strings.
//
// The shape is fixed at this T-0564 ship; only additions are allowed.
package http

import (
	"context"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// HealthStatus is the top-level health verdict.
type HealthStatus string

const (
	HealthHealthy   HealthStatus = "healthy"
	HealthDegraded  HealthStatus = "degraded"
	HealthFailed    HealthStatus = "failed"
	HealthUpgrading HealthStatus = "upgrading" // reserved for T-0580
)

// HealthzEnvelope is the wire shape of a GET /healthz response.
//
// New top-level fields (e.g. `Upgrade` for T-0580) MUST be added with
// `omitempty` so older clients receive a byte-identical payload until
// the field is populated.
type HealthzEnvelope struct {
	Health        HealthStatus     `json:"health"`
	Version       string           `json:"version"`
	UptimeSeconds int64            `json:"uptime_seconds"`
	Checks        HealthChecks     `json:"checks"`
	Upgrade       *UpgradeSnapshot `json:"upgrade,omitempty"` // ADR-070 §5, T-0580.
}

// UpgradeSnapshot is the read-only view of the in-flight upgrade reported
// by /healthz. Mirror of internal/upgrade.Status — duplicated here so the
// HTTP layer doesn't pull a service-side dependency for a wire-only type.
//
// Populated when the dpkms server's HealthzProbes.Upgrade callback returns
// a non-nil snapshot. When the state is "in_progress" the top-level Health
// field flips to "upgrading"; readers MUST treat both signals as the same
// fact ("daemon is busy upgrading"), and consumers SHOULD parse the
// upgrade envelope rather than the top-level health for granular detail.
type UpgradeSnapshot struct {
	State      string  `json:"state"`
	Bucket     string  `json:"bucket,omitempty"`
	Progress   float64 `json:"progress,omitempty"`
	Done       int     `json:"done,omitempty"`
	Total      int     `json:"total,omitempty"`
	EtaSeconds int     `json:"eta_seconds,omitempty"`
	StartedAt  string  `json:"started_at,omitempty"` // RFC3339; empty when zero.
	LastError  string  `json:"last_error,omitempty"`
}

// HealthChecks groups per-subsystem signals.
type HealthChecks struct {
	Process  string         `json:"process"`
	RESTAPI  string         `json:"rest_api"`
	GRPCAPI  string         `json:"grpc_api"`
	DB       DBCheck        `json:"db"`
	Queue    QueueCheck     `json:"queue"`
	Watchers []WatcherCheck `json:"watchers"`
}

// DBCheck reports DB reachability and the most recent write timestamp.
type DBCheck struct {
	Status    string `json:"status"`
	LastWrite string `json:"last_write,omitempty"`
}

// QueueCheck reports current job-queue depths.
type QueueCheck struct {
	Pending int `json:"pending"`
	Running int `json:"running"`
	Failed  int `json:"failed"`
}

// WatcherCheck reports the state of one registered watcher.
type WatcherCheck struct {
	Name          string `json:"name"`
	Subscriptions int    `json:"subscriptions"`
	LastEvent     string `json:"last_event,omitempty"`
}

// HealthzProbes injects optional runtime signals that the dpkms serve
// command knows about (version, gRPC liveness, watcher introspection,
// upgrade state). Any field may be nil; the handler degrades gracefully.
type HealthzProbes struct {
	Version  string
	Started  time.Time
	GRPC     func(ctx context.Context) bool          // returns true when gRPC is serving
	Watchers func(ctx context.Context) []WatcherCheck
	// Upgrade returns the current upgrade-state envelope or nil when no
	// upgrade is in progress. Wired by dpkms serve to the in-process
	// upgrade.Manager (ADR-070 §5, T-0580).
	Upgrade func(ctx context.Context) *UpgradeSnapshot
}

// queueDepthsFromService probes the JobStore for {pending, running, failed}
// counts. On any error we return zeroes — the handler treats a probe
// failure as "queue: ok with zero depth" rather than poisoning the
// envelope.
func queueDepthsFromService(ctx context.Context, svc *service.Service) QueueCheck {
	if svc == nil || svc.Store == nil {
		return QueueCheck{}
	}
	jobStore := svc.Store.Jobs()
	if jobStore == nil {
		return QueueCheck{}
	}
	out := QueueCheck{}
	for status, target := range map[storage.JobStatus]*int{
		storage.JobPending: &out.Pending,
		storage.JobRunning: &out.Running,
		storage.JobFailed:  &out.Failed,
	} {
		_, total, err := jobStore.List(ctx, storage.JobFilter{Status: status, Limit: 1})
		if err == nil {
			*target = total
		}
	}
	return out
}

// Healthz returns the GET /healthz handler. probes is consulted for
// version/start-time/gRPC/watcher signals; a zero-value HealthzProbes
// causes those fields to be filled with safe defaults.
func Healthz(svc *service.Service, probes HealthzProbes) http.HandlerFunc {
	if probes.Started.IsZero() {
		probes.Started = time.Now()
	}
	startNanos := probes.Started.UnixNano()
	var startedRef atomic.Int64
	startedRef.Store(startNanos)

	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		env := HealthzEnvelope{
			Health:        HealthHealthy,
			Version:       probes.Version,
			UptimeSeconds: int64(time.Since(time.Unix(0, startedRef.Load())).Seconds()),
			Checks: HealthChecks{
				Process:  "ok",
				RESTAPI:  "ok",
				GRPCAPI:  "unknown",
				Watchers: []WatcherCheck{},
			},
		}

		// DB probe — failed DB is the canonical "failed" signal.
		dbStatus := "ok"
		if svc == nil || svc.Store == nil {
			dbStatus = "failed"
		} else if err := svc.Store.Health(ctx); err != nil {
			dbStatus = "failed"
		}
		env.Checks.DB = DBCheck{Status: dbStatus}

		// Queue probe.
		env.Checks.Queue = queueDepthsFromService(ctx, svc)

		// gRPC probe — optional.
		if probes.GRPC != nil {
			if probes.GRPC(ctx) {
				env.Checks.GRPCAPI = "ok"
			} else {
				env.Checks.GRPCAPI = "failed"
			}
		}

		// Watcher introspection — optional. Stubbed to [] in T-0564 when
		// no provider is wired; T-0580 callers (or a future watcher API)
		// can supply richer data.
		if probes.Watchers != nil {
			if list := probes.Watchers(ctx); list != nil {
				env.Checks.Watchers = list
			}
		}

		// Upgrade probe — optional (T-0580). When present and reporting
		// in_progress, the top-level Health flips to "upgrading"; for
		// any other non-nil state we attach the envelope for visibility
		// but leave the verdict to computeHealth.
		if probes.Upgrade != nil {
			env.Upgrade = probes.Upgrade(ctx)
		}

		// Verdict.
		env.Health = computeHealth(env.Checks)
		if env.Upgrade != nil && env.Upgrade.State == "in_progress" {
			env.Health = HealthUpgrading
		}

		status := http.StatusOK
		if env.Health == HealthFailed {
			status = http.StatusServiceUnavailable
		}
		WriteJSON(w, status, env)
	}
}

// computeHealth derives the top-level verdict from the checks map.
//
//   - failed   = DB unreachable, gRPC explicitly failed, or process is
//     somehow unable to answer (the latter is a tautology — if you got
//     a response, process is alive).
//   - degraded = any non-fatal red signal (queue.failed > 0 OR a watcher
//     is stale OR gRPC reports "unknown" while a probe was supplied).
//   - healthy  = everything green.
func computeHealth(c HealthChecks) HealthStatus {
	if c.DB.Status != "ok" {
		return HealthFailed
	}
	if c.GRPCAPI == "failed" {
		return HealthFailed
	}
	if c.Queue.Failed > 0 {
		return HealthDegraded
	}
	for _, w := range c.Watchers {
		if w.LastEvent == "stale" {
			return HealthDegraded
		}
	}
	return HealthHealthy
}
