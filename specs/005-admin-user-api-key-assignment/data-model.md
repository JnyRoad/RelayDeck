# Data Model: 目标用户 API 密钥控制台

本功能不新增数据库表或字段，复用已有 `api_keys` 与管理员操作审计记录。

## Existing Entities

| Entity | Required fields | Use in this feature |
|---|---|---|
| User | `id`, status, allowed groups, subscriptions | 每个管理员请求的明确目标；领域服务用它验证可绑定分组。 |
| APIKey | `id`, `user_id`, `key`, configuration, quota/rate usage, status | 模态框管理对象；所有变更必须同时满足 `key.user_id == target user_id`。 |
| Admin audit record | actor, action, target, timestamp, non-secret metadata | 记录管理员读写目标用户 Key 的动作，不保存 `key` 字段全文。 |

## Request Boundary

```text
authenticated administrator
  -> target user id in URL
  -> target Key id in URL for mutations
  -> APIKeyService(..., target user id, ...)
  -> verify APIKey.user_id == target user id
  -> batch usage request carries target_user_id
  -> usage query filters both api_key_id and user_id
```

No migration is required. A missing or deleted target user cannot be read or changed. A disabled target user may be inspected, but cannot create, edit, enable, disable, reset or delete a Key. A Key associated with another user must be indistinguishable from an inaccessible record to the requester and must never be returned, mutated or included in target-scoped usage statistics.
