package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// approvalStore is the approval mechanism's store (migration 42,
// mirroring SQLite 061): approval_requests and approval_decisions.
// Every statement runs on conn(ctx), so the store joins a transaction
// InTx put in the context.
type approvalStore struct{ db *sql.DB }

const approvalColumns = `id, kind, action, requested_by, subject_id, reason, payload, payload_digest,
	preconditions, approvals_required, status, requested_at, expires_at, decided_at, result_ref`

func (s *approvalStore) InTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return inTx(ctx, s.db, fn)
}

func (s *approvalStore) Create(ctx context.Context, r *storage.ApprovalRequest) error {
	if err := storage.PrepareNewApprovalRequest(r, time.Now()); err != nil {
		return err
	}
	var subject any
	if r.SubjectID != "" {
		subject = r.SubjectID
	}
	_, err := conn(ctx, s.db).ExecContext(ctx, `INSERT INTO approval_requests (`+approvalColumns+`)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, NULL, '')`,
		r.ID, r.Kind, r.Action, r.RequestedBy, subject, r.Reason, string(r.Payload), r.PayloadDigest,
		string(r.Preconditions), r.ApprovalsRequired, string(r.Status), r.RequestedAt.UTC(), r.ExpiresAt.UTC())
	if sentinel := constraintErr(err); sentinel != nil {
		return fmt.Errorf("approval request %q: %w", r.ID, sentinel)
	}
	if err != nil {
		return fmt.Errorf("approval request create: %w", err)
	}
	return nil
}

func scanApprovalRequest(row interface{ Scan(...any) error }) (*storage.ApprovalRequest, error) {
	r := &storage.ApprovalRequest{}
	var subject sql.NullString
	var payload, preconditions, status string
	var decided sql.NullTime
	if err := row.Scan(&r.ID, &r.Kind, &r.Action, &r.RequestedBy, &subject, &r.Reason, &payload, &r.PayloadDigest,
		&preconditions, &r.ApprovalsRequired, &status, &r.RequestedAt, &r.ExpiresAt, &decided, &r.ResultRef); err != nil {
		return nil, err
	}
	r.SubjectID = subject.String
	r.Payload = json.RawMessage(payload)
	r.Preconditions = json.RawMessage(preconditions)
	r.Status = storage.ApprovalStatus(status)
	r.RequestedAt = r.RequestedAt.UTC()
	r.ExpiresAt = r.ExpiresAt.UTC()
	r.DecidedAt = timePtr(decided)
	r.Decisions = []storage.ApprovalDecision{}
	return r, nil
}

