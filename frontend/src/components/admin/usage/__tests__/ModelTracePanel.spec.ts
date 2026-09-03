import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import ModelTracePanel from '../ModelTracePanel.vue'

const { list, getDetail, getConfig, updateConfig, previewCleanup, runCleanup } = vi.hoisted(() => ({
  list: vi.fn(),
  getDetail: vi.fn(),
  getConfig: vi.fn(),
  updateConfig: vi.fn(),
  previewCleanup: vi.fn(),
  runCleanup: vi.fn(),
}))

vi.mock('@/api/admin/modelTrace', () => ({
  modelTraceAPI: { list, getDetail, getConfig, updateConfig, previewCleanup, runCleanup },
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

describe('ModelTracePanel', () => {
  beforeEach(() => {
    list.mockReset().mockResolvedValue({ items: [{
      trace_id: 'trace-list', route: '/v1/chat/completions', requested_model: 'gpt-test',
      outcome: 'succeeded', status_code: 200, created_at: '2026-09-03T12:00:00Z',
      request_capture_status: 'redacted', response_capture_status: 'redacted',
    }], total: 1, page: 1, page_size: 20, pages: 1 })
    getConfig.mockReset().mockResolvedValue({ enabled: false, payload_capture_enabled: false, auto_cleanup_enabled: true, retention_days: 7 })
    getDetail.mockReset().mockResolvedValue({
      trace: { trace_id: 'trace-list' },
      payloads: [{ kind: 'client_request', content_status: 'available', content: '{"prompt":"[REDACTED]"}', ciphertext: 'must-not-render' }],
    })
    updateConfig.mockReset()
    previewCleanup.mockReset()
    runCleanup.mockReset()
  })

  it('loads lightweight rows, then reveals only safe detail content on demand', async () => {
    const wrapper = mount(ModelTracePanel)
    await flushPromises()

    expect(list).toHaveBeenCalledWith(expect.objectContaining({ page: 1, page_size: 20 }))
    expect(wrapper.get('[data-testid="model-trace-row-trace-list"]').text()).toContain('gpt-test')

    await wrapper.get('[data-testid="model-trace-row-trace-list"]').trigger('click')
    await flushPromises()

    expect(getDetail).toHaveBeenCalledWith('trace-list')
    expect(wrapper.text()).toContain('[REDACTED]')
    expect(wrapper.text()).not.toContain('must-not-render')
  })

  it('persists the retention and auto-cleanup switches as one explicit config update', async () => {
    updateConfig.mockResolvedValue({ enabled: true, payload_capture_enabled: true, auto_cleanup_enabled: false, retention_days: 14 })
    const wrapper = mount(ModelTracePanel)
    await flushPromises()

    await wrapper.get('[data-testid="model-trace-enabled"]').setValue(true)
    await wrapper.get('[data-testid="model-trace-payload-enabled"]').setValue(true)
    await wrapper.get('[data-testid="model-trace-auto-cleanup"]').setValue(false)
    await wrapper.get('[data-testid="model-trace-retention-days"]').setValue(14)
    await wrapper.get('[data-testid="model-trace-save"]').trigger('click')

    expect(updateConfig).toHaveBeenCalledWith({ enabled: true, payload_capture_enabled: true, auto_cleanup_enabled: false, retention_days: 14 })
  })
})
