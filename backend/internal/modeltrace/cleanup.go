package modeltrace

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

const (
	traceCleanupInterval   = 24 * time.Hour
	traceCleanupBatchSize  = 500
	traceCleanupMaxBatches = 100
	traceCleanupFinishWait = 5 * time.Second
)

// CleanupMode 标识一次清理由保留期任务还是管理员手动操作触发。
type CleanupMode string

const (
	// CleanupModeAutomatic 标识后台周期性保留期清理。
	CleanupModeAutomatic CleanupMode = "automatic"
	// CleanupModeManual 标识管理员确认后的即时清理。
	CleanupModeManual CleanupMode = "manual"
)

// CleanupPreview 是管理员确认前看到的到期数据影响范围，不包含任何正文。
type CleanupPreview struct {
	ExpiredTraces   int64     `json:"expired_traces"`
	ExpiredAttempts int64     `json:"expired_attempts"`
	ExpiredPayloads int64     `json:"expired_payloads"`
	StoredBytes     int64     `json:"stored_bytes"`
	CutoffAt        time.Time `json:"cutoff_at"`
}

// CleanupResult 是一轮批量删除的聚合统计，不包含被删除记录的内容。
type CleanupResult struct {
	DeletedTraces   int64 `json:"deleted_traces"`
	DeletedAttempts int64 `json:"deleted_attempts"`
	DeletedPayloads int64 `json:"deleted_payloads"`
	DeletedBytes    int64 `json:"deleted_bytes"`
}

// CleanupRepository 定义预览、批量删除和运行摘要的持久化边界。
type CleanupRepository interface {
	PreviewExpired(ctx context.Context, cutoff time.Time) (CleanupPreview, error)
	DeleteExpired(ctx context.Context, cutoff time.Time, batchSize int) (CleanupResult, error)
	StartCleanupRun(ctx context.Context, mode CleanupMode, requestedBy *int64, cutoff time.Time) (int64, error)
	FinishCleanupRun(ctx context.Context, runID int64, result CleanupResult, runErr error) error
}

// CleanupService 以独立保留期运行模型调用追踪清理，避免与用量记录清理混用。
type CleanupService struct {
	configStore ConfigStore
	repository  CleanupRepository
	now         func() time.Time
	interval    time.Duration
	batchSize   int

	startOnce   sync.Once
	stopOnce    sync.Once
	stopCh      chan struct{}
	doneCh      chan struct{}
	lifecycleMu sync.Mutex
	started     bool
	stopped     bool
	runMu       sync.Mutex
	running     bool
}

// NewCleanupService uses the default daily scan period and bounded batch size.
func NewCleanupService(configStore ConfigStore, repository CleanupRepository) *CleanupService {
	return &CleanupService{
		configStore: configStore,
		repository:  repository,
		now:         time.Now,
		interval:    traceCleanupInterval,
		batchSize:   traceCleanupBatchSize,
		stopCh:      make(chan struct{}),
		doneCh:      make(chan struct{}),
	}
}

// Start 启动周期扫描；实际删除仍受每次读取到的自动清理开关控制。
func (s *CleanupService) Start() {
	if s == nil || s.repository == nil || s.configStore == nil {
		return
	}
	s.startOnce.Do(func() {
		s.lifecycleMu.Lock()
		if s.stopped {
			s.lifecycleMu.Unlock()
			return
		}
		s.started = true
		s.lifecycleMu.Unlock()
		go func() {
			defer close(s.doneCh)
			s.runLoop()
		}()
	})
}