func (s *approvalStore) get(ctx context.Context, id, lock string) (*storage.ApprovalRequest, error) {
	q := conn(ctx, s.db)
	r, err := scanApprovalRequest(q.QueryRowContext(ctx, `SELECT `+approvalColumns+` FROM approval_requests WHERE id = $1`+lock, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("approval request %q: %w", id, storage.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("approval request %q: %w", id, err)
	}
	if err := fillApprovalDecisions(ctx, q, []*storage.ApprovalRequest{r}); err != nil {
		return nil, err
	}
	return r, nil
}

func (s *approvalStore) Get(ctx context.Context, id string) (*storage.ApprovalRequest, error) {
	return s.get(ctx, id, "")
}

// GetForUpdate locks the request's row (SELECT ... FOR UPDATE) until the
// transaction of ctx ends: a second decider blocks on it and then reads
// the row as the first left it.
func (s *approvalStore) GetForUpdate(ctx context.Context, id string) (*storage.ApprovalRequest, error) {
	return s.get(ctx, id, " FOR UPDATE")
}

func (s *approvalStore) List(ctx context.Context, f storage.ApprovalFilter) ([]*storage.ApprovalRequest, error) {
	var where []string
	var args []any
	for _, c := range []struct{ col, val string }{
		{"kind", f.Kind}, {"status", string(f.Status)}, {"requested_by", f.RequestedBy}, {"subject_id", f.SubjectID},
	} {
		if c.val != "" {
			args = append(args, c.val)
			where = append(where, fmt.Sprintf("%s = $%d", c.col, len(args)))
		}
	}
	query := `SELECT ` + approvalColumns + ` FROM approval_requests`
	if len(where) > 0 {
		query += ` WHERE ` + strings.Join(where, " AND ")
	}
	query += ` ORDER BY requested_at DESC, id DESC`
	if f.Limit > 0 {
		args = append(args, f.Limit)
		query += fmt.Sprintf(` LIMIT $%d`, len(args))
	}
	q := conn(ctx, s.db)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("approval requests: %w", err)
	}
	defer rows.Close()
	out := []*storage.ApprovalRequest{}
	for rows.Next() {
		r, err := scanApprovalRequest(rows)
		if err != nil {
			return nil, fmt.Errorf("approval requests: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("approval requests: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("approval requests: %w", err)
	}
	if err := fillApprovalDecisions(ctx, q, out); err != nil {
		return nil, err
	}
	return out, nil
}

// fillApprovalDecisions loads the decisions of reqs, oldest first per
// request.
func fillApprovalDecisions(ctx context.Context, q querier, reqs []*storage.ApprovalRequest) error {
	if len(reqs) == 0 {
		return nil
	}
	byID := make(map[string]*storage.ApprovalRequest, len(reqs))
	ids := make([]string, 0, len(reqs))
	for _, r := range reqs {
		byID[r.ID] = r
		ids = append(ids, r.ID)
	}
	rows, err := q.QueryContext(ctx, `SELECT request_id, approver_id, decision, payload_digest, note, decided_at
		FROM approval_decisions WHERE request_id = ANY($1)
		ORDER BY decided_at, approver_id`, pq.Array(ids))
	if err != nil {
		return fmt.Errorf("approval decisions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var d storage.ApprovalDecision
		var verdict string
		if err := rows.Scan(&d.RequestID, &d.ApproverID, &verdict, &d.PayloadDigest, &d.Note, &d.DecidedAt); err != nil {
			return fmt.Errorf("approval decisions: %w", err)
		}
		d.Decision = storage.ApprovalVerdict(verdict)
		d.DecidedAt = d.DecidedAt.UTC()
		byID[d.RequestID].Decisions = append(byID[d.RequestID].Decisions, d)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("approval decisions: %w", err)
	}
	return nil
}

func (s *approvalStore) AddDecision(ctx context.Context, d *storage.ApprovalDecision) error {
	if err := storage.PrepareApprovalDecision(d, time.Now()); err != nil {
		return err
	}
	_, err := conn(ctx, s.db).ExecContext(ctx, `INSERT INTO approval_decisions
		(request_id, approver_id, decision, payload_digest, note, decided_at) VALUES ($1, $2, $3, $4, $5, $6)`,
		d.RequestID, d.ApproverID, string(d.Decision), d.PayloadDigest, d.Note, d.DecidedAt.UTC())
	if sentinel := constraintErr(err); sentinel != nil {
		return fmt.Errorf("decision of %q on %q: %w", d.ApproverID, d.RequestID, sentinel)
	}
	if err != nil {
		return fmt.Errorf("approval decision: %w", err)
	}
	return nil
}

func (s *approvalStore) Transition(ctx context.Context, id string, to storage.ApprovalStatus, at time.Time, resultRef string) error {
	if err := storage.ValidateApprovalTransition(to); err != nil {
		return err
	}
	q := conn(ctx, s.db)
	res, err := q.ExecContext(ctx, `UPDATE approval_requests SET status = $1, decided_at = $2, result_ref = $3
		WHERE id = $4 AND status = $5`, string(to), at.UTC(), resultRef, id, string(storage.ApprovalRequested))
	if err != nil {
		return fmt.Errorf("approval transition %q: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 1 {
		return nil
	}
	var status string
	err = q.QueryRowContext(ctx, `SELECT status FROM approval_requests WHERE id = $1`, id).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("approval request %q: %w", id, storage.ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("approval transition %q: %w", id, err)
	}
	return fmt.Errorf("approval request %q is %s: %w", id, status, storage.ErrApprovalNotPending)
}
