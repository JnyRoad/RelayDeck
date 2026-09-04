package modeltrace

import (
	"database/sql"

	"github.com/JnyRoad/RelayDeck/internal/service"
)

// ProvideSettingsConfigStore 组装唯一的运行时配置快照，供记录、查询配置和清理任务共享。
func ProvideSettingsConfigStore(settings service.SettingRepository) *SettingsConfigStore {
	return NewSettingsConfigStore(settings)
}

// ProvideService 组装生产环境的模型调用追踪记录器，并复用既有 AES 密钥管理。
func ProvideService(configStore *SettingsConfigStore, db *sql.DB, encryptor service.SecretEncryptor) *Service {
	return NewService(configStore, NewPostgresRepository(db), encryptor)
}

// ProvideQueryService 组装管理端按需解密查询服务，不向网关热路径注入查询依赖。
func ProvideQueryService(db *sql.DB, decryptor service.SecretEncryptor) *QueryService {
	return NewQueryService(NewPostgresRepository(db), decryptor)
}

// ProvideCleanupService 组装模型调用追踪的独立保留期清理任务。
func ProvideCleanupService(configStore *SettingsConfigStore, db *sql.DB) *CleanupService {
	return NewCleanupService(configStore, NewPostgresRepository(db))
}
