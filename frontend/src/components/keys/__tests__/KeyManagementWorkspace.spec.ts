import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'

import KeyManagementWorkspace from '../KeyManagementWorkspace.vue'
import type { KeyManagementAdapter } from '../keyManagementAdapter'

// createAdapter returns a stable adapter identity for composition verification.
const createAdapter = (): KeyManagementAdapter => ({
  list: async () => ({ items: [], total: 0, page: 1, page_size: 20, pages: 1 }),
  create: async () => {
    throw new Error('not used')
  },
  update: async () => {
    throw new Error('not used')
  },
  delete: async () => ({ message: 'deleted' }),
  getAvailableGroups: async () => [],
  getUserGroupRates: async () => ({}),
})

describe('KeyManagementWorkspace', () => {
  // It keeps one interactive KeysView implementation while forcing the embedded presentation for modal hosts.
  it('forwards the supplied target-scoped adapter to the embedded Key workspace', () => {
    const adapter = createAdapter()
    const wrapper = mount(KeyManagementWorkspace, {
      props: { adapter },
      global: {
        stubs: {
          KeysView: {
            name: 'KeysView',
            props: ['adapter', 'embedded'],
            template: '<div data-test="workspace" :data-embedded="String(embedded)" />',
          },
        },
      },
    })

    const keysView = wrapper.findComponent({ name: 'KeysView' })
    const receivedAdapter = keysView.props('adapter') as KeyManagementAdapter
    expect(receivedAdapter.list).toBe(adapter.list)
    expect(receivedAdapter.create).toBe(adapter.create)
    expect(receivedAdapter.update).toBe(adapter.update)
    expect(receivedAdapter.delete).toBe(adapter.delete)
    expect(keysView.props('embedded')).toBe(true)
    expect(wrapper.get('[data-test="workspace"]').attributes('data-embedded')).toBe('true')
  })
})
