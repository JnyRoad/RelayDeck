# Contract: 管理员目标用户 API Key 管理

All routes require the existing authenticated-admin middleware. Response envelopes and `APIKey` DTOs follow the existing `/api/v1/keys` contract.

## List and lookup data

| Method | Path | Purpose |
|---|---|---|
| GET | `/api/v1/admin/users/{user_id}/api-keys` | Paginated target-user Key list. Supports `page`, `page_size`, `search`, `group_id`, `status`, `sort_by`, `sort_order`. |
| GET | `/api/v1/admin/users/{user_id}/api-keys/available-groups` | Same eligible group set that this target user sees on `/keys`. |
| GET | `/api/v1/admin/users/{user_id}/api-keys/group-rates` | Target user's group-rate map for Key group labels. |

## Mutations

| Method | Path | Request body | Result |
|---|---|---|---|
| POST | `/api/v1/admin/users/{user_id}/api-keys` | Same create fields as `/keys`: `name`, `group_id`, `custom_key`, IP rules, `quota`, `expires_in_days`, 5h/1d/7d limits. | One created target-user `APIKey`. Idempotency key behavior matches `/keys` creation. |
| PUT | `/api/v1/admin/users/{user_id}/api-keys/{key_id}` | Same update fields as `/keys`: name/group/status, IP rules, quota/reset, expires_at, rate limits/reset. | Updated target-user `APIKey`. |
| DELETE | `/api/v1/admin/users/{user_id}/api-keys/{key_id}` | None. | Existing successful deletion envelope. |

For every endpoint containing `{key_id}`, a Key owned by another user must be rejected and no Key value or metadata from it may be returned.
