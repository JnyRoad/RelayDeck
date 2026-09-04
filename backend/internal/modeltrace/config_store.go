package modeltrace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/JnyRoad/RelayDeck/internal/service"
)

const (
	// SettingKeyModelCallTraceConfig 是持久化模型调用追踪策略的系统设置键。
	SettingKeyModelCallTraceConfig = "model_call_trace_config"
	// DefaultRetentionDays 是首次启用模型调用追踪时采用的默认保留期。
	DefaultRetentionDays = 7
	// maxRetentionDays 限制可通过管理端配置的最长正文保留时间。
	maxRetentionDays = 365
	// traceConfigCacheTTL 控制热路径重新读取系统设置的最短间隔。
	traceConfigCacheTTL = 10 * time.Second
)

// SettingsRepository 是模型调用追踪所需的最小系统设置读写边界。
type SettingsRepository interface {
	GetValue(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key, value string) error
}

// SettingsConfigStore 从系统设置加载模型调用追踪策略，并缓存已验证的快照。
type SettingsConfigStore struct {
	repository SettingsRepository
	now        func() time.Time
	ttl        time.Duration

	mu        sync.Mutex
	cached    TraceConfig
	expiresAt time.Time
	hasCached bool
}

// NewSettingsConfigStore 构建默认关闭且热路径无需逐请求读库的追踪配置存储。
func NewSettingsConfigStore(repository SettingsRepository) *SettingsConfigStore {
	return &SettingsConfigStore{
		repository: repository,
		now:        time.Now,
		ttl:        traceConfigCacheTTL,
	}
}

// DefaultTraceConfig 返回首次部署和缺失设置记录时使用的安全默认策略。
func DefaultTraceConfig() TraceConfig {
	return TraceConfig{
		Enabled:               false,
		PayloadCaptureEnabled: false,
		AutoCleanupEnabled:    false,
		RetentionDays:         DefaultRetentionDays,
	}
}

// Load 返回经校验的缓存策略；缺失设置表示安全默认值，其他读取错误保持可见。
func (s *SettingsConfigStore) Load(ctx context.Context) (TraceConfig, error) {
	if s == nil || s.repository == nil {
		return DefaultTraceConfig(), nil
	}

	now := s.currentTime()
	s.mu.Lock()
	if s.hasCached && now.Before(s.expiresAt) {
		cached := s.cached
		s.mu.Unlock()
		return cached, nil
	}
	s.mu.Unlock()

	raw, err := s.repository.GetValue(ctx, SettingKeyModelCallTraceConfig)
	if errors.Is(err, service.ErrSettingNotFound) {
		config := DefaultTraceConfig()
		s.cache(config, now)
		return config, nil
	}
	if err != nil {
		return TraceConfig{}, fmt.Errorf("read model trace setting: %w", err)
	}
	config, err := ParseTraceConfig(raw)
	if err != nil {
		return TraceConfig{}, err
	}
	s.cache(config, now)
	return config, nil
}

// Save 校验并原子替换模型调用追踪策略，同时立即更新当前进程的缓存快照。
func (s *SettingsConfigStore) Save(ctx context.Context, config TraceConfig) error {
	if s == nil || s.repository == nil {
		return fmt.Errorf("model trace settings repository is unavailable")
	}
	if err := ValidateTraceConfig(config); err != nil {
		return err
	}
	raw, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("encode model trace config: %w", err)
	}
	if err := s.repository.Set(ctx, SettingKeyModelCallTraceConfig, string(raw)); err != nil {
		return fmt.Errorf("save model trace setting: %w", err)
	}
	s.cache(config, s.currentTime())
	return nil
}

// ParseTraceConfig 解码已保存的模型调用追踪策略，并拒绝不完整或越界的保留期。
func ParseTraceConfig(raw string) (TraceConfig, error) {
	if raw == "" {
		return DefaultTraceConfig(), nil
	}
	var config TraceConfig
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		return TraceConfig{}, fmt.Errorf("decode model trace config: %w", err)
	}
	if err := ValidateTraceConfig(config); err != nil {
		return TraceConfig{}, err
	}
	return config, nil
}

// ValidateTraceConfig 将正文存储保留期限制在产品确认的 1 至 365 天范围内。
func ValidateTraceConfig(config TraceConfig) error {
	if config.RetentionDays < 1 || config.RetentionDays > maxRetentionDays {
		return fmt.Errorf("model trace retention days must be between 1 and %d", maxRetentionDays)
	}
	return nil
}

// cache 用互斥锁发布一个完整配置快照，避免热路径读取到部分更新状态。
func (s *SettingsConfigStore) cache(config TraceConfig, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cached = config
	s.expiresAt = now.Add(s.ttl)
	s.hasCached = true
}

// currentTime 提供可注入时钟，供缓存边界测试和生产逻辑共用。
func (s *SettingsConfigStore) currentTime() time.Time {
	if s.now == nil {
		return time.Now()
	}
	return s.now()
}