// Stop stops the periodic worker and waits for it before callers close the
// database. The supplied shutdown context bounds the wait without abandoning
// a caller that can still safely wait for in-flight cleanup finalization.
func (s *CleanupService) Stop(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.stopOnce.Do(func() {
		s.lifecycleMu.Lock()
		s.stopped = true
		close(s.stopCh)
		s.lifecycleMu.Unlock()
	})
	s.lifecycleMu.Lock()
	started := s.started
	s.lifecycleMu.Unlock()
	if !started {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-s.doneCh:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Preview 返回当前已过期记录的影响范围，绝不创建清理运行或执行删除。
func (s *CleanupService) Preview(ctx context.Context) (CleanupPreview, error) {
	if s == nil || s.repository == nil {
		return CleanupPreview{}, fmt.Errorf("model trace cleanup repository is unavailable")
	}
	cutoff := s.currentTime().UTC()
	preview, err := s.repository.PreviewExpired(ctx, cutoff)
	if err != nil {
		return CleanupPreview{}, fmt.Errorf("preview expired model traces: %w", err)
	}
	preview.CutoffAt = cutoff
	return preview, nil
}

// RunAutomatic 仅在当前设置启用自动清理时删除已到期记录。
func (s *CleanupService) RunAutomatic(ctx context.Context) (CleanupResult, error) {
	if s == nil || s.configStore == nil {
		return CleanupResult{}, nil
	}
	config, err := s.configStore.Load(ctx)
	if err != nil {
		return CleanupResult{}, fmt.Errorf("load model trace cleanup config: %w", err)
	}
	if !config.AutoCleanupEnabled {
		return CleanupResult{}, nil
	}
	return s.run(ctx, CleanupModeAutomatic, nil)
}

// RunManual 执行管理员已确认的即时清理，并以操作者标识写入运行摘要。
func (s *CleanupService) RunManual(ctx context.Context, requestedBy int64) (CleanupResult, error) {
	if requestedBy <= 0 {
		return CleanupResult{}, fmt.Errorf("model trace cleanup operator is required")
	}
	return s.run(ctx, CleanupModeManual, &requestedBy)
}

// runLoop 在启动后和每天一次执行受开关约束的自动清理。
func (s *CleanupService) runLoop() {
	s.runAutomaticOnce()
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.runAutomaticOnce()
		case <-s.stopCh:
			return
		}
	}
}

// runAutomaticOnce 使用有限超时避免后台清理无限占用数据库连接。
func (s *CleanupService) runAutomaticOnce() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := s.RunAutomatic(ctx); err != nil {
		slog.Warn("model trace automatic cleanup failed", "error", err)
	}
}

// run 通过单实例闸门执行有限批量删除，并在数据库中留下不含正文的运行摘要。
func (s *CleanupService) run(ctx context.Context, mode CleanupMode, requestedBy *int64) (CleanupResult, error) {
	if s == nil || s.repository == nil {
		return CleanupResult{}, fmt.Errorf("model trace cleanup repository is unavailable")
	}
	if !s.beginRun() {
		return CleanupResult{}, nil
	}
	defer s.endRun()

	cutoff := s.currentTime().UTC()
	runID, err := s.repository.StartCleanupRun(ctx, mode, requestedBy, cutoff)
	if err != nil {
		return CleanupResult{}, fmt.Errorf("start model trace cleanup run: %w", err)
	}
	result := CleanupResult{}
	var runErr error
	for batch := 0; batch < traceCleanupMaxBatches; batch++ {
		deleted, deleteErr := s.repository.DeleteExpired(ctx, cutoff, s.cleanupBatchSize())
		if deleteErr != nil {
			runErr = fmt.Errorf("delete expired model traces: %w", deleteErr)
			break
		}
		result.DeletedTraces += deleted.DeletedTraces
		result.DeletedAttempts += deleted.DeletedAttempts
		result.DeletedPayloads += deleted.DeletedPayloads
		result.DeletedBytes += deleted.DeletedBytes
		if deleted.DeletedTraces < int64(s.cleanupBatchSize()) {
			break
		}
	}
	finishCtx, cancelFinish := cleanupFinishContext(ctx)
	defer cancelFinish()
	if finishErr := s.repository.FinishCleanupRun(finishCtx, runID, result, runErr); finishErr != nil && runErr == nil {
		runErr = fmt.Errorf("finish model trace cleanup run: %w", finishErr)
	}
	return result, runErr
}

// cleanupFinishContext detaches the final audit write from a caller's canceled
// request while preserving values and imposing a short, explicit DB deadline.
func cleanupFinishContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(context.WithoutCancel(ctx), traceCleanupFinishWait)
}

// beginRun 获得进程内的单实例清理闸门，防止定时任务与管理员操作重复删除。
func (s *CleanupService) beginRun() bool {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if s.running {
		return false
	}
	s.running = true
	return true
}

// endRun 释放进程内清理闸门。
func (s *CleanupService) endRun() {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	s.running = false
}

// cleanupBatchSize 返回安全的正数批量，防御测试或未来配置错误。
func (s *CleanupService) cleanupBatchSize() int {
	if s.batchSize <= 0 {
		return traceCleanupBatchSize
	}
	return s.batchSize
}

// currentTime 返回 UTC 计算所依赖的可替换时钟。
func (s *CleanupService) currentTime() time.Time {
	if s.now == nil {
		return time.Now()
	}
	return s.now()
}
