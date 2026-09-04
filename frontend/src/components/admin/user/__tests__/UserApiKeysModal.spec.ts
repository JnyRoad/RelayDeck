import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import UserApiKeysModal from '../UserApiKeysModal.vue'
import type { AdminUser, ApiKey, CreateApiKeyRequest, UpdateApiKeyRequest } from '@/types'

const {
  createUserApiKey,
  deleteUserApiKey,
  getBatchApiKeysUsage,
  getUserApiKeyAvailableGroups,
  getUserApiKeyGroupRates,
  listUserApiKeys,
  updateUserApiKey,
} = vi.hoisted(() => ({
  createUserApiKey: vi.fn(),
  deleteUserApiKey: vi.fn(),
  getBatchApiKeysUsage: vi.fn(),
  getUserApiKeyAvailableGroups: vi.fn(),
  getUserApiKeyGroupRates: vi.fn(),
  listUserApiKeys: vi.fn(),
  updateUserApiKey: vi.fn(),
}))

vi.mock('@/api/admin/users', () => ({
  createUserApiKey,
  deleteUserApiKey,
  getUserApiKeyAvailableGroups,
  getUserApiKeyGroupRates,
  listUserApiKeys,
  updateUserApiKey,
}))

vi.mock('@/api/admin/dashboard', () => ({
  getBatchApiKeysUsage,
}))

vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key }),
}))

vi.mock('@/components/keys/KeyManagementWorkspace.vue', () => ({
  default: {
    name: 'KeyManagementWorkspace',
    props: ['adapter'],
    template: '<div data-test="key-workspace" />',
  },
}))

// createUser supplies only the fields rendered by the modal header.
const createUser = (id: number): AdminUser => ({
  id,
  email: `user-${id}@example.com`,
  username: `user-${id}`,
} as AdminUser)

// createApiKey is the minimal Key payload used to exercise adapter methods.
const createApiKey = (): ApiKey => ({ id: 31 } as ApiKey)

// mountModal renders the dialog slot and exposes the workspace adapter through a stable stub.
const mountModal = (user = createUser(42)) => mount(UserApiKeysModal, {
  props: { show: true, user },
  global: {
    stubs: {
      BaseDialog: { template: '<div><slot /></div>' },
      KeyManagementWorkspace: {
        name: 'KeyManagementWorkspace',
        props: ['adapter'],
        template: '<div data-test="key-workspace" />',
      },
    },
  },
})

describe('UserApiKeysModal', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  // It binds every full-parity workspace operation to the selected target user.
  it('forwards list, create, update, delete, group, rate and usage operations to one target user', async () => {
    const wrapper = mountModal()
    const adapter = wrapper.findComponent({ name: 'KeyManagementWorkspace' }).props('adapter') as {
      list: (page: number, pageSize: number, filters?: Record<string, unknown>) => Promise<unknown>
      create: (payload: CreateApiKeyRequest) => Promise<unknown>
      update: (keyID: number, updates: UpdateApiKeyRequest) => Promise<unknown>
      delete: (keyID: number) => Promise<unknown>
      getAvailableGroups: () => Promise<unknown>
      getUserGroupRates: () => Promise<unknown>
      getUsageStats: (keyIDs: number[], options?: { signal?: AbortSignal }) => Promise<unknown>
    }

    listUserApiKeys.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20, pages: 1 })
    createUserApiKey.mockResolvedValue(createApiKey())
    updateUserApiKey.mockResolvedValue(createApiKey())
    deleteUserApiKey.mockResolvedValue({ message: 'deleted' })
    getUserApiKeyAvailableGroups.mockResolvedValue([])
    getUserApiKeyGroupRates.mockResolvedValue({})
    getBatchApiKeysUsage.mockResolvedValue({ stats: {} })

    await adapter.list(2, 50, { search: 'assigned', status: 'active' })
    await adapter.create({ name: 'assigned' })
    await adapter.update(31, { reset_quota: true })
    await adapter.delete(31)
    await adapter.getAvailableGroups()
    await adapter.getUserGroupRates()
    const controller = new AbortController()
    await adapter.getUsageStats([31], { signal: controller.signal })

    expect(listUserApiKeys).toHaveBeenCalledWith(42, 2, 50, { search: 'assigned', status: 'active' }, undefined)
    expect(createUserApiKey).toHaveBeenCalledWith(42, { name: 'assigned' })
    expect(updateUserApiKey).toHaveBeenCalledWith(42, 31, { reset_quota: true })
    expect(deleteUserApiKey).toHaveBeenCalledWith(42, 31)
    expect(getUserApiKeyAvailableGroups).toHaveBeenCalledWith(42)
    expect(getUserApiKeyGroupRates).toHaveBeenCalledWith(42)
    expect(getBatchApiKeysUsage).toHaveBeenCalledWith([31], 42, { signal: controller.signal })
  })

  // It rebuilds the adapter when the administrator selects a different user before a request completes.
  it('rebuilds the adapter when the selected user changes', async () => {
    const wrapper = mountModal(createUser(42))
    await wrapper.setProps({ user: createUser(99) })
    const adapter = wrapper.findComponent({ name: 'KeyManagementWorkspace' }).props('adapter') as {
      getAvailableGroups: () => Promise<unknown>
    }

    getUserApiKeyAvailableGroups.mockResolvedValue([])
    await adapter.getAvailableGroups()

    expect(getUserApiKeyAvailableGroups).toHaveBeenCalledWith(99)
  })
})
