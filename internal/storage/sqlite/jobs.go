package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type JobStore struct {
	db *sql.DB
}

func (s *JobStore) Create(ctx context.Context, job *storage.Job) error {
	var startedAt, completedAt *string
	if job.StartedAt != nil {
		v := job.StartedAt.Format(time.RFC3339)
		startedAt = &v
	}
	if job.CompletedAt != nil {
		v := job.CompletedAt.Format(time.RFC3339)
		completedAt = &v
	}

	_, err := s.db.ExecContext(ctx, `INSERT INTO jobs (
		id, type, status, payload, pipeline, source, result_id, error,
		retry_count, max_retries, created_at, updated_at, started_at, completed_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		job.ID, job.Type, string(job.Status), job.Payload, job.Pipeline, job.Source,
		job.ResultID, job.Error, job.RetryCount, job.MaxRetries,
		job.CreatedAt.Format(time.RFC3339), job.UpdatedAt.Format(time.RFC3339),
		startedAt, completedAt,
	)
	if err != nil {
		return fmt.Errorf("create job: %w", err)
	}
	return nil
}

func (s *JobStore) Get(ctx context.Context, id string) (*storage.Job, error) {
	row := s.db.QueryRowContext(ctx, `SELECT
		id, type, status, payload, pipeline, source, result_id, error,
		retry_count, max_retries, created_at, updated_at, started_at, completed_at
	FROM jobs WHERE id = ?`, id)
	return scanJob(row)
}

func (s *JobStore) List(ctx context.Context, filter storage.JobFilter) ([]*storage.Job, int, error) {
	var conditions []string
	var args []any

	if filter.Status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, string(filter.Status))
	}
	if filter.Type != "" {
		conditions = append(conditions, "type = ?")
		args = append(args, filter.Type)
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + joinAnd(conditions)
	}

	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM jobs "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count jobs: %w", err)
	}

	query := `SELECT
		id, type, status, payload, pipeline, source, result_id, error,
		retry_count, max_retries, created_at, updated_at, started_at, completed_at
	FROM jobs ` + where + " ORDER BY created_at DESC"

	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", filter.Limit)
	}
	if filter.Offset > 0 {
		query += fmt.Sprintf(" OFFSET %d", filter.Offset)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list jobs: %w", err)
	}
	defer rows.Close()

	var jobs []*storage.Job
	for rows.Next() {
		j, err := scanJobFromRows(rows)
		if err != nil {
			return nil, 0, err
		}
		jobs = append(jobs, j)
	}
	return jobs, total, rows.Err()
}

func (s *JobStore) AcquireNext(ctx context.Context) (*storage.Job, error) {
	now := time.Now().Format(time.RFC3339)

	// SQLite doesn't support UPDATE...RETURNING in all versions, so use a transaction.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	var id string
	err = tx.QueryRowContext(ctx,
		"SELECT id FROM jobs WHERE status = 'pending' ORDER BY created_at ASC LIMIT 1",
	).Scan(&id)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("acquire next: %w", err)
	}

	_, err = tx.ExecContext(ctx,
		"UPDATE jobs SET status = 'running', started_at = ?, updated_at = ? WHERE id = ?",
		now, now, id)
	if err != nil {
		return nil, fmt.Errorf("set running: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit acquire: %w", err)
	}

	return s.Get(ctx, id)
}

func (s *JobStore) Complete(ctx context.Context, id string, resultID string) error {
	now := time.Now().Format(time.RFC3339)
	result, err := s.db.ExecContext(ctx,
		"UPDATE jobs SET status = 'completed', result_id = ?, completed_at = ?, updated_at = ? WHERE id = ?",
		resultID, now, now, id)
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
	now := time.Now().Format(time.RFC3339)
	result, err := s.db.ExecContext(ctx,
		"UPDATE jobs SET status = 'failed', error = ?, updated_at = ? WHERE id = ?",
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
	now := time.Now().Format(time.RFC3339)
	result, err := s.db.ExecContext(ctx, `UPDATE jobs SET
		status = 'pending', error = '', started_at = NULL, completed_at = NULL,
		retry_count = retry_count + 1, updated_at = ?
	WHERE id = ? AND retry_count < max_retries`, now, id)
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
	now := time.Now().Format(time.RFC3339)
	result, err := s.db.ExecContext(ctx,
		"UPDATE jobs SET status = 'cancelled', updated_at = ? WHERE id = ? AND status IN ('pending', 'running')",
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
	cutoff := time.Now().Add(-time.Duration(timeoutSeconds) * time.Second).Format(time.RFC3339)
	now := time.Now().Format(time.RFC3339)

	result, err := s.db.ExecContext(ctx,
		"UPDATE jobs SET status = 'pending', started_at = NULL, updated_at = ? WHERE status = 'running' AND started_at <= ?",
		now, cutoff)
	if err != nil {
		return 0, fmt.Errorf("recover stale: %w", err)
	}
	n, _ := result.RowsAffected()
	return int(n), nil
}

func scanJob(row *sql.Row) (*storage.Job, error) {
	var j storage.Job
	var status, createdAt, updatedAt string
	var startedAt, completedAt sql.NullString

	err := row.Scan(&j.ID, &j.Type, &status, &j.Payload, &j.Pipeline, &j.Source,
		&j.ResultID, &j.Error, &j.RetryCount, &j.MaxRetries,
		&createdAt, &updatedAt, &startedAt, &completedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("job not found")
		}
		return nil, fmt.Errorf("scan job: %w", err)
	}

	j.Status = storage.JobStatus(status)
	j.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	j.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	if startedAt.Valid {
		t, _ := time.Parse(time.RFC3339, startedAt.String)
		j.StartedAt = &t
	}
	if completedAt.Valid {
		t, _ := time.Parse(time.RFC3339, completedAt.String)
		j.CompletedAt = &t
	}
	return &j, nil
}

func scanJobFromRows(rows *sql.Rows) (*storage.Job, error) {
	var j storage.Job
	var status, createdAt, updatedAt string
	var startedAt, completedAt sql.NullString

	err := rows.Scan(&j.ID, &j.Type, &status, &j.Payload, &j.Pipeline, &j.Source,
		&j.ResultID, &j.Error, &j.RetryCount, &j.MaxRetries,
		&createdAt, &updatedAt, &startedAt, &completedAt)
	if err != nil {
		return nil, fmt.Errorf("scan job row: %w", err)
	}

	j.Status = storage.JobStatus(status)
	j.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	j.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	if startedAt.Valid {
		t, _ := time.Parse(time.RFC3339, startedAt.String)
		j.StartedAt = &t
	}
	if completedAt.Valid {
		t, _ := time.Parse(time.RFC3339, completedAt.String)
		j.CompletedAt = &t
	}
	return &j, nil
}

func joinAnd(parts []string) string {
	result := parts[0]
	for _, p := range parts[1:] {
		result += " AND " + p
	}
	return result
}

// marshalJSON is a small helper to marshal any value, returning "{}" on error for maps.
func marshalJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(data)
}
