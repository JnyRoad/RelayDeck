# Research: 模型调用追踪

## Decision 1: 独立追踪表，不扩展 `usage_logs`

- **Decision**: 使用独立的调用头、正文和清理运行表；用 `request_id` 关联既有用量记录。
- **Rationale**: 用量表用于成本和聚合，正文属于高体积、高敏感且需独立留存的数据。独立表让列表不读取正文，清理也不会影响计费历史。
- **Alternatives considered**: 在 `usage_logs` 添加请求/响应列（会让统计查询和保留策略耦合）；复用提示词审计（它只覆盖安全扫描输入，且没有通用模型响应）。

## Decision 2: 在模型网关路由组以中间件记录 HTTP 回合

- **Decision**: 在 API Key 鉴权后、路由处理前挂载调用追踪中间件；它读取并恢复可捕获请求体，包装响应写入器，并在 `Next` 返回后写入终态。
- **Rationale**: 一个入口能一致覆盖同步、流式、策略拦截和上游失败，避免把同一逻辑复制到每个网关处理器。流式内容在写出时旁路采集，不等待响应完整后才发送。
- **Alternatives considered**: 在每个 handler 中重复采集（遗漏风险高）；只从用量写入开始追踪（无法记录拦截和失败调用）。

## Decision 3: WebSocket 以每个模型请求轮次单独记录

- **Decision**: HTTP 中间件记录升级前的基础信息；`ResponsesWebSocket` 对每个模型请求轮次显式创建和完成 trace。
- **Rationale**: WebSocket 升级后不再经过 HTTP 响应写入器，且一个连接可含多个模型调用。
- **Alternatives considered**: 把整个连接当一条调用（不能准确定位某一轮）；尝试通过通用 HTTP writer 捕获升级后帧（不可靠）。

## Decision 4: 正文先脱敏、再 AES-GCM 加密，默认关闭

- **Decision**: 复用当前注入的 `service.SecretEncryptor` 保存经过结构化脱敏后的正文；默认只记录调用头，管理员明确开启正文采集后才写入正文。
- **Rationale**: 现有加密器已使用固定的 `TOTP_ENCRYPTION_KEY` 和 AES-256-GCM。关闭时避免敏感数据和存储成本；开启时仍防止凭据落库。
- **Alternatives considered**: 明文正文（不可接受）；新建外部对象存储或密钥服务（本期没有已验证容量与运维授权）。

## Decision 5: 每个正文最多 1 MiB，二进制内容不保存

- **Decision**: 首期采用固定的 1 MiB 保存上限，使用完整、截断、脱敏、不适用和采集失败状态明确表达结果。
- **Rationale**: 保证单一异常请求不会占满进程内存或数据库；对图片、音视频和 Base64 大字段保存元数据与哈希即可支持排障。
- **Alternatives considered**: 无限保存（容量和延迟无法界定）；静默丢弃超限正文（会误导管理员）。

## Decision 6: 留存配置使用独立 settings 键和独立后台服务

- **Decision**: 以 `model_call_trace_config` 保存开关和天数；新清理服务复用现有 cron、Redis 领导锁、分批删除的可靠性模式，但不写入 Ops 清理策略。
- **Rationale**: 调用正文的安全边界、默认值和删除确认不同于系统日志；独立设置减少误删和耦合。
- **Alternatives considered**: 合并至 `ops_advanced_settings`（职责混杂）；仅人工清理（不满足自动清理需求）。

## Decision 7: 首期保存客户端侧完整语义，上游只保存尝试摘要

- **Decision**: 保存客户端请求和实际客户端响应的安全正文；上游保存账户、模型、尝试号、状态和错误摘要，不采集上游原始线报。
- **Rationale**: 这直接满足排查“客户端请求—模型回复”需求，同时避免在众多 provider transport 中引入高风险、易泄密的重复采集逻辑。
- **Alternatives considered**: 逐 provider 捕获上游原始请求和响应（范围显著扩大，需单独的适配器覆盖和安全评审）。

## Resolved Constraints

- 仅模型网关请求在范围内；管理 REST、查询状态和下载内容不触发正文记录。
- 追踪故障不得改变网关对客户端的结果；记录失败状态、结构化日志和指标。
- 正文读取和清理使用现有管理员鉴权；当部署启用了 step-up 时，正文读取、复制和清理额外调用既有 step-up 门控。
