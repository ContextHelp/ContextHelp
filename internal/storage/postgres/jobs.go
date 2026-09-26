package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// errJobNotFound is returned by scanJob when no row matches.
//
// Mirrors the sqlite driver's sentinel so the same condition is the
// same error across backends: storage.ErrNotFound is what the CLI
// matches to answer NOT_FOUND, and sql.ErrNoRows is what AcquireNext
// reads as "queue empty", not as a failure. Neither sentinel's text
// enters the message — see the sqlite driver for why joining them
// would put "sql: no rows in result set" back into Error().
var errJobNotFound error = jobNotFoundError{}

// jobNotFoundError is the "no such job" fact, phrased for whoever reads
// it and matchable by either sentinel.
type jobNotFoundError struct{}

func (jobNotFoundError) Error() string { return "job not found" }

// Is reports both sentinels without embedding their text.
func (jobNotFoundError) Is(target error) bool {
	return target == storage.ErrNotFound || target == sql.ErrNoRows
}

func (s *JobStore) Create(ctx context.Context, job *storage.Job) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO jobs (
		id, type, status, payload, pipeline, source, result_id, error,
		retry_count, max_retries, created_at, updated_at, started_at, completed_at,
		user_mentions, user_hints, user_profile, user_note, idempotency_key
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)`,
		job.ID, job.Type, string(job.Status), job.Payload, job.Pipeline, job.Source,
		job.ResultID, job.Error, job.RetryCount, job.MaxRetries,
		job.CreatedAt.UTC(), job.UpdatedAt.UTC(),
		nullableTime(job.StartedAt), nullableTime(job.CompletedAt),
		encodeUserStrings(job.UserMentions),
		encodeUserStrings(job.UserHints),
		job.UserProfile, job.UserNote, job.IdempotencyKey,
	)
	if err != nil {
		return fmt.Errorf("create job: %w", err)
	}
	return nil
}

// encodeUserStrings marshals a caller-asserted string slice to JSON for
// columns like user_mentions and user_hints. Empty slice → empty string
// (DB default is empty string; matches the column DEFAULT clause set
// in the migrations).
func encodeUserStrings(m []string) string {
	if len(m) == 0 {
		return ""
	}
	b, _ := json.Marshal(m)
	return string(b)
}

// decodeUserStrings parses a JSON-encoded string-array column back to a
// slice. Empty or invalid JSON → nil so callers can len()-check uniformly.
func decodeUserStrings(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil
	}
	return out
}

func (s *JobStore) Get(ctx context.Context, id string) (*storage.Job, error) {
	row := s.db.QueryRowContext(ctx, `SELECT
		id, type, status, payload, pipeline, source, result_id, error,
		retry_count, max_retries, created_at, updated_at, started_at, completed_at,
		user_mentions, user_hints, user_profile, user_note, idempotency_key
	FROM jobs WHERE id = $1`, id)
	return scanJob(row)
}

func (s *JobStore) List(ctx context.Context, filter storage.JobFilter) ([]*storage.Job, int, error) {
	var conditions []string
	var args []any
	idx := 1

	if filter.Status != "" {
		conditions = append(conditions, fmt.Sprintf("status = $%d", idx))
		args = append(args, string(filter.Status))
		idx++
	}
	if filter.Type != "" {
		conditions = append(conditions, fmt.Sprintf("type = $%d", idx))
		args = append(args, filter.Type)
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM jobs "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count jobs: %w", err)
	}

	query := `SELECT
		id, type, status, payload, pipeline, source, result_id, error,
		retry_count, max_retries, created_at, updated_at, started_at, completed_at,
		user_mentions, user_hints, user_profile, user_note, idempotency_key
	FROM jobs ` + where + " ORDER BY created_at DESC"

	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", filter.Limit)
	}
	if filter.Offset > 0 {
		query += fmt.Sprintf(" OFFSET %d", filter.Offset) // #nosec G202 -- integer value
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list jobs: %w", err)
	}
	defer rows.Close()

	var jobs []*storage.Job
	for rows.Next() {
		j, err := scanJobRow(rows)
		if err != nil {
			return nil, 0, err
		}
		jobs = append(jobs, j)
	}
	return jobs, total, rows.Err()
}

func (s *JobStore) AcquireNext(ctx context.Context) (*storage.Job, error) {
	now := time.Now().UTC()

	// SKIP LOCKED hands concurrent acquirers different rows. Each claim
	// mints a new token and starts unleased; see ExtendLease.
	claim := uuid.NewString()
	row := s.db.QueryRowContext(ctx, `UPDATE jobs SET
		status = 'running', started_at = $1, updated_at = $1,
		claim_token = $2, lease_expires_at = NULL
	WHERE id = (
		SELECT id FROM jobs WHERE status = 'pending' ORDER BY created_at ASC LIMIT 1 FOR UPDATE SKIP LOCKED
	) AND status = 'pending'
	RETURNING id, type, status, payload, pipeline, source, result_id, error,
		retry_count, max_retries, created_at, updated_at, started_at, completed_at,
		user_mentions, user_hints, user_profile, user_note, idempotency_key`, now, claim)

	j, err := scanJob(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("acquire next: %w", err)
	}
	j.Claim = claim
	return j, nil
}

func (s *JobStore) Complete(ctx context.Context, id string, resultID string) error {
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx,
		"UPDATE jobs SET status = 'completed', result_id = $1, completed_at = $2, updated_at = $2 WHERE id = $3",
		resultID, now, id)
	if err != nil {
		return fmt.Errorf("complete job: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("job %s not found", id)
	}
	return nil
}

func (s *JobStore) Fail(ctx context.Context, id string, errMsg string) error {
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx,
		"UPDATE jobs SET status = 'failed', error = $1, updated_at = $2 WHERE id = $3",
		errMsg, now, id)
	if err != nil {
		return fmt.Errorf("fail job: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("job %s not found", id)
	}
	return nil
}

func (s *JobStore) Retry(ctx context.Context, id string) error {
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, `UPDATE jobs SET
		status = 'pending', error = '', started_at = NULL, completed_at = NULL,
		claim_token = '', lease_expires_at = NULL,
		retry_count = retry_count + 1, updated_at = $1
	WHERE id = $2 AND retry_count < max_retries`, now, id)
	if err != nil {
		return fmt.Errorf("retry job: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("job %s not found or max retries exceeded", id)
	}
	return nil
}

func (s *JobStore) Cancel(ctx context.Context, id string) error {
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx,
		"UPDATE jobs SET status = 'cancelled', updated_at = $1 WHERE id = $2 AND status IN ('pending', 'running')",
		now, id)
	if err != nil {
		return fmt.Errorf("cancel job: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("job %s not found or not cancellable", id)
	}
	return nil
}

func (s *JobStore) RecoverStale(ctx context.Context, timeoutSeconds int64) (int, error) {
	cutoff := time.Now().Add(-time.Duration(timeoutSeconds) * time.Second).UTC()
	now := time.Now().UTC()

	// Leases are stamped and compared on the database clock, so two
	// hosts with skewed clocks agree on expiry.
	result, err := s.db.ExecContext(ctx, `UPDATE jobs SET
		status = 'pending', started_at = NULL, claim_token = '', lease_expires_at = NULL, updated_at = $1
	WHERE status = 'running' AND CASE
		WHEN lease_expires_at IS NULL THEN started_at <= $2
		ELSE lease_expires_at <= now()
	END`, now, cutoff)
	if err != nil {
		return 0, fmt.Errorf("recover stale: %w", err)
	}
	n, _ := result.RowsAffected()
	return int(n), nil
}

func (s *JobStore) ExtendLease(ctx context.Context, id, claim string, ttl time.Duration) (bool, error) {
	if claim == "" {
		return false, nil
	}
	result, err := s.db.ExecContext(ctx, `UPDATE jobs
		SET lease_expires_at = now() + make_interval(secs => $1)
		WHERE id = $2 AND claim_token = $3 AND status = 'running'`,
		ttl.Seconds(), id, claim)
	if err != nil {
		return false, fmt.Errorf("extend lease: %w", err)
	}
	n, _ := result.RowsAffected()
	return n == 1, nil
}

func (s *JobStore) ReleaseLease(ctx context.Context, id, claim string) error {
	if claim == "" {
		return nil
	}
	if _, err := s.db.ExecContext(ctx,
		"UPDATE jobs SET lease_expires_at = NULL WHERE id = $1 AND claim_token = $2 AND status = 'running'",
		id, claim); err != nil {
		return fmt.Errorf("release lease: %w", err)
	}
	return nil
}

func scanJob(row *sql.Row) (*storage.Job, error) {
	var j storage.Job
	var status string
	var startedAt, completedAt sql.NullTime
	var userMentions, userHints, userProfile, userNote, idemKey sql.NullString

	err := row.Scan(&j.ID, &j.Type, &status, &j.Payload, &j.Pipeline, &j.Source,
		&j.ResultID, &j.Error, &j.RetryCount, &j.MaxRetries,
		&j.CreatedAt, &j.UpdatedAt, &startedAt, &completedAt, &userMentions, &userHints, &userProfile, &userNote, &idemKey)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errJobNotFound
		}
		return nil, fmt.Errorf("scan job: %w", err)
	}
	j.Status = storage.JobStatus(status)
	if startedAt.Valid {
		t := startedAt.Time
		j.StartedAt = &t
	}
	if completedAt.Valid {
		t := completedAt.Time
		j.CompletedAt = &t
	}
	if userMentions.Valid {
		j.UserMentions = decodeUserStrings(userMentions.String)
	}
	if userHints.Valid {
		j.UserHints = decodeUserStrings(userHints.String)
	}
	if userProfile.Valid {
		j.UserProfile = userProfile.String
	}
	if userNote.Valid {
		j.UserNote = userNote.String
	}
	if idemKey.Valid {
		j.IdempotencyKey = idemKey.String
	}
	return &j, nil
}

func scanJobRow(rows *sql.Rows) (*storage.Job, error) {
	var j storage.Job
	var status string
	var startedAt, completedAt sql.NullTime
	var userMentions, userHints, userProfile, userNote, idemKey sql.NullString

	err := rows.Scan(&j.ID, &j.Type, &status, &j.Payload, &j.Pipeline, &j.Source,
		&j.ResultID, &j.Error, &j.RetryCount, &j.MaxRetries,
		&j.CreatedAt, &j.UpdatedAt, &startedAt, &completedAt, &userMentions, &userHints, &userProfile, &userNote, &idemKey)
	if err != nil {
		return nil, fmt.Errorf("scan job row: %w", err)
	}
	j.Status = storage.JobStatus(status)
	if startedAt.Valid {
		t := startedAt.Time
		j.StartedAt = &t
	}
	if completedAt.Valid {
		t := completedAt.Time
		j.CompletedAt = &t
	}
	if userMentions.Valid {
		j.UserMentions = decodeUserStrings(userMentions.String)
	}
	if userHints.Valid {
		j.UserHints = decodeUserStrings(userHints.String)
	}
	if userProfile.Valid {
		j.UserProfile = userProfile.String
	}
	if userNote.Valid {
		j.UserNote = userNote.String
	}
	if idemKey.Valid {
		j.IdempotencyKey = idemKey.String
	}
	return &j, nil
}

// GetByIdempotencyKey returns the job carrying the given non-empty
// idempotency key, or (nil, nil) when no such job exists. The empty key is
// the "no dedupe" sentinel and never matches a row.
func (s *JobStore) GetByIdempotencyKey(ctx context.Context, key string) (*storage.Job, error) {
	if key == "" {
		return nil, nil
	}
	row := s.db.QueryRowContext(ctx, `SELECT
		id, type, status, payload, pipeline, source, result_id, error,
		retry_count, max_retries, created_at, updated_at, started_at, completed_at,
		user_mentions, user_hints, user_profile, user_note, idempotency_key
	FROM jobs WHERE idempotency_key = $1`, key)
	j, err := scanJob(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get job by idempotency key: %w", err)
	}
	return j, nil
}

func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC()
}
