package postgres

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

func (s *JobStore) Create(ctx context.Context, job *storage.Job) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO jobs (
		id, type, status, payload, pipeline, source, result_id, error,
		retry_count, max_retries, created_at, updated_at, started_at, completed_at,
		user_mentions
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
		job.ID, job.Type, string(job.Status), job.Payload, job.Pipeline, job.Source,
		job.ResultID, job.Error, job.RetryCount, job.MaxRetries,
		job.CreatedAt.UTC(), job.UpdatedAt.UTC(),
		nullableTime(job.StartedAt), nullableTime(job.CompletedAt),
		encodeUserMentions(job.UserMentions),
	)
	if err != nil {
		return fmt.Errorf("create job: %w", err)
	}
	return nil
}

// encodeUserMentions marshals caller-asserted mentions to JSON for the
// user_mentions column. Empty slice → empty string (DB default).
func encodeUserMentions(m []string) string {
	if len(m) == 0 {
		return ""
	}
	b, _ := json.Marshal(m)
	return string(b)
}

// decodeUserMentions parses the user_mentions column back to a slice. Empty
// or invalid JSON → nil.
func decodeUserMentions(s string) []string {
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
		user_mentions
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
		idx++
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
		user_mentions
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

	row := s.db.QueryRowContext(ctx, `UPDATE jobs SET
		status = 'running', started_at = $1, updated_at = $1
	WHERE id = (
		SELECT id FROM jobs WHERE status = 'pending' ORDER BY created_at ASC LIMIT 1 FOR UPDATE SKIP LOCKED
	)
	RETURNING id, type, status, payload, pipeline, source, result_id, error,
		retry_count, max_retries, created_at, updated_at, started_at, completed_at,
		user_mentions`, now)

	j, err := scanJob(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("acquire next: %w", err)
	}
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

	result, err := s.db.ExecContext(ctx,
		"UPDATE jobs SET status = 'pending', started_at = NULL, updated_at = $1 WHERE status = 'running' AND started_at <= $2",
		now, cutoff)
	if err != nil {
		return 0, fmt.Errorf("recover stale: %w", err)
	}
	n, _ := result.RowsAffected()
	return int(n), nil
}

func scanJob(row *sql.Row) (*storage.Job, error) {
	var j storage.Job
	var status string
	var startedAt, completedAt sql.NullTime
	var userMentions sql.NullString

	err := row.Scan(&j.ID, &j.Type, &status, &j.Payload, &j.Pipeline, &j.Source,
		&j.ResultID, &j.Error, &j.RetryCount, &j.MaxRetries,
		&j.CreatedAt, &j.UpdatedAt, &startedAt, &completedAt, &userMentions)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, sql.ErrNoRows
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
		j.UserMentions = decodeUserMentions(userMentions.String)
	}
	return &j, nil
}

func scanJobRow(rows *sql.Rows) (*storage.Job, error) {
	var j storage.Job
	var status string
	var startedAt, completedAt sql.NullTime
	var userMentions sql.NullString

	err := rows.Scan(&j.ID, &j.Type, &status, &j.Payload, &j.Pipeline, &j.Source,
		&j.ResultID, &j.Error, &j.RetryCount, &j.MaxRetries,
		&j.CreatedAt, &j.UpdatedAt, &startedAt, &completedAt, &userMentions)
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
		j.UserMentions = decodeUserMentions(userMentions.String)
	}
	return &j, nil
}

func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC()
}
