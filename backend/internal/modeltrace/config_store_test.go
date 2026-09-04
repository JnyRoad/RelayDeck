package modeltrace

import (
	"context"
	"errors"
	"testing"

	"github.com/JnyRoad/RelayDeck/internal/service"
)

// traceSettingsRepositoryStub 以可观测的内存设置仓库替代 PostgreSQL，验证配置存取边界。
type traceSettingsRepositoryStub struct {
	value    string
	getErr   error
	setErr   error
	getCalls int
	setCalls int
}

// GetValue 返回测试预置的模型追踪配置原文或读取错误。
func (s *traceSettingsRepositoryStub) GetValue(context.Context, string) (string, error) {
	s.getCalls++
	return s.value, s.getErr
}

// Set 保存测试中的模型追踪配置原文，并保留调用次数供断言使用。
func (s *traceSettingsRepositoryStub) Set(_ context.Context, _ string, value string) error {
	s.setCalls++
	if s.setErr != nil {
		return s.setErr
	}
	s.value = value
	return nil
}

// TestSettingsConfigStoreDefaultsToDisabled 验证首次部署没有设置记录时保持默认关闭。
func TestSettingsConfigStoreDefaultsToDisabled(t *testing.T) {
	repository := &traceSettingsRepositoryStub{getErr: service.ErrSettingNotFound}
	store := NewSettingsConfigStore(repository)

	config, err := store.Load(context.Background())

	if err != nil {
		t.Fatalf("load default trace config: %v", err)
	}
	if config.Enabled || config.PayloadCaptureEnabled {
		t.Fatalf("default trace config = %#v, want tracing and payload capture disabled", config)
	}
	if config.RetentionDays != DefaultRetentionDays || config.AutoCleanupEnabled {
		t.Fatalf("default trace config = %#v, want %d-day cleanup disabled", config, DefaultRetentionDays)
	}
}

// TestSettingsConfigStoreRejectsUnsafeRetention 验证持久化配置不能绕过保留期上限。
func TestSettingsConfigStoreRejectsUnsafeRetention(t *testing.T) {
	for _, raw := range []string{
		`{"enabled":true,"payload_capture_enabled":true,"auto_cleanup_enabled":true,"retention_days":366}`,
		`{"enabled":true,"payload_capture_enabled":true,"auto_cleanup_enabled":true,"retention_days":0}`,
		`{"enabled":true,"payload_capture_enabled":true,"auto_cleanup_enabled":true,"retention_days":-1}`,
		`{"enabled":true,"payload_capture_enabled":true,"auto_cleanup_enabled":true,"retention_days":1.5}`,
	} {
		store := NewSettingsConfigStore(&traceSettingsRepositoryStub{value: raw})
		if _, err := store.Load(context.Background()); err == nil {
			t.Fatalf("load invalid retention config %s succeeded", raw)
		}
	}
}

// TestSettingsConfigStoreAcceptsMaximumConfiguredRetention verifies that the
// administrator may choose the agreed 365-day upper bound for complete text.
func TestSettingsConfigStoreAcceptsMaximumConfiguredRetention(t *testing.T) {
	repository := &traceSettingsRepositoryStub{value: `{"enabled":true,"payload_capture_enabled":true,"auto_cleanup_enabled":true,"retention_days":365}`}
	store := NewSettingsConfigStore(repository)

	config, err := store.Load(context.Background())

	if err != nil {
		t.Fatalf("load 365-day trace config: %v", err)
	}
	if config.RetentionDays != 365 {
		t.Fatalf("retention_days=%d, want 365", config.RetentionDays)
	}
}

// TestSettingsConfigStoreSaveUpdatesCache 验证管理员保存后下一次读取立即使用新策略。
func TestSettingsConfigStoreSaveUpdatesCache(t *testing.T) {
	repository := &traceSettingsRepositoryStub{getErr: service.ErrSettingNotFound}
	store := NewSettingsConfigStore(repository)
	if _, err := store.Load(context.Background()); err != nil {
		t.Fatalf("load initial trace config: %v", err)
	}

	want := TraceConfig{Enabled: true, PayloadCaptureEnabled: true, AutoCleanupEnabled: false, RetentionDays: 14}
	if err := store.Save(context.Background(), want); err != nil {
		t.Fatalf("save trace config: %v", err)
	}
	got, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("reload saved trace config: %v", err)
	}
	if got != want {
		t.Fatalf("saved trace config = %#v, want %#v", got, want)
	}
	if repository.setCalls != 1 || repository.getCalls != 1 {
		t.Fatalf("settings calls get=%d set=%d, want cached read plus one save", repository.getCalls, repository.setCalls)
	}

	if err := store.Save(context.Background(), TraceConfig{RetentionDays: 0}); err == nil {
		t.Fatal("save invalid trace config succeeded")
	}
	if repository.setCalls != 1 {
		t.Fatalf("invalid config performed %d writes, want 1", repository.setCalls)
	}
}

// TestSettingsConfigStorePropagatesUnavailableStorage 验证不可恢复的设置读取故障不会被误判为已关闭。
func TestSettingsConfigStorePropagatesUnavailableStorage(t *testing.T) {
	repository := &traceSettingsRepositoryStub{getErr: errors.New("settings unavailable")}
	store := NewSettingsConfigStore(repository)

	_, err := store.Load(context.Background())

	if err == nil {
		t.Fatal("storage failure was converted to a trace policy")
	}
}
