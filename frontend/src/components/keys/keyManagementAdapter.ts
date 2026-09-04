import type { BatchApiKeysUsageResponse } from '@/api/usage'
import type {
  ApiKey,
  CreateApiKeyRequest,
  Group,
  PaginatedResponse,
  UpdateApiKeyRequest,
} from '@/types'

// KeyManagementListFilters matches the server-side filters shared by user and administrator Key lists.
export interface KeyManagementListFilters {
  search?: string
  status?: string
  group_id?: number | string
  sort_by?: string
  sort_order?: 'asc' | 'desc'
}

// KeyManagementRequestOptions carries cancellation without exposing an owner-independent request client.
export interface KeyManagementRequestOptions {
  signal?: AbortSignal
}

// KeyManagementAdapter binds every Key operation to exactly one ownership scope.
export interface KeyManagementAdapter {
  list: (
    page: number,
    pageSize: number,
    filters?: KeyManagementListFilters,
    options?: KeyManagementRequestOptions,
  ) => Promise<PaginatedResponse<ApiKey>>
  create: (payload: CreateApiKeyRequest) => Promise<ApiKey>
  update: (keyID: number, updates: UpdateApiKeyRequest) => Promise<ApiKey>
  delete: (keyID: number) => Promise<{ message: string }>
  getAvailableGroups: () => Promise<Group[]>
  getUserGroupRates: () => Promise<Record<number, number>>
  getUsageStats?: (
    apiKeyIDs: number[],
    options?: KeyManagementRequestOptions,
  ) => Promise<BatchApiKeysUsageResponse>
}
