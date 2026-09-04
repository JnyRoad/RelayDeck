# Verification Quickstart: 目标用户 API 密钥控制台

## Focused backend tests

```bash
cd backend
go test ./internal/handler/admin -run 'TestAdminUserAPIKey' -count=1
```

## Focused frontend tests

```bash
cd frontend
pnpm test:run src/components/keys/__tests__/KeyManagementWorkspace.spec.ts src/components/admin/user/__tests__/UserApiKeysModal.spec.ts
```

## Regression gates

```bash
cd backend
go test ./internal/handler/admin ./internal/handler ./internal/service -count=1

cd ../frontend
pnpm typecheck
pnpm test:run src/views/user/__tests__/KeysView.spec.ts src/components/keys/__tests__/KeyManagementWorkspace.spec.ts src/components/admin/user/__tests__/UserApiKeysModal.spec.ts
pnpm build
```

## Manual acceptance

1. In user management, open two different users' API-key modal windows one after another.
2. Verify filtering, pagination, editing, enabling/disabling, reset actions, use-key guidance, CCS import, copying and deletion match `/keys` for the selected user.
3. Attempt to address a Key owned by the other user through the administrator endpoint; confirm that it is rejected and no Key data is shown.
4. Confirm that the list stays masked while copy puts the complete Key into the clipboard.
