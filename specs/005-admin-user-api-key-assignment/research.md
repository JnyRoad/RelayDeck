# Research: 目标用户 API 密钥控制台

## Decision 1: 复用既有 API Key 领域服务，不复制业务规则

- `APIKeyService.Create(ctx, userID, request)` 已按传入的 `userID` 校验用户、分组、订阅、IP、限额并生成或验证 Key。
- `APIKeyService.Update(ctx, keyID, userID, request)` 与 `Delete(ctx, keyID, userID)` 已验证 Key 所有者，并负责认证缓存与限流缓存失效。
- 管理员 HTTP 层将目标用户 ID 传入这些方法；管理员身份只决定能否进入该路由，不取代 Key 所有权校验。

**Reason**: 这样管理员入口与 `/keys` 使用同一业务语义，且每次变更均同时验证 Key 与目标用户的归属。

## Decision 2: 新增显式的目标用户 API Key 路由

新增目标用户资源下的 GET、POST、PUT、DELETE 与目标用户可用分组/倍率读取接口。不会调用当前 `/keys` 路由，因为该路由从登录会话读取管理员自己的用户 ID。

**Reason**: URI 中的 `user_id` 是目标边界，处理器与领域服务都要验证它；不通过伪造用户会话实现代管。

## Decision 3: 从 `/keys` 提取可复用工作区

`KeysView.vue` 的表格、筛选、编辑表单、确认框、使用指引与 CCS 导入都迁移到一个无页面导航的 `KeyManagementWorkspace`。该组件通过一个明确的 Key 管理适配器执行列表和变更；`KeysView` 提供当前用户适配器，`UserApiKeysModal` 提供目标用户管理员适配器。

**Reason**: 保证功能和交互天然保持一致，避免维护两份 Key 管理页面，也让后续 `/keys` 新功能默认可以复用。

## Decision 4: 保持掩码展示，复制时使用完整值

列表继续调用既有 `maskApiKey` 呈现 Key。适配器返回的完整值只传给既有复制、使用指引与 CCS 导入流程；不显示在通知、错误消息或审计正文中。

**Reason**: 这是当前 `/keys` 的实际交互，既满足管理员交付凭据，也避免在页面上持续暴露完整 Key。

## Rejected Alternatives

- **在 `UserApiKeysModal.vue` 复制 `/keys` 全部代码**：会形成两份独立的约 1800 行 Key 管理逻辑，未来容易产生功能差异。
- **管理员调用 `/keys` 并伪造目标用户身份**：破坏认证语义，无法安全审计，也无法可靠限制目标范围。
- **只新增创建、复制、删除按钮**：不能满足用户要求的 `/keys` 全功能对等。
