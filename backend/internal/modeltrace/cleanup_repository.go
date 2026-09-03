package modeltrace

import (
	"context"
	"fmt"
	"time"
)

// PreviewExpired 汇总到期调用及其级联正文占用，供管理员确认前展示影响范围。
func (r *PostgresRepository) PreviewExpired(ctx context.Context, cutoff time.Time) (CleanupPreview, error) {
	if r == nil || r.db == nil {
		return CleanupPreview{}, fmt.Errorf("model trace database is unavailable")
	}
	preview := CleanupPreview{CutoffAt: cutoff.UTC()}
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT t.id), COUNT(p.id), COALESCE(SUM(p.stored_bytes), 0)
		FROM model_call_traces t
		LEFT JOIN model_call_payloads p ON p.model_call_trace_id=t.id
		WHERE t.expires_at <= $1
	`, preview.CutoffAt).Scan(&preview.ExpiredTraces, &preview.ExpiredPayloads, &preview.StoredBytes)
	if err != nil {
		return CleanupPreview{}, fmt.Errorf("preview expired model traces: %w", err)
	}
	return preview, nil
}

// DeleteExpired 删除一个有界批次的到期调用，并在级联前计算被删正文的统计值。
func (r *PostgresRepository) DeleteExpired(ctx context.Context, cutoff time.Time, batchSize int) (CleanupResult, error) {
	if r == nil || r.db == nil {
		return CleanupResult{}, fmt.Errorf("model trace database is unavailable")
	}
	if batchSize < 1 {
		return CleanupResult{}, fmt.Errorf("model trace cleanup batch size must be positive")
	}
	result := CleanupResult{}
	err := r.db.QueryRowContext(ctx, `
		WITH targets AS MATERIALIZED (
			SELECT id
			FROM model_call_traces
			WHERE expires_at <= $1
			ORDER BY expires_at ASC, id ASC
			LIMIT $2
		), metrics AS MATERIALIZED (
			SELECT COUNT(DISTINCT target.id) AS deleted_traces,
				COUNT(payload.id) AS deleted_payloads,
				COALESCE(SUM(payload.stored_bytes), 0) AS deleted_bytes
			FROM targets target
			LEFT JOIN model_call_payloads payload ON payload.model_call_trace_id=target.id
		), deleted AS (
			DELETE FROM model_call_traces trace
			USING targets target
			WHERE trace.id=target.id
			RETURNING trace.id
		)
		SELECT (SELECT COUNT(*) FROM deleted), metrics.deleted_payloads, metrics.deleted_bytes
		FROM metrics
	`, cutoff.UTC(), batchSize).Scan(&result.DeletedTraces, &result.DeletedPayloads, &result.DeletedBytes)
	if err != nil {
		return CleanupResult{}, fmt.Errorf("delete expired model traces: %w", err)
	}
	return result, nil
}

// StartCleanupRun 创建不含正文的清理运行摘要，便于审计自动和手动删除的影响。
func (r *PostgresRepository) StartCleanupRun(ctx context.Context, mode CleanupMode, requestedBy *int64, cutoff time.Time) (int64, error) {
	if r == nil || r.db == nil {
		return 0, fmt.Errorf("model trace database is unavailable")
	}
	var runID int64
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO model_call_trace_cleanup_runs (mode, requested_by, cutoff_at, status)
		VALUES ($1, $2, $3, 'running')
		RETURNING id
	`, string(mode), requestedBy, cutoff.UTC()).Scan(&runID)
	if err != nil {
		return 0, fmt.Errorf("start model trace cleanup run: %w", err)
	}
	return runID, nil
}

// FinishCleanupRun 写入清理结果；底层错误仅归类为稳定代码，绝不写入异常原文。
func (r *PostgresRepository) FinishCleanupRun(ctx context.Context, runID int64, result CleanupResult, runErr error) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("model trace database is unavailable")
	}
	status := "succeeded"
	errorCode := ""
	if runErr != nil {
		status = "failed"
		errorCode = "cleanup_failed"
	}
	_, err := r.db.ExecContext(ctx, `
		UPDATE model_call_trace_cleanup_runs
		SET status=$2, deleted_traces=$3, deleted_payloads=$4, deleted_bytes=$5,
			error_code=$6, finished_at=$7
		WHERE id=$1
	`, runID, status, result.DeletedTraces, result.DeletedPayloads, result.DeletedBytes, errorCode, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("finish model trace cleanup run: %w", err)
	}
	return nil
}
