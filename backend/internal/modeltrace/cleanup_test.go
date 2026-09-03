package modeltrace

import (
	"context"
	"testing"
	"time"
)

// traceCleanupRepositoryStub 记录清理服务对存储层的意图，避免单元测试执行真实删除。
type traceCleanupRepositoryStub struct {
	preview      CleanupPreview
	deleted      CleanupResult
	previewCalls int
	deleteCalls  int
	startedRuns  int
	finishedRuns int
}

// PreviewExpired 返回预置的到期调用计数与存储量估算。
func (s *traceCleanupRepositoryStub) PreviewExpired(context.Context, time.Time) (CleanupPreview, error) {
	s.previewCalls++
	return s.preview, nil
}

// DeleteExpired 返回预置的一批删除结果。
func (s *traceCleanupRepositoryStub) DeleteExpired(context.Context, time.Time, int) (CleanupResult, error) {
	s.deleteCalls++
	return s.deleted, nil
}

// StartCleanupRun 返回确定的清理运行标识。
func (s *traceCleanupRepositoryStub) StartCleanupRun(context.Context, CleanupMode, *int64, time.Time) (int64, error) {
	s.startedRuns++
	return 1, nil
}

// FinishCleanupRun 记录终态统计但不写入真实审计表。
func (s *traceCleanupRepositoryStub) FinishCleanupRun(context.Context, int64, CleanupResult, error) error {
	s.finishedRuns++
	return nil
}

// TestCleanupServiceSkipsAutomaticRunWhenDisabled 验证自动清理开关关闭时不会发起删除或运行记录。
func TestCleanupServiceSkipsAutomaticRunWhenDisabled(t *testing.T) {
	repository := &traceCleanupRepositoryStub{}
	service := NewCleanupService(traceConfigStoreStub{config: TraceConfig{RetentionDays: 7}}, repository)

	result, err := service.RunAutomatic(context.Background())

	if err != nil {
		t.Fatalf("run automatic cleanup: %v", err)
	}
	if result.DeletedTraces != 0 || repository.deleteCalls != 0 || repository.startedRuns != 0 {
		t.Fatalf("disabled automatic cleanup result=%#v repository=%#v", result, repository)
	}
}

// TestCleanupServiceDeletesExpiredTracesInBatches 验证自动清理仅作用于已到期记录，并同步留下运行摘要。
func TestCleanupServiceDeletesExpiredTracesInBatches(t *testing.T) {
	repository := &traceCleanupRepositoryStub{deleted: CleanupResult{DeletedTraces: 2, DeletedPayloads: 3, DeletedBytes: 2048}}
	service := NewCleanupService(traceConfigStoreStub{config: TraceConfig{AutoCleanupEnabled: true, RetentionDays: 7}}, repository)
	fixedNow := time.Date(2026, time.September, 3, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return fixedNow }

	result, err := service.RunAutomatic(context.Background())

	if err != nil {
		t.Fatalf("run automatic cleanup: %v", err)
	}
	if result.DeletedTraces != 2 || result.DeletedPayloads != 3 || result.DeletedBytes != 2048 {
		t.Fatalf("cleanup result=%#v", result)
	}
	if repository.deleteCalls != 1 || repository.startedRuns != 1 || repository.finishedRuns != 1 {
		t.Fatalf("cleanup repository calls=%#v", repository)
	}
}

// TestCleanupServicePreviewNeverDeletes 验证管理员预览只返回影响范围，不创建运行记录也不删除数据。
func TestCleanupServicePreviewNeverDeletes(t *testing.T) {
	repository := &traceCleanupRepositoryStub{preview: CleanupPreview{ExpiredTraces: 5, ExpiredPayloads: 8, StoredBytes: 4096}}
	service := NewCleanupService(traceConfigStoreStub{config: TraceConfig{RetentionDays: 7}}, repository)

	preview, err := service.Preview(context.Background())

	if err != nil {
		t.Fatalf("preview cleanup: %v", err)
	}
	if preview.ExpiredTraces != 5 || repository.previewCalls != 1 || repository.deleteCalls != 0 || repository.startedRuns != 0 {
		t.Fatalf("cleanup preview=%#v repository=%#v", preview, repository)
	}
}
