package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// approvalStore is the approval mechanism's store (migration 061):
// approval_requests and approval_decisions. Times are Unix
// milliseconds, UTC. Every statement runs on conn(ctx), so the store
// joins a transaction InTx put in the context.
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
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, '')`,
		r.ID, r.Kind, r.Action, r.RequestedBy, subject, r.Reason, string(r.Payload), r.PayloadDigest,
		string(r.Preconditions), r.ApprovalsRequired, string(r.Status), toMillis(r.RequestedAt), toMillis(r.ExpiresAt))
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
	var requested, expires int64
	var decided sql.NullInt64
	if err := row.Scan(&r.ID, &r.Kind, &r.Action, &r.RequestedBy, &subject, &r.Reason, &payload, &r.PayloadDigest,
		&preconditions, &r.ApprovalsRequired, &status, &requested, &expires, &decided, &r.ResultRef); err != nil {
		return nil, err
	}
	r.SubjectID = subject.String
	r.Payload = json.RawMessage(payload)
	r.Preconditions = json.RawMessage(preconditions)
	r.Status = storage.ApprovalStatus(status)
	r.RequestedAt = fromMillis(requested)
	r.ExpiresAt = fromMillis(expires)
	r.DecidedAt = millisPtr(decided)
	r.Decisions = []storage.ApprovalDecision{}
	return r, nil
}

func (s *approvalStore) Get(ctx context.Context, id string) (*storage.ApprovalRequest, error) {
	q := conn(ctx, s.db)
	r, err := scanApprovalRequest(q.QueryRowContext(ctx, `SELECT `+approvalColumns+` FROM approval_requests WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("approval request %q: %w", id, storage.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("approval request %q: %w", id, err)
	}
	if err := s.fillDecisions(ctx, q, []*storage.ApprovalRequest{r}); err != nil {
		return nil, err
	}
	return r, nil
}

// GetForUpdate is Get: the transaction InTx opens holds SQLite's write
// lock from BEGIN (_txlock=immediate), which already serialises every
// decider.
func (s *approvalStore) GetForUpdate(ctx context.Context, id string) (*storage.ApprovalRequest, error) {
	return s.Get(ctx, id)
}

func (s *approvalStore) List(ctx context.Context, f storage.ApprovalFilter) ([]*storage.ApprovalRequest, error) {
	var where []string
	var args []any
	for _, c := range []struct{ col, val string }{
		{"kind", f.Kind}, {"status", string(f.Status)}, {"requested_by", f.RequestedBy}, {"subject_id", f.SubjectID},
	} {
		if c.val != "" {
			where = append(where, c.col+" = ?")
			args = append(args, c.val)
		}
	}
	query := `SELECT ` + approvalColumns + ` FROM approval_requests`
	if len(where) > 0 {
		query += ` WHERE ` + strings.Join(where, " AND ")
	}
	// LIMIT -1 is no limit in SQLite.
	limit := -1
	if f.Limit > 0 {
		limit = f.Limit
	}
	query += ` ORDER BY requested_at DESC, id DESC LIMIT ?`
	args = append(args, limit)
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
	if err := s.fillDecisions(ctx, q, out); err != nil {
		return nil, err
	}
	return out, nil
}

// fillDecisions loads the decisions of reqs, oldest first per request.
func (s *approvalStore) fillDecisions(ctx context.Context, q querier, reqs []*storage.ApprovalRequest) error {
	if len(reqs) == 0 {
		return nil
	}
	byID := make(map[string]*storage.ApprovalRequest, len(reqs))
	marks := make([]string, 0, len(reqs))
	args := make([]any, 0, len(reqs))
	for _, r := range reqs {
		byID[r.ID] = r
		marks = append(marks, "?")
		args = append(args, r.ID)
	}
	rows, err := q.QueryContext(ctx, `SELECT request_id, approver_id, decision, payload_digest, note, decided_at
		FROM approval_decisions WHERE request_id IN (`+strings.Join(marks, ", ")+`)
		ORDER BY decided_at, approver_id`, args...)
	if err != nil {
		return fmt.Errorf("approval decisions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var d storage.ApprovalDecision
		var verdict string
		var at int64
		if err := rows.Scan(&d.RequestID, &d.ApproverID, &verdict, &d.PayloadDigest, &d.Note, &at); err != nil {
			return fmt.Errorf("approval decisions: %w", err)
		}
		d.Decision = storage.ApprovalVerdict(verdict)
		d.DecidedAt = fromMillis(at)
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
		(request_id, approver_id, decision, payload_digest, note, decided_at) VALUES (?, ?, ?, ?, ?, ?)`,
		d.RequestID, d.ApproverID, string(d.Decision), d.PayloadDigest, d.Note, toMillis(d.DecidedAt))
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
	res, err := q.ExecContext(ctx, `UPDATE approval_requests SET status = ?, decided_at = ?, result_ref = ?
		WHERE id = ? AND status = ?`, string(to), toMillis(at), resultRef, id, string(storage.ApprovalRequested))
	if err != nil {
		return fmt.Errorf("approval transition %q: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 1 {
		return nil
	}
	var status string
	err = q.QueryRowContext(ctx, `SELECT status FROM approval_requests WHERE id = ?`, id).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("approval request %q: %w", id, storage.ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("approval transition %q: %w", id, err)
	}
	return fmt.Errorf("approval request %q is %s: %w", id, status, storage.ErrApprovalNotPending)
}
